package api

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The guard reader behind testdata/route_guards.txt, which TestRouteBinding
// (route_binding_test.go) writes and checks.

// guardHelper gives a require* helper its canonical kind. A helper with a
// role parameter appends ":<role>", the value of the role constant passed.
type guardHelper struct {
	kind      string
	roleParam string
}

// guardHelpers maps every require* helper in package api to its canonical
// kind. Helpers that check the same thing share a kind, so merging or
// renaming them leaves route_guards.txt unchanged. A require* helper that is
// missing from this table fails the test.
//
//   - session: a signed-in user from the session cookie, else 401.
//   - worker: a worker key, else 403.
//   - worker-run: the {id} run belongs to the worker's workspace and, for a
//     personal key, to its user or to no one; else 404.
//   - pool-node: the deployment's runner-pool key, else 403.
//   - platform-admin: a signed-in platform admin, else 401 or 403.
//   - json-body: Content-Type application/json, else 415.
//   - runner-sessions: transient runners are configured, else 400.
//   - plan-writable: the plan read-only gate on its own, 403 plan_read_only
//     on a write to a workspace over its plan.
//   - project:<role>: the project role ladder reaches <role>; then, for POST,
//     PUT, PATCH and DELETE, the plan read-only gate, which alwaysWritable
//     routes (route_handlers.txt) skip.
//   - org:<role>: workspace membership, admin when <role> is admin; then the
//     plan read-only gate, as for project.
//   - run:<role>: the run's launcher; else project:<role> for a run in a
//     project, else org:admin.
//   - scoped-write: project:editor for a crew, automation or attribute
//     definition pinned to a project, else org:admin (an attribute
//     definition with neither scope is refused with 400).
//
// Two notations join kinds: "a|b" is one guard whose role is chosen at run
// time from constants the code assigns; "?" is a role argument the reader
// cannot resolve to a constant.
var guardHelpers = map[string]guardHelper{
	"requireUser":                     {kind: "session"},
	"requireHumanUser":                {kind: "session"},
	"requireWorker":                   {kind: "worker"},
	"requireWorkerRun":                {kind: "worker-run"},
	"requirePoolNode":                 {kind: "pool-node"},
	"requirePlatformAdmin":            {kind: "platform-admin"},
	"requireJSONBody":                 {kind: "json-body"},
	"requireRunnerSessions":           {kind: "runner-sessions"},
	"requireWritable":                 {kind: "plan-writable"},
	"requireProjectRole":              {kind: "project", roleParam: "minRole"},
	"requireOrgRole":                  {kind: "org", roleParam: "minRole"},
	"requireRunAccess":                {kind: "run", roleParam: "minRole"},
	"requireTeamWrite":                {kind: "scoped-write"},
	"requireAutomationWrite":          {kind: "scoped-write"},
	"requireAttributeDefinitionWrite": {kind: "scoped-write"},
}

// roleConstants are the role constants a guard's role argument may name.
// Another Role* constant fails the test until it is added here.
var roleConstants = map[string]string{
	"members.RoleViewer":   members.RoleViewer,
	"members.RoleReviewer": members.RoleReviewer,
	"members.RoleEditor":   members.RoleEditor,
	"members.RoleOwner":    members.RoleOwner,
	"orgs.RoleAdmin":       orgs.RoleAdmin,
	"orgs.RoleMember":      orgs.RoleMember,
}

