package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decodeAliasTypes are the types whose JSON decode errors must not reach a
// response (R8). encoding/json names the target type in its errors, package
// qualified ("Go value of type exports.ProjectExport"), so a type that moves
// to a new package behind an alias changes that text. Entries are
// "<package>.<Type>" with the module-relative package. P1 adds
// internal/domain/exports.ProjectExport and P3 agentruns.FinishRequest, each
// in a class T commit before its move. The list only grows.
var decodeAliasTypes = []string{}

// decodeErrorSources maps the name of a function or method whose returned
// error can carry the decode error of an alias-list type to that type, for
// decodes the rule cannot see: those in a helper or outside internal/api.
// An error assigned from a call by that name counts as the decode's error.
// Today's sites return an exports.ProjectExport decode error:
// Handler.projectExport (suite_handlers.go), and the export service's
// ImportProject and ImportProjectWithOverrides (handlers.go). P1 adds those
// three names with the ProjectExport entry, and X14 adds snapshot's Load.
var decodeErrorSources = map[string]string{}

func checkDecodeAliases(c *check) {
	leaks := decodeLeaks(c.m, decodeAliasTypes, decodeErrorSources)
	c.baseline("%d types on the alias list; %d handlers write their decode errors", len(decodeAliasTypes), len(leaks))
	for _, l := range leaks {
		c.violation("%s", l)
	}
}

// decodeLeaks finds, in internal/api, a JSON decode into a type that is or
// contains an alias-list type whose error then reaches the response: a call
// in the error branch that takes the http.ResponseWriter and formats the
// error (err.Error(), or err passed to fmt.Sprint*/Errorf). Passing err
// itself to a writer that logs it, such as respondError, is fine. The
// analysis is syntactic: it follows local variables, struct fields and the
// module's named types, not values passed through helper functions, except
// the error returned by a call named in sources (function or method name ->
// alias type).
func decodeLeaks(m *module, aliases []string, sources map[string]string) []string {
	if len(aliases) == 0 {
		return nil
	}
	var out []string
	for _, f := range apiFiles(m) {
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				s := newDecodeScope(m, f, fd, setOf(aliases), sources)
				out = append(out, s.leaks...)
			}
		}
	}
	return out
}

// texpr is a type expression and the file it is written in, which decides
// what its qualified names refer to.
type texpr struct {
	f *file
	e ast.Expr
}

type decodeScope struct {
	m        *module
	f        *file
	fn       *ast.FuncDecl
	targets  map[string]bool
	sources  map[string]string     // callee name -> alias type its error carries
	vars     map[string][]ast.Expr // local variable -> declared or literal types
	writers  map[string]bool       // http.ResponseWriter parameters
	decoders map[string]bool       // variables holding json.NewDecoder(...)
	leaks    []string
}

func newDecodeScope(m *module, f *file, fd *ast.FuncDecl, targets map[string]bool, sources map[string]string) *decodeScope {
	s := &decodeScope{m: m, f: f, fn: fd, targets: targets, sources: sources, vars: map[string][]ast.Expr{},
		writers: map[string]bool{}, decoders: map[string]bool{}}
	s.params(fd.Type)
	ast.Inspect(fd.Body, s.collect)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BlockStmt:
			s.scan(x.List)
		case *ast.CaseClause:
			s.scan(x.Body)
		case *ast.CommClause:
			s.scan(x.Body)
		}
		return true
	})
	return s
}

func (s *decodeScope) params(ft *ast.FuncType) {
	for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
		if fl == nil {
			continue
		}
		for _, field := range fl.List {
			sel, isSel := field.Type.(*ast.SelectorExpr)
			writer := isSel && sel.Sel.Name == "ResponseWriter" && s.f.selectorPkg(sel) == "net/http"
			for _, id := range field.Names {
				s.vars[id.Name] = append(s.vars[id.Name], field.Type)
				s.writers[id.Name] = s.writers[id.Name] || writer
			}
		}
	}
}

