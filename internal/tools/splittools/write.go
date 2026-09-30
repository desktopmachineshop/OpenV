package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// newFiles renders each new file: its header, the package clause, the
// imports its entries use (named as the table's file names them), and its
// constructors in spec order, each entry's lines copied as they are. The
// constructors sit at the literal's depth, so gofmt leaves every copied
// line alone.
func newFiles(p *pkg, t *table, plans []*ctorPlan, headers map[string]string) (map[string][]byte, []string, error) {
	var order []string
	byFile := map[string][]*ctorPlan{}
	for _, cp := range plans {
		if byFile[cp.File] == nil {
			order = append(order, cp.File)
		}
		byFile[cp.File] = append(byFile[cp.File], cp)
	}
	out := map[string][]byte{}
	for _, name := range order {
		var b bytes.Buffer
		if h := strings.TrimSpace(headers[name]); h != "" {
			b.WriteString(h + "\n\n")
		}
		fmt.Fprintf(&b, "package %s\n", p.pkgName())
		need := map[string]string{}
		for _, cp := range byFile[name] {
			for q := range usedImports(t.file, cp.entries) {
				need[q] = t.file.imports[q]
			}
		}
		b.WriteString(importBlock(need))
		for _, cp := range byFile[name] {
			b.WriteString("\n")
			if doc := strings.TrimSpace(cp.Doc); doc != "" {
				b.WriteString(doc + "\n")
			}
			fmt.Fprintf(&b, "func %s() []%s {\n\treturn []%s{\n", cp.Func, t.elem, t.elem)
			for i, e := range cp.entries {
				b.Write(entryText(e, i))
			}
			b.WriteString("\t}\n}\n")
		}
		src, err := format.Source(b.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("%s: gofmt: %v", name, err)
		}
		out[name] = src
	}
	return out, order, nil
}

// usedImports returns the local names of f's imports that the entries use
// as package qualifiers. A name bound inside an entry (a parameter, a local
// variable) resolves to its object, so it is not taken for a package.
func usedImports(f *goFile, entries []entry) map[string]bool {
	used := map[string]bool{}
	for _, e := range entries {
		ast.Inspect(e.elt, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
					if _, ok := f.imports[id.Name]; ok {
						used[id.Name] = true
					}
				}
			}
			return true
		})
	}
	return used
}

// importBlock renders imports as goimports groups them: the standard
// library, then the rest, each sorted by path; an import keeps the name it
// had when that differs from the one its path implies.
func importBlock(need map[string]string) string {
	if len(need) == 0 {
		return ""
	}
	var std, other []string
	for name, ip := range need {
		spec := strconv.Quote(ip)
		if name != assumedName(ip) {
			spec = name + " " + spec
		}
		if isStd(ip) {
			std = append(std, spec)
		} else {
			other = append(other, spec)
		}
	}
	byPath := func(l []string) {
		sort.Slice(l, func(i, j int) bool { return unnamed(l[i]) < unnamed(l[j]) })
	}
	byPath(std)
	byPath(other)
	var b strings.Builder
	b.WriteString("\nimport (\n")
	for _, s := range std {
		b.WriteString("\t" + s + "\n")
	}
	if len(std) > 0 && len(other) > 0 {
		b.WriteString("\n")
	}
	for _, s := range other {
		b.WriteString("\t" + s + "\n")
	}
	b.WriteString(")\n")
	return b.String()
}

func unnamed(spec string) string {
	if i := strings.IndexByte(spec, '"'); i >= 0 {
		return spec[i:]
	}
	return spec
}

// isStd reports whether an import path is the standard library's: its
// first element has no dot.
func isStd(ip string) bool {
	first, _, _ := strings.Cut(ip, "/")
	return !strings.Contains(first, ".")
}

// rewriteTable replaces the function's literal with the concatenation of
// the constructors, in order (a single constructor is returned as it is),
// drops the imports only the moved entries used, and adds slices when the
// concatenation needs it. Nothing else in the file changes.
func rewriteTable(p *pkg, t *table, plans []*ctorPlan) ([]byte, error) {
	tf := p.fset.File(t.lit.Pos())
	src := t.file.src
	slicesName, add, err := slicesImport(p, t, plans)
	if err != nil {
		return nil, err
	}
	var call strings.Builder
	if len(plans) == 1 {
		call.WriteString(plans[0].Func + "()")
		add = false
	} else {
		call.WriteString(slicesName + ".Concat(\n")
		for _, cp := range plans {
			call.WriteString("\t\t" + cp.Func + "(),\n")
		}
		call.WriteString("\t)")
	}
	var b bytes.Buffer
	b.Write(src[:tf.Offset(t.lit.Pos())])
	b.WriteString(call.String())
	b.Write(src[tf.Offset(t.lit.Rbrace)+1:])
	out, err := dropUnusedImports(t.file.name, b.Bytes())
	if err != nil {
		return nil, err
	}
	if add {
		if out, err = addStdImport(t.file.name, out, "slices"); err != nil {
			return nil, err
		}
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("%s: gofmt: %v", t.file.name, err)
	}
	return formatted, nil
}

