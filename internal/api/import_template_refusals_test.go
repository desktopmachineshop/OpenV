package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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
