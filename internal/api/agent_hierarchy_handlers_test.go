package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// statsRuns answers AgentStats and records what it was asked.
type statsRuns struct {
	agentruns.Service
	asked []string
	since time.Time
}

func (s *statsRuns) AgentStats(orgID, projectID string, since time.Time) ([]agentruns.AgentStats, error) {
	s.asked = append(s.asked, orgID+"/"+projectID)
	s.since = since
	return []agentruns.AgentStats{{AgentID: "agent-1", Open: 2, Done: 5, CostUSD: 1.25, PendingProposals: 3}}, nil
}

// arrangeAgents answers Arrange: placements naming "ghost" are an unknown
// agent, and one of "loop" a cycle; anything else is stored.
type arrangeAgents struct {
	agents.Service
	got []agents.Placement
}

func (a *arrangeAgents) Arrange(orgID string, p []agents.Placement) ([]*agents.Agent, error) {
	for _, pl := range p {
		switch pl.Slug {
		case "ghost":
			return nil, fmt.Errorf("%w: ghost", agents.ErrNotFound)
		case "loop":
			return nil, agents.ErrHierarchyCycle
		}
	}
	a.got = append(a.got, p...)
	return []*agents.Agent{{ID: "agent-1", OrgID: orgID, Slug: p[0].Slug, ReportsTo: p[0].ReportsTo,
		GroupName: p[0].GroupName, SortOrder: p[0].SortOrder}}, nil
}

func hierarchyFixture(t *testing.T, plan string) (*Handler, *statsRuns, *arrangeAgents) {
	t.Helper()
	runs, agentSvc := &statsRuns{}, &arrangeAgents{}
	h := newTestHandler(t, func(h *Handler) {
		h.RunService = runs
		h.AgentService = agentSvc
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}}
		h.OrgService = &fakeOrgService{plan: plan, roles: map[string]map[string]string{
			"org-1": {"admin": orgs.RoleAdmin, "viewer": orgs.RoleMember},
		}}
		h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"viewer": members.RoleViewer},
		}}
	})
	return h, runs, agentSvc
}

func hierarchyRequest(method, path, userID, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	return r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-1"))
}

// GET /agent-stats answers every agent's numbers over 7 days in one call,
// for a reader of the project, and the project's org; a project the caller
// cannot read answers as one no row has.
func TestAgentStatsAnswersEveryAgentInOneCall(t *testing.T) {
	h, runs, _ := hierarchyFixture(t, "")
	w := httptest.NewRecorder()
	before := time.Now().UTC()
	h.GetAgentStats(w, hierarchyRequest(http.MethodGet, "/api/v1/agent-stats?project_id=proj-1", "viewer", ""))
	wantAnswer(t, w, http.StatusOK, "")
	var got agentStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Days != 7 || len(got.Agents) != 1 || got.Agents[0].Open != 2 || got.Agents[0].PendingProposals != 3 {
		t.Errorf("answer %s", w.Body.String())
	}
	if len(runs.asked) != 1 || runs.asked[0] != "org-1/proj-1" {
		t.Errorf("asked %v, want org-1/proj-1 once", runs.asked)
	}
	if d := before.Sub(runs.since); d < 7*24*time.Hour-time.Minute || d > 7*24*time.Hour+time.Minute {
		t.Errorf("since %s is not 7 days back", runs.since)
	}

	w = httptest.NewRecorder()
	h.GetAgentStats(w, hierarchyRequest(http.MethodGet, "/api/v1/agent-stats", "viewer", ""))
	wantAnswer(t, w, http.StatusBadRequest, "project_id is required")

	w = httptest.NewRecorder()
	h.GetAgentStats(w, hierarchyRequest(http.MethodGet, "/api/v1/agent-stats?project_id=proj-1", "stranger", ""))
	wantAnswer(t, w, http.StatusNotFound, "project not found")
	if len(runs.asked) != 1 {
		t.Errorf("a refused read asked for stats")
	}
}

// PUT /agent-arrangement is a workspace admin's, behind the agent-hierarchy
// feature, and answers the service's refusals: an unknown agent 404, a cycle
// 400.
func TestArrangeAgents(t *testing.T) {
	body := `{"agents":[{"slug":"analyst","reports_to":"lead","group_name":"Requirements","sort_order":2}]}`
	h, _, agentSvc := hierarchyFixture(t, "")
	w := httptest.NewRecorder()
	h.ArrangeAgents(w, hierarchyRequest(http.MethodPut, "/api/v1/agent-arrangement", "admin", body))
	wantAnswer(t, w, http.StatusOK, "")
	if len(agentSvc.got) != 1 || agentSvc.got[0] != (agents.Placement{Slug: "analyst", ReportsTo: "lead",
		GroupName: "Requirements", SortOrder: 2}) {
		t.Errorf("arranged %+v", agentSvc.got)
	}

	for _, tc := range []struct {
		name, user, body string
		status           int
		errText          string
	}{
		{"a member", "viewer", body, http.StatusForbidden, ""},
		{"nothing to arrange", "admin", `{"agents":[]}`, http.StatusBadRequest, "agents is required"},
		{"an unknown agent", "admin", `{"agents":[{"slug":"ghost"}]}`, http.StatusNotFound, "agent not found"},
		{"a cycle", "admin", `{"agents":[{"slug":"loop","reports_to":"loop"}]}`, http.StatusBadRequest,
			agents.ErrHierarchyCycle.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ArrangeAgents(w, hierarchyRequest(http.MethodPut, "/api/v1/agent-arrangement", tc.user, tc.body))
			wantAnswer(t, w, tc.status, tc.errText)
		})
	}

	// On the stable channel before the release that carries the feature.
	h, _, agentSvc = hierarchyFixture(t, orgs.PlanBusiness)
	w = httptest.NewRecorder()
	h.ArrangeAgents(w, hierarchyRequest(http.MethodPut, "/api/v1/agent-arrangement", "admin", body))
	wantAnswer(t, w, http.StatusForbidden, featureGateMessage)
	if len(agentSvc.got) != 0 {
		t.Error("a gated arrangement was stored")
	}
}
