package exports

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// This file is the import-fields guard of refactor plan step S9 (invariant
// I15, "which fields import carries over"; the plan's recipe (b), "Field on
// an entity"). testdata/import_fields.txt lists every field reachable from
// ProjectExport, as encoding/json sees it, and classifies it by what the
// JSON import (ImportProject: a new project, refs kept) does with it:
//
//   - carried: changing the field in the imported document changes what the
//     import writes, and the line names what it reaches (the new project, its
//     product profile, an artifact, a link or an attribute definition, by
//     JSON key);
//   - dropped: the import writes exactly the same with the field changed.
//
// The class is not declared by hand: TestImportFields measures it. It builds
// one document with every field set (a field a later change adds is filled
// by reflection, so it needs no edit here), imports it into recording fakes
// under the real artifact and attribute services, then, field by field,
// imports the same document with that one field changed and compares what
// was written, ids the import mints and times it stamps aside. A field added to ProjectExport, or
// to a type it reaches, is therefore a new line the golden lacks: the test
// fails, naming the field and what the import does with it, until the golden
// is regenerated, where the import mapper (createProjectFromExport and
// importArtifactsAndLinks in export.go) would otherwise drop it silently. A
// field the mapper starts or stops carrying changes its line too.
//
// Regenerate, for a deliberate change only, with
//
//	UPDATE_GOLDEN=1 go test ./internal/domain/exports -count=1 -run '^TestImportFields$'
//
// Only the value 1 regenerates; any other value compares. Paths and type
// names carry no package path, so P1, which moves ProjectExport and
// LinkedArtifact to internal/domain/snapshot under the same names (R8),
// leaves the golden byte-identical.

const (
	importFieldsGolden = "testdata/import_fields.txt"
	importFieldsShown  = "internal/domain/exports/testdata/import_fields.txt"
	importFieldsCmd    = "UPDATE_GOLDEN=1 go test ./internal/domain/exports -count=1 -run '^TestImportFields$'"
	// importFieldsUpdateEnv set to exactly 1 rewrites the golden; any other
	// value compares.
	importFieldsUpdateEnv = "UPDATE_GOLDEN"
)

// importField is one leaf reachable from ProjectExport.
type importField struct {
	path   string // the JSON path: artifacts[].title
	goName string // the declaring type and field: Artifact.Title
	goType string // the field's type, without package paths for this module's types
	steps  []int  // struct field indexes from ProjectExport; -1 descends into a slice element
}

var timeType = reflect.TypeOf(time.Time{})

// importFieldsModule is the module path; a type declared in it is written by
// its bare name (Artifact, not artifacts.Artifact), so a move between
// packages under the same name leaves the golden alone.
const importFieldsModule = "github.com/openv/requirements-platform/"

// walkImportFields lists the leaves of t in declaration order, as
// encoding/json reaches them: exported fields with a JSON name, through
// pointers, structs (embedded ones flattened) and slices of structs. A
// time.Time, a map and a slice of anything but structs are leaves.
func walkImportFields(t reflect.Type, prefix string, steps []int, out *[]importField) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, ok := importFieldJSONName(f)
		if !ok {
			continue
		}
		st := append(append([]int(nil), steps...), i)
		ft := derefType(f.Type)
		if f.Anonymous && ft.Kind() == reflect.Struct && f.Tag.Get("json") == "" {
			walkImportFields(ft, prefix, st, out)
			continue
		}
		path := prefix + name
		switch {
		case ft.Kind() == reflect.Struct && ft != timeType:
			walkImportFields(ft, path+".", st, out)
		case ft.Kind() == reflect.Slice && derefType(ft.Elem()).Kind() == reflect.Struct && derefType(ft.Elem()) != timeType:
			walkImportFields(derefType(ft.Elem()), path+"[].", append(st, -1), out)
		default:
			*out = append(*out, importField{path: path, goName: t.Name() + "." + f.Name, goType: typeName(f.Type), steps: st})
		}
	}
}

