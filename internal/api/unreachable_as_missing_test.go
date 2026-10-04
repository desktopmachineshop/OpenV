package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Issue #379's decisions 10, 12, 13 and 15, at the handlers: a resource the
// caller cannot reach answers exactly as one no row has (I3, OpenV REQ-17);
// a platform admin's event and run lists hold the named project's rows; a
// plain member's proposal list with no project is a bad request; and a
// project role for an account no row has is that account's 404.

// unreachableFixture is project P (proj-1) of workspace W (org-1), with an
// artifact, a run and a proposal in it, and W's people-team team-1, its
// workspace-wide crew crew-1, attribute definition def-1 and an unscoped
// run, which "stranger" cannot reach: it belongs to workspace org-own only,
// as editor of its project proj-own, where it has an interview, and as the
// admin of org-own, whose workspace-wide crew crew-own it may copy. Org-own
// also has a run in proj-own, run-own, and one in its project proj-sib,
// run-sib. The projects are the real service's over a map, so an update
// checks a parent as it does against the database.
func unreachableFixture() *Handler {
	own, ownRun, sib := "proj-1", "proj-own", "proj-sib"
	w, ownOrg := "org-1", "org-own"
	return NewHandler(HandlerDeps{
		ProjectService: projects.NewService(mapProjects{
			"proj-1":   {ID: "proj-1", OrgID: "org-1"},
			"proj-own": {ID: "proj-own", OrgID: "org-own"},
			"proj-sib": {ID: "proj-sib", OrgID: "org-own"},
		}),
		OrgTeamService: mapOrgTeams{byID: map[string]*orgs.OrgTeam{"team-1": {ID: "team-1", OrgID: "org-1"}}},
		TeamService: &fakeCrewGraphs{graphs: map[string]*teams.TeamGraph{
			"crew-own": {Team: &teams.Team{ID: "crew-own", OrgID: ownOrg, Name: "Own"}},
			"crew-1":   {Team: &teams.Team{ID: "crew-1", OrgID: w, Name: "W's"}},
		}},
		InterviewService: &oneInterview{interview: &interviews.Interview{ID: "iv-own", ProjectID: "proj-own"}},
		AttributeService: &fakeAttributeService{byID: map[string]*attributes.Definition{
			"def-1": {ID: "def-1", OrgID: &w, Key: "w"},
		}},
		OrgService: &roleAnyOrgs{fakeOrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1":   {"owner": orgs.RoleAdmin},
			"org-own": {"stranger": orgs.RoleAdmin},
		}}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-own": {"stranger": members.RoleEditor},
		}},
		ArtifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"art-1":   {ID: "art-1", ProjectID: "proj-1", Type: "requirement"},
			"art-own": {ID: "art-own", ProjectID: "proj-own", Type: "requirement"},
		}},
		LinkService: &fakeLinkService{},
		RunService: &fakeRunService{byID: map[string]*agentruns.Run{
			"run-1":        {ID: "run-1", OrgID: "org-1", ProjectID: &own},
			"run-unscoped": {ID: "run-unscoped", OrgID: "org-1"},
			"run-own":      {ID: "run-own", OrgID: ownOrg, ProjectID: &ownRun},
			"run-sib":      {ID: "run-sib", OrgID: ownOrg, ProjectID: &sib},
		}},
		ProposalService: &fakeProposalService{byID: map[string]*proposals.Proposal{
			"prop-1": {ID: "prop-1", ProjectID: "proj-1"},
		}},
	})
}

// mapProjects is a project store over a map: what the service's reads and
// updates ask of it.
type mapProjects map[string]*projects.Project

func (m mapProjects) GetByID(id string) (*projects.Project, error) {
	if p, ok := m[id]; ok {
		copied := *p
		return &copied, nil
	}
	return nil, errors.New("project not found")
}

func (m mapProjects) Update(p *projects.Project) error { m[p.ID] = p; return nil }

func (mapProjects) Create(*projects.Project) error                   { return nil }
func (mapProjects) GetAll() ([]*projects.Project, error)             { return nil, nil }
func (mapProjects) ListByOrg(string) ([]*projects.Project, error)    { return nil, nil }
func (mapProjects) Delete(string) error                              { return nil }
func (mapProjects) ListChildren(string) ([]*projects.Project, error) { return nil, nil }

