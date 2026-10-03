// Package vocabparity holds refactor plan step S13's Go side (invariant I24):
// the vocabularies the frontend copies by hand from Go, written from the Go
// catalogues into contracts/vocab.json at the repository root. The package
// has only this test file; it imports the domain packages whose catalogues
// are exported and parses the Go sources for those that are not (a constant
// block, an unexported table or function), so it sits apart from both, and
// nothing imports it.
//
// frontend/src/arch/vocabParity.test.ts compares every hand-written
// TypeScript copy with contracts/vocab.json; the differences it tolerates
// are today's, listed in contracts/vocab-allowed-diffs.json. A Go change to
// any vocabulary fails here first; a TypeScript change fails there. Only
// UPDATE_GOLDEN=1 rewrites the file; any other value compares.
package vocabparity

import (
	"bytes"
	"encoding/json"
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

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/quality"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

const vocabFile = "contracts/vocab.json"

// updateEnv set to exactly 1 rewrites contracts/vocab.json instead of
// comparing against it.
const updateEnv = "UPDATE_GOLDEN"

const regenerate = updateEnv + "=1 go test ./internal/vocabparity -count=1 -run '^TestVocabulary$'"

const vocabAbout = "Go vocabularies the frontend copies by hand (refactor plan S13, invariant I24), each read from its " +
	"Go catalogue: link rules with their text, artifact types and the ones the quality linter scores, artifact " +
	"statuses with the transitions each allows, feature keys in registry order, domain event types, plans (with " +
	"the legacy aliases), API error codes, V&V gap labels by JSON key (the two Go tables in internal/domain/" +
	"reports are asserted equal), agent providers in display order, upload extensions with their kind, the paths " +
	"the API serves without a session, and the guided wizard's step labels. Written by TestVocabulary " +
	"(internal/vocabparity/vocab_test.go); frontend/src/arch/vocabParity.test.ts checks every TypeScript copy " +
	"against it, allowing only the differences in contracts/vocab-allowed-diffs.json. Regenerate only for a " +
	"deliberate change: " + regenerate

// vocabulary is the document contracts/vocab.json holds.
type vocabulary struct {
	About              string           `json:"about"`
	LinkRules          []linkRule       `json:"link_rules"`
	ArtifactTypes      []artifactType   `json:"artifact_types"`
	QualityLintedTypes []string         `json:"quality_linted_types"`
	ArtifactStatuses   []artifactStatus `json:"artifact_statuses"`
	FeatureKeys        []string         `json:"feature_keys"`
	EventTypes         []string         `json:"event_types"`
	Plans              []string         `json:"plans"`
	ErrorCodes         []string         `json:"error_codes"`
	GapLabels          []gapLabel       `json:"gap_labels"`
	Providers          []string         `json:"providers"`
	UploadExtensions   []uploadExt      `json:"upload_extensions"`
	OpenPaths          openPaths        `json:"open_paths"`
	WizardStepLabels   []string         `json:"wizard_step_labels"`
}

type linkRule struct {
	Type             string   `json:"type"`
	Label            string   `json:"label"`
	InverseLabel     string   `json:"inverse_label"`
	AllowedFromTypes []string `json:"allowed_from_types"`
	AllowedToTypes   []string `json:"allowed_to_types"`
	Description      string   `json:"description"`
}

type artifactType struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type artifactStatus struct {
	Value string   `json:"value"`
	Next  []string `json:"next"`
}

type gapLabel struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type uploadExt struct {
	Ext  string `json:"ext"`
	Kind string `json:"kind"`
}

type openPaths struct {
	Exact    []string `json:"exact"`
	Prefixes []string `json:"prefixes"`
}

// TestVocabulary builds the vocabularies from the Go catalogues and compares
// them with contracts/vocab.json.
func TestVocabulary(t *testing.T) {
	src := loadSources(t)
	got := buildVocabulary(t, src)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(got); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(src.root, filepath.FromSlash(vocabFile))
	if os.Getenv(updateEnv) == "1" {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write %s: %v", vocabFile, err)
		}
		t.Logf("regenerated %s", vocabFile)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with:\n  %s", vocabFile, err, regenerate)
	}
	if bytes.Equal(want, buf.Bytes()) {
		return
	}
	t.Fatalf("%s does not match the Go catalogues; first differences (- pinned, + current, by line):\n%s\n"+
		"The frontend holds hand-written copies of these vocabularies, and stored data, saved automations and "+
		"deployed clients use their values, so a refactor never changes this file. If the change is deliberate, "+
		"regenerate it with:\n  %s\n(only %s=1 regenerates; any other value compares), then run "+
		"`cd frontend && npx vitest run src/arch/vocabParity.test.ts`, which names every TypeScript copy that "+
		"now disagrees.", vocabFile, strings.Join(lineDiff(string(want), buf.String(), 20), "\n"), regenerate, updateEnv)
}

