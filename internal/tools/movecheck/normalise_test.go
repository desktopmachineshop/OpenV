package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// normaliseRepo commits testdata/normalise/base as the package p of a new
// git repository, then puts the head side in its place: testdata's
// faithful split with edits applied (file -> old, new; an empty old
// appends new; the file base.go edits the base's main.go before it is
// committed). It returns the package directory.
func normaliseRepo(t *testing.T, edits map[string][][2]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	pkg := filepath.Join(repo, "p")
	base := readTestdata(t, "normalise/base/main.go")
	for _, e := range edits["base.go"] {
		if !strings.Contains(base, e[0]) {
			t.Fatalf("the base does not contain %q", e[0])
		}
		base = strings.Replace(base, e[0], e[1], 1)
	}
	writeFiles(t, pkg, map[string]string{"main.go": base})
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false",
			"-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	head := map[string]string{}
	for _, name := range []string{"main.go", "app.go", "wire_start.go", "wire_serve.go"} {
		head[name] = readTestdata(t, "normalise/faithful/"+name)
	}
	for name, list := range edits {
		if name == "base.go" {
			continue
		}
		for _, e := range list {
			src, ok := head[name]
			switch {
			case e[0] == "":
				head[name] = src + e[1]
				continue
			case !ok || !strings.Contains(src, e[0]):
				t.Fatalf("%s does not contain %q", name, e[0])
			}
			head[name] = strings.Replace(src, e[0], e[1], 1)
		}
	}
	writeFiles(t, pkg, head)
	return pkg
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestNormaliseFaithfulSplit: testdata/normalise/faithful, base's main()
// as internal/tools/stageextract splits it (fields, a split err declared
// by each stage, a var of a field with and without a value, a := of a
// field beside a new local, two returned cleanups), flattens to base's
// statements once normalised: exit 0, naming what was undone.
func TestNormaliseFaithfulSplit(t *testing.T) {
	pkg := normaliseRepo(t, nil)
	code, out, errs := runTool("-flatten", "main", "-base", "HEAD", pkg)
	want := "movecheck: main flattens to the same statements in HEAD and the working tree once the stage rewrites are undone " +
		"(7 locals became fields of a, 2 cleanups returned for main to defer)\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d (%s):\n%s\nwant:\n%s", code, errs, out, want)
	}
	// The same split with the base as the head side, and the other way
	// round, is no difference either: normalising is the same on each side.
	if code, out, errs := runTool("-flatten", "main", "-base", "HEAD", "-head", "HEAD", pkg); code != 0 ||
		out != "movecheck: main flattens to the same statements in HEAD and HEAD\n" {
		t.Errorf("base against itself: exit %d (%s)\n%s", code, errs, out)
	}
}

// reg is a type with a pointer method that keeps its receiver.
const reg = "\ntype reg struct{ n int }\n\nvar seen []*reg\n\nfunc (r *reg) add() { seen = append(seen, r) }\n"

