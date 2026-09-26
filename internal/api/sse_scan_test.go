package api

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The source scan behind TestSSEContract (refactor plan step S6): where an
// SSE event name enters a stream, and on which stream keys. It reads syntax
// only, like internal/archtest, so it gives the same answer on every host,
// and it fails rather than guess when a name is not a literal.

// sseSite is one place an event name enters a stream.
type sseSite struct {
	event   string
	streams []string
	how     string // BroadcastSession, emit, sseEvent or write
	pos     string
}

type sseScan struct {
	root   string
	sites  []sseSite
	served []sseStream
}

func (s *sseScan) contractEvents() []sseContractName {
	byName := map[string]map[string]bool{}
	for _, site := range s.sites {
		if byName[site.event] == nil {
			byName[site.event] = map[string]bool{}
		}
		for _, stream := range site.streams {
			byName[site.event][stream] = true
		}
	}
	var out []sseContractName
	for _, name := range sortedKeys(byName) {
		out = append(out, sseContractName{Name: name, Streams: sortedKeys(byName[name])})
	}
	return out
}

func (s *sseScan) contractStreams() []sseStream {
	replay := map[string]bool{}
	for _, st := range s.served {
		replay[st.Key] = replay[st.Key] || st.Replay
	}
	var out []sseStream
	for _, key := range sortedKeys(replay) {
		out = append(out, sseStream{Key: key, Replay: replay[key]})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// goSource is one parsed non-test file of the module.
type goSource struct {
	rel  string // module-relative, slash-separated
	dir  string // module-relative directory
	file *ast.File
}

// sseScanner holds the parsed module while it is scanned.
type sseScanner struct {
	t      *testing.T
	fset   *token.FileSet
	files  []goSource
	funcs  map[string][]*ast.FuncDecl // package-level functions by name
	consts map[string]map[string]string
	scan   *sseScan
	hub    []pendingWrite // ServeStream's own writes, placed once streams are known
}

type pendingWrite struct {
	event      string
	pos        string
	replayOnly bool
}

// scanSSESources parses the module's production Go files (the root package,
// cmd/ and internal/, as internal/archtest reads them) and collects every
// site where an event name enters a stream.
func scanSSESources(t *testing.T) *sseScan {
	t.Helper()
	mod := loadModuleSources(t)
	s := &sseScanner{
		t: t, fset: mod.fset, files: mod.files,
		funcs: map[string][]*ast.FuncDecl{}, consts: map[string]map[string]string{},
		scan: &sseScan{root: mod.root},
	}
	for _, f := range s.files {
		for _, d := range f.file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Body != nil {
					s.funcs[d.Name.Name] = append(s.funcs[d.Name.Name], d)
				}
			case *ast.GenDecl:
				s.addConsts(f.dir, d)
			}
		}
	}
	for _, f := range s.files {
		for _, d := range f.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				s.scanFunc(f, fd)
			}
		}
	}
	s.placeHubWrites()
	sort.Slice(s.scan.sites, func(i, j int) bool { return s.scan.sites[i].pos < s.scan.sites[j].pos })
	return s.scan
}

// moduleSources is the module's parsed production Go code.
type moduleSources struct {
	root  string // absolute directory holding go.mod
	path  string // module path from go.mod
	fset  *token.FileSet
	files []goSource // sorted by path
}

// loadModuleSources parses the non-test Go files of the root package and
// of every package under cmd/ and internal/, skipping testdata and
// directories starting with "." or "_" (the scope internal/archtest reads),
// and files no build selects. File names in fset are module-relative.
func loadModuleSources(t *testing.T) *moduleSources {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("the module root %s has no go.mod: %v", root, err)
	}
	mod := &moduleSources{root: root, fset: token.NewFileSet()}
	for _, line := range strings.Split(string(gomod), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			mod.path = strings.TrimSpace(rest)
		}
	}
	var paths []string
	for _, top := range []string{".", "cmd", "internal"} {
		start := filepath.Join(root, top)
		err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if p != start && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || top == ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", start, err)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(mod.fset, rel, mustRead(t, p), parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		if !buildIgnored(f) {
			mod.files = append(mod.files, goSource{rel: rel, dir: filepath.ToSlash(filepath.Dir(rel)), file: f})
		}
	}
	return mod
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// buildIgnored reports a file no build selects (//go:build ignore).
func buildIgnored(f *ast.File) bool {
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") && strings.Contains(c.Text, "ignore") {
				return true
			}
		}
	}
	return false
}

