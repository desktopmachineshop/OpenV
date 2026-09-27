package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// This file writes and checks testdata/boot_steps.txt, the static half of
// the boot harness (refactor plan §6.4 S4a; invariants I10 and I17): the
// call statements of main() and of internal/api.NewHandler, in source order,
// read from the syntax tree with go/types for the callee and receiver types.
// The black-box half, harness_test.go and boot_smoke_test.go, runs the built
// binary; this half sees the branches a default boot does not take and the
// order of wiring that leaves no trace in a response or a log line: the bus
// subscribers (hooks, notifier, budget monitor, trigger matcher), the
// setters, the Start calls, the goroutines and the defers.
//
// What counts as a step (the golden's header says the same):
//
//   - a call statement: an expression statement that calls something, a
//     call whose results are all assigned to blank identifiers, or a call of
//     which an error result is kept, whatever else it keeps (`if err := f();
//     ...`, `ids, err := svc.PurgeExpired(now)`, `a.db, err = connect()`):
//     the fallible calls whose outcome the boot checks, among them the
//     purge, the reaper's sweeps and the grandfathering write, which keep a
//     value beside the error. A call whose results are kept and include no
//     error (a constructor, a getter) is not a step. A method chain lists
//     each of its calls, innermost first. A method is named by its
//     receiver's static type, and an AddSubscriber or Subscribe* call also
//     by its arguments' static types, which say who subscribes;
//   - a go or defer statement. When it starts code of this package (a
//     function literal, a package-level function, a stage) the steps of that
//     body follow, indented, unless the body is a single call statement or
//     returns a single call, which is shown as that call; a defer reached
//     through an inlined function (see below) runs when that function
//     returns and is written defer.inner; a go or defer that only logs is
//     left out, like any other logging;
//   - internal/api.NewHandler, where main calls it: its own steps follow,
//     indented, so the billing rewiring it does is ordered against
//     billing.Start in main (quirk Q11).
//
// Calls into log and log/slog are left out: the boot log golden pins what is
// logged, and a log line is not wiring. Package-level functions of package
// main, the stage methods declared in its wire_*.go files, and closures bound
// to a variable are inlined where a statement calls them (an expression
// statement, the only right-hand side of an assignment, or a go or defer):
// their steps appear in place and the call itself does not, so moving code
// into a helper, a named function or an a.<stage>() method changes nothing
// (M1 to M4), while moving a step across another one does. In NewHandler the
// same goes for internal/api's package-level functions and *Handler methods.
// A call in the return statement of an inlined function is made for the
// statement that called the function and keeps what that statement keeps, so
// `db, err := connect(dsn)` over `return postgres.Connect(dsn)` reads like
// `db, err := postgres.Connect(dsn)`. A variable bound to a closure, to a
// method value or to another variable, directly or through a stage's final
// return, is followed when it is called, so `stop := a.config(); defer
// stop()` reads like `defer stop()`. Blocks
// (if, else, switch, case, default, for, select) are written only when a step
// is inside them, by nesting, with no condition. No file name, line number or
// condition appears, and no argument beyond a subscriber's type. Since a
// receiver's static type is part of a step, code extracted into a function or
// a stage keeps the types its locals had: a parameter or an app field typed
// as an interface where the local was a concrete type changes the step.

// ----------------------------------------------------------------------------
// Goldens

// updateGoldenEnv set to exactly 1 rewrites the goldens of this package from
// the current code instead of comparing against them; any other value
// compares. Only a deliberate behavior change regenerates a golden; a
// refactor never does.
const updateGoldenEnv = "UPDATE_GOLDEN"

func updatingGoldens() bool { return os.Getenv(updateGoldenEnv) == "1" }

const bootStepsGolden = "testdata/boot_steps.txt"

// bootStepsRegenerate is the command that rewrites boot_steps.txt.
const bootStepsRegenerate = updateGoldenEnv + "=1 go test ./cmd/server -count=1 -run '^TestBootSteps$'"

// checkGolden compares got with the golden file at path (relative to this
// package), or rewrites it when UPDATE_GOLDEN is 1. regenerate is the
// command a failure prints.
func checkGolden(t *testing.T, path string, got []byte, regenerate string) {
	t.Helper()
	shown := "cmd/server/" + filepath.ToSlash(path)
	how := regenerate + "\n(only " + updateGoldenEnv + "=1 regenerates; any other value compares)"
	if updatingGoldens() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the directory for %s: %v", shown, err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", shown, err, how)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s does not match the code:\n%s\n"+
			"A refactor never changes a golden. If this is a deliberate behavior change, regenerate it with:\n  %s",
			shown, lineDiff(string(want), string(got)), how)
	}
}

// lineDiff's table is quadratic in the lines it spans: the lines both texts
// share at the start and at the end stay out of it, and past lineDiffCells
// cells (4 bytes each, so 64 MiB) it compares only the first lineDiffSpan
// lines of each side's changed middle, far more than the 80 printed lines
// show. A tour golden runs to 20,000 lines, and changes may lie far apart.
const (
	lineDiffCells = 1 << 24
	lineDiffSpan  = 4000
)

