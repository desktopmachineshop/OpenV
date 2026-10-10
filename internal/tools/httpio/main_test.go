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
// httpio makes of testdata/fixture; any other value compares.
const updateGoldenEnv = "UPDATE_GOLDEN"

const fixtureRegenerate = updateGoldenEnv + "=1 go test ./internal/tools/httpio -count=1 -run '^TestFixture$'"

// fixtureRoot copies testdata/fixture into api/ of a fresh directory and
// returns the directory. edits rewrite a file's source first (name -> old,
// new), failing when the old text is not there.
func fixtureRoot(t *testing.T, edits map[string][2]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
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

// runTool runs httpio from root with args, as scripts/refactor/httpio.sh
// runs it from the repository root.
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
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// snapshot reads every file under dir, by path relative to it.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = readFile(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goCmd runs the go command in dir and fails the test on an error.
func goCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := goOut(dir, args...)
	if err != nil {
		t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

// goOut runs the go command in dir without the caller's UPDATE_*
// variables, so a golden or ratchet update asked of another test never
// turns a comparison here into a rewrite.
func goOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "UPDATE_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestFixture rewrites testdata/fixture and compares every file and the
// report with testdata/want.
func TestFixture(t *testing.T) {
	root := fixtureRoot(t, nil)
	code, stdout, stderr := runTool(t, root, "api")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := snapshot(t, filepath.Join(root, "api"))
	update := os.Getenv(updateGoldenEnv) == "1"
	want := map[string]string{"stdout.txt": stdout}
	for name, src := range got {
		if src != readFile(t, filepath.Join("testdata", "fixture", name)) {
			want[name+".golden"] = src
		}
	}
	if update {
		if err := os.RemoveAll(filepath.Join("testdata", "want")); err != nil {
			t.Fatal(err)
		}
		for name, src := range want {
			writeFile(t, filepath.Join("testdata", "want", name), src)
		}
		return
	}
	goldens, _ := filepath.Glob(filepath.Join("testdata", "want", "*"))
	if len(goldens) != len(want) {
		t.Errorf("%d goldens, want %d (changed files and stdout.txt); regenerate with %s", len(goldens), len(want), fixtureRegenerate)
	}
	for name, src := range want {
		if golden := readFile(t, filepath.Join("testdata", "want", name)); golden != src {
			t.Errorf("%s differs from its golden; regenerate with %s\n--- got\n%s", name, fixtureRegenerate, src)
		}
	}
}

// TestSecondRunChangesNothing runs httpio on its own output: nothing left
// to rewrite, every file as it was, every site left or kept as before.
func TestSecondRunChangesNothing(t *testing.T) {
	root := fixtureRoot(t, nil)
	if code, _, stderr := runTool(t, root, "api"); code != 0 {
		t.Fatalf("first run: exit %d: %s", code, stderr)
	}
	before := snapshot(t, root)
	code, stdout, stderr := runTool(t, root, "api")
	if code != 0 {
		t.Fatalf("second run: exit %d: %s", code, stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if !strings.Contains(line, ": left: ") && !strings.Contains(line, ": kept: ") && !strings.HasPrefix(line, "total: ") {
			t.Errorf("the second run plans %q", line)
		}
	}
	if after := snapshot(t, root); !equalMaps(before, after) {
		t.Error("the second run changed files")
	}
	if !strings.Contains(stdout, "total: encodes 13 (writeJSON 0, writeJSONOK 0, writeJSONBare 0, writeJSONBareStatus 0; left 13)") {
		t.Errorf("the second run's total: %s", stdout)
	}
}

// TestDryRunWritesNothing: -n prints the plan a write prints, and writes
// nothing, even where the helpers are missing.
func TestDryRunWritesNothing(t *testing.T) {
	root := fixtureRoot(t, nil)
	before := snapshot(t, root)
	code, stdout, stderr := runTool(t, root, "-n", "api")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !equalMaps(before, snapshot(t, root)) {
		t.Error("-n wrote files")
	}
	if want := readFile(t, filepath.Join("testdata", "want", "stdout.txt")); stdout != want {
		t.Errorf("-n prints another plan than a write:\n%s", stdout)
	}
	if err := os.Remove(filepath.Join(root, "api", "respond.go")); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runTool(t, root, "-n", "api"); code != 0 {
		t.Errorf("-n without the helpers: exit %d: %s", code, stderr)
	}
}

// TestRefusesWithoutTheHelpers: the rewrite writes nothing into a package
// that does not declare each helper it calls exactly as helpers.go does.
func TestRefusesWithoutTheHelpers(t *testing.T) {
	cases := []struct {
		name  string
		edits map[string][2]string
		want  string
	}{
		{"writeJSON missing", map[string][2]string{"respond.go": {"func writeJSON(", "func writeJSONOld("}},
			"writeJSON is not declared"},
		{"writeJSON reordered", map[string][2]string{"respond.go": {
			"\tw.Header().Set(\"Content-Type\", \"application/json\")\n\tw.WriteHeader(status)\n",
			"\tw.WriteHeader(status)\n\tw.Header().Set(\"Content-Type\", \"application/json\")\n"}},
			"writeJSON is not declared as internal/tools/httpio/helpers.go's helperSource declares it"},
		{"the constant changed", map[string][2]string{"respond.go": {
			`const invalidRequestBody = "invalid request body"`, `const invalidRequestBody = "Invalid request body"`}},
			"invalidRequestBody is not declared as"},
		{"decodeJSONMsg's status changed", map[string][2]string{"respond.go": {
			"writeJSONError(w, http.StatusBadRequest, msg)", "writeJSONError(w, http.StatusUnprocessableEntity, msg)"}},
			"decodeJSONMsg is not declared as"},
		{"writeJSONError missing", map[string][2]string{"httperr.go": {"func writeJSONError(", "func writeJSONErr("}},
			"writeJSONError is not declared"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixtureRoot(t, c.edits)
			before := snapshot(t, root)
			code, _, stderr := runTool(t, root, "api")
			if code != 1 || !strings.Contains(stderr, c.want) || !strings.Contains(stderr, "X1a adds the helpers") {
				t.Errorf("exit %d, stderr %q; want 1 and %q", code, stderr, c.want)
			}
			if !equalMaps(before, snapshot(t, root)) {
				t.Error("a refusal wrote files")
			}
		})
	}
}

// TestAreas: -area rewrites only the files docs/areas.json gives that area
// (or those areas), and the report sums up per area.
func TestAreas(t *testing.T) {
	index := `{"areas": [
		{"name": "alpha", "globs": ["api/handlers.go", "api/bare.go"]},
		{"name": "beta", "globs": ["api/left.go", "api/httperr.go", "api/respond.go", "api/cases.go"]}
	]}`
	root := fixtureRoot(t, nil)
	writeFile(t, filepath.Join(root, areasFile), index)
	before := snapshot(t, filepath.Join(root, "api"))
	code, stdout, stderr := runTool(t, root, "-area", "alpha", "api")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	after := snapshot(t, filepath.Join(root, "api"))
	for name := range before {
		changed := before[name] != after[name]
		if want := name == "handlers.go" || name == "bare.go"; changed != want {
			t.Errorf("%s changed: %v, want %v", name, changed, want)
		}
	}
	if !strings.Contains(stdout, "area alpha: encodes 9 (writeJSON 3, writeJSONOK 2, writeJSONBare 2, writeJSONBareStatus 2; left 0)") ||
		strings.Contains(stdout, "area beta") || strings.Contains(stdout, "left.go") {
		t.Errorf("report:\n%s", stdout)
	}

	root = fixtureRoot(t, nil)
	writeFile(t, filepath.Join(root, areasFile), index)
	code, stdout, stderr = runTool(t, root, "-area", "alpha,beta", "api")
	if code != 0 || !strings.Contains(stdout, "area alpha:") || !strings.Contains(stdout, "area beta: encodes 14 (writeJSON 1,") {
		t.Errorf("two areas: exit %d, %s\n%s", code, stderr, stdout)
	}

	if code, _, stderr := runTool(t, root, "-area", "gamma", "api"); code != 2 || !strings.Contains(stderr, `no area "gamma"`) {
		t.Errorf("an unknown area: exit %d, %s", code, stderr)
	}
}

// TestUsage: a mistyped path or flag never passes by rewriting nothing.
func TestUsage(t *testing.T) {
	root := fixtureRoot(t, nil)
	writeFile(t, filepath.Join(root, "empty", "README.md"), "no Go here\n")
	cases := [][]string{
		nil,
		{"-x", "api"},
		{"missing"},
		{"empty"},
		{"empty/README.md"},
	}
	for _, args := range cases {
		if code, _, _ := runTool(t, root, args...); code != 2 {
			t.Errorf("httpio %s: exit %d, want 2", strings.Join(args, " "), code)
		}
	}
	if code, stdout, stderr := runTool(t, root, "api/bare.go"); code != 0 || !strings.Contains(stdout, "api/bare.go:15: writeJSONBare (E)") {
		t.Errorf("one file: exit %d, %s\n%s", code, stderr, stdout)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
