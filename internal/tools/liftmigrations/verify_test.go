package main

import (
	"regexp"
	"strings"
	"testing"
)

// tamper is a fault in what the lift writes: the self-check must catch it,
// name it, and leave every file as it was.
type tamper struct {
	name string
	file string // the rendered file it edits
	old  string
	new  string
	want string // a regexp the message must match
}

// TestSelfCheckCatchesEachFault renders the fixture's lift, plants one
// fault in the rendered files, and applies them: the lift must refuse,
// restore the registry's file, remove the files it added, and say what is
// wrong.
func TestSelfCheckCatchesEachFault(t *testing.T) {
	const (
		reg     = "migrations.go"
		widgets = "migration_0002_create_widgets.go"
		labels  = "migration_0003_widget_labels.go"
	)
	cases := []tamper{
		{name: "a raw string reindented", file: widgets, old: "\t\t\tCREATE TABLE widgets (", new: "\t\tCREATE TABLE widgets (",
			want: `the code of migration 0002 create_widgets differs from its literal's, which changes its S3 freeze hash`},
		{name: "a statement dropped", file: widgets, old: "\t_, err := tx.Exec(str.TrimSpace(widgetSQL(2))) // a trailing comment\n", new: "\tvar err error\n",
			want: `the body of migration 0002 create_widgets is not its literal's, token for token`},
		{name: "a comment inside reworded", file: widgets, old: "// The table.", new: "// The tables.",
			want: `line \d+ of the literal has "// The table\." where line \d+ of the function has "// The tables\."`},
		{name: "a trailing comment dropped", file: widgets, old: " // a trailing comment", new: "",
			want: `"// a trailing comment" \(line \d+ of the literal\) is missing|line \d+ of the literal has "// a trailing comment"`},
		{name: "the doc comment dropped", file: widgets, old: "// 0002: a body that uses fmt", new: "// 0002: a body that uses",
			want: `m0002CreateWidgets \(migration_0002_create_widgets.go:\d+\): the comment above it is not, line for line, the comment above migration 0002`},
		{name: "a detached comment attached", file: labels, old: "doc comment.\n\nfunc", new: "doc comment.\nfunc",
			want: `m0003WidgetLabels \(migration_0003_widget_labels.go:\d+\): its comment is its doc comment, where the lift detached it`},
		{name: "two entries swapped", file: reg,
			old:  "\t{Version: 3, Name: \"widget_labels\", Run: m0003WidgetLabels},\n\t{Version: 4, Name: \"no_comment\", Run: m0004NoComment},\n",
			new:  "\t{Version: 4, Name: \"no_comment\", Run: m0004NoComment},\n\t{Version: 3, Name: \"widget_labels\", Run: m0003WidgetLabels},\n",
			want: `holds 0004 no_comment \(Run\), where the lift started from 0003 widget_labels \(Run\): the registry's order is behavior`},
		{name: "an entry renamed", file: reg, old: `Name: "no_comment"`, new: `Name: "no_comments"`,
			want: `holds 0004 no_comments \(Run\), where the lift started from 0004 no_comment`},
		{name: "an entry pointing elsewhere", file: reg, old: "Run: m0004NoComment}", new: "Run: m0003WidgetLabels}",
			want: "migration 0004 no_comment's Run is `m0003WidgetLabels`, not m0004NoComment"},
		{name: "an entry dropped", file: reg, old: "\t{Version: 4, Name: \"no_comment\", Run: m0004NoComment},\n", new: "",
			want: `the registry holds 6 entries, where the lift started from 7`},
		{name: "a kept entry edited", file: reg, old: "Run: m0006AlreadyNamed}", new: "Run: m0006AlreadyNamed, RunDB: nil}",
			want: `migration 0006 already_named was not lifted, but its entry changed`},
		{name: "a kept comment dropped", file: reg, old: "\t// 0006 was lifted before; 0005 was reserved and never used.\n", new: "",
			want: `migrations.go changed beyond the lifted entries: line \d+ of the file with the lift's edits has "// 0006 was lifted before; 0005 was reserved and never used\."`},
		{name: "the file's header changed", file: reg, old: "// This file is liftmigrations' fixture", new: "// This file is the fixture",
			want: `migrations.go changed beyond the lifted entries: line \d+ of the file with the lift's edits has "// This file is liftmigrations' fixture`},
		{name: "an import the runner needs dropped", file: reg, old: "\t\"context\"\n", new: "",
			want: `migrations.go imports \["database/sql" "fmt" "str strings"\], want \["context" "database/sql" "fmt" "str strings"\]`},
		{name: "a second declaration in a migration's file", file: labels, old: "func m0003WidgetLabels", new: "var extra = 1\n\nfunc m0003WidgetLabels",
			want: `migration_0003_widget_labels.go declares more than m0003WidgetLabels`},
		{name: "another declaration changed", file: reg, old: `const lockKey int64 = 42`, new: `const lockKey int64 = 43`,
			want: `lockKey changed`},
		{name: "a doc comment of another declaration changed", file: reg, old: "// apply runs one migration.", new: "// apply runs a migration.",
			want: `apply changed`},
		{name: "an import dropped", file: widgets, old: "\t\"fmt\"\n", new: "",
			want: `migration_0002_create_widgets.go imports \["database/sql" "str strings"\], where its body uses \["database/sql" "fmt" "str strings"\]`},
		{name: "a stray comment", file: labels, old: "\treturn err\n}\n", new: "\treturn err\n}\n\n// stray\n",
			want: `migration_0003_widget_labels.go has comments neither above nor inside m0003WidgetLabels \(migration_0003_widget_labels.go:\d+\)`},
		{name: "another package", file: widgets, old: "package store\n", new: "package other\n",
			want: `migration_0003_widget_labels.go is package store, but migration_0002_create_widgets.go is package other`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, dir := copyFixture(t)
			before := readTree(t, dir)
			p, err := loadPackage(dir)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := planLift(p)
			if err != nil {
				t.Fatal(err)
			}
			contents, err := plan.render()
			if err != nil {
				t.Fatal(err)
			}
			src := string(contents[c.file])
			if !strings.Contains(src, c.old) {
				t.Fatalf("%s has no %q:\n%s", c.file, c.old, src)
			}
			contents[c.file] = []byte(strings.Replace(src, c.old, c.new, 1))
			err = apply(plan, contents, root, false)
			if err == nil {
				t.Fatal("the self-check passed a lift with the fault")
			}
			if !regexp.MustCompile(c.want).MatchString(err.Error()) {
				t.Errorf("message does not match %q:\n%v", c.want, err)
			}
			if !strings.HasSuffix(err.Error(), "every file is restored") {
				t.Errorf("message does not say the files are restored:\n%v", err)
			}
			if !equalTrees(readTree(t, dir), before) {
				t.Error("the files were not restored")
			}
		})
	}
}