func importFieldJSONName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	return name, true
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// typeName writes a type the way the golden shows it: this module's named
// types by their bare name, everything else as reflect names it.
func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + typeName(t.Elem())
	case reflect.Slice:
		return "[]" + typeName(t.Elem())
	case reflect.Map:
		return "map[" + typeName(t.Key()) + "]" + typeName(t.Elem())
	}
	if t.Name() != "" && strings.HasPrefix(t.PkgPath(), importFieldsModule) {
		return t.Name()
	}
	return t.String()
}

// locate walks steps from root to a settable leaf, allocating nil pointers
// on the way. A slice step takes element elem, growing the slice to reach it.
func locate(root reflect.Value, steps []int, elem int) reflect.Value {
	v := root
	for _, s := range steps {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		if s == -1 {
			for v.Len() <= elem {
				v.Set(reflect.Append(v, reflect.Zero(v.Type().Elem())))
			}
			v = v.Index(elem)
			continue
		}
		v = v.Field(s)
	}
	return v
}

// importFieldsBase is when the document's own times are set: far from any
// clock the test reads, so a stamped time is never mistaken for one.
var importFieldsBase = time.Date(2025, 3, 14, 15, 9, 26, 0, time.UTC)

// importFieldsDocument is the document every probe starts from: a heading
// over a requirement over a test case, a link between the last two and one
// to an artifact outside the document, a figure, a product profile, an
// attribute definition and a linked artifact. Every field is set; a field
// this function does not name is filled by fillImportFields.
func importFieldsDocument(fields []importField) *ProjectExport {
	at := func(m int) time.Time { return importFieldsBase.Add(time.Duration(m) * time.Minute) }
	ptrTime := func(m int) *time.Time { t := at(m); return &t }
	ptr := func(s string) *string { return &s }
	org := "org-doc"
	doc := &ProjectExport{
		ExportedAt:   at(1),
		Version:      "1.0",
		ProjectID:    "project-doc",
		ProjectName:  "Imported name",
		ProjectDesc:  "Imported description",
		BaselineName: "Baseline doc",
		Artifacts: []*artifacts.Artifact{
			{ID: "a1", ProjectID: "project-doc", Type: "heading", Ref: "H-1", DocNumber: "1", Title: "Heading",
				Body: "Heading body", SortOrder: 3, Status: "approved",
				Attributes: map[string]interface{}{"status": "approved", "owner": "alice"},
				Version:    4, ValidFrom: at(2), ValidTo: ptrTime(3), CreatedAt: at(4), UpdatedAt: at(5)},
			{ID: "a2", ProjectID: "project-doc", ParentID: ptr("a1"), Type: "requirement", Ref: "REQ-7",
				DocNumber: "1.1", Title: "Requirement", Body: "The system shall import.", SortOrder: 5,
				Status: "approved", Attributes: map[string]interface{}{"status": "approved", "priority": "must"},
				Version: 6, ValidFrom: at(6), ValidTo: ptrTime(7), CreatedAt: at(8), UpdatedAt: at(9)},
			{ID: "a3", ProjectID: "project-doc", ParentID: ptr("a2"), Type: "test-case", Ref: "TC-2",
				DocNumber: "1.1.1", Title: "Test case", Body: "Import, then read back.", SortOrder: 7,
				Status: "approved", Attributes: map[string]interface{}{"status": "approved"},
				Version: 2, ValidFrom: at(10), ValidTo: ptrTime(11), CreatedAt: at(12), UpdatedAt: at(13)},
		},
		Links: []*links.Link{
			{ID: "l1", FromID: "a3", ToID: "a2", Type: "verifies", Suspect: true,
				Attributes: map[string]interface{}{"note": "link note"}, Version: 3,
				ValidFrom: at(14), ValidTo: ptrTime(15), CreatedAt: at(16), UpdatedAt: at(17)},
			{ID: "l2", FromID: "a2", ToID: "outside", Type: "derives-from", Suspect: true,
				Attributes: map[string]interface{}{"note": "dangling"}, Version: 2,
				ValidFrom: at(18), ValidTo: ptrTime(19), CreatedAt: at(20), UpdatedAt: at(21)},
		},
		Attachments: []*attachments.Attachment{
			{ID: "f1", ArtifactID: "a2", Filename: "REQ-7-FIG-1.png", OriginalFilename: "plot.png", Title: "Plot",
				MimeType: "image/png", FilePath: "uploads/f1.png", FileSize: 68, FigureRef: "REQ-7-FIG-1",
				FigureNum: 1, Version: 2, CreatedAt: at(22)},
		},
		ProductProfile: &products.ProductProfile{
			ProjectID: "project-doc", Vision: "Vision", ProblemStatement: "Problem", TargetUsers: "Users",
			Constraints:    []map[string]interface{}{{"text": "Constraint"}},
			SuccessMetrics: []map[string]interface{}{{"name": "Metric", "target": "1"}},
			Settings:       map[string]interface{}{"setting": "on"},
			CreatedAt:      at(23), UpdatedAt: at(24),
		},
		AttributeDefs: []*attributes.Definition{
			{ID: "d1", OrgID: &org, ProjectID: ptr("project-doc"), Key: "risk", Label: "Risk", DataType: "enum",
				EnumValues: []string{"low", "high"}, AppliesToType: "requirement", Required: true, SortOrder: 1,
				CreatedAt: at(25)},
		},
		LinkedArtifacts: []*LinkedArtifact{
			{ID: "x1", ProjectID: "project-other", ProjectName: "Other", Ref: "REQ-1", Type: "requirement",
				Title: "Foreign", Status: "draft"},
		},
	}
	fillImportFields(doc, fields)
	return doc
}

