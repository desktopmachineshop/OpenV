package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
)

// TestPersonalRunnerKeyActsWithItsHoldersRole is the regression test for a
// member's personal runner key that approved agent proposals and wrote
// artifacts in any project of its workspace, whatever its holder's role
// there: requireProjectRole passed every worker key of the project's
// workspace and never looked at whose key it was, so a member who only
// views a project approved a proposal there, or created an artifact, with
// the key their own session is refused (OpenV REQ-16, REQ-79, REQ-21 with
// TC-7). A personal key is its holder acting: an editor's key and a
// workspace admin's key pass, a viewer's and a roleless member's are
// refused with the project's 403 and reach nothing. A workspace runner key
// has no holder and keeps REQ-42's workspace-wide editor rights.
func TestPersonalRunnerKeyActsWithItsHoldersRole(t *testing.T) {
	const (
		org     = "o1" // copyHandler's p1 is in o1
		project = "p1"
		denied  = `{"error":"you do not have access to this project"}`
	)
	keys := []struct {
		name     string
		holder   string // "" for a workspace runner key
		wantPass bool
	}{
		{"a viewer's personal key", "val", false},
		{"a roleless member's personal key", "mo", false},
		{"an editor's personal key", "eve", true},
		{"a workspace admin's personal key", "ada", true},
		{"a workspace runner key", "", true},
	}
	roles := func() (*fakeOrgService, *fakeMemberService) {
		return &fakeOrgService{roles: map[string]map[string]string{
				org: {"val": orgs.RoleMember, "mo": orgs.RoleMember, "eve": orgs.RoleMember, "ada": orgs.RoleAdmin},
			}}, &fakeMemberService{roles: map[string]map[string]string{
				project: {"val": members.RoleViewer, "eve": members.RoleEditor},
			}}
	}
	keyCtx := func(r *http.Request, holder string) *http.Request {
		ctx := context.WithValue(r.Context(), ctxWorkerOrg, org)
		if holder != "" {
			ctx = context.WithValue(ctx, ctxWorkerUser, holder)
		}
		return r.WithContext(ctx)
	}
	check := func(t *testing.T, w *httptest.ResponseRecorder, wantPass bool, okCode int, reached int) {
		t.Helper()
		if wantPass {
			if w.Code != okCode || reached != 1 {
				t.Fatalf("status = %d, reached %d: want %d, reached once (body %q)", w.Code, reached, okCode, w.Body.String())
			}
			return
		}
		if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != denied {
			t.Fatalf("status = %d, body %q: want 403 %s", w.Code, w.Body.String(), denied)
		}
		if reached != 0 {
			t.Fatalf("a refused key reached the service %d times, want none", reached)
		}
	}

	for _, key := range keys {
		for _, approve := range []bool{true, false} {
			action, review := "approve", (*Handler).ApproveProposal
			if !approve {
				action, review = "reject", (*Handler).RejectProposal
			}
			t.Run(action+" by "+key.name, func(t *testing.T) {
				svc := &fakeProposalService{byID: map[string]*proposals.Proposal{"pr-1": {ID: "pr-1", RunID: "run-1",
					ProjectID: project, Op: proposals.OpCreateArtifact, Status: proposals.StatusPending}}}
				h := proposalTestHandler(svc, map[string]*projects.Project{project: {ID: project, OrgID: org}}, nil)
				h.orgService, h.memberService = roles()
				r := httptest.NewRequest(http.MethodPost, "/api/v1/proposals/pr-1/"+action, strings.NewReader(`{}`))
				r = mux.SetURLVars(keyCtx(r, key.holder), map[string]string{"id": "pr-1"})
				w := httptest.NewRecorder()
				review(h, w, r)
				check(t, w, key.wantPass, http.StatusOK, len(svc.approved)+len(svc.rejected))
			})
		}
		t.Run("create an artifact with "+key.name, func(t *testing.T) {
			h, _ := copyHandler()
			h.orgService, h.memberService = roles()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts",
				strings.NewReader(`{"project_id":"p1","type":"requirement","title":"New","body":"x"}`))
			w := httptest.NewRecorder()
			h.CreateArtifact(w, keyCtx(r, key.holder))
			check(t, w, key.wantPass, http.StatusCreated, len(h.artifactService.(*copyArtifactFake).created))
		})
	}
}