// mapOrgTeams is a people-team store that answers GetTeam from a map.
type mapOrgTeams struct {
	orgs.TeamService
	byID map[string]*orgs.OrgTeam
}

func (m mapOrgTeams) GetTeam(id string) (*orgs.OrgTeam, error) {
	if t, ok := m.byID[id]; ok {
		return t, nil
	}
	return nil, errors.New("team not found")
}

// oneInterview is an interview store holding one interview.
type oneInterview struct {
	interviews.Service
	interview *interviews.Interview
}

func (o *oneInterview) GetInterview(id string) (*interviews.Interview, error) {
	if id == o.interview.ID {
		return o.interview, nil
	}
	return nil, errors.New("interview not found")
}

// roleAnyOrgs answers RoleInOrgAny from the roles, as RoleInOrg does.
type roleAnyOrgs struct{ *fakeOrgService }

func (f *roleAnyOrgs) RoleInOrgAny(orgID, userID string) (string, error) {
	return f.roles[orgID][userID], nil
}

// asStranger sends the request as "stranger", acting in its own workspace.
func asStranger(method, target, body string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r = mux.SetURLVars(r, vars)
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "stranger"})
	return r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-own"))
}

func TestAResourceTheCallerCannotReachAnswersAsOneNoRowHas(t *testing.T) {
	type send func(h *Handler, id string) *httptest.ResponseRecorder
	path := func(handler func(*Handler) http.HandlerFunc, method string) send {
		return func(h *Handler, id string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			handler(h)(w, asStranger(method, "/", "", map[string]string{"id": id}))
			return w
		}
	}
	body := func(handler func(*Handler) http.HandlerFunc, format string) send {
		return func(h *Handler, id string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			handler(h)(w, asStranger(http.MethodPost, "/", strings.ReplaceAll(format, "ID", id), nil))
			return w
		}
	}
	// on sends a body naming the id to a route of the stranger's own
	// resource, pathID.
	on := func(handler func(*Handler) http.HandlerFunc, method, pathID, format string) send {
		return func(h *Handler, id string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			handler(h)(w, asStranger(method, "/", strings.ReplaceAll(format, "ID", id), map[string]string{"id": pathID}))
			return w
		}
	}
	// pathWith sends the id in the path with a well-formed body, which the
	// handler decodes before it looks the id up.
	pathWith := func(handler func(*Handler) http.HandlerFunc, method, body string) send {
		return func(h *Handler, id string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			handler(h)(w, asStranger(method, "/", body, map[string]string{"id": id}))
			return w
		}
	}
	// inQuery sends the id as the query's project_id.
	inQuery := func(handler func(*Handler) http.HandlerFunc, body string) send {
		return func(h *Handler, id string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			handler(h)(w, asStranger(http.MethodPost, "/?project_id="+id, body, nil))
			return w
		}
	}
	for _, tc := range []struct {
		name       string
		real       string
		send       send
		wantStatus int
		wantBody   string
	}{
		{"a project", "proj-1", path(func(h *Handler) http.HandlerFunc { return h.GetProject }, http.MethodGet),
			http.StatusNotFound, "project not found"},
		{"a project's AI map", "proj-1", path(func(h *Handler) http.HandlerFunc { return h.ProjectAIMap }, http.MethodGet),
			http.StatusNotFound, "project not found"},
		{"a project's members", "proj-1", path(func(h *Handler) http.HandlerFunc { return h.ListProjectMembers }, http.MethodGet),
			http.StatusNotFound, "project not found"},
		{"an artifact", "art-1", path(func(h *Handler) http.HandlerFunc { return h.GetArtifact }, http.MethodGet),
			http.StatusNotFound, "artifact not found"},
		{"an agent run", "run-1", path(func(h *Handler) http.HandlerFunc { return h.GetAgentRun }, http.MethodGet),
			http.StatusNotFound, "agent run not found"},
		{"a workspace", "org-1", path(func(h *Handler) http.HandlerFunc { return h.GetOrg }, http.MethodGet),
			http.StatusNotFound, "workspace not found"},
		{"a deleted workspace's restore", "org-1", path(func(h *Handler) http.HandlerFunc { return h.RestoreOrg }, http.MethodPost),
			http.StatusNotFound, "workspace not found"},
		{"a link's source", "art-1", body(func(h *Handler) http.HandlerFunc { return h.CreateLink },
			`{"from_id":"ID","to_id":"art-own","type":"relates-to"}`), http.StatusBadRequest, "source artifact not found"},
		{"a link's target", "art-1", body(func(h *Handler) http.HandlerFunc { return h.CreateLink },
			`{"from_id":"art-own","to_id":"ID","type":"relates-to"}`), http.StatusBadRequest, "target artifact not found"},
		{"a crew's pin", "proj-1", body(func(h *Handler) http.HandlerFunc { return h.CreateTeam },
			`{"name":"Crew","project_id":"ID"}`), http.StatusBadRequest, "project not found"},
		{"a crew copy's pin", "proj-1", on(func(h *Handler) http.HandlerFunc { return h.CloneTeam },
			http.MethodPost, "crew-own", `{"name":"Copy","project_id":"ID"}`), http.StatusBadRequest, "project not found"},
		{"a crew import's pin", "proj-1", inQuery(func(h *Handler) http.HandlerFunc { return h.ImportCrew }, `{}`),
			http.StatusBadRequest, "project not found"},
		{"an artifact's update", "art-1", pathWith(func(h *Handler) http.HandlerFunc { return h.UpdateArtifact },
			http.MethodPut, `{"title":"Renamed"}`), http.StatusNotFound, "artifact not found"},
		{"an artifact's restore", "art-1", pathWith(func(h *Handler) http.HandlerFunc { return h.RestoreArtifactVersion },
			http.MethodPost, `{"version":1}`), http.StatusNotFound, "artifact not found"},
		{"a flow-down parent", "proj-1", on(func(h *Handler) http.HandlerFunc { return h.UpdateProject },
			http.MethodPut, "proj-own", `{"parent_project_id":"ID"}`), http.StatusBadRequest, "parent project not found"},
		{"a people-team granted a project", "team-1", on(func(h *Handler) http.HandlerFunc { return h.GrantProjectTeamAccess },
			http.MethodPut, "proj-own", `{"org_team_id":"ID","role":"viewer"}`), http.StatusNotFound, "team not found"},
		{"a work item's crew assignee", "crew-1", on(func(h *Handler) http.HandlerFunc { return h.CreateWorkItem },
			http.MethodPost, "proj-own", `{"title":"T","assignee_type":"team","assignee_id":"ID"}`), http.StatusNotFound, "team not found"},
		{"a new interview's persona", "art-1", on(func(h *Handler) http.HandlerFunc { return h.CreateInterview },
			http.MethodPost, "proj-own", `{"name":"I","persona_artifact_id":"ID"}`), http.StatusBadRequest, "persona artifact not found"},
		{"an interview's persona", "art-1", on(func(h *Handler) http.HandlerFunc { return h.SetInterviewPersona },
			http.MethodPut, "iv-own", `{"persona_artifact_id":"ID"}`), http.StatusBadRequest, "persona artifact not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.send(unreachableFixture(), "phantom")
			got := tc.send(unreachableFixture(), tc.real)
			if want.Code != tc.wantStatus || strings.TrimSpace(want.Body.String()) != `{"error":"`+tc.wantBody+`"}` {
				t.Fatalf("an id no row has: %d %q, want %d %s", want.Code, want.Body.String(), tc.wantStatus, tc.wantBody)
			}
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Fatalf("a real id the caller cannot reach: %d %q; an id no row has: %d %q", got.Code,
					got.Body.String(), want.Code, want.Body.String())
			}
		})
	}

	// A proposal in a bulk review, which answers each id in its body.
	h := unreachableFixture()
	w := httptest.NewRecorder()
	h.BulkReviewProposals(w, asStranger(http.MethodPost, "/", `{"action":"approve","ids":["prop-1","phantom"]}`, nil))
	var bulk struct {
		Results []bulkOutcome `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bulk); err != nil || len(bulk.Results) != 2 {
		t.Fatalf("bulk review: %d %q", w.Code, w.Body.String())
	}
	if bulk.Results[0].Error != bulk.Results[1].Error || bulk.Results[0].Error != "proposal not found" {
		t.Fatalf("bulk review of an unreachable and a phantom proposal: %+v, want both \"proposal not found\"", bulk.Results)
	}
}

// Another workspace's worker key or run token, at a route that looks its
// resource up before the workspace guard, gets the answer of an id no row
// has, where the guard's 401 for a caller with no session told it the
// resource existed; a run token polling a run outside its project, another
// workspace's or a sibling project's, likewise gets a run no row has
// (issue #379's decision 10). W's own key keeps the guard's 401, which a
// phantom of W's does not reveal to anyone outside W.
func TestAnotherWorkspacesKeyOrRunAnswersAsForAnIDNoRowHas(t *testing.T) {
	type caller func(r *http.Request) *http.Request
	with := func(key contextKey, value any) caller {
		return func(r *http.Request) *http.Request { return r.WithContext(context.WithValue(r.Context(), key, value)) }
	}
	ownKey := with(ctxWorkerOrg, "org-own")
	personalKey := func(r *http.Request) *http.Request {
		return with(ctxWorkerUser, "stranger")(ownKey(r))
	}
	ownRun := with(ctxRun, &agentruns.Run{ID: "run-own", OrgID: "org-own", ProjectID: func() *string { p := "proj-own"; return &p }()})
	send := func(handler func(*Handler) http.HandlerFunc, method string, as caller, id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/", strings.NewReader(`{"name":"renamed","user_id":"u"}`))
		w := httptest.NewRecorder()
		handler(unreachableFixture())(w, as(mux.SetURLVars(r, map[string]string{"id": id, "userId": "u"})))
		return w
	}
	for _, tc := range []struct {
		name    string
		handler func(*Handler) http.HandlerFunc
		method  string
		as      caller
		real    string
		want    string
	}{
		{"a people-team's update, another workspace's key", func(h *Handler) http.HandlerFunc { return h.UpdateOrgTeam },
			http.MethodPut, ownKey, "team-1", `404 {"error":"team not found"}`},
		{"a people-team's delete, a personal key", func(h *Handler) http.HandlerFunc { return h.DeleteOrgTeam },
			http.MethodDelete, personalKey, "team-1", `404 {"error":"team not found"}`},
		{"a people-team's new member, a run token", func(h *Handler) http.HandlerFunc { return h.AddOrgTeamMember },
			http.MethodPost, ownRun, "team-1", `404 {"error":"team not found"}`},
		{"a people-team's member removed, another workspace's key", func(h *Handler) http.HandlerFunc { return h.RemoveOrgTeamMember },
			http.MethodDelete, ownKey, "team-1", `404 {"error":"team not found"}`},
		{"a workspace-wide definition's update, another workspace's key", func(h *Handler) http.HandlerFunc { return h.UpdateAttributeDefinition },
			http.MethodPut, ownKey, "def-1", `404 {"error":"attribute definition not found"}`},
		{"a workspace-wide definition's delete, a run token", func(h *Handler) http.HandlerFunc { return h.DeleteAttributeDefinition },
			http.MethodDelete, ownRun, "def-1", `404 {"error":"attribute definition not found"}`},
		{"another workspace's run, polled as a delegation", func(h *Handler) http.HandlerFunc { return h.DelegateStatus },
			http.MethodGet, ownRun, "run-1", `404 {"error":"agent run not found"}`},
		{"a sibling project's run, polled as a delegation", func(h *Handler) http.HandlerFunc { return h.DelegateStatus },
			http.MethodGet, ownRun, "run-sib", `404 {"error":"agent run not found"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := func(w *httptest.ResponseRecorder) string {
				return strconv.Itoa(w.Code) + " " + strings.TrimSpace(w.Body.String())
			}
			if got := answer(send(tc.handler, tc.method, tc.as, "phantom")); got != tc.want {
				t.Fatalf("an id no row has: %s, want %s", got, tc.want)
			}
			if got := answer(send(tc.handler, tc.method, tc.as, tc.real)); got != tc.want {
				t.Fatalf("a real id of another workspace or project: %s, want %s as for an id no row has", got, tc.want)
			}
		})
	}

	// W's own key and a run of its own project keep what they got: the
	// guard's 401 for a caller with no session, and a run of the caller's
	// project that is not its child the delegation's 403.
	wKey := with(ctxWorkerOrg, "org-1")
	if w := send(func(h *Handler) http.HandlerFunc { return h.UpdateOrgTeam }, http.MethodPut, wKey, "team-1"); w.Code != http.StatusUnauthorized {
		t.Fatalf("W's key on W's people-team: %d %q, want the guard's 401", w.Code, w.Body.String())
	}
	sibRun := with(ctxRun, &agentruns.Run{ID: "run-other", OrgID: "org-own", ProjectID: func() *string { p := "proj-sib"; return &p }()})
	if w := send(func(h *Handler) http.HandlerFunc { return h.DelegateStatus }, http.MethodGet, sibRun, "run-sib"); w.Code != http.StatusForbidden {
		t.Fatalf("a run of the caller's own project, not its child: %d %q, want 403", w.Code, w.Body.String())
	}
}

