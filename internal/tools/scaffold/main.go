// Command scaffold writes the first files of a change the area guides
// describe, in the shape the code beside them has, and appends the one
// registration each needs (refactor plan step N3):
//
//	go run ./internal/tools/scaffold [-n] api-area <name>
//	go run ./internal/tools/scaffold [-n] migration <name>
//	go run ./internal/tools/scaffold [-n] [-area <area>] mcp-tool <name>
//
// api-area writes internal/api/<name>_handlers.go, with its registrar
// register<Name>Routes and one stub handler, List<Name>, for
// GET /api/v1/projects/{id}/<name>, and appends the registrar's call to
// RegisterRoutes in routes.go. migration writes
// internal/persistence/postgres/migration_<NNNN>_<name>.go, NNNN the next
// free version, declaring m<NNNN><Name>, and appends its line to the
// registry in migrations.go. mcp-tool appends a Tool named <name> to the
// constructor of internal/mcp/tools_<area>.go that comes last in Tools();
// without -area, to the constructor last in Tools(), so the tool is the
// table's last; an -area with no file yet gets one, whose constructor is
// appended to Tools(). A registration is always appended, never inserted:
// route registration order (I2), the migrations' order (I16) and the
// tools' order (I11) are behaviour.
//
// A name is lower-case words of letters and digits joined by '-' or '_',
// and both spellings give the same result: snake case for file, migration
// and tool names (widget_reports), kebab case for a URL segment
// (widget-reports), and each word capitalised for a Go identifier
// (WidgetReports). There is no initialism table: ai-map gives AiMap.
//
// It refuses to overwrite a file or to reuse a name already taken: a
// declaration of the package, a route, a migration's name or a tool's.
// Having written, it prints what is left to do by hand: a docs/areas.json
// glob when no area claims a new file (as `go run ./internal/tools/areas
// which` answers), the golden regenerate commands the area's README names,
// and a release note, since a new route, migration or tool changes
// behaviour. -n prints what it would write and writes nothing. It runs from
// anywhere in the repository. Exit status 1 is a refusal, 2 a usage error.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const usage = `usage:
  go run ./internal/tools/scaffold [-n] api-area <name>
  go run ./internal/tools/scaffold [-n] migration <name>
  go run ./internal/tools/scaffold [-n] [-area <area>] mcp-tool <name>`

func main() {
	cwd, err := os.Getwd()
	if err == nil {
		cwd, err = findRoot(cwd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "scaffold:", err)
		os.Exit(2)
	}
	os.Exit(run(os.Args[1:], cwd, os.Stdout, os.Stderr, areaStep))
}

// run scaffolds in the repository at root. areaOf words the step that
// says whether an area claims a new file.
func run(args []string, root string, stdout, stderr io.Writer, areaOf func(root, path string) string) int {
	fs := flag.NewFlagSet("scaffold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, usage)
		fs.PrintDefaults()
	}
	dryRun := fs.Bool("n", false, "print what would be written, and write nothing")
	areaFlag := fs.String("area", "", "mcp-tool only: append to internal/mcp/tools_`<area>`.go (default: the file whose constructor is last in Tools())")
	// Flags may come before or after the kind and the name.
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) != 2 {
		fs.Usage()
		return 2
	}
	kind := pos[0]
	n, err := parseName(pos[1])
	if err == nil && *areaFlag != "" && kind != "mcp-tool" {
		err = errors.New("-area applies to mcp-tool only")
	}
	if err != nil {
		fmt.Fprintln(stderr, "scaffold:", err)
		return 2
	}
	var p *plan
	switch kind {
	case "api-area":
		p, err = planAPIArea(root, n)
	case "migration":
		p, err = planMigration(root, n)
	case "mcp-tool":
		var area *name
		if *areaFlag != "" {
			a, aerr := parseName(*areaFlag)
			if aerr != nil {
				fmt.Fprintln(stderr, "scaffold: -area:", aerr)
				return 2
			}
			area = &a
		}
		p, err = planMCPTool(root, n, area)
	default:
		fmt.Fprintf(stderr, "scaffold: unknown kind %q\n", kind)
		fs.Usage()
		return 2
	}
	if err == nil && !*dryRun {
		err = p.apply(root)
	}
	if err != nil {
		fmt.Fprintln(stderr, "scaffold:", err)
		return 1
	}
	p.report(stdout, root, *dryRun, areaOf)
	return 0
}

// name is a scaffold name split into its words.
type name struct{ words []string }

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)

func parseName(s string) (name, error) {
	if !nameRe.MatchString(s) {
		return name{}, fmt.Errorf("name %q: want lower-case words of letters and digits joined by '-' or '_', starting with a letter (widget-reports)", s)
	}
	return name{strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })}, nil
}

func (n name) snake() string { return strings.Join(n.words, "_") }
func (n name) kebab() string { return strings.Join(n.words, "-") }
func (n name) text() string  { return strings.Join(n.words, " ") }

// camel capitalises each word: widget_reports is WidgetReports.
func (n name) camel() string {
	var b strings.Builder
	for _, w := range n.words {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

// lowerCamel is camel with the first word as it is: widgetReports.
func (n name) lowerCamel() string { return n.words[0] + n.camel()[len(n.words[0]):] }

// plan is what one scaffold writes, and what it then says.
type plan struct {
	changes []change
	notes   []string // where each registration went, and why there
	steps   []string // what is left to do by hand, after the area step
}

// change is one file the plan creates or edits.
type change struct {
	path     string // relative to the root, with forward slashes
	old, new []byte // old is nil for a file the plan creates
}

func (p *plan) create(path string, src []byte) {
	p.changes = append(p.changes, change{path: path, new: src})
}
func (p *plan) edit(f *goFile, src []byte) {
	p.changes = append(p.changes, change{path: f.path, old: f.src, new: src})
}

// apply writes the plan: new files first, each refused if it exists by
// then, then the edits.
func (p *plan) apply(root string) error {
	for _, c := range p.changes {
		if c.old != nil {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(c.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, err = f.Write(c.new)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	for _, c := range p.changes {
		if c.old != nil {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(c.path)), c.new, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// report prints the files written (or, under -n, what would be written),
// where each registration went, and the steps left.
func (p *plan) report(w io.Writer, root string, dryRun bool, areaOf func(root, path string) string) {
	var steps []string
	for _, c := range p.changes {
		switch {
		case c.old == nil && dryRun:
			fmt.Fprintf(w, "would create %s:\n%s\n", c.path, indent(string(c.new)))
		case c.old == nil:
			fmt.Fprintf(w, "created %s\n", c.path)
		case dryRun:
			line, added := insertion(c.old, c.new)
			fmt.Fprintf(w, "would add to %s, at line %d:\n%s\n", c.path, line, indent(added))
		default:
			line, _ := insertion(c.old, c.new)
			fmt.Fprintf(w, "edited %s (lines added at %d)\n", c.path, line)
		}
		if c.old == nil {
			steps = append(steps, areaOf(root, c.path))
		}
	}
	for _, n := range p.notes {
		fmt.Fprintf(w, "\n%s\n", n)
	}
	fmt.Fprintln(w, "\nLeft to do:")
	for i, s := range append(steps, p.steps...) {
		fmt.Fprintf(w, "  %d. %s\n", i+1, s)
	}
}

// insertion returns the first line at which new differs from old, and the
// lines inserted there: every edit a scaffold makes is one insertion.
func insertion(old, new []byte) (int, string) {
	o, n := strings.SplitAfter(string(old), "\n"), strings.SplitAfter(string(new), "\n")
	i := 0
	for i < len(o) && i < len(n) && o[i] == n[i] {
		i++
	}
	j := 0
	for j < len(o)-i && j < len(n)-i && o[len(o)-1-j] == n[len(n)-1-j] {
		j++
	}
	return i + 1, strings.Join(n[i:len(n)-j], "")
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n    ")
}

// areaStep says whether an area of docs/areas.json claims path, asking
// `go run ./internal/tools/areas which`, the index's one reader.
func areaStep(root, path string) string {
	cmd := exec.Command("go", "run", "./internal/tools/areas", "which", path)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return fmt.Sprintf("Area: %s belongs to %s; docs/areas.json needs no glob.", path, strings.TrimSpace(stdout.String()))
	case errors.As(err, &exit) && exit.ExitCode() == 1 && strings.HasPrefix(stderr.String(), "areas: "):
		msg, _, _ := strings.Cut(stderr.String(), "\n") // go run adds "exit status 1"
		return "Area: " + strings.TrimPrefix(msg, "areas: ") + " (K15)."
	default:
		return fmt.Sprintf("Area: check that one area claims %s: go run ./internal/tools/areas which %s", path, path)
	}
}

// findRoot walks up from dir to the module root, the directory with go.mod.
func findRoot(dir string) (string, error) {
	for d := filepath.Clean(dir); ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no go.mod in %s or above it; run inside the repository", dir)
		}
		d = parent
	}
}
