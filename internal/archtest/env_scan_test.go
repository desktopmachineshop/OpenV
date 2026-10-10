package archtest

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/constant"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file is the env scan of the inventory (refactor plan S8): it finds
// every read of the process environment in the typed program and resolves
// the variable's name. A getter is found by data flow, not by its name: any
// function whose string parameter reaches an env read, directly or through
// another getter, and any function-typed parameter that a call binds to
// os.Getenv or os.LookupEnv. A name resolves when it is a constant or a
// constant expression, a string parameter the function never assigns to or
// takes the address of (the function is then a getter, and each call site
// resolves it from its argument), a concatenation of those, a local variable
// assigned exactly once and never address-taken, or the element of a range
// over a package-level table of string constants that nothing but range
// statements uses. Anything else is unresolved, which the inventory refuses
// unless an exemption covers it, and so is a call through an interface
// method that a getter implements, since the scan cannot tell which function
// the call reaches.

// envSinks are the standard functions that read the environment, and what
// each reads: getenv and lookup take the name as their first argument,
// expand reads the $NAMEs of a constant template, environ reads everything.
var envSinks = map[string]string{
	"os.Getenv": "getenv", "syscall.Getenv": "lookup", "os.LookupEnv": "lookup",
	"os.ExpandEnv": "expand", "os.Expand": "expand",
	"os.Environ": "environ", "syscall.Environ": "environ",
}

// envSnapshot is an env snapshot (refactor plan X10, internal/config): a
// loader whose function-typed parameter records every variable of a table,
// and the method that reads one recorded variable back by its name. While no
// call binds the loader's parameter to an env reader, the snapshot reads
// nothing. Once one does, a call of the reader is a read of the variable it
// names, made where the snapshot's accessors read it back, so each variable
// keeps the row a getter gives it, with the default its accessor passes; the
// loader's own reads of its table are no rows, since every variable it
// records is read back where it is used; and the value method, used as a
// value, is an env reader that reads as os.Getenv does (Config.getenv,
// handed to billing.ConfigFromEnv).
type envSnapshot struct {
	loader string // package:Func of the loader
	param  string // the loader's function-typed parameter that reads the environment
	reader string // package:Receiver.Method that reads one recorded variable by name
	value  string // package:Receiver.Method that, as a value, reads as os.Getenv does
}

// envSnapshots are the module's env snapshots: internal/config's, which
// cmd/server hands os.LookupEnv from refactor step X10b on.
var envSnapshots = []envSnapshot{
	{loader: "internal/config:Load", param: "lookup", reader: "internal/config:Config.lookup", value: "internal/config:Config.getenv"},
}

// envMaxPasses bounds the getter fixpoint; the module settles in a few.
const envMaxPasses = 12

// envPart is one piece of a name template: literal text, or the value of the
// enclosing function's parameter number param.
type envPart struct {
	lit   string
	param int // -1 for literal text
}

type envTmpl []envPart

// envName is what an expression can name: one or more templates, or unknown
// with the reason.
type envName struct {
	alts    []envTmpl
	unknown string
}

func envLit(s string) envName       { return envName{alts: []envTmpl{{{lit: s, param: -1}}}} }
func envParam(i int) envName        { return envName{alts: []envTmpl{{{param: i}}}} }
func envUnknown(why string) envName { return envName{unknown: why} }

// firstParam is the first parameter a template reads, or -1.
func (t envTmpl) firstParam() int {
	for _, p := range t {
		if p.param >= 0 {
			return p.param
		}
	}
	return -1
}

func (t envTmpl) String() string {
	var b strings.Builder
	for _, p := range t {
		if p.param >= 0 {
			fmt.Fprintf(&b, "{%d}", p.param)
		} else {
			b.WriteString(p.lit)
		}
	}
	return b.String()
}

// envConcat is every template of a followed by every template of b.
func envConcat(a, b envName) envName {
	if a.unknown != "" {
		return a
	}
	if b.unknown != "" {
		return b
	}
	var out envName
	for _, x := range a.alts {
		for _, y := range b.alts {
			out.alts = append(out.alts, append(append(envTmpl{}, x...), y...))
		}
	}
	return out
}

