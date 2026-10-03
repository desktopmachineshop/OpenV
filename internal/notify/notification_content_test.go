package notify

// TestNotificationContent is refactor plan step S10 (docs/plans/
// codebase-refactor.md §6.4; invariant I18, quirk Q7): what every
// notification type delivers on every channel, pinned per type under
// testdata/notifications/<type>/ in four goldens:
//
//   - row.json: the row handed to the notifications store (every field, the
//     id and created_at normalised, EntityRef's Go value types beside it);
//   - sse.txt: the `notification` frame on the recipient's stream, its data
//     line exactly as the SSE hub writes it (json.Marshal of the value
//     broadcast; the hub's framing is S6's);
//   - email.txt: the SMTP envelope and the whole message the real
//     SMTPMailer hands to net/smtp (subject, plain-text body with its deep
//     link, line ends); there is no HTML part;
//   - push.json: the payload each subscribed device is sent before
//     encryption, which is what the service worker shows and follows.
//
// Every delivery path is driven directly, through the components cmd/server
// wires: the bus notifier (project content and membership), the budget and
// hosted-minutes monitors, the release announcer (release.go), the stable
// scheduler's announcement, reminder and turn-on (stable.go) and the
// dedicated instance's support-window warnings (dedicated.go). X6 rewrites
// the seven copies of store -> SSE -> email -> push behind notify.Delivery;
// these goldens are its proof, so they must stay byte for byte.
//
// Each scenario runs twice: with OPENV_EMAIL_NOTIFICATION_TYPES and
// OPENV_PUSH_NOTIFICATION_TYPES unset (the default allow-lists), and with
// both set to exactly the types the defaults leave out, so every type's
// email and push content is pinned once, and so is the default eligibility
// of each type and that an override replaces the defaults rather than
// adding to them. The row and the SSE frame must not differ between the
// two runs.
//
// The goldens do not show the order of a row's channels, so the test also
// fails when a row's email is recorded before its SSE frame, or its push
// before either (ncOrderProblem), or when a channel is not addressed to the
// row's recipient.
//
// TestNotificationStoreFailure, below, pins what each of the seven copies
// does when the store refuses a recipient's row: nothing is sent to that
// recipient, the next recipient still gets theirs, and the lines each copy
// logs and the count Announce returns.
//
// The bell's own deep links, which are the email and push links built here
// since the R7 fix for #379's bugs 58 and 59 (quirk Q7), are pinned beside
// them by the frontend's src/components/NotificationBell.paths.test.tsx,
// which reads these goldens.
// internal/domain/notifications's TestEveryNotificationTypeHasAContentGolden
// fails when a type constant has no golden directory.
//
// Regenerate (a deliberate, release-noted behavior change only):
//
//	UPDATE_GOLDEN=1 go test ./internal/notify -count=1 -run '^TestNotificationContent$'

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	// The stable scheduler's windows are in a named zone; the embedded
	// database makes them the same on a machine without one.
	_ "time/tzdata"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// --- Fixtures -------------------------------------------------------------

const (
	ncOrg     = "org-acme"
	ncOrgName = "Acme Rockets"
	// ncZurich is a stable-channel workspace with an upgrade window in a
	// named zone, and a name outside ASCII, which the email subject carries
	// RFC 2047-encoded (#379, bug 64).
	ncZurich     = "org-zurich"
	ncZurichName = "Zürich Labs"
	ncProject    = "proj-apollo"
)

// ncProjectMembers is the project's member list: an owner and an editor, who
// count as reviewers, and a viewer, who does not.
func ncProjectMembers() []*members.Member {
	return []*members.Member{
		{UserID: "user-ada", Role: members.RoleOwner, UserName: "Ada Lovelace", UserEmail: "ada@example.test"},
		{UserID: "user-ben", Role: members.RoleEditor, UserName: "Ben Okafor", UserEmail: "ben@example.test"},
		{UserID: "user-cy", Role: members.RoleViewer, UserName: "Cy Twombly", UserEmail: "cy@example.test"},
	}
}

// ncOrgMembers is each workspace's member list: admins and a member.
var ncOrgMembers = map[string][]*orgs.Member{
	ncOrg: {
		{OrgID: ncOrg, UserID: "user-ada", Role: orgs.RoleAdmin},
		{OrgID: ncOrg, UserID: "user-ben", Role: orgs.RoleMember},
		{OrgID: ncOrg, UserID: "user-dee", Role: orgs.RoleAdmin},
	},
	ncZurich: {
		{OrgID: ncZurich, UserID: "user-gus", Role: orgs.RoleAdmin},
		{OrgID: ncZurich, UserID: "user-hal", Role: orgs.RoleMember},
	},
}

// ncNames resolves display names; user-eve has none, so the copy falls back.
var ncNames = map[string]string{"user-ada": "Ada Lovelace", "user-ben": "Ben Okafor", "user-dee": "Dee Rao"}

type ncProjectLister struct{}

func (ncProjectLister) ListMembers(string) ([]*members.Member, error) { return ncProjectMembers(), nil }

// ncOrgs is every org-shaped dependency of the delivery paths.
type ncOrgs struct {
	list []*orgs.Org
}