// What the body names in a project or workspace the caller does reach is
// refused for what it is: a parent, a people-team, a work item's crew
// assignee or a persona of another workspace or project.
func TestWhatTheCallerReachesElsewhereIsRefusedAsSuch(t *testing.T) {
	h := unreachableFixture()
	h.OrgService.(*roleAnyOrgs).roles["org-1"]["stranger"] = orgs.RoleMember
	h.MemberService.(*fakeMemberService).roles["proj-1"] = map[string]string{"stranger": members.RoleViewer}
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		method  string
		pathID  string
		body    string
		want    string
	}{
		{"a flow-down parent", h.UpdateProject, http.MethodPut, "proj-own", `{"parent_project_id":"proj-1"}`,
			`400 {"error":"a parent project must be in the same workspace"}`},
		{"a people-team", h.GrantProjectTeamAccess, http.MethodPut, "proj-own", `{"org_team_id":"team-1","role":"viewer"}`,
			`400 {"error":"team belongs to a different workspace"}`},
		{"a work item's crew assignee", h.CreateWorkItem, http.MethodPost, "proj-own",
			`{"title":"T","assignee_type":"team","assignee_id":"crew-1"}`, `400 {"error":"team belongs to a different workspace"}`},
		{"a persona", h.CreateInterview, http.MethodPost, "proj-own", `{"name":"I","persona_artifact_id":"art-1"}`,
			`400 {"error":"persona artifact belongs to a different project"}`},
	} {
		w := httptest.NewRecorder()
		tc.handler(w, asStranger(tc.method, "/", tc.body, map[string]string{"id": tc.pathID}))
		if got := strconv.Itoa(w.Code) + " " + strings.TrimSpace(w.Body.String()); got != tc.want {
			t.Errorf("%s the caller reaches elsewhere: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// A caller who reads the project but lacks the role a write needs is still
// refused 403: the resource exists for it.
func TestAReaderStillGetsTheRoleRefusal(t *testing.T) {
	h := unreachableFixture()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"from_id":"art-1","to_id":"art-1","type":"relates-to"}`))
	h.MemberService.(*fakeMemberService).roles["proj-1"] = map[string]string{"reader": members.RoleViewer}
	h.CreateLink(w, r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "reader"})))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a viewer's link: %d %q, want the role's 403", w.Code, w.Body.String())
	}
}

// A platform admin passes P's guard with no role, and its event and run lists
// of P hold P's rows, filtered by P's workspace rather than by the one the
// admin acts in, as every other read of P answers it (issue #379's
// decision 12).
func TestAPlatformAdminListsTheNamedProjectsEventsAndRuns(t *testing.T) {
	runs := &fakeRunService{}
	h := NewHandler(HandlerDeps{
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}},
		OrgService:     &fakeOrgService{},
		MemberService:  &fakeMemberService{},
		RunService:     runs,
		EventRepo: &fakeEventRepo{byOrg: map[string][]events.Event{
			"org-1": {{ID: "e1", OrgID: "org-1", ProjectID: "proj-1", EventType: "artifact.updated"}},
		}},
	})
	admin := func(target string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true})
		return r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-admin"))
	}

	w := httptest.NewRecorder()
	h.ListDomainEvents(w, admin("/api/v1/events?project_id=proj-1"))
	var list []events.Event
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].ID != "e1" {
		t.Fatalf("the admin's events of P: %d %q, want P's event", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.ListAgentRuns(w, admin("/api/v1/agent-runs?project_id=proj-1"))
	if w.Code != http.StatusOK || len(runs.listFilters) != 1 || runs.listFilters[0].OrgID != "org-1" {
		t.Fatalf("the admin's runs of P: %d, filters %+v, want P's workspace org-1", w.Code, runs.listFilters)
	}
}

// A plain member's proposal list with no project_id is a request missing a
// parameter, 400; a workspace admin keeps its workspace-wide list (issue
// #379's decision 13).
func TestProposalListWithNoProjectAsksForOne(t *testing.T) {
	h := NewHandler(HandlerDeps{
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"member": orgs.RoleMember, "admin": orgs.RoleAdmin},
		}},
		ProposalService: &listingProposals{},
	})
	list := func(userID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/proposals", nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
		w := httptest.NewRecorder()
		h.ListProposals(w, r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-1")))
		return w
	}
	if w := list("member"); w.Code != http.StatusBadRequest || strings.TrimSpace(w.Body.String()) != `{"error":"project_id is required"}` {
		t.Fatalf("a member's list with no project: %d %q, want 400 project_id is required", w.Code, w.Body.String())
	}
	if w := list("admin"); w.Code != http.StatusOK {
		t.Fatalf("a workspace admin's list with no project: %d %q, want 200", w.Code, w.Body.String())
	}
}

// listingProposals answers every list with none.
type listingProposals struct{ proposals.Service }

func (listingProposals) List(orgID, projectID, status, runID string) ([]*proposals.Proposal, error) {
	return nil, nil
}

// A project role for an account no row has, or for an id that is not one,
// is the account's 404, where the store's refusal answered 500 (issue #379's
// decision 15).
func TestAProjectRoleForAnAccountNoRowHasAnswers404(t *testing.T) {
	h := NewHandler(HandlerDeps{
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}},
		OrgService:     &fakeOrgService{roles: map[string]map[string]string{"org-1": {"owner": orgs.RoleAdmin}}},
		MemberService:  unknownUserMembers{},
	})
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"role":"viewer"}`))
	r = mux.SetURLVars(r, map[string]string{"id": "proj-1", "userId": "not-a-uuid"})
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "owner"})
	w := httptest.NewRecorder()
	h.UpdateProjectMember(w, r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-1")))
	if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != `{"error":"user not found"}` {
		t.Fatalf("a role for an account no row has: %d %q, want 404 user not found", w.Code, w.Body.String())
	}
}

