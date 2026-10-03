package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Each scaffold's test copies the files it reads (go.mod and the
// package's directory) into a temporary root, scaffolds there through the
// command line's own code, checks that every edit is one block appended to
// a list, and compiles the result: `go vet -overlay` on this module with
// each file written laid over the repository's, so the real tree is never
// written and the new code type-checks against everything it uses, test
// files included.

func TestAPIAreaCompiles(t *testing.T) {
	repo, tmp := scratch(t, "internal/api", "internal/api/testdata/routes.txt")
	scaffold(t, tmp, "api-area", "widget-reports")
	changed := changedFiles(t, repo, tmp, "internal/api")
	want := []string{"internal/api/routes.go", "internal/api/widget_reports_handlers.go"}
	if strings.Join(changed, " ") != strings.Join(want, " ") {
		t.Fatalf("api-area wrote %v; want %v", changed, want)
	}
	added := appendedTo(t, repo, tmp, "internal/api/routes.go")
	if added != "\th.registerWidgetReportsRoutes(router)\n" {
		t.Errorf("routes.go gained %q; want the registrar's call as RegisterRoutes' last line", added)
	}
	vet(t, repo, overlay(t, repo, tmp, changed), "./internal/api")
}

func TestMigrationCompiles(t *testing.T) {
	repo, tmp := scratch(t, "internal/persistence/postgres")
	before := registryLines(t, filepath.Join(repo, postgresDir, "migrations.go"))
	var last int
	if _, err := fmt.Sscanf(before[len(before)-1], "{Version: %d,", &last); err != nil {
		t.Fatal(err)
	}
	scaffold(t, tmp, "migration", "widget-reports")
	scaffold(t, tmp, "migration", "widget_counts") // the next free version after the first
	changed := changedFiles(t, repo, tmp, postgresDir)
	if len(changed) != 3 {
		t.Fatalf("two migrations wrote %v; want two new files and migrations.go", changed)
	}
	appendedTo(t, repo, tmp, postgresDir+"/migrations.go")
	lines := registryLines(t, filepath.Join(tmp, postgresDir, "migrations.go"))
	for i, want := range []string{
		fmt.Sprintf(`{Version: %d, Name: "widget_reports", Run: m%04dWidgetReports},`, last+1, last+1),
		fmt.Sprintf(`{Version: %d, Name: "widget_counts", Run: m%04dWidgetCounts},`, last+2, last+2),
	} {
		if got := lines[len(before)+i]; got != want {
			t.Errorf("registry line %d is %q; want %q", len(before)+i+1, got, want)
		}
	}
	for i, file := range []string{"widget_reports", "widget_counts"} {
		if p := fmt.Sprintf("%s/migration_%04d_%s.go", postgresDir, last+1+i, file); !slices.Contains(changed, p) {
			t.Errorf("no %s among %v", p, changed)
		}
	}
	ov := overlay(t, repo, tmp, changed)
	vet(t, repo, ov, "./"+postgresDir)

	// The package's own layout test (M10), with no database: built with
	// the overlay and run in the temporary copy, so the files it reads are
	// the files it was built from.
	bin := filepath.Join(t.TempDir(), "postgres.test")
	goCmd(t, repo, "test", "-c", "-overlay", ov, "-o", bin, "./"+postgresDir)
	cmd := exec.Command(bin, "-test.v", "-test.run", "^(TestEachMigrationFileRegistersItsVersion|TestRegistryIsOrderedWithoutDB)$")
	cmd.Dir = filepath.Join(tmp, filepath.FromSlash(postgresDir))
	cmd.Env = append(os.Environ(), "OPENV_TEST_DATABASE_URL=")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.Count(string(out), "--- PASS") != 2 {
		t.Fatalf("the migration layout tests on the scaffolded package: %v\n%s", err, out)
	}
}

func TestMCPToolCompiles(t *testing.T) {
	repo, tmp := scratch(t, "internal/mcp")
	scaffold(t, tmp, "-area", "vv", "mcp-tool", "get-vv-summary")    // a file whose constructor is not last
	scaffold(t, tmp, "mcp-tool", "-area", "widgets", "list_widgets") // a new area file
	scaffold(t, tmp, "mcp-tool", "count_widgets")                    // the default: the last constructor
	changed := changedFiles(t, repo, tmp, mcpDir)
	want := []string{"internal/mcp/tools.go", "internal/mcp/tools_vv.go", "internal/mcp/tools_widgets.go"}
	if strings.Join(changed, " ") != strings.Join(want, " ") {
		t.Fatalf("mcp-tool wrote %v; want %v", changed, want)
	}
	if added := appendedTo(t, repo, tmp, "internal/mcp/tools.go"); added != "\t\twidgetsTools(),\n" {
		t.Errorf("tools.go gained %q; want the new constructor as Tools()' last", added)
	}
	if added := appendedTo(t, repo, tmp, "internal/mcp/tools_vv.go"); !strings.Contains(added, `Name:        "get_vv_summary",`) {
		t.Errorf("tools_vv.go gained\n%s\nwant the tool get_vv_summary", added)
	}
	src, err := os.ReadFile(filepath.Join(tmp, "internal/mcp/tools_widgets.go"))
	if err != nil {
		t.Fatal(err)
	}
	if i, j := bytes.Index(src, []byte(`"list_widgets"`)), bytes.Index(src, []byte(`"count_widgets"`)); i < 0 || j < i {
		t.Errorf("tools_widgets.go does not hold list_widgets then count_widgets:\n%s", src)
	}
	vet(t, repo, overlay(t, repo, tmp, changed), "./"+mcpDir)
}

