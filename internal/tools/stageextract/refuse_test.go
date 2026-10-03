package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specWith is the fixture's spec with change applied to its decoded JSON.
func specWith(t *testing.T, change func(m map[string]any)) *spec {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readFile(t, "testdata/fixture.json")), &m); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "spec.json")
	writeFile(t, path, string(b))
	sp, err := loadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func stageOf(m map[string]any, name string) map[string]any {
	for _, s := range m["stages"].([]any) {
		if st := s.(map[string]any); st["name"] == name {
			return st
		}
	}
	panic("no stage " + name)
}

// TestRefusals: each shape stageextract cannot move as it is, or would have
// to guess, is refused with errPlan and a message saying what to change,
// and nothing is written.
func TestRefusals(t *testing.T) {
	const rest = "\tfmt.Println(\"rest\", rest)\n"
	const scratch = "\tfmt.Println(\"scratch\", scratch)\n"
	long := "\t<-done\n" + strings.Repeat("\tfmt.Println(\"padding\")\n", 90)
	cases := []struct {
		name  string
		edits map[string][2]string
		extra map[string]string
		spec  func(m map[string]any)
		want  string
	}{
		{name: "a return in a range", edits: map[string][2]string{"main.go": {rest, "\tif rest < 0 { return }\n"}},
			want: "stage services: line 65 holds a return, which would end the stage instead of main()"},
		{name: "a defer in a block of a range", edits: map[string][2]string{"main.go": {rest, "\tif rest < 0 { defer fmt.Println() }\n"}},
			want: "stage services: line 65 holds a defer, which would run when the stage returns"},
		{name: "a recover in a range", edits: map[string][2]string{"main.go": {rest, "\t_ = recover(); _ = rest\n"}},
			want: "line 65 holds a recover"},
		{name: "a label in a range", edits: map[string][2]string{"main.go": {rest, "\tL: for range names { _ = rest; break L }\n"}},
			want: "line 65 holds a label"},
		{name: "a range of a defer alone", spec: func(m map[string]any) {
			stages := m["stages"].([]any)
			closing := map[string]any{"name": "closing", "file": "wire_closing.go", "lines": "46-46", "starts": "defer db.Close()"}
			m["stages"] = append(stages[:3:3], append([]any{closing}, stages[3:]...)...)
		}, want: "stage closing (from line 46) holds no statement before the defer at line 46"},
		{name: "lines that do not match the statements", spec: func(m map[string]any) { stageOf(m, "settings")["lines"] = "26-38" },
			want: "stage settings: the spec says lines 26-38, but its statements run 26-39"},
		{name: "an anchor no line reads", spec: func(m map[string]any) { stageOf(m, "settings")["starts"] = "// Settingz." },
			want: "stage settings: line 26 is \"// Settings.\", and no line of main() where a range may start reads \"// Settingz.\""},
		{name: "an anchor two lines read", edits: map[string][2]string{"main.go": {"\t// Jobs.\n", "\t// Settings.\n"}},
			spec: func(m map[string]any) {
				stageOf(m, "jobs")["starts"] = "// Settings."
				stageOf(m, "jobs")["lines"] = "70-80"
			},
			want: "lines 26, 69 of main() all read \"// Settings.\""},
		{name: "the receiver's name in use", edits: map[string][2]string{"main.go": {scratch, "\ta := 1; fmt.Println(\"scratch\", scratch, a)\n"}},
			want: "main() already uses the name a (line 35"},
		{name: "the type's name taken", edits: map[string][2]string{"helpers.go": {"type store struct", "type app struct{}\n\ntype store struct"}},
			want: "the package already declares app"},
		{name: "the type's name taken by a test", extra: map[string]string{"fixture/x_test.go": "package main\n\ntype app int\n"},
			want: "x_test.go already declares app"},
		{name: "a stage file that exists", extra: map[string]string{"fixture/wire_jobs.go": "package main\n"},
			want: "fixture/wire_jobs.go exists"},
		{name: "a stage over K14's budget", edits: map[string][2]string{"main.go": {"\t<-done\n", long}},
			want: "stage jobs would span 105 lines, over K14's 100; split its range in the spec"},
		{name: "main over the spec's budget", spec: func(m map[string]any) { m["main_budget"] = 10 },
			want: "main() would span 19 lines, over the spec's budget of 10"},
		{name: "a cleanup name main() uses", edits: map[string][2]string{"main.go": {"\tstop()\n}", "\tstop()\n\tcloseDB()\n}"},
			"helpers.go": {"func shout(", "func closeDB() {}\n\nfunc shout("}},
			want: "the cleanup it returns for the defer at line 46 would be bound to closeDB, which main() (line 86) already uses"},
		{name: "cleanups miscounted", spec: func(m map[string]any) { stageOf(m, "connect")["cleanups"] = []any{"one", "two"} },
			want: "stage connect names 2 cleanups, and returns 1"},
		{name: "a stage named like a field", spec: func(m map[string]any) { stageOf(m, "settings")["name"] = "port" },
			want: "the local port becomes a field of app, and a stage has its name"},
		{name: "a function with parameters", spec: func(m map[string]any) { m["func"] = "fatal"; m["file"] = "helpers.go" },
			want: "fatal takes parameters or returns results"},
		{name: "no such function", spec: func(m map[string]any) { m["func"] = "nope" },
			want: "main.go declares no function nope with a body"},
		{name: "no such file", spec: func(m map[string]any) { m["file"] = "nope.go" },
			want: "has no file nope.go that the go command builds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixtureModule(t, tc.edits, tc.extra)
			before := snapshot(t, dir)
			_, err := generate(dir, specWith(t, tc.spec), options{vet: goVet})
			if !errors.Is(err, errPlan) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v\nwant a refusal (errPlan) containing %q", err, tc.want)
			}
			if after := snapshot(t, dir); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Error("a refusal wrote files")
			}
		})
	}
}

