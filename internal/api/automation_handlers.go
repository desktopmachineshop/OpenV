package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/scheduler"
)

// registerAutomationRoutes wires the automations and running one now.
func (h *Handler) registerAutomationRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/automations", h.ListAutomations).Methods("GET")
	router.HandleFunc("/api/v1/automations", h.CreateAutomation).Methods("POST")
	router.HandleFunc("/api/v1/automations/{id}", h.GetAutomation).Methods("GET")
	router.HandleFunc("/api/v1/automations/{id}", h.UpdateAutomation).Methods("PUT")
	router.HandleFunc("/api/v1/automations/{id}", h.DeleteAutomation).Methods("DELETE")
	router.HandleFunc("/api/v1/automations/{id}/run-now", h.RunAutomationNow).Methods("POST")
}

func (h *Handler) ListAutomations(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.AutomationService.List(ActiveOrg(r), r.URL.Query().Get("project_id"))
	if err != nil {
		respondInternal(w, r, "failed to list automations", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	var req automations.CreateAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.OrgID = ActiveOrg(r)
	req.CreatedBy = CurrentUserID(r)
	if !h.requireAutomationWrite(w, r, req.ProjectID, req.OrgID, notFound{}) {
		return
	}
	if problem := h.automationPlacement(req.OrgID, req.ProjectID, req.AgentID, req.TeamID); problem != "" {
		writeJSONError(w, http.StatusBadRequest, problem)
		return
	}
	automation, err := h.AutomationService.Create(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(automation)
}

func (h *Handler) GetAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.AutomationService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireOrgRoleFor(w, r, automation.OrgID, orgs.RoleMember, missing("automation not found")) {
		return
	}
	json.NewEncoder(w).Encode(automation)
}

func (h *Handler) UpdateAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.AutomationService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID, missing("automation not found")) {
		return
	}
	var req automations.UpdateAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A move to another scope takes the write rights of where it goes too:
	// a workspace admin's for the whole workspace, an editor's for a
	// project. Moving is new with the workspace-automations feature, so a
	// workspace that has not received it cannot move one; a project_id
	// naming the scope the automation has already is no move.
	moved := req.ProjectID != nil && scopeOf(req.ProjectID) != scopeOf(automation.ProjectID)
	if moved {
		if !h.requireAutomationWrite(w, r, req.ProjectID, automation.OrgID, notFound{}) {
			return
		}
		if !h.featureEnabled(r, automation.OrgID, release.FeatureWorkspaceAutomations) {
			writeJSONError(w, http.StatusForbidden, featureGateMessage)
			return
		}
	}
	// Where it is to run, checked when the scope or the target changes, so
	// that an edit of anything else never refuses an automation saved
	// before the check existed.
	project, agent, team := automation.ProjectID, automation.AgentID, automation.TeamID
	if req.ProjectID != nil {
		project = req.ProjectID
	}
	if req.AgentID != nil {
		agent = req.AgentID
	}
	if req.TeamID != nil {
		team = req.TeamID
	}
	retargeted := scopeOf(agent) != scopeOf(automation.AgentID) || scopeOf(team) != scopeOf(automation.TeamID)
	if moved || retargeted {
		if problem := h.automationPlacement(automation.OrgID, project, agent, team); problem != "" {
			writeJSONError(w, http.StatusBadRequest, problem)
			return
		}
	}
	updated, err := h.AutomationService.Update(automation.ID, req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.AutomationService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID, missing("automation not found")) {
		return
	}
	if err := h.AutomationService.Delete(automation.ID); err != nil {
		respondInternal(w, r, "failed to delete automation", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// automationPlacement answers what is wrong with where an automation is to
// run, or "" when nothing is: its project must be one of its workspace's,
// and its target one that its scope can launch, an agent of the workspace,
// or a crew of the workspace that is pinned to no project or, for a
// project's automation, to that project. A whole-workspace automation runs
// in the project of each event that fires it, so a crew pinned to one
// project cannot serve it. An agent or crew of another workspace answers
// as one no row has (I3). With no target, or two, it answers "" and leaves
// the refusal to the service.
func (h *Handler) automationPlacement(orgID string, projectID, agentID, teamID *string) string {
	project := scopeOf(projectID)
	if project != "" {
		if p, err := h.ProjectService.GetProject(project); err != nil || p == nil || p.OrgID != orgID {
			return "project does not belong to this workspace"
		}
	}
	agent, team := scopeOf(agentID), scopeOf(teamID)
	switch {
	case agent != "" && team == "":
		if a, err := h.AgentService.Get(agent); err != nil || a == nil || a.OrgID != orgID {
			return "agent not found"
		}
	case team != "" && agent == "":
		graph, err := h.TeamService.GetTeam(team)
		if err != nil || graph == nil || graph.Team == nil || (graph.Team.OrgID != "" && graph.Team.OrgID != orgID) {
			return "crew not found"
		}
		if pin := h.teamPin(graph.Team); pin != "" && pin != project {
			if project == "" {
				return "a crew pinned to a project cannot run an automation for the whole workspace"
			}
			return "the crew is pinned to another project"
		}
	}
	return ""
}

// scopeOf is an optional id as a string: "" for none.
func scopeOf(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// RunAutomationNow launches an automation's run immediately.
func (h *Handler) RunAutomationNow(w http.ResponseWriter, r *http.Request) {
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
	if !requireUser(w, r) {
		return
	}
	automation, err := h.AutomationService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID, missing("automation not found")) {
		return
	}
	agentID, teamID, teamNodeID, err := scheduler.ResolveTarget(automation, h.TeamService)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	prompt := automations.RenderPrompt(automation.PromptTemplate, map[string]string{
		"automation.name": automation.Name,
	})
	if strings.TrimSpace(prompt) == "" {
		prompt = "Manual run of automation: " + automation.Name
	}
	automationID := automation.ID
	run, err := h.launchRun(r, agentruns.LaunchRequest{
		OrgID:        automation.OrgID,
		AgentID:      agentID,
		ProjectID:    automation.ProjectID,
		AutomationID: &automationID,
		TeamID:       teamID,
		TeamNodeID:   teamNodeID,
		Prompt:       prompt,
		LaunchedBy:   CurrentUserID(r),
	})
	if err != nil {
		writeLaunchError(w, r, launchErrs400, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}
