package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The refactor plan's S14 row asks each window generator for a test that
// runs it on d11dee8 and gets a result that compiles and passes
// internal/archtest with no ratchets.json entry added. CI checks out one
// commit, so d11dee8 is not there to read: TestM11aOnD11dee8 runs on a
// committed copy of d11dee8's internal/mcp (checked against git's copy
// wherever d11dee8 is reachable), and TestM11aOnTheWorkingTree runs M11a
// for real on a copy of the working tree's module, as M11a will run on
// master, with every proof the row and M11's row name: go vet, go test of
// internal/mcp (readonly_test.go among them), cmd/openv-mcp,
// internal/seeds (interviewer_test.go) and internal/archtest, the ratchets
// and the S7 goldens.

// d11dee8 is the commit the plan pins its invariants at.
const d11dee8 = "d11dee8815c1486286253e6c1f1c2986ec78cce6"

// m11aSpec is M11a's committed spec.
const m11aSpec = "specs/M11a.json"

// artifactsStub stands in for internal/domain/artifacts in the d11dee8
// module: the two names d11dee8's internal/mcp uses, with their signatures.
const artifactsStub = `// Package artifacts stands in for internal/domain/artifacts in splittools's
// TestM11aOnD11dee8: the two names d11dee8's internal/mcp uses.
package artifacts

// NormalizeRef stands in for the real one.
func NormalizeRef(query string) string { return query }

// ParseRef stands in for the real one.
func ParseRef(ref string) (prefix string, num int, ok bool) { return "", 0, false }
`

// TestM11aOnD11dee8 splits d11dee8's tool table with M11a's spec in a module
// holding d11dee8's internal/mcp and a stub of the one module package it
// imports, and requires a result that vets, fits K14 with R6's headroom,
// moves every entry's lines byte for byte in today's order, and leaves the
// rest of tools.go as it was.
func TestM11aOnD11dee8(t *testing.T) {
	checkFixtureIsD11dee8(t)
	mcp, orig, res := splitD11dee8(t, nil)
	joined := strings.Join(res.lines, "\n")
	if !strings.Contains(joined, "splittools: 31 entries of internal/mcp's Tools() into ") ||
		!strings.Contains(joined, "splittools: go vet ./internal/mcp passed") {
		t.Errorf("the report:\n%s", joined)
	}
	if n := strings.Count(joined, " -> "); n != 31 {
		t.Errorf("the map has %d lines, want one per tool of d11dee8's table, 31", n)
	}
	checkM11aOutput(t, orig, mcp)
	checkHeadroom(t, snapshot(t, mcp))
	if _, err := generate(mcp, loadM11aSpec(t), budgets()); !errors.Is(err, errAlreadySplit) {
		t.Errorf("a second run: %v, want errAlreadySplit", err)
	}
}

// TestM11aBlankLineAtABoundary: a blank line between the last entry of one
// constructor and the first of the next is dropped with that entry's
// leading blank lines, and the independent check still accepts the split.
func TestM11aBlankLineAtABoundary(t *testing.T) {
	boundary := "\t\t},\n\t\t{\n\t\t\tName:        \"search_artifacts\","
	mcp, orig, _ := splitD11dee8(t, func(src string) string {
		if strings.Count(src, boundary) != 1 {
			t.Fatalf("d11dee8's tools.go has no single %q", boundary)
		}
		return strings.Replace(src, boundary, "\t\t},\n\n\t\t{\n\t\t\tName:        \"search_artifacts\",", 1)
	})
	if !strings.Contains(orig, "\t\t},\n\n\t\t{\n\t\t\tName:        \"search_artifacts\",") {
		t.Fatal("the blank line was not added")
	}
	checkM11aOutput(t, orig, mcp)
}

