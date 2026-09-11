package notify

// Workspace invitation mail (REQ-95). The invitations domain owns the
// tokens; this file owns what the deployment does with one: build the link
// and render the message. Sending is optional by design — with no SMTP the
// invitation still exists and the admin copies the link out of the UI, which
// is the only way a self-hosted stack with no mailer can invite anyone.

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// InvitationLink is the URL in the email: the sign-in page with the raw
// token, which opens the create-account form with the address prefilled.
func InvitationLink(linkBase, token string) string {
	return strings.TrimRight(strings.TrimSpace(linkBase), "/") + "/login?invite=" + url.QueryEscape(token)
}

// RenderInvitationEmail returns the plain-text subject and body.
func RenderInvitationEmail(orgName, invitedByName, link string, ttl time.Duration) (subject, body string) {
	workspace := strings.TrimSpace(orgName)
	if workspace == "" {
		workspace = "an OpenV workspace"
	}
	inviter := strings.TrimSpace(invitedByName)

	var b strings.Builder
	b.WriteString("Hi,\n\n")
	if inviter != "" {
		fmt.Fprintf(&b, "%s has invited you to join %s on OpenV.\n\n", inviter, workspace)
	} else {
		fmt.Fprintf(&b, "You have been invited to join %s on OpenV.\n\n", workspace)
	}
	b.WriteString(link)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "The link is valid for %d days and works once. If you were not expecting this invitation, ignore this message.\n", int(ttl.Hours()/24))
	return "You have been invited to " + workspace + " on OpenV", b.String()
}