func (o *ncOrgs) Get(id string) (*orgs.Org, error) {
	for _, org := range o.list {
		if org.ID == id {
			return org, nil
		}
	}
	return nil, nil
}
func (o *ncOrgs) ListMembers(orgID string) ([]*orgs.Member, error)    { return ncOrgMembers[orgID], nil }
func (o *ncOrgs) ClaimBudgetAlert(string, string, int) (bool, error)  { return true, nil }
func (o *ncOrgs) ClaimMinutesAlert(string, string, int) (bool, error) { return true, nil }
func (o *ncOrgs) ListOrgsByChannel(string) ([]*orgs.Org, error)       { return o.list, nil }
func (o *ncOrgs) SetStableRelease(id, version string) (*orgs.Org, error) {
	for _, org := range o.list {
		if org.ID == id {
			org.StableRelease = version
		}
	}
	return nil, nil
}
func (o *ncOrgs) ListAll() ([]string, error) {
	ids := make([]string, 0, len(o.list))
	for _, org := range o.list {
		ids = append(ids, org.ID)
	}
	return ids, nil
}

// ncClaims wins every claim except the ones listed as already taken.
type ncClaims struct{ taken map[string]bool }

func (c *ncClaims) ClaimReleaseAnnouncement(key string, _ time.Time) (bool, error) {
	return c.claim(key), nil
}
func (c *ncClaims) ClaimStableStep(orgID, version, step string, _ time.Time) (bool, error) {
	return c.claim(orgID + "/" + version + "/" + step), nil
}
func (c *ncClaims) claim(key string) bool {
	if c.taken == nil {
		c.taken = map[string]bool{}
	}
	if c.taken[key] {
		return false
	}
	c.taken[key] = true
	return true
}

type ncChannelMembers struct{}

func (ncChannelMembers) ListMemberUserIDsByChannel(channel string) ([]string, error) {
	if channel != orgs.ChannelNightly {
		return nil, fmt.Errorf("unexpected channel %q", channel)
	}
	return []string{"user-ada", "user-ben"}, nil
}

type ncSpend struct{ usd float64 }

func (s ncSpend) MonthlySpend(string, time.Time) (float64, error) { return s.usd, nil }

type ncMinutes struct{ used int }

func (m ncMinutes) MinutesUsed(string, time.Time) (int, error) { return m.used, nil }

func ncAt(s string) func() time.Time {
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return at }
}

func ncReleases(t *testing.T, markdown string) *release.DefaultService {
	t.Helper()
	svc, err := release.NewService(markdown)
	if err != nil {
		t.Fatalf("release notes fixture: %v", err)
	}
	return svc
}

// ncNightlyNotes is a release with more notes than an announcement shows.
const ncNightlyNotes = `## 0.16.0 — 2026-10-01

### New features

- **Baselines compare side by side.** Pick two baselines and see what
  changed between them.
- **Requirements import keeps enum attributes.**
- **The bell opens on the tab you left.**

### Maintenance updates

- **Projects load faster.**
- **Exports are named after their project.**

### Bug fixes

- **A comment that mentions you notifies you once.**
- **The review queue counts drafts correctly.**
`

// ncStableNotes designates 0.15.0 the stable release on 2026-09-01; its
// notes are merged with 0.14.2's, down to the previous stable, 0.14.0.
const ncStableNotes = `## 0.15.0 — 2026-08-28

Stable channel release since 2026-09-01.

### New features

- **Baselines compare side by side.**
- **Requirements import keeps enum attributes.**

### Bug fixes

- **The bell counts only unread rows.**

## 0.14.2 — 2026-08-20

### Maintenance updates

- **Projects load faster.**

### Bug fixes

- **Exports keep line breaks.**

## 0.14.0 — 2026-07-01

Stable channel release since 2026-07-03.

### New features

- **Interview links can be shared.**
`

// ncDedicatedNotes is what a dedicated instance on stable 0.14.0 was built
// with.
const ncDedicatedNotes = `## 0.14.0 — 2026-07-01

Stable channel release since 2026-07-03.

### New features

- **Interview links can be shared.**
`

// ncZurichOrg is a stable-channel workspace on 0.14.0 whose window opens on
// the 10th at 09:00 in Zurich: for a stable cut on 2026-09-01 that is
// 2026-09-10T07:00Z (inside the 14 days orgs.UpgradeWindowDays allows), with
// the reminder due from 2026-09-09T07:00Z.
func ncZurichOrg() *orgs.Org {
	return &orgs.Org{ID: ncZurich, Name: ncZurichName, StableRelease: "0.14.0",
		UpgradeDay: 10, UpgradeHour: 9, UpgradeTimezone: "Europe/Zurich"}
}

// --- Scenarios ------------------------------------------------------------

// ncScenario is one thing that happens, in words (the name every golden
// shows), and how to make it happen through the code cmd/server wires.
type ncScenario struct {
	name  string
	drive func(t *testing.T, ch *ncChannels)
}

func (ch *ncChannels) notifier() *Notifier {
	return NewNotifier(ch.store, ncProjectLister{}, ch.bc).
		SetEmailDispatcher(ch.email).
		SetPushDispatcher(ch.push).
		SetOrgService(&ncOrgs{}).
		SetUserNamer(UserNamerFunc(func(id string) string { return ncNames[id] }))
}

func ncEvent(eventType, actor, entityID string, payload map[string]interface{}) domainevents.Event {
	return domainevents.Event{EventType: eventType, OrgID: ncOrg, ProjectID: ncProject, EntityID: entityID,
		Actor: actor, Payload: payload}
}