// guardLines lists each METHOD PATH, sorted as in routes.txt, with the
// guards of the handler it is bound to.
func guardLines(t *testing.T, src *apiSource, routes []boundRoute) []string {
	t.Helper()
	checkGuardHelpers(t, src)
	checkGuardResultsUsed(t, src)
	type row struct{ key, line string }
	var rows []row
	for _, r := range routes {
		guards := "-"
		if kinds := src.guardsOf(t, r.handler); len(kinds) > 0 {
			guards = strings.Join(kinds, ", ")
		}
		for _, m := range r.methods {
			key := m + " " + r.path
			rows = append(rows, row{key, key + ": " + guards})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.line
	}
	return lines
}

// checkGuardHelpers fails when a require* helper has no canonical kind, or a
// role-taking helper lost the parameter guardHelpers reads the role from.
func checkGuardHelpers(t *testing.T, src *apiSource) {
	t.Helper()
	var decls []*ast.FuncDecl
	for _, m := range []map[string]*ast.FuncDecl{src.funcs, src.methods} {
		for _, decl := range m {
			decls = append(decls, decl)
		}
	}
	sort.Slice(decls, func(i, j int) bool { return decls[i].Name.Name < decls[j].Name.Name })
	var missing []string
	for _, decl := range decls {
		name := decl.Name.Name
		g, known := guardHelpers[name]
		switch {
		case !known && len(name) > 7 && strings.HasPrefix(name, "require") && name[7] >= 'A' && name[7] <= 'Z':
			missing = append(missing, name)
		case known && g.roleParam != "" && !slices.Contains(paramNames(decl), g.roleParam):
			t.Fatalf("%s: %s has no %s parameter; update guardHelpers", src.pos(decl), name, g.roleParam)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("require* helpers without a canonical kind: %s; add them to guardHelpers in route_binding_guards_test.go",
			strings.Join(missing, ", "))
	}
}

// checkGuardResultsUsed fails when the result of a require* call does not
// decide whether its caller goes on, so that route_guards.txt never counts a
// guard that guards nothing. Every such call in package api, closures
// included, must take one of these shapes; anything else, such as a bare
// h.requireUser(w, r), a "_ =" or an if without a return, fails:
//
//   - the condition of an if whose body ends in a return, seen through !,
//     parentheses, || and &&, and a comparison with nil, as in
//     if !h.requireProjectRole(...) { return };
//   - an operand of a return, which hands the result to the caller, as the
//     helpers in authz.go do;
//   - the sole value of an assignment or var declaration that names the
//     helper's last result, the one that says whether to go on, when that
//     name is then tested by an if of the first shape: the if the
//     assignment opens, or the statement right after it, as in
//     caller := h.requirePlatformAdmin(w, r); if caller == nil { return } or
//     if _, ok := h.requireHumanUser(w, r); !ok { return }.
func checkGuardResultsUsed(t *testing.T, src *apiSource) {
	t.Helper()
	type use struct {
		at   token.Position
		name string
	}
	var bad []use
	for _, m := range []map[string]*ast.FuncDecl{src.funcs, src.methods} {
		for _, fn := range m {
			recv := receiverName(fn)
			var parents []ast.Node
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if n == nil {
					parents = parents[:len(parents)-1]
					return true
				}
				if call, ok := n.(*ast.CallExpr); ok {
					name, decl := src.callee(call, recv)
					if _, guard := guardHelpers[name]; guard && decl != nil && !guardResultUsed(call, decl, parents) {
						bad = append(bad, use{src.pos(call), name})
					}
				}
				parents = append(parents, n)
				return true
			})
		}
	}
	if len(bad) == 0 {
		return
	}
	sort.Slice(bad, func(i, j int) bool {
		a, b := bad[i].at, bad[j].at
		return a.Filename < b.Filename || a.Filename == b.Filename && a.Offset < b.Offset
	})
	lines := make([]string, len(bad))
	for i, u := range bad {
		lines[i] = u.at.String() + ": " + u.name
	}
	t.Fatalf("the result of a require* guard is not checked:\n  %s\n"+
		"A guard must stop the handler, as in if !h.requireProjectRole(...) { return }; checkGuardResultsUsed lists the accepted shapes",
		strings.Join(lines, "\n  "))
}

// guardResultUsed reports whether call, a call to the require* helper decl
// under parents (outermost first), takes a shape checkGuardResultsUsed
// accepts.
func guardResultUsed(call *ast.CallExpr, decl *ast.FuncDecl, parents []ast.Node) bool {
	var child ast.Expr = call
	i := len(parents) - 1
	for ; i >= 0 && keepsGuardResult(parents[i]); i-- {
		child = parents[i].(ast.Expr)
	}
	if i < 0 {
		return false
	}
	switch p := parents[i].(type) {
	case *ast.IfStmt:
		body := p.Body.List
		if p.Cond != child || len(body) == 0 {
			return false
		}
		_, returns := body[len(body)-1].(*ast.ReturnStmt)
		return returns
	case *ast.ReturnStmt:
		return true
	case *ast.AssignStmt:
		name := lastResultName(p.Lhs, decl)
		if len(p.Rhs) != 1 || p.Rhs[0] != call || name == "" || i == 0 {
			return false
		}
		if opened, ok := parents[i-1].(*ast.IfStmt); ok && opened.Init == ast.Stmt(p) {
			return stopsOn(opened, name)
		}
		return stopsOn(nextStmt(parents[i-1], p), name)
	case *ast.ValueSpec:
		lhs := make([]ast.Expr, len(p.Names))
		for k, id := range p.Names {
			lhs[k] = id
		}
		name := lastResultName(lhs, decl)
		// A var declaration sits in a GenDecl in a DeclStmt in a block.
		if len(p.Values) != 1 || p.Values[0] != call || name == "" || i < 3 {
			return false
		}
		stmt, ok := parents[i-2].(*ast.DeclStmt)
		return ok && stopsOn(nextStmt(parents[i-3], stmt), name)
	}
	return false
}

