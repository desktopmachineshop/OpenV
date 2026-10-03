// Command movecheck shows where a package's declarations live, and how a
// move changed that; with -flatten it inlines a function split into stages
// (refactor class B, M4) back into one statement list, and fails on the
// stage shapes that would change when statements run.
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
// does a stage with a value receiver (its field writes would go to a
// copy), and a stage that contains defer, recover or a return other than
// its last statement, outside function literals: those would run when the
// stage returns rather than when the function does.
//
// With -base it is the proof of a stage split (refactor step M4, made by
// internal/tools/stageextract): it flattens the function at the ref and on
// the head side, undoes on both what stageextract rewrites (normalise.go,
// S14c): the recv := &T{} line goes, recv.x reads as x for a field x of T,
// the first top-level write of a field reads as its :=, a var a stage
// declares a local with just before writing it goes, a var of a field
// goes or reads as :=, and a cleanup a stage returns and the function
// binds and defers at once reads as the defer it was. Then it compares
// the statements and what each identifier names (a package-level name, a
// top-level local or field, a local of a nested block, the receiver),
// and fails, with the diff, on any difference left: a statement moved
// across a stage boundary, dropped or changed, or a name that now binds
// otherwise. It also fails what equal text would not show: a top-level
// local of a stage named like a field; a local declared at the top of two
// of the function's bodies (its own and its stages') that a function
// literal captures, whose address is taken (explicitly, or by a pointer
// method or slicing), or whose copy one body reads after another body
// wrote its own (main()'s err read in the tail after a stage's err); and
// a field, or a top-level local, whose type is not the base's local's.
// For the last two it type-checks both sides, against the packages they
// import as the go command builds them here (normalise_types.go).
//
// Exit status: 0 on success (with -base, when both sides flatten to the
// same statements once normalised), 1 when -flatten finds a violation or
// -base a difference, 2 on a usage or read error, when -flatten -base
// cannot type-check a side, or when the directory holds no declaration on
// any side (a mistyped path, or a ./... pattern, which is not expanded).
package main

import (
	"flag"
	"fmt"
	"go/ast"
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
		return flattenCmd(load, dir, *flat, *recv, *base, *head, stdout, stderr)
	}
	h, err := load(*head, *tests)
	if err != nil {
		fmt.Fprintln(stderr, "movecheck:", err)
		return 2
	}
	noDecls := fmt.Sprintf("movecheck: %s holds no Go declarations; name a package directory (./... is not expanded)\n", dir)
	if *base == "" {
		if len(h.decls) == 0 {
			fmt.Fprint(stderr, noDecls)
			return 2
		}
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
	if len(b.decls)+len(h.decls) == 0 {
		fmt.Fprint(stderr, noDecls)
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

// flattenCmd prints the flattened function, or with base compares it with
// the base's, both normalised (normalise.go), and fails on a difference.
func flattenCmd(load func(string, bool) (*pkgInfo, error), dir, name, recv, base, head string, stdout, stderr io.Writer) int {
	side := func(ref string) (*flattener, []ast.Stmt, int) {
		p, err := load(ref, false)
		if err != nil {
			fmt.Fprintln(stderr, "movecheck:", err)
			return nil, nil, 2
		}
		fl, list, err := flattenStmts(p, name, recv)
		if err != nil {
			fmt.Fprintln(stderr, "movecheck:", err)
			return nil, nil, 1
		}
		return fl, list, 0
	}
	hfl, hlist, code := side(head)
	if code != 0 {
		return code
	}
	if base == "" {
		for _, l := range strings.Split(strings.Join(hfl.renderAll(hlist), "\n"), "\n") {
			fmt.Fprintln(stdout, l)
		}
		return 0
	}
	bfl, blist, code := side(base)
	if code != 0 {
		return code
	}
	headName := head
	if headName == "" {
		headName = "the working tree"
	}
	hs, bs := newSide(hfl, hlist), newSide(bfl, blist)
	fields := map[string]bool{}
	for f := range hs.fields {
		fields[f] = true
	}
	for f := range bs.fields {
		fields[f] = true
	}
	h, err := hs.normalise(fields)
	if err != nil {
		fmt.Fprintf(stderr, "movecheck: %s: %v\n", headName, err)
		return 1
	}
	b, err := bs.normalise(fields)
	if err != nil {
		fmt.Fprintf(stderr, "movecheck: %s: %v\n", base, err)
		return 1
	}
	if d := unifiedDiff(b, h, base, headName); len(d) > 0 {
		for _, l := range d {
			fmt.Fprintln(stdout, l)
		}
		fmt.Fprintf(stderr, "movecheck: %s flattens to other statements in %s than in %s (the diff above, both sides "+
			"normalised): only a local that became a field of the stages' receiver (%s.x), its := and var forms, and a "+
			"cleanup a stage returns for %s to defer may differ; anything else is a change\n", name, headName, base, recv, name)
		return 1
	}
	if msg := bindingMismatch(bs, hs, base, headName); msg != "" {
		fmt.Fprintln(stderr, "movecheck:", msg)
		return 1
	}
	msg, err := typedChecks(dir, bfl.p, hfl.p, base, headName, name, recv)
	if err != nil {
		fmt.Fprintln(stderr, "movecheck:", err)
		return 2
	}
	if msg != "" {
		fmt.Fprintln(stderr, "movecheck: "+strings.ReplaceAll(msg, "\n", "\nmovecheck: "))
		return 1
	}
	renamed := len(hs.renamed) + len(bs.renamed)
	cleanups := hs.cleanups + bs.cleanups
	if renamed+cleanups == 0 {
		fmt.Fprintf(stdout, "movecheck: %s flattens to the same statements in %s and %s\n", name, base, headName)
		return 0
	}
	fmt.Fprintf(stdout, "movecheck: %s flattens to the same statements in %s and %s once the stage rewrites are undone (%s, %s)\n",
		name, base, headName, count(renamed, "local became a field of "+recv, "locals became fields of "+recv),
		count(cleanups, "cleanup returned for "+name+" to defer", "cleanups returned for "+name+" to defer"))
	return 0
}

// count is "1 thing" or "n things".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// bindingMismatch compares what the identifiers of two normalised sides
// name, which equal text alone does not show, and describes the first
// that differs.
func bindingMismatch(base, head *side, baseName, headName string) string {
	b, h := base.bindings(), head.bindings()
	for i := 0; i < len(b) && i < len(h); i++ {
		if b[i].name != h[i].name || b[i].bind != h[i].bind {
			line := strings.SplitN(head.fl.render(head.list[h[i].stmt]), "\n", 2)[0]
			return fmt.Sprintf("%s: in %q, %s names %s in %s but %s in %s", head.fl.p.fset.Position(h[i].pos), line,
				h[i].name, h[i].bind, headName, b[i].bind, baseName)
		}
	}
	if len(b) != len(h) {
		return fmt.Sprintf("the normalised sides read alike but name %d identifiers in %s and %d in %s", len(b), baseName, len(h), headName)
	}
	return ""
}