// TestNormaliseFailsOnAChange: every change a refactor must not make,
// planted in the faithful split, exits 1 with what differs.
func TestNormaliseFailsOnAChange(t *testing.T) {
	cases := []struct {
		name  string
		edits map[string][][2]string
		out   string // in the diff on standard output
		errs  string // on standard error
	}{
		{"a statement swapped across a stage boundary", map[string][][2]string{
			"wire_start.go": {{"\ta.port = \"8080\"\n", ""}},
			"wire_serve.go": {{"\t// Serve.\n", "\t// Serve.\n\ta.port = \"8080\"\n"}},
		}, "-port := \"8080\"\n db, err := open(dsn)", "flattens to other statements in the working tree than in HEAD"},
		{"a statement dropped", map[string][][2]string{
			"wire_serve.go": {{"\tfmt.Println(\"extra\", extra)\n", ""}},
		}, "-fmt.Println(\"extra\", extra)", "flattens to other statements"},
		{"a statement changed", map[string][][2]string{
			"wire_serve.go": {{"\ta.count = len(a.port)\n", "\ta.count = len(a.dsn)\n"}},
		}, "-count := len(port)\n+count := len(dsn)", "flattens to other statements"},
		{"two stages called in the other order", map[string][][2]string{
			"main.go": {{"\tcloseDB := a.connect()\n\tdefer closeDB()\n\ta.serve()\n", "\ta.serve()\n\tcloseDB := a.connect()\n\tdefer closeDB()\n"}},
		}, "@@", "flattens to other statements"},
		{"a stage called twice", map[string][][2]string{
			"main.go": {{"\tdefer closeDB()\n\ta.serve()\n", "\tdefer closeDB()\n\ta.serve()\n\ta.serve()\n"}},
		}, "", "(*app).serve is called more than once"},
		{"a cleanup deferred later than it was", map[string][][2]string{
			"main.go": {{"\tdefer closeDB()\n\ta.serve()\n", "\ta.serve()\n\tdefer closeDB()\n"}},
		}, "+closeDB := db.Close", "flattens to other statements"},
		{"a cleanup not deferred", map[string][][2]string{
			"main.go": {{"\tdefer closeDB()\n", "\t_ = closeDB\n"}},
		}, "-defer db.Close()", "flattens to other statements"},
		{"a field read as a package-level name of the same text", map[string][][2]string{
			"wire_serve.go": {{"a.label, a.ctx.Err()", "label, a.ctx.Err()"}, {"", "\nvar label = \"package-level\"\n"}},
		}, "", "label names a package-level or predeclared name in the working tree but a local of the function's top level (or an app field) in HEAD"},
		{"a field read where a nested local of its name is in scope", map[string][][2]string{
			"base.go":       {{"\tfmt.Println(\"extra\", extra)\n", "\tif port := \"x\"; port != \"\" {\n\t\tfmt.Println(\"extra\", extra, port)\n\t}\n"}},
			"wire_serve.go": {{"\tfmt.Println(\"extra\", extra)\n", "\tif port := \"x\"; port != \"\" {\n\t\tfmt.Println(\"extra\", extra, a.port)\n\t}\n"}},
		}, "", "port names a local of the function's top level (or an app field) in the working tree but a local of a nested block in HEAD"},
		{"a stage local named like a field", map[string][][2]string{
			"wire_serve.go": {{"\tvar extra int\n", "\tvar extra int\n\tport := a.port\n\t_ = port\n"}},
		}, "", "(*app).serve declares a local port, the name of a field of the stages' receiver"},
		{"a local of two stages that a function literal captures", map[string][][2]string{
			"wire_start.go": {{"\tvar err error\n\ta.db, err = open(a.dsn)\n", "\tvar err error\n\ta.db, err = open(a.dsn)\n\tgo func() { _ = err }()\n"}},
		}, "", "err is declared at the top of 2 of main's bodies, and a function literal captures it in (*app).connect"},
		{"a local of two stages whose address is taken", map[string][][2]string{
			"wire_serve.go": {{"\tvar err error\n", "\tvar err error\n\tpe := &err\n\t_ = pe\n"}},
		}, "", "err is declared at the top of 2 of main's bodies, and its address is taken in (*app).serve"},
		{"main()'s local read after a stage wrote its own", map[string][][2]string{
			"base.go": {{"func main() {\n", "func main() {\n\terr := check(0)\n"},
				{"\tfmt.Println(\"serving\", port, dsn, count, label)\n", "\tfmt.Println(\"serving\", port, dsn, count, label, err)\n"}},
			"main.go": {{"\ta := &app{}\n", "\ta := &app{}\n\terr := check(0)\n"},
				{"\tfmt.Println(\"serving\", a.port, a.dsn, a.count, a.label)\n", "\tfmt.Println(\"serving\", a.port, a.dsn, a.count, a.label, err)\n"}},
		}, "", "main reads its err after (*app).serve wrote its own err: err is declared at the top of both"},
		{"a region main() keeps after a defer writes a local a later stage writes its own of, and the tail reads it", map[string][][2]string{
			"base.go": {{"\tdefer db.Close()\n", "\tdefer db.Close()\n\terr = check(1)\n"},
				{"\tfmt.Println(\"serving\", port, dsn, count, label)\n", "\tfmt.Println(\"serving\", port, dsn, count, label, err)\n"}},
			"main.go": {{"\tdefer closeDB()\n", "\tdefer closeDB()\n\tvar err error\n\terr = check(1)\n"},
				{"\tfmt.Println(\"serving\", a.port, a.dsn, a.count, a.label)\n", "\tfmt.Println(\"serving\", a.port, a.dsn, a.count, a.label, err)\n"}},
		}, "", "main reads its err after (*app).serve wrote its own err"},
		{"a local of two stages whose pointer method is called", map[string][][2]string{
			"base.go": {{"func check(n int) error { return nil }\n", "func check(n int) error { return nil }\n" + reg},
				{"\tdb, err := open(dsn)\n", "\tvar r reg\n\tr = reg{n: 1}\n\tr.add()\n\tdb, err := open(dsn)\n"},
				{"\tfmt.Println(\"extra\", extra)\n", "\tfmt.Println(\"extra\", extra)\n\tr = reg{n: 2}\n\tr.add()\n"}},
			"main.go":       {{"func check(n int) error { return nil }\n", "func check(n int) error { return nil }\n" + reg}},
			"wire_start.go": {{"\tvar err error\n\ta.db, err = open(a.dsn)\n", "\tvar r reg\n\tr = reg{n: 1}\n\tr.add()\n\tvar err error\n\ta.db, err = open(a.dsn)\n"}},
			"wire_serve.go": {{"\tfmt.Println(\"extra\", extra)\n", "\tfmt.Println(\"extra\", extra)\n\tvar r reg\n\tr = reg{n: 2}\n\tr.add()\n"}},
		}, "", "r is declared at the top of 2 of main's bodies, and a method with a pointer receiver takes its address in (*app).connect (the working tree)"},
		{"a field typed otherwise than its local", map[string][][2]string{
			"app.go": {{"\tctx   context.Context\n", "\tctx   interface{ Err() error }\n"}},
		}, "", "ctx is interface{Err() error} in the working tree but context.Context in HEAD"},
		{"a var a stage declares a local with, typed otherwise than the function's", map[string][][2]string{
			"wire_start.go": {{"\tvar err error\n\ta.db, err = open(a.dsn)\n", "\tvar err interface{ Error() string }\n\ta.db, err = open(a.dsn)\n"}},
		}, "", "err is error and interface{Error() string} in the working tree but error in HEAD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := normaliseRepo(t, tc.edits)
			code, out, errs := runTool("-flatten", "main", "-base", "HEAD", pkg)
			if code != 1 || !strings.Contains(out, tc.out) || !strings.Contains(errs, tc.errs) {
				t.Errorf("exit %d, want 1 with %q on stdout and %q on stderr\nstdout:\n%s\nstderr:\n%s", code, tc.out, tc.errs, out, errs)
			}
		})
	}
}

