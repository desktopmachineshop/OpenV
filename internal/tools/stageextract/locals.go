package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// classify finds the function's top-level locals and decides what becomes
// of each (stays, field or split).
func (p *plan) classify() error {
	if err := p.collectLocals(); err != nil {
		return err
	}
	if err := p.collectRefs(); err != nil {
		return err
	}
	return p.decide()
}

// collectLocals lists the variables the function's top-level statements
// declare; a local constant or type is refused when two segments use it.
func (p *plan) collectLocals() error {
	info := p.ps.info
	for _, s := range p.fd.Body.List {
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
			gd := x.Decl.(*ast.GenDecl)
			for _, spec := range gd.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					if gd.Tok == token.VAR {
						ids = append(ids, sp.Names...)
					} else if err := p.localDecl(s, sp.Names...); err != nil {
						return err
					}
				case *ast.TypeSpec:
					if err := p.localDecl(s, sp.Name); err != nil {
						return err
					}
				}
			}
		}
		for _, id := range ids {
			if v, ok := info.Defs[id].(*types.Var); ok && id.Name != "_" {
				l := &local{obj: v, decl: s, declSeg: p.seg(p.line(s.Pos())), segs: map[int]bool{}, first: map[int]*ast.Ident{}}
				p.locals = append(p.locals, l)
				p.byObj[v] = l
			}
		}
	}
	return nil
}

// collectRefs records every reference to a top-level local, in source
// order, with what it says about splitting: its segment, whether a
// function literal holds it, whether its address is taken. It refuses a
// function that already uses the receiver's name.
func (p *plan) collectRefs() error {
	info := p.ps.info
	recv := p.sp.recv()
	var used []string
	var lits int
	var stack []ast.Node
	ast.Inspect(p.fd.Body, func(n ast.Node) bool {
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
		case *ast.Ident:
			if x.Name == recv {
				used = append(used, fmt.Sprintf("line %d", p.line(x.Pos())))
			}
			obj := info.Uses[x]
			if obj == nil {
				obj = info.Defs[x]
			}
			if l := p.byObj[asVar(obj)]; l != nil {
				s := p.seg(p.line(x.Pos()))
				l.refs = append(l.refs, x)
				l.segs[s] = true
				if l.first[s] == nil {
					l.first[s] = x
				}
				if lits > 0 {
					l.captured = true
				}
			}
		case ast.Expr:
			if l := p.byObj[addressedVar(info, x)]; l != nil {
				l.addr = true
			}
		}
		return true
	})
	if len(used) > 0 {
		return refuse(fmt.Errorf("%s() already uses the name %s (%s); name the receiver otherwise in the spec (recv)", p.sp.funcName(), recv, strings.Join(used, ", ")))
	}
	return nil
}

// decide makes a local that more than one segment references a field,
// unless each segment can declare its own (split).
func (p *plan) decide() error {
	stageNames := map[string]bool{}
	for _, st := range p.stages {
		stageNames[st.spec.Name] = true
	}
	for _, l := range p.locals {
		if owners(l) <= 1 {
			continue
		}
		if l.why = p.splitBlocker(l); l.why == "" {
			l.kind = split
			continue
		}
		l.kind = field
		if stageNames[l.obj.Name()] {
			return refuse(fmt.Errorf("the local %s becomes a field of %s, and a stage has its name; rename the stage in the spec", l.obj.Name(), p.sp.typeName()))
		}
		if err := p.nameable(l.obj.Type()); err != nil {
			return refuse(fmt.Errorf("the local %s (line %d) becomes a field of %s, but %v", l.obj.Name(), p.line(l.obj.Pos()), p.sp.typeName(), err))
		}
	}
	return nil
}

// localDecl refuses a local constant or type that more than one segment
// uses: it cannot become a field.
func (p *plan) localDecl(s ast.Stmt, names ...*ast.Ident) error {
	for _, n := range names {
		obj := p.ps.info.Defs[n]
		if obj == nil {
			continue
		}
		l := &local{segs: map[int]bool{p.seg(p.line(s.Pos())): true}}
		for id, o := range p.ps.info.Uses {
			if o == obj {
				l.segs[p.seg(p.line(id.Pos()))] = true
			}
		}
		if owners(l) > 1 {
			return refuse(fmt.Errorf("the local %s %s (line %d) is used by more than one stage, and a constant or a type cannot become a field; "+
				"declare it at package level first", map[bool]string{true: "type", false: "constant"}[isType(obj)], n.Name, p.line(n.Pos())))
		}
	}
	return nil
}

func isType(o types.Object) bool { _, ok := o.(*types.TypeName); return ok }

