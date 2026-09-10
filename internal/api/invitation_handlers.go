package api

// Workspace invitations (REQ-95 / HAZ-15). An admin invites an address; the
// invitation holds the workspace and role until whoever controls that
// address arrives — by registering, by signing in through the identity
// provider for the first time, or, already signed in, by opening the link.
//
// The two public routes live under /api/v1/auth/, which the middleware
// leaves open: the preview must answer a browser that has no session yet
// (that is the whole point of the invite link), and accept reads the session
// cookie itself, never a Bearer credential — a runner key must not be able
// to join its holder to a workspace.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/notify"
)

func (h *Handler) registerInvitationRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/invitations", h.ListOrgInvitations).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/invitations", h.CreateOrgInvitation).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/invitations/{invId}", h.RevokeOrgInvitation).Methods("DELETE")
}

// invitationResponse is what an admin gets back when an invitation is
// created: the row, the one-time link (shown once, like a worker key), and
// whether the server actually mailed it — with no SMTP the admin has to send
// the link themselves, and the UI says so.
type invitationResponse struct {
	Invitation *invitations.Invitation `json:"invitation"`
	Link       string                  `json:"link"`
	Emailed    bool                    `json:"emailed"`
}

// CreateOrgInvitation invites an email address to the workspace (admin).
func (h *Handler) CreateOrgInvitation(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.inviteToOrg(r, orgID, req.Email, req.Role)
	if err != nil {
		h.writeInvitationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// inviteToOrg mints the invitation and mails it when the deployment can, and
// is shared with AddOrgMember's fallback for an address that has no account.
func (h *Handler) inviteToOrg(r *http.Request, orgID, email, role string) (*invitationResponse, error) {
	if h.invitationService == nil {
		return nil, errInvitationsUnavailable
	}
	var invitedBy *string
	if user := CurrentUser(r); user != nil {
		invitedBy = &user.ID
	}
	inv, token, err := h.invitationService.Create(orgID, email, role, invitedBy)
	if err != nil {
		return nil, err
	}
	link := notify.InvitationLink(h.emailLinkBase, token)
	return &invitationResponse{Invitation: inv, Link: link, Emailed: h.sendInvitationMail(inv, link)}, nil
}

// sendInvitationMail delivers the link when SMTP is configured, reporting
// whether it went out. A failure is logged and swallowed: the invitation is
// already real, and the admin can still hand the link over.
func (h *Handler) sendInvitationMail(inv *invitations.Invitation, link string) bool {
	if h.mailer == nil || !h.mailer.Enabled() {
		return false
	}
	subject, body := notify.RenderInvitationEmail(inv.OrgName, inv.InvitedByName, link, invitations.DefaultTTL)
	if err := notify.SendWithTimeout(h.mailer, inv.Email, subject, body, notify.VerificationSendTimeout); err != nil {
		slog.Warn("invitation: failed to send invitation email", "org_id", inv.OrgID, "error", err)
		return false
	}
	return true
}

// errInvitationsUnavailable marks a deployment wired without the invitation
// service (only possible in tests today).
var errInvitationsUnavailable = errors.New("invitations are not configured on this server")

// writeInvitationError maps the domain's user-facing failures onto statuses.
func (h *Handler) writeInvitationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, invitations.ErrInvalidEmail), errors.Is(err, orgs.ErrInvalidRole):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, orgs.ErrPersonalOrgMembers):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, invitations.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errInvitationsUnavailable):
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		respondInternal(w, r, "failed to invite to the workspace", err)
	}
}

// ListOrgInvitations returns the workspace's pending invitations (admin).
func (h *Handler) ListOrgInvitations(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if h.invitationService == nil {
		json.NewEncoder(w).Encode([]*invitations.Invitation{})
		return
	}
	list, err := h.invitationService.ListPending(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list invitations", err)
		return
	}
	if list == nil {
		list = []*invitations.Invitation{}
	}
	json.NewEncoder(w).Encode(list)
}

// RevokeOrgInvitation deletes a pending invitation (admin).
func (h *Handler) RevokeOrgInvitation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireOrgRole(w, r, vars["id"], orgs.RoleAdmin) {
		return
	}
	if h.invitationService == nil {
		writeJSONError(w, http.StatusNotFound, errInvitationsUnavailable.Error())
		return
	}
	if err := h.invitationService.Revoke(vars["id"], vars["invId"]); err != nil {
		h.writeInvitationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PreviewInvitation describes an invitation to the sign-up page (open). It
// reveals only what the link's holder already has — the address it was sent
// to, the workspace, the role — and answers one 404 for every way a link can
// fail, so a probe learns nothing about which tokens exist.
func (h *Handler) PreviewInvitation(w http.ResponseWriter, r *http.Request) {
	if h.invitationService == nil {
		writeJSONError(w, http.StatusNotFound, invitations.ErrInvalidToken.Error())
		return
	}
	// A 256-bit token is not guessable; this only bounds how fast one
	// address can make the database look.
	if ok, retryAfter := h.authIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many attempts from this address; try again later.", retryAfter)
		return
	}
	inv, err := h.invitationService.Lookup(mux.Vars(r)["token"])
	if err != nil {
		writeJSONError(w, http.StatusNotFound, invitations.ErrInvalidToken.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"email":      inv.Email,
		"org_name":   inv.OrgName,
		"role":       inv.Role,
		"expires_at": inv.ExpiresAt,
	})
}

// AcceptInvitation joins the signed-in account to the invitation's workspace
// (open route, session cookie only). This is the path for someone who
// already has an account and clicks an invite link.
func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !requireJSONBody(w, r) {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user := h.sessionUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if h.invitationService == nil {
		writeJSONError(w, http.StatusNotFound, invitations.ErrInvalidToken.Error())
		return
	}
	inv, err := h.invitationService.AcceptToken(req.Token, user.ID)
	if err != nil {
		if errors.Is(err, invitations.ErrInvalidToken) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		h.writeInvitationError(w, r, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"org_id": inv.OrgID, "org_name": inv.OrgName, "role": inv.Role})
}

// acceptPendingInvitations joins a newly arrived account to every workspace
// that invited its address. Called after registration and after a first SSO
// sign-in; best-effort, since a failed join must never fail the sign-in that
// the person actually asked for.
func (h *Handler) acceptPendingInvitations(userID, email string) {
	if h.invitationService == nil {
		return
	}
	accepted, err := h.invitationService.AcceptAllForEmail(email, userID)
	if err != nil {
		slog.Warn("invitation: failed to accept pending invitations", "user_id", userID, "error", err)
		return
	}
	if len(accepted) > 0 {
		slog.Info("invitation: new account joined invited workspaces", "user_id", userID, "count", len(accepted))
	}
}
