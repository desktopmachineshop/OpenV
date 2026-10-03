package archtest

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"
)

// handlersSuffix marks an area file of internal/api (K1): one area's
// handlers and its register<Area>Routes. handlers.go itself is not one.
const handlersSuffix = "_handlers.go"

// helperDecl is an unexported function or method declared in a
// *_handlers.go file.
type helperDecl struct {
	key  string // package:Func or package:Receiver.Method, as func_lines keys it
	file *file
	pos  string
}

// checkHelperHomes fails a cross-file helper declared in an area file (K3):
// an unexported function or method declared in a *_handlers.go file of
// internal/api, or a package below it, and referenced from another
// production file of its package. Such a helper belongs in its home
// (respond.go, httperr.go, errmap.go, authz.go, publish.go, cookies.go or a
// middleware_*.go file). register<Area>Routes is K1's, not a helper, and is
// left out. The rule reads syntax only: a function is referenced by its bare
// name (a call or a function value), a method by any selector naming it
// that is not a package-qualified name, so a local, field or other type's
// method spelled like a helper counts as a reference too.
func checkHelperHomes(c *check) {
	why := map[string]string{}
	var found []string
	for _, name := range c.m.names {
		p := c.m.pkgs[name]
		if !under(name, "internal/api") {
			continue
		}
		funcs, methods := handlerHelpers(c.m, p)
		for key, users := range helperUsers(p, funcs, methods) {
			d := declOf(key, funcs, methods)
			why[key] = fmt.Sprintf("%s, declared at %s, is used by %s; move it to its home (respond.go, httperr.go, "+
				"errmap.go, authz.go, publish.go, cookies.go or a middleware_*.go file), or keep it in the one file that uses it",
				key, d.pos, strings.Join(users, ", "))
			found = append(found, key)
		}
	}
	c.baseline("%d helpers declared in *%s files and used from another file: %v", len(found), handlersSuffix, sortedUnique(found))
	c.next.HelperHomes = c.judgeSet(c.stored.HelperHomes, found,
		func(k string) string { return why[k] },
		func(k string) string {
			return fmt.Sprintf("%s has moved to a shared file or is used by one file only: remove it from helper_homes", k)
		})
}

// handlerHelpers returns the unexported functions and methods declared in
// p's *_handlers.go files, apart from registrars, each by the name a
// reference spells.
func handlerHelpers(m *module, p *pkg) (funcs, methods map[string][]helperDecl) {
	funcs, methods = map[string][]helperDecl{}, map[string][]helperDecl{}
	for _, f := range p.files {
		if !strings.HasSuffix(f.base, handlersSuffix) {
			continue
		}
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || ast.IsExported(fd.Name.Name) || fd.Name.Name == "_" || registrarRe.MatchString(fd.Name.Name) {
				continue
			}
			hd := helperDecl{key: p.name + ":" + funcName(fd), file: f, pos: m.pos(fd)}
			if fd.Recv == nil {
				funcs[fd.Name.Name] = append(funcs[fd.Name.Name], hd)
			} else {
				methods[fd.Name.Name] = append(methods[fd.Name.Name], hd)
			}
		}
	}
	return funcs, methods
}

// helperUsers maps each helper referenced outside its own file to the
// other production files that reference it, sorted.
func helperUsers(p *pkg, funcs, methods map[string][]helperDecl) map[string][]string {
	users := map[string]map[string]bool{}
	use := func(decls []helperDecl, f *file) {
		for _, d := range decls {
			if d.file == f {
				continue
			}
			if users[d.key] == nil {
				users[d.key] = map[string]bool{}
			}
			users[d.key][f.base] = true
		}
	}
	for _, f := range p.files {
		visit := func(n ast.Node) bool { return visitRef(n, f, funcs, methods, use) }
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				ast.Inspect(d, visit)
				continue
			}
			// A declaration's own name is not a reference; the rest is.
			if fd.Recv != nil {
				ast.Inspect(fd.Recv, visit)
			}
			ast.Inspect(fd.Type, visit)
			if fd.Body != nil {
				ast.Inspect(fd.Body, visit)
			}
		}
	}
	out := map[string][]string{}
	for key, files := range users {
		out[key] = sortedKeys(files)
	}
	return out
}

// visitRef records n when it references a helper: a bare identifier naming
// a function, or a selector naming a method. A selector's own name is never
// a bare reference, and a package-qualified selector names something of
// another package.
func visitRef(n ast.Node, f *file, funcs, methods map[string][]helperDecl, use func([]helperDecl, *file)) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		if f.selectorPkg(x) == "" {
			if decls := methods[x.Sel.Name]; decls != nil {
				use(decls, f)
			}
		}
		ast.Inspect(x.X, func(n ast.Node) bool { return visitRef(n, f, funcs, methods, use) })
		return false
	case *ast.Ident:
		if decls := funcs[x.Name]; decls != nil {
			use(decls, f)
		}
	}
	return true
}

func declOf(key string, maps ...map[string][]helperDecl) helperDecl {
	for _, mp := range maps {
		for _, decls := range mp {
			for _, d := range decls {
				if d.key == key {
					return d
				}
			}
		}
	}
	return helperDecl{}
}

// TestHelperHomes proves on a fixture that a helper function or method
// declared in a *_handlers.go file and used from another production file
// fails, and that a registrar, an exported function, a helper used only in
// its own file or only from tests, and a helper declared outside an area
// file do not.
func TestHelperHomes(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":                          "module example.com/fixture\n\ngo 1.25\n",
		"internal/api/a_handlers.go":      helperFixtureA,
		"internal/api/b_handlers.go":      helperFixtureB,
		"internal/api/authz.go":           "package api\n\nfunc requireShared() bool { return true }\n",
		"internal/api/a_handlers_test.go": "package api\n\nfunc useLocal() { _ = localOnly() }\n",
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	got := runRule(t, m, &ratchets{}, checkHelperHomes)
	want := []string{
		"internal/api:Handler.guard, declared at internal/api/a_handlers.go:9, is used by b_handlers.go;",
		"internal/api:respond, declared at internal/api/a_handlers.go:7, is used by b_handlers.go;",
	}
	if len(got) != len(want) {
		t.Fatalf("violations = %q, want %d", got, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("violation %d = %q, want prefix %q", i, got[i], want[i])
		}
	}
	allowed := &ratchets{HelperHomes: []string{"internal/api:Handler.guard", "internal/api:respond", "internal/api:gone"}}
	if got := runRule(t, m, allowed, checkHelperHomes); len(got) != 0 {
		t.Errorf("with both helpers allowed, violations = %q, want none", got)
	}
}

const helperFixtureA = `package api

type Handler struct{}

func (h *Handler) Exported() {}

func respond() {}

func (h *Handler) guard() bool { return localOnly() }

func localOnly() bool { return true }

func (h *Handler) registerARoutes() {}
`

const helperFixtureB = `package api

func (h *Handler) registerBRoutes() {
	h.registerARoutes()
	h.Exported()
	respond()
	f := h.guard
	_ = f() && requireShared()
}
`
