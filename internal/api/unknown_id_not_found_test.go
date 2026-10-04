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
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// An id that names nothing answers 404, the not-found answer of the route's
// siblings, and not the 500 of a failure on the server's side: a deleted
// agent's delete, an unknown pool node's release, and a delegation from a
// crew node removed after its run launched. Each test runs the real domain
// service over a store that holds nothing, so the not-found comes from the
// domain as it does in production (the S5d tour's agents_automations.json
// 40 and 41, providers_repos_pool.json 122, orchestration_budget.json 9).
// The stores answer as the Postgres repositories do, with their domain's
// not-found sentinel (#379 bug 88: agents and crew nodes answered nil, nil).

// emptyAgentStore is an agent registry with no agents.
type emptyAgentStore struct{ agents.Repository }

func (emptyAgentStore) FindBySlug(orgID, slug string) (*agents.Agent, error) {
	return nil, agents.ErrNotFound
}

func (emptyAgentStore) FindByID(id string) (*agents.Agent, error) { return nil, agents.ErrNotFound }

// agentReq is a request from a platform admin acting in workspace org-1.
func agentReq(method, path string, vars map[string]string, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = mux.SetURLVars(r, vars)
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	return r.WithContext(ctx)
}

// Reading an agent no row has answers 404 "agent not found", as it did when
// the store answered nil, nil, and not the 500 "failed to load agent" its
// error would otherwise be (#379 bug 88).
func TestGetAgentThatIsGoneAnswers404(t *testing.T) {
	svc, err := agents.NewFileService(t.TempDir(), emptyAgentStore{})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(HandlerDeps{AgentService: svc})
	for _, slug := range []string{"tour-doomed", "Not_A_Slug"} {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.GetAgent(w, agentReq(http.MethodGet, "/api/v1/agents/"+slug, map[string]string{"slug": slug}, ""))
			assertNotFound(t, w, "agent not found")
		})
	}
}

// Drafting test cases in a workspace with no test-case author answers 404
// with how to seed it, as it did when the store answered nil, nil, and not
// the 500 "failed to load the test-case author agent" (#379 bug 88).
func TestDraftTestCasesWithNoAuthorAnswers404(t *testing.T) {
	svc, err := agents.NewFileService(t.TempDir(), emptyAgentStore{})
	if err != nil {
		t.Fatal(err)
	}
	runs := &fakeRunService{}
	h := NewHandler(HandlerDeps{AgentService: svc, RunService: runs,
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}}})
	w := httptest.NewRecorder()
	h.DraftTestCases(w, agentReq(http.MethodPost, "/api/v1/projects/proj-1/draft-test-cases", map[string]string{"id": "proj-1"},
		`{"requirement_ids":["11111111-1111-4111-8111-111111111111"]}`))
	assertNotFound(t, w, "the test-case author agent is not available in this workspace; sync agents from disk to seed it")
	if len(runs.launchReqs) != 0 {
		t.Errorf("a draft with no author launched %d runs", len(runs.launchReqs))
	}
}

