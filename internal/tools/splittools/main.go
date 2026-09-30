// Command splittools generates refactor step M11a (class B, rule R4): the
// entries of the MCP tool table, the []Tool literal that internal/mcp's
// Tools() returns, move into per-area constructors in new tools_<area>.go
// files, as a committed spec says, and Tools() returns their
// concatenation, in today's order.
//
//	go run ./internal/tools/splittools [-n] [-vet=false] -spec internal/tools/splittools/specs/M11a.json
//	go run ./internal/tools/splittools -check -base <ref> [-head <ref>] [-func Tools] <pkg dir>
//
// Each entry moves as the whole lines it occupies, with the comment lines
// above it, into a constructor at the literal's own depth, so the moved
// lines are byte for byte the old ones and gofmt leaves them alone; each
// new file imports what its entries use, named as the old file names it,
// and the old file drops the imports only the moved entries used. The spec
// keys tools by Name: a tool it does not name joins the constructor of the
// tool before it, and a name the table no longer has is dropped, each with
// a note, so the split can be regenerated on the latest master; a spec
// whose order the concatenation could not reproduce is refused.
//
// splittools refuses what it could not move whole or would have to guess
// (a comment in Tools() outside its entries, two entries on one line, an
// entry without a Name string, a dot import, a table file with build
// constraints, a constructor name the package already declares, an
// existing target file, a file name with a GOOS or GOARCH suffix), and a
// constructor over K14's 100-line function budget; each such refusal is
// the spec's to settle (errPlan). Then it checks its own result before
// writing it: Tools() flattened through the constructors lists the same
// entries in the same order, each rendered identically with its comments;
// every other declaration of the package, with the import paths its
// qualifiers resolve to, is unchanged; and each constructor's span and each
// new file's length, measured as internal/archtest measures them, stay
// within K14's 100 and 800 lines. After writing it runs go vet on the
// package, and restores every file if that fails.
//
// -check compares the package at a git ref with the working tree (or
// -head's ref) with the self-check's comparisons (its size checks are
// internal/archtest's in CI), and prints the tool-to-constructor map of the
// head side: the proof a reviewer of M11a's class B commit reads. It exits
// 1 on any entry of Tools() added, dropped, reordered or changed, whichever
// shape each side has; on any other declaration of the package added,
// dropped or changed, matched by name, so that a declaration moved to
// another file (M11a's class A commit) is no change; and on Tools()'s doc
// comment or signature changed.
//
// Exit status: 0 on success, 1 when splittools refuses the split, its
// self-check fails or -check finds a difference, 2 on a usage, spec or read
// error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// K14's budgets (internal/archtest/size_test.go): lines per function,
// "func" line to closing brace, and per non-test file.
const (
	funcBudget = 100
	fileBudget = 800
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("splittools", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specPath := fs.String("spec", "", "the split spec, a JSON `file`")
	dryRun := fs.Bool("n", false, "print the split and write nothing")
	vet := fs.Bool("vet", true, "run go vet on the package after writing it")
	check := fs.Bool("check", false, "compare the table at -base with the head side")
	base := fs.String("base", "", "with -check, the git `ref` to compare with")
	head := fs.String("head", "", "with -check, read the head side at this git `ref` instead of the working tree")
	fn := fs.String("func", "Tools", "with -check, the `function` whose table is compared")
	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: splittools [-n] [-vet=false] -spec <spec.json>\n"+
			"       splittools -check -base <ref> [-head <ref>] [-func Tools] <pkg dir>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *check {
		if *base == "" || fs.NArg() != 1 || *specPath != "" {
			fs.Usage()
			return 2
		}
		return checkCmd(fs.Arg(0), *fn, *base, *head, stdout, stderr)
	}
	if *specPath == "" || fs.NArg() != 0 || *base != "" || *head != "" {
		fs.Usage()
		return 2
	}
	sp, err := loadSpec(*specPath)
	if err != nil {
		fmt.Fprintln(stderr, "splittools:", err)
		return 2
	}
	dir := packageDir(sp.Package)
	opts := options{dryRun: *dryRun, maxFunc: funcBudget, maxFile: fileBudget}
	if *vet {
		opts.vet = goVet
	}
	res, err := generate(dir, sp, opts)
	if res != nil {
		for _, l := range res.lines {
			fmt.Fprintln(stdout, l)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "splittools:", err)
		if errors.Is(err, errNoSource) {
			return 2
		}
		return 1
	}
	return 0
}

// errNoSource means the spec's package directory could not be read or holds
// no Go file: a mistyped path, not a split to refuse.
var errNoSource = errors.New("no Go source")

