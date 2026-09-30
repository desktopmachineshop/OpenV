package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The fixture (testdata/fixture) is a package in the shapes the real
// registry has: a RunDB baseline, keyed and positional entries, one already
// lifted, a version gap, two entries with one name, a comment with two
// paragraphs, one of two groups a blank line apart (the lift joins them with
// an empty "//" line), one gofmt would reword as a doc comment, one with no
// comment, raw strings indented deeper than the code, an aliased import, a
// helper reached through another, a method of the element type and a
// runner. The goldens under testdata/want are what the tool makes of it.

// goldenEnv set to exactly 1 rewrites the goldens instead of comparing; any
// other value compares.
const goldenEnv = "UPDATE_GOLDEN"

func updating() bool { return os.Getenv(goldenEnv) == "1" }

func regenerate(test string) string {
	return goldenEnv + "=1 go test ./internal/tools/liftmigrations -count=1 -run '^" + test + "$'" +
		" (only " + goldenEnv + "=1 regenerates; any other value compares)"
}

// copyFixture copies testdata/fixture into a fresh module and returns the
// module root and the package directory.
func copyFixture(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "store")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("testdata/fixture", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, e.Name()), string(b))
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fixture\n\ngo 1.22\n")
	return root, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readTree reads every file of a directory, by name.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

// runTool runs the command in-process from dir and returns its exit status
// and output.
func runTool(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	}()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// buildTool builds a sibling tool (declhash, declmove), so the tests prove
// the result with the tools a reviewer and the Refactor guard run.
func buildTool(t *testing.T, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("go", "build", "-o", bin, "../"+name).CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, out)
	}
	return bin
}

// runCmd runs a command in dir and returns its combined output.
func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runGo(dir string, args ...string) (string, error) { return runCmd(dir, "go", args...) }

// command runs a command in dir and fails the test, with its output, if it
// fails.
func command(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	out, err := runCmd(dir, name, args...)
	if err != nil {
		t.Fatalf("%s %s (in %s): %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return out
}

// compareGolden compares got with testdata/want/<name>, or rewrites it.
func compareGolden(t *testing.T, test, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "want", name)
	if updating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, got)
		t.Logf("regenerated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", path, err, regenerate(test))
	}
	if string(want) != got {
		t.Errorf("%s differs from what the tool now makes of testdata/fixture:\n--- want\n%s\n--- got\n%s\n"+
			"If the change is deliberate, regenerate with:\n  %s", path, want, got, regenerate(test))
	}
}

// stable replaces the temporary directory in the tool's output.
func stable(s, dir string) string {
	return strings.ReplaceAll(s, dir, "<dir>")
}

