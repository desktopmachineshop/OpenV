package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRefusals: each shape splittools could not move whole, each spec it
// could not honour, and each budget it would break is refused with a
// message that says why, marked errPlan (the spec's to settle, so
// TestM11aOnTheWorkingTree skips on it), and nothing is written.
func TestRefusals(t *testing.T) {
	type specEdit func(*spec)
	ctor := func(sp *spec, fn string) *constructor {
		for i := range sp.Constructors {
			if sp.Constructors[i].Func == fn {
				return &sp.Constructors[i]
			}
		}
		t.Fatalf("no constructor %s in the fixture spec", fn)
		return nil
	}
	cases := []struct {
		name  string
		edits []func(string) string
		spec  specEdit
		opts  func(*options)
		setup func(dir string)
		want  string
	}{
		{name: "constructors out of the table's order",
			spec: func(sp *spec) {
				sp.Constructors[1], sp.Constructors[2] = sp.Constructors[2], sp.Constructors[1]
			},
			want: `Tools() lists "delta" (constructor deltaTools) right after "gamma" (constructor middleTools), but the spec puts deltaTools first`},
		{name: "a constructor's tools out of order",
			spec: func(sp *spec) { ctor(sp, "firstTools").Tools = []string{"beta", "alpha"} },
			want: `constructor firstTools lists "alpha" after "beta", but Tools() lists it first`},
		{name: "a tool listed twice",
			spec: func(sp *spec) { ctor(sp, "lastTools").Tools = []string{"epsilon", "alpha"} },
			want: `the spec lists "alpha" twice, in firstTools and lastTools`},
		{name: "a constructor over the function budget",
			opts: func(o *options) { o.maxFunc = 17 },
			want: "constructor firstTools would span 18 lines, over K14's 17-line function budget"},
		{name: "a file over the file budget",
			opts: func(o *options) { o.maxFile = 30 },
			want: "tools_first.go would have 47 lines, over K14's 30-line file budget"},
		{name: "a target file that exists",
			setup: func(dir string) {
				_ = os.WriteFile(filepath.Join(dir, "tools_delta.go"), []byte("package fixture\n"), 0o644)
			},
			want: "constructor deltaTools: tools_delta.go already exists; splittools writes new files only"},
		{name: "a constructor named like a declaration",
			spec: func(sp *spec) { ctor(sp, "deltaTools").Func = "prefix" },
			want: "constructor prefix: the package already declares prefix in table.go"},
		{name: "a constructor named like a test helper",
			spec: func(sp *spec) { ctor(sp, "deltaTools").Func = "helperFromTest" },
			want: "the package already declares helperFromTest in table_test.go"},
		{name: "a constructor named like an import",
			spec: func(sp *spec) { ctor(sp, "deltaTools").Func = "url" },
			want: "the package already declares url in table.go (an import)"},
		{name: "a constructor named slices",
			spec: func(sp *spec) { ctor(sp, "deltaTools").Func = "slices" },
			want: "constructor slices would hide package slices"},
		{name: "a package-level slices",
			edits: []func(string) string{func(s string) string { return s + "\nfunc slices() {}\n" }},
			want:  "table.go declares slices, which the concatenation needs"},
		{name: "a function the package does not have",
			spec: func(sp *spec) { sp.Func = "Missing" },
			want: "no production file of the package declares func Missing()"},
		{name: "an entry without a Name",
			edits: []func(string) string{replace(t, `Name: "alpha",`, `Title: "alpha",`)},
			want:  "entry 1 of Tools() has no Name field"},
		{name: "a Name that is not a string literal",
			edits: []func(string) string{replace(t, `Name: "alpha",`, `Name: prefix,`)},
			want:  "entry 1 of Tools() has a Name that is not a string literal"},
		{name: "an unkeyed entry",
			edits: []func(string) string{replace(t, "\t\t{\n\t\t\tName: \"alpha\",", "\t\t{\"zeta\", nil},\n\t\t{\n\t\t\tName: \"alpha\",")},
			want:  "entry 1 of Tools() has an unkeyed field"},
		{name: "an entry that is not a literal",
			edits: []func(string) string{replace(t, "\t\t{\n\t\t\tName: \"alpha\",", "\t\textra,\n\t\t{\n\t\t\tName: \"alpha\",")},
			want:  "entry 1 of Tools() is not a Tool{...} literal"},
		{name: "two entries on one line",
			edits: []func(string) string{replace(t, "\t\t{\n\t\t\tName: \"alpha\",", "\t\t{Name: \"x\"}, {Name: \"y\"},\n\t\t{\n\t\t\tName: \"alpha\",")},
			want:  `entry "x" shares its last line with other code`},
		{name: "an entry that does not end its line",
			edits: []func(string) string{replace(t, "\t\t\t},\n\t\t},\n\t}\n}\n\n// Names", "\t\t\t},\n\t\t}}\n}\n\n// Names")},
			want:  `entry "epsilon" must end its line with its comma`},
		{name: "a comment after the last entry",
			edits: []func(string) string{replace(t, "\t\t},\n\t}\n}\n\n// Names", "\t\t},\n\t\t// trailing\n\t}\n}\n\n// Names")},
			want:  "a comment after the last entry of Tools() has no entry to travel with"},
		{name: "a comment outside the literal",
			edits: []func(string) string{replace(t, "\treturn []Tool{\n", "\t// the table\n\treturn []Tool{\n")},
			want:  "a comment in Tools() outside its []Tool literal would be lost"},
		{name: "a comment on the opening brace's line",
			edits: []func(string) string{replace(t, "\treturn []Tool{\n", "\treturn []Tool{ // the table\n")},
			want:  "the []Tool literal's opening brace must end its line"},
		{name: "a name the table lists twice",
			edits: []func(string) string{replace(t, `Name: "delta",`, `Name: "gamma",`)},
			want:  `Tools() lists "gamma" twice`},
		{name: "a dot import",
			edits: []func(string) string{replace(t, "\t_ \"embed\"\n", "\t_ \"embed\"\n\t. \"math\"\n")},
			want:  "table.go has a dot import"},
		{name: "a build-constrained table",
			edits: []func(string) string{func(s string) string { return "//go:build linux\n\n" + s }},
			want:  "table.go is built only under linux; the new files would be built everywhere"},
		{name: "a function that does not return a literal",
			edits: []func(string) string{replace(t, "\treturn []Tool{\n", "\treturn nil\n\treturn []Tool{\n")},
			want:  "Tools() must hold one statement, return []Tool{...}"},
		{name: "a function that takes arguments",
			edits: []func(string) string{replace(t, "func Tools() []Tool {", "func Tools(n int) []Tool {")},
			want:  "Tools() must take no arguments and return one []T"},
		{name: "an empty table",
			edits: []func(string) string{func(s string) string {
				i, j := strings.Index(s, "\treturn []Tool{\n"), strings.Index(s, "\n}\n\n// Names")
				return s[:i] + "\treturn []Tool{\n\t}" + s[j:]
			}},
			want: "Tools() returns an empty table"},
		{name: "a spec that places nothing",
			spec: func(sp *spec) {
				for i := range sp.Constructors {
					sp.Constructors[i].Tools = []string{"retired" + sp.Constructors[i].Func}
				}
			},
			want: "the spec places none of Tools()'s 5 entries"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fixtureModule(t, c.edits...)
			if c.setup != nil {
				c.setup(dir)
			}
			sp := fixtureSpec(t)
			if c.spec != nil {
				c.spec(sp)
			}
			opts := budgets()
			if c.opts != nil {
				c.opts(&opts)
			}
			before := snapshot(t, dir)
			_, err := generate(dir, sp, opts)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v\nwant it to say %q", err, c.want)
			}
			if !errors.Is(err, errPlan) {
				t.Errorf("the refusal is not marked errPlan: %v", err)
			}
			after := snapshot(t, dir)
			if strings.Join(sortedKeys(after), " ") != strings.Join(sortedKeys(before), " ") || after["table.go"] != before["table.go"] {
				t.Error("a refused split wrote files")
			}
		})
	}
}

