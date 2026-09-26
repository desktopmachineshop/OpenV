package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// spec is a move spec: which declarations of one package go to which files.
type spec struct {
	// Description says what the spec is for; declmove ignores it.
	Description string `json:"description,omitempty"`
	// Package is the package directory, relative to the module root.
	Package string `json:"package"`
	// Tests selects the package's in-package _test.go files instead of its
	// production files.
	Tests bool `json:"tests,omitempty"`
	// Targets are applied in order.
	Targets []target `json:"targets"`
}

// target lists the declarations that go to one file, in the order they are
// appended to it.
type target struct {
	File string `json:"file"`
	// Header is a comment written above the package clause, with a blank
	// line between, when the file is created. It may hold //go:build lines.
	Header string `json:"header,omitempty"`
	// Decls are declhash keys: Name, (*T).Name or T.Name. A key declared
	// more than once in the package (init, _, build-tagged variants) is
	// ambiguous; Name#2 picks the second, in file-name and source order.
	Decls []string `json:"decls"`
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
	if s.Package == "" || len(s.Targets) == 0 {
		return nil, fmt.Errorf("%s: a spec needs a package and at least one target", path)
	}
	return &s, nil
}

// move is one declaration, a function or a whole const, var or type
// declaration, and the file it goes to.
type move struct {
	node ast.Decl
	keys []string // the spec entries that name it, in spec order
	from *goFile
	to   string
}

