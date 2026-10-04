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
// purePackages, pureBuiltins, a conversion, or a method on a value a pure
// standard-library function built). Package initialisation then does no
// I/O, reads no environment and registers nothing. A function literal's
// body is not counted, because it runs only when called, unless the
// initialiser calls it: immediately, or by passing it to a pure call that
// runs it (maps.Collect running an iterator), when its body is held to the
// same rule. Today's hits are grandfathered.
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

// impureCall returns the first callee in expr that is not known to be pure,
// or "". Function literals are skipped, as they run only when called,
// except one passed to a pure call that may run it (funcArgsRun): its body
// is read too, with calls of its own parameters counted as pure, since
// they call back into the pure call that runs it (an iterator's yield).
func impureCall(m *module, f *file, expr ast.Expr) string {
	return impureCallIn(m, f, expr, nil)
}

// impureCallIn is impureCall over node, where params are the parameters
// of the function literals node is the body of.
func impureCallIn(m *module, f *file, node ast.Node, params map[string]bool) string {
	found := ""
	ast.Inspect(node, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && params[id.Name] {
				return true // a call back into the pure call running this literal
			}
			if !pureCallee(m, f, x.Fun) {
				found = render(m, x.Fun)
				return false
			}
			if !funcArgsRun(m, f, x.Fun) {
				return true
			}
			for _, arg := range x.Args {
				lit, ok := arg.(*ast.FuncLit)
				if !ok {
					continue
				}
				if callee := impureCallIn(m, f, lit.Body, withParams(params, lit.Type)); callee != "" {
					found = callee + " in a func literal passed to " + render(m, x.Fun)
					return false
				}
			}
		}
		return true
	})
	return found
}

// funcStorers are the pure functions, keyed "importpath.Name", that keep a
// function they are passed for later rather than call it: sync's Once
// wrappers, and net/http.HandlerFunc, a conversion.
var funcStorers = setOf([]string{
	"sync.OnceFunc", "sync.OnceValue", "sync.OnceValues", "net/http.HandlerFunc",
})

// funcArgsRun reports whether a pure call of fun may run a function literal
// it is passed while the package initialises: anything but a conversion,
// which keeps the literal as a value, a builtin (append keeps it too) and
// the funcStorers.
func funcArgsRun(m *module, f *file, fun ast.Expr) bool {
	switch x := fun.(type) {
	case *ast.ParenExpr:
		return funcArgsRun(m, f, x.X)
	case *ast.IndexExpr:
		return funcArgsRun(m, f, x.X)
	case *ast.IndexListExpr:
		return funcArgsRun(m, f, x.X)
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.StructType:
		return false // a conversion
	case *ast.Ident:
		return false // a pure one is a conversion or a builtin; neither calls a function
	case *ast.SelectorExpr:
		if ip := f.selectorPkg(x); ip != "" {
			if funcStorers[ip+"."+x.Sel.Name] {
				return false
			}
			p := m.pkgs[m.nameOf(ip)]
			return !(m.internal(ip) && p != nil && p.types[x.Sel.Name] != nil)
		}
	}
	return true
}

// withParams is params with the parameters of a function literal of type
// ft added.
func withParams(params map[string]bool, ft *ast.FuncType) map[string]bool {
	out := map[string]bool{}
	for name := range params {
		out[name] = true
	}
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			for _, name := range field.Names {
				out[name.Name] = true
			}
		}
	}
	return out
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
			return builtByStd(f, call.Fun) // a method on a value built by a pure call
		}
	}
	return false
}

