package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// registerProviderSettingsRoutes wires the provider settings and a worker's
// provider availability report.
func (h *Handler) registerProviderSettingsRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/provider-settings", h.ListProviderSettings).Methods("GET")
	router.HandleFunc("/api/v1/provider-settings", h.UpsertProviderSetting).Methods("PUT")
	router.HandleFunc("/api/v1/provider-settings/detect", h.RecordProviderDetection).Methods("POST")
}

func (h *Handler) ListProviderSettings(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.ProviderService.List(ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to list provider settings", err)
		return
	}
	// A provider the workspace's channel has not received is not offered.
	offered := list[:0]
	for _, setting := range list {
		if h.providerOffered(r, ActiveOrg(r), setting.Provider) {
			offered = append(offered, setting)
		}
	}
	json.NewEncoder(w).Encode(offered)
}

func (h *Handler) UpsertProviderSetting(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var setting providers.ProviderSetting
	if err := json.NewDecoder(r.Body).Decode(&setting); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	setting.OrgID = ActiveOrg(r)
	if !h.providerOffered(r, setting.OrgID, setting.Provider) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	if err := h.ProviderService.Upsert(&setting); err != nil {
		if errors.Is(err, providers.ErrInvalidSetting) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to save provider setting", err)
		}
		return
	}
	json.NewEncoder(w).Encode(setting)
}

// RecordProviderDetection stores the worker's provider availability report.
// Every provider the report names that the server knows is recorded, in name
// order, before one it refuses (a provider it does not know) answers 400: a
// runner reports all of its adapters at once, and which of them were stored
// must not depend on the order a Go map is ranged in.
func (h *Handler) RecordProviderDetection(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	var req workerproto.DetectionReport
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	names := make([]string, 0, len(req))
	for provider := range req {
		names = append(names, provider)
	}
	sort.Strings(names)
	var refused error
	for _, provider := range names {
		err := h.ProviderService.RecordDetection(WorkerOrg(r), provider, req[provider])
		switch {
		case err == nil:
		case errors.Is(err, providers.ErrInvalidSetting):
			if refused == nil {
				refused = err
			}
		default:
			respondInternal(w, r, "failed to record provider detection", err)
			return
		}
	}
	if refused != nil {
		writeJSONError(w, http.StatusBadRequest, refused.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
