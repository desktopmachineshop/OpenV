// This file is shared, byte for byte, by declhash, declmove and movecheck,
// like decls.go, and for the same reason. declhash's
// TestSharedFileIsIdentical fails when the copies differ: edit
// internal/tools/declhash/effects.go, then copy it over the other two.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"sort"
	"strings"
)

// effects returns the manifest entries, per file set, for what a package
// does at initialisation that no single declaration's hash shows, and that
// a pure move leaves alone:
//
//   - "(blank imports)": the packages imported only for their side effects,
//     each with the importing file's build constraints. A blank import of
//     embed is left out: it only enables //go:embed, and declmove adds one
//     where it moves an embedded variable.
//   - "(init order)": the code package initialisation runs, in the order it
//     runs it. Go initialises package-level variables in dependency order
//     and otherwise in declaration order (file name, then source order),
//     then runs the init functions in that order. The entry lists every
//     variable whose initialiser calls something not known to be pure,
//     with every variable those initialisers may read, directly or through
//     the functions and methods they may call, in declaration order; then
//     the init functions. The order among those variables settles the order
//     their side effects run in, so a move that changes it changes the
//     entry, while moving `var errX = errors.New(...)` does not.
//
// The test set is initialised after the production files, which never
// depend on it, so each set's order is its own.
func (p *pkgInfo) effects(label string) []string {
	var out []string
	for _, set := range []string{"", "test", "xtest"} {
		entry := func(key string, parts []string) {
			sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
			out = append(out, setLabel(label, set)+"\t"+key+"\t"+hex.EncodeToString(sum[:]))
		}
		if b := p.blankImports(set); len(b) > 0 {
			entry("(blank imports)", b)
		}
		if o := p.initOrder(set); len(o) > 1 {
			entry("(init order)", o)
		}
	}
	return out
}

func (p *pkgInfo) blankImports(set string) []string {
	seen := map[string]bool{}
	for _, f := range p.files {
		for _, ip := range f.blanks {
			if f.set == set && ip != "embed" {
				seen[f.build+"|"+ip] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// initOrder lists what the set's initialisation runs, as effects describes.
func (p *pkgInfo) initOrder(set string) []string {
	types := map[string]bool{}
	named := map[string][]*decl{} // vars and funcs by name, methods by ".name"
	for _, d := range p.decls {
		if d.file.set != set && (set != "test" || d.file.set != "") {
			continue // the test set also calls production code
		}
		switch d.kind {
		case "type":
			types[d.key] = true
		case "var", "func":
			named[d.key] = append(named[d.key], d)
		case "method":
			m := "." + d.node.(*ast.FuncDecl).Name.Name
			named[m] = append(named[m], d)
		}
	}
	// Every variable an impure initialiser may read, through any call.
	ordered, seen := map[*decl]bool{}, map[*decl]bool{}
	var work []*decl
	for _, d := range p.decls {
		if d.kind == "var" && d.file.set == set && impure(types, d.file, d.exprs) {
			ordered[d], seen[d] = true, true
			work = append(work, d)
		}
	}
	for len(work) > 0 {
		d := work[len(work)-1]
		work = work[:len(work)-1]
		nodes := d.exprs
		if d.kind != "var" {
			nodes = []ast.Node{d.node.(*ast.FuncDecl).Body}
		}
		for name := range refsOf(d.file, nodes) {
			for _, t := range named[name] {
				if !seen[t] {
					seen[t] = true
					ordered[t] = ordered[t] || (t.kind == "var" && t.file.set == set)
					work = append(work, t)
				}
			}
		}
	}
	var out, inits []string
	for _, d := range p.decls {
		switch {
		case d.file.set != set:
		case ordered[d]:
			out = append(out, "var "+d.key+" "+d.hash)
		case d.kind == "func" && d.key == "init":
			inits = append(inits, "init "+d.hash)
		}
	}
	return append(out, inits...)
}

// refsOf lists the package-level names the nodes may refer to: each
// identifier that is not declared inside them or in a local scope, and
// ".m" for each selector m, which may be a method value.
func refsOf(f *goFile, nodes []ast.Node) map[string]bool {
	out := map[string]bool{}
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Obj == nil {
				if _, pkg := f.imports[id.Name]; pkg {
					return false
				}
			}
			out["."+x.Sel.Name] = true
			ast.Inspect(x.X, visit)
			return false
		case *ast.Ident:
			if x.Obj == nil || x.Obj == f.ast.Scope.Lookup(x.Name) {
				out[x.Name] = true
			}
		}
		return true
	}
	for _, n := range nodes {
		if n != nil {
			ast.Inspect(n, visit)
		}
	}
	return out
}

// impure reports whether the nodes call anything outside function literals
// that is not known to be pure: a conversion, a pure builtin, a function of
// pureFuncs or purePackages, or a method on the result of one of those.
// Like S1's ban on side-effecting package variables, which it follows, it
// errs towards impure: any other call may have a side effect.
func impure(types map[string]bool, f *goFile, nodes []ast.Node) bool {
	found := false
	for _, n := range nodes {
		if n == nil {
			continue
		}
		ast.Inspect(n, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.FuncLit:
				return false
			case *ast.CallExpr:
				found = found || !pureCallee(types, f, x.Fun)
			}
			return !found
		})
	}
	return found
}

