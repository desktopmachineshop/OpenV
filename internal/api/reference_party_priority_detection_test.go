package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// memSettingsStore keeps one project's settings in memory.
type memSettingsStore struct{ project map[string]interface{} }

func (m *memSettingsStore) OrgSettings(string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (m *memSettingsStore) SetOrgSettings(string, map[string]interface{}) error { return nil }
func (m *memSettingsStore) ProjectSettings(string) (map[string]interface{}, error) {
	return m.project, nil
}
func (m *memSettingsStore) SetProjectSettings(_ string, s map[string]interface{}) error {
	m.project = s
	return nil
}

func priorityDetectionRequest(t *testing.T, method, role, body string) (*Handler, *httptest.ResponseRecorder, *http.Request) {
	store := &memSettingsStore{project: map[string]interface{}{}}
	h := newTestHandler(t, func(h *Handler) {
		h.SettingsService = settings.NewService(store)
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "o1"}}}
		h.MemberService = &fakeMemberService{roles: map[string]map[string]string{"p1": {"u1": role}}}
	})
	r := httptest.NewRequest(method, "/api/v1/projects/p1/priority-detection", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = mux.SetURLVars(r, map[string]string{"id": "p1"})
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1"}))
	return h, httptest.NewRecorder(), r
}

// TestPriorityDetection: off by default for a viewer, an editor turns it on,
// and a viewer cannot change it.
func TestPriorityDetection(t *testing.T) {
	h, w, r := priorityDetectionRequest(t, http.MethodGet, "viewer", "")
	h.GetProjectPriorityDetection(w, r)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"enabled":false}` {
		t.Fatalf("GET = %d %s", w.Code, w.Body.String())
	}

	h, w, r = priorityDetectionRequest(t, http.MethodPut, "editor", `{"enabled":true}`)
	h.UpdateProjectPriorityDetection(w, r)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"enabled":true}` {
		t.Fatalf("PUT as editor = %d %s", w.Code, w.Body.String())
	}
	if on, _ := h.SettingsService.ProjectPriorityDetection("p1"); !on {
		t.Fatalf("not stored")
	}

	h, w, r = priorityDetectionRequest(t, http.MethodPut, "viewer", `{"enabled":true}`)
	h.UpdateProjectPriorityDetection(w, r)
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("PUT as viewer = %d, want refused", w.Code)
	}
}
