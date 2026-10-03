package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// spec is a committed stage split of one function: the contiguous ranges
// of its body that move into stage methods, in the order the function
// calls them. specs/M4.json is refactor step M4's, for cmd/server's main().
type spec struct {
	Description string `json:"description,omitempty"`
	// Package is the package directory, relative to the module root.
	Package string `json:"package"`
	// File holds the function; main.go when empty.
	File string `json:"file,omitempty"`
	// Func is the function split into stages; main when empty. It takes no
	// parameters and returns nothing.
	Func string `json:"func,omitempty"`
	// Type is the struct whose fields the function's shared locals become,
	// and whose pointer the stages are methods of; app when empty.
	Type string `json:"type,omitempty"`
	// TypeFile is the new file that declares Type; app.go when empty.
	TypeFile string `json:"type_file,omitempty"`
	// TypeDoc is Type's doc comment, as // lines.
	TypeDoc string `json:"type_doc,omitempty"`
	// Recv names the function's *Type local and each stage's receiver; a
	// when empty.
	Recv string `json:"recv,omitempty"`
	// MainBudget caps the function's lines after the split, "func" line to
	// closing brace, as internal/archtest measures a function; 0 for none.
	MainBudget int `json:"main_budget,omitempty"`
	// Stages are the ranges that move, in body order.
	Stages []stageSpec `json:"stages"`
	// Main is where the function's own tail starts: from that line to the
	// end of the body, the statements stay in the function. Nil when every
	// statement after the last stage's range belongs to it.
	Main *anchor `json:"main,omitempty"`
}

// stageSpec is one stage: a method of *Type in File, holding a contiguous
// range of the function's body.
type stageSpec struct {
	Name string `json:"name"`
	File string `json:"file"`
	// Doc is the method's doc comment, as // lines.
	Doc string `json:"doc,omitempty"`
	// Lines is the range as authored, "first-last", in the function's file.
	Lines string `json:"lines"`
	// Starts is the text of the range's first line, trimmed: the anchor that
	// finds the range again when the file has moved on (rule R4).
	Starts string `json:"starts"`
	// Cleanups name the variables the function binds the cleanups this
	// stage returns to (one per defer right after the range), when the
	// derived names (the deferred variable's own, or closeDB for
	// db.Close) do not suit.
	Cleanups []string `json:"cleanups,omitempty"`

	first, last int // Lines, parsed
}

// anchor is a line of the function's body, found by its trimmed text when
// the line number has moved.
type anchor struct {
	Line   int    `json:"line"`
	Starts string `json:"starts"`
}

