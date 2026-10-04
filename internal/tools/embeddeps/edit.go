package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/types"
	"sort"
	"strings"
)

// embedDoc is the doc comment of the embedded field.
var embedDoc = []string{
	"// " + depsName + " holds every dependency " + newName + " was given, embedded so",
	"// that a dependency is declared once, as a " + depsName + " field. The",
	"// fields below are what " + newName + " derives from it; a handler reads",
	"// those, not the " + depsName + " fields they come from.",
}

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
}

// render applies the plan and returns the new source of every file it
// changes, gofmt'd.
func (p *plan) render() (map[string][]byte, error) {
	edits := map[*srcFile][]edit{}
	add := func(f *srcFile, e edit) { edits[f] = append(edits[f], e) }
	for _, r := range p.renames {
		f := p.ps.fileOf(r.sel.Pos())
		add(f, edit{f.tf.Offset(r.sel.Pos()), f.tf.Offset(r.sel.End()), r.to})
	}
	sf := p.ps.fileOf(p.st.Pos())
	body, err := p.structBody(sf)
	if err != nil {
		return nil, err
	}
	add(sf, body)
	lf := p.ps.fileOf(p.lit.Pos())
	litEdits, err := p.literalEdits(lf)
	if err != nil {
		return nil, err
	}
	for _, e := range litEdits {
		add(lf, e)
	}
	out := map[string][]byte{}
	for f, es := range edits {
		sort.Slice(es, func(i, j int) bool { return es[i].start < es[j].start })
		src := append([]byte{}, f.src...)
		for i := len(es) - 1; i >= 0; i-- {
			if i > 0 && es[i-1].end > es[i].start {
				return nil, fmt.Errorf("internal error: overlapping edits in %s at offset %d", f.name, es[i].start)
			}
			src = append(src[:es[i].start], append([]byte(es[i].text), src[es[i].end:]...)...)
		}
		formatted, err := format.Source(src)
		if err != nil {
			return nil, fmt.Errorf("internal error: %s does not parse after the rewrite: %v", f.name, err)
		}
		out[f.name] = formatted
	}
	return out, nil
}

// lines is a span of whole lines, first and last 1-based and inclusive.
type lines struct{ first, last int }

// fieldLines is the lines a copied field's declaration takes, with its doc
// and line comments.
func (p *plan) fieldLines(f *srcFile, c *copied) lines {
	start, end := c.decl.Pos(), c.decl.End()
	if c.decl.Doc != nil {
		start = c.decl.Doc.Pos()
	}
	if c.decl.Comment != nil {
		end = c.decl.Comment.End()
	}
	return lines{f.tf.Line(start), f.tf.Line(end)}
}

// structBody rewrites Handler's field list: HandlerDeps first, then every
// line that is not a copied field's, with blank lines collapsed.
func (p *plan) structBody(f *srcFile) (edit, error) {
	open, closing := f.tf.Line(p.st.Fields.Opening), f.tf.Line(p.st.Fields.Closing)
	if open == closing {
		return edit{}, refuse("%s is declared on one line", handlerName)
	}
	drop := map[int]bool{}
	for _, c := range p.copies {
		span := p.fieldLines(f, c)
		for _, other := range p.st.Fields.List {
			if other == c.decl {
				continue
			}
			if l := f.tf.Line(other.Pos()); l >= span.first && l <= span.last ||
				f.tf.Line(other.End()) >= span.first && f.tf.Line(other.End()) <= span.last {
				return edit{}, refuse("%s.%s shares a line with another field at %s", handlerName, c.field.Name(),
					p.ps.where(other.Pos()))
			}
		}
		for l := span.first; l <= span.last; l++ {
			drop[l] = true
		}
	}
	out := append([]string{}, indent(embedDoc)...)
	out = append(out, "\t"+depsName, "")
	for l := open + 1; l < closing; l++ {
		if !drop[l] {
			out = append(out, lineText(f, l))
		}
	}
	out = collapseBlanks(out)
	start := f.tf.Offset(f.tf.LineStart(open + 1))
	end := f.tf.Offset(f.tf.LineStart(closing))
	return edit{start, end, strings.Join(out, "\n") + "\n"}, nil
}

