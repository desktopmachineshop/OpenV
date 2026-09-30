package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/printer"
	"go/scanner"
	"go/token"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// verifyLift re-reads the package after a lift and checks that nothing but
// the layout changed:
//
//   - the registry holds the same entries in the same order, each lifted one
//     now naming its function, which its own file declares, alone;
//   - a lifted function's signature and body are its literal's token for
//     token, comments and raw-string bytes included (the stored-data freeze,
//     S3, hashes the same text without the comments: its part (a) is then
//     unchanged by construction), and the comment above the entry is above
//     it, line for line: its doc comment, or detached where gofmt would
//     reword a doc comment (see render);
//   - every comment of the registry's file is still there, in its order:
//     in the file, or in the lifted file of the entry it was above or inside
//     (verifyComments, which reads the files and not the plan, so a planning
//     fault that loses or reorders a comment cannot hide behind the checks
//     above, which compare with the plan; one that hands a comment to the
//     neighbouring migration in the same order is not caught here);
//   - an entry that was not lifted, and every other declaration of the
//     package, is unchanged byte for byte, doc comment included.
func verifyLift(plan *liftPlan) error {
	after, err := loadPackage(plan.src.dir)
	if err != nil {
		return err
	}
	regDecl, lit, err := after.registry()
	if err != nil {
		return err
	}
	entries, err := after.entries(lit)
	if err != nil {
		return err
	}
	var problems []string
	if len(entries) != len(plan.entries) {
		problems = append(problems, fmt.Sprintf("the registry holds %d entries, where the lift started from %d", len(entries), len(plan.entries)))
	} else {
		for i, e := range entries {
			problems = append(problems, plan.verifyEntry(after, plan.entries[i], e)...)
		}
		problems = append(problems, plan.verifyComments(after, entries)...)
	}
	problems = append(problems, plan.verifyRegistryFile(after)...)
	for _, l := range plan.lifts {
		problems = append(problems, verifyFile(after, l)...)
	}
	skip := map[string]bool{}
	for _, l := range plan.lifts {
		skip[l.funcName] = true
	}
	before, now := declTexts(plan.src, plan.regDecl, skip), declTexts(after, regDecl, skip)
	problems = append(problems, compareTexts(before, now)...)
	if len(problems) > 0 {
		return errors.New("the lift changed more than the layout:\n  - " + strings.Join(problems, "\n  - "))
	}
	return nil
}

// verifyEntry checks one registry entry after the lift against the entry it
// started from.
func (plan *liftPlan) verifyEntry(after *pkgSource, old, e *entry) []string {
	if e.version != old.version || e.name != old.name || e.field != old.field {
		return []string{fmt.Sprintf("%s holds %s (%s), where the lift started from %s (%s): the registry's order is behavior (R9)",
			e.pos, e.label(), e.field, old.label(), old.field)}
	}
	l := plan.byEntry[old]
	if l == nil {
		if got, want := after.text(e.lit), plan.src.text(old.lit); got != want {
			return []string{fmt.Sprintf("%s: migration %s was not lifted, but its entry changed", e.pos, e.label())}
		}
		return nil
	}
	fd := after.namedFunc(e.value)
	if fd == nil || fd.Name.Name != l.funcName {
		return []string{fmt.Sprintf("%s: migration %s's %s is `%s`, not %s", e.pos, e.label(), e.field, after.text(e.value), l.funcName)}
	}
	if f := after.file(fd.Pos()); f == nil || f.name != l.file {
		return []string{fmt.Sprintf("%s is declared at %s, not in %s", l.funcName, after.where(fd.Pos()), l.file)}
	}
	var problems []string
	if canonical(old.fn.Type)+canonical(old.fn.Body) != canonical(fd.Type)+canonical(fd.Body) {
		problems = append(problems, fmt.Sprintf("%s (%s): the code of migration %s differs from its literal's, which changes its S3 freeze hash",
			l.funcName, after.where(fd.Pos()), old.label()))
	}
	if d := diffTokens(plan.src.text(old.fn), funcText(after, fd), "the literal", "the function"); d != "" {
		problems = append(problems, fmt.Sprintf("%s (%s): the body of migration %s is not its literal's, token for token: %s",
			l.funcName, after.where(fd.Pos()), old.label(), d))
	}
	f := after.file(fd.Pos())
	if got := commentAbove(f.ast); !slices.Equal(got, l.doc) {
		problems = append(problems, fmt.Sprintf("%s (%s): the comment above it is not, line for line, the comment above migration %s:\n      want %q\n      got  %q",
			l.funcName, after.where(fd.Pos()), old.label(), l.doc, got))
	} else if attached := fd.Doc != nil; len(l.doc) > 0 && attached == l.detached {
		problems = append(problems, fmt.Sprintf("%s (%s): its comment is %s, where the lift %s it",
			l.funcName, after.where(fd.Pos()), map[bool]string{true: "its doc comment", false: "detached"}[attached],
			map[bool]string{true: "detached", false: "attached"}[l.detached]))
	}
	return problems
}