// splitD11dee8 runs M11a's spec, with go vet, on a module holding d11dee8's
// internal/mcp, its tools.go edited first when edit is set, and returns the
// package directory, tools.go as it was before the split, and the report.
func splitD11dee8(t *testing.T, edit func(string) string) (string, string, *result) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/openv/requirements-platform\n\ngo 1.25\n")
	mcp := filepath.Join(root, "internal", "mcp")
	stub := filepath.Join(root, "internal", "domain", "artifacts")
	for _, d := range []string{mcp, stub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(stub, "artifacts.go"), artifactsStub)
	fixture, err := filepath.Glob("testdata/d11dee8/*.go")
	if err != nil || len(fixture) == 0 {
		t.Fatalf("no d11dee8 fixture: %v", err)
	}
	for _, f := range fixture {
		src := readFile(t, f)
		if edit != nil && filepath.Base(f) == "tools.go" {
			src = edit(src)
		}
		writeFile(t, filepath.Join(mcp, filepath.Base(f)), src)
	}
	orig := readFile(t, filepath.Join(mcp, "tools.go"))
	res, err := generate(mcp, loadM11aSpec(t), options{maxFunc: funcBudget, maxFile: fileBudget, vet: goVet})
	if err != nil {
		t.Fatal(err)
	}
	return mcp, orig, res
}

func loadM11aSpec(t *testing.T) *spec {
	t.Helper()
	sp, err := loadSpec(m11aSpec)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// headroomBudget is K14's function budget less R6's 10% headroom: on
// d11dee8's table, no constructor of M11a's spec comes nearer to K14's 100
// lines than this, so that a tool added before or after M11a merges fits
// the constructor it joins.
const headroomBudget = archtestFuncBudget * 9 / 10

// checkHeadroom requires every constructor of a split to span at most
// headroomBudget lines.
func checkHeadroom(t *testing.T, files map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	n := 0
	for name, src := range files {
		if !strings.HasPrefix(name, "tools_") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				n++
				if l := fset.Position(fd.End()).Line - fset.Position(fd.Pos()).Line + 1; l > headroomBudget {
					t.Errorf("%s: %s spans %d lines on d11dee8's table, over %d (K14's %d less R6's 10%% headroom); "+
						"give some of its tools a constructor of their own in %s", name, fd.Name.Name, l, headroomBudget,
						archtestFuncBudget, m11aSpec)
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no constructor in any tools_*.go file")
	}
}

// checkFixtureIsD11dee8 compares testdata/d11dee8 with d11dee8's
// internal/mcp production files, where the commit is reachable (a full
// clone); a shallow clone, like CI's, relies on the committed copy.
func checkFixtureIsD11dee8(t *testing.T) {
	t.Helper()
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = "."
		return cmd.Output()
	}
	if _, err := git("cat-file", "-e", d11dee8+"^{commit}"); err != nil {
		t.Logf("d11dee8 is not reachable (a shallow clone): testdata/d11dee8 stands in for it unchecked")
		return
	}
	listing, err := git("ls-tree", "--name-only", "--full-tree", d11dee8, "internal/mcp/")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, p := range strings.Fields(string(listing)) {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			want = append(want, filepath.Base(p))
			blob, err := git("show", d11dee8+":"+p)
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join("testdata", "d11dee8", filepath.Base(p))); got != string(blob) {
				t.Errorf("testdata/d11dee8/%s differs from %s at d11dee8", filepath.Base(p), p)
			}
		}
	}
	have, _ := filepath.Glob("testdata/d11dee8/*.go")
	for i := range have {
		have[i] = filepath.Base(have[i])
	}
	sort.Strings(want)
	sort.Strings(have)
	if strings.Join(have, " ") != strings.Join(want, " ") {
		t.Errorf("testdata/d11dee8 holds %v; d11dee8's internal/mcp production files are %v", have, want)
	}
}

// ctorBody matches a constructor as splittools writes it.
var ctorBody = regexp.MustCompile(`(?s)\nfunc (\w+)\(\) \[\]Tool \{\n\treturn \[\]Tool\{\n(.*?)\n\t\}\n\}\n`)

