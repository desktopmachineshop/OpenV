// Command declhash proves a pure move (refactor class A): it hashes every
// package-level declaration of Go packages, so that moving declarations
// between the files of a package leaves the manifest unchanged and any
// other edit changes it.
//
//	go run ./internal/tools/declhash [-o manifest] [-tests=false] <pkg dir>...
//	go run ./internal/tools/declhash -compare base.txt head.txt
//	go run ./internal/tools/declhash -base <git-ref> [-head <git-ref>] <pkg dir>...
//
// Each function, method ((*T).Name or T.Name), type, var and const is one
// entry; a grouped spec is split into one entry per name, and a const keeps
// the type and expression it gets by implicit repetition, and its iota when
// the expression uses iota. An entry's hash covers its go/printer rendering
// with its doc and inner comments, its file's build constraints, and the
// import path of each package qualifier it uses. File names and import
// blocks are not part of it. The manifest is sorted
// "package<TAB>declaration<TAB>sha256" lines; a _test.go file's
// declarations are listed under "package (test)" or "package (xtest)".
//
// -compare and -base report every added, removed or changed declaration and
// exit 1 on any difference. -base reads the packages at a git ref (without
// touching the working tree) and compares them with the working tree, or
// with -head's ref. Exit status 2 means a usage or read error.
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
	fs := flag.NewFlagSet("declhash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write the manifest to this `file` instead of standard output")
	compare := fs.Bool("compare", false, "compare two manifest files given as arguments: base then head")
	base := fs.String("base", "", "compare the packages at this git `ref` with the head side")
	head := fs.String("head", "", "with -base, read the head side at this git `ref` instead of the working tree")
	tests := fs.Bool("tests", true, "include _test.go files")
	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: declhash [-o manifest] [-tests=false] <pkg dir>...\n"+
			"       declhash -compare base.txt head.txt\n"+
			"       declhash -base <git-ref> [-head <git-ref>] <pkg dir>...\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch {
	case *compare:
		if fs.NArg() != 2 || *base != "" {
			fs.Usage()
			return 2
		}
		return compareFiles(fs.Arg(0), fs.Arg(1), stdout, stderr)
	case fs.NArg() == 0 || (*head != "" && *base == ""):
		fs.Usage()
		return 2
	case *base != "":
		return compareRefs(*base, *head, fs.Args(), *tests, stdout, stderr)
	}
	lines, err := manifestAt("", fs.Args(), *tests)
	if err != nil {
		fmt.Fprintln(stderr, "declhash:", err)
		return 2
	}
	text := strings.Join(lines, "\n") + "\n"
	if *out == "" {
		fmt.Fprint(stdout, text)
		return 0
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		fmt.Fprintln(stderr, "declhash:", err)
		return 2
	}
	return 0
}

// manifestAt builds the manifest of the package directories, read from the
// working tree when ref is empty and from git otherwise.
func manifestAt(ref string, dirs []string, tests bool) ([]string, error) {
	var lines []string
	for _, dir := range dirs {
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
		p, err := parsePackage(srcs, tests)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		lines = append(lines, p.manifest(moduleLabel(dir))...)
	}
	sort.Strings(lines)
	return lines, nil
}

func compareRefs(base, head string, dirs []string, tests bool, stdout, stderr io.Writer) int {
	b, err := manifestAt(base, dirs, tests)
	if err != nil {
		fmt.Fprintln(stderr, "declhash:", err)
		return 2
	}
	h, err := manifestAt(head, dirs, tests)
	if err != nil {
		fmt.Fprintln(stderr, "declhash:", err)
		return 2
	}
	headName := head
	if headName == "" {
		headName = "the working tree"
	}
	return report(b, h, fmt.Sprintf("%s and %s", base, headName), stdout)
}

func compareFiles(basePath, headPath string, stdout, stderr io.Writer) int {
	var sides [2][]string
	for i, p := range []string{basePath, headPath} {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(stderr, "declhash:", err)
			return 2
		}
		for n, line := range strings.Split(string(b), "\n") {
			if line == "" {
				continue
			}
			if strings.Count(line, "\t") != 2 {
				fmt.Fprintf(stderr, "declhash: %s:%d: not a manifest line\n", p, n+1)
				return 2
			}
			sides[i] = append(sides[i], line)
		}
	}
	return report(sides[0], sides[1], basePath+" and "+headPath, stdout)
}

// report prints each declaration that differs between the two manifests and
// returns the exit status: 0 when they are equal, 1 otherwise.
func report(base, head []string, what string, w io.Writer) int {
	diffs, total := diffManifests(base, head)
	for _, d := range diffs {
		fmt.Fprintln(w, d)
	}
	if len(diffs) == 0 {
		fmt.Fprintf(w, "declhash: %d declarations identical in %s\n", total, what)
		return 0
	}
	fmt.Fprintf(w, "declhash: %d of %d declarations differ between %s\n", len(diffs), total, what)
	return 1
}

// diffManifests compares two manifests entry by entry. A key may occur more
// than once (init, _, or build-tagged variants); its hashes are then
// compared as a multiset. Each difference is "added", "removed" or
// "changed", a tab, the package, a tab and the declaration.
func diffManifests(base, head []string) (diffs []string, total int) {
	index := func(lines []string) map[string][]string {
		m := map[string][]string{}
		for _, l := range lines {
			i := strings.LastIndexByte(l, '\t')
			m[l[:i]] = append(m[l[:i]], l[i+1:])
		}
		for _, hs := range m {
			sort.Strings(hs)
		}
		return m
	}
	b, h := index(base), index(head)
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range h {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		bh, hh := b[k], h[k]
		switch {
		case len(bh) == 0:
			diffs = append(diffs, "added\t"+k)
		case len(hh) == 0:
			diffs = append(diffs, "removed\t"+k)
		case strings.Join(bh, ",") != strings.Join(hh, ","):
			diffs = append(diffs, "changed\t"+k)
		}
	}
	return diffs, len(sorted)
}
