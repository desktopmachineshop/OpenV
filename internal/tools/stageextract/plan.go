package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// plan is a split worked out against the function, before any text is
// written.
type plan struct {
	ps *pkgSource
	sp *spec
	mf *srcFile      // the function's file
	fd *ast.FuncDecl // the function
	// lines holds mf's lines, 1-based (lines[0] is unused), without their
	// newlines; bodyFirst and bodyLast are the lines inside its braces.
	lines               []string
	bodyFirst, bodyLast int
	stages              []*stage
	prefix, tail        []ast.Stmt // statements before the first range, and from Main on
	tailLine            int        // Main's line, 0 when there is no tail
	locals              []*local   // the function's top-level locals, in declaration order
	byObj               map[*types.Var]*local
	edits               []edit
	marked              []types.Type // the types typeMark stands for
	notes               []string
	drifted             bool
}

// stage is one range of the body and the method it becomes.
type stage struct {
	spec       *stageSpec
	start, end int              // the lines that move
	stmts      []ast.Stmt       // the statements that move
	defers     []*ast.DeferStmt // the defers right after the range whose functions the stage returns (through a field)
	cleanups   []string         // the names the function binds those to
	kept       []ast.Stmt       // the statements after the range, up to the next one, that stay (defers first)
	keptTo     int              // the last line before the next range, or the tail
}

// localKind is what becomes of a top-level local of the function.
type localKind int

const (
	// stays: every reference is in one stage, or every one is in the
	// function's own statements, so it stays a local there, declared as it
	// was.
	stays localKind = iota
	// field: referenced from a stage and from somewhere else, it becomes a
	// field of the app type, and every reference a.name.
	field
	// split: referenced from a stage and from somewhere else, but in each
	// segment after the one that declares it first written whole by a plain
	// assignment, never captured by a function literal and never
	// addressed: each stage that uses it declares its own (err, most
	// often), and the function its own once, so no value crosses a stage
	// boundary.
	split
)

// local is a top-level local of the function.
type local struct {
	obj      *types.Var
	decl     ast.Stmt // the top-level statement that declares it
	declSeg  int
	kind     localKind
	refs     []*ast.Ident // in source order
	segs     map[int]bool
	first    map[int]*ast.Ident // the first reference in each segment
	captured bool               // referenced inside a function literal
	addr     bool               // its address is taken, explicitly or implicitly (addressedVar)
	why      string             // why a local referenced from several segments is not split
}

// A segment is where a statement sits once the function is split: a
// stage's range (the stage's index, 0 and up), or one of the regions of
// the statements that stay in the function, which are negative: the
// prefix before the first range, what a range's span keeps after it (the
// defers, and anything after them), and the tail. The function's regions
// share its scope, but a stage call runs between any two of them, so each
// is a segment of its own when a local is split: a value must not cross
// from one region to the next past a stage that has its own copy.
const prefixSeg = -1

// keptSeg is the segment of the statements stage i's span keeps.
func keptSeg(i int) int { return -2 - i }

// tailSeg is the segment of the function's tail.
func (p *plan) tailSeg() int { return -2 - len(p.stages) }

// inFunc: segment s is a region of the function's own statements.
func inFunc(s int) bool { return s < 0 }

// edit replaces src[off:end] with text; off == end inserts.
type edit struct {
	off, end int
	text     string
	ident    bool // an a. inserted before a field's reference
}

