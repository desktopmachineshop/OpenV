package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

// The normalisation -flatten -base applies to both sides before comparing
// them (refactor plan S14c, the proof of M4): it undoes what
// internal/tools/stageextract does to a function it splits into stages,
// and nothing else, so that a faithful split compares equal and any other
// difference is left to fail.
//
//   - The recv := &T{} statement that makes the stages' receiver goes, and
//     T's fields are the side's renamed locals.
//   - recv.x, for a field x of T, reads as x, whether recv is the
//     function's local or a stage's receiver (which flatten renamed to
//     recv).
//   - At the top level, an = that writes a field no earlier statement
//     mentions reads as :=, since the field's first write was the local's
//     declaration.
//   - var x T, for a field x, goes (the field starts as T's zero value);
//     var x T = v reads as x := v.
//   - var v T followed at once by an = or := that writes v and does not
//     read it goes: a stage declares a local the function had declared in
//     an earlier range (err), or a name it declares beside a field.
//   - c := recv.stage() inlined is c := <the stage's returned expression>;
//     when the statements right after it are defer c() for each name it
//     binds, and nothing else uses them, it goes and the defers call the
//     returned expressions: stop := a.stop; defer stop() reads as defer
//     stop(), closeDB := a.db.Close; defer closeDB() as defer db.Close().
//
// Text alone could hide a change of binding, so each identifier also
// carries what it names: a package-level or predeclared name, a local or
// field of the function's top level, a local of a nested block or function
// literal, or the receiver. Both sides must agree on those too. And the
// shapes the text cannot show are refused outright: a local declared at
// the top level of a stage (or of the function) with the name of a field,
// which would read as that field; and a local declared at the top level of
// two of the function's bodies that a function literal captures, whose
// address is taken, or whose copy one body reads after another body wrote
// its own (normalise_flow.go), each of which would no longer see the other
// body's writes. What needs types, a field or local typed otherwise than
// the base's local and an address a pointer method or slicing takes, is
// checked last (normalise_types.go).

// binding is what an identifier names.
type binding byte

const (
	bindFree   binding = 'F' // package-level, predeclared, or an import
	bindTop    binding = 'T' // a local of the function's top level, or a field
	bindNested binding = 'N' // a local of a nested block or a function literal
	bindRecv   binding = 'R' // the stages' receiver
)

func (b binding) String() string {
	switch b {
	case bindFree:
		return "a package-level or predeclared name"
	case bindTop:
		return "a local of the function's top level (or an app field)"
	case bindNested:
		return "a local of a nested block"
	case bindRecv:
		return "the stages' receiver"
	}
	return "nothing"
}

// side is one flattened function, normalised for comparison.
type side struct {
	fl       *flattener
	list     []ast.Stmt
	app      *ast.AssignStmt // recv := &T{}; nil on a side without stages
	appObj   *ast.Object
	fields   map[string]bool // T's fields
	tags     map[*ast.Ident]binding
	renamed  map[string]bool // fields read as locals
	cleanups int
	bound    map[*ast.Object]bool // the cleanups' names, bound in the function
}

// newSide finds a flattened side's receiver statement and its type's
// fields, and records what every identifier of the function and of its
// stages names.
func newSide(fl *flattener, list []ast.Stmt) *side {
	sd := &side{fl: fl, list: list, fields: map[string]bool{}, tags: map[*ast.Ident]binding{},
		renamed: map[string]bool{}, bound: map[*ast.Object]bool{}}
	for _, s := range list {
		if a, ok := s.(*ast.AssignStmt); ok && sd.isApp(a) {
			sd.app = a
			sd.appObj = a.Lhs[0].(*ast.Ident).Obj
			sd.fields = structFields(fl.p, a.Rhs[0].(*ast.UnaryExpr).X.(*ast.CompositeLit).Type.(*ast.Ident).Name)
			break
		}
	}
	sd.tagFunc(fl.target, nil)
	for _, st := range fl.stages {
		var recv *ast.Object
		if names := st.fd.Recv.List[0].Names; len(names) > 0 {
			recv = names[0].Obj
		}
		sd.tagFunc(st.fd, recv)
	}
	return sd
}

// isApp: recv := &T{}, with an empty literal of a named type.
func (sd *side) isApp(a *ast.AssignStmt) bool {
	if a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return false
	}
	id, ok := a.Lhs[0].(*ast.Ident)
	if !ok || id.Name != sd.fl.recv {
		return false
	}
	u, ok := a.Rhs[0].(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return false
	}
	cl, ok := u.X.(*ast.CompositeLit)
	if !ok || len(cl.Elts) > 0 {
		return false
	}
	_, ok = cl.Type.(*ast.Ident)
	return ok
}

