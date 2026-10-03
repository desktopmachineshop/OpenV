package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

const (
	handlerName = "Handler"
	depsName    = "HandlerDeps"
	newName     = "NewHandler"
)

// copied is one Handler field NewHandler sets to a HandlerDeps field as it
// is.
type copied struct {
	field   *types.Var // the Handler field
	dep     *types.Var // the HandlerDeps field it copies
	decl    *ast.Field // its declaration in Handler
	elt     ast.Expr   // its element in NewHandler's literal
	renames int        // selectors renamed
}

// plan is the rewrite, worked out from the package as it is.
type plan struct {
	ps       *pkgSource
	handler  *types.Named
	deps     *types.Named
	st       *ast.StructType // Handler's declaration
	lit      *ast.CompositeLit
	stmt     *ast.AssignStmt // h := &Handler{...}
	hName    string
	depsName string
	param    *types.Var // NewHandler's HandlerDeps parameter
	copies   []*copied  // in Handler's field order
	kept     []string   // the other Handler fields, in order
	renames  []rename
}

// rename is one selector whose field name changes.
type rename struct {
	sel *ast.Ident
	to  string
}

func makePlan(ps *pkgSource) (*plan, error) {
	p := &plan{ps: ps}
	var err error
	if p.handler, p.st, err = p.structNamed(handlerName); err != nil {
		return nil, err
	}
	if p.deps, _, err = p.structNamed(depsName); err != nil {
		return nil, err
	}
	hs := p.handler.Underlying().(*types.Struct)
	for i := 0; i < hs.NumFields(); i++ {
		if f := hs.Field(i); f.Embedded() && types.Identical(f.Type(), p.deps) {
			return nil, errDone
		}
	}
	if err := p.findLiteral(); err != nil {
		return nil, err
	}
	if err := p.findCopies(); err != nil {
		return nil, err
	}
	if err := p.checkCollisions(); err != nil {
		return nil, err
	}
	if err := p.findSelectors(); err != nil {
		return nil, err
	}
	return p, nil
}

// structNamed finds the package's struct type name and its declaration.
func (p *plan) structNamed(name string) (*types.Named, *ast.StructType, error) {
	obj, _ := p.ps.types.Scope().Lookup(name).(*types.TypeName)
	if obj == nil || obj.IsAlias() {
		return nil, nil, refuse("%s declares no type %s", p.ps.rel, name)
	}
	named, ok := obj.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 {
		return nil, nil, refuse("%s is not a plain named type", name)
	}
	if _, ok := named.Underlying().(*types.Struct); !ok {
		return nil, nil, refuse("%s is not a struct", name)
	}
	for _, f := range p.ps.files {
		for _, d := range f.ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				if p.ps.info.Defs[ts.Name] == obj {
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						return nil, nil, refuse("%s is not declared as a struct literal type", name)
					}
					return named, st, nil
				}
			}
		}
	}
	return nil, nil, refuse("no declaration of %s", name)
}

// findLiteral finds NewHandler(deps HandlerDeps) *Handler and its one
// `h := &Handler{...}` statement.
func (p *plan) findLiteral() error {
	fn, _ := p.ps.types.Scope().Lookup(newName).(*types.Func)
	if fn == nil {
		return refuse("%s declares no func %s", p.ps.rel, newName)
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 1 || !types.Identical(sig.Params().At(0).Type(), p.deps) ||
		sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), types.NewPointer(p.handler)) {
		return refuse("%s is not func(%s) *%s", newName, depsName, handlerName)
	}
	param := sig.Params().At(0)
	if param.Name() == "" || param.Name() == "_" {
		return refuse("%s's parameter has no name", newName)
	}
	p.depsName, p.param = param.Name(), param
	var decl *ast.FuncDecl
	for _, f := range p.ps.files {
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && p.ps.info.Defs[fd.Name] == fn {
				decl = fd
			}
		}
	}
	if decl == nil || decl.Body == nil {
		return refuse("%s has no body", newName)
	}
	var lits []*ast.CompositeLit
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok && types.Identical(p.ps.info.Types[cl].Type, p.handler) {
			lits = append(lits, cl)
		}
		return true
	})
	if len(lits) != 1 {
		return refuse("%s builds %d %s literals; the rewrite needs exactly one", newName, len(lits), handlerName)
	}
	p.lit = lits[0]
	for _, s := range decl.Body.List {
		as, ok := s.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			continue
		}
		if u, ok := as.Rhs[0].(*ast.UnaryExpr); ok && u.Op == token.AND && u.X == ast.Expr(p.lit) {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
				p.stmt, p.hName = as, id.Name
			}
		}
	}
	if p.stmt == nil {
		return refuse("%s's literal is not the statement `h := &%s{...}` in its body", newName, handlerName)
	}
	return nil
}

