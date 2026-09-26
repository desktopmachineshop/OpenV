package archtest

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// rootName is how ratchets.json and failure messages name the package at the
// module root (release_notes.go), whose module-relative directory is ".".
const rootName = "(root)"

// module is the parsed source of this Go module: the root package and every
// package under cmd/ and internal/. Nothing is built or type-checked; the
// rules read syntax only, which keeps a run fast and the same on every host.
// Files are kept whatever their build constraints (windows-only files
// included), except those no build can select, such as //go:build ignore.
type module struct {
	root  string          // absolute directory holding go.mod
	path  string          // module path from go.mod
	fset  *token.FileSet  // file names are module-relative, slash-separated
	pkgs  map[string]*pkg // keyed by pkg.name
	names []string        // sorted keys of pkgs
	edges map[string][]string
}

type pkg struct {
	name  string // module-relative directory, or rootName
	dir   string // module-relative directory, "." for the root
	gname string // Go package name of the production files
	files []*file
	tests []*file
	types map[string]*typeDecl // package-level types of the production files
}

type typeDecl struct {
	expr ast.Expr
	file *file
}

type file struct {
	rel     string // module-relative, slash-separated
	base    string
	ast     *ast.File
	lines   int
	test    bool
	gen     bool // has a "Code generated ... DO NOT EDIT." header
	pkg     *pkg
	imports map[string]string // local name -> import path
	paths   []string          // import paths, in source order
}

// loadModule parses the module that contains the working directory, which
// under go test is this package's directory.
func loadModule() (*module, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return parseModule(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, errors.New("archtest: no go.mod above the working directory")
		}
		dir = parent
	}
}

var modulePathRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// parseModule parses the module rooted at root.
func parseModule(root string) (*module, error) {
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	mm := modulePathRe.FindSubmatch(gomod)
	if mm == nil {
		return nil, errors.New("archtest: go.mod has no module line")
	}
	m := &module{root: root, path: string(mm[1]), fset: token.NewFileSet(), pkgs: map[string]*pkg{}}
	if err := m.parseDir("."); err != nil {
		return nil, err
	}
	for _, top := range []string{"cmd", "internal"} {
		if err := m.walk(top); err != nil {
			return nil, err
		}
	}
	sort.Strings(m.names)
	for _, name := range m.names {
		p := m.pkgs[name]
		for _, f := range append(append([]*file{}, p.files...), p.tests...) {
			f.imports = m.importNames(f.ast)
		}
	}
	m.edges = productionEdges(m)
	return m, nil
}

// walk parses every package directory under top, skipping what the go
// command skips: testdata, and names starting with "." or "_".
func (m *module) walk(top string) error {
	start := filepath.Join(m.root, top)
	if _, err := os.Stat(start); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		n := d.Name()
		if p != start && (n == "testdata" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
			return filepath.SkipDir // a nested module is not part of this one
		}
		rel, err := filepath.Rel(m.root, p)
		if err != nil {
			return err
		}
		return m.parseDir(filepath.ToSlash(rel))
	})
}

func (m *module) parseDir(dir string) error {
	entries, err := os.ReadDir(filepath.Join(m.root, filepath.FromSlash(dir)))
	if err != nil {
		return err
	}
	var p *pkg
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
			continue
		}
		rel := path.Join(dir, n)
		src, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		af, err := parser.ParseFile(m.fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("archtest: %w", err)
		}
		if !buildable(af) {
			continue
		}
		if p == nil {
			p = m.addPkg(dir)
		}
		f := &file{rel: rel, base: n, ast: af, lines: countLines(src),
			test: strings.HasSuffix(n, "_test.go"), gen: ast.IsGenerated(af), pkg: p}
		for _, is := range af.Imports {
			ip, _ := strconv.Unquote(is.Path.Value)
			f.paths = append(f.paths, ip)
		}
		if f.test {
			p.tests = append(p.tests, f)
			continue
		}
		p.files = append(p.files, f)
		p.gname = af.Name.Name
		for _, d := range af.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, s := range gd.Specs {
					ts := s.(*ast.TypeSpec)
					p.types[ts.Name.Name] = &typeDecl{expr: ts.Type, file: f}
				}
			}
		}
	}
	return nil
}

func (m *module) addPkg(dir string) *pkg {
	name := dir
	if dir == "." {
		name = rootName
	}
	p := &pkg{name: name, dir: dir, types: map[string]*typeDecl{}}
	m.pkgs[name] = p
	m.names = append(m.names, name)
	return p
}

// buildable reports whether some build can select the file: its //go:build
// expression is satisfiable with the "ignore" tag off. File-name suffixes
// such as _windows.go always are.
func buildable(af *ast.File) bool {
	for _, cg := range af.Comments {
		if cg.Pos() > af.Package {
			break
		}
		for _, c := range cg.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			expr, err := constraint.Parse(c.Text)
			if err != nil {
				return true
			}
			return satisfiable(expr)
		}
	}
	return true
}

