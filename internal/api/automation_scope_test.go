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

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// An automation's scope (#379 question 42): this project or the whole
// workspace. A whole-workspace automation is stored with no project, fires
// on events of every project of its workspace and on the workspace's own,
// and its run takes the event's project. Writing one is a workspace admin's
// (requireAutomationWrite, which the S5d tour pins for create, update, run
// now and delete); these tests pin what the scope choice added: moving an
// automation to another scope, which takes the write rights of where it
// goes and the workspace-automations feature, and where an automation may
// run, checked on create and whenever an update moves or retargets it.

// scopeRepo stores automations in memory for the real automations service.
type scopeRepo struct {
	automations.Repository
	byID map[string]*automations.Automation
}

func (r *scopeRepo) put(a *automations.Automation) error {
	c := *a
	r.byID[a.ID] = &c
	return nil
}

func (r *scopeRepo) Save(a *automations.Automation) error   { return r.put(a) }
func (r *scopeRepo) Update(a *automations.Automation) error { return r.put(a) }

func (r *scopeRepo) FindByID(id string) (*automations.Automation, error) {
	a, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	c := *a
	return &c, nil
}

// scopeFixture is workspace org-1, whose admin is "admin" and whose member
// "editor" edits its projects proj-1 and proj-2; workspace org-2, which
// "admin" also administers, with project proj-x. org-1 holds the agent
// agent-1, the crew crew-wide (pinned to no project), crew-1 (pinned to
// proj-1) and crew-2 (pinned to proj-2); org-2 the agent agent-x and the
// crew crew-x. Automations:
//   - au-p1: agent-1's, pinned to proj-1;
//   - au-wide: agent-1's, for the whole workspace;
//   - au-legacy: for the whole workspace, of crew-1, as one saved before
//     where an automation may run was checked.
//
// plan is the workspace's plan: on Business, with no stable release turned
// on, the stable channel has no gated feature yet.
func scopeFixture(t *testing.T, plan string) (*Handler, *scopeRepo) {
	t.Helper()
	proj1, proj2, agent1 := "proj-1", "proj-2", "agent-1"
	crew1 := "crew-1"
	repo := &scopeRepo{byID: map[string]*automations.Automation{}}
	seed := func(id string, project, agent, team *string) {
		repo.byID[id] = &automations.Automation{ID: id, OrgID: "org-1", Name: id, AgentID: agent, TeamID: team,
			ProjectID: project, Kind: automations.KindManual, Enabled: true, EventFilter: map[string]interface{}{}}
	}
	seed("au-p1", &proj1, &agent1, nil)
	seed("au-wide", nil, &agent1, nil)
	seed("au-legacy", nil, nil, &crew1)
	pinned := func(id string) *string { return &id }
	h := newTestHandler(t, func(h *Handler) {
		h.AutomationService = automations.NewDefaultService(repo)
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
			proj1:    {ID: proj1, OrgID: "org-1"},
			proj2:    {ID: proj2, OrgID: "org-1"},
			"proj-x": {ID: "proj-x", OrgID: "org-2"},
		}}
		h.OrgService = &fakeOrgService{plan: plan, roles: map[string]map[string]string{
			"org-1": {"admin": orgs.RoleAdmin, "editor": orgs.RoleMember},
			"org-2": {"admin": orgs.RoleAdmin},
		}}
		h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
			proj1: {"editor": members.RoleEditor},
			proj2: {"editor": members.RoleEditor},
		}}
		h.AgentService = &fakeAgentService{byID: map[string]*agents.Agent{
			agent1:    {ID: agent1, OrgID: "org-1"},
			"agent-x": {ID: "agent-x", OrgID: "org-2"},
		}}
		h.TeamService = &fakeCrewGraphs{graphs: map[string]*teams.TeamGraph{
			"crew-wide": {Team: &teams.Team{ID: "crew-wide", OrgID: "org-1"}},
			crew1:       {Team: &teams.Team{ID: crew1, OrgID: "org-1", ProjectID: pinned(proj1)}},
			"crew-2":    {Team: &teams.Team{ID: "crew-2", OrgID: "org-1", ProjectID: pinned(proj2)}},
			"crew-x":    {Team: &teams.Team{ID: "crew-x", OrgID: "org-2"}},
		}}
	})
	return h, repo
}

