package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The refactor plan's S14 row asks each window generator for a test that
// runs it on d11dee8 and gets a result that compiles and passes
// internal/archtest with no ratchets.json entry added. CI checks out one
// commit, so d11dee8 is not there to read, and cmd/server's main.go has
// moved on since it; M4's spec is written against today's main.go, as M4
// will run on master. So TestM4OnTheWorkingTree generates M4 on a copy of
// the working tree's module and proves it as M4's pull request would.

// m4Spec is M4's committed spec.
const m4Spec = "specs/M4.json"

// TestM4OnTheWorkingTree splits cmd/server's main() with M4's spec on a
// copy of this module and proves the result: it builds and vets; the S4
// boot steps (boot_steps.txt, which reads stages and fields through) are
// byte for byte the committed golden; internal/archtest passes, and
// UPDATE_RATCHETS=1 only lowers or removes entries, among them
// cmd/server/main.go's and cmd/server:main's; movecheck -flatten main
// -base HEAD, with its S14c normalisation, exits 0 on the split and 1 when
// a statement moves across a stage boundary or one is dropped; and, with
// OPENV_TEST_DATABASE_URL set, the S4 boot profiles, misconfigured boots
// and smoke test pass against their goldens. It skips under -short, once
// M4 has merged (cmd/server has wire_*.go files), and when stageextract
// refuses for a reason the spec or main()'s shape gives (errPlan), which
// it names: that is M4's author's to settle in the spec when regenerating
// the split (rule R4), never a feature pull request's (§8.4). A proof that
// fails after the split still fails the test.
func TestM4OnTheWorkingTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests a copy of the module; run without -short")
	}
	root, err := moduleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if wires, _ := filepath.Glob(filepath.Join(root, "cmd", "server", "wire_*.go")); len(wires) > 0 {
		t.Skipf("M4 has merged: cmd/server holds %s", filepath.Base(wires[0]))
	}
	untouched := snapshot(t, filepath.Join(root, "cmd", "server"))
	copyRoot := copyModule(t, root)
	gitInit(t, copyRoot)
	server := filepath.Join(copyRoot, "cmd", "server")
	sp, err := loadSpec(m4Spec)
	if err != nil {
		t.Fatal(err)
	}
	res, err := generate(server, sp, options{vet: goVet})
	if errors.Is(err, errPlan) {
		t.Skipf("M4 does not generate on the working tree: %v\n"+
			"Not this pull request's to fix (§8.4): M4's author edits the spec, internal/tools/stageextract/%s, as the "+
			"message says, when regenerating the split on the latest master (rule R4)", err, m4Spec)
	}
	if err != nil {
		t.Fatalf("M4 does not generate on the working tree: %v", err)
	}
	for _, l := range res.lines {
		t.Log(l)
	}
	goCmd(t, copyRoot, nil, "build", "-o", os.DevNull, "./cmd/server")
	goCmd(t, copyRoot, nil, "test", "-count=1", "-run", "^TestBootSteps$", "./cmd/server")
	goCmd(t, copyRoot, nil, "test", "-count=1", "./internal/archtest")
	checkRatchetsAfterM4(t, copyRoot)
	if code, out := movecheck(t, server); code != 0 || !strings.Contains(out, "once the stage rewrites are undone") {
		t.Fatalf("movecheck -flatten main -base HEAD on the split: exit %d\n%s", code, out)
	} else {
		t.Log(strings.TrimSpace(out))
	}
	checkMovecheckCatches(t, server, sp)
	if os.Getenv("OPENV_TEST_DATABASE_URL") != "" {
		goCmd(t, copyRoot, nil, "test", "-count=1", "-run", "^TestBoot", "./cmd/server")
	} else {
		t.Log("OPENV_TEST_DATABASE_URL is unset: the S4 boot profiles were not run on the split")
	}
	if after := snapshot(t, filepath.Join(root, "cmd", "server")); fmt.Sprint(after) != fmt.Sprint(untouched) {
		t.Error("the test changed the working tree's cmd/server")
	}
}

