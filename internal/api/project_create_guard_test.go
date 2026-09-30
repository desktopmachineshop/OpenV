package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/templates"
)

// createImports imports into the fixture's project store, as the export
// service's imports create a project, so that the project count sees them.
type createImports struct {
	exports.Service
	store *fakeProjectService
}

func (f *createImports) create(orgID string) string {
	id := fmt.Sprintf("proj-imported-%d", len(f.store.created)+1)
	_ = f.store.CreateProject(&projects.Project{ID: id, OrgID: orgID})
	return id
}

func (f *createImports) ImportProject(data []byte, orgID string) (string, error) {
	return f.create(orgID), nil
}

func (f *createImports) ImportProjectWithOverrides(data []byte, name, desc, orgID string) (string, error) {
	return f.create(orgID), nil
}

// projectCreateFixture is workspace org-1 holding the projects held (named
// proj-1 to proj-<held>), where "owner" and "member" are members, behind the
// registered routes; the template tpl-1 is the workspace's own.
func projectCreateFixture(held int) (http.Handler, *fakeProjectService, *fakeOrgService) {
	store := &fakeProjectService{byID: map[string]*projects.Project{}}
	for i := 1; i <= held; i++ {
		id := fmt.Sprintf("proj-%d", i)
		store.byID[id] = &projects.Project{ID: id, OrgID: "org-1"}
	}
	imports := &createImports{store: store}
	org := &fakeOrgService{roles: map[string]map[string]string{"org-1": {"owner": orgs.RoleAdmin, "member": orgs.RoleMember}}}
	h := NewHandler(HandlerDeps{
		ProjectService: store,
		ExportService:  imports,
		TemplateService: templates.NewService(&templateStore{byID: map[string]*templates.Template{
			"tpl-1": {ID: "tpl-1", OrgID: "org-1", Snapshot: []byte(`{}`)},
		}}, imports),
		OrgService:    org,
		MemberService: &fakeMemberService{},
	})
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router, store, org
}

// projectCreates are the three routes that create a project, each with a
// well-formed body.
var projectCreates = []struct{ route, path, body string }{
	{"POST /api/v1/projects", "/api/v1/projects", `{"name":"New"}`},
	{"POST /api/v1/templates/{id}/projects", "/api/v1/templates/tpl-1/projects", `{"name":"From the template"}`},
	{"POST /api/v1/projects/import", "/api/v1/projects/import",
		`{"project_name":"Imported","artifacts":[{"id":"r","type":"requirement","title":"R","body":"The system shall."}]}`},
}

func createAs(router http.Handler, path, body string, ctx func(context.Context) context.Context) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r.WithContext(ctx(r.Context())))
	return w
}

// TestRunnerKeysCreateNoProject is the regression test for a runner key
// that created projects: POST /projects asked a caller for nothing but an
// active workspace and that it was no run, so a workspace's runner key made
// a project in it that no one owned, since only a person is made the
// creator-owner; the template and import routes answered a key the 401 of a
// request with no person behind it. Every project create now refuses a
// runner key, a workspace's or a member's own, with 403 before it reads the
// body, and creates nothing; a person still creates the project and owns it.
func TestRunnerKeysCreateNoProject(t *testing.T) {
	const refused = `{"error":"runner keys cannot create projects"}`
	keys := []struct {
		name string
		ctx  func(context.Context) context.Context
	}{
		{"a workspace runner key", func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxWorkerOrg, "org-1")
		}},
		{"a member's personal runner key", func(ctx context.Context) context.Context {
			return context.WithValue(context.WithValue(ctx, ctxWorkerOrg, "org-1"), ctxWorkerUser, "member")
		}},
	}
	for _, c := range projectCreates {
		for _, key := range keys {
			t.Run(c.route+" by "+key.name, func(t *testing.T) {
				router, store, _ := projectCreateFixture(1)
				w := createAs(router, c.path, c.body, key.ctx)
				if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != refused {
					t.Fatalf("status = %d, body %q: want 403 %s", w.Code, w.Body.String(), refused)
				}
				if len(store.created) != 0 {
					t.Fatalf("a runner key created %d projects", len(store.created))
				}
			})
		}
		t.Run(c.route+" by a member, who owns the project", func(t *testing.T) {
			router, store, _ := projectCreateFixture(1)
			w := createAs(router, c.path, c.body, asPerson("member", "org-1"))
			if w.Code != http.StatusCreated || len(store.created) != 1 {
				t.Fatalf("status = %d, created %d: want 201 and one project (body %q)", w.Code, len(store.created), w.Body.String())
			}
		})
	}
}