// sendAutomation serves POST /api/v1/automations, or PUT
// /api/v1/automations/{id} when id is set, as userID active in org-1.
func sendAutomation(h *Handler, userID, id, body string) *httptest.ResponseRecorder {
	method, path, handle := http.MethodPost, "/api/v1/automations", h.CreateAutomation
	if id != "" {
		method, path, handle = http.MethodPut, "/api/v1/automations/"+id, h.UpdateAutomation
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	r = r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-1"))
	if id != "" {
		r = mux.SetURLVars(r, map[string]string{"id": id})
	}
	w := httptest.NewRecorder()
	handle(w, r)
	return w
}

// scopeOfStored prints a stored automation's scope and target.
func scopeOfStored(t *testing.T, repo *scopeRepo, id string) string {
	t.Helper()
	a := repo.byID[id]
	if a == nil {
		t.Fatalf("no automation %s is stored", id)
	}
	return fmt.Sprintf("project %s, agent %s, crew %s", orDash(a.ProjectID), orDash(a.AgentID), orDash(a.TeamID))
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func wantAnswer(t *testing.T, w *httptest.ResponseRecorder, status int, errText string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", w.Code, status, w.Body.String())
	}
	if errText == "" {
		return
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != errText {
		t.Fatalf("answer %q, want the error %q", w.Body.String(), errText)
	}
}

func TestMovingAnAutomationToAnotherScope(t *testing.T) {
	cases := []struct {
		name, user, id, body string
		status               int
		errText              string
		// stored is the automation's scope and target after the request.
		stored string
	}{
		{name: "an admin moves a project's automation to the whole workspace",
			user: "admin", id: "au-p1", body: `{"project_id":""}`, status: http.StatusOK,
			stored: "project -, agent agent-1, crew -"},
		{name: "a project editor may not: the workspace admin guard of where it goes",
			user: "editor", id: "au-p1", body: `{"project_id":""}`, status: http.StatusForbidden,
			stored: "project proj-1, agent agent-1, crew -"},
		{name: "an admin moves a whole-workspace automation to a project",
			user: "admin", id: "au-wide", body: `{"project_id":"proj-2"}`, status: http.StatusOK,
			stored: "project proj-2, agent agent-1, crew -"},
		{name: "a project editor may not move a whole-workspace one: the admin guard of where it is",
			user: "editor", id: "au-wide", body: `{"project_id":"proj-1"}`, status: http.StatusForbidden,
			stored: "project -, agent agent-1, crew -"},
		{name: "an editor of both projects moves an automation between them",
			user: "editor", id: "au-p1", body: `{"project_id":"proj-2"}`, status: http.StatusOK,
			stored: "project proj-2, agent agent-1, crew -"},
		{name: "not to a project the caller cannot reach: as one no row has",
			user: "editor", id: "au-p1", body: `{"project_id":"proj-x"}`, status: http.StatusNotFound,
			errText: "project not found", stored: "project proj-1, agent agent-1, crew -"},
		{name: "not to another workspace's project, even one the caller administers",
			user: "admin", id: "au-p1", body: `{"project_id":"proj-x"}`, status: http.StatusBadRequest,
			errText: "project does not belong to this workspace", stored: "project proj-1, agent agent-1, crew -"},
		{name: "the scope it has already, named, is no move",
			user: "editor", id: "au-p1", body: `{"project_id":"proj-1","name":"Renamed"}`, status: http.StatusOK,
			stored: "project proj-1, agent agent-1, crew -"},
		{name: "a null project_id leaves the scope as it is",
			user: "admin", id: "au-wide", body: `{"project_id":null}`, status: http.StatusOK,
			stored: "project -, agent agent-1, crew -"},
		{name: "not with a crew pinned to a project, to the whole workspace",
			user: "admin", id: "au-p1", body: `{"project_id":"","agent_id":"","team_id":"crew-1"}`,
			status: http.StatusBadRequest, errText: "a crew pinned to a project cannot run an automation for the whole workspace",
			stored: "project proj-1, agent agent-1, crew -"},
		{name: "with the workspace's crew, to the whole workspace",
			user: "admin", id: "au-p1", body: `{"project_id":"","agent_id":"","team_id":"crew-wide"}`,
			status: http.StatusOK, stored: "project -, agent -, crew crew-wide"},
		{name: "a pinned crew moves with its project's automation to that project",
			user: "admin", id: "au-legacy", body: `{"project_id":"proj-1"}`, status: http.StatusOK,
			stored: "project proj-1, agent -, crew crew-1"},
		{name: "but not to another project",
			user: "admin", id: "au-legacy", body: `{"project_id":"proj-2"}`, status: http.StatusBadRequest,
			errText: "the crew is pinned to another project", stored: "project -, agent -, crew crew-1"},
		{name: "retargeting a whole-workspace automation to a crew pinned to a project",
			user: "admin", id: "au-wide", body: `{"agent_id":"","team_id":"crew-1"}`, status: http.StatusBadRequest,
			errText: "a crew pinned to a project cannot run an automation for the whole workspace",
			stored:  "project -, agent agent-1, crew -"},
		{name: "retargeting a project's automation to another workspace's agent",
			user: "editor", id: "au-p1", body: `{"agent_id":"agent-x"}`, status: http.StatusBadRequest,
			errText: "agent not found", stored: "project proj-1, agent agent-1, crew -"},
		{name: "an edit that neither moves nor retargets an automation saved before the check is not refused",
			user: "admin", id: "au-legacy", body: `{"name":"Renamed","team_id":"crew-1"}`, status: http.StatusOK,
			stored: "project -, agent -, crew crew-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := scopeFixture(t, "")
			wantAnswer(t, sendAutomation(h, tc.user, tc.id, tc.body), tc.status, tc.errText)
			if got := scopeOfStored(t, repo, tc.id); got != tc.stored {
				t.Errorf("stored %s, want %s", got, tc.stored)
			}
		})
	}
}

// TestMovingAnAutomationWaitsForTheFeature: on the stable channel before
// the release that carries workspace-automations has turned on, a move is
// refused with the feature gate's answer, and the automation stays where it
// was. Creating a whole-workspace automation, which admins could already do
// through the API, is not gated, nor is an update that does not move one.
func TestMovingAnAutomationWaitsForTheFeature(t *testing.T) {
	h, repo := scopeFixture(t, orgs.PlanBusiness)
	wantAnswer(t, sendAutomation(h, "admin", "au-p1", `{"project_id":""}`), http.StatusForbidden, featureGateMessage)
	if got, want := scopeOfStored(t, repo, "au-p1"), "project proj-1, agent agent-1, crew -"; got != want {
		t.Errorf("stored %s, want %s, unmoved", got, want)
	}
	wantAnswer(t, sendAutomation(h, "admin", "au-wide", `{"project_id":null,"name":"Renamed"}`), http.StatusOK, "")
	wantAnswer(t, sendAutomation(h, "admin", "", `{"name":"Wide","kind":"manual","agent_id":"agent-1"}`),
		http.StatusCreated, "")
}

// TestWhereAnAutomationMayRun pins the create side: a whole-workspace
// automation's target must be one its workspace can launch in any of its
// projects, an agent of the workspace or a crew pinned to no project; a
// project's may also have a crew pinned to that project. An agent or crew of
// another workspace answers as one no row has.
func TestWhereAnAutomationMayRun(t *testing.T) {
	cases := []struct {
		name, user, body string
		status           int
		errText          string
	}{
		{"the whole workspace, the workspace's agent", "admin", `{"agent_id":"agent-1"}`, http.StatusCreated, ""},
		{"the whole workspace, a crew pinned to no project", "admin", `{"team_id":"crew-wide"}`, http.StatusCreated, ""},
		{"the whole workspace, a crew pinned to a project", "admin", `{"team_id":"crew-1"}`, http.StatusBadRequest,
			"a crew pinned to a project cannot run an automation for the whole workspace"},
		{"the whole workspace, an empty project_id", "admin", `{"agent_id":"agent-1","project_id":""}`,
			http.StatusCreated, ""},
		{"the whole workspace, as a project editor: the workspace admin guard first", "editor",
			`{"team_id":"crew-1"}`, http.StatusForbidden, ""},
		{"the whole workspace, another workspace's agent", "admin", `{"agent_id":"agent-x"}`, http.StatusBadRequest,
			"agent not found"},
		{"the whole workspace, an agent no row has", "admin", `{"agent_id":"agent-gone"}`, http.StatusBadRequest,
			"agent not found"},
		{"the whole workspace, another workspace's crew", "admin", `{"team_id":"crew-x"}`, http.StatusBadRequest,
			"crew not found"},
		{"the whole workspace, a crew no row has", "admin", `{"team_id":"crew-gone"}`, http.StatusBadRequest,
			"crew not found"},
		{"a project, a crew pinned to it", "editor", `{"team_id":"crew-1","project_id":"proj-1"}`, http.StatusCreated, ""},
		{"a project, a crew pinned to no project", "editor", `{"team_id":"crew-wide","project_id":"proj-1"}`,
			http.StatusCreated, ""},
		{"a project, a crew pinned to another project", "editor", `{"team_id":"crew-2","project_id":"proj-1"}`,
			http.StatusBadRequest, "the crew is pinned to another project"},
		{"a project, another workspace's agent", "editor", `{"agent_id":"agent-x","project_id":"proj-1"}`,
			http.StatusBadRequest, "agent not found"},
		{"another workspace's project its admin reaches", "admin", `{"agent_id":"agent-1","project_id":"proj-x"}`,
			http.StatusBadRequest, "project does not belong to this workspace"},
		{"no target: the service's own refusal", "admin", `{}`, http.StatusBadRequest,
			"exactly one of agent_id or team_id must be set"},
		{"two targets: the service's own refusal", "admin", `{"agent_id":"agent-x","team_id":"crew-x"}`,
			http.StatusBadRequest, "exactly one of agent_id or team_id must be set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := scopeFixture(t, "")
			before := len(repo.byID)
			body := `{"name":"Watch","kind":"manual"}`
			if tc.body != "{}" {
				body = `{"name":"Watch","kind":"manual",` + strings.TrimPrefix(tc.body, "{")
			}
			wantAnswer(t, sendAutomation(h, tc.user, "", body), tc.status, tc.errText)
			made := len(repo.byID) - before
			if want := map[bool]int{true: 1, false: 0}[tc.status == http.StatusCreated]; made != want {
				t.Errorf("stored %d new automations, want %d", made, want)
			}
		})
	}
}