// findCopies sorts the literal's elements into pure copies and derived
// values, and finds each copy's declaration in Handler.
func (p *plan) findCopies() error {
	byField := map[*types.Var]*copied{}
	byDep := map[*types.Var]*copied{}
	for _, e := range p.lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return refuse("%s's literal has an unkeyed element at %s", newName, p.ps.where(e.Pos()))
		}
		key, _ := kv.Key.(*ast.Ident)
		field, _ := p.ps.info.Uses[key].(*types.Var)
		if field == nil {
			return refuse("%s's literal has an element with no field key at %s", newName, p.ps.where(e.Pos()))
		}
		dep := p.depField(kv.Value)
		if dep == nil || !types.Identical(field.Type(), dep.Type()) {
			continue // derived
		}
		if other := byDep[dep]; other != nil {
			return refuse("%s copies %s.%s into both %s and %s", newName, depsName, dep.Name(), other.field.Name(), field.Name())
		}
		c := &copied{field: field, dep: dep, elt: kv}
		byField[field], byDep[dep] = c, c
	}
	for _, f := range p.st.Fields.List {
		for _, n := range f.Names {
			v, _ := p.ps.info.Defs[n].(*types.Var)
			c := byField[v]
			if c == nil {
				p.kept = append(p.kept, n.Name)
				continue
			}
			if len(f.Names) != 1 {
				return refuse("%s declares %s together with other fields at %s; give it a line of its own",
					handlerName, n.Name, p.ps.where(n.Pos()))
			}
			c.decl = f
			p.copies = append(p.copies, c)
		}
		if len(f.Names) == 0 {
			p.kept = append(p.kept, types.ExprString(f.Type))
		}
	}
	if len(p.copies) == 0 {
		return refuse("%s copies no %s field as it is", newName, depsName)
	}
	return nil
}

// depField is the HandlerDeps field e reads when e is exactly `deps.F`.
func (p *plan) depField(e ast.Expr) *types.Var {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	if p.ps.info.Uses[x] != p.param {
		return nil
	}
	s := p.ps.info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal || len(s.Index()) != 1 || !types.Identical(s.Recv(), p.deps) {
		return nil
	}
	v, _ := s.Obj().(*types.Var)
	return v
}

// checkCollisions refuses a HandlerDeps that would bring more than its
// fields into Handler, or a field whose promoted name a Handler field or
// method that stays would shadow.
func (p *plan) checkCollisions() error {
	ds := p.deps.Underlying().(*types.Struct)
	if ms := types.NewMethodSet(types.NewPointer(p.deps)); ms.Len() > 0 {
		return refuse("%s has methods (%s), which embedding would promote into %s", depsName,
			ms.At(0).Obj().Name(), handlerName)
	}
	taken := map[string]string{}
	for _, name := range p.kept {
		taken[name] = "field"
	}
	ms := types.NewMethodSet(types.NewPointer(p.handler))
	for i := 0; i < ms.Len(); i++ {
		taken[ms.At(i).Obj().Name()] = "method"
	}
	if what := taken[depsName]; what != "" {
		return refuse("%s already has a %s named %s", handlerName, what, depsName)
	}
	for i := 0; i < ds.NumFields(); i++ {
		f := ds.Field(i)
		if f.Embedded() {
			return refuse("%s embeds %s, which embedding would promote into %s", depsName, f.Name(), handlerName)
		}
		if what := taken[f.Name()]; what != "" {
			return refuse("%s's %s %s would shadow the promoted %s.%s", handlerName, what, f.Name(), depsName, f.Name())
		}
	}
	return nil
}

// findSelectors finds every reference to a copied field: a selector whose
// receiver is a Handler or *Handler is renamed; NewHandler's literal keys
// go with their elements; anything else is refused.
func (p *plan) findSelectors() error {
	byField := map[types.Object]*copied{}
	for _, c := range p.copies {
		byField[c.field] = c
	}
	done := map[*ast.Ident]bool{}
	for _, e := range p.lit.Elts {
		done[e.(*ast.KeyValueExpr).Key.(*ast.Ident)] = true
	}
	var bad []string
	for _, f := range p.ps.files {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			s := p.ps.info.Selections[sel]
			if s == nil || s.Kind() != types.FieldVal {
				return true
			}
			c := byField[s.Obj()]
			if c == nil {
				return true
			}
			done[sel.Sel] = true
			recv := s.Recv()
			if ptr, ok := recv.(*types.Pointer); ok {
				recv = ptr.Elem()
			}
			if len(s.Index()) != 1 || !types.Identical(recv, p.handler) {
				bad = append(bad, p.ps.where(sel.Sel.Pos())+": "+types.ExprString(sel)+" reaches "+handlerName+"."+
					c.field.Name()+" through another type's embedding")
				return true
			}
			p.renames = append(p.renames, rename{sel: sel.Sel, to: c.dep.Name()})
			c.renames++
			return true
		})
	}
	for id, obj := range p.ps.info.Uses {
		if c := byField[obj]; c != nil && !done[id] {
			bad = append(bad, p.ps.where(id.Pos())+": "+handlerName+"."+c.field.Name()+
				" is named outside a selector (a composite literal key?); build the Handler with NewHandler or newTestHandler")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return refuse("a copied field is used in a way the rename cannot carry:\n  %s", strings.Join(bad, "\n  "))
	}
	sort.Slice(p.renames, func(i, j int) bool { return p.renames[i].sel.Pos() < p.renames[j].sel.Pos() })
	return nil
}