// TestRefusalsOfTwoEdits: the refusals whose fixture takes more than one
// edit to the same file.
func TestRefusalsOfTwoEdits(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(src string) string
		extra map[string]string
		want  string
	}{
		{"a local constant two stages use", func(src string) string {
			src = strings.Replace(src, "\tfmt.Println(\"scratch\", scratch)\n", "\tconst greeting = \"hi\"; fmt.Println(\"scratch\", scratch, greeting)\n", 1)
			return strings.Replace(src, "\tconst greeting = \"hello\"\n", "\tfmt.Println()\n", 1)
		}, nil, "the local constant greeting (line 35) is used by more than one stage"},
		{"a dot import", func(src string) string {
			src = strings.Replace(src, "\tstr \"strconv\"\n", "\tstr \"strconv\"\n\t. \"strings\"\n", 1)
			return strings.Replace(src, "\tfmt.Println(\"rest\", rest)\n", "\tfmt.Println(\"rest\", rest, ToUpper(\"x\"))\n", 1)
		}, nil, "main.go has a dot import"},
		{"a field whose type no other file names", func(src string) string {
			src = strings.Replace(src, "\tstr \"strconv\"\n", "\tstr \"strconv\"\n\n\t\"example.com/fixture/inner\"\n", 1)
			src = strings.Replace(src, "\t<-done\n", "\t<-done; _ = th\n", 1)
			return strings.Replace(src, "\tfmt.Println(\"scratch\", scratch)\n", "\tth := inner.New(); fmt.Println(\"scratch\", scratch, th != nil)\n", 1)
		}, map[string]string{"inner/inner.go": "package inner\n\ntype thing struct{}\n\nfunc New() *thing { return &thing{} }\n"},
			"the local th (line 37) becomes a field of app, but its type example.com/fixture/inner.thing is unexported in another package"},
		{"a grouped var of a field", func(src string) string {
			src = strings.Replace(src, "\tvar count = limit * 2\n", "\tvar (count = limit * 2; extra = 1)\n", 1)
			return strings.Replace(src, "\tfmt.Println(\"rest\", rest)\n", "\tfmt.Println(\"rest\", rest, extra)\n", 1)
		}, nil, "line 56: a grouped var declaration declares a field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixtureModule(t, nil, tc.extra)
			writeFile(t, filepath.Join(dir, "main.go"), tc.edit(readFile(t, filepath.Join(dir, "main.go"))))
			_, err := generate(dir, fixtureSpec(t), options{})
			if !errors.Is(err, errPlan) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v\nwant a refusal (errPlan) containing %q", err, tc.want)
			}
		})
	}
}

// TestVetFailureRestores: a split go vet fails is no refusal: every file
// is restored and the error says so.
func TestVetFailureRestores(t *testing.T) {
	dir := fixtureModule(t, map[string][2]string{"main.go": {"\tfmt.Println(\"scratch\", scratch)\n", "\tfmt.Printf(\"%d\\n\", scratch)\n"}}, nil)
	before := snapshot(t, dir)
	_, err := generate(dir, fixtureSpec(t), options{vet: goVet})
	if err == nil || errors.Is(err, errPlan) || !strings.Contains(err.Error(), "every file is restored") {
		t.Fatalf("got %v, want a vet failure that restores the files", err)
	}
	if after := snapshot(t, dir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Error("the files were not restored")
	}
}