// collect records local variable types, decoder variables and closure
// parameters. Names are not scoped: a name declared twice keeps both types.
func (s *decodeScope) collect(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.FuncLit:
		s.params(x.Type)
	case *ast.ValueSpec:
		for i, id := range x.Names {
			if x.Type != nil {
				s.vars[id.Name] = append(s.vars[id.Name], x.Type)
			} else if i < len(x.Values) {
				s.assign(id.Name, x.Values[i])
			}
		}
	case *ast.AssignStmt:
		if len(x.Lhs) == len(x.Rhs) {
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					s.assign(id.Name, x.Rhs[i])
				}
			}
		}
	}
	return true
}

func (s *decodeScope) assign(name string, v ast.Expr) {
	v = ast.Unparen(v)
	if u, ok := v.(*ast.UnaryExpr); ok && u.Op == token.AND {
		v = ast.Unparen(u.X)
	}
	switch x := v.(type) {
	case *ast.CompositeLit:
		if x.Type != nil {
			s.vars[name] = append(s.vars[name], x.Type)
		}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
			s.vars[name] = append(s.vars[name], x.Args[0])
		}
		if s.f.isPkgFunc(x, "encoding/json", "NewDecoder") {
			s.decoders[name] = true
		}
	}
}

// scan looks through one statement list for an error assigned from a decode
// into an alias-list type, and checks the if statements that test it.
func (s *decodeScope) scan(list []ast.Stmt) {
	for i, st := range list {
		switch x := st.(type) {
		case *ast.IfStmt:
			if as, ok := x.Init.(*ast.AssignStmt); ok {
				if errName, alias := s.decodeAssign(as); errName != "" {
					s.branch(x, errName, alias)
				}
			}
		case *ast.AssignStmt:
			errName, alias := s.decodeAssign(x)
			if errName == "" {
				continue
			}
			for _, later := range list[i+1:] {
				if ifs, ok := later.(*ast.IfStmt); ok && mentions(ifs.Cond, errName) {
					s.branch(ifs, errName, alias)
				}
				if as, ok := later.(*ast.AssignStmt); ok && mentionsAny(as.Lhs, errName) {
					break
				}
			}
		}
	}
}

// decodeAssign returns the error variable and the alias type when as is
// err := <JSON decode into a type containing an alias-list type>, or
// ..., err := <call of a decodeErrorSources name>.
func (s *decodeScope) decodeAssign(as *ast.AssignStmt) (string, string) {
	if len(as.Rhs) != 1 {
		return "", ""
	}
	call, ok := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return "", ""
	}
	id, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !ok || id.Name == "_" {
		return "", ""
	}
	if alias := s.sources[calleeName(call)]; alias != "" && s.targets[alias] {
		return id.Name, alias
	}
	target := s.decodeTarget(call)
	if target == nil {
		return "", ""
	}
	for _, t := range s.targetTypes(target) {
		if alias := s.contains(t, map[string]bool{}); alias != "" {
			return id.Name, alias
		}
	}
	return "", ""
}

// calleeName returns the function or method name a call names, or "".
func calleeName(call *ast.CallExpr) string {
	switch x := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// decodeTarget returns v in json.Unmarshal(data, v) or <decoder>.Decode(v).
func (s *decodeScope) decodeTarget(call *ast.CallExpr) ast.Expr {
	if s.f.isPkgFunc(call, "encoding/json", "Unmarshal") && len(call.Args) == 2 {
		return call.Args[1]
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Decode" || len(call.Args) != 1 {
		return nil
	}
	switch x := ast.Unparen(sel.X).(type) {
	case *ast.CallExpr:
		if s.f.isPkgFunc(x, "encoding/json", "NewDecoder") {
			return call.Args[0]
		}
	case *ast.Ident:
		if s.decoders[x.Name] {
			return call.Args[0]
		}
	}
	return nil
}

// targetTypes resolves a decode target (&v, v or &v.Field) to types.
func (s *decodeScope) targetTypes(e ast.Expr) []texpr {
	e = ast.Unparen(e)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = ast.Unparen(u.X)
	}
	var out []texpr
	switch x := e.(type) {
	case *ast.Ident:
		for _, t := range s.vars[x.Name] {
			out = append(out, texpr{s.f, t})
		}
	case *ast.SelectorExpr:
		if base, ok := x.X.(*ast.Ident); ok {
			for _, t := range s.vars[base.Name] {
				if ft, ok := s.field(texpr{s.f, t}, x.Sel.Name, 0); ok {
					out = append(out, ft)
				}
			}
		}
	case *ast.CompositeLit:
		out = append(out, texpr{s.f, x.Type})
	}
	return out
}