// builtByStd reports whether fun, called, is a pure function of the
// standard library (pureFuncs, purePackages), or a method on a value one
// built, so that the value's methods are the standard library's too. A
// conversion or a builtin such as new may build a value of this module's
// own type, whose methods are the module's code and may do anything:
// someType(nil).method() is not pure because the conversion is.
func builtByStd(f *file, fun ast.Expr) bool {
	switch x := fun.(type) {
	case *ast.ParenExpr:
		return builtByStd(f, x.X)
	case *ast.IndexExpr:
		return builtByStd(f, x.X)
	case *ast.IndexListExpr:
		return builtByStd(f, x.X)
	case *ast.SelectorExpr:
		if ip := f.selectorPkg(x); ip != "" {
			return pureFuncs[ip+"."+x.Sel.Name] || purePackages[ip]
		}
		if call, ok := x.X.(*ast.CallExpr); ok {
			return builtByStd(f, call.Fun)
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

// TestPackageVarRules proves on a fixture what the side-effecting package
// variable rule finds, and in particular the two ways initialisation-time
// work used to slip past it (#379 bug 97): a function literal that a pure
// call runs while the package initialises, such as maps.Collect running an
// iterator, whose body went unread; and a method on a conversion's or a
// builtin's result, which counted as a method of a value a pure call built
// although it is the module's own code. A literal that is only stored, by
// sync.OnceValue, a conversion or a plain assignment, is still not read.
func TestPackageVarRules(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":                "module example.com/fixture\n\ngo 1.25\n",
		"internal/other/t.go":   "package other\n\ntype T []string\n\nfunc (T) Load() T { return nil }\n",
		"internal/vars/vars.go": packageVarsFixture,
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, v := range runRule(t, m, &ratchets{}, checkPackageVars) {
		key, _, _ := strings.Cut(v, " ")
		got[key] = v
	}
	want := map[string]string{
		"internal/vars:collected":   "calls os.Getenv in a func literal passed to maps.Collect at package initialisation",
		"internal/vars:nested":      "calls os.Getenv in a func literal passed to slices.SortFunc in a func literal passed to maps.Collect at package initialisation",
		"internal/vars:indexed":     "calls os.Getenv in a func literal passed to slices.IndexFunc at package initialisation",
		"internal/vars:converted":   "calls loader(nil).Load at package initialisation",
		"internal/vars:pointer":     "calls (*loader)(nil).Load at package initialisation",
		"internal/vars:allocated":   "calls new(loader).Load at package initialisation",
		"internal/vars:otherType":   "calls other.T(nil).Load at package initialisation",
		"internal/vars:invoked":     "calls an immediately invoked func literal at package initialisation",
		"internal/vars:environment": "calls os.Getenv at package initialisation",
	}
	for key, what := range want {
		if !strings.Contains(got[key], what) {
			t.Errorf("%s: violation %q, want one that %s", key, got[key], what)
		}
	}
	for key, v := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("%s is pure, but the rule reported %q", key, v)
		}
	}
}

const packageVarsFixture = `package vars

import (
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	"example.com/fixture/internal/other"
)

type loader []string

func (loader) Load() loader { return nil }

// Initialisation-time work the rule finds.
var (
	collected = maps.Collect(func(yield func(string, int) bool) {
		yield(os.Getenv("COLLECTED"), 1)
	})
	nested = maps.Collect(func(yield func(string, int) bool) {
		names := []string{"b", "a"}
		slices.SortFunc(names, func(a, b string) int { return strings.Compare(os.Getenv(a), b) })
		yield(names[0], 1)
	})
	indexed   = slices.IndexFunc([]string{"x"}, func(s string) bool { return s == os.Getenv("INDEXED") })
	converted = loader(nil).Load()
	pointer   = (*loader)(nil).Load()
	allocated = new(loader).Load()
	otherType = other.T(nil).Load()
	invoked   = func() int { return 1 }()
	environment = os.Getenv("ENVIRONMENT")
)

// Pure, or work left for later: none of these is reported.
var (
	pureIterator = maps.Collect(func(yield func(string, int) bool) {
		for _, k := range []string{"a", "b"} {
			if !yield(strings.ToUpper(k), len(k)) {
				return
			}
		}
	})
	replaced   = strings.NewReplacer("a", "b").Replace("abc")
	conversion = loader(nil)
	once       = sync.OnceValue(func() string { return os.Getenv("ONCE") })
	handler    = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { os.Exit(1) })
	stored     = func() string { return os.Getenv("STORED") }
	table      = map[string]func() string{"a": func() string { return os.Getenv("TABLE") }}
	appended   = append([]func() string{}, func() string { return os.Getenv("APPENDED") })
)
`

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