// errPlan marks a refusal that the spec or the table's shape causes, not a
// fault of the split: a tool the spec's constructors cannot take within
// K14's budgets, an order it cannot reproduce, a name or file that exists,
// a shape splittools cannot move whole. The spec is edited to settle one
// (rule R4: M11a's author regenerates the split on the latest master), so
// TestM11aOnTheWorkingTree skips on it instead of failing a feature pull
// request (§8.4); a self-check failure is never marked.
var errPlan = errors.New("refused by the spec or the table's shape")

// planError marks err with errPlan and keeps its message.
type planError struct{ error }

func (e planError) Unwrap() error        { return e.error }
func (e planError) Is(target error) bool { return target == errPlan }

// refuse marks a refusal with errPlan.
func refuse(err error) error {
	if err == nil {
		return nil
	}
	return planError{err}
}

// packageDir resolves a spec's package against the module root holding the
// working directory; an absolute path is used as it is.
func packageDir(pkg string) string {
	if filepath.IsAbs(pkg) {
		return pkg
	}
	if root, err := moduleRoot("."); err == nil {
		return filepath.Join(root, filepath.FromSlash(pkg))
	}
	return pkg
}

// moduleRoot is the directory holding the go.mod above dir.
func moduleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no go.mod above %s", abs)
		}
		d = parent
	}
}

// moduleLabel names a package directory by its path from its module root,
// or as given outside a module.
func moduleLabel(dir string) string {
	root, err := moduleRoot(dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	return filepath.ToSlash(rel)
}

// goVet runs go vet on the package directory from its module root.
func goVet(dir string) error {
	root, err := moduleRoot(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "vet", "./"+filepath.ToSlash(rel))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go vet ./%s: %v\n%s", filepath.ToSlash(rel), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// options says how generate runs; vet is nil to skip that step.
type options struct {
	dryRun           bool
	maxFunc, maxFile int
	vet              func(dir string) error
}

// result is what generate did or would do: the map, notes and summary,
// as printed.
type result struct {
	lines []string
}

// generate plans the spec's split of the package in dir, renders it, checks
// the rendering, and (unless dry-running) writes it and runs go vet,
// restoring every file if any step fails.
func generate(dir string, sp *spec, opts options) (*result, error) {
	srcs, err := readDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNoSource, err)
	}
	if len(srcs) == 0 {
		return nil, fmt.Errorf("%w: %s holds no Go files; the spec's package is a directory relative to the module root", errNoSource, dir)
	}
	p, err := parsePkg(srcs)
	if err != nil {
		return nil, err
	}
	t, err := p.readTable(sp.funcName())
	if err != nil {
		return nil, refuse(err)
	}
	if t.file.dots > 0 {
		return nil, refuse(fmt.Errorf("%s has a dot import, so splittools cannot tell which import an entry uses", t.file.name))
	}
	if c := buildConstraint(t.file); c != "" {
		return nil, refuse(fmt.Errorf("%s is built only %s; the new files would be built everywhere, so splittools refuses", t.file.name, c))
	}
	plans, notes, err := planSplit(sp, t, opts.maxFunc)
	if err != nil {
		return nil, refuse(err)
	}
	if err := checkNames(dir, p, sp, plans); err != nil {
		return nil, refuse(err)
	}
	res := &result{lines: mapLines(plans)}
	for _, n := range notes {
		res.lines = append(res.lines, "splittools: note: "+n)
	}
	files, order, err := render(p, t, sp, plans)
	if err != nil {
		return res, err
	}
	if err := selfCheck(p, t, sp, plans, files, opts.maxFunc, opts.maxFile); err != nil {
		return res, fmt.Errorf("self-check: %w; nothing written", err)
	}
	label := moduleLabel(dir)
	res.lines = append(res.lines, fmt.Sprintf("splittools: %d entries of %s's %s() into %d constructors in %d new files, in the same order; "+
		"each entry and every other declaration unchanged", len(t.entries), label, sp.funcName(), len(plans), len(order)),
		"splittools: "+sizeLine(plans, opts.maxFunc))
	if opts.dryRun {
		res.lines = append(res.lines, "splittools: dry run; nothing written")
		return res, nil
	}
	written := append([]string{t.file.name}, order...)
	if err := writeAll(dir, files, written, t.file); err != nil {
		return res, err
	}
	if opts.vet != nil {
		if err := opts.vet(dir); err != nil {
			restore(dir, written, t.file)
			return res, fmt.Errorf("%w; every file is restored", err)
		}
		res.lines = append(res.lines, "splittools: go vet ./"+label+" passed")
	}
	res.lines = append(res.lines, "splittools: next, tighten the ratchets the split leaves loose: "+
		"UPDATE_RATCHETS=1 go test -count=1 -run '^TestArchitecture$' ./internal/archtest")
	return res, nil
}