// verifyFile checks that a lifted migration's file declares its function
// and nothing else.
func verifyFile(after *pkgSource, l *lift) []string {
	for _, f := range after.files {
		if f.name != l.file {
			continue
		}
		var others []string
		for _, d := range f.ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == l.funcName {
				continue
			}
			others = append(others, after.where(d.Pos()))
		}
		var problems []string
		if len(others) > 0 {
			problems = append(problems, fmt.Sprintf("%s declares more than %s (%s)", l.file, l.funcName, strings.Join(others, ", ")))
		}
		var want []string
		for _, r := range l.imports {
			want = append(want, importKey(r.named, r.name, r.path))
		}
		sort.Strings(want)
		if got := importSet(f.ast, nil); !slices.Equal(got, want) {
			problems = append(problems, fmt.Sprintf("%s imports %q, where its body uses %q", l.file, got, want))
		}
		if stray := strayComments(after, f); len(stray) > 0 {
			problems = append(problems, fmt.Sprintf("%s has comments neither above nor inside %s (%s)", l.file, l.funcName, strings.Join(stray, ", ")))
		}
		return problems
	}
	return []string{fmt.Sprintf("%s was not written", l.file)}
}

// strayComments lists the comments of a lifted file that are neither above
// its function (commentAbove's) nor inside it.
func strayComments(p *pkgSource, f *srcFile) []string {
	var fd *ast.FuncDecl
	after := f.ast.Name.End()
	for _, d := range f.ast.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			after = gd.End()
		} else if x, ok := d.(*ast.FuncDecl); ok && fd == nil {
			fd = x
		}
	}
	var out []string
	for _, cg := range f.ast.Comments {
		above := fd != nil && cg.Pos() >= after && cg.End() <= fd.Pos()
		inside := fd != nil && cg.Pos() >= fd.Pos() && cg.End() <= fd.End()
		if !above && !inside {
			out = append(out, p.where(cg.Pos()))
		}
	}
	return out
}

// verifyRegistryFile checks the registry's file as a whole: apart from its
// imports it is the original with the lift's edits, token for token,
// comments included (the file's header, the registry's doc comment, the
// comment of an entry left as it is, the rest of the file), and its imports
// are the original's less those only the lifted bodies used.
func (plan *liftPlan) verifyRegistryFile(after *pkgSource) []string {
	var af *srcFile
	for _, f := range after.files {
		if f.name == plan.regFile.name {
			af = f
		}
	}
	if af == nil {
		return []string{plan.regFile.name + " is gone"}
	}
	want, err := applyEdits(plan.regFile.src, append(plan.literalEdits(), importCuts(plan.src, plan.regFile)...))
	if err != nil {
		return []string{err.Error()}
	}
	got, err := applyEdits(af.src, importCuts(after, af))
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	if d := diffTokens(string(want), string(got), "the file with the lift's edits", "the file written"); d != "" {
		problems = append(problems, fmt.Sprintf("%s changed beyond the lifted entries: %s", af.name, d))
	}
	if w, g := importSet(plan.regFile.ast, plan.droppedImports()), importSet(af.ast, nil); !slices.Equal(w, g) {
		problems = append(problems, fmt.Sprintf("%s imports %q, want %q", af.name, g, w))
	}
	return problems
}

