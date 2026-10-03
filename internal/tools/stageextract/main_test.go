package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// updateGoldenEnv set to exactly 1 rewrites the goldens under testdata/want
// from what stageextract makes of testdata/fixture; any other value
// compares.
const updateGoldenEnv = "UPDATE_GOLDEN"

const fixtureRegenerate = updateGoldenEnv + "=1 go test ./internal/tools/stageextract -count=1 -run '^TestFixture$'"

// fixtureFiles are the files of the split fixture, as the goldens hold
// them.
var fixtureFiles = []string{"app.go", "main.go", "wire_jobs.go", "wire_services.go", "wire_start.go"}

// fixtureModule copies testdata/fixture into a module of its own and
// returns the package directory. edit, when set, rewrites a file's source
// first (name -> old, new), failing when the old text is not there.
func fixtureModule(t *testing.T, edits map[string][2]string, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "fixture")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.25\n")
	srcs, err := filepath.Glob("testdata/fixture/*.go")
	if err != nil || len(srcs) == 0 {
		t.Fatalf("no fixture: %v", err)
	}
	for _, f := range srcs {
		src := readFile(t, f)
		if e, ok := edits[filepath.Base(f)]; ok {
			if !strings.Contains(src, e[0]) {
				t.Fatalf("%s does not contain %q", f, e[0])
			}
			src = strings.Replace(src, e[0], e[1], 1)
		}
		writeFile(t, filepath.Join(dir, filepath.Base(f)), src)
	}
	for name, src := range extra {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, src)
	}
	return dir
}

func fixtureSpec(t *testing.T) *spec {
	t.Helper()
	sp, err := loadSpec("testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	return sp
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

// snapshot reads every file directly in dir.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.Type().IsRegular() {
			out[e.Name()] = readFile(t, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// goCmd runs the go command in dir and returns its output, failing the
// test when it fails.
func goCmd(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := goRun(dir, env, args...)
	if err != nil {
		t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

func goRun(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = childEnv(env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// childEnv is this process's environment without what would turn a child
// go command's comparison into a rewrite: no UPDATE_* (UPDATE_GOLDEN=1 or
// UPDATE_RATCHETS=1 asked of this run would have the copy's TestBootSteps
// or internal/archtest rewrite boot_steps.txt or ratchets.json instead of
// checking them, and the proof would pass whatever the split did), and no
// go.work; then extra.
func childEnv(extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "UPDATE_") || k == "GOWORK" {
			continue
		}
		out = append(out, kv)
	}
	return append(append(out, "GOWORK=off"), extra...)
}

// program builds the fixture's package and runs it, returning what it
// prints.
func program(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fixture")
	goCmd(t, dir, nil, "build", "-o", bin, ".")
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the fixture program fails: %v\n%s", err, out)
	}
	return string(out)
}

// TestFixture splits testdata/fixture with testdata/fixture.json and
// compares every file it writes, and its report, with the goldens under
// testdata/want; the split vets, and the program prints what the
// original printed; a second run refuses, since the split's files exist.
func TestFixture(t *testing.T) {
	dir := fixtureModule(t, nil, nil)
	before := program(t, dir)
	res, err := generate(dir, fixtureSpec(t), options{vet: goVet})
	if err != nil {
		t.Fatal(err)
	}
	report := strings.Join(res.lines, "\n") + "\n"
	got := snapshot(t, dir)
	if keys := strings.Join(sortedKeys(got), " "); keys != "app.go helpers.go main.go wire_jobs.go wire_services.go wire_start.go" {
		t.Fatalf("the split holds %s", keys)
	}
	checkWant(t, "stdout.txt", report)
	for _, name := range fixtureFiles {
		checkWant(t, name+".golden", got[name])
	}
	if after := program(t, dir); after != before {
		t.Errorf("the split program prints\n%s\nthe original printed\n%s", after, before)
	}
	if _, err := generate(dir, fixtureSpec(t), options{}); !errors.Is(err, errPlan) || !strings.Contains(err.Error(), "the package already declares app") {
		t.Errorf("a second run: %v, want a refusal: the split's type exists", err)
	}
}

// checkWant compares got with testdata/want/name, or rewrites it when
// UPDATE_GOLDEN is exactly 1.
func checkWant(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "want", name)
	if os.Getenv(updateGoldenEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, got)
		t.Logf("regenerated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with:\n  %s", path, err, fixtureRegenerate)
	}
	if string(want) != got {
		t.Errorf("internal/tools/stageextract/%s differs from what stageextract makes of the fixture:\n%s\n"+
			"If the tool changed on purpose, regenerate with:\n  %s\n(only %s=1 regenerates; any other value compares)",
			filepath.ToSlash(path), firstDiff(string(want), got), fixtureRegenerate, updateGoldenEnv)
	}
}

// firstDiff shows the first line where two texts differ.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b || i >= len(w) || i >= len(g) {
			return "line " + itoa(i+1) + ":\n- " + a + "\n+ " + b
		}
	}
	return "(equal)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// TestDryRun: -n prints the stage map and writes nothing.