// lineDiff is a longest-common-subsequence line diff of two goldens, with
// two lines of context around each change and a cap on what it prints.
func lineDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	cut := (len(ma)+1)*(len(mb)+1) > lineDiffCells
	if cut {
		ma, mb = ma[:min(len(ma), lineDiffSpan)], mb[:min(len(mb), lineDiffSpan)]
	}
	lcs := make([][]int32, len(ma)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(mb)+1)
	}
	for i := len(ma) - 1; i >= 0; i-- {
		for j := len(mb) - 1; j >= 0; j-- {
			if ma[i] == mb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		kind byte
		text string
		line int
	}
	var ops []op
	for k := 0; k < pre; k++ {
		ops = append(ops, op{' ', a[k], k + 1})
	}
	i, j := 0, 0
	for i < len(ma) || j < len(mb) {
		switch {
		case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
			ops = append(ops, op{' ', ma[i], pre + i + 1})
			i, j = i+1, j+1
		case j < len(mb) && (i == len(ma) || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', mb[j], pre + i + 1})
			j++
		default:
			ops = append(ops, op{'-', ma[i], pre + i + 1})
			i++
		}
	}
	if cut {
		ops = append(ops, op{'.', fmt.Sprintf("... (diff cut: the changes span more than the %d lines it compares)",
			lineDiffSpan), pre + i + 1})
	} else {
		for k := len(a) - suf; k < len(a); k++ {
			ops = append(ops, op{' ', a[k], k + 1})
		}
	}
	show := make([]bool, len(ops))
	for k, o := range ops {
		if o.kind != ' ' {
			for c := max(0, k-2); c <= min(len(ops)-1, k+2); c++ {
				show[c] = true
			}
		}
	}
	var out strings.Builder
	printed := 0
	for k, o := range ops {
		if !show[k] {
			continue
		}
		if printed == 80 {
			out.WriteString("... (diff truncated)\n")
			break
		}
		if o.kind == '.' {
			out.WriteString(o.text + "\n")
			break
		}
		if k == 0 || !show[k-1] {
			fmt.Fprintf(&out, "@@ golden line %d @@\n", o.line)
		}
		fmt.Fprintf(&out, "%c %s\n", o.kind, o.text)
		printed++
	}
	return out.String()
}

// ----------------------------------------------------------------------------
// The tests

// TestBootSteps writes the call statements of main() and NewHandler and
// compares them with testdata/boot_steps.txt.
func TestBootSteps(t *testing.T) {
	ld := newSourceLoader(t)
	api := ld.check(t, modulePath(t)+"/internal/api", nil)
	server := ld.check(t, modulePath(t)+"/cmd/server", nil)
	got := renderBootSteps(t, server, map[string]*checkedPkg{api.path: api}, modulePath(t))
	checkGolden(t, bootStepsGolden, []byte(got), bootStepsRegenerate)
}

// TestBootStepsFollowStages proves the rules on a synthetic program with the
// shapes cmd/server's main() has today (testdata/stages/flat) and on the
// same program as M2, M3 and M4 would leave it (testdata/stages/staged):
// main() calls a.<stage>() methods declared in wire_x.go, locals are app
// fields, a stage that opens a resource returns its cleanup for main() to
// defer at the same point and opens it through a helper that returns the
// fallible call (M1), the HTTP chain is a helper that returns its error, and
// the closures and goroutine bodies are named functions. Both must write
// exactly the steps below. Then three changes a refactor must not make, each
// applied to the staged program, must change them: a Subscribe moved out of
// its stage to after the next one, the database's defer moved into the stage
// that opens it, and the purge goroutine no longer purging at once (I17).
func TestBootStepsFollowStages(t *testing.T) {
	ld := newSourceLoader(t)
	const path = "example.com/bootsteps/synthetic"
	const want = `func main()
  defer value stop
  call database/sql.Open
  if
    call os.Exit
  defer (*database/sql.DB).Close
  call (*database/sql.DB).Ping
  if
    call os.Exit
  call (*example.com/bootsteps/synthetic.service).SetHub
  call (*example.com/bootsteps/synthetic.service).SetOwnerLookup
  call (*database/sql.DB).QueryRow -> (*database/sql.Row).Scan
  call (*example.com/bootsteps/synthetic.service).AddSubscriber(*example.com/bootsteps/synthetic.hub)
  call (*example.com/bootsteps/synthetic.hub).Subscribe(string)
  call example.com/bootsteps/synthetic.newNotifier -> (*example.com/bootsteps/synthetic.notifier).SetMailer -> (*example.com/bootsteps/synthetic.notifier).Start
  go
    call (*example.com/bootsteps/synthetic.service).Purge
    defer (*time.Ticker).Stop
    for
      select
        case
          call (*example.com/bootsteps/synthetic.service).Purge
          call (*example.com/bootsteps/synthetic.service).Sweep
  call (*net/http.ServeMux).Handle
  call net/url.Parse
  if
    call os.Exit
  go
    call (*net/http.Server).ListenAndServe
  select
    case
      if
        call os.Exit
    case
      call value stop
      defer value cancel
      call (*net/http.Server).Shutdown
      if
        call (*net/http.Server).Close
`
	for _, dir := range []string{"testdata/stages/flat", "testdata/stages/staged"} {
		got := bootStepsBody(t, ld.check(t, path, &syntheticSource{dir: dir}), nil, modulePath(t))
		if got != want {
			t.Errorf("%s writes other steps than the rules give:\n%s", dir, lineDiff(want, got))
		}
	}

	for _, m := range []struct {
		name     string
		edits    map[string][2]string
		wantLine string
	}{
		{"a Subscribe moved past the next stage", map[string][2]string{
			"wire_x.go": {"\ta.hub.Subscribe(\"hooks\")\n", ""},
			"main.go":   {"\ta.jobs()\n", "\ta.jobs()\n\ta.hub.Subscribe(\"hooks\")\n"},
		}, "call (*example.com/bootsteps/synthetic.hub).Subscribe(string)"},
		{"a defer moved into its stage", map[string][2]string{
			"wire_x.go": {"\treturn a.db.Close\n", "\tdefer a.db.Close()\n\treturn func() error { return nil }\n"},
		}, "defer.inner (*database/sql.DB).Close"},
		{"the purge no longer runs at once", map[string][2]string{
			"wire_x.go": {"\t}\n\tpurge()\n\tticker", "\t}\n\tticker"},
		}, "call (*example.com/bootsteps/synthetic.service).Purge"},
	} {
		t.Run(m.name, func(t *testing.T) {
			src := &syntheticSource{dir: "testdata/stages/staged", edit: m.edits}
			got := bootStepsBody(t, ld.check(t, path, src), nil, modulePath(t))
			if got == want {
				t.Fatalf("%s left the steps unchanged", m.name)
			}
			if diff := lineDiff(want, got); !strings.Contains(diff, m.wantLine) {
				t.Errorf("%s: the diff does not show %q:\n%s", m.name, m.wantLine, diff)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Loading and type-checking from source

var (
	modOnce sync.Once
	modRoot string
	modName string
	modErr  error
)

// moduleRoot is the directory holding go.mod, found upward from this
// package's directory (go test runs a package's tests there).
func moduleRoot(t testing.TB) string {
	t.Helper()
	modOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			modErr = err
			return
		}
		for {
			if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
				modRoot = dir
				for _, line := range strings.Split(string(data), "\n") {
					if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
						modName = f[1]
					}
				}
				if modName == "" {
					modErr = fmt.Errorf("%s has no module line", filepath.Join(dir, "go.mod"))
				}
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				modErr = errors.New("no go.mod above the test's directory")
				return
			}
			dir = parent
		}
	})
	if modErr != nil {
		t.Fatalf("find the module root: %v", modErr)
	}
	return modRoot
}

func modulePath(t testing.TB) string {
	moduleRoot(t)
	return modName
}

// goTool is the go command that runs this test.
func goTool() string {
	if p := filepath.Join(runtime.GOROOT(), "bin", "go"); fileExists(p) {
		return p
	}
	return "go"
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// listedPkg is what `go list -export -json` says about one package.
type listedPkg struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Export     string
	Error      *struct{ Err string }
}

var (
	listOnce sync.Once
	listed   map[string]listedPkg
	listErr  error
)

// listPackages lists cmd/server and everything it depends on, standard
// library included, with the export data the compiler wrote for each, once
// per test process.
func listPackages(t testing.TB) map[string]listedPkg {
	t.Helper()
	root := moduleRoot(t)
	listOnce.Do(func() {
		cmd := exec.Command(goTool(), "list", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,Export,Error", "./cmd/server")
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			listErr = fmt.Errorf("go list -export -deps ./cmd/server: %v\n%s", err, stderr.String())
			return
		}
		listed = map[string]listedPkg{}
		dec := json.NewDecoder(bytes.NewReader(out))
		for dec.More() {
			var p listedPkg
			if err := dec.Decode(&p); err != nil {
				listErr = fmt.Errorf("decode go list output: %v", err)
				return
			}
			if p.Error != nil {
				listErr = fmt.Errorf("go list: %s: %s", p.ImportPath, p.Error.Err)
				return
			}
			listed[p.ImportPath] = p
		}
	})
	if listErr != nil {
		t.Fatal(listErr)
	}
	return listed
}

// sourceLoader type-checks packages from source, importing everything else
// from the compiler's export data. A package it checked is imported from its
// source check, so internal/api's types are the same objects in both checks.
type sourceLoader struct {
	fset *token.FileSet
	pkgs map[string]listedPkg
	gc   types.Importer
	src  map[string]*types.Package
}

func newSourceLoader(t testing.TB) *sourceLoader {
	pkgs := listPackages(t)
	fset := token.NewFileSet()
	gc := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		p, ok := pkgs[path]
		if !ok || p.Export == "" {
			return nil, fmt.Errorf("no export data for %s (not a dependency of cmd/server)", path)
		}
		return os.Open(p.Export)
	})
	return &sourceLoader{fset: fset, pkgs: pkgs, gc: gc, src: map[string]*types.Package{}}
}

