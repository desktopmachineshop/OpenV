package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"sort"
	"strings"
)

// stage is a method declared in a wire_*.go file.
type stage struct {
	fd      *ast.FuncDecl
	file    *goFile
	checked bool
}

// flattener inlines the stage calls of one function.
type flattener struct {
	p      *pkgInfo
	recv   string
	stages map[string]*stage
	active map[*stage]bool
	files  map[ast.Stmt]*goFile // the file each statement comes from
	synth  map[ast.Stmt]bool    // assignments built from a stage's return
	// placeholders maps the name standing in for a stage call inside a
	// nested block to the statements it expands to.
	placeholders map[string][]ast.Stmt
	errs         []string
}

// flatten returns the statements of the named function with its stage
// calls inlined, each rendered by go/printer.
func flatten(p *pkgInfo, name, recv string) ([]string, error) {
	var target *decl
	for _, d := range p.decls {
		if d.key == name && d.file.set == "" && (d.kind == "func" || d.kind == "method") {
			if target != nil {
				return nil, fmt.Errorf("%s is declared in %s and %s", name, target.file.name, d.file.name)
			}
			target = d
		}
	}
	if target == nil {
		return nil, fmt.Errorf("no function %s", name)
	}
	fd := target.node.(*ast.FuncDecl)
	if fd.Body == nil {
		return nil, fmt.Errorf("%s has no body", name)
	}
	fl := &flattener{p: p, recv: recv, stages: map[string]*stage{}, active: map[*stage]bool{},
		files: map[ast.Stmt]*goFile{}, synth: map[ast.Stmt]bool{}, placeholders: map[string][]ast.Stmt{}}
	for _, d := range p.decls {
		if d.kind != "method" || d.file.set != "" || !strings.HasPrefix(d.file.name, "wire_") {
			continue
		}
		sfd := d.node.(*ast.FuncDecl)
		if prev := fl.stages[sfd.Name.Name]; prev != nil {
			return nil, fmt.Errorf("stage methods %s (%s) and %s (%s) share a name", funcKey(prev.fd), prev.file.name, d.key, d.file.name)
		}
		fl.stages[sfd.Name.Name] = &stage{fd: sfd, file: d.file}
	}
	list := fl.splice(fd.Body.List, target.file, true)
	fl.leftovers(list)
	for _, stmts := range fl.placeholders {
		fl.leftovers(stmts)
	}
	sort.Strings(fl.errs)
	if len(fl.errs) > 0 {
		return nil, errors.New(strings.Join(fl.errs, "\n"))
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, fl.render(s))
	}
	return out, nil
}

// call is a stage call in statement position.
type call struct {
	st  *stage
	x   *ast.Ident
	ce  *ast.CallExpr
	lhs []ast.Expr
	tok token.Token
}

// splice returns list with each stage call replaced by the stage's body,
// recursing into nested blocks. Statements are rendered one by one, each
// from its own file; inside a nested block a stage call is replaced by a
// placeholder at the call's position instead, and render expands it, so
// the block keeps the layout it has in the source.
func (fl *flattener) splice(list []ast.Stmt, file *goFile, top bool) []ast.Stmt {
	var out []ast.Stmt
	for _, s := range list {
		if c := fl.stageCall(s); c != nil {
			inlined := fl.inline(c)
			if top {
				out = append(out, inlined...)
				continue
			}
			name := fmt.Sprintf("flatten_placeholder_%d", len(fl.placeholders))
			fl.placeholders[name] = inlined
			out = append(out, &ast.ExprStmt{X: &ast.Ident{NamePos: s.Pos(), Name: name}})
			continue
		}
		if fl.files[s] == nil {
			fl.files[s] = file
		}
		fl.nested(s, file)
		out = append(out, s)
	}
	return out
}

// nested splices the statement lists inside a compound statement.
func (fl *flattener) nested(s ast.Stmt, file *goFile) {
	switch x := s.(type) {
	case *ast.BlockStmt:
		x.List = fl.splice(x.List, file, false)
	case *ast.IfStmt:
		fl.nested(x.Body, file)
		if x.Else != nil {
			fl.nested(x.Else, file)
		}
	case *ast.ForStmt:
		fl.nested(x.Body, file)
	case *ast.RangeStmt:
		fl.nested(x.Body, file)
	case *ast.SwitchStmt:
		fl.nested(x.Body, file)
	case *ast.TypeSwitchStmt:
		fl.nested(x.Body, file)
	case *ast.SelectStmt:
		fl.nested(x.Body, file)
	case *ast.CaseClause:
		x.Body = fl.splice(x.Body, file, false)
	case *ast.CommClause:
		x.Body = fl.splice(x.Body, file, false)
	case *ast.LabeledStmt:
		fl.nested(x.Stmt, file)
	}
}

// stageCall recognises recv.stage() as an expression statement or as the
// only right-hand side of an assignment.
func (fl *flattener) stageCall(s ast.Stmt) *call {
	var e ast.Expr
	c := &call{}
	switch x := s.(type) {
	case *ast.ExprStmt:
		e = x.X
	case *ast.AssignStmt:
		if len(x.Rhs) == 1 {
			e, c.lhs, c.tok = x.Rhs[0], x.Lhs, x.Tok
		}
	}
	ce, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	c.st, c.x = fl.stageOf(ce)
	if c.st == nil {
		return nil
	}
	c.ce = ce
	return c
}

func (fl *flattener) stageOf(ce *ast.CallExpr) (*stage, *ast.Ident) {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != fl.recv {
		return nil, nil
	}
	return fl.stages[sel.Sel.Name], id
}

