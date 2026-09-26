package archtest

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
)

// Count ratchets. Each counts one legacy idiom; the ceiling in ratchets.json
// may only fall, so the idiom can shrink but never spread. Counts are totals
// over the scope named, not per file, so moving code between files of a
// package never trips them.

var registrarRe = regexp.MustCompile(`^register[A-Z][A-Za-z0-9]*Routes$`)

// checkHandleFuncOutsideRegistrars counts route registrations, a
// two-argument .HandleFunc or .Handle call, made in production code outside
// a registrar: a function named register<Area>Routes (K1). RegisterRoutes is
// the ordered list of registrar calls, so the routes it registers inline
// count, and so does /metrics in cmd/server.
func checkHandleFuncOutsideRegistrars(c *check) {
	var hits []hit
	for _, f := range c.m.production() {
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || registrarRe.MatchString(fd.Name.Name) {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 2 {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "HandleFunc" || sel.Sel.Name == "Handle") {
						hits = append(hits, hit{f.pkg.name, c.m.pos(call)})
					}
				}
				return true
			})
		}
	}
	c.judgeCount("handle_func_outside_registrars", "route registrations outside register<Area>Routes functions", hits)
}

// checkHandlerLiterals counts api.Handler composite literals (&Handler{...}
// or Handler{...}) in test files other than testkit_test.go, whose
// newTestHandler is the one place tests should build a Handler (K6).
// NewHandler's literal is production code and not counted.
func checkHandlerLiterals(c *check) {
	apiPath := c.m.path + "/internal/api"
	var hits []hit
	for _, name := range c.m.names {
		for _, f := range c.m.pkgs[name].tests {
			if f.base == "testkit_test.go" {
				continue
			}
			ast.Inspect(f.ast, func(n ast.Node) bool {
				if lit, ok := n.(*ast.CompositeLit); ok && isAPIHandler(f, lit.Type, apiPath) {
					hits = append(hits, hit{f.pkg.name, c.m.pos(lit)})
				}
				return true
			})
		}
	}
	c.judgeCount("handler_literals_in_tests", "api.Handler literals in test files other than testkit_test.go", hits)
}

func isAPIHandler(f *file, t ast.Expr, apiPath string) bool {
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name == "Handler" && f.pkg.name == "internal/api" && f.ast.Name.Name == "api"
	case *ast.SelectorExpr:
		return x.Sel.Name == "Handler" && f.selectorPkg(x) == apiPath
	}
	return false
}

// apiFiles returns the production files of internal/api and its
// subpackages.
func apiFiles(m *module) []*file {
	var out []*file
	for _, f := range m.production() {
		if under(f.pkg.name, "internal/api") {
			out = append(out, f)
		}
	}
	return out
}

// checkRawEncodes counts json.NewEncoder calls in internal/api outside
// respond.go, the home of the JSON writers (K3, K4). Today every one is
// json.NewEncoder(w).Encode(...) writing a response directly.
func checkRawEncodes(c *check) {
	var hits []hit
	for _, f := range apiFiles(c.m) {
		if f.base == "respond.go" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && f.isPkgFunc(call, "encoding/json", "NewEncoder") {
				hits = append(hits, hit{f.pkg.name, c.m.pos(call)})
			}
			return true
		})
	}
	c.judgeCount("raw_json_encodes", "json.NewEncoder calls in internal/api outside respond.go", hits)
}

// checkInvalidBodyLiterals counts the string literal "invalid request body"
// in internal/api. X1's decodeJSON keeps the one that remains.
func checkInvalidBodyLiterals(c *check) {
	var hits []hit
	for _, f := range apiFiles(c.m) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && s == "invalid request body" {
					hits = append(hits, hit{f.pkg.name, c.m.pos(lit)})
				}
			}
			return true
		})
	}
	c.judgeCount("invalid_request_body_literals", `"invalid request body" literals in internal/api`, hits)
}

var requireRe = regexp.MustCompile(`^require[A-Z]`)

// checkRequireOutsideAuthz counts require* guard helpers declared in
// internal/api outside authz.go, their home (K3).
func checkRequireOutsideAuthz(c *check) {
	var hits []hit
	for _, f := range apiFiles(c.m) {
		if f.base == "authz.go" {
			continue
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && requireRe.MatchString(fd.Name.Name) {
				hits = append(hits, hit{f.pkg.name, c.m.pos(fd)})
			}
		}
	}
	c.judgeCount("require_outside_authz", "require* helpers declared in internal/api outside authz.go", hits)
}

// checkEnvReads counts os.Getenv, os.LookupEnv and os.Environ calls in
// production code under internal/, per package (K8: configuration is read
// in internal/config and cmd/*/config.go, apart from S8's reasoned
// exemptions). A package not listed may read none.
func checkEnvReads(c *check) {
	var hits []hit
	getenv, lines := 0, map[string]bool{}
	for _, f := range c.m.production() {
		if !under(f.pkg.name, "internal") {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && f.isPkgFunc(call, "os", "Getenv", "LookupEnv", "Environ") {
				h := hit{f.pkg.name, c.m.pos(call)}
				hits = append(hits, h)
				if !f.isPkgFunc(call, "os", "Environ") {
					getenv++
					lines[h.pos] = true
				}
			}
			return true
		})
	}
	c.baseline("%d direct env reads under internal/: %d os.Getenv/os.LookupEnv calls on %d lines, plus %d os.Environ calls",
		len(hits), getenv, len(lines), len(hits)-getenv)
	c.next.EnvReads = c.judgePackages(c.stored.EnvReads, hits, "direct env reads")
}