// checkMovecheckCatches plants the two faults the plan's S14c row names in
// the split and requires movecheck to fail on each: the last statement of
// one stage moved after the first of the stage that follows it, and a
// statement dropped. Each fault is undone after.
func checkMovecheckCatches(t *testing.T, dir string, sp *spec) {
	t.Helper()
	stmts := stageStatements(t, dir, sp)
	var from, to int = -1, -1
	var last, first stmtLines
	for i := 0; i+1 < len(sp.Stages) && from < 0; i++ {
		a, b := stmts[sp.Stages[i].Name], stmts[sp.Stages[i+1].Name]
		if len(a) < 2 || a[len(a)-1].ret || a[len(a)-1].decl {
			continue
		}
		for _, s := range b {
			if !s.decl {
				from, to, last, first = i, i+1, a[len(a)-1], s
				break
			}
		}
	}
	if from < 0 {
		t.Fatal("no two adjacent stages to swap a statement between")
	}
	files := map[string]string{}
	for _, name := range []string{last.file, first.file} {
		files[name] = readFile(t, filepath.Join(dir, name))
	}
	restore := func() {
		for name, src := range files {
			writeFile(t, filepath.Join(dir, name), src)
		}
	}
	// The swap: the last statement of one stage follows the first of the
	// next.
	moved := lines(files[last.file], last.from, last.to)
	src := cutLines(files[last.file], last.from, last.to)
	if first.file == last.file {
		shift := 0
		if first.from > last.to {
			shift = last.to - last.from + 1
		}
		src = insertAfter(src, first.to-shift, moved)
		writeFile(t, filepath.Join(dir, last.file), src)
	} else {
		writeFile(t, filepath.Join(dir, last.file), src)
		writeFile(t, filepath.Join(dir, first.file), insertAfter(files[first.file], first.to, moved))
	}
	code, out := movecheck(t, dir)
	t.Logf("the last statement of stage %s moved after a statement of stage %s:\n%s", sp.Stages[from].Name, sp.Stages[to].Name, out)
	if code != 1 || !strings.Contains(out, "flattens to other statements") {
		t.Errorf("the last statement of stage %s moved after a statement of stage %s: exit %d, want 1\n%s",
			sp.Stages[from].Name, sp.Stages[to].Name, code, out)
	}
	restore()
	// The drop: the stage after's first statement other than a var.
	writeFile(t, filepath.Join(dir, first.file), cutLines(files[first.file], first.from, first.to))
	code, out = movecheck(t, dir)
	t.Logf("a statement of stage %s dropped:\n%s", sp.Stages[to].Name, out)
	if code != 1 || !strings.Contains(out, "flattens to other statements") {
		t.Errorf("a statement of stage %s dropped: exit %d, want 1\n%s", sp.Stages[to].Name, code, out)
	}
	restore()
	if code, out := movecheck(t, dir); code != 0 {
		t.Errorf("the split, restored: exit %d\n%s", code, out)
	}
}

// stmtLines is a top-level statement of a stage: its file and lines, and
// whether it is a return or a declaration.
type stmtLines struct {
	file      string
	from, to  int
	ret, decl bool
}

// stageStatements lists each stage method's top-level statements.
func stageStatements(t *testing.T, dir string, sp *spec) map[string][]stmtLines {
	t.Helper()
	out := map[string][]stmtLines{}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, st := range sp.Stages {
		if seen[st.File] {
			continue
		}
		seen[st.File] = true
		af, err := parser.ParseFile(fset, filepath.Join(dir, st.File), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil {
				continue
			}
			for _, s := range fd.Body.List {
				_, ret := s.(*ast.ReturnStmt)
				_, decl := s.(*ast.DeclStmt)
				out[fd.Name.Name] = append(out[fd.Name.Name], stmtLines{file: st.File,
					from: fset.Position(s.Pos()).Line, to: fset.Position(s.End()).Line, ret: ret, decl: decl})
			}
		}
	}
	return out
}

func lines(src string, from, to int) string {
	l := strings.SplitAfter(src, "\n")
	return strings.Join(l[from-1:to], "")
}

func cutLines(src string, from, to int) string {
	l := strings.SplitAfter(src, "\n")
	return strings.Join(append(append([]string{}, l[:from-1]...), l[to:]...), "")
}

func insertAfter(src string, line int, text string) string {
	l := strings.SplitAfter(src, "\n")
	return strings.Join(append(append(append([]string{}, l[:line]...), text), l[line:]...), "")
}

