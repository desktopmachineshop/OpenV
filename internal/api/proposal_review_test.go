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
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// proposalTestHandler is the Handler the proposal review tests drive: the
// proposals, the projects they belong to, and each account's effective role
// in those projects.
func proposalTestHandler(t *testing.T, svc *fakeProposalService, byID map[string]*projects.Project, roles map[string]map[string]string) *Handler {
	return newTestHandler(t, func(h *Handler) {
		h.ProposalService = svc
		h.ProjectService = &fakeProjectService{byID: byID}
		h.MemberService = &fakeMemberService{roles: roles}
	})
}

// TestReviewProposalRefusesRunTokens is the regression test for an agent
// that approved its own proposals: requireProjectRole passes a run token as
// an editor of its own project, and the single review routes had no other
// check, so a proposal-mode run's token approved the write it had just
// proposed, which then landed with no person's review. A run token is the
// agent, never a person, so both routes refuse it with 403 before anything
// is loaded, a direct-mode run's included, whichever run raised the
// proposal (OpenV REQ-21, REQ-79). A workspace runner key still reviews, as
// REQ-42's workspace-wide editor rights with no proposal gating let it,
// and a person still needs the editor role.
func TestReviewProposalRefusesRunTokens(t *testing.T) {
	project := "proj-1"
	proposal := func() *proposals.Proposal {
		return &proposals.Proposal{ID: "p-1", RunID: "run-1", ProjectID: project, Op: proposals.OpCreateArtifact,
			Status: proposals.StatusPending}
	}
	ownRun := &agentruns.Run{ID: "run-1", OrgID: "org-1", AgentID: "agent-proposal", ProjectID: &project}
	otherRun := &agentruns.Run{ID: "run-2", OrgID: "org-1", AgentID: "agent-direct", ProjectID: &project}

	const refused = `{"error":"agent runs cannot review proposals"}`
	cases := []struct {
		name     string
		ctx      func(context.Context) context.Context
		wantCode int
		wantBody string // "" for any
	}{
		{"the proposing run's own token", func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxRun, ownRun)
		}, http.StatusForbidden, refused},
		{"another run's token in the same project", func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxRun, otherRun)
		}, http.StatusForbidden, refused},
		{"a workspace runner key of the project's workspace", func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxWorkerOrg, "org-1")
		}, http.StatusOK, ""},
		{"a project editor", func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxUser, &users.User{ID: "eve"})
		}, http.StatusOK, ""},
		{"a project viewer", func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxUser, &users.User{ID: "val"})
		}, http.StatusForbidden, ""},
	}
	for _, approve := range []bool{true, false} {
		action, review := "approve", (*Handler).ApproveProposal
		if !approve {
			action, review = "reject", (*Handler).RejectProposal
		}
		for _, tc := range cases {
			t.Run(action+" by "+tc.name, func(t *testing.T) {
				svc := &fakeProposalService{byID: map[string]*proposals.Proposal{"p-1": proposal()}}
				h := proposalTestHandler(t, svc,
					map[string]*projects.Project{project: {ID: project, OrgID: "org-1"}},
					map[string]map[string]string{project: {"eve": members.RoleEditor, "val": members.RoleViewer}})
				r := httptest.NewRequest(http.MethodPost, "/api/v1/proposals/p-1/"+action, strings.NewReader(`{}`))
				r = mux.SetURLVars(r.WithContext(tc.ctx(r.Context())), map[string]string{"id": "p-1"})
				w := httptest.NewRecorder()
				review(h, w, r)

				if w.Code != tc.wantCode {
					t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
				}
				if got := strings.TrimSpace(w.Body.String()); tc.wantBody != "" && got != tc.wantBody {
					t.Fatalf("body = %s, want %s", got, tc.wantBody)
				}
				reviewed := len(svc.approved) + len(svc.rejected)
				if tc.wantCode == http.StatusOK && reviewed != 1 {
					t.Fatalf("approved %v, rejected %v: want the proposal reviewed once", svc.approved, svc.rejected)
				}
				if tc.wantCode != http.StatusOK && reviewed != 0 {
					t.Fatalf("approved %v, rejected %v: a refused review must reach no proposal", svc.approved, svc.rejected)
				}
			})
		}
	}
}