func (s *sseScanner) addConsts(dir string, d *ast.GenDecl) {
	if d.Tok != token.CONST {
		return
	}
	for _, spec := range d.Specs {
		vs := spec.(*ast.ValueSpec)
		for i, name := range vs.Names {
			if i < len(vs.Values) {
				if v, ok := stringLit(vs.Values[i]); ok {
					if s.consts[dir] == nil {
						s.consts[dir] = map[string]string{}
					}
					s.consts[dir][name.Name] = v
				}
			}
		}
	}
}

func (s *sseScanner) pos(n ast.Node) string {
	p := s.fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

func (s *sseScanner) fail(n ast.Node, format string, args ...interface{}) {
	s.t.Helper()
	s.t.Fatalf("%s: %s\nThe S6 scan (internal/api/sse_contract_test.go) reads SSE event names only as string "+
		"literals (or same-package string constants) at BroadcastSession, a replay's emit, sseEvent{Event: ...} "+
		"and ServeStream's write; send the name that way, or extend the scan in the same PR.",
		s.pos(n), fmt.Sprintf(format, args...))
}

func (s *sseScanner) add(site sseSite) { s.scan.sites = append(s.scan.sites, site) }

// scanFunc visits one function declaration with the stack of enclosing
// nodes, so each site knows its function and the if statements around it.
func (s *sseScanner) scanFunc(f goSource, fd *ast.FuncDecl) {
	var stack []ast.Node
	ast.Inspect(fd, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			s.visitCall(f, fd, n, stack)
		case *ast.CompositeLit:
			s.visitEventLit(f, fd, n, stack)
		}
		stack = append(stack, n)
		return true
	})
}

func calledMethod(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

func (s *sseScanner) visitCall(f goSource, fd *ast.FuncDecl, call *ast.CallExpr, stack []ast.Node) {
	switch {
	case calledMethod(call) == "BroadcastSession" && len(call.Args) == 3:
		s.add(sseSite{
			event:   s.eventName(f, call.Args[1]),
			streams: s.streamKeys(f, fd, call.Args[0]),
			how:     "BroadcastSession", pos: s.pos(call),
		})
	case calledMethod(call) == "ServeStream" && len(call.Args) == 4:
		s.visitServeStream(f, fd, call)
	case calledMethod(call) == "broadcast" && f.dir == "internal/api":
		if len(call.Args) != 2 || !isSSEEventLit(call.Args[1]) {
			s.fail(call, "SSEHub.broadcast called with something other than an sseEvent{...} literal")
		}
	case isHubServeStream(fd):
		s.visitHubWrite(fd, call, stack)
	}
}

func isSSEEventLit(e ast.Expr) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	id, ok := lit.Type.(*ast.Ident)
	return ok && id.Name == "sseEvent"
}

func isHubServeStream(fd *ast.FuncDecl) bool {
	return fd.Name.Name == "ServeStream" && sseRecvType(fd) == "SSEHub"
}

// visitEventLit reads sseEvent{Event: "name"} literals, which SSEHub's
// run-stream methods pass to broadcast; the key is broadcast's first argument.
func (s *sseScanner) visitEventLit(f goSource, fd *ast.FuncDecl, lit *ast.CompositeLit, stack []ast.Node) {
	if !isSSEEventLit(lit) {
		return
	}
	var event ast.Expr
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok && isIdent(kv.Key, "Event") {
			event = kv.Value
		}
	}
	if event == nil {
		s.fail(lit, "an sseEvent literal without a keyed Event field")
	}
	// BroadcastSession forwards its own parameter; its callers are the sites.
	if fd.Name.Name == "BroadcastSession" && sseRecvType(fd) == "SSEHub" {
		if id, ok := event.(*ast.Ident); ok && isParam(fd.Type, id.Name) {
			return
		}
	}
	var parent *ast.CallExpr
	if len(stack) > 0 {
		parent, _ = stack[len(stack)-1].(*ast.CallExpr)
	}
	if parent == nil || calledMethod(parent) != "broadcast" || len(parent.Args) != 2 || parent.Args[1] != ast.Expr(lit) {
		s.fail(lit, "an sseEvent literal that is not the second argument of SSEHub.broadcast")
	}
	s.add(sseSite{
		event:   s.eventName(f, event),
		streams: s.streamKeys(f, fd, parent.Args[0]),
		how:     "sseEvent", pos: s.pos(lit),
	})
}

