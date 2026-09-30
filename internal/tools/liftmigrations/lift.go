package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// migrationName is what a registry Name must look like to become part of a
// file name: lower case letters, digits and underscores.
var migrationName = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// liftPlan is a lift worked out against the package, before anything is
// written.
type liftPlan struct {
	src     *pkgSource
	regDecl *declInfo
	regFile *srcFile
	lit     *ast.CompositeLit
	entries []*entry
	lifts   []*lift
	byEntry map[*entry]*lift
}

// lift is one registry entry whose function literal becomes a function of
// its own file.
type lift struct {
	e        *entry
	funcName string   // m00NN<Name>
	file     string   // migration_00NN_<name>.go
	doc      []string // the comment above the entry, one comment line per element
	detached bool     // doc stays above the function with a blank line between (see render)
	cut      [2]int   // the lines between the previous element and this one, which hold its comment
	imports  []importRef
}

// importRef is an import the lifted body uses, as the registry's file
// declares it.
type importRef struct {
	name, path string
	named      bool // the file names it explicitly
}

// funcName is the function a migration's body becomes: m, the version in
// four digits and the name in camel case (unique_personal_org_per_user
// becomes m0002UniquePersonalOrgPerUser).
func funcName(version int64, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "m%04d", version)
	for _, part := range strings.Split(name, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// fileName is the file a migration's body moves to.
func fileName(version int64, name string) string {
	return fmt.Sprintf("migration_%04d_%s.go", version, name)
}

// notEverywhere says why the go command would not build a file of this
// name into the package on every platform, or "" if it would: a name ending
// in _test.go is a test, and one whose last element is an operating system
// or an architecture (_linux.go, _amd64.go, _windows_arm64.go) builds only
// there. go/build decides, since the lists of those words it goes by are
// internal to the standard library: two platforms that share neither word
// both build a name only when it has no such suffix. It reads no file.
func notEverywhere(file string) string {
	if strings.HasSuffix(file, "_test.go") {
		return "a test file"
	}
	for _, platform := range [][2]string{{"linux", "amd64"}, {"windows", "arm64"}} {
		ctxt := build.Context{GOOS: platform[0], GOARCH: platform[1], Compiler: "gc",
			OpenFile: func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("package p\n")), nil }}
		if ok, err := ctxt.MatchFile(".", file); err != nil || !ok {
			return "a file of only the operating system or architecture its name ends in"
		}
	}
	return ""
}

// planLift works out the lift of every registry entry that holds a function
// literal. An entry that already runs a package-level function (the 0001
// baseline's RunDB, or a migration lifted before) is left as it is.
func planLift(p *pkgSource) (*liftPlan, error) {
	regDecl, lit, err := p.registry()
	if err != nil {
		return nil, err
	}
	entries, err := p.entries(lit)
	if err != nil {
		return nil, err
	}
	plan := &liftPlan{src: p, regDecl: regDecl, regFile: regDecl.file, lit: lit, entries: entries, byEntry: map[*entry]*lift{}}
	if err := plan.checkFile(); err != nil {
		return nil, err
	}
	var errs []string
	taken := map[string]bool{}
	prevEnd := lit.Lbrace
	for i, e := range entries {
		if i > 0 && e.version <= entries[i-1].version {
			errs = append(errs, fmt.Sprintf("%s: version %04d follows %04d; the registry must be unique and ascending",
				e.pos, e.version, entries[i-1].version))
		}
		l, err := plan.planEntry(e, prevEnd, taken)
		prevEnd = e.lit.End()
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if l != nil {
			plan.lifts = append(plan.lifts, l)
			plan.byEntry[e] = l
		}
	}
	if err := plan.checkClosingBrace(prevEnd); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return plan, nil
}

// checkFile refuses a registry file the lifted files could not mirror.
func (plan *liftPlan) checkFile() error {
	f := plan.regFile
	for _, cg := range f.ast.Comments {
		if cg.Pos() >= f.ast.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
				return fmt.Errorf("%s has build constraints, which liftmigrations does not carry to the files it writes", f.name)
			}
		}
	}
	for _, is := range f.ast.Imports {
		if is.Name != nil && is.Name.Name == "." {
			return fmt.Errorf("%s has a dot import, which liftmigrations cannot carry", f.name)
		}
	}
	return nil
}

