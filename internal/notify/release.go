package notify

import (
	"log/slog"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// Release announcements.
//
// The API knows which release it is from the notes it was built with. At
// boot it claims that release in the database and, when the claim is won,
// tells every account with a workspace on the nightly channel: one inbox
// row per member carrying the release's customer-facing notes, pushed live
// to open tabs (REQ-140). Accounts whose workspaces are all on the stable
// channel hear about releases when their stable turns on instead (see
// stable.go). A replica that loses the claim, or a restart of the same
// release, announces nothing.

// ReleaseClaimer is the persistence slice the announcer needs: the atomic
// once-per-release claim.
type ReleaseClaimer interface {
	ClaimReleaseAnnouncement(version string, at time.Time) (bool, error)
}

// ChannelMembers lists the accounts with a workspace on a channel;
// orgs.Service satisfies it.
type ChannelMembers interface {
	ListMemberUserIDsByChannel(channel string) ([]string, error)
}

// ReleaseAnnouncer materialises the release notification.
type ReleaseAnnouncer struct {
	claims      ReleaseClaimer
	members     ChannelMembers
	store       notifications.Service
	broadcaster Broadcaster
	email       *EmailDispatcher
	push        *PushDispatcher
	now         func() time.Time
}

// NewReleaseAnnouncer creates an announcer. broadcaster may be nil.
func NewReleaseAnnouncer(claims ReleaseClaimer, members ChannelMembers, store notifications.Service, broadcaster Broadcaster) *ReleaseAnnouncer {
	return &ReleaseAnnouncer{claims: claims, members: members, store: store, broadcaster: broadcaster, now: time.Now}
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

// Announce tells every nightly-channel account about rel, once. A nil
// release (a build with no dated section yet) announces nothing. Returns
// how many accounts were notified; 0 when the claim was lost or nobody is
// on the channel.
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
	ids, err := a.members.ListMemberUserIDsByChannel(orgs.ChannelNightly)
	if err != nil {
		slog.Error("release: failed to list accounts to notify", "version", rel.Version, "error", err)
		return 0
	}
	title, body := ReleaseMessage(rel.Version, rel.Notes)
	ref := map[string]interface{}{"kind": "release", "version": rel.Version}
	notified := a.deliver(ids, notifications.TypeReleasePublished, title, body, ref)
	slog.Info("release: announced", "version", rel.Version, "channel", orgs.ChannelNightly, "accounts", notified)
	return notified
}

// deliver stores one notification per account and pushes it live; the
// email and push side channels stay nil-safe.
func (a *ReleaseAnnouncer) deliver(userIDs []string, ntype, title, body string, ref map[string]interface{}) int {
	notified := 0
	for _, id := range userIDs {
		n := notifications.New("", id, ntype, title, body, ref)
		if err := a.store.Create(n); err != nil {
			slog.Error("release: failed to store notification", "user_id", id, "error", err)
			continue
		}
		notified++
		if a.broadcaster != nil {
			a.broadcaster.BroadcastSession(StreamKey(id), "notification", n)
		}
		a.email.Dispatch(n)
		a.push.Dispatch(n)
	}
	return notified
}

// ReleaseMessage is the notification copy: a title naming the release and a
// body listing its first bullets, one per line, with a pointer to the rest.
func ReleaseMessage(version string, notes []release.Note) (title, body string) {
	title = "OpenV was updated (" + version + ")"
	lines := notes
	more := 0
	if len(lines) > maxAnnouncedNotes {
		more = len(lines) - maxAnnouncedNotes
		lines = lines[:maxAnnouncedNotes]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("• ")
		if l.Fix {
			b.WriteString("Fix: ")
		}
		b.WriteString(l.Text)
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