// inline returns a stage's statements, then the assignment of its final
// return's expressions to what the call assigned.
func (fl *flattener) inline(c *call) []ast.Stmt {
	st := c.st
	if len(c.ce.Args) > 0 || st.fd.Type.Params.NumFields() > 0 {
		fl.fail(c.ce.Pos(), "%s takes arguments; flatten inlines only stages called as %s.stage()", funcKey(st.fd), fl.recv)
		return nil
	}
	if fl.active[st] {
		fl.fail(c.ce.Pos(), "%s calls itself", funcKey(st.fd))
		return nil
	}
	fl.check(st)
	fl.renameReceiver(st, c.x.Name)
	body := st.fd.Body.List
	var ret *ast.ReturnStmt
	if n := len(body); n > 0 {
		if r, ok := body[n-1].(*ast.ReturnStmt); ok {
			ret, body = r, body[:n-1]
		}
	}
	fl.active[st] = true
	out := fl.splice(body, st.file, true)
	delete(fl.active, st)
	var results []ast.Expr
	if ret != nil {
		results = ret.Results
	}
	switch {
	case c.lhs != nil && len(results) == 0:
		fl.fail(c.ce.Pos(), "%s is assigned, but %s does not end with a return of its results", fl.render(&ast.ExprStmt{X: c.ce}), funcKey(st.fd))
	case c.lhs != nil:
		a := &ast.AssignStmt{Lhs: c.lhs, Tok: c.tok, Rhs: results}
		fl.synth[a] = true
		out = append(out, a)
	case len(results) > 0:
		lhs := make([]ast.Expr, len(results))
		for i := range lhs {
			lhs[i] = ast.NewIdent("_")
		}
		a := &ast.AssignStmt{Lhs: lhs, Tok: token.ASSIGN, Rhs: results}
		fl.synth[a] = true
		out = append(out, a)
	}
	return out
}

// check fails a stage that contains defer, recover or a return before its
// last statement. Function literals are not searched: they run on their own
// frames (goroutines, callbacks), so their defers and returns are theirs.
func (fl *flattener) check(st *stage) {
	if st.checked {
		return
	}
	st.checked = true
	body := st.fd.Body.List
	for i, s := range body {
		last := i == len(body)-1
		ast.Inspect(s, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.DeferStmt:
				fl.stageFail(st, x.Pos(), "defer")
			case *ast.ReturnStmt:
				if !last || x != s {
					fl.stageFail(st, x.Pos(), "an early return")
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "recover" && id.Obj == nil {
					fl.stageFail(st, x.Pos(), "recover")
				}
			}
			return true
		})
	}
}

func (fl *flattener) stageFail(st *stage, pos token.Pos, what string) {
	fl.fail(pos, "stage %s contains %s; defer, recover and return belong in the function that calls the stages "+
		"(a stage may only end by returning its results, such as a cleanup for the caller to defer)", funcKey(st.fd), what)
}

func (fl *flattener) fail(pos token.Pos, format string, args ...any) {
	p := fl.p.fset.Position(pos)
	fl.errs = append(fl.errs, fmt.Sprintf("%s:%d: ", p.Filename, p.Line)+fmt.Sprintf(format, args...))
}

// renameReceiver gives a stage's receiver the name the call uses, so the
// flattened statements read as the caller's.
func (fl *flattener) renameReceiver(st *stage, name string) {
	names := st.fd.Recv.List[0].Names
	if len(names) == 0 || names[0].Name == name || names[0].Obj == nil {
		return
	}
	obj := names[0].Obj
	ast.Inspect(st.fd.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Obj == obj {
			id.Name = name
		}
		return true
	})
	names[0].Name = name
}

// leftovers fails every stage call flatten could not inline.
func (fl *flattener) leftovers(list []ast.Stmt) {
	for _, s := range list {
		ast.Inspect(s, func(n ast.Node) bool {
			if ce, ok := n.(*ast.CallExpr); ok {
				if st, _ := fl.stageOf(ce); st != nil {
					fl.fail(ce.Pos(), "%s: a stage call must be a statement of its own or the right side of an assignment, "+
						"not in defer, go, a function literal or an expression", fl.render(&ast.ExprStmt{X: ce}))
				}
			}
			return true
		})
	}
}

// render prints a statement with the comments inside it, expanding the
// placeholders of nested stage calls at their indentation; an assignment
// built from a stage's return is printed from its parts.
func (fl *flattener) render(s ast.Stmt) string {
	if a, ok := s.(*ast.AssignStmt); ok && fl.synth[a] {
		return fl.exprs(a.Lhs) + " " + a.Tok.String() + " " + fl.exprs(a.Rhs)
	}
	var text string
	if f := fl.files[s]; f != nil {
		text = printNode(fl.p.fset, f, s)
	} else {
		var b bytes.Buffer
		if err := printer.Fprint(&b, fl.p.fset, s); err != nil {
			return "!print: " + err.Error()
		}
		text = b.String()
	}
	if len(fl.placeholders) == 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		stmts, ok := fl.placeholders[strings.TrimSpace(line)]
		if !ok {
			out = append(out, line)
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, "\t "))]
		for _, st := range stmts {
			for _, l := range strings.Split(fl.render(st), "\n") {
				out = append(out, indent+l)
			}
		}
	}
	return strings.Join(out, "\n")
}

func (fl *flattener) exprs(list []ast.Expr) string {
	parts := make([]string, len(list))
	for i, e := range list {
		var b bytes.Buffer
		if err := printer.Fprint(&b, fl.p.fset, e); err != nil {
			parts[i] = "!print: " + err.Error()
			continue
		}
		parts[i] = b.String()
	}
	return strings.Join(parts, ", ")
}