// TestDryRunAndRefusalsWriteNothing runs each kind with -n, and each
// refusal, and checks the copy is as it was. The names it refuses are
// taken in the copy first, by hand or by a scaffold, so the test leans on
// no name of the real tree.
func TestDryRunAndRefusalsWriteNothing(t *testing.T) {
	_, tmp := scratch(t, "internal/api", "internal/api/testdata/routes.txt", "internal/persistence/postgres", "internal/mcp")
	write := func(path, text string, flag int) {
		f, err := os.OpenFile(filepath.Join(tmp, filepath.FromSlash(path)), flag|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = f.WriteString(text)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	write("internal/api/widget_files_handlers.go", "package api\n", os.O_CREATE|os.O_EXCL)
	write("internal/api/widget_counts_test.go", "package api\n\nfunc registerWidgetCountsRoutes() {}\n", os.O_CREATE|os.O_EXCL)
	write("internal/api/testdata/routes.txt", "GET /api/v1/projects/{id}/widget-totals\n", os.O_APPEND)
	scaffold(t, tmp, "migration", "widget_logs")
	scaffold(t, tmp, "mcp-tool", "list_widgets")
	before := map[string][]byte{}
	for _, dir := range []string{"internal/api", "internal/api/testdata", postgresDir, mcpDir} {
		for _, p := range listFiles(t, tmp, dir) {
			before[p], _ = os.ReadFile(filepath.Join(tmp, filepath.FromSlash(p)))
		}
	}

	for _, c := range []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"-n", "api-area", "widget-reports"}, 0, "would create internal/api/widget_reports_handlers.go"},
		{[]string{"migration", "widget_reports", "-n"}, 0, "would add to internal/persistence/postgres/migrations.go"},
		{[]string{"-n", "-area", "widgets", "mcp-tool", "list-widget-reports"}, 0, "would create internal/mcp/tools_widgets.go"},
		{[]string{"api-area", "widget-files"}, 1, "internal/api/widget_files_handlers.go already exists"},
		{[]string{"api-area", "widget-counts"}, 1, "registerWidgetCountsRoutes is already declared in internal/api/widget_counts_test.go"},
		{[]string{"api-area", "widget-totals"}, 1, "GET /api/v1/projects/{id}/widget-totals is already a route"},
		{[]string{"migration", "widget-logs"}, 1, `is already named "widget_logs"`},
		{[]string{"mcp-tool", "list-widgets"}, 1, `a tool named "list_widgets" is already declared`},
		{[]string{"api-area", "Widgets"}, 2, "lower-case words"},
		{[]string{"api-area", "widget--reports"}, 2, "lower-case words"},
		{[]string{"-area", "vv", "migration", "x"}, 2, "-area applies to mcp-tool only"},
		{[]string{"page", "widgets"}, 2, `unknown kind "page"`},
		{[]string{"api-area"}, 2, "usage:"},
	} {
		var out, errb bytes.Buffer
		code := run(c.args, tmp, &out, &errb, func(root, path string) string { return "Area: " + path })
		if code != c.code || !strings.Contains(out.String()+errb.String(), c.out) {
			t.Errorf("scaffold %s: exit %d, want %d, and output containing %q:\n%s%s", strings.Join(c.args, " "), code, c.code, c.out, &out, &errb)
		}
	}
	after := 0
	for _, dir := range []string{"internal/api", "internal/api/testdata", postgresDir, mcpDir} {
		for _, p := range listFiles(t, tmp, dir) {
			after++
			if b, _ := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(p))); !bytes.Equal(b, before[p]) {
				t.Errorf("a dry run or a refusal wrote %s", p)
			}
		}
	}
	if after != len(before) {
		t.Errorf("a dry run or a refusal changed the number of files from %d to %d", len(before), after)
	}
}

