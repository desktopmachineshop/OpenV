package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeProjectSessionLister embeds the interface so only the project-wide
// session listing needs an implementation.
type fakeProjectSessionLister struct {
	interviews.Service
	sessions   []*interviews.Session
	calls      int
	gotProject string
	gotLimit   int
}

func (f *fakeProjectSessionLister) ListProjectSessions(projectID string, limit int) ([]*interviews.Session, error) {
	f.calls++
	f.gotProject = projectID
	f.gotLimit = limit
	return f.sessions, nil
}

func newProjectSessionsHandler(fake *fakeProjectSessionLister) *Handler {
	return &Handler{
		interviewService: fake,
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"viewer": members.RoleViewer},
		}},
	}
}

func getProjectSessions(h *Handler, userID, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-1/interview-sessions"+query, nil)
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	r = mux.SetURLVars(r, map[string]string{"id": "proj-1"})
	w := httptest.NewRecorder()
	h.ListProjectInterviewSessions(w, r)
	return w
}

// TestListProjectInterviewSessions locks in the endpoint's authz (viewer role
// on the project) and response shape (a JSON array of session objects, never
// null), plus the limit plumbing to the domain service.
func TestListProjectInterviewSessions(t *testing.T) {
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	sessions := []*interviews.Session{
		{ID: "sess-2", InterviewID: "int-2", InviteID: "inv-2", ParticipantName: "Bea", Status: interviews.SessionStatusActive, StartedAt: started.Add(time.Hour)},
		{ID: "sess-1", InterviewID: "int-1", InviteID: "inv-1", ParticipantName: "Ada", Status: interviews.SessionStatusCompleted, StartedAt: started},
	}

	t.Run("viewer gets sessions across interviews", func(t *testing.T) {
		fake := &fakeProjectSessionLister{sessions: sessions}
		w := getProjectSessions(newProjectSessionsHandler(fake), "viewer", "?limit=7")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if fake.gotProject != "proj-1" {
			t.Errorf("service got project %q, want proj-1", fake.gotProject)
		}
		if fake.gotLimit != 7 {
			t.Errorf("service got limit %d, want 7", fake.gotLimit)
		}
		var got []struct {
			ID              string `json:"id"`
			InterviewID     string `json:"interview_id"`
			ParticipantName string `json:"participant_name"`
			Status          string `json:"status"`
			StartedAt       string `json:"started_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding response: %v (body %q)", err, w.Body.String())
		}
		if len(got) != 2 {
			t.Fatalf("got %d sessions, want 2", len(got))
		}
		if got[0].ID != "sess-2" || got[0].InterviewID != "int-2" || got[0].ParticipantName != "Bea" {
			t.Errorf("first session = %+v, want sess-2/int-2/Bea", got[0])
		}
		if got[1].StartedAt == "" || got[1].Status != interviews.SessionStatusCompleted {
			t.Errorf("second session = %+v, want a started_at stamp and completed status", got[1])
		}
	})

	t.Run("missing limit defaults to zero for the service to fill", func(t *testing.T) {
		fake := &fakeProjectSessionLister{}
		if w := getProjectSessions(newProjectSessionsHandler(fake), "viewer", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if fake.gotLimit != 0 {
			t.Errorf("service got limit %d, want 0 (service applies the default)", fake.gotLimit)
		}
	})

	t.Run("empty result encodes as an array, not null", func(t *testing.T) {
		fake := &fakeProjectSessionLister{}
		w := getProjectSessions(newProjectSessionsHandler(fake), "viewer", "")
		if body := w.Body.String(); body != "[]\n" {
			t.Fatalf("body = %q, want an empty JSON array", body)
		}
	})

	t.Run("non-integer limit gets 400", func(t *testing.T) {
		fake := &fakeProjectSessionLister{}
		w := getProjectSessions(newProjectSessionsHandler(fake), "viewer", "?limit=lots")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if fake.calls != 0 {
			t.Fatalf("service called %d times on a rejected request, want 0", fake.calls)
		}
	})

	t.Run("non-member gets 403", func(t *testing.T) {
		fake := &fakeProjectSessionLister{sessions: sessions}
		w := getProjectSessions(newProjectSessionsHandler(fake), "stranger", "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if fake.calls != 0 {
			t.Fatalf("service called %d times on a denied request, want 0", fake.calls)
		}
	})

	t.Run("unauthenticated gets 401", func(t *testing.T) {
		fake := &fakeProjectSessionLister{sessions: sessions}
		w := getProjectSessions(newProjectSessionsHandler(fake), "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
		}
		if fake.calls != 0 {
			t.Fatalf("service called %d times on an unauthenticated request, want 0", fake.calls)
		}
	})
}
