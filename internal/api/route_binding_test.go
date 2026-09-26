package api

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// The route inventory (route_inventory_test.go) pins which METHOD PATH
// pairs exist. It cannot see which handler serves each one, in what order
// they are registered, or which guards the handler calls, and all three are
// part of what a client of the HTTP API observes (REQ-143). This test pins
// them in three goldens under testdata/:
//
//   - route_handlers.txt: every registration, in the order RegisterRoutes
//     makes it, as "METHODS PATH -> Handler [wrappers]". gorilla/mux serves
//     the first registered route that matches, so the order is the contract.
//   - route_overlaps.txt: for every pair of same-method templates that one
//     request path can match, such a path and the route router.Match picks
//     for it, with the routes it shadows.
//   - route_guards.txt: per METHOD PATH, the require* guards its handler
//     visibly calls, as canonical kinds in call order. The reader and the
//     kinds are in route_binding_guards_test.go.
//
// Handlers are named twice, and the two must agree. The source is read with
// go/ast, starting at RegisterRoutes and following every registrar it
// calls. The router is read at run time, with runtime.FuncForPC. The source
// is needed because a wrapped handler such as h.alwaysWritable(h.DeleteProject)
// is, at run time, only a closure inside alwaysWritable. The runtime check
// proves the reader saw the registrations the router really holds, in the
// same order. Any disagreement, or a registration shape the reader does not
// know, fails the test instead of producing a wrong golden.
//
// The goldens change only by regeneration. Adding or removing a route
// changes testdata/routes.txt (route_inventory_test.go) as well, so one
// command rewrites all four and names each file it writes:
//
//	UPDATE_ROUTES=1 go test ./internal/api -count=1 -v -run 'TestRouteInventory|TestRouteBinding'
//
// A change to route_handlers.txt or route_overlaps.txt changes routing or
// binding, so it is never part of a refactor. A change to route_guards.txt
// is either an authorization change or a change in call shape only, such as
// an inline refusal becoming a require* helper. This test cannot tell the
// two apart; the black-box authorization matrix (step S5e of
// docs/plans/codebase-refactor.md), unchanged, is what shows it is the
// second.
const regenerateRouteGoldens = "UPDATE_ROUTES=1 go test ./internal/api -count=1 -v -run 'TestRouteInventory|TestRouteBinding'"

// What a changed golden means, as its failure message says it.
const (
	bindingChange = "A change to this golden changes routing or binding, so it is never a refactor."
	guardsChange  = "A change to this golden is either an authorization change or a change in call shape only, " +
		"such as an inline refusal becoming a require* helper. This test cannot tell the two apart; " +
		"the black-box authorization matrix (step S5e of docs/plans/codebase-refactor.md), unchanged, " +
		"is what shows it is the second."
)

func TestRouteBinding(t *testing.T) {
	src := parseAPISource(t)
	router := routeTable()
	routes := bindRoutes(t, src, router)

	t.Run("handlers", func(t *testing.T) {
		checkRouteGolden(t, "route_handlers.txt", handlersHeader, bindingChange, handlerLines(routes))
	})
	t.Run("overlaps", func(t *testing.T) {
		checkRouteGolden(t, "route_overlaps.txt", overlapsHeader, bindingChange, overlapLines(t, router, routes))
	})
	t.Run("guards", func(t *testing.T) {
		checkRouteGolden(t, "route_guards.txt", guardsHeader, guardsChange, guardLines(t, src, routes))
	})
}

var handlersHeader = []string{
	"Route registration order and handler binding, one line per registration in",
	"the order RegisterRoutes makes it: METHODS PATH -> Handler [wrappers].",
	"gorilla/mux serves the first registered match, so the order is the contract.",
}

var overlapsHeader = []string{
	"Same-method templates that one request path can match: the path, the route",
	"router.Match picks for it, and the routes it shadows, in registration order.",
}