// checkM11aOutput checks a split internal/mcp against the tools.go it came
// from, independently of splittools's own self-check: the constructors'
// literal bodies, in the order Tools() concatenates them, are the old
// literal's body byte for byte, but for the blank lines above each
// constructor's first entry, which splittools drops by design; tools.go is
// the old one with only its imports and Tools()'s body changed; every
// function and every new file fits K14's budgets, measured as
// internal/archtest measures them.
func checkM11aOutput(t *testing.T, orig, dir string) {
	t.Helper()
	files := snapshot(t, dir)
	tools := files["tools.go"]
	head := "func Tools() []Tool {\n\treturn "
	i := strings.Index(orig, head+"[]Tool{\n")
	if i < 0 {
		t.Fatal("the original tools.go has no Tools() literal")
	}
	body := orig[i+len(head+"[]Tool{\n"):]
	body = body[:strings.Index(body, "\n\t}\n}\n")]
	j := strings.Index(tools, head+"slices.Concat(\n")
	if j < 0 {
		t.Fatalf("tools.go's Tools() is not a slices.Concat:\n%s", tools)
	}
	call := tools[j+len(head+"slices.Concat(\n"):]
	call = call[:strings.Index(call, "\t)\n}\n")]
	bodies := map[string]string{}
	for name, src := range files {
		if strings.HasPrefix(name, "tools_") && !strings.HasSuffix(name, "_test.go") {
			for _, m := range ctorBody.FindAllStringSubmatch(src, -1) {
				bodies[m[1]] = m[2]
			}
		}
	}
	var names, joined []string
	for _, line := range strings.Split(strings.TrimSuffix(call, "\n"), "\n") {
		name := strings.TrimSuffix(strings.TrimSpace(line), "(),")
		b, ok := bodies[name]
		if !ok {
			t.Fatalf("Tools() concatenates %s, which no tools_*.go file declares as splittools writes it", name)
		}
		names, joined = append(names, name), append(joined, b)
	}
	if d := literalDiff(names, joined, body); d != "" {
		t.Errorf("the constructors' bodies, in Tools() order, are not the old literal's body byte for byte "+
			"(blank lines above a constructor's first entry aside): %s", d)
	}
	rest := func(src, tail string) string {
		k := strings.Index(src, head)
		end := strings.Index(src[k:], tail)
		out := src[:k] + src[k+end+len(tail):]
		a, b := strings.Index(out, "\nimport ("), strings.Index(out, "\n)\n")
		return out[:a] + out[b:]
	}
	if rest(orig, "\n\t}\n}\n") != rest(tools, "\n\t)\n}\n") {
		t.Error("tools.go changed outside its imports and Tools()'s body")
	}
	checkK14(t, files)
}

// literalDiff returns "" when the constructors' bodies, named by names, are
// in order the old literal's body: each constructor's lines follow the
// previous one's byte for byte, and the old body may only have blank lines
// more, where a constructor starts. Otherwise it names the first
// constructor that parts from the old body and the first line where it
// does, or the old body's first line that no constructor holds.
func literalDiff(names, bodies []string, old string) string {
	rest := old
	for i, b := range bodies {
		for {
			line, after, found := strings.Cut(rest, "\n")
			if !found || strings.TrimSpace(line) != "" {
				break
			}
			rest = after
		}
		after, ok := strings.CutPrefix(rest, b)
		if ok && i < len(bodies)-1 {
			after, ok = strings.CutPrefix(after, "\n")
		}
		if !ok {
			return fmt.Sprintf("constructor %s parts from the old literal (-), counting from its first line:\n%s", names[i], lineDiff(rest, b))
		}
		rest = after
	}
	if rest != "" {
		line, _, _ := strings.Cut(strings.TrimPrefix(rest, "\n"), "\n")
		return fmt.Sprintf("the old literal goes on after the last constructor's body: %q", line)
	}
	return ""
}

// archtest's K14 budgets (internal/archtest/size_test.go), restated here so
// that the check does not lean on the tool's own constants.
const (
	archtestFuncBudget = 100
	archtestFileBudget = 800
)