// newPlan works out the split of sp's function in ps.
func newPlan(ps *pkgSource, sp *spec) (*plan, error) {
	p := &plan{ps: ps, sp: sp, byObj: map[*types.Var]*local{}}
	if p.mf = ps.file(sp.file()); p.mf == nil {
		return nil, refuse(fmt.Errorf("%s has no file %s that the go command builds", ps.rel, sp.file()))
	}
	for _, d := range p.mf.ast.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == sp.funcName() {
			p.fd = fd
		}
	}
	if p.fd == nil || p.fd.Body == nil {
		return nil, refuse(fmt.Errorf("%s declares no function %s with a body", sp.file(), sp.funcName()))
	}
	if p.fd.Type.Params.NumFields() > 0 || p.fd.Type.Results.NumFields() > 0 || p.fd.Type.TypeParams != nil {
		return nil, refuse(fmt.Errorf("%s takes parameters or returns results; stageextract splits a function of neither, such as main", sp.funcName()))
	}
	if err := p.checkNames(); err != nil {
		return nil, err
	}
	p.lines = append([]string{""}, strings.Split(string(p.mf.src), "\n")...)
	p.bodyFirst, p.bodyLast = p.line(p.fd.Body.Lbrace)+1, p.line(p.fd.Body.Rbrace)-1
	if p.line(p.fd.Body.Lbrace) == p.line(p.fd.Body.Rbrace) {
		return nil, refuse(fmt.Errorf("%s's body is on one line", sp.funcName()))
	}
	if err := p.ranges(); err != nil {
		return nil, err
	}
	if err := p.checkStages(); err != nil {
		return nil, err
	}
	if err := p.classify(); err != nil {
		return nil, err
	}
	if err := p.nameCleanups(); err != nil {
		return nil, err
	}
	if err := p.rewrite(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *plan) line(pos token.Pos) int { return p.ps.fset.Position(pos).Line }
func (p *plan) off(pos token.Pos) int  { return p.ps.fset.Position(pos).Offset }

// lineStart is the offset where line n of the function's file starts.
func (p *plan) lineStart(n int) int {
	if n > p.mf.tf.LineCount() {
		return len(p.mf.src)
	}
	return p.mf.tf.Offset(p.mf.tf.LineStart(n))
}

// checkNames refuses a split whose new names the package, its tests or the
// directory already has.
func (p *plan) checkNames() error {
	sp := p.sp
	if obj := p.ps.types.Scope().Lookup(sp.typeName()); obj != nil {
		return refuse(fmt.Errorf("the package already declares %s (%s); name the app type otherwise in the spec", sp.typeName(), p.ps.fset.Position(obj.Pos())))
	}
	for _, f := range p.ps.tests {
		if f.ast.Name.Name != p.ps.types.Name() {
			continue
		}
		for _, d := range f.ast.Decls {
			for _, name := range declNames(d) {
				if name == sp.typeName() {
					return refuse(fmt.Errorf("%s already declares %s; name the app type otherwise in the spec", f.name, name))
				}
			}
		}
	}
	files := []string{sp.typeFile()}
	for _, st := range sp.Stages {
		files = append(files, st.File)
	}
	for _, name := range files {
		if _, err := os.Stat(filepath.Join(p.ps.dir, name)); err == nil {
			return refuse(fmt.Errorf("%s/%s exists; stageextract writes only new files besides %s (has the split already run?)", p.ps.rel, name, sp.file()))
		}
	}
	for _, f := range p.mf.ast.Imports {
		if f.Name != nil && f.Name.Name == "." {
			return refuse(fmt.Errorf("%s has a dot import, whose names stageextract cannot tell from the function's own", sp.file()))
		}
	}
	return nil
}

func declNames(d ast.Decl) []string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv == nil {
			return []string{x.Name.Name}
		}
	case *ast.GenDecl:
		var out []string
		for _, s := range x.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				out = append(out, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					out = append(out, n.Name)
				}
			}
		}
		return out
	}
	return nil
}

// validStarts are the lines a range or the tail may start at: the first
// line of a top-level statement, or a line of a comment between two.
func (p *plan) validStarts() map[int]bool {
	ok := map[int]bool{}
	body := p.fd.Body
	stmtLines := map[int]bool{}
	for _, s := range body.List {
		ok[p.line(s.Pos())] = true
		for l := p.line(s.Pos()); l <= p.line(s.End()); l++ {
			stmtLines[l] = true
		}
	}
	for _, cg := range p.mf.ast.Comments {
		if cg.Pos() < body.Lbrace || cg.End() > body.Rbrace {
			continue
		}
		for l := p.line(cg.Pos()); l <= p.line(cg.End()); l++ {
			if !stmtLines[l] {
				ok[l] = true
			}
		}
	}
	return ok
}