// planEntry plans one entry: nil for one that is left as it is.
func (plan *liftPlan) planEntry(e *entry, prevEnd token.Pos, taken map[string]bool) (*lift, error) {
	p := plan.src
	if e.fn == nil {
		if p.namedFunc(e.value) != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: migration %s's %s is `%s`, neither a function literal nor the name of a package-level function",
			e.pos, e.label(), e.field, p.text(e.value))
	}
	if !migrationName.MatchString(e.name) {
		return nil, fmt.Errorf("%s: migration %04d's Name %q cannot be part of a file name; liftmigrations accepts lower-case letters, digits and single underscores",
			e.pos, e.version, e.name)
	}
	l := &lift{e: e, funcName: funcName(e.version, e.name), file: fileName(e.version, e.name)}
	if what := notEverywhere(l.file); what != "" {
		return nil, fmt.Errorf("%s: migration %s would move to %s, which the go command would read as %s; rename the migration "+
			"(its name may not end in test, nor in an operating system or architecture such as linux, windows, amd64 or arm)",
			e.pos, e.label(), l.file, what)
	}
	if p.pkg.Scope().Lookup(l.funcName) != nil || taken[l.funcName] {
		return nil, fmt.Errorf("%s: migration %s would become %s, which the package already declares", e.pos, e.label(), l.funcName)
	}
	if _, err := os.Stat(filepath.Join(p.dir, l.file)); err == nil || taken[l.file] {
		return nil, fmt.Errorf("%s: migration %s would move to %s, which already exists", e.pos, e.label(), l.file)
	}
	taken[l.funcName], taken[l.file] = true, true
	if err := plan.planComments(l, prevEnd); err != nil {
		return nil, err
	}
	imports, err := plan.imports(e.fn)
	if err != nil {
		return nil, fmt.Errorf("%s: migration %s: %w", e.pos, e.label(), err)
	}
	l.imports = imports
	return l, nil
}

// planComments takes the comment above an entry, which becomes its
// function's doc comment: every comment on the lines between the previous
// element (or the registry's opening brace) and this one. Those lines leave
// the registry with it, so the registry keeps one line per entry. A comment
// inside the entry but outside its function literal has nowhere to go.
func (plan *liftPlan) planComments(l *lift, prevEnd token.Pos) error {
	p, f, e := plan.src, plan.regFile, l.e
	if p.fset.Position(prevEnd).Line == p.fset.Position(e.lit.Pos()).Line {
		return fmt.Errorf("%s: migration %s shares a line with the element before it; run gofmt with one entry per line first", e.pos, e.label())
	}
	from, to := lineEnd(f.src, p.off(prevEnd)), lineStart(f.src, p.off(e.lit.Pos()))
	l.cut = [2]int{from, to}
	rest := []byte(nil)
	last := from
	var groups [][]string
	for _, cg := range f.ast.Comments {
		start, end := p.off(cg.Pos()), p.off(cg.End())
		switch {
		case start < to && end > from && (start < from || end > to):
			return fmt.Errorf("%s: migration %s: a comment above it shares a line with code (%s); run gofmt first",
				e.pos, e.label(), p.where(cg.Pos()))
		case start >= from && end <= to:
			rest = append(rest, f.src[last:start]...)
			last = end
			var lines []string
			for _, c := range cg.List {
				lines = append(lines, c.Text)
			}
			groups = append(groups, lines)
		case start >= p.off(e.lit.Pos()) && end <= p.off(e.lit.End()) &&
			(start < p.off(e.fn.Pos()) || end > p.off(e.fn.End())):
			return fmt.Errorf("%s: migration %s has a comment inside its entry but outside its function (%s); move it above the entry first",
				e.pos, e.label(), p.where(cg.Pos()))
		}
	}
	rest = append(rest, f.src[last:to]...)
	if len(bytes.TrimSpace(rest)) != 0 {
		return fmt.Errorf("%s: migration %s: the lines above it hold more than comments", e.pos, e.label())
	}
	for i, g := range groups {
		if i > 0 {
			l.doc = append(l.doc, "//")
		}
		l.doc = append(l.doc, g...)
	}
	return nil
}

// checkClosingBrace refuses a registry whose closing brace shares a line
// with its last entry (a comment after the last entry stays where it is).
func (plan *liftPlan) checkClosingBrace(lastEnd token.Pos) error {
	p := plan.src
	if len(plan.entries) > 0 && p.fset.Position(lastEnd).Line == p.fset.Position(plan.lit.Rbrace).Line {
		return fmt.Errorf("%s: the registry's closing brace shares a line with its last entry; run gofmt first", p.where(plan.lit.Rbrace))
	}
	return nil
}

