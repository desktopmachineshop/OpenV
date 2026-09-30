package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// source is one Go file of a package directory: its base name and content.
type source struct {
	name string
	src  []byte
}

// goFile is a parsed file. Object resolution is on, so an identifier bound
// in the file (a local variable, a parameter) has an Obj and a package
// qualifier has none.
type goFile struct {
	name    string
	src     []byte
	ast     *ast.File
	test    bool              // a _test.go file
	imports map[string]string // local name -> path, for plain and named imports
	dots    int               // dot imports
}

// pkg is every Go file of one package directory, test files included, so
// that a new constructor's name is checked against the test helpers too.
type pkg struct {
	fset  *token.FileSet
	files []*goFile // by name
}

func readDir(dir string) ([]source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []source
	for _, e := range entries {
		if e.IsDir() || !isGoFile(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, source{e.Name(), b})
	}
	return out, nil
}

func isGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

func parsePkg(srcs []source) (*pkg, error) {
	srcs = append([]source(nil), srcs...)
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].name < srcs[j].name })
	p := &pkg{fset: token.NewFileSet()}
	for _, s := range srcs {
		af, err := parser.ParseFile(p.fset, s.name, s.src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		f := &goFile{name: s.name, src: s.src, ast: af, test: strings.HasSuffix(s.name, "_test.go"),
			imports: map[string]string{}}
		for _, is := range af.Imports {
			ip, _ := strconv.Unquote(is.Path.Value)
			name := assumedName(ip)
			if is.Name != nil {
				name = is.Name.Name
			}
			switch name {
			case ".":
				f.dots++
			case "_":
			default:
				f.imports[name] = ip
			}
		}
		p.files = append(p.files, f)
	}
	return p, nil
}

// sources returns the package's files as sources.
func (p *pkg) sources() []source {
	out := make([]source, len(p.files))
	for i, f := range p.files {
		out[i] = source{f.name, f.src}
	}
	return out
}

// pkgName is the package clause of the production files.
func (p *pkg) pkgName() string {
	for _, f := range p.files {
		if !f.test {
			return f.ast.Name.Name
		}
	}
	return ""
}

// sameScope reports whether f's declarations share the package scope of the
// production files: every non-test file, and the in-package test files.
func (p *pkg) sameScope(f *goFile) bool { return f.ast.Name.Name == p.pkgName() }

// assumedName is the name an unnamed import is referred to by: its last
// path element, minus a /vN suffix, a go- prefix and anything from the
// first character that cannot be in an identifier (gopkg.in/yaml.v3).
func assumedName(importPath string) string {
	base := path.Base(importPath)
	if strings.HasPrefix(base, "v") {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			if dir := path.Dir(importPath); dir != "." {
				base = path.Base(dir)
			}
		}
	}
	base = strings.TrimPrefix(base, "go-")
	if i := strings.IndexFunc(base, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}); i >= 0 {
		base = base[:i]
	}
	return base
}

// buildConstraint describes what limits the builds a file is in: a
// //go:build line above its package clause, or a _GOOS/_GOARCH name.
func buildConstraint(f *goFile) string {
	for _, cg := range f.ast.Comments {
		if cg.Pos() > f.ast.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") {
				return "under " + strings.TrimSpace(strings.TrimPrefix(c.Text, "//go:build"))
			}
		}
	}
	if c := nameConstraint(f.name); c != "" {
		return "for " + c
	}
	return ""
}

// errAlreadySplit means the function no longer returns a literal: the split
// has been made (M11a has merged), and there is nothing to generate.
var errAlreadySplit = errors.New("already split")

// entry is one element of the table's literal: a tool.
type entry struct {
	name  string
	elt   *ast.CompositeLit
	chunk []byte // the source lines it occupies, with the comment lines above it
}

// table is the function being split and its literal's entries.
type table struct {
	file    *goFile
	fn      *ast.FuncDecl
	lit     *ast.CompositeLit
	elem    string // the element type's name, Tool
	entries []entry
}

