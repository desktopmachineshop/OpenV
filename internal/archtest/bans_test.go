package archtest

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
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

// checkOneRouter bans Subrouter, PathPrefix and custom 404/405 handlers in
// production code. A field is caught when set through a selector on a value
// (router.NotFoundHandler = h) or in a composite literal.
func checkOneRouter(c *check) {
	n := 0
	for _, f := range c.m.production() {
		ast.Inspect(f.ast, func(node ast.Node) bool {
			var what string
			switch x := node.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && bannedRouterCalls[sel.Sel.Name] {
					what = "." + sel.Sel.Name + "(...)"
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
				c.violation("%s: %s; register each route as a full template on the one router, and leave 404 and 405 to gorilla/mux's defaults", c.m.pos(node), what)
			}
			return true
		})
	}
	c.baseline("%d Subrouter, PathPrefix, NotFoundHandler or MethodNotAllowedHandler uses", n)
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
