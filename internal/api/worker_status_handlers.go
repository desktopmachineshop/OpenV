package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerWorkerStatusRoutes wires the worker status, the workspace's runner
// fleet and queue depth, and the usage rollup, its run counts, tokens and
// cost by agent and by day (both member).
func (h *Handler) registerWorkerStatusRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/worker-status", h.GetWorkerStatus).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/usage", h.GetOrgUsage).Methods("GET")
}

// GetWorkerStatus returns the workspace's runner fleet and queue depth
// (member).
func (h *Handler) GetWorkerStatus(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	keys, err := h.WorkerKeyService.List(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list worker keys", err)
		return
	}
	hostedKeyID := ""
	if h.HostedWorkerService != nil {
		if record, err := h.HostedWorkerService.Get(orgID); err == nil && record != nil && record.WorkerKeyID != nil {
			hostedKeyID = *record.WorkerKeyID
		}
	}
	workers := []map[string]interface{}{}
	for _, key := range keys {
		online := !key.Revoked && key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < workerOnlineWindow
		workers = append(workers, map[string]interface{}{
			"id":           key.ID,
			"name":         key.Name,
			"personal":     key.UserID != nil,
			"hosted":       key.ID == hostedKeyID || key.Name == hostedRunnerKeyName,
			"user_name":    key.UserName,
			"online":       online,
			"revoked":      key.Revoked,
			"last_used_at": key.LastUsedAt,
		})
	}
	queue, err := h.RunService.QueueStats(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load queue stats", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": workers,
		"queue":   queue,
	})
}

// Usage-window bounds: ?days= is clamped to [1, maxUsageDays]; 0/absent
// falls back to defaultUsageDays.
const (
	defaultUsageDays = 30
	maxUsageDays     = 365
)

// GetOrgUsage rolls up the workspace's agent-run usage (runs, tokens, cost)
// by agent and by day over a trailing window (?days=30 by default).
// Access decision: every workspace member gets the org-wide read — the
// workspace is their shared context and the rollup carries no run content,
// only counts and spend. Admin-only gating was considered and rejected as
// needless friction; revisit if orgs ever want per-member spend privacy.
func (h *Handler) GetOrgUsage(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	// The workspace-wide rollup is the gated thing; a member's own runs
	// and their cost stay on the runs list whatever the plan.
	if err := h.checkFlag(orgID, orgs.LimitWorkspaceBudget); err != nil {
		h.writeLimitError(w, err)
		return
	}
	days := defaultUsageDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONError(w, http.StatusBadRequest, "days must be a positive integer")
			return
		}
		days = parsed
		if days > maxUsageDays {
			days = maxUsageDays
		}
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	summary, err := h.RunService.Usage(orgID, since)
	if err != nil {
		respondInternal(w, r, "failed to load usage", err)
		return
	}
	summary.Days = days
	// Month-to-date spend for the budget bar — independent of the trailing
	// window, and the same figure budget alerts fire against.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if spend, err := h.RunService.MonthlySpend(orgID, monthStart); err == nil {
		summary.MonthToDateCostUSD = spend
	}
	json.NewEncoder(w).Encode(summary)
}
