package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// updateGoldenEnv set to exactly 1 rewrites testdata/want from what
// embeddeps makes of testdata/fixture; any other value compares.
const updateGoldenEnv = "UPDATE_GOLDEN"

const fixtureRegenerate = updateGoldenEnv + "=1 go test ./internal/tools/embeddeps -count=1 -run '^TestFixture$'"

// fixtureModule copies testdata/fixture into internal/api of a module of
// its own and returns the module root. edits rewrite a file's source first
// (name -> old, new), failing when the old text is not there.
func fixtureModule(t *testing.T, edits map[string][2]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.25\n")
	for _, name := range fixtureNames(t) {
		src := readFile(t, filepath.Join("testdata", "fixture", name))
		if e, ok := edits[name]; ok {
			if !strings.Contains(src, e[0]) {
				t.Fatalf("%s does not contain %q", name, e[0])
			}
			src = strings.Replace(src, e[0], e[1], 1)
		}
		writeFile(t, filepath.Join(dir, name), src)
	}
	return root
}

func fixtureNames(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "fixture", "*.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixture: %v", err)
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	sort.Strings(names)
	return names
}

// runTool runs embeddeps on root's internal/api from root, as
// scripts/refactor/embed_deps.sh does from the repository root.
func runTool(t *testing.T, root string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	}()
	var out, errOut bytes.Buffer
	code = run(append(args, "internal/api"), &out, &errOut)
	return code, out.String(), errOut.String()
}

// goCmd runs the go command in root and fails the test on an error.
func goCmd(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, name := range fixtureNames(t) {
		got[name] = readFile(t, filepath.Join(root, "internal", "api", name))
	}
	return got
}

// TestFixture rewrites the fixture and compares every file with
// testdata/want (a file the rewrite leaves as it is has no golden and must
// be the fixture's byte for byte), then builds, vets and tests the result,
// whose tests pin what NewHandler wires, and runs the tool again, which
// finds nothing to do.
func TestFixture(t *testing.T) {
	root := fixtureModule(t, nil)
	goCmd(t, root, "test", "-count=1", "./...")
	code, stdout, stderr := runTool(t, root)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	got := snapshot(t, root)
	update := os.Getenv(updateGoldenEnv) == "1"
	for _, name := range fixtureNames(t) {
		golden := filepath.Join("testdata", "want", name+".golden")
		if update {
			if got[name] == readFile(t, filepath.Join("testdata", "fixture", name)) {
				_ = os.Remove(golden)
			} else {
				writeFile(t, golden, got[name])
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if os.IsNotExist(err) {
			want = []byte(readFile(t, filepath.Join("testdata", "fixture", name)))
		} else if err != nil {
			t.Fatal(err)
		}
		if got[name] != string(want) {
			t.Errorf("%s differs from %s (%s):\n%s", name, golden, fixtureRegenerate, got[name])
		}
	}
	if update {
		writeFile(t, filepath.Join("testdata", "want", "stdout.txt"), stdout)
	} else if want := readFile(t, filepath.Join("testdata", "want", "stdout.txt")); stdout != want {
		t.Errorf("stdout = %q, want %q (%s)", stdout, want, fixtureRegenerate)
	}
	// The fields another type declares with a copied field's name keep
	// their names, and so does every selector of them.
	if got["middleware.go"] != readFile(t, filepath.Join("testdata", "fixture", "middleware.go")) {
		t.Errorf("middleware.go changed: AuthMiddleware.userService and .store are not Handler's")
	}
	for _, keep := range []string{"s.store.Get(id)", "&AuthMiddleware{userService: fakeUsers{}}", "m.store = h.Store"} {
		if !strings.Contains(got["use.go"]+got["use_test.go"], keep) {
			t.Errorf("the rewrite lost %q", keep)
		}
	}
	goCmd(t, root, "vet", "./...")
	goCmd(t, root, "test", "-count=1", "./...")
	code, stdout, stderr = runTool(t, root)
	if code != 0 || !strings.Contains(stdout, "nothing to do") {
		t.Fatalf("second run: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for name, src := range snapshot(t, root) {
		if src != got[name] {
			t.Errorf("the second run changed %s", name)
		}
	}
}

// TestDryRun prints the plan and writes nothing.
func TestDryRun(t *testing.T) {
	root := fixtureModule(t, nil)
	before := snapshot(t, root)
	code, stdout, stderr := runTool(t, root, "-n")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if want := readFile(t, filepath.Join("testdata", "want", "stdout.txt")); stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	for name, src := range snapshot(t, root) {
		if src != before[name] {
			t.Errorf("-n wrote %s", name)
		}
	}
}

// TestRefusals: each shape the rename cannot carry exits 1, names it, and
// writes nothing.
func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edits map[string][2]string
		want  string
	}{
		{"composite literal key", map[string][2]string{"use_test.go": {
			"\th := &Handler{}\n", "\th := &Handler{store: nil}\n"}},
			"use_test.go:15: Handler.store is named outside a selector"},
		{"through another type's embedding", map[string][2]string{"use.go": {
			"// sha reads", "type wrapped struct{ *Handler }\n\nfunc (w wrapped) id() Store { return w.store }\n\n// sha reads"}},
			"w.store reaches Handler.store through another type's embedding"},
		{"a method shadows a promoted field", map[string][2]string{"use.go": {
			"// sha reads", "func (h *Handler) BuildSHA() string { return h.buildSHA }\n\n// sha reads"}},
			"Handler's method BuildSHA would shadow the promoted HandlerDeps.BuildSHA"},
		{"a field that stays shadows a promoted one", map[string][2]string{"handlers.go": {
			"\tsameSite      http.SameSite\n", "\tsameSite      http.SameSite\n\tFrontendURL   string\n"}},
			"Handler's field FrontendURL would shadow the promoted HandlerDeps.FrontendURL"},
		{"no NewHandler", map[string][2]string{
			"handlers.go": {"func NewHandler(deps HandlerDeps) *Handler {", "func newHandler(deps HandlerDeps) *Handler {"},
			"use_test.go": {"h := NewHandler(", "h := newHandler("}},
			"declares no func NewHandler"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtureModule(t, tc.edits)
			before := snapshot(t, root)
			code, _, stderr := runTool(t, root)
			if code == 0 || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stderr %q, want a failure naming %q", code, stderr, tc.want)
			}
			for name, src := range snapshot(t, root) {
				if src != before[name] {
					t.Errorf("a refusal wrote %s", name)
				}
			}
		})
	}
}

// TestCopiedTwice refuses a HandlerDeps field NewHandler copies into two
// Handler fields: one name cannot stand for both.
func TestCopiedTwice(t *testing.T) {
	root := fixtureModule(t, map[string][2]string{"handlers.go": {
		"\tbuildSHA string // a line comment, which goes with the field\n",
		"\tbuildSHA string // a line comment, which goes with the field\n\tstore2   Store\n"}})
	src := readFile(t, filepath.Join(root, "internal", "api", "handlers.go"))
	src = strings.Replace(src, "\t\tstore:       deps.Store,\n", "\t\tstore:       deps.Store,\n\t\tstore2:      deps.Store,\n", 1)
	writeFile(t, filepath.Join(root, "internal", "api", "handlers.go"), src)
	code, _, stderr := runTool(t, root)
	if want := "copies HandlerDeps.Store into both store and store2"; code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("exit %d, stderr %q, want 1 naming %q", code, stderr, want)
	}
}

func TestUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Errorf("no package: exit %d, want 2", code)
	}
	if code := run([]string{t.TempDir()}, &out, &errOut); code != 2 {
		t.Errorf("no module: exit %d, want 2", code)
	}
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
