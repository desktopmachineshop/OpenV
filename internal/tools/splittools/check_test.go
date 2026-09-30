package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfCheckCatches renders the fixture's split, tampers with it as a
// faulty generator could, and requires the self-check to refuse each: the
// check, not the generator's care, is what makes a written split faithful.
// Only a new file over budget is the spec's to settle (errPlan); every other
// failure is a fault, which TestM11aOnTheWorkingTree never skips.
func TestSelfCheckCatches(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		old    string
		new    string
		want   string
		budget int
		plan   bool
	}{
		{"constructors swapped", "table.go", "\t\tmiddleTools(),\n\t\tdeltaTools(),\n", "\t\tdeltaTools(),\n\t\tmiddleTools(),\n",
			"the order differs:\n  base: alpha, beta, gamma, delta, epsilon\n  head: alpha, beta, delta, gamma, epsilon", 0, false},
		{"a constructor dropped", "table.go", "\t\tdeltaTools(),\n", "", `"delta" is gone`, 0, false},
		{"a handler changed", "tools_delta.go", `strings.Join(sort.Keys, ",")`, `strings.Join(sort.Keys, ";")`,
			"\"delta\" changed (deltaTools in tools_delta.go):\n  line 5", 0, false},
		{"a comment dropped", "tools_first.go", "\t\t// beta's comment travels with it.\n", "",
			`"beta" changed (firstTools in tools_first.go)`, 0, false},
		{"a trailing comment dropped", "tools_first.go", "}, // beta ends here", "},", `"beta" changed`, 0, false},
		{"an import another declaration uses dropped", "table.go", "\t\"fmt\"\n", "",
			"fixture.Names changed:\n  line ", 0, false},
		{"the function's doc changed", "table.go", "// Tools returns the table.", "// Tools returns a table.",
			"Tools() changed its signature, doc comment or file", 0, false},
		{"another declaration changed", "table.go", `const prefix = "tool:"`, `const prefix = "tool;"`,
			"fixture.const prefix changed", 0, false},
		{"a declaration added", "tools_last.go", "// lastTools returns", "var extra = 1\n\n// lastTools returns",
			"fixture.var extra is new", 0, false},
		{"a new file over budget", "tools_first.go", "", "", "tools_first.go would have 47 lines, over K14's 46-line file budget", 46, true},
		{"a constructor longer than planned", "tools_delta.go", "\t\t\t},\n\t\t},\n\t}\n}\n", "\t\t\t},\n\t\t},\n\n\t}\n}\n",
			"constructor deltaTools spans 12 lines (planned 11; K14's function budget is 100)", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fixtureModule(t)
			srcs, err := readDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			p, err := parsePkg(srcs)
			if err != nil {
				t.Fatal(err)
			}
			sp := fixtureSpec(t)
			tb, err := p.readTable(sp.funcName())
			if err != nil {
				t.Fatal(err)
			}
			plans, _, err := planSplit(sp, tb, funcBudget)
			if err != nil {
				t.Fatal(err)
			}
			files, _, err := render(p, tb, sp, plans)
			if err != nil {
				t.Fatal(err)
			}
			budget := fileBudget
			if c.budget > 0 {
				budget = c.budget
			}
			if err := selfCheck(p, tb, sp, plans, files, funcBudget, fileBudget); err != nil {
				t.Fatalf("the untampered split fails its self-check: %v", err)
			}
			if c.old != "" {
				if !bytes.Contains(files[c.file], []byte(c.old)) {
					t.Fatalf("%s has no %q:\n%s", c.file, c.old, files[c.file])
				}
				files[c.file] = bytes.Replace(files[c.file], []byte(c.old), []byte(c.new), 1)
			}
			err = selfCheck(p, tb, sp, plans, files, funcBudget, budget)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("self-check: %v\nwant it to say %q", err, c.want)
			}
			if errors.Is(err, errPlan) != c.plan {
				t.Errorf("errors.Is(err, errPlan) = %v, want %v", !c.plan, c.plan)
			}
		})
	}
}

// gitRepo makes dir's module a git repository with one commit.
func gitRepo(t *testing.T, root string) func(args ...string) string {
	t.Helper()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=splittools", "-c", "user.email=splittools@example.com",
			"-c", "commit.gpgsign=false", "-c", "gc.auto=0"}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	return git
}