// TestEveryProjectCreateTakesTheGateAndTheCount is the regression test for
// the project creates that asked neither the read-only plan gate nor the
// project maximum (quirk Q13): only POST /projects counted, and none asked
// the gate, so a workspace over its seats took new projects, and a template's
// project and an import passed the maximum and took the workspace over it
// (REQ-176, REQ-177). All three now ask the gate of the workspace they create
// in and count toward its maximum before they create anything; an import's
// route stays always writable (REQ-177), so the gate passes it and the
// count alone can refuse it.
func TestEveryProjectCreateTakesTheGateAndTheCount(t *testing.T) {
	t.Cleanup(func() { orgs.SetDeploymentLimits(nil) })
	errorCode := func(w *httptest.ResponseRecorder) string {
		var body struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body.Code
	}

	for _, c := range projectCreates {
		t.Run(c.route+" at the project maximum", func(t *testing.T) {
			orgs.SetDeploymentLimits(map[string]interface{}{orgs.LimitMaxProjects: 2})
			router, store, _ := projectCreateFixture(2)
			w := createAs(router, c.path, c.body, asPerson("owner", "org-1"))
			if w.Code != http.StatusForbidden || errorCode(w) != ErrCodeLimitReached {
				t.Fatalf("status = %d, body %q: want 403 limit_reached", w.Code, w.Body.String())
			}
			if len(store.created) != 0 {
				t.Fatalf("created %d projects past the maximum", len(store.created))
			}
		})

		t.Run(c.route+" under the project maximum", func(t *testing.T) {
			orgs.SetDeploymentLimits(map[string]interface{}{orgs.LimitMaxProjects: 2})
			router, store, _ := projectCreateFixture(1)
			w := createAs(router, c.path, c.body, asPerson("owner", "org-1"))
			if w.Code != http.StatusCreated || len(store.created) != 1 || store.created[0].OrgID != "org-1" {
				t.Fatalf("status = %d, created %d: want 201 and one project in org-1 (body %q)", w.Code, len(store.created), w.Body.String())
			}
		})

		t.Run(c.route+" on a workspace over its seats", func(t *testing.T) {
			orgs.SetDeploymentLimits(nil)
			enforceTiers(t)
			router, store, org := projectCreateFixture(1)
			org.roles["org-1"]["third"] = orgs.RoleMember // three members on the free tier's two seats
			w := createAs(router, c.path, c.body, asPerson("owner", "org-1"))
			if c.route == "POST /api/v1/projects/import" {
				// Always writable (REQ-177), and under the maximum.
				if w.Code != http.StatusCreated || len(store.created) != 1 {
					t.Fatalf("status = %d, created %d: want the import's 201 (body %q)", w.Code, len(store.created), w.Body.String())
				}
				return
			}
			if w.Code != http.StatusForbidden || errorCode(w) != ErrCodePlanReadOnly {
				t.Fatalf("status = %d, body %q: want 403 plan_read_only", w.Code, w.Body.String())
			}
			if len(store.created) != 0 {
				t.Fatalf("a read-only workspace took %d projects", len(store.created))
			}
		})

		t.Run(c.route+" on a workspace over its project maximum", func(t *testing.T) {
			orgs.SetDeploymentLimits(map[string]interface{}{orgs.LimitMaxProjects: 2})
			router, store, _ := projectCreateFixture(3)
			w := createAs(router, c.path, c.body, asPerson("owner", "org-1"))
			want := ErrCodePlanReadOnly
			if c.route == "POST /api/v1/projects/import" {
				want = ErrCodeLimitReached // past the gate, not past the count
			}
			if w.Code != http.StatusForbidden || errorCode(w) != want {
				t.Fatalf("status = %d, body %q: want 403 %s", w.Code, w.Body.String(), want)
			}
			if len(store.created) != 0 {
				t.Fatalf("created %d projects over the maximum", len(store.created))
			}
		})
	}
}

// TestARunListsItsOwnProjectAlone is the regression test for a run's token
// that listed every project of its workspace: ListProjects filtered by the
// signed-in person, and a run has none, so GET /projects answered a run the
// projects whose own routes refuse it as not scoped to them. A run's token
// now lists the one project it acts in, and a run with no project an empty
// list; a person's list is as it was.
func TestARunListsItsOwnProjectAlone(t *testing.T) {
	h := NewHandler(HandlerDeps{
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1":     {ID: "proj-1", OrgID: "org-1"},
			"proj-2":     {ID: "proj-2", OrgID: "org-1"},
			"proj-other": {ID: "proj-other", OrgID: "org-2"},
		}},
		OrgService:    &fakeOrgService{roles: map[string]map[string]string{"org-1": {"owner": orgs.RoleAdmin, "editor": orgs.RoleMember}}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{"proj-1": {"editor": members.RoleEditor}}},
	})
	list := func(ctx func(context.Context) context.Context) (string, []string) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		w := httptest.NewRecorder()
		h.ListProjects(w, r.WithContext(ctx(r.Context())))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var got []projects.Project
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode %q: %v", w.Body.String(), err)
		}
		ids := make([]string, 0, len(got))
		for _, p := range got {
			ids = append(ids, p.ID)
		}
		return strings.TrimSpace(w.Body.String()), ids
	}
	run := func(projectID *string) func(context.Context) context.Context {
		return func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: "run-1", OrgID: "org-1", AgentID: "agent-1", ProjectID: projectID})
		}
	}
	p1 := "proj-1"

	if _, ids := list(run(&p1)); len(ids) != 1 || ids[0] != "proj-1" {
		t.Fatalf("a run of proj-1 lists %v, want [proj-1]", ids)
	}
	if body, ids := list(run(nil)); len(ids) != 0 || body != "[]" {
		t.Fatalf("a run with no project lists %q, want []", body)
	}
	if _, ids := list(asPerson("editor", "org-1")); len(ids) != 1 || ids[0] != "proj-1" {
		t.Fatalf("the editor lists %v, want [proj-1]", ids)
	}
	if _, ids := list(asPerson("owner", "org-1")); len(ids) != 2 {
		t.Fatalf("the workspace admin lists %v, want both of org-1's projects", ids)
	}
}