// TestSelfCheckCatchesAPlanningFault plants a fault in the plan rather than
// in what it renders: the checks that compare the files with the plan's
// comments agree with it, so only verifyComments, which reads the files
// alone, can say that a comment is lost or out of order.
func TestSelfCheckCatchesAPlanningFault(t *testing.T) {
	const lostOrMoved = `the comments of migrations.go before the lift are not, in order, its comments after it with each lifted migration's own in the place of its entry: `
	cases := []struct {
		name  string
		fault func(t *testing.T, byFile map[string]*lift)
		want  string
	}{
		{name: "the first of two comment groups lost", fault: func(t *testing.T, byFile map[string]*lift) {
			l := byFile["migration_0008_same_name.go"]
			if len(l.doc) < 3 || l.doc[0] != "// Positional entries follow." || l.doc[1] != "//" {
				t.Fatalf("0008's comment is %q; the fixture gives it two groups", l.doc)
			}
			l.doc = l.doc[2:]
		}, want: lostOrMoved + `"// 0008: positional fields; the comment above it is two groups\." \(migration_0008_same_name\.go:\d+\) ` +
			`is where "// Positional entries follow\." \(migrations\.go:\d+ before the lift\) was`},
		{name: "two migrations' comments swapped", fault: func(t *testing.T, byFile map[string]*lift) {
			a, b := byFile["migration_0002_create_widgets.go"], byFile["migration_0003_widget_labels.go"]
			a.doc, b.doc = b.doc, a.doc
		}, want: lostOrMoved + `"// 0003: existing rows read '' \(SQL's empty string\), which gofmt would" \(migration_0002_create_widgets\.go:\d+\) ` +
			`is where "// 0002: a body that uses fmt, an aliased import and a helper, with a" \(migrations\.go:\d+ before the lift\) was`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, dir := copyFixture(t)
			before := readTree(t, dir)
			p, err := loadPackage(dir)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := planLift(p)
			if err != nil {
				t.Fatal(err)
			}
			byFile := map[string]*lift{}
			for _, l := range plan.lifts {
				byFile[l.file] = l
			}
			c.fault(t, byFile)
			contents, err := plan.render()
			if err != nil {
				t.Fatal(err)
			}
			err = apply(plan, contents, root, false)
			if err == nil {
				t.Fatal("the self-check passed a lift with the fault")
			}
			if !regexp.MustCompile(c.want).MatchString(err.Error()) {
				t.Errorf("message does not match %q:\n%v", c.want, err)
			}
			if n := strings.Count(err.Error(), "\n  - "); n != 1 {
				t.Errorf("%d problems, want 1 (verifyComments' alone; the checks against the plan agree with it):\n%v", n, err)
			}
			if !equalTrees(readTree(t, dir), before) {
				t.Error("the files were not restored")
			}
		})
	}
}

// TestSelfCheckPassesTheRender is the control: the unedited render passes
// every check and the build.
func TestSelfCheckPassesTheRender(t *testing.T) {
	root, dir := copyFixture(t)
	p, err := loadPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planLift(p)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := plan.render()
	if err != nil {
		t.Fatal(err)
	}
	if err := apply(plan, contents, root, true); err != nil {
		t.Fatal(err)
	}
	var detached []string
	for _, l := range plan.lifts {
		if l.detached {
			detached = append(detached, l.file)
		}
	}
	if strings.Join(detached, " ") != "migration_0003_widget_labels.go" {
		t.Errorf("detached comments in %v; want only 0003's, whose '' gofmt would reword in a doc comment", detached)
	}
}

// TestBuildCheck: the go command is the authority on types, which the
// syntax checks do not model. A package that does not build after the lift
// (here because it did not before) is restored, and -build=false skips the
// check.
func TestBuildCheck(t *testing.T) {
	root, dir := copyFixture(t)
	writeFile(t, dir+"/broken.go", "package store\n\nvar broken int = \"x\"\n")
	before := readTree(t, dir)
	code, stdout, stderr := runTool(t, root, dir)
	if code != 1 || !regexp.MustCompile(`the lifted package does not build \(go build \./store\):\n(.|\n)*broken\.go:3:\d+: cannot use "x"`).MatchString(stderr) {
		t.Fatalf("exit %d\n%s%s", code, stdout, stderr)
	}
	if !strings.HasSuffix(strings.TrimSpace(stderr), "every file is restored") || !equalTrees(readTree(t, dir), before) {
		t.Fatalf("the files were not restored:\n%s", stderr)
	}
	if code, stdout, stderr := runTool(t, root, "-build=false", dir); code != 0 {
		t.Fatalf("-build=false: exit %d\n%s%s", code, stdout, stderr)
	}
}
