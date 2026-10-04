package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestRunTokensCreateNoProject is the regression test for an agent run's
// token that created projects: CreateProject asked for nothing but an active
// workspace, so the token of a run in a project answered 201 with a new
// project in the run's workspace that no one owned, a proposal-mode run's
// token too, whose writes are meant to wait for a person's review, though a
// run acts only inside its own project (OpenV REQ-42, REQ-21). Every route
// that creates a project refuses a run token with 403 before it reads the
// body and creates nothing; the template and import routes, which answered
// it the 401 they give a request with no person behind it, give it the same
// 403. A person still creates a project and becomes its owner.
func TestRunTokensCreateNoProject(t *testing.T) {
	const refused = `{"error":"agent runs cannot create projects"}`
	project := "p1" // copyHandler's p1 is in o1
	runs := []struct {
		name string
		run  *agentruns.Run
	}{
		{"a direct-mode run's token", &agentruns.Run{ID: "run-1", OrgID: "o1", AgentID: "agent-direct", ProjectID: &project}},
		{"a proposal-mode run's token", &agentruns.Run{ID: "run-2", OrgID: "o1", AgentID: "agent-proposal", ProjectID: &project}},
		{"the token of a run with no project", &agentruns.Run{ID: "run-3", OrgID: "o1"}},
	}
	routes := []struct {
		name   string
		create func(h *Handler, w http.ResponseWriter, r *http.Request)
		body   string
	}{
		{"POST /api/v1/projects", (*Handler).CreateProject, `{"name":"Elsewhere"}`},
		{"POST /api/v1/templates/{id}/projects", (*Handler).CreateProjectFromTemplate, `{"name":"Elsewhere"}`},
		{"POST /api/v1/projects/import", (*Handler).ImportProject,
			`{"project_name":"Elsewhere","artifacts":[{"id":"r","type":"requirement","title":"R","body":"The system shall."}]}`},
	}
	request := func(body string, ctx func(context.Context) context.Context) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r = mux.SetURLVars(r, map[string]string{"id": "tour-template"})
		return r.WithContext(ctx(r.Context()))
	}

	for _, route := range routes {
		for _, tc := range runs {
			t.Run(route.name+" by "+tc.name, func(t *testing.T) {
				h, _ := copyHandler(t)
				store := h.ProjectService.(*fakeProjectService)
				w := httptest.NewRecorder()
				route.create(h, w, request(route.body, func(ctx context.Context) context.Context {
					return context.WithValue(ctx, ctxRun, tc.run)
				}))
				if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != refused {
					t.Fatalf("status = %d, body %q: want 403 %s", w.Code, w.Body.String(), refused)
				}
				if len(store.created) != 0 {
					t.Fatalf("a run's token created %d projects, want none", len(store.created))
				}
			})
		}
	}

	t.Run("a person creates a project and owns it", func(t *testing.T) {
		h, _ := copyHandler(t)
		store := h.ProjectService.(*fakeProjectService)
		w := httptest.NewRecorder()
		h.CreateProject(w, request(`{"name":"Mine"}`, func(ctx context.Context) context.Context {
			ctx = context.WithValue(ctx, ctxUser, &users.User{ID: "u1"})
			return context.WithValue(ctx, ctxActiveOrg, "o1")
		}))
		if w.Code != http.StatusCreated || len(store.created) != 1 {
			t.Fatalf("status = %d, created %d: want 201 and one project (body %q)", w.Code, len(store.created), w.Body.String())
		}
		created := store.created[0]
		if created.OrgID != "o1" {
			t.Fatalf("project created in %q, want the active workspace o1", created.OrgID)
		}
		if role, _ := h.MemberService.EffectiveRole(created.ID, "u1"); role != members.RoleOwner {
			t.Fatalf("creator's role = %q, want %q", role, members.RoleOwner)
		}
	})
}