func (l *sourceLoader) Import(path string) (*types.Package, error) {
	if p, ok := l.src[path]; ok {
		return p, nil
	}
	return l.gc.Import(path)
}

// syntheticSource is a package under testdata, which go list does not see,
// with optional text edits (file name -> old, new) applied before parsing.
type syntheticSource struct {
	dir  string
	edit map[string][2]string
}

// checkedPkg is one package checked from source.
type checkedPkg struct {
	path  string
	fset  *token.FileSet
	types *types.Package
	info  *types.Info
	decls map[*types.Func]*ast.FuncDecl
}

// check type-checks the package at import path: a listed package from its
// GoFiles, or a synthetic one from its directory.
func (l *sourceLoader) check(t testing.TB, path string, syn *syntheticSource) *checkedPkg {
	t.Helper()
	var dir string
	var names []string
	if syn == nil {
		p, ok := l.pkgs[path]
		if !ok {
			t.Fatalf("%s is not cmd/server or one of its dependencies", path)
		}
		dir, names = p.Dir, p.GoFiles
	} else {
		dir = syn.dir
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".go") {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	var files []*ast.File
	for _, name := range names {
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if syn != nil {
			if e, ok := syn.edit[name]; ok {
				if !bytes.Contains(src, []byte(e[0])) {
					t.Fatalf("%s/%s does not contain %q", dir, name, e[0])
				}
				src = bytes.Replace(src, []byte(e[0]), []byte(e[1]), 1)
			}
		}
		f, err := parser.ParseFile(l.fset, filepath.Join(dir, name), src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		files = append(files, f)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: l}
	tp, err := conf.Check(path, l.fset, files, info)
	if err != nil {
		t.Fatalf("type-check %s: %v", path, err)
	}
	if syn == nil {
		l.src[path] = tp
	}
	cp := &checkedPkg{path: path, fset: l.fset, types: tp, info: info, decls: map[*types.Func]*ast.FuncDecl{}}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := info.Defs[fd.Name].(*types.Func); ok {
					cp.decls[fn] = fd
				}
			}
		}
	}
	return cp
}