func ncOrgEvent(eventType, actor string, payload map[string]interface{}) domainevents.Event {
	return domainevents.Event{EventType: eventType, OrgID: ncOrg, Actor: actor, Payload: payload}
}

func ncProjectEvent(eventType, actor string, payload map[string]interface{}) domainevents.Event {
	return domainevents.Event{EventType: eventType, OrgID: ncOrg, ProjectID: ncProject, Actor: actor, Payload: payload}
}

func (ch *ncChannels) handle(e domainevents.Event) { ch.notifier().Handle(e) }

func (ch *ncChannels) budget(t *testing.T, spend float64) {
	budget := 250.0
	org := &orgs.Org{ID: ncOrg, Name: ncOrgName, MonthlyBudgetUSD: &budget}
	m := NewBudgetMonitor(&ncOrgs{list: []*orgs.Org{org}}, ncSpend{usd: spend}, ch.store, ch.bc).
		SetEmailDispatcher(ch.email).
		SetPushDispatcher(ch.push)
	m.now = ncAt("2026-10-03T09:00:00Z")
	m.Handle(domainevents.Event{EventType: domainevents.RunFinished, OrgID: ncOrg, ProjectID: ncProject,
		EntityID: "run-42", Actor: "agent:run-42",
		Payload: map[string]interface{}{"agent_id": "agent-1", "launched_by": "user-ben", "status": "completed"}})
}

func (ch *ncChannels) minutes(t *testing.T, used int) {
	org := &orgs.Org{ID: ncOrg, Name: ncOrgName,
		Limits: map[string]interface{}{orgs.LimitHostedRunnerMinutesMonth: 600}}
	m := NewMinutesMonitor(&ncOrgs{list: []*orgs.Org{org}}, ncMinutes{used: used}, ch.store, ch.bc).
		SetEmailDispatcher(ch.email).
		SetPushDispatcher(ch.push)
	m.now = ncAt("2026-10-03T09:00:00Z")
	m.Check(ncOrg)
}

// announce announces the release the notes make current, and returns how
// many accounts Announce says it notified.
func (ch *ncChannels) announce(t *testing.T, markdown string) int {
	a := NewReleaseAnnouncer(&ncClaims{}, ncChannelMembers{}, ch.store, ch.bc).
		SetEmailDispatcher(ch.email).
		SetPushDispatcher(ch.push)
	a.now = ncAt("2026-10-03T09:00:00Z")
	got := a.Announce(ncReleases(t, markdown).Current())
	if got == 0 {
		t.Errorf("the release announcement notified nobody")
	}
	return got
}

// stable runs the stable scheduler once at now over the workspaces given,
// with the steps listed already claimed.
func (ch *ncChannels) stable(t *testing.T, now string, list []*orgs.Org, taken ...string) {
	claims := &ncClaims{taken: map[string]bool{}}
	for _, k := range taken {
		claims.taken[k] = true
	}
	s := NewStableScheduler(ncReleases(t, ncStableNotes), &ncOrgs{list: list}, claims, ch.store, ch.bc).
		SetEmailDispatcher(ch.email).
		SetPushDispatcher(ch.push)
	s.now = ncAt(now)
	s.Run()
}

// dedicated runs the support-window watcher of an instance on stable
// 0.14.0 once at now, against a feed whose stable is 0.15.0, designated on
// 2026-09-01: support for 0.14.0 ends on 2026-11-30.
func (ch *ncChannels) dedicated(t *testing.T, now string) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.16.0","stable":"0.15.0","stable_since":"2026-09-01"}`))
	}))
	defer feed.Close()
	list := []*orgs.Org{{ID: ncOrg, Name: ncOrgName}, ncZurichOrg()}
	w := NewSupportWindowWatcher(feed.URL, ncReleases(t, ncDedicatedNotes), &ncOrgs{list: list}, &ncClaims{},
		ch.store, ch.bc).
		SetEmailDispatcher(ch.email).
		SetPushDispatcher(ch.push)
	w.now = ncAt(now)
	w.Run()
}

// ncLongComment is longer than the 160 characters a mention's body keeps,
// with characters outside ASCII where it is cut.
var ncLongComment = "@ben the brake pedal force table needs another pass: the ≤ 50 N limit applies to the " +
	"emergency brake only, the service brake row still says ≥ 65 N and the derived requirement " +
	"REQ-118 quotes both — can you reconcile them before Thursday's review?"