// nextStmt returns the statement after stmt in the statement list of block
// (a block or a case clause), or nil.
func nextStmt(block ast.Node, stmt ast.Stmt) ast.Stmt {
	var list []ast.Stmt
	switch b := block.(type) {
	case *ast.BlockStmt:
		list = b.List
	case *ast.CaseClause:
		list = b.Body
	case *ast.CommClause:
		list = b.Body
	}
	for k, s := range list {
		if s == stmt && k+1 < len(list) {
			return list[k+1]
		}
	}
	return nil
}

// stopsOn reports whether stmt is an if whose condition tests name, seen
// through the operators keepsGuardResult allows, and whose body ends in a
// return.
func stopsOn(stmt ast.Stmt, name string) bool {
	ifs, ok := stmt.(*ast.IfStmt)
	if !ok || len(ifs.Body.List) == 0 || !testsName(ifs.Cond, name) {
		return false
	}
	_, returns := ifs.Body.List[len(ifs.Body.List)-1].(*ast.ReturnStmt)
	return returns
}

// testsName reports whether cond decides on the variable name through only
// !, parentheses, || and &&, and a comparison with nil.
func testsName(cond ast.Expr, name string) bool {
	switch e := cond.(type) {
	case *ast.Ident:
		return e.Name == name
	case *ast.ParenExpr:
		return testsName(e.X, name)
	case *ast.UnaryExpr:
		return e.Op == token.NOT && testsName(e.X, name)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LOR, token.LAND:
			return testsName(e.X, name) || testsName(e.Y, name)
		case token.EQL, token.NEQ:
			return isIdent(e.Y, "nil") && testsName(e.X, name) || isIdent(e.X, "nil") && testsName(e.Y, name)
		}
	}
	return false
}

// keepsGuardResult reports whether a guard's result, as an operand of n,
// still decides the outcome: !, parentheses, || and &&, and a comparison
// with nil.
func keepsGuardResult(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.ParenExpr:
		return true
	case *ast.UnaryExpr:
		return n.Op == token.NOT
	case *ast.BinaryExpr:
		switch n.Op {
		case token.LOR, token.LAND:
			return true
		case token.EQL, token.NEQ:
			return isIdent(n.X, "nil") || isIdent(n.Y, "nil")
		}
	}
	return false
}

// lastResultName returns the name lhs, assigned from a call to decl, gives
// decl's last result, or "" when it gives it none or "_".
func lastResultName(lhs []ast.Expr, decl *ast.FuncDecl) string {
	results := 0
	if decl.Type.Results != nil {
		for _, field := range decl.Type.Results.List {
			results += max(1, len(field.Names))
		}
	}
	if results == 0 || results != len(lhs) {
		return ""
	}
	if id, ok := lhs[results-1].(*ast.Ident); ok && id.Name != "_" {
		return id.Name
	}
	return ""
}

// guardsOf lists the canonical kinds of the guards a handler visibly calls,
// in source order, one entry per call: a kind called twice appears twice,
// so removing one of two identical checks changes the list. It records the
// require* calls in the handler's own body, including closures in it, and
// those directly in the body of any package function or *Handler method the
// handler calls (one level: hasProjectRole and helpers such as
// evidenceBundleChecked, whose role parameter is bound to the handler's
// argument). It does not follow calls two levels down, calls through
// function values or other receivers, or the guards inside a require*
// helper, and it does not record inline checks: CurrentUser(r) == nil,
// IsWorker(r), sessionUser, isOrgAdmin, feature gates, rate limits or
// maybePropose. A call whose result does not stop the handler is not
// counted: checkGuardResultsUsed fails on it first. Who may call a route is
// proved black-box, not here.
func (s *apiSource) guardsOf(t *testing.T, handler string) []string {
	t.Helper()
	fn := s.methods[handler]
	if fn == nil {
		t.Fatalf("handler %s is not a method on *Handler", handler)
	}
	var kinds []string
	add := func(kind string) { kinds = append(kinds, kind) }
	recv, top := receiverName(fn), &roleScope{fn: fn}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, decl := s.callee(call, recv)
		switch _, guard := guardHelpers[name]; {
		case decl == nil || decl == fn:
		case guard:
			add(s.guardKind(t, name, decl, call, top))
		default:
			inner := &roleScope{fn: decl, bindings: bindParams(decl, call), parent: top}
			for _, g := range s.guardCalls(decl) {
				add(s.guardKind(t, g.name, g.decl, g.call, inner))
			}
		}
		return true
	})
	return kinds
}