var guardsHeader = []string{
	"The require* guards each route's handler visibly calls, directly or through",
	"one helper, as canonical kinds in call order; \"-\" when it calls none. A list",
	"names every call found, and a branch may make only some of them. The kinds",
	"are documented at guardHelpers in the internal/api tests. This records what",
	"the code visibly does; it is not an authorization proof.",
}

// ---- Reading the source ----

// apiSource is the production code of package api, parsed: the files go
// build compiles for this platform, without tests.
type apiSource struct {
	fset    *token.FileSet
	funcs   map[string]*ast.FuncDecl   // package-level functions
	methods map[string]*ast.FuncDecl   // methods on Handler
	imports map[string]map[string]bool // file name -> the package names it imports
}

func parseAPISource(t *testing.T) *apiSource {
	t.Helper()
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("list the files of package api: %v", err)
	}
	src := &apiSource{
		fset:    token.NewFileSet(),
		funcs:   map[string]*ast.FuncDecl{},
		methods: map[string]*ast.FuncDecl{},
		imports: map[string]map[string]bool{},
	}
	files := append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...)
	for _, name := range files {
		file, err := parser.ParseFile(src.fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		src.imports[name] = map[string]bool{}
		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			local := importPath[strings.LastIndex(importPath, "/")+1:]
			if imp.Name != nil {
				local = imp.Name.Name
			}
			src.imports[name][local] = true
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			switch {
			case !ok || fn.Body == nil:
			case fn.Recv == nil:
				src.funcs[fn.Name.Name] = fn
			case receiverType(fn) == "Handler":
				src.methods[fn.Name.Name] = fn
			}
		}
	}
	return src
}

func (s *apiSource) pos(n ast.Node) token.Position { return s.fset.Position(n.Pos()) }

// callee resolves a call to a package function, f(...), or to a method on
// the caller's own receiver, h.f(...). Any other call resolves to nil.
func (s *apiSource) callee(call *ast.CallExpr, recv string) (string, *ast.FuncDecl) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name, s.funcs[fun.Name]
	case *ast.SelectorExpr:
		if recv != "" && isIdent(fun.X, recv) {
			return fun.Sel.Name, s.methods[fun.Sel.Name]
		}
	}
	return "", nil
}

func receiverType(fn *ast.FuncDecl) string {
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// paramNames lists fn's parameters in order, with "" for an unnamed one.
func paramNames(fn *ast.FuncDecl) []string {
	var names []string
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			names = append(names, "")
		}
		for _, id := range field.Names {
			names = append(names, id.Name)
		}
	}
	return names
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// ---- Registration order and binding ----

// boundRoute is one registration, read from the source and then checked
// against the route the router holds at the same position.
type boundRoute struct {
	methods  []string
	path     string
	handler  string   // the *Handler method the registration binds
	wrappers []string // outermost first, such as alwaysWritable
	at       token.Position
	route    *mux.Route
}

// readRegistrations reads RegisterRoutes and every registrar it calls, in
// call order. A registrar holds only these two statement shapes; anything
// else fails, so that this reader is extended rather than silently wrong:
//
//	router.HandleFunc("<path>", <handler>).Methods("<METHOD>", ...)
//	h.register<Area>Routes(router)
func readRegistrations(t *testing.T, src *apiSource) []boundRoute {
	t.Helper()
	var out []boundRoute
	visited := map[string]bool{}
	var read func(name string)
	read = func(name string) {
		fn := src.methods[name]
		if fn == nil || visited[name] {
			t.Fatalf("registrar %s is missing or called twice", name)
		}
		visited[name] = true
		recv, params := receiverName(fn), paramNames(fn)
		if len(params) != 1 {
			t.Fatalf("%s: registrar %s must take only the router", src.pos(fn), name)
		}
		for _, stmt := range fn.Body.List {
			var call *ast.CallExpr
			if es, ok := stmt.(*ast.ExprStmt); ok {
				call, _ = es.X.(*ast.CallExpr)
			}
			if call == nil {
				t.Fatalf("%s: registrar %s holds a statement readRegistrations does not know; extend route_binding_test.go",
					src.pos(stmt), name)
			}
			if sub := registrarCall(call, recv, params[0]); sub != "" {
				read(sub)
				continue
			}
			out = append(out, readRegistration(t, src, call, recv, params[0]))
		}
	}
	read("RegisterRoutes")
	return out
}

