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
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// workItemAssigneeFixture is workspace W (org-w) with project P, its
// workspace-wide crew "crew-w" and the P-pinned crew "crew-p", and
// workspace X (org-x) with its crew "crew-x". "editor" is a plain member of
// W and X who edits P; "guest" edits P from a workspace of its own (org-g)
// and is no member of W or X. P holds the card "card", assigned to crew-p,
// the card "card-w", assigned to crew-w, and the card "card-u", assigned to
// a person whose id, which is not looked up, is crew-x's.
func workItemAssigneeFixture() (*Handler, *payloadWorkItems) {
	pin := "proj-p"
	crew := func(id, orgID string, projectID *string) *teams.TeamGraph {
		return &teams.TeamGraph{Team: &teams.Team{ID: id, OrgID: orgID, Name: id, ProjectID: projectID}}
	}
	card := func(id, assigneeType, assigneeID string) *workitems.WorkItem {
		return &workitems.WorkItem{ID: id, ProjectID: "proj-p", Title: "Plan", Column: workitems.ColumnTodo,
			AssigneeType: assigneeType, AssigneeID: &assigneeID, ArtifactIDs: []string{}}
	}
	repo := &payloadWorkItems{items: map[string]*workitems.WorkItem{
		"card":   card("card", workitems.AssigneeTeam, "crew-p"),
		"card-w": card("card-w", workitems.AssigneeTeam, "crew-w"),
		"card-u": card("card-u", workitems.AssigneeUser, "crew-x"),
	}}
	h := NewHandler(HandlerDeps{
		WorkItemService: workitems.NewDefaultService(repo, nil),
		TeamService: &fakeCrewGraphs{graphs: map[string]*teams.TeamGraph{
			"crew-w": crew("crew-w", "org-w", nil),
			"crew-p": crew("crew-p", "org-w", &pin),
			"crew-x": crew("crew-x", "org-x", nil),
		}},
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-p": {ID: "proj-p", OrgID: "org-w"},
		}},
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-w": {"editor": orgs.RoleMember},
			"org-x": {"editor": orgs.RoleMember},
			"org-g": {"guest": orgs.RoleMember},
		}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-p": {"editor": members.RoleEditor, "guest": members.RoleEditor},
		}},
	})
	return h, repo
}

func workItemReq(method, userID, orgID, id, body string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/x/"+id, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": id})
}

