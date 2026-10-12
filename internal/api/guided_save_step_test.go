package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// savingGuidedService serves SaveGuidedStep: one session, and a record of
// each step it was asked to save. A session that is not in progress is
// refused as the real service refuses it.
type savingGuidedService struct {
	guided.Service
	session *guided.Session
	saved   []int
}

func (f *savingGuidedService) GetSession(string) (*guided.Session, error) { return f.session, nil }

func (f *savingGuidedService) SaveStep(_ string, step int, _ map[string]interface{}) (*guided.Session, error) {
	if f.session.Status != guided.StatusInProgress {
		return nil, guided.ErrSessionNotEditable
	}
	f.saved = append(f.saved, step)
	s := *f.session
	s.CurrentStep = step
	return &s, nil
}

// TestSaveGuidedStepRejectsAStepTheWizardDoesNotHave is the regression test
// for #645: PUT /guided-sessions/{id}/step stored any step number, so the
// session's current step could be 0, negative or past the last step, one the
// wizard cannot render. The range is the wizard's steps as the server defines
// them (guidedStepLabels): 1 to the last answers as before, anything outside
// it 400 naming the range and stores nothing, and a session no longer in
// progress keeps its own refusal.
func TestSaveGuidedStepRejectsAStepTheWizardDoesNotHave(t *testing.T) {
	last := len(guidedStepLabels)
	newFixture := func(status string) (*Handler, *savingGuidedService) {
		svc := &savingGuidedService{session: &guided.Session{ID: "gs-1", ProjectID: "proj-1", Status: status}}
		h := newTestHandler(t, func(h *Handler) {
			h.GuidedService = svc
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{"proj-1": {"editor-1": members.RoleEditor}}}
		})
		return h, svc
	}
	save := func(h *Handler, step int) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"step":%d,"answers":{"step_1":{"vision":"v"}}}`, step)
		r := httptest.NewRequest(http.MethodPut, "/api/v1/guided-sessions/gs-1/step", strings.NewReader(body))
		r = mux.SetURLVars(r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor-1"})),
			map[string]string{"id": "gs-1"})
		w := httptest.NewRecorder()
		h.SaveGuidedStep(w, r)
		return w
	}

	for _, step := range []int{1, 2, last - 1, last} {
		t.Run(fmt.Sprintf("step %d is saved", step), func(t *testing.T) {
			h, svc := newFixture(guided.StatusInProgress)
			if w := save(h, step); w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
			if len(svc.saved) != 1 || svc.saved[0] != step {
				t.Errorf("saved %v, want [%d]", svc.saved, step)
			}
		})
	}

	wantMessage := fmt.Sprintf("step must be between 1 and %d", last)
	for _, step := range []int{0, -1, last + 1, 99} {
		t.Run(fmt.Sprintf("step %d is refused", step), func(t *testing.T) {
			h, svc := newFixture(guided.StatusInProgress)
			w := save(h, step)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), wantMessage) {
				t.Errorf("body %q does not name the range (%s)", w.Body.String(), wantMessage)
			}
			if len(svc.saved) != 0 {
				t.Errorf("a refused step was saved: %v", svc.saved)
			}
		})
	}

	t.Run("a session that is not in progress keeps its refusal", func(t *testing.T) {
		h, _ := newFixture(guided.StatusCommitted)
		w := save(h, 1)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), guided.ErrSessionNotEditable.Error()) {
			t.Fatalf("%d %q, want 400 %q", w.Code, w.Body.String(), guided.ErrSessionNotEditable.Error())
		}
	})
}
