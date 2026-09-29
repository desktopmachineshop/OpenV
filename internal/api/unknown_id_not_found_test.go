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

// emptyAgentStore is an agent registry with no agents.
type emptyAgentStore struct{ agents.Repository }

func (emptyAgentStore) FindBySlug(orgID, slug string) (*agents.Agent, error) { return nil, nil }

func TestDeleteAgentThatIsGoneAnswers404(t *testing.T) {
	svc, err := agents.NewFileService(t.TempDir(), emptyAgentStore{})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(HandlerDeps{AgentService: svc})
	for _, slug := range []string{"tour-doomed", "Not_A_Slug"} {
		t.Run(slug, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+slug, nil)
			r = mux.SetURLVars(r, map[string]string{"slug": slug})
			ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true})
			ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
			w := httptest.NewRecorder()
			h.DeleteAgent(w, r.WithContext(ctx))
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

func (emptyCrews) FindNodeByID(id string) (*teams.Node, error) { return nil, nil }

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
