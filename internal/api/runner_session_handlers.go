package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
)

// RegisterRunnerSessionRoutes wires the transient runner endpoints: the pool
// side (nodes registering and heartbeating with the deployment's pool key)
// and the member side (leasing, extending and ending a cloud runner).
func (h *Handler) registerRunnerSessionRoutes(router *mux.Router) {
	// Pool nodes.
	router.HandleFunc("/api/v1/runner-pool/nodes", h.RegisterPoolNode).Methods("POST")
	router.HandleFunc("/api/v1/runner-pool/nodes/{id}/heartbeat", h.PoolNodeHeartbeat).Methods("POST")
	router.HandleFunc("/api/v1/runner-pool/nodes/{id}/release", h.ReleasePoolNode).Methods("POST")

	// Members.
	router.HandleFunc("/api/v1/orgs/{id}/runner-session", h.GetRunnerSession).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/runner-session", h.StartRunnerSession).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/runner-session/extend", h.ExtendRunnerSession).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/runner-session", h.EndRunnerSession).Methods("DELETE")

	// Workspace admins: who is holding what.
	router.HandleFunc("/api/v1/orgs/{id}/runner-pool", h.GetRunnerPool).Methods("GET")
}

// requirePoolNode gates the pool endpoints on the deployment's pool key.
func requirePoolNode(w http.ResponseWriter, r *http.Request) bool {
	if !IsPoolNode(r) {
		writeJSONError(w, http.StatusForbidden, "runner pool credentials required")
		return false
	}
	return true
}

// runnerSessionsEnabled reports whether this deployment runs transient
// runners at all (the service is only wired when a pool key is configured).
func (h *Handler) runnerSessionsEnabled() bool {
	return h.runnerSessionService != nil
}

func (h *Handler) requireRunnerSessions(w http.ResponseWriter) bool {
	if !h.runnerSessionsEnabled() {
		writeJSONError(w, http.StatusBadRequest, "transient runners are not enabled on this deployment")
		return false
	}
	return true
}

// --- Pool node endpoints ---