// normalise applies the rules above to a side, with fields the union of
// both sides' fields, and returns its rendered lines.
func (sd *side) normalise(fields map[string]bool) ([]string, error) {
	if err := sd.once(); err != nil {
		return nil, err
	}
	if err := sd.refuse(fields); err != nil {
		return nil, err
	}
	var list []ast.Stmt
	for _, s := range sd.list {
		if sd.app == nil || s != ast.Stmt(sd.app) {
			list = append(list, s)
		}
	}
	list = sd.dropCleanups(list)
	mentioned := map[string]bool{}
	var out []ast.Stmt
	for i := 0; i < len(list); i++ {
		s := list[i]
		sd.rename(s, fields)
		if gd := varDecl(s); gd != nil {
			vs := gd.Specs[0].(*ast.ValueSpec)
			switch {
			case len(vs.Values) == 0 && len(vs.Names) == 1 && writesFirst(nextNonVar(list, i), vs.Names[0]):
				continue // var v T, then v written whole: as if v were declared there
			case len(vs.Values) == 0 && anyIn(vs.Names, fields):
				// A field starts as its zero value; the other names keep a
				// var each, as stageextract writes them.
				out = append(out, sd.keepVars(s.(*ast.DeclStmt), vs, fields)...)
				continue
			case len(vs.Values) > 0 && anyIn(vs.Names, fields):
				s = sd.defineFrom(s.(*ast.DeclStmt), vs)
			}
		}
		if a, ok := s.(*ast.AssignStmt); ok && a.Tok == token.ASSIGN && allIdents(a.Lhs) {
			for _, e := range a.Lhs {
				if id := e.(*ast.Ident); sd.fieldIdent(id, fields) && !mentioned[id.Name] {
					a.Tok = token.DEFINE
				}
			}
		}
		for _, n := range sd.expand(s) {
			ast.Inspect(n, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && sd.fieldIdent(id, fields) {
					mentioned[id.Name] = true
				}
				return true
			})
		}
		out = append(out, s)
	}
	sd.list = out
	return strings.Split(strings.Join(sd.fl.renderAll(out), "\n"), "\n"), nil
}

// once fails a stage inlined more than once: its statements would be the
// same nodes twice, which refuse and the rewrites below must not see
// twice, and a split calls each stage once.
func (sd *side) once() error {
	seen := map[ast.Stmt]bool{}
	for _, s := range sd.list {
		for _, n := range sd.expand(s) {
			st, ok := n.(ast.Stmt)
			if !ok {
				continue
			}
			if seen[st] {
				for _, stage := range sd.fl.stages {
					if stage.fd.Body.Pos() <= st.Pos() && st.End() <= stage.fd.Body.End() {
						return fmt.Errorf("%s is called more than once; a split calls each stage once", funcKey(stage.fd))
					}
				}
				return fmt.Errorf("%s: a statement is inlined more than once", sd.fl.p.fset.Position(st.Pos()))
			}
			seen[st] = true
		}
	}
	return nil
}

// fieldIdent: id reads a field, or on a side without stages the local a
// field was made from: a renamed recv.x, or a top-level local with a
// field's name.
func (sd *side) fieldIdent(id *ast.Ident, fields map[string]bool) bool {
	return sd.renamedIdent(id) || (fields[id.Name] && sd.tags[id] == bindTop)
}

// nextNonVar is the first statement after list[i] that is not a var of one
// name without a value (the vars stageextract writes in a row), or nil.
func nextNonVar(list []ast.Stmt, i int) ast.Stmt {
	for j := i + 1; j < len(list); j++ {
		gd := varDecl(list[j])
		if gd == nil {
			return list[j]
		}
		if vs := gd.Specs[0].(*ast.ValueSpec); len(vs.Values) > 0 || len(vs.Names) != 1 {
			return list[j]
		}
	}
	return nil
}

// keepVars is var x, y T without the fields among its names: a var of its
// own for each other name, as stageextract keeps them.
func (sd *side) keepVars(ds *ast.DeclStmt, vs *ast.ValueSpec, fields map[string]bool) []ast.Stmt {
	var out []ast.Stmt
	gd := ds.Decl.(*ast.GenDecl)
	for _, n := range vs.Names {
		if fields[n.Name] || n.Name == "_" {
			continue
		}
		d := &ast.DeclStmt{Decl: &ast.GenDecl{TokPos: gd.TokPos, Tok: token.VAR,
			Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{n}, Type: vs.Type}}}}
		sd.fl.files[d] = sd.fl.files[ds]
		out = append(out, d)
	}
	return out
}

// renamedMark marks the identifiers rename made from recv.x.
var renamedMark = &ast.Object{Kind: ast.Var, Name: "(an app field)"}

func (sd *side) renamedIdent(id *ast.Ident) bool { return id.Obj == renamedMark }