// resolve finds an anchor: its line when that line reads as the spec says,
// else the one start line of the body that does.
func (p *plan) resolve(what string, line int, starts string, valid map[int]bool) (int, error) {
	if line >= p.bodyFirst && line <= p.bodyLast && strings.TrimSpace(p.lines[line]) == starts && valid[line] {
		return line, nil
	}
	var found []int
	for l := p.bodyFirst; l <= p.bodyLast; l++ {
		if valid[l] && strings.TrimSpace(p.lines[l]) == starts {
			found = append(found, l)
		}
	}
	at := "outside the body"
	if line >= p.bodyFirst && line <= p.bodyLast {
		at = fmt.Sprintf("%q", strings.TrimSpace(p.lines[line]))
	}
	switch len(found) {
	case 1:
		p.drifted = true
		p.notes = append(p.notes, fmt.Sprintf("%s starts at line %d, not %d: %s has moved on since the spec was written", what, found[0], line, p.sp.file()))
		return found[0], nil
	case 0:
		return 0, refuse(fmt.Errorf("%s: line %d is %s, and no line of %s() where a range may start reads %q; "+
			"%s has moved on: update the spec (rule R4)", what, line, at, p.sp.funcName(), starts, p.sp.file()))
	}
	return 0, refuse(fmt.Errorf("%s: line %d is %s, and lines %s of %s() all read %q; update the spec's line (rule R4)",
		what, line, at, joinInts(found), p.sp.funcName(), starts))
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}

// ranges resolves the spec's ranges against the body: where each starts,
// which statements it moves, which defers after it become its cleanups,
// and which statements stay.
func (p *plan) ranges() error {
	valid := p.validStarts()
	var starts []int
	for i := range p.sp.Stages {
		st := &p.sp.Stages[i]
		l, err := p.resolve("stage "+st.Name, st.first, st.Starts, valid)
		if err != nil {
			return err
		}
		if len(starts) > 0 && l <= starts[len(starts)-1] {
			return refuse(fmt.Errorf("stage %s starts at line %d, before the stage listed ahead of it; stages keep the body's order", st.Name, l))
		}
		starts = append(starts, l)
		p.stages = append(p.stages, &stage{spec: st, start: l})
	}
	end := p.bodyLast + 1
	if m := p.sp.Main; m != nil {
		l, err := p.resolve("main", m.Line, m.Starts, valid)
		if err != nil {
			return err
		}
		if l <= starts[len(starts)-1] {
			return refuse(fmt.Errorf("main's line %d is not after the last stage's start, line %d", l, starts[len(starts)-1]))
		}
		p.tailLine, end = l, l
	}
	for i, st := range p.stages {
		st.keptTo = end - 1
		if i+1 < len(p.stages) {
			st.keptTo = p.stages[i+1].start - 1
		}
	}
	for _, s := range p.fd.Body.List {
		l := p.line(s.Pos())
		switch {
		case l < starts[0]:
			p.prefix = append(p.prefix, s)
		case p.tailLine > 0 && l >= p.tailLine:
			p.tail = append(p.tail, s)
		default:
			for _, st := range p.stages {
				if l >= st.start && l <= st.keptTo {
					if p.line(s.End()) > st.keptTo {
						return refuse(fmt.Errorf("stage %s: the statement at line %d runs past line %d, where the next range starts", st.spec.Name, l, st.keptTo))
					}
					st.stmts = append(st.stmts, s)
				}
			}
		}
	}
	for _, st := range p.stages {
		if err := p.splitSpan(st); err != nil {
			return err
		}
	}
	return p.checkLines()
}

// splitSpan divides the statements from a range's start to the next one's
// into those that move (up to the first defer), the defers right after
// them whose functions the stage returns, and the rest, which stay.
func (p *plan) splitSpan(st *stage) error {
	all := st.stmts
	st.stmts = nil
	i := 0
	for ; i < len(all); i++ {
		if _, ok := all[i].(*ast.DeferStmt); ok {
			break
		}
		st.stmts = append(st.stmts, all[i])
	}
	if len(st.stmts) == 0 {
		what := "no statement"
		if i < len(all) {
			what = fmt.Sprintf("no statement before the defer at line %d", p.line(all[i].Pos()))
		}
		return refuse(fmt.Errorf("stage %s (from line %d) holds %s; a defer stays in %s(), so a range ends before it", st.spec.Name, st.start, what, p.sp.funcName()))
	}
	for ; i < len(all); i++ {
		d, ok := all[i].(*ast.DeferStmt)
		if !ok || !p.cleanupShape(d) {
			break
		}
		st.defers = append(st.defers, d)
	}
	st.kept = all[len(st.stmts):]
	for _, s := range all[i:] {
		if _, ok := s.(*ast.DeferStmt); !ok {
			p.notes = append(p.notes, fmt.Sprintf("stage %s: line %d stays in %s() after the defer at line %d; start a stage there to move it",
				st.spec.Name, p.line(s.Pos()), p.sp.funcName(), p.line(st.kept[0].Pos())))
		}
	}
	st.end = p.line(st.stmts[len(st.stmts)-1].End())
	return nil
}

