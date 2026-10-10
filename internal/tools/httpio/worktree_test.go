package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// countSites counts what internal/archtest's raw_json_encodes and
// invalid_request_body_literals count in dir, independently of the tool:
// json.NewEncoder calls outside respond.go, and "invalid request body"
// literals, in non-test files.
func countSites(t *testing.T, dir string) (encodes, literals int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		jsonName := ""
		for _, imp := range f.Imports {
			if imp.Path.Value == `"encoding/json"` {
				jsonName = "json"
				if imp.Name != nil {
					jsonName = imp.Name.Name
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewEncoder" && filepath.Base(p) != "respond.go" {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == jsonName && jsonName != "" {
						encodes++
					}
				}
			case *ast.BasicLit:
				if s, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil && s == invalidBody {
					literals++
				}
			}
			return true
		})
	}
	return encodes, literals
}

var totalRe = regexp.MustCompile(`total: encodes (\d+) \(.*; left (\d+)\), "invalid request body" literals (\d+) \(.*; left (\d+)\), decodeJSONMsg \d+, kept (\d+)`)

// totals reads the report's total line: encode sites and those left,
// literal sites and those left, and the kept literal.
func totals(t *testing.T, stdout string) [5]int {
	t.Helper()
	m := totalRe.FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("no total line in:\n%s", stdout)
	}
	var out [5]int
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

// TestWorkingTree runs X1 on this repository's internal/api as its pull
// requests will: -n accounts for every site the two ratchets count, and the
// rewrite of every area, on a copy whose respond.go holds the helpers
// (added from helperSource until X1a has), builds and vets in place of the
// real package (go build -overlay), leaving only the sites it reported as
// left. Skipped under -short.
func TestWorkingTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds internal/api with the rewrite; run without -short")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	api := filepath.Join(repo, "internal", "api")
	encodes, literals := countSites(t, api)

	code, stdout, stderr := runTool(t, repo, "-n", "internal/api")
	if code != 0 {
		t.Fatalf("-n: exit %d: %s", code, stderr)
	}
	tot := totals(t, stdout)
	if tot[0] != encodes || tot[2] != literals {
		t.Fatalf("the report counts %d encodes and %d literals; internal/api has %d and %d", tot[0], tot[2], encodes, literals)
	}
	if !strings.Contains(stdout, "\narea ") {
		t.Errorf("no summary per area:\n%s", stdout)
	}

	tmp := t.TempDir()
	copyDir := filepath.Join(tmp, "api")
	paths, _ := filepath.Glob(filepath.Join(api, "*.go"))
	overlay := map[string]string{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		dst := filepath.Join(copyDir, filepath.Base(p))
		writeFile(t, dst, readFile(t, p))
		overlay[p] = dst
	}
	all := map[string]bool{}
	for _, h := range helperNames {
		all[h] = true
	}
	if problems, err := checkHelpers(copyDir, all); err != nil {
		t.Fatal(err)
	} else if len(problems) > 0 {
		respond := filepath.Join(copyDir, "respond.go")
		writeFile(t, respond, readFile(t, respond)+helperDecls(t))
	}
	code, stdout, stderr = runTool(t, tmp, "api")
	if code != 0 {
		t.Fatalf("rewrite: exit %d: %s", code, stderr)
	}
	after := totals(t, stdout)
	if left, lits := countSites(t, copyDir); left != after[1] || lits != after[4] {
		t.Errorf("after the rewrite %d encodes and %d literals remain; the report left %d and kept %d", left, lits, after[1], after[4])
	}
	data, err := json.Marshal(map[string]any{"Replace": overlay})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(tmp, "overlay.json")
	writeFile(t, overlayFile, string(data))
	goCmd(t, repo, "build", "-overlay", overlayFile, "./internal/api")
	goCmd(t, repo, "vet", "-overlay", overlayFile, "./internal/api")
}

// helperDecls returns helperSource's declarations without its package
// clause and imports, to append to a respond.go that imports encoding/json
// and net/http.
func helperDecls(t *testing.T) string {
	t.Helper()
	i := strings.Index(helperSource, "\n)\n")
	if i < 0 {
		t.Fatal("helperSource has no import block")
	}
	return helperSource[i+2:]
}

func TestHelperDeclsAppend(t *testing.T) {
	src := "package api\n\nimport (\n\t\"encoding/json\"\n\t\"net/http\"\n)\n" + helperDecls(t)
	if _, err := parser.ParseFile(token.NewFileSet(), "respond.go", src, 0); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "respond.go"), src)
	writeFile(t, filepath.Join(dir, "httperr.go"), "package api\n\nimport \"net/http\"\n\nfunc writeJSONError(w http.ResponseWriter, status int, message string) {}\n")
	all := map[string]bool{}
	for _, h := range helperNames {
		all[h] = true
	}
	problems, err := checkHelpers(dir, all)
	if err != nil || len(problems) > 0 {
		t.Fatalf("helperSource's own declarations fail the check: %v %v", problems, err)
	}
	if fixture := readFile(t, filepath.Join("testdata", "fixture", "respond.go")); !strings.HasSuffix(fixture, helperDecls(t)) {
		t.Error("testdata/fixture/respond.go no longer holds helperSource's declarations")
	}
	if _, err := os.Stat(filepath.Join("testdata", "fixture", "httperr.go")); err != nil {
		t.Fatal(err)
	}
}
