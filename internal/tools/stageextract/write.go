package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// output is what a split writes: each file's gofmt'd source, and what the
// report and the budgets read from it.
type output struct {
	files     map[string][]byte
	order     []string // the new files in spec order, then the function's file
	spans     map[string]int
	mainLines int
}

// importSpec is one import of the function's file.
type importSpec struct {
	path, name, text string
	group            int
	blank            bool
}

// render writes the split's files: the function's file with the function
// sequencing the stages, one wire_*.go file per stage file of the spec,
// and the type's file.
func (p *plan) render() (*output, error) {
	imps := p.imports()
	out := &output{files: map[string][]byte{}, spans: map[string]int{}}
	var files []string
	byFile := map[string][]*stage{}
	for _, st := range p.stages {
		if byFile[st.spec.File] == nil {
			files = append(files, st.spec.File)
		}
		byFile[st.spec.File] = append(byFile[st.spec.File], st)
	}
	for _, name := range files {
		q := p.newNamer(imps)
		var body strings.Builder
		var ranges [][2]int
		for i, st := range byFile[name] {
			if i > 0 {
				body.WriteString("\n")
			}
			body.WriteString(p.stageText(st, q))
			ranges = append(ranges, [2]int{p.lineStart(st.start), p.lineStart(st.end + 1)})
		}
		for _, path := range p.pkgsIn(ranges) {
			q.used[path] = true
		}
		if q.err != nil {
			return nil, refuse(q.err)
		}
		src := "package " + p.ps.types.Name() + "\n\n" + p.importBlock(q) + "\n" + body.String()
		if err := p.gofmt(out, name, src); err != nil {
			return nil, err
		}
	}
	q := p.newNamer(imps)
	var fields strings.Builder
	for _, l := range p.locals {
		if l.kind == field {
			fields.WriteString("\t" + l.obj.Name() + " " + types.TypeString(l.obj.Type(), q.qualify) + "\n")
		}
	}
	doc := p.sp.TypeDoc
	if doc == "" {
		doc = fmt.Sprintf("// %s holds the locals of %s() that more than one of its stages uses, as\n"+
			"// fields: %s() makes one and calls the stages, its methods in the wire_*.go\n// files, in order.",
			p.sp.typeName(), p.sp.funcName(), p.sp.funcName())
	}
	if q.err != nil {
		return nil, refuse(q.err)
	}
	src := "package " + p.ps.types.Name() + "\n\n" + p.importBlock(q) + "\n" + strings.TrimRight(doc, "\n") + "\n" +
		"type " + p.sp.typeName() + " struct {\n" + fields.String() + "}\n"
	if err := p.gofmt(out, p.sp.typeFile(), src); err != nil {
		return nil, err
	}
	out.order = append(append(files, p.sp.typeFile()), p.sp.file())
	mainSrc, err := p.mainFile(imps)
	if err != nil {
		return nil, err
	}
	if err := p.gofmt(out, p.sp.file(), mainSrc); err != nil {
		return nil, err
	}
	return out, p.measure(out)
}