// envFunc is one function declaration with a body, and what the scan has
// learned about it.
type envFunc struct {
	key    string // package:Func or package:Receiver.Method
	obj    *types.Func
	decl   *ast.FuncDecl
	pkg    *envPkg
	params []*types.Var
	// gets are the reads the function makes through its string parameters,
	// keyed by template: it is a getter when it has any.
	gets map[string]envGet
	// bound are its function-typed parameters that some call binds to an env
	// reader, and that reader (os.Getenv or os.LookupEnv).
	bound map[int]string
	// shapes are the calls whose value is only compared with a string
	// constant, and the comparison (` =="true"`).
	shapes map[*ast.CallExpr]string
	// called is set when the walk follows a call of the function. A getter
	// no followed call reaches is called through an interface or a value,
	// or not at all, so the names it reads are nowhere in the inventory.
	called bool
}

// envGet is one read a getter makes through its parameters.
type envGet struct {
	tmpl envTmpl
	sink string
}

// envRead is one read of the environment the walk found.
type envRead struct {
	fn      *envFunc
	node    ast.Node
	read    string // the read column: os.Getenv, or package:getter(param), and the shape
	sink    string // the standard function underneath
	name    envName
	def     ast.Expr // the constant or expression paired with the name as its default
	environ bool     // reads the whole environment
}

// envScan is the scan of one typed program.
type envScan struct {
	prog  *envProgram
	funcs map[*types.Func]*envFunc
	order []*envFunc // sorted by key
	// methods are the getter methods by name, once settle has run and the
	// getters are final; ifaces caches, per interface method, the keys of
	// those that implement it (see ifaceGetters).
	methods map[string][]*envFunc
	ifaces  map[*types.Func][]string
	// byKey finds a function by its key, for envSnapshots.
	byKey map[string]*envFunc
}

func newEnvScan(prog *envProgram) *envScan {
	s := &envScan{prog: prog, funcs: map[*types.Func]*envFunc{}, ifaces: map[*types.Func][]string{}, byKey: map[string]*envFunc{}}
	for _, p := range prog.pkgs {
		for _, f := range p.files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				obj, ok := p.info.Defs[fd.Name].(*types.Func)
				if !ok {
					continue
				}
				sig := obj.Type().(*types.Signature)
				ef := &envFunc{key: p.name + ":" + funcName(fd), obj: obj, decl: fd, pkg: p,
					gets: map[string]envGet{}, bound: map[int]string{}, shapes: envShapes(p.info, fd.Body)}
				for i := 0; i < sig.Params().Len(); i++ {
					ef.params = append(ef.params, sig.Params().At(i))
				}
				s.funcs[obj] = ef
				s.byKey[ef.key] = ef
				s.order = append(s.order, ef)
			}
		}
	}
	sort.SliceStable(s.order, func(i, j int) bool { return s.order[i].key < s.order[j].key })
	return s
}

// envShapes finds the calls that are an operand of == or != whose other
// operand is a string constant.
func envShapes(info *types.Info, body *ast.BlockStmt) map[*ast.CallExpr]string {
	out := map[*ast.CallExpr]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
			return true
		}
		for _, pair := range [][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
			call, ok := ast.Unparen(pair[0]).(*ast.CallExpr)
			if tv := info.Types[pair[1]]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
				out[call] = " " + be.Op.String() + strconv.Quote(constant.StringVal(tv.Value))
			}
		}
		return true
	})
	return out
}

