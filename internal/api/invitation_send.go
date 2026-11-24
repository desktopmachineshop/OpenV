package api

// Sending an invitation: minting it, attaching a project, and mailing it.
// Shared by the workspace's members and invitations endpoints
// (addOrInviteToOrg) and a project's members endpoint (inviteToProject),
// so neither handler file reaches into the other.

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/notify"
)

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
//
// grant, when not nil, is a project the invitation also opens (a project
// owner adding somebody with no account, email-invites). It is added to the
// invitation whether a fresh link goes out or a recent one is left to do
// its work: either way, accepting the link the person holds joins the
// project too.
func (h *Handler) inviteToOrg(r *http.Request, orgID, email, role string, grant *invitations.ProjectGrant) (*invitationResponse, error) {
	if h.InvitationService == nil {
		return nil, errInvitationsUnavailable
	}
	if h.Mailer != nil && h.Mailer.Enabled() {
		if pending := h.unchangedPendingInvitation(orgID, email, role); pending != nil {
			if grant != nil {
				grants, err := h.InvitationService.GrantProject(pending.ID, grant.ProjectID, grant.Role)
				if err != nil {
					return nil, err
				}
				pending.Projects = grants
			}
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
	inv, token, err := h.InvitationService.Create(orgID, email, role, invitedBy)
	if err != nil {
		return nil, err
	}
	// The project goes on before the mail, so the mail can name it.
	if grant != nil {
		grants, err := h.InvitationService.GrantProject(inv.ID, grant.ProjectID, grant.Role)
		if err != nil {
			return nil, err
		}
		inv.Projects = grants
	}
	link := notify.InvitationLink(h.EmailLinkBase, token)
	// Admins are told an invitation went out. The invitee is not notified in
	// app — the address usually has no account yet, so there is nobody to
	// notify; the invitation email IS their notification.
	h.publishOrgEvent(r, events.OrgInvitationSent, orgID, inv.ID, map[string]interface{}{
		"email": email,
		"role":  role,
	})
	return &invitationResponse{Invitation: inv, Link: link, Emailed: h.sendInvitationMailAsync(inv, link)}, nil
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
	inv, err := h.InvitationService.FindPending(orgID, email)
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
	if h.Mailer == nil || !h.Mailer.Enabled() {
		return false
	}
	projectNames := make([]string, 0, len(inv.Projects))
	for _, g := range inv.Projects {
		projectNames = append(projectNames, g.ProjectName)
	}
	subject, body := notify.RenderInvitationEmailWithProjects(inv.OrgName, inv.InvitedByName, projectNames, link, invitations.DefaultTTL)
	go func() {
		if err := notify.SendWithTimeout(h.Mailer, inv.Email, subject, body, notify.VerificationSendTimeout); err != nil {
			slog.Warn("invitation: failed to send invitation email", "org_id", inv.OrgID, "error", err)
			return
		}
		if err := h.InvitationService.MarkEmailed(inv.ID, time.Now()); err != nil {
			slog.Warn("invitation: could not record that the link was emailed",
				"invitation_id", inv.ID, "org_id", inv.OrgID, "error", err)
		}
	}()
	return true
}

// inviteToProject answers POST /projects/{id}/members for an address with
// no account (email-invites): it invites the address to the project's
// workspace, carrying the project, so accepting the link joins both. The
// caller is the project's owner (AddProjectMember checked), and must also
// be an admin of the workspace: an invitation brings somebody new into the
// workspace and takes a seat there, which is a workspace admin's decision.
// An owner who is not one is told to ask one, in words they can act on.
//
// The invitation is for the workspace's member role, or the role a pending
// invitation for the address already offers, so adding a project never
// demotes an admin invitation. Answers 202 with the invitation, as a
// workspace invitation does.
func (h *Handler) inviteToProject(w http.ResponseWriter, r *http.Request, projectID, email, role string) {
	if !members.ValidRole(role) || role == members.RoleOwner {
		writeJSONError(w, http.StatusBadRequest, "invalid role: invite someone as an editor, reviewer or viewer, then make them an owner once they have joined")
		return
	}
	project, err := h.ProjectService.GetProject(projectID)
	if err != nil || project == nil || project.OrgID == "" {
		unknownProject.write(w)
		return
	}
	orgID := project.OrgID
	if !h.isOrgAdmin(r, orgID) {
		writeJSONError(w, http.StatusForbidden, projectInviteNeedsAdmin)
		return
	}
	pending, err := h.InvitationService.FindPending(orgID, email)
	if err != nil {
		h.writeInvitationError(w, r, err)
		return
	}
	orgRole := orgs.RoleMember
	if pending != nil {
		orgRole = pending.Role
	} else if err := h.checkOrgSeats(orgID, 1); err != nil {
		// A pending invitation already holds its seat; a new one takes one.
		h.writeInvitationError(w, r, err)
		return
	}
	resp, err := h.inviteToOrg(r, orgID, email, orgRole, &invitations.ProjectGrant{ProjectID: projectID, Role: role})
	if err != nil {
		h.writeInvitationError(w, r, err)
		return
	}
	if pending == nil {
		h.seatsChanged(orgID)
	}
	writeJSONBareStatus(w, http.StatusAccepted, resp)
}

// projectInviteNeedsAdmin is the answer to a project owner, not a workspace
// admin, who adds an address with no account.
const projectInviteNeedsAdmin = "only a workspace admin can invite someone who has no OpenV account yet: ask one to invite them from Workspace settings, then add them here"