// resolve follows a named type of this module to its definition.
func (s *decodeScope) resolve(t texpr) (texpr, string, bool) {
	var p *pkg
	var name string
	switch x := t.e.(type) {
	case *ast.Ident:
		p, name = t.f.pkg, x.Name
	case *ast.SelectorExpr:
		if ip := t.f.selectorPkg(x); s.m.internal(ip) {
			p, name = s.m.pkgs[s.m.nameOf(ip)], x.Sel.Name
		}
	}
	if p == nil || p.types[name] == nil {
		return texpr{}, "", false
	}
	td := p.types[name]
	return texpr{td.file, td.expr}, p.name + "." + name, true
}

// field returns the type of a named field of a struct type.
func (s *decodeScope) field(t texpr, name string, depth int) (texpr, bool) {
	if depth > 8 || t.e == nil {
		return texpr{}, false
	}
	switch x := t.e.(type) {
	case *ast.StarExpr:
		return s.field(texpr{t.f, x.X}, name, depth+1)
	case *ast.StructType:
		for _, fl := range x.Fields.List {
			for _, id := range fl.Names {
				if id.Name == name {
					return texpr{t.f, fl.Type}, true
				}
			}
		}
	case *ast.Ident, *ast.SelectorExpr:
		if def, _, ok := s.resolve(t); ok {
			return s.field(def, name, depth+1)
		}
	}
	return texpr{}, false
}

// contains returns the alias-list type that t is or contains, or "".
func (s *decodeScope) contains(t texpr, seen map[string]bool) string {
	var parts []ast.Expr
	switch x := t.e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		def, key, ok := s.resolve(t)
		if !ok || seen[key] {
			return ""
		}
		if s.targets[key] {
			return key
		}
		seen[key] = true
		return s.contains(def, seen)
	case *ast.StarExpr:
		parts = []ast.Expr{x.X}
	case *ast.ParenExpr:
		parts = []ast.Expr{x.X}
	case *ast.ArrayType:
		parts = []ast.Expr{x.Elt}
	case *ast.MapType:
		parts = []ast.Expr{x.Key, x.Value}
	case *ast.IndexExpr:
		parts = []ast.Expr{x.X, x.Index}
	case *ast.IndexListExpr:
		parts = append([]ast.Expr{x.X}, x.Indices...)
	case *ast.StructType:
		for _, fl := range x.Fields.List {
			parts = append(parts, fl.Type)
		}
	}
	for _, p := range parts {
		if key := s.contains(texpr{t.f, p}, seen); key != "" {
			return key
		}
	}
	return ""
}

// branch reports the first call in the if statement's branches that writes
// a response and formats errName.
func (s *decodeScope) branch(ifs *ast.IfStmt, errName, alias string) {
	var leak ast.Node
	find := func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && leak == nil && s.writes(call) && formatsErr(s.f, call.Args, errName) {
			leak = call
		}
		return leak == nil
	}
	ast.Inspect(ifs.Body, find)
	if ifs.Else != nil {
		ast.Inspect(ifs.Else, find)
	}
	if leak != nil {
		s.leaks = append(s.leaks, fmt.Sprintf("%s: %s writes the error from decoding into %s to the response; "+
			"encoding/json names that type in it, so answer with a fixed message or log the error (respondError)",
			s.m.pos(leak), funcName(s.fn), alias))
	}
}

// writes reports whether a call takes a response writer, as an argument or
// as (part of) its receiver.
func (s *decodeScope) writes(call *ast.CallExpr) bool {
	found := false
	visit := func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && s.writers[id.Name] {
			found = true
		}
		return !found
	}
	ast.Inspect(call.Fun, visit)
	for _, a := range call.Args {
		ast.Inspect(a, visit)
	}
	return found
}