// ----------------------------------------------------------------------------
// Rendering the steps

// externalInline names the functions of other packages whose steps are shown
// where main calls them.
var externalInline = []string{"internal/api.NewHandler"}

// renderBootSteps writes the golden text for main() of pkg; others holds the
// source-checked packages whose functions externalInline names.
func renderBootSteps(t testing.TB, pkg *checkedPkg, others map[string]*checkedPkg, module string) string {
	t.Helper()
	return bootStepsHeader + bootStepsBody(t, pkg, others, module)
}

// bootStepsBody is the golden text below its header.
func bootStepsBody(t testing.TB, pkg *checkedPkg, others map[string]*checkedPkg, module string) string {
	t.Helper()
	var mainDecl *ast.FuncDecl
	for fn, fd := range pkg.decls {
		if fn.Name() == "main" && fd.Recv == nil {
			mainDecl = fd
		}
	}
	if mainDecl == nil {
		t.Fatalf("%s has no func main", pkg.path)
	}
	w := &stepWalker{
		module:   module,
		others:   others,
		bindings: map[types.Object]binding{},
		active:   map[*ast.BlockStmt]bool{},
	}
	w.walkList(mainDecl.Body.List, scope{pkg: pkg})
	var b strings.Builder
	b.WriteString("func main()\n")
	for _, line := range w.lines {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

const bootStepsHeader = `# The call statements of cmd/server main() and of internal/api.NewHandler, in
# source order, from the syntax tree (refactor plan §6.4 S4a; invariants I10
# and I17). Written by TestBootSteps (cmd/server/boot_steps_test.go, which
# gives the rules in full); regenerate with
#   UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootSteps$'
#
#   call X       an expression statement calling X (a chain lists each call,
#                innermost first), a call assigned only to blank identifiers,
#                or a call of which an error result is kept, whatever else
#                it keeps (a call keeping only other results is no step)
#   go, defer    a go or defer statement: with X, it starts X; without, the
#                steps of the code it starts follow, indented
#   defer.inner  a defer in an inlined function, run when that one returns
#   inline F     F's own steps follow, indented (internal/api.NewHandler)
#   value v      a call of the function value in variable v
#   (T).M        method M called on a receiver of static type T; after an
#                AddSubscriber or Subscribe*, its arguments' static types
#   if, else, switch, case, default, for, select
#                blocks with a step inside, by nesting, without conditions
#
# Package-level functions of cmd/server, the a.<stage>() methods of its
# wire_*.go files and closures bound to a variable are inlined where a
# statement calls them, and so are internal/api's functions and *Handler
# methods inside NewHandler; a call an inlined function returns counts as made
# by the statement that called it. Calls into log and log/slog are left out.
# No file name, line number, argument or condition appears: moving code
# between files, into helpers or into stages leaves this file as it is;
# moving a step past another one changes it. Being arguments, timer and
# ticker durations (the purge's 24 h, the reaper's first tick at 30 s) are
# not pinned here.

`

type binding struct {
	expr ast.Expr
	pkg  *checkedPkg
}

// scope is where the walk is: the package whose code it reads, the nesting
// depth of the lines it writes, whether it is inside an inlined function
// (whose defers run when that function returns, not when its caller does),
// and where that function's results go.
type scope struct {
	pkg   *checkedPkg
	depth int
	inner bool
	ret   into
}

// into is where a call's results go: the left-hand side of the assignment
// that made the call, in its package, or nothing for an expression statement,
// a go or a defer, which drop them.
type into struct {
	lhs []ast.Expr
	pkg *checkedPkg
}

func (s scope) deeper() scope { s.depth++; return s }

type stepWalker struct {
	module   string
	others   map[string]*checkedPkg
	bindings map[types.Object]binding
	active   map[*ast.BlockStmt]bool
	lines    []string
	pending  []*header
}

// header is a block line (if, case, ...) written only once a step is found
// inside the block.
type header struct {
	text    string
	depth   int
	written bool
}

func (w *stepWalker) open(text string, depth int) *header {
	h := &header{text: text, depth: depth}
	w.pending = append(w.pending, h)
	return h
}

func (w *stepWalker) close(h *header) {
	if n := len(w.pending); n == 0 || w.pending[n-1] != h {
		panic("boot steps: block headers closed out of order")
	}
	w.pending = w.pending[:len(w.pending)-1]
}

func (w *stepWalker) emit(depth int, text string) {
	for _, h := range w.pending {
		if !h.written {
			w.lines = append(w.lines, strings.Repeat("  ", h.depth)+h.text)
			h.written = true
		}
	}
	w.lines = append(w.lines, strings.Repeat("  ", depth)+text)
}

func (w *stepWalker) walkList(list []ast.Stmt, sc scope) {
	for _, s := range list {
		w.walkStmt(s, sc)
	}
}

// walkStmt writes the steps of one statement.
func (w *stepWalker) walkStmt(s ast.Stmt, sc scope) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		if call, ok := unparen(s.X).(*ast.CallExpr); ok {
			w.call(call, into{}, sc)
		}
	case *ast.AssignStmt:
		w.assign(s.Lhs, s.Rhs, sc)
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				lhs := make([]ast.Expr, len(vs.Names))
				for i, n := range vs.Names {
					lhs[i] = n
				}
				if len(vs.Values) > 0 {
					w.assign(lhs, vs.Values, sc)
				}
			}
		}
	case *ast.ReturnStmt:
		w.returns(s.Results, sc)
	case *ast.IfStmt:
		if s.Init != nil {
			w.walkStmt(s.Init, sc)
		}
		h := w.open("if", sc.depth)
		w.walkList(s.Body.List, sc.deeper())
		if s.Else != nil {
			e := w.open("else", sc.depth)
			switch el := s.Else.(type) {
			case *ast.BlockStmt:
				w.walkList(el.List, sc.deeper())
			default:
				w.walkStmt(el, sc.deeper())
			}
			w.close(e)
		}
		w.close(h)
	case *ast.SwitchStmt:
		if s.Init != nil {
			w.walkStmt(s.Init, sc)
		}
		w.clauses("switch", s.Body, sc)
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			w.walkStmt(s.Init, sc)
		}
		w.clauses("switch", s.Body, sc)
	case *ast.SelectStmt:
		w.clauses("select", s.Body, sc)
	case *ast.ForStmt:
		h := w.open("for", sc.depth)
		w.walkList(s.Body.List, sc.deeper())
		w.close(h)
	case *ast.RangeStmt:
		h := w.open("for", sc.depth)
		w.walkList(s.Body.List, sc.deeper())
		w.close(h)
	case *ast.BlockStmt:
		w.walkList(s.List, sc)
	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt, sc)
	case *ast.GoStmt:
		w.start("go", s.Call, sc)
	case *ast.DeferStmt:
		kw := "defer"
		if sc.inner {
			kw = "defer.inner"
		}
		w.start(kw, s.Call, sc)
	}
}

