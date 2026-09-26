// Command movecheck shows where a package's declarations live, and how a
// move changed that; with -flatten it proves that a function split into
// stages (refactor class B, M4) still runs the same statements.
//
//	go run ./internal/tools/movecheck [-tests] [-head <ref>] <pkg dir>
//	go run ./internal/tools/movecheck [-tests] -base <ref> [-head <ref>] <pkg dir>
//	go run ./internal/tools/movecheck -flatten <func> [-recv a] [-base <ref>] [-head <ref>] <pkg dir>
//
// The first form prints the declaration-to-file map, "key -> file.go", in
// file and source order; keys are declhash's (Name, (*T).Name, T.Name).
// With -base it prints only the declarations whose file differs between
// the ref and the head side, "key: old.go -> new.go", which is the map a
// move PR's description carries. The head side is the working tree, or
// -head's ref; refs are read with git, leaving the working tree alone.
//
// -flatten prints the named function's statements with every stage call
// inlined: a statement recv.stage() or x, y := recv.stage(), where stage is
// a method declared in a wire_*.go file of the package, is replaced by the
// stage's body, and an assignment takes the expressions of the stage's
// final return. Stage calls nested in blocks are inlined too; any other
// stage call (in defer, go, a function literal or an expression) fails. So
// does a stage that contains defer, recover or a return other than its
// last statement, outside function literals: those would run when the
// stage returns rather than when the function does. With -base it prints a
// diff of the flattened statements at the ref against the head side.
//
// Exit status: 0 on success, 1 when -flatten finds a violation, 2 on a
// usage or read error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("movecheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("base", "", "compare with the package at this git `ref`")
	head := fs.String("head", "", "read the head side at this git `ref` instead of the working tree")
	tests := fs.Bool("tests", false, "include _test.go files in the map")
	flat := fs.String("flatten", "", "print this `function` with its stage calls inlined")
	recv := fs.String("recv", "a", "with -flatten, the `name` stage methods are called on")
	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: movecheck [-tests] [-base <ref>] [-head <ref>] <pkg dir>\n"+
			"       movecheck -flatten <func> [-recv a] [-base <ref>] [-head <ref>] <pkg dir>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	dir := fs.Arg(0)
	load := func(ref string, tests bool) (*pkgInfo, error) {
		var srcs []source
		var err error
		if ref == "" {
			srcs, err = readDir(dir)
		} else {
			srcs, err = readGitDir(ref, dir)
		}
		if err != nil {
			return nil, err
		}
		return parsePackage(srcs, tests)
	}
	if *flat != "" {
		return flattenCmd(load, *flat, *recv, *base, *head, stdout, stderr)
	}
	h, err := load(*head, *tests)
	if err != nil {
		fmt.Fprintln(stderr, "movecheck:", err)
		return 2
	}
	if *base == "" {
		for _, d := range h.decls {
			fmt.Fprintf(stdout, "%s -> %s\n", d.key, d.file.name)
		}
		return 0
	}
	b, err := load(*base, *tests)
	if err != nil {
		fmt.Fprintln(stderr, "movecheck:", err)
		return 2
	}
	lines, total := moved(b, h)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "movecheck: %d of %d declarations changed file\n", len(lines), total)
	return 0
}

// moved lists each declaration whose files differ between two versions of
// a package, as "key: before -> after", ordered by the new file. A key
// declared more than once lists all its files; a missing side is "(none)".
func moved(base, head *pkgInfo) (lines []string, total int) {
	where := func(p *pkgInfo) map[string][]string {
		m := map[string][]string{}
		for _, d := range p.decls {
			m[d.key] = append(m[d.key], d.file.name)
		}
		for _, fs := range m {
			sort.Strings(fs)
		}
		return m
	}
	b, h := where(base), where(head)
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range h {
		keys[k] = true
	}
	type move struct{ key, from, to string }
	var moves []move
	for k := range keys {
		from, to := files(b[k]), files(h[k])
		if from != to {
			moves = append(moves, move{k, from, to})
		}
	}
	sort.Slice(moves, func(i, j int) bool {
		if moves[i].to != moves[j].to {
			return moves[i].to < moves[j].to
		}
		return moves[i].key < moves[j].key
	})
	for _, m := range moves {
		lines = append(lines, fmt.Sprintf("%s: %s -> %s", m.key, m.from, m.to))
	}
	return lines, len(keys)
}

func files(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// flattenCmd prints the flattened function, or with base a diff of it.
func flattenCmd(load func(string, bool) (*pkgInfo, error), name, recv, base, head string, stdout, stderr io.Writer) int {
	side := func(ref string) ([]string, int) {
		p, err := load(ref, false)
		if err != nil {
			fmt.Fprintln(stderr, "movecheck:", err)
			return nil, 2
		}
		stmts, err := flatten(p, name, recv)
		if err != nil {
			fmt.Fprintln(stderr, "movecheck:", err)
			return nil, 1
		}
		return strings.Split(strings.Join(stmts, "\n"), "\n"), 0
	}
	h, code := side(head)
	if code != 0 {
		return code
	}
	if base == "" {
		for _, l := range h {
			fmt.Fprintln(stdout, l)
		}
		return 0
	}
	b, code := side(base)
	if code != 0 {
		return code
	}
	headName := head
	if headName == "" {
		headName = "the working tree"
	}
	d := unifiedDiff(b, h, base, headName)
	if len(d) == 0 {
		fmt.Fprintf(stdout, "movecheck: %s flattens to the same statements in %s and %s\n", name, base, headName)
		return 0
	}
	for _, l := range d {
		fmt.Fprintln(stdout, l)
	}
	return 0
}
