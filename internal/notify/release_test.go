package notify

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

type fakeClaimer struct {
	claimed map[string]bool
	err     error
}

func (f *fakeClaimer) ClaimReleaseAnnouncement(version string, _ time.Time) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.claimed[version] {
		return false, nil
	}
	f.claimed[version] = true
	return true, nil
}

type fakeUserLister struct{ list []*users.User }

func (f fakeUserLister) ListUsers() ([]*users.User, error) { return f.list, nil }

type captureStore struct {
	notifications.Service
	rows []*notifications.Notification
}

func (c *captureStore) Create(n *notifications.Notification) error {
	c.rows = append(c.rows, n)
	return nil
}

type captureBroadcaster struct{ keys []string }

func (c *captureBroadcaster) BroadcastSession(key, _ string, _ interface{}) {
	c.keys = append(c.keys, key)
}

func announcerFixture() (*ReleaseAnnouncer, *fakeClaimer, *captureStore, *captureBroadcaster) {
	claims := &fakeClaimer{claimed: map[string]bool{}}
	store := &captureStore{}
	bc := &captureBroadcaster{}
	lister := fakeUserLister{list: []*users.User{{ID: "u1"}, {ID: "u2"}}}
	return NewReleaseAnnouncer(claims, lister, store, bc), claims, store, bc
}

// TestAnnounceNotifiesEveryAccountOnce: every account gets one row for the
// release, pushed live; a second announcement of the same version, as a
// restart or another replica would make, does nothing.
func TestAnnounceNotifiesEveryAccountOnce(t *testing.T) {
	a, _, store, bc := announcerFixture()
	rel := &release.Release{Version: "2026-09-13", Date: "2026-09-13", Notes: []string{"Alpha", "Beta"}}
	if got := a.Announce(rel); got != 2 {
		t.Fatalf("notified = %d, want 2", got)
	}
	if len(store.rows) != 2 || len(bc.keys) != 2 {
		t.Fatalf("rows = %d, broadcasts = %d", len(store.rows), len(bc.keys))
	}
	row := store.rows[0]
	if row.Type != notifications.TypeReleasePublished || row.OrgID != "" {
		t.Fatalf("row = %+v", row)
	}
	if row.EntityRef["kind"] != "release" || row.EntityRef["version"] != "2026-09-13" {
		t.Fatalf("entity_ref = %v", row.EntityRef)
	}
	if !strings.Contains(row.Title, "2026-09-13") || !strings.Contains(row.Body, "• Alpha\n• Beta") {
		t.Fatalf("copy = %q / %q", row.Title, row.Body)
	}
	if bc.keys[0] != StreamKey("u1") {
		t.Fatalf("broadcast key = %q", bc.keys[0])
	}
	if got := a.Announce(rel); got != 0 || len(store.rows) != 2 {
		t.Fatalf("second announce notified %d (rows %d), want 0", got, len(store.rows))
	}
}

// TestAnnounceSkipsNilAndFailedClaim: a build with no release, and a claim
// that errors, both announce nothing rather than half of something.
func TestAnnounceSkipsNilAndFailedClaim(t *testing.T) {
	a, claims, store, _ := announcerFixture()
	if got := a.Announce(nil); got != 0 {
		t.Fatalf("nil release notified %d", got)
	}
	claims.err = errors.New("db down")
	if got := a.Announce(&release.Release{Version: "2026-09-13"}); got != 0 || len(store.rows) != 0 {
		t.Fatalf("failed claim notified %d", got)
	}
}

// TestReleaseMessageCapsBullets: the body carries the first five bullets and
// points at the rest; an empty release still says where to look.
func TestReleaseMessageCapsBullets(t *testing.T) {
	notes := []string{"a", "b", "c", "d", "e", "f", "g"}
	_, body := ReleaseMessage(&release.Release{Version: "2026-09-13", Notes: notes})
	if strings.Count(body, "• ") != 5 || !strings.Contains(body, "…and more") {
		t.Fatalf("body = %q", body)
	}
	_, body = ReleaseMessage(&release.Release{Version: "2026-09-13"})
	if !strings.Contains(body, "What's new") {
		t.Fatalf("empty body = %q", body)
	}
}
