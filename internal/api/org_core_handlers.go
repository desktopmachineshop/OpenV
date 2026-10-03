package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerOrgCoreRoutes wires the workspaces themselves: listing and
// creating them, reading, updating and deleting one, its plan, restoring a
// deleted one, and making one the session's active workspace.
func (h *Handler) registerOrgCoreRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs", h.ListOrgs).Methods("GET")
	router.HandleFunc("/api/v1/orgs", h.CreateOrg).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}", h.GetOrg).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}", h.UpdateOrg).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}", h.alwaysWritable(h.DeleteOrg)).Methods("DELETE")
	router.HandleFunc("/api/v1/orgs/{id}/plan", h.SetOrgPlan).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/restore", h.RestoreOrg).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/activate", h.alwaysWritable(h.ActivateOrg)).Methods("POST")
}

// ListOrgs returns the caller's workspaces with roles.
func (h *Handler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	list, err := h.orgService.ListForUser(user.ID)
	if r.URL.Query().Get("deleted") == "true" {
		list, err = h.orgService.ListDeletedForUser(user.ID)
	}
	if err != nil {
		respondInternal(w, r, "failed to list workspaces", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"orgs":       list,
		"active_org": ActiveOrg(r),
	})
}

// DeleteOrg soft-deletes a company workspace (admin). It disappears from
// pickers and every request against it is refused; it stays restorable for
// orgs.DeletionGraceDays, after which the daily purge hard-deletes it and all
// its data. Personal workspaces cannot be deleted.
func (h *Handler) DeleteOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	org, err := h.orgService.DeleteOrg(orgID)
	if err != nil {
		switch {
		case errors.Is(err, orgs.ErrPersonalOrgDelete):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, orgs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			respondInternal(w, r, "failed to delete workspace", err)
		}
		return
	}
	// A paid workspace stops being billed when its paid period ends, not
	// before: a restore inside the grace period takes this back.
	h.billing.OnWorkspaceDeleted(r.Context(), org)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted_at":  org.DeletedAt,
		"purge_after": org.DeletedAt.Add(orgs.DeletionGraceDays * 24 * time.Hour),
	})
}

