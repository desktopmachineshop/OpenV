package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runTool(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
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

func flattenOf(t *testing.T, files map[string]string) ([]string, error) {
	t.Helper()
	var srcs []source
	for name, src := range files {
		srcs = append(srcs, source{name, []byte(src)})
	}
	p, err := parsePackage(srcs, false)
	if err != nil {
		t.Fatal(err)
	}
	return flatten(p, "main", "a")
}

func TestMapListsEveryDeclarationInFileOrder(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"b.go":      "package p\n\ntype T struct{}\n\nfunc (t *T) M() {}\n\nconst (\n\tX = 1\n\tY = 2\n)\n",
		"a.go":      "package p\n\nfunc F() {}\n\nvar v, w = 1, 2\n",
		"a_test.go": "package p\n\nfunc helper() {}\n",
	})
	code, out, _ := runTool(dir)
	want := "F -> a.go\nv -> a.go\nw -> a.go\nT -> b.go\n(*T).M -> b.go\nX -> b.go\nY -> b.go\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d:\n%s\nwant:\n%s", code, out, want)
	}
	_, out, _ = runTool("-tests", dir)
	if !strings.Contains(out, "w -> a.go\nhelper -> a_test.go\nT -> b.go\n") {
		t.Fatalf("-tests:\n%s", out)
	}
}

func TestBaseListsWhatMoved(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	pkg := filepath.Join(repo, "p")
	writeFiles(t, pkg, map[string]string{
		"a.go": "package p\n\nfunc F() {}\n\nfunc G() {}\n\nfunc init() {}\n",
		"b.go": "package p\n\nfunc init() {}\n",
	})
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
	writeFiles(t, pkg, map[string]string{
		"a.go": "package p\n\nfunc G() {}\n",
		"b.go": "package p\n\nfunc init() {}\n\nfunc init() {}\n",
		"c.go": "package p\n\nfunc F() {}\n\nfunc H() {}\n",
	})
	code, out, errs := runTool("-base", "HEAD", pkg)
	want := "init: a.go, b.go -> b.go, b.go\nF: a.go -> c.go\nH: (none) -> c.go\nmovecheck: 3 of 4 declarations changed file\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d (%s):\n%s\nwant:\n%s", code, errs, out, want)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "head")
	if _, out2, _ := runTool("-base", "HEAD~1", "-head", "HEAD", pkg); out2 != want {
		t.Fatalf("-head HEAD:\n%s", out2)
	}
	if _, out3, _ := runTool("-head", "HEAD~1", pkg); !strings.HasPrefix(out3, "F -> a.go\nG -> a.go\n") {
		t.Fatalf("map at a ref:\n%s", out3)
	}
}

func TestFlattenInlinesStages(t *testing.T) {
	code, out, errs := runTool("-flatten", "main", "testdata/stages/ok")
	want := `a := &app{}
a.name = "demo"
cleanup := func() { println("stopped") }
stop := cleanup
defer stop()
a.db = a.name + ".db"
go func() {
	defer println("background done")
	for i := 0; i < 3; i++ {
		if i == 2 {
			return
		}
	}
}()
a.ready = true
if a.ready {
	println("serving", a.db)
}
a.done = true
`
	if code != 0 || out != want {
		t.Fatalf("exit %d (%s):\n%s\nwant:\n%s", code, errs, out, want)
	}
}

func TestFlattenFailsOnDeferRecoverAndEarlyReturn(t *testing.T) {
	code, out, errs := runTool("-flatten", "main", "testdata/stages/defer")
	if code != 1 || out != "" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	for _, want := range []string{
		"wire_open.go:12: stage (*app).open contains defer",
		"wire_open.go:19: stage (*app).check contains an early return",
		"wire_open.go:26: stage (*app).guard contains recover",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}
}

