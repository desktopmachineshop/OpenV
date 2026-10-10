package main

import (
	"go/ast"
	"go/token"
	"net/http"
	"strconv"
	"strings"
)

// invalidBody is the message the invalid_request_body_literals ratchet
// counts and decodeJSON keeps.
const invalidBody = "invalid request body"

// The kinds of site, after what each ratchet of internal/archtest counts.
const (
	kindEncode  = "encode"  // a json.NewEncoder call (raw_json_encodes)
	kindLiteral = "literal" // an "invalid request body" literal (invalid_request_body_literals)
	kindDecode  = "decode"  // a decode answered 400 with another message (decodeJSONMsg)
)

// site is one candidate: rewritten (helper set) or left (reason set).
type site struct {
	file   string
	line   int
	kind   string
	helper string // the helper the rewrite calls; "" when left
	shape  string // the statement sequence matched, for the report
	reason string // why the site is left as it is
	kept   bool   // the helper's own literal, which the ratchet keeps
	start  int    // byte offsets of what the rewrite replaces
	end    int
	text   string // the replacement
}

// fileInfo is one parsed file and the names its imports bind.
type fileInfo struct {
	path     string
	fset     *token.FileSet
	file     *ast.File
	src      []byte
	jsonName string // the name encoding/json is imported as, or ""
	httpName string // the name net/http is imported as, or ""
	base     string // the file name: respond.go's encodes are not counted
}

func (fi *fileInfo) line(p token.Pos) int { return fi.fset.Position(p).Line }

func (fi *fileInfo) off(p token.Pos) int { return fi.fset.Position(p).Offset }

func (fi *fileInfo) text(n ast.Node) string { return string(fi.src[fi.off(n.Pos()):fi.off(n.End())]) }

// importName returns the name path is imported as in f, or "" when f does
// not import it (or imports it as _ or .).
func importName(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != path {
			continue
		}
		if imp.Name == nil {
			return path[strings.LastIndex(path, "/")+1:]
		}
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return ""
		}
		return imp.Name.Name
	}
	return ""
}

// isPkgCall reports whether call is pkg.name(...), with pkg the package
// name a file binds (not shadowed by a local).
func isPkgCall(call *ast.CallExpr, pkg, name string) bool {
	if pkg == "" {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && id.Obj == nil
}

// isPkgSel reports whether e is pkg.name.
func isPkgSel(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || pkg == "" || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && id.Obj == nil
}

// stringLit returns the value of a string literal.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// param resolves id to a parameter of an enclosing function, and returns
// it when the parameter's type is want ("ResponseWriter" for
// http.ResponseWriter, "*Request" for *http.Request).
func (fi *fileInfo) param(id *ast.Ident, want string) (*ast.Object, bool) {
	if id.Obj == nil || id.Obj.Kind != ast.Var {
		return nil, false
	}
	field, ok := id.Obj.Decl.(*ast.Field)
	if !ok {
		return nil, false
	}
	t := field.Type
	if strings.HasPrefix(want, "*") {
		star, ok := t.(*ast.StarExpr)
		if !ok {
			return nil, false
		}
		t, want = star.X, want[1:]
	}
	return id.Obj, isPkgSel(t, fi.httpName, want)
}

// writerCall matches W.method(args...) for the writer object w and returns
// the call.
func writerCall(s ast.Stmt, w *ast.Object, method string) (*ast.CallExpr, bool) {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != method {
		return nil, false
	}
	id, ok := sel.X.(*ast.Ident)
	return call, ok && id.Obj == w
}

// headerCall matches W.Header().method(args...) and returns the call.
func headerCall(s ast.Stmt, w *ast.Object) (call *ast.CallExpr, method string, ok bool) {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return nil, "", false
	}
	call, ok = es.X.(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, "", false
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok || len(inner.Args) != 0 {
		return nil, "", false
	}
	hsel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || hsel.Sel.Name != "Header" {
		return nil, "", false
	}
	id, ok := hsel.X.(*ast.Ident)
	if !ok || id.Obj != w {
		return nil, "", false
	}
	return call, sel.Sel.Name, true
}

// contentType reports whether s sets the writer's Content-Type, and to
// what (the value's source text when it is not a string literal).
func (fi *fileInfo) contentType(s ast.Stmt, w *ast.Object) (value string, ok bool) {
	call, method, ok := headerCall(s, w)
	if !ok || (method != "Set" && method != "Add") || len(call.Args) != 2 {
		return "", false
	}
	key, ok := stringLit(call.Args[0])
	if !ok || http.CanonicalHeaderKey(key) != "Content-Type" {
		return "", false
	}
	if method == "Set" && key == "Content-Type" {
		if v, ok := stringLit(call.Args[1]); ok {
			return v, true
		}
	}
	return fi.text(call), true
}

// isWriterStmt reports whether s writes the response's header or status:
// W.Header().<anything>(...) or W.WriteHeader(...).
func isWriterStmt(s ast.Stmt, w *ast.Object) bool {
	if _, _, ok := headerCall(s, w); ok {
		return true
	}
	_, ok := writerCall(s, w, "WriteHeader")
	return ok
}

// mentions reports whether n refers to the object o anywhere.
func mentions(n ast.Node, o *ast.Object) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Obj == o {
			found = true
		}
		return !found
	})
	return found
}

// localFuncCall returns the name of a function value declared in the file
// (a local or a parameter, not a function declaration) that e calls: such a
// closure may write the response, so evaluating it earlier is not safe.
func localFuncCall(e ast.Expr) string {
	name := ""
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || name != "" {
			return name == ""
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Obj != nil && id.Obj.Kind == ast.Var {
			name = id.Name
		}
		return name == ""
	})
	return name
}

// commentsOutside returns the first comment inside [from, to) that lies
// outside every one of keep: the rewrite would drop it.
func (fi *fileInfo) commentsOutside(from, to token.Pos, keep ...ast.Node) *ast.Comment {
	for _, g := range fi.file.Comments {
		for _, c := range g.List {
			if c.Pos() < from || c.End() > to {
				continue
			}
			inside := false
			for _, k := range keep {
				if k != nil && c.Pos() >= k.Pos() && c.End() <= k.End() {
					inside = true
				}
			}
			if !inside {
				return c
			}
		}
	}
	return nil
}
