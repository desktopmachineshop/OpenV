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

// The ownership test of refactor plan step N1 (convention K15): every
// tracked file that is not a test file belongs to exactly one area of
// docs/areas.json, no file belongs to two, and every glob matches a
// tracked file. frontend/src/arch/areas.test.ts checks the same from the
// frontend's side.

// repoRoot is the repository root: go test runs in this package's
// directory, three levels below it.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %s: %v", root, err)
	}
	return root
}

// trackedFiles lists every file git tracks, relative to the root.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "-c", "core.quotePath=false", "ls-files", "-z")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v\n%s", err, stderr.String())
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) < 100 {
		t.Fatalf("git ls-files listed %d files; expected the whole repository", len(files))
	}
	return files
}

func TestEveryFileBelongsToExactlyOneArea(t *testing.T) {
	root := repoRoot(t)
	idx, err := LoadIndex(filepath.Join(root, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	files := trackedFiles(t, root)

	var problems []string
	owned := map[string]int{}
	for _, f := range files {
		claims := idx.Claims(f)
		if len(claims) == 1 {
			owned[claims[0].Area]++
			continue
		}
		if len(claims) == 0 && idx.IsTestFile(f) {
			continue
		}
		_, err := idx.Owner(f)
		problems = append(problems, err.Error())
	}
	if len(problems) > 0 {
		t.Errorf("%d tracked files do not belong to exactly one area:\n  %s\nCheck a path with: go run ./internal/tools/areas which <path>",
			len(problems), strings.Join(problems, "\n  "))
	}

	var unused []string
	for _, a := range idx.Areas {
		for i, rx := range a.globRegexp {
			found := false
			for _, f := range files {
				if rx.MatchString(f) {
					found = true
					break
				}
			}
			if !found {
				unused = append(unused, a.Name+": "+a.Globs[i])
			}
		}
	}
	if len(unused) > 0 {
		t.Errorf("%d globs in %s match no tracked file (a typo, or a file that moved):\n  %s",
			len(unused), IndexFile, strings.Join(unused, "\n  "))
	}

	for _, a := range idx.Areas {
		if owned[a.Name] == 0 {
			t.Errorf("area %s owns no file other than tests", a.Name)
		}
	}
}

func TestGlobSyntax(t *testing.T) {
	// The cases of refactor_guard.py's glob_regex, which the index shares.
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"internal/api/*.go", "internal/api/routes.go", true},
		{"internal/api/*.go", "internal/api/testdata/x.go", false},
		{"internal/api/org_*.go", "internal/api/org_handlers.go", true},
		{"internal/api/org_*.go", "internal/api/orgs.go", false},
		{"internal/runner/**", "internal/runner", true},
		{"internal/runner/**", "internal/runner/testdata/wire/claim.json", true},
		{"internal/runner/**", "internal/runners/x.go", false},
		{"**/*_test.go", "main_test.go", true},
		{"**/*_test.go", "cmd/agentd/cli_test.go", true},
		{"**/testdata/**", "cmd/agentd/testdata/cli/help.txt", true},
		{"frontend/src/api/**/vv*", "frontend/src/api/vv.ts", true},
		{"frontend/src/api/**/vv*", "frontend/src/api/types/vv.ts", true},
		{"e2e/*", "e2e/.gitignore", true},
		{"e2e/*", "e2e/tests/smoke.spec.ts", false},
		{"Dockerfile*", "Dockerfile.api", true},
		{"Dockerfile*", "frontend/Dockerfile", false},
		{"docs/api-spec.md", "docs/api-spec.mdx", false},
		{"cmd/?gentd/**", "cmd/agentd/main.go", true},
		{"a.b", "axb", false},
	}
	for _, c := range cases {
		rx, err := compileGlob(c.glob)
		if err != nil {
			t.Fatal(err)
		}
		if got := rx.MatchString(c.path); got != c.want {
			t.Errorf("glob %q on %q: got %v, want %v", c.glob, c.path, got, c.want)
		}
	}
	for _, bad := range []string{"", "/abs/path", "./rel", `win\path`} {
		if _, err := compileGlob(bad); err == nil {
			t.Errorf("glob %q: accepted, want an error", bad)
		}
	}
}

