// Command stageextract generates refactor step M4 (class B, rule R4): the
// contiguous ranges of a function's body that a committed spec names move,
// line for line, into stage methods of a new struct type in wire_*.go
// files, and the function calls the stages in today's order. M4 splits
// cmd/server's main() with specs/M4.json.
//
//	go run ./internal/tools/stageextract [-n] [-vet=false] -spec internal/tools/stageextract/specs/M4.json
//
// A stage is a range of the function's top-level statements, from the
// line the spec gives (a statement's first line or a comment above one)
// to its last statement before the next range, the function's own tail
// or a defer. The spec gives each range its first line and that line's
// text; when the file has moved on, a range starts at the one line with
// that text, with a note, so the split regenerates on the latest master
// (R4), and a range no line matches is refused.
//
// The function's top-level locals that more than one range (or a range
// and the statements that stay) reference become fields of the type
// (app), and every reference to one becomes a.name; a local that one
// range alone uses, or the function's own statements alone, stays a local
// there. A local that is never captured by a function literal nor
// addressed (explicitly, or by a pointer method or slicing: address.go),
// and that each range after its declaring one first writes whole with =
// or := and then reads, is split instead: each range declares its own,
// and the function its own once, so no value crosses a stage boundary
// (err, which every fallible call writes and its fatal check reads). The
// function's own statements between two stage calls count as a range of
// their own here: a local main() reads in its tail, after a stage wrote
// it, is a field. The := rules:
//
//   - x := v, x a field: a.x = v.
//   - x, err := v, x a field, err new in its range (split, or a local of
//     the range): var err error on a line of its own, then a.x, err = v.
//     A name the := reused stays reused: a.x, err = v.
//   - x, y := v with no field: unchanged; a split local it writes first in
//     its range is declared there by the same :=.
//   - err = v writing a split local first in a range that did not declare
//     it: var err error, then err = v.
//   - var x T of a field goes (the field starts as T's zero value); var x
//     T = v becomes a.x = v; another name of the same var keeps a var
//     line of its own.
//
// Every defer stays in the function: a range ends before a top-level
// defer, and each defer right after a range of the shape defer f() or
// defer x.f() (no arguments), f or x a field, becomes a cleanup the stage
// returns: the stage ends with return a.stop (or a.db.Close), and the
// function binds it and defers it at today's point: stop := a.signals();
// defer stop(). Other defers stay as they are, and so does one of a local
// that stays the function's, with every defer after it (with a note). The function's statements that stay
// (before the first range, after its defers, and the tail from the spec's
// main line) keep their text, with the same rewrites; the function starts
// with a := &app{}.
//
// stageextract refuses, writing nothing, what it could not move as it is
// or would have to guess: a return, defer, recover, label or goto inside
// a range; a range with no statement before a defer; a range that does not
// run as the spec's lines say when nothing has moved; a local constant or
// type two ranges use; a field whose type no other file can name; the
// receiver's name in use in the function; a name the package already
// declares or a file that exists; a dot import; a stage over K14's 100
// lines, a new file over 800, or the function over the spec's budget
// (main_budget). Each is the spec's (or the function's shape's) to settle,
// and carries errPlan. Then it type-checks the package as the split leaves
// it, from source against the export data of its dependencies, writes the
// files, runs go vet on the package, and restores every file if vet fails.
//
// The proof a reviewer runs on the result is movecheck's, with S14c's
// normalisation: go run ./internal/tools/movecheck -flatten main -base
// <ref> cmd/server inlines the stages and undoes these rewrites, and exits
// 1 on any other difference.
//
// Exit status: 0 on success, 1 when stageextract refuses the split or the
// result does not type-check or vet, 2 on a usage, spec or read error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	fs := flag.NewFlagSet("stageextract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specPath := fs.String("spec", "", "the stage spec, a JSON `file`")
	dryRun := fs.Bool("n", false, "print the stage map and write nothing")
	vet := fs.Bool("vet", true, "run go vet on the package after writing it")
	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: stageextract [-n] [-vet=false] -spec <spec.json>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *specPath == "" || fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	sp, err := loadSpec(*specPath)
	if err != nil {
		fmt.Fprintln(stderr, "stageextract:", err)
		return 2
	}
	opts := options{dryRun: *dryRun}
	if *vet {
		opts.vet = goVet
	}
	res, err := generate(packageDir(sp.Package), sp, opts)
	if res != nil {
		for _, l := range res.lines {
			fmt.Fprintln(stdout, l)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "stageextract:", err)
		if errors.Is(err, errNoSource) {
			return 2
		}
		return 1
	}
	return 0
}