// fillImportFields gives every leaf still at its zero value a value of its
// own, derived from its path, in every element of every slice: a field a
// later change adds needs no edit to the document above.
func fillImportFields(doc *ProjectExport, fields []importField) {
	root := reflect.ValueOf(doc).Elem()
	for _, f := range fields {
		for i := 0; i < sliceElems(root, f.steps); i++ {
			v := locate(root, f.steps, i)
			if v.IsZero() {
				v.Set(distinctValue(v.Type(), f.path))
			}
		}
	}
}

// sliceElems is how many elements the first slice on a field's path holds
// (1 when there is none, or when it is empty and so gets one).
func sliceElems(root reflect.Value, steps []int) int {
	v := root
	for _, s := range steps {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return 1
			}
			v = v.Elem()
		}
		if s == -1 {
			return max(v.Len(), 1)
		}
		v = v.Field(s)
	}
	return 1
}

func pathHash(path string) int {
	h := fnv.New32a()
	h.Write([]byte(path))
	return int(h.Sum32() % 997)
}

// distinctValue is a value of type t that no other field holds: a string
// naming the path, a number, true, a time some minutes into the document's
// day, a map or a list holding the path.
func distinctValue(t reflect.Type, path string) reflect.Value {
	v := reflect.New(t).Elem()
	switch {
	case t == timeType:
		v.Set(reflect.ValueOf(importFieldsBase.Add(time.Duration(100+pathHash(path)) * time.Minute)))
		return v
	case t.Kind() == reflect.Ptr:
		v.Set(reflect.New(t.Elem()))
		v.Elem().Set(distinctValue(t.Elem(), path))
		return v
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString("s9:" + path)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(1000 + pathHash(path)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(1000 + pathHash(path)))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(1000+pathHash(path)) + 0.5)
	case reflect.Map:
		v.Set(reflect.MakeMap(t))
		v.SetMapIndex(distinctValue(t.Key(), "key"), distinctValue(elemOrString(t.Elem()), path))
	case reflect.Slice:
		v.Set(reflect.Append(v, distinctValue(elemOrString(t.Elem()), path)))
	case reflect.Interface:
		v.Set(reflect.ValueOf("s9:" + path))
	}
	return v
}