func satisfiable(expr constraint.Expr) bool {
	seen := map[string]bool{}
	var tags []string
	var collect func(constraint.Expr)
	collect = func(e constraint.Expr) {
		switch x := e.(type) {
		case *constraint.TagExpr:
			if !seen[x.Tag] && x.Tag != "ignore" {
				seen[x.Tag] = true
				tags = append(tags, x.Tag)
			}
		case *constraint.NotExpr:
			collect(x.X)
		case *constraint.AndExpr:
			collect(x.X)
			collect(x.Y)
		case *constraint.OrExpr:
			collect(x.X)
			collect(x.Y)
		}
	}
	collect(expr)
	if len(tags) > 12 {
		return true
	}
	for bits := 0; bits < 1<<len(tags); bits++ {
		on := map[string]bool{}
		for i, tag := range tags {
			on[tag] = bits&(1<<i) != 0
		}
		if expr.Eval(func(tag string) bool { return on[tag] }) {
			return true
		}
	}
	return false
}

func countLines(src []byte) int {
	n := bytes.Count(src, []byte{'\n'})
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}

var majorSuffixRe = regexp.MustCompile(`^v[0-9]+$`)

// importNames maps each import's local name to its path. A module package
// is named by its package clause; any other by the last path element,
// minus a /vN suffix and a go- prefix or -go suffix.
func (m *module) importNames(af *ast.File) map[string]string {
	names := map[string]string{}
	for _, is := range af.Imports {
		ip, _ := strconv.Unquote(is.Path.Value)
		var name string
		switch {
		case is.Name != nil:
			name = is.Name.Name
		case m.internal(ip) && m.pkgs[m.nameOf(ip)] != nil:
			name = m.pkgs[m.nameOf(ip)].gname
		default:
			parts := strings.Split(ip, "/")
			name = parts[len(parts)-1]
			if len(parts) > 1 && majorSuffixRe.MatchString(name) {
				name = parts[len(parts)-2]
			}
			name = strings.TrimSuffix(strings.TrimPrefix(name, "go-"), "-go")
		}
		if name != "_" && name != "." {
			names[name] = ip
		}
	}
	return names
}

// internal reports whether an import path belongs to this module.
func (m *module) internal(importPath string) bool {
	return importPath == m.path || strings.HasPrefix(importPath, m.path+"/")
}

// nameOf turns an import path of this module into a package name.
func (m *module) nameOf(importPath string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(importPath, m.path), "/")
	if rel == "" {
		return rootName
	}
	return rel
}

// pos renders a node's position as file:line.
func (m *module) pos(n ast.Node) string {
	p := m.fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

// line returns the line a position is on.
func (m *module) line(p token.Pos) int { return m.fset.Position(p).Line }

// production returns every production file of every package, in order.
func (m *module) production() []*file {
	var out []*file
	for _, name := range m.names {
		out = append(out, m.pkgs[name].files...)
	}
	return out
}

// under reports whether a package name is dir or lies below it.
func under(name, dir string) bool {
	return name == dir || strings.HasPrefix(name, dir+"/")
}

// selectorPkg returns the import path a selector's qualifier names in f, or
// "" when the qualifier is not an imported package.
func (f *file) selectorPkg(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return f.imports[id.Name]
	}
	return ""
}

// isPkgFunc reports whether call is importPath.name(...) in f.
func (f *file) isPkgFunc(call *ast.CallExpr, importPath string, names ...string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || f.selectorPkg(sel) != importPath {
		return false
	}
	for _, n := range names {
		if sel.Sel.Name == n {
			return true
		}
	}
	return false
}

// productionEdges maps each package with production files to the module
// packages its production files import. Test files add no edges.
func productionEdges(m *module) map[string][]string {
	edges := map[string][]string{}
	for _, name := range m.names {
		seen := map[string]bool{}
		for _, f := range m.pkgs[name].files {
			for _, ip := range f.paths {
				to := m.nameOf(ip)
				if m.internal(ip) && to != name && !seen[to] {
					seen[to] = true
					edges[name] = append(edges[name], to)
				}
			}
		}
		sort.Strings(edges[name])
	}
	return edges
}

// linked returns every package reachable from start over edges, each mapped
// to the package it was first reached from ("" for start itself).
func linked(edges map[string][]string, start string) map[string]string {
	parent := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range edges[cur] {
			if _, ok := parent[next]; !ok {
				parent[next] = cur
				queue = append(queue, next)
			}
		}
	}
	return parent
}

// chain renders the import path from the start of a linked walk to name.
func chain(parent map[string]string, name string) string {
	var hops []string
	for cur := name; cur != ""; cur = parent[cur] {
		hops = append([]string{cur}, hops...)
	}
	return strings.Join(hops, " -> ")
}
