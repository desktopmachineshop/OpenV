package archtest

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// checkNoInit bans func init() in production code (K9): it runs at import
// time, in an order no one reads, in every binary and test that links the
// package. Registries are explicit ordered lists called from main.
func checkNoInit(c *check) {
	n := 0
	for _, f := range c.m.production() {
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "init" {
				n++
				c.violation("%s: func init(); do the work in an explicit call from main or a constructor", c.m.pos(fd))
			}
		}
	}
	c.baseline("%d init functions in production code", n)
}

// Router calls and fields K2 bans: there is one gorilla/mux router, and
// every route is a full template registered on it, in order (I2).
var (
	bannedRouterCalls  = setOf([]string{"Subrouter", "PathPrefix"})
	bannedRouterFields = setOf([]string{"NotFoundHandler", "MethodNotAllowedHandler"})
)

const (
	muxPath      = "github.com/gorilla/mux"
	oneRouterFix = "register each route as a full template on the one router, and leave 404 and 405 to gorilla/mux's defaults"
)

// checkOneRouter bans a second router, Subrouter, PathPrefix and custom
// 404/405 handlers in production code. The one router is the one
// mux.NewRouter() call in cmd/server; another mux.NewRouter(), there or
// elsewhere, or an http.NewServeMux() is a second router. A field is caught
// when set through a selector on a value (router.NotFoundHandler = h) or in
// a composite literal.
func checkOneRouter(c *check) {
	n := 0
	var routers []string // mux.NewRouter() calls in cmd/server
	for _, f := range c.m.production() {
		ast.Inspect(f.ast, func(node ast.Node) bool {
			var what string
			switch x := node.(type) {
			case *ast.CallExpr:
				sel, isSel := x.Fun.(*ast.SelectorExpr)
				switch {
				case isSel && bannedRouterCalls[sel.Sel.Name]:
					what = "." + sel.Sel.Name + "(...)"
				case f.isPkgFunc(x, "net/http", "NewServeMux"):
					what = "http.NewServeMux(), a second router"
				case f.isPkgFunc(x, muxPath, "NewRouter") && f.pkg.name == "cmd/server":
					routers = append(routers, c.m.pos(x))
				case f.isPkgFunc(x, muxPath, "NewRouter"):
					what = "mux.NewRouter() outside cmd/server, a second router"
				}
			case *ast.SelectorExpr:
				if bannedRouterFields[x.Sel.Name] && f.selectorPkg(x) == "" {
					what = "." + x.Sel.Name
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && bannedRouterFields[id.Name] {
					what = id.Name + ":"
				}
			}
			if what != "" {
				n++
				c.violation("%s: %s; %s", c.m.pos(node), what, oneRouterFix)
			}
			return true
		})
	}
	if len(routers) > 1 {
		n += len(routers) - 1
		c.violation("cmd/server builds %d gorilla/mux routers (%s); %s", len(routers), strings.Join(routers, ", "), oneRouterFix)
	}
	c.baseline("%d mux.NewRouter calls in cmd/server; %d second routers or Subrouter, PathPrefix, NotFoundHandler or MethodNotAllowedHandler uses",
		len(routers), n)
}

// pureFuncs are functions and conversions, keyed "importpath.Name", that
// only compute a value from their arguments, so calling them in a
// package-level var initialiser has no side effect.
var pureFuncs = setOf([]string{
	"errors.New", "errors.Join",
	"fmt.Errorf", "fmt.Sprint", "fmt.Sprintf", "fmt.Sprintln",
	"regexp.MustCompile", "regexp.MustCompilePOSIX",
	"text/template.Must", "text/template.New", "html/template.Must", "html/template.New",
	"sync.OnceFunc", "sync.OnceValue", "sync.OnceValues",
	"time.Date", "time.Duration", "time.FixedZone", "time.Month", "time.Unix", "time.Weekday",
	"reflect.TypeFor", "reflect.TypeOf",
	"encoding/json.RawMessage",
	"net/http.HandlerFunc", "net/http.StatusText",
	"math/big.NewFloat", "math/big.NewInt", "math/big.NewRat",
})

// purePackages are standard packages whose every function is pure.
var purePackages = setOf([]string{
	"bytes", "maps", "math", "math/bits", "path", "slices", "sort", "strconv", "strings",
	"unicode", "unicode/utf16", "unicode/utf8",
})

// pureBuiltins are the builtins that compute or allocate without side
// effects, and the predeclared types, whose "call" is a conversion.
var pureBuiltins = setOf([]string{
	"append", "cap", "complex", "imag", "len", "make", "max", "min", "new", "real",
	"any", "bool", "byte", "complex64", "complex128", "error", "float32", "float64",
	"int", "int8", "int16", "int32", "int64", "rune", "string",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
})

// checkPackageVars bans side-effecting package-level variables: a var whose
// initialiser calls anything but a pure constructor (pureFuncs,
// purePackages, pureBuiltins, a conversion, or a method on the result of one
// of those). Package initialisation then does no I/O, reads no environment
// and registers nothing, and a function literal's body is not counted,
// because it runs only when called. Today's hits are grandfathered.
func checkPackageVars(c *check) {
	why := map[string]string{}
	var found []string
	for _, f := range c.m.production() {
		for _, d := range f.ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				for i, v := range vs.Values {
					callee := impureCall(c.m, f, v)
					if callee == "" {
						continue
					}
					names := vs.Names
					if len(vs.Values) == len(vs.Names) {
						names = vs.Names[i : i+1]
					}
					for _, id := range names {
						key := f.pkg.name + ":" + id.Name
						if id.Name == "_" {
							key += "@" + f.base
						}
						why[key] = fmt.Sprintf("%s (%s) calls %s at package initialisation", key, c.m.pos(id), callee)
						found = append(found, key)
					}
				}
			}
		}
	}
	c.baseline("%d package-level vars call something impure: %v", len(found), sortedUnique(found))
	c.next.SideEffectVars = c.judgeSet(c.stored.SideEffectVars, found,
		func(k string) string {
			return why[k] + "; build the value where it is needed, or wrap the call in sync.OnceValue"
		},
		func(k string) string {
			return fmt.Sprintf("%s is gone or pure now: remove it from side_effect_vars", k)
		})
}