// render builds every file the split writes: the table's file rewritten,
// and the new files.
func render(p *pkg, t *table, sp *spec, plans []*ctorPlan) (map[string][]byte, []string, error) {
	files, order, err := newFiles(p, t, plans, sp.Headers)
	if err != nil {
		return nil, nil, err
	}
	rewritten, err := rewriteTable(p, t, plans)
	if err != nil {
		return nil, nil, err
	}
	files[t.file.name] = rewritten
	return files, order, nil
}

// selfCheck parses the package as the split leaves it and proves the split
// faithful before anything is written.
func selfCheck(p *pkg, t *table, sp *spec, plans []*ctorPlan, files map[string][]byte, maxFunc, maxFile int) error {
	var srcs []source
	for _, s := range p.sources() {
		if b, ok := files[s.name]; ok {
			s.src = b
		}
		srcs = append(srcs, s)
	}
	for name, b := range files {
		if name != t.file.name {
			srcs = append(srcs, source{name, b})
		}
	}
	after, err := parsePkg(srcs)
	if err != nil {
		return err
	}
	name := sp.funcName()
	before, err := flatten(p, name)
	if err != nil {
		return err
	}
	now, err := flatten(after, name)
	if err != nil {
		return err
	}
	if diffs := compareFlat(before, now); len(diffs) > 0 {
		return fmt.Errorf("%s() no longer returns the same entries:\n%s", name, strings.Join(diffs, "\n"))
	}
	if diffs := sameOutside(p, after, name, before, now); len(diffs) > 0 {
		return fmt.Errorf("the package changed outside %s()'s entries:\n%s", name, strings.Join(diffs, "\n"))
	}
	return checkSizes(after, plans, files, maxFunc, maxFile)
}

// sameOutside reports every way a package differs between two sides of a
// split outside the table's entries: a package-level declaration added,
// dropped or changed, with its doc comment and the import paths its
// qualifiers resolve to, other than the function and the constructors
// whose literals hold each side's entries (a function that is a constructor
// on one side only counts as a declaration on the other); and the
// function's doc comment, signature or file. Declarations are matched by
// name, not by file, so a declaration moved to another file (M11a's class A
// commit) is unchanged.
func sameOutside(before, after *pkg, name string, bf, af []flat) []string {
	skip := func(l []flat) map[string]bool {
		s := map[string]bool{name: true}
		for _, e := range l {
			s[e.fn] = true
		}
		return s
	}
	diffs := compareDecls(otherDecls(before, skip(bf)), otherDecls(after, skip(af)))
	if err := sameSignature(before, after, name); err != nil {
		diffs = append(diffs, err.Error())
	}
	return diffs
}

// checkSizes measures the written constructors and new files as
// internal/archtest's K14 rules do: a function from its "func" line to its
// closing brace, a file by its lines. The table's own file only shrinks,
// and may be grandfathered, so it is not judged.
func checkSizes(after *pkg, plans []*ctorPlan, files map[string][]byte, maxFunc, maxFile int) error {
	for _, cp := range plans {
		_, fn, err := after.findFunc(cp.Func)
		if err != nil {
			return err
		}
		if n := after.fset.Position(fn.End()).Line - after.fset.Position(fn.Pos()).Line + 1; n != cp.lines || n > maxFunc {
			return fmt.Errorf("constructor %s spans %d lines (planned %d; K14's function budget is %d)", cp.Func, n, cp.lines, maxFunc)
		}
		if n := countLines(files[cp.File]); n > maxFile {
			return refuse(fmt.Errorf("%s would have %d lines, over K14's %d-line file budget (internal/archtest); "+
				"spread its constructors over more files in the spec", cp.File, n, maxFile))
		}
	}
	return nil
}

// sameSignature checks that the split function keeps its doc comment and
// signature.
func sameSignature(before, after *pkg, name string) error {
	bf, bfn, err := before.findFunc(name)
	if err != nil {
		return err
	}
	af, afn, err := after.findFunc(name)
	if err != nil {
		return err
	}
	if printNode(before.fset, bf, bfn.Type) != printNode(after.fset, af, afn.Type) ||
		docText(bfn) != docText(afn) || bf.name != af.name {
		return fmt.Errorf("%s() changed its signature, doc comment or file", name)
	}
	return nil
}

func docText(fn *ast.FuncDecl) string {
	if fn.Doc == nil {
		return ""
	}
	return commentText(fn.Doc)
}
