package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// This file turns the env scan (env_scan_test.go) into the inventory's rows
// and its failures: names that do not resolve and no exemption covers, env
// readers or getters used as values the scan cannot follow, getters no
// followed call reaches, reads outside a function declaration, exemptions
// that match nothing, and reads in files the typed build leaves out.

// envRow is one resolved read of one variable.
type envRow struct {
	name string
	read string // the read column
	def  string // the default column
	at   envRead
}

// envResult is everything the scan found.
type envResult struct {
	rows    []envRow
	unknown []envRead // reads whose name does not resolve
	environ []envRead // reads of the whole environment
	bad     []string  // escapes, unfollowed getters and reads outside a function declaration
	getters []*envFunc
}

// envExemption is a read the inventory accepts without a name, or a named
// read that stays where it is when X10 moves configuration into
// internal/config. The golden holds the id, the kind, the names and the
// reason; at, which functions it covers, is guard code only, so moving a
// read changes this table and not the golden.
type envExemption struct {
	id     string
	kind   string   // unresolved, environ or placement
	names  []string // placement: the variables it keeps in place
	at     []string // package:Func of the functions whose reads it covers
	reason string
}

// run scans a typed program: it settles the getters, then collects every
// read, escape and read outside a function.
func (s *envScan) run() (*envResult, error) {
	if err := s.settle(); err != nil {
		return nil, err
	}
	res := &envResult{}
	unused := s.unusedAccessors()
	for _, f := range s.order {
		if len(f.gets) > 0 || len(f.bound) > 0 {
			res.getters = append(res.getters, f)
		}
		reads, _ := s.walk(f)
		for _, r := range reads {
			switch {
			case r.environ:
				res.environ = append(res.environ, r)
			case r.name.unknown != "":
				res.unknown = append(res.unknown, r)
			case unused[f]:
				// An accessor of a live snapshot that nothing outside its
				// package uses reads nothing anyone sees.
			default:
				for _, t := range r.name.alts {
					if t.firstParam() < 0 {
						res.rows = append(res.rows, envRow{name: t.String(), read: r.read, def: envDefault(f.pkg, r.def), at: r})
					}
				}
			}
		}
	}
	res.bad = append(s.escapes(), s.unfollowed()...)
	return res, nil
}

// unfollowed lists the getters that read through a string parameter but
// that no call the walk follows reaches: called only through an interface
// method or a function value, whose names are then nowhere in the
// inventory, or not called at all. A getter reached through a function
// value is also an escape; one reached through an interface method that
// is also called directly is a read with no name at the interface call.
func (s *envScan) unfollowed() []string {
	var out []string
	for _, g := range s.order {
		if len(g.gets) > 0 && !g.called {
			out = append(out, fmt.Sprintf("%s: the getter %s reads the environment, but no call of it is followed: a call through"+
				" an interface or a function value hides the names it reads (call it directly), and a getter nothing calls goes",
				envPos(s.prog.fset, g.decl), g.key))
		}
	}
	return out
}

// envReaderRef reports whether obj is something that reads the environment
// when called: a standard env function, a getter, an interface method a
// getter implements, or a parameter bound to an env reader.
func (s *envScan) envReaderRef(obj types.Object, boundVars map[*types.Var]bool) bool {
	switch o := obj.(type) {
	case *types.Func:
		if q := qualified(o); q != "os.Expand" && envSinks[q] != "" {
			return true
		}
		if len(s.ifaceGetters(o)) > 0 {
			return true
		}
		g := s.funcs[o]
		return g != nil && len(g.gets) > 0
	case *types.Var:
		return boundVars[o]
	}
	return false
}

// escapes lists the references to an env reader that the scan cannot
// follow: anything but a call's function, an argument bound to a
// function-typed parameter, or os.Expand's mapping. A call outside every
// function declaration (a package variable's initialiser, a function literal
// stored in one) is listed too, since the scan reads function bodies only.
func (s *envScan) escapes() []string {
	boundVars := map[*types.Var]bool{}
	for _, f := range s.order {
		for i := range f.bound {
			boundVars[f.params[i]] = true
		}
	}
	var out []string
	for _, p := range s.prog.pkgs {
		for _, file := range p.files {
			calls, allowed := s.allowedRefs(p, file)
			for _, d := range file.Decls {
				_, inFunc := d.(*ast.FuncDecl)
				var visit func(n ast.Node) bool
				visit = func(n ast.Node) bool {
					e, ok := n.(ast.Expr)
					if !ok {
						return true
					}
					switch x := e.(type) {
					case *ast.Ident, *ast.SelectorExpr:
						if s.envReaderRef(s.objectOf(p, x), boundVars) {
							where := s.prog.fset.Position(x.Pos())
							switch {
							case !allowed[x]:
								out = append(out, fmt.Sprintf("%s:%d: %s is used as a value, which the scan cannot follow (an escape)",
									where.Filename, where.Line, envSource(s.prog.fset, x)))
							case calls[x] && !inFunc:
								out = append(out, fmt.Sprintf("%s:%d: %s is called outside a function declaration, where the scan does not look",
									where.Filename, where.Line, envSource(s.prog.fset, x)))
							}
						}
						if sel, ok := x.(*ast.SelectorExpr); ok {
							ast.Inspect(sel.X, visit)
							return false
						}
					}
					return true
				}
				ast.Inspect(d, visit)
			}
		}
	}
	return out
}