// checkLines holds each range to the spec's lines when no anchor moved,
// and notes where a range now runs when one did.
func (p *plan) checkLines() error {
	for _, st := range p.stages {
		if st.start == st.spec.first && st.end == st.spec.last {
			continue
		}
		if !p.drifted {
			return refuse(fmt.Errorf("stage %s: the spec says lines %s, but its statements run %d-%d (a range ends at its last statement "+
				"before the next range, the tail or a defer); correct the spec", st.spec.Name, st.spec.Lines, st.start, st.end))
		}
		p.notes = append(p.notes, fmt.Sprintf("stage %s runs lines %d-%d, not %s", st.spec.Name, st.start, st.end, st.spec.Lines))
	}
	return nil
}

// cleanupShape is a defer the stage before it can return the function of:
// a call with no arguments of a top-level local of the function or of a
// selector on one (stop(), db.Close()), so evaluating the function in the
// stage and calling it in the deferred call is what the defer did. Once
// the locals are classified, nameCleanups keeps only those whose local is
// a field, which the stage can reach.
func (p *plan) cleanupShape(d *ast.DeferStmt) bool {
	if len(d.Call.Args) > 0 || d.Call.Ellipsis.IsValid() {
		return false
	}
	e := d.Call.Fun
	for {
		switch x := e.(type) {
		case *ast.Ident:
			v, ok := p.ps.info.Uses[x].(*types.Var)
			return ok && v.Parent() != nil && v.Parent() == p.ps.info.Scopes[p.fd.Type]
		case *ast.SelectorExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return false
		}
	}
}

// checkStages refuses a range holding what would run differently in a
// method: a return, a defer or a recover outside a function literal, a
// label or a goto.
func (p *plan) checkStages() error {
	var errs []string
	for _, st := range p.stages {
		for _, s := range st.stmts {
			ast.Inspect(s, func(n ast.Node) bool {
				what := ""
				switch x := n.(type) {
				case *ast.FuncLit:
					return false
				case *ast.ReturnStmt:
					what = "a return, which would end the stage instead of " + p.sp.funcName() + "()"
				case *ast.DeferStmt:
					what = "a defer, which would run when the stage returns"
				case *ast.LabeledStmt:
					what = "a label"
				case *ast.BranchStmt:
					if x.Label != nil || x.Tok == token.GOTO {
						what = "a " + x.Tok.String() + " to a label"
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && p.ps.info.Uses[id] == types.Universe.Lookup("recover") {
						what = "a recover, which works only in a deferred call"
					}
				}
				if what != "" {
					errs = append(errs, fmt.Sprintf("stage %s: line %d holds %s", st.spec.Name, p.line(n.Pos()), what))
				}
				return true
			})
		}
	}
	if len(errs) > 0 {
		return refuse(fmt.Errorf("%s", strings.Join(errs, "; ")))
	}
	return nil
}

// seg is the segment a line of the body belongs to: the stage whose range
// holds it, or the region of the function's own statements.
func (p *plan) seg(line int) int {
	if line < p.stages[0].start {
		return prefixSeg
	}
	for i, st := range p.stages {
		switch {
		case line >= st.start && line <= st.end:
			return i
		case line > st.end && line <= st.keptTo:
			return keptSeg(i)
		}
	}
	return p.tailSeg()
}

// owners counts the stages that reference l, and the function as one more
// when any of its regions does.
func owners(l *local) int {
	set := map[int]bool{}
	for s := range l.segs {
		set[max(s, -1)] = true
	}
	return len(set)
}

func (p *plan) segName(s int) string {
	if inFunc(s) {
		return p.sp.funcName() + "()"
	}
	return "stage " + p.stages[s].spec.Name
}

// top is the top-level statement of the body that holds pos.
func (p *plan) top(pos token.Pos) ast.Stmt {
	for _, s := range p.fd.Body.List {
		if s.Pos() <= pos && pos < s.End() {
			return s
		}
	}
	return nil
}