// unknownUserMembers is a membership store that knows no account: every role
// it is asked to set names one no row has.
type unknownUserMembers struct{ members.Service }

func (unknownUserMembers) RoleFor(projectID, userID string) (string, error) { return "", nil }
func (unknownUserMembers) SetRole(projectID, userID, role string) error {
	return members.ErrUnknownUser
}

// A download or its options naming a baseline no row has, by a well-formed
// id or a malformed one (the baseline service answers both, and another
// project's, baselines.ErrNotFound), answers 404 "baseline not found", as
// the report and the V&V reads answer it, where the download answered 500
// (issue #379's decision 15: a malformed id is never a 500).
func TestADownloadOfABaselineNoRowHasAnswers404(t *testing.T) {
	h := unreachableFixture()
	h.DownloadService = missingBaselines{}
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"the options", h.DownloadOptions},
		{"a JSON download", h.DownloadJSON},
		{"a PDF download", h.DownloadPDF},
	} {
		r := httptest.NewRequest(http.MethodGet, "/?baseline_id=not-a-uuid", nil)
		r = mux.SetURLVars(r, map[string]string{"id": "proj-1"})
		w := httptest.NewRecorder()
		tc.handler(w, r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "owner"})))
		if got := strconv.Itoa(w.Code) + " " + strings.TrimSpace(w.Body.String()); got != `404 {"error":"baseline not found"}` {
			t.Errorf("%s of a baseline no row has: %s, want 404 baseline not found", tc.name, got)
		}
	}
}

// missingBaselines is a download service that finds no baseline.
type missingBaselines struct{}

func (missingBaselines) Options(string, string) (*downloads.Options, error) {
	return nil, baselines.ErrNotFound
}
func (missingBaselines) Download(downloads.Request) (*downloads.Result, error) {
	return nil, baselines.ErrNotFound
}