// allowedRefs marks, in one file, the expressions an env reader may be:
// a call's function (calls) and the arguments bound to a function-typed
// parameter or given to os.Expand as its mapping (allowed holds all).
func (s *envScan) allowedRefs(p *envPkg, file *ast.File) (calls, allowed map[ast.Expr]bool) {
	calls, allowed = map[ast.Expr]bool{}, map[ast.Expr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := ast.Unparen(call.Fun)
		switch x := fun.(type) {
		case *ast.IndexExpr:
			fun = ast.Unparen(x.X)
		case *ast.IndexListExpr:
			fun = ast.Unparen(x.X)
		}
		calls[fun], allowed[fun] = true, true
		fn, ok := s.callee(p, call).(*types.Func)
		if !ok {
			return true
		}
		if qualified(fn) == "os.Expand" && len(call.Args) == 2 {
			allowed[ast.Unparen(call.Args[1])] = true
		}
		if g := s.funcs[fn]; g != nil {
			for i := range g.bound {
				if i < len(call.Args) {
					allowed[ast.Unparen(call.Args[i])] = true
				}
			}
		}
		return true
	})
	return calls, allowed
}

// judge matches the unresolved and environ reads against the exemptions
// and checks that each exemption still matches what it says: an unresolved
// or environ exemption exactly one read in each function it names, a
// placement exemption a read of each of its names in one of its functions.
func (res *envResult) judge(fset *token.FileSet, exemptions []envExemption) []string {
	var bad []string
	covered := map[string]int{} // exemption id + function -> reads matched
	match := func(r envRead, kind string) bool {
		for _, ex := range exemptions {
			for _, at := range ex.at {
				if ex.kind == kind && at == r.fn.key {
					covered[ex.id+" "+at]++
					return true
				}
			}
		}
		return false
	}
	for _, r := range res.unknown {
		if !match(r, "unresolved") {
			bad = append(bad, fmt.Sprintf("%s: %s in %s: the variable's name does not resolve: %s",
				envPos(fset, r.node), r.read, r.fn.key, r.name.unknown))
		}
	}
	for _, r := range res.environ {
		if !match(r, "environ") {
			bad = append(bad, fmt.Sprintf("%s: %s in %s reads the whole environment", envPos(fset, r.node), r.read, r.fn.key))
		}
	}
	named := map[string]bool{} // name + function
	for _, row := range res.rows {
		named[row.name+" "+row.at.fn.key] = true
	}
	for _, ex := range exemptions {
		switch ex.kind {
		case "unresolved", "environ":
			for _, at := range ex.at {
				if n := covered[ex.id+" "+at]; n != 1 {
					bad = append(bad, fmt.Sprintf("exemption %s (%s) matches %d reads in %s, not exactly one: a stale exemption goes, a second read needs its own reasoning",
						ex.id, ex.kind, n, at))
				}
			}
		case "placement":
			for _, name := range ex.names {
				found := false
				for _, at := range ex.at {
					found = found || named[name+" "+at]
				}
				if !found {
					bad = append(bad, fmt.Sprintf("exemption %s (placement) names %s, which none of %s reads: a stale entry goes",
						ex.id, name, strings.Join(ex.at, ", ")))
				}
			}
		default:
			bad = append(bad, fmt.Sprintf("exemption %s has the unknown kind %q", ex.id, ex.kind))
		}
	}
	return append(bad, res.bad...)
}

// envHoles lists env reads in production files the typed build leaves out
// (another GOOS, a build tag): S1's syntax trees hold every variant, so a
// reference to an env reader there, or a call of a getter of the same
// package by its name, is a read the inventory cannot see. Run the scan on
// that GOOS, or move the read to a file every build holds.
func envHoles(m *module, typed map[string]bool, getters []*envFunc) []string {
	names := map[string]map[string]bool{}
	for _, g := range getters {
		if names[g.pkg.name] == nil {
			names[g.pkg.name] = map[string]bool{}
		}
		names[g.pkg.name][g.decl.Name.Name] = true
	}
	var bad []string
	for _, f := range m.production() {
		if typed[f.rel] {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if envReaders[f.selectorPkg(x)+"."+x.Sel.Name] {
					bad = append(bad, fmt.Sprintf("%s: %s.%s in a file this build of the scan leaves out (%s)",
						m.pos(x), f.selectorPkg(x), x.Sel.Name, envBuildNote))
				}
			case *ast.CallExpr:
				if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && names[f.pkg.name][id.Name] {
					bad = append(bad, fmt.Sprintf("%s: a call of the getter %s in a file this build of the scan leaves out (%s)",
						m.pos(x), id.Name, envBuildNote))
				}
			}
			return true
		})
	}
	sort.Strings(bad)
	return bad
}

const envBuildNote = "move the read to a file every build holds, or run the scan with that GOOS or tag"

func envPos(fset *token.FileSet, n ast.Node) string {
	p := fset.Position(n.Pos())
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}
