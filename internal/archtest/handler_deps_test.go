package archtest

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// depsHome is the one file of internal/api that may read the raw settings:
// it declares HandlerDeps and NewHandler, which derives Handler's private
// values from them.
const depsHome = "handlers.go"

// rawDeps maps each HandlerDeps field NewHandler derives a private value
// from (K5, M14) to the value a handler reads instead. Handler embeds
// HandlerDeps, so h.FrontendURL compiles, but it is the untrimmed origin,
// and the raw cookie flags miss CrossSiteCookies forcing Secure on.
var rawDeps = map[string]string{
	"CrossSiteCookies": "h.cookieSameSite and h.secureCookies",
	"FrontendURL":      "h.frontendURL, which has no trailing slash",
	"SecureCookies":    "h.secureCookies, which CrossSiteCookies forces on",
}

// checkRawHandlerDeps fails a selector naming a rawDeps field in a file of
// internal/api, production or test, other than handlers.go: h.FrontendURL,
// fx.h.SecureCookies, h.HandlerDeps.CrossSiteCookies, a write as much as a
// read. The rule reads syntax only, so it cannot tell a Handler from
// another type: a selector of a dependency the Handler holds,
// h.GoogleOAuth.FrontendURL or h.OIDC.FrontendURL (one whose operand is a
// selector naming a field of Handler or HandlerDeps other than the
// embedded HandlerDeps), reads that dependency's own field and passes; any
// other look-alike fails. A HandlerDeps composite literal key is no
// selector, so a test still sets the settings there. It also fails when
// HandlerDeps no longer declares a rawDeps field, so that a rename cannot
// turn the rule off unseen.
func checkRawHandlerDeps(c *check) {
	p := c.m.pkgs["internal/api"]
	if p == nil {
		return
	}
	deps := structFieldNames(p, "HandlerDeps")
	for _, name := range sortedKeys(rawDeps) {
		if !deps[name] {
			c.violation("HandlerDeps in internal/api declares no field %s: update rawDeps in internal/archtest/handler_deps_test.go", name)
		}
	}
	holders := structFieldNames(p, "Handler")
	for name := range deps {
		holders[name] = true
	}
	delete(holders, "HandlerDeps")
	n := 0
	for _, f := range append(append([]*file{}, p.files...), p.tests...) {
		if f.base == depsHome {
			continue
		}
		ast.Inspect(f.ast, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok || rawDeps[sel.Sel.Name] == "" || f.selectorPkg(sel) != "" {
				return true
			}
			if x, ok := sel.X.(*ast.SelectorExpr); ok && holders[x.Sel.Name] {
				return true // a dependency's own field: h.GoogleOAuth.FrontendURL
			}
			n++
			c.violation("%s: %s names the raw HandlerDeps.%s outside %s; read %s", c.m.pos(sel),
				types.ExprString(sel), sel.Sel.Name, depsHome, rawDeps[sel.Sel.Name])
			return true
		})
	}
	c.baseline("%d raw HandlerDeps reads (%s) in internal/api outside %s", n,
		strings.Join(sortedKeys(rawDeps), ", "), depsHome)
}

// structFieldNames is the field names (an embedded field's type name
// included) of the struct type name declares in p's production files.
func structFieldNames(p *pkg, name string) map[string]bool {
	names := map[string]bool{}
	td := p.types[name]
	if td == nil {
		return names
	}
	st, ok := td.expr.(*ast.StructType)
	if !ok {
		return names
	}
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			names[n.Name] = true
		}
		if len(f.Names) == 0 {
			t := f.Type
			if star, ok := t.(*ast.StarExpr); ok {
				t = star.X
			}
			if id, ok := t.(*ast.Ident); ok {
				names[id.Name] = true
			}
		}
	}
	return names
}

// TestRawHandlerDeps proves the rule on a fixture: a raw setting read or
// written through a Handler outside handlers.go fails, in production and
// test files; handlers.go itself, a dependency's own field, a composite
// literal key, the derived private values and another package do not; and a
// HandlerDeps that no longer declares a setting fails.
func TestRawHandlerDeps(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":                          "module example.com/fixture\n\ngo 1.25\n",
		"internal/api/handlers.go":        rawDepsHandlers,
		"internal/api/a_handlers.go":      rawDepsArea,
		"internal/api/a_handlers_test.go": rawDepsTest,
		"internal/other/other.go":         "package other\n\ntype cfg struct{ SecureCookies bool }\n\nfunc f(c cfg) bool { return c.SecureCookies }\n",
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	got := runRule(t, m, &ratchets{}, checkRawHandlerDeps)
	sort.Strings(got)
	want := []string{
		"internal/api/a_handlers.go:6: h.FrontendURL names the raw HandlerDeps.FrontendURL outside handlers.go;",
		"internal/api/a_handlers.go:7: h.SecureCookies names the raw HandlerDeps.SecureCookies outside handlers.go;",
		"internal/api/a_handlers.go:8: h.HandlerDeps.CrossSiteCookies names the raw HandlerDeps.CrossSiteCookies outside handlers.go;",
		"internal/api/a_handlers_test.go:9: fx.h.SecureCookies names the raw HandlerDeps.SecureCookies outside handlers.go;",
	}
	if len(got) != len(want) {
		t.Fatalf("violations = %q, want %d", got, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("violation %d = %q, want prefix %q", i, got[i], want[i])
		}
	}

	root = writeFixture(t, map[string]string{
		"go.mod":                   "module example.com/fixture\n\ngo 1.25\n",
		"internal/api/handlers.go": strings.Replace(rawDepsHandlers, "\tCrossSiteCookies bool\n", "", 1),
	})
	if m, err = parseModule(root); err != nil {
		t.Fatal(err)
	}
	got = runRule(t, m, &ratchets{}, checkRawHandlerDeps)
	if len(got) != 1 || !strings.HasPrefix(got[0], "HandlerDeps in internal/api declares no field CrossSiteCookies") {
		t.Errorf("with CrossSiteCookies gone, violations = %q", got)
	}
}

const rawDepsHandlers = `package api

type OAuthConfig struct{ FrontendURL string }

type HandlerDeps struct {
	FrontendURL      string
	SecureCookies    bool
	CrossSiteCookies bool
	GoogleOAuth      *OAuthConfig
}

type Handler struct {
	HandlerDeps
	frontendURL   string
	secureCookies bool
}

func NewHandler(deps HandlerDeps) *Handler {
	h := &Handler{frontendURL: deps.FrontendURL, secureCookies: deps.SecureCookies || deps.CrossSiteCookies}
	h.HandlerDeps = deps
	_ = h.FrontendURL
	return h
}
`

const rawDepsArea = `package api

import "net/http"

func (h *Handler) area(w http.ResponseWriter) {
	_ = h.FrontendURL
	_ = h.SecureCookies
	_ = h.HandlerDeps.CrossSiteCookies
	_ = h.GoogleOAuth.FrontendURL + h.frontendURL
	_ = h.secureCookies
	_ = http.Cookie{Secure: true}
}
`

const rawDepsTest = `package api

import "testing"

type fixture struct{ h *Handler }

func TestArea(t *testing.T) {
	fx := fixture{h: NewHandler(HandlerDeps{SecureCookies: true, FrontendURL: "https://x/"})}
	_ = fx.h.SecureCookies
}
`
