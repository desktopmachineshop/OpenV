package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"sort"
	"strings"
)

// flat is one entry of the table as Tools() returns it, wherever it is
// written: in the function's own literal, or in the literal of a
// constructor the function concatenates.
type flat struct {
	name string
	fn   string // the function whose literal holds it
	file string
	text string // go/printer's rendering, with the comments inside it and above it
}

// flatten lists the table's entries in the order the function returns
// them. It reads both shapes: return []T{...}, and return
// slices.Concat(a(), b(), ...) or return a(), where each constructor is a
// function of the package that returns a []T literal.
func flatten(p *pkg, name string) ([]flat, error) {
	f, fn, err := p.findFunc(name)
	if err != nil {
		return nil, err
	}
	elem, err := sliceResult(fn)
	if err != nil {
		return nil, fmt.Errorf("%s() %w", name, err)
	}
	ret, err := onlyReturn(fn)
	if err != nil {
		return nil, err
	}
	if lit, ok := ret.(*ast.CompositeLit); ok {
		return p.flatLiteral(f, fn, lit, elem)
	}
	var calls []ast.Expr
	switch call := ret.(type) {
	case *ast.CallExpr:
		if id, ok := call.Fun.(*ast.Ident); ok && len(call.Args) == 0 {
			calls = []ast.Expr{id}
			break
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Concat" || !isImportOf(f, sel.X, "slices") || call.Ellipsis.IsValid() {
			return nil, fmt.Errorf("%s() returns neither a []%s literal, nor slices.Concat of constructors, nor one constructor", name, elem)
		}
		for _, a := range call.Args {
			c, ok := a.(*ast.CallExpr)
			if !ok || len(c.Args) != 0 {
				return nil, fmt.Errorf("%s(): slices.Concat takes something other than a constructor call: %s", name, oneLine(p.text(f, a)))
			}
			calls = append(calls, c.Fun)
		}
	default:
		return nil, fmt.Errorf("%s() does not return a []%s literal or a concatenation of constructors", name, elem)
	}
	var out []flat
	for _, c := range calls {
		id, ok := c.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("%s() calls %s, which is not a function of its package", name, oneLine(p.text(f, c)))
		}
		cf, cfn, err := p.findFunc(id.Name)
		if err != nil {
			return nil, fmt.Errorf("%s() calls %s(): %w", name, id.Name, err)
		}
		if ce, err := sliceResult(cfn); err != nil || ce != elem {
			return nil, fmt.Errorf("constructor %s() must take nothing and return []%s", id.Name, elem)
		}
		body, err := onlyReturn(cfn)
		if err != nil {
			return nil, err
		}
		lit, ok := body.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("constructor %s() must return a []%s literal", id.Name, elem)
		}
		entries, err := p.flatLiteral(cf, cfn, lit, elem)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	return out, nil
}

func onlyReturn(fn *ast.FuncDecl) (ast.Expr, error) {
	if fn.Body != nil && len(fn.Body.List) == 1 {
		if ret, ok := fn.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			return ret.Results[0], nil
		}
	}
	return nil, fmt.Errorf("%s() must hold one statement, a return of one value", fn.Name.Name)
}

func isImportOf(f *goFile, x ast.Expr, ip string) bool {
	id, ok := x.(*ast.Ident)
	return ok && id.Obj == nil && f.imports[id.Name] == ip
}

// flatLiteral renders each entry of a []T literal with the comments inside
// it, above it (after the previous entry) and after it on its last line.
func (p *pkg) flatLiteral(f *goFile, fn *ast.FuncDecl, lit *ast.CompositeLit, elem string) ([]flat, error) {
	if !isSliceOf(lit.Type, elem) {
		return nil, fmt.Errorf("%s() returns a literal that is not []%s", fn.Name.Name, elem)
	}
	var out []flat
	prev := lit.Lbrace
	for i, e := range lit.Elts {
		cl, ok := e.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("entry %d of %s() is not a %s{...} literal", i+1, fn.Name.Name, elem)
		}
		name, err := entryName(cl)
		if err != nil {
			return nil, fmt.Errorf("entry %d of %s() %w", i+1, fn.Name.Name, err)
		}
		next := lit.Rbrace
		if i+1 < len(lit.Elts) {
			next = lit.Elts[i+1].Pos()
		}
		var b strings.Builder
		endLine, prevLine := p.fset.Position(cl.End()).Line, p.fset.Position(prev).Line
		for _, cg := range f.ast.Comments {
			// A comment on the previous entry's last line is that entry's.
			if cg.Pos() > prev && cg.End() <= cl.Pos() && p.fset.Position(cg.Pos()).Line > prevLine {
				b.WriteString("above: " + commentText(cg))
			}
		}
		b.WriteString(printNode(p.fset, f, cl) + "\n")
		for _, cg := range f.ast.Comments {
			if cg.Pos() >= cl.End() && cg.End() <= next && p.fset.Position(cg.Pos()).Line == endLine {
				b.WriteString("after: " + commentText(cg))
			}
		}
		out = append(out, flat{name: name, fn: fn.Name.Name, file: f.name, text: b.String()})
		prev = cl.End()
	}
	return out, nil
}

