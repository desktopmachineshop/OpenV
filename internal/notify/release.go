package notify

import (
	"log/slog"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Release announcements.
//
// The API knows which release it is from the notes it was built with. At
// boot it claims that release in the database and, when the claim is won,
// tells every account: one inbox row per member carrying the release's
// customer-facing notes, pushed live to open tabs. A replica that loses the
// claim, or a restart of the same release, announces nothing.

// ReleaseClaimer is the persistence slice the announcer needs: the atomic
// once-per-release claim.
type ReleaseClaimer interface {
	ClaimReleaseAnnouncement(version string, at time.Time) (bool, error)
}

// UserLister lists every account; users.Service satisfies it.
type UserLister interface {
	ListUsers() ([]*users.User, error)
}

// ReleaseAnnouncer materialises the release notification.
type ReleaseAnnouncer struct {
	claims      ReleaseClaimer
	users       UserLister
	store       notifications.Service
	broadcaster Broadcaster
	email       *EmailDispatcher
	push        *PushDispatcher
	now         func() time.Time
}

// NewReleaseAnnouncer creates an announcer. broadcaster may be nil.
func NewReleaseAnnouncer(claims ReleaseClaimer, users UserLister, store notifications.Service, broadcaster Broadcaster) *ReleaseAnnouncer {
	return &ReleaseAnnouncer{claims: claims, users: users, store: store, broadcaster: broadcaster, now: time.Now}
}

// SetEmailDispatcher attaches the email side channel (nil leaves it off).
func (a *ReleaseAnnouncer) SetEmailDispatcher(d *EmailDispatcher) *ReleaseAnnouncer {
	a.email = d
	return a
}

// SetPushDispatcher attaches the web push side channel (nil leaves it off).
func (a *ReleaseAnnouncer) SetPushDispatcher(d *PushDispatcher) *ReleaseAnnouncer {
	a.push = d
	return a
}

// maxAnnouncedNotes bounds the body of the notification: the first few
// bullets are the summary, and the What's new page has the rest.
const maxAnnouncedNotes = 5

// Announce tells every account about rel, once. A nil release (a build with
// no dated section yet) announces nothing. Returns how many accounts were
// notified; 0 when the claim was lost or nobody exists yet.
func (a *ReleaseAnnouncer) Announce(rel *release.Release) int {
	if rel == nil || rel.Version == "" {
		return 0
	}
	won, err := a.claims.ClaimReleaseAnnouncement(rel.Version, a.now())
	if err != nil {
		slog.Error("release: failed to claim announcement", "version", rel.Version, "error", err)
		return 0
	}
	if !won {
		return 0
	}
	all, err := a.users.ListUsers()
	if err != nil {
		slog.Error("release: failed to list accounts to notify", "version", rel.Version, "error", err)
		return 0
	}
	title, body := ReleaseMessage(rel)
	ref := map[string]interface{}{"kind": "release", "version": rel.Version}
	notified := 0
	for _, u := range all {
		n := notifications.New("", u.ID, notifications.TypeReleasePublished, title, body, ref)
		if err := a.store.Create(n); err != nil {
			slog.Error("release: failed to store notification", "user_id", u.ID, "error", err)
			continue
		}
		notified++
		if a.broadcaster != nil {
			a.broadcaster.BroadcastSession(StreamKey(u.ID), "notification", n)
		}
		a.email.Dispatch(n)
		a.push.Dispatch(n)
	}
	slog.Info("release: announced", "version", rel.Version, "accounts", notified)
	return notified
}

// ReleaseMessage is the notification copy: a title naming the release and a
// body listing its first bullets, one per line, with a pointer to the rest.
func ReleaseMessage(rel *release.Release) (title, body string) {
	title = "OpenV was updated (" + rel.Version + ")"
	lines := rel.Notes
	more := 0
	if len(lines) > maxAnnouncedNotes {
		more = len(lines) - maxAnnouncedNotes
		lines = lines[:maxAnnouncedNotes]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("• ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	switch {
	case more > 0:
		b.WriteString("…and more under What's new.")
	case len(lines) == 0:
		b.WriteString("See What's new for details.")
	}
	return title, strings.TrimSpace(b.String())
}