// stageText is one stage method: its doc, its signature, its range's lines
// with the edits, and the return of the cleanups the function defers.
func (p *plan) stageText(st *stage, q *namer) string {
	var b strings.Builder
	doc := st.spec.Doc
	if doc == "" {
		doc = fmt.Sprintf("// %s is one stage of %s(), which calls its stages in order.", st.spec.Name, p.sp.funcName())
	}
	b.WriteString(strings.TrimRight(doc, "\n") + "\n")
	fmt.Fprintf(&b, "func (%s *%s) %s()", p.sp.recv(), p.sp.typeName(), st.spec.Name)
	var results, exprs []string
	for _, d := range st.defers {
		results = append(results, types.TypeString(p.ps.info.TypeOf(d.Call.Fun), q.qualify))
		exprs = append(exprs, p.returned(d.Call.Fun))
	}
	switch len(results) {
	case 0:
	case 1:
		b.WriteString(" " + results[0])
	default:
		b.WriteString(" (" + strings.Join(results, ", ") + ")")
	}
	b.WriteString(" {\n")
	b.WriteString(q.resolve(p.edited(p.lineStart(st.start), p.lineStart(st.end+1))))
	if len(exprs) > 0 {
		b.WriteString("\treturn " + strings.Join(exprs, ", ") + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// returned is a deferred function as its stage returns it: a.stop for
// stop, a.db.Close for db.Close.
func (p *plan) returned(fun ast.Expr) string {
	text := string(p.mf.src[p.off(fun.Pos()):p.off(fun.End())])
	root := fun
	for {
		switch x := unparen(root).(type) {
		case *ast.SelectorExpr:
			root = x.X
			continue
		case *ast.Ident:
			if l := p.byObj[asVar(p.ps.info.Uses[x])]; l != nil && l.kind == field {
				at := p.off(x.Pos()) - p.off(fun.Pos())
				return text[:at] + p.sp.recv() + "." + text[at:]
			}
		}
		return text
	}
}

// mainFile is the function's file with the function sequencing the
// stages: the receiver made first, each range replaced by its stage's
// call (binding the cleanups it returns), the statements that stay as
// they were but for the edits, the blank lines between the calls dropped.
func (p *plan) mainFile(imps []importSpec) (string, error) {
	q := p.newNamer(imps)
	indent := "\t"
	var b strings.Builder
	b.WriteString(indent + p.sp.recv() + " := &" + p.sp.typeName() + "{}\n")
	b.WriteString(p.edited(p.lineStart(p.bodyFirst), p.lineStart(p.stages[0].start)))
	for _, st := range p.stages {
		call := p.sp.recv() + "." + st.spec.Name + "()"
		if len(st.cleanups) > 0 {
			call = strings.Join(st.cleanups, ", ") + " := " + call
		}
		b.WriteString(indent + call + "\n")
		b.WriteString(trimBlankLines(p.edited(p.lineStart(st.end+1), p.lineStart(st.keptTo+1))))
	}
	if p.tailLine > 0 {
		b.WriteString("\n" + p.edited(p.lineStart(p.tailLine), p.lineStart(p.bodyLast+1)))
	}
	body := q.resolve(b.String())
	// The file outside the moved ranges: what decides its imports.
	keep := [][2]int{{0, p.lineStart(p.stages[0].start)}}
	for i, st := range p.stages {
		next := len(p.mf.src)
		if i+1 < len(p.stages) {
			next = p.lineStart(p.stages[i+1].start)
		}
		keep = append(keep, [2]int{p.lineStart(st.end + 1), next})
	}
	for _, path := range p.pkgsIn(keep) {
		q.used[path] = true
	}
	head, err := p.pruneImports(imps, q)
	if err != nil {
		return "", err
	}
	return head + body + string(p.mf.src[p.lineStart(p.bodyLast+1):]), nil
}

// trimBlankLines drops the blank lines at the start and the end of text,
// which separated a range from what follows it; a blank line inside a
// statement that stays is kept.
func trimBlankLines(text string) string {
	lines := strings.SplitAfter(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "")
}

// edited is src[from:to] of the function's file with the edits inside it
// applied.
func (p *plan) edited(from, to int) string {
	var b strings.Builder
	at := from
	for _, e := range p.edits {
		if e.off < from || e.off >= to {
			continue
		}
		if e.end > to {
			panic(fmt.Sprintf("stageextract: an edit at %d runs past %d", e.off, to))
		}
		b.Write(p.mf.src[at:e.off])
		b.WriteString(e.text)
		at = e.end
	}
	b.Write(p.mf.src[at:to])
	return b.String()
}

// imports lists the function file's imports, with the group each is in.
func (p *plan) imports() []importSpec {
	var out []importSpec
	group, last := 0, 0
	for _, d := range p.mf.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if last > 0 {
			group++
		}
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			if last > 0 && p.line(is.Pos()) > last+1 {
				group++
			}
			last = p.line(is.End())
			path, _ := strconv.Unquote(is.Path.Value)
			spec := importSpec{path: path, group: group, text: string(p.mf.src[p.off(is.Pos()):p.off(is.End())])}
			if is.Name != nil {
				spec.name, spec.blank = is.Name.Name, is.Name.Name == "_"
			} else if pn, ok := p.ps.info.Implicits[is].(*types.PkgName); ok {
				spec.name = pn.Imported().Name()
			}
			out = append(out, spec)
		}
	}
	return out
}

// pkgsIn lists the import paths the function file's code uses inside the
// byte ranges, outside the edits that replace code.
func (p *plan) pkgsIn(ranges [][2]int) []string {
	seen := map[string]bool{}
	var out []string
	for id, obj := range p.ps.info.Uses {
		pn, ok := obj.(*types.PkgName)
		if !ok || p.ps.fset.File(id.Pos()) != p.mf.tf {
			continue
		}
		o := p.off(id.Pos())
		in := false
		for _, r := range ranges {
			in = in || (o >= r[0] && o < r[1])
		}
		for _, e := range p.edits {
			if e.end > e.off && o >= e.off && o < e.end {
				in = false
			}
		}
		if in && !seen[pn.Imported().Path()] {
			seen[pn.Imported().Path()] = true
			out = append(out, pn.Imported().Path())
		}
	}
	sort.Strings(out)
	return out
}

// namer names types for one output file: a package the function's file
// imports by that file's name for it, any other by its own name, which
// the file then imports.
type namer struct {
	p     *plan
	imps  []importSpec
	used  map[string]bool
	extra map[string]string // path -> name, for packages the function's file does not import
	err   error
}

func (p *plan) newNamer(imps []importSpec) *namer {
	return &namer{p: p, imps: imps, used: map[string]bool{}, extra: map[string]string{}}
}

func (q *namer) qualify(pkg *types.Package) string {
	if pkg == q.p.ps.types {
		return ""
	}
	for _, is := range q.imps {
		if is.path == pkg.Path() && !is.blank {
			q.used[is.path] = true
			return is.name
		}
	}
	name := pkg.Name()
	for _, is := range q.imps {
		if is.name == name && is.path != pkg.Path() && q.err == nil {
			q.err = fmt.Errorf("a field's type needs package %s, whose name %s the function's file gives to %s", pkg.Path(), name, is.path)
		}
	}
	if q.p.ps.types.Scope().Lookup(name) != nil && q.err == nil {
		q.err = fmt.Errorf("a field's type needs package %s, whose name %s the package already declares", pkg.Path(), name)
	}
	q.extra[pkg.Path()] = name
	q.used[pkg.Path()] = true
	return name
}

var markRe = regexp.MustCompile("\x00([0-9]+)\x00")

// resolve names the types that edit text marks.
func (q *namer) resolve(text string) string {
	return markRe.ReplaceAllStringFunc(text, func(m string) string {
		i, _ := strconv.Atoi(strings.Trim(m, "\x00"))
		return types.TypeString(q.p.marked[i], q.qualify)
	})
}

// importBlock is a new file's imports, grouped as the function's file
// groups them; a package it does not import joins the standard library's
// group or a last one of its own.
func (p *plan) importBlock(q *namer) string {
	type line struct {
		group int
		text  string
	}
	var lines []line
	maxGroup, stdGroup := 0, -1
	for _, is := range q.imps {
		maxGroup = max(maxGroup, is.group)
		if stdGroup < 0 && isStd(is.path) {
			stdGroup = is.group
		}
	}
	for _, is := range q.imps {
		if q.used[is.path] && !is.blank {
			lines = append(lines, line{is.group, is.text})
		}
	}
	for path, name := range q.extra {
		g := maxGroup + 1
		if isStd(path) && stdGroup >= 0 {
			g = stdGroup
		}
		text := strconv.Quote(path)
		if name != pathBase(path) {
			text = name + " " + text
		}
		lines = append(lines, line{g, text})
	}
	if len(lines) == 0 {
		return ""
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].group < lines[j].group })
	var b strings.Builder
	b.WriteString("import (\n")
	for i, l := range lines {
		if i > 0 && l.group != lines[i-1].group {
			b.WriteString("\n")
		}
		b.WriteString("\t" + l.text + "\n")
	}
	b.WriteString(")\n")
	return b.String()
}

func isStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

func pathBase(p string) string { return path.Base(p) }

// pruneImports is the function's file up to its body, with the imports
// nothing outside the moved ranges uses dropped, and any package the
// function's own new lines name but the file lacks added.
func (p *plan) pruneImports(imps []importSpec, q *namer) (string, error) {
	if q.err != nil {
		return "", refuse(q.err)
	}
	var edits []edit
	for _, d := range p.mf.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		dropped := 0
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			path, _ := strconv.Unquote(is.Path.Value)
			if (is.Name != nil && is.Name.Name == "_") || q.used[path] {
				continue
			}
			dropped++
			edits = append(edits, edit{off: p.lineStart(p.line(is.Pos())), end: p.lineStart(p.line(is.End()) + 1)})
		}
		if dropped == len(gd.Specs) {
			edits = append(edits[:len(edits)-dropped], edit{off: p.lineStart(p.line(gd.Pos())), end: p.lineStart(p.line(gd.End()) + 1)})
		} else if len(q.extra) > 0 && gd.Lparen.IsValid() {
			var add strings.Builder
			for path, name := range q.extra {
				text := strconv.Quote(path)
				if name != pathBase(path) {
					text = name + " " + text
				}
				add.WriteString("\t" + text + "\n")
			}
			at := p.lineStart(p.line(gd.Rparen))
			edits = append(edits, edit{off: at, end: at, text: add.String()})
			q.extra = map[string]string{}
		}
	}
	if len(q.extra) > 0 {
		return "", refuse(fmt.Errorf("%s needs imports it has no parenthesized import block to take", p.sp.file()))
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].off < edits[j].off })
	head := p.mf.src[:p.lineStart(p.bodyFirst)]
	var b strings.Builder
	at := 0
	for _, e := range edits {
		b.Write(head[at:e.off])
		b.WriteString(e.text)
		at = e.end
	}
	b.Write(head[at:])
	return b.String(), nil
}