// verifyComments checks, from the files alone, that the lift lost and
// reordered no comment of the registry's file (a comment handed to the
// neighbouring migration in the same order is not caught): the
// file's comments before the lift, in order, are its comments after it with
// the comments of each lifted migration's file, above its function and
// inside it, in the place of its entry. Comments inside import declarations
// are left out (verifyRegistryFile compares the imports), and so are empty
// "//" lines, since the lift joins the comment groups above an entry into
// one doc comment with such a line between them.
func (plan *liftPlan) verifyComments(after *pkgSource, entries []*entry) []string {
	var af *srcFile
	for _, f := range after.files {
		if f.name == plan.regFile.name {
			af = f
		}
	}
	if af == nil {
		return nil // verifyRegistryFile says it is gone
	}
	got := fileComments(after, af)
	for i, e := range entries {
		if plan.entries[i].fn == nil {
			continue // not a literal before the lift: its comments never left the file
		}
		fd := after.namedFunc(e.value)
		if fd == nil {
			continue // verifyEntry says what it names instead
		}
		if f := after.file(fd.Pos()); f != nil && f != af {
			for _, c := range fileComments(after, f) {
				c.pos = e.lit.Pos()
				got = append(got, c)
			}
		}
	}
	sort.SliceStable(got, func(i, j int) bool { return got[i].pos < got[j].pos })
	want := fileComments(plan.src, plan.regFile)
	for i := 0; i < len(want) || i < len(got); i++ {
		var d string
		switch {
		case i >= len(got):
			d = fmt.Sprintf("%q (%s before the lift) is in no file after it", want[i].text, want[i].at)
		case i >= len(want):
			d = fmt.Sprintf("%q (%s) was not there before the lift", got[i].text, got[i].at)
		case want[i].text != got[i].text:
			d = fmt.Sprintf("%q (%s) is where %q (%s before the lift) was", got[i].text, got[i].at, want[i].text, want[i].at)
		default:
			continue
		}
		return []string{fmt.Sprintf("the comments of %s before the lift are not, in order, its comments after it with each lifted "+
			"migration's own in the place of its entry: %s", plan.regFile.name, d)}
	}
	return nil
}

// positioned is one comment line, where it sorts (its own position in the
// registry's file; for a lifted file's comment, that of the entry naming its
// function) and where it is, file:line.
type positioned struct {
	pos  token.Pos
	text string
	at   string
}

// fileComments lists a file's comment lines in order, but those inside its
// import declarations and the empty "//" ones.
func fileComments(p *pkgSource, f *srcFile) []positioned {
	var imports []ast.Node
	for _, d := range f.ast.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			imports = append(imports, gd)
		}
	}
	var out []positioned
	for _, cg := range f.ast.Comments {
		for _, c := range cg.List {
			if strings.TrimSpace(strings.TrimPrefix(c.Text, "//")) == "" ||
				slices.ContainsFunc(imports, func(n ast.Node) bool { return c.Pos() >= n.Pos() && c.End() <= n.End() }) {
				continue
			}
			out = append(out, positioned{c.Pos(), c.Text, p.where(c.Pos())})
		}
	}
	return out
}

// importCuts are edits that cut a file's import declarations.
func importCuts(p *pkgSource, f *srcFile) []edit {
	var out []edit
	for _, d := range f.ast.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			out = append(out, edit{p.off(gd.Pos()), p.off(gd.End()), ""})
		}
	}
	return out
}

