package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/hosting"
)

// hostedRunnerKeyName is the worker key name minted for hosted runners.
const hostedRunnerKeyName = "hosted-runner"

// workerOnlineWindow is how recently a key must have polled to count as
// online (worker_keys.last_used_at is touched on every poll).
const workerOnlineWindow = 30 * time.Second

// hostedProviderEnv maps provider_keys fields to the env var each vendor CLI
// reads inside the runner container.
var hostedProviderEnv = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"gemini":    "GEMINI_API_KEY",
}

// hostedContainerName derives the org's runner container name.
func hostedContainerName(orgID string) string {
	short := orgID
	if len(short) > 8 {
		short = short[:8]
	}
	return "openv-runner-" + short
}

func (h *Handler) hostedRunnersEnabled() bool {
	return h.provisioner != nil && h.provisioner.Enabled()
}

// keyOnline reports whether a worker key has polled recently.
func (h *Handler) keyOnline(orgID string, keyID *string) bool {
	if keyID == nil {
		return false
	}
	key, err := h.workerKeyService.Get(orgID, *keyID)
	if err != nil || key == nil || key.Revoked {
		return false
	}
	return key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < workerOnlineWindow
}

// GetHostedRunner returns the workspace's hosted runner record plus live
// container/online state (admin).
func (h *Handler) GetHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	record, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load hosted runner", err)
		return
	}
	containerState := ""
	online := false
	if record != nil {
		if h.hostedRunnersEnabled() {
			if state, err := h.provisioner.ContainerState(record.ContainerName); err == nil {
				containerState = state
			}
		}
		online = h.keyOnline(orgID, record.WorkerKeyID)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"record":          record,
		"enabled":         h.hostedRunnersEnabled(),
		"container_state": containerState,
		"online":          online,
	})
}

// CreateHostedRunner provisions the workspace's hosted runner container
// (admin). Provider API keys travel to the container environment only — they
// are never persisted by the platform.
func (h *Handler) CreateHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.hostedRunnersEnabled() {
		writeJSONError(w, http.StatusBadRequest, "hosted runners are not enabled on this deployment")
		return
	}
	if err := h.checkFlag(orgID, orgs.LimitHostedAutomation); err != nil {
		h.writeLimitError(w, err)
		return
	}
	var req struct {
		ProviderKeys map[string]string `json:"provider_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load hosted runner", err)
		return
	}
	if existing != nil {
		writeJSONError(w, http.StatusConflict, "a hosted runner already exists for this workspace")
		return
	}

	// The container is capped at the org's effective limits (explicit
	// org limits merged over its plan's defaults).
	org, err := h.orgService.Get(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load workspace", err)
		return
	}
	limits := hosting.ResourceLimitsForOrg(org)

	// Mint a workspace worker key for the container (plaintext shown to the
	// container env only).
	key, plaintext, err := h.workerKeyService.Create(orgID, hostedRunnerKeyName, CurrentUserID(r), nil)
	if err != nil {
		respondInternal(w, r, "failed to create worker key", err)
		return
	}

	extraEnv := map[string]string{}
	for provider, envName := range hostedProviderEnv {
		if v := strings.TrimSpace(req.ProviderKeys[provider]); v != "" {
			extraEnv[envName] = v
		}
	}

	keyID := key.ID
	record, err := h.hostedWorkerService.Create(orgID, hostedContainerName(orgID), &keyID, CurrentUserID(r))
	if err != nil {
		_ = h.workerKeyService.Revoke(orgID, key.ID)
		respondInternal(w, r, "failed to create hosted runner", err)
		return
	}

	status, detail := hostedworkers.StatusRunning, ""
	if err := h.provisioner.Provision(orgID, record.ContainerName, plaintext, extraEnv, limits); err != nil {
		status, detail = hostedworkers.StatusError, err.Error()
	}
	record, err = h.hostedWorkerService.SetStatus(record.ID, status, detail)
	if err != nil {
		respondInternal(w, r, "failed to update hosted runner status", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(record)
}

// hostedRunnerChecked loads the org's hosted runner record after enforcing
// admin role; writes the error response when absent.
func (h *Handler) hostedRunnerChecked(w http.ResponseWriter, r *http.Request, orgID string) *hostedworkers.HostedWorker {
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return nil
	}
	record, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load hosted runner", err)
		return nil
	}
	if record == nil {
		writeJSONError(w, http.StatusNotFound, "no hosted runner for this workspace")
		return nil
	}
	return record
}

// StartHostedRunner starts a stopped hosted runner container (admin).
func (h *Handler) StartHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	record := h.hostedRunnerChecked(w, r, orgID)
	if record == nil {
		return
	}
	if err := h.provisioner.Start(record.ContainerName); err != nil {
		respondInternal(w, r, "failed to start hosted runner", err)
		return
	}
	updated, err := h.hostedWorkerService.SetStatus(record.ID, hostedworkers.StatusRunning, "")
	if err != nil {
		respondInternal(w, r, "failed to update hosted runner status", err)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

// StopHostedRunner stops the hosted runner container (admin).
func (h *Handler) StopHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	record := h.hostedRunnerChecked(w, r, orgID)
	if record == nil {
		return
	}
	if err := h.provisioner.Stop(record.ContainerName); err != nil {
		respondInternal(w, r, "failed to stop hosted runner", err)
		return
	}
	updated, err := h.hostedWorkerService.SetStatus(record.ID, hostedworkers.StatusStopped, "")
	if err != nil {
		respondInternal(w, r, "failed to update hosted runner status", err)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

// DeleteHostedRunner removes the hosted runner container (and optionally its
// data volume with ?purge=true), revokes its worker key, and deletes the
// record (admin).
func (h *Handler) DeleteHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	record := h.hostedRunnerChecked(w, r, orgID)
	if record == nil {
		return
	}
	purge := r.URL.Query().Get("purge") == "true"
	if h.hostedRunnersEnabled() {
		if err := h.provisioner.Remove(record.ContainerName, purge, orgID); err != nil {
			respondInternal(w, r, "failed to remove hosted runner", err)
			return
		}
	}
	if record.WorkerKeyID != nil {
		_ = h.workerKeyService.Revoke(orgID, *record.WorkerKeyID)
	}
	if err := h.hostedWorkerService.Delete(record.ID); err != nil {
		respondInternal(w, r, "failed to delete hosted runner", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
