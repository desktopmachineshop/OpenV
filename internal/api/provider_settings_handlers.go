package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// registerProviderSettingsRoutes wires the provider settings and a worker's
// provider availability report.
func (h *Handler) registerProviderSettingsRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/provider-settings", h.ListProviderSettings).Methods("GET")
	router.HandleFunc("/api/v1/provider-settings", h.UpsertProviderSetting).Methods("PUT")
	router.HandleFunc("/api/v1/provider-settings/detect", h.RecordProviderDetection).Methods("POST")
	router.HandleFunc("/api/v1/provider-api-keys", h.ListProviderAPIKeys).Methods("GET")
	router.HandleFunc("/api/v1/provider-api-keys/{env}", h.SetProviderAPIKey).Methods("PUT")
	router.HandleFunc("/api/v1/provider-api-keys/{env}", h.DeleteProviderAPIKey).Methods("DELETE")
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
	writeJSONBare(w, offered)
}

func (h *Handler) UpsertProviderSetting(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var setting providers.ProviderSetting
	if !decodeJSON(w, r, &setting) {
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
	writeJSONBare(w, setting)
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
	if !decodeJSON(w, r, &req) {
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

// providerAPIKeysResponse is the workspace's pasted keys, never their
// values: enabled is false when the server has no OPENV_SECRETS_KEY and so
// cannot store one. self_hosted is true on an operator's own deployment,
// whose runner hosts may set the key variables themselves, so the app names
// them there and nowhere else: on the hosted service they are a detail of
// how a run gets its key.
type providerAPIKeysResponse struct {
	Enabled    bool                   `json:"enabled"`
	Keys       []*providers.StoredKey `json:"keys"`
	SelfHosted bool                   `json:"self_hosted"`
}

// ListProviderAPIKeys answers which provider key variables have a pasted
// key in the active workspace, with each key's last four characters.
// Workspace admins only.
func (h *Handler) ListProviderAPIKeys(w http.ResponseWriter, r *http.Request) {
	orgID := ActiveOrg(r)
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	keys, err := h.ProviderKeyService.ListKeys(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list provider API keys", err)
		return
	}
	writeJSON(w, http.StatusOK, providerAPIKeysResponse{
		Enabled: h.ProviderKeyService.KeysEnabled(), Keys: keys, SelfHosted: orgs.SelfHosted(),
	})
}

// SetProviderAPIKey stores a pasted key for one provider key variable
// ({"key": "..."}), replacing any earlier one, and answers it without its
// value. Workspace admins only.
func (h *Handler) SetProviderAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID := ActiveOrg(r)
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.featureEnabled(r, orgID, release.FeatureWorkspaceAPIKeys) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	userID := ""
	if user := CurrentUser(r); user != nil {
		userID = user.ID
	}
	key, err := h.ProviderKeyService.SetKey(orgID, mux.Vars(r)["env"], req.Key, userID)
	switch {
	case errors.Is(err, providers.ErrInvalidKey):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, providers.ErrKeyStoreOff):
		writeJSONError(w, http.StatusServiceUnavailable, err.Error())
	case err != nil:
		respondInternal(w, r, "failed to save provider API key", err)
	default:
		writeJSON(w, http.StatusOK, key)
	}
}

// DeleteProviderAPIKey removes the pasted key for one provider key
// variable; runs then read it from their runner's host again. Not gated, so
// a key can always be taken back. Workspace admins only.
func (h *Handler) DeleteProviderAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID := ActiveOrg(r)
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	removed, err := h.ProviderKeyService.DeleteKey(orgID, mux.Vars(r)["env"])
	switch {
	case errors.Is(err, providers.ErrInvalidKey):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		respondInternal(w, r, "failed to remove provider API key", err)
	case !removed:
		writeJSONError(w, http.StatusNotFound, "no API key is stored for that variable")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
