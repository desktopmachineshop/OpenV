package notify

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

type fakeWindowOrgs struct {
	members map[string][]*orgs.Member
	// order is what ListAll answers; when it is empty, ListAll answers the
	// keys of members in no particular order.
	order []string
}

func (f fakeWindowOrgs) ListAll() ([]string, error) {
	if len(f.order) > 0 {
		return f.order, nil
	}
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

// TestSupportWindowWarnsAt30And7DaysAndOnClose: with 0.4.0 designated on
// 2026-10-01 the window closes on 2026-12-30; an instance on 0.3.0 is
// warned once inside 30 days, once inside 7, and once after the close, and
// only its admins are.
func TestSupportWindowWarnsAt30And7DaysAndOnClose(t *testing.T) {
	feed := `{"version":"0.5.0","stable":"0.4.0","stable_since":"2026-10-01"}`
	w, store, _ := watcherFixture(t, feed, &release.Stable{Version: "0.3.0", Since: "2026-09-01"})

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
	feed := `{"version":"0.5.0","stable":"0.4.0","stable_since":"2026-10-01"}`
	w, store, _ := watcherFixture(t, feed, &release.Stable{Version: "0.4.0", Since: "2026-10-01"})
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

// TestSupportWindowWarningCountsTheDaysLeft: the warning's title counts the
// days left before the window closes rounding up, in the singular or the
// plural, and says "today" on the last day. It read "Upgrade OpenV within 1
// days" a day and a half out, and "within 0 days" on the last day (#379,
// bug 61). Once the window has closed, the next run sends the closed
// warning; the truncated count kept sending "within 0 days" for the first
// day after the close.
func TestSupportWindowWarningCountsTheDaysLeft(t *testing.T) {
	// 0.4.0 designated on 2026-10-01: the window closes at the start of
	// 2026-12-30 (UTC).
	closes := time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC)
	feed := `{"version":"0.5.0","stable":"0.4.0","stable_since":"2026-10-01"}`
	day := 24 * time.Hour
	cases := []struct {
		left time.Duration
		want string
	}{
		{day / 2, "Upgrade OpenV today"},
		{time.Minute, "Upgrade OpenV today"},
		{day, "Upgrade OpenV within 1 day"},
		{day + day/2, "Upgrade OpenV within 2 days"},
		{2 * day, "Upgrade OpenV within 2 days"},
		{6*day + day/2, "Upgrade OpenV within 7 days"},
		{24*day + day/2, "Upgrade OpenV within 25 days"},
		{-time.Hour, "OpenV support window has closed"},
	}
	for _, tc := range cases {
		w, store, _ := watcherFixture(t, feed, &release.Stable{Version: "0.3.0", Since: "2026-09-01"})
		now := closes.Add(-tc.left)
		w.now = func() time.Time { return now }
		w.Run()
		if len(store.rows) != 1 || store.rows[0].Title != tc.want {
			titles := []string{}
			for _, r := range store.rows {
				titles = append(titles, r.Title)
			}
			t.Errorf("%v before the close: titles %q, want [%q]", tc.left, titles, tc.want)
		}
	}
}

// TestSupportWindowWarnsEachAdminOnce: a warning is about the instance, not
// a workspace, so it reaches each admin once, by bell, email and push,
// however many workspaces they administer (a personal one counts). Their
// one row is stored under the first workspace they administer, in ListAll
// order; an admin of one workspace is warned as before. Each warning used to
// reach an admin once for every workspace they administer (#379 bug 221).
func TestSupportWindowWarnsEachAdminOnce(t *testing.T) {
	for _, k := range ncEnvKeys {
		t.Setenv(k, notifications.TypeReleaseSupportWindow)
	}
	ch := newNCChannels(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.5.0","stable":"0.4.0","stable_since":"2026-10-01"}`))
	}))
	t.Cleanup(feed.Close)
	// ada is only a member of o-team, administers her own o-ada, and
	// administers o-lab with dee; gus administers o-team alone.
	o := fakeWindowOrgs{
		order: []string{"o-team", "o-ada", "o-lab"},
		members: map[string][]*orgs.Member{
			"o-team": {
				{OrgID: "o-team", UserID: "user-ada", Role: orgs.RoleMember},
				{OrgID: "o-team", UserID: "user-gus", Role: orgs.RoleAdmin},
			},
			"o-ada": {{OrgID: "o-ada", UserID: "user-ada", Role: orgs.RoleAdmin}},
			"o-lab": {
				{OrgID: "o-lab", UserID: "user-dee", Role: orgs.RoleAdmin},
				{OrgID: "o-lab", UserID: "user-ada", Role: orgs.RoleAdmin},
			},
		},
	}
	w := NewSupportWindowWatcher(feed.URL, fakeReleases{stable: &release.Stable{Version: "0.3.0", Since: "2026-09-01"}},
		o, &fakeClaimer{claimed: map[string]bool{}}, ch.store, ch.bc).
		SetChannels(Channels{Email: ch.email, Push: ch.push})

	// The window closes on 2026-12-30: the 7-day warning, then the closed one.
	for _, now := range []time.Time{
		time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC),
	} {
		w.now = func() time.Time { return now }
		w.Run()
	}

	deliveries, strays := ch.rec.take()
	if len(strays) > 0 {
		t.Errorf("sent with no row stored: %v", strays)
	}
	var got []string
	for _, d := range deliveries {
		got = append(got, fmt.Sprintf("%s in %s: %q by %d SSE, %d email, %d push",
			d.row.UserID, d.row.OrgID, d.row.Title, len(d.frames), len(d.emails), len(d.pushes)))
		for _, p := range ncAddressProblems(d) {
			t.Error(p)
		}
		if p := ncOrderProblem(d); p != "" {
			t.Error(p)
		}
	}
	want := []string{
		`user-gus in o-team: "Upgrade OpenV within 4 days" by 1 SSE, 1 email, 1 push`,
		`user-ada in o-ada: "Upgrade OpenV within 4 days" by 1 SSE, 1 email, 1 push`,
		`user-dee in o-lab: "Upgrade OpenV within 4 days" by 1 SSE, 1 email, 1 push`,
		`user-gus in o-team: "OpenV support window has closed" by 1 SSE, 1 email, 1 push`,
		`user-ada in o-ada: "OpenV support window has closed" by 1 SSE, 1 email, 1 push`,
		`user-dee in o-lab: "OpenV support window has closed" by 1 SSE, 1 email, 1 push`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("delivered:\n  %s\nwant each admin warned once per warning:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