// rename reads recv.x as x in s, and in the stage statements its nested
// stage calls stand for, and returns the fields it mentions.
func (sd *side) rename(s ast.Stmt, fields map[string]bool) map[string]bool {
	here := map[string]bool{}
	for _, n := range sd.expand(s) {
		replaceIn(n, func(e ast.Expr) ast.Expr {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok || !fields[sel.Sel.Name] {
				return e
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Name != sd.fl.recv || x.Obj == nil || (x.Obj != sd.appObj && sd.tags[x] != bindRecv) {
				return e
			}
			id := &ast.Ident{NamePos: x.NamePos, Name: sel.Sel.Name, Obj: renamedMark}
			sd.tags[id] = bindTop
			sd.renamed[id.Name] = true
			here[id.Name] = true
			return id
		})
		ast.Inspect(n, func(m ast.Node) bool {
			if id, ok := m.(*ast.Ident); ok && sd.renamedIdent(id) {
				here[id.Name] = true
			}
			return true
		})
	}
	return here
}

// expand is s and the statements of every nested stage call in it.
func (sd *side) expand(s ast.Stmt) []ast.Node {
	out := []ast.Node{s}
	for i := 0; i < len(out); i++ {
		ast.Inspect(out[i], func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				for _, st := range sd.fl.placeholders[id.Name] {
					out = append(out, st)
				}
			}
			return true
		})
	}
	return out
}

// dropCleanups drops each inlined c := <returned> whose names the next
// statements only defer, and has those defers call what was returned.
func (sd *side) dropCleanups(list []ast.Stmt) []ast.Stmt {
	var out []ast.Stmt
	for i := 0; i < len(list); i++ {
		a, ok := list[i].(*ast.AssignStmt)
		if !ok || !sd.fl.synth[a] || a.Tok != token.DEFINE || len(a.Lhs) != len(a.Rhs) || i+len(a.Lhs) >= len(list) {
			out = append(out, list[i])
			continue
		}
		var defers []*ast.DeferStmt
		for k, e := range a.Lhs {
			id, ok := e.(*ast.Ident)
			d, isDefer := list[i+1+k].(*ast.DeferStmt)
			if !ok || id.Obj == nil || !isDefer || len(d.Call.Args) > 0 || !isIdentOf(d.Call.Fun, id.Obj) || sd.uses(list, id.Obj) != 2 {
				defers = nil
				break
			}
			defers = append(defers, d)
		}
		if defers == nil {
			out = append(out, list[i])
			continue
		}
		for k, d := range defers {
			d.Call.Fun = a.Rhs[k]
			sd.fl.parts[d] = true
			sd.bound[a.Lhs[k].(*ast.Ident).Obj] = true
		}
		sd.cleanups += len(defers)
	}
	return out
}

func isIdentOf(e ast.Expr, obj *ast.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Obj == obj
}

// uses counts the identifiers of obj in the statements.
func (sd *side) uses(list []ast.Stmt, obj *ast.Object) int {
	n := 0
	for _, s := range list {
		for _, x := range sd.expand(s) {
			ast.Inspect(x, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && id.Obj == obj {
					n++
				}
				return true
			})
		}
	}
	return n
}

func varDecl(s ast.Stmt) *ast.GenDecl {
	ds, ok := s.(*ast.DeclStmt)
	if !ok {
		return nil
	}
	gd, ok := ds.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
		return nil
	}
	return gd
}

// writesFirst: s is an = or := that writes the variable v declares, and
// mentions it nowhere else.
func writesFirst(s ast.Stmt, v *ast.Ident) bool {
	a, ok := s.(*ast.AssignStmt)
	if !ok || (a.Tok != token.ASSIGN && a.Tok != token.DEFINE) || v.Obj == nil || v.Name == "_" {
		return false
	}
	onLeft, n := false, 0
	ast.Inspect(a, func(m ast.Node) bool {
		if id, ok := m.(*ast.Ident); ok && id.Obj == v.Obj {
			n++
		}
		return true
	})
	for _, e := range a.Lhs {
		onLeft = onLeft || isIdentOf(e, v.Obj)
	}
	return onLeft && n == 1
}

func allIn(names []*ast.Ident, set map[string]bool) bool {
	for _, n := range names {
		if !set[n.Name] {
			return false
		}
	}
	return true
}

func anyIn(names []*ast.Ident, set map[string]bool) bool {
	for _, n := range names {
		if set[n.Name] {
			return true
		}
	}
	return false
}

func allIdents(es []ast.Expr) bool {
	for _, e := range es {
		if _, ok := e.(*ast.Ident); !ok {
			return false
		}
	}
	return true
}

