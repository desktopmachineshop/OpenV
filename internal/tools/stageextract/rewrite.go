package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// nameCleanups names the variables the function binds each stage's
// returned cleanups to: the spec's names, or the deferred variable's own
// (stop), or the method and its receiver's (db.Close: closeDB). A stage
// returns the deferred function only when it can reach it, through a
// field: a defer of a local that stays the function's (c := closer{}
// before the first range, then defer c.Close() after one) stays in the
// function as it is, and so do the defers after it.
func (p *plan) nameCleanups() error {
	for _, st := range p.stages {
		for i, d := range st.defers {
			if root := p.cleanupRoot(d); root != nil && root.kind != field {
				p.notes = append(p.notes, fmt.Sprintf("stage %s: the defer at line %d stays in %s() as it is, and so does any defer after it: "+
					"%s stays a local of %s(), which the stage cannot reach", st.spec.Name, p.line(d.Pos()), p.sp.funcName(), root.obj.Name(), p.sp.funcName()))
				st.defers = st.defers[:i]
				break
			}
		}
	}
	taken := map[string]string{p.sp.recv(): "the receiver"}
	info := p.ps.info
	stay := func(stmts []ast.Stmt) {
		for _, s := range stmts {
			ast.Inspect(s, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					ast.Inspect(sel.X, func(m ast.Node) bool { return p.noteName(m, info, taken) })
					return false
				}
				return p.noteName(n, info, taken)
			})
		}
	}
	stay(p.prefix)
	stay(p.tail)
	for _, st := range p.stages {
		stay(st.kept)
	}
	for _, st := range p.stages {
		if len(st.spec.Cleanups) > 0 && len(st.spec.Cleanups) != len(st.defers) {
			return refuse(fmt.Errorf("stage %s names %d cleanups, and returns %d (one per defer right after its range)", st.spec.Name, len(st.spec.Cleanups), len(st.defers)))
		}
		for i, d := range st.defers {
			name := derivedName(d.Call.Fun)
			if len(st.spec.Cleanups) > 0 {
				name = st.spec.Cleanups[i]
			}
			if by, ok := taken[name]; ok {
				return refuse(fmt.Errorf("stage %s: the cleanup it returns for the defer at line %d would be bound to %s, which %s already uses; "+
					"name it in the spec's cleanups", st.spec.Name, p.line(d.Pos()), name, by))
			}
			taken[name] = fmt.Sprintf("the cleanup of stage %s", st.spec.Name)
			st.cleanups = append(st.cleanups, name)
		}
	}
	return nil
}

// cleanupRoot is the top-level local a cleanup defer calls (stop in
// stop(), db in db.Close()).
func (p *plan) cleanupRoot(d *ast.DeferStmt) *local {
	e := d.Call.Fun
	for {
		switch x := unparen(e).(type) {
		case *ast.SelectorExpr:
			e = x.X
		case *ast.Ident:
			return p.byObj[asVar(p.ps.info.Uses[x])]
		default:
			return nil
		}
	}
}

// noteName records a name the function's remaining statements use, other
// than a field's (which they reach as a.name).
func (p *plan) noteName(n ast.Node, info *types.Info, taken map[string]string) bool {
	id, ok := n.(*ast.Ident)
	if !ok {
		return true
	}
	obj := info.Uses[id]
	if obj == nil {
		obj = info.Defs[id]
	}
	if l := p.byObj[asVar(obj)]; l != nil && l.kind == field {
		return true
	}
	if obj != nil {
		taken[id.Name] = fmt.Sprintf("%s() (line %d)", p.sp.funcName(), p.line(id.Pos()))
	}
	return true
}

// derivedName is the default name of a returned cleanup: the variable's
// own for stop(), the method's then its receiver's for db.Close() (an
// initialism when the receiver's name is short, as in closeDB).
func derivedName(fun ast.Expr) string {
	switch x := unparen(fun).(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		recv := rootName(x.X)
		if len(recv) <= 3 {
			recv = strings.ToUpper(recv)
		} else {
			r, n := utf8.DecodeRuneInString(recv)
			recv = string(unicode.ToUpper(r)) + recv[n:]
		}
		r, n := utf8.DecodeRuneInString(x.Sel.Name)
		return string(unicode.ToLower(r)) + x.Sel.Name[n:] + recv
	}
	return "cleanup"
}