// importSet lists a file's imports but the dropped ones, sorted.
func importSet(f *ast.File, drop map[*ast.ImportSpec]bool) []string {
	var out []string
	for _, is := range f.Imports {
		if drop[is] {
			continue
		}
		path, _ := strconv.Unquote(is.Path.Value)
		name := ""
		if is.Name != nil {
			name = is.Name.Name
		}
		out = append(out, importKey(is.Name != nil, name, path))
	}
	sort.Strings(out)
	return out
}

func importKey(named bool, name, path string) string {
	if named {
		return name + " " + path
	}
	return path
}

// funcText is a function declaration's text from its func keyword on, with
// its name left out: what its literal was.
func funcText(p *pkgSource, fd *ast.FuncDecl) string {
	f := p.file(fd.Pos())
	return "func" + string(f.src[p.off(fd.Name.End()):p.off(fd.End())])
}

// canonical renders a node as the S3 freeze does (migration_freeze_test.go):
// go/printer with no source positions and no comments, so only the code and
// its literals, byte for byte, are left.
func canonical(n ast.Node) string {
	var b bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&b, token.NewFileSet(), n); err != nil {
		return fmt.Sprintf("<print %T: %v>", n, err)
	}
	return b.String()
}

// tokenText is one token of a scan, comments included.
type tokenText struct {
	line int
	tok  token.Token
	lit  string
}

func (t tokenText) String() string {
	if t.lit != "" {
		return fmt.Sprintf("%q", t.lit)
	}
	return t.tok.String()
}

// scanTokens lists the tokens of src with comments; a raw string's literal
// is its source bytes.
func scanTokens(src string) []tokenText {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, scanner.ScanComments)
	var out []tokenText
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		out = append(out, tokenText{fset.Position(pos).Line, tok, lit})
	}
}

// diffTokens names the first token where two texts differ, or "". The
// names say what each text is ("the literal", "the function").
func diffTokens(want, got, wantName, gotName string) string {
	a, b := scanTokens(want), scanTokens(got)
	for i := 0; i < len(a) || i < len(b); i++ {
		switch {
		case i >= len(a):
			return fmt.Sprintf("%s added at line %d of %s", b[i], b[i].line, gotName)
		case i >= len(b):
			return fmt.Sprintf("%s (line %d of %s) is missing", a[i], a[i].line, wantName)
		case a[i].tok != b[i].tok || a[i].lit != b[i].lit:
			return fmt.Sprintf("line %d of %s has %s where line %d of %s has %s", a[i].line, wantName, a[i], b[i].line, gotName, b[i])
		}
	}
	return ""
}

// declTexts maps every package-level declaration but the registry and the
// named functions to its text, doc comment included.
func declTexts(p *pkgSource, reg *declInfo, skip map[string]bool) map[string]string {
	all := map[string][]string{}
	seen := map[*declInfo]bool{}
	for _, d := range p.decls {
		if seen[d] || d == reg {
			continue
		}
		seen[d] = true
		key := strings.Join(d.keys, ",")
		if skip[key] {
			continue
		}
		start := d.node.Pos()
		switch x := d.node.(type) {
		case *ast.FuncDecl:
			if x.Doc != nil {
				start = x.Doc.Pos()
			}
		case *ast.GenDecl:
			if x.Doc != nil {
				start = x.Doc.Pos()
			}
		}
		f := p.file(start)
		all[key] = append(all[key], string(f.src[p.off(start):p.off(d.node.End())]))
	}
	out := map[string]string{}
	for key, texts := range all {
		sort.Strings(texts) // init functions and build-tag variants share a key
		out[key] = strings.Join(texts, "\n")
	}
	return out
}

// compareTexts lists the declarations whose text differs.
func compareTexts(before, after map[string]string) []string {
	var problems []string
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		b, inBefore := before[k]
		a, inAfter := after[k]
		switch {
		case !inAfter:
			problems = append(problems, k+" is gone")
		case !inBefore:
			problems = append(problems, k+" is new")
		case a != b:
			problems = append(problems, k+" changed")
		}
	}
	return problems
}