// defineFrom reads var x, y = v1, v2 as x, y := v1, v2.
func (sd *side) defineFrom(ds *ast.DeclStmt, vs *ast.ValueSpec) ast.Stmt {
	lhs := make([]ast.Expr, len(vs.Names))
	for i, n := range vs.Names {
		lhs[i] = n
	}
	a := &ast.AssignStmt{Lhs: lhs, TokPos: vs.Names[len(vs.Names)-1].End(), Tok: token.DEFINE, Rhs: vs.Values}
	sd.fl.files[a] = sd.fl.files[ds]
	return a
}

// refuse fails the shapes the text cannot show: a top-level local with a
// field's name on a side with stages, and a local declared at the top of
// two of the function's bodies (the function's own and its stages') that
// a function literal captures, whose address is taken, or whose copy one
// body reads after another body wrote its own (normalise_flow.go). The
// address a pointer method or slicing takes needs types: typedChecks.
func (sd *side) refuse(fields map[string]bool) error {
	bodies := []*ast.FuncDecl{sd.fl.target}
	var names []string
	for name := range sd.fl.stages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bodies = append(bodies, sd.fl.stages[name].fd)
	}
	declaredIn := map[string][]*ast.FuncDecl{}
	var errs []string
	for _, fd := range bodies {
		for _, obj := range topObjects(fd) {
			declaredIn[obj.Name] = append(declaredIn[obj.Name], fd)
			if sd.app != nil && fields[obj.Name] && declNode(obj) != ast.Node(sd.app) && !sd.cleanupBinding(obj) {
				errs = append(errs, fmt.Sprintf("%s: %s declares a local %s, the name of a field of the stages' receiver; "+
					"once %s.%s reads as %s the two would be one, so rename the local",
					sd.fl.p.fset.Position(declNode(obj).Pos()), funcKey(fd), obj.Name, sd.fl.recv, obj.Name, obj.Name))
			}
		}
	}
	for name, fds := range declaredIn {
		if len(fds) < 2 {
			continue
		}
		for _, fd := range fds {
			if pos, what := captured(fd, name); what != "" {
				errs = append(errs, fmt.Sprintf("%s: %s is declared at the top of %d of %s's bodies, and %s in %s: "+
					"it would not see what the others write to their own %s; make it a field of the stages' receiver",
					sd.fl.p.fset.Position(pos), name, len(fds), funcKey(sd.fl.target), what, funcKey(fd), name))
			}
		}
	}
	errs = append(errs, sd.crossings(bodies, declaredIn)...)
	sort.Strings(errs)
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// cleanupBinding: obj is c in c := recv.stage(), bound to a cleanup the
// stage returns; it is checked once dropCleanups has seen the defers.
func (sd *side) cleanupBinding(obj *ast.Object) bool {
	a, ok := declNode(obj).(*ast.AssignStmt)
	if !ok || len(a.Rhs) != 1 {
		return false
	}
	c, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	st, _ := sd.fl.stageOf(c)
	return st != nil
}

// topObjects are the objects fd's top-level statements declare.
func topObjects(fd *ast.FuncDecl) []*ast.Object {
	var out []*ast.Object
	seen := map[*ast.Object]bool{}
	for n := range topDecls(fd) {
		var ids []*ast.Ident
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, e := range x.Lhs {
				if id, ok := e.(*ast.Ident); ok {
					ids = append(ids, id)
				}
			}
		case *ast.ValueSpec:
			ids = x.Names
		case *ast.TypeSpec:
			ids = []*ast.Ident{x.Name}
		}
		for _, id := range ids {
			if id.Obj != nil && declNode(id.Obj) == n && !seen[id.Obj] && id.Name != "_" {
				seen[id.Obj] = true
				out = append(out, id.Obj)
			}
		}
	}
	return out
}

// captured finds, in fd, a function literal that reads fd's top-level
// local name, or an &name.
func captured(fd *ast.FuncDecl, name string) (token.Pos, string) {
	top := topDecls(fd)
	var pos token.Pos
	what := ""
	lits := 0
	var stack []ast.Node
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil {
			if _, ok := stack[len(stack)-1].(*ast.FuncLit); ok {
				lits--
			}
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		switch x := n.(type) {
		case *ast.FuncLit:
			lits++
		case *ast.UnaryExpr:
			if id, ok := x.X.(*ast.Ident); ok && x.Op == token.AND && id.Name == name && id.Obj != nil && top[declNode(id.Obj)] && what == "" {
				pos, what = id.Pos(), "its address is taken"
			}
		case *ast.Ident:
			if lits > 0 && x.Name == name && x.Obj != nil && top[declNode(x.Obj)] && what == "" {
				pos, what = x.Pos(), "a function literal captures it"
			}
		}
		return true
	})
	return pos, what
}
