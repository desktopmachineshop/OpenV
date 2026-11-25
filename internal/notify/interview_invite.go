package notify

// Interview invitation mail. An interview invite is a link anybody can open
// with no account: the AI interviewer asks, the participant answers. A
// member can copy the link and send it themselves, or give an address and
// have OpenV send it, which is what this message is. The interviews domain
// owns the token; this file owns the link and the words around it.

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// InterviewInviteLink is the URL in the email: the public interview page
// with the raw token, the same path the app copies to the clipboard.
func InterviewInviteLink(linkBase, token string) string {
	return strings.TrimRight(strings.TrimSpace(linkBase), "/") + "/interview/" + url.PathEscape(token)
}

// RenderInterviewInviteEmail returns the plain-text subject and body.
// expiresAt is the invite's own expiry, nil when the link does not expire.
func RenderInterviewInviteEmail(invitedByName, interviewName, link string, expiresAt *time.Time) (subject, body string) {
	inviter := strings.TrimSpace(memberText(invitedByName))
	name := strings.TrimSpace(memberText(interviewName))

	var b strings.Builder
	b.WriteString("Hi,\n\n")
	switch {
	case inviter != "" && name != "":
		fmt.Fprintf(&b, "%s would like to hear from you, and has invited you to an interview on OpenV: %s.\n\n", inviter, name)
	case inviter != "":
		fmt.Fprintf(&b, "%s would like to hear from you, and has invited you to an interview on OpenV.\n\n", inviter)
	case name != "":
		fmt.Fprintf(&b, "You have been invited to an interview on OpenV: %s.\n\n", name)
	default:
		b.WriteString("You have been invited to an interview on OpenV.\n\n")
	}
	b.WriteString("An AI interviewer asks the questions and you answer in your own words, at your own pace. You do not need an account.\n\n")
	b.WriteString(link)
	b.WriteString("\n\n")
	if expiresAt != nil {
		fmt.Fprintf(&b, "The link works until %s. ", expiresAt.UTC().Format("2 January 2006"))
	}
	b.WriteString("Anyone with the link can answer in your place, so please do not forward it. If you were not expecting this invitation, ignore this message.\n")

	subject = "You are invited to an interview on OpenV"
	if inviter != "" {
		subject = inviter + " invited you to an interview on OpenV"
	}
	return subject, b.String()
}