// printNode renders a node as gofmt would, with the comments inside it.
func printNode(fset *token.FileSet, f *goFile, n ast.Node) string {
	var b bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&b, fset, &printer.CommentedNode{Node: n, Comments: f.ast.Comments}); err != nil {
		return "!print: " + err.Error()
	}
	return b.String()
}

func commentText(cg *ast.CommentGroup) string {
	var b strings.Builder
	for _, c := range cg.List {
		b.WriteString(c.Text + "\n")
	}
	return b.String()
}

// compareFlat reports every way two flattened tables differ: an entry
// added, dropped or moved, or an entry whose text changed.
func compareFlat(base, head []flat) []string {
	var out []string
	names := func(l []flat) []string {
		s := make([]string, len(l))
		for i, e := range l {
			s[i] = e.name
		}
		return s
	}
	bn, hn := names(base), names(head)
	if strings.Join(bn, "\n") != strings.Join(hn, "\n") {
		out = append(out, fmt.Sprintf("the order differs:\n  base: %s\n  head: %s", strings.Join(bn, ", "), strings.Join(hn, ", ")))
	}
	byName := map[string]flat{}
	for _, e := range head {
		byName[e.name] = e
	}
	for _, b := range base {
		h, ok := byName[b.name]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%q is gone", b.name))
		case h.text != b.text:
			out = append(out, fmt.Sprintf("%q changed (%s in %s):\n%s", b.name, h.fn, h.file, lineDiff(b.text, h.text)))
		}
	}
	return out
}

// lineDiff shows the first lines where two texts part.
func lineDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("  line %d\n  - %s\n  + %s", i+1, x, y)
		}
	}
	return "  (same lines)"
}

// otherDecls renders every package-level declaration but the named ones,
// keyed by name (a method as Recv.Name), each with its doc comment and the
// import paths its qualifiers resolve to, so that dropping an import that
// a declaration still uses shows as a change.
func otherDecls(p *pkg, skip map[string]bool) map[string]string {
	out := map[string]string{}
	add := func(key, text string) {
		k := key
		for n := 2; out[k] != ""; n++ {
			k = fmt.Sprintf("%s#%d", key, n)
		}
		out[k] = text
	}
	for _, f := range p.files {
		for _, d := range f.ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			key := declKey(d)
			if skip[key] {
				continue
			}
			add(f.ast.Name.Name+"."+key, printNode(p.fset, f, d)+"\nimports: "+qualifiers(f, d))
		}
	}
	return out
}

func declKey(d ast.Decl) string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv != nil && len(x.Recv.List) > 0 {
			return recvName(x.Recv.List[0].Type) + "." + x.Name.Name
		}
		return x.Name.Name
	case *ast.GenDecl:
		return x.Tok.String() + " " + strings.Join(declNames(x), ",")
	}
	return "?"
}

func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.ParenExpr:
		return recvName(x.X)
	case *ast.IndexExpr:
		return recvName(x.X)
	case *ast.IndexListExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}

// qualifiers lists "name=path" for each import the node uses.
func qualifiers(f *goFile, n ast.Node) string {
	seen := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if sel, ok := x.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
				if ip, ok := f.imports[id.Name]; ok {
					seen[id.Name+"="+ip] = true
				} else if f.dots == 0 {
					// Not this file's import nor its own declaration (the
					// parser resolves those): another file's, or a
					// qualifier whose import is gone.
					seen[id.Name+"=?"] = true
				}
			}
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for q := range seen {
		out = append(out, q)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// compareDecls reports the declarations that differ between two renderings.
func compareDecls(before, after map[string]string) []string {
	var out []string
	for k, v := range before {
		switch w, ok := after[k]; {
		case !ok:
			out = append(out, k+" is gone")
		case w != v:
			out = append(out, k+" changed:\n"+lineDiff(v, w))
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			out = append(out, k+" is new")
		}
	}
	sort.Strings(out)
	return out
}
