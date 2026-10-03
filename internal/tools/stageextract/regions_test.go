package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// smallStage is a stage of a small module's main(): its range runs from
// the line reading from to the line reading to.
type smallStage struct{ name, from, to string }

// smallSplit writes src as the main.go of a module of its own and splits
// its main() into the stages, each in wire_<name>.go, with the tail from
// the line reading tail when tail is set. It returns the package
// directory, what main() printed before the split, the report, and what
// the split program prints.
func smallSplit(t *testing.T, src, tail string, stages ...smallStage) (dir, before, report, after string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "small")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/small\n\ngo 1.25\n")
	writeFile(t, filepath.Join(dir, "main.go"), src)
	m := map[string]any{"package": "small"}
	var list []any
	for _, st := range stages {
		list = append(list, map[string]any{"name": st.name, "file": "wire_" + st.name + ".go",
			"lines": fmt.Sprintf("%d-%d", lineOf(t, src, st.from), lineOf(t, src, st.to)), "starts": st.from})
	}
	m["stages"] = list
	if tail != "" {
		m["main"] = map[string]any{"line": lineOf(t, src, tail), "starts": tail}
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "spec.json")
	writeFile(t, specPath, string(b))
	sp, err := loadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	before = program(t, dir)
	res, err := generate(dir, sp, options{vet: goVet})
	if err != nil {
		t.Fatalf("the split: %v", err)
	}
	report = strings.Join(res.lines, "\n")
	return dir, before, report, program(t, dir)
}

// lineOf is the line of src that reads text, trimmed; there must be one.
func lineOf(t *testing.T, src, text string) int {
	t.Helper()
	at := 0
	for i, l := range strings.Split(src, "\n") {
		if strings.TrimSpace(l) == text {
			if at > 0 {
				t.Fatalf("two lines read %q", text)
			}
			at = i + 1
		}
	}
	if at == 0 {
		t.Fatalf("no line reads %q", text)
	}
	return at
}

const smallHead = "package main\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n)\n\n"