// elemOrString is t, or string where t is an interface a string fills.
func elemOrString(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Interface {
		return reflect.TypeOf("")
	}
	return t
}

// importFieldsVariants names how a field is changed where changing its
// value alone would say something other than what the import does with it.
// An id is renamed with every reference to it, since the import keys its
// relations by id and mints new ones; a relation moves to another artifact of
// the document; a type, a ref or a status takes another valid value.
var importFieldsVariants = map[string]func(doc *ProjectExport){
	"artifacts[].id": func(doc *ProjectExport) { renameArtifact(doc, "a2", "a2-renamed") },
	"links[].id":     func(doc *ProjectExport) { doc.Links[0].ID = "l1-renamed" },
	"artifacts[].parent_id": func(doc *ProjectExport) {
		id := "a1"
		doc.Artifacts[2].ParentID = &id
	},
	"links[].from_id":           func(doc *ProjectExport) { doc.Links[0].FromID = "a1" },
	"links[].to_id":             func(doc *ProjectExport) { doc.Links[0].ToID = "a1" },
	"links[].type":              func(doc *ProjectExport) { doc.Links[0].Type = "satisfies" },
	"artifacts[].type":          func(doc *ProjectExport) { doc.Artifacts[1].Type = "user-need" },
	"artifacts[].ref":           func(doc *ProjectExport) { doc.Artifacts[1].Ref = "REQ-9" },
	"artifacts[].status":        func(doc *ProjectExport) { doc.Artifacts[1].Status = "in_review" },
	"attachments[].artifact_id": func(doc *ProjectExport) { doc.Attachments[0].ArtifactID = "a3" },
	// A definition's key, data type and type take other valid values, so
	// what changes is the definition written rather than whether it is.
	"attribute_definitions[].key":             func(doc *ProjectExport) { doc.AttributeDefs[0].Key = "hazard_level" },
	"attribute_definitions[].data_type":       func(doc *ProjectExport) { doc.AttributeDefs[0].DataType = "text" },
	"attribute_definitions[].applies_to_type": func(doc *ProjectExport) { doc.AttributeDefs[0].AppliesToType = "test-case" },
}

// renameArtifact gives an artifact another id and follows it everywhere the
// document names it.
func renameArtifact(doc *ProjectExport, from, to string) {
	for _, a := range doc.Artifacts {
		if a.ID == from {
			a.ID = to
		}
		if a.ParentID != nil && *a.ParentID == from {
			id := to
			a.ParentID = &id
		}
	}
	for _, l := range doc.Links {
		if l.FromID == from {
			l.FromID = to
		}
		if l.ToID == from {
			l.ToID = to
		}
	}
	for _, f := range doc.Attachments {
		if f.ArtifactID == from {
			f.ArtifactID = to
		}
	}
}

// varyImportField changes one field of the document: the variant above, or
// for every other field the first element's value, by kind.
func varyImportField(doc *ProjectExport, f importField) {
	if fn, ok := importFieldsVariants[f.path]; ok {
		fn(doc)
		return
	}
	v := locate(reflect.ValueOf(doc).Elem(), f.steps, 0)
	v.Set(otherValue(v, f.path))
}

// importFieldsProbeKey is the key a map field gains when it is changed, so a
// map the import carries shows as carried to that key (artifact.attributes.s9-probe).
const importFieldsProbeKey = "s9-probe"

