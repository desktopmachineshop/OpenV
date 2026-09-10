package api

// Workspace invitations (REQ-95 / HAZ-15). An admin invites an address; the
// invitation holds the workspace and role until the acting account PROVABLY
// OWNS that address. There are exactly three ways to prove it, and nothing
// else converts an invitation into a membership:
//
//   - a sign-up carries the link's token and registers the invited address
//     (POST /auth/register with invite_token);
//   - a signed-in account whose own address IS the invited one posts the
//     token (POST /auth/invitations/accept);
//   - an identity provider signs the account in and asserts the invited
//     address as email_verified.
//
// Registering the invited address proves nothing on its own — anyone can
// type an address into a form — and neither does confirming an emailed
// verification link, because the change-of-address flow lets an account aim
// that mail at an address it does not own.
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
	"github.com/openv/requirements-platform/internal/domain/users"
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

// CreateOrgInvitation brings an email address into the workspace (admin).
// It answers the same three outcomes as POST /orgs/{id}/members, from the
// same branch: an address that already has an account joins now (200 with
// the membership), one that is already a member is a conflict (409), and one
// with no account is invited (201 with the one-time link).
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
	outcome, err := h.addOrInviteToOrg(r, orgID, req.Email, req.Role)
	if err != nil {
		h.writeInvitationError(w, r, err)
		return
	}
	if outcome.Member != nil {
		json.NewEncoder(w).Encode(outcome.Member)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(outcome.Invitation)
}

// memberOrInvitation is what bringing an address into a workspace produced:
// a membership, when the address had an account, or an invitation when it
// did not. Exactly one is set.
type memberOrInvitation struct {
	Member     *orgs.Member
	Invitation *invitationResponse
}

// errAlreadyOrgMember marks an address that is already in the workspace, so
// both entry points answer 409 rather than silently rewriting a role — role
// changes belong to PUT /orgs/{id}/members/{userId}.
var errAlreadyOrgMember = errors.New("that address is already a member of this workspace")

// addOrInviteToOrg is the one branch behind both ways an admin brings
// somebody in — POST /orgs/{id}/members and POST /orgs/{id}/invitations — so
// neither can invite an address that already has an account, or duplicate a
// membership. The two handlers differ only in the statuses they map the
// outcome onto.
func (h *Handler) addOrInviteToOrg(r *http.Request, orgID, email, role string) (*memberOrInvitation, error) {
	if role == "" {
		role = orgs.RoleMember
	}
	user, err := h.userService.FindByEmail(email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		resp, err := h.inviteToOrg(r, orgID, email, role)
		if err != nil {
			return nil, err
		}
		return &memberOrInvitation{Invitation: resp}, nil
	}
	if h.orgService == nil {
		return nil, errors.New("the workspace service is not configured on this server")
	}
	existing, err := h.orgService.RoleInOrg(orgID, user.ID)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, errAlreadyOrgMember
	}
	if err := h.orgService.AddMember(orgID, user.ID, role); err != nil {
		return nil, err
	}
	return &memberOrInvitation{Member: &orgs.Member{
		OrgID:     orgID,
		UserID:    user.ID,
		Role:      role,
		UserName:  user.Name,
		UserEmail: user.Email,
		AvatarURL: user.AvatarURL,
	}}, nil
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
	case errors.Is(err, errAlreadyOrgMember):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, orgs.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, invitations.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errInvitationsUnavailable):
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		respondInternal(w, r, "failed to bring the address into the workspace", err)
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
	// A 256-bit token is not guessable, so this only bounds how fast one
	// address can make the database look. It draws on its OWN bucket rather
	// than the sign-in one: opening an invite link is not a credential
	// attempt, and a page that previews the link (a reload, a second tab,
	// the link followed again after signing out) must never spend somebody's
	// sign-in budget and lock them out of the very account they were invited
	// to use.
	if ok, retryAfter := h.invitePreviewLimiter.allow(clientIP(r)); !ok {
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
//
// It converts only when the session's own address IS the invited one.
// Holding the link is not enough by itself: a link forwarded, or found, or
// simply guessed at by whoever is signed in on that browser would otherwise
// put a stranger's account into the workspace under the invited person's
// name. A mismatch is 403 invitation_email_mismatch, and the body does not
// say which address was invited — the preview endpoint already tells the
// link's holder that, and this answer goes to whoever is signed in, who is
// not necessarily the same person.
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
	acc, err := h.invitationService.AcceptTokenForEmail(req.Token, user.Email, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, invitations.ErrEmailMismatch):
			writeJSONErrorCode(w, http.StatusForbidden,
				"this invitation was sent to a different address; sign in as that address to accept it",
				ErrCodeInvitationEmailMismatch)
		case errors.Is(err, invitations.ErrInvalidToken):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			h.writeInvitationError(w, r, err)
		}
		return
	}
	// role is what the account holds now, which is the invited role only
	// when it was not already a member: an invitation never rewrites a role.
	json.NewEncoder(w).Encode(map[string]any{
		"org_id":         acc.Invitation.OrgID,
		"org_name":       acc.Invitation.OrgName,
		"role":           acc.Role,
		"already_member": acc.AlreadyMember,
	})
}

// acceptInvitationsForProviderVerifiedEmail joins an account to every
// workspace that invited its address. Membership is a credential, so the
// name states the one precondition: an identity provider has just asserted
// this address as verified for the account signing in (see the OIDC
// callback, which refuses an unverified or absent claim before reaching
// here). Nothing weaker qualifies — registering the address does not, and
// neither does confirming an emailed verification link, since an account can
// aim that mail at an address it does not own.
//
// A membership that could not be written is logged at ERROR and swallowed:
// the sign-in the person actually asked for still succeeds, and the
// invitation stays pending so its link can take it up later.
func (h *Handler) acceptInvitationsForProviderVerifiedEmail(userID, email string) {
	if h.invitationService == nil {
		return
	}
	accepted, err := h.invitationService.AcceptAllForProviderVerifiedEmail(email, userID)
	if err != nil {
		slog.Error("invitation: a provider-verified address could not join every workspace that invited it",
			"user_id", userID, "error", err)
	}
	if len(accepted) > 0 {
		slog.Info("invitation: provider-verified address joined invited workspaces", "user_id", userID, "count", len(accepted))
	}
}

// acceptInvitationToken takes up the one invitation a registration carried a
// token for. The token is the proof of control that registration itself
// lacks: it reached the invited mailbox. A token that does not match the
// address it was sent to, or has expired, simply grants nothing — the
// account is already created and signed in, and the wrong invitation must
// not be a reason to fail that.
func (h *Handler) acceptInvitationToken(token string, user *users.User) {
	if token == "" || h.invitationService == nil || user == nil {
		return
	}
	acc, err := h.invitationService.AcceptTokenForEmail(token, user.Email, user.ID)
	if err != nil {
		slog.Warn("invitation: registration token was not accepted", "user_id", user.ID, "error", err)
		return
	}
	slog.Info("invitation: new account joined its invited workspace", "user_id", user.ID, "org_id", acc.Invitation.OrgID)
}
