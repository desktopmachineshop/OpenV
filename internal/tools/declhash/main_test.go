package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// manifestOf hashes an in-memory package.
func manifestOf(t *testing.T, files map[string]string) []string {
	t.Helper()
	var srcs []source
	for name, src := range files {
		srcs = append(srcs, source{name, []byte(src)})
	}
	p, err := parsePackage(srcs, true)
	if err != nil {
		t.Fatal(err)
	}
	return p.manifest("pkg")
}

// keysOf returns the manifest's "package<TAB>key" part, one per line.
func keysOf(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l[:strings.LastIndexByte(l, '\t')]
	}
	return out
}

const oneFile = `package pkg

import (
	"fmt"
	"strings"
)

// Kind is a kind.
type Kind int

// The kinds.
const (
	KindA Kind = iota // first
	KindB
	KindC
)

const limit = 10

var (
	// names are names.
	names    = []string{"a", "b"}
	x, y     = 1, 2
	pair1, _ = split("a,b")
)

// T holds things.
type T struct{ n int }

// Get returns n.
func (t *T) Get() int { return t.n }

// Name is a value method.
func (t T) Name() string { return fmt.Sprint(t.n) }

func split(s string) (string, string) {
	a, b, _ := strings.Cut(s, ",")
	return a, b
}

func init() { _ = limit }
`

// The same declarations spread over three files, with their own import
// blocks and a floating comment of their own.
var threeFiles = map[string]string{
	"kinds.go": `package pkg

// Kind is a kind.
type Kind int

// The kinds.
const (
	KindA Kind = iota // first
	KindB
	KindC
)

// A floating comment that belongs to no declaration.

const limit = 10

func init() { _ = limit }
`,
	"t.go": `package pkg

import "fmt"

// T holds things.
type T struct{ n int }

// Name is a value method.
func (t T) Name() string { return fmt.Sprint(t.n) }

// Get returns n.
func (t *T) Get() int { return t.n }
`,
	"vars.go": `package pkg

import (
	"strings"
)

var (
	// names are names.
	names    = []string{"a", "b"}
	x, y     = 1, 2
	pair1, _ = split("a,b")
)

func split(s string) (string, string) {
	a, b, _ := strings.Cut(s, ",")
	return a, b
}
`,
}

func TestMovingBetweenFilesKeepsTheManifest(t *testing.T) {
	a := manifestOf(t, map[string]string{"one.go": oneFile})
	b := manifestOf(t, threeFiles)
	if diffs, _ := diffManifests(a, b); len(diffs) > 0 {
		t.Fatalf("a pure move changed the manifest:\n%s", strings.Join(diffs, "\n"))
	}
	want := []string{
		"pkg\t(*T).Get", "pkg\tKind", "pkg\tKindA", "pkg\tKindB", "pkg\tKindC", "pkg\tT", "pkg\tT.Name",
		"pkg\t_", "pkg\tinit", "pkg\tlimit", "pkg\tnames", "pkg\tpair1", "pkg\tsplit", "pkg\tx", "pkg\ty",
	}
	if got := keysOf(a); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("keys:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEditsChangeTheHash(t *testing.T) {
	cases := []struct {
		name, from, to, key string
	}{
		{"body", "return t.n }", "return t.n + 1 }", "(*T).Get"},
		{"doc comment", "// Get returns n.", "// Get returns the n.", "(*T).Get"},
		{"inner comment", "a, b, _ := strings.Cut", "a, b, _ := /* cut */ strings.Cut", "split"},
		{"line comment of a const", "iota // first", "iota // the first", "KindA"},
		{"group doc", "// The kinds.", "// Kinds.", "KindC"},
		{"implicit type of a const", "KindA Kind = iota", "KindA int = iota", "KindB"},
		{"var value", `"a", "b"}`, `"a", "c"}`, "names"},
		{"one of a multi-value var", `split("a,b")`, `split("a;b")`, "pair1"},
		{"receiver", "func (t *T) Get()", "func (t T) Get()", "T.Get"},
	}
	base := manifestOf(t, map[string]string{"one.go": oneFile})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(oneFile, c.from) {
				t.Fatalf("fixture lacks %q", c.from)
			}
			head := manifestOf(t, map[string]string{"one.go": strings.Replace(oneFile, c.from, c.to, 1)})
			diffs, _ := diffManifests(base, head)
			found := false
			for _, d := range diffs {
				found = found || strings.HasSuffix(d, "\t"+c.key)
			}
			if !found {
				t.Fatalf("editing %q did not change %s; diffs: %v", c.from, c.key, diffs)
			}
		})
	}
}