func (w *stepWalker) clauses(kind string, body *ast.BlockStmt, sc scope) {
	h := w.open(kind, sc.depth)
	for _, c := range body.List {
		label, stmts := "case", []ast.Stmt(nil)
		switch c := c.(type) {
		case *ast.CaseClause:
			if c.List == nil {
				label = "default"
			}
			stmts = c.Body
		case *ast.CommClause:
			if c.Comm == nil {
				label = "default"
			}
			stmts = c.Body
		}
		ch := w.open(label, sc.depth+1)
		w.walkList(stmts, sc.deeper().deeper())
		w.close(ch)
	}
	w.close(h)
}

// call handles a call a statement makes, whose results go to dst: inlined,
// shown with its own steps, left out as logging, or written as a step when
// dst drops every result or keeps an error. A function value an inlined
// function returns is remembered in dst for when it is called.
func (w *stepWalker) call(call *ast.CallExpr, dst into, sc scope) {
	if body, pkg := w.inlineTarget(call, sc); body != nil {
		results := w.inline(body, pkg, sc, dst)
		if len(results) == len(dst.lhs) {
			for i := range dst.lhs {
				// A call among the results was handled, into dst, where the
				// return statement was walked.
				if _, ok := unparen(results[i]).(*ast.CallExpr); !ok {
					w.bind(dst.lhs[i], results[i], dst.pkg, pkg)
				}
			}
		}
		return
	}
	for _, e := range dst.lhs {
		if obj := objectOf(e, dst.pkg); obj != nil {
			delete(w.bindings, obj)
		}
	}
	if w.external(call, dst, sc) {
		return
	}
	if w.logging(call, sc.pkg) {
		return
	}
	if allBlank(dst.lhs) || keepsError(dst.lhs, dst.pkg) {
		w.emit(sc.depth, "call "+w.chain(call, sc.pkg))
	}
}