func rootName(e ast.Expr) string {
	var parts []string
	for {
		switch x := unparen(e).(type) {
		case *ast.Ident:
			parts = append([]string{x.Name}, parts...)
			return strings.Join(parts, "")
		case *ast.SelectorExpr:
			parts = append([]string{x.Sel.Name}, parts...)
			e = x.X
		default:
			return strings.Join(parts, "")
		}
	}
}

// rewrite works out the edits to the function's file: a.name for every
// reference to a field; = for a := that declares or assigns a field, with
// a var line for each other name it declared in that segment; a var line
// before the plain assignment that first writes a split local in a
// segment that did not declare it; a var of fields dropped or turned into
// an assignment; and each cleanup defer calling the name it is bound to.
func (p *plan) rewrite() error {
	info := p.ps.info
	isField := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		if !ok {
			return false
		}
		l := p.byObj[asVar(info.ObjectOf(id))]
		return l != nil && l.kind == field
	}
	for _, l := range p.locals {
		if l.kind != field {
			continue
		}
		for _, r := range l.refs {
			p.edits = append(p.edits, edit{off: p.off(r.Pos()), end: p.off(r.Pos()), text: p.sp.recv() + ".", ident: true})
		}
	}
	// newHere: a name a statement in segment s declares there.
	newHere := func(id *ast.Ident, s int) bool {
		l := p.byObj[asVar(info.ObjectOf(id))]
		if l == nil {
			return info.Defs[id] != nil
		}
		switch l.kind {
		case split:
			return l.first[s] == id && (s == l.declSeg || p.declaresIn(l, s))
		case stays:
			return info.Defs[id] != nil
		}
		return false
	}
	for _, s := range p.fd.Body.List {
		sg := p.seg(p.line(s.Pos()))
		switch x := s.(type) {
		case *ast.AssignStmt:
			switch x.Tok {
			case token.DEFINE:
				has := false
				for _, e := range x.Lhs {
					has = has || isField(e)
				}
				if !has {
					continue
				}
				p.edits = append(p.edits, edit{off: p.off(x.TokPos), end: p.off(x.TokPos) + 2, text: "="})
				for _, e := range x.Lhs {
					if id := e.(*ast.Ident); id.Name != "_" && !isField(id) && newHere(id, sg) {
						if err := p.declareBefore(s, id); err != nil {
							return err
						}
					}
				}
			case token.ASSIGN:
				for _, e := range x.Lhs {
					id, ok := e.(*ast.Ident)
					if !ok {
						continue
					}
					if l := p.byObj[asVar(info.Uses[id])]; l != nil && l.kind == split && l.first[sg] == id && p.declaresIn(l, sg) {
						if err := p.declareBefore(s, id); err != nil {
							return err
						}
					}
				}
			}
		case *ast.DeclStmt:
			if err := p.rewriteVar(x); err != nil {
				return err
			}
		}
	}
	for _, st := range p.stages {
		for i, d := range st.defers {
			p.edits = append(p.edits, edit{off: p.off(d.Call.Fun.Pos()), end: p.off(d.Call.Fun.End()), text: st.cleanups[i]})
		}
	}
	return p.settleEdits()
}

// declaresIn: segment s, which uses the split local l but does not declare
// it, needs a declaration of its own: every stage does, and the function
// once, in its first region that uses l, when a stage declared l. The
// function's regions share its scope, so a second var there would
// redeclare l.
func (p *plan) declaresIn(l *local, s int) bool {
	switch {
	case s == l.declSeg:
		return false
	case !inFunc(s):
		return true
	case inFunc(l.declSeg):
		return false
	}
	for _, r := range l.refs {
		if t := p.seg(p.line(r.Pos())); inFunc(t) {
			return t == s
		}
	}
	return false
}

// declareBefore inserts `var name T` on a line of its own before the
// top-level statement s.
func (p *plan) declareBefore(s ast.Stmt, id *ast.Ident) error {
	t := p.ps.info.ObjectOf(id).Type()
	if err := p.nameable(t); err != nil {
		return refuse(fmt.Errorf("line %d: %s needs a var of its own in its stage, but %v", p.line(id.Pos()), id.Name, err))
	}
	ls := p.lineStart(p.line(s.Pos()))
	indent := string(p.mf.src[ls:p.off(s.Pos())])
	if strings.TrimSpace(indent) != "" {
		return refuse(fmt.Errorf("line %d: the statement does not start its line", p.line(s.Pos())))
	}
	p.edits = append(p.edits, edit{off: ls, end: ls, text: indent + "var " + id.Name + " " + p.typeMark(t) + "\n"})
	return nil
}