// otherValue is a value of v's type that differs from v: a string, a number
// or a time moved on, a flag flipped, a map with importFieldsProbeKey added,
// a list one longer.
func otherValue(v reflect.Value, path string) reflect.Value {
	t := v.Type()
	out := reflect.New(t).Elem()
	switch {
	case t == timeType:
		out.Set(reflect.ValueOf(v.Interface().(time.Time).Add(time.Hour)))
		return out
	case t.Kind() == reflect.Ptr:
		if v.IsNil() {
			return distinctValue(t, path)
		}
		out.Set(reflect.New(t.Elem()))
		out.Elem().Set(otherValue(v.Elem(), path))
		return out
	}
	switch t.Kind() {
	case reflect.String:
		out.SetString(v.String() + "~")
	case reflect.Bool:
		out.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		out.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		out.SetFloat(v.Float() + 1)
	case reflect.Map:
		out.Set(reflect.MakeMap(t))
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), iter.Value())
		}
		key := distinctValue(t.Key(), "s9-probe")
		if t.Key().Kind() == reflect.String {
			key = reflect.ValueOf(importFieldsProbeKey).Convert(t.Key())
		}
		out.SetMapIndex(key, distinctValue(elemOrString(t.Elem()), "s9-probe"))
	case reflect.Slice:
		out.Set(reflect.AppendSlice(reflect.MakeSlice(t, 0, v.Len()+1), v))
		out.Set(reflect.Append(out, distinctValue(elemOrString(t.Elem()), "s9-probe")))
	default:
		out.Set(distinctValue(t, path+"~"))
	}
	return out
}

// --- recording fakes: what an import writes ----------------------------------

// fieldsArtifactRepo stores what the real artifact service saves.
type fieldsArtifactRepo struct {
	artifacts.Repository
	saved []*artifacts.Artifact
}

func (r *fieldsArtifactRepo) Save(a *artifacts.Artifact) error {
	r.saved = append(r.saved, a)
	return nil
}

func (r *fieldsArtifactRepo) NextSortOrder(string, *string) (int, error) {
	return len(r.saved) + 1, nil
}

type fieldsLinkService struct {
	links.Service
	created []*links.Link
}

func (s *fieldsLinkService) CreateLink(l *links.Link) error {
	s.created = append(s.created, l)
	return nil
}

type fieldsProjectService struct {
	projects.Service
	created []*projects.Project
}

func (s *fieldsProjectService) CreateProject(p *projects.Project) error {
	s.created = append(s.created, p)
	return nil
}

type fieldsProductService struct {
	products.Service
	updates []products.UpdateProfileRequest
}

func (s *fieldsProductService) UpdateProfile(_ string, req products.UpdateProfileRequest) (*products.ProductProfile, error) {
	s.updates = append(s.updates, req)
	return &products.ProductProfile{}, nil
}

// fieldsAttributeRepo stores what the real attribute service saves; the
// workspace has no definitions of its own.
type fieldsAttributeRepo struct {
	attributes.Repository
	created []*attributes.Definition
}

func (r *fieldsAttributeRepo) Create(d *attributes.Definition) error {
	r.created = append(r.created, d)
	return nil
}

func (r *fieldsAttributeRepo) ListByOrg(string) ([]*attributes.Definition, error) { return nil, nil }
func (r *fieldsAttributeRepo) ListByProject(string) ([]*attributes.Definition, error) {
	return nil, nil
}

// importWrites is everything one import wrote, in the order it wrote it.
type importWrites struct {
	Project     []*projects.Project             `json:"project"`
	Profile     []products.UpdateProfileRequest `json:"profile"`
	Artifacts   []*artifacts.Artifact           `json:"artifact"`
	Links       []*links.Link                   `json:"link"`
	Definitions []*attributes.Definition        `json:"definition"`
}

var (
	importFieldsUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	importFieldsTime = regexp.MustCompile(`"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))"`)
)

