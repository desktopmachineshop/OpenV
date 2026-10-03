package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The checks of -flatten -base that need types (S14c). Each side's
// production files that build here are type-checked from source, against
// the export data of the packages they import as the go command builds
// them where movecheck runs (for M4, the working tree's, which the split
// leaves alone). Then:
//
//   - a name the normalisation reads as one variable must have one type on
//     both sides: each field of the stages' receiver type and each
//     top-level local of the function and of its stages, by name, has the
//     type its local had (a field typed as a concrete pointer where its
//     local was an interface turns a nil into a non-nil interface; a var a
//     stage declares, which the normalisation drops, must declare the
//     local's type);
//   - a local declared at the top of two of the function's bodies must not
//     have its address taken implicitly either (addressedVar: a pointer
//     method called or taken as a value on it or a part of it, an array of
//     it sliced): the method can keep a pointer to one body's copy, which
//     the other body's writes no longer reach.

// typedSide is one side of the comparison, type-checked.
type typedSide struct {
	label string
	fset  *token.FileSet
	files []*ast.File
	names []string // each file's base name
	pkg   *types.Package
	info  *types.Info
}

// typedChecks type-checks both sides and runs the checks above. It returns
// what fails them, or an error when a side does not type-check.
func typedChecks(dir string, base, head *pkgInfo, baseName, headName, fn, recv string) (string, error) {
	sides := []*typedSide{{label: baseName}, {label: headName}}
	imports := map[string]bool{}
	for i, p := range []*pkgInfo{base, head} {
		if err := sides[i].parse(dir, p, imports); err != nil {
			return "", err
		}
	}
	exports, err := exportData(dir, imports)
	if err != nil {
		return "", err
	}
	for _, ts := range sides {
		if err := ts.check(exports); err != nil {
			return "", err
		}
	}
	var msgs []string
	for _, ts := range sides {
		msgs = append(msgs, ts.addressed(fn)...)
	}
	msgs = append(msgs, typeDiffs(sides[0], sides[1], fn, recv)...)
	return strings.Join(msgs, "\n"), nil
}

// parse parses the side's production files that the go command builds
// here, and adds what they import to imports.
func (ts *typedSide) parse(dir string, p *pkgInfo, imports map[string]bool) error {
	srcs := map[string][]byte{}
	for _, f := range p.files {
		if f.set == "" {
			srcs[f.name] = f.src
		}
	}
	ctxt := build.Default
	ctxt.OpenFile = func(path string) (io.ReadCloser, error) {
		src, ok := srcs[filepath.Base(path)]
		if !ok {
			return nil, os.ErrNotExist
		}
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	var names []string
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)
	ts.fset = token.NewFileSet()
	for _, name := range names {
		if ok, err := ctxt.MatchFile(dir, name); err != nil || !ok {
			continue
		}
		af, err := parser.ParseFile(ts.fset, name, srcs[name], parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ts.files = append(ts.files, af)
		ts.names = append(ts.names, name)
		for _, is := range af.Imports {
			if path, err := strconv.Unquote(is.Path.Value); err == nil && path != "unsafe" {
				imports[path] = true
			}
		}
	}
	return nil
}

// exportData lists the export data of the imported packages and their
// dependencies, as the go command builds them from dir.
func exportData(dir string, imports map[string]bool) (map[string]string, error) {
	out := map[string]string{}
	if len(imports) == 0 {
		return out, nil
	}
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Export,Error"}
	for path := range imports {
		args = append(args, path)
	}
	sort.Strings(args[4:])
	cmd := exec.Command("go", args...)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	list, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export -deps of what %s imports: %v\n%s", dir, err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(list))
	for dec.More() {
		var p struct {
			ImportPath, Export string
			Error              *struct{ Err string }
		}
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decode go list output: %v", err)
		}
		if p.Error != nil {
			return nil, fmt.Errorf("go list: %s: %s", p.ImportPath, p.Error.Err)
		}
		out[p.ImportPath] = p.Export
	}
	return out, nil
}

// check type-checks the side against the export data.
func (ts *typedSide) check(exports map[string]string) error {
	if len(ts.files) == 0 {
		return fmt.Errorf("%s: no production file to type-check", ts.label)
	}
	imp := importer.ForCompiler(ts.fset, "gc", func(path string) (io.ReadCloser, error) {
		if e := exports[path]; e != "" {
			return os.Open(e)
		}
		return nil, fmt.Errorf("no export data for %s", path)
	})
	ts.info = &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var errs []string
	conf := types.Config{Importer: imp, Error: func(err error) { errs = append(errs, err.Error()) }}
	ts.pkg, _ = conf.Check(ts.files[0].Name.Name, ts.fset, ts.files, ts.info)
	if len(errs) > 0 {
		if len(errs) > 10 {
			errs = append(errs[:10], fmt.Sprintf("... and %d more", len(errs)-10))
		}
		return fmt.Errorf("%s does not type-check against the packages it imports as they build here, "+
			"so the types of its fields and locals cannot be compared:\n  %s", ts.label, strings.Join(errs, "\n  "))
	}
	return nil
}

// bodies are the function and its stages, the methods of its wire_*.go
// files, as flatten finds them.
func (ts *typedSide) bodies(fn string) []*ast.FuncDecl {
	var target *ast.FuncDecl
	var stages []*ast.FuncDecl
	for i, af := range ts.files {
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			switch {
			case funcKey(fd) == fn:
				target = fd
			case fd.Recv != nil && strings.HasPrefix(ts.names[i], "wire_"):
				stages = append(stages, fd)
			}
		}
	}
	if target == nil {
		return nil
	}
	return append([]*ast.FuncDecl{target}, stages...)
}