// TestLiftFixture pins what the lift makes of the fixture: every file it
// writes, and the function-to-file map it prints.
func TestLiftFixture(t *testing.T) {
	root, dir := copyFixture(t)
	before := readTree(t, dir)
	code, stdout, stderr := runTool(t, root, dir)
	if code != 0 {
		t.Fatalf("liftmigrations exited %d:\n%s%s", code, stdout, stderr)
	}
	after := readTree(t, dir)
	var names []string
	for name, content := range after {
		if before[name] != content {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	want := []string{"migration_0002_create_widgets.go", "migration_0003_widget_labels.go", "migration_0004_no_comment.go",
		"migration_0007_same_name.go", "migration_0008_same_name.go", "migrations.go"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("the lift wrote %v, want %v", names, want)
	}
	for _, name := range names {
		compareGolden(t, "TestLiftFixture", "lift/"+name+".golden", after[name])
	}
	compareGolden(t, "TestLiftFixture", "lift/stdout.txt", stable(stdout, dir))
	command(t, root, "go", "vet", "./store")
	command(t, root, "go", "test", "-count=1", "./store")
}

// TestSpecFixture pins the declmove spec of the class A commit for the
// fixture: what the registry reaches goes to migration_helpers.go, in source
// order, and the rest but the registry, its element type and that type's
// methods to migrate_runner.go.
func TestSpecFixture(t *testing.T) {
	root, dir := copyFixture(t)
	spec := filepath.Join(root, "M10.json")
	code, stdout, stderr := runTool(t, root, "-spec", spec, dir)
	if code != 0 {
		t.Fatalf("liftmigrations -spec exited %d:\n%s%s", code, stdout, stderr)
	}
	b, err := os.ReadFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "TestSpecFixture", "spec.json", string(b))
	compareGolden(t, "TestSpecFixture", "spec_stdout.txt", stable(stdout, root))
	if got := readTree(t, dir); !equalTrees(got, readTree(t, "testdata/fixture")) {
		t.Error("-spec changed the package; it only writes the spec")
	}
}

func equalTrees(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestFixturePipeline runs M10's recipe on the fixture: the spec, declmove,
// the lift. The move leaves the declhash manifest identical (class A); the
// lift changes only the registry and adds the m00NN functions (class B);
// the result builds, its tests pass, and a second run changes nothing.
func TestFixturePipeline(t *testing.T) {
	declhash, declmove := buildTool(t, "declhash"), buildTool(t, "declmove")
	root, dir := copyFixture(t)
	spec := filepath.Join(root, "M10.json")
	manifest := func(name string) string {
		out := filepath.Join(root, name)
		command(t, root, declhash, "-o", out, "store")
		return out
	}
	base := manifest("base.txt")
	if code, stdout, stderr := runTool(t, root, "-spec", spec, dir); code != 0 {
		t.Fatalf("-spec exited %d:\n%s%s", code, stdout, stderr)
	}
	command(t, root, declmove, "-goimports=false", "-spec", spec)
	moved := manifest("moved.txt")
	command(t, root, declhash, "-compare", base, moved)
	for _, f := range []string{"migration_helpers.go", "migrate_runner.go"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("the move did not create %s: %v", f, err)
		}
	}
	if code, stdout, stderr := runTool(t, root, dir); code != 0 {
		t.Fatalf("the lift exited %d:\n%s%s", code, stdout, stderr)
	}
	lifted := manifest("lifted.txt")
	out, _ := runCmd(root, declhash, "-compare", moved, lifted)
	var diffs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "declhash:") {
			diffs = append(diffs, line)
		}
	}
	want := []string{"changed\tstore\tmigrations"}
	for _, f := range []string{"m0002CreateWidgets", "m0003WidgetLabels", "m0004NoComment", "m0007SameName", "m0008SameName"} {
		want = append(want, "added\tstore\t"+f)
	}
	sort.Strings(diffs)
	sort.Strings(want)
	if strings.Join(diffs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("declhash after the lift:\n%s\nwant only:\n%s", out, strings.Join(want, "\n"))
	}
	command(t, root, "go", "vet", "./store")
	command(t, root, "go", "test", "-count=1", "./store")
	registry := readTree(t, dir)["migrations.go"]
	if n := strings.Count(registry, "\n"); n > 40 {
		t.Errorf("migrations.go is %d lines after the recipe; it should hold only the type and the registry:\n%s", n, registry)
	}
	if !strings.Contains(registry, "\nimport \"database/sql\"\n") {
		t.Errorf("migrations.go should import database/sql alone, on one line, once the imports only the bodies used are gone:\n%s", registry)
	}

	settled := readTree(t, dir)
	code, stdout, _ := runTool(t, root, "-spec", spec, dir)
	if code != 0 || !strings.Contains(stdout, "nothing to move") {
		t.Errorf("-spec on the moved package: exit %d, %q; want nothing to move", code, stdout)
	}
	code, stdout, _ = runTool(t, root, dir)
	if code != 0 || !strings.Contains(stdout, "nothing to lift") {
		t.Errorf("a second lift: exit %d, %q; want nothing to lift", code, stdout)
	}
	if !equalTrees(readTree(t, dir), settled) {
		t.Error("a second run changed the package")
	}
}

// TestNames pins the naming rule M10 and N3's scaffold share.
func TestNames(t *testing.T) {
	for _, c := range []struct {
		version  int64
		name     string
		fn, file string
	}{
		{2, "unique_personal_org_per_user", "m0002UniquePersonalOrgPerUser", "migration_0002_unique_personal_org_per_user.go"},
		{16, "pgvector_artifact_embeddings", "m0016PgvectorArtifactEmbeddings", "migration_0016_pgvector_artifact_embeddings.go"},
		{51, "timestamptz_share_link_expiry", "m0051TimestamptzShareLinkExpiry", "migration_0051_timestamptz_share_link_expiry.go"},
		{120, "v2_tables", "m0120V2Tables", "migration_0120_v2_tables.go"},
		{12345, "x", "m12345X", "migration_12345_x.go"},
	} {
		if got := funcName(c.version, c.name); got != c.fn {
			t.Errorf("funcName(%d, %q) = %q, want %q", c.version, c.name, got, c.fn)
		}
		if got := fileName(c.version, c.name); got != c.file {
			t.Errorf("fileName(%d, %q) = %q, want %q", c.version, c.name, got, c.file)
		}
	}
}