// findFunc returns the production function named name, without a receiver.
func (p *pkg) findFunc(name string) (*goFile, *ast.FuncDecl, error) {
	var found []*goFile
	var fn *ast.FuncDecl
	for _, f := range p.files {
		if f.test {
			continue
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				found, fn = append(found, f), fd
			}
		}
	}
	switch len(found) {
	case 0:
		return nil, nil, fmt.Errorf("no production file of the package declares func %s()", name)
	case 1:
		return found[0], fn, nil
	}
	return nil, nil, fmt.Errorf("func %s() is declared %d times (build-tagged variants?); splittools needs exactly one", name, len(found))
}

// readTable finds the function's literal and cuts it into entries, one per
// tool, each with the whole lines it occupies. It refuses a shape whose
// text it could not move whole: a comment outside the entries, two entries
// on one line, an entry without a Name string.
func (p *pkg) readTable(name string) (*table, error) {
	f, fn, err := p.findFunc(name)
	if err != nil {
		return nil, err
	}
	at := func(n ast.Node) string { return fmt.Sprintf("%s:%d", f.name, p.fset.Position(n.Pos()).Line) }
	elem, err := sliceResult(fn)
	if err != nil {
		return nil, fmt.Errorf("%s: %s() %w", at(fn), name, err)
	}
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil, fmt.Errorf("%s: %s() must hold one statement, return []%s{...}", at(fn), name, elem)
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, fmt.Errorf("%s: %s() must hold one statement, return []%s{...}", at(fn), name, elem)
	}
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	if !ok {
		if _, call := ret.Results[0].(*ast.CallExpr); call {
			return nil, fmt.Errorf("%s: %s() returns %s, not a []%s literal: the table is %w (M11a has run), so there is nothing to generate",
				at(ret), name, oneLine(p.text(f, ret.Results[0])), elem, errAlreadySplit)
		}
		return nil, fmt.Errorf("%s: %s() does not return a []%s literal", at(ret), name, elem)
	}
	if !isSliceOf(lit.Type, elem) {
		return nil, fmt.Errorf("%s: %s() returns a literal that is not []%s", at(lit), name, elem)
	}
	t := &table{file: f, fn: fn, lit: lit, elem: elem}
	if err := p.checkBodyComments(t, at); err != nil {
		return nil, err
	}
	if err := p.cutEntries(t, at); err != nil {
		return nil, err
	}
	return t, nil
}

// sliceResult returns T when fn takes nothing and returns one unnamed []T.
func sliceResult(fn *ast.FuncDecl) (string, error) {
	ft := fn.Type
	if ft.TypeParams != nil || (ft.Params != nil && len(ft.Params.List) > 0) ||
		ft.Results == nil || len(ft.Results.List) != 1 || len(ft.Results.List[0].Names) > 0 {
		return "", fmt.Errorf("must take no arguments and return one []T")
	}
	at, ok := ft.Results.List[0].Type.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return "", fmt.Errorf("must return a slice")
	}
	id, ok := at.Elt.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("must return a slice of a named type of its package")
	}
	return id.Name, nil
}

func isSliceOf(e ast.Expr, elem string) bool {
	at, ok := e.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return false
	}
	id, ok := at.Elt.(*ast.Ident)
	return ok && id.Name == elem
}

// checkBodyComments refuses a comment in the function body outside the
// literal's braces: no entry would carry it into a constructor.
func (p *pkg) checkBodyComments(t *table, at func(ast.Node) string) error {
	for _, cg := range t.file.ast.Comments {
		if cg.Pos() > t.fn.Body.Lbrace && cg.End() <= t.fn.Body.Rbrace &&
			(cg.End() <= t.lit.Lbrace || cg.Pos() >= t.lit.Rbrace) {
			return fmt.Errorf("%s: a comment in %s() outside its []%s literal would be lost; move it into an entry or above the function",
				at(cg), t.fn.Name.Name, t.elem)
		}
	}
	return nil
}