func ncScenarios() []ncScenario {
	return []ncScenario{
		// The bus notifier: project content.
		{"proposal.created by an agent run: the project's editors and owners", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ProposalCreated, "agent:run-42", "prop-7",
				map[string]interface{}{"op": "create_artifact", "run_id": "run-42"}))
		}},
		{"proposal.created with no run id in its payload", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ProposalCreated, "agent:run-43", "prop-8",
				map[string]interface{}{"op": "update_artifact"}))
		}},
		{"agentrun.finished, failed, launched by a member", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.RunFinished, "agent:run-42", "run-42",
				map[string]interface{}{"agent_id": "agent-1", "launched_by": "user-ben", "status": "failed"}))
		}},
		{"artifact.status_changed to in_review by its owner, titled with quotes", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ArtifactStatusChanged, "user:user-ada", "art-9",
				map[string]interface{}{"artifact_type": "requirement", "from": "draft", "to": "in_review",
					"title": `Brake "pedal" force ≤ 50 N`, "version": 3}))
		}},
		{"artifact.status_changed to in_review with no title, by the system", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ArtifactStatusChanged, "system", "art-10",
				map[string]interface{}{"from": "draft", "to": "in_review", "review_round": true}))
		}},
		{"chatter.created: an interview participant finished", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ChatterCreated, "system", "sess-5",
				map[string]interface{}{"kind": "interview-completed"}))
		}},
		{"chatter.created: a viewer's comment mentions @ben and @ada", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ChatterCreated, "user:user-cy", "chat-3",
				map[string]interface{}{"artifact_id": "art-9", "entry_type": "comment",
					"message": "@ben please check @ada's note on the pedal force"}))
		}},
		{"chatter.created: a long comment mentions @ben", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncEvent(domainevents.ChatterCreated, "user:user-ada", "chat-4",
				map[string]interface{}{"artifact_id": "art-9", "entry_type": "comment", "message": ncLongComment}))
		}},

		// The bus notifier: membership.
		{"org.member_added: an admin adds a member", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncOrgEvent(domainevents.OrgMemberAdded, "user:user-ada",
				map[string]interface{}{"user_id": "user-ben", "role": "member"}))
		}},
		{"org.invitation_sent: an admin invites an address as an admin", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncOrgEvent(domainevents.OrgInvitationSent, "user:user-ada",
				map[string]interface{}{"email": "frank@example.test", "role": "admin"}))
		}},
		{"org.invitation_accepted: someone with no display name joins", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncOrgEvent(domainevents.OrgInvitationAccepted, "user:user-eve",
				map[string]interface{}{"user_id": "user-eve", "role": "member"}))
		}},
		{"org.member_role_changed: a member is made an admin", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncOrgEvent(domainevents.OrgMemberRoleChanged, "user:user-ada",
				map[string]interface{}{"user_id": "user-ben", "from": "member", "to": "admin"}))
		}},
		{"org.member_removed by an admin", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncOrgEvent(domainevents.OrgMemberRemoved, "user:user-ada",
				map[string]interface{}{"user_id": "user-ben", "self": false}))
		}},
		{"org.member_removed: a member leaves", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncOrgEvent(domainevents.OrgMemberRemoved, "user:user-ben",
				map[string]interface{}{"user_id": "user-ben", "self": true}))
		}},
		{"project.member_added as an editor", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncProjectEvent(domainevents.ProjectMemberAdded, "user:user-ada",
				map[string]interface{}{"user_id": "user-ben", "role": "editor"}))
		}},
		{"project.member_role_changed from editor to viewer", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncProjectEvent(domainevents.ProjectMemberRoleChanged, "user:user-ada",
				map[string]interface{}{"user_id": "user-ben", "from": "editor", "to": "viewer"}))
		}},
		{"project.member_removed by an owner", func(t *testing.T, ch *ncChannels) {
			ch.handle(ncProjectEvent(domainevents.ProjectMemberRemoved, "user:user-ada",
				map[string]interface{}{"user_id": "user-ben", "self": false}))
		}},

		// The budget and hosted-minutes monitors.
		{"agentrun.finished takes the month's spend past 80% of the budget", func(t *testing.T, ch *ncChannels) {
			ch.budget(t, 212.5)
		}},
		{"agentrun.finished takes the month's spend past the budget", func(t *testing.T, ch *ncChannels) {
			ch.budget(t, 263.456)
		}},
		{"a lease takes the month's cloud runner minutes past 80% of the allowance", func(t *testing.T, ch *ncChannels) {
			ch.minutes(t, 510)
		}},
		{"a lease uses up the month's cloud runner minutes", func(t *testing.T, ch *ncChannels) {
			ch.minutes(t, 600)
		}},

		// The release announcer (nightly channel).
		{"a release with more notes than the announcement shows", func(t *testing.T, ch *ncChannels) {
			ch.announce(t, ncNightlyNotes)
		}},
		{"a release from before version numbers", func(t *testing.T, ch *ncChannels) {
			ch.announce(t, "## 2026-01-15\n\n- Interview links expire after 30 days.\n- The requirements table remembers its sort.\n")
		}},
		{"a release with no notes", func(t *testing.T, ch *ncChannels) {
			ch.announce(t, "## 0.16.1 — 2026-10-02\n")
		}},

		// The stable scheduler (stable channel).
		{"stable cut, the workspace's window eight days away", func(t *testing.T, ch *ncChannels) {
			ch.stable(t, "2026-09-02T12:00:00Z", []*orgs.Org{ncZurichOrg()})
		}},
		{"stable cut announced, the window opens tomorrow", func(t *testing.T, ch *ncChannels) {
			ch.stable(t, "2026-09-09T08:00:00Z", []*orgs.Org{ncZurichOrg()}, ncZurich+"/0.15.0/announced")
		}},
		{"first run inside the reminder's day: announced and reminded at once", func(t *testing.T, ch *ncChannels) {
			ch.stable(t, "2026-09-09T08:00:00Z", []*orgs.Org{ncZurichOrg()})
		}},
		{"the workspace's window opens", func(t *testing.T, ch *ncChannels) {
			ch.stable(t, "2026-09-10T07:30:00Z", []*orgs.Org{ncZurichOrg()},
				ncZurich+"/0.15.0/announced", ncZurich+"/0.15.0/reminded")
		}},
		{"first run after the window has passed: announced and turned on at once", func(t *testing.T, ch *ncChannels) {
			ch.stable(t, "2026-09-20T10:00:00Z", []*orgs.Org{ncZurichOrg()})
		}},
		{"stable cut for a workspace with no upgrade window", func(t *testing.T, ch *ncChannels) {
			ch.stable(t, "2026-09-01T10:00:00Z",
				[]*orgs.Org{{ID: ncOrg, Name: ncOrgName, StableRelease: "0.14.0"}})
		}},

		// The dedicated instance's support window.
		{"support for the running stable ends in 24 days", func(t *testing.T, ch *ncChannels) {
			ch.dedicated(t, "2026-11-05T12:00:00Z")
		}},
		{"support for the running stable ends in 6 days", func(t *testing.T, ch *ncChannels) {
			ch.dedicated(t, "2026-11-23T12:00:00Z")
		}},
		{"support for the running stable ends tomorrow", func(t *testing.T, ch *ncChannels) {
			ch.dedicated(t, "2026-11-28T12:00:00Z")
		}},
		{"support for the running stable ends today", func(t *testing.T, ch *ncChannels) {
			ch.dedicated(t, "2026-11-29T18:00:00Z")
		}},
		{"support for the running stable has ended", func(t *testing.T, ch *ncChannels) {
			ch.dedicated(t, "2026-12-05T00:00:00Z")
		}},
	}
}

