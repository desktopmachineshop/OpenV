package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// createdProjects is a project store that keeps what the service creates.
type createdProjects struct {
	mapProjects
	created []*projects.Project
}

func (c *createdProjects) Create(p *projects.Project) error {
	c.created = append(c.created, p)
	return nil
}

// failingProjects is a project store whose create fails as a database would.
type failingProjects struct{ mapProjects }

func (failingProjects) Create(*projects.Project) error { return errors.New("connection refused") }

// TestAnUnknownAgentAuthAnswers400 is the regression test for #643: creating
// or updating a project with an agent_auth other than user-account or
// api-key answered 500, as a server fault, because the service's plain error
// reached respondInternal. It now answers 400 with a message that names the
// field and the values it takes, and stores nothing. A store failure still
// answers 500.
func TestAnUnknownAgentAuthAnswers400(t *testing.T) {
	const want = `agent_auth must be \"user-account\" or \"api-key\"`
	newHandler := func(store projects.Repository) *Handler {
		return newTestHandler(t, withProjectService(projects.NewService(store)))
	}
	assertRefused := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("body %q does not name agent_auth and its values (%s)", w.Body.String(), want)
		}
	}

	t.Run("create", func(t *testing.T) {
		store := &createdProjects{mapProjects: mapProjects{}}
		w := httptest.NewRecorder()
		newHandler(store).CreateProject(w, agentReq(http.MethodPost, "/api/v1/projects", nil, `{"name":"New","agent_auth":"bogus"}`))
		assertRefused(t, w)
		if len(store.created) != 0 {
			t.Errorf("a refused create stored %d projects", len(store.created))
		}
	})

	t.Run("create with a mode it takes", func(t *testing.T) {
		for _, mode := range []string{"", "user-account", "api-key"} {
			store := &createdProjects{mapProjects: mapProjects{}}
			w := httptest.NewRecorder()
			newHandler(store).CreateProject(w, agentReq(http.MethodPost, "/api/v1/projects", nil, `{"name":"New","agent_auth":"`+mode+`"}`))
			if w.Code != http.StatusCreated || len(store.created) != 1 {
				t.Errorf("agent_auth %q: status = %d, created %d; want 201 and one project (body %q)", mode, w.Code, len(store.created), w.Body.String())
			}
		}
	})

	t.Run("update", func(t *testing.T) {
		store := mapProjects{"proj-1": {ID: "proj-1", OrgID: "org-1", Name: "Keep", AgentAuth: projects.AgentAuthUserAccount}}
		w := httptest.NewRecorder()
		newHandler(store).UpdateProject(w, agentReq(http.MethodPut, "/api/v1/projects/proj-1",
			map[string]string{"id": "proj-1"}, `{"name":"Renamed","agent_auth":"bogus"}`))
		assertRefused(t, w)
		if got := store["proj-1"]; got.Name != "Keep" || got.AgentAuth != projects.AgentAuthUserAccount {
			t.Errorf("stored project = %+v, want it unchanged", got)
		}
	})

	t.Run("a store failure on create is still a 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		newHandler(failingProjects{mapProjects{}}).CreateProject(w,
			agentReq(http.MethodPost, "/api/v1/projects", nil, `{"name":"New"}`))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
		}
	})
}
