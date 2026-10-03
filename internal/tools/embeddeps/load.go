package main

import (
	"bytes"
	"encoding/json"
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
	"strings"
)

// pkgSource is the package being rewritten: its production files and its
// in-package test files, type-checked together from source against the
// compiler's export data of everything they import.
type pkgSource struct {
	dir   string // package directory, absolute
	rel   string // dir relative to the module root, slash-separated
	path  string // import path
	files []*srcFile
	fset  *token.FileSet
	imp   types.Importer
	types *types.Package
	info  *types.Info
}

type srcFile struct {
	name string // base name
	src  []byte
	ast  *ast.File
	tf   *token.File
}

// listedPkg is what `go list -export -json` says about one package.
type listedPkg struct {
	ImportPath     string
	Dir            string
	GoFiles        []string
	TestGoFiles    []string
	IgnoredGoFiles []string
	Export         string
	Error          *struct{ Err string }
}

// loadPackage lists the package at dir with its dependencies and its tests'
// dependencies, with the export data the compiler writes for each, and
// type-checks the package's files and in-package test files from source.
// External test files (package x_test) cannot name the package's
// unexported fields, so they are not read.
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
	cmd := exec.Command("go", "list", "-export", "-deps", "-test",
		"-json=ImportPath,Dir,GoFiles,TestGoFiles,IgnoredGoFiles,Export,Error", "./"+rel)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export -deps -test ./%s: %v\n%s", rel, err, strings.TrimSpace(stderr.String()))
	}
	exports := map[string]string{}
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
		// A test variant ("p [q.test]") is a package recompiled for q's tests
		// because it imports q; the in-package tests read here cannot import
		// such a package, so only the plain entries are needed. "q.test" is
		// the generated test main, in q's directory.
		if strings.Contains(p.ImportPath, " ") || strings.HasSuffix(p.ImportPath, ".test") {
			continue
		}
		exports[p.ImportPath] = p.Export
		if sameDir(p.Dir, abs) {
			p := p
			target = &p
		}
	}
	if target == nil || len(target.GoFiles) == 0 {
		return nil, fmt.Errorf("./%s holds no Go file the go command builds", rel)
	}
	if len(target.IgnoredGoFiles) > 0 {
		return nil, refuse("./%s has files this platform does not build (%s), which the rewrite would not read",
			rel, strings.Join(target.IgnoredGoFiles, ", "))
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		export := exports[path]
		if export == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(export)
	})
	ps := &pkgSource{dir: abs, rel: rel, path: target.ImportPath, imp: imp}
	srcs := map[string][]byte{}
	for _, name := range append(append([]string{}, target.GoFiles...), target.TestGoFiles...) {
		b, err := os.ReadFile(filepath.Join(abs, name))
		if err != nil {
			return nil, err
		}
		srcs[name] = b
	}
	names := append(append([]string{}, target.GoFiles...), target.TestGoFiles...)
	if err := ps.check(names, srcs); err != nil {
		return nil, err
	}
	return ps, nil
}

// check parses the named files from srcs and type-checks them as the
// package, replacing ps's files, type information and file set.
func (ps *pkgSource) check(names []string, srcs map[string][]byte) error {
	fset := token.NewFileSet()
	var files []*srcFile
	var asts []*ast.File
	for _, name := range names {
		af, err := parser.ParseFile(fset, filepath.Join(ps.dir, name), srcs[name], parser.ParseComments)
		if err != nil {
			return err
		}
		files = append(files, &srcFile{name: name, src: srcs[name], ast: af, tf: fset.File(af.Pos())})
		asts = append(asts, af)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var errs []string
	conf := types.Config{Importer: ps.imp, Error: func(err error) { errs = append(errs, err.Error()) }}
	tp, _ := conf.Check(ps.path, fset, asts, info)
	if len(errs) > 0 {
		if len(errs) > 10 {
			errs = append(errs[:10], fmt.Sprintf("... and %d more", len(errs)-10))
		}
		return fmt.Errorf("type-check %s:\n  %s", ps.path, strings.Join(errs, "\n  "))
	}
	ps.files, ps.fset, ps.types, ps.info = files, fset, tp, info
	return nil
}

// fileNames is the package's files in the order they were read.
func (ps *pkgSource) fileNames() []string {
	names := make([]string, len(ps.files))
	for i, f := range ps.files {
		names[i] = f.name
	}
	return names
}

// fileOf is the file holding pos.
func (ps *pkgSource) fileOf(pos token.Pos) *srcFile {
	for _, f := range ps.files {
		if f.tf.Base() <= int(pos) && int(pos) <= f.tf.Base()+f.tf.Size() {
			return f
		}
	}
	return nil
}

// where is pos as file:line.
func (ps *pkgSource) where(pos token.Pos) string {
	p := ps.fset.Position(pos)
	return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// moduleRoot is the directory holding the go.mod above dir.
func moduleRoot(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		d = parent
	}
}