func TestDryRun(t *testing.T) {
	dir := fixtureModule(t, nil, nil)
	before := snapshot(t, dir)
	res, err := generate(dir, fixtureSpec(t), options{dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(res.lines, "\n")
	for _, want := range []string{
		"signals -> wire_start.go (lines 16-22, 11 lines), returns stop, tick.Stop for main() to defer as stop, stopTick",
		"connect -> wire_start.go (lines 41-45, 9 lines), returns db.Close for main() to defer as closeDB",
		"stageextract: main() of fixture into 5 stages in 3 files; 15 locals become fields of app (app.go), declared by each stage that uses it: err; 3 cleanups returned",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report lacks %q:\n%s", want, joined)
		}
	}
	if after := snapshot(t, dir); strings.Join(sortedKeys(after), " ") != strings.Join(sortedKeys(before), " ") {
		t.Errorf("-n wrote files: %v", sortedKeys(after))
	}
}

// TestRunExitStatus: the command's exit statuses and where it prints.
func TestRunExitStatus(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run(nil, &out, &errs); code != 2 || !strings.Contains(errs.String(), "usage") {
		t.Errorf("no spec: exit %d, %s", code, errs.String())
	}
	errs.Reset()
	if code := run([]string{"-spec", "testdata/nope.json"}, &out, &errs); code != 2 {
		t.Errorf("a missing spec: exit %d", code)
	}
	dir := fixtureModule(t, nil, nil)
	sp := strings.Replace(readFile(t, "testdata/fixture.json"), `"internal/tools/stageextract/testdata/fixture"`, `"`+filepath.ToSlash(dir)+`"`, 1)
	specPath := filepath.Join(t.TempDir(), "spec.json")
	writeFile(t, specPath, sp)
	out.Reset()
	errs.Reset()
	if code := run([]string{"-n", "-spec", specPath}, &out, &errs); code != 0 || !strings.Contains(out.String(), "jobs -> wire_jobs.go") {
		t.Errorf("-n: exit %d\n%s%s", code, out.String(), errs.String())
	}
	writeFile(t, specPath, strings.Replace(sp, `"main_budget": 30`, `"main_budget": 5`, 1))
	errs.Reset()
	if code := run([]string{"-spec", specPath}, &out, &errs); code != 1 || !strings.Contains(errs.String(), "over the spec's budget of 5") {
		t.Errorf("a refusal: exit %d, %s", code, errs.String())
	}
	writeFile(t, specPath, strings.Replace(sp, `"`+filepath.ToSlash(dir)+`"`, `"`+filepath.ToSlash(t.TempDir())+`"`, 1))
	errs.Reset()
	if code := run([]string{"-spec", specPath}, &out, &errs); code != 2 {
		t.Errorf("a directory with no package: exit %d, %s", code, errs.String())
	}
}

// TestFixtureProvedByMovecheck runs the proof M4's reviewer runs on the
// split fixture: movecheck -flatten main -base HEAD exits 0 on the
// faithful split, and 1 on a statement moved across a stage boundary, on
// a statement dropped, and on a field read as a package-level name of
// the same text.
func TestFixtureProvedByMovecheck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := fixtureModule(t, nil, nil)
	gitInit(t, filepath.Dir(dir))
	if _, err := generate(dir, fixtureSpec(t), options{}); err != nil {
		t.Fatal(err)
	}
	split := snapshot(t, dir)
	code, out := movecheck(t, dir)
	if code != 0 || !strings.Contains(out, "flattens to the same statements in HEAD and the working tree once the stage rewrites are undone (15 locals became fields of a, 3 cleanups returned for main to defer)") {
		t.Fatalf("the faithful split: exit %d\n%s", code, out)
	}
	for _, m := range []struct {
		name  string
		edits map[string][2]string
		want  string
	}{
		{"a statement moved across a stage boundary", map[string][2]string{
			"wire_start.go": {"\ta.tick = newTicker(\"1h\")\n", ""},
		}, "+tick = newTicker(\"1h\")"},
		{"a statement dropped", map[string][2]string{
			"wire_services.go": {"\ta.count = a.limit * 2\n", ""},
		}, "-count := limit * 2"},
		{"a field read as a package-level name", map[string][2]string{
			"wire_jobs.go": {"a.label, a.total", "label, a.total"},
			"helpers.go":   {"type store struct", "var label = \"package-level\"\n\ntype store struct"},
		}, "label names a package-level or predeclared name in the working tree but a local of the function's top level"},
	} {
		t.Run(m.name, func(t *testing.T) {
			for name, e := range m.edits {
				src := split[name]
				if !strings.Contains(src, e[0]) {
					t.Fatalf("%s does not contain %q", name, e[0])
				}
				src = strings.Replace(src, e[0], e[1], 1)
				if m.name == "a statement moved across a stage boundary" {
					src = strings.Replace(src, "\t// Settings.\n", "\t// Settings.\n\ta.tick = newTicker(\"1h\")\n", 1)
				}
				writeFile(t, filepath.Join(dir, name), src)
			}
			defer func() {
				for name := range m.edits {
					writeFile(t, filepath.Join(dir, name), split[name])
				}
			}()
			goCmd(t, dir, nil, "build", "-o", os.DevNull, ".")
			code, out := movecheck(t, dir)
			t.Log(out)
			if code != 1 || !strings.Contains(out, m.want) {
				t.Errorf("exit %d, want 1 with %q:\n%s", code, m.want, out)
			}
		})
	}
	if code, out := movecheck(t, dir); code != 0 {
		t.Errorf("the restored split: exit %d\n%s", code, out)
	}
}

// gitInit makes dir a git repository with one commit of what it holds.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com",
			"-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// movecheck runs this repository's movecheck -flatten main -base HEAD on
// dir and returns its exit status and output.
func movecheck(t *testing.T, dir string) (int, string) {
	t.Helper()
	root, err := moduleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./internal/tools/movecheck", "-flatten", "main", "-base", "HEAD", dir)
	cmd.Dir = root
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("go run movecheck: %v\n%s", err, out)
	return 0, ""
}
