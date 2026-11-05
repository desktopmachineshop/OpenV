package api

// The agent hierarchy (issue 733): the live numbers the Agents page's cards
// show, every agent's in one call, and the Edit mode's arrangement of who
// reports to whom. Display and arrangement only: no agent starts another.

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// agentStatsDays is the window of the Agents page's numbers.
const agentStatsDays = 7

// registerAgentHierarchyRoutes wires the Agents page's numbers and its
// arrangement. Neither path is under /agents/, where {slug} would take it.
func (h *Handler) registerAgentHierarchyRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-stats", h.GetAgentStats).Methods("GET")
	router.HandleFunc("/api/v1/agent-arrangement", h.ArrangeAgents).Methods("PUT")
}

// agentStatsResponse is GET /agent-stats' answer.
type agentStatsResponse struct {
	Days   int                    `json:"days"`
	Since  time.Time              `json:"since"`
	Agents []agentruns.AgentStats `json:"agents"`
}

// GetAgentStats answers each agent's numbers over the last 7 days in a
// project: its open and done runs, their cost, and the proposals of its runs
// waiting for review, in one call for every card on the page. A project the
// caller cannot read answers as one no row has.
func (h *Handler) GetAgentStats(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	if !h.requireProjectVisible(w, r, projectID, missing("project not found")) {
		return
	}
	project, err := h.ProjectService.GetProject(projectID)
	if err != nil || project == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -agentStatsDays)
	stats, err := h.RunService.AgentStats(project.OrgID, projectID, since)
	if err != nil {
		respondInternal(w, r, "failed to load agent stats", err)
		return
	}
	writeJSONBare(w, agentStatsResponse{Days: agentStatsDays, Since: since, Agents: stats})
}

// ArrangeAgents places agents of the active workspace in the hierarchy:
// `{"agents": [{slug, reports_to, group_name, sort_order}]}`, each written
// to the agent's file. The arrangement is checked whole: an agent the
// workspace does not have answers 404, a parent it does not have or a cycle
// 400. It answers the workspace's agents. Workspace admins only, as for an
// agent's edit, and behind the agent-hierarchy feature.
func (h *Handler) ArrangeAgents(w http.ResponseWriter, r *http.Request) {
	orgID := ActiveOrg(r)
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.featureEnabled(r, orgID, release.FeatureAgentHierarchy) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req struct {
		Agents []agents.Placement `json:"agents"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Agents) == 0 {
		writeJSONError(w, http.StatusBadRequest, "agents is required")
		return
	}
	list, err := h.AgentService.Arrange(orgID, req.Agents)
	switch {
	case errors.Is(err, agents.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "agent not found")
		return
	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, list)
}