// TestNotEverywhere pins which file names the lift refuses to write: those
// the go command would build only for one operating system or architecture,
// or take for a test.
func TestNotEverywhere(t *testing.T) {
	for file, want := range map[string]string{
		"migration_0051_timestamptz_share_link_expiry.go": "",
		"migration_0004_linux_tune.go":                    "",
		"migration_0004_unix_socket.go":                   "",
		"migration_0004_tests.go":                         "",
		"migration_0004_tune_linux.go":                    "a file of only the operating system or architecture its name ends in",
		"migration_0004_push_tokens_ios.go":               "a file of only the operating system or architecture its name ends in",
		"migration_0004_robot_arm.go":                     "a file of only the operating system or architecture its name ends in",
		"migration_0004_sync_windows_arm64.go":            "a file of only the operating system or architecture its name ends in",
		"migration_0004_x_test.go":                        "a test file",
		"migration_0004_tune_linux_test.go":               "a test file",
	} {
		if got := notEverywhere(file); got != want {
			t.Errorf("notEverywhere(%q) = %q, want %q", file, got, want)
		}
	}
}

// TestUsage covers the usage errors, which exit 2 and write nothing.
func TestUsage(t *testing.T) {
	root, dir := copyFixture(t)
	for _, args := range [][]string{{"-nope"}, {dir, dir}, {filepath.Join(root, "missing")}} {
		if code, _, _ := runTool(t, root, args...); code != 2 {
			t.Errorf("liftmigrations %v exited %d, want 2", args, code)
		}
	}
	if code, _, stderr := runTool(t, os.TempDir(), "-n"); code != 2 || !strings.Contains(stderr, "not inside a Go module") {
		t.Errorf("outside a module: exit %d, %q", code, stderr)
	}
	if !equalTrees(readTree(t, dir), readTree(t, "testdata/fixture")) {
		t.Error("a usage error wrote a file")
	}
}

