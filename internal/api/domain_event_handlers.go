package api

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
)

// --- Domain events ---

// registerDomainEventRoutes wires the domain event audit.
func (h *Handler) registerDomainEventRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/events", h.ListDomainEvents).Methods("GET")
}

// listOrg is the workspace a list read filters by: a named project's, which
// the project guard just let the caller read, so that the list holds that
// project's rows whatever workspace the caller acts in (a platform admin's
// own, say); else the active workspace.
func (h *Handler) listOrg(r *http.Request, projectID string) string {
	if projectID != "" {
		if org := h.orgIDForProject(projectID); org != "" {
			return org
		}
	}
	return ActiveOrg(r)
}

func (h *Handler) ListDomainEvents(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	q := r.URL.Query()
	projectID := q.Get("project_id")
	if projectID != "" && !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	limit, ok := parseLimit(w, r, domainEventsLimit)
	if !ok {
		return
	}
	// "before" is a keyset cursor (an event ID from a previous page): the
	// repo returns only events strictly older than it, so "load more" pages
	// stay stable while new events keep arriving. It is cast to uuid in SQL,
	// so a malformed value would otherwise surface as a 500 — validate it here
	// and reject bad input with 400 instead.
	before := q.Get("before")
	if before != "" {
		if _, err := uuid.Parse(before); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid before cursor")
			return
		}
	}
	list, err := h.EventRepo.List(h.listOrg(r, projectID), projectID, q.Get("event_type"), before, limit)
	if err != nil {
		respondInternal(w, r, "failed to list events", err)
		return
	}
	// Advertise the next cursor from the RAW page (before membership
	// filtering below): a full page means there may be older events. Using
	// the raw tail keeps the cursor monotonic even when the visibility filter
	// drops the trailing rows for non-admin members.
	if len(list) >= limit {
		w.Header().Set("X-Next-Cursor", list[len(list)-1].ID)
	}
	// Without a project filter, org admins see the whole workspace audit;
	// plain members only see events for projects they can access (mirrors
	// ListAgentRuns).
	if projectID == "" && !h.isOrgAdmin(r, ActiveOrg(r)) {
		user := CurrentUser(r)
		allowed := map[string]bool{}
		if h.MemberService != nil {
			ids, err := h.MemberService.ProjectIDsForUser(user.ID)
			if err != nil {
				respondInternal(w, r, "failed to resolve project memberships", err)
				return
			}
			for _, id := range ids {
				allowed[id] = true
			}
		}
		visible := list[:0]
		for _, e := range list {
			if e.ProjectID != "" && allowed[e.ProjectID] {
				visible = append(visible, e)
			}
		}
		list = visible
	}
	// Decorate only what survived the visibility filter: naming an event
	// dereferences the IDs it carries, so events the caller may not see are
	// never looked up.
	writeJSONBare(w, h.decorateEvents(list))
}
