package contract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/vv"
)

// The readers below are internal/vocabparity's (refactor plan S13), copied
// because that package has only test files and so cannot be imported. They
// find a catalogue by declaration name, never by file or line, so a move
// within its package leaves them alone. TestContractAgreesWithS13 holds the
// two packages' results equal wherever both read the same vocabulary.

// sources holds the parsed non-test files of the packages the readers need,
// keyed by module-relative directory.
type sources struct {
	root  string
	fset  *token.FileSet
	files map[string][]*ast.File
}

func loadSources(t *testing.T) *sources {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module root %s has no go.mod: %v", root, err)
	}
	return &sources{root: root, fset: token.NewFileSet(), files: map[string][]*ast.File{}}
}

// path is a module-relative slash path as a path on disk.
func (s *sources) path(rel string) string {
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

// pkg parses a package directory's non-test .go files once, in name order.
func (s *sources) pkg(t *testing.T, dir string) []*ast.File {
	t.Helper()
	if files, ok := s.files[dir]; ok {
		return files
	}
	matches, err := filepath.Glob(filepath.Join(s.path(dir), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	var files []*ast.File
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(s.fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("%s: no Go files", dir)
	}
	s.files[dir] = files
	return files
}

func (s *sources) pos(n ast.Node) string {
	p := s.fset.Position(n.Pos())
	if rel, err := filepath.Rel(s.root, p.Filename); err == nil {
		p.Filename = filepath.ToSlash(rel)
	}
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

func (s *sources) stringLit(t *testing.T, e ast.Expr) string {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s: expected a string literal", s.pos(e))
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("%s: %v", s.pos(e), err)
	}
	return v
}

// constBlock returns the string values of the const block in dir that
// declares anchor, in declaration order. Every constant of the block is
// part of the vocabulary, so a constant added to it is seen.
func (s *sources) constBlock(t *testing.T, dir, anchor string) []string {
	t.Helper()
	for _, f := range s.pkg(t, dir) {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST || !declares(gd, anchor) {
				continue
			}
			var out []string
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if len(vs.Values) != len(vs.Names) {
					t.Fatalf("%s: a constant without its own value in the block of %s", s.pos(vs), anchor)
				}
				for _, v := range vs.Values {
					out = append(out, s.stringLit(t, v))
				}
			}
			return out
		}
	}
	t.Fatalf("%s: no const block declares %s", dir, anchor)
	return nil
}

func declares(gd *ast.GenDecl, name string) bool {
	for _, spec := range gd.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			for _, n := range vs.Names {
				if n.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// funcDecl returns the package-level function name (not a method) in dir.
func (s *sources) funcDecl(t *testing.T, dir, name string) *ast.FuncDecl {
	t.Helper()
	for _, f := range s.pkg(t, dir) {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return fd
			}
		}
	}
	t.Fatalf("%s: no func %s", dir, name)
	return nil
}

// gapTables reads both Go gap-label tables in internal/domain/reports: the
// one buildVVReportPDF declares as gapSections (the PDF V&V report) and the
// one the gapSections function returns (the PDF and DOCX project reports).
// Each row is {"label", gaps.Field}; the field becomes vv.GapReport's JSON
// key, the bucket name GET /api/v1/projects/{id}/vv/gaps sends.
func gapTables(t *testing.T, src *sources) (vvReport, project []gapLabel) {
	t.Helper()
	const dir = "internal/domain/reports"
	build := src.funcDecl(t, dir, "buildVVReportPDF")
	var lit *ast.CompositeLit
	ast.Inspect(build.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == "gapSections" {
				lit, _ = as.Rhs[0].(*ast.CompositeLit)
			}
		}
		return true
	})
	if lit == nil {
		t.Fatalf("%s: buildVVReportPDF declares no gapSections := []struct{...}{...}", dir)
	}
	vvReport = src.gapRows(t, lit)

	fn := src.funcDecl(t, dir, "gapSections")
	lit = nil
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			if cl, ok := ret.Results[0].(*ast.CompositeLit); ok {
				lit = cl
			}
		}
		return true
	})
	if lit == nil {
		t.Fatalf("%s: func gapSections returns no []struct{...}{...} literal", dir)
	}
	project = src.gapRows(t, lit)
	return vvReport, project
}

func (s *sources) gapRows(t *testing.T, lit *ast.CompositeLit) []gapLabel {
	t.Helper()
	report := reflect.TypeOf(vv.GapReport{})
	var rows []gapLabel
	for _, e := range lit.Elts {
		row, ok := e.(*ast.CompositeLit)
		if !ok || len(row.Elts) != 2 {
			t.Fatalf("%s: a gap row that is not {\"label\", gaps.Field}", s.pos(e))
		}
		label := s.stringLit(t, row.Elts[0])
		sel, ok := row.Elts[1].(*ast.SelectorExpr)
		if !ok {
			t.Fatalf("%s: a gap row whose list is not gaps.Field", s.pos(row.Elts[1]))
		}
		field, ok := report.FieldByName(sel.Sel.Name)
		if !ok {
			t.Fatalf("%s: vv.GapReport has no field %s", s.pos(sel), sel.Sel.Name)
		}
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if key == "" {
			t.Fatalf("vv.GapReport.%s has no JSON name", sel.Sel.Name)
		}
		rows = append(rows, gapLabel{Key: key, Label: label})
	}
	return rows
}

// lineDiff lists the lines that differ between pinned and current, by
// longest common subsequence, at most limit of them.
func lineDiff(pinned, current string, limit int) []string {
	a := strings.Split(strings.TrimSuffix(pinned, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(current, "\n"), "\n")
	common := make([][]int, len(a)+1)
	for i := range common {
		common[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out []string
	changed := 0
	emit := func(sign string, n int, line string) {
		if changed++; changed <= limit {
			out = append(out, fmt.Sprintf("  %s %d: %s", sign, n, line))
		}
	}
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || common[i+1][j] >= common[i][j+1]):
			emit("-", i+1, a[i])
			i++
		default:
			emit("+", j+1, b[j])
			j++
		}
	}
	if changed > limit {
		out = append(out, fmt.Sprintf("  ... and %d more changed lines", changed-limit))
	}
	return out
}
