package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeGuidedCommitService serves the commit handler one session and counts
// the commits. Each commit reports approved as its approvals and, when err is
// set, fails with it after making them.
type fakeGuidedCommitService struct {
	guided.Service
	session  *guided.Session
	commits  int
	approved []*artifacts.Artifact
	err      error
}

func (f *fakeGuidedCommitService) GetSession(id string) (*guided.Session, error) {
	return f.session, nil
}

func (f *fakeGuidedCommitService) Commit(sessionID string) (*guided.CommitResult, error) {
	f.commits++
	if f.err != nil {
		return &guided.CommitResult{Approved: f.approved}, f.err
	}
	committed := *f.session
	committed.Status = guided.StatusCommitted
	return &guided.CommitResult{Session: &committed, Approved: f.approved}, nil
}

func newGuidedCommitFixture(pid string, svc *fakeGuidedCommitService) (*Handler, *recordingBus) {
	bus := &recordingBus{}
	return NewHandler(HandlerDeps{
		GuidedService:  svc,
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{pid: {ID: pid, OrgID: "org-1"}}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			pid: {"editor-1": members.RoleEditor},
		}},
		AgentService: &fakeAgentService{byID: map[string]*agents.Agent{
			"agent-direct":   {ID: "agent-direct", WriteMode: agents.WriteModeDirect},
			"agent-proposal": {ID: "agent-proposal", WriteMode: agents.WriteModeProposal},
		}},
		Bus: bus,
	}), bus
}

func postGuidedCommit(h *Handler, ctx func(context.Context) context.Context) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/guided-sessions/gs-1/commit", nil)
	r = mux.SetURLVars(r.WithContext(ctx(r.Context())), map[string]string{"id": "gs-1"})
	w := httptest.NewRecorder()
	h.CommitGuidedSession(w, r)
	return w
}

func asGuidedEditor(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxUser, &users.User{ID: "editor-1"})
}

// The event stream is the sign-off history (issue #127), so each draft a
// guided commit approves gets the artifact.status_changed event an approval
// by hand gets, with the committing user as its actor, and the guided
// session it came from; the activity log and triggered automations read it
// there. Only the approval is published, never the step into review before
// it, which the notifier would turn into review requests for every editor.
func TestCommitGuidedSessionPublishesEachApproval(t *testing.T) {
	pid := "proj-1"
	approved := []*artifacts.Artifact{
		{ID: "art-1", ProjectID: pid, Type: "requirement", Title: "Warn before the spindle starts", Status: artifacts.StatusApproved, Version: 3},
		{ID: "art-2", ProjectID: pid, Type: "hazard", Title: "Spindle starts with the guard open", Status: artifacts.StatusApproved, Version: 2},
	}
	want := func(a *artifacts.Artifact) map[string]interface{} {
		return map[string]interface{}{
			"artifact_type":  a.Type,
			"title":          a.Title,
			"from":           artifacts.StatusInReview,
			"to":             artifacts.StatusApproved,
			"version":        a.Version,
			"guided_session": "gs-1",
		}
	}
	check := func(t *testing.T, bus *recordingBus, approved []*artifacts.Artifact) {
		t.Helper()
		if len(bus.published) != len(approved) {
			t.Fatalf("published %d events (%v), want one per approval (%d)", len(bus.published), bus.types(), len(approved))
		}
		for i, a := range approved {
			e := bus.published[i]
			if e.EventType != events.ArtifactStatusChanged || e.EntityID != a.ID || e.ProjectID != pid {
				t.Errorf("event %d: %s on %s in %s, want %s on %s in %s", i, e.EventType, e.EntityID, e.ProjectID,
					events.ArtifactStatusChanged, a.ID, pid)
			}
			if e.Actor != "user:editor-1" {
				t.Errorf("event %d: actor %q, want the committing user", i, e.Actor)
			}
			if e.OrgID != "org-1" {
				t.Errorf("event %d: org %q, want the project's", i, e.OrgID)
			}
			if !reflect.DeepEqual(e.Payload, want(a)) {
				t.Errorf("event %d: payload %v, want %v", i, e.Payload, want(a))
			}
		}
	}

	t.Run("a commit", func(t *testing.T) {
		svc := &fakeGuidedCommitService{session: &guided.Session{ID: "gs-1", ProjectID: pid, Status: guided.StatusInProgress}, approved: approved}
		h, bus := newGuidedCommitFixture(pid, svc)
		if w := postGuidedCommit(h, asGuidedEditor); w.Code != http.StatusOK {
			t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
		}
		check(t, bus, approved)
	})

	// A failed commit keeps the approvals it made before failing, and a
	// retry leaves them alone, so they are published with the error.
	t.Run("a commit that fails part way", func(t *testing.T) {
		svc := &fakeGuidedCommitService{session: &guided.Session{ID: "gs-1", ProjectID: pid, Status: guided.StatusInProgress},
			approved: approved[:1], err: errors.New("store unavailable")}
		h, bus := newGuidedCommitFixture(pid, svc)
		if w := postGuidedCommit(h, asGuidedEditor); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		check(t, bus, approved[:1])
	})
}

// A guided commit approves the session's drafts, so it is a review-state
// change like PUT /artifacts/{id}/status: an editor and a direct-mode agent
// run may commit, and a proposal-mode agent run, whose writes wait for a
// human, is refused before anything is approved.
func TestCommitGuidedSessionRefusesProposalModeRuns(t *testing.T) {
	pid := "proj-1"
	newFixture := func() (*Handler, *fakeGuidedCommitService, *recordingBus) {
		svc := &fakeGuidedCommitService{session: &guided.Session{ID: "gs-1", ProjectID: pid, Status: guided.StatusInProgress},
			approved: []*artifacts.Artifact{{ID: "art-1", ProjectID: pid, Status: artifacts.StatusApproved, Version: 3}}}
		h, bus := newGuidedCommitFixture(pid, svc)
		return h, svc, bus
	}
	commit := postGuidedCommit
	asRun := func(runID, agentID string) func(context.Context) context.Context {
		return func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: runID, AgentID: agentID, ProjectID: &pid})
		}
	}

	t.Run("an editor", func(t *testing.T) {
		h, svc, _ := newFixture()
		w := commit(h, asGuidedEditor)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
		}
		if svc.commits != 1 {
			t.Fatalf("Commit called %d times, want 1", svc.commits)
		}
	})

	t.Run("a direct-mode agent run", func(t *testing.T) {
		h, svc, bus := newFixture()
		if w := commit(h, asRun("run-1", "agent-direct")); w.Code != http.StatusOK {
			t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
		}
		if svc.commits != 1 {
			t.Fatalf("Commit called %d times, want 1", svc.commits)
		}
		if len(bus.published) != 1 || bus.published[0].Actor != "agent:run-1" {
			t.Fatalf("published %+v, want the approval with the run as its actor", bus.published)
		}
	})

	t.Run("a proposal-mode agent run is refused", func(t *testing.T) {
		h, svc, bus := newFixture()
		w := commit(h, asRun("run-2", "agent-proposal"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if svc.commits != 0 {
			t.Fatalf("Commit called %d times for a proposal-mode run, want 0", svc.commits)
		}
		if len(bus.published) != 0 {
			t.Fatalf("a refused commit published %v", bus.types())
		}
	})
}
