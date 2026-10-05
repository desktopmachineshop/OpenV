package notify

import (
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

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
	claims   ReleaseClaimer
	members  ChannelMembers
	delivery Delivery
	now      func() time.Time
}

// NewReleaseAnnouncer creates an announcer. broadcaster may be nil.
func NewReleaseAnnouncer(claims ReleaseClaimer, members ChannelMembers, store notifications.Service, broadcaster Broadcaster) *ReleaseAnnouncer {
	return &ReleaseAnnouncer{claims: claims, members: members, delivery: Delivery{Store: store, Broadcaster: broadcaster}, now: time.Now}
}

// SetChannels attaches the email and web push side channels at once (a nil
// one is left off).
func (a *ReleaseAnnouncer) SetChannels(c Channels) *ReleaseAnnouncer {
	a.delivery.Channels = c
	return a
}

// SetEmailDispatcher attaches the email side channel (nil leaves it off).
func (a *ReleaseAnnouncer) SetEmailDispatcher(d *EmailDispatcher) *ReleaseAnnouncer {
	a.delivery.Email = d
	return a
}

// SetPushDispatcher attaches the web push side channel (nil leaves it off).
func (a *ReleaseAnnouncer) SetPushDispatcher(d *PushDispatcher) *ReleaseAnnouncer {
	a.delivery.Push = d
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
	// announced_at is a TIMESTAMP holding a UTC wall clock (#379 bug 162).
	won, err := a.claims.ClaimReleaseAnnouncement(rel.Version, a.now().UTC())
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
		if err := a.delivery.Deliver(n); err != nil {
			slog.Error("release: failed to store notification", "user_id", userID, "error", err)
			continue
		}
		notified++
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
//
// The bell, the email and the push show text, not Markdown, so each bullet
// is rendered as plain text (plainNote); the What's new page renders the
// notes' Markdown itself.
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
			b.WriteString(plainNote(l))
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

// The inline Markdown plainNote reads. A code span's text and an escaped
// character are set aside first, each replaced by a private-use character
// that stands for it (mdSetAside), so that nothing inside them is read as a
// marker. A span's text is one character or more, and the rest of it is
// optional lazily (??): a one-character span such as **J** closes at its own
// marker rather than at the next span's on the same line.
var (
	mdLink     = regexp.MustCompile(`!?\[([^\]]*)\]\([^()\s]*(?:\s+"[^"]*")?\)`)
	mdAutolink = regexp.MustCompile(`<((?:https?|mailto):[^<>\s]+)>`)
	mdStrong   = regexp.MustCompile(`\*\*(\S(?:.*?\S)??)\*\*`)
	mdStrike   = regexp.MustCompile(`~~(\S(?:.*?\S)??)~~`)
	mdEm       = regexp.MustCompile(`\*(\S(?:.*?\S)??)\*`)
	// Underscores mark emphasis only at a word's edge, never inside one, so
	// a name such as hosted_runner_minutes keeps them.
	mdUnderStrong = regexp.MustCompile(`(^|[^\p{L}\p{N}_])__(\S(?:.*?\S)??)__($|[^\p{L}\p{N}_])`)
	mdUnderEm     = regexp.MustCompile(`(^|[^\p{L}\p{N}_])_(\S(?:.*?\S)??)_($|[^\p{L}\p{N}_])`)
)

// mdSetAside is the first of the private-use characters (U+E000 to U+F8FF)
// that stand for the text set aside, the nth for the nth piece. A note's own
// character in that range is set aside too, so it comes back as written.
const (
	mdSetAside     = '\uE000'
	mdSetAsideLast = '\uF8FF'
)

// plainNote renders one release note, Markdown as RELEASE_NOTES.md has it,
// as plain text (#379, bug 62): emphasis and strikethrough markers and a
// code span's backticks are dropped, a link or an image keeps its text and
// an autolink its address, and a backslash escape keeps the character it
// escapes. Only inline Markdown is read, since a note is the text of one
// bullet; anything else stays as written.
func plainNote(md string) string {
	var aside []string
	var b strings.Builder
	setAside := func(text string) {
		b.WriteRune(mdSetAside + rune(len(aside)))
		aside = append(aside, text)
	}
	for i := 0; i < len(md); {
		if mdSetAside+rune(len(aside)) > mdSetAsideLast {
			// More pieces to set aside than characters to stand for
			// them, which no note comes near: shown as written.
			return md
		}
		c := md[i]
		if c == '\\' && i+1 < len(md) && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", md[i+1]) >= 0 {
			setAside(md[i+1 : i+2])
			i += 2
			continue
		}
		if c == '`' {
			ticks := len(md[i:]) - len(strings.TrimLeft(md[i:], "`"))
			if end := closingTicks(md, i+ticks, ticks); end >= 0 {
				setAside(codeSpanText(md[i+ticks : end]))
				i = end + ticks
				continue
			}
			// No closing run: the ticks are text.
			b.WriteString(md[i : i+ticks])
			i += ticks
			continue
		}
		r, size := utf8.DecodeRuneInString(md[i:])
		if r >= mdSetAside && r <= mdSetAsideLast {
			setAside(string(r))
		} else {
			b.WriteRune(r)
		}
		i += size
	}
	s := mdLink.ReplaceAllString(b.String(), "$1")
	s = mdAutolink.ReplaceAllString(s, "$1")
	for {
		next := mdStrong.ReplaceAllString(s, "$1")
		next = mdUnderStrong.ReplaceAllString(next, "${1}${2}${3}")
		next = mdStrike.ReplaceAllString(next, "$1")
		next = mdEm.ReplaceAllString(next, "$1")
		next = mdUnderEm.ReplaceAllString(next, "${1}${2}${3}")
		if next == s {
			break
		}
		s = next
	}
	var out strings.Builder
	for _, r := range s {
		if k := int(r - mdSetAside); r >= mdSetAside && k < len(aside) {
			out.WriteString(aside[k])
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// closingTicks finds the run of exactly n backticks that closes a code span
// opened before from, or -1.
func closingTicks(md string, from, n int) int {
	for i := from; i < len(md); {
		if md[i] != '`' {
			i++
			continue
		}
		run := len(md[i:]) - len(strings.TrimLeft(md[i:], "`"))
		if run == n {
			return i
		}
		i += run
	}
	return -1
}

// codeSpanText is a code span's text: one space on each side is padding
// when both are there and the text is not only spaces.
func codeSpanText(s string) string {
	if len(s) >= 2 && s[0] == ' ' && s[len(s)-1] == ' ' && strings.Trim(s, " ") != "" {
		return s[1 : len(s)-1]
	}
	return s
}
