package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The area guides of refactor plan step N2: each directory below has a
// README.md of at most 150 lines, whose "## Areas" table names the areas of
// docs/areas.json that own its files in its first column, and a CLAUDE.md
// of at most 15 lines that imports it. Every area is named by at least one
// guide, so every area maps to a README section.
var areaGuides = []string{
	"cmd/server",
	"internal/api",
	"internal/persistence/postgres",
	"internal/domain",
	"internal/runner",
	"internal/notify",
	"frontend/src",
	"frontend/src/api",
	"e2e",
	"scripts",
}

const (
	maxReadmeLines = 150
	maxClaudeLines = 15
)

func TestEveryAreaMapsToAGuide(t *testing.T) {
	root := repoRoot(t)
	idx, err := LoadIndex(filepath.Join(root, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	areas := map[string]bool{}
	for _, a := range idx.Areas {
		areas[a.Name] = true
	}

	named := map[string][]string{}
	for _, dir := range areaGuides {
		readme := readGuide(t, root, dir, "README.md", maxReadmeLines)
		claude := readGuide(t, root, dir, "CLAUDE.md", maxClaudeLines)
		if !strings.HasPrefix(claude, "@README.md\n") {
			t.Errorf("%s/CLAUDE.md: the first line must be @README.md, which imports the guide", dir)
		}
		rows := areaRows(readme)
		if len(rows) == 0 {
			t.Errorf("%s/README.md: no table under \"## Areas\" names an area in its first column", dir)
		}
		for _, name := range rows {
			if !areas[name] {
				t.Errorf("%s/README.md: the Areas table names %q, which is not an area of %s", dir, name, IndexFile)
				continue
			}
			named[name] = append(named[name], dir)
		}
	}

	var missing []string
	for name := range areas {
		if len(named[name]) == 0 {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no guide's Areas table names these areas of %s: %s\nAdd a row to the README.md of the directory that holds their files.",
			IndexFile, strings.Join(missing, ", "))
	}
}

// readGuide reads dir/name and fails the test if it is missing or longer
// than limit lines.
func readGuide(t *testing.T, root, dir, name string, limit int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), name))
	if err != nil {
		t.Errorf("%s/%s: %v", dir, name, err)
		return ""
	}
	s := string(b)
	if n := strings.Count(s, "\n"); n > limit {
		t.Errorf("%s/%s has %d lines; a guide of this kind has at most %d (refactor plan N2)", dir, name, n, limit)
	}
	return s
}

// areaRows returns the first cell of every body row of the tables in the
// README's "## Areas" section: the names of the areas it covers.
func areaRows(readme string) []string {
	_, section, ok := strings.Cut(readme, "\n## Areas\n")
	if !ok {
		return nil
	}
	section, _, _ = strings.Cut(section, "\n## ")
	lines := strings.Split(section, "\n")
	var names []string
	for i, line := range lines {
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|-") {
			continue
		}
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "|-") {
			continue // a header row
		}
		cells := strings.Split(line, "|")
		if len(cells) > 1 {
			names = append(names, strings.TrimSpace(cells[1]))
		}
	}
	return names
}