// assign handles an assignment, a definition or a var declaration: a call
// on the right is handled as a call into the left-hand side, and a function
// value on the right is remembered for when it is called.
func (w *stepWalker) assign(lhs, rhs []ast.Expr, sc scope) {
	if len(rhs) == 1 {
		if call, ok := unparen(rhs[0]).(*ast.CallExpr); ok {
			w.call(call, into{lhs: lhs, pkg: sc.pkg}, sc)
			return
		}
	}
	if len(lhs) == len(rhs) {
		for i := range lhs {
			if call, ok := unparen(rhs[i]).(*ast.CallExpr); ok {
				w.call(call, into{lhs: lhs[i : i+1], pkg: sc.pkg}, sc)
				continue
			}
			w.bind(lhs[i], rhs[i], sc.pkg, sc.pkg)
		}
	}
}

// returns handles a return statement: a call among its results is made for
// the statement that called the inlined function, and its results go where
// that statement puts them (sc.ret); in a go or defer body they are dropped.
func (w *stepWalker) returns(results []ast.Expr, sc scope) {
	for i, r := range results {
		call, ok := unparen(r).(*ast.CallExpr)
		if !ok {
			continue
		}
		dst := into{pkg: sc.ret.pkg}
		switch {
		case len(results) == 1:
			dst.lhs = sc.ret.lhs
		case len(results) == len(sc.ret.lhs):
			dst.lhs = sc.ret.lhs[i : i+1]
		}
		w.call(call, dst, sc)
	}
}

// bind remembers that the variable lhs (in lpkg) holds the value of rhs (in
// rpkg) when rhs is a function literal, a method value or another variable.
func (w *stepWalker) bind(lhs, rhs ast.Expr, lpkg, rpkg *checkedPkg) {
	obj := objectOf(lhs, lpkg)
	if obj == nil {
		return
	}
	switch r := unparen(rhs).(type) {
	case *ast.FuncLit, *ast.Ident, *ast.SelectorExpr:
		w.bindings[obj] = binding{expr: r, pkg: rpkg}
	default:
		delete(w.bindings, obj)
	}
}

// objectOf is the variable an assignable expression names: a local or a
// struct field (a stage's a.x).
func objectOf(e ast.Expr, pkg *checkedPkg) types.Object {
	switch e := unparen(e).(type) {
	case *ast.Ident:
		if e.Name == "_" {
			return nil
		}
		return pkg.info.ObjectOf(e)
	case *ast.SelectorExpr:
		if sel := pkg.info.Selections[e]; sel != nil && sel.Kind() == types.FieldVal {
			return sel.Obj()
		}
		return pkg.info.Uses[e.Sel]
	}
	return nil
}

// resolve follows the bindings from a variable to the expression it was
// last given that is not itself a variable (a function literal or a method
// value), and names the last variable on the way.
func (w *stepWalker) resolve(obj types.Object) (ast.Expr, *checkedPkg, string) {
	name := obj.Name()
	for range 16 {
		b, ok := w.bindings[obj]
		if !ok {
			return nil, nil, name
		}
		if sel, ok := b.expr.(*ast.SelectorExpr); ok {
			if s := b.pkg.info.Selections[sel]; s != nil && s.Kind() == types.MethodVal {
				return b.expr, b.pkg, name
			}
		}
		next, isVar := objectOf(b.expr, b.pkg).(*types.Var)
		if !isVar {
			return b.expr, b.pkg, name
		}
		obj, name = next, next.Name()
	}
	return nil, nil, name
}