// formatsErr reports whether args contain errName.Error() or errName passed
// to a fmt formatting function.
func formatsErr(f *file, args []ast.Expr, errName string) bool {
	found := false
	for _, a := range args {
		ast.Inspect(a, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || found {
				return !found
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && isIdent(sel.X, errName) {
				found = true
			}
			if f.isPkgFunc(call, "fmt", "Sprint", "Sprintf", "Sprintln", "Errorf") && mentionsAny(call.Args, errName) {
				found = true
			}
			return !found
		})
	}
	return found
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == name
}

func mentions(e ast.Expr, name string) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

func mentionsAny(list []ast.Expr, name string) bool {
	for _, e := range list {
		if mentions(e, name) {
			return true
		}
	}
	return false
}

// TestDecodeAliasRule proves the R8 check on a fixture module: four
// handlers leak a decode error of an alias-list type (directly, nested in a
// literal struct, through a named type with a decoder variable, and from a
// helper named in the error sources), three do not (a fixed message; a type
// not on the list; a helper's error answered with a fixed message).
func TestDecodeAliasRule(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":                              "module example.com/fixture\n\ngo 1.25\n",
		"internal/domain/exports/export.go":   "package exports\n\ntype ProjectExport struct {\n\tName string `json:\"name\"`\n}\n",
		"internal/api/handlers.go":            decodeFixture,
		"internal/api/handlers_test.go":       "package api\n",
		"cmd/server/main.go":                  "package main\n\nfunc main() {}\n",
		"internal/domain/exports/doc_test.go": "package exports\n",
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{"loadExport": "internal/domain/exports.ProjectExport"}
	if leaks := decodeLeaks(m, nil, sources); len(leaks) != 0 {
		t.Fatalf("an empty alias list must find nothing, found %v", leaks)
	}
	leaks := decodeLeaks(m, []string{"internal/domain/exports.ProjectExport"}, sources)
	want := []string{"Handler.LeakDirect", "Handler.LeakNested", "Handler.LeakThroughNamedType", "Handler.LeakThroughHelper"}
	if len(leaks) != len(want) {
		t.Fatalf("found %d leaks, want %d:\n%s", len(leaks), len(want), strings.Join(leaks, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(leaks[i], " "+w+" writes the error from decoding into internal/domain/exports.ProjectExport") {
			t.Errorf("leak %d = %q, want it to name %s", i, leaks[i], w)
		}
	}
}

const decodeFixture = `package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"example.com/fixture/internal/domain/exports"
)

type Handler struct{}

type importBody struct {
	Project *exports.ProjectExport ` + "`json:\"project\"`" + `
}

func writeJSONError(w http.ResponseWriter, status int, msg string) { http.Error(w, msg, status) }

func (h *Handler) LeakDirect(w http.ResponseWriter, r *http.Request) {
	var in exports.ProjectExport
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
}

func (h *Handler) LeakNested(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	var body struct{ Items []exports.ProjectExport }
	err := json.Unmarshal(data, &body)
	if err != nil {
		http.Error(w, fmt.Sprintf("bad body: %v", err), http.StatusBadRequest)
	}
}

func (h *Handler) LeakThroughNamedType(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	req := importBody{}
	if err := dec.Decode(&req); err != nil {
		_, _ = w.Write([]byte(err.Error()))
	}
}

func (h *Handler) FixedMessage(w http.ResponseWriter, r *http.Request) {
	var in exports.ProjectExport
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
	}
}

func (h *Handler) OtherType(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
	}
}

func (h *Handler) loadExport(data []byte) (*exports.ProjectExport, error) {
	var out exports.ProjectExport
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse export: %w", err)
	}
	return &out, nil
}

func (h *Handler) LeakThroughHelper(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	export, err := h.loadExport(data)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = export
}

func (h *Handler) HelperFixedMessage(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	if _, err := h.loadExport(data); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid export")
	}
}
`

// writeFixture writes files (module-relative path -> content) under a
// temporary directory and returns it.
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
