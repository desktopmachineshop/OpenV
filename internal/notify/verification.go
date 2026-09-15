package notify

// Email verification for sign-ups (SEC-15 / REQ-95). The users domain owns
// the tokens; this file owns what the deployment can do with them: decide
// whether verification is enforced at all, build the link a person clicks,
// render the mail, and send it with a bound on how long a request waits.

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// VerificationSendTimeout bounds a send made on a request path. net/smtp
// carries no context, so the send runs in a goroutine and the caller stops
// waiting at the timeout; the goroutine still finishes and logs its result.
const VerificationSendTimeout = 10 * time.Second

// ErrSendTimeout is returned by SendWithTimeout when the mailer did not
// answer in time. The message may still have been delivered.
var ErrSendTimeout = errors.New("email: send timed out")

// VerificationPolicyFromEnv decides whether password accounts must verify
// their address: on iff the mailer can send and OPENV_EMAIL_VERIFICATION is
// not "off". It logs one line naming the state, so an operator reading the
// boot log knows whether their users are about to meet the wall.
func VerificationPolicyFromEnv(m Mailer) users.EmailVerificationPolicy {
	switched := strings.ToLower(strings.TrimSpace(os.Getenv("OPENV_EMAIL_VERIFICATION")))
	switch {
	case switched == "off":
		slog.Info("email verification: disabled (OPENV_EMAIL_VERIFICATION=off)")
		return users.EmailVerificationPolicy{}
	case m == nil || !m.Enabled():
		slog.Info("email verification: disabled (no OPENV_SMTP_HOST; accounts are verified at sign-up)")
		return users.EmailVerificationPolicy{}
	default:
		slog.Info("email verification: required for password accounts (SMTP configured; set OPENV_EMAIL_VERIFICATION=off to disable)")
		return users.EmailVerificationPolicy{Required: true}
	}
}

// VerificationLink is the URL in the email: the frontend's verify page with
// the raw token as a query parameter.
func VerificationLink(linkBase, token string) string {
	return strings.TrimRight(strings.TrimSpace(linkBase), "/") + "/verify-email?token=" + url.QueryEscape(token)
}

// RenderVerificationEmail returns the plain-text subject and body.
func RenderVerificationEmail(name, link string, ttl time.Duration) (subject, body string) {
	greeting := "Hi,"
	if n := strings.TrimSpace(name); n != "" {
		greeting = "Hi " + n + ","
	}
	hours := int(ttl.Hours())
	var b strings.Builder
	b.WriteString(greeting)
	b.WriteString("\n\nConfirm this address to start using OpenV:\n\n")
	b.WriteString(link)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "The link is valid for %d hours and works once. If you did not create an OpenV account, ignore this message.\n", hours)
	return "Verify your email for OpenV", b.String()
}

// PasswordResetLink is the URL in a reset email: the frontend's reset page
// with the raw token as a query parameter (REQ-158).
func PasswordResetLink(linkBase, token string) string {
	return strings.TrimRight(strings.TrimSpace(linkBase), "/") + "/reset-password?token=" + url.QueryEscape(token)
}

// RenderPasswordResetEmail returns the plain-text subject and body of a
// reset mail. It never says whether the request came from the account's
// owner, only what to do if it did not.
func RenderPasswordResetEmail(name, link string, ttl time.Duration) (subject, body string) {
	greeting := "Hi,"
	if n := strings.TrimSpace(name); n != "" {
		greeting = "Hi " + n + ","
	}
	var b strings.Builder
	b.WriteString(greeting)
	b.WriteString("\n\nSomebody asked to reset the password of your OpenV account. Set a new one here:\n\n")
	b.WriteString(link)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "The link is valid for %s and works once. If you did not ask for this, ignore this message: your password stays as it is.\n", describeTTL(ttl))
	return "Reset your OpenV password", b.String()
}

func describeTTL(ttl time.Duration) string {
	if ttl < 2*time.Hour {
		return fmt.Sprintf("%d minutes", int(ttl.Minutes()))
	}
	return fmt.Sprintf("%d hours", int(ttl.Hours()))
}

// SendWithTimeout delivers one message and gives up waiting after timeout.
func SendWithTimeout(m Mailer, to, subject, body string, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		err := m.Send(to, subject, body)
		if err != nil {
			slog.Error("email: failed to send verification email", "to", to, "error", err)
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		slog.Warn("email: verification send still pending after timeout", "to", to, "timeout", timeout)
		return ErrSendTimeout
	}
}