// imports lists the imports a function literal uses, as the registry's file
// declares them.
func (plan *liftPlan) imports(fn *ast.FuncLit) ([]importRef, error) {
	p := plan.src
	specs := plan.importSpecs()
	seen := map[*types.PkgName]bool{}
	var out []importRef
	var err error
	ast.Inspect(fn, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		pn, ok := p.info.Uses[id].(*types.PkgName)
		if !ok || seen[pn] {
			return true
		}
		seen[pn] = true
		is := specs[pn]
		if is == nil {
			err = fmt.Errorf("%s names package %s, which %s does not import", p.where(id.Pos()), id.Name, plan.regFile.name)
			return false
		}
		path, _ := strconv.Unquote(is.Path.Value)
		out = append(out, importRef{name: pn.Name(), path: path, named: is.Name != nil})
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, err
}

// importSpecs maps each import of the registry's file to its spec.
func (plan *liftPlan) importSpecs() map[*types.PkgName]*ast.ImportSpec {
	p := plan.src
	out := map[*types.PkgName]*ast.ImportSpec{}
	for _, is := range plan.regFile.ast.Imports {
		var obj types.Object
		if is.Name != nil {
			obj = p.info.Defs[is.Name]
		} else {
			obj = p.info.Implicits[is]
		}
		if pn, ok := obj.(*types.PkgName); ok {
			out[pn] = is
		}
	}
	return out
}

// render returns the new content of every file the lift writes: one file
// per lifted migration and the registry's file.
//
// The comment above an entry becomes its function's doc comment, unless
// gofmt would reword it there: gofmt reformats doc comments, and turns two
// single quotes, or two backquotes, into one typographic quote, which in a
// migration's comment is SQL's empty string (0014's and 0017's comments
// quote it). Such a comment stays above the function word for word,
// detached by a blank line, where gofmt leaves it alone.
func (plan *liftPlan) render() (map[string][]byte, error) {
	out := map[string][]byte{}
	pkgName := plan.regFile.ast.Name.Name
	for _, l := range plan.lifts {
		b, err := format.Source([]byte(plan.liftedFile(pkgName, l)))
		if err == nil && !l.detached && !keepsComment(b, l) {
			l.detached = true
			b, err = format.Source([]byte(plan.liftedFile(pkgName, l)))
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.file, err)
		}
		out[l.file] = b
	}
	reg, err := plan.registryFile()
	if err != nil {
		return nil, err
	}
	out[plan.regFile.name] = reg
	return out, nil
}

// liftedFile is the text of the file a migration moves to, before gofmt:
// the package clause, the imports its body uses, its comment and its
// function literal, named.
func (plan *liftPlan) liftedFile(pkgName string, l *lift) string {
	var b strings.Builder
	b.WriteString("package " + pkgName + "\n\n")
	b.WriteString(importBlock(l.imports))
	b.WriteString("\n")
	for _, line := range l.doc {
		b.WriteString(line + "\n")
	}
	if l.detached {
		b.WriteString("\n")
	}
	fn := plan.src.text(l.e.fn)
	b.WriteString("func " + l.funcName + strings.TrimPrefix(fn, "func") + "\n")
	return b.String()
}

// keepsComment reports whether a formatted lifted file carries the entry's
// comment line for line above its function.
func keepsComment(src []byte, l *lift) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, l.file, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	return slices.Equal(commentAbove(f), l.doc)
}

// commentAbove is every comment line of a lifted file between its imports
// and its function, comment groups separated by an empty "//" line, as the
// lift joins them.
func commentAbove(f *ast.File) []string {
	after, before := f.Name.End(), token.NoPos
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			after = gd.End()
			continue
		}
		before = d.Pos()
		break
	}
	var lines []string
	for _, cg := range f.Comments {
		if cg.Pos() < after || (before.IsValid() && cg.Pos() >= before) {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "//")
		}
		for _, c := range cg.List {
			lines = append(lines, c.Text)
		}
	}
	return lines
}