// errPlan marks a refusal that the spec or the function's shape causes,
// not a fault of the split: a range that no longer matches its line, a
// stage over K14's budget, a statement a stage cannot hold, a name that
// is taken. The spec is edited to settle one (rule R4: M4's author
// regenerates the split on the latest master), so TestM4OnTheWorkingTree
// skips on it instead of failing a feature pull request (§8.4); a failed
// type-check or vet of the result is never marked.
var errPlan = errors.New("refused by the spec or the function's shape")

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

// options says how generate runs; vet is nil to skip that step.
type options struct {
	dryRun bool
	vet    func(dir string) error
}

// result is what generate did or would do: notes, the stage map and a
// summary, and the files it wrote (or would write).
type result struct {
	lines []string
	files map[string][]byte
}

// generate splits the spec's function in the package at dir.
func generate(dir string, sp *spec, opts options) (*result, error) {
	ps, err := loadPackage(dir)
	if err != nil {
		return nil, err
	}
	p, err := newPlan(ps, sp)
	if err != nil {
		return nil, err
	}
	out, err := p.render()
	if err != nil {
		return nil, err
	}
	res := &result{files: out.files}
	for _, n := range p.notes {
		res.lines = append(res.lines, "stageextract: note: "+n)
	}
	res.lines = append(res.lines, p.report(out)...)
	if err := ps.recheck(out.files); err != nil {
		return res, fmt.Errorf("the split does not type-check, so nothing is written: %v", err)
	}
	if opts.dryRun {
		return res, nil
	}
	if err := write(dir, sp.file(), out); err != nil {
		return res, err
	}
	if opts.vet != nil {
		if err := opts.vet(dir); err != nil {
			restore(dir, sp.file(), ps.file(sp.file()).src, out.order)
			return res, fmt.Errorf("%v\nevery file is restored", err)
		}
		res.lines = append(res.lines, fmt.Sprintf("stageextract: go vet ./%s passed", ps.rel))
	}
	return res, nil
}

// report is the stage map, what the pull request's description carries,
// and a summary.
func (p *plan) report(out *output) []string {
	var lines []string
	biggest, size := "", 0
	cleanups := 0
	for _, st := range p.stages {
		l := fmt.Sprintf("%s -> %s (lines %d-%d, %d lines)", st.spec.Name, st.spec.File, st.start, st.end, out.spans[st.spec.Name])
		if len(st.cleanups) > 0 {
			var what []string
			for _, d := range st.defers {
				what = append(what, string(p.mf.src[p.off(d.Call.Fun.Pos()):p.off(d.Call.Fun.End())]))
			}
			l += fmt.Sprintf(", returns %s for %s() to defer as %s", strings.Join(what, ", "), p.sp.funcName(), strings.Join(st.cleanups, ", "))
			cleanups += len(st.cleanups)
		}
		lines = append(lines, l)
		if out.spans[st.spec.Name] > size {
			biggest, size = st.spec.Name, out.spans[st.spec.Name]
		}
	}
	split := "none"
	if s := p.splitNames(); len(s) > 0 {
		split = strings.Join(s, ", ")
	}
	lines = append(lines,
		fmt.Sprintf("stageextract: %s() of %s into %d stages in %d files; %d locals become fields of %s (%s), declared by each stage that uses it: %s; %d cleanups returned",
			p.sp.funcName(), p.ps.rel, len(p.stages), len(out.order)-2, p.fieldCount(), p.sp.typeName(), p.sp.typeFile(), split, cleanups),
		fmt.Sprintf("stageextract: %s() spans %d lines; the largest stage, %s, %d", p.sp.funcName(), out.mainLines, biggest, size))
	return lines
}

// write writes the new files, then the function's file; if a write fails,
// every file is restored.
func write(dir, mainName string, out *output) error {
	orig, err := os.ReadFile(filepath.Join(dir, mainName))
	if err != nil {
		return err
	}
	for i, name := range out.order {
		if err := os.WriteFile(filepath.Join(dir, name), out.files[name], 0o644); err != nil {
			restore(dir, mainName, orig, out.order[:i+1])
			return fmt.Errorf("write %s: %w; every file is restored", name, err)
		}
	}
	return nil
}

// restore puts the function's file back and removes the new files.
func restore(dir, mainName string, orig []byte, written []string) {
	for _, name := range written {
		p := filepath.Join(dir, name)
		if name == mainName {
			_ = os.WriteFile(p, orig, 0o644)
		} else {
			_ = os.Remove(p)
		}
	}
}