// typeMark stands for a type in edit text until render names it for the
// file the text lands in.
func (p *plan) typeMark(t types.Type) string {
	i := 0
	for ; i < len(p.marked); i++ {
		if types.Identical(p.marked[i], t) && types.TypeString(p.marked[i], nil) == types.TypeString(t, nil) {
			break
		}
	}
	if i == len(p.marked) {
		p.marked = append(p.marked, t)
	}
	return fmt.Sprintf("\x00%d\x00", i)
}

// rewriteVar handles a top-level var declaration of fields: without values
// it goes (a field starts as the zero value), with values it becomes an
// assignment to the fields; any other name it declares keeps a var line.
func (p *plan) rewriteVar(x *ast.DeclStmt) error {
	gd := x.Decl.(*ast.GenDecl)
	if gd.Tok != token.VAR {
		return nil
	}
	var fields []bool
	any := false
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		for _, n := range vs.Names {
			l := p.byObj[asVar(p.ps.info.Defs[n])]
			f := l != nil && l.kind == field
			fields = append(fields, f)
			any = any || f
		}
	}
	if !any {
		return nil
	}
	if len(gd.Specs) != 1 {
		return refuse(fmt.Errorf("line %d: a grouped var declaration declares a field; write one var per line first", p.line(x.Pos())))
	}
	vs := gd.Specs[0].(*ast.ValueSpec)
	ls := p.lineStart(p.line(x.Pos()))
	indent := string(p.mf.src[ls:p.off(x.Pos())])
	var text strings.Builder
	var lhs []string
	for i, n := range vs.Names {
		switch {
		case fields[i]:
			lhs = append(lhs, p.sp.recv()+"."+n.Name)
		case n.Name == "_":
			lhs = append(lhs, "_")
		default:
			t := p.ps.info.Defs[n].Type()
			if err := p.nameable(t); err != nil {
				return refuse(fmt.Errorf("line %d: %s keeps a var of its own, but %v", p.line(n.Pos()), n.Name, err))
			}
			if text.Len() > 0 {
				text.WriteString(indent)
			}
			text.WriteString("var " + n.Name + " " + p.typeMark(t) + "\n")
			lhs = append(lhs, n.Name)
		}
	}
	if len(vs.Values) == 0 {
		// Drop the declaration, keeping var lines for any other names.
		end := p.off(x.End())
		start := p.off(x.Pos())
		rest := strings.TrimSpace(string(p.mf.src[end:p.lineStart(p.line(x.End())+1)]))
		if text.Len() == 0 && rest == "" {
			start, end = ls, p.lineStart(p.line(x.End())+1)
		} else {
			t := strings.TrimSuffix(text.String(), "\n")
			text.Reset()
			text.WriteString(t)
		}
		p.edits = append(p.edits, edit{off: start, end: end, text: text.String()})
		return nil
	}
	if text.Len() > 0 {
		text.WriteString(indent)
	}
	text.WriteString(strings.Join(lhs, ", ") + " = ")
	p.edits = append(p.edits, edit{off: p.off(x.Pos()), end: p.off(vs.Values[0].Pos()), text: text.String()})
	return nil
}

// settleEdits sorts the edits and drops the a. insertions that a
// replacement covers; any other overlap is a fault.
func (p *plan) settleEdits() error {
	var spans []edit
	for _, e := range p.edits {
		if e.end > e.off {
			spans = append(spans, e)
		}
	}
	var out []edit
	for _, e := range p.edits {
		if e.ident {
			covered := false
			for _, s := range spans {
				if e.off >= s.off && e.off < s.end {
					covered = true
				}
			}
			if covered {
				continue
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].off != out[j].off {
			return out[i].off < out[j].off
		}
		// Inserts before a replacement at the same offset.
		return out[i].end < out[j].end
	})
	for i := 1; i < len(out); i++ {
		if out[i].off < out[i-1].end {
			return fmt.Errorf("internal: overlapping edits at offsets %d and %d", out[i-1].off, out[i].off)
		}
	}
	p.edits = out
	return nil
}

// fieldCount, splitNames: for the report.
func (p *plan) fieldCount() int {
	n := 0
	for _, l := range p.locals {
		if l.kind == field {
			n++
		}
	}
	return n
}

func (p *plan) splitNames() []string {
	var out []string
	for _, l := range p.locals {
		if l.kind == split {
			out = append(out, l.obj.Name())
		}
	}
	return out
}