// importDocument imports doc as ImportProject does and returns what was
// written, as JSON values: an id the import minted becomes the name of what
// it identifies (<project>, <artifact N>, <link N>, by the order of writing),
// and every time the import stamped (one within the call) becomes <now>, so
// two imports of documents that differ in one field compare equal unless the
// field reached the writes.
func importDocument(t *testing.T, doc *ProjectExport) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal the document: %v", err)
	}
	repo := &fieldsArtifactRepo{}
	linkSvc := &fieldsLinkService{}
	projSvc := &fieldsProjectService{}
	prodSvc := &fieldsProductService{}
	attrRepo := &fieldsAttributeRepo{}
	svc := NewService(artifacts.NewDefaultService(repo), linkSvc, nil, nil, projSvc)
	svc.SetProductService(prodSvc)
	svc.SetAttributeService(attributes.NewDefaultService(attrRepo, artifacts.ValidType))
	before := time.Now()
	if _, err := svc.ImportProject(raw, "org-import"); err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	after := time.Now()
	written, err := json.Marshal(importWrites{Project: projSvc.created, Profile: prodSvc.updates,
		Artifacts: repo.saved, Links: linkSvc.created, Definitions: attrRepo.created})
	if err != nil {
		t.Fatalf("marshal what the import wrote: %v", err)
	}
	ids := map[string]string{}
	for _, p := range projSvc.created {
		ids[p.ID] = "<project>"
	}
	for i, a := range repo.saved {
		ids[a.ID] = fmt.Sprintf("<artifact %d>", i+1)
	}
	for i, l := range linkSvc.created {
		ids[l.ID] = fmt.Sprintf("<link %d>", i+1)
	}
	for i, d := range attrRepo.created {
		ids[d.ID] = fmt.Sprintf("<definition %d>", i+1)
	}
	text := importFieldsUUID.ReplaceAllStringFunc(string(written), func(id string) string {
		if _, ok := ids[id]; !ok {
			ids[id] = fmt.Sprintf("<id %d>", len(ids)+1)
		}
		return ids[id]
	})
	text = importFieldsTime.ReplaceAllStringFunc(text, func(quoted string) string {
		ts, err := time.Parse(time.RFC3339Nano, strings.Trim(quoted, `"`))
		if err == nil && !ts.Before(before.Truncate(time.Second)) && !ts.After(after) {
			return `"<now>"`
		}
		return quoted
	})
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("read back what the import wrote: %v", err)
	}
	return out
}