// visitServeStream records the stream a ServeStream call serves and the
// names its replay emits.
func (s *sseScanner) visitServeStream(f goSource, fd *ast.FuncDecl, call *ast.CallExpr) {
	streams := s.streamKeys(f, fd, call.Args[2])
	switch replay := call.Args[3].(type) {
	case *ast.Ident:
		if replay.Name != "nil" {
			s.fail(call, "a ServeStream replay that is neither nil nor a function literal")
		}
		for _, key := range streams {
			s.scan.served = append(s.scan.served, sseStream{Key: key})
		}
	case *ast.FuncLit:
		params := replay.Type.Params.List
		if len(params) != 1 || len(params[0].Names) != 1 {
			s.fail(call, "a ServeStream replay whose emit parameter is unnamed")
		}
		emit := params[0].Names[0].Name
		for _, key := range streams {
			s.scan.served = append(s.scan.served, sseStream{Key: key, Replay: true})
		}
		ast.Inspect(replay.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isIdent(c.Fun, emit) {
				if len(c.Args) != 2 {
					s.fail(c, "%s called with %d arguments", emit, len(c.Args))
				}
				s.add(sseSite{event: s.eventName(f, c.Args[0]), streams: streams, how: "emit", pos: s.pos(c)})
			}
			return true
		})
	default:
		s.fail(call, "a ServeStream replay that is neither nil nor a function literal")
	}
}

// visitHubWrite reads calls inside SSEHub.ServeStream of the closure it hands
// to replay as emit (the write closure): a literal name is the hub's own
// event; write(e.Event, e.Data) forwards a broadcast and is not a site.
func (s *sseScanner) visitHubWrite(fd *ast.FuncDecl, call *ast.CallExpr, stack []ast.Node) {
	write := hubWriteName(fd)
	if write == "" {
		s.fail(fd, "SSEHub.ServeStream no longer hands a named closure to replay; the scan cannot find its write")
	}
	if !isIdent(call.Fun, write) {
		return
	}
	if len(call.Args) != 2 {
		s.fail(call, "%s called with %d arguments", write, len(call.Args))
	}
	if sel, ok := call.Args[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "Event" {
		return
	}
	name, ok := stringLit(call.Args[0])
	if !ok {
		s.fail(call, "ServeStream's %s called with a name that is neither a literal nor a forwarded .Event", write)
	}
	s.hub = append(s.hub, pendingWrite{event: name, pos: s.pos(call), replayOnly: insideReplayCheck(stack)})
}

// hubWriteName is the identifier ServeStream passes to replay(...).
func hubWriteName(fd *ast.FuncDecl) string {
	name := ""
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && isIdent(c.Fun, "replay") && len(c.Args) == 1 {
			if id, ok := c.Args[0].(*ast.Ident); ok {
				name = id.Name
			}
		}
		return true
	})
	return name
}

// insideReplayCheck reports whether a node sits in the body of an if whose
// init or condition calls replay: `if err := replay(write); err != nil {...}`.
func insideReplayCheck(stack []ast.Node) bool {
	for i, n := range stack {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || i+1 >= len(stack) || stack[i+1] != ast.Node(ifs.Body) {
			continue
		}
		calls := false
		for _, part := range []ast.Node{ifs.Init, ifs.Cond} {
			if part == nil {
				continue
			}
			ast.Inspect(part, func(m ast.Node) bool {
				if c, ok := m.(*ast.CallExpr); ok && isIdent(c.Fun, "replay") {
					calls = true
				}
				return true
			})
		}
		if calls {
			return true
		}
	}
	return false
}

