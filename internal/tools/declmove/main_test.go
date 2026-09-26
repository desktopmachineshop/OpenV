package main

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// copyFixture copies testdata/fixture into a fresh module and returns the
// package directory.
func copyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "fixture")
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
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	srcs, err := readDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, s := range srcs {
		out[s.name] = string(s.src)
	}
	return out
}

func fixtureSpec(t *testing.T) *spec {
	t.Helper()
	sp, err := loadSpec("testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// declhashTool builds the real declhash command, so the tests prove the
// move with the tool a reviewer runs, not only with declmove's own check.
func declhashTool(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "declhash")
	if out, err := exec.Command("go", "build", "-o", bin, "../declhash").CombinedOutput(); err != nil {
		t.Fatalf("building declhash: %v\n%s", err, out)
	}
	return bin
}

func goCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// proveMove applies the fixture spec with the given goimports step and
// checks the result: declhash equal before and after, the expected files,
// gofmt-clean output, and a module that still builds and passes its test.
func proveMove(t *testing.T, goimports func(string, []string) error) map[string]string {
	dir := copyFixture(t)
	declhash := declhashTool(t)
	manifest := func(name string) string {
		path := filepath.Join(t.TempDir(), name)
		if out, err := exec.Command(declhash, "-o", path, dir).CombinedOutput(); err != nil {
			t.Fatalf("declhash: %v\n%s", err, out)
		}
		return path
	}
	before := manifest("before.txt")

	res, err := apply(dir, fixtureSpec(t), options{goimports: goimports})
	if err != nil {
		t.Fatal(err)
	}
	after := manifest("after.txt")
	if out, err := exec.Command(declhash, "-compare", before, after).CombinedOutput(); err != nil {
		t.Fatalf("declhash -compare after the move: %v\n%s", err, out)
	}
	wantLines := []string{
		"(*Server).Handle: server.go -> handlers.go", "render: server.go -> handlers.go",
		"Mode: server.go -> modes.go", "ModeOff: server.go -> modes.go", "ModeOn: server.go -> modes.go",
		"ModeLoud: server.go -> modes.go", "helper: server.go -> small.go", "errEmpty: server.go -> small.go",
		"helperSorted: extra.go -> small.go",
	}
	if strings.Join(res.lines, "\n") != strings.Join(wantLines, "\n") {
		t.Errorf("lines:\n%s", strings.Join(res.lines, "\n"))
	}
	if strings.Join(res.deleted, ",") != "extra.go" || res.targets != 3 || res.label != "fixture" {
		t.Errorf("deleted %v, %d targets, label %q", res.deleted, res.targets, res.label)
	}
	files := readFiles(t, dir)
	for name, src := range files {
		if formatted, err := format.Source([]byte(src)); err != nil || !bytes.Equal(formatted, []byte(src)) {
			t.Errorf("%s is not gofmt-clean (%v)", name, err)
		}
	}
	goCmd(t, dir, "vet", ".")
	goCmd(t, dir, "test", ".")
	return files
}

func TestMoveWithoutGoimports(t *testing.T) {
	files := proveMove(t, nil)
	h := files["handlers.go"]
	if !strings.HasPrefix(h, "// Request handling, moved out of server.go.\n\npackage fixture\n") {
		t.Errorf("handlers.go does not start with its header and package clause:\n%s", h)
	}
	if strings.Index(h, "func (s *Server) Handle") > strings.Index(h, "func render") {
		t.Error("handlers.go does not keep spec order")
	}
	for _, want := range []string{"htmltemplate \"html/template\"", "\"strings\"", "// escaped", "// Handle answers one request."} {
		if !strings.Contains(h, want) {
			t.Errorf("handlers.go lacks %s:\n%s", want, h)
		}
	}
	s := files["server.go"]
	for _, gone := range []string{"errors", "strings", "html/template", "func render", "ModeLoud"} {
		if strings.Contains(s, gone) {
			t.Errorf("server.go still holds %s:\n%s", gone, s)
		}
	}
	if !strings.Contains(s, "\"fmt\"") || !strings.Contains(s, "// Package fixture") {
		t.Errorf("server.go lost what stays:\n%s", s)
	}
	if want := "import (\n\t\"errors\"\n\t\"fmt\"\n\t\"sort\"\n)\n"; !strings.Contains(files["small.go"], want) {
		t.Errorf("small.go's imports are not one block:\n%s", files["small.go"])
	}
	if _, ok := files["extra.go"]; ok {
		t.Error("extra.go was left empty but not deleted")
	}
}

func TestMoveWithPinnedGoimports(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: go run " + goimportsPkg + " may download it")
	}
	probe := filepath.Join(t.TempDir(), "probe.go")
	writeFile(t, probe, "package probe\n")
	if err := runGoimports(filepath.Dir(probe), []string{probe}); err != nil {
		t.Skipf("goimports is unavailable here (%v); TestMoveWithoutGoimports covers the move itself", err)
	}
	proveMove(t, runGoimports)
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := copyFixture(t)
	before := readFiles(t, dir)
	res, err := apply(dir, fixtureSpec(t), options{dryRun: true})
	if err != nil || len(res.lines) != 9 {
		t.Fatalf("dry run: %v, %d lines", err, len(res.lines))
	}
	if after := readFiles(t, dir); !sameFiles(before, after) {
		t.Fatal("a dry run changed files")
	}
}