// reached lists the written keys whose values differ between two imports:
// project.name, artifact.title, artifact.attributes.links_snapshot,
// link.type, definition.label. A written list that changed length is named
// by itself.
func reached(base, varied map[string]interface{}) []string {
	set := map[string]bool{}
	for _, kind := range []string{"project", "profile", "artifact", "link", "definition"} {
		b, _ := base[kind].([]interface{})
		v, _ := varied[kind].([]interface{})
		if len(b) != len(v) {
			set[kind+" (count)"] = true
		}
		for i := 0; i < min(len(b), len(v)); i++ {
			diffJSON(kind, b[i], v[i], set)
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffJSON records the paths at which two JSON values differ, descending
// through objects; a list is compared whole.
func diffJSON(path string, a, b interface{}, set map[string]bool) {
	am, aok := a.(map[string]interface{})
	bm, bok := b.(map[string]interface{})
	if !aok || !bok {
		if !reflect.DeepEqual(a, b) {
			set[path] = true
		}
		return
	}
	keys := map[string]bool{}
	for k := range am {
		keys[k] = true
	}
	for k := range bm {
		keys[k] = true
	}
	for k := range keys {
		diffJSON(path+"."+k, am[k], bm[k], set)
	}
}

// TestImportFields writes testdata/import_fields.txt: every field reachable
// from ProjectExport, carried or dropped by the JSON import, measured (see
// the top of this file).
func TestImportFields(t *testing.T) {
	var fields []importField
	walkImportFields(reflect.TypeOf(ProjectExport{}), "", nil, &fields)
	if len(fields) == 0 {
		t.Fatal("no fields reachable from ProjectExport")
	}
	base := importDocument(t, importFieldsDocument(fields))

	var b strings.Builder
	b.WriteString(importFieldsHeader)
	carried, dropped := 0, 0
	for _, f := range fields {
		doc := importFieldsDocument(fields)
		varyImportField(doc, f)
		reach := reached(base, importDocument(t, doc))
		class := "dropped"
		if len(reach) > 0 {
			class = "carried to " + strings.Join(reach, ", ")
			carried++
		} else {
			dropped++
		}
		fmt.Fprintf(&b, "%s (%s %s): %s\n", f.path, f.goName, f.goType, class)
	}
	fmt.Fprintf(&b, "# %d fields: %d carried, %d dropped\n", len(fields), carried, dropped)
	checkImportFieldsGolden(t, []byte(b.String()))
}

const importFieldsHeader = `# Every field reachable from exports.ProjectExport, as encoding/json reaches
# it, and what the JSON import (POST /api/v1/projects/import, ImportProject:
# a new project, refs kept) does with it (refactor plan S9, invariant I15).
# "carried to" names what changing the field changes in what the import
# writes: the project, its product profile, an artifact, a link or an
# attribute definition, by JSON key. "dropped" means the import writes the
# same whatever the field holds.
# Measured, not declared: TestImportFields imports a document with the one
# field changed and compares the writes, minted ids and stamped times aside.
# An id is renamed with its references, a relation moved to another artifact,
# and a map gains the key s9-probe.
# A new field fails the test until this file is regenerated, so the import
# mapper cannot drop it unnoticed. A refactor never changes this file; for a
# deliberate change regenerate it with
#   UPDATE_GOLDEN=1 go test ./internal/domain/exports -count=1 -run '^TestImportFields$'
`

// checkImportFieldsGolden compares got with the golden, or rewrites it under
// UPDATE_GOLDEN=1. A mismatch names each field that is new, gone or
// reclassified.
func checkImportFieldsGolden(t *testing.T, got []byte) {
	t.Helper()
	if os.Getenv(importFieldsUpdateEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(importFieldsGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(importFieldsGolden, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", importFieldsShown, err)
		}
		t.Logf("regenerated %s", importFieldsShown)
		return
	}
	want, err := os.ReadFile(importFieldsGolden)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with:\n  %s", importFieldsShown, err, importFieldsCmd)
	}
	if bytes.Equal(want, got) {
		return
	}
	wantLines, gotLines := fieldLines(string(want)), fieldLines(string(got))
	var msgs []string
	for path, line := range gotLines {
		old, ok := wantLines[path]
		switch {
		case !ok:
			msgs = append(msgs, "new field, not classified: "+line+
				"\n    the JSON import "+verb(line)+" it; if it should carry it over, map it in createProjectFromExport or importArtifactsAndLinks (export.go) first")
		case old != line:
			msgs = append(msgs, "reclassified:\n    was "+old+"\n    now "+line)
		}
	}
	for path, line := range wantLines {
		if _, ok := gotLines[path]; !ok {
			msgs = append(msgs, "field gone: "+line)
		}
	}
	sort.Strings(msgs)
	if len(msgs) == 0 {
		msgs = append(msgs, "the header, the order or the totals changed")
	}
	t.Errorf("%s does not match what the JSON import does:\n  %s\n"+
		"A refactor never changes this file. If the change is deliberate, regenerate it with:\n  %s\n"+
		"(only %s=1 regenerates; any other value compares)",
		importFieldsShown, strings.Join(msgs, "\n  "), importFieldsCmd, importFieldsUpdateEnv)
}

// fieldLines indexes the golden's field lines by their path.
func fieldLines(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path, _, _ := strings.Cut(line, " ")
		out[path] = line
	}
	return out
}

func verb(line string) string {
	if strings.HasSuffix(line, ": dropped") {
		return "drops"
	}
	return "carries"
}
