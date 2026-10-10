package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

// The statement shapes the encode rewrite matches, H being
// W.Header().Set("Content-Type", "application/json"), S W.WriteHeader(X)
// and E json.NewEncoder(W).Encode(V), with or without `_ =`.
var encodeShapes = map[string]string{
	"H S E": hWriteJSON,
	"H E":   hWriteJSONOK,
	"S E":   hWriteJSONBareStatus,
	"E":     hWriteJSONBare,
}

// stmtList returns the statement list n holds, if any.
func stmtList(n ast.Node) []ast.Stmt {
	switch n := n.(type) {
	case *ast.BlockStmt:
		return n.List
	case *ast.CaseClause:
		return n.Body
	case *ast.CommClause:
		return n.Body
	}
	return nil
}

// indexOf returns the index of s in list, or -1.
func indexOf(list []ast.Stmt, s ast.Stmt) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// funcContext is what the rewrite needs to know of the functions around a
// site: the outermost declaration (a helper's own body is never rewritten,
// an SSE handler never touched) and the stack index of the innermost
// function, where dominance stops.
type funcContext struct {
	decl  *ast.FuncDecl
	inner int
}

func enclosing(stack []ast.Node) funcContext {
	fc := funcContext{inner: -1}
	for i := len(stack) - 1; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.FuncLit:
			if fc.inner < 0 {
				fc.inner = i
			}
		case *ast.FuncDecl:
			if fc.inner < 0 {
				fc.inner = i
			}
			fc.decl = n
		}
	}
	return fc
}

// isSSE reports whether fd streams server-sent events.
func isSSE(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		if s, ok := stringLit(asExpr(n)); ok && s == "text/event-stream" {
			found = true
		}
		return !found
	})
	return found
}

func asExpr(n ast.Node) ast.Expr {
	e, _ := n.(ast.Expr)
	return e
}

// encodeSite classifies the json.NewEncoder call at the top of stack.
func (fi *fileInfo) encodeSite(stack []ast.Node) site {
	call := stack[len(stack)-1].(*ast.CallExpr)
	s := site{file: fi.path, line: fi.line(call.Pos()), kind: kindEncode}
	fc := enclosing(stack)
	switch {
	case fc.decl != nil && isHelperName(fc.decl.Name.Name) && fc.decl.Recv == nil:
		s.reason, s.kept = "the helper "+fc.decl.Name.Name+" itself", true
		return s
	case fc.decl != nil && isSSE(fc.decl):
		s.reason = "an SSE handler (" + fc.decl.Name.Name + "), which X1 does not touch"
		return s
	}
	n := len(stack)
	encSel, ok := stack[n-2].(*ast.SelectorExpr)
	if !ok || encSel.Sel.Name != "Encode" || n < 5 {
		s.reason = "the encoder is not used as json.NewEncoder(w).Encode(v)"
		return s
	}
	enc, ok := stack[n-3].(*ast.CallExpr)
	if !ok || enc.Fun != encSel || len(enc.Args) != 1 {
		s.reason = "the encoder is not used as json.NewEncoder(w).Encode(v)"
		return s
	}
	stmt, ok := stack[n-4].(ast.Stmt)
	if !ok || !discards(stmt, enc) {
		s.reason = "Encode's error is used"
		return s
	}
	list := stmtList(stack[n-5])
	idx := indexOf(list, stmt)
	if idx < 0 {
		s.reason = "the encode is not a statement of a block"
		return s
	}
	if len(call.Args) != 1 {
		s.reason = "json.NewEncoder has no single writer argument"
		return s
	}
	wid, ok := call.Args[0].(*ast.Ident)
	if !ok {
		s.reason = "the writer is " + fi.text(call.Args[0]) + ", not a parameter"
		return s
	}
	w, ok := fi.param(wid, "ResponseWriter")
	if !ok {
		s.reason = "the writer " + wid.Name + " is not an http.ResponseWriter parameter"
		return s
	}
	return fi.encodeSequence(s, stack[:n-4], fc, list, idx, w, enc.Args[0])
}

// discards reports whether stmt is `enc` or `_ = enc`.
func discards(stmt ast.Stmt, enc *ast.CallExpr) bool {
	switch st := stmt.(type) {
	case *ast.ExprStmt:
		return st.X == enc
	case *ast.AssignStmt:
		if st.Tok != token.ASSIGN || len(st.Lhs) != 1 || len(st.Rhs) != 1 || st.Rhs[0] != enc {
			return false
		}
		id, ok := st.Lhs[0].(*ast.Ident)
		return ok && id.Name == "_"
	}
	return false
}