// TestSpecValidation: what a spec says on its own is checked when it loads.
func TestSpecValidation(t *testing.T) {
	good := `{"package": "fixture", "constructors": [{"func": "firstTools", "file": "tools_first.go", "tools": ["alpha"]}]}`
	cases := []struct{ name, json, want string }{
		{"no constructors", `{"package": "fixture", "constructors": []}`, "at least one constructor"},
		{"no package", `{"constructors": [{"func": "a", "file": "a.go", "tools": ["x"]}]}`, "needs a package"},
		{"an unknown field", strings.Replace(good, `"package"`, `"pkg": 1, "package"`, 1), `unknown field "pkg"`},
		{"an exported constructor", strings.Replace(good, "firstTools", "FirstTools", 1), "a constructor is unexported"},
		{"a predeclared name", strings.Replace(good, "firstTools", "len", 1), "reserved or predeclared"},
		{"init", strings.Replace(good, "firstTools", "init", 1), "reserved or predeclared"},
		{"not an identifier", strings.Replace(good, "firstTools", "first-tools", 1), "not a Go identifier"},
		{"no tools", strings.Replace(good, `["alpha"]`, `[]`, 1), "lists no tools"},
		{"a test file", strings.Replace(good, "tools_first.go", "tools_first_test.go", 1), "is a test file"},
		{"a GOOS file", strings.Replace(good, "tools_first.go", "tools_linux.go", 1), "is built only for linux"},
		{"a GOARCH file", strings.Replace(good, "tools_first.go", "tools_arm64.go", 1), "is built only for arm64"},
		{"a path", strings.Replace(good, "tools_first.go", "sub/tools.go", 1), "is not a file name"},
		{"not Go", strings.Replace(good, "tools_first.go", "tools_first.txt", 1), "is not a Go file"},
		{"a doc that is not a comment", strings.Replace(good, `"tools":`, `"doc": "firstTools returns", "tools":`, 1), "must be // comment lines"},
		{"a header that is not a comment", strings.Replace(good, `"constructors"`, `"headers": {"tools_first.go": "package x"}, "constructors"`, 1), "must be // comment lines"},
		{"a header for no constructor's file", strings.Replace(good, `"constructors"`, `"headers": {"nope.go": "// x"}, "constructors"`, 1), "nope.go is not the file of any constructor"},
		{"a constructor twice", strings.Replace(good, `["alpha"]}]}`, `["alpha"]}, {"func": "firstTools", "file": "b.go", "tools": ["b"]}]}`, 1), "constructor firstTools is declared twice"},
		{"a func that is not an identifier", strings.Replace(good, `"package"`, `"func": "a.b", "package"`, 1), `func "a.b" is not a Go identifier`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "spec.json")
			writeFile(t, p, c.json)
			_, err := loadSpec(p)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v\nwant it to say %q", err, c.want)
			}
		})
	}
	p := filepath.Join(t.TempDir(), "spec.json")
	writeFile(t, p, good)
	if _, err := loadSpec(p); err != nil {
		t.Fatalf("a good spec: %v", err)
	}
}