// TestVocabularyGapLabelCopiesAgree: internal/domain/reports titles the V&V
// gap lists twice, in the PDF V&V report (buildVVReportPDF's gapSections)
// and in the project report (the gapSections function the PDF and DOCX
// project reports share). vocab.json holds one table, so the two must be
// equal, row for row and in order.
func TestVocabularyGapLabelCopiesAgree(t *testing.T) {
	src := loadSources(t)
	vvReport, project := gapTables(t, src)
	if !reflect.DeepEqual(vvReport, project) {
		t.Fatalf("the two Go gap-label tables in internal/domain/reports differ:\n  buildVVReportPDF: %v\n"+
			"  gapSections:      %v\nkeep them equal (refactor plan X4a makes them one table)", vvReport, project)
	}
}

// buildVocabulary reads every vocabulary. Each must be non-empty: a reader
// that finds nothing has lost its catalogue, not emptied it.
func buildVocabulary(t *testing.T, src *sources) vocabulary {
	t.Helper()
	v := vocabulary{About: vocabAbout}

	for _, r := range links.GetLinkTypeRules() {
		v.LinkRules = append(v.LinkRules, linkRule{
			Type: r.Type, Label: r.Label, InverseLabel: r.InverseLabel,
			AllowedFromTypes: r.AllowedFromTypes, AllowedToTypes: r.AllowedToTypes, Description: r.Description,
		})
	}

	for _, d := range artifacts.TypeCatalog() {
		v.ArtifactTypes = append(v.ArtifactTypes, artifactType{Value: d.Value, Label: d.Label})
		if quality.IsRequirementType(d.Value) {
			v.QualityLintedTypes = append(v.QualityLintedTypes, d.Value)
		}
	}

	// The statuses are the constant block StatusDraft heads; the state
	// machine is unexported, so each status's next ones are asked of
	// CanTransition, in the block's order.
	statuses := src.constBlock(t, "internal/domain/artifacts", "StatusDraft")
	for _, s := range statuses {
		if !artifacts.ValidStatus(s) {
			t.Fatalf("status constant %q is not a valid status (artifacts.ValidStatus)", s)
		}
		next := []string{}
		for _, to := range statuses {
			if artifacts.CanTransition(s, to) {
				next = append(next, to)
			}
		}
		v.ArtifactStatuses = append(v.ArtifactStatuses, artifactStatus{Value: s, Next: next})
	}

	for _, f := range release.Registry {
		v.FeatureKeys = append(v.FeatureKeys, f.Key)
	}

	v.EventTypes = src.constBlock(t, "internal/domain/events", "ArtifactCreated")

	v.Plans = src.constBlock(t, "internal/domain/orgs", "PlanSingle")
	for _, p := range v.Plans {
		if !orgs.ValidPlan(p) {
			t.Fatalf("plan constant %q is not a valid plan (orgs.ValidPlan)", p)
		}
	}

	v.ErrorCodes = src.constBlock(t, "internal/api", "ErrCodeEmailUnverified")

	vvReport, _ := gapTables(t, src)
	v.GapLabels = vvReport

	v.Providers = providers.KnownProviders()

	exts := attachments.AcceptedExtensions()
	sort.Strings(exts)
	for _, ext := range exts {
		_, kind, ok := attachments.AcceptUpload("", "upload"+ext)
		if !ok {
			t.Fatalf("attachments.AcceptedExtensions lists %q but AcceptUpload refuses it", ext)
		}
		v.UploadExtensions = append(v.UploadExtensions, uploadExt{Ext: ext, Kind: string(kind)})
	}

	v.OpenPaths = src.openPaths(t)

	v.WizardStepLabels = src.stringSlice(t, "internal/api", "guidedStepLabels")

	for name, n := range map[string]int{
		"link_rules": len(v.LinkRules), "artifact_types": len(v.ArtifactTypes),
		"quality_linted_types": len(v.QualityLintedTypes), "artifact_statuses": len(v.ArtifactStatuses),
		"feature_keys": len(v.FeatureKeys), "event_types": len(v.EventTypes), "plans": len(v.Plans),
		"error_codes": len(v.ErrorCodes), "gap_labels": len(v.GapLabels), "providers": len(v.Providers),
		"upload_extensions": len(v.UploadExtensions), "open_paths.prefixes": len(v.OpenPaths.Prefixes),
		"wizard_step_labels": len(v.WizardStepLabels),
	} {
		if n == 0 {
			t.Errorf("%s: the reader found nothing; its Go catalogue has moved or changed shape", name)
		}
	}
	return v
}