// topVars are the variables fd's top-level statements declare.
func (ts *typedSide) topVars(fd *ast.FuncDecl) []*types.Var {
	var out []*types.Var
	for _, s := range fd.Body.List {
		var ids []*ast.Ident
		switch x := s.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, e := range x.Lhs {
					if id, ok := e.(*ast.Ident); ok {
						ids = append(ids, id)
					}
				}
			}
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, sp := range gd.Specs {
					ids = append(ids, sp.(*ast.ValueSpec).Names...)
				}
			}
		}
		for _, id := range ids {
			if v, ok := ts.info.Defs[id].(*types.Var); ok && id.Name != "_" {
				out = append(out, v)
			}
		}
	}
	return out
}

// addressed describes each local declared at the top of two or more of
// the side's bodies whose address one of them takes implicitly (or
// explicitly, which refuse has already failed).
func (ts *typedSide) addressed(fn string) []string {
	bodies := ts.bodies(fn)
	declaredIn := map[string]int{}
	for _, fd := range bodies {
		for _, v := range ts.topVars(fd) {
			declaredIn[v.Name()]++
		}
	}
	var out []string
	for _, fd := range bodies {
		mine := map[*types.Var]bool{}
		for _, v := range ts.topVars(fd) {
			mine[v] = declaredIn[v.Name()] > 1
		}
		seen := map[*types.Var]bool{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			v := addressedVar(ts.info, e)
			if v == nil || !mine[v] || seen[v] {
				return true
			}
			seen[v] = true
			what := "its address is taken"
			switch e.(type) {
			case *ast.SelectorExpr:
				what = "a method with a pointer receiver takes its address"
			case *ast.SliceExpr:
				what = "slicing it takes its address"
			}
			out = append(out, fmt.Sprintf("%s: %s is declared at the top of %d of %s's bodies, and %s in %s (%s): "+
				"it would not see what the others write to their own %s; make it a field of the stages' receiver",
				ts.fset.Position(e.Pos()), v.Name(), declaredIn[v.Name()], fn, what, funcKey(fd), ts.label, v.Name()))
			return true
		})
	}
	return out
}

// typesByName lists the types of the names the normalisation reads as
// one variable: the fields of the stages' receiver type, and the
// top-level locals of the function and its stages but the receiver and
// the cleanups the function binds a stage's results to.
func (ts *typedSide) typesByName(fn, recv string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	qualify := func(p *types.Package) string {
		if p == ts.pkg {
			return ""
		}
		return p.Path()
	}
	add := func(name string, t types.Type) {
		if out[name] == nil {
			out[name] = map[string]bool{}
		}
		out[name][types.TypeString(t, qualify)] = true
	}
	bodies := ts.bodies(fn)
	if len(bodies) == 0 {
		return out
	}
	stageNames := map[string]bool{}
	for _, fd := range bodies[1:] {
		stageNames[fd.Name.Name] = true
	}
	skip := map[*types.Var]bool{}
	for _, s := range bodies[0].Body.List {
		a, ok := s.(*ast.AssignStmt)
		if !ok || a.Tok != token.DEFINE || len(a.Rhs) != 1 {
			continue
		}
		if typ := appType(a, recv); typ != "" {
			if v, ok := ts.info.Defs[a.Lhs[0].(*ast.Ident)].(*types.Var); ok {
				skip[v] = true
			}
			if tn, ok := ts.pkg.Scope().Lookup(typ).(*types.TypeName); ok {
				if st, ok := tn.Type().Underlying().(*types.Struct); ok {
					for i := 0; i < st.NumFields(); i++ {
						add(st.Field(i).Name(), st.Field(i).Type())
					}
				}
			}
			continue
		}
		if c, ok := a.Rhs[0].(*ast.CallExpr); ok {
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && stageNames[sel.Sel.Name] {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == recv {
					for _, e := range a.Lhs {
						if id, ok := e.(*ast.Ident); ok {
							if v, ok := ts.info.Defs[id].(*types.Var); ok {
								skip[v] = true
							}
						}
					}
				}
			}
		}
	}
	for _, fd := range bodies {
		for _, v := range ts.topVars(fd) {
			if !skip[v] {
				add(v.Name(), v.Type())
			}
		}
	}
	return out
}

// appType is T in recv := &T{}, or "".
func appType(a *ast.AssignStmt, recv string) string {
	if len(a.Lhs) != 1 {
		return ""
	}
	if id, ok := a.Lhs[0].(*ast.Ident); !ok || id.Name != recv {
		return ""
	}
	u, ok := a.Rhs[0].(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return ""
	}
	cl, ok := u.X.(*ast.CompositeLit)
	if !ok || len(cl.Elts) > 0 {
		return ""
	}
	if id, ok := cl.Type.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// typeDiffs describes each name both sides read as one variable with
// another type on one side than on the other.
func typeDiffs(base, head *typedSide, fn, recv string) []string {
	b, h := base.typesByName(fn, recv), head.typesByName(fn, recv)
	var out []string
	for name, ht := range h {
		bt, ok := b[name]
		if !ok || fmt.Sprint(sortedSet(bt)) == fmt.Sprint(sortedSet(ht)) {
			continue
		}
		out = append(out, fmt.Sprintf("%s is %s in %s but %s in %s: a field of the stages' receiver keeps the type of the local it was "+
			"made from, and a local a stage declares the type the function gave it", name, strings.Join(sortedSet(ht), " and "), head.label,
			strings.Join(sortedSet(bt), " and "), base.label))
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