// TestNormaliseBindings: the identifiers' bindings the comparison reads,
// on the faithful split: a field read through the receiver is a top-level
// local, a function literal's own local is nested, a package-level
// function is free.
func TestNormaliseBindings(t *testing.T) {
	pkg := normaliseRepo(t, nil)
	srcs, err := readDir(pkg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := parsePackage(srcs, false)
	if err != nil {
		t.Fatal(err)
	}
	fl, list, err := flattenStmts(p, "main", "a")
	if err != nil {
		t.Fatal(err)
	}
	sd := newSide(fl, list)
	if sd.app == nil || len(sd.fields) != 7 {
		t.Fatalf("the receiver's statement or its type's fields: %v, %v", sd.app, sd.fields)
	}
	if _, err := sd.normalise(sd.fields); err != nil {
		t.Fatal(err)
	}
	got := map[string]binding{}
	for _, b := range sd.bindings() {
		if prev, ok := got[b.name]; ok && prev != b.bind {
			got[b.name] = '?'
			continue
		}
		got[b.name] = b.bind
	}
	for name, want := range map[string]binding{"port": bindTop, "label": bindTop, "err": bindTop, "extra": bindTop,
		"open": bindFree, "check": bindFree, "context": bindFree, "len": bindFree, "v": bindNested} {
		if got[name] != want {
			t.Errorf("%s names %c, want %c", name, got[name], want)
		}
	}
}

// TestReplaceIn: the reflection walk replaces an expression in every kind
// of slot it sits in, and leaves the parser's objects, which point back
// into the tree, alone.
func TestReplaceIn(t *testing.T) {
	src := "package p\n\nfunc f() {\n\tx := a.n + 1\n\tfor i := 0; i < a.n; i++ {\n\t\tdefer println(a.n, x, []int{a.n}, map[int]int{a.n: 1}, -a.n)\n\t}\n}\n"
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := af.Decls[0].(*ast.FuncDecl).Body
	n := 0
	replaceIn(body, func(e ast.Expr) ast.Expr {
		if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == "n" {
			n++
			return &ast.Ident{NamePos: sel.Pos(), Name: "n"}
		}
		return e
	})
	var b strings.Builder
	if err := printer.Fprint(&b, fset, body); err != nil {
		t.Fatal(err)
	}
	if n != 6 || strings.Contains(b.String(), "a.n") {
		t.Errorf("replaced %d, want 6:\n%s", n, b.String())
	}
}

// TestNormaliseFaithfulVariants: other shapes stageextract writes, each a
// faithful split of an edited base, exit 0: a field the base declares with
// var and first writes at the top level, one it reads before writing, and
// a var of a field and a local together.
func TestNormaliseFaithfulVariants(t *testing.T) {
	cases := []struct {
		name  string
		edits map[string][][2]string
	}{
		{"a field declared by var, then written", map[string][][2]string{
			"base.go": {{"\tport := \"8080\"\n", "\tvar port string\n\tport = \"8080\"\n"}},
		}},
		{"a field declared by var, read, then written", map[string][][2]string{
			"base.go":       {{"\tport := \"8080\"\n", "\tvar port string\n\tfmt.Println(port)\n\tport = \"8080\"\n"}},
			"wire_start.go": {{"\ta.port = \"8080\"\n", "\tfmt.Println(a.port)\n\ta.port = \"8080\"\n"}, {"\t\"os\"\n", "\t\"fmt\"\n\t\"os\"\n"}},
		}},
		{"a var of a field and a local", map[string][][2]string{
			"base.go":       {{"\tvar dsn string\n", "\tvar dsn, spare string\n\t_ = spare\n"}},
			"wire_start.go": {{"\t// Connect.\n", "\t// Connect.\n\tvar spare string\n\t_ = spare\n"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := normaliseRepo(t, tc.edits)
			if code, out, errs := runTool("-flatten", "main", "-base", "HEAD", pkg); code != 0 {
				t.Errorf("exit %d, want 0\n%s%s", code, out, errs)
			}
		})
	}
}

// TestNormaliseNeedsTypes: -flatten -base type-checks both sides, against
// the packages they import as they build here, to compare the types of
// the fields and locals the normalisation reads as one; a side that does
// not type-check exits 2, saying so.
func TestNormaliseNeedsTypes(t *testing.T) {
	pkg := normaliseRepo(t, map[string][][2]string{"app.go": {{"\tcount int\n", "\tcount integer\n"}}})
	code, out, errs := runTool("-flatten", "main", "-base", "HEAD", pkg)
	if code != 2 || !strings.Contains(errs, "movecheck: the working tree does not type-check against the packages it imports as they build here") ||
		!strings.Contains(errs, "undefined: integer") {
		t.Errorf("exit %d, want 2 naming the side that does not type-check\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}
}

// TestAddressFileIsShared: movecheck's address.go, what takes a local's
// address, is internal/tools/stageextract's byte for byte, so the proof
// and the generator agree on what a split local may not do.
func TestAddressFileIsShared(t *testing.T) {
	mine := readTestdata(t, "../address.go")
	theirs := readTestdata(t, "../../stageextract/address.go")
	if mine != theirs {
		t.Error("internal/tools/movecheck/address.go differs from internal/tools/stageextract/address.go; edit one and copy it over the other")
	}
}