// settle runs the getter fixpoint: it walks every function until no getter
// gains a read and no function-typed parameter gains a binding.
func (s *envScan) settle() error {
	for pass := 0; pass < envMaxPasses; pass++ {
		changed := false
		for _, f := range s.order {
			reads, bound := s.walk(f)
			changed = changed || bound
			for _, r := range reads {
				if r.name.unknown != "" {
					continue
				}
				for _, t := range r.name.alts {
					if _, ok := f.gets[t.String()]; t.firstParam() >= 0 && !ok {
						f.gets[t.String()] = envGet{tmpl: t, sink: r.sink}
						changed = true
					}
				}
			}
		}
		if !changed {
			s.methods = map[string][]*envFunc{}
			for _, f := range s.order {
				if len(f.gets) > 0 && f.obj.Type().(*types.Signature).Recv() != nil {
					s.methods[f.obj.Name()] = append(s.methods[f.obj.Name()], f)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("the getter summaries did not settle in %d passes: a getter calls itself with a growing name", envMaxPasses)
}

// snapshotForm is the env reader a call binds snap's loader parameter to,
// or "" while none does.
func (s *envScan) snapshotForm(snap envSnapshot) string {
	l := s.byKey[snap.loader]
	if l == nil {
		return ""
	}
	for i, p := range l.params {
		if p.Name() == snap.param {
			return l.bound[i]
		}
	}
	return ""
}

// snapshotOf is the snapshot in which the function g plays the role that
// role names (its reader or its value method), and whether there is one.
func snapshotOf(g *envFunc, role func(envSnapshot) string) (envSnapshot, bool) {
	for _, snap := range envSnapshots {
		if g != nil && role(snap) == g.key {
			return snap, true
		}
	}
	return envSnapshot{}, false
}

// unusedAccessors are the functions of each live env snapshot's package
// that no function outside the package refers to, directly or through the
// package's other functions: an accessor nothing uses, whose reads reach no
// one, gives no row. A reference is a call or a function value (an accessor
// handed on as a func, as cmd/server hands hosting the pids limit's).
func (s *envScan) unusedAccessors() map[*envFunc]bool {
	unused := map[*envFunc]bool{}
	for _, snap := range envSnapshots {
		if s.snapshotForm(snap) == "" {
			continue
		}
		pkg, _, _ := strings.Cut(snap.loader, ":")
		refs := map[*envFunc][]*envFunc{}
		used, todo := map[*envFunc]bool{}, []*envFunc(nil)
		for _, f := range s.order {
			ast.Inspect(f.decl.Body, func(n ast.Node) bool {
				var g *envFunc
				switch x := n.(type) {
				case *ast.Ident:
					if fn, ok := s.objectOf(f.pkg, x).(*types.Func); ok {
						g = s.funcs[fn]
					}
				case *ast.SelectorExpr:
					if fn, ok := s.objectOf(f.pkg, x).(*types.Func); ok {
						g = s.funcs[fn]
					}
				}
				if g == nil || g.pkg.name != pkg {
					return true
				}
				if f.pkg.name == pkg {
					refs[f] = append(refs[f], g)
				} else if !used[g] {
					used[g] = true
					todo = append(todo, g)
				}
				return true
			})
		}
		for len(todo) > 0 {
			g := todo[len(todo)-1]
			todo = todo[:len(todo)-1]
			for _, h := range refs[g] {
				if !used[h] {
					used[h] = true
					todo = append(todo, h)
				}
			}
		}
		for _, f := range s.order {
			if f.pkg.name == pkg && !used[f] {
				unused[f] = true
			}
		}
	}
	return unused
}

// isSnapshotLoad reports whether v is the parameter of f through which an
// env snapshot's loader records its table.
func isSnapshotLoad(f *envFunc, v *types.Var) bool {
	for _, snap := range envSnapshots {
		if snap.loader == f.key && v.Name() == snap.param {
			return true
		}
	}
	return false
}

// walk returns the reads in f's body, closures included, given what is
// known so far, and reports whether a call bound a new function-typed
// parameter to an env reader.
func (s *envScan) walk(f *envFunc) (reads []envRead, bound bool) {
	ast.Inspect(f.decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		shape := f.shapes[call]
		switch obj := s.callee(f.pkg, call).(type) {
		case *types.Func:
			if sink := envSinks[qualified(obj)]; sink != "" {
				if r, ok := s.sinkRead(f, call, qualified(obj), sink); ok {
					r.read += shape
					reads = append(reads, r)
				}
				return true
			}
			if snap, ok := snapshotOf(s.funcs[obj], func(x envSnapshot) string { return x.reader }); ok {
				g := s.funcs[obj]
				g.called = true
				if form := s.snapshotForm(snap); form != "" && len(call.Args) > 0 && len(g.params) > 0 {
					reads = append(reads, envRead{fn: f, node: call, read: g.key + "(" + g.params[0].Name() + ")" + shape,
						sink: form, name: s.eval(f, call.Args[0], 0)})
				}
				return true
			}
			if g := s.funcs[obj]; g != nil {
				g.called = true
				bound = s.bind(f, g, call) || bound
				reads = append(reads, s.getterReads(f, g, call, shape)...)
			}
			if impl := s.ifaceGetters(obj); len(impl) > 0 {
				who := "the getter " + impl[0] + " implements"
				if len(impl) > 1 {
					who = "the getters " + strings.Join(impl, ", ") + " implement"
				}
				reads = append(reads, envRead{fn: f, node: call, read: envIfaceMethod(obj), sink: "interface",
					name: envUnknown("the call goes through an interface method that " + who +
						", so the scan cannot tell which function reads the name: call the getter directly")})
			}
		case *types.Var:
			if i := paramIndex(f.params, obj); i >= 0 && f.bound[i] != "" && len(call.Args) > 0 && !isSnapshotLoad(f, obj) {
				reads = append(reads, envRead{fn: f, node: call, read: f.key + "(" + obj.Name() + ")" + shape,
					sink: f.bound[i], name: s.eval(f, call.Args[0], 0)})
			}
		}
		return true
	})
	return reads, bound
}

// sinkRead is the read a call of a standard env function makes.
func (s *envScan) sinkRead(f *envFunc, call *ast.CallExpr, fn, sink string) (envRead, bool) {
	r := envRead{fn: f, node: call, read: fn, sink: fn}
	switch {
	case sink == "environ":
		r.environ = true
	case len(call.Args) == 0:
		return r, false
	case sink == "expand" && fn == "os.Expand" && (len(call.Args) < 2 || s.readerForm(f, call.Args[1]) == ""):
		return r, false // os.Expand with a mapping of its own reads nothing
	case sink == "expand":
		r.name = envExpand(s.eval(f, call.Args[0], 0))
	default:
		r.name = s.eval(f, call.Args[0], 0)
	}
	return r, true
}

// envExpand is the $NAMEs of constant templates.
func envExpand(n envName) envName {
	if n.unknown != "" {
		return n
	}
	var out envName
	for _, t := range n.alts {
		if t.firstParam() >= 0 {
			return envUnknown("the template to expand depends on a parameter")
		}
		os.Expand(t.String(), func(k string) string {
			out.alts = append(out.alts, envTmpl{{lit: k, param: -1}})
			return ""
		})
	}
	if len(out.alts) == 0 {
		return envUnknown("the template names no variable")
	}
	return out
}

// getterReads are the reads a call of the getter g makes, its arguments
// substituted for g's parameters. The i-th name parameter of g pairs with
// the i-th of its other parameters, when there are as many of each, and the
// argument there is the read's default.
func (s *envScan) getterReads(f, g *envFunc, call *ast.CallExpr, shape string) []envRead {
	if len(g.gets) == 0 {
		return nil
	}
	keys := make([]string, 0, len(g.gets))
	names := map[int]bool{}
	for k, get := range g.gets {
		keys = append(keys, k)
		for _, p := range get.tmpl {
			if p.param >= 0 {
				names[p.param] = true
			}
		}
	}
	sort.Strings(keys)
	var nameIdx, otherIdx []int
	for i := range g.params {
		if names[i] {
			nameIdx = append(nameIdx, i)
		} else {
			otherIdx = append(otherIdx, i)
		}
	}
	variadic := g.obj.Type().(*types.Signature).Variadic()
	var out []envRead
	for _, k := range keys {
		get := g.gets[k]
		first := get.tmpl.firstParam()
		r := envRead{fn: f, node: call, read: g.key + "(" + g.params[first].Name() + ")" + shape, sink: get.sink}
		r.name = s.substitute(f, get.tmpl, call.Args, variadic, len(g.params))
		for i, p := range nameIdx {
			if p == first && len(nameIdx) == len(otherIdx) && otherIdx[i] < len(call.Args) &&
				!(variadic && otherIdx[i] == len(g.params)-1) {
				r.def = call.Args[otherIdx[i]]
			}
		}
		out = append(out, r)
	}
	return out
}

// substitute resolves a getter's template with a call's arguments, in the
// caller f.
func (s *envScan) substitute(f *envFunc, t envTmpl, args []ast.Expr, variadic bool, nparams int) envName {
	out := envName{alts: []envTmpl{{}}}
	for _, p := range t {
		if p.param < 0 {
			out = envConcat(out, envName{alts: []envTmpl{{p}}})
			continue
		}
		if p.param >= len(args) || (variadic && p.param == nparams-1) {
			return envUnknown("the name is a variadic argument")
		}
		out = envConcat(out, s.eval(f, args[p.param], 0))
		if out.unknown != "" {
			return out
		}
	}
	return out
}

// bind marks g's function-typed parameters that this call passes an env
// reader to, and reports whether one was new.
func (s *envScan) bind(f, g *envFunc, call *ast.CallExpr) bool {
	changed := false
	for i, arg := range call.Args {
		if i >= len(g.params) {
			break
		}
		if _, isFunc := g.params[i].Type().Underlying().(*types.Signature); !isFunc {
			continue
		}
		if form := s.readerForm(f, arg); form != "" && g.bound[i] == "" {
			g.bound[i] = form
			changed = true
		}
	}
	return changed
}

// readerForm reports whether e, in f, is an env reader used as a value
// (os.Getenv, os.LookupEnv, syscall.Getenv, or a parameter of f bound to
// one), and which.
func (s *envScan) readerForm(f *envFunc, e ast.Expr) string {
	switch obj := s.objectOf(f.pkg, e).(type) {
	case *types.Func:
		if sink := envSinks[qualified(obj)]; sink == "getenv" || sink == "lookup" {
			return qualified(obj)
		}
		if snap, ok := snapshotOf(s.funcs[obj], func(x envSnapshot) string { return x.value }); ok && s.snapshotForm(snap) != "" {
			return "os.Getenv"
		}
	case *types.Var:
		if i := paramIndex(f.params, obj); i >= 0 {
			return f.bound[i]
		}
	}
	return ""
}

// callee is the object a call calls: a function or method (its generic
// origin), a variable of function type, or nil.
func (s *envScan) callee(p *envPkg, call *ast.CallExpr) types.Object {
	fun := ast.Unparen(call.Fun)
	switch x := fun.(type) {
	case *ast.IndexExpr:
		fun = x.X
	case *ast.IndexListExpr:
		fun = x.X
	}
	return s.objectOf(p, fun)
}

// objectOf resolves an identifier or selector to its object, a function as
// its generic origin.
func (s *envScan) objectOf(p *envPkg, e ast.Expr) types.Object {
	var obj types.Object
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj = p.info.Uses[x]
	case *ast.SelectorExpr:
		if sel, ok := p.info.Selections[x]; ok {
			obj = sel.Obj()
		} else {
			obj = p.info.Uses[x.Sel]
		}
	}
	if fn, ok := obj.(*types.Func); ok {
		return fn.Origin()
	}
	return obj
}

// ifaceGetters lists the getter methods (by key) that implement fn when fn
// is an interface method: a method of the same name with a string-parameter
// read, whose receiver type or a pointer to it implements the interface. A
// getter method of a generic type counts whenever its name matches, since
// types.Implements does not judge an uninstantiated type. It answers once
// settle has made the getters final, and nothing before: the read it leads
// to has no name, which settle does not use.
func (s *envScan) ifaceGetters(fn *types.Func) []string {
	sig := fn.Type().(*types.Signature)
	if s.methods == nil || len(s.methods[fn.Name()]) == 0 || sig.Recv() == nil || !types.IsInterface(sig.Recv().Type()) {
		return nil
	}
	if impl, ok := s.ifaces[fn]; ok {
		return impl
	}
	iface, _ := sig.Recv().Type().Underlying().(*types.Interface)
	var impl []string
	for _, g := range s.methods[fn.Name()] {
		recv := g.obj.Type().(*types.Signature).Recv().Type()
		if p, ok := recv.(*types.Pointer); ok {
			recv = p.Elem()
		}
		named, _ := recv.(*types.Named)
		switch {
		case named != nil && named.TypeParams().Len() > 0, iface == nil,
			types.Implements(recv, iface), types.Implements(types.NewPointer(recv), iface):
			impl = append(impl, g.key)
		}
	}
	s.ifaces[fn] = impl
	return impl
}

// envIfaceMethod names an interface method by its package's name, its
// interface and itself ("hosting.envSource.get").
func envIfaceMethod(fn *types.Func) string {
	recv := types.TypeString(fn.Type().(*types.Signature).Recv().Type(), func(p *types.Package) string { return p.Name() })
	return recv + "." + fn.Name()
}

// qualified names a package-level function path.Name ("os.Getenv"), or "".
func qualified(fn *types.Func) string {
	if fn.Pkg() == nil || fn.Type().(*types.Signature).Recv() != nil {
		return ""
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

func paramIndex(params []*types.Var, v *types.Var) int {
	for i, p := range params {
		if p == v {
			return i
		}
	}
	return -1
}

// eval resolves a name expression in f.
func (s *envScan) eval(f *envFunc, e ast.Expr, depth int) envName {
	if depth > 16 {
		return envUnknown("the name passes through more than 16 expressions")
	}
	e = ast.Unparen(e)
	info := f.pkg.info
	if tv, ok := info.Types[e]; ok && tv.Value != nil {
		if tv.Value.Kind() == constant.String {
			return envLit(constant.StringVal(tv.Value))
		}
		return envUnknown("a constant that is not a string")
	}
	switch x := e.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return envConcat(s.eval(f, x.X, depth+1), s.eval(f, x.Y, depth+1))
		}
	case *ast.Ident:
		v, ok := info.Uses[x].(*types.Var)
		if !ok {
			break
		}
		if i := paramIndex(f.params, v); i >= 0 {
			b, ok := v.Type().Underlying().(*types.Basic)
			if !ok || b.Info()&types.IsString == 0 {
				return envUnknown("the parameter " + v.Name() + " is not a string")
			}
			// A getter that rewrites its parameter (an alias table, a
			// default) reads another name than its callers pass.
			switch w := envWritesOf(info, f.decl, v); {
			case w.addressed:
				return envUnknown("the parameter " + v.Name() + " has its address taken")
			case w.assigns > 0 || w.rng != nil || w.keyed:
				return envUnknown("the parameter " + v.Name() + " is reassigned")
			}
			return envParam(i)
		}
		if v.Parent() == f.pkg.types.Scope() {
			return envUnknown("the package variable " + v.Name())
		}
		return s.evalLocal(f, v, depth)
	}
	return envUnknown("the expression " + envSource(s.prog.fset, e))
}

// evalLocal follows a local variable to its one assignment, or to the range
// over a table it is the element of.
func (s *envScan) evalLocal(f *envFunc, v *types.Var, depth int) envName {
	w := envWritesOf(f.pkg.info, f.decl, v)
	switch {
	case w.param: // f's own parameters stop in eval
		return envUnknown("the parameter " + v.Name() + " of a function literal, whose calls the scan does not follow")
	case w.addressed:
		return envUnknown("the local " + v.Name() + " has its address taken")
	case w.keyed:
		return envUnknown("the local " + v.Name() + " is the key of a range")
	case w.rng != nil && w.assigns == 0:
		return s.evalRange(f, w.rng, v)
	case w.assigns != 1 || w.def == nil:
		return envUnknown(fmt.Sprintf("the local %s is assigned %d times", v.Name(), w.assigns))
	}
	return s.eval(f, w.def, depth+1)
}

// envWrites is what a function declaration, closures included, does to a
// variable: how often it assigns it (a declaration without a value and a
// named result count, as the zero value) and the value of its one
// assignment, the range it is the element or the key of, whether its
// address is taken, and whether it is a parameter.
type envWrites struct {
	assigns   int
	def       ast.Expr
	rng       *ast.RangeStmt
	keyed     bool
	addressed bool
	param     bool
}

func envWritesOf(info *types.Info, fd *ast.FuncDecl, v *types.Var) envWrites {
	var w envWrites
	is := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && (info.Defs[id] == v || info.Uses[id] == v)
	}
	ast.Inspect(fd, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if is(lhs) {
					w.assigns++
					if len(x.Lhs) == len(x.Rhs) && (x.Tok == token.DEFINE || x.Tok == token.ASSIGN) {
						w.def = x.Rhs[i]
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if info.Defs[id] == v {
					w.assigns++ // a declaration without a value assigns the zero value
					if i < len(x.Values) {
						w.def = x.Values[i]
					}
				}
			}
		case *ast.RangeStmt:
			if x.Value != nil && is(x.Value) {
				w.rng = x
			}
			if x.Key != nil && is(x.Key) {
				w.keyed = true
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && is(ast.Unparen(x.X)) {
				w.addressed = true
			}
		case *ast.FuncType:
			for _, field := range x.Params.List {
				for _, id := range field.Names {
					w.param = w.param || info.Defs[id] == v
				}
			}
			if x.Results != nil {
				for _, field := range x.Results.List {
					for _, id := range field.Names {
						if info.Defs[id] == v {
							w.assigns++ // a named result starts as the zero value
						}
					}
				}
			}
		}
		return true
	})
	return w
}

// evalRange resolves the element of a range over a package-level table: a
// variable initialised with a composite literal of string constants that
// the package uses in range statements only, so no code can change it.
func (s *envScan) evalRange(f *envFunc, rng *ast.RangeStmt, v *types.Var) envName {
	id, ok := ast.Unparen(rng.X).(*ast.Ident)
	tbl, isVar := f.pkg.info.Uses[id].(*types.Var)
	if !ok || !isVar || tbl.Parent() != f.pkg.types.Scope() {
		return envUnknown("the range variable " + v.Name() + " over " + envSource(s.prog.fset, rng.X))
	}
	if !envRangeOnly(f.pkg)[tbl] {
		return envUnknown("the range variable " + v.Name() + " over " + tbl.Name() + ", which is used other than by range statements")
	}
	lit := envTableLiteral(f.pkg, tbl)
	if lit == nil {
		return envUnknown("the range variable " + v.Name() + " over " + tbl.Name() + ", which is not a composite literal")
	}
	var out envName
	seen := map[string]bool{}
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			elt = kv.Value
		}
		tv := f.pkg.info.Types[elt]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return envUnknown("the range variable " + v.Name() + " over " + tbl.Name() + ", which holds " + envSource(s.prog.fset, elt))
		}
		if name := constant.StringVal(tv.Value); !seen[name] {
			seen[name] = true
			out.alts = append(out.alts, envTmpl{{lit: name, param: -1}})
		}
	}
	if len(out.alts) == 0 {
		return envUnknown("the range variable " + v.Name() + " over the empty " + tbl.Name())
	}
	return out
}

