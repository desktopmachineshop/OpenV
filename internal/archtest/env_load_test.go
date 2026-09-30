package archtest

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
	"runtime"
	"sort"
	"strings"
	"sync"
)

// This file loads the typed program the env inventory scans (refactor plan
// S8; env_inventory_test.go): `go list -export -deps` once over the module
// root, cmd/... and internal/..., the scope S1 reads, then go/types checks
// every module package from source in go list's dependency order and imports
// everything else from the compiler's export data, as
// cmd/server/boot_steps_test.go does. Unlike S1's syntax rules this builds:
// it needs the go command and the module's dependencies, and a cold build
// cache compiles export data once. Only the build variant of the host's GOOS
// and GOARCH is typed; envHoles checks the files that build leaves out.

// envListed is what `go list -json` says about one package.
type envListed struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Export     string
	Module     *struct{ Path string }
	Deps       []string
	Error      *struct{ Err string }
}

// envListing is the go list output: every package by import path, and the
// import paths in go list's order, where a package follows its dependencies.
type envListing struct {
	pkgs  map[string]envListed
	order []string
}

var envList struct {
	once sync.Once
	l    *envListing
	err  error
}

// envGoTool is the go command that runs this test.
func envGoTool() string {
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return "go"
}

// listEnvPackages runs go list once per test process.
func listEnvPackages(root string) (*envListing, error) {
	envList.once.Do(func() {
		cmd := exec.Command(envGoTool(), "list", "-e", "-export", "-deps",
			"-json=ImportPath,Name,Dir,GoFiles,CgoFiles,Export,Module,Deps,Error", ".", "./cmd/...", "./internal/...")
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			envList.err = fmt.Errorf("go list -export -deps . ./cmd/... ./internal/...: %v\n%s", err, stderr.String())
			return
		}
		l := &envListing{pkgs: map[string]envListed{}}
		dec := json.NewDecoder(bytes.NewReader(out))
		for dec.More() {
			var p envListed
			if err := dec.Decode(&p); err != nil {
				envList.err = fmt.Errorf("decode go list output: %v", err)
				return
			}
			if p.Error != nil && len(p.GoFiles) > 0 {
				envList.err = fmt.Errorf("go list: %s: %s", p.ImportPath, p.Error.Err)
				return
			}
			l.pkgs[p.ImportPath] = p
			l.order = append(l.order, p.ImportPath)
		}
		envList.l = l
	})
	return envList.l, envList.err
}

// envPkg is one module package, type-checked from source.
type envPkg struct {
	path  string // import path
	name  string // module-relative directory, rootName for the root, as S1 names packages
	files []*ast.File
	info  *types.Info
	types *types.Package
	// rangeOnly holds the package-level variables whose every use is the
	// operand of a range statement (filled on first use by envRangeOnly).
	rangeOnly map[*types.Var]bool
}

// envProgram is the typed module the scan reads.
type envProgram struct {
	fset  *token.FileSet
	pkgs  []*envPkg           // in dependency order
	bins  map[string][]string // package name -> the cmd/ binaries that link it
	typed map[string]bool     // module-relative, slash-separated files the typed build holds
}

// envImporter imports a package checked from source when it has one, and
// anything else from the compiler's export data.
type envImporter struct {
	gc  types.Importer
	src map[string]*types.Package
}

func newEnvImporter(fset *token.FileSet, l *envListing) *envImporter {
	gc := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		p, ok := l.pkgs[path]
		if !ok || p.Export == "" {
			return nil, fmt.Errorf("no export data for %s (not a dependency of the module's packages)", path)
		}
		return os.Open(p.Export)
	})
	return &envImporter{gc: gc, src: map[string]*types.Package{}}
}

func (im *envImporter) Import(path string) (*types.Package, error) {
	if p, ok := im.src[path]; ok {
		return p, nil
	}
	return im.gc.Import(path)
}

// check type-checks one package from its parsed files and makes it
// importable from source by the packages checked after it.
func (im *envImporter) check(fset *token.FileSet, path, name string, files []*ast.File) (*envPkg, error) {
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: im}
	tp, err := conf.Check(path, fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("type-check %s: %v", path, err)
	}
	im.src[path] = tp
	return &envPkg{path: path, name: name, files: files, info: info, types: tp}, nil
}

// loadEnvProgram lists and type-checks the module rooted at root, whose
// module path is module.
func loadEnvProgram(root, module string) (*envProgram, error) {
	l, err := listEnvPackages(root)
	if err != nil {
		return nil, err
	}
	prog := &envProgram{fset: token.NewFileSet(), bins: map[string][]string{}, typed: map[string]bool{}}
	im := newEnvImporter(prog.fset, l)
	name := func(ip string) string {
		if rel := strings.TrimPrefix(strings.TrimPrefix(ip, module), "/"); rel != "" {
			return rel
		}
		return rootName
	}
	for _, ip := range l.order {
		p := l.pkgs[ip]
		if p.Module == nil || p.Module.Path != module || len(p.GoFiles) == 0 {
			continue
		}
		if len(p.CgoFiles) > 0 {
			return nil, fmt.Errorf("%s has cgo files, which the env scan cannot type-check from source", ip)
		}
		var files []*ast.File
		for _, f := range p.GoFiles {
			rel, err := filepath.Rel(root, filepath.Join(p.Dir, f))
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			src, err := os.ReadFile(filepath.Join(p.Dir, f))
			if err != nil {
				return nil, err
			}
			af, err := parser.ParseFile(prog.fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			files = append(files, af)
			prog.typed[rel] = true
		}
		ep, err := im.check(prog.fset, ip, name(ip), files)
		if err != nil {
			return nil, err
		}
		prog.pkgs = append(prog.pkgs, ep)
		if p.Name == "main" && strings.HasPrefix(ip, module+"/cmd/") {
			for _, dep := range append([]string{ip}, p.Deps...) {
				if dep == module || strings.HasPrefix(dep, module+"/") {
					prog.bins[name(dep)] = append(prog.bins[name(dep)], name(ip))
				}
			}
		}
	}
	for _, b := range prog.bins {
		sort.Strings(b)
	}
	return prog, nil
}
