package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

type fakeSteps struct{ taken map[string]bool }

func (f *fakeSteps) ClaimStableStep(orgID, version, step string, _ time.Time) (bool, error) {
	key := orgID + "/" + version + "/" + step
	if f.taken[key] {
		return false, nil
	}
	f.taken[key] = true
	return true, nil
}

type fakeStableOrgs struct {
	list    []*orgs.Org
	members map[string][]*orgs.Member
	turned  map[string]string
}

func (f *fakeStableOrgs) ListOrgsByChannel(string) ([]*orgs.Org, error) { return f.list, nil }
func (f *fakeStableOrgs) ListMembers(orgID string) ([]*orgs.Member, error) {
	return f.members[orgID], nil
}
func (f *fakeStableOrgs) SetStableRelease(id, version string) (*orgs.Org, error) {
	f.turned[id] = version
	for _, o := range f.list {
		if o.ID == id {
			o.StableRelease = version
		}
	}
	return nil, nil
}

type fakeReleases struct{ stable *release.Stable }

func (f fakeReleases) Current() *release.Release      { return nil }
func (f fakeReleases) Released() []release.Release    { return nil }
func (f fakeReleases) CurrentStable() *release.Stable { return f.stable }
func (f fakeReleases) Stable(string) *release.Stable  { return f.stable }

// stableOf builds a stable the way the parser does: a merged, grouped set
// of notes and the day it was designated.
func stableOf(version, since string, groups ...release.Category) *release.Stable {
	s := &release.Stable{Version: version, Since: since, Categories: groups}
	for _, g := range groups {
		s.Notes = append(s.Notes, g.Notes...)
	}
	return s
}

func schedulerFixture(stable *release.Stable, list []*orgs.Org) (*StableScheduler, *fakeStableOrgs, *captureStore) {
	store := &captureStore{}
	o := &fakeStableOrgs{list: list, turned: map[string]string{}, members: map[string][]*orgs.Member{}}
	for _, org := range list {
		o.members[org.ID] = []*orgs.Member{
			{OrgID: org.ID, UserID: org.ID + "-admin", Role: orgs.RoleAdmin},
			{OrgID: org.ID, UserID: org.ID + "-member", Role: orgs.RoleMember},
		}
	}
	s := NewStableScheduler(fakeReleases{stable: stable}, o, &fakeSteps{taken: map[string]bool{}}, store, nil)
	return s, o, store
}

func rowsOf(store *captureStore, userID, ntype string) []*notifications.Notification {
	var out []*notifications.Notification
	for _, r := range store.rows {
		if r.UserID == userID && r.Type == ntype {
			out = append(out, r)
		}
	}
	return out
}

// TestSchedulerTurnsOnAtTheCutWithoutAWindow: a workspace with no window
// moves at once; its admins get the cut notice, every member the release,
// and a second run repeats nothing.
func TestSchedulerTurnsOnAtTheCutWithoutAWindow(t *testing.T) {
	stable := stableOf("0.4.0", "2026-10-01", release.Category{Name: release.CategoryFeatures, Notes: []string{"A"}}, release.Category{Name: release.CategoryFixes, Notes: []string{"B"}})
	org := &orgs.Org{ID: "o1", Name: "Acme", Plan: orgs.PlanBusiness}
	s, o, store := schedulerFixture(stable, []*orgs.Org{org})
	s.now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	s.Run()
	if o.turned["o1"] != "0.4.0" {
		t.Fatalf("not turned on: %v", o.turned)
	}
	if got := rowsOf(store, "o1-admin", notifications.TypeReleaseScheduled); len(got) != 1 || !strings.Contains(got[0].Body, "turns on for the workspace now") || !strings.Contains(got[0].Body, "1 new feature(s) and 1 other change(s)") {
		t.Fatalf("admin cut notice = %+v", got)
	}
	if got := rowsOf(store, "o1-member", notifications.TypeReleasePublished); len(got) != 1 || !strings.Contains(got[0].Title, "moved to stable release 0.4.0") || !strings.Contains(got[0].Body, "New features\n• A\nBug fixes\n• B") {
		t.Fatalf("member release = %+v", got)
	}
	if got := rowsOf(store, "o1-member", notifications.TypeReleaseScheduled); len(got) != 0 {
		t.Fatalf("a member got the admin notice")
	}
	before := len(store.rows)
	s.Run()
	if len(store.rows) != before {
		t.Fatalf("second run repeated notifications")
	}
}

