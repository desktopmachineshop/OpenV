package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// registryVar and registryType name the registry the tool reads: the
// package-level `var migrations = []Migration{...}` of
// internal/persistence/postgres (K9).
const (
	registryVar  = "migrations"
	registryType = "Migration"
)

// pkgSource is one package's production files, parsed with comments and
// type-checked against empty stand-ins for its imports: enough to evaluate
// each entry's Version and Name, to know which package-level declaration
// or import an identifier names, and fast, because nothing is loaded.
type pkgSource struct {
	dir   string
	fset  *token.FileSet
	files []*srcFile // sorted by name
	pkg   *types.Package
	info  *types.Info
	decls map[types.Object]*declInfo
}

// srcFile is one production file of the package.
type srcFile struct {
	name string
	src  []byte
	ast  *ast.File
}

// declInfo is one package-level declaration: a function or method, or a
// whole const, var or type declaration, with every name it declares.
type declInfo struct {
	keys []string // declhash and declmove keys: Name, (*T).Name or T.Name
	file *srcFile
	node ast.Decl
}

// entry is one element of the registry.
type entry struct {
	lit     *ast.CompositeLit
	version int64
	name    string
	field   string       // Run or RunDB
	value   ast.Expr     // the value of that field
	fn      *ast.FuncLit // the value, when it is a function literal
	pos     string       // file:line of the element
}

func (e *entry) label() string { return fmt.Sprintf("%04d %s", e.version, e.name) }

// stubImporter hands the type checker an empty package for every import,
// named as the go command would name it. Selectors into it fail to resolve,
// which is fine: the tool follows identifiers of this package and the
// package names of its imports, never what an import declares.
type stubImporter map[string]*types.Package

func (s stubImporter) Import(path string) (*types.Package, error) {
	if p, ok := s[path]; ok {
		return p, nil
	}
	p := types.NewPackage(path, assumedName(path))
	p.MarkComplete()
	s[path] = p
	return p, nil
}

// assumedName is the package name an import path implies: its last
// element, without a gopkg.in-style ".vN" or a "/vN" major version.
func assumedName(path string) string {
	name := path[strings.LastIndex(path, "/")+1:]
	if i := strings.Index(name, "."); i > 0 {
		name = name[:i]
	}
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
		trimmed := strings.TrimSuffix(path, "/"+name)
		name = trimmed[strings.LastIndex(trimmed, "/")+1:]
	}
	return strings.NewReplacer("-", "_", ".", "_").Replace(name)
}

// loadPackage reads and resolves the production files of the package in
// dir: every non-test .go file the default build context selects.
func loadPackage(dir string) (*pkgSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	p := &pkgSource{
		dir:  dir,
		fset: token.NewFileSet(),
		info: &types.Info{
			Defs:      map[*ast.Ident]types.Object{},
			Uses:      map[*ast.Ident]types.Object{},
			Implicits: map[ast.Node]types.Object{},
			Types:     map[ast.Expr]types.TypeAndValue{},
		},
		decls: map[types.Object]*declInfo{},
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if ok, err := build.Default.MatchFile(dir, name); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(p.fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		p.files = append(p.files, &srcFile{name: name, src: src, ast: f})
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no production Go files", dir)
	}
	sort.Slice(p.files, func(i, j int) bool { return p.files[i].name < p.files[j].name })
	for _, f := range p.files[1:] {
		if first := p.files[0]; f.ast.Name.Name != first.ast.Name.Name {
			return nil, fmt.Errorf("%s: %s is package %s, but %s is package %s", dir, f.name, f.ast.Name.Name, first.name, first.ast.Name.Name)
		}
	}
	conf := types.Config{Importer: stubImporter{}, Error: func(error) {}}
	p.pkg, _ = conf.Check(files[0].Name.Name, p.fset, files, p.info) // errors come from the stub imports only
	for _, f := range p.files {
		for _, d := range f.ast.Decls {
			p.addDecl(f, d)
		}
	}
	return p, nil
}

// addDecl records a package-level declaration under each name it declares.
func (p *pkgSource) addDecl(f *srcFile, d ast.Decl) {
	di := &declInfo{file: f, node: d}
	var ids []*ast.Ident
	switch x := d.(type) {
	case *ast.FuncDecl:
		key := x.Name.Name
		if x.Recv != nil && len(x.Recv.List) == 1 {
			key = receiverKey(x.Recv.List[0].Type) + "." + key
		}
		di.keys = []string{key}
		ids = []*ast.Ident{x.Name}
	case *ast.GenDecl:
		if x.Tok == token.IMPORT {
			return
		}
		for _, spec := range x.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					di.keys = append(di.keys, n.Name)
					ids = append(ids, n)
				}
			case *ast.TypeSpec:
				di.keys = append(di.keys, sp.Name.Name)
				ids = append(ids, sp.Name)
			}
		}
	}
	for _, id := range ids {
		if obj := p.info.Defs[id]; obj != nil {
			p.decls[obj] = di
		}
	}
}

