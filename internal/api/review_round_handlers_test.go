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
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// roundArtifactService fakes artifacts.Service for the review-round endpoint,
// recording the scope it was asked for so the handler's request decoding is
// tested without a repository.
type roundArtifactService struct {
	artifacts.Service
	calls  int
	scopes [][]string
	moved  []*artifacts.Artifact
	err    error
}

func (f *roundArtifactService) StartProjectReview(projectID string, req artifacts.ReviewRoundRequest) (*artifacts.ReviewRoundResult, error) {
	f.calls++
	f.scopes = append(f.scopes, req.Types)
	if f.err != nil {
		return nil, f.err
	}
	types := req.Types
	if len(types) == 0 {
		types = artifacts.DefaultRoundTypes()
	}
	return &artifacts.ReviewRoundResult{
		Moved:           f.moved,
		AlreadyInReview: 1,
		Approved:        2,
		Types:           types,
	}, nil
}

func TestStartProjectReview(t *testing.T) {
	const (
		projectID = "proj-1"
		orgID     = "org-1"
	)

	newFixture := func() (*Handler, *roundArtifactService, *fakeChatterService) {
		artifactSvc := &roundArtifactService{moved: []*artifacts.Artifact{
			{ID: "art-1", ProjectID: projectID, Type: "requirement", Title: "Req 1", Status: artifacts.StatusInReview, Version: 2},
			{ID: "art-2", ProjectID: projectID, Type: "test-case", Title: "TC 1", Status: artifacts.StatusInReview, Version: 3},
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

	do := func(t *testing.T, h *Handler, ctxSetup func(context.Context) context.Context, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/review-round", strings.NewReader(body))
		r = mux.SetURLVars(r, map[string]string{"id": projectID})
		if ctxSetup != nil {
			r = r.WithContext(ctxSetup(r.Context()))
		}
		w := httptest.NewRecorder()
		h.StartProjectReview(w, r)
		return w
	}

	asUser := func(id string) func(context.Context) context.Context {
		return func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxUser, &users.User{ID: id})
		}
	}

	t.Run("an editor starts a round and gets its counts back", func(t *testing.T) {
		h, svc, chatterSvc := newFixture()
		w := do(t, h, asUser("editor"), "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var got artifacts.ReviewRoundResult
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got.Moved) != 2 || got.Approved != 2 || got.AlreadyInReview != 1 {
			t.Errorf("result = %+v, want 2 moved, 2 approved, 1 already in review", got)
		}
		if len(got.Types) == 0 {
			t.Error("result carried no type scope; a caller that named none must learn the default it got")
		}
		if svc.calls != 1 {
			t.Errorf("StartProjectReview calls = %d, want 1", svc.calls)
		}
		// Each moved artifact gets the feed note it would have got one at a
		// time, so its own history explains how it entered review.
		if len(chatterSvc.entries) != 2 {
			t.Fatalf("chatter entries = %d, want one per moved artifact", len(chatterSvc.entries))
		}
		if !strings.Contains(chatterSvc.entries[0].Message, "in_review") {
			t.Errorf("feed note = %q, want the status change in it", chatterSvc.entries[0].Message)
		}
	})

	t.Run("an explicit scope reaches the service", func(t *testing.T) {
		h, svc, _ := newFixture()
		w := do(t, h, asUser("editor"), `{"types":["requirement"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if len(svc.scopes) != 1 || len(svc.scopes[0]) != 1 || svc.scopes[0][0] != "requirement" {
			t.Errorf("scope = %v, want [[requirement]]", svc.scopes)
		}
	})

	t.Run("an unknown type maps to 400", func(t *testing.T) {
		h, svc, _ := newFixture()
		svc.err = artifacts.ErrInvalidType
		w := do(t, h, asUser("editor"), `{"types":["nonsense"]}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("a viewer is forbidden", func(t *testing.T) {
		h, svc, _ := newFixture()
		w := do(t, h, asUser("viewer"), "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Errorf("service called %d times on a denied request, want 0", svc.calls)
		}
	})

	t.Run("unauthenticated gets 401", func(t *testing.T) {
		h, svc, _ := newFixture()
		w := do(t, h, nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Errorf("service called %d times without a session, want 0", svc.calls)
		}
	})

	// A proposal-mode agent's writes go to the proposal queue for a human to
	// review. Letting one put the whole project into review would be that
	// agent driving the very process it is gated by.
	t.Run("a proposal-mode agent run is refused", func(t *testing.T) {
		h, svc, _ := newFixture()
		pid := projectID
		w := do(t, h, func(ctx context.Context) context.Context {
			ctx = context.WithValue(ctx, ctxUser, &users.User{ID: "editor"})
			return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: "run-1", AgentID: "agent-proposal", ProjectID: &pid})
		}, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Errorf("service called %d times for a proposal-mode run, want 0", svc.calls)
		}
	})

	t.Run("a direct-mode agent run is allowed", func(t *testing.T) {
		h, svc, _ := newFixture()
		pid := projectID
		w := do(t, h, func(ctx context.Context) context.Context {
			ctx = context.WithValue(ctx, ctxUser, &users.User{ID: "editor"})
			return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: "run-2", AgentID: "agent-direct", ProjectID: &pid})
		}, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 1 {
			t.Errorf("StartProjectReview calls = %d, want 1", svc.calls)
		}
	})

	t.Run("a malformed body maps to 400", func(t *testing.T) {
		h, svc, _ := newFixture()
		w := do(t, h, asUser("editor"), `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Errorf("service called %d times on a malformed body, want 0", svc.calls)
		}
	})
}