// gofmt formats one output file; code that does not parse is a fault of
// the split, not a refusal.
func (p *plan) gofmt(out *output, name, src string) error {
	b, err := format.Source([]byte(src))
	if err != nil {
		return fmt.Errorf("the split's %s does not parse (%v):\n%s", name, err, numbered(src))
	}
	out.files[name] = b
	return nil
}

func numbered(src string) string {
	var b strings.Builder
	for i, l := range strings.Split(src, "\n") {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, l)
	}
	return b.String()
}

// measure reads each written function's span and each new file's length
// as internal/archtest does, and refuses a split past K14's budgets or the
// spec's budget for the function.
func (p *plan) measure(out *output) error {
	fset := token.NewFileSet()
	var over []string
	for name, src := range out.files {
		af, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return err
		}
		if name != p.sp.file() {
			if n := countLines(src); n > fileBudget {
				over = append(over, fmt.Sprintf("%s would have %d lines, over K14's %d", name, n, fileBudget))
			}
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			n := fset.Position(fd.End()).Line - fset.Position(fd.Pos()).Line + 1
			switch {
			case name == p.sp.file() && fd.Recv == nil && fd.Name.Name == p.sp.funcName():
				out.mainLines = n
				if p.sp.MainBudget > 0 && n > p.sp.MainBudget {
					over = append(over, fmt.Sprintf("%s() would span %d lines, over the spec's budget of %d; move more of it into stages", p.sp.funcName(), n, p.sp.MainBudget))
				}
			case fd.Recv != nil && name != p.sp.file():
				out.spans[fd.Name.Name] = n
				if n > funcBudget {
					over = append(over, fmt.Sprintf("stage %s would span %d lines, over K14's %d; split its range in the spec", fd.Name.Name, n, funcBudget))
				}
			}
		}
	}
	sort.Strings(over)
	if len(over) > 0 {
		return refuse(fmt.Errorf("%s", strings.Join(over, "; ")))
	}
	return nil
}

func countLines(src []byte) int {
	n := bytes.Count(src, []byte{'\n'})
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}