// TestSpecDrift: the table has moved on since the spec was written. A tool
// the spec does not name joins its neighbour's constructor, a name the table
// no longer has is left out, a constructor left empty is not written, each
// with a note, and the split is still faithful.
func TestSpecDrift(t *testing.T) {
	newEntry := func(name string) string {
		return "\t\t{\n\t\t\tName: \"" + name + "\",\n\t\t\tHandler: func(args map[string]string) (string, error) {\n" +
			"\t\t\t\treturn \"" + name + "\", nil\n\t\t\t},\n\t\t},\n"
	}
	dir := fixtureModule(t,
		replace(t, "\t\t{\n\t\t\tName: \"alpha\",", newEntry("first")+"\t\t{\n\t\t\tName: \"alpha\","),
		replace(t, "\t\t},\n\t}\n}\n\n// Names", "\t\t},\n"+newEntry("zeta")+"\t}\n}\n\n// Names"))
	sp := fixtureSpec(t)
	sp.Constructors[2].Tools = []string{"retired"} // deltaTools: delta is no longer named
	res, err := generate(dir, sp, options{maxFunc: funcBudget, maxFile: fileBudget, vet: goVet})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(res.lines, "\n")
	for _, want := range []string{
		`note: the spec names "retired" (in deltaTools), which Tools() does not list: left out`,
		`note: "first" is not in the spec: it joins firstTools, before "alpha"`,
		`note: "delta" is not in the spec: it joins middleTools, after "gamma"`,
		`note: "zeta" is not in the spec: it joins lastTools, after "epsilon"`,
		"note: constructor deltaTools is left with no tool: not written",
		"first -> firstTools (tools_first.go)", "delta -> middleTools (tools_first.go)", "zeta -> lastTools (tools_last.go)",
		"7 entries of fixture's Tools() into 3 constructors in 2 new files",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report lacks %q:\n%s", want, joined)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "tools_delta.go")); !os.IsNotExist(err) {
		t.Error("the empty constructor's file was written")
	}
	if got := readFile(t, filepath.Join(dir, "table.go")); !strings.Contains(got, "\treturn slices.Concat(\n\t\tfirstTools(),\n\t\tmiddleTools(),\n\t\tlastTools(),\n\t)\n") {
		t.Errorf("table.go's Tools():\n%s", got)
	}
}

// TestOneConstructor: a spec with one constructor returns it as it is, with
// no slices import.
func TestOneConstructor(t *testing.T) {
	dir := fixtureModule(t)
	sp := &spec{Package: "fixture", Constructors: []constructor{{Func: "allTools", File: "tools_all.go",
		Tools: []string{"alpha", "beta", "gamma", "delta", "epsilon"}}}}
	if _, err := generate(dir, sp, options{maxFunc: funcBudget, maxFile: fileBudget, vet: goVet}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, "table.go"))
	if !strings.Contains(got, "func Tools() []Tool {\n\treturn allTools()\n}\n") || strings.Contains(got, `"slices"`) {
		t.Errorf("table.go:\n%s", got)
	}
	// The blank line between beta and gamma stays, and counts toward the
	// span as internal/archtest measures it.
	all := readFile(t, filepath.Join(dir, "tools_all.go"))
	if !strings.Contains(all, "\t\t}, // beta ends here\n\n\t\t{\n\t\t\tName: \"gamma\",") {
		t.Errorf("tools_all.go lost the blank line between beta and gamma:\n%s", all)
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "tools_all.go", all, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := af.Decls[len(af.Decls)-1].(*ast.FuncDecl)
	if n := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1; n != 47 {
		t.Errorf("allTools spans %d lines, want 47: 4, the entries' 42 and the blank line between beta and gamma", n)
	}
}