// envRangeOnly is the set of p's package-level variables whose every use is
// the operand of a range statement.
func envRangeOnly(p *envPkg) map[*types.Var]bool {
	if p.rangeOnly != nil {
		return p.rangeOnly
	}
	uses, ranged := map[*types.Var]int{}, map[*types.Var]int{}
	for _, obj := range p.info.Uses {
		if v, ok := obj.(*types.Var); ok && v.Parent() == p.types.Scope() {
			uses[v]++
		}
	}
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			if rs, ok := n.(*ast.RangeStmt); ok {
				if id, ok := ast.Unparen(rs.X).(*ast.Ident); ok {
					if v, ok := p.info.Uses[id].(*types.Var); ok {
						ranged[v]++
					}
				}
			}
			return true
		})
	}
	p.rangeOnly = map[*types.Var]bool{}
	for v, n := range uses {
		if n == ranged[v] {
			p.rangeOnly[v] = true
		}
	}
	return p.rangeOnly
}

// envTableLiteral is the composite literal a package-level variable is
// declared with, or nil.
func envTableLiteral(p *envPkg, v *types.Var) *ast.CompositeLit {
	for _, f := range p.files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if p.info.Defs[id] == v && i < len(vs.Values) {
						lit, _ := ast.Unparen(vs.Values[i]).(*ast.CompositeLit)
						return lit
					}
				}
			}
		}
	}
	return nil
}

// envDefault renders a read's default: a constant by its type (strings
// quoted, durations as time.Duration prints them, floats in the shortest
// form), (computed) for anything else, - when there is none.
func envDefault(p *envPkg, e ast.Expr) string {
	if e == nil {
		return "-"
	}
	tv := p.info.Types[e]
	if tv.Value == nil {
		return "(computed)"
	}
	if n, ok := tv.Type.(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Duration" {
		if i, exact := constant.Int64Val(tv.Value); exact {
			return time.Duration(i).String()
		}
	}
	switch tv.Value.Kind() {
	case constant.String:
		return strconv.Quote(constant.StringVal(tv.Value))
	case constant.Bool:
		return tv.Value.String()
	}
	if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Info()&types.IsFloat != 0 {
		fv, _ := constant.Float64Val(tv.Value)
		return strconv.FormatFloat(fv, 'g', -1, 64)
	}
	return tv.Value.ExactString()
}

// envSource prints an expression as source.
func envSource(fset *token.FileSet, n ast.Node) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "?"
	}
	return b.String()
}
