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
// tells every account on the nightly channel: one inbox row per member
// carrying the release's customer-facing notes, pushed live to open tabs.
// A replica that loses the claim, or a restart of the same release,
// announces nothing. Accounts whose every workspace is on the stable
// channel hear nothing here; the stable scheduler (stable.go) tells them
// when their release turns on (REQ-140).

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
// release (a build with no release section yet) announces nothing. Returns
// how many accounts were notified; 0 when the claim was lost or nobody
// exists yet.
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
	all, err := a.members.ListMemberUserIDsByChannel(orgs.ChannelNightly)
	if err != nil {
		slog.Error("release: failed to list accounts to notify", "version", rel.Version, "error", err)
		return 0
	}
	title, body := ReleaseMessage(rel)
	ref := map[string]interface{}{"kind": "release", "version": rel.Version}
	notified := 0
	for _, userID := range all {
		n := notifications.New("", userID, notifications.TypeReleasePublished, title, body, ref)
		if err := a.store.Create(n); err != nil {
			slog.Error("release: failed to store notification", "user_id", userID, "error", err)
			continue
		}
		notified++
		if a.broadcaster != nil {
			a.broadcaster.BroadcastSession(StreamKey(userID), "notification", n)
		}
		a.email.Dispatch(n)
		a.push.Dispatch(n)
	}
	slog.Info("release: announced", "version", rel.Version, "accounts", notified)
	return notified
}

// ReleaseMessage is the notification copy: a title naming the release and a
// body listing its first bullets under the group each belongs to, with a
// pointer to the rest.
//
// The groups are here because "OpenV was updated, here are five bullets" is
// not what anyone wants to know. A member wants to tell a new capability
// apart from a fix to something that was annoying them, at a glance, without
// opening the page.
func ReleaseMessage(rel *release.Release) (title, body string) {
	title = release.Headline(rel.Version)
	var b strings.Builder
	left, more := maxAnnouncedNotes, 0
	for _, c := range rel.Categories {
		shown := c.Notes
		if len(shown) > left {
			shown = shown[:left]
		}
		left -= len(shown)
		more += len(c.Notes) - len(shown)
		if len(shown) == 0 {
			continue
		}
		// A legacy section has no groups of its own; its bullets are simply
		// the release, so heading them "Changes" adds a word and no meaning.
		if c.Name != release.UncategorizedNotes {
			b.WriteString(c.Name)
			b.WriteString("\n")
		}
		for _, l := range shown {
			b.WriteString("• ")
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	switch {
	case more > 0:
		b.WriteString("…and more under What's new.")
	case b.Len() == 0:
		b.WriteString("See What's new for details.")
	}
	return title, strings.TrimSpace(b.String())
}
