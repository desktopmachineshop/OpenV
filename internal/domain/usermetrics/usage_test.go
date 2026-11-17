package usermetrics

import (
	"testing"
	"time"
)

func TestPaceOf(t *testing.T) {
	w := func(prev, recent int) []int {
		s := make([]int, UsageWeeks)
		for i := UsageWeeks - 2*PaceWeeks; i < UsageWeeks-PaceWeeks; i++ {
			s[i] = prev
		}
		for i := UsageWeeks - PaceWeeks; i < UsageWeeks; i++ {
			s[i] = recent
		}
		return s
	}
	cases := []struct {
		name  string
		weeks []int
		trend string
	}{
		{"idle", w(0, 0), TrendIdle},
		{"dormant", w(5, 0), TrendDormant},
		{"new", w(0, 3), TrendNew},
		{"accelerating", w(4, 5), TrendAccelerating},
		{"slowing", w(4, 3), TrendSlowing},
		{"steady", w(10, 11), TrendSteady},
	}
	for _, c := range cases {
		if got := PaceOf(c.weeks); got.Trend != c.trend {
			t.Errorf("%s: trend %q, want %q (%+v)", c.name, got.Trend, c.trend, got)
		}
	}
	p := PaceOf(w(4, 6))
	if p.Recent != 24 || p.Previous != 16 || p.Change != 0.5 {
		t.Errorf("pace %+v, want recent 24, previous 16, change 0.5", p)
	}
	// An older week outside both windows changes nothing.
	old := w(0, 0)
	old[0] = 50
	if got := PaceOf(old); got.Trend != TrendIdle {
		t.Errorf("a week before both windows counted: %+v", got)
	}
}

type fakeUsageRepo struct {
	counts   []WorkspaceCounts
	members  []MemberActivity
	wsDays   []KeyedDay
	userDays []KeyedDay
	active   []KeyedDay
	lastWS   map[string]time.Time
	lastUser map[string]time.Time
	ms       []Membership
}

func (f *fakeUsageRepo) WorkspaceCounts() ([]WorkspaceCounts, error)        { return f.counts, nil }
func (f *fakeUsageRepo) MemberActivity(time.Time) ([]MemberActivity, error) { return f.members, nil }
func (f *fakeUsageRepo) WorkspaceChanges(time.Time) ([]KeyedDay, error)     { return f.wsDays, nil }
func (f *fakeUsageRepo) UserChanges(time.Time) ([]KeyedDay, error)          { return f.userDays, nil }
func (f *fakeUsageRepo) UserActiveDays(time.Time) ([]KeyedDay, error)       { return f.active, nil }
func (f *fakeUsageRepo) LastWorkspaceChange() (map[string]time.Time, error) { return f.lastWS, nil }
func (f *fakeUsageRepo) LastUserActive() (map[string]time.Time, error)      { return f.lastUser, nil }
func (f *fakeUsageRepo) Memberships() ([]Membership, error)                 { return f.ms, nil }

func TestUsageBucketsByRollingWeek(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	today := day(now)
	repo := &fakeUsageRepo{
		counts:  []WorkspaceCounts{{OrgID: "w1", Requirements: 3, TestCases: 2, Links: 4}},
		members: []MemberActivity{{OrgID: "w1", Members: 2, Active30d: 1}},
		wsDays: []KeyedDay{
			{Key: "w1", Day: today, Count: 2},                    // this week
			{Key: "w1", Day: today.AddDate(0, 0, -6), Count: 1},  // still this week
			{Key: "w1", Day: today.AddDate(0, 0, -7), Count: 5},  // the week before
			{Key: "w1", Day: today.AddDate(0, 0, -84), Count: 9}, // outside the series
		},
		userDays: []KeyedDay{{Key: "u1", Day: today, Count: 3}},
		active:   []KeyedDay{{Key: "u2", Day: today.AddDate(0, 0, -40), Count: 1}},
		lastWS:   map[string]time.Time{"w1": now},
		lastUser: map[string]time.Time{"u1": now},
		ms:       []Membership{{OrgID: "w1", UserID: "u1", Role: "owner"}},
	}
	s := NewDefaultUsageService(repo)
	s.now = func() time.Time { return now }
	u, err := s.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Weeks) != UsageWeeks || u.Weeks[UsageWeeks-1] != "2026-11-10" || u.Weeks[0] != "2026-08-25" {
		t.Fatalf("weeks %v", u.Weeks)
	}
	if len(u.Workspaces) != 1 {
		t.Fatalf("workspaces %+v", u.Workspaces)
	}
	w := u.Workspaces[0]
	if w.Changes[UsageWeeks-1] != 3 || w.Changes[UsageWeeks-2] != 5 || w.Requirements != 3 || w.Members != 2 || w.Active30d != 1 {
		t.Errorf("workspace row %+v", w)
	}
	if w.LastActivity != "2026-11-10" || w.Pace.Recent != 8 || w.Pace.Trend != TrendNew {
		t.Errorf("workspace pace %+v last %q", w.Pace, w.LastActivity)
	}
	if len(u.Users) != 2 {
		t.Fatalf("users %+v", u.Users)
	}
	u1, u2 := u.Users[0], u.Users[1]
	if u1.ID != "u1" || len(u1.Workspaces) != 1 || u1.Workspaces[0].Role != "owner" || u1.Changes[UsageWeeks-1] != 3 {
		t.Errorf("u1 %+v", u1)
	}
	// Someone who only reads is paced on their signed-in days.
	if u2.ID != "u2" || u2.ActiveDays[UsageWeeks-6] != 1 || u2.Pace.Trend != TrendDormant || len(u2.Workspaces) != 0 {
		t.Errorf("u2 %+v", u2)
	}
}