func (s *spec) file() string     { return or(s.File, "main.go") }
func (s *spec) funcName() string { return or(s.Func, "main") }
func (s *spec) typeName() string { return or(s.Type, "app") }
func (s *spec) typeFile() string { return or(s.TypeFile, "app.go") }
func (s *spec) recv() string     { return or(s.Recv, "a") }

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func loadSpec(path string) (*spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var s spec
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// validate checks what the spec says on its own; the plan checks it
// against the function.
func (s *spec) validate() error {
	if s.Package == "" || len(s.Stages) == 0 {
		return fmt.Errorf("a spec needs a package and at least one stage")
	}
	for what, name := range map[string]string{"func": s.funcName(), "type": s.typeName(), "recv": s.recv()} {
		if !token.IsIdentifier(name) || name == "_" {
			return fmt.Errorf("%s %q is not a Go identifier", what, name)
		}
		if types.Universe.Lookup(name) != nil {
			return fmt.Errorf("%s %s is a predeclared name", what, name)
		}
	}
	if s.typeName() == s.recv() || s.typeName() == s.funcName() || s.recv() == s.funcName() {
		return fmt.Errorf("func, type and recv need three names, not %s, %s and %s", s.funcName(), s.typeName(), s.recv())
	}
	if err := checkFileName(s.file()); err != nil {
		return fmt.Errorf("file: %w", err)
	}
	if err := checkFileName(s.typeFile()); err != nil {
		return fmt.Errorf("type_file: %w", err)
	}
	if strings.HasPrefix(s.typeFile(), "wire_") {
		return fmt.Errorf("type_file %s: wire_*.go files hold stages (movecheck -flatten and the S4 boot steps read their methods as stages); name the type's file otherwise", s.typeFile())
	}
	if err := commentLines(s.TypeDoc); err != nil {
		return fmt.Errorf("type_doc %w", err)
	}
	if s.MainBudget < 0 {
		return fmt.Errorf("main_budget %d is negative", s.MainBudget)
	}
	names := map[string]bool{}
	prev := 0
	for i := range s.Stages {
		st := &s.Stages[i]
		if err := st.validate(); err != nil {
			return err
		}
		if names[st.Name] {
			return fmt.Errorf("stage %s is named twice", st.Name)
		}
		names[st.Name] = true
		if st.File == s.typeFile() || st.File == s.file() {
			return fmt.Errorf("stage %s: file %s is the function's or the type's; a stage goes in a wire_*.go file of its own", st.Name, st.File)
		}
		if st.first <= prev {
			return fmt.Errorf("stage %s: lines %s start at or before the end of the stage before it (line %d); stages are listed in body order and do not overlap", st.Name, st.Lines, prev)
		}
		prev = st.last
	}
	if s.Main != nil {
		if s.Main.Line <= prev || strings.TrimSpace(s.Main.Starts) == "" {
			return fmt.Errorf("main: line %d must come after the last stage (line %d), with the text it starts with", s.Main.Line, prev)
		}
	}
	return nil
}

func (st *stageSpec) validate() error {
	switch {
	case !token.IsIdentifier(st.Name) || st.Name == "_":
		return fmt.Errorf("stage %q: name is not a Go identifier", st.Name)
	case token.IsExported(st.Name):
		return fmt.Errorf("stage %s: a stage is unexported, like the function it comes from", st.Name)
	case st.Name == "init" || st.Name == "main" || types.Universe.Lookup(st.Name) != nil:
		return fmt.Errorf("stage %s: that name is reserved or predeclared", st.Name)
	case !strings.HasPrefix(st.File, "wire_"):
		return fmt.Errorf("stage %s: file %s is not a wire_*.go file, which movecheck -flatten and the S4 boot steps read stages from", st.Name, st.File)
	case strings.TrimSpace(st.Starts) == "" || strings.TrimSpace(st.Starts) != st.Starts:
		return fmt.Errorf("stage %s: starts must be the range's first line, trimmed", st.Name)
	}
	if err := checkFileName(st.File); err != nil {
		return fmt.Errorf("stage %s: %w", st.Name, err)
	}
	if err := commentLines(st.Doc); err != nil {
		return fmt.Errorf("the doc of stage %s %w", st.Name, err)
	}
	a, b, ok := strings.Cut(st.Lines, "-")
	first, err1 := strconv.Atoi(a)
	last, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil || first < 1 || last < first {
		return fmt.Errorf("stage %s: lines %q is not a range first-last", st.Name, st.Lines)
	}
	st.first, st.last = first, last
	seen := map[string]bool{}
	for _, c := range st.Cleanups {
		if !token.IsIdentifier(c) || c == "_" || types.Universe.Lookup(c) != nil || seen[c] {
			return fmt.Errorf("stage %s: cleanup name %q is not a fresh Go identifier", st.Name, c)
		}
		seen[c] = true
	}
	return nil
}

// checkFileName accepts the base name of a non-test Go file that every
// build of the package compiles: no directory, no _test.go, and no _GOOS
// or _GOARCH suffix, which would leave its code out of the other builds.
func checkFileName(name string) error {
	switch {
	case name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\`):
		return fmt.Errorf("%q is not a file name in the package directory", name)
	case !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_"):
		return fmt.Errorf("%s is not a Go file the go command reads", name)
	case strings.HasSuffix(name, "_test.go"):
		return fmt.Errorf("%s is a test file; stages are production code", name)
	}
	if c := nameConstraint(name); c != "" {
		return fmt.Errorf("%s is built only for %s (go/build's file-name rule); pick another name", name, c)
	}
	return nil
}

// commentLines accepts text made of // comment lines, or nothing.
func commentLines(text string) error {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "//") {
			return fmt.Errorf("must be // comment lines")
		}
	}
	return nil
}

// nameConstraint applies go/build's file-name rule: name_GOOS_GOARCH,
// name_GOOS or name_GOARCH, optionally followed by _test.
func nameConstraint(name string) string {
	name, _, _ = strings.Cut(name, ".")
	i := strings.Index(name, "_")
	if i < 0 {
		return ""
	}
	l := strings.Split(name[i:], "_")
	if n := len(l); n > 0 && l[n-1] == "test" {
		l = l[:n-1]
	}
	n := len(l)
	if n >= 2 && knownOS[l[n-2]] && knownArch[l[n-1]] {
		return l[n-2] + "_" + l[n-1]
	}
	if n >= 1 && (knownOS[l[n-1]] || knownArch[l[n-1]]) {
		return l[n-1]
	}
	return ""
}

// knownOS and knownArch are go/build's lists (syslist.go).
var knownOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
	"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true, "openbsd": true,
	"plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
}

var knownArch = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true, "arm64be": true,
	"loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true, "mips64p32": true,
	"mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true,
	"s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}