// TestDrift: when main.go has moved on, each range is found again by its
// first line's text, with a note; a statement added inside a range moves
// with it, one added before the first range stays in main(), and the split
// still prints what the original prints.
func TestDrift(t *testing.T) {
	dir := fixtureModule(t, map[string][2]string{"main.go": {
		"\tfmt.Println(\"start\")\n",
		"\tfmt.Println(\"start\")\n\tfmt.Println(\"one more line\")\n",
	}}, nil)
	writeFile(t, filepath.Join(dir, "main.go"), strings.Replace(readFile(t, filepath.Join(dir, "main.go")),
		"\tfmt.Println(\"reset:\", err)\n", "\tfmt.Println(\"reset:\", err)\n\tfmt.Println(\"a new statement in services\")\n", 1))
	before := program(t, dir)
	res, err := generate(dir, fixtureSpec(t), options{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(res.lines, "\n")
	for _, want := range []string{
		"stageextract: note: stage signals starts at line 17, not 16: main.go has moved on since the spec was written",
		"stageextract: note: stage jobs starts at line 71, not 69",
		"stageextract: note: stage services runs lines 51-69, not 50-67",
		"services -> wire_services.go (lines 51-69, 23 lines)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report lacks %q:\n%s", want, joined)
		}
	}
	if src := readFile(t, filepath.Join(dir, "wire_services.go")); !strings.Contains(src, "a new statement in services") {
		t.Errorf("the statement added to services is not in its stage:\n%s", src)
	}
	if src := readFile(t, filepath.Join(dir, "main.go")); !strings.Contains(src, "a := &app{}\n\tfmt.Println(\"start\")\n\tfmt.Println(\"one more line\")\n") {
		t.Errorf("the statements before the first range are not main()'s:\n%s", src)
	}
	if after := program(t, dir); after != before {
		t.Errorf("the split prints\n%s\nthe original\n%s", after, before)
	}
}

// TestSpecValidation: what loadSpec refuses in a spec on its own.
func TestSpecValidation(t *testing.T) {
	good := readFile(t, "testdata/fixture.json")
	cases := []struct {
		name, old, new, want string
	}{
		{"an unknown field", `"main_budget": 30`, `"main_budget": 30, "extra": 1`, "unknown field"},
		{"no stages", `"stages": [`, `"stages": [], "x": [`, "unknown field"},
		{"an exported stage", `"name": "jobs"`, `"name": "Jobs"`, "a stage is unexported"},
		{"a predeclared stage name", `"name": "jobs"`, `"name": "len"`, "reserved or predeclared"},
		{"not an identifier", `"name": "jobs"`, `"name": "job-s"`, "not a Go identifier"},
		{"a stage file not wire_", `"file": "wire_jobs.go"`, `"file": "jobs.go"`, "is not a wire_*.go file"},
		{"a test file", `"file": "wire_jobs.go"`, `"file": "wire_jobs_test.go"`, "is a test file"},
		{"a GOOS file", `"file": "wire_jobs.go"`, `"file": "wire_linux.go"`, "is built only for linux"},
		{"a malformed range", `"lines": "69-80"`, `"lines": "80-69"`, "is not a range first-last"},
		{"ranges out of order", `"lines": "69-80"`, `"lines": "60-80"`, "start at or before the end of the stage before it"},
		{"a stage named twice", `"name": "jobs"`, `"name": "services"`, "stage services is named twice"},
		{"an untrimmed anchor", `"starts": "// Jobs."`, `"starts": " // Jobs."`, "starts must be the range's first line, trimmed"},
		{"a type file of stages", `"main_budget": 30`, `"main_budget": 30, "type_file": "wire_app.go"`, "wire_*.go files hold stages"},
		{"a stage in the function's file", `"main_budget": 30`, `"main_budget": 30, "file": "wire_jobs.go"`, "is the function's or the type's"},
		{"one name for type and recv", `"main_budget": 30`, `"main_budget": 30, "recv": "app"`, "need three names"},
		{"a predeclared recv", `"main_budget": 30`, `"main_budget": 30, "recv": "nil"`, "is a predeclared name"},
		{"a tail inside the stages", `"line": 82`, `"line": 70`, "must come after the last stage"},
		{"a bad cleanup name", `"lines": "41-45",`, `"lines": "41-45", "cleanups": ["close-db"],`, "is not a fresh Go identifier"},
		{"a doc that is no comment", `"lines": "69-80"`, `"lines": "69-80", "doc": "jobs runs the jobs"`, "must be // comment lines"},
		{"a negative budget", `"main_budget": 30`, `"main_budget": -1`, "is negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(good, tc.old) {
				t.Fatalf("the fixture spec lacks %q", tc.old)
			}
			path := filepath.Join(t.TempDir(), "spec.json")
			if err := os.WriteFile(path, []byte(strings.Replace(good, tc.old, tc.new, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSpec(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "spec.json")
	writeFile(t, path, `{"package": "x", "stages": []}`)
	if _, err := loadSpec(path); err == nil || !strings.Contains(err.Error(), "at least one stage") {
		t.Errorf("an empty spec: %v", err)
	}
}

// TestDerivedName: the default name a returned cleanup is bound to.
func TestDerivedName(t *testing.T) {
	sp := fixtureSpec(t)
	for _, st := range sp.Stages {
		if len(st.Cleanups) > 0 {
			t.Fatalf("the fixture names a cleanup: %v", st.Cleanups)
		}
	}
	dir := fixtureModule(t, nil, nil)
	res, err := generate(dir, specWith(t, func(m map[string]any) { stageOf(m, "connect")["cleanups"] = []any{"closeStore"} }), options{dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(res.lines, "\n"); !strings.Contains(joined, "returns db.Close for main() to defer as closeStore") {
		t.Errorf("a cleanup the spec names:\n%s", joined)
	}
}
