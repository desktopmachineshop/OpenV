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
// to join its holder to a workspace. Both are POSTs carrying the token in a
// JSON body, never a path segment: an invite link IS a credential, and a URL
// travels through access logs, proxy logs, browser history and Referer
// headers where a credential must not sit.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/events"
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
// whether the server QUEUED a mail for it — with no SMTP the admin has to
// send the link themselves, and the UI says so. Emailed is true when a
// mailer is configured and the send was handed off, not when the mail
// landed: the send happens off the request path, so nobody waits on SMTP,
// and a failure is logged (the row's last_emailed_at is stamped only on
// success, which is what makes a repeat click re-send). Reason explains an
// invitation that was NOT mailed for a reason other than a missing mailer,
// and is empty otherwise.
type invitationResponse struct {
	Invitation *invitations.Invitation `json:"invitation"`
	Link       string                  `json:"link"`
	Emailed    bool                    `json:"emailed"`
	Reason     string                  `json:"reason,omitempty"`
}

// inviteResendWindow is how long an unchanged pending invitation whose link
// WAS delivered suppresses a second mail to the same address, and
// inviteReasonRecentlySent is what the admin is told when it does.
const (
	inviteResendWindow       = time.Hour
	inviteReasonRecentlySent = "an unchanged invitation to this address was emailed less than an hour ago; its link was not re-sent"
)

// errThrottled marks a request a limiter refused inside the shared
// add-or-invite branch, so both endpoints answer 429 with a Retry-After
// instead of the 500 an unrecognised error would get.
type errThrottled struct {
	message    string
	retryAfter time.Duration
}

func (e *errThrottled) Error() string { return e.message }

// CreateOrgInvitation brings an email address into the workspace (admin).
// It answers the same three outcomes as POST /orgs/{id}/members, from the
// same branch AND with the same statuses, so a client cannot need to know
// which of the two endpoints it called: an address that already has an
// account joins now (201 with the membership), one that is already a member
// is a conflict (409), and one with no account is invited (202 with the
// one-time link).
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
	writeAddOrInviteOutcome(w, outcome)
}

