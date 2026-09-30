// Command liftmigrations generates refactor step M10 (one file per
// migration) from internal/persistence/postgres/migrations.go, so the move
// is regenerated on the latest master rather than rebased (rule R4).
//
//	go run ./internal/tools/liftmigrations -spec internal/tools/declmove/specs/M10.json [-n] [dir]
//	go run ./internal/tools/liftmigrations [-n] [-build=false] [dir]
//
// dir is the package directory; by default the module's
// internal/persistence/postgres. M10 lands as two commits:
//
//   - Class A. -spec writes the declmove spec that moves every declaration
//     of the registry's file but the registry (`var migrations`) and its
//     element type (Migration): what a registry entry reaches goes to
//     migration_helpers.go (backfillRefPrefix and embeddingDimensions
//     today), everything else, the runner, to migrate_runner.go.
//     internal/tools/declmove then applies it and proves it with declhash.
//     -spec changes nothing else.
//   - Class B. Without -spec, each registry entry whose Run is a function
//     literal becomes `func m00NN<Name>(tx *sql.Tx) error` in
//     migration_00NN_<name>.go, with the comment above the entry as its doc
//     comment, and the entry becomes one line naming it. The registry stays
//     an explicit, ordered list: no init(). An entry that already names a
//     package-level function (the 0001 baseline's RunDB, or a migration
//     lifted before) is left as it is, so a second run changes nothing.
//
// A lift checks its own result before it stops: the registry holds the
// same entries in the same order; each lifted function is its literal
// token for token, comments and raw-string bytes included, which leaves
// every hash of the stored-data freeze (S3, testdata/freeze/) as it was; its
// doc comment says what the entry's comment said, and no comment of the
// registry's file is lost or reordered; each new file
// declares its function alone; every other declaration is byte-identical;
// and the package builds. If any check fails, every file is restored and
// the exit status is 1. -n prints the function-to-file map, or the spec,
// and writes nothing. Exit status 2 is a usage or read error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultPackage is the package M10 lifts, relative to the module root.
const defaultPackage = "internal/persistence/postgres"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("liftmigrations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specOut := fs.String("spec", "", "write the declmove spec of M10's class A commit (the helper and runner move) to this `file` instead of lifting")
	dryRun := fs.Bool("n", false, "print the function-to-file map, or the spec, and write nothing")
	goBuild := fs.Bool("build", true, "after a lift, build the package with the go command, and restore every file if it fails")
	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: liftmigrations -spec <spec.json> [-n] [dir]\n"+
			"       liftmigrations [-n] [-build=false] [dir]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return 2
	}
	dir, root, err := packageDir(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 2
	}
	p, err := loadPackage(dir)
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 2
	}
	if *specOut != "" {
		return writeSpec(p, root, *specOut, *dryRun, stdout, stderr)
	}
	return doLift(p, root, *dryRun, *goBuild, stdout, stderr)
}

// packageDir resolves the package directory (dir, or the module's
// internal/persistence/postgres) and the root of the module that holds it.
func packageDir(arg string) (dir, root string, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	if arg == "" {
		if root, err = moduleRoot(wd); err != nil {
			return "", "", err
		}
		return filepath.Join(root, filepath.FromSlash(defaultPackage)), root, nil
	}
	if dir, err = filepath.Abs(arg); err != nil {
		return "", "", err
	}
	if root, err = moduleRoot(dir); err != nil {
		return "", "", err
	}
	return dir, root, nil
}

// moduleRoot is the nearest directory at or above dir that holds go.mod.
func moduleRoot(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("%s is not inside a Go module", dir)
		}
		d = parent
	}
}

// writeSpec writes (or, with -n, prints) the declmove spec of the class A
// commit.
func writeSpec(p *pkgSource, root, out string, dryRun bool, stdout, stderr io.Writer) int {
	rel, err := filepath.Rel(root, p.dir)
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 2
	}
	sp, err := planMove(p, filepath.ToSlash(rel))
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 1
	}
	if sp == nil {
		fmt.Fprintln(stdout, "liftmigrations: the registry's file holds only the registry and its element type; nothing to move")
		return 0
	}
	b, err := sp.encode()
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 2
	}
	for _, t := range sp.Targets {
		fmt.Fprintf(stdout, "%s: %s\n", t.File, strings.Join(t.Decls, ", "))
	}
	if dryRun {
		stdout.Write(b)
		fmt.Fprintln(stdout, "liftmigrations: dry run; nothing written")
		return 0
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 2
	}
	fmt.Fprintf(stdout, "liftmigrations: wrote %s; apply it with: go run ./internal/tools/declmove -spec %s\n", out, out)
	return 0
}

// doLift lifts, checks the result and restores every file if a check
// fails.
func doLift(p *pkgSource, root string, dryRun, goBuild bool, stdout, stderr io.Writer) int {
	plan, err := planLift(p)
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 1
	}
	for _, e := range plan.entries {
		if l := plan.byEntry[e]; l != nil {
			fmt.Fprintf(stdout, "%s: %s -> %s %s\n", e.label(), e.pos, l.file, l.funcName)
		} else {
			fd := p.namedFunc(e.value)
			fmt.Fprintf(stdout, "%s: %s %s (%s), left as it is\n", e.label(), e.field, fd.Name.Name, p.where(fd.Pos()))
		}
	}
	if len(plan.lifts) == 0 {
		fmt.Fprintln(stdout, "liftmigrations: every migration already runs a named function; nothing to lift")
		return 0
	}
	contents, err := plan.render()
	if err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 1
	}
	if dryRun {
		fmt.Fprintf(stdout, "liftmigrations: dry run; %d migrations would move, nothing written\n", len(plan.lifts))
		return 0
	}
	if err := apply(plan, contents, root, goBuild); err != nil {
		fmt.Fprintln(stderr, "liftmigrations:", err)
		return 1
	}
	lines := strings.Count(string(contents[plan.regFile.name]), "\n")
	fmt.Fprintf(stdout, "liftmigrations: lifted %d migrations into files of their own; %s is %d lines\n",
		len(plan.lifts), plan.regFile.name, lines)
	return 0
}

// apply writes the lift, checks it and restores every file on failure.
func apply(plan *liftPlan, contents map[string][]byte, root string, goBuild bool) error {
	dir := plan.src.dir
	restore := func(cause error) error {
		_ = os.WriteFile(filepath.Join(dir, plan.regFile.name), plan.regFile.src, 0o644)
		for _, l := range plan.lifts {
			_ = os.Remove(filepath.Join(dir, l.file))
		}
		return fmt.Errorf("%w\nevery file is restored", cause)
	}
	for name, b := range contents {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return restore(err)
		}
	}
	if err := verifyLift(plan); err != nil {
		return restore(err)
	}
	if goBuild {
		if err := buildPackage(root, dir); err != nil {
			return restore(err)
		}
	}
	return nil
}

// buildPackage builds the package with the go command.
func buildPackage(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "./"+filepath.ToSlash(rel))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("the lifted package does not build (go build ./" + filepath.ToSlash(rel) + "):\n" +
			strings.TrimSpace(string(out)))
	}
	return nil
}
