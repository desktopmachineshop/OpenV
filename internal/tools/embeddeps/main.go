// Command embeddeps generates refactor step M14
// (docs/plans/codebase-refactor.md, K5): Handler embeds HandlerDeps, so a
// handler dependency is declared once, as a HandlerDeps field.
//
//	go run ./internal/tools/embeddeps [-n] <package dir>
//
// scripts/refactor/embed_deps.sh runs it on internal/api; the class R commit
// is exactly what it writes, and the Refactor guard re-runs the script on
// that commit's parent and requires the commit byte for byte.
//
// A Handler field is a pure copy when NewHandler's `h := &Handler{...}`
// literal sets it to `deps.F`, the HandlerDeps field F as it is, of an
// identical type. Every other element (a call, a local, a trimmed string)
// is derived and stays a private field where it is. For each pure copy the
// tool, from the package's type information:
//
//   - renames the field to F in every selector whose receiver has type
//     Handler or *Handler, in production and test files, so it reads the
//     promoted HandlerDeps field; a selector of another type's field with
//     the same name (AuthMiddleware.userService) is a different object and
//     is left as it is;
//   - deletes the field, with its doc and line comments, from Handler, and
//     the element from the literal.
//
// Handler gains HandlerDeps as its first field, and NewHandler gains
// `h.HandlerDeps = deps` right after the literal. Each changed file is
// gofmt'd. It refuses, writing nothing, what it would have to guess: a
// pure-copy field named by anything but such a selector (a composite
// literal key outside NewHandler, a selector through another embedding), a
// HandlerDeps field copied twice, or a HandlerDeps field or method that
// would collide with a Handler field or method that stays. Before writing,
// it type-checks the rewritten package, tests included, and counts that
// every renamed selector now reads the promoted HandlerDeps field. On a
// package where Handler already embeds HandlerDeps it does nothing.
//
// Exit status: 0 done (or nothing to do), 1 refused, 2 a usage or load
// error. -n prints the plan and writes nothing.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// errRefuse marks a refusal: the package has a shape the rewrite would have
// to guess about.
var errRefuse = errors.New("refused")

// errDone means Handler already embeds HandlerDeps.
var errDone = errors.New("Handler already embeds HandlerDeps")

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errRefuse, fmt.Sprintf(format, args...))
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("embeddeps", flag.ContinueOnError)
	fl.SetOutput(stderr)
	dry := fl.Bool("n", false, "print the plan and write nothing")
	fl.Usage = func() {
		fmt.Fprintln(stderr, "usage: go run ./internal/tools/embeddeps [-n] <package dir>")
	}
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() != 1 {
		fl.Usage()
		return 2
	}
	ps, err := loadPackage(fl.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "embeddeps:", err)
		if errors.Is(err, errRefuse) {
			return 1
		}
		return 2
	}
	p, err := makePlan(ps)
	if errors.Is(err, errDone) {
		fmt.Fprintf(stdout, "embeddeps: %s: %v; nothing to do\n", ps.rel, err)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "embeddeps:", err)
		return 1
	}
	out, err := p.render()
	if err == nil {
		err = p.verify(out)
	}
	if err != nil {
		fmt.Fprintln(stderr, "embeddeps:", err)
		return 1
	}
	p.report(stdout, out)
	if *dry {
		return 0
	}
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(ps.dir, name)
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintln(stderr, "embeddeps:", err)
			return 2
		}
		if err := os.WriteFile(path, out[name], info.Mode().Perm()); err != nil {
			fmt.Fprintln(stderr, "embeddeps:", err)
			return 2
		}
	}
	return 0
}

// report prints what the rewrite does, in a stable order.
func (p *plan) report(w io.Writer, out map[string][]byte) {
	fmt.Fprintf(w, "embeddeps: %s: Handler embeds HandlerDeps\n", p.ps.rel)
	fmt.Fprintf(w, "promoted (%d fields NewHandler copied as they are):\n", len(p.copies))
	total := 0
	for _, c := range p.copies {
		fmt.Fprintf(w, "  %s -> %s (%d selectors)\n", c.field.Name(), c.dep.Name(), c.renames)
		total += c.renames
	}
	fmt.Fprintf(w, "kept private (%d fields NewHandler derives): %s\n", len(p.kept), strings.Join(p.kept, ", "))
	files := make([]string, 0, len(out))
	for name := range out {
		files = append(files, name)
	}
	sort.Strings(files)
	fmt.Fprintf(w, "%d selectors renamed; %d files written: %s\n", total, len(files), strings.Join(files, ", "))
	fmt.Fprintf(w, "NewHandler: %s.HandlerDeps = %s after its literal\n", p.hName, p.depsName)
}