// cutEntries splits the literal into whole-line chunks: an entry's chunk
// runs from the line after the previous entry (or the opening brace) to the
// line holding its closing brace and comma, so the comment lines above an
// entry travel with it.
func (p *pkg) cutEntries(t *table, at func(ast.Node) string) error {
	tf := p.fset.File(t.lit.Pos())
	line := func(pos token.Pos) int { return tf.Line(pos) }
	off := func(pos token.Pos) int { return tf.Offset(pos) }
	src := t.file.src
	if rest := restOfLine(src, off(t.lit.Lbrace)+1); strings.TrimSpace(rest) != "" {
		return fmt.Errorf("%s: the []%s literal's opening brace must end its line", at(t.lit), t.elem)
	}
	prev := line(t.lit.Lbrace)
	names := map[string]bool{}
	for i, e := range t.lit.Elts {
		cl, ok := e.(*ast.CompositeLit)
		if !ok || (cl.Type != nil && !isIdent(cl.Type, t.elem)) {
			return fmt.Errorf("%s: entry %d of %s() is not a %s{...} literal", at(e), i+1, t.fn.Name.Name, t.elem)
		}
		name, err := entryName(cl)
		if err != nil {
			return fmt.Errorf("%s: entry %d of %s() %w", at(e), i+1, t.fn.Name.Name, err)
		}
		if names[name] {
			return fmt.Errorf("%s: %s() lists %q twice; splittools keys entries by Name", at(e), t.fn.Name.Name, name)
		}
		names[name] = true
		end := line(cl.End())
		rest := strings.TrimSpace(restOfLine(src, off(cl.End())))
		if !strings.HasPrefix(rest, ",") {
			return fmt.Errorf("%s: entry %q must end its line with its comma", at(e), name)
		}
		if after := strings.TrimSpace(rest[1:]); after != "" && !strings.HasPrefix(after, "//") {
			return fmt.Errorf("%s: entry %q shares its last line with other code; give each entry its own lines", at(e), name)
		}
		chunk := src[lineStart(tf, prev+1):lineEndOffset(tf, src, end)]
		t.entries = append(t.entries, entry{name: name, elt: cl, chunk: chunk})
		prev = end
	}
	if len(t.entries) == 0 {
		return fmt.Errorf("%s: %s() returns an empty table", at(t.lit), t.fn.Name.Name)
	}
	closing := line(t.lit.Rbrace)
	if tail := src[lineEndOffset(tf, src, prev):lineStart(tf, closing)]; len(bytes.TrimSpace(tail)) != 0 {
		return fmt.Errorf("%s: a comment after the last entry of %s() has no entry to travel with; move it into that entry",
			at(t.entries[len(t.entries)-1].elt), t.fn.Name.Name)
	}
	if lead := src[lineStart(tf, closing):off(t.lit.Rbrace)]; len(bytes.TrimSpace(lead)) != 0 {
		return fmt.Errorf("%s: the []%s literal's closing brace must start its line", at(t.lit), t.elem)
	}
	return nil
}

// entryName returns an entry's Name: "..." value.
func entryName(cl *ast.CompositeLit) (string, error) {
	for _, e := range cl.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return "", fmt.Errorf("has an unkeyed field; splittools keys entries by their Name field")
		}
		if isIdent(kv.Key, "Name") {
			if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				return strconv.Unquote(bl.Value)
			}
			return "", fmt.Errorf("has a Name that is not a string literal; splittools keys entries by it")
		}
	}
	return "", fmt.Errorf("has no Name field; splittools keys entries by it")
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// restOfLine returns src from off to the end of its line.
func restOfLine(src []byte, off int) string {
	end := bytes.IndexByte(src[off:], '\n')
	if end < 0 {
		return string(src[off:])
	}
	return string(src[off : off+end])
}

// lineStart is the offset of line n's first byte.
func lineStart(tf *token.File, n int) int { return tf.Offset(tf.LineStart(n)) }

// lineEndOffset is the offset just past the newline ending line n.
func lineEndOffset(tf *token.File, src []byte, n int) int {
	if n < tf.LineCount() {
		return lineStart(tf, n+1)
	}
	return len(src)
}

// trimBlankLines drops the blank lines a chunk starts with.
func trimBlankLines(b []byte) []byte {
	for {
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 || len(bytes.TrimSpace(b[:nl])) != 0 {
			return b
		}
		b = b[nl+1:]
	}
}

// countLines counts lines as internal/archtest does.
func countLines(src []byte) int {
	n := bytes.Count(src, []byte{'\n'})
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}

// text returns a node's source text.
func (p *pkg) text(f *goFile, n ast.Node) string {
	return string(f.src[p.fset.Position(n.Pos()).Offset:p.fset.Position(n.End()).Offset])
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}