// registrarCall names the method a call hands the router to, as in
// h.registerAgentRoutes(router), or returns "".
func registrarCall(call *ast.CallExpr, recv, router string) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !isIdent(sel.X, recv) || len(call.Args) != 1 || !isIdent(call.Args[0], router) {
		return ""
	}
	return sel.Sel.Name
}

func readRegistration(t *testing.T, src *apiSource, call *ast.CallExpr, recv, router string) boundRoute {
	t.Helper()
	r := boundRoute{at: src.pos(call)}
	fail := func(what string) {
		t.Fatalf("%s: %s; readRegistration knows router.HandleFunc(\"<path>\", <handler>).Methods(...) only, so extend route_binding_test.go",
			r.at, what)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Methods" {
		fail("a registration without .Methods(...) as its last call")
	}
	for _, arg := range call.Args {
		method, ok := stringLit(arg)
		if !ok {
			fail("a method that is not a string literal")
		}
		r.methods = append(r.methods, strings.ToUpper(method))
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		fail("an unknown call before .Methods")
	}
	fun, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || fun.Sel.Name != "HandleFunc" || !isIdent(fun.X, router) || len(inner.Args) != 2 {
		fail("a registration that is not router.HandleFunc(path, handler)")
	}
	if r.path, ok = stringLit(inner.Args[0]); !ok {
		fail("a path that is not a string literal")
	}
	if r.handler, r.wrappers, ok = handlerExpr(inner.Args[1], recv); !ok {
		fail("a handler that is neither h.Method nor a wrapper call such as h.alwaysWritable(h.Method)")
	}
	return r
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// handlerExpr names the method a registration binds, peeling wrapper calls
// such as h.alwaysWritable(h.DeleteProject), outermost first.
func handlerExpr(e ast.Expr, recv string) (string, []string, bool) {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		if isIdent(e.X, recv) {
			return e.Sel.Name, nil, true
		}
	case *ast.CallExpr:
		wrapper := ""
		switch fun := e.Fun.(type) {
		case *ast.SelectorExpr:
			if isIdent(fun.X, recv) {
				wrapper = fun.Sel.Name
			}
		case *ast.Ident:
			wrapper = fun.Name
		}
		if wrapper == "" || len(e.Args) != 1 {
			return "", nil, false
		}
		name, inner, ok := handlerExpr(e.Args[0], recv)
		return name, append([]string{wrapper}, inner...), ok
	}
	return "", nil, false
}