func pureCallee(types map[string]bool, f *goFile, fun ast.Expr) bool {
	switch x := fun.(type) {
	case *ast.ParenExpr:
		return pureCallee(types, f, x.X)
	case *ast.IndexExpr:
		return pureCallee(types, f, x.X)
	case *ast.IndexListExpr:
		return pureCallee(types, f, x.X)
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.StructType:
		return true // a conversion
	case *ast.Ident:
		return types[x.Name] || pureBuiltins[x.Name]
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && id.Obj == nil {
			if ip, ok := f.imports[id.Name]; ok {
				return pureFuncs[ip+"."+x.Sel.Name] || purePackages[ip]
			}
		}
		if call, ok := x.X.(*ast.CallExpr); ok {
			return pureCallee(types, f, call.Fun) // a method on a value built by a pure call
		}
	}
	return false
}

// pureFuncs, purePackages and pureBuiltins are S1's lists
// (internal/archtest/bans_test.go), without the module's own types, which
// a tool that reads one package at a time cannot see.
var pureFuncs = map[string]bool{
	"errors.New": true, "errors.Join": true,
	"fmt.Errorf": true, "fmt.Sprint": true, "fmt.Sprintf": true, "fmt.Sprintln": true,
	"regexp.MustCompile": true, "regexp.MustCompilePOSIX": true,
	"text/template.Must": true, "text/template.New": true, "html/template.Must": true, "html/template.New": true,
	"sync.OnceFunc": true, "sync.OnceValue": true, "sync.OnceValues": true,
	"time.Date": true, "time.Duration": true, "time.FixedZone": true, "time.Month": true, "time.Unix": true, "time.Weekday": true,
	"reflect.TypeFor": true, "reflect.TypeOf": true,
	"encoding/json.RawMessage": true,
	"net/http.HandlerFunc":     true, "net/http.StatusText": true,
	"math/big.NewFloat": true, "math/big.NewInt": true, "math/big.NewRat": true,
}

var purePackages = map[string]bool{
	"bytes": true, "maps": true, "math": true, "math/bits": true, "path": true, "slices": true, "sort": true,
	"strconv": true, "strings": true, "unicode": true, "unicode/utf16": true, "unicode/utf8": true,
}

var pureBuiltins = map[string]bool{
	"append": true, "cap": true, "complex": true, "imag": true, "len": true, "make": true, "max": true, "min": true,
	"new": true, "real": true,
	"any": true, "bool": true, "byte": true, "complex64": true, "complex128": true, "error": true, "float32": true,
	"float64": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true, "rune": true, "string": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
}