// inlineTarget is the body a call statement runs that this walk reads in
// place: a function literal, a package-level function of the package, a
// stage method (main) or *Handler method (internal/api), or a variable bound
// to a function literal.
func (w *stepWalker) inlineTarget(call *ast.CallExpr, sc scope) (*ast.BlockStmt, *checkedPkg) {
	info := sc.pkg.info
	switch f := unindex(unparen(call.Fun)).(type) {
	case *ast.FuncLit:
		return f.Body, sc.pkg
	case *ast.Ident:
		switch o := info.Uses[f].(type) {
		case *types.Func:
			if fd := sc.pkg.decls[o]; fd != nil && fd.Recv == nil {
				return fd.Body, sc.pkg
			}
		case *types.Var:
			if e, pkg, _ := w.resolve(o); e != nil {
				if lit, ok := e.(*ast.FuncLit); ok {
					return lit.Body, pkg
				}
			}
		}
	case *ast.SelectorExpr:
		sel := info.Selections[f]
		if sel == nil {
			return nil, nil
		}
		switch sel.Kind() {
		case types.MethodVal:
			fn := sel.Obj().(*types.Func)
			if fd := sc.pkg.decls[fn]; fd != nil && inlinedMethod(sc.pkg, fn, fd) {
				return fd.Body, sc.pkg
			}
		case types.FieldVal:
			if e, pkg, _ := w.resolve(sel.Obj()); e != nil {
				if lit, ok := e.(*ast.FuncLit); ok {
					return lit.Body, pkg
				}
			}
		}
	}
	return nil, nil
}

// inlinedMethod says which methods of a package are read in place: in
// package main, the stages (methods declared in a wire_*.go file); in
// internal/api, the methods of Handler.
func inlinedMethod(pkg *checkedPkg, fn *types.Func, fd *ast.FuncDecl) bool {
	if pkg.types.Name() == "main" {
		base := filepath.Base(pkg.fset.Position(fd.Pos()).Filename)
		return strings.HasPrefix(base, "wire_") && strings.HasSuffix(base, ".go")
	}
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == "Handler"
}

// inline walks a body in place, its results going to dst, and returns the
// results of its final return.
func (w *stepWalker) inline(body *ast.BlockStmt, pkg *checkedPkg, sc scope, dst into) []ast.Expr {
	if w.active[body] {
		w.emit(sc.depth, "recursive call")
		return nil
	}
	w.active[body] = true
	defer delete(w.active, body)
	w.walkList(body.List, scope{pkg: pkg, depth: sc.depth, inner: true, ret: dst})
	if n := len(body.List); n > 0 {
		if r, ok := body.List[n-1].(*ast.ReturnStmt); ok {
			return r.Results
		}
	}
	return nil
}

// external writes a function of another package that externalInline names,
// with its steps, its results going to dst, and reports whether call was one.
func (w *stepWalker) external(call *ast.CallExpr, dst into, sc scope) bool {
	fn := calledFunc(call, sc.pkg)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	name := w.pkgName(fn.Pkg().Path()) + "." + fn.Name()
	for _, want := range externalInline {
		if name != want {
			continue
		}
		other := w.others[fn.Pkg().Path()]
		if other == nil {
			return false
		}
		for f, fd := range other.decls {
			if f.Name() == fn.Name() && fd.Recv == nil {
				w.emit(sc.depth, "inline "+name)
				w.inline(fd.Body, other, scope{pkg: other, depth: sc.depth + 1}, dst)
				return true
			}
		}
	}
	return false
}

// start writes a go or defer statement.
func (w *stepWalker) start(kw string, call *ast.CallExpr, sc scope) {
	if body, pkg := w.inlineTarget(call, sc); body != nil {
		if len(body.List) == 1 {
			var only ast.Expr
			switch s := body.List[0].(type) {
			case *ast.ExprStmt:
				only = s.X
			case *ast.ReturnStmt:
				if len(s.Results) == 1 {
					only = s.Results[0]
				}
			}
			if inner, ok := unparen(only).(*ast.CallExpr); ok {
				w.start(kw, inner, scope{pkg: pkg, depth: sc.depth, inner: sc.inner})
				return
			}
		}
		if w.active[body] {
			w.emit(sc.depth, kw+" recursive call")
			return
		}
		w.emit(sc.depth, kw)
		w.active[body] = true
		w.walkList(body.List, scope{pkg: pkg, depth: sc.depth + 1})
		delete(w.active, body)
		return
	}
	if w.logging(call, sc.pkg) {
		return
	}
	w.emit(sc.depth, kw+" "+w.chain(call, sc.pkg))
}

// chain describes a call and the calls it is chained on, innermost first.
func (w *stepWalker) chain(call *ast.CallExpr, pkg *checkedPkg) string {
	var parts []string
	var walk func(c *ast.CallExpr)
	walk = func(c *ast.CallExpr) {
		fun := unindex(unparen(c.Fun))
		if sel, ok := fun.(*ast.SelectorExpr); ok {
			if inner, ok := unparen(sel.X).(*ast.CallExpr); ok {
				walk(inner)
			}
		}
		if inner, ok := fun.(*ast.CallExpr); ok {
			walk(inner)
			parts = append(parts, "(its result)")
			return
		}
		parts = append(parts, w.callee(c, pkg)+w.subscriberArgs(c, pkg))
	}
	walk(call)
	return strings.Join(parts, " -> ")
}

