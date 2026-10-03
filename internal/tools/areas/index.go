package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// IndexFile is the area index, relative to the repository root.
const IndexFile = "docs/areas.json"

// Index is docs/areas.json: the areas, each with the globs of the files it
// owns, the globs that make a file a test file, and the glossary.
type Index struct {
	About      string    `json:"about"`
	GlobSyntax string    `json:"glob_syntax"`
	TestFiles  TestFiles `json:"test_files"`
	Glossary   []Term    `json:"glossary"`
	Areas      []Area    `json:"areas"`
	testRegexp []*regexp.Regexp
}

// TestFiles says which files need no area.
type TestFiles struct {
	About string   `json:"about"`
	Globs []string `json:"globs"`
}

// Term is one glossary entry: a word the product uses, the other names the
// code, the API or the UI give it, and what it means.
type Term struct {
	Term    string   `json:"term"`
	Aka     []string `json:"aka"`
	Meaning string   `json:"meaning"`
}

// Area is one slice of the product and the globs of the files it owns.
type Area struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Globs       []string `json:"globs"`
	globRegexp  []*regexp.Regexp
}

// Claim is an area's claim on a path, through the first of its globs that
// matches it.
type Claim struct {
	Area string
	Glob string
}

// LoadIndex reads and checks an area index file.
func LoadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseIndex(data)
}

// ParseIndex decodes an area index, refusing unknown keys, and checks its
// shape: unique, named areas with a description and well-formed globs.
func ParseIndex(data []byte) (*Index, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var idx Index
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("%s: %w", IndexFile, err)
	}
	if len(idx.Areas) == 0 {
		return nil, errors.New(IndexFile + ": no areas")
	}
	if len(idx.TestFiles.Globs) == 0 {
		return nil, errors.New(IndexFile + ": test_files has no globs")
	}
	for _, g := range idx.TestFiles.Globs {
		rx, err := compileGlob(g)
		if err != nil {
			return nil, fmt.Errorf("%s: test_files: %w", IndexFile, err)
		}
		idx.testRegexp = append(idx.testRegexp, rx)
	}
	seen := map[string]bool{}
	for i := range idx.Areas {
		a := &idx.Areas[i]
		switch {
		case a.Name == "":
			return nil, fmt.Errorf("%s: area %d has no name", IndexFile, i+1)
		case seen[a.Name]:
			return nil, fmt.Errorf("%s: area %s is listed twice", IndexFile, a.Name)
		case strings.TrimSpace(a.Description) == "":
			return nil, fmt.Errorf("%s: area %s has no description", IndexFile, a.Name)
		case len(a.Globs) == 0:
			return nil, fmt.Errorf("%s: area %s has no globs", IndexFile, a.Name)
		}
		seen[a.Name] = true
		for _, g := range a.Globs {
			rx, err := compileGlob(g)
			if err != nil {
				return nil, fmt.Errorf("%s: area %s: %w", IndexFile, a.Name, err)
			}
			a.globRegexp = append(a.globRegexp, rx)
		}
	}
	for i, t := range idx.Glossary {
		if t.Term == "" || strings.TrimSpace(t.Meaning) == "" {
			return nil, fmt.Errorf("%s: glossary entry %d needs a term and a meaning", IndexFile, i+1)
		}
	}
	return &idx, nil
}

// Claims lists the areas whose globs match path (relative to the repository
// root, with forward slashes), in index order, one claim per area.
func (idx *Index) Claims(path string) []Claim {
	var out []Claim
	for _, a := range idx.Areas {
		for i, rx := range a.globRegexp {
			if rx.MatchString(path) {
				out = append(out, Claim{Area: a.Name, Glob: a.Globs[i]})
				break
			}
		}
	}
	return out
}

// IsTestFile reports whether path is a test file, which needs no area.
func (idx *Index) IsTestFile(path string) bool {
	for _, rx := range idx.testRegexp {
		if rx.MatchString(path) {
			return true
		}
	}
	return false
}

// Owner returns the one area that claims path, or an error naming the path
// and the areas when no area or more than one claims it.
func (idx *Index) Owner(path string) (string, error) {
	claims := idx.Claims(path)
	switch len(claims) {
	case 1:
		return claims[0].Area, nil
	case 0:
		if idx.IsTestFile(path) {
			return "", fmt.Errorf("no area claims %s; it is a test file, which needs none", path)
		}
		return "", fmt.Errorf("no area claims %s; add a glob that matches it to one area in %s", path, IndexFile)
	default:
		parts := make([]string, len(claims))
		for i, c := range claims {
			parts[i] = fmt.Sprintf("%s (%s)", c.Area, c.Glob)
		}
		return "", fmt.Errorf("%s is claimed by %d areas, %s; a file belongs to exactly one, so narrow the globs in %s",
			path, len(claims), strings.Join(parts, ", "), IndexFile)
	}
}

// compileGlob turns a path glob into an anchored regexp, with the syntax of
// glob_regex in scripts/refactor/refactor_guard.py: `**/` spans any number
// of directories (none included), a trailing `/**` everything below, and
// `*` and `?` stay within one path segment.
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
