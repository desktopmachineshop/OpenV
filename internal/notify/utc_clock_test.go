package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// utcStamps records the time each claim and each push mark is handed: the
// release announcement and support-window claims, the stable steps, and a
// device's last_used_at and failed_at, all TIMESTAMP columns holding UTC
// wall clocks.
type utcStamps struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (s *utcStamps) record(what string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at[what] = at
}

func (s *utcStamps) get(what string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.at[what]
	return at, ok
}

func (s *utcStamps) ClaimReleaseAnnouncement(version string, at time.Time) (bool, error) {
	s.record("the announcement claim of "+version, at)
	return true, nil
}

func (s *utcStamps) ClaimStableStep(_, _, step string, at time.Time) (bool, error) {
	s.record("the stable step "+step, at)
	return true, nil
}

func (s *utcStamps) ListForUser(userID string) ([]*pushsubs.Subscription, error) {
	return []*pushsubs.Subscription{
		{ID: "alive", UserID: userID, Endpoint: "https://push.example.com/alive"},
		{ID: "failing", UserID: userID, Endpoint: "https://push.example.com/failing"},
	}, nil
}

func (s *utcStamps) Forget(string) (int64, error) { return 0, nil }

func (s *utcStamps) MarkUsed(id string, at time.Time) error {
	s.record("last_used_at of the device "+id, at)
	return nil
}

func (s *utcStamps) MarkFailed(id string, at time.Time) error {
	s.record("failed_at of the device "+id, at)
	return nil
}

// utcOrgs is one stable-channel workspace with no members.
type utcOrgs struct{}

func (utcOrgs) ListOrgsByChannel(string) ([]*orgs.Org, error) {
	return []*orgs.Org{{ID: "org-1", Name: "Machine shop", StableRelease: "0.1.0"}}, nil
}
func (utcOrgs) ListMembers(string) ([]*orgs.Member, error) { return nil, nil }
func (utcOrgs) SetStableRelease(id, version string) (*orgs.Org, error) {
	return &orgs.Org{ID: id, StableRelease: version}, nil
}
func (utcOrgs) ListAll() ([]string, error)                          { return []string{"org-1"}, nil }
func (utcOrgs) ListMemberUserIDsByChannel(string) ([]string, error) { return nil, nil }

// utcReleases runs stable as its stable release.
type utcReleases struct{ stable *release.Stable }

func (utcReleases) Current() *release.Release             { return nil }
func (utcReleases) Released() []release.Release           { return nil }
func (r utcReleases) CurrentStable() *release.Stable      { return r.stable }
func (utcReleases) Stable(version string) *release.Stable { return nil }

// utcSender delivers to the device "alive" and is refused for any other.
type utcSender struct{}

func (utcSender) Send(sub *pushsubs.Subscription, _ []byte) (int, error) {
	if sub.ID == "alive" {
		return http.StatusCreated, nil
	}
	return http.StatusInternalServerError, nil
}

// utcUsers opts every account into push.
type utcUsers struct{}

func (utcUsers) GetByID(id string) (*users.User, error) {
	return &users.User{ID: id, PushNotifications: true}, nil
}

// The release announcer, the stable scheduler, the support window watcher
// and the push dispatcher hand the database their clock's time in UTC (#379
// bug 162). Each stamps a TIMESTAMP column (release_announcements'
// announced_at, release_schedule's announced_at and turned_on_at,
// push_subscriptions' last_used_at and failed_at), which keeps the wall
// clock it is sent and is read back as UTC, and each clock is time.Now,
// which reads in the server's zone. Each runs here on a clock that reads as
// time.Now() does on a server two hours east of UTC and then five hours
// west: every time it hands a claim or a mark is that instant, in UTC.
func TestTheReleaseClocksAndPushMarksAreUTC(t *testing.T) {
	instant := time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC)
	for _, zone := range []*time.Location{time.FixedZone("CEST", 2*60*60), time.FixedZone("EST", -5*60*60)} {
		t.Run(zone.String(), func(t *testing.T) {
			local := func() time.Time { return instant.In(zone) }
			stamps := &utcStamps{at: map[string]time.Time{}}

			announcer := NewReleaseAnnouncer(stamps, utcOrgs{}, nil, nil)
			announcer.now = local
			announcer.Announce(&release.Release{Version: "9.9.0", Notes: []string{"Faster exports"}})

			// Cut a month ago, with no upgrade window: announced and turned
			// on in one run.
			scheduler := NewStableScheduler(utcReleases{stable: &release.Stable{Version: "9.9.0", Since: "2026-09-01"}},
				utcOrgs{}, stamps, nil, nil)
			scheduler.now = local
			scheduler.Run()

			// The next stable was cut long enough ago that this instance's
			// support window has closed.
			feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(ReleaseFeed{Version: "9.9.0", Stable: "9.9.0", StableSince: "2026-01-01"})
			}))
			defer feed.Close()
			watcher := NewSupportWindowWatcher(feed.URL, utcReleases{stable: &release.Stable{Version: "9.8.0", Since: "2025-10-01"}},
				utcOrgs{}, stamps, nil, nil)
			watcher.now = local
			watcher.Run()

			push := NewPushDispatcher(utcSender{}, stamps, utcUsers{}, DefaultPushTypes())
			push.now = local
			push.Dispatch(notifications.New("", "user-1", DefaultPushTypes()[0], "Run failed", "The nightly run failed.", nil))
			push.Wait()

			for _, what := range []string{
				"the announcement claim of 9.9.0",
				"the stable step announced",
				"the stable step turned_on",
				"the announcement claim of support-window:9.9.0:closed",
				"last_used_at of the device alive",
				"failed_at of the device failing",
			} {
				at, ok := stamps.get(what)
				switch {
				case !ok:
					t.Errorf("%s: never stamped", what)
				case !at.Equal(instant) || at.Location() != time.UTC:
					t.Errorf("%s was handed %s, want %s: it is stored in a TIMESTAMP, which keeps the wall clock it is sent",
						what, at.Format(time.RFC3339), instant.Format(time.RFC3339))
				}
			}
		})
	}
}