// plan resolves a spec against the package. It refuses a missing or
// ambiguous name, a name listed twice, a declaration already in its target,
// and a grouped declaration whose names are not all listed for one target.
func plan(p *pkgInfo, sp *spec) ([]*move, error) {
	set := ""
	if sp.Tests {
		set = "test"
	}
	byKey := map[string][]*decl{}
	for _, d := range p.decls {
		if d.file.set == set {
			byKey[d.key] = append(byKey[d.key], d)
		}
	}
	var errs []string
	var moves []*move
	byNode := map[ast.Decl]*move{}
	requested := map[*decl]string{}
	for _, t := range sp.Targets {
		if err := checkTarget(p, t, set); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, name := range t.Decls {
			d, err := resolve(p, byKey, name)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if prev, ok := requested[d]; ok {
				errs = append(errs, fmt.Sprintf("%s: listed twice (for %s and %s)", name, prev, t.File))
				continue
			}
			requested[d] = t.File
			if d.file.name == t.File {
				errs = append(errs, fmt.Sprintf("%s: already in %s", name, t.File))
				continue
			}
			m := byNode[d.node]
			if m == nil {
				m = &move{node: d.node, from: d.file, to: t.File}
				byNode[d.node] = m
				moves = append(moves, m)
			} else if m.to != t.File {
				errs = append(errs, fmt.Sprintf("%s: declared together with %s (%s), which goes to %s; a grouped declaration moves as one",
					name, m.keys[0], where(p, d), m.to))
				continue
			}
			m.keys = append(m.keys, name)
		}
	}
	for _, m := range moves {
		for _, d := range p.decls {
			if d.node == m.node && requested[d] == "" {
				errs = append(errs, fmt.Sprintf("%s: declared together with %s (%s); list it for %s too",
					d.key, m.keys[0], where(p, d), m.to))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return moves, nil
}

func where(p *pkgInfo, d *decl) string {
	pos := p.fset.Position(d.node.Pos())
	return fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
}

// resolve finds the declaration a spec entry names.
func resolve(p *pkgInfo, byKey map[string][]*decl, name string) (*decl, error) {
	key, nth := name, 0
	if i := strings.LastIndexByte(name, '#'); i >= 0 {
		n, err := strconv.Atoi(name[i+1:])
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%s: after # comes a number from 1", name)
		}
		key, nth = name[:i], n
	}
	ds := byKey[key]
	switch {
	case len(ds) == 0:
		return nil, fmt.Errorf("%s: no such declaration", name)
	case nth > len(ds):
		return nil, fmt.Errorf("%s: only %d declarations are named %s", name, len(ds), key)
	case nth > 0:
		return ds[nth-1], nil
	case len(ds) > 1:
		var cands []string
		for i, d := range ds {
			cands = append(cands, fmt.Sprintf("%s#%d (%s)", key, i+1, where(p, d)))
		}
		return nil, fmt.Errorf("%s: ambiguous, pick one of %s", name, strings.Join(cands, ", "))
	}
	return ds[0], nil
}

// checkTarget refuses a target that is not a plain Go file name in the
// package directory, or that is in the other file set.
func checkTarget(p *pkgInfo, t target, set string) error {
	name := t.File
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || !isGoFile(name) {
		return fmt.Errorf("target %q: a target is a .go file name in the package directory", name)
	}
	if isTest := strings.HasSuffix(name, "_test.go"); isTest != (set == "test") {
		return fmt.Errorf("target %s: a spec with \"tests\": %v moves into %s files only", name, set == "test",
			map[bool]string{true: "_test.go", false: "non-test"}[set == "test"])
	}
	for _, f := range p.files {
		if f.name == name && f.set != set {
			return fmt.Errorf("target %s: in package %s, not the one the spec moves within", name, f.ast.Name.Name)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(t.Header), "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "//") {
			return fmt.Errorf("target %s: the header must be // comment lines", name)
		}
	}
	return nil
}

// declRange returns the byte range of the lines a declaration occupies:
// from the start of the line holding its doc comment (or its first token)
// to just past the newline that ends its last line, so a trailing comment
// on that line moves with it.
func declRange(p *pkgInfo, f *goFile, d ast.Decl) (int, int, error) {
	start := d.Pos()
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Doc != nil {
			start = x.Doc.Pos()
		}
	case *ast.GenDecl:
		if x.Doc != nil {
			start = x.Doc.Pos()
		}
	}
	s, e := p.fset.Position(start).Offset, p.fset.Position(d.End()).Offset
	src := f.src
	ls := bytes.LastIndexByte(src[:s], '\n') + 1
	le := len(src)
	if i := bytes.IndexByte(src[e:], '\n'); i >= 0 {
		le = e + i + 1
	}
	rest := strings.TrimSpace(string(src[e:le]))
	if strings.TrimSpace(string(src[ls:s])) != "" || (rest != "" && !strings.HasPrefix(rest, "//")) {
		pos := p.fset.Position(d.Pos())
		return 0, 0, fmt.Errorf("%s:%d: the declaration shares a line with other code; run gofmt first", pos.Filename, pos.Line)
	}
	return ls, le, nil
}

// fileEdit collects what happens to one file.
type fileEdit struct {
	name   string
	orig   []byte  // nil for a file the move creates
	file   *goFile // nil for a file the move creates
	cuts   [][2]int
	adds   []string
	header string
	need   map[string]importRef // imports the added declarations use, by local name
}

// importRef is an import as the moved code's source file declares it.
type importRef struct {
	name  string
	path  string
	named bool   // the source file names it explicitly
	text  string // an existing spec's source text, kept as it is
}

// buildEdits works out every touched file's new content. A nil content
// means the file is left with nothing but its package clause and is
// deleted.
func buildEdits(p *pkgInfo, sp *spec, moves []*move) (map[string][]byte, error) {
	edits := map[string]*fileEdit{}
	get := func(name string) *fileEdit {
		if e := edits[name]; e != nil {
			return e
		}
		e := &fileEdit{name: name, need: map[string]importRef{}}
		for _, f := range p.files {
			if f.name == name {
				e.orig, e.file = f.src, f
			}
		}
		edits[name] = e
		return e
	}
	headers := map[string]string{}
	for _, t := range sp.Targets {
		if headers[t.File] == "" {
			headers[t.File] = t.Header
		}
	}
	var pkgName string
	for _, m := range moves {
		if len(m.from.dots) > 0 {
			return nil, fmt.Errorf("%s: %s has dot imports, which declmove cannot carry", m.keys[0], m.from.name)
		}
		ls, le, err := declRange(p, m.from, m.node)
		if err != nil {
			return nil, err
		}
		src, dst := get(m.from.name), get(m.to)
		src.cuts = append(src.cuts, [2]int{ls, le})
		dst.adds = append(dst.adds, string(m.from.src[ls:le]))
		dst.header = headers[m.to]
		for q := range usedQualifiers(m.from, m.node) {
			ref, err := importOf(m.from, q)
			if err != nil {
				return nil, err
			}
			if prev, ok := dst.need[q]; ok && prev.path != ref.path {
				return nil, fmt.Errorf("%s: %s names %s, but code moving to %s from elsewhere names %s so",
					m.keys[0], q, ref.path, m.to, prev.path)
			}
			dst.need[q] = ref
		}
		if hasEmbed(m.node) {
			// A //go:embed directive needs the file to import embed, which a
			// string or []byte variable does not use as a qualifier.
			dst.need["_ embed"] = importRef{name: "_", path: "embed", named: true}
		}
		pkgName = m.from.ast.Name.Name
	}
	out := map[string][]byte{}
	names := make([]string, 0, len(edits))
	for name := range edits {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := edits[name].content(pkgName)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = b
	}
	return out, nil
}

// hasEmbed reports whether a declaration carries a //go:embed directive.
func hasEmbed(d ast.Decl) bool {
	gd, ok := d.(*ast.GenDecl)
	if !ok {
		return false
	}
	groups := []*ast.CommentGroup{gd.Doc}
	for _, s := range gd.Specs {
		if vs, ok := s.(*ast.ValueSpec); ok {
			groups = append(groups, vs.Doc)
		}
	}
	for _, cg := range groups {
		if cg == nil {
			continue
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:embed ") {
				return true
			}
		}
	}
	return false
}