// TestAWorkItemsAssigneeIsOneTheBoardKnows pins OpenV REQ-23's assignees on
// a create and an update: an assignee_type other than user, agent or team
// answers 400 with the types it takes, and a crew (assignee_type team) no
// row has answers 404 "team not found", as the crew routes answer it. Both
// used to be stored as sent. A crew the caller may not know of (a
// workspace-wide crew of W, or X's crew, to an editor of P from another
// workspace) answers the same 404 (I3), and one of another workspace than
// the card's project that the caller may know of (X's crew, to a member of
// X) answers 400, refused as such (REQ-17). An update that sends no
// assignee_type judges its assignee_id by the card's stored type, and one
// that re-sends the card's own assignee is not checked again, so the guest
// keeps W's crew on card-w; one that turns the card's person into a crew of
// the same id is. A person's or an agent's id is not looked up.
func TestAWorkItemsAssigneeIsOneTheBoardKnows(t *testing.T) {
	const badType = "invalid assignee_type: must be user, agent or team"
	const elsewhere = "team belongs to a different workspace"
	cases := []struct {
		name, method, user, org, card, body string
		want                                int
		wantErr, wantCrew                   string
	}{
		{"create: an unknown type", http.MethodPost, "editor", "org-w", "",
			`{"title":"Plan","assignee_type":"robot"}`, http.StatusBadRequest, badType, ""},
		{"create: a crew no row has", http.MethodPost, "editor", "org-w", "",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-none"}`, http.StatusNotFound, "team not found", ""},
		{"create: a crew of another workspace, to a member of it", http.MethodPost, "editor", "org-w", "",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-x"}`, http.StatusBadRequest, elsewhere, ""},
		{"create: a crew of another workspace, to a guest who may not know of it", http.MethodPost, "guest", "org-g", "",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-x"}`, http.StatusNotFound, "team not found", ""},
		{"create: W's crew, to a guest who may not know of it", http.MethodPost, "guest", "org-g", "",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-w"}`, http.StatusNotFound, "team not found", ""},
		{"create: W's crew", http.MethodPost, "editor", "org-w", "",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-w"}`, http.StatusCreated, "", ""},
		{"create: P's crew, by the guest", http.MethodPost, "guest", "org-g", "",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-p"}`, http.StatusCreated, "", ""},
		{"create: an agent's id, not looked up", http.MethodPost, "editor", "org-w", "",
			`{"title":"Plan","assignee_type":"agent","assignee_id":"agent-any"}`, http.StatusCreated, "", ""},
		{"create: a person, by default", http.MethodPost, "editor", "org-w", "",
			`{"title":"Plan","assignee_id":"editor"}`, http.StatusCreated, "", ""},
		{"update: an unknown type", http.MethodPut, "editor", "org-w", "card",
			`{"title":"Plan","assignee_type":"robot","assignee_id":"editor"}`, http.StatusBadRequest, badType, ""},
		{"update: a crew no row has, by the card's type", http.MethodPut, "editor", "org-w", "card",
			`{"title":"Plan","assignee_id":"crew-none"}`, http.StatusNotFound, "team not found", ""},
		{"update: a crew of another workspace, to a member of it", http.MethodPut, "editor", "org-w", "card",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-x"}`, http.StatusBadRequest, elsewhere, ""},
		{"update: a crew of another workspace, to a guest who may not know of it", http.MethodPut, "guest", "org-g", "card",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-x"}`, http.StatusNotFound, "team not found", ""},
		{"update: W's crew, to a guest who may not know of it", http.MethodPut, "guest", "org-g", "card",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-w"}`, http.StatusNotFound, "team not found", ""},
		{"update: the guest re-sends the card's own crew, W's", http.MethodPut, "guest", "org-g", "card-w",
			`{"title":"Plan 2","assignee_type":"team","assignee_id":"crew-w"}`, http.StatusOK, "", "crew-w"},
		{"update: the guest re-sends the card's own crew, W's, by the card's type", http.MethodPut, "guest", "org-g", "card-w",
			`{"title":"Plan 2","assignee_id":"crew-w"}`, http.StatusOK, "", "crew-w"},
		{"update: a person's id sent back as a crew's is checked", http.MethodPut, "guest", "org-g", "card-u",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-x"}`, http.StatusNotFound, "team not found", ""},
		{"update: W's crew", http.MethodPut, "editor", "org-w", "card",
			`{"title":"Plan","assignee_type":"team","assignee_id":"crew-w"}`, http.StatusOK, "", "crew-w"},
		{"update: to a person", http.MethodPut, "editor", "org-w", "card",
			`{"title":"Plan","assignee_type":"user","assignee_id":"editor"}`, http.StatusOK, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := workItemAssigneeFixture()
			before := *repo.items["card"]
			if tc.card != "" {
				before = *repo.items[tc.card]
			}
			w := httptest.NewRecorder()
			if tc.method == http.MethodPost {
				h.CreateWorkItem(w, workItemReq(tc.method, tc.user, tc.org, "proj-p", tc.body))
			} else {
				h.UpdateWorkItem(w, workItemReq(tc.method, tc.user, tc.org, tc.card, tc.body))
			}
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if after := repo.items[tc.card]; tc.wantCrew != "" && (after.AssigneeType != workitems.AssigneeTeam ||
				after.AssigneeID == nil || *after.AssigneeID != tc.wantCrew) {
				t.Fatalf("the card's assignee = %s %v, want the crew %s", after.AssigneeType, after.AssigneeID, tc.wantCrew)
			}
			if tc.wantErr == "" {
				return
			}
			if !strings.Contains(w.Body.String(), `"error":"`+tc.wantErr+`"`) {
				t.Fatalf("body = %q, want the error %q", w.Body.String(), tc.wantErr)
			}
			if len(repo.items) != 3 {
				t.Fatalf("a refused create stored an item: %d items", len(repo.items))
			}
			if after := repo.items[before.ID]; after.Title != before.Title || after.AssigneeType != before.AssigneeType ||
				*after.AssigneeID != *before.AssigneeID {
				t.Fatalf("a refused update changed the card: %+v", after)
			}
		})
	}
}
