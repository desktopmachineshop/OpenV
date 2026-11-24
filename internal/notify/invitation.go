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
	return RenderInvitationEmailWithProjects(orgName, invitedByName, nil, link, ttl)
}

// RenderInvitationEmailWithProjects is RenderInvitationEmail for an
// invitation that also opens projects (email-invites): the message names
// them, since a project is usually why the person was asked. With no
// projects it is the plain workspace invitation, word for word.
func RenderInvitationEmailWithProjects(orgName, invitedByName string, projectNames []string, link string, ttl time.Duration) (subject, body string) {
	workspace := strings.TrimSpace(memberText(orgName))
	if workspace == "" {
		workspace = "an OpenV workspace"
	}
	inviter := strings.TrimSpace(memberText(invitedByName))
	projects := projectList(projectNames)

	var b strings.Builder
	b.WriteString("Hi,\n\n")
	switch {
	case projects != "" && inviter != "":
		fmt.Fprintf(&b, "%s has invited you to %s in %s on OpenV.\n\n", inviter, projects, workspace)
	case projects != "":
		fmt.Fprintf(&b, "You have been invited to %s in %s on OpenV.\n\n", projects, workspace)
	case inviter != "":
		fmt.Fprintf(&b, "%s has invited you to join %s on OpenV.\n\n", inviter, workspace)
	default:
		fmt.Fprintf(&b, "You have been invited to join %s on OpenV.\n\n", workspace)
	}
	b.WriteString(link)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "The link is valid for %d days and works once. If you were not expecting this invitation, ignore this message.\n", int(ttl.Hours()/24))
	if projects != "" {
		return "You have been invited to " + projects + " on OpenV", b.String()
	}
	return "You have been invited to " + workspace + " on OpenV", b.String()
}

// projectList names projects for a sentence: "the project Brakes", "the
// projects Brakes and Steering", "the projects A, B and C"; "" for none.
func projectList(names []string) string {
	var clean []string
	for _, n := range names {
		if n = strings.TrimSpace(memberText(n)); n != "" {
			clean = append(clean, n)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return "the project " + clean[0]
	default:
		return "the projects " + strings.Join(clean[:len(clean)-1], ", ") + " and " + clean[len(clean)-1]
	}
}