// --- The test -------------------------------------------------------------

// ncEnvKeys are the allow-list overrides, email then push.
var ncEnvKeys = []string{"OPENV_EMAIL_NOTIFICATION_TYPES", "OPENV_PUSH_NOTIFICATION_TYPES"}

// ncRunAll runs every scenario under one wiring of the side channels.
func ncRunAll(t *testing.T, scenarios []ncScenario) map[string]ncRun {
	t.Helper()
	ch := newNCChannels(t)
	runs := map[string]ncRun{}
	for _, s := range scenarios {
		ch.rec.reset()
		start := time.Now()
		s.drive(t, ch)
		deliveries, strays := ch.rec.take()
		end := time.Now()
		if len(strays) > 0 {
			t.Errorf("scenario %q: sent before any row was stored: %s", s.name, strings.Join(strays, "; "))
		}
		if len(deliveries) == 0 {
			t.Errorf("scenario %q stored no notification", s.name)
		}
		runs[s.name] = ncRun{deliveries: deliveries, start: start, end: end}
	}
	return runs
}

// ncEntry is one row of one type, as both runs delivered it.
type ncEntry struct {
	scenario       string
	seq            string // "k of n": the row's place among everything the scenario stored
	def, ovr       *ncDelivery
	defRun, ovrRun ncRun
}

func TestNotificationContent(t *testing.T) {
	// The components log each send; a failure here is about the goldens.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	constants := ncTypeConstants(t)
	known := map[string]string{}
	for _, c := range constants {
		known[c.value] = c.name
	}
	scenarios := ncScenarios()

	// The defaults: both overrides unset, as on a deployment that sets
	// neither.
	for _, k := range ncEnvKeys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	defaultEmail, defaultPush := EmailTypesFromEnv(), PushTypesFromEnv()
	defRuns := ncRunAll(t, scenarios)

	// The override: each list set to exactly the types its default leaves
	// out.
	for i, defaults := range [][]string{defaultEmail, defaultPush} {
		var others []string
		for _, c := range constants {
			if !ncContains(defaults, c.value) {
				others = append(others, c.value)
			}
		}
		if len(others) == 0 {
			t.Fatalf("every type is on the default %s list; the override run would pin nothing", ncEnvKeys[i])
		}
		t.Setenv(ncEnvKeys[i], strings.Join(others, ","))
	}
	ovrRuns := ncRunAll(t, scenarios)

	// Group every row by its type, in scenario order.
	byType := map[string][]ncEntry{}
	for _, s := range scenarios {
		def, ovr := defRuns[s.name], ovrRuns[s.name]
		if len(def.deliveries) != len(ovr.deliveries) {
			t.Errorf("scenario %q stored %d rows with the default allow-lists and %d with the overrides",
				s.name, len(def.deliveries), len(ovr.deliveries))
			continue
		}
		for i, d := range def.deliveries {
			byType[d.row.Type] = append(byType[d.row.Type], ncEntry{scenario: s.name,
				seq: fmt.Sprintf("%d of %d", i+1, len(def.deliveries)), def: d, ovr: ovr.deliveries[i],
				defRun: def, ovrRun: ovr})
		}
	}

	// Completeness: every type constant is delivered by some scenario, and
	// nothing is delivered under a type that is not a constant.
	for _, c := range constants {
		if len(byType[c.value]) == 0 {
			t.Errorf("no scenario delivers notifications.%s (%q): add one to ncScenarios in "+
				"internal/notify/notification_content_test.go, then create its goldens with:\n  %s",
				c.name, c.value, ncRegenerate())
		}
	}
	var delivered []string
	for typ := range byType {
		delivered = append(delivered, typ)
	}
	sort.Strings(delivered)
	for _, typ := range delivered {
		if _, ok := known[typ]; !ok {
			t.Errorf("type %q is delivered but is not a Type* constant of internal/domain/notifications", typ)
		}
	}
	ncCheckNoStrayGoldens(t, known)

	for _, c := range constants {
		entries := byType[c.value]
		if len(entries) == 0 {
			continue
		}
		t.Run(c.value, func(t *testing.T) {
			ncCheckType(t, c, entries, ncContains(defaultEmail, c.value), ncContains(defaultPush, c.value))
		})
	}
}

