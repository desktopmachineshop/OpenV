package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// fakeGuidedNudgeService serves the nudge handler: one session, and a record
// of what was parked on it.
type fakeGuidedNudgeService struct {
	guided.Service
	session *guided.Session
	parked  []*guided.PendingNudge
}

func (f *fakeGuidedNudgeService) GetSession(id string) (*guided.Session, error) {
	return f.session, nil
}

func (f *fakeGuidedNudgeService) SetPendingNudge(sessionID string, nudge *guided.PendingNudge) error {
	f.parked = append(f.parked, nudge)
	return nil
}

// No runner key has ever been used, so the wizard is told turns are queuing
// unanswered — irrelevant to coalescing, and it keeps the fixture small.
type fakeWorkerKeyService struct {
	workerkeys.Service
}

func (f *fakeWorkerKeyService) List(orgID string) ([]*workerkeys.Key, error) { return nil, nil }

func nudgeFixture(runStatus string) (*Handler, *fakeGuidedNudgeService, *fakeRunService) {
	runID := "run-1"
	session := &guided.Session{ID: "gs-1", ProjectID: "proj-1", Status: guided.StatusInProgress}
	if runStatus != "" {
		session.AgentRunID = &runID
	}
	guidedSvc := &fakeGuidedNudgeService{session: session}
	runSvc := &fakeRunService{byID: map[string]*agentruns.Run{
		runID: {ID: runID, OrgID: "org-1", Status: runStatus},
	}}
	h := &Handler{
		guidedService:  guidedSvc,
		runService:     runSvc,
		projectService: &fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"editor-1": members.RoleEditor},
		}},
		workerKeyService: &fakeWorkerKeyService{},
		// No copilot agent in this workspace: a launch attempt fails
		// cleanly, which is enough to tell it apart from parking.
		agentService: &fakeAgentDefService{bySlug: map[string]*agents.Agent{}},
	}
	return h, guidedSvc, runSvc
}

func nudgeReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/guided-sessions/gs-1/chat/nudge", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor-1"})
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": "gs-1"})
}

func nudgeStatus(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Status
}

// A nudge that lands while a turn is in flight is parked, not dropped: the
// answer comes when the running turn finishes. The newest one wins.
func TestNudgeParksWhileARunIsInFlight(t *testing.T) {
	for _, status := range []string{agentruns.StatusQueued, agentruns.StatusClaimed, agentruns.StatusRunning} {
		t.Run(status, func(t *testing.T) {
			h, guidedSvc, runSvc := nudgeFixture(status)

			w := httptest.NewRecorder()
			h.NudgeGuidedChat(w, nudgeReq(`{"step":3,"state":{"step_3":"needs"},"event":"saved step 3"}`))
			if got := nudgeStatus(t, w); got != "pending" {
				t.Fatalf("status = %q, want pending", got)
			}

			w = httptest.NewRecorder()
			h.NudgeGuidedChat(w, nudgeReq(`{"step":4,"state":{"step_4":"requirements"},"event":"saved step 4"}`))
			if got := nudgeStatus(t, w); got != "pending" {
				t.Fatalf("second status = %q, want pending", got)
			}

			if len(guidedSvc.parked) != 2 {
				t.Fatalf("parked %d nudges, want both (the later overwriting the earlier)", len(guidedSvc.parked))
			}
			last := guidedSvc.parked[1]
			if last.Step != 4 || last.Event != "saved step 4" || last.State["step_4"] != "requirements" {
				t.Fatalf("parked %+v, want the newest nudge with its state", last)
			}
			if len(runSvc.launchReqs) != 0 {
				t.Fatalf("launched %d runs while one was in flight, want 0", len(runSvc.launchReqs))
			}
		})
	}
}

// With no run in flight the nudge launches a turn immediately and parks
// nothing.
func TestNudgeLaunchesWhenTheSessionIsFree(t *testing.T) {
	for _, status := range []string{"", agentruns.StatusSucceeded, agentruns.StatusFailed} {
		name := status
		if name == "" {
			name = "no run yet"
		}
		t.Run(name, func(t *testing.T) {
			h, guidedSvc, runSvc := nudgeFixture(status)
			// The copilot agent cannot be resolved in this fixture, so the
			// launch attempt answers "unavailable" — what matters is that the
			// handler tried to launch instead of parking.
			w := httptest.NewRecorder()
			h.NudgeGuidedChat(w, nudgeReq(`{"step":2,"state":{},"event":"saved step 2"}`))
			if got := nudgeStatus(t, w); got != "unavailable" && got != "launched" {
				t.Fatalf("status = %q, want a launch attempt", got)
			}
			if len(guidedSvc.parked) != 0 {
				t.Fatalf("parked %+v with a free session", guidedSvc.parked)
			}
			_ = runSvc
		})
	}
}

// An empty event still reads as something: the parked nudge is what the
// copilot is later asked to comment on.
func TestNudgeParksADefaultEvent(t *testing.T) {
	h, guidedSvc, _ := nudgeFixture(agentruns.StatusRunning)
	w := httptest.NewRecorder()
	h.NudgeGuidedChat(w, nudgeReq(`{"step":1,"state":{},"event":"   "}`))
	if got := nudgeStatus(t, w); got != "pending" {
		t.Fatalf("status = %q, want pending", got)
	}
	if len(guidedSvc.parked) != 1 || guidedSvc.parked[0].Event != "updated the wizard" {
		t.Fatalf("parked %+v, want the default event text", guidedSvc.parked)
	}
}
