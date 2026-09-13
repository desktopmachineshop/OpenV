package api

// Platform administration (REQ-155): the page a platform admin uses to
// see every workspace and move it between plans, and to grant or remove
// platform-admin standing. Platform admin is users.is_admin: the first
// account ever registered has it, and from here it is handed on.

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func (h *Handler) registerAdminRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/admin/workspaces", h.AdminListWorkspaces).Methods("GET")
	router.HandleFunc("/api/v1/admin/users", h.AdminListUsers).Methods("GET")
	router.HandleFunc("/api/v1/admin/users/{id}/admin", h.AdminSetUserAdmin).Methods("PUT")
}

// requirePlatformAdmin writes 401/403 and answers nil unless the caller is
// a signed-in platform admin.
func (h *Handler) requirePlatformAdmin(w http.ResponseWriter, r *http.Request) *users.User {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return nil
	}
	if !user.IsAdmin {
		writeJSONError(w, http.StatusForbidden, "platform admins only")
		return nil
	}
	return user
}

// adminWorkspace is one row of the workspace listing.
type adminWorkspace struct {
	*orgs.Org
	Members int `json:"members"`
}

// AdminListWorkspaces answers every live workspace with its plan and
// member count, oldest first.
func (h *Handler) AdminListWorkspaces(w http.ResponseWriter, r *http.Request) {
	if h.requirePlatformAdmin(w, r) == nil {
		return
	}
	ids, err := h.orgService.ListAll()
	if err != nil {
		respondInternal(w, r, "failed to list workspaces", err)
		return
	}
	out := make([]adminWorkspace, 0, len(ids))
	for _, id := range ids {
		org, err := h.orgService.Get(id)
		if err != nil || org == nil {
			continue
		}
		row := adminWorkspace{Org: org}
		if members, err := h.orgService.ListMembers(id); err == nil {
			row.Members = len(members)
		}
		out = append(out, row)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// adminUser is one row of the people listing: what an admin needs to pick
// somebody out, and nothing a member's own settings would keep private.
type adminUser struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	AuthProvider string `json:"auth_provider"`
	IsAdmin      bool   `json:"is_admin"`
	CreatedAt    string `json:"created_at"`
}

func toAdminUser(u *users.User) adminUser {
	return adminUser{ID: u.ID, Name: u.Name, Email: u.Email, AuthProvider: u.AuthProvider, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
}

// AdminListUsers answers every account, admins first then by name.
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if h.requirePlatformAdmin(w, r) == nil {
		return
	}
	list, err := h.userService.ListUsers()
	if err != nil {
		respondInternal(w, r, "failed to list users", err)
		return
	}
	out := make([]adminUser, 0, len(list))
	for _, u := range list {
		out = append(out, toAdminUser(u))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsAdmin != out[j].IsAdmin {
			return out[i].IsAdmin
		}
		return out[i].Name < out[j].Name
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// AdminSetUserAdmin grants or removes platform-admin standing: {"is_admin"}
// → the user. An admin cannot demote themselves (somebody else does that),
// and the last admin cannot be demoted at all.
func (h *Handler) AdminSetUserAdmin(w http.ResponseWriter, r *http.Request) {
	caller := h.requirePlatformAdmin(w, r)
	if caller == nil {
		return
	}
	var req struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := mux.Vars(r)["id"]
	if id == caller.ID && !req.IsAdmin {
		writeJSONError(w, http.StatusBadRequest, "you cannot remove your own platform-admin standing; ask another platform admin")
		return
	}
	u, err := h.userService.SetAdmin(id, req.IsAdmin)
	if err != nil {
		switch {
		case errors.Is(err, users.ErrUserNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, users.ErrLastAdmin):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		default:
			respondInternal(w, r, "failed to update platform-admin standing", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toAdminUser(u))
}
