package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// areasFile is the area index (refactor step N1), relative to the
// repository root. X1 lands one area per pull request (X1a-X1h).
const areasFile = "docs/areas.json"

// areaIndex is the part of docs/areas.json the tool reads: each area's
// name and globs. internal/tools/areas owns the file and checks it; this
// copy of its glob syntax (scripts/refactor/refactor_guard.py's
// glob_regex) only reads it, since the module's import edges are frozen
// (S1) and a tool here is a package main of its own.
type areaIndex struct {
	root  string
	areas []indexArea
}

type indexArea struct {
	name  string
	globs []*regexp.Regexp
}

// findAreas walks up from dir to the directory holding docs/areas.json and
// loads it. It returns nil, nil when there is none.
func findAreas(dir string) (*areaIndex, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, areasFile))
		if err == nil {
			return parseAreas(dir, data)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

func parseAreas(root string, data []byte) (*areaIndex, error) {
	var raw struct {
		Areas []struct {
			Name  string   `json:"name"`
			Globs []string `json:"globs"`
		} `json:"areas"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", areasFile, err)
	}
	idx := &areaIndex{root: root}
	for _, a := range raw.Areas {
		ia := indexArea{name: a.Name}
		for _, g := range a.Globs {
			rx, err := compileGlob(g)
			if err != nil {
				return nil, fmt.Errorf("%s: area %s: %w", areasFile, a.Name, err)
			}
			ia.globs = append(ia.globs, rx)
		}
		idx.areas = append(idx.areas, ia)
	}
	return idx, nil
}

// has reports whether the index names the area.
func (idx *areaIndex) has(name string) bool {
	for _, a := range idx.areas {
		if a.name == name {
			return true
		}
	}
	return false
}

// owner returns the one area that claims path (a file path the tool was
// given), or "" when none or more than one does.
func (idx *areaIndex) owner(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(idx.root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	rel = filepath.ToSlash(rel)
	found := ""
	for _, a := range idx.areas {
		for _, rx := range a.globs {
			if rx.MatchString(rel) {
				if found != "" {
					return ""
				}
				found = a.name
				break
			}
		}
	}
	return found
}

// compileGlob is internal/tools/areas's compileGlob: `**/` spans any
// number of directories (none included), a trailing `/**` everything
// below, and `*` and `?` stay within one path segment.
func compileGlob(glob string) (*regexp.Regexp, error) {
	if glob == "" || strings.HasPrefix(glob, "/") || strings.HasPrefix(glob, "./") || strings.Contains(glob, `\`) {
		return nil, fmt.Errorf("glob %q: want a path relative to the repository root, with forward slashes", glob)
	}
	var b strings.Builder
	b.WriteString("^")
	literal := 0
	flush := func(i int) {
		b.WriteString(regexp.QuoteMeta(glob[literal:i]))
	}
	for i := 0; i < len(glob); {
		var rx string
		var n int
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			rx, n = "(?:.*/)?", 3
		case glob[i:] == "/**":
			rx, n = "(?:/.*)?", 3
		case strings.HasPrefix(glob[i:], "**"):
			rx, n = ".*", 2
		case glob[i] == '*':
			rx, n = "[^/]*", 1
		case glob[i] == '?':
			rx, n = "[^/]", 1
		default:
			i++
			continue
		}
		flush(i)
		b.WriteString(rx)
		i += n
		literal = i
	}
	flush(len(glob))
	b.WriteString("$")
	return regexp.Compile(b.String())
}
