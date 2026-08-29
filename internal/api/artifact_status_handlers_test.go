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
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// statusArtifactService fakes just enough of artifacts.Service for the
// status endpoint: GetArtifact for authz resolution and ChangeStatus, which
// delegates to the real domain state machine so sentinel mapping is tested
// against genuine domain errors.
type statusArtifactService struct {
	artifacts.Service
	byID  map[string]*artifacts.Artifact
	calls int
}

func (f *statusArtifactService) GetArtifact(id string) (*artifacts.Artifact, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, artifacts.ErrNotFound
}

func (f *statusArtifactService) ChangeStatus(id, to string) (*artifacts.Artifact, error) {
	f.calls++
	if !artifacts.ValidStatus(to) {
		return nil, fmt.Errorf("%w: %q", artifacts.ErrInvalidStatus, to)
	}
	a, ok := f.byID[id]
	if !ok {
		return nil, artifacts.ErrNotFound
	}
	from := artifacts.NormalizeStatus(a.Status)
	if !artifacts.CanTransition(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s", artifacts.ErrInvalidStatusTransition, from, to)
	}
	copied := *a
	copied.Status = to
	copied.Version++
	return &copied, nil
}

func TestChangeArtifactStatus(t *testing.T) {
	const (
		projectID  = "proj-1"
		orgID      = "org-1"
		artifactID = "art-1"
	)

	newFixture := func(status string) (*Handler, *statusArtifactService, *fakeChatterService) {
		artifactSvc := &statusArtifactService{byID: map[string]*artifacts.Artifact{
			artifactID: {ID: artifactID, ProjectID: projectID, Type: "requirement", Title: "Req", Status: status, Version: 1},
		}}
		chatterSvc := &fakeChatterService{}
		h := &Handler{
			artifactService: artifactSvc,
			chatterService:  chatterSvc,
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				projectID: {ID: projectID, OrgID: orgID},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{orgID: {}}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				projectID: {
					"editor": members.RoleEditor,
					"viewer": members.RoleViewer,
				},
			}},
			agentService: &fakeAgentService{byID: map[string]*agents.Agent{
				"agent-direct":   {ID: "agent-direct", WriteMode: agents.WriteModeDirect},
				"agent-proposal": {ID: "agent-proposal", WriteMode: agents.WriteModeProposal},
			}},
		}
		return h, artifactSvc, chatterSvc
	}

	do := func(t *testing.T, h *Handler, ctxSetup func(context.Context) context.Context, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/"+target+"/status", strings.NewReader(body))
		r = mux.SetURLVars(r, map[string]string{"id": target})
		if ctxSetup != nil {
			r = r.WithContext(ctxSetup(r.Context()))
		}
		w := httptest.NewRecorder()
		h.ChangeArtifactStatus(w, r)
		return w
	}

	asUser := func(id string) func(context.Context) context.Context {
		return func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxUser, &users.User{ID: id})
		}
	}

	t.Run("editor performs a legal transition", func(t *testing.T) {
		h, svc, chatterSvc := newFixture(artifacts.StatusDraft)
		w := do(t, h, asUser("editor"), artifactID, `{"status":"in_review"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var got artifacts.Artifact
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.Status != artifacts.StatusInReview {
			t.Errorf("response status = %q, want in_review", got.Status)
		}
		if svc.calls != 1 {
			t.Errorf("ChangeStatus calls = %d, want 1", svc.calls)
		}
		if len(chatterSvc.entries) != 1 || !strings.Contains(chatterSvc.entries[0].Message, "draft") {
			t.Errorf("expected one status-change chatter entry, got %+v", chatterSvc.entries)
		}
	})

	t.Run("viewer is forbidden", func(t *testing.T) {
		h, svc, _ := newFixture(artifacts.StatusDraft)
		w := do(t, h, asUser("viewer"), artifactID, `{"status":"in_review"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Errorf("ChangeStatus called %d times on a denied request, want 0", svc.calls)
		}
	})

	t.Run("unauthenticated gets 401", func(t *testing.T) {
		h, _, _ := newFixture(artifacts.StatusDraft)
		w := do(t, h, nil, artifactID, `{"status":"in_review"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("unknown status maps to 400", func(t *testing.T) {
		h, _, _ := newFixture(artifacts.StatusDraft)
		w := do(t, h, asUser("editor"), artifactID, `{"status":"shipped"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("illegal transition maps to 409", func(t *testing.T) {
		h, _, _ := newFixture(artifacts.StatusDraft)
		w := do(t, h, asUser("editor"), artifactID, `{"status":"approved"}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body %q)", w.Code, w.Body.String())
		}
		var body errorBody
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error == "" {
			t.Errorf("expected JSON error envelope, got %q", w.Body.String())
		}
	})

	t.Run("missing artifact maps to 404", func(t *testing.T) {
		h, _, _ := newFixture(artifacts.StatusDraft)
		w := do(t, h, asUser("editor"), "art-missing", `{"status":"in_review"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("malformed body maps to 400", func(t *testing.T) {
		h, _, _ := newFixture(artifacts.StatusDraft)
		w := do(t, h, asUser("editor"), artifactID, `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("direct-mode agent run may transition", func(t *testing.T) {
		h, _, _ := newFixture(artifacts.StatusInReview)
		pid := projectID
		asRun := func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: "run-1", AgentID: "agent-direct", ProjectID: &pid})
		}
		w := do(t, h, asRun, artifactID, `{"status":"approved"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("proposal-mode agent run is refused", func(t *testing.T) {
		h, svc, _ := newFixture(artifacts.StatusInReview)
		pid := projectID
		asRun := func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: "run-2", AgentID: "agent-proposal", ProjectID: &pid})
		}
		w := do(t, h, asRun, artifactID, `{"status":"approved"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Errorf("ChangeStatus called %d times for a proposal-mode run, want 0", svc.calls)
		}
	})
}