// RegisterPoolNode records a pre-warmed worker process as available.
func (h *Handler) RegisterPoolNode(w http.ResponseWriter, r *http.Request) {
	if !requirePoolNode(w, r) || !h.requireRunnerSessions(w) {
		return
	}
	var req struct {
		Name      string   `json:"name"`
		Pool      string   `json:"pool"`
		Providers []string `json:"providers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	node, err := h.runnerSessionService.RegisterNode(req.Pool, req.Name, req.Providers)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "failed to register pool node", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(node)
}

// PoolNodeHeartbeat records a beat and returns the node's assignment, if it
// has one. The session's worker key is included exactly once, on the beat
// that picks the lease up.
func (h *Handler) PoolNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !requirePoolNode(w, r) || !h.requireRunnerSessions(w) {
		return
	}
	node, assignment, err := h.runnerSessionService.Heartbeat(mux.Vars(r)["id"])
	if err != nil {
		if errors.Is(err, runnersessions.ErrNodeNotFound) {
			// The row is gone (a reset database, a purged pool): tell the
			// node to register again rather than leaving it beating at
			// nothing.
			writeJSONError(w, http.StatusNotFound, "pool node is not registered")
			return
		}
		respondInternal(w, r, "failed to record pool heartbeat", err)
		return
	}
	if assignment != nil && assignment.UserName == "" && h.userService != nil {
		if user, err := h.userService.GetByID(assignment.UserID); err == nil && user != nil {
			assignment.UserName = user.Name
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"node":       node,
		"assignment": assignment,
	})
}

// ReleasePoolNode is a node's confirmation that it has wiped the lease's
// state and is ready for the next member.
func (h *Handler) ReleasePoolNode(w http.ResponseWriter, r *http.Request) {
	if !requirePoolNode(w, r) || !h.requireRunnerSessions(w) {
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.runnerSessionService.ReleaseNode(mux.Vars(r)["id"], req.SessionID); err != nil {
		respondInternal(w, r, "failed to release pool node", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Member endpoints ---

// runnerSessionPayload is the shape the UI reads: the member's lease (or
// null), how long it has left, and whether the pool has anything free.
func (h *Handler) runnerSessionPayload(session *runnersessions.Session) map[string]interface{} {
	payload := map[string]interface{}{
		"enabled": h.runnerSessionsEnabled(),
		"session": session,
	}
	if session != nil {
		deadline := session.Deadline()
		payload["deadline"] = deadline
		payload["seconds_remaining"] = int(time.Until(deadline).Seconds())
	}
	if counts, err := h.runnerSessionService.Counts(runnersessions.DefaultPool); err == nil {
		payload["pool"] = counts
	}
	return payload
}

// sessionLimits reads the workspace's lease timings from its effective org
// limits, falling back to the package defaults.
func (h *Handler) sessionLimits(orgID string) (sessionMinutes, idleMinutes int) {
	sessionMinutes, idleMinutes = runnersessions.DefaultSessionMinutes, runnersessions.DefaultIdleMinutes
	org, err := h.orgService.Get(orgID)
	if err != nil || org == nil {
		return
	}
	eff := org.EffectiveLimits()
	if v, ok := orgs.LimitFloat(eff, orgs.LimitRunnerSessionMinutes); ok && v > 0 {
		sessionMinutes = int(v)
	}
	if v, ok := orgs.LimitFloat(eff, orgs.LimitRunnerSessionIdleMinutes); ok && v > 0 {
		idleMinutes = int(v)
	}
	return
}

// GetRunnerSession returns the member's current cloud runner, if any.
func (h *Handler) GetRunnerSession(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	if !h.runnerSessionsEnabled() {
		json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false, "session": nil})
		return
	}
	user := CurrentUser(r)
	session, err := h.runnerSessionService.Get(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load runner session", err)
		return
	}
	json.NewEncoder(w).Encode(h.runnerSessionPayload(session))
}

// StartRunnerSession leases a pool node to the member. Clicking twice returns
// the lease they already hold rather than taking a second node.
func (h *Handler) StartRunnerSession(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) || !h.requireRunnerSessions(w) {
		return
	}
	user := CurrentUser(r)
	sessionMinutes, idleMinutes := h.sessionLimits(orgID)
	session, err := h.runnerSessionService.Start(orgID, user.ID, sessionMinutes, idleMinutes)
	if err != nil {
		if errors.Is(err, runnersessions.ErrNoNodes) {
			// Not an error the member did anything wrong: every runner in
			// the pool is busy or the pool is empty.
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(h.runnerSessionPayload(nil))
			return
		}
		respondInternal(w, r, "failed to start a runner session", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(h.runnerSessionPayload(session))
}

// ExtendRunnerSession pushes the member's lease deadlines out.
func (h *Handler) ExtendRunnerSession(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) || !h.requireRunnerSessions(w) {
		return
	}
	user := CurrentUser(r)
	session, err := h.runnerSessionService.Get(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load runner session", err)
		return
	}
	if session == nil {
		writeJSONError(w, http.StatusNotFound, "you have no runner session to extend")
		return
	}
	sessionMinutes, _ := h.sessionLimits(orgID)
	extended, err := h.runnerSessionService.Extend(session.ID, sessionMinutes)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "failed to extend the runner session", err)
		return
	}
	json.NewEncoder(w).Encode(h.runnerSessionPayload(extended))
}

// EndRunnerSession hands the member's node back to the pool early. The
// node wipes the CLI sign-ins it holds, so a new session means signing in
// again — that is the deal, not a bug.
func (h *Handler) EndRunnerSession(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) || !h.requireRunnerSessions(w) {
		return
	}
	user := CurrentUser(r)
	session, err := h.runnerSessionService.Get(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load runner session", err)
		return
	}
	if session == nil {
		json.NewEncoder(w).Encode(h.runnerSessionPayload(nil))
		return
	}
	if _, err := h.runnerSessionService.End(session.ID, runnersessions.EndReasonUser); err != nil {
		respondInternal(w, r, "failed to end the runner session", err)
		return
	}
	json.NewEncoder(w).Encode(h.runnerSessionPayload(nil))
}

// GetRunnerPool reports pool occupancy and the workspace's live leases
// (admin).
func (h *Handler) GetRunnerPool(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.runnerSessionsEnabled() {
		json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false})
		return
	}
	counts, err := h.runnerSessionService.Counts(runnersessions.DefaultPool)
	if err != nil {
		respondInternal(w, r, "failed to read the runner pool", err)
		return
	}
	sessions, err := h.runnerSessionService.ListLive(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list runner sessions", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":  true,
		"pool":     counts,
		"sessions": sessions,
	})
}

// touchRunnerSession records run activity against a transient runner lease,
// so the idle clock measures actual use rather than the poll loop. A no-op
// for every other credential.
func (h *Handler) touchRunnerSession(r *http.Request) {
	if h.runnerSessionService == nil {
		return
	}
	if session := WorkerSession(r); session != "" {
		_ = h.runnerSessionService.Touch(session)
	}
}
