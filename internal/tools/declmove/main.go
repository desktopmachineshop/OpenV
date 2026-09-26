// Command declmove performs a pure move (refactor class A, rule R4): it
// moves package-level declarations between the files of one Go package as
// a committed spec says, keyed by declaration name, so that a move PR is
// regenerated on the latest master rather than rebased.
//
//	go run ./internal/tools/declmove [-n] [-goimports=false] -spec internal/tools/declmove/specs/<step>.json
//
// Each declaration moves with its doc comment and any comment on its last
// line, in spec order, appended to its target file; a missing target is
// created with the spec's header comment and the package clause. The moved
// code takes the imports it uses with it, a source file drops the imports
// only the moved code used, and a source file left with nothing but its
// package clause is deleted. Then the pinned goimports (goimportsPkg) tidies
// the touched files, so only import blocks change.
//
// declmove refuses a spec with a missing or ambiguous name, and checks its
// own work: the declhash manifest of the package (every declaration, test
// files included) must be identical before and after, and every moved
// declaration must be in its target file. If not, or if goimports fails,
// it restores every file and exits 1.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// goimportsPkg is the one pin for goimports. go run fetches it into the
// module cache without touching go.mod. v0.42.0 needs Go 1.24, so it runs
// on the repository's toolchain without a toolchain switch.
const goimportsPkg = "golang.org/x/tools/cmd/goimports@v0.42.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("declmove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specPath := fs.String("spec", "", "the move spec, a JSON `file`")
	useGoimports := fs.Bool("goimports", true, "tidy the touched files with "+goimportsPkg)
	dryRun := fs.Bool("n", false, "print the moves and write nothing")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: declmove [-n] [-goimports=false] -spec <spec.json>")
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
		fmt.Fprintln(stderr, "declmove:", err)
		return 2
	}
	opts := options{dryRun: *dryRun}
	if *useGoimports {
		opts.goimports = runGoimports
	}
	res, err := apply(packageDir(sp.Package), sp, opts)
	if res != nil {
		for _, l := range res.lines {
			fmt.Fprintln(stdout, l)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "declmove:", err)
		return 1
	}
	switch {
	case *dryRun:
		fmt.Fprintln(stdout, "declmove: dry run; nothing written")
	default:
		for _, f := range res.deleted {
			fmt.Fprintf(stdout, "declmove: deleted %s, which was left empty\n", f)
		}
		fmt.Fprintf(stdout, "declmove: moved %d declarations into %d files; the declhash manifest of %s is unchanged (%d declarations)\n",
			len(res.lines), res.targets, res.label, res.decls)
	}
	return 0
}

// packageDir resolves a spec's package against the module root holding the
// working directory; an absolute path is used as it is.
func packageDir(pkg string) string {
	if filepath.IsAbs(pkg) {
		return pkg
	}
	wd, err := os.Getwd()
	if err != nil {
		return pkg
	}
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, filepath.FromSlash(pkg))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return pkg
		}
		dir = parent
	}
}

// options says how apply runs; goimports is nil to skip that step.
type options struct {
	dryRun    bool
	goimports func(dir string, files []string) error
}

// result is what apply did.
type result struct {
	lines   []string // "key: from.go -> to.go", in spec order
	label   string
	decls   int
	targets int
	deleted []string
}

// apply plans the spec's moves, writes them, tidies imports and verifies
// that the package's declarations are unchanged, restoring every file if
// any step fails.
func apply(dir string, sp *spec, opts options) (*result, error) {
	srcs, err := readDir(dir)
	if err != nil {
		return nil, err
	}
	if len(srcs) == 0 {
		return nil, fmt.Errorf("%s: no Go files", dir)
	}
	p, err := parsePackage(srcs, true)
	if err != nil {
		return nil, err
	}
	moves, err := plan(p, sp)
	if err != nil {
		return nil, err
	}
	res := &result{label: moduleLabel(dir)}
	targets := map[string]bool{}
	for _, m := range moves {
		targets[m.to] = true
		for _, k := range m.keys {
			res.lines = append(res.lines, fmt.Sprintf("%s: %s -> %s", k, m.from.name, m.to))
		}
	}
	res.targets = len(targets)
	if opts.dryRun {
		return res, nil
	}
	contents, err := buildEdits(p, sp, moves)
	if err != nil {
		return res, err
	}
	before := p.manifest(res.label)
	res.decls = len(before)
	backup := map[string][]byte{}
	for _, f := range p.files {
		if _, ok := contents[f.name]; ok {
			backup[f.name] = f.src
		}
	}
	restore := func(cause error) error {
		for name := range contents {
			path := filepath.Join(dir, name)
			if orig, ok := backup[name]; ok {
				_ = os.WriteFile(path, orig, 0o644)
			} else {
				_ = os.Remove(path)
			}
		}
		return fmt.Errorf("%w; every file is restored", cause)
	}
	written, err := write(dir, contents, res)
	if err != nil {
		return res, restore(err)
	}
	if opts.goimports != nil {
		if err := opts.goimports(dir, written); err != nil {
			return res, restore(fmt.Errorf("goimports: %w (with -goimports=false, declmove's own import handling stands alone)", err))
		}
	}
	if err := verify(dir, res.label, before, moves); err != nil {
		return res, restore(err)
	}
	return res, nil
}

// write stores the new contents and deletes emptied files, returning the
// paths it wrote.
func write(dir string, contents map[string][]byte, res *result) ([]string, error) {
	names := make([]string, 0, len(contents))
	for name := range contents {
		names = append(names, name)
	}
	sort.Strings(names)
	var written []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if contents[name] == nil {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			res.deleted = append(res.deleted, name)
			continue
		}
		if err := os.WriteFile(path, contents[name], 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

// verify re-reads the package: its manifest must equal before, and each
// moved declaration must be in its target file.
func verify(dir, label string, before []string, moves []*move) error {
	srcs, err := readDir(dir)
	if err != nil {
		return err
	}
	p, err := parsePackage(srcs, true)
	if err != nil {
		return err
	}
	after := p.manifest(label)
	if diff := manifestDiff(before, after); diff != "" {
		return errors.New("the move changed declarations, so it is not a pure move:\n" + diff)
	}
	in := map[string]map[string]bool{}
	for _, d := range p.decls {
		if in[d.key] == nil {
			in[d.key] = map[string]bool{}
		}
		in[d.key][d.file.name] = true
	}
	for _, m := range moves {
		for _, k := range m.keys {
			key, _, _ := strings.Cut(k, "#")
			if !in[key][m.to] {
				return fmt.Errorf("%s did not arrive in %s", k, m.to)
			}
		}
	}
	return nil
}

// manifestDiff lists the lines only one side has, as - and + lines.
func manifestDiff(before, after []string) string {
	count := map[string]int{}
	for _, l := range before {
		count[l]--
	}
	for _, l := range after {
		count[l]++
	}
	var out []string
	for l, n := range count {
		for ; n < 0; n++ {
			out = append(out, "- "+l)
		}
		for ; n > 0; n-- {
			out = append(out, "+ "+l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][2:] < out[j][2:] || (out[i][2:] == out[j][2:] && out[i] < out[j]) })
	return strings.Join(out, "\n")
}

// runGoimports runs the pinned goimports over the files, in place.
func runGoimports(dir string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	cmd := exec.Command("go", append([]string{"run", goimportsPkg, "-w"}, files...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