// TestSplitLocalAcrossFunctionRegions: the statements main() keeps
// between two stage calls are regions of their own when a local is split,
// since a stage that has its own copy runs between them. A local a region
// of main() reads before writing, after a stage wrote it, becomes a field,
// so the read still sees the stage's write; one that every region and
// stage after its declaration writes before reading is split, and main()
// declares its own once, however many of its regions use it.
func TestSplitLocalAcrossFunctionRegions(t *testing.T) {
	work := smallStage{"work", "// Work.", `fmt.Println("stage", err)`}
	one := smallStage{"one", "// One.", `fmt.Println("one", err)`}
	two := smallStage{"two", "// Two.", `fmt.Println("two", err)`}
	cases := []struct {
		name, src string
		stages    []smallStage
		summary   string   // in the report
		main      []string // in main.go after the split
		vars      int      // var err error lines in main.go
	}{
		{"main()'s prefix declares it, a stage writes it, the tail reads it",
			smallHead + "func main() {\n\terr := errors.New(\"prefix\")\n\tfmt.Println(\"prefix\", err)\n" +
				"\t// Work.\n\terr = errors.New(\"stage\")\n\tfmt.Println(\"stage\", err)\n" +
				"\t// Tail.\n\tfmt.Println(\"tail\", err)\n}\n",
			[]smallStage{work}, "1 locals become fields of app (app.go), declared by each stage that uses it: none",
			[]string{"\ta.err = errors.New(\"prefix\")\n", "\tfmt.Println(\"tail\", a.err)\n"}, 0},
		{"a region main() keeps after a defer writes it, a later stage writes it, the tail reads it",
			smallHead + "func main() {\n\t// One.\n\terr := errors.New(\"one\")\n\tstop := func() { fmt.Println(\"stopped\") }\n" +
				"\tfmt.Println(\"one\", err)\n\tdefer stop()\n\terr = errors.New(\"main\")\n" +
				"\t// Two.\n\terr = errors.New(\"two\")\n\tfmt.Println(\"two\", err)\n" +
				"\t// Tail.\n\tfmt.Println(\"tail\", err)\n}\n",
			[]smallStage{one, two}, "2 locals become fields of app (app.go), declared by each stage that uses it: none",
			[]string{"\ta.err = errors.New(\"main\")\n", "\tfmt.Println(\"tail\", a.err)\n"}, 0},
		{"main()'s prefix declares it, and the stage and the tail each write it first",
			smallHead + "func main() {\n\terr := errors.New(\"prefix\")\n\tfmt.Println(\"prefix\", err)\n" +
				"\t// Work.\n\terr = errors.New(\"stage\")\n\tfmt.Println(\"stage\", err)\n" +
				"\t// Tail.\n\terr = errors.New(\"tail\")\n\tfmt.Println(\"tail\", err)\n}\n",
			[]smallStage{work}, "0 locals become fields of app (app.go), declared by each stage that uses it: err",
			[]string{"\terr := errors.New(\"prefix\")\n", "\ta.work()\n\n\t// Tail.\n\terr = errors.New(\"tail\")\n"}, 0},
		{"a stage declares it, and two regions of main() each write it first",
			smallHead + "func main() {\n\t// One.\n\terr := errors.New(\"one\")\n\tstop := func() { fmt.Println(\"stopped\") }\n" +
				"\tfmt.Println(\"one\", err)\n\tdefer stop()\n\terr = errors.New(\"main\")\n\tfmt.Println(\"main\", err)\n" +
				"\t// Two.\n\terr = errors.New(\"two\")\n\tfmt.Println(\"two\", err)\n" +
				"\t// Tail.\n\terr = errors.New(\"tail\")\n\tfmt.Println(\"tail\", err)\n}\n",
			[]smallStage{one, two}, "1 locals become fields of app (app.go), declared by each stage that uses it: err",
			[]string{"\tdefer stop()\n\tvar err error\n\terr = errors.New(\"main\")\n", "\t// Tail.\n\terr = errors.New(\"tail\")\n"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, before, report, after := smallSplit(t, tc.src, "// Tail.", tc.stages...)
			if !strings.Contains(report, tc.summary) {
				t.Errorf("the report lacks %q:\n%s", tc.summary, report)
			}
			mainSrc := readFile(t, filepath.Join(dir, "main.go"))
			for _, want := range tc.main {
				if !strings.Contains(mainSrc, want) {
					t.Errorf("main.go lacks %q:\n%s", want, mainSrc)
				}
			}
			if n := strings.Count(mainSrc, "var err error"); n != tc.vars {
				t.Errorf("main.go declares err with var %d times, want %d:\n%s", n, tc.vars, mainSrc)
			}
			if after != before {
				t.Errorf("the split prints\n%s\nmain() printed\n%s", after, before)
			}
		})
	}
}