// SetOrgPlan moves a workspace to another plan (REQ-154): {"plan"} → the
// workspace. Platform admins only: a plan is what the operator grants (the
// open-source tier, a negotiated enterprise plan), never something a
// workspace picks for itself, and a workspace admin who could raise their
// own plan would be raising their own limits. A grant (enterprise,
// open_source) over a live subscription is 409 already_subscribed: the
// subscription is cancelled first (REQ-168).
func (h *Handler) SetOrgPlan(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !user.IsAdmin {
		writeJSONError(w, http.StatusForbidden, "only a platform admin can change a workspace's plan")
		return
	}
	var req struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	org, err := h.orgService.SetPlan(mux.Vars(r)["id"], req.Plan)
	if err != nil {
		switch {
		case errors.Is(err, orgs.ErrInvalidPlan):
			writeJSONError(w, http.StatusBadRequest, "unknown plan: one of single, business_lite, business, enterprise, self_host, open_source")
		case errors.Is(err, orgs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, orgs.ErrBillingActive):
			// The checkout's refusal for the same state: a live
			// subscription decides the plan until it is cancelled.
			writeJSONErrorCode(w, http.StatusConflict, err.Error(), ErrCodeAlreadySubscribed)
		default:
			respondInternal(w, r, "failed to set the workspace plan", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(org)
}

// RestoreOrg brings a soft-deleted workspace back within the grace period.
// Deleted workspaces fail the normal role check by design, so authorization
// uses the any-state role lookup: platform admins and the workspace's own
// admins may restore. To a caller who is no member, the workspace answers as
// one no row has, as the workspace guard answers (I3).
func (h *Handler) RestoreOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !user.IsAdmin {
		role, err := h.orgService.RoleInOrgAny(orgID, user.ID)
		if err != nil {
			respondInternal(w, r, "failed to resolve workspace access", err)
			return
		}
		if role == "" {
			unknownWorkspace.write(w)
			return
		}
		if role != orgs.RoleAdmin {
			writeJSONError(w, http.StatusForbidden, "only workspace admins can restore a deleted workspace")
			return
		}
	}
	org, err := h.orgService.RestoreOrg(orgID)
	if err != nil {
		switch {
		case errors.Is(err, orgs.ErrNotDeleted):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, orgs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			respondInternal(w, r, "failed to restore workspace", err)
		}
		return
	}
	h.billing.OnWorkspaceRestored(r.Context(), org)
	json.NewEncoder(w).Encode(org)
}

// CreateOrg creates a company workspace with the caller as admin.
func (h *Handler) CreateOrg(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.checkSharedWorkspaceCount(user.ID); err != nil {
		if h.writeLimitError(w, err) {
			return
		}
		respondInternal(w, r, "failed to check the workspace limit", err)
		return
	}
	org, err := h.orgService.CreateOrg(req.Name, orgs.TypeCompany, user.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.orgSeeder != nil {
		if err := h.orgSeeder(org.ID); err != nil {
			// Non-fatal: the workspace exists; defaults can be re-seeded.
			respondInternal(w, r, "workspace created but seeding defaults failed", err)
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(org)
}

// GetOrg returns a workspace the caller belongs to.
func (h *Handler) GetOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	org, err := h.orgService.Get(orgID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "workspace not found", err)
		return
	}
	json.NewEncoder(w).Encode(org)
}

// UpdateOrg renames a workspace and/or sets its monthly spend budget (admin).
// monthly_budget_usd is honored only when present in the body: a JSON number
// sets the budget, JSON null clears it, and an omitted key leaves it
// unchanged (so a plain rename never disturbs the budget). Every part of the
// request is checked before any part is written (checkOrgUpdate), so a
// request refused for one part changes nothing.
func (h *Handler) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Name             *string         `json:"name"`
		MonthlyBudgetUSD json.RawMessage `json:"monthly_budget_usd"`
		// ReleaseChannel: "nightly", "stable", or "" for the plan's default.
		// Only company plans may set it (REQ-136).
		ReleaseChannel *string `json:"release_channel"`
		// UpgradeWindow: when stable releases turn on (REQ-138); null
		// clears it so they turn on at the cut. Company plans only.
		UpgradeWindow json.RawMessage `json:"upgrade_window"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	update, ok := h.checkOrgUpdate(w, orgID, req.MonthlyBudgetUSD, req.ReleaseChannel, req.UpgradeWindow)
	if !ok {
		return
	}

	org, err := h.orgService.UpdateOrg(orgID, req.Name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Budget is only touched when the key is present. json.RawMessage is nil
	// for an absent key; "null" clears the budget, a number sets it.
	if len(req.MonthlyBudgetUSD) > 0 {
		org, err = h.orgService.SetMonthlyBudget(orgID, update.budget)
		if err != nil {
			if errors.Is(err, orgs.ErrInvalidBudget) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			} else {
				respondInternal(w, r, "failed to update budget", err)
			}
			return
		}
	}

	if req.ReleaseChannel != nil {
		org, err = h.orgService.SetReleaseChannel(orgID, *req.ReleaseChannel)
		if err != nil {
			if errors.Is(err, orgs.ErrInvalidChannel) || errors.Is(err, orgs.ErrChannelLocked) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			} else {
				respondInternal(w, r, "failed to update release channel", err)
			}
			return
		}
	}

	if len(req.UpgradeWindow) > 0 {
		org, err = h.orgService.SetUpgradeWindow(orgID, update.day, update.hour, update.timezone)
		if err != nil {
			if errors.Is(err, orgs.ErrInvalidWindow) || errors.Is(err, orgs.ErrChannelLocked) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			} else {
				respondInternal(w, r, "failed to update upgrade window", err)
			}
			return
		}
	}

	json.NewEncoder(w).Encode(org)
}

// orgUpdate is what a workspace update sets beside the name, as
// checkOrgUpdate parsed it: the budget (nil clears it) and the upgrade window
// (day 0 clears it). Each is written only when its key was sent.
type orgUpdate struct {
	budget    *float64
	day, hour int
	timezone  string
}

// checkOrgUpdate parses and checks the parts of a workspace update that can
// be refused, before UpdateOrg writes any of them: a rename sent with a
// budget the plan does not include used to be stored before the budget was
// refused, and a budget before a refused channel or window. The checks are
// the writes' own (the service's validation, and the plan flag the budget
// needs), in the order the writes run, so each refusal is the answer it was;
// the workspace is read first, so an unknown one is still answered by the
// rename's not-found (a 400, Q19). It answers a refusal itself, and reports
// whether the update may go ahead.
func (h *Handler) checkOrgUpdate(w http.ResponseWriter, orgID string, budget json.RawMessage, channel *string, window json.RawMessage) (orgUpdate, bool) {
	var update orgUpdate
	org, err := h.orgService.Get(orgID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return update, false
	}
	if len(budget) > 0 {
		if err := json.Unmarshal(budget, &update.budget); err != nil {
			writeJSONError(w, http.StatusBadRequest, "monthly_budget_usd must be a number or null")
			return update, false
		}
		if err := h.checkFlag(orgID, orgs.LimitWorkspaceBudget); err != nil {
			h.writeLimitError(w, err)
			return update, false
		}
		if err := orgs.ValidateMonthlyBudget(update.budget); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return update, false
		}
	}
	if channel != nil {
		if err := orgs.CheckReleaseChannel(org.BilledPlan, *channel); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return update, false
		}
	}
	if len(window) > 0 {
		var parsed *struct {
			Day      int    `json:"day"`
			Hour     int    `json:"hour"`
			Timezone string `json:"timezone"`
		}
		if err := json.Unmarshal(window, &parsed); err != nil {
			writeJSONError(w, http.StatusBadRequest, "upgrade_window must be {day, hour, timezone} or null")
			return update, false
		}
		if parsed != nil {
			update.day, update.hour, update.timezone = parsed.Day, parsed.Hour, parsed.Timezone
		}
		if err := orgs.CheckUpgradeWindow(org.BilledPlan, update.day, update.hour, update.timezone); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return update, false
		}
	}
	return update, true
}

// ActivateOrg persists the session's default workspace. The workspace is
// looked up first: sessions.active_org_id would take any id, and one with no
// workspace behind it is refused rather than stored.
func (h *Handler) ActivateOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	if _, err := h.orgService.Get(orgID); err != nil {
		respondError(w, r, http.StatusNotFound, "workspace not found", err)
		return
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		writeJSONError(w, http.StatusBadRequest, "session required")
		return
	}
	if err := h.userService.SetActiveOrg(cookie.Value, orgID); err != nil {
		respondInternal(w, r, "failed to switch workspace", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