func TestOwnerNamesTheAreas(t *testing.T) {
	idx, err := ParseIndex([]byte(`{
		"test_files": {"globs": ["**/*_test.go"]},
		"areas": [
			{"name": "first", "description": "One.", "globs": ["x/**"]},
			{"name": "second", "description": "Two.", "globs": ["y/*.go", "x/*.go"]}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := idx.Owner("x/sub/a.txt"); err != nil || got != "first" {
		t.Errorf("x/sub/a.txt: got %q, %v; want first", got, err)
	}
	if got, err := idx.Owner("y/a.go"); err != nil || got != "second" {
		t.Errorf("y/a.go: got %q, %v; want second", got, err)
	}
	for path, want := range map[string]string{
		"x/a.go":      "x/a.go is claimed by 2 areas, first (x/**), second (x/*.go)",
		"z/a.go":      "no area claims z/a.go; add a glob",
		"z/a_test.go": "no area claims z/a_test.go; it is a test file",
	} {
		if _, err := idx.Owner(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got error %v, want one containing %q", path, err, want)
		}
	}

	for name, doc := range map[string]string{
		"unknown key":    `{"test_files": {"globs": ["t"]}, "areas": [{"name": "a", "description": "A.", "globs": ["x"], "owner": "me"}]}`,
		"duplicate area": `{"test_files": {"globs": ["t"]}, "areas": [{"name": "a", "description": "A.", "globs": ["x"]}, {"name": "a", "description": "A.", "globs": ["y"]}]}`,
		"no globs":       `{"test_files": {"globs": ["t"]}, "areas": [{"name": "a", "description": "A.", "globs": []}]}`,
		"no description": `{"test_files": {"globs": ["t"]}, "areas": [{"name": "a", "globs": ["x"]}]}`,
		"no test files":  `{"areas": [{"name": "a", "description": "A.", "globs": ["x"]}]}`,
	} {
		if _, err := ParseIndex([]byte(doc)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

func TestWhich(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		name      string
		cwd       string
		args      []string
		code      int
		stdout    string
		stderrHas string
	}{
		{"from the root", root, []string{"which", "internal/api/routes.go"}, 0, "platform-http\n", ""},
		{"dot-relative", root, []string{"which", "./frontend/src/views/ModuleView.tsx"}, 0, "requirements-core\n", ""},
		{"from a subdirectory", filepath.Join(root, "internal", "api"), []string{"which", "org_member_handlers.go"}, 0, "tenancy-identity\n", ""},
		{"absolute", filepath.Join(root, "frontend"), []string{"which", filepath.Join(root, "cmd", "agentd", "main.go")}, 0, "runner-fleet\n", ""},
		{"a file that does not exist yet", root, []string{"which", "internal/domain/vv/new.go"}, 0, "verification\n", ""},
		{"unclaimed", root, []string{"which", "internal/newpackage/new.go"}, 1, "", "no area claims internal/newpackage/new.go"},
		{"unclaimed test file", root, []string{"which", "internal/api/route_inventory_test.go"}, 1, "", "it is a test file"},
		{"outside the repository", root, []string{"which", filepath.Join("..", "elsewhere.go")}, 2, "", "outside the repository"},
		{"no path", root, []string{"which"}, 2, "", "usage:"},
		{"unknown command", root, []string{"list"}, 2, "", "usage:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(c.args, c.cwd, &stdout, &stderr)
			if code != c.code || stdout.String() != c.stdout || !strings.Contains(stderr.String(), c.stderrHas) {
				t.Errorf("run(%q) in %s: exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr containing %q",
					c.args, c.cwd, code, stdout.String(), stderr.String(), c.code, c.stdout, c.stderrHas)
			}
		})
	}

	// Every area answers for one of its own files.
	idx, err := LoadIndex(filepath.Join(root, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range trackedFiles(t, root) {
		claims := idx.Claims(f)
		if len(claims) != 1 || idx.IsTestFile(f) || seen[claims[0].Area] {
			continue
		}
		seen[claims[0].Area] = true
		var stdout, stderr bytes.Buffer
		if code := run([]string{"which", f}, root, &stdout, &stderr); code != 0 || stdout.String() != claims[0].Area+"\n" {
			t.Errorf("which %s: exit %d, %q %q; want %s", f, code, stdout.String(), stderr.String(), claims[0].Area)
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) != len(idx.Areas) {
		t.Errorf("which answered for %d areas (%s), want all %d", len(names), strings.Join(names, ", "), len(idx.Areas))
	}
}