func sameFiles(a, b map[string]string) bool {
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

func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		targets []target
		tests   bool
		want    string
	}{
		{"missing", []target{{File: "x.go", Decls: []string{"nope"}}}, false, "nope: no such declaration"},
		{"ambiguous", []target{{File: "x.go", Decls: []string{"init"}}}, false, "init: ambiguous, pick one of init#1 (server.go:"},
		{"out of range", []target{{File: "x.go", Decls: []string{"init#3"}}}, false, "only 2 declarations are named init"},
		{"part of a group", []target{{File: "x.go", Decls: []string{"ModeOff", "ModeOn"}}}, false,
			"ModeLoud: declared together with ModeOff (server.go:15); list it for x.go too"},
		{"group split", []target{{File: "x.go", Decls: []string{"ModeOff"}}, {File: "y.go", Decls: []string{"ModeOn", "ModeLoud"}}}, false,
			"ModeOn: declared together with ModeOff (server.go:15), which goes to x.go"},
		{"twice", []target{{File: "x.go", Decls: []string{"render"}}, {File: "y.go", Decls: []string{"render"}}}, false,
			"render: listed twice (for x.go and y.go)"},
		{"already there", []target{{File: "small.go", Decls: []string{"Sorted"}}}, false, "Sorted: already in small.go"},
		{"path", []target{{File: "sub/x.go", Decls: []string{"render"}}}, false, "a target is a .go file name"},
		{"not go", []target{{File: "x.txt", Decls: []string{"render"}}}, false, "a target is a .go file name"},
		{"test target", []target{{File: "x_test.go", Decls: []string{"render"}}}, false, "moves into non-test files only"},
		{"header", []target{{File: "x.go", Header: "package oops", Decls: []string{"render"}}}, false, "the header must be // comment lines"},
		{"test set", []target{{File: "x_test.go", Decls: []string{"render"}}}, true, "render: no such declaration"},
	}
	dir := copyFixture(t)
	before := readFiles(t, dir)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := apply(dir, &spec{Package: "fixture", Tests: c.tests, Targets: c.targets}, options{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
	if !sameFiles(before, readFiles(t, dir)) {
		t.Fatal("a refused spec changed files")
	}
}

func TestPicksADuplicateByNumberAndMovesTests(t *testing.T) {
	dir := copyFixture(t)
	sp := &spec{Package: "fixture", Targets: []target{{File: "boot.go", Decls: []string{"init#2"}}}}
	if _, err := apply(dir, sp, options{}); err != nil {
		t.Fatal(err)
	}
	files := readFiles(t, dir)
	if !strings.Contains(files["boot.go"], `"small"`) || strings.Contains(files["small.go"], "func init") {
		t.Fatalf("init#2 is small.go's init; boot.go:\n%s", files["boot.go"])
	}
	sp = &spec{Package: "fixture", Tests: true, Targets: []target{{File: "handle_test.go", Decls: []string{"TestHandle"}}}}
	if _, err := apply(dir, sp, options{}); err != nil {
		t.Fatal(err)
	}
	files = readFiles(t, dir)
	if _, ok := files["server_test.go"]; ok || !strings.Contains(files["handle_test.go"], "import \"testing\"") {
		t.Fatalf("moving the test: %v", keys(files))
	}
	goCmd(t, dir, "test", ".")
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestRestoresWhenTheMoveIsNotPure(t *testing.T) {
	dir := copyFixture(t)
	// A file-name build constraint: moving into it changes what builds
	// contain the function, so declhash differs and the move is undone.
	writeFile(t, filepath.Join(dir, "only_linux.go"), "package fixture\n\nfunc linuxOnly() {}\n")
	before := readFiles(t, dir)
	sp := &spec{Package: "fixture", Targets: []target{
		{File: "new.go", Decls: []string{"render"}},
		{File: "only_linux.go", Decls: []string{"helper"}},
	}}
	_, err := apply(dir, sp, options{})
	if err == nil || !strings.Contains(err.Error(), "not a pure move") || !strings.Contains(err.Error(), "every file is restored") {
		t.Fatalf("got %v", err)
	}
	if !sameFiles(before, readFiles(t, dir)) {
		t.Fatal("files were not restored")
	}
}

func TestRefusesAnImportClash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package p\n\nimport \"html/template\"\n\nfunc Esc(s string) string { return template.HTMLEscapeString(s) }\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package p\n\nimport \"text/template\"\n\nvar T = template.New(\"t\")\n")
	before := readFiles(t, dir)
	_, err := apply(dir, &spec{Package: "p", Targets: []target{{File: "b.go", Decls: []string{"Esc"}}}}, options{})
	if err == nil || !strings.Contains(err.Error(), "imports text/template as template, but the code moving in names html/template so") {
		t.Fatalf("got %v", err)
	}
	if !sameFiles(before, readFiles(t, dir)) {
		t.Fatal("a refused move changed files")
	}
}