// writeAddOrInviteOutcome writes the one status pair both entry points use:
// 201 with the membership when an account joined, 202 with the invitation
// when a link went out instead. Sharing the writer is what keeps the two
// endpoints from drifting apart, the way they had (one answered 200/201 for
// the outcomes the other answered 201/202 for).
func writeAddOrInviteOutcome(w http.ResponseWriter, outcome *memberOrInvitation) {
	if outcome.Member != nil {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(outcome.Member)
		return
	}
	w.WriteHeader(http.StatusAccepted)
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
//
// An account is added directly only when the deployment already knows the
// address is theirs. Where verification is required and the account has NOT
// verified it, adding would hand the workspace to whoever typed that address
// into a sign-up form: nobody has proved they read the mailbox. Such an
// address is invited instead, exactly as one with no account at all is, so
// the membership waits for the link — which, once used, also marks the
// account verified. The admin sees the same 202 either way and never learns
// whether the address has an account.
func (h *Handler) addOrInviteToOrg(r *http.Request, orgID, email, role string) (*memberOrInvitation, error) {
	if role == "" {
		role = orgs.RoleMember
	}
	// One seat check covers both branches below. Whether this address ends up
	// added outright or invited, it is a seat either way, and checking once
	// here means the two paths cannot disagree about how full the workspace
	// is.
	if err := h.checkOrgSeats(orgID, 1); err != nil {
		return nil, err
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
	if h.emailVerification.Required && !user.EmailVerified {
		resp, err := h.inviteToOrg(r, orgID, email, role)
		if err != nil {
			return nil, err
		}
		return &memberOrInvitation{Invitation: resp}, nil
	}
	if err := h.orgService.AddMember(orgID, user.ID, role); err != nil {
		return nil, err
	}
	h.publishOrgEvent(r, events.OrgMemberAdded, orgID, user.ID, map[string]interface{}{
		"user_id": user.ID,
		"role":    role,
	})
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
//
// Two things stand between a click and an outbound mail. An invitation that
// is already pending for the address, unchanged (same role, not expired) and
// whose link was actually DELIVERED less than an hour ago, is handed
// straight back WITHOUT mailing anything: re-posting the same invitation —
// an impatient admin, a double-submitted form, a retrying script — must not
// turn into repeated mail to somebody who has one link sitting in their
// inbox already. An invitation whose send failed, or never happened, has
// nothing sitting in any inbox, so it is minted and sent again. The link is not
// re-shown, because it exists only in that mail: the server keeps a hash,
// and minting a new one is what "resend" means (it replaces the old link).
// That trade only holds where the server can mail at all: with no SMTP the
// link in the response IS the delivery, and withholding it would leave an
// admin who closed the dialog with no way to invite that person for an
// hour, so a mailer-less deployment always mints a fresh one.
//
// What is left is bounded per inviting account, because minting a link DOES
// send mail to an address the sender chose — the endpoint is a relay, and
// an unbounded one spends the deployment's sending reputation.
func (h *Handler) inviteToOrg(r *http.Request, orgID, email, role string) (*invitationResponse, error) {
	if h.invitationService == nil {
		return nil, errInvitationsUnavailable
	}
	if h.mailer != nil && h.mailer.Enabled() {
		if pending := h.unchangedPendingInvitation(orgID, email, role); pending != nil {
			return &invitationResponse{Invitation: pending, Reason: inviteReasonRecentlySent}, nil
		}
	}
	if ok, retryAfter := h.inviteLimiter.allow(inviteBudgetKey(r)); !ok {
		return nil, &errThrottled{
			message:    "Too many invitations from this account; try again later.",
			retryAfter: retryAfter,
		}
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
	// Admins are told an invitation went out. The invitee is not notified in
	// app — the address usually has no account yet, so there is nobody to
	// notify; the invitation email IS their notification.
	h.publishOrgEvent(r, events.OrgInvitationSent, orgID, inv.ID, map[string]interface{}{
		"email": email,
		"role":  role,
	})
	return &invitationResponse{Invitation: inv, Link: link, Emailed: h.sendInvitationMailAsync(inv, link)}, nil
}

// inviteBudgetKey is the bucket an invitation is charged to: the inviting
// account, so one admin's bulk invite cannot exhaust their colleagues'
// budget wherever either of them works from. The address is only a fallback
// for a caller with no session, which the admin check ahead of here already
// rules out.
func inviteBudgetKey(r *http.Request) string {
	if user := CurrentUser(r); user != nil {
		return "user:" + user.ID
	}
	return "ip:" + clientIP(r)
}

// unchangedPendingInvitation returns the workspace's live invitation for the
// address when it says exactly what a fresh one would — same role, still
// valid — AND its link reached the address inside inviteResendWindow. The
// row is looked up by (workspace, address), not by reading every pending
// invitation the workspace holds.
//
// The window is measured from the delivery, never from the row: mail goes
// out off the request path, so an invitation that exists is not an
// invitation anybody received. A row whose send failed, or has not been
// attempted, has no last_emailed_at and is re-sent.
//
// nil means "mint and mail one", which is also the answer when the lookup
// fails: failing to suppress a mail is better than failing to invite
// somebody.
func (h *Handler) unchangedPendingInvitation(orgID, email, role string) *invitations.Invitation {
	inv, err := h.invitationService.FindPending(orgID, email)
	if err != nil || inv == nil {
		return nil
	}
	now := time.Now()
	if inv.Role == role && inv.Pending(now) &&
		inv.LastEmailedAt != nil && now.Sub(*inv.LastEmailedAt) < inviteResendWindow {
		return inv
	}
	return nil
}

// sendInvitationMailAsync hands the link to SMTP without making the request
// wait, the way registration's verification link goes out: an admin's click
// must not sit on a slow relay. It reports whether the send was QUEUED — a
// mailer is configured and the goroutine is away — which is what the
// response's emailed means.
//
// A failure is logged and swallowed: the invitation is already real, and the
// admin can still hand the link over. Only a send that SUCCEEDED stamps the
// row, so a failed one is re-sent by the next click rather than suppressed
// as "already emailed".
func (h *Handler) sendInvitationMailAsync(inv *invitations.Invitation, link string) bool {
	if h.mailer == nil || !h.mailer.Enabled() {
		return false
	}
	subject, body := notify.RenderInvitationEmail(inv.OrgName, inv.InvitedByName, link, invitations.DefaultTTL)
	go func() {
		if err := notify.SendWithTimeout(h.mailer, inv.Email, subject, body, notify.VerificationSendTimeout); err != nil {
			slog.Warn("invitation: failed to send invitation email", "org_id", inv.OrgID, "error", err)
			return
		}
		if err := h.invitationService.MarkEmailed(inv.ID, time.Now()); err != nil {
			slog.Warn("invitation: could not record that the link was emailed",
				"invitation_id", inv.ID, "org_id", inv.OrgID, "error", err)
		}
	}()
	return true
}

// errInvitationsUnavailable marks a deployment wired without the invitation
// service (only possible in tests today).
var errInvitationsUnavailable = errors.New("invitations are not configured on this server")

// writeInvitationError maps the domain's user-facing failures onto statuses.
func (h *Handler) writeInvitationError(w http.ResponseWriter, r *http.Request, err error) {
	var throttled *errThrottled
	switch {
	case errors.As(err, &throttled):
		writeRateLimited(w, throttled.message, throttled.retryAfter)
	case errors.Is(err, orgs.ErrLimitReached):
		// A full workspace is not a bad request: the caller did nothing
		// wrong, the workspace is simply out of seats, and the refusal
		// carries the remedy.
		h.writeLimitError(w, err)
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
//
// The token arrives in the POST body, never in the path. An invitation token
// is a credential: a GET would write it into the server's access log, every
// proxy in front of it, the browser's history, and any Referer the page
// leaks — all places a credential must not be readable long after the
// invitation was taken up. POST /auth/verify-email takes its token the same
// way, for the same reason.
func (h *Handler) PreviewInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
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
	// Only an unusable link is a 404. A database that cannot be read is not
	// evidence that the token is wrong, and answering "invalid or expired"
	// to it sends the invitee off to ask for a new invitation that will fail
	// exactly the same way; it is reported as the server fault it is.
	inv, err := h.invitationService.Lookup(req.Token)
	if err != nil || inv == nil {
		if err == nil || errors.Is(err, invitations.ErrInvalidToken) {
			writeJSONError(w, http.StatusNotFound, invitations.ErrInvalidToken.Error())
			return
		}
		respondInternal(w, r, "failed to read the invitation", err)
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
	// Presenting the token proves this account read the invited mailbox —
	// the link was mailed there and nowhere else — and the address it was
	// issued to IS this account's own, which AcceptTokenForEmail has just
	// checked. That is the same proof Register accepts from an invite token,
	// so the address is marked verified here too: an invitee must not be
	// walled behind a second mail immediately after following the first.
	outcome := InviteOutcomeAccepted
	if acc.AlreadyMember {
		outcome = InviteOutcomeAlreadyMember
	}
	h.verifiedByInvitation(user, outcome)
	// Taking up an invitation is the join, so this is what the admins see.
	// Someone who was already a member joined nothing, so nothing is said.
	if !acc.AlreadyMember {
		h.publishOrgEvent(r, events.OrgInvitationAccepted, acc.Invitation.OrgID, user.ID, map[string]interface{}{
			"user_id": user.ID,
			"role":    acc.Role,
		})
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
	for _, acc := range accepted {
		if acc == nil || acc.AlreadyMember || acc.Invitation == nil {
			continue
		}
		h.publishOrgEventAs("user:"+userID, events.OrgInvitationAccepted,
			acc.Invitation.OrgID, userID, map[string]interface{}{
				"user_id": userID,
				"role":    acc.Role,
			})
	}
	if len(accepted) > 0 {
		slog.Info("invitation: provider-verified address joined invited workspaces", "user_id", userID, "count", len(accepted))
	}
}

// Invitation outcomes a registration reports back, so the sign-up page can
// say what the link it carried actually did instead of silently dropping
// it. Absent from the response when no token was supplied.
const (
	// InviteOutcomeAccepted: the membership the link named was granted.
	InviteOutcomeAccepted = "accepted"
	// InviteOutcomeAlreadyMember: the account was in that workspace already,
	// so it kept the role it had; the link is spent either way.
	InviteOutcomeAlreadyMember = "already_member"
	// InviteOutcomeEmailMismatch: the link is live but was issued to another
	// address than the one being registered, so it granted nothing.
	InviteOutcomeEmailMismatch = "email_mismatch"
	// InviteOutcomeInvalid: unknown, revoked, spent or expired — including a
	// link revoked in the moment between the sign-up being allowed and the
	// membership being claimed.
	InviteOutcomeInvalid = "invalid"
)

// acceptResolvedInvitation takes up the invitation a registration carried a
// token for, and names what happened. The token is the proof of control that
// registration itself lacks: it reached the invited mailbox.
//
// The invitation was resolved once, before the account was created, and is
// claimed here — so a link revoked in between simply grants nothing, and the
// registration still stands. What must NOT happen is a silent nothing: the
// person followed a link to join a workspace, and if they did not join, the
// outcome says so and the page can tell them.
func (h *Handler) acceptResolvedInvitation(inv *invitations.Invitation, user *users.User) string {
	if inv == nil || h.invitationService == nil || user == nil {
		return InviteOutcomeInvalid
	}
	acc, err := h.invitationService.AcceptResolvedForEmail(inv, user.Email, user.ID)
	if err != nil {
		slog.Warn("invitation: registration token was not accepted", "user_id", user.ID, "error", err)
		if errors.Is(err, invitations.ErrEmailMismatch) {
			return InviteOutcomeEmailMismatch
		}
		return InviteOutcomeInvalid
	}
	if acc.AlreadyMember {
		slog.Info("invitation: new account was already in its invited workspace", "user_id", user.ID, "org_id", acc.Invitation.OrgID)
		return InviteOutcomeAlreadyMember
	}
	slog.Info("invitation: new account joined its invited workspace", "user_id", user.ID, "org_id", acc.Invitation.OrgID)
	return InviteOutcomeAccepted
}
