package notify

// Once-only notices to a workspace's admins, sent by a module through ext
// (ext.Mail, docs/extending.md): the hosted service's billing emails about
// a failed payment or a trial about to end. A module knows when; this file
// knows how: who administers the workspace, at which address, and the
// record (orgs.Notices, migration 72) that stops a polling loop sending one
// notice twice.
//
// A notice is a service message about the workspace itself, not an activity
// notification, so the email opt-out (issue #187) does not apply to it, as
// it does not to a password reset; it is never stored in the bell either.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// WorkspaceNotice is one notice: Key names the occurrence it is about and is
// sent at most once per workspace; Body is plain text, put between a
// greeting and the link to Path (an app path such as
// "/org/settings?tab=billing"; "" for no link).
type WorkspaceNotice struct {
	Key     string
	Subject string
	Body    string
	Path    string
}

// NoticeStore is what WorkspaceMail reads and records; orgs.Service
// satisfies it.
type NoticeStore interface {
	Get(id string) (*orgs.Org, error)
	ListMembers(orgID string) ([]*orgs.Member, error)
	orgs.Notices
}

// WorkspaceMail sends WorkspaceNotices over the server's mailer.
type WorkspaceMail struct {
	mailer   Mailer
	store    NoticeStore
	users    UserDirectory
	linkBase string
}

// NewWorkspaceMail wires the notices to the mailer, the workspaces and the
// accounts; linkBase is the app's origin, as the notification emails use.
func NewWorkspaceMail(mailer Mailer, store NoticeStore, dir UserDirectory, linkBase string) *WorkspaceMail {
	return &WorkspaceMail{mailer: mailer, store: store, users: dir, linkBase: strings.TrimRight(linkBase, "/")}
}

// Enabled reports whether the deployment sends email at all (OPENV_SMTP_HOST).
func (w *WorkspaceMail) Enabled() bool { return w != nil && w.mailer != nil && w.mailer.Enabled() }

// ErrNoticeMalformed is a notice without a key, a subject or a body.
var ErrNoticeMalformed = errors.New("notice needs a key, a subject and a body")

// noticeKeyMax is workspace_notices.notice_key's width.
const noticeKeyMax = 200

// SendToAdmins emails n to every admin of the workspace orgID, unless a
// notice under n.Key was already sent there; sent reports whether this call
// sent it. With mail off, or no admin with an address, nothing is sent or
// recorded, so a later call can still send it. The claim is taken before
// the first email and released when no email got out, so a notice that
// failed everywhere is tried again on the next call and one that reached
// some admins is never repeated to them; err then names the failures.
func (w *WorkspaceMail) SendToAdmins(ctx context.Context, orgID string, n WorkspaceNotice) (sent bool, err error) {
	if strings.TrimSpace(n.Key) == "" || len(n.Key) > noticeKeyMax || strings.TrimSpace(n.Subject) == "" || strings.TrimSpace(n.Body) == "" {
		return false, ErrNoticeMalformed
	}
	if !w.Enabled() {
		return false, nil
	}
	org, err := w.store.Get(orgID)
	if err != nil {
		return false, err
	}
	members, err := w.store.ListMembers(orgID)
	if err != nil {
		return false, err
	}
	type recipient struct{ name, email string }
	var to []recipient
	seen := map[string]bool{}
	ToOrgAdmins(members, func(userID string) {
		u, err := w.users.GetByID(userID)
		if err != nil || u == nil {
			return
		}
		addr := strings.TrimSpace(u.Email)
		if addr == "" || seen[strings.ToLower(addr)] {
			return
		}
		seen[strings.ToLower(addr)] = true
		to = append(to, recipient{name: u.Name, email: addr})
	})
	if len(to) == 0 {
		return false, nil
	}
	claimed, err := w.store.ClaimNotice(orgID, n.Key, n.Subject)
	if err != nil || !claimed {
		return false, err
	}
	var failures []error
	delivered := 0
	for _, r := range to {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if err := w.mailer.Send(r.email, n.Subject, w.render(org, r.name, n)); err != nil {
			failures = append(failures, fmt.Errorf("send to an admin: %w", err))
			continue
		}
		delivered++
	}
	if delivered == 0 {
		if err := w.store.ReleaseNotice(orgID, n.Key); err != nil {
			slog.Error("notice: could not release the claim of a notice that reached nobody; it will not be retried", "org_id", orgID, "key", n.Key, "error", err)
		}
		return false, errors.Join(failures...)
	}
	if err := w.store.RecordNotice(orgID, n.Key, delivered); err != nil {
		slog.Warn("notice: sent but the recipient count was not recorded", "org_id", orgID, "key", n.Key, "error", err)
	}
	return true, errors.Join(failures...)
}

// render is one admin's email: a greeting, the module's text, the link and
// a line saying why they got it.
func (w *WorkspaceMail) render(org *orgs.Org, name string, n WorkspaceNotice) string {
	var b strings.Builder
	if v := strings.TrimSpace(memberText(name)); v != "" {
		b.WriteString("Hi " + v + ",\n\n")
	} else {
		b.WriteString("Hi,\n\n")
	}
	b.WriteString(strings.TrimSpace(n.Body))
	b.WriteString("\n")
	if n.Path != "" {
		b.WriteString("\n" + w.linkBase + n.Path + "\n")
	}
	workspace := "an OpenV workspace"
	if org != nil {
		if v := strings.TrimSpace(memberText(org.Name)); v != "" {
			workspace = "the OpenV workspace " + v
		}
	}
	b.WriteString("\n--\nYou are receiving this because you administer " + workspace + ".\n")
	return b.String()
}
