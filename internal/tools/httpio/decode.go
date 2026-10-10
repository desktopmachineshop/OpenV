package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

// decodeIf is a matched decode:
//
//	if err := json.NewDecoder(R.Body).Decode(P); err != nil {
//		writeJSONError(W, http.StatusBadRequest, "<msg>")
//		return ...
//	}
type decodeIf struct {
	stmt *ast.IfStmt
	w, r *ast.Ident
	p    ast.Expr
	msg  *ast.BasicLit
	ret  *ast.ReturnStmt
}

// isDecodeInit reports whether ifs's init is `err := json.NewDecoder(...).Decode(...)`.
func (fi *fileInfo) decodeCall(ifs *ast.IfStmt) (errName *ast.Ident, dec, decode *ast.CallExpr, ok bool) {
	as, ok := ifs.Init.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return nil, nil, nil, false
	}
	errName, ok = as.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, nil, nil, false
	}
	decode, ok = as.Rhs[0].(*ast.CallExpr)
	if !ok {
		return nil, nil, nil, false
	}
	sel, ok := decode.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Decode" || len(decode.Args) != 1 {
		return nil, nil, nil, false
	}
	dec, ok = sel.X.(*ast.CallExpr)
	if !ok || !isPkgCall(dec, fi.jsonName, "NewDecoder") || len(dec.Args) != 1 {
		return nil, nil, nil, false
	}
	return errName, dec, decode, true
}

// matchDecode matches ifs against the decode shape. When ifs decodes JSON
// but is not the shape, reason says why; when it does not decode JSON at
// all, both results are zero.
func (fi *fileInfo) matchDecode(ifs *ast.IfStmt) (m *decodeIf, reason string) {
	errName, dec, decode, ok := fi.decodeCall(ifs)
	if !ok {
		return nil, ""
	}
	body, ok := dec.Args[0].(*ast.SelectorExpr)
	if !ok || body.Sel.Name != "Body" {
		return nil, "the decode reads " + fi.text(dec.Args[0]) + ", not the request body"
	}
	rid, ok := body.X.(*ast.Ident)
	if !ok {
		return nil, "the decode reads " + fi.text(dec.Args[0]) + ", not the request body"
	}
	if _, ok := fi.param(rid, "*Request"); !ok {
		return nil, "the decode reads " + rid.Name + ".Body, and " + rid.Name + " is not an *http.Request parameter"
	}
	cond, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ || !isIdent(cond.X, errName.Name) || !isIdent(cond.Y, "nil") {
		return nil, "the condition is " + fi.text(ifs.Cond) + ", not " + errName.Name + " != nil"
	}
	if ifs.Else != nil {
		return nil, "the if has an else"
	}
	if len(ifs.Body.List) != 2 {
		return nil, fmt.Sprintf("the error branch has %d statements, not writeJSONError then return", len(ifs.Body.List))
	}
	m = &decodeIf{stmt: ifs, r: rid, p: decode.Args[0]}
	if reason := fi.matchAnswer(m, ifs.Body.List[0]); reason != "" {
		return nil, reason
	}
	ret, ok := ifs.Body.List[1].(*ast.ReturnStmt)
	if !ok {
		return nil, "the error branch ends with " + fi.text(ifs.Body.List[1]) + ", not a return"
	}
	m.ret = ret
	if mentions(ret, errName.Obj) {
		return nil, "the return reads " + errName.Name
	}
	if !simpleTarget(m.p) {
		return nil, "the decode target " + fi.text(m.p) + " is not a variable or its address"
	}
	if c := fi.commentsOutside(ifs.Pos(), ifs.End(), ret); c != nil {
		return nil, fmt.Sprintf("a comment at line %d sits inside the if", fi.line(c.Pos()))
	}
	return m, ""
}

