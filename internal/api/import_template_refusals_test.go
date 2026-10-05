package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/templates"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// An import whose JSON does not parse is the client's to fix, as a
// malformed ReqIF already was (REQ-71): it answered 500 "failed to import
// project". The real export service parses before it writes anything, so
// the handler is tested over it.
func TestImportProjectWhoseJSONDoesNotParseAnswers400(t *testing.T) {
	h := NewHandler(HandlerDeps{ExportService: exports.NewService(nil, nil, nil, nil, nil)})

	for _, body := range []string{`{`, `{"project_name":`, `{"artifacts":5}`} {
		w := httptest.NewRecorder()
		h.ImportProject(w, importRequest(t, body, "", true))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("import of %s: %d %q, want 400", body, w.Code, w.Body.String())
		}
		if !strings.HasPrefix(w.Body.String(), `{"error":"failed to import JSON: malformed JSON: `) {
			t.Fatalf("import of %s: %q, want the parser's reason", body, w.Body.String())
		}
	}
}

// goTypeText is what encoding/json's own messages name of the program: a Go
// value or struct field, a method such as Time.UnmarshalJSON, or a package
// qualified type such as exports.ProjectExport or artifacts.Artifact.
var goTypeText = regexp.MustCompile(`Go (value|struct)|UnmarshalJSON|\b[a-z][a-z0-9]*\.[A-Z][A-Za-z0-9]*`)

// A JSON import that is not an OpenV export is refused in the terms of the
// file, never of the Go types it is read into (#379 bug 184): the reason
// was encoding/json's, which named them ("Go value of type
// exports.ProjectExport", "Go struct field ProjectExport.linked_artifacts of
// type []*exports.LinkedArtifact"), so it read as the program's internals
// and changed with the program's layout. A value of the wrong kind now
// names its field, as the file spells it, and what it is; a syntax error,
// and a time that does not parse, keep the parser's text, which names no
// Go type (the S5 tour pins the truncated document's).
func TestAMalformedImportNamesNoGoType(t *testing.T) {
	h := NewHandler(HandlerDeps{ExportService: exports.NewService(nil, nil, nil, nil, nil)})

	const prefix = "failed to import JSON: malformed JSON: "
	for _, tc := range []struct{ body, want string }{
		{`[]`, `json: cannot unmarshal array into a project export`},
		{`"x"`, `json: cannot unmarshal string into a project export`},
		{`{"linked_artifacts":"x"}`, `json: cannot unmarshal string into field "linked_artifacts"`},
		{`{"linked_artifacts":[1]}`, `json: cannot unmarshal number into field "linked_artifacts"`},
		{`{"artifacts":5}`, `json: cannot unmarshal number into field "artifacts"`},
		{`{"artifacts":[{"title":5}]}`, `json: cannot unmarshal number into field "artifacts.title"`},
		{`{"exported_at":5}`, `json: a date and time must be a JSON string`},
		{`{"exported_at":"soon"}`, `parsing time "soon" as "2006-01-02T15:04:05Z07:00": cannot parse "soon" as "2006"`},
		{`{`, `unexpected end of JSON input`},
		{`{"project_name":`, `unexpected end of JSON input`},
		{`{"project_name" "x"}`, `invalid character '"' after object key`},
		{`x`, `invalid character 'x' looking for beginning of value`},
	} {
		w := httptest.NewRecorder()
		h.ImportProject(w, importRequest(t, tc.body, "", true))
		var got struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusBadRequest {
			t.Errorf("import of %s: %d %q, want 400 with an error", tc.body, w.Code, w.Body.String())
			continue
		}
		if got.Error != prefix+tc.want {
			t.Errorf("import of %s answers\n  %q\nwant\n  %q", tc.body, got.Error, prefix+tc.want)
		}
		if m := goTypeText.FindString(w.Body.String()); m != "" {
			t.Errorf("import of %s names %q of the program: %s", tc.body, m, w.Body.String())
		}
	}
}

// templateStore is an in-memory templates.Repository.
type templateStore struct {
	templates.Repository
	byID map[string]*templates.Template
}

func (s *templateStore) GetByID(id string) (*templates.Template, error) {
	if tpl, ok := s.byID[id]; ok {
		return tpl, nil
	}
	return nil, templates.ErrNotFound
}

// templateImports records the projects a template's snapshot was imported
// into.
type templateImports struct {
	exports.Service
	orgs []string
}

func (f *templateImports) ImportProjectWithOverrides(data []byte, name, desc, orgID string) (string, error) {
	f.orgs = append(f.orgs, orgID)
	return "proj-new", nil
}

// A project from a template no row has answered 500 "failed to create
// project from template". It now answers 404, and so does another
// workspace's template, which the caller's workspace does not list and whose
// content a project made from it would copy out (REQ-72: a workspace
// template is visible only inside its workspace). A built-in and the
// workspace's own template still make a project.
func TestCreateProjectFromATemplateTheWorkspaceCannotSeeAnswers404(t *testing.T) {
	store := &templateStore{byID: map[string]*templates.Template{
		"tpl-builtin": {ID: "tpl-builtin", Snapshot: []byte(`{}`)},
		"tpl-own":     {ID: "tpl-own", OrgID: "org-1", Snapshot: []byte(`{}`)},
		"tpl-other":   {ID: "tpl-other", OrgID: "org-2", Snapshot: []byte(`{}`)},
	}}
	imports := &templateImports{}
	h := NewHandler(HandlerDeps{
		TemplateService: templates.NewService(store, imports),
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-new": {ID: "proj-new", OrgID: "org-1"},
		}},
	})
	create := func(templateID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"From a template"}`))
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1"})
		ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
		w := httptest.NewRecorder()
		h.CreateProjectFromTemplate(w, mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": templateID}))
		return w
	}

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "tpl-other"} {
		w := create(id)
		if w.Code != http.StatusNotFound || w.Body.String() != `{"error":"template not found"}`+"\n" {
			t.Fatalf("template %s: %d %q, want 404 template not found", id, w.Code, w.Body.String())
		}
	}
	if len(imports.orgs) != 0 {
		t.Fatalf("a refused template was imported into %v", imports.orgs)
	}
	for _, id := range []string{"tpl-builtin", "tpl-own"} {
		if w := create(id); w.Code != http.StatusCreated {
			t.Fatalf("template %s: %d %q, want 201", id, w.Code, w.Body.String())
		}
	}
	if len(imports.orgs) != 2 || imports.orgs[0] != "org-1" || imports.orgs[1] != "org-1" {
		t.Fatalf("imports went to %v, want the caller's workspace twice", imports.orgs)
	}
}