func TestIotaAndImplicitRepetition(t *testing.T) {
	grouped := manifestOf(t, map[string]string{"a.go": "package pkg\n\nconst (\n\tA = iota\n\tB\n\tC = 7\n\tD\n)\n"})
	// C and D do not use iota, so they may leave the group; B may not.
	split := manifestOf(t, map[string]string{
		"a.go": "package pkg\n\nconst (\n\tA = iota\n\tB\n)\n",
		"b.go": "package pkg\n\nconst C = 7\n\nconst D = 7\n",
	})
	if diffs, _ := diffManifests(grouped, split); len(diffs) > 0 {
		t.Fatalf("moving consts that do not use iota changed: %v", diffs)
	}
	reordered := manifestOf(t, map[string]string{"a.go": "package pkg\n\nconst (\n\tB = iota\n\tA\n\tC = 7\n\tD\n)\n"})
	diffs, _ := diffManifests(grouped, reordered)
	if strings.Join(diffs, "\n") != "changed\tpkg\tA\nchanged\tpkg\tB" {
		t.Fatalf("swapping two iota consts: %v", diffs)
	}
	alone := manifestOf(t, map[string]string{
		"a.go": "package pkg\n\nconst (\n\tA = iota\n\tC = 7\n\tD\n)\n",
		"b.go": "package pkg\n\nconst B = iota\n",
	})
	diffs, _ = diffManifests(grouped, alone)
	if strings.Join(diffs, "\n") != "changed\tpkg\tB" {
		t.Fatalf("moving an iota const out of its group: %v", diffs)
	}
}

func TestImportsAndBuildConstraintsArePartOfTheHash(t *testing.T) {
	src := func(imp string) string {
		return "package pkg\n\nimport \"" + imp + "\"\n\nfunc Render() any { return template.New(\"x\") }\n"
	}
	html := manifestOf(t, map[string]string{"a.go": src("html/template")})
	text := manifestOf(t, map[string]string{"a.go": src("text/template")})
	if diffs, _ := diffManifests(html, text); len(diffs) != 1 {
		t.Fatalf("rebinding template to another package went unnoticed: %v", diffs)
	}
	plain := manifestOf(t, map[string]string{"a.go": "package pkg\n\nfunc F() {}\n"})
	for name, files := range map[string]map[string]string{
		"go:build line":    {"a.go": "//go:build linux\n\npackage pkg\n\nfunc F() {}\n"},
		"file-name suffix": {"a_windows.go": "package pkg\n\nfunc F() {}\n"},
		"test file":        {"a_test.go": "package pkg\n\nfunc F() {}\n"},
	} {
		if diffs, _ := diffManifests(plain, manifestOf(t, files)); len(diffs) == 0 {
			t.Errorf("%s: a move into a constrained file went unnoticed", name)
		}
	}
}

func TestDuplicateKeysCompareAsAMultiset(t *testing.T) {
	a := manifestOf(t, map[string]string{
		"a.go": "package pkg\n\nfunc init() { println(1) }\n\nvar _ = 1\n",
		"b.go": "package pkg\n\nfunc init() { println(2) }\n\nvar _ = 2\n",
	})
	b := manifestOf(t, map[string]string{
		"a.go": "package pkg\n\nfunc init() { println(2) }\n\nvar _ = 2\n",
		"c.go": "package pkg\n\nfunc init() { println(1) }\n\nvar _ = 1\n",
	})
	if diffs, _ := diffManifests(a, b); len(diffs) > 0 {
		t.Fatalf("moving init functions between files changed: %v", diffs)
	}
	c := manifestOf(t, map[string]string{"a.go": "package pkg\n\nfunc init() { println(1) }\n\nvar _ = 1\n"})
	diffs, _ := diffManifests(a, c)
	if strings.Join(diffs, "\n") != "changed\tpkg\t_\nchanged\tpkg\tinit" {
		t.Fatalf("dropping one init: %v", diffs)
	}
}