// ncCheckType renders and compares one type's four goldens.
func ncCheckType(t *testing.T, c ncTypeConstant, entries []ncEntry, emailDefault, pushDefault bool) {
	dir := filepath.Join(ncGoldenDir, c.value)
	header := func(what string) string {
		return fmt.Sprintf("# %s for notification type %s (notifications.%s): refactor plan step S10.\n"+
			"# Regenerate (a deliberate, release-noted behavior change only):\n#   %s\n",
			what, c.value, c.name, ncRegenerateCommand)
	}
	onOff := func(sent bool) string {
		if sent {
			return "sent"
		}
		return "not sent"
	}

	var rows []interface{}
	var sse, email strings.Builder
	var pushes []interface{}
	sse.WriteString(header("SSE frames"))
	sse.WriteString("# Each frame as the hub writes it on the recipient's stream; the row's id and\n" +
		"# created_at are normalised.\n")
	email.WriteString(header("Email"))
	fmt.Fprintf(&email, "# Emailed by default (%s unset): %s. The override run sets it to exactly the\n"+
		"# types the defaults leave out. Each message is the SMTP hand-off of the real mailer;\n"+
		"# every line ends CRLF on the wire (a bare LF would show as ␊, a bare CR as ␍).\n",
		ncEnvKeys[0], ncYesNo(emailDefault))
	sawEmail, sawPush := false, false

	for _, e := range entries {
		// The order of a row's channels, which the goldens do not show, and
		// that each is addressed to the row's recipient.
		for _, run := range []struct {
			label string
			d     *ncDelivery
		}{{"default run", e.def}, {"override run", e.ovr}} {
			if p := ncOrderProblem(run.d); p != "" {
				t.Errorf("%s (%s), %s: %s", e.scenario, e.seq, run.label, p)
			}
			for _, p := range ncAddressProblems(run.d) {
				t.Errorf("%s (%s), %s: %s", e.scenario, e.seq, run.label, p)
			}
		}

		view, problems := ncRowView(e.def, e.defRun)
		for _, p := range problems {
			t.Errorf("%s (%s): %s", e.scenario, e.seq, p)
		}
		ovrView, ovrProblems := ncRowView(e.ovr, e.ovrRun)
		for _, p := range ovrProblems {
			t.Errorf("%s (%s), override run: %s", e.scenario, e.seq, p)
		}
		if a, b := string(ncJSON(t, view)), string(ncJSON(t, ovrView)); a != b {
			t.Errorf("%s (%s): the stored row differs between the default and the override run:\n%s",
				e.scenario, e.seq, ncDiff(a, b))
		}
		rows = append(rows, map[string]interface{}{"scenario": e.scenario, "delivery": e.seq, "row": view})

		// SSE: the same in both runs, since no allow-list governs it.
		defFrames, ovrFrames := ncFrames(e.def), ncFrames(e.ovr)
		if defFrames != ovrFrames {
			t.Errorf("%s (%s): the SSE frames differ between the default and the override run:\n%s",
				e.scenario, e.seq, ncDiff(defFrames, ovrFrames))
		}
		fmt.Fprintf(&sse, "\n=== %s | delivery %s | to %s\n", e.scenario, e.seq, e.def.row.UserID)
		if defFrames == "" {
			sse.WriteString("(no frame)\n")
		}
		sse.WriteString(defFrames)

		// Email: whichever run sent it; a run that did not shows none.
		defMail, ovrMail := ncEmails(e.def), ncEmails(e.ovr)
		mail := defMail
		if mail == "" {
			mail = ovrMail
		} else if ovrMail != "" && ovrMail != defMail {
			t.Errorf("%s (%s): the email differs between the default and the override run:\n%s",
				e.scenario, e.seq, ncDiff(defMail, ovrMail))
		}
		sawEmail = sawEmail || mail != ""
		fmt.Fprintf(&email, "\n======== %s | delivery %s | to %s\n", e.scenario, e.seq, e.def.row.UserID)
		fmt.Fprintf(&email, "with the default allow-list: %s; with the override: %s\n", onOff(defMail != ""), onOff(ovrMail != ""))
		if mail == "" {
			email.WriteString("(no email)\n")
		}
		email.WriteString(mail)

		// Push: likewise.
		defSends, ovrSends := ncSends(e.def), ncSends(e.ovr)
		sends := defSends
		if len(sends) == 0 {
			sends = ovrSends
		} else if len(ovrSends) > 0 && string(ncJSON(t, defSends)) != string(ncJSON(t, ovrSends)) {
			t.Errorf("%s (%s): the push payload differs between the default and the override run", e.scenario, e.seq)
		}
		for _, s := range sends {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(s["payload"]), &parsed); err != nil {
				t.Errorf("%s (%s): push payload is not JSON: %v", e.scenario, e.seq, err)
			}
		}
		sawPush = sawPush || len(sends) > 0
		pushes = append(pushes, map[string]interface{}{
			"scenario": e.scenario, "delivery": e.seq, "user": e.def.row.UserID,
			"sent with the default allow-list": len(defSends) > 0, "sent with the override": len(ovrSends) > 0,
			"sends": sends})
	}
	if !sawEmail {
		t.Errorf("no delivery of %s sent an email in either run, so its email content is not pinned", c.value)
	}
	if !sawPush {
		t.Errorf("no delivery of %s sent a push in either run, so its push payload is not pinned", c.value)
	}

	ncCheckGolden(t, filepath.Join(dir, "row.json"), ncJSON(t, map[string]interface{}{
		"about": "The row each delivery hands to the notifications store, in the order the scenario stored them " +
			"(refactor plan step S10). The id and created_at are generated by notifications.New and normalised.",
		"regenerate": ncRegenerateCommand, "type": c.value, "constant": c.name, "deliveries": rows}))
	ncCheckGolden(t, filepath.Join(dir, "sse.txt"), []byte(sse.String()))
	ncCheckGolden(t, filepath.Join(dir, "email.txt"), []byte(email.String()))
	ncCheckGolden(t, filepath.Join(dir, "push.json"), ncJSON(t, map[string]interface{}{
		"about": "The web push payload each subscribed device is sent, before encryption (refactor plan step S10). " +
			"The override run sets " + ncEnvKeys[1] + " to exactly the types the default allow-list leaves out.",
		"regenerate": ncRegenerateCommand, "type": c.value, "constant": c.name, "pushed by default": pushDefault,
		"deliveries": pushes}))
}

