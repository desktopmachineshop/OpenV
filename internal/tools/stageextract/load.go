package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// pkgSource is the package being split, type-checked from source against
// the compiler's export data of everything it imports.
type pkgSource struct {
	root, dir string // module root and package directory, absolute
	rel       string // dir relative to root, slash-separated
	fset      *token.FileSet
	files     []*srcFile // the non-test files the go command builds
	tests     []*srcFile // its _test.go files of the same package clause
	types     *types.Package
	info      *types.Info
	imp       types.Importer
}

type srcFile struct {
	name string // base name
	src  []byte
	ast  *ast.File
	tf   *token.File
}

// listedPkg is what `go list -export -json` says about one package.
type listedPkg struct {
	ImportPath   string
	Name         string
	Dir          string
	GoFiles      []string
	TestGoFiles  []string
	IgnoredFiles []string
	Export       string
	Error        *struct{ Err string }
}

// loadPackage lists the package at dir and its dependencies, with the
// export data the compiler writes for each, and type-checks the package
// from its source files.
func loadPackage(dir string) (*pkgSource, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err := moduleRoot(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	cmd := exec.Command(goTool(), "list", "-export", "-deps",
		"-json=ImportPath,Name,Dir,GoFiles,TestGoFiles,IgnoredFiles,Export,Error", "./"+rel)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export -deps ./%s: %v\n%s", rel, err, strings.TrimSpace(stderr.String()))
	}
	deps := map[string]listedPkg{}
	var target *listedPkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p listedPkg
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decode go list output: %v", err)
		}
		if p.Error != nil {
			return nil, fmt.Errorf("go list: %s: %s", p.ImportPath, p.Error.Err)
		}
		deps[p.ImportPath] = p
		if sameDir(p.Dir, abs) {
			p := p
			target = &p
		}
	}
	if target == nil {
		return nil, fmt.Errorf("%w: go list did not list ./%s", errNoSource, rel)
	}
	if len(target.GoFiles) == 0 {
		return nil, fmt.Errorf("%w: ./%s holds no Go file the go command builds", errNoSource, rel)
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		p, ok := deps[path]
		if !ok || p.Export == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(p.Export)
	})
	ps := &pkgSource{root: root, dir: abs, rel: rel, fset: fset, imp: imp}
	for _, name := range target.GoFiles {
		f, err := ps.parse(name, nil)
		if err != nil {
			return nil, err
		}
		ps.files = append(ps.files, f)
	}
	for _, name := range target.TestGoFiles {
		f, err := ps.parse(name, nil)
		if err != nil {
			return nil, err
		}
		ps.tests = append(ps.tests, f)
	}
	if err := ps.check(target.ImportPath); err != nil {
		return nil, err
	}
	return ps, nil
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// parse reads (or takes) one file of the package and parses it with its
// comments and the parser's object resolution.
func (ps *pkgSource) parse(name string, src []byte) (*srcFile, error) {
	if src == nil {
		b, err := os.ReadFile(filepath.Join(ps.dir, name))
		if err != nil {
			return nil, err
		}
		src = b
	}
	af, err := parser.ParseFile(ps.fset, filepath.Join(ps.dir, name), src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &srcFile{name: name, src: src, ast: af, tf: ps.fset.File(af.Pos())}, nil
}

// check type-checks ps.files.
func (ps *pkgSource) check(path string) error {
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	var files []*ast.File
	for _, f := range ps.files {
		files = append(files, f.ast)
	}
	var errs []string
	conf := types.Config{Importer: ps.imp, Error: func(err error) { errs = append(errs, err.Error()) }}
	tp, _ := conf.Check(path, ps.fset, files, info)
	if len(errs) > 0 {
		if len(errs) > 10 {
			errs = append(errs[:10], fmt.Sprintf("... and %d more", len(errs)-10))
		}
		return fmt.Errorf("type-check %s:\n  %s", path, strings.Join(errs, "\n  "))
	}
	ps.types, ps.info = tp, info
	return nil
}

// recheck type-checks the package as it would be with files replaced or
// added (name -> source), reusing the importer: the package's
// dependencies do not change.
func (ps *pkgSource) recheck(files map[string][]byte) error {
	next := &pkgSource{root: ps.root, dir: ps.dir, rel: ps.rel, fset: token.NewFileSet(), imp: nil}
	next.imp = ps.imp
	var names []string
	for _, f := range ps.files {
		if _, ok := files[f.name]; !ok {
			names = append(names, f.name)
		}
	}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := next.parse(name, files[name])
		if err != nil {
			return err
		}
		next.files = append(next.files, f)
	}
	// The importer resolves dependencies through its own file set, which is
	// ps.fset; types from it carry no positions this check reports.
	return next.check(ps.types.Path())
}

func (ps *pkgSource) file(name string) *srcFile {
	for _, f := range ps.files {
		if f.name == name {
			return f
		}
	}
	return nil
}

// goTool is the go command on PATH, as the other refactor tools run it.
func goTool() string { return "go" }

// moduleRoot is the directory holding the go.mod above dir.
func moduleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("%w: no go.mod above %s", errNoSource, abs)
		}
		d = parent
	}
}

// packageDir resolves a spec's package against the module root holding the
// working directory; an absolute path is used as it is.
func packageDir(pkg string) string {
	if filepath.IsAbs(pkg) {
		return pkg
	}
	if root, err := moduleRoot("."); err == nil {
		return filepath.Join(root, filepath.FromSlash(pkg))
	}
	return pkg
}

// goVet runs go vet on the package directory from its module root.
func goVet(dir string) error {
	root, err := moduleRoot(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	cmd := exec.Command(goTool(), "vet", "./"+filepath.ToSlash(rel))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go vet ./%s: %v\n%s", filepath.ToSlash(rel), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var errNoSource = errors.New("no Go source")