// importOf returns the import spec of f that local name q refers to.
func importOf(f *goFile, q string) (importRef, error) {
	for _, is := range f.ast.Imports {
		ip, _ := strconv.Unquote(is.Path.Value)
		name := assumedName(ip)
		if is.Name != nil {
			name = is.Name.Name
		}
		if name == q {
			return importRef{name: q, path: ip, named: is.Name != nil}, nil
		}
	}
	return importRef{}, fmt.Errorf("%s: no import named %s", f.name, q)
}

// content builds the file: the original minus the moved-out declarations,
// or a new file with its header and package clause, then the moved-in
// declarations in spec order, the imports they need, and the imports the
// moved-out ones alone used dropped.
func (e *fileEdit) content(pkgName string) ([]byte, error) {
	var b []byte
	if e.orig == nil {
		var h strings.Builder
		if hdr := strings.TrimSpace(e.header); hdr != "" {
			h.WriteString(hdr + "\n\n")
		}
		h.WriteString("package " + pkgName + "\n")
		b = []byte(h.String())
	} else {
		b = cut(e.orig, e.cuts)
	}
	if len(e.adds) > 0 {
		b = append(bytes.TrimRight(b, "\n"), '\n')
		for _, a := range e.adds {
			b = append(append(b, '\n'), a...)
		}
	}
	b, err := addImports(e.name, b, e.need)
	if err != nil {
		return nil, err
	}
	if e.file != nil && len(e.cuts) > 0 {
		if b, err = dropUnusedImports(e.file, b); err != nil {
			return nil, err
		}
	}
	b, err = format.Source(b)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(b)) == "package "+pkgName {
		return nil, nil
	}
	return b, nil
}

// cut removes byte ranges from src.
func cut(src []byte, ranges [][2]int) []byte {
	rs := append([][2]int(nil), ranges...)
	sort.Slice(rs, func(i, j int) bool { return rs[i][0] > rs[j][0] })
	out := append([]byte(nil), src...)
	for _, r := range rs {
		out = append(out[:r[0]], out[r[1]:]...)
	}
	return out
}
