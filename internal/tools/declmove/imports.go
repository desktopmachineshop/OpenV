package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Import handling: moved code takes its imports with it, exactly as its
// source file declares them (alias included), and a source file drops the
// imports only the moved-out code used. The pinned goimports then only has
// to tidy the import blocks; it never has to guess which package a name
// means, and declhash's per-declaration import paths prove it did not.

// addImports adds to src the imports in need that it does not have yet. An
// import joins the first group that holds standard-library imports, or the
// last group that holds others; failing that it starts a group.
func addImports(name string, src []byte, need map[string]importRef) ([]byte, error) {
	if len(need) == 0 {
		return src, nil
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	have := map[string]string{}
	paths := map[string]bool{}
	for _, is := range af.Imports {
		ip, _ := strconv.Unquote(is.Path.Value)
		local := assumedName(ip)
		if is.Name != nil {
			local = is.Name.Name
		}
		have[local] = ip
		paths[ip] = true
	}
	var add []importRef
	for q, ref := range need {
		switch ip, ok := have[q]; {
		case ref.name == "_":
			if !paths[ref.path] {
				add = append(add, ref)
			}
		case !ok:
			add = append(add, ref)
		case ip != ref.path:
			return nil, fmt.Errorf("it imports %s as %s, but the code moving in names %s so", ip, q, ref.path)
		}
	}
	if len(add) == 0 {
		return src, nil
	}
	sort.Slice(add, func(i, j int) bool { return add[i].path < add[j].path })
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var decls []*ast.GenDecl
	for _, d := range af.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			decls = append(decls, gd)
		}
	}
	switch {
	case len(decls) == 0:
		at := lineEnd(src, off(af.Name.End()))
		return splice(src, at, at, "\n"+importBlock(add)), nil
	case len(decls) == 1 && !decls[0].Lparen.IsValid():
		gd := decls[0]
		is := gd.Specs[0].(*ast.ImportSpec)
		ip, _ := strconv.Unquote(is.Path.Value)
		old := importRef{path: ip, text: string(src[off(is.Pos()):off(is.End())])}
		return splice(src, off(gd.Pos()), off(gd.End()), strings.TrimSuffix(importBlock(append(add, old)), "\n")), nil
	}
	var block *ast.GenDecl
	for _, gd := range decls {
		if gd.Lparen.IsValid() {
			block = gd
		}
	}
	if block == nil {
		at := lineEnd(src, off(decls[len(decls)-1].End()))
		return splice(src, at, at, "\n"+importBlock(add)), nil
	}
	return insertIntoBlock(fset, src, block, add), nil
}

// insertIntoBlock adds imports to a parenthesized import declaration.
func insertIntoBlock(fset *token.FileSet, src []byte, gd *ast.GenDecl, add []importRef) []byte {
	type group struct {
		end      int // offset just past the group's last line
		std, ext bool
	}
	var groups []group
	lastLine := -1
	for _, s := range gd.Specs {
		is := s.(*ast.ImportSpec)
		ip, _ := strconv.Unquote(is.Path.Value)
		line := fset.Position(is.Pos()).Line
		if lastLine < 0 || line > lastLine+1 {
			groups = append(groups, group{})
		}
		g := &groups[len(groups)-1]
		g.end = lineEnd(src, fset.Position(is.End()).Offset)
		g.std, g.ext = g.std || isStd(ip), g.ext || !isStd(ip)
		lastLine = fset.Position(is.End()).Line
	}
	inserts := map[int]string{}
	var fresh []importRef
	for _, ref := range add {
		at := -1
		for i := range groups {
			if isStd(ref.path) && groups[i].std {
				at = groups[i].end
				break
			}
			if !isStd(ref.path) && groups[i].ext {
				at = groups[i].end
			}
		}
		if at < 0 {
			fresh = append(fresh, ref)
			continue
		}
		inserts[at] += importLine(ref)
	}
	if len(fresh) > 0 {
		rp := fset.Position(gd.Rparen).Offset
		at := bytes.LastIndexByte(src[:rp], '\n') + 1
		lines := ""
		if len(groups) > 0 {
			lines = "\n"
		}
		for _, ref := range fresh {
			lines += importLine(ref)
		}
		inserts[at] += lines
	}
	offs := make([]int, 0, len(inserts))
	for at := range inserts {
		offs = append(offs, at)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(offs)))
	for _, at := range offs {
		src = splice(src, at, at, inserts[at])
	}
	return src
}

// importBlock renders an import declaration: the standard-library imports,
// then the others in a group of their own.
func importBlock(refs []importRef) string {
	if len(refs) == 1 {
		return "import " + strings.TrimSpace(importLine(refs[0])) + "\n"
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, std := range []bool{true, false} {
		wrote := false
		for _, ref := range refs {
			if isStd(ref.path) == std {
				if !wrote && b.Len() > len("import (\n") {
					b.WriteString("\n")
				}
				b.WriteString(importLine(ref))
				wrote = true
			}
		}
	}
	b.WriteString(")\n")
	return b.String()
}

func importLine(ref importRef) string {
	if ref.text != "" {
		return "\t" + ref.text + "\n"
	}
	if ref.named || assumedName(ref.path) != ref.name {
		return "\t" + ref.name + " " + strconv.Quote(ref.path) + "\n"
	}
	return "\t" + strconv.Quote(ref.path) + "\n"
}

// isStd applies goimports' rule: a standard-library path has no dot in its
// first element.
func isStd(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

// dropUnusedImports removes from src, the new content of before, each
// import that before used as a qualifier and src no longer does. Blank and
// dot imports, and imports declhash cannot see used, are left alone.
func dropUnusedImports(before *goFile, src []byte) ([]byte, error) {
	usedBefore := usedQualifiers(before, before.ast)
	p, err := parsePackage([]source{{before.name, src}}, true)
	if err != nil {
		return nil, err
	}
	after := p.files[0]
	usedAfter := usedQualifiers(after, after.ast)
	off := func(pos token.Pos) int { return p.fset.Position(pos).Offset }
	var cuts [][2]int
	for _, d := range after.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var drop [][2]int
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			ip, _ := strconv.Unquote(is.Path.Value)
			name := assumedName(ip)
			if is.Name != nil {
				name = is.Name.Name
			}
			if usedBefore[name] && !usedAfter[name] {
				start := is.Pos()
				if is.Doc != nil {
					start = is.Doc.Pos()
				}
				drop = append(drop, [2]int{lineStart(src, off(start)), lineEnd(src, off(is.End()))})
			}
		}
		switch {
		case len(drop) == 0:
		case len(drop) == len(gd.Specs):
			cuts = append(cuts, [2]int{lineStart(src, off(gd.Pos())), lineEnd(src, off(gd.End()))})
		default:
			cuts = append(cuts, drop...)
		}
	}
	return cut(src, cuts), nil
}

// lineStart and lineEnd widen an offset to the start of its line, or to
// just past the newline that ends it.
func lineStart(src []byte, off int) int {
	return bytes.LastIndexByte(src[:off], '\n') + 1
}

func lineEnd(src []byte, off int) int {
	if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}

func splice(src []byte, from, to int, text string) []byte {
	out := make([]byte, 0, len(src)+len(text))
	out = append(out, src[:from]...)
	out = append(out, text...)
	return append(out, src[to:]...)
}
