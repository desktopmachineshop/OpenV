package api

import (
	"go/ast"
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

// guardsOf lists the canonical kinds of the guards a handler visibly calls,
// in source order of first appearance. It records the require* calls in
// the handler's own body, including closures in it, and those directly in
// the body of any package function or *Handler method the handler calls
// (one level: hasProjectRole and helpers such as evidenceBundleChecked,
// whose role parameter is bound to the handler's argument). It does not
// follow calls two levels down, calls through function values or other
// receivers, or the guards inside a require* helper, and it does not
// record inline checks: CurrentUser(r) == nil, IsWorker(r), sessionUser,
// isOrgAdmin, feature gates, rate limits or maybePropose. Who may call a
// route is proved black-box, not here.
func (s *apiSource) guardsOf(t *testing.T, handler string) []string {
	t.Helper()
	fn := s.methods[handler]
	if fn == nil {
		t.Fatalf("handler %s is not a method on *Handler", handler)
	}
	var kinds []string
	add := func(kind string) {
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
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