// slicesImport returns the name the table's file refers to package slices
// by, and whether the import must be added. The name must be free.
func slicesImport(p *pkg, t *table, plans []*ctorPlan) (string, bool, error) {
	for name, ip := range t.file.imports {
		if ip == "slices" {
			return name, false, nil
		}
	}
	if len(plans) == 1 {
		return "", false, nil
	}
	if ip, ok := t.file.imports["slices"]; ok {
		return "", false, refuse(fmt.Errorf("%s imports %s as slices; the concatenation needs package slices under that name", t.file.name, ip))
	}
	for _, f := range p.files {
		if !p.sameScope(f) {
			continue
		}
		for _, d := range f.ast.Decls {
			for _, name := range declNames(d) {
				if name == "slices" {
					return "", false, refuse(fmt.Errorf("%s declares slices, which the concatenation needs for package slices", f.name))
				}
			}
		}
	}
	for _, cp := range plans {
		if cp.Func == "slices" {
			return "", false, refuse(fmt.Errorf("constructor slices would hide package slices, which the concatenation needs"))
		}
	}
	return "slices", true, nil
}

// dropUnusedImports removes the import specs of src that no code in it uses
// any more (blank and dot imports stay), each with its whole line, and an
// import declaration left empty.
func dropUnusedImports(name string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s after the rewrite: %v", name, err)
	}
	used := map[string]bool{}
	ast.Inspect(af, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
				used[id.Name] = true
			}
		}
		return true
	})
	tf := fset.File(af.Pos())
	type cut struct{ from, to int }
	var cuts []cut
	for _, d := range af.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		kept := 0
		var drop []cut
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			ip, _ := strconv.Unquote(is.Path.Value)
			local := assumedName(ip)
			if is.Name != nil {
				local = is.Name.Name
			}
			if local == "_" || local == "." || used[local] {
				kept++
				continue
			}
			from := is.Pos()
			if is.Doc != nil {
				from = is.Doc.Pos()
			}
			drop = append(drop, cut{lineStart(tf, tf.Line(from)), lineEndOffset(tf, src, tf.Line(is.End()))})
		}
		if kept == 0 && len(drop) > 0 {
			from := gd.Pos()
			if gd.Doc != nil {
				from = gd.Doc.Pos()
			}
			drop = []cut{{lineStart(tf, tf.Line(from)), lineEndOffset(tf, src, tf.Line(gd.End()))}}
		}
		cuts = append(cuts, drop...)
	}
	out := append([]byte(nil), src...)
	for i := len(cuts) - 1; i >= 0; i-- {
		out = append(out[:cuts[i].from], out[cuts[i].to:]...)
	}
	return out, nil
}

// addStdImport adds a standard-library import to the first group of
// standard-library imports, or starts a group or a declaration for it;
// gofmt then sorts it into place.
func addStdImport(name string, src []byte, ip string) ([]byte, error) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	tf := fset.File(af.Pos())
	line := "\t" + strconv.Quote(ip) + "\n"
	splice := func(at int, text string) []byte {
		return append(append(append([]byte(nil), src[:at]...), text...), src[at:]...)
	}
	for _, d := range af.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if !gd.Lparen.IsValid() {
			is := gd.Specs[0].(*ast.ImportSpec)
			sep := "" // one group with a standard-library import
			if p, _ := strconv.Unquote(is.Path.Value); !isStd(p) {
				sep = "\n"
			}
			block := "import (\n" + line + sep + "\t" + string(src[tf.Offset(is.Pos()):tf.Offset(is.End())]) + "\n)"
			return append(append(append([]byte(nil), src[:tf.Offset(gd.Pos())]...), block...), src[tf.Offset(gd.End()):]...), nil
		}
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			if p, _ := strconv.Unquote(is.Path.Value); isStd(p) {
				return splice(lineStart(tf, tf.Line(is.Pos())), line), nil
			}
		}
		return splice(lineEndOffset(tf, src, tf.Line(gd.Lparen)), line+"\n"), nil
	}
	return splice(lineEndOffset(tf, src, tf.Line(af.Name.End())), "\nimport "+strconv.Quote(ip)+"\n"), nil
}
