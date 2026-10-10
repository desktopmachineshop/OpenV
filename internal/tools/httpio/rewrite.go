package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"sort"
)

// rewrite applies the rewrites among sites to src, drops the imports of
// encoding/json the file no longer uses, and gofmts the result. Each
// rewrite replaces exactly the statements it matched, in place.
func rewrite(path string, src []byte, sites []site) ([]byte, error) {
	var edits []site
	for _, s := range sites {
		if s.helper != "" {
			edits = append(edits, s)
		}
	}
	if len(edits) == 0 {
		return src, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), src...)
	prev := len(out) + 1
	for _, e := range edits {
		if e.end > prev {
			return nil, fmt.Errorf("%s:%d: rewrites overlap", path, e.line)
		}
		out = append(out[:e.start:e.start], append([]byte(e.text), out[e.end:]...)...)
		prev = e.start
	}
	out, err := dropUnusedImport(path, out, "encoding/json")
	if err != nil {
		return nil, err
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("%s: the rewrite does not format: %w", path, err)
	}
	return formatted, nil
}

// dropUnusedImport removes the import of path when src no longer refers to
// the name it binds.
func dropUnusedImport(file string, src []byte, path string) ([]byte, error) {
	fi, err := parseFile(file, src)
	if err != nil {
		return nil, fmt.Errorf("%s: the rewrite does not parse: %w", file, err)
	}
	name := importName(fi.file, path)
	if name == "" || usesPackage(fi.file, name) {
		return src, nil
	}
	for _, d := range fi.file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || len(gd.Specs) == 0 {
			continue
		}
		for _, sp := range gd.Specs {
			is, ok := sp.(*ast.ImportSpec)
			if !ok || is.Path.Value != `"`+path+`"` {
				continue
			}
			var from, to int
			if len(gd.Specs) == 1 {
				from, to = fi.off(gd.Pos()), fi.off(gd.End())
			} else {
				from, to = fi.off(is.Pos()), fi.off(is.End())
			}
			from = lineStart(src, from)
			to = lineEnd(src, to)
			return append(src[:from:from], src[to:]...), nil
		}
	}
	return src, nil
}

// usesPackage reports whether f refers to the package name binds.
func usesPackage(f *ast.File, name string) bool {
	used := false
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && id.Obj == nil {
				used = true
			}
		}
		return !used
	})
	return used
}

// lineStart returns the offset of the start of the line holding off, when
// only blanks precede off on it.
func lineStart(src []byte, off int) int {
	i := off
	for i > 0 && (src[i-1] == ' ' || src[i-1] == '\t') {
		i--
	}
	if i == 0 || src[i-1] == '\n' {
		return i
	}
	return off
}

// lineEnd returns the offset past the newline ending the line of off, when
// only blanks follow off on it.
func lineEnd(src []byte, off int) int {
	rest := src[off:]
	n := bytes.IndexByte(rest, '\n')
	if n < 0 || len(bytes.TrimSpace(rest[:n])) != 0 {
		return off
	}
	return off + n + 1
}