// placeHubWrites puts ServeStream's own events on the streams that can send
// them: a write inside the replay-failure branch reaches only the streams
// served with a replay; any other reaches every served stream.
func (s *sseScanner) placeHubWrites() {
	for _, w := range s.hub {
		keys := map[string]bool{}
		for _, st := range s.scan.served {
			if st.Replay || !w.replayOnly {
				keys[st.Key] = true
			}
		}
		s.add(sseSite{event: w.event, streams: sortedKeys(keys), how: "write", pos: w.pos})
	}
}

// eventName reads an event name: a string literal, or a string constant of
// the same package.
func (s *sseScanner) eventName(f goSource, e ast.Expr) string {
	if v, ok := stringLit(e); ok {
		return v
	}
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := s.consts[f.dir][id.Name]; ok {
			return v
		}
	}
	s.fail(e, "an SSE event name that is not a string literal or constant")
	return ""
}

// streamKeys reads the stream key patterns an expression can evaluate to:
// "prefix:" + x reads as "prefix:<id>", a call of a one-return function
// reads as its return expression, a local variable as every value assigned
// to it (an empty string placeholder skipped), and anything else, such as
// run.ID or a path variable, as a bare "<id>".
func (s *sseScanner) streamKeys(f goSource, fd *ast.FuncDecl, e ast.Expr) []string {
	set := map[string]bool{}
	s.collectKeys(f, fd, e, set, 0)
	if len(set) == 0 {
		set["<id>"] = true
	}
	return sortedKeys(set)
}

func (s *sseScanner) collectKeys(f goSource, fd *ast.FuncDecl, e ast.Expr, set map[string]bool, depth int) {
	if depth > 4 {
		set["<id>"] = true
		return
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		s.collectKeys(f, fd, x.X, set, depth+1)
		return
	case *ast.BasicLit:
		if v, ok := stringLit(x); ok {
			if v != "" {
				set[v] = true
			}
			return
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			if prefix, ok := leftmostLit(x); ok {
				set[prefix+"<id>"] = true
				return
			}
		}
	case *ast.CallExpr:
		if ret := s.singleReturn(x); ret != nil {
			s.collectKeys(f, fd, ret, set, depth+1)
			return
		}
	case *ast.Ident:
		if values := sseAssignments(fd, x.Name); len(values) > 0 {
			for _, v := range values {
				s.collectKeys(f, fd, v, set, depth+1)
			}
			return
		}
	}
	set["<id>"] = true
}

func leftmostLit(e *ast.BinaryExpr) (string, bool) {
	switch l := e.X.(type) {
	case *ast.BinaryExpr:
		return leftmostLit(l)
	default:
		return stringLit(l)
	}
}

// singleReturn is the returned expression of the package-level function a
// call names, when its body is a single return of one value. A qualified
// call (notify.StreamKey) prefers the function in the package so named.
func (s *sseScanner) singleReturn(call *ast.CallExpr) ast.Expr {
	var name, qual string
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name = fn.Name
	case *ast.SelectorExpr:
		name = fn.Sel.Name
		if id, ok := fn.X.(*ast.Ident); ok {
			qual = id.Name
		}
	default:
		return nil
	}
	var ret ast.Expr
	for _, fd := range s.funcs[name] {
		if qual != "" && s.fset.Position(fd.Pos()).Filename != "" {
			dir := filepath.ToSlash(filepath.Dir(s.fset.Position(fd.Pos()).Filename))
			if filepath.Base(dir) != qual {
				continue
			}
		}
		if len(fd.Body.List) != 1 {
			continue
		}
		if r, ok := fd.Body.List[0].(*ast.ReturnStmt); ok && len(r.Results) == 1 {
			ret = r.Results[0]
		}
	}
	return ret
}

// sseAssignments lists what a function assigns to a local variable name,
// := and = alike; nil when the name is never assigned (a parameter, say).
func sseAssignments(fd *ast.FuncDecl, name string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i, l := range n.Lhs {
					if isIdent(l, name) {
						out = append(out, n.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				if id.Name == name && i < len(n.Values) {
					out = append(out, n.Values[i])
				}
			}
		}
		return true
	})
	return out
}

// sseRecvType is a method's receiver type name, "" for a plain function.
func sseRecvType(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	return receiverType(fd)
}

func isParam(ft *ast.FuncType, name string) bool {
	for _, field := range ft.Params.List {
		for _, n := range field.Names {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}