func runTool(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// TestCheckMode: -check proves a split against the table at a git ref, in
// the working tree or at -head, and fails on a changed, reordered or
// dropped entry.
func TestCheckMode(t *testing.T) {
	dir := fixtureModule(t)
	git := gitRepo(t, filepath.Dir(dir))
	if code, out, errs := runTool("-check", "-base", "HEAD", dir); code != 0 ||
		!strings.Contains(out, "alpha -> Tools (table.go)") || !strings.Contains(out, "returns the same 5 entries") {
		t.Fatalf("unsplit against itself: %d\n%s%s", code, out, errs)
	}
	if _, err := generate(dir, fixtureSpec(t), budgets()); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runTool("-check", "-base", "HEAD", dir)
	if code != 0 || !strings.Contains(out, "gamma -> middleTools (tools_first.go)\n") ||
		!strings.Contains(out, "splittools: Tools() returns the same 5 entries in the same order as at HEAD, each unchanged") {
		t.Fatalf("the split against its base: %d\n%s%s", code, out, errs)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "split")
	if code, out, errs := runTool("-check", "-base", "HEAD~1", "-head", "HEAD", dir); code != 0 {
		t.Fatalf("-head HEAD against HEAD~1: %d\n%s%s", code, out, errs)
	}
	edit := func(name, old, new string) {
		p := filepath.Join(dir, name)
		src := readFile(t, p)
		if !strings.Contains(src, old) {
			t.Fatalf("%s has no %q", name, old)
		}
		writeFile(t, p, strings.Replace(src, old, new, 1))
	}
	edit("tools_last.go", "sort.Strings(keys)", "sort.Sort(sort.Reverse(sort.StringSlice(keys)))")
	code, out, _ = runTool("-check", "-base", "HEAD~1", dir)
	if code != 1 || !strings.Contains(out, `splittools: "epsilon" changed (lastTools in tools_last.go)`) ||
		!strings.Contains(out, "Tools() differs from HEAD~1: not a faithful split") {
		t.Fatalf("a changed handler: %d\n%s", code, out)
	}
	git("checkout", "-q", "--", ".")
	edit("table.go", "\t\tfirstTools(),\n\t\tmiddleTools(),\n", "\t\tmiddleTools(),\n\t\tfirstTools(),\n")
	if code, out, _ = runTool("-check", "-base", "HEAD~1", dir); code != 1 || !strings.Contains(out, "the order differs") {
		t.Fatalf("reordered constructors: %d\n%s", code, out)
	}
	git("checkout", "-q", "--", ".")
	edit("table.go", "\t\tdeltaTools(),\n", "")
	if code, out, _ = runTool("-check", "-base", "HEAD~1", dir); code != 1 || !strings.Contains(out, `"delta" is gone`) {
		t.Fatalf("a dropped constructor: %d\n%s", code, out)
	}
	git("checkout", "-q", "--", ".")
	// Outside the entries: a declaration changed, one added in a new file,
	// and Tools()'s doc changed each fail; a declaration moved to another
	// file, as M11a's class A commit moves them, does not.
	outside := "the package differs from HEAD~1 outside Tools()'s entries: not a faithful split"
	edit("table.go", `const prefix = "tool:"`, `const prefix = "tool;"`)
	if code, out, _ = runTool("-check", "-base", "HEAD~1", dir); code != 1 || !strings.Contains(out, "splittools: fixture.const prefix changed") ||
		!strings.Contains(out, outside) || strings.Contains(out, "Tools() differs from") {
		t.Fatalf("a declaration changed outside the entries: %d\n%s", code, out)
	}
	git("checkout", "-q", "--", ".")
	extra := filepath.Join(dir, "tools_extra.go")
	writeFile(t, extra, "package fixture\n\nfunc init() { extra = true }\n\nvar extra bool\n")
	if code, out, _ = runTool("-check", "-base", "HEAD~1", dir); code != 1 || !strings.Contains(out, "splittools: fixture.init is new") ||
		!strings.Contains(out, "splittools: fixture.var extra is new") || !strings.Contains(out, outside) {
		t.Fatalf("an init and a var added in a new file: %d\n%s", code, out)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	edit("table.go", "// Tools returns the table.", "// Tools returns a table.")
	if code, out, _ = runTool("-check", "-base", "HEAD~1", dir); code != 1 ||
		!strings.Contains(out, "splittools: Tools() changed its signature, doc comment or file") || !strings.Contains(out, outside) {
		t.Fatalf("Tools()'s doc changed: %d\n%s", code, out)
	}
	git("checkout", "-q", "--", ".")
	moved := "// prefix is used by an entry and stays here.\nconst prefix = \"tool:\"\n"
	edit("table.go", moved, "")
	writeFile(t, filepath.Join(dir, "prefix.go"), "package fixture\n\n"+moved)
	if code, out, errs := runTool("-check", "-base", "HEAD~1", dir); code != 0 ||
		!strings.Contains(out, "every other declaration of the package, and Tools()'s doc comment and signature, are as they were") {
		t.Fatalf("a declaration moved to another file: %d\n%s%s", code, out, errs)
	}
	if err := os.Remove(filepath.Join(dir, "prefix.go")); err != nil {
		t.Fatal(err)
	}
	git("checkout", "-q", "--", ".")
	edit("table.go", "return slices.Concat(", "return append(")
	if code, _, errs = runTool("-check", "-base", "HEAD~1", dir); code != 2 ||
		!strings.Contains(errs, "returns neither a []Tool literal, nor slices.Concat of constructors, nor one constructor") {
		t.Fatalf("an unknown shape: %d\n%s", code, errs)
	}
	if code, _, errs = runTool("-check", "-base", "HEAD~1", filepath.Join(dir, "nope")); code != 2 || !strings.Contains(errs, "holds no Go files") {
		t.Fatalf("a directory with no Go files: %d\n%s", code, errs)
	}
}

// TestRunExitStatus: 0 for a split, 1 for a refusal, 2 for usage and spec
// errors.
func TestRunExitStatus(t *testing.T) {
	dir := fixtureModule(t)
	specFile := func(body string) string {
		p := filepath.Join(t.TempDir(), "spec.json")
		writeFile(t, p, body)
		return p
	}
	fixtureJSON := strings.Replace(readFile(t, "testdata/fixture.json"), `"package": "fixture"`,
		`"package": "`+filepath.ToSlash(dir)+`"`, 1)
	swapped := strings.Replace(strings.Replace(fixtureJSON, `["alpha", "beta"]`, `["TMP"]`, 1), `["gamma"]`, `["alpha", "beta"]`, 1)
	swapped = strings.Replace(swapped, `["TMP"]`, `["gamma"]`, 1)
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no arguments", nil, 2, "usage: splittools"},
		{"a stray argument", []string{"-spec", "x.json", "extra"}, 2, "usage: splittools"},
		{"-base without -check", []string{"-spec", "x.json", "-base", "HEAD"}, 2, "usage: splittools"},
		{"-check without -base", []string{"-check", dir}, 2, "usage: splittools"},
		{"-check with -spec", []string{"-check", "-base", "HEAD", "-spec", "x.json", dir}, 2, "usage: splittools"},
		{"a missing spec", []string{"-spec", filepath.Join(t.TempDir(), "none.json")}, 2, "no such file"},
		{"a malformed spec", []string{"-spec", specFile("{")}, 2, "unexpected EOF"},
		{"a refused split", []string{"-n", "-spec", specFile(swapped)}, 1, "constructors are concatenated in spec order"},
		{"a package with no Go files", []string{"-n", "-spec", specFile(strings.Replace(fixtureJSON, filepath.ToSlash(dir), filepath.ToSlash(t.TempDir()), 1))},
			2, "holds no Go files"},
		{"a package that is not there", []string{"-n", "-spec", specFile(strings.Replace(fixtureJSON, filepath.ToSlash(dir), filepath.ToSlash(dir)+"/nope", 1))},
			2, "no Go source"},
		{"a dry run", []string{"-n", "-spec", specFile(fixtureJSON)}, 0, "splittools: dry run; nothing written"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errs := runTool(c.args...)
			if code != c.code || !strings.Contains(out+errs, c.want) {
				t.Fatalf("exit %d, want %d; output:\n%s%s\nwant it to say %q", code, c.code, out, errs, c.want)
			}
		})
	}
	if code, out, errs := runTool("-vet=false", "-spec", specFile(fixtureJSON)); code != 0 ||
		!strings.Contains(out, "into 4 constructors in 3 new files") || strings.Contains(out, "go vet") {
		t.Fatalf("a split without vet: %d\n%s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(dir, "tools_last.go")); err != nil {
		t.Fatal("the split wrote no tools_last.go")
	}
}

// TestSpecsLoad: every committed spec is valid on its own.
func TestSpecsLoad(t *testing.T) {
	specs, err := filepath.Glob("specs/*.json")
	if err != nil || len(specs) == 0 {
		t.Fatalf("no specs: %v", err)
	}
	for _, p := range specs {
		if _, err := loadSpec(p); err != nil {
			t.Error(err)
		}
	}
}