// literalEdits deletes the copied elements of NewHandler's literal, with
// any comment lines right above them, and adds `h.HandlerDeps = deps` on
// the line after the literal's statement.
func (p *plan) literalEdits(f *srcFile) ([]edit, error) {
	var es []edit
	elts := p.lit.Elts
	for i, e := range elts {
		var c *copied
		for _, cc := range p.copies {
			if cc.elt == e {
				c = cc
			}
		}
		if c == nil {
			continue
		}
		first, last := f.tf.Line(e.Pos()), f.tf.Line(e.End())
		prevEnd := f.tf.Line(p.lit.Lbrace)
		if i > 0 {
			prevEnd = f.tf.Line(elts[i-1].End())
		}
		if prevEnd == first || i+1 < len(elts) && f.tf.Line(elts[i+1].Pos()) == last {
			return nil, refuse("%s's element %s shares a line with another at %s", newName, c.field.Name(),
				p.ps.where(e.Pos()))
		}
		// Comment lines right above the element, after the one before it.
		for _, cg := range f.ast.Comments {
			if l := f.tf.Line(cg.End()); l == first-1 && f.tf.Line(cg.Pos()) > prevEnd {
				first = f.tf.Line(cg.Pos())
			}
		}
		es = append(es, edit{f.tf.Offset(f.tf.LineStart(first)), lineEnd(f, last), ""})
	}
	stmtLine := f.tf.Line(p.stmt.Pos())
	ind := lineText(f, stmtLine)
	ind = ind[:len(ind)-len(strings.TrimLeft(ind, " \t"))]
	at := lineEnd(f, f.tf.Line(p.stmt.End()))
	es = append(es, edit{at, at, ind + p.hName + "." + depsName + " = " + p.depsName + "\n"})
	return es, nil
}

// lineText is line l of f without its newline.
func lineText(f *srcFile, l int) string {
	start := f.tf.Offset(f.tf.LineStart(l))
	end := lineEnd(f, l)
	return strings.TrimRight(string(f.src[start:end]), "\n")
}

// lineEnd is the offset just past line l's newline.
func lineEnd(f *srcFile, l int) int {
	if l < f.tf.LineCount() {
		return f.tf.Offset(f.tf.LineStart(l + 1))
	}
	return len(f.src)
}

func indent(ls []string) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = "\t" + l
	}
	return out
}

// collapseBlanks drops blank lines at either end and runs of them inside.
func collapseBlanks(ls []string) []string {
	var out []string
	for _, l := range ls {
		blank := strings.TrimSpace(l) == ""
		if blank && (len(out) == 0 || strings.TrimSpace(out[len(out)-1]) == "") {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// verify type-checks the package as the rewrite leaves it, tests included,
// and counts, per HandlerDeps field, the selectors that read it promoted
// through a Handler: each must equal the selectors renamed to it, so every
// rename reads the field it was meant to, and nothing else does.
func (p *plan) verify(out map[string][]byte) error {
	next := &pkgSource{dir: p.ps.dir, rel: p.ps.rel, path: p.ps.path, imp: p.ps.imp}
	srcs := map[string][]byte{}
	for _, f := range p.ps.files {
		srcs[f.name] = f.src
		if b, ok := out[f.name]; ok {
			srcs[f.name] = b
		}
	}
	if err := next.check(p.ps.fileNames(), srcs); err != nil {
		return fmt.Errorf("internal error: the rewritten package does not type-check: %v", err)
	}
	handler := next.types.Scope().Lookup(handlerName).Type()
	deps := next.types.Scope().Lookup(depsName).Type()
	got := map[string]int{}
	for sel, s := range next.info.Selections {
		recv := s.Recv()
		if ptr, ok := recv.(*types.Pointer); ok {
			recv = ptr.Elem()
		}
		if s.Kind() == types.FieldVal && len(s.Index()) == 2 && types.Identical(recv, handler) {
			embedded := handler.Underlying().(*types.Struct).Field(s.Index()[0])
			if embedded.Embedded() && types.Identical(embedded.Type(), deps) {
				got[sel.Sel.Name]++
			}
		}
	}
	want := map[string]int{}
	for _, c := range p.copies {
		if c.renames > 0 {
			want[c.dep.Name()] = c.renames
		}
	}
	var diffs []string
	for name := range union(got, want) {
		if got[name] != want[name] {
			diffs = append(diffs, fmt.Sprintf("%s: %d selectors read it through %s, %d were renamed to it",
				name, got[name], handlerName, want[name]))
		}
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		return fmt.Errorf("internal error: the rewrite does not read what it renamed:\n  %s", strings.Join(diffs, "\n  "))
	}
	if !bytes.Contains(out[p.ps.fileOf(p.lit.Pos()).name], []byte(p.hName+"."+depsName+" = "+p.depsName+"\n")) {
		return fmt.Errorf("internal error: %s does not assign %s", newName, depsName)
	}
	return nil
}

func union(a, b map[string]int) map[string]bool {
	u := map[string]bool{}
	for k := range a {
		u[k] = true
	}
	for k := range b {
		u[k] = true
	}
	return u
}
