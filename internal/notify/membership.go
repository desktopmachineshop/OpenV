package notify

import (
	"fmt"
	"log/slog"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Membership and privilege notifications.
//
// Two audiences, for two different reasons. The person whose access changed
// needs to know what they can do now — being handed admin, or losing it, is
// otherwise something you discover by noticing a button has gone. The
// workspace's admins need to know who joined and who left, which is a
// governance question rather than a courtesy: an unnoticed arrival in a
// workspace is exactly the thing an audit asks about later.
//
// Nobody is told about their own action. An admin who changes somebody's role
// does not need an admin alert about it, and a member who leaves voluntarily
// does not need telling that they left.

// OrgMemberLister is the slice of orgs.Service the membership fan-out needs.
// Kept narrow for the same reason MemberLister is: the notifier should not be
// able to write anything.
type OrgMemberLister interface {
	ListMembers(orgID string) ([]*orgs.Member, error)
}

// UserNamer resolves a user id to a display name, so an admin's notification
// can say who joined rather than quoting a UUID at them. Optional: without it
// the copy falls back to "someone".
type UserNamer interface {
	GetUserName(userID string) string
}

// UserNamerFunc adapts a plain lookup to UserNamer, so a caller can pass a
// closure over its user service rather than declaring a type for it.
type UserNamerFunc func(userID string) string

// GetUserName implements UserNamer.
func (f UserNamerFunc) GetUserName(userID string) string { return f(userID) }

// SetOrgService attaches the workspace member lister that the membership
// notifications need. Without it those events are ignored rather than
// half-delivered.
func (n *Notifier) SetOrgService(svc OrgMemberLister) *Notifier {
	n.orgSvc = svc
	return n
}

// SetUserNamer attaches the name resolver used in membership copy.
func (n *Notifier) SetUserNamer(namer UserNamer) *Notifier {
	n.userNamer = namer
	return n
}

// handleMembership routes the membership events. Returns false when the event
// is not one of them, so Handle can fall through to its other cases.
func (n *Notifier) handleMembership(e domainevents.Event) bool {
	switch e.EventType {
	case domainevents.OrgMemberAdded,
		domainevents.OrgMemberRoleChanged,
		domainevents.OrgMemberRemoved,
		domainevents.OrgInvitationSent,
		domainevents.OrgInvitationAccepted,
		domainevents.ProjectMemberAdded,
		domainevents.ProjectMemberRoleChanged,
		domainevents.ProjectMemberRemoved:
	default:
		return false
	}

	subject := payloadString(e, "user_id")
	name := n.nameOf(subject)

	// The affected person, told about their own access. An invitation to an
	// address with no account has no one to tell in app — that person gets the
	// invitation email instead — so it is skipped here rather than faked.
	if subject != "" && e.EventType != domainevents.OrgInvitationSent {
		if title, body, ok := accessMessage(e); ok {
			n.deliver(e, subject, notifications.TypeAccessChanged, title, body, membershipRef(e, subject))
		}
	}

	// The workspace's admins, told who came and went.
	if title, body, ok := adminMessage(e, name); ok {
		n.alertOrgAdmins(e, title, body, membershipRef(e, subject))
	}
	return true
}

// nameOf resolves a display name, falling back to something readable rather
// than a UUID a person cannot act on.
func (n *Notifier) nameOf(userID string) string {
	if userID == "" || n.userNamer == nil {
		return "Someone"
	}
	if name := n.userNamer.GetUserName(userID); name != "" {
		return name
	}
	return "Someone"
}

// membershipRef points the frontend at the thing that changed: a project's
// members tab for project events, the workspace's for the rest.
func membershipRef(e domainevents.Event, subject string) map[string]interface{} {
	ref := map[string]interface{}{
		"kind":    "membership",
		"org_id":  e.OrgID,
		"user_id": subject,
	}
	if e.ProjectID != "" {
		ref["kind"] = "project_membership"
		ref["project_id"] = e.ProjectID
	}
	return ref
}

// accessMessage is what the affected person reads. It states the new state
// rather than the transition, because "you are now an admin" is what they
// need and "your role changed" is not.
func accessMessage(e domainevents.Event) (title, body string, ok bool) {
	role := payloadString(e, "role")
	to := payloadString(e, "to")
	switch e.EventType {
	case domainevents.OrgMemberAdded, domainevents.OrgInvitationAccepted:
		return "You joined a workspace",
			fmt.Sprintf("You are now a member of this workspace with the %s role.", roleWord(role)), true
	case domainevents.OrgMemberRoleChanged:
		return "Your workspace access changed",
			fmt.Sprintf("Your role in this workspace is now %s.", roleWord(to)), true
	case domainevents.OrgMemberRemoved:
		// Someone who left knows they left. Only removal by another person is
		// news, and it is news they will otherwise meet as an unexplained
		// permission error.
		if payloadBool(e, "self") {
			return "", "", false
		}
		return "You were removed from a workspace",
			"You no longer have access to this workspace.", true
	case domainevents.ProjectMemberAdded:
		return "You were added to a project",
			fmt.Sprintf("You now have %s access to this project.", roleWord(role)), true
	case domainevents.ProjectMemberRoleChanged:
		return "Your project access changed",
			fmt.Sprintf("Your role in this project is now %s.", roleWord(to)), true
	case domainevents.ProjectMemberRemoved:
		if payloadBool(e, "self") {
			return "", "", false
		}
		return "You were removed from a project",
			"You no longer have access to this project.", true
	}
	return "", "", false
}

// adminMessage is what the workspace's admins read. Project-level changes are
// deliberately not reported to workspace admins: they happen constantly and
// would drown the arrivals and departures that matter.
func adminMessage(e domainevents.Event, name string) (title, body string, ok bool) {
	switch e.EventType {
	case domainevents.OrgMemberAdded, domainevents.OrgInvitationAccepted:
		return "Someone joined the workspace",
			fmt.Sprintf("%s joined this workspace as %s.", name, roleWord(payloadString(e, "role"))), true
	case domainevents.OrgInvitationSent:
		return "An invitation was sent",
			fmt.Sprintf("%s was invited to this workspace as %s.",
				payloadString(e, "email"), roleWord(payloadString(e, "role"))), true
	case domainevents.OrgMemberRoleChanged:
		return "A workspace role changed",
			fmt.Sprintf("%s is now %s in this workspace (was %s).",
				name, roleWord(payloadString(e, "to")), roleWord(payloadString(e, "from"))), true
	case domainevents.OrgMemberRemoved:
		if payloadBool(e, "self") {
			return "Someone left the workspace",
				fmt.Sprintf("%s left this workspace.", name), true
		}
		return "Someone was removed from the workspace",
			fmt.Sprintf("%s was removed from this workspace.", name), true
	}
	return "", "", false
}

// roleWord renders a role for a sentence, without inventing one for an empty
// value.
func roleWord(role string) string {
	if role == "" {
		return "a member"
	}
	switch role {
	case orgs.RoleAdmin:
		return "an admin"
	case orgs.RoleMember:
		return "a member"
	}
	return "a " + role
}

// alertOrgAdmins delivers to every admin of the event's workspace except the
// actor. It is the membership counterpart of the budget monitor's fan-out and
// shares its shape: list, filter to admins, skip the actor, deliver.
func (n *Notifier) alertOrgAdmins(e domainevents.Event, title, body string, ref map[string]interface{}) {
	if n.orgSvc == nil || e.OrgID == "" {
		return
	}
	list, err := n.orgSvc.ListMembers(e.OrgID)
	if err != nil {
		slog.Error("notify: failed to list workspace members",
			"event_type", e.EventType, "org_id", e.OrgID, "error", err)
		return
	}
	for _, mem := range list {
		if mem.Role != orgs.RoleAdmin {
			continue
		}
		n.deliver(e, mem.UserID, notifications.TypeMembershipChanged, title, body, ref)
	}
}

// payloadBool reads a boolean payload field, treating anything else as false.
func payloadBool(e domainevents.Event, key string) bool {
	if v, ok := e.Payload[key].(bool); ok {
		return v
	}
	return false
}
