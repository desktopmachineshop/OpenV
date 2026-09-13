package notify

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

type fakeWindowOrgs struct{ members map[string][]*orgs.Member }

func (f fakeWindowOrgs) ListAll() ([]string, error) {
	ids := []string{}
	for id := range f.members {
		ids = append(ids, id)
	}
	return ids, nil
}
func (f fakeWindowOrgs) ListMembers(orgID string) ([]*orgs.Member, error) {
	return f.members[orgID], nil
}

func watcherFixture(t *testing.T, feed string, own *release.Stable) (*SupportWindowWatcher, *captureStore, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(feed))
	}))
	t.Cleanup(srv.Close)
	store := &captureStore{}
	o := fakeWindowOrgs{members: map[string][]*orgs.Member{
		"o1": {{OrgID: "o1", UserID: "a1", Role: orgs.RoleAdmin}, {OrgID: "o1", UserID: "m1", Role: orgs.RoleMember}},
	}}
	w := NewSupportWindowWatcher(srv.URL, fakeReleases{stable: own}, o, &fakeClaimer{claimed: map[string]bool{}}, store, nil)
	return w, store, srv
}

// TestSupportWindowWarnsAt30And7DaysAndOnClose: with 2026.10 cut on
// 2026-10-01 the window closes on 2026-12-30; an instance on 2026.09 is
// warned once inside 30 days, once inside 7, and once after the close, and
// only its admins are.
func TestSupportWindowWarnsAt30And7DaysAndOnClose(t *testing.T) {
	feed := `{"nightly":"2026-10-15","stable":"2026.10","stable_cut_on":"2026-10-01"}`
	w, store, _ := watcherFixture(t, feed, &release.Stable{Version: "2026.09", CutOn: "2026-09-01", CutFrom: "2026-08-25"})

	w.now = func() time.Time { return time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC) }
	w.Run()
	if len(store.rows) != 0 {
		t.Fatalf("warned with more than 30 days left")
	}
	w.now = func() time.Time { return time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC) }
	w.Run()
	w.Run()
	if len(store.rows) != 1 || store.rows[0].UserID != "a1" || store.rows[0].Type != notifications.TypeReleaseSupportWindow {
		t.Fatalf("30-day warning rows = %+v", store.rows)
	}
	w.now = func() time.Time { return time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC) }
	w.Run()
	if len(store.rows) != 2 {
		t.Fatalf("7-day warning rows = %d", len(store.rows))
	}
	w.now = func() time.Time { return time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC) }
	w.Run()
	w.Run()
	if len(store.rows) != 3 || store.rows[2].Title != "OpenV support window has closed" {
		t.Fatalf("closed rows = %+v", store.rows)
	}
}

// TestSupportWindowQuietWhenCurrent: an instance on the feed's stable, or
// with no stable at all, is never warned.
func TestSupportWindowQuietWhenCurrent(t *testing.T) {
	feed := `{"nightly":"2026-10-15","stable":"2026.10","stable_cut_on":"2026-10-01"}`
	w, store, _ := watcherFixture(t, feed, &release.Stable{Version: "2026.10", CutOn: "2026-10-01", CutFrom: "2026-09-24"})
	w.now = func() time.Time { return time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC) }
	w.Run()
	if len(store.rows) != 0 {
		t.Fatalf("warned an instance on the current stable")
	}
	w, store, _ = watcherFixture(t, feed, nil)
	w.Run()
	if len(store.rows) != 0 {
		t.Fatalf("warned an instance with no stable")
	}
}
