package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixtureModule copies testdata/fixture into a fresh module, example.com,
// and returns the package directory, example.com/fixture. Each edit rewrites
// table.go's text first.
func fixtureModule(t *testing.T, edits ...func(string) string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "fixture")
	err := filepath.WalkDir("testdata/fixture", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/fixture", p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com\n\ngo 1.25\n")
	if len(edits) > 0 {
		src := readFile(t, filepath.Join(dir, "table.go"))
		for _, e := range edits {
			src = e(src)
		}
		writeFile(t, filepath.Join(dir, "table.go"), src)
	}
	return dir
}

// replace returns an edit that must change the text.
func replace(t *testing.T, old, new string) func(string) string {
	return func(s string) string {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("the fixture has no %q to replace", old)
		}
		return strings.Replace(s, old, new, 1)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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

func fixtureSpec(t *testing.T) *spec {
	t.Helper()
	sp, err := loadSpec("testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func budgets() options { return options{maxFunc: funcBudget, maxFile: fileBudget} }

// snapshot is every file under dir, by relative path.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
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

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// goCmd runs the go command in dir.
func goCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return goRun(t, dir, nil, args...)
}

// goRun runs the go command in dir with env added to childEnv's.
func goRun(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = childEnv(env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s go %s in %s: %v\n%s", strings.Join(env, " "), strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// childEnv is this process's environment without what would change what a
// child go test compares or serves: no UPDATE_* (a golden or ratchet
// rewrite asked of the outer run must not turn the inner comparison into a
// rewrite), no OPENV_MCP_TOOLS, and no go.work; then extra.
func childEnv(extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "UPDATE_") || k == "OPENV_MCP_TOOLS" || k == "GOWORK" {
			continue
		}
		out = append(out, kv)
	}
	return append(append(out, "GOWORK=off"), extra...)
}

const wantFirst = `// The first tools of the fixture's table.

package fixture

import (
	"fmt"
	"net/url"
	str "strconv"
	"strings"

	"example.com/fixture/ref"
)

// firstTools returns alpha and beta.
func firstTools() []Tool {
	return []Tool{
		{
			Name: "alpha",
			Handler: func(args map[string]string) (string, error) {
				return prefix + strings.ToUpper(args["x"]), nil
			},
		},
		// beta's comment travels with it.
		{
			Name: "beta",
			Handler: func(args map[string]string) (string, error) {
				q := url.Values{"id": {args["id"]}}
				return q.Encode(), nil // the query as sent
			},
		}, // beta ends here
	}
}

func middleTools() []Tool {
	return []Tool{
		{
			Name: "gamma",
			Handler: func(args map[string]string) (string, error) {
				n, err := str.Atoi(args["n"])
				if err != nil {
					return "", fmt.Errorf("gamma: %w", err)
				}
				return ref.Normalize(fmt.Sprint(n)), nil
			},
		},
	}
}
`

// wantDelta imports strings only: delta's sort is a local variable.
const wantDelta = `package fixture

import (
	"strings"
)

// deltaTools returns delta, whose local sort is not package sort.
func deltaTools() []Tool {
	return []Tool{
		{
			Name: "delta",
			Handler: func(args map[string]string) (string, error) {
				sort := struct{ Keys []string }{Keys: []string{args["k"]}}
				return strings.Join(sort.Keys, ","), nil
			},
		},
	}
}
`

const wantLast = `package fixture

import (
	"sort"
	"strings"
)

// lastTools returns the rest,
// in the table's order.
func lastTools() []Tool {
	return []Tool{
		{
			Name: "epsilon",
			Handler: func(args map[string]string) (string, error) {
				keys := make([]string, 0, len(args))
				for k := range args {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				return strings.Join(keys, ","), nil
			},
		},
	}
}
`

// TestSplitFixture splits the fixture and checks every file it writes, that
// nothing else changes, that the result builds, vets and answers as before,
// and that each moved entry's lines are the old ones byte for byte.
func TestSplitFixture(t *testing.T) {
	dir := fixtureModule(t)
	before := snapshot(t, dir)
	goCmd(t, dir, "test", "-count=1", "./...")
	res, err := generate(dir, fixtureSpec(t), options{maxFunc: funcBudget, maxFile: fileBudget, vet: goVet})
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir)
	for _, name := range []string{"tools_first.go", "tools_delta.go", "tools_last.go"} {
		if _, ok := before[name]; ok {
			t.Fatalf("%s existed before the split", name)
		}
	}
	for name, want := range map[string]string{"tools_first.go": wantFirst, "tools_delta.go": wantDelta, "tools_last.go": wantLast} {
		if got := after[name]; got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}
	}
	changed := []string{}
	for _, name := range sortedKeys(after) {
		if before[name] != after[name] {
			changed = append(changed, name)
		}
	}
	if got := strings.Join(changed, " "); got != "table.go tools_delta.go tools_first.go tools_last.go" {
		t.Errorf("the split changed %s; want table.go and the three new files", got)
	}
	table := after["table.go"]
	wantTable := before["table.go"]
	start := strings.Index(wantTable, "\treturn []Tool{\n")
	end := strings.Index(wantTable, "\n}\n\n// Names")
	wantTable = wantTable[:start] + "\treturn slices.Concat(\n\t\tfirstTools(),\n\t\tmiddleTools(),\n\t\tdeltaTools(),\n\t\tlastTools(),\n\t)" + wantTable[end:]
	wantTable = strings.Replace(wantTable, "\t\"net/url\"\n\t\"sort\"\n\tstr \"strconv\"\n\t\"strings\"\n\n\t\"example.com/fixture/ref\"\n",
		"\t\"slices\"\n", 1)
	if table != wantTable {
		t.Errorf("table.go:\n%s\nwant:\n%s", table, wantTable)
	}
	// Every entry's lines, with the comments above and after it, are in
	// exactly one new file, as they were.
	for _, chunk := range []string{"\t\t{\n\t\t\tName: \"alpha\"", "\t\t// beta's comment travels with it.\n\t\t{\n",
		"\t\t}, // beta ends here\n", "\t\t\t\tsort := struct{ Keys []string }"} {
		n := strings.Count(after["tools_first.go"]+after["tools_delta.go"]+after["tools_last.go"], chunk)
		if n != 1 {
			t.Errorf("%q is in the new files %d times, want once", chunk, n)
		}
	}
	joined := strings.Join(res.lines, "\n")
	for _, want := range []string{"alpha -> firstTools (tools_first.go)", "gamma -> middleTools (tools_first.go)",
		"delta -> deltaTools (tools_delta.go)", "epsilon -> lastTools (tools_last.go)",
		"5 entries of fixture's Tools() into 4 constructors in 3 new files",
		"constructor lines (budget 100): firstTools 18, middleTools 14, deltaTools 11, lastTools 15",
		"go vet ./fixture passed", "UPDATE_RATCHETS=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report lacks %q:\n%s", want, joined)
		}
	}
	goCmd(t, dir, "test", "-count=1", "./...")
}