// impureCall returns the first callee in expr, outside function literals,
// that is not known to be pure, or "".
func impureCall(m *module, f *file, expr ast.Expr) string {
	found := ""
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if !pureCallee(m, f, x.Fun) {
				found = render(m, x.Fun)
			}
		}
		return true
	})
	return found
}

func pureCallee(m *module, f *file, fun ast.Expr) bool {
	switch x := fun.(type) {
	case *ast.ParenExpr:
		return pureCallee(m, f, x.X)
	case *ast.IndexExpr:
		return pureCallee(m, f, x.X)
	case *ast.IndexListExpr:
		return pureCallee(m, f, x.X)
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.StructType:
		return true // a conversion
	case *ast.Ident:
		_, isType := f.pkg.types[x.Name]
		return isType || pureBuiltins[x.Name]
	case *ast.SelectorExpr:
		if ip := f.selectorPkg(x); ip != "" {
			if pureFuncs[ip+"."+x.Sel.Name] || purePackages[ip] {
				return true
			}
			p := m.pkgs[m.nameOf(ip)]
			return m.internal(ip) && p != nil && p.types[x.Sel.Name] != nil
		}
		if call, ok := x.X.(*ast.CallExpr); ok {
			return pureCallee(m, f, call.Fun) // a method on a value built by a pure call
		}
	}
	return false
}

// render prints an expression, shortened to one line.
func render(m *module, e ast.Expr) string {
	if _, ok := e.(*ast.FuncLit); ok {
		return "an immediately invoked func literal"
	}
	var b bytes.Buffer
	if err := printer.Fprint(&b, m.fset, e); err != nil {
		return "?"
	}
	s := b.String()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + "..."
	}
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
}

// TestRouterRules proves on a fixture that a second router fails the one
// router ban, in cmd/server or elsewhere, and that routes registered through
// a route-builder chain outside a registrar are counted.
func TestRouterRules(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":              "module example.com/fixture\n\ngo 1.25\n",
		"cmd/server/main.go":  routerFixtureMain,
		"cmd/server/wire.go":  "package main\n\nimport \"github.com/gorilla/mux\"\n\nfunc newRouter() *mux.Router { return mux.NewRouter() }\n",
		"internal/api/zz.go":  routerFixtureAPI,
		"internal/api/doc.go": "package api\n",
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	got := runRule(t, m, &ratchets{}, checkOneRouter)
	if len(got) != 3 || !strings.Contains(got[0], "zz.go:14: mux.NewRouter() outside cmd/server, a second router") ||
		!strings.Contains(got[1], "zz.go:17: http.NewServeMux(), a second router") ||
		!strings.Contains(got[2], "cmd/server builds 2 gorilla/mux routers (cmd/server/main.go:10, cmd/server/wire.go:5)") {
		t.Errorf("one router violations = %q, want the mux.NewRouter and http.NewServeMux in internal/api and the second router in cmd/server", got)
	}
	got = runRule(t, m, &ratchets{}, checkHandleFuncOutsideRegistrars)
	if len(got) != 1 || !strings.HasPrefix(got[0], "4 route registrations outside register<Area>Routes functions, above the ceiling of 0") {
		t.Errorf("route count violations = %q, want 4: /health, the builder HandlerFunc and Handler, and Handle", got)
	}
}

const routerFixtureMain = `package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/health", nil).Methods("GET")
	_ = http.ListenAndServe(":0", r)
}
`

const routerFixtureAPI = `package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

func registerZzRoutes(r *mux.Router) {
	r.Path("/in-registrar").Methods("GET").HandlerFunc(nil)
}

func mount(r *mux.Router, collector interface{ Handler(http.Handler) http.Handler }) {
	sub := mux.NewRouter()
	r.NewRoute().Handler(sub)
	r.Path("/api/v1/zz").Methods("GET").HandlerFunc(nil)
	sm := http.NewServeMux()
	r.Handle("/sm", sm)
	_ = http.HandlerFunc(nil)
	_ = collector.Handler(sm)
}
`
