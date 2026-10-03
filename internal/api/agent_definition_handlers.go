package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerAgentDefinitionRoutes wires the agent definitions (file-backed):
// list, create, sync, read, update and delete, and the raw definition file.
func (h *Handler) registerAgentDefinitionRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agents", h.ListAgents).Methods("GET")
	router.HandleFunc("/api/v1/agents", h.CreateAgent).Methods("POST")
	router.HandleFunc("/api/v1/agents/sync", h.SyncAgents).Methods("POST")
	router.HandleFunc("/api/v1/agents/{slug}", h.GetAgent).Methods("GET")
	router.HandleFunc("/api/v1/agents/{slug}", h.UpdateAgent).Methods("PUT")
	router.HandleFunc("/api/v1/agents/{slug}", h.DeleteAgent).Methods("DELETE")
	router.HandleFunc("/api/v1/agents/{slug}/raw", h.GetAgentRaw).Methods("GET")
	router.HandleFunc("/api/v1/agents/{slug}/raw", h.SaveAgentRaw).Methods("PUT")
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.agentService.List(ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to list agents", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var def agents.Definition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate before the store does, so the definition rules — an allowlist
	// above all (REQ-91) — answer 400 with their own wording whatever the
	// service behind this happens to be.
	if err := def.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Friendly pre-check; the (org_id, slug) unique index is the real guard,
	// so a concurrent create that slips past this still conflicts below.
	if existing, _ := h.agentService.GetBySlug(ActiveOrg(r), def.Slug); existing != nil {
		writeJSONError(w, http.StatusConflict, "an agent with this slug already exists")
		return
	}
	agent, err := h.agentService.SaveDefinition(ActiveOrg(r), &def)
	if err != nil {
		if errors.Is(err, agents.ErrSlugExists) {
			writeJSONError(w, http.StatusConflict, "an agent with this slug already exists")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	agent, err := h.agentService.GetBySlug(ActiveOrg(r), mux.Vars(r)["slug"])
	if err != nil {
		respondInternal(w, r, "failed to load agent", err)
		return
	}
	if agent == nil {
		writeJSONError(w, http.StatusNotFound, "agent not found")
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	slug := mux.Vars(r)["slug"]
	var def agents.Definition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if def.Slug == "" {
		def.Slug = slug
	}
	if def.Slug != slug {
		writeJSONError(w, http.StatusBadRequest, "slug in body does not match URL")
		return
	}
	// Same rules as on create: an update may not take an agent's allowlist
	// away either (REQ-91).
	if err := def.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, err := h.agentService.SaveDefinition(ActiveOrg(r), &def)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	if err := h.agentService.Delete(ActiveOrg(r), mux.Vars(r)["slug"]); err != nil {
		if errors.Is(err, agents.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "agent not found")
			return
		}
		respondInternal(w, r, "failed to delete agent", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetAgentRaw(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	content, err := h.agentService.RawFile(ActiveOrg(r), mux.Vars(r)["slug"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent not found", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"content": content})
}

func (h *Handler) SaveAgentRaw(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agent, err := h.agentService.SaveRawFile(ActiveOrg(r), mux.Vars(r)["slug"], req.Content)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) SyncAgents(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	if err := h.agentService.SyncFromDisk(ActiveOrg(r)); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := h.agentService.List(ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to list agents", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}