// subscriberArgs writes the static types of the arguments of an
// AddSubscriber or Subscribe* call, which say who subscribes, so that two
// subscriptions to the same service in another order read differently.
func (w *stepWalker) subscriberArgs(c *ast.CallExpr, pkg *checkedPkg) string {
	fn := calledFunc(c, pkg)
	if fn == nil || (fn.Name() != "AddSubscriber" && !strings.HasPrefix(fn.Name(), "Subscribe")) {
		return ""
	}
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		args[i] = types.TypeString(pkg.info.TypeOf(a), w.qualifier)
	}
	return "(" + strings.Join(args, ", ") + ")"
}

// callee names what one call calls: a function by package path and name, a
// method by the static type of its receiver, a function value by the
// variable holding it (or what that variable was bound to).
func (w *stepWalker) callee(c *ast.CallExpr, pkg *checkedPkg) string {
	info := pkg.info
	switch f := unindex(unparen(c.Fun)).(type) {
	case *ast.SelectorExpr:
		if sel := info.Selections[f]; sel != nil {
			switch sel.Kind() {
			case types.MethodVal, types.MethodExpr:
				return "(" + types.TypeString(sel.Recv(), w.qualifier) + ")." + f.Sel.Name
			case types.FieldVal:
				return w.value(sel.Obj())
			}
		}
		switch o := info.Uses[f.Sel].(type) {
		case *types.Func:
			return w.pkgName(o.Pkg().Path()) + "." + o.Name()
		case *types.Var:
			return w.value(o)
		}
	case *ast.Ident:
		switch o := info.Uses[f].(type) {
		case *types.Func:
			return w.pkgName(o.Pkg().Path()) + "." + o.Name()
		case *types.Var:
			return w.value(o)
		case *types.Builtin:
			return "builtin " + o.Name()
		case *types.TypeName:
			return "conversion to " + types.TypeString(o.Type(), w.qualifier)
		}
	case *ast.FuncLit:
		return "func literal"
	}
	return fmt.Sprintf("unresolved %T", c.Fun)
}

// value names a call of the function value in a variable, following its
// bindings to a method value or function where they lead to one.
func (w *stepWalker) value(obj types.Object) string {
	e, pkg, name := w.resolve(obj)
	if e != nil {
		switch e := e.(type) {
		case *ast.SelectorExpr:
			if sel := pkg.info.Selections[e]; sel != nil && sel.Kind() == types.MethodVal {
				return "(" + types.TypeString(sel.Recv(), w.qualifier) + ")." + e.Sel.Name
			}
			if fn, ok := pkg.info.Uses[e.Sel].(*types.Func); ok {
				return w.pkgName(fn.Pkg().Path()) + "." + fn.Name()
			}
		case *ast.Ident:
			if fn, ok := pkg.info.Uses[e].(*types.Func); ok {
				return w.pkgName(fn.Pkg().Path()) + "." + fn.Name()
			}
		case *ast.FuncLit:
			return "value " + name + " (a func literal)"
		}
	}
	return "value " + name
}

// logging reports whether a call (the last of its chain) goes into log or
// log/slog.
func (w *stepWalker) logging(call *ast.CallExpr, pkg *checkedPkg) bool {
	fn := calledFunc(call, pkg)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	p := fn.Pkg().Path()
	return p == "log" || p == "log/slog"
}

// calledFunc is the function or method a call calls, when it names one.
func calledFunc(call *ast.CallExpr, pkg *checkedPkg) *types.Func {
	switch f := unindex(unparen(call.Fun)).(type) {
	case *ast.SelectorExpr:
		if sel := pkg.info.Selections[f]; sel != nil {
			fn, _ := sel.Obj().(*types.Func)
			return fn
		}
		fn, _ := pkg.info.Uses[f.Sel].(*types.Func)
		return fn
	case *ast.Ident:
		fn, _ := pkg.info.Uses[f].(*types.Func)
		return fn
	}
	return nil
}

// pkgName writes a package path relative to the module when it is in it.
func (w *stepWalker) pkgName(path string) string {
	if path == w.module {
		return "(module root)"
	}
	return strings.TrimPrefix(path, w.module+"/")
}

func (w *stepWalker) qualifier(p *types.Package) string { return w.pkgName(p.Path()) }

func allBlank(lhs []ast.Expr) bool {
	for _, e := range lhs {
		if id, ok := e.(*ast.Ident); !ok || id.Name != "_" {
			return false
		}
	}
	return true
}

// keepsError reports whether a left-hand side (in pkg) keeps an error: a
// local, a field (a stage's a.err) or any other place of type error.
func keepsError(lhs []ast.Expr, pkg *checkedPkg) bool {
	errType := types.Universe.Lookup("error").Type()
	for _, e := range lhs {
		var t types.Type
		if id, ok := unparen(e).(*ast.Ident); ok {
			if id.Name == "_" {
				continue
			}
			if obj := pkg.info.ObjectOf(id); obj != nil {
				t = obj.Type()
			}
		} else {
			t = pkg.info.TypeOf(e)
		}
		if t != nil && types.Identical(t, errType) {
			return true
		}
	}
	return false
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func unindex(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.IndexExpr:
		return x.X
	case *ast.IndexListExpr:
		return x.X
	}
	return e
}