// TestSplitIsDoneOnce: on its own output, splittools finds nothing to split
// and says so, which is how the M11a test knows M11a has run.
func TestSplitIsDoneOnce(t *testing.T) {
	dir := fixtureModule(t)
	if _, err := generate(dir, fixtureSpec(t), budgets()); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	_, err := generate(dir, fixtureSpec(t), budgets())
	if !errors.Is(err, errAlreadySplit) || !strings.Contains(err.Error(), "returns slices.Concat( firstTools(),") {
		t.Fatalf("second run: %v, want errAlreadySplit naming the concatenation", err)
	}
	if after := snapshot(t, dir); len(after) != len(before) || after["table.go"] != before["table.go"] {
		t.Error("the refused second run changed files")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := fixtureModule(t)
	before := snapshot(t, dir)
	opts := budgets()
	opts.dryRun = true
	res, err := generate(dir, fixtureSpec(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, dir); len(after) != len(before) || after["table.go"] != before["table.go"] {
		t.Error("a dry run wrote files")
	}
	if last := res.lines[len(res.lines)-1]; last != "splittools: dry run; nothing written" {
		t.Errorf("last line %q", last)
	}
}

// TestVetFailureRestores: when go vet refuses the written split, the table's
// file gets its bytes back and the new files go.
func TestVetFailureRestores(t *testing.T) {
	dir := fixtureModule(t)
	before := snapshot(t, dir)
	opts := budgets()
	opts.vet = func(string) error { return errors.New("vet says no") }
	_, err := generate(dir, fixtureSpec(t), opts)
	if err == nil || !strings.Contains(err.Error(), "vet says no; every file is restored") {
		t.Fatalf("err = %v", err)
	}
	after := snapshot(t, dir)
	if strings.Join(sortedKeys(after), " ") != strings.Join(sortedKeys(before), " ") || after["table.go"] != before["table.go"] {
		t.Errorf("files not restored: %v", sortedKeys(after))
	}
}