// Creating an agent goes on to store it when the registry answers the new
// slug with agents.ErrNotFound (#379 bug 88): the create's own lookup reads
// that answer as a slug no agent has.
func TestCreateAgentOverASentinelStore(t *testing.T) {
	store := &savingAgentStore{}
	svc, err := agents.NewFileService(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(HandlerDeps{AgentService: svc})
	w := httptest.NewRecorder()
	h.CreateAgent(w, agentReq(http.MethodPost, "/api/v1/agents", nil,
		`{"slug":"drafter","name":"Drafter","provider":"claude-code","allowed_tools":["mcp__openv__*"]}`))
	if w.Code != http.StatusCreated || len(store.saved) != 1 {
		t.Fatalf("create over a store answering ErrNotFound: %d %q, %d saved; want 201 and the agent saved",
			w.Code, w.Body.String(), len(store.saved))
	}
}

// savingAgentStore is an empty agent registry that records what is saved.
type savingAgentStore struct {
	emptyAgentStore
	saved []*agents.Agent
}

func (s *savingAgentStore) Save(a *agents.Agent) error {
	s.saved = append(s.saved, a)
	return nil
}

// vanishingProjects holds one project, which is gone by the time it is
// written to: what a project deleted between a route's guard and its write
// looks like to the project service.
type vanishingProjects struct{ projects.Repository }

func (vanishingProjects) GetByID(id string) (*projects.Project, error) {
	if id == "proj-1" {
		return &projects.Project{ID: id, OrgID: "org-1", Name: "Going", AgentAuth: projects.AgentAuthUserAccount}, nil
	}
	return nil, projects.ErrNotFound
}
func (vanishingProjects) Update(*projects.Project) error           { return projects.ErrNotFound }
func (vanishingProjects) Delete(string) (*projects.Removed, error) { return nil, projects.ErrNotFound }

// A project deleted between the guard and the write answers as one no row
// has, 404 "project not found", where its update and delete answered 500
// (#379 bug 88: the repository's not-found had no sentinel to tell it by).
func TestAProjectGoneBeforeItsWriteAnswers404(t *testing.T) {
	h := NewHandler(HandlerDeps{ProjectService: projects.NewService(vanishingProjects{})})
	vars := map[string]string{"id": "proj-1"}
	w := httptest.NewRecorder()
	h.UpdateProject(w, agentReq(http.MethodPut, "/api/v1/projects/proj-1", vars, `{"name":"Renamed"}`))
	assertNotFound(t, w, "project not found")
	w = httptest.NewRecorder()
	h.DeleteProject(w, agentReq(http.MethodDelete, "/api/v1/projects/proj-1", vars, ""))
	assertNotFound(t, w, "project not found")
}

func TestDeleteAgentThatIsGoneAnswers404(t *testing.T) {
	svc, err := agents.NewFileService(t.TempDir(), emptyAgentStore{})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(HandlerDeps{AgentService: svc})
	for _, slug := range []string{"tour-doomed", "Not_A_Slug"} {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.DeleteAgent(w, agentReq(http.MethodDelete, "/api/v1/agents/"+slug, map[string]string{"slug": slug}, ""))
			assertNotFound(t, w, "agent not found")
		})
	}
}

// emptyPool is a runner pool with no nodes.
type emptyPool struct{ runnersessions.Repository }

func (emptyPool) FindNodeByID(id string) (*runnersessions.Node, error) { return nil, nil }

func TestReleaseUnknownPoolNodeAnswers404(t *testing.T) {
	h := NewHandler(HandlerDeps{RunnerSessionService: runnersessions.NewDefaultService(emptyPool{}, nil)})
	const node = "3f6c2a4e-8d1b-4c7a-9e5f-0a1b2c3d4e5f"
	r := httptest.NewRequest(http.MethodPost, "/api/v1/runner-pool/nodes/"+node+"/release", strings.NewReader(`{}`))
	r = mux.SetURLVars(r, map[string]string{"id": node})
	r = r.WithContext(context.WithValue(r.Context(), ctxPoolNode, true))
	w := httptest.NewRecorder()
	h.ReleasePoolNode(w, r)
	// The heartbeat's answer for the same node: a 404, which the runner does
	// not retry as it does a 5xx (its heartbeat's 404 has it register again).
	assertNotFound(t, w, "pool node is not registered")
}

// emptyCrews is a crew store with no nodes.
type emptyCrews struct{ teams.Repository }

func (emptyCrews) FindNodeByID(id string) (*teams.Node, error) { return nil, teams.ErrNodeNotFound }

func TestDelegateFromRemovedCrewNodeAnswers404(t *testing.T) {
	runs := &fakeRunService{}
	h := NewHandler(HandlerDeps{TeamService: teams.NewDefaultService(emptyCrews{}), RunService: runs})
	node, team, project := "node-gone", "crew-1", "proj-1"
	run := &agentruns.Run{ID: "run-1", OrgID: "org-1", AgentID: "agent-1", ProjectID: &project, TeamID: &team, TeamNodeID: &node}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/delegate",
		strings.NewReader(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	r = r.WithContext(context.WithValue(r.Context(), ctxRun, run))
	w := httptest.NewRecorder()
	h.DelegateRun(w, r)
	// The answer the crew-node routes give a node id no node has.
	assertNotFound(t, w, "team node not found")
	if len(runs.launchReqs) != 0 {
		t.Errorf("a delegation from a removed node launched %d runs", len(runs.launchReqs))
	}
}

func assertNotFound(t *testing.T, w *httptest.ResponseRecorder, message string) {
	t.Helper()
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %q)", w.Code, w.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != message {
		t.Errorf("body = %q, want the error %q", w.Body.String(), message)
	}
}