// TestDryRun prints the map and the spec and writes nothing.
func TestDryRun(t *testing.T) {
	root, dir := copyFixture(t)
	code, stdout, stderr := runTool(t, root, "-n", dir)
	if code != 0 || !strings.Contains(stdout, "0002 create_widgets: migrations.go:35 -> migration_0002_create_widgets.go m0002CreateWidgets") ||
		!strings.Contains(stdout, "dry run; 5 migrations would move") {
		t.Errorf("-n: exit %d\n%s%s", code, stdout, stderr)
	}
	code, stdout, stderr = runTool(t, root, "-n", "-spec", filepath.Join(root, "M10.json"), dir)
	if code != 0 || !strings.Contains(stdout, `"file": "migrate_runner.go"`) {
		t.Errorf("-n -spec: exit %d\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "M10.json")); err == nil {
		t.Error("-n -spec wrote the spec")
	}
	if !equalTrees(readTree(t, dir), readTree(t, "testdata/fixture")) {
		t.Error("-n wrote a file")
	}
}

// refusal is a registry the lift must refuse, leaving every file as it was.
type refusal struct {
	name  string
	edit  func(src string) string
	extra map[string]string // files to add to the package
	want  string            // a regexp the message must match
}

// TestLiftRefusals covers what the lift cannot carry faithfully: it exits 1
// with a message naming the entry and what to do, and writes nothing.
func TestLiftRefusals(t *testing.T) {
	replace := func(old, new string) func(string) string {
		return func(src string) string {
			if !strings.Contains(src, old) {
				t.Fatalf("the fixture has no %q", old)
			}
			return strings.Replace(src, old, new, 1)
		}
	}
	cases := []refusal{
		{name: "comment inside an entry", edit: replace(`{Version: 3, Name: "widget_labels", Run:`,
			"{Version: 3, Name: \"widget_labels\", // stray\n\t\tRun:"),
			want: `migrations.go:\d+: migration 0003 widget_labels has a comment inside its entry but outside its function`},
		{name: "a call for a body", edit: replace(`{Version: 7, Name: "same_name", Run: func(tx *sql.Tx) error { return nil }},`,
			`{Version: 7, Name: "same_name", Run: makeRun()},`),
			extra: map[string]string{"make.go": "package store\n\nimport \"database/sql\"\n\nfunc makeRun() func(*sql.Tx) error { return nil }\n"},
			want:  "migration 0007 same_name's Run is `makeRun\\(\\)`, neither a function literal nor the name of a package-level function"},
		{name: "a name that is no file name", edit: replace(`"widget_labels"`, `"Widget-Labels"`),
			want: `migration 0003's Name "Widget-Labels" cannot be part of a file name`},
		{name: "a name that makes a linux file", edit: replace(`"no_comment"`, `"tune_linux"`),
			want: `migration 0004 tune_linux would move to migration_0004_tune_linux\.go, which the go command would read as a file of only ` +
				`the operating system or architecture its name ends in; rename the migration`},
		{name: "a name that makes a windows file", edit: replace(`"no_comment"`, `"maintenance_windows"`),
			want: `migration 0004 maintenance_windows would move to migration_0004_maintenance_windows\.go, which the go command would read as a file of only`},
		{name: "a name that makes an arm file", edit: replace(`"no_comment"`, `"robot_arm"`),
			want: `migration 0004 robot_arm would move to migration_0004_robot_arm\.go, which the go command would read as a file of only`},
		{name: "a name that makes a test", edit: replace(`"no_comment"`, `"x_test"`),
			want: `migration 0004 x_test would move to migration_0004_x_test\.go, which the go command would read as a test file; rename the migration`},
		{name: "the file exists", extra: map[string]string{"migration_0004_no_comment.go": "package store\n"},
			want: `migration 0004 no_comment would move to migration_0004_no_comment.go, which already exists`},
		{name: "the function exists", extra: map[string]string{"other.go": "package store\n\nfunc m0004NoComment() {}\n"},
			want: `migration 0004 no_comment would become m0004NoComment, which the package already declares`},
		{name: "versions out of order", edit: replace(`{Version: 4, Name: "no_comment"`, `{Version: 2, Name: "no_comment"`),
			want: `version 0002 follows 0003; the registry must be unique and ascending`},
		{name: "Run and RunDB", edit: replace(`{Version: 4, Name: "no_comment", Run:`, `{Version: 4, Name: "no_comment", RunDB: initSchema, Run:`),
			want: `migration 0004 no_comment sets both Run and RunDB`},
		{name: "a variable version", edit: replace(`{Version: 4,`, `{Version: four,`),
			extra: map[string]string{"four.go": "package store\n\nvar four = 4\n"},
			want:  `a migration's Version is not an integer constant`},
		{name: "no registry", edit: replace(`var migrations = []Migration{`, `var registry = []Migration{`),
			want: `no package-level var migrations`},
		{name: "two entries on a line", edit: replace("}},\n\t{Version: 4, Name: \"no_comment\"", "}}, {Version: 4, Name: \"no_comment\""),
			want: `migration 0004 no_comment shares a line with the element before it`},
		{name: "build constraints", edit: func(src string) string { return "//go:build !windows\n\n" + src },
			want: `migrations.go has build constraints`},
		{name: "a dot import", edit: replace(`str "strings"`, "str \"strings\"\n\t. \"errors\""),
			want: `migrations.go has a dot import`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, dir := copyFixture(t)
			path := filepath.Join(dir, "migrations.go")
			if c.edit != nil {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, c.edit(string(b)))
			}
			for name, content := range c.extra {
				writeFile(t, filepath.Join(dir, name), content)
			}
			before := readTree(t, dir)
			code, stdout, stderr := runTool(t, root, dir)
			if code == 0 {
				t.Fatalf("the lift passed:\n%s", stdout)
			}
			if !regexp.MustCompile(c.want).MatchString(stderr) {
				t.Errorf("message %q does not match %q", stderr, c.want)
			}
			if !equalTrees(readTree(t, dir), before) {
				t.Error("a refused lift wrote a file")
			}
		})
	}
}