// TestSchedulerHonoursTheWindow: with a window on the 10th at 09:00 in
// Europe/London the release waits; the admins hear at the cut, are reminded
// the day before, and the members hear at the turn-on. Nothing repeats.
func TestSchedulerHonoursTheWindow(t *testing.T) {
	stable := stableOf("0.4.0", "2026-10-01", release.Category{Name: release.CategoryFeatures, Notes: []string{"A"}})
	org := &orgs.Org{ID: "o1", Name: "Acme", Plan: orgs.PlanBusiness, UpgradeDay: 10, UpgradeHour: 9, UpgradeTimezone: "Europe/London"}
	s, o, store := schedulerFixture(stable, []*orgs.Org{org})
	london, _ := time.LoadLocation("Europe/London")

	s.now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	s.Run()
	if o.turned["o1"] != "" {
		t.Fatalf("turned on before the window")
	}
	if got := rowsOf(store, "o1-admin", notifications.TypeReleaseScheduled); len(got) != 1 || !strings.Contains(got[0].Body, "Sat 10 Oct 2026 at 09:00 BST") {
		t.Fatalf("cut notice = %+v", got)
	}

	s.now = func() time.Time { return time.Date(2026, 10, 9, 10, 0, 0, 0, london) }
	s.Run()
	if got := rowsOf(store, "o1-admin", notifications.TypeReleaseScheduled); len(got) != 2 || !strings.Contains(got[1].Title, "tomorrow") {
		t.Fatalf("reminder = %+v", got)
	}
	s.Run()
	if got := rowsOf(store, "o1-admin", notifications.TypeReleaseScheduled); len(got) != 2 {
		t.Fatalf("reminder repeated")
	}

	s.now = func() time.Time { return time.Date(2026, 10, 10, 9, 0, 0, 0, london) }
	s.Run()
	if o.turned["o1"] != "0.4.0" {
		t.Fatalf("not turned on at the window")
	}
	if got := rowsOf(store, "o1-member", notifications.TypeReleasePublished); len(got) != 1 {
		t.Fatalf("member release = %+v", got)
	}
}

// TestSchedulerSkipsWorkspacesAlreadyOnTheRelease: nothing happens for a
// workspace already on the newest stable, or when no stable exists.
func TestSchedulerSkipsWorkspacesAlreadyOnTheRelease(t *testing.T) {
	stable := stableOf("0.4.0", "2026-10-01")
	org := &orgs.Org{ID: "o1", Plan: orgs.PlanBusiness, StableRelease: "0.4.0"}
	s, _, store := schedulerFixture(stable, []*orgs.Org{org})
	s.Run()
	if len(store.rows) != 0 {
		t.Fatalf("notified a workspace already on the release")
	}
	s, _, store = schedulerFixture(nil, []*orgs.Org{org})
	s.Run()
	if len(store.rows) != 0 {
		t.Fatalf("notified without a stable release")
	}
}

func TestUpgradeTimeFor(t *testing.T) {
	cut := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if got := orgs.UpgradeTimeFor(cut, 0, 0, ""); !got.Equal(cut) {
		t.Fatalf("no window: %v", got)
	}
	if got := orgs.UpgradeTimeFor(cut, 10, 9, "UTC"); !got.Equal(time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("same month: %v", got)
	}
	// The 1st at 07:00 has passed by 08:00: next month's is beyond 14 days, so it clamps.
	if got := orgs.UpgradeTimeFor(cut, 1, 7, "UTC"); !got.Equal(cut.Add(14 * 24 * time.Hour)) {
		t.Fatalf("clamped: %v", got)
	}
	if got := orgs.UpgradeTimeFor(cut, 20, 9, "Nowhere/Invalid"); !got.Equal(cut.Add(14 * 24 * time.Hour)) {
		t.Fatalf("unknown zone, clamped: %v", got)
	}
}
