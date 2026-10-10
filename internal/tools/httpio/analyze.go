package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
)

// parseFile parses src, keeping comments and resolving identifiers to
// their declarations in the file (parameters among them).
func parseFile(path string, src []byte) (*fileInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &fileInfo{
		path:     path,
		fset:     fset,
		file:     f,
		src:      src,
		jsonName: importName(f, "encoding/json"),
		httpName: importName(f, "net/http"),
		base:     filepath.Base(path),
	}, nil
}

// analyze finds every site of the file: each json.NewEncoder call outside
// respond.go and each "invalid request body" literal, as the ratchets
// count them, and each decode answered 400 with another literal message.
// Sites come back in source order.
func (fi *fileInfo) analyze() []site {
	var sites []site
	matched := map[*ast.BasicLit]bool{}
	var stack []ast.Node
	ast.Inspect(fi.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.IfStmt:
			if fc := enclosing(stack); fc.decl != nil && fc.decl.Recv == nil && isHelperName(fc.decl.Name.Name) {
				return true
			}
			if m, _ := fi.matchDecode(n); m != nil {
				matched[m.msg] = true
				sites = append(sites, fi.decodeSite(m))
			}
		case *ast.CallExpr:
			if isPkgCall(n, fi.jsonName, "NewEncoder") && fi.base != "respond.go" {
				sites = append(sites, fi.encodeSite(stack))
			}
		case *ast.BasicLit:
			if s, ok := stringLit(n); ok && s == invalidBody && !matched[n] {
				sites = append(sites, fi.literalSite(n, stack))
			}
		}
		return true
	})
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].line < sites[j].line })
	return sites
}

// literalSite classifies an "invalid request body" literal that is not the
// message of a matched decode: it becomes the constant invalidRequestBody,
// the same untyped string constant, unless it is the constant's own value.
// A struct tag or an import path is never such a literal (neither is an
// expression), so the swap can change no value.
func (fi *fileInfo) literalSite(lit *ast.BasicLit, stack []ast.Node) site {
	s := site{file: fi.path, line: fi.line(lit.Pos()), kind: kindLiteral}
	for _, n := range stack {
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Names) == 1 && vs.Names[0].Name == hInvalidBody {
			s.reason, s.kept = "the constant "+hInvalidBody+"'s own value, the one literal the ratchet keeps", true
			return s
		}
	}
	switch stack[len(stack)-2].(type) {
	case *ast.Field, *ast.ImportSpec:
		s.reason = "a struct tag or import path, not a string value"
		return s
	}
	s.helper, s.shape = hInvalidBody, "literal; "+fi.literalReason(stack)
	s.start, s.end, s.text = fi.off(lit.Pos()), fi.off(lit.End()), hInvalidBody
	return s
}