// encodeSequence matches the statements before the encode at list[idx]
// and, when they form a shape, builds the rewrite.
func (fi *fileInfo) encodeSequence(s site, outer []ast.Node, fc funcContext, list []ast.Stmt, idx int, w *ast.Object, v ast.Expr) site {
	start := idx
	for start > 0 && isWriterStmt(list[start-1], w) {
		start--
	}
	run := list[start:idx]
	first, shape := idx, "E"
	var status ast.Expr
	if k := len(run); k > 0 {
		if call, ok := writerCall(run[k-1], w, "WriteHeader"); ok && len(call.Args) == 1 {
			first, shape, status = idx-1, "S E", call.Args[0]
			run = run[:k-1]
		}
	}
	if k := len(run); k > 0 {
		if ct, ok := fi.contentType(run[k-1], w); ok {
			if ct != "application/json" {
				s.reason = fmt.Sprintf("the Content-Type is %q, not application/json", ct)
				return s
			}
			first, shape = first-1, "H "+shape
			run = run[:k-1]
		}
	}
	if r := fi.dominating(outer, fc, list[:first], w); r != "" {
		s.reason = r
		return s
	}
	s.shape, s.helper = shape, encodeShapes[shape]
	if s.helper == "" {
		s.reason = "no helper for " + shape
		return s
	}
	args := []ast.Expr{}
	if status != nil {
		args = append(args, status)
	}
	args = append(args, v)
	if shape != "E" {
		for _, a := range args {
			if mentions(a, w) {
				s.helper, s.reason = "", "the argument "+fi.text(a)+" reads the writer, and would be evaluated before the header or status is written"
				return s
			}
			if f := localFuncCall(a); f != "" {
				s.helper, s.reason = "", "the argument "+fi.text(a)+" calls the function value "+f+", which would run before the header or status is written"
				return s
			}
		}
	}
	from, to := list[first].Pos(), list[idx].End()
	if c := fi.commentsOutside(from, to, status, v); c != nil {
		s.helper, s.reason = "", fmt.Sprintf("a comment at line %d sits inside the sequence", fi.line(c.Pos()))
		return s
	}
	text := s.helper + "(" + fi.text(asIdentOf(w))
	for _, a := range args {
		text += ", " + fi.text(a)
	}
	s.start, s.end, s.text = fi.off(from), fi.off(to), text+")"
	return s
}

// asIdentOf returns the parameter's declaring identifier.
func asIdentOf(o *ast.Object) ast.Node {
	field := o.Decl.(*ast.Field)
	for _, n := range field.Names {
		if n.Obj == o {
			return n
		}
	}
	return field.Names[0]
}

// dominating names a statement that runs before the sequence on every path
// to it (an earlier statement of its block or of an enclosing block of the
// same function) and sets the writer's Content-Type or status, or writes
// its body: a helper call there would misname the response (a bare encode
// that a Content-Type set earlier covers). It returns "" when there is
// none.
func (fi *fileInfo) dominating(outer []ast.Node, fc funcContext, before []ast.Stmt, w *ast.Object) string {
	check := func(list []ast.Stmt) string {
		for i := len(list) - 1; i >= 0; i-- {
			st := list[i]
			if _, ok := fi.contentType(st, w); ok {
				return fmt.Sprintf("the Content-Type is set at line %d, not next to the encode", fi.line(st.Pos()))
			}
			if _, ok := writerCall(st, w, "WriteHeader"); ok {
				return fmt.Sprintf("the status is written at line %d, not next to the encode", fi.line(st.Pos()))
			}
			if _, ok := writerCall(st, w, "Write"); ok {
				return fmt.Sprintf("the body is written at line %d, before the encode", fi.line(st.Pos()))
			}
		}
		return ""
	}
	if r := check(before); r != "" {
		return r
	}
	for i := len(outer) - 1; i > fc.inner && i > 0; i-- {
		list := stmtList(outer[i-1])
		st, ok := outer[i].(ast.Stmt)
		if list == nil || !ok {
			continue
		}
		if j := indexOf(list, st); j >= 0 {
			if r := check(list[:j]); r != "" {
				return r
			}
		}
	}
	return ""
}