// checkK14 measures every function and new file of a split package as
// internal/archtest's size rules do.
func checkK14(t *testing.T, files map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	for name, src := range files {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.Contains(name, "/") {
			continue
		}
		af, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(name, "tools_") && fd.Name.Name != "Tools" {
				continue
			}
			if n := fset.Position(fd.End()).Line - fset.Position(fd.Pos()).Line + 1; n > archtestFuncBudget {
				t.Errorf("%s: %s spans %d lines, over K14's %d", name, fd.Name.Name, n, archtestFuncBudget)
			}
		}
		if strings.HasPrefix(name, "tools_") {
			if n := countLines([]byte(src)); n > archtestFileBudget {
				t.Errorf("%s has %d lines, over K14's %d", name, n, archtestFileBudget)
			}
		}
	}
}

// TestM11aOnTheWorkingTree generates M11a on a copy of this module, as the
// M11a pull request will on master, and proves it: go vet passes on
// internal/mcp and cmd/openv-mcp; go test passes on internal/mcp,
// cmd/openv-mcp, internal/seeds and internal/archtest; UPDATE_RATCHETS=1
// only lowers or removes ratchets.json entries; and the MCP goldens under
// internal/mcp/testdata (S7), rewritten with UPDATE_GOLDEN=1, are byte for
// byte the committed ones. It skips once M11a has merged, when Tools() no
// longer returns a literal, and when splittools refuses for a reason the
// spec or the table's shape gives (errPlan), such as a feature pull request
// growing a constructor past K14's budget: that is M11a's to settle by
// editing the spec (rule R4), and never fails a feature pull request
// (§8.4). A proof that fails after the split still fails the test.
func TestM11aOnTheWorkingTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests a copy of the module; run without -short")
	}
	root, err := moduleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "internal", "mcp")
	srcs, err := readDir(src)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parsePkg(srcs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.readTable("Tools"); errors.Is(err, errAlreadySplit) {
		t.Skipf("nothing to generate: %v", err)
	} else if err != nil {
		skipRefusal(t, err) // a shape splittools cannot move whole
	}
	logD11dee8Identity(t, src)
	untouched := snapshot(t, src)
	copyRoot := copyModule(t, root)
	mcp := filepath.Join(copyRoot, "internal", "mcp")
	sp, err := loadSpec(m11aSpec)
	if err != nil {
		t.Fatal(err)
	}
	res, err := generate(mcp, sp, options{maxFunc: funcBudget, maxFile: fileBudget, vet: goVet})
	if errors.Is(err, errPlan) {
		skipRefusal(t, err)
	}
	if err != nil {
		t.Fatalf("M11a does not generate on the working tree: %v", err)
	}
	for _, l := range res.lines {
		if strings.HasPrefix(l, "splittools: note: ") {
			t.Log(l)
		}
	}
	checkM11aOutput(t, untouched["tools.go"], mcp)
	goCmd(t, copyRoot, "vet", "./internal/mcp", "./cmd/openv-mcp")
	goCmd(t, copyRoot, "test", "-count=1", "./internal/mcp", "./cmd/openv-mcp", "./internal/seeds", "./internal/archtest")
	checkRatchetsOnlyShrink(t, copyRoot)
	goldens := snapshot(t, filepath.Join(src, "testdata"))
	goRun(t, copyRoot, []string{"UPDATE_GOLDEN=1"}, "test", "-count=1", "./internal/mcp")
	regenerated := snapshot(t, filepath.Join(mcp, "testdata"))
	if strings.Join(sortedKeys(regenerated), " ") != strings.Join(sortedKeys(goldens), " ") {
		t.Errorf("internal/mcp/testdata holds %v after UPDATE_GOLDEN=1, want %v", sortedKeys(regenerated), sortedKeys(goldens))
	}
	for name, want := range goldens {
		if regenerated[name] != want {
			t.Errorf("internal/mcp/testdata/%s changed when regenerated on the split: M11a would change an S7 golden", name)
		}
	}
	if after := snapshot(t, src); fmt.Sprint(after) != fmt.Sprint(untouched) {
		t.Error("the test changed the working tree's internal/mcp")
	}
}