// checkRatchetsAfterM4 runs archtest's regenerate command on the split and
// requires every ratchets.json entry to stay or fall, and the two entries
// M4's row names, cmd/server/main.go's file_lines and cmd/server:main's
// func_lines, to fall or go.
func checkRatchetsAfterM4(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "internal", "archtest", "ratchets.json")
	var before, after map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &before); err != nil {
		t.Fatal(err)
	}
	goCmd(t, root, []string{"UPDATE_RATCHETS=1"}, "test", "-count=1", "-run", "^TestArchitecture$", "./internal/archtest")
	if err := json.Unmarshal([]byte(readFile(t, path)), &after); err != nil {
		t.Fatal(err)
	}
	for _, g := range ratchetGrowth(before, after) {
		t.Errorf("ratchets.json after the split: %s", g)
	}
	for _, k := range [][2]string{{"file_lines", "cmd/server/main.go"}, {"func_lines", "cmd/server:main"}} {
		b, _ := before[k[0]].(map[string]any)
		a, _ := after[k[0]].(map[string]any)
		bv, had := b[k[1]].(float64)
		av, has := a[k[1]].(float64)
		switch {
		case !had:
			t.Logf("ratchets.json has no %s entry for %s", k[0], k[1])
		case !has:
			t.Logf("the split lets M4 remove ratchets.json's %s entry for %s (%v)", k[0], k[1], bv)
		case av < bv:
			t.Logf("the split lets M4 lower ratchets.json's %s entry for %s from %v to %v", k[0], k[1], bv, av)
		default:
			t.Errorf("ratchets.json's %s entry for %s stays %v after the split; M4 must lower or remove it", k[0], k[1], bv)
		}
	}
}

// ratchetGrowth lists what after holds beyond before: an entry added, or a
// number raised.
func ratchetGrowth(before, after any) []string {
	var out []string
	var walk func(path string, b, a any)
	walk = func(path string, b, a any) {
		switch av := a.(type) {
		case map[string]any:
			bm, _ := b.(map[string]any)
			for k, v := range av {
				bv, ok := bm[k]
				if !ok {
					out = append(out, path+"/"+k+" added")
					continue
				}
				walk(path+"/"+k, bv, v)
			}
		case []any:
			have := map[string]bool{}
			bl, _ := b.([]any)
			for _, x := range bl {
				have[fmt.Sprint(x)] = true
			}
			for _, x := range av {
				if !have[fmt.Sprint(x)] {
					out = append(out, fmt.Sprintf("%s: %v added", path, x))
				}
			}
		case float64:
			if bv, ok := b.(float64); !ok || av > bv {
				out = append(out, fmt.Sprintf("%s raised from %v to %v", path, b, av))
			}
		}
	}
	walk("", before, after)
	sort.Strings(out)
	return out
}

// TestRatchetGrowth: what checkRatchetsAfterM4 calls growth.
func TestRatchetGrowth(t *testing.T) {
	var before, after any
	if err := json.Unmarshal([]byte(`{"file_lines": {"a.go": 900, "b.go": 850}, "import_edges": {"p": ["q"]}}`), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"file_lines": {"a.go": 950, "c.go": 810}, "import_edges": {"p": ["q", "r"]}, "new": {}}`), &after); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ratchetGrowth(before, after), "\n")
	want := "/file_lines/a.go raised from 900 to 950\n/file_lines/c.go added\n/import_edges/p: r added\n/new added"
	if got != want {
		t.Errorf("growth:\n%s\nwant:\n%s", got, want)
	}
	var shrunk any
	if err := json.Unmarshal([]byte(`{"file_lines": {"a.go": 800}, "import_edges": {"p": []}}`), &shrunk); err != nil {
		t.Fatal(err)
	}
	if g := ratchetGrowth(before, shrunk); len(g) != 0 {
		t.Errorf("lowering and removing entries is growth: %v", g)
	}
}

// copyModule copies what the go command, internal/archtest and the boot
// harness read of the module at root: its top-level files but the hidden
// ones, and cmd, internal, examples and contracts, leaving out VCS and
// dependency directories.
func copyModule(t *testing.T, root string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		// Not .git, which in a linked worktree is a file naming the
		// repository's git directory: the copy gets a repository of its own.
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			copyFile(t, filepath.Join(root, e.Name()), filepath.Join(dst, e.Name()))
		}
	}
	for _, d := range []string{"cmd", "internal", "examples", "contracts"} {
		err := filepath.WalkDir(filepath.Join(root, d), func(p string, e os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			if e.IsDir() {
				if n := e.Name(); n == "node_modules" || (strings.HasPrefix(n, ".") && n != ".") {
					return filepath.SkipDir
				}
				return os.MkdirAll(filepath.Join(dst, rel), 0o755)
			}
			if e.Type().IsRegular() {
				copyFile(t, p, filepath.Join(dst, rel))
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return dst
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}