func asVar(o types.Object) *types.Var {
	v, _ := o.(*types.Var)
	return v
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// splitBlocker says why a local referenced from several segments must
// become a field, or "" when each segment can declare its own: it is never
// captured by a function literal nor addressed, and each segment after
// the one that declares it first writes it whole, with = or :=, in a
// top-level statement that does not read it, and then reads it. The
// function's own regions count one by one: one that reads the local
// before writing it would read what the stage before it wrote to its own
// copy (main()'s err in the tail after a stage's err).
func (p *plan) splitBlocker(l *local) string {
	switch {
	case l.captured:
		return "a function literal captures it"
	case l.addr:
		return "its address is taken"
	}
	for _, s := range p.segsInOrder(l) {
		if !p.readIn(l, s) {
			return fmt.Sprintf("%s writes it without reading it", p.segName(s))
		}
		if s == l.declSeg {
			continue
		}
		id := l.first[s]
		if !p.wholeWrite(l, id) {
			return fmt.Sprintf("%s reads it at line %d before writing it", p.segName(s), p.line(id.Pos()))
		}
	}
	return ""
}

// segsInOrder lists the segments that reference l, in source order.
func (p *plan) segsInOrder(l *local) []int {
	var out []int
	seen := map[int]bool{}
	for _, r := range l.refs {
		if s := p.seg(p.line(r.Pos())); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// wholeWrite: id, a reference to l, is a left-hand operand of a top-level
// = or := statement that does not otherwise mention l.
func (p *plan) wholeWrite(l *local, id *ast.Ident) bool {
	a, ok := p.top(id.Pos()).(*ast.AssignStmt)
	if !ok || (a.Tok != token.ASSIGN && a.Tok != token.DEFINE) {
		return false
	}
	onLeft := false
	for _, e := range a.Lhs {
		if e == ast.Expr(id) {
			onLeft = true
		}
	}
	if !onLeft {
		return false
	}
	n := 0
	for _, r := range l.refs {
		if a.Pos() <= r.Pos() && r.Pos() < a.End() {
			n++
		}
	}
	return n == 1
}

// readIn: l is read somewhere in segment s (a reference other than as the
// whole left-hand operand of an = or := statement, or as a declared name).
func (p *plan) readIn(l *local, s int) bool {
	for _, r := range l.refs {
		if p.seg(p.line(r.Pos())) != s {
			continue
		}
		if p.ps.info.Defs[r] != nil {
			continue
		}
		if a, ok := p.top(r.Pos()).(*ast.AssignStmt); ok && (a.Tok == token.ASSIGN || a.Tok == token.DEFINE) && isLHS(a, r) {
			continue
		}
		return true
	}
	return false
}

func isLHS(a *ast.AssignStmt, id *ast.Ident) bool {
	for _, e := range a.Lhs {
		if e == ast.Expr(id) {
			return true
		}
	}
	return false
}

// nameable says whether a type can be written in another file of the
// package: every named type in it is exported or declared in this package.
func (p *plan) nameable(t types.Type) error {
	var bad error
	var walk func(t types.Type)
	seen := map[types.Type]bool{}
	walk = func(t types.Type) {
		if bad != nil || seen[t] {
			return
		}
		seen[t] = true
		switch x := t.(type) {
		case *types.Named:
			obj := x.Obj()
			if obj.Pkg() != nil && obj.Pkg() != p.ps.types && !obj.Exported() {
				bad = fmt.Errorf("its type %s is unexported in another package, so no other file can name it", types.TypeString(t, nil))
				return
			}
			if obj.Parent() != nil && obj.Parent() != obj.Pkg().Scope() && obj.Pkg() == p.ps.types {
				bad = fmt.Errorf("its type %s is declared inside a function", obj.Name())
				return
			}
			if args := x.TypeArgs(); args != nil {
				for i := 0; i < args.Len(); i++ {
					walk(args.At(i))
				}
			}
		case *types.Alias:
			obj := x.Obj()
			if obj.Pkg() != nil && obj.Pkg() != p.ps.types && !obj.Exported() {
				bad = fmt.Errorf("its type %s is unexported in another package", obj.Name())
			}
		case *types.Pointer:
			walk(x.Elem())
		case *types.Slice:
			walk(x.Elem())
		case *types.Array:
			walk(x.Elem())
		case *types.Map:
			walk(x.Key())
			walk(x.Elem())
		case *types.Chan:
			walk(x.Elem())
		case *types.Signature:
			for i := 0; i < x.Params().Len(); i++ {
				walk(x.Params().At(i).Type())
			}
			for i := 0; i < x.Results().Len(); i++ {
				walk(x.Results().At(i).Type())
			}
		case *types.Struct:
			for i := 0; i < x.NumFields(); i++ {
				if !x.Field(i).Exported() && x.Field(i).Pkg() != p.ps.types {
					bad = fmt.Errorf("its struct type has an unexported field of another package")
				}
				walk(x.Field(i).Type())
			}
		case *types.Interface:
			for i := 0; i < x.NumMethods(); i++ {
				walk(x.Method(i).Type())
			}
		}
	}
	walk(t)
	return bad
}