// TestCleanupOfALocalOfMain: a defer right after a range whose function
// hangs off a local that stays main()'s is no cleanup the stage can
// return; it stays in main() as it is, with any defer after it, and a
// note says so. One before it that hangs off a field is still returned.
func TestCleanupOfALocalOfMain(t *testing.T) {
	const closer = "type closer struct{ name string }\n\nfunc (c closer) Close() { fmt.Println(\"closed\", c.name) }\n\n"
	cases := []struct {
		name, src string
		returns   string // how the report's line of stage one ends
		main      string // in main.go after the split
	}{
		{"declared before the first range",
			"package main\n\nimport \"fmt\"\n\n" + closer + "func main() {\n\tc := closer{name: \"c\"}\n" +
				"\t// One.\n\tfmt.Println(\"one\")\n\tdefer c.Close()\n" +
				"\t// Two.\n\tfmt.Println(\"two\")\n}\n",
			"lines)", "\tc := closer{name: \"c\"}\n\ta.one()\n\tdefer c.Close()\n\ta.two()\n"},
		{"after a defer of a field",
			"package main\n\nimport \"fmt\"\n\n" + closer + "func main() {\n\tc := closer{name: \"c\"}\n" +
				"\t// One.\n\td := closer{name: \"d\"}\n\tfmt.Println(\"one\")\n\tdefer d.Close()\n\tdefer c.Close()\n" +
				"\t// Two.\n\tfmt.Println(\"two\", d.name)\n}\n",
			"lines), returns d.Close for main() to defer as closeD", "\tcloseD := a.one()\n\tdefer closeD()\n\tdefer c.Close()\n\ta.two()\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, before, report, after := smallSplit(t, tc.src, "",
				smallStage{"one", "// One.", `fmt.Println("one")`}, smallStage{"two", "// Two.", strings.TrimSpace(lastLine(tc.src))})
			note := fmt.Sprintf("stageextract: note: stage one: the defer at line %d stays in main() as it is, and so does any defer after it: "+
				"c stays a local of main(), which the stage cannot reach", lineOf(t, tc.src, "defer c.Close()"))
			stage := fmt.Sprintf("one -> wire_one.go (lines %d-%d, ", lineOf(t, tc.src, "// One."), lineOf(t, tc.src, `fmt.Println("one")`))
			var noted, staged bool
			for _, l := range strings.Split(report, "\n") {
				noted = noted || l == note
				if strings.HasPrefix(l, stage) {
					staged = true
					if !strings.HasSuffix(l, tc.returns) {
						t.Errorf("the report's line of stage one is %q, want it to end %q", l, tc.returns)
					}
				}
			}
			if !noted || !staged {
				t.Errorf("the report lacks the line %q or a line starting %q:\n%s", note, stage, report)
			}
			if mainSrc := readFile(t, filepath.Join(dir, "main.go")); !strings.Contains(mainSrc, tc.main) {
				t.Errorf("main.go lacks %q:\n%s", tc.main, mainSrc)
			}
			if after != before {
				t.Errorf("the split prints\n%s\nmain() printed\n%s", after, before)
			}
		})
	}
}

// lastLine is the last statement line of src's main(), the line before its
// closing brace.
func lastLine(src string) string {
	l := strings.Split(strings.TrimRight(src, "\n"), "\n")
	return l[len(l)-2]
}

// TestSplitLocalAddressedThroughAField: a local of two stages whose
// address a field's pointer method takes (p.c.add()) is no split local:
// the method can keep the pointer, which a second copy would no longer
// reach. It becomes a field, and the split prints what main() printed.
func TestSplitLocalAddressedThroughAField(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\n" +
		"type counter struct{ n int }\n\nvar seen []*counter\n\nfunc (c *counter) add() { c.n++; seen = append(seen, c) }\n\n" +
		"type pair struct{ c counter }\n\n" +
		"func main() {\n\t// One.\n\tvar p pair\n\tp = pair{c: counter{n: 1}}\n\tp.c.add()\n\tfmt.Println(\"one\", p.c.n)\n" +
		"\t// Two.\n\tp = pair{c: counter{n: 10}}\n\tp.c.add()\n\tfmt.Println(\"two\", p.c.n)\n" +
		"\t// Tail.\n\tfmt.Println(\"first seen\", seen[0].n)\n}\n"
	_, before, report, after := smallSplit(t, src, "// Tail.",
		smallStage{"one", "// One.", `fmt.Println("one", p.c.n)`}, smallStage{"two", "// Two.", `fmt.Println("two", p.c.n)`})
	if want := "1 locals become fields of app (app.go), declared by each stage that uses it: none"; !strings.Contains(report, want) {
		t.Errorf("the report lacks %q:\n%s", want, report)
	}
	if after != before {
		t.Errorf("the split prints\n%s\nmain() printed\n%s", after, before)
	}
}
