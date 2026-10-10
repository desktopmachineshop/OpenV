package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// helperSource is the contract of the helpers the rewrite calls, as X1a
// adds them to internal/api/respond.go: these declarations, token for
// token (doc comments aside). The tool refuses to write a rewrite into a
// package that does not declare each helper it calls exactly so, and its
// tests prove, through httptest, that every shape it rewrites answers
// byte for byte what the helper call answers when the helpers are these.
//
// Each writer makes the same calls on the http.ResponseWriter, in the same
// order, as the statements it replaces, so a wrapper (the gzip writer, a
// status recorder) sees no difference either. The encode keeps
// json.NewEncoder, so the body keeps its HTML escaping and its trailing
// newline, and the encode error is dropped as every handler dropped it.
const helperSource = `package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON answers status with v as a JSON body: Content-Type
// application/json, then the status, then v as json.NewEncoder encodes it
// (HTML escaped, with a trailing newline).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONOK answers v as a JSON body with Content-Type application/json
// and the implicit 200: it calls no WriteHeader, so the first write sends
// the status.
func writeJSONOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONBare answers v as a JSON body with the implicit 200 and no
// Content-Type of its own, so net/http sniffs one from the body (quirk Q1).
func writeJSONBare(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONBareStatus answers status with v as a JSON body and no
// Content-Type of its own (quirk Q1).
func writeJSONBareStatus(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// invalidRequestBody is the message of a request body that does not
// decode, the one "invalid request body" literal of the package.
const invalidRequestBody = "invalid request body"

// decodeJSON decodes the request body into v. When that fails it answers
// 400 {"error":"invalid request body"} and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONMsg(w, r, v, invalidRequestBody)
}

// decodeJSONMsg is decodeJSON for a handler whose 400 says msg (quirk Q19).
func decodeJSONMsg(w http.ResponseWriter, r *http.Request, v any, msg string) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, msg)
		return false
	}
	return true
}
`

// The helper names, as the rewrite calls them.
const (
	hWriteJSON           = "writeJSON"
	hWriteJSONOK         = "writeJSONOK"
	hWriteJSONBare       = "writeJSONBare"
	hWriteJSONBareStatus = "writeJSONBareStatus"
	hDecodeJSON          = "decodeJSON"
	hDecodeJSONMsg       = "decodeJSONMsg"
	// hInvalidBody is the constant an "invalid request body" literal that
	// no decodeJSON replaces becomes.
	hInvalidBody = "invalidRequestBody"
)

// helperNames lists the helpers in helperSource's order. decodeJSON calls
// decodeJSONMsg, and both call writeJSONError, which httperr.go declares.
var helperNames = []string{hWriteJSON, hWriteJSONOK, hWriteJSONBare, hWriteJSONBareStatus, hDecodeJSON, hDecodeJSONMsg}

// helperDeps names what a helper calls in its own package.
var helperDeps = map[string][]string{
	hDecodeJSON:    {hDecodeJSONMsg, hInvalidBody},
	hDecodeJSONMsg: {"writeJSONError"},
}

// isHelperName reports whether name is one of the helpers, whose bodies
// have the shapes the tool rewrites and are never rewritten.
func isHelperName(name string) bool {
	for _, n := range helperNames {
		if n == name {
			return true
		}
	}
	return false
}

// canonicalHelpers renders each helper of helperSource without comments.
func canonicalHelpers() map[string]string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "helpers.go", helperSource, 0)
	if err != nil {
		panic("httpio: helperSource does not parse: " + err.Error())
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		if name, text := renderDecl(fset, d); name != "" {
			out[name] = text
		}
	}
	return out
}

// renderDecl prints a package-level function, or a single-name const, by
// name and without comments; other declarations give "".
func renderDecl(fset *token.FileSet, d ast.Decl) (name, text string) {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			return d.Name.Name, renderFunc(fset, d)
		}
	case *ast.GenDecl:
		if d.Tok != token.CONST {
			return "", ""
		}
		for _, sp := range d.Specs {
			vs := sp.(*ast.ValueSpec)
			if len(vs.Names) == 1 && len(vs.Values) == 1 && vs.Names[0].Name == hInvalidBody {
				cp := *vs
				cp.Doc, cp.Comment = nil, nil
				var b bytes.Buffer
				if err := printer.Fprint(&b, fset, &cp); err != nil {
					return "", ""
				}
				return hInvalidBody, "const " + b.String()
			}
		}
	}
	return "", ""
}

// renderFunc prints a function declaration without its doc comment.
func renderFunc(fset *token.FileSet, fd *ast.FuncDecl) string {
	cp := *fd
	cp.Doc = nil
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, &cp); err != nil {
		return "<" + err.Error() + ">"
	}
	return b.String()
}

// checkHelpers reports, for the package in dir, each helper in need that
// it does not declare as helperSource does, with what the helper itself
// needs (decodeJSON needs decodeJSONMsg). A nil result means every one is
// declared exactly so.
func checkHelpers(dir string, need map[string]bool) ([]string, error) {
	want := canonicalHelpers()
	closure := map[string]bool{}
	var add func(string)
	add = func(n string) {
		if closure[n] {
			return
		}
		closure[n] = true
		for _, d := range helperDeps[n] {
			add(d)
		}
	}
	for n := range need {
		add(n)
	}
	have, err := packageFuncs(dir, closure)
	if err != nil {
		return nil, err
	}
	var problems []string
	names := make([]string, 0, len(closure))
	for n := range closure {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		got, ok := have[n]
		switch {
		case !ok:
			problems = append(problems, n+" is not declared")
		case want[n] != "" && got != want[n]:
			problems = append(problems, n+" is not declared as internal/tools/httpio/helpers.go's helperSource declares it")
		}
	}
	return problems, nil
}

// packageFuncs renders the package-level functions (and the constant) of
// dir's non-test Go files whose names are in names.
func packageFuncs(dir string, names map[string]bool) (map[string]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, d := range f.Decls {
			if name, text := renderDecl(fset, d); names[name] {
				out[name] = text
			}
		}
	}
	return out, nil
}
