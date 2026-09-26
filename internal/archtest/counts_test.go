package archtest

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Count ratchets. Each counts one legacy idiom; the ceiling in ratchets.json
// may only fall, so the idiom can shrink but never spread. Counts are totals
// over the scope named, not per file, so moving code between files of a
// package never trips them.

var registrarRe = regexp.MustCompile(`^register[A-Z][A-Za-z0-9]*Routes$`)

// checkHandleFuncOutsideRegistrars counts route registrations made in
// production code outside a registrar: a function named
// register<Area>Routes (K1). RegisterRoutes is the ordered list of registrar
// calls, so the routes it registers inline count, and so does /metrics in
// cmd/server.
func checkHandleFuncOutsideRegistrars(c *check) {
	var hits []hit
	for _, f := range c.m.production() {
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || registrarRe.MatchString(fd.Name.Name) {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && registersRoute(call) {
					hits = append(hits, hit{f.pkg.name, c.m.pos(call)})
				}
				return true
			})
		}
	}
	c.judgeCount("handle_func_outside_registrars", "route registrations outside register<Area>Routes functions", hits)
}

// registersRoute reports whether call registers a route: a two-argument
// .HandleFunc(path, h) or .Handle(path, h), or a one-argument .HandlerFunc(h)
// or .Handler(h) ending a route-builder chain, such as
// r.Path(p).Methods(m).HandlerFunc(h) or r.NewRoute().Handler(h). Requiring
// the chain skips conversions such as http.HandlerFunc(fn) and methods of a
// plain value, such as collector.Handler(next).
func registersRoute(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "HandleFunc", "Handle":
		return len(call.Args) == 2
	case "HandlerFunc", "Handler":
		_, chained := sel.X.(*ast.CallExpr)
		return len(call.Args) == 1 && chained
	}
	return false
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

// envReaders are the functions that read the process environment, keyed
// "importpath.Name".
var envReaders = setOf([]string{
	"os.Getenv", "os.LookupEnv", "os.ExpandEnv", "os.Environ", "syscall.Getenv", "syscall.Environ",
})

// checkEnvReads counts references to the envReaders in production code under
// internal/, per package (K8: configuration is read in internal/config and
// cmd/*/config.go, apart from S8's reasoned exemptions). A reference is a
// call or a function value (var getenv = os.Getenv; os.Expand(s,
// os.Getenv)); a call counts once, at its selector. A package not listed
// may read none.
func checkEnvReads(c *check) {
	var hits []hit
	getenv, lines := 0, map[string]bool{}
	for _, f := range c.m.production() {
		if !under(f.pkg.name, "internal") {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !envReaders[f.selectorPkg(sel)+"."+sel.Sel.Name] {
				return true
			}
			h := hit{f.pkg.name, c.m.pos(sel)}
			hits = append(hits, h)
			if sel.Sel.Name != "Environ" {
				getenv++
				lines[h.pos] = true
			}
			return true
		})
	}
	c.baseline("%d direct env reads under internal/: %d Getenv/LookupEnv/ExpandEnv references on %d lines, plus %d Environ references",
		len(hits), getenv, len(lines), len(hits)-getenv)
	c.next.EnvReads = c.judgePackages(c.stored.EnvReads, hits, "direct env reads")
}

// TestEnvReadForms proves on a fixture that the env-read rule counts every
// reference to an env reader under internal/, called or passed as a value,
// and nothing under cmd/.
func TestEnvReadForms(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":                   "module example.com/fixture\n\ngo 1.25\n",
		"cmd/tool/main.go":         "package main\n\nimport \"os\"\n\nfunc main() { _ = os.Getenv(\"A\") }\n",
		"internal/domain/x/env.go": envFixture,
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	got := runRule(t, m, &ratchets{}, checkEnvReads)
	want := "internal/domain/x: 7 direct env reads, above its ceiling of 0:\n" + strings.Join([]string{
		"internal/domain/x/env.go:9", "internal/domain/x/env.go:12", "internal/domain/x/env.go:13", "internal/domain/x/env.go:14",
		"internal/domain/x/env.go:15", "internal/domain/x/env.go:16", "internal/domain/x/env.go:17"}, "\n")
	if len(got) != 1 || got[0] != want {
		t.Errorf("violations = %q, want %q", got, want)
	}
}

const envFixture = `package x

import (
	"os"
	"strings"
	"syscall"
)

var getenv = os.Getenv

func read() {
	_ = os.Getenv("A")
	_ = os.ExpandEnv("$X")
	_ = os.Expand("$Y", os.Getenv)
	_, _ = syscall.Getenv("Z")
	_ = syscall.Environ()
	_ = len(os.Environ())
	_ = strings.ToUpper("os.Getenv")
}
`