// gapTables reads both Go gap-label tables: the one buildVVReportPDF
// declares as gapSections and the one the gapSections function returns.
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

// openPaths reads isOpenPath (internal/api) in the one shape it has: a run of
// `if path == "<exact>" { return true }` and
// `if strings.HasPrefix(path, "<prefix>") { return true }`, then
// `return false`. Any other shape fails rather than being read as open.
func (s *sources) openPaths(t *testing.T) openPaths {
	t.Helper()
	fn := s.funcDecl(t, "internal/api", "isOpenPath")
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) != 1 {
		t.Fatalf("%s: isOpenPath does not take one parameter", s.pos(fn))
	}
	param := params[0].Names[0].Name
	isParam := func(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == param }
	isIdent := func(e ast.Expr, name string) bool { id, ok := e.(*ast.Ident); return ok && id.Name == name }
	out := openPaths{Exact: []string{}, Prefixes: []string{}}
	stmts := fn.Body.List
	for i, st := range stmts {
		if i == len(stmts)-1 {
			if ret, ok := st.(*ast.ReturnStmt); !ok || len(ret.Results) != 1 || !isIdent(ret.Results[0], "false") {
				t.Fatalf("%s: isOpenPath must end in `return false`; this guard cannot read its shape", s.pos(st))
			}
			break
		}
		ifs, ok := st.(*ast.IfStmt)
		if !ok || ifs.Init != nil || ifs.Else != nil || len(ifs.Body.List) != 1 {
			t.Fatalf("%s: isOpenPath has a statement this guard cannot read", s.pos(st))
		}
		if ret, ok := ifs.Body.List[0].(*ast.ReturnStmt); !ok || len(ret.Results) != 1 || !isIdent(ret.Results[0], "true") {
			t.Fatalf("%s: an isOpenPath branch must only `return true`", s.pos(ifs.Body))
		}
		switch c := ifs.Cond.(type) {
		case *ast.BinaryExpr:
			if c.Op != token.EQL || !isParam(c.X) {
				t.Fatalf("%s: isOpenPath compares something other than %s == \"...\"", s.pos(c), param)
			}
			out.Exact = append(out.Exact, s.stringLit(t, c.Y))
		case *ast.CallExpr:
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || !isIdent(sel.X, "strings") || sel.Sel.Name != "HasPrefix" || len(c.Args) != 2 || !isParam(c.Args[0]) {
				t.Fatalf("%s: isOpenPath calls something other than strings.HasPrefix(%s, \"...\")", s.pos(c), param)
			}
			out.Prefixes = append(out.Prefixes, s.stringLit(t, c.Args[1]))
		default:
			t.Fatalf("%s: isOpenPath has a condition this guard cannot read", s.pos(ifs.Cond))
		}
	}
	return out
}

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

// pkg parses a package directory's non-test .go files once, in name order.
func (s *sources) pkg(t *testing.T, dir string) []*ast.File {
	t.Helper()
	if files, ok := s.files[dir]; ok {
		return files
	}
	matches, err := filepath.Glob(filepath.Join(s.root, filepath.FromSlash(dir), "*.go"))
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

// stringSlice returns the elements of the package-level
// `var name = []string{...}` in dir.
func (s *sources) stringSlice(t *testing.T, dir, name string) []string {
	t.Helper()
	for _, f := range s.pkg(t, dir) {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR || !declares(gd, name) {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if len(vs.Names) != 1 || vs.Names[0].Name != name {
					continue
				}
				if len(vs.Values) != 1 {
					t.Fatalf("%s: var %s has no single value", s.pos(vs), name)
				}
				lit, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("%s: var %s is not a []string literal", s.pos(vs), name)
				}
				var out []string
				for _, e := range lit.Elts {
					out = append(out, s.stringLit(t, e))
				}
				return out
			}
		}
	}
	t.Fatalf("%s: no package-level var %s", dir, name)
	return nil
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