// TestImportShapes: the table's file gets package slices, and keeps what it
// still uses, whatever its import declarations look like; each result vets.
func TestImportShapes(t *testing.T) {
	const table = "\n// Tool is an entry.\ntype Tool struct{ Name string }\n\n" +
		"// Tools returns the table.\nfunc Tools() []Tool {\n\treturn []Tool{\n" +
		"\t\t{\n\t\t\tName: \"a\",\n\t\t},\n\t\t{\n\t\t\tName: \"b\",\n\t\t},\n\t}\n}\n"
	cases := []struct {
		name, imports, uses, want string
	}{
		{"no imports", "", "", "package p\n\nimport \"slices\"\n\n// Tool is an entry."},
		{"one standard import", "import \"fmt\"\n", "var _ = fmt.Sprint\n",
			"package p\n\nimport (\n\t\"fmt\"\n\t\"slices\"\n)\n"},
		{"one module import", "import \"example.com/dep\"\n", "var _ = dep.X\n",
			"package p\n\nimport (\n\t\"slices\"\n\n\t\"example.com/dep\"\n)\n"},
		{"a group of module imports", "import (\n\t\"example.com/dep\"\n)\n", "var _ = dep.X\n",
			"package p\n\nimport (\n\t\"slices\"\n\n\t\"example.com/dep\"\n)\n"},
		{"slices already there", "import (\n\t\"fmt\"\n\t\"slices\"\n)\n", "var _ = fmt.Sprint\nvar _ = slices.Max[[]int]\n",
			"package p\n\nimport (\n\t\"fmt\"\n\t\"slices\"\n)\n"},
		{"slices under another name", "import sl \"slices\"\n", "var _ = sl.Max[[]int]\n",
			"package p\n\nimport sl \"slices\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module example.com\n\ngo 1.25\n")
			for _, d := range []string{"p", "dep"} {
				if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n\n// X is used.\nvar X = 1\n")
			src := "package p\n"
			if c.imports != "" {
				src += "\n" + c.imports
			}
			writeFile(t, filepath.Join(root, "p", "p.go"), src+table+"\n"+c.uses)
			sp := &spec{Package: "p", Constructors: []constructor{
				{Func: "aTools", File: "tools_a.go", Tools: []string{"a"}},
				{Func: "bTools", File: "tools_b.go", Tools: []string{"b"}}}}
			if _, err := generate(filepath.Join(root, "p"), sp, options{maxFunc: funcBudget, maxFile: fileBudget, vet: goVet}); err != nil {
				t.Fatal(err)
			}
			got := readFile(t, filepath.Join(root, "p", "p.go"))
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("p.go:\n%s\nwant it to start with:\n%s", got, c.want)
			}
			if strings.Count(got, `"slices"`) != 1 {
				t.Errorf("p.go imports slices %d times", strings.Count(got, `"slices"`))
			}
			concat := "slices.Concat("
			if strings.Contains(c.imports, "sl \"slices\"") {
				concat = "sl.Concat("
			}
			if !strings.Contains(got, "\treturn "+concat+"\n\t\taTools(),\n\t\tbTools(),\n\t)\n") {
				t.Errorf("p.go's Tools():\n%s", got)
			}
		})
	}
}

// TestBudgetsAreArchtests: the budgets splittools plans with are the ones
// internal/archtest enforces, read from its source, so a constructor the
// generator accepts is one K14 accepts.
func TestBudgetsAreArchtests(t *testing.T) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "../../archtest/size_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"funcBudget": funcBudget, "fileBudget": fileBudget}
	found := 0
	ast.Inspect(af, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, id := range vs.Names {
			if w, ok := want[id.Name]; ok && i < len(vs.Values) {
				found++
				if lit, ok := vs.Values[i].(*ast.BasicLit); !ok || lit.Value != strconv.Itoa(w) {
					t.Errorf("internal/archtest's %s is %s; splittools plans with %d", id.Name, printNode(fset, &goFile{ast: af}, vs.Values[i]), w)
				}
			}
		}
		return true
	})
	if found != len(want) {
		t.Fatalf("found %d of internal/archtest's budgets %v in size_test.go", found, want)
	}
}