// importBlock renders an import declaration: the standard library, then the
// rest in a group of their own.
func importBlock(refs []importRef) string {
	line := func(r importRef) string {
		if r.named {
			return r.name + " " + strconv.Quote(r.path)
		}
		return strconv.Quote(r.path)
	}
	switch len(refs) {
	case 0:
		return ""
	case 1:
		return "import " + line(refs[0]) + "\n"
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for i, std := range []bool{true, false} {
		wrote := false
		for _, r := range refs {
			if isStd(r.path) != std {
				continue
			}
			if !wrote && i > 0 && b.Len() > len("import (\n") {
				b.WriteString("\n")
			}
			b.WriteString("\t" + line(r) + "\n")
			wrote = true
		}
	}
	b.WriteString(")\n")
	return b.String()
}

// isStd applies goimports' rule: a standard-library path has no dot in its
// first element.
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// edit replaces src[from:to] with text.
type edit struct {
	from, to int
	text     string
}

// registryFile is the registry's file after the lift: each lifted entry's
// comment lines gone and its function literal replaced by the function's
// name, and the imports only the lifted bodies used dropped.
func (plan *liftPlan) registryFile() ([]byte, error) {
	drops, err := plan.importDrops()
	if err != nil {
		return nil, err
	}
	src, err := applyEdits(plan.regFile.src, append(plan.literalEdits(), drops...))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", plan.regFile.name, err)
	}
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", plan.regFile.name, err)
	}
	return out, nil
}

// literalEdits are the lift's edits of the registry: each lifted entry's
// comment lines cut, and its function literal replaced by its name.
func (plan *liftPlan) literalEdits() []edit {
	p := plan.src
	var edits []edit
	for _, l := range plan.lifts {
		edits = append(edits, edit{l.cut[0], l.cut[1], ""})
		edits = append(edits, edit{p.off(l.e.fn.Pos()), p.off(l.e.fn.End()), l.funcName})
	}
	return edits
}

// applyEdits applies non-overlapping edits to a copy of src.
func applyEdits(src []byte, edits []edit) ([]byte, error) {
	edits = append([]edit(nil), edits...)
	sort.Slice(edits, func(i, j int) bool { return edits[i].from > edits[j].from })
	out := append([]byte(nil), src...)
	limit := len(out)
	for _, e := range edits {
		if e.from > e.to || e.to > limit {
			return nil, fmt.Errorf("overlapping edits at byte %d", e.from)
		}
		out = append(out[:e.from], append([]byte(e.text), out[e.to:]...)...)
		limit = e.from
	}
	return out, nil
}

// droppedImports are the imports of the registry's file that only the
// lifted bodies used; a blank import, and one nothing used, stay.
func (plan *liftPlan) droppedImports() map[*ast.ImportSpec]bool {
	p, f := plan.src, plan.regFile
	inLift := func(pos token.Pos) bool {
		for _, l := range plan.lifts {
			if pos >= l.e.fn.Pos() && pos < l.e.fn.End() {
				return true
			}
		}
		return false
	}
	used, usedOutside := map[*types.PkgName]int{}, map[*types.PkgName]int{}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if pn, ok := p.info.Uses[id].(*types.PkgName); ok {
				used[pn]++
				if !inLift(id.Pos()) {
					usedOutside[pn]++
				}
			}
		}
		return true
	})
	drop := map[*ast.ImportSpec]bool{}
	for pn, is := range plan.importSpecs() {
		if used[pn] > 0 && usedOutside[pn] == 0 {
			drop[is] = true
		}
	}
	return drop
}

// importDrops removes the imports droppedImports names from the registry's
// file, and writes a block left with one import on one line.
func (plan *liftPlan) importDrops() ([]edit, error) {
	p, f := plan.src, plan.regFile
	drop := plan.droppedImports()
	var edits []edit
	for _, d := range f.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		var mine []edit
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			if !drop[is] {
				continue
			}
			start := is.Pos()
			if is.Doc != nil {
				start = is.Doc.Pos()
			}
			from, to := lineStart(f.src, p.off(start)), lineEnd(f.src, p.off(is.End()))
			if strings.Count(string(f.src[from:to]), "\"") != 2 {
				return nil, fmt.Errorf("%s: an import shares its line with another; run gofmt first", p.where(is.Pos()))
			}
			mine = append(mine, edit{from, to, ""})
		}
		switch kept := len(gd.Specs) - len(mine); {
		case len(mine) == 0:
		case kept == 0:
			start := gd.Pos()
			if gd.Doc != nil {
				start = gd.Doc.Pos()
			}
			mine = []edit{{lineStart(f.src, p.off(start)), lineEnd(f.src, p.off(gd.End())), ""}}
		case kept == 1 && gd.Lparen.IsValid():
			// One import left: `import "database/sql"`, as gofmt users write it.
			for _, s := range gd.Specs {
				if is := s.(*ast.ImportSpec); !drop[is] && is.Doc == nil && is.Comment == nil {
					mine = []edit{{p.off(gd.Pos()), p.off(gd.End()), "import " + p.text(is)}}
				}
			}
		}
		edits = append(edits, mine...)
	}
	return edits, nil
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