func TestFlattenRefusesStageCallsItCannotInline(t *testing.T) {
	stages := "package main\n\ntype app struct{ n int }\n\nfunc (a *app) one() int { return 1 }\n\nfunc (a *app) arg(n int) { a.n = n }\n\nfunc (a *app) none() {}\n"
	cases := map[string]string{
		"deferred":       "defer a.none()",
		"in a goroutine": "go func() { a.none() }()",
		"in a condition": "if a.one() > 0 {\n\t}",
		"with arguments": "a.arg(1)",
		"no result":      "x := a.none()\n\t_ = x",
	}
	for name, stmt := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := flattenOf(t, map[string]string{
				"main.go":     "package main\n\nfunc main() {\n\ta := &app{}\n\t" + stmt + "\n}\n",
				"wire_app.go": stages,
			})
			if err == nil {
				t.Fatal("flatten accepted it")
			}
		})
	}
	stmts, err := flattenOf(t, map[string]string{
		"main.go":     "package main\n\nfunc main() {\n\ta := &app{}\n\ta.one()\n\tswitch {\n\tcase a.n > 0:\n\t\tn := a.one()\n\t\t_ = n\n\t}\n}\n",
		"wire_app.go": stages,
	})
	got := strings.Join(stmts, "\n")
	want := "a := &app{}\n_ = 1\nswitch {\ncase a.n > 0:\n\tn := 1\n\t_ = n\n}"
	if err != nil || got != want {
		t.Fatalf("discarded result and a call in a case clause (%v):\n%s\nwant:\n%s", err, got, want)
	}
}

func TestFlattenDiffAgainstBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	pkg := filepath.Join(repo, "cmd")
	writeFiles(t, pkg, map[string]string{
		"main.go": "package main\n\nfunc main() {\n\tname := \"demo\"\n\tprintln(name)\n\tprintln(\"serving\")\n}\n",
	})
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
	if code, out, _ := runTool("-flatten", "main", "-base", "HEAD", pkg); code != 0 ||
		out != "movecheck: main flattens to the same statements in HEAD and the working tree\n" {
		t.Fatalf("unchanged: exit %d\n%s", code, out)
	}
	writeFiles(t, pkg, map[string]string{
		"main.go":      "package main\n\nfunc main() {\n\ta := &app{}\n\ta.config()\n\tprintln(\"serving\")\n}\n",
		"wire_app.go":  "package main\n\ntype app struct{ name string }\n",
		"wire_conf.go": "package main\n\nfunc (a *app) config() {\n\ta.name = \"demo\"\n\tprintln(a.name)\n}\n",
	})
	code, out, errs := runTool("-flatten", "main", "-base", "HEAD", pkg)
	want := "--- HEAD\n+++ the working tree\n@@ -1,3 +1,4 @@\n-name := \"demo\"\n-println(name)\n+a := &app{}\n+a.name = \"demo\"\n+println(a.name)\n println(\"serving\")\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d (%s):\n%s\nwant:\n%s", code, errs, out, want)
	}
}

func TestUnifiedDiffHunks(t *testing.T) {
	a := strings.Split("1 2 3 4 5 6 7 8 9 10 11 12 13 14 15", " ")
	b := strings.Split("1 2 3 4 5 six 7 8 9 10 11 12 13 14 15 16", " ")
	got := strings.Join(unifiedDiff(a, b, "a", "b"), "\n")
	want := "--- a\n+++ b\n@@ -3,7 +3,7 @@\n 3\n 4\n 5\n-6\n+six\n 7\n 8\n 9\n@@ -13,3 +13,4 @@\n 13\n 14\n 15\n+16"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if unifiedDiff(a, a, "a", "b") != nil {
		t.Fatal("equal inputs differ")
	}
}

func TestUsage(t *testing.T) {
	if code, _, _ := runTool(); code != 2 {
		t.Fatalf("no package: exit %d", code)
	}
	if code, _, errs := runTool("-flatten", "nope", "testdata/stages/ok"); code != 1 || !strings.Contains(errs, "no function nope") {
		t.Fatalf("unknown function: exit %d: %s", code, errs)
	}
}
