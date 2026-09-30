package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// spec is a committed split: which constructor each tool's entry moves
// into, and which new file each constructor goes to. specs/M11a.json is
// refactor step M11a's.
type spec struct {
	Description string `json:"description,omitempty"`
	// Package is the package directory, relative to the module root.
	Package string `json:"package"`
	// Func names the function whose []T literal is split; Tools when empty.
	Func string `json:"func,omitempty"`
	// Headers holds, per new file, a comment written above its package
	// clause with a blank line between.
	Headers map[string]string `json:"headers,omitempty"`
	// Constructors are concatenated in this order, so their tools, taken in
	// this order, must be the table's order.
	Constructors []constructor `json:"constructors"`
}

// constructor is one function returning a run of the table's entries.
type constructor struct {
	Func string `json:"func"`
	File string `json:"file"`
	// Doc is the constructor's doc comment, as // lines.
	Doc string `json:"doc,omitempty"`
	// Tools are the entries' Name values, in the table's order.
	Tools []string `json:"tools"`
}

func (s *spec) funcName() string {
	if s.Func == "" {
		return "Tools"
	}
	return s.Func
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

// validate checks what the spec says on its own; checkAgainst checks it
// against the package.
func (s *spec) validate() error {
	if s.Package == "" || len(s.Constructors) == 0 {
		return fmt.Errorf("a spec needs a package and at least one constructor")
	}
	if !token.IsIdentifier(s.funcName()) {
		return fmt.Errorf("func %q is not a Go identifier", s.Func)
	}
	funcs := map[string]bool{}
	files := map[string]bool{}
	for _, c := range s.Constructors {
		if err := c.validate(); err != nil {
			return err
		}
		if funcs[c.Func] {
			return fmt.Errorf("constructor %s is declared twice", c.Func)
		}
		funcs[c.Func] = true
		files[c.File] = true
	}
	for name, h := range s.Headers {
		if !files[name] {
			return fmt.Errorf("headers: %s is not the file of any constructor", name)
		}
		if err := commentLines(h); err != nil {
			return fmt.Errorf("the header of %s %w", name, err)
		}
	}
	return nil
}

func (c constructor) validate() error {
	switch {
	case !token.IsIdentifier(c.Func) || c.Func == "_":
		return fmt.Errorf("constructor %q: func is not a Go identifier", c.Func)
	case token.IsExported(c.Func):
		return fmt.Errorf("constructor %s: a constructor is unexported, so the package's API stays Tools()", c.Func)
	case c.Func == "init" || c.Func == "main" || types.Universe.Lookup(c.Func) != nil:
		return fmt.Errorf("constructor %s: that name is reserved or predeclared", c.Func)
	case len(c.Tools) == 0:
		return fmt.Errorf("constructor %s lists no tools", c.Func)
	}
	if err := checkFileName(c.File); err != nil {
		return fmt.Errorf("constructor %s: %w", c.Func, err)
	}
	if err := commentLines(c.Doc); err != nil {
		return fmt.Errorf("the doc of constructor %s %w", c.Func, err)
	}
	return nil
}

// checkFileName accepts the base name of a new non-test Go file that every
// build of the package compiles: no directory, no _test.go, and no
// _GOOS or _GOARCH suffix, which would leave its constructor out of the
// other builds.
func checkFileName(name string) error {
	switch {
	case name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\`):
		return fmt.Errorf("file %q is not a file name in the package directory", name)
	case !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_"):
		return fmt.Errorf("file %s is not a Go file the go command reads", name)
	case strings.HasSuffix(name, "_test.go"):
		return fmt.Errorf("file %s is a test file; the table is production code", name)
	}
	if c := nameConstraint(name); c != "" {
		return fmt.Errorf("file %s is built only for %s (go/build's file-name rule); pick another name", name, c)
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