// matchAnswer matches writeJSONError(W, http.StatusBadRequest, "<msg>").
func (fi *fileInfo) matchAnswer(m *decodeIf, st ast.Stmt) string {
	es, ok := st.(*ast.ExprStmt)
	call, ok2 := asCall(es, ok)
	if !ok2 {
		return "the error branch starts with " + fi.text(st) + ", not writeJSONError"
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "writeJSONError" || len(call.Args) != 3 {
		return "the error branch answers with " + fi.text(call.Fun) + ", not writeJSONError"
	}
	wid, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return "the writer is " + fi.text(call.Args[0]) + ", not a parameter"
	}
	if _, ok := fi.param(wid, "ResponseWriter"); !ok {
		return "the writer " + wid.Name + " is not an http.ResponseWriter parameter"
	}
	if !isBadRequest(call.Args[1], fi.httpName) {
		return "the status is " + fi.text(call.Args[1]) + ", not 400"
	}
	lit, ok := call.Args[2].(*ast.BasicLit)
	if _, isStr := stringLit(call.Args[2]); !ok || !isStr {
		return "the message is " + fi.text(call.Args[2]) + ", not a string literal"
	}
	m.w, m.msg = wid, lit
	return ""
}

func asCall(es *ast.ExprStmt, ok bool) (*ast.CallExpr, bool) {
	if !ok {
		return nil, false
	}
	call, ok := es.X.(*ast.CallExpr)
	return call, ok
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// isBadRequest reports whether e is http.StatusBadRequest or 400.
func isBadRequest(e ast.Expr, httpName string) bool {
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Kind == token.INT && lit.Value == "400"
	}
	return isPkgSel(e, httpName, "StatusBadRequest")
}

// simpleTarget reports whether p is x, &x or &x.f...: evaluating it before
// the decode instead of after json.NewDecoder(r.Body) changes nothing.
func simpleTarget(p ast.Expr) bool {
	if u, ok := p.(*ast.UnaryExpr); ok && u.Op == token.AND {
		p = u.X
	}
	for {
		switch x := p.(type) {
		case *ast.Ident:
			return true
		case *ast.SelectorExpr:
			p = x.X
		case *ast.ParenExpr:
			p = x.X
		default:
			return false
		}
	}
}

// decodeSite builds the rewrite of a matched decode.
func (fi *fileInfo) decodeSite(m *decodeIf) site {
	msg, _ := stringLit(m.msg)
	s := site{file: fi.path, line: fi.line(m.stmt.Pos()), shape: "if decode 400"}
	args := fi.text(m.w) + ", " + fi.text(m.r) + ", " + fi.text(m.p)
	if msg == invalidBody {
		s.kind, s.helper = kindLiteral, hDecodeJSON
		s.line = fi.line(m.msg.Pos())
	} else {
		s.kind, s.helper = kindDecode, hDecodeJSONMsg
		args += ", " + m.msg.Value
	}
	s.start, s.end = fi.off(m.stmt.Pos()), fi.off(m.stmt.End())
	s.text = "if !" + s.helper + "(" + args + ") {\n" + fi.text(m.ret) + "\n}"
	return s
}

// literalReason says why an "invalid request body" literal that is not
// the message of a matched decode is left as it is, from what it sits in.
func (fi *fileInfo) literalReason(stack []ast.Node) string {
	call, _ := stack[len(stack)-2].(*ast.CallExpr)
	what := "the literal"
	if call != nil {
		what = "the message of " + fi.text(call.Fun)
	}
	for i := len(stack) - 1; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.IfStmt:
			if _, reason := fi.matchDecode(n); reason != "" {
				return what + " after a JSON decode, but " + reason
			}
			if _, _, _, ok := fi.decodeCall(n); !ok {
				return what + " in `if " + fi.text(n.Cond) + "`, which is not a JSON decode's error check"
			}
		case *ast.FuncDecl, *ast.FuncLit:
			return what + ", not in a decode's error check"
		}
	}
	return what + ", not in a function"
}