type guardCall struct {
	name string
	decl *ast.FuncDecl
	call *ast.CallExpr
}

// guardCalls lists the require* calls directly in fn's body.
func (s *apiSource) guardCalls(fn *ast.FuncDecl) []guardCall {
	var out []guardCall
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			name, decl := s.callee(call, receiverName(fn))
			if _, guard := guardHelpers[name]; guard && decl != nil {
				out = append(out, guardCall{name, decl, call})
			}
		}
		return true
	})
	return out
}

func (s *apiSource) guardKind(t *testing.T, name string, decl *ast.FuncDecl, call *ast.CallExpr, scope *roleScope) string {
	t.Helper()
	g := guardHelpers[name]
	if g.roleParam == "" {
		return g.kind
	}
	i := slices.Index(paramNames(decl), g.roleParam)
	if i < 0 || i >= len(call.Args) {
		t.Fatalf("%s: %s is called without its %s argument", s.pos(call), name, g.roleParam)
	}
	roles := scope.roles(t, s, call.Args[i], 0)
	kinds := make([]string, len(roles))
	for k, role := range roles {
		kinds[k] = g.kind + ":" + role
	}
	return strings.Join(kinds, "|")
}

// roleScope resolves role arguments inside fn. When fn is a helper reached
// from a handler, bindings maps its parameters to the handler's arguments,
// which are resolved in parent, the handler's scope.
type roleScope struct {
	fn       *ast.FuncDecl
	bindings map[string]ast.Expr
	parent   *roleScope
}

// roles resolves a role argument to the role values it can hold: a role
// constant such as members.RoleEditor; a helper parameter, through the
// handler's argument; a local variable, through every value fn assigns it.
// Anything else resolves to "?".
func (sc *roleScope) roles(t *testing.T, s *apiSource, e ast.Expr, depth int) []string {
	t.Helper()
	if depth > 4 {
		return []string{"?"}
	}
	switch e := e.(type) {
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok || !s.imports[s.pos(e).Filename][pkg.Name] {
			break // a field such as run.Role, not a constant
		}
		if role, known := roleConstants[pkg.Name+"."+e.Sel.Name]; known {
			return []string{role}
		}
		t.Fatalf("%s: unknown role constant %s.%s; add it to roleConstants", s.pos(e), pkg.Name, e.Sel.Name)
	case *ast.Ident:
		var values []string
		if arg, bound := sc.bindings[e.Name]; bound && sc.parent != nil {
			values = append(values, sc.parent.roles(t, s, arg, depth+1)...)
		}
		for _, v := range assignedValues(sc.fn, e.Name) {
			values = append(values, sc.roles(t, s, v, depth+1)...)
		}
		if len(values) > 0 {
			slices.Sort(values)
			return slices.Compact(values)
		}
	}
	return []string{"?"}
}

// assignedValues lists what fn assigns to the variable name, in name := v,
// name = v and var name = v. A form it does not follow (a multi-value
// assignment, a range variable, a declaration without a value) adds nil,
// which resolves to "?".
func assignedValues(fn *ast.FuncDecl, name string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				switch {
				case !isIdent(lhs, name):
				case len(n.Rhs) == len(n.Lhs):
					out = append(out, n.Rhs[i])
				default:
					out = append(out, nil)
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				switch {
				case id.Name != name:
				case i < len(n.Values):
					out = append(out, n.Values[i])
				default:
					out = append(out, nil)
				}
			}
		case *ast.RangeStmt:
			if isIdent(n.Key, name) || isIdent(n.Value, name) {
				out = append(out, nil)
			}
		}
		return true
	})
	return out
}

// bindParams maps fn's parameters to the arguments of a call to it.
func bindParams(fn *ast.FuncDecl, call *ast.CallExpr) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for i, name := range paramNames(fn) {
		if name != "" && name != "_" && i < len(call.Args) {
			out[name] = call.Args[i]
		}
	}
	return out
}