// ncFrames renders a delivery's SSE frames as the hub writes them.
func ncFrames(d *ncDelivery) string {
	var b strings.Builder
	for _, f := range d.frames {
		fmt.Fprintf(&b, "(stream %s)\nevent: %s\ndata: %s\n\n", f.key, f.event, ncNormalise(f.data, d.row))
	}
	return b.String()
}

// ncEmails renders a delivery's emails: the envelope, then the message.
func ncEmails(d *ncDelivery) string {
	var b strings.Builder
	for _, e := range d.emails {
		auth := "no auth"
		if e.auth {
			auth = "PLAIN auth"
		}
		fmt.Fprintf(&b, "envelope: %s, MAIL FROM %s, RCPT TO %s, %s\n", e.addr, e.from, strings.Join(e.to, ", "), auth)
		b.WriteString("-------- message\n")
		b.WriteString(ncWire(e.msg))
		b.WriteString("\n-------- end\n")
	}
	return b.String()
}

// ncSends lists a delivery's pushes by device, each payload as sent.
func ncSends(d *ncDelivery) []map[string]string {
	out := []map[string]string{}
	for _, p := range d.pushes {
		out = append(out, map[string]string{"endpoint": p.endpoint, "payload": string(p.payload)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["endpoint"] < out[j]["endpoint"] })
	return out
}

// ncCheckNoStrayGoldens fails on a golden directory, or a file in one, that
// no type constant owns: a renamed or removed type leaves its goldens
// behind otherwise.
func ncCheckNoStrayGoldens(t *testing.T, known map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(ncGoldenDir)
	if err != nil {
		if os.IsNotExist(err) && ncUpdating() {
			return
		}
		t.Fatalf("read %s: %v", ncGoldenDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			if name != ".gitattributes" {
				t.Errorf("internal/notify/%s/%s: only per-type directories and .gitattributes belong here", ncGoldenDir, name)
			}
			continue
		}
		if _, ok := known[name]; !ok {
			t.Errorf("internal/notify/%s/%s/ belongs to no Type* constant of internal/domain/notifications: "+
				"remove it with the type it pinned", ncGoldenDir, name)
			continue
		}
		files, err := os.ReadDir(filepath.Join(ncGoldenDir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if !ncContains(ncGoldenFiles, f.Name()) {
				t.Errorf("internal/notify/%s/%s/%s is not one of the goldens %v", ncGoldenDir, name, f.Name(), ncGoldenFiles)
			}
		}
	}
}

// --- A refused row ---------------------------------------------------------

// ncRefusal is one of the seven copies X6 rewrites, driven with the store
// refusing its first recipient's row.
type ncRefusal struct {
	site   string // the copy: its file and function
	refuse string // the recipient whose row the store refuses
	drive  func(t *testing.T, ch *ncChannels)
	stored []string // the recipients whose rows are stored, in order
	logs   []string // every line logged at Info and above while it runs
}

// TestNotificationStoreFailure is the other half of S10's characterization
// of the seven copies of store -> SSE -> email -> push: what each does when
// the store refuses a row. Each copy is driven once, with the store refusing
// the first recipient's row, and must (a) send that recipient nothing (no SSE
// frame, email or push), (b) still store and send the next recipient's (so
// the copy goes on to the next recipient rather than giving up), and (c) log
// exactly the lines it logs today, its own message for the failure (the
// support-window watcher logs none) and the count of rows stored where it
// logs one; Announce must also return the count of rows stored. X6 keeps
// "each site's log message and success counting" against this test.
func TestNotificationStoreFailure(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Every type emailed and pushed, so every stored row shows all three
	// channels.
	var all []string
	for _, c := range ncTypeConstants(t) {
		all = append(all, c.value)
	}
	for _, k := range ncEnvKeys {
		t.Setenv(k, strings.Join(all, ","))
	}
	logs := &ncLogs{}
	slog.SetDefault(slog.New(logs))
	ch := newNCChannels(t)

	cases := []ncRefusal{
		{site: "notifier.go deliver", refuse: "user-ada",
			drive: func(t *testing.T, ch *ncChannels) {
				ch.handle(ncEvent(domainevents.ProposalCreated, "agent:run-42", "prop-7",
					map[string]interface{}{"op": "create_artifact", "run_id": "run-42"}))
			},
			stored: []string{"user-ben"},
			logs: []string{
				"ERROR notify: failed to store notification event_type=proposal.created user_id=user-ada error=refused",
			}},
		{site: "budgets.go alertAdmins", refuse: "user-ada",
			drive:  func(t *testing.T, ch *ncChannels) { ch.budget(t, 263.456) },
			stored: []string{"user-dee"},
			logs: []string{
				"ERROR budget: failed to store notification org_id=org-acme user_id=user-ada error=refused",
			}},
		{site: "minutes.go alertAdmins", refuse: "user-ada",
			drive:  func(t *testing.T, ch *ncChannels) { ch.minutes(t, 600) },
			stored: []string{"user-dee"},
			logs: []string{
				"ERROR minutes: failed to store notification org_id=org-acme user_id=user-ada error=refused",
			}},
		{site: "release.go Announce", refuse: "user-ada",
			drive: func(t *testing.T, ch *ncChannels) {
				if got := ch.announce(t, ncNightlyNotes); got != 1 {
					t.Errorf("Announce returned %d with one of two rows stored; it returns the rows stored, 1", got)
				}
			},
			stored: []string{"user-ben"},
			logs: []string{
				"ERROR release: failed to store notification user_id=user-ada error=refused",
				"INFO release: announced version=0.16.0 accounts=1",
			}},
		{site: "stable.go turnOn", refuse: "user-gus",
			drive: func(t *testing.T, ch *ncChannels) {
				ch.stable(t, "2026-09-10T07:30:00Z", []*orgs.Org{ncZurichOrg()},
					ncZurich+"/0.15.0/announced", ncZurich+"/0.15.0/reminded")
			},
			stored: []string{"user-hal"},
			logs: []string{
				"ERROR release: failed to store notification user_id=user-gus error=refused",
				"INFO release: stable turned on org_id=org-zurich version=0.15.0 members=1",
			}},
		{site: "stable.go notifyAdmins", refuse: "user-ada",
			drive: func(t *testing.T, ch *ncChannels) {
				// The announcement only: the window is eight days away.
				ch.stable(t, "2026-09-02T12:00:00Z", []*orgs.Org{{ID: ncOrg, Name: ncOrgName, StableRelease: "0.14.0",
					UpgradeDay: 10, UpgradeHour: 9, UpgradeTimezone: "Europe/Zurich"}})
			},
			stored: []string{"user-dee"},
			logs: []string{
				"ERROR release: failed to store notification user_id=user-ada error=refused",
			}},
		{site: "dedicated.go warn", refuse: "user-ada",
			drive:  func(t *testing.T, ch *ncChannels) { ch.dedicated(t, "2026-11-23T12:00:00Z") },
			stored: []string{"user-dee", "user-gus"},
			logs: []string{
				"WARN release: support window warning sent running=0.14.0 available=0.15.0 closes=2026-11-30",
			}},
	}
	for _, c := range cases {
		t.Run(c.site, func(t *testing.T) {
			ch.rec.reset()
			logs.take()
			ch.store.refuse = map[string]bool{c.refuse: true}
			defer func() { ch.store.refuse = nil }()

			c.drive(t, ch)
			deliveries, strays := ch.rec.take()
			refused := ch.rec.refusals()
			lines := logs.take()

			if want := []string{c.refuse}; !reflect.DeepEqual(refused, want) {
				t.Errorf("the store was asked for the refused rows of %v, want %v", refused, want)
			}
			// (a) The refused recipient comes first, so anything sent for
			// them is sent before any row is stored.
			if len(strays) > 0 {
				t.Errorf("sent with no row stored (to the refused %s): %s", c.refuse, strings.Join(strays, "; "))
			}
			// (b) The later recipients' rows, each with its three channels.
			var stored []string
			for _, d := range deliveries {
				stored = append(stored, d.row.UserID)
				if len(d.frames) != 1 || len(d.emails) != 1 || len(d.pushes) != 1 {
					t.Errorf("the row for %s was followed by %d SSE frames, %d emails and %d pushes, want one of each",
						d.row.UserID, len(d.frames), len(d.emails), len(d.pushes))
				}
				for _, p := range ncAddressProblems(d) {
					t.Error(p)
				}
				if p := ncOrderProblem(d); p != "" {
					t.Error(p)
				}
			}
			if !reflect.DeepEqual(stored, c.stored) {
				t.Errorf("rows stored for %v, want %v: a refused row ends only its own recipient's delivery", stored, c.stored)
			}
			// (c) What the copy logs, and the count it logs.
			if !reflect.DeepEqual(lines, c.logs) {
				t.Errorf("logged:\n  %s\nwant:\n  %s", strings.Join(lines, "\n  "), strings.Join(c.logs, "\n  "))
			}
		})
	}
}

func ncContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func ncYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
