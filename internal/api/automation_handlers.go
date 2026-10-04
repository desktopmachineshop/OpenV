package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
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
	if prompt == "" {
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}
