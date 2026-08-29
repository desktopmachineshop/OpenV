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
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/seeds"
)

// Valid requirement UUIDs for the draft tests: DraftTestCases now rejects any
// requirement_id that is not a UUID (issue #245), so the ids must be well-formed.
const (
	draftReqUUID1 = "11111111-1111-4111-8111-111111111111"
	draftReqUUID2 = "22222222-2222-4222-8222-222222222222"
)

// draftReq builds an authenticated "Draft test cases" request for a project.
func draftReq(userID, projectID, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/draft-test-cases", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": projectID})
}

func newDraftFixture() (*Handler, *fakeRunService) {
	runSvc := &fakeRunService{}
	return &Handler{
		runService: runSvc,
		agentService: &fakeAgentService{byID: map[string]*agents.Agent{
			"tca": {ID: "tca", OrgID: "org-1", Slug: seeds.TestCaseAuthorSlug, Name: "Test Case Author", Provider: "claude-code"},
		}},
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"org-admin": orgs.RoleAdmin, "editor": orgs.RoleMember, "viewer": orgs.RoleMember},
		}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"editor": members.RoleEditor, "viewer": members.RoleViewer},
		}},
	}, runSvc
}

// TestDraftTestCasesAuthz locks in that the launch action mirrors the project
// write ladder: an editor (or org admin) may launch, a viewer is denied, and a
// denied request never reaches the run service.
func TestDraftTestCasesAuthz(t *testing.T) {
	cases := []struct {
		name       string
		userID     string
		wantCode   int // 0 = created
		wantLaunch bool
	}{
		{"editor launches", "editor", 0, true},
		{"org admin launches", "org-admin", 0, true},
		{"viewer is denied", "viewer", http.StatusForbidden, false},
		{"non-member is denied", "stranger", http.StatusForbidden, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, runSvc := newDraftFixture()
			w := httptest.NewRecorder()
			h.DraftTestCases(w, draftReq(tc.userID, "proj-1", `{"requirement_ids":["`+draftReqUUID1+`"]}`))
			if tc.wantCode == 0 {
				if w.Code != http.StatusCreated {
					t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
				}
			} else if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if got := len(runSvc.launchReqs) > 0; got != tc.wantLaunch {
				t.Fatalf("launched = %v, want %v", got, tc.wantLaunch)
			}
		})
	}
}

// TestDraftTestCasesRoundTrip locks in the requirement-id conveyance: the IDs
// posted by the caller reach the launched run's prompt (deduped, blanks
// dropped), scoped to the right agent, project and org — so the lean-context
// agent can fetch each requirement at run time.
func TestDraftTestCasesRoundTrip(t *testing.T) {
	h, runSvc := newDraftFixture()
	w := httptest.NewRecorder()
	// Includes a blank and a duplicate to prove normalization.
	h.DraftTestCases(w, draftReq("editor", "proj-1", `{"requirement_ids":["`+draftReqUUID1+`"," ","`+draftReqUUID2+`","`+draftReqUUID1+`"]}`))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
	}
	if len(runSvc.launchReqs) != 1 {
		t.Fatalf("launch calls = %d, want 1", len(runSvc.launchReqs))
	}
	got := runSvc.launchReqs[0]
	if got.AgentID != "tca" {
		t.Errorf("AgentID = %q, want the test-case author agent", got.AgentID)
	}
	if got.OrgID != "org-1" {
		t.Errorf("OrgID = %q, want the project's org", got.OrgID)
	}
	if got.ProjectID == nil || *got.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %v, want proj-1", got.ProjectID)
	}
	if got.LaunchedBy == nil || *got.LaunchedBy != "editor" {
		t.Errorf("LaunchedBy = %v, want editor", got.LaunchedBy)
	}
	if !strings.Contains(got.Prompt, draftReqUUID1) || !strings.Contains(got.Prompt, draftReqUUID2) {
		t.Errorf("prompt %q must carry both requirement IDs", got.Prompt)
	}
	// The blank must not survive normalization: no ", ," and no doubled ID.
	if strings.Count(got.Prompt, draftReqUUID1) != 1 {
		t.Errorf("prompt %q should list the first requirement exactly once (deduped)", got.Prompt)
	}

	var run agentruns.Run
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil || run.ID != "run-new" {
		t.Fatalf("body = %q (err %v), want the queued run", w.Body.String(), err)
	}
}

// TestDraftTestCasesValidation covers the two request-shape rejections that
// never reach the run service: no usable IDs, and a missing seeded agent.
func TestDraftTestCasesValidation(t *testing.T) {
	t.Run("empty ids answers 400", func(t *testing.T) {
		h, runSvc := newDraftFixture()
		w := httptest.NewRecorder()
		h.DraftTestCases(w, draftReq("editor", "proj-1", `{"requirement_ids":[" ",""]}`))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.launchReqs) != 0 {
			t.Fatal("must not launch a run with no requirement ids")
		}
	})

	t.Run("non-uuid id answers 400 before launch", func(t *testing.T) {
		h, runSvc := newDraftFixture()
		w := httptest.NewRecorder()
		// One valid UUID and one malformed id: the whole request is rejected
		// before the prompt is built (issue #245).
		h.DraftTestCases(w, draftReq("editor", "proj-1", `{"requirement_ids":["`+draftReqUUID1+`","not-a-uuid"]}`))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.launchReqs) != 0 {
			t.Fatal("must not launch a run when any requirement id is not a UUID")
		}
	})

	t.Run("missing seeded agent answers 404", func(t *testing.T) {
		h, runSvc := newDraftFixture()
		h.agentService = &fakeAgentService{byID: map[string]*agents.Agent{}} // agent not seeded
		w := httptest.NewRecorder()
		h.DraftTestCases(w, draftReq("editor", "proj-1", `{"requirement_ids":["`+draftReqUUID1+`"]}`))
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.launchReqs) != 0 {
			t.Fatal("must not launch when the agent is unavailable")
		}
	})
}
