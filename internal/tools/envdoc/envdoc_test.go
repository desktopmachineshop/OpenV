package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docNameRe is a variable's row in a generated table: its first cell.
var docNameRe = regexp.MustCompile("^\\| `([A-Z][A-Z0-9_]*)` \\|")

// TestEnvVarsDoc holds docs/env-vars.md to the code (refactor plan step D1):
// every variable S8's inventory says the code reads has a row, no row names
// a variable the code no longer reads, and the tables are what
// go run ./internal/tools/envdoc writes.
func TestEnvVarsDoc(t *testing.T) {
	root := repoRoot(t)
	rows, err := ReadInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{}
	for _, r := range rows {
		read[r.Name] = true
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(docFile)))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	documented := map[string]bool{}
	for _, line := range strings.Split(generatedPart(t, doc), "\n") {
		if m := docNameRe.FindStringSubmatch(line); m != nil {
			documented[m[1]] = true
		}
	}
	for _, name := range sortedKeys(read) {
		if !documented[name] {
			t.Errorf("%s is read by the code (%s) but %s has no row for it: add an entry to %s and run go run ./internal/tools/envdoc",
				name, inventoryFile, docFile, varsFile)
		}
	}
	for _, name := range sortedKeys(documented) {
		if !read[name] {
			t.Errorf("%s has a row in %s but the code no longer reads it (%s): remove its entry from %s and run go run ./internal/tools/envdoc",
				name, docFile, inventoryFile, varsFile)
		}
	}
	want, err := Generate(root, doc)
	if err != nil {
		t.Fatalf("go run ./internal/tools/envdoc would refuse:\n%v", err)
	}
	if want != doc {
		t.Errorf("%s is not what go run ./internal/tools/envdoc writes: edit %s rather than the table, then run it.\nFirst difference at line %d:\n have: %s\n want: %s",
			docFile, varsFile, firstDiffLine(doc, want), lineAt(doc, firstDiffLine(doc, want)), lineAt(want, firstDiffLine(doc, want)))
	}
}

// TestDefaultCell pins how the Default column is chosen.
func TestDefaultCell(t *testing.T) {
	for _, tc := range []struct {
		inventory []string
		own, want string
		fails     bool
	}{
		{[]string{`"./dist"`}, "", "`./dist`", false},
		{[]string{"24h0m0s"}, "", "`24h0m0s`", false},
		{[]string{`""`}, "", "none", false},
		{[]string{`""`}, "`default`", "`default`", false},
		{[]string{"32"}, "`64`", "", true},
		{[]string{"-"}, "", "", true},
		{[]string{"(computed)"}, "`x`", "`x`", false},
		{[]string{`"a"`, "(computed)"}, "", "", true},
		{[]string{`"a"`, "-"}, "`a`", "`a`", false},
	} {
		got, err := defaultCell(tc.inventory, tc.own)
		if (err != nil) != tc.fails || got != tc.want {
			t.Errorf("defaultCell(%q, %q) = %q, %v; want %q, failing %v", tc.inventory, tc.own, got, err, tc.want, tc.fails)
		}
	}
}

func generatedPart(t *testing.T, doc string) string {
	t.Helper()
	begin, end := strings.Index(doc, beginMarker), strings.Index(doc, endMarker)
	if begin < 0 || end < begin {
		t.Fatalf("%s has lost its markers %q and %q", docFile, beginMarker, endMarker)
	}
	return doc[begin:end]
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := findRoot(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func firstDiffLine(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}

func lineAt(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n-1 < len(lines) {
		return lines[n-1]
	}
	return "(end of file)"
}