// TestNames pins the name mapping main.go documents.
func TestNames(t *testing.T) {
	for _, s := range []string{"widget-reports", "widget_reports"} {
		n, err := parseName(s)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join([]string{n.snake(), n.kebab(), n.camel(), n.lowerCamel(), n.text()}, " | ")
		if want := "widget_reports | widget-reports | WidgetReports | widgetReports | widget reports"; got != want {
			t.Errorf("%s maps to %s; want %s", s, got, want)
		}
	}
	if n, _ := parseName("ai-map2"); n.camel() != "AiMap2" {
		t.Errorf("ai-map2 is %s; want AiMap2 (no initialisms)", n.camel())
	}
	for _, bad := range []string{"", "Widget", "2widgets", "widget-", "-widget", "widget__x", "wid get", "widget.go"} {
		if _, err := parseName(bad); err == nil {
			t.Errorf("parseName(%q) accepted it", bad)
		}
	}
}

// TestRegenerateCommandsAreTheREADMEs keeps the commands the scaffold
// prints the ones the area READMEs name.
func TestRegenerateCommandsAreTheREADMEs(t *testing.T) {
	repo := repoRoot(t)
	for cmd, readme := range map[string]string{
		regenRoutes:    "internal/api/README.md",
		regenMigration: "internal/persistence/postgres/README.md",
		regenMCPTools:  "internal/runner/README.md",
	} {
		b, err := os.ReadFile(filepath.Join(repo, readme))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "`"+cmd+"`") {
			t.Errorf("%s does not name %s", readme, cmd)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// scratch copies go.mod, and each path (a directory's own files, or one
// file), from the repository into a temporary root.
func scratch(t *testing.T, paths ...string) (repo, tmp string) {
	t.Helper()
	repo, tmp = repoRoot(t), t.TempDir()
	copyFile(t, repo, tmp, "go.mod")
	for _, p := range paths {
		entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(p)))
		if err != nil {
			copyFile(t, repo, tmp, p)
			continue
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				copyFile(t, repo, tmp, p+"/"+e.Name())
			}
		}
	}
	return repo, tmp
}

func copyFile(t *testing.T, from, to, path string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(to, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func scaffold(t *testing.T, root string, args ...string) {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(args, root, &out, &errb, func(root, path string) string { return "Area: " + path }); code != 0 {
		t.Fatalf("scaffold %s: exit %d\n%s%s", strings.Join(args, " "), code, &out, &errb)
	}
	if !strings.Contains(out.String(), "Left to do:") {
		t.Errorf("scaffold %s printed no steps:\n%s", strings.Join(args, " "), &out)
	}
}

// listFiles lists the regular files directly in dir under root, sorted.
func listFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			out = append(out, dir+"/"+e.Name())
		}
	}
	return out
}

// changedFiles lists the files of dir in tmp that are new or differ from
// the repository's, sorted.
func changedFiles(t *testing.T, repo, tmp, dir string) []string {
	t.Helper()
	var out []string
	for _, p := range listFiles(t, tmp, dir) {
		got, _ := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(p)))
		old, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(p)))
		if err != nil || !bytes.Equal(got, old) {
			out = append(out, p)
		}
	}
	return out
}

// appendedTo checks that path differs from the repository's copy by one
// block of lines inserted just above the line that closes a list (a brace
// or a parenthesis), so no line moved or changed, and returns the block.
func appendedTo(t *testing.T, repo, tmp, path string) string {
	t.Helper()
	read := func(root string) []string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		return strings.SplitAfter(string(b), "\n")
	}
	old, new := read(repo), read(tmp)
	i := 0
	for i < len(old) && i < len(new) && old[i] == new[i] {
		i++
	}
	n := len(new) - len(old)
	if i == len(old) || n <= 0 || strings.Join(new[i+n:], "") != strings.Join(old[i:], "") {
		t.Fatalf("%s is not its old lines with one block inserted", path)
	}
	if closing := strings.TrimSpace(old[i]); closing != "}" && closing != ")" {
		t.Errorf("%s: the block is inserted above %q, not above the line that closes a list", path, closing)
	}
	return strings.Join(new[i:i+n], "")
}

// overlay writes a go build overlay that lays each changed file of tmp
// over the repository's.
func overlay(t *testing.T, repo, tmp string, changed []string) string {
	t.Helper()
	replace := map[string]string{}
	for _, p := range changed {
		replace[filepath.Join(repo, filepath.FromSlash(p))] = filepath.Join(tmp, filepath.FromSlash(p))
	}
	b, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// vet type-checks pkgs, their tests included, with the overlay.
func vet(t *testing.T, repo, overlay string, pkgs ...string) {
	t.Helper()
	goCmd(t, repo, append([]string{"vet", "-overlay", overlay}, pkgs...)...)
}

func goCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// registryLines returns the lines of the migrations registry, trimmed.
func registryLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "{Version: ") {
			out = append(out, l)
		}
	}
	return out
}