// bindRoutes pairs each registration read from the source with the route
// the router holds at the same position, and fails on any disagreement in
// order, count, methods, path or handler.
func bindRoutes(t *testing.T, src *apiSource, router *mux.Router) []boundRoute {
	t.Helper()
	routes := readRegistrations(t, src)
	var held []*mux.Route
	err := router.Walk(func(route *mux.Route, _ *mux.Router, ancestors []*mux.Route) error {
		if len(ancestors) > 0 {
			return fmt.Errorf("a subrouter holds routes; the route goldens assume one flat router")
		}
		held = append(held, route)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	for i := 0; i < len(routes) && i < len(held); i++ {
		r := &routes[i]
		r.route = held[i]
		path, _ := held[i].GetPathTemplate()
		methods, _ := held[i].GetMethods()
		want := r.handler
		if len(r.wrappers) > 0 {
			want = "closure:" + r.wrappers[0]
		}
		got := handlerIdentity(held[i].GetHandler())
		if path != r.path || strings.Join(methods, ",") != strings.Join(r.methods, ",") || got != want {
			t.Fatalf("route %d: the source registers %s %s -> %s at %s, but the router holds %s %s -> %s there",
				i+1, strings.Join(r.methods, ","), r.path, want, r.at, strings.Join(methods, ","), path, got)
		}
	}
	if len(routes) != len(held) {
		t.Fatalf("the source registers %d routes but the router holds %d; readRegistrations missed a registration",
			len(routes), len(held))
	}
	return routes
}

// closureName matches the compiler's name for a function literal, such as
// alwaysWritable.func1, or func1.2 for one nested inside another.
var closureName = regexp.MustCompile(`\.func\d+(\.\d+)*$`)

// handlerIdentity names the function a registered handler runs. A method
// value (h.ListProjects, compiled as "(*Handler).ListProjects-fm") becomes
// "ListProjects". A closure becomes "closure:" plus the named function that
// holds its literal: the compiler calls alwaysWritable's closure
// "(*Handler).alwaysWritable.func1" when inlining is off (-gcflags=-l) and,
// once inlined, "(*Handler).RegisterRoutes.(*Handler).alwaysWritable.func1",
// numbered per call site, so both become "closure:alwaysWritable".
func handlerIdentity(h http.Handler) string {
	v := reflect.ValueOf(h)
	if v.Kind() != reflect.Func {
		return fmt.Sprintf("%T", h)
	}
	name := runtime.FuncForPC(v.Pointer()).Name()
	name = name[strings.LastIndex(name, "/")+1:] // the import path
	name = name[strings.Index(name, ".")+1:]     // the package name
	if closureName.MatchString(name) {
		outer := closureName.ReplaceAllString(name, "")
		return "closure:" + outer[strings.LastIndex(outer, ".")+1:]
	}
	return strings.TrimPrefix(strings.TrimSuffix(name, "-fm"), "(*Handler).")
}

func handlerLines(routes []boundRoute) []string {
	lines := make([]string, len(routes))
	for i, r := range routes {
		lines[i] = strings.Join(r.methods, ",") + " " + r.path + " -> " + r.handler
		if len(r.wrappers) > 0 {
			lines[i] += " [" + strings.Join(r.wrappers, " ") + "]"
		}
	}
	return lines
}

// ---- Overlapping templates ----

// overlapLines finds every pair of same-method templates that one request
// path can match, builds such a path, and records the route router.Match
// picks for it. Pairs are visited in registration order.
func overlapLines(t *testing.T, router *mux.Router, routes []boundRoute) []string {
	t.Helper()
	var lines []string
	seen := map[string]bool{}
	for i, a := range routes {
		for _, b := range routes[i+1:] {
			for _, method := range sharedMethods(a.methods, b.methods) {
				path, ok := commonPath(t, a.path, b.path)
				if !ok || !matchesAlone(a.route, method, path) || !matchesAlone(b.route, method, path) {
					continue
				}
				if line := describeOverlap(t, router, routes, method, path); !seen[line] {
					seen[line] = true
					lines = append(lines, line)
				}
			}
		}
	}
	return lines
}

func sharedMethods(a, b []string) []string {
	var out []string
	for _, m := range a {
		for _, n := range b {
			if m == n {
				out = append(out, m)
			}
		}
	}
	return out
}

// commonPath builds the most specific path both templates can match: equal
// literal segments stay, a literal fills the other template's variable, and
// two variables take "x". It reports false when two literals differ or the
// segment counts do (mux's default variable pattern never spans a slash).
func commonPath(t *testing.T, a, b string) (string, bool) {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	if len(as) != len(bs) {
		return "", false
	}
	out := make([]string, len(as))
	for i := range as {
		av, bv := templateVar(t, as[i]), templateVar(t, bs[i])
		switch {
		case av && bv:
			out[i] = "x"
		case av:
			out[i] = bs[i]
		case bv, as[i] == bs[i]:
			out[i] = as[i]
		default:
			return "", false
		}
	}
	return strings.Join(out, "/"), true
}

// templateVar reports whether a path segment is one whole variable, {id}.
// commonPath assumes mux's default pattern, so a custom one ({id:[0-9]+})
// or a variable inside a segment (report-{id}.pdf) fails the test rather
// than be guessed at.
func templateVar(t *testing.T, seg string) bool {
	if !strings.ContainsAny(seg, "{}") {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
	if len(name) != len(seg)-2 || name == "" || strings.ContainsAny(name, "{}:") {
		t.Fatalf("path segment %q: commonPath knows whole {name} variables only; extend route_binding_test.go", seg)
	}
	return true
}

func matchesAlone(route *mux.Route, method, path string) bool {
	var match mux.RouteMatch
	return route.Match(httptest.NewRequest(method, path, nil), &match)
}

// describeOverlap asks the whole router which route serves method and path,
// and lists every other route whose template matches it too.
func describeOverlap(t *testing.T, router *mux.Router, routes []boundRoute, method, path string) string {
	t.Helper()
	var match mux.RouteMatch
	if !router.Match(httptest.NewRequest(method, path, nil), &match) {
		t.Fatalf("%s %s: two templates match it but router.Match finds no route", method, path)
	}
	picked, shadowed := "", []string{}
	for _, r := range routes {
		if len(sharedMethods(r.methods, []string{method})) == 0 || !matchesAlone(r.route, method, path) {
			continue
		}
		desc := r.handler + " (" + r.path + ")"
		if r.route == match.Route {
			picked = desc
		} else {
			shadowed = append(shadowed, desc)
		}
	}
	if picked == "" {
		t.Fatalf("%s %s: router.Match picked a route that is not in the registration list", method, path)
	}
	return method + " " + path + " -> " + picked + ", shadows " + strings.Join(shadowed, ", ")
}

// ---- Golden files ----

// checkRouteGolden compares lines, under a "# " header, with
// testdata/<name>, and on a mismatch says what a change means. With
// UPDATE_ROUTES set it rewrites the file instead, which is the only way
// these goldens change.
func checkRouteGolden(t *testing.T, name string, header []string, meaning string, lines []string) {
	t.Helper()
	var b strings.Builder
	for _, h := range header {
		b.WriteString("# " + h + "\n")
	}
	b.WriteString("# Regenerate: " + regenerateRouteGoldens + "\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	current := b.String()
	file := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_ROUTES") != "" {
		if err := os.WriteFile(file, []byte(current), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		t.Logf("wrote %s", file)
		return
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with: %s", file, err, regenerateRouteGoldens)
	}
	pinned := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if pinned == current {
		return
	}
	t.Fatalf("%s does not match the code; first differences (- pinned, + current, by line):\n%s\n"+
		"%s If the change is intended, regenerate with:\n  %s",
		file, strings.Join(lineDiff(pinned, current, 12), "\n"), meaning, regenerateRouteGoldens)
}

// lineDiff returns up to limit lines of a minimal line diff of two texts, as
// "- n: line" for a pinned line and "+ n: line" for a current one.
func lineDiff(pinned, current string, limit int) []string {
	a := strings.Split(strings.TrimSuffix(pinned, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(current, "\n"), "\n")
	// common[i][j] is the length of the longest common subsequence of
	// a[i:] and b[j:].
	common := make([][]int, len(a)+1)
	for i := range common {
		common[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out []string
	changed := 0
	emit := func(sign string, n int, line string) {
		if changed++; changed <= limit {
			out = append(out, fmt.Sprintf("  %s %d: %s", sign, n, line))
		}
	}
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || common[i+1][j] >= common[i][j+1]):
			emit("-", i+1, a[i])
			i++
		default:
			emit("+", j+1, b[j])
			j++
		}
	}
	switch {
	case changed > limit:
		out = append(out, fmt.Sprintf("  ... and %d more changed lines", changed-limit))
	case changed == 0:
		out = append(out, "  (the files differ only in their final newline)")
	}
	return out
}