func TestGenericReceiversAndTestSets(t *testing.T) {
	m := manifestOf(t, map[string]string{
		"a.go":      "package pkg\n\ntype S[K comparable, V any] map[K]V\n\nfunc (s S[K, V]) Len() int { return len(s) }\n\nfunc (s *S[K, V]) Reset() {}\n",
		"a_test.go": "package pkg\n\nfunc helper() {}\n",
		"x_test.go": "package pkg_test\n\nfunc TestX() {}\n",
	})
	want := "pkg\t(*S).Reset\npkg\tS\npkg\tS.Len\npkg (test)\thelper\npkg (xtest)\tTestX"
	if got := strings.Join(keysOf(m), "\n"); got != want {
		t.Fatalf("keys:\n%s\nwant:\n%s", got, want)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runTool(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestManifestAndCompareFiles(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	writeFiles(t, pkg, map[string]string{"one.go": oneFile})
	baseFile := filepath.Join(dir, "base.txt")
	if code, _, errs := runTool("-o", baseFile, pkg); code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	code, first, _ := runTool(pkg)
	_, second, _ := runTool(pkg)
	if code != 0 || first != second {
		t.Fatalf("manifest is not deterministic (exit %d)", code)
	}
	if b, _ := os.ReadFile(baseFile); string(b) != first {
		t.Fatal("-o wrote something other than the manifest")
	}

	if err := os.Remove(filepath.Join(pkg, "one.go")); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, pkg, threeFiles)
	headFile := filepath.Join(dir, "head.txt")
	runTool("-o", headFile, pkg)
	if code, out, _ := runTool("-compare", baseFile, headFile); code != 0 {
		t.Fatalf("pure move: exit %d\n%s", code, out)
	}

	edited := strings.Replace(threeFiles["t.go"], "return t.n }", "return -t.n }", 1)
	edited = strings.Replace(edited, "// Name is a value method.\nfunc (t T) Name() string { return fmt.Sprint(t.n) }\n", "", 1)
	writeFiles(t, pkg, map[string]string{"t.go": edited + "\nfunc Extra() {}\n"})
	runTool("-o", headFile, pkg)
	code, out, _ := runTool("-compare", baseFile, headFile)
	for _, want := range []string{"changed\t", "\t(*T).Get\n", "removed\t", "\tT.Name\n", "added\t", "\tExtra\n", "3 of 16 declarations differ"} {
		if !strings.Contains(out, want) {
			t.Errorf("compare output lacks %q:\n%s", want, out)
		}
	}
	if code != 1 {
		t.Fatalf("differences: exit %d, want 1", code)
	}
}

func TestBaseRefComparesAgainstGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	pkg := filepath.Join(repo, "pkg")
	writeFiles(t, pkg, map[string]string{"one.go": oneFile})
	writeFiles(t, repo, map[string]string{"go.mod": "module example.com/m\n"})
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "base")

	if err := os.Remove(filepath.Join(pkg, "one.go")); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, pkg, threeFiles)
	if code, out, errs := runTool("-base", "HEAD", pkg); code != 0 {
		t.Fatalf("pure move against HEAD: exit %d\n%s%s", code, out, errs)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "move")
	if code, out, errs := runTool("-base", "HEAD~1", "-head", "HEAD", pkg); code != 0 {
		t.Fatalf("pure move between refs: exit %d\n%s%s", code, out, errs)
	}

	writeFiles(t, pkg, map[string]string{"vars.go": strings.Replace(threeFiles["vars.go"], `"a", "b"}`, `"a", "B"}`, 1)})
	code, out, _ := runTool("-base", "HEAD", pkg)
	if code != 1 || !strings.Contains(out, "changed\tpkg\tnames\n") {
		t.Fatalf("one changed character: exit %d\n%s", code, out)
	}
	if code, _, errs := runTool("-base", "no-such-ref", pkg); code != 2 {
		t.Fatalf("bad ref: exit %d, want 2 (%s)", code, errs)
	}
}

// TestSharedFileIsIdentical keeps the three copies of decls.go in step.
func TestSharedFileIsIdentical(t *testing.T) {
	want, err := os.ReadFile("decls.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"../declmove/decls.go", "../movecheck/decls.go"} {
		got, err := os.ReadFile(other)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from declhash/decls.go; copy declhash/decls.go over it", other)
		}
	}
}