// receiverKey renders a method receiver type as declhash does: *T or T.
func receiverKey(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return "(*" + receiverKey(x.X) + ")"
	case *ast.ParenExpr:
		return receiverKey(x.X)
	case *ast.IndexExpr:
		return receiverKey(x.X)
	case *ast.IndexListExpr:
		return receiverKey(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}

// where names a position as file:line.
func (p *pkgSource) where(pos token.Pos) string {
	at := p.fset.Position(pos)
	return fmt.Sprintf("%s:%d", at.Filename, at.Line)
}

// off is a position's byte offset in its file.
func (p *pkgSource) off(pos token.Pos) int { return p.fset.Position(pos).Offset }

// file returns the file that holds a position.
func (p *pkgSource) file(pos token.Pos) *srcFile {
	name := p.fset.Position(pos).Filename
	for _, f := range p.files {
		if f.name == name {
			return f
		}
	}
	return nil
}

// text is the source text of a node.
func (p *pkgSource) text(n ast.Node) string {
	f := p.file(n.Pos())
	return string(f.src[p.off(n.Pos()):p.off(n.End())])
}

// registry finds `var migrations = []Migration{...}` and returns its
// declaration and composite literal.
func (p *pkgSource) registry() (*declInfo, *ast.CompositeLit, error) {
	obj := p.pkg.Scope().Lookup(registryVar)
	d := p.decls[obj]
	if d == nil {
		return nil, nil, fmt.Errorf("%s: no package-level var %s; liftmigrations reads the registry `var %s = []%s{...}`",
			p.dir, registryVar, registryVar, registryType)
	}
	gd, ok := d.node.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
		return nil, nil, fmt.Errorf("%s: %s is not declared on its own as `var %s = []%s{...}`",
			p.where(d.node.Pos()), registryVar, registryVar, registryType)
	}
	vs := gd.Specs[0].(*ast.ValueSpec)
	if len(vs.Names) != 1 || len(vs.Values) != 1 {
		return nil, nil, fmt.Errorf("%s: %s is not declared on its own", p.where(vs.Pos()), registryVar)
	}
	lit, ok := ast.Unparen(vs.Values[0]).(*ast.CompositeLit)
	if !ok {
		return nil, nil, fmt.Errorf("%s: %s is not initialised by a composite literal", p.where(vs.Pos()), registryVar)
	}
	return d, lit, nil
}

// entries reads the registry's elements, keyed or positional.
func (p *pkgSource) entries(lit *ast.CompositeLit) ([]*entry, error) {
	var fields []string
	if tn, ok := p.pkg.Scope().Lookup(registryType).(*types.TypeName); ok {
		if st, ok := tn.Type().Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				fields = append(fields, st.Field(i).Name())
			}
		}
	}
	var out []*entry
	var errs []string
	for _, elt := range lit.Elts {
		e, err := p.entry(elt, fields)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		out = append(out, e)
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return out, nil
}

// entry reads one registry element.
func (p *pkgSource) entry(elt ast.Expr, fields []string) (*entry, error) {
	e := &entry{pos: p.where(elt.Pos())}
	lit, ok := ast.Unparen(elt).(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("%s: a registry element is not a %s literal", e.pos, registryType)
	}
	e.lit = lit
	values := map[string]ast.Expr{}
	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				values[id.Name] = kv.Value
			}
		} else if i < len(fields) {
			values[fields[i]] = el
		}
	}
	if v := p.info.Types[values["Version"]].Value; v != nil && v.Kind() == constant.Int {
		e.version, _ = constant.Int64Val(v)
	} else {
		return nil, fmt.Errorf("%s: a migration's Version is not an integer constant", e.pos)
	}
	if v := p.info.Types[values["Name"]].Value; v != nil && v.Kind() == constant.String {
		e.name = constant.StringVal(v)
	} else {
		return nil, fmt.Errorf("%s: migration %04d's Name is not a string constant", e.pos, e.version)
	}
	for _, field := range []string{"Run", "RunDB"} {
		v := values[field]
		if v == nil {
			continue
		}
		if id, ok := ast.Unparen(v).(*ast.Ident); ok && id.Name == "nil" && p.info.Uses[id] == types.Universe.Lookup("nil") {
			continue
		}
		if e.field != "" {
			return nil, fmt.Errorf("%s: migration %s sets both Run and RunDB", e.pos, e.label())
		}
		e.field, e.value = field, v
		e.fn, _ = ast.Unparen(v).(*ast.FuncLit)
	}
	if e.field == "" {
		return nil, fmt.Errorf("%s: migration %s sets neither Run nor RunDB", e.pos, e.label())
	}
	return e, nil
}

// namedFunc returns the package-level function an expression names, or nil.
func (p *pkgSource) namedFunc(x ast.Expr) *ast.FuncDecl {
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return nil
	}
	if d := p.decls[p.info.Uses[id]]; d != nil {
		if fd, ok := d.node.(*ast.FuncDecl); ok && fd.Recv == nil {
			return fd
		}
	}
	return nil
}

// reach returns the package-level declarations the nodes reach, directly or
// through other declarations, apart from those in stop.
func (p *pkgSource) reach(stop map[*declInfo]bool, nodes ...ast.Node) map[*declInfo]bool {
	seen := map[*declInfo]bool{}
	queue := append([]ast.Node(nil), nodes...)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		ast.Inspect(n, func(x ast.Node) bool {
			id, ok := x.(*ast.Ident)
			if !ok {
				return true
			}
			obj := p.info.Uses[id]
			if f, ok := obj.(*types.Func); ok {
				obj = f.Origin()
			}
			if d := p.decls[obj]; d != nil && !seen[d] && !stop[d] {
				seen[d] = true
				queue = append(queue, d.node)
			}
			return true
		})
	}
	return seen
}