func TestMovesEmbedDirectivesWithTheirImport(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/p\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "data.txt"), "hello\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package p\n\nimport _ \"embed\"\n\n// data is embedded.\n//\n//go:embed data.txt\nvar data string\n\nfunc Keep() {}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package p\n\nfunc Data() string { return data }\n")
	if _, err := apply(dir, &spec{Package: "p", Targets: []target{{File: "b.go", Decls: []string{"data"}}}}, options{}); err != nil {
		t.Fatal(err)
	}
	if b := readFiles(t, dir)["b.go"]; !strings.Contains(b, "import _ \"embed\"") || !strings.Contains(b, "//go:embed data.txt") {
		t.Fatalf("b.go:\n%s", b)
	}
	goCmd(t, dir, "build", ".")
}

func TestSpecsLoad(t *testing.T) {
	paths, err := filepath.Glob("specs/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no specs (%v)", err)
	}
	for _, p := range paths {
		sp, err := loadSpec(p)
		if err != nil {
			t.Error(err)
			continue
		}
		for _, tg := range sp.Targets {
			if err := checkTarget(&pkgInfo{}, tg, map[bool]string{true: "test"}[sp.Tests]); err != nil || len(tg.Decls) == 0 {
				t.Errorf("%s: %s: %v (%d decls)", p, tg.File, err, len(tg.Decls))
			}
		}
	}
	if _, err := loadSpec("testdata/fixture.json"); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	writeFile(t, bad, `{"package": "p", "targets": [{"file": "x.go", "declz": []}]}`)
	if _, err := loadSpec(bad); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("an unknown field: %v", err)
	}
}

func TestCommandLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("no spec: exit %d", code)
	}
	dir := copyFixture(t)
	specPath := filepath.Join(t.TempDir(), "spec.json")
	writeFile(t, specPath, `{"package": "`+filepath.ToSlash(dir)+`", "targets": [{"file": "esc.go", "decls": ["render"]}]}`)
	stdout.Reset()
	if code := run([]string{"-goimports=false", "-spec", specPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if want := "render: server.go -> esc.go\ndeclmove: moved 1 declarations into 1 files; the declhash manifest of fixture is unchanged (17 declarations)\n"; stdout.String() != want {
		t.Fatalf("output:\n%s", stdout.String())
	}
}