// skipRefusal skips on a refusal that M11a's spec or the table's shape
// gives, and says who settles it and how.
func skipRefusal(t *testing.T, err error) {
	t.Helper()
	t.Skipf("M11a does not generate on the working tree: %v\n"+
		"Not this pull request's to fix (§8.4): M11a's author edits the spec, %s, as the message says, "+
		"when regenerating the split on the latest master (rule R4)", err, filepath.Join("internal/tools/splittools", m11aSpec))
}

// logD11dee8Identity says whether the working tree's internal/mcp production
// files are still d11dee8's, which makes this run the row's d11dee8 run.
func logD11dee8Identity(t *testing.T, dir string) {
	t.Helper()
	have, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	same := true
	for _, f := range have {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fx, err := os.ReadFile(filepath.Join("testdata", "d11dee8", filepath.Base(f)))
		if err != nil || string(fx) != readFile(t, f) {
			same = false
		}
	}
	if fx, _ := filepath.Glob("testdata/d11dee8/*.go"); len(fx) != len(have)-countTests(have) {
		same = false
	}
	if same {
		t.Log("internal/mcp's production files are d11dee8's, byte for byte: this run is also the d11dee8 run")
	} else {
		t.Log("internal/mcp has moved on since d11dee8: this run splits today's table, as M11a will")
	}
}

func countTests(files []string) int {
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			n++
		}
	}
	return n
}

// copyModule copies what the go command and internal/archtest read of the
// module at root: its top-level files, and cmd, internal, examples and
// contracts, leaving out VCS and dependency directories.
func copyModule(t *testing.T, root string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
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

// checkRatchetsOnlyShrink runs archtest's regenerate command on the split
// module and requires every ratchets.json entry to stay or fall: none added,
// none raised. It logs what the split lets M11a tighten.
func checkRatchetsOnlyShrink(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "internal", "archtest", "ratchets.json")
	var before, after interface{}
	if err := json.Unmarshal([]byte(readFile(t, path)), &before); err != nil {
		t.Fatal(err)
	}
	goRun(t, root, []string{"UPDATE_RATCHETS=1"}, "test", "-count=1", "-run", "^TestArchitecture$", "./internal/archtest")
	if err := json.Unmarshal([]byte(readFile(t, path)), &after); err != nil {
		t.Fatal(err)
	}
	for _, g := range ratchetChanges("", before, after, true) {
		t.Errorf("ratchets.json after the split: %s", g)
	}
	for _, s := range ratchetChanges("", after, before, false) {
		t.Logf("the split lets M11a tighten ratchets.json: %s", s)
	}
}

// ratchetChanges lists what after holds beyond before: an entry added, or a
// number raised (grow); with grow false, an entry before lacks.
func ratchetChanges(path string, before, after interface{}, grow bool) []string {
	var out []string
	switch a := after.(type) {
	case map[string]interface{}:
		b, _ := before.(map[string]interface{})
		for k, av := range a {
			bv, ok := b[k]
			if !ok {
				out = append(out, fmt.Sprintf("%s/%s %s", path, k, map[bool]string{true: "added", false: "removed"}[grow]))
				continue
			}
			out = append(out, ratchetChanges(path+"/"+k, bv, av, grow)...)
		}
	case []interface{}:
		b, _ := before.([]interface{})
		have := map[string]bool{}
		for _, x := range b {
			have[fmt.Sprint(x)] = true
		}
		for _, x := range a {
			if !have[fmt.Sprint(x)] {
				out = append(out, fmt.Sprintf("%s: %v %s", path, x, map[bool]string{true: "added", false: "removed"}[grow]))
			}
		}
	case float64:
		if b, ok := before.(float64); grow && (!ok || a > b) {
			out = append(out, fmt.Sprintf("%s raised from %v to %v", path, before, a))
		} else if !grow && ok && a > b {
			out = append(out, fmt.Sprintf("%s lowered from %v to %v", path, a, b))
		}
	}
	sort.Strings(out)
	return out
}

// TestRatchetChanges: what checkRatchetsOnlyShrink calls growth, and what
// it logs as tightened.
func TestRatchetChanges(t *testing.T) {
	var before, after interface{}
	if err := json.Unmarshal([]byte(`{"about": "x", "file_lines": {"a.go": 900, "b.go": 850},
		"import_edges": {"p": ["q"]}, "counts": {"n": 3}}`), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"about": "y", "file_lines": {"a.go": 950, "c.go": 810},
		"import_edges": {"p": ["q", "r"]}, "counts": {"n": 2}, "new_rule": {}}`), &after); err != nil {
		t.Fatal(err)
	}
	grew := strings.Join(ratchetChanges("", before, after, true), "\n")
	want := "/file_lines/a.go raised from 900 to 950\n/file_lines/c.go added\n/import_edges/p: r added\n/new_rule added"
	if grew != want {
		t.Errorf("growth:\n%s\nwant:\n%s", grew, want)
	}
	shrank := strings.Join(ratchetChanges("", after, before, false), "\n")
	want = "/counts/n lowered from 3 to 2\n/file_lines/b.go removed"
	if shrank != want {
		t.Errorf("tightening:\n%s\nwant:\n%s", shrank, want)
	}
	if got := ratchetChanges("", before, before, true); len(got) != 0 {
		t.Errorf("an unchanged file grows: %v", got)
	}
}

// TestLiteralDiff: checkM11aOutput's byte comparison forgives only the
// blank lines above a constructor's first entry, and names the constructor
// and the line where a split parts from the old literal.
func TestLiteralDiff(t *testing.T) {
	a, b, c := "\t\t{\n\t\t\tName: \"a\",\n\t\t},", "\t\t{\n\t\t\tName: \"b\",\n\t\t},", "\t\t// c\n\t\t{\n\t\t\tName: \"c\",\n\t\t},"
	cases := []struct {
		name   string
		bodies []string
		old    string
		want   string // "" for a faithful split, else what the difference says
	}{
		{"the same bytes", []string{a, b + "\n" + c}, a + "\n" + b + "\n" + c, ""},
		{"a blank line where a constructor starts", []string{a, b + "\n" + c}, a + "\n\n" + b + "\n" + c, ""},
		{"blank lines above the first constructor", []string{a, b}, "\n\n" + a + "\n" + b, ""},
		{"a blank line inside a constructor, kept", []string{a, b + "\n\n" + c}, a + "\n" + b + "\n\n" + c, ""},
		{"a blank line inside a constructor, dropped", []string{a, b + "\n" + c}, a + "\n" + b + "\n\n" + c,
			"constructor second parts from the old literal (-), counting from its first line:\n  line 4\n  - \n  + \t\t// c"},
		{"a blank line the literal lacks", []string{a, "\n" + b}, a + "\n" + b, "constructor second parts from the old literal (-)"},
		{"a changed byte", []string{a, b}, a + "\n" + strings.Replace(b, `"b"`, `"B"`, 1),
			"constructor second parts from the old literal (-), counting from its first line:\n  line 2\n  - \t\t\tName: \"B\",\n  + \t\t\tName: \"b\","},
		{"a trailing comment dropped", []string{a, b}, a + " // a\n" + b,
			"constructor first parts from the old literal (-), counting from its first line:\n  line 3\n  - \t\t}, // a\n  + \t\t},"},
		{"an entry dropped", []string{a}, a + "\n" + b, `the old literal goes on after the last constructor's body: "\t\t{"`},
		{"an entry added", []string{a, b, c}, a + "\n" + b, "constructor second parts from the old literal (-)"},
		{"constructors swapped", []string{b, a}, a + "\n" + b, "constructor first parts from the old literal (-)"},
	}
	for _, tc := range cases {
		names := []string{"first", "second", "third"}[:len(tc.bodies)]
		got := literalDiff(names, tc.bodies, tc.old)
		if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
