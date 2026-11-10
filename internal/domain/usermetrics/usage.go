package usermetrics

// Usage: the platform admin's view of how much each workspace and each
// person uses OpenV, and whether that use is growing or fading.
//
// Only counts leave the database: how many requirements, test cases, links,
// runs and comments a workspace holds, how many changes it and each person
// made per week, and on how many days a person was signed in. No artifact
// text, name or reference is read, so an admin can see that a workspace is
// busy without seeing what it is busy with.
//
// A change is a domain event (the activity log's rows) with a user or agent
// actor; system events, such as a nightly sweep, are not anyone's use. The
// series is UsageWeeks rolling seven-day windows that end today, so the
// last window is always a whole week and the pace never dips because a
// calendar week has only just begun.

import (
	"sort"
	"time"
)

// UsageWeeks is the length of each weekly series; PaceWeeks the two windows
// a pace compares (the last PaceWeeks weeks against the PaceWeeks before).
const (
	UsageWeeks = 12
	PaceWeeks  = 4
	// PaceThreshold is the change either way that counts as speeding up or
	// slowing down rather than steady.
	PaceThreshold = 0.25
)

// Pace trends.
const (
	TrendAccelerating = "accelerating"
	TrendSteady       = "steady"
	TrendSlowing      = "slowing"
	// TrendNew: active lately, idle in the window before.
	TrendNew = "new"
	// TrendDormant: active before, idle lately.
	TrendDormant = "dormant"
	// TrendIdle: no activity in either window.
	TrendIdle = "idle"
)

// WorkspaceCounts is what a workspace holds now: live rows only, so an
// archived artifact or a removed link no longer counts.
type WorkspaceCounts struct {
	OrgID          string `json:"-"`
	Projects       int    `json:"projects"`
	Requirements   int    `json:"requirements"`
	TestCases      int    `json:"test_cases"`
	OtherArtifacts int    `json:"other_artifacts"`
	Links          int    `json:"links"`
	TestRuns       int    `json:"test_runs"`
	TestResults    int    `json:"test_results"`
	Comments       int    `json:"comments"`
}

// KeyedDay is one day's count for a workspace or a person.
type KeyedDay struct {
	Key   string
	Day   time.Time
	Count int
}

// Membership is one person's place in one workspace.
type Membership struct {
	OrgID  string `json:"workspace_id"`
	UserID string `json:"-"`
	Role   string `json:"role"`
}

// MemberActivity counts a workspace's members and those signed in lately.
type MemberActivity struct {
	OrgID     string
	Members   int
	Active30d int
}

// UsageRepository answers the usage view's aggregates. Every day is a UTC
// calendar day.
type UsageRepository interface {
	WorkspaceCounts() ([]WorkspaceCounts, error)
	MemberActivity(today time.Time) ([]MemberActivity, error)
	// WorkspaceChanges and UserChanges count user and agent events per
	// workspace and per acting user for each day on or after since.
	WorkspaceChanges(since time.Time) ([]KeyedDay, error)
	UserChanges(since time.Time) ([]KeyedDay, error)
	// UserActiveDays answers one row (Count 1) per day a user was signed in.
	UserActiveDays(since time.Time) ([]KeyedDay, error)
	// LastWorkspaceChange and LastUserActive answer each key's latest day.
	LastWorkspaceChange() (map[string]time.Time, error)
	LastUserActive() (map[string]time.Time, error)
	Memberships() ([]Membership, error)
}

// Pace compares the last PaceWeeks weeks with the PaceWeeks before them.
// Change is (recent − previous) / previous, 0 when previous is 0.
type Pace struct {
	Recent   int     `json:"recent"`
	Previous int     `json:"previous"`
	Change   float64 `json:"change"`
	Trend    string  `json:"trend"`
}

// WorkspaceUsage is one workspace's row of the usage view.
type WorkspaceUsage struct {
	ID string `json:"id"`
	WorkspaceCounts
	Members   int `json:"members"`
	Active30d int `json:"active_members_30d"`
	// Changes is one count per week, oldest first.
	Changes      []int  `json:"changes"`
	LastActivity string `json:"last_activity,omitempty"`
	Pace         Pace   `json:"pace"`
}

// UserUsage is one person's row of the usage view.
type UserUsage struct {
	ID         string       `json:"id"`
	Workspaces []Membership `json:"workspaces"`
	// Changes and ActiveDays are one count per week, oldest first.
	Changes    []int  `json:"changes"`
	ActiveDays []int  `json:"active_days"`
	LastActive string `json:"last_active,omitempty"`
	// Pace is over changes; a person who only reads is paced on the days
	// they were signed in instead.
	Pace Pace `json:"pace"`
}

// Usage is GET /api/v1/admin/metrics/usage.
type Usage struct {
	GeneratedAt string `json:"generated_at"`
	// Weeks names each window by its last day, oldest first.
	Weeks      []string         `json:"weeks"`
	PaceWeeks  int              `json:"pace_weeks"`
	Workspaces []WorkspaceUsage `json:"workspaces"`
	Users      []UserUsage      `json:"users"`
}

// UsageService builds the usage view.
type UsageService interface {
	Usage() (*Usage, error)
}

// DefaultUsageService implements UsageService.
type DefaultUsageService struct {
	repo UsageRepository
	now  func() time.Time
}

// NewDefaultUsageService creates the service.
func NewDefaultUsageService(repo UsageRepository) *DefaultUsageService {
	return &DefaultUsageService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// weekIndex places a day in the series: 0 is the oldest week, UsageWeeks−1
// the one ending today, −1 outside.
func weekIndex(d, today time.Time) int {
	ago := int(today.Sub(day(d)).Hours() / 24)
	if ago < 0 || ago >= UsageWeeks*7 {
		return -1
	}
	return UsageWeeks - 1 - ago/7
}

func bucket(rows []KeyedDay, today time.Time) map[string][]int {
	out := map[string][]int{}
	for _, r := range rows {
		i := weekIndex(r.Day, today)
		if i < 0 {
			continue
		}
		s := out[r.Key]
		if s == nil {
			s = make([]int, UsageWeeks)
			out[r.Key] = s
		}
		s[i] += r.Count
	}
	return out
}

func series(m map[string][]int, key string) []int {
	if s, ok := m[key]; ok {
		return s
	}
	return make([]int, UsageWeeks)
}

// PaceOf compares a weekly series' last PaceWeeks with the PaceWeeks before.
func PaceOf(weeks []int) Pace {
	n := len(weeks)
	p := Pace{}
	for i := n - PaceWeeks; i < n; i++ {
		if i >= 0 {
			p.Recent += weeks[i]
		}
	}
	for i := n - 2*PaceWeeks; i < n-PaceWeeks; i++ {
		if i >= 0 {
			p.Previous += weeks[i]
		}
	}
	switch {
	case p.Recent == 0 && p.Previous == 0:
		p.Trend = TrendIdle
	case p.Recent == 0:
		p.Trend, p.Change = TrendDormant, -1
	case p.Previous == 0:
		p.Trend = TrendNew
	default:
		p.Change = float64(p.Recent-p.Previous) / float64(p.Previous)
		switch {
		case p.Change >= PaceThreshold:
			p.Trend = TrendAccelerating
		case p.Change <= -PaceThreshold:
			p.Trend = TrendSlowing
		default:
			p.Trend = TrendSteady
		}
	}
	return p
}

func isZero(s []int) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

// Usage implements UsageService.
func (s *DefaultUsageService) Usage() (*Usage, error) {
	now := s.now()
	today := day(now)
	since := today.AddDate(0, 0, -(UsageWeeks*7 - 1))

	counts, err := s.repo.WorkspaceCounts()
	if err != nil {
		return nil, err
	}
	members, err := s.repo.MemberActivity(today)
	if err != nil {
		return nil, err
	}
	wsDays, err := s.repo.WorkspaceChanges(since)
	if err != nil {
		return nil, err
	}
	userDays, err := s.repo.UserChanges(since)
	if err != nil {
		return nil, err
	}
	activeDays, err := s.repo.UserActiveDays(since)
	if err != nil {
		return nil, err
	}
	lastWS, err := s.repo.LastWorkspaceChange()
	if err != nil {
		return nil, err
	}
	lastUser, err := s.repo.LastUserActive()
	if err != nil {
		return nil, err
	}
	memberships, err := s.repo.Memberships()
	if err != nil {
		return nil, err
	}

	u := &Usage{GeneratedAt: now.Format(time.RFC3339), PaceWeeks: PaceWeeks, Workspaces: []WorkspaceUsage{}, Users: []UserUsage{}}
	for i := 0; i < UsageWeeks; i++ {
		u.Weeks = append(u.Weeks, today.AddDate(0, 0, -7*(UsageWeeks-1-i)).Format("2006-01-02"))
	}

	// Workspaces: every one that holds something, has members or changed.
	ws := map[string]*WorkspaceUsage{}
	get := func(id string) *WorkspaceUsage {
		if w, ok := ws[id]; ok {
			return w
		}
		w := &WorkspaceUsage{ID: id}
		ws[id] = w
		return w
	}
	for _, c := range counts {
		get(c.OrgID).WorkspaceCounts = c
	}
	for _, m := range members {
		w := get(m.OrgID)
		w.Members, w.Active30d = m.Members, m.Active30d
	}
	wsWeeks := bucket(wsDays, today)
	for id := range wsWeeks {
		get(id)
	}
	for id, w := range ws {
		w.Changes = series(wsWeeks, id)
		w.Pace = PaceOf(w.Changes)
		if t, ok := lastWS[id]; ok {
			w.LastActivity = day(t).Format("2006-01-02")
		}
		u.Workspaces = append(u.Workspaces, *w)
	}
	sort.Slice(u.Workspaces, func(i, j int) bool { return u.Workspaces[i].ID < u.Workspaces[j].ID })

	u.Users = peopleUsage(memberships, bucket(userDays, today), bucket(activeDays, today), lastUser)
	return u, nil
}

// peopleUsage answers a row for everyone in a workspace, signed in or who
// changed something, by id.
func peopleUsage(memberships []Membership, changes, active map[string][]int, lastUser map[string]time.Time) []UserUsage {
	out := []UserUsage{}
	byUser := map[string][]Membership{}
	ids := map[string]bool{}
	for _, m := range memberships {
		byUser[m.UserID] = append(byUser[m.UserID], m)
		ids[m.UserID] = true
	}
	for id := range changes {
		ids[id] = true
	}
	for id := range active {
		ids[id] = true
	}
	for id := range lastUser {
		ids[id] = true
	}
	for id := range ids {
		row := UserUsage{ID: id, Workspaces: byUser[id], Changes: series(changes, id), ActiveDays: series(active, id)}
		if row.Workspaces == nil {
			row.Workspaces = []Membership{}
		}
		sort.Slice(row.Workspaces, func(i, j int) bool { return row.Workspaces[i].OrgID < row.Workspaces[j].OrgID })
		if isZero(row.Changes) {
			row.Pace = PaceOf(row.ActiveDays)
		} else {
			row.Pace = PaceOf(row.Changes)
		}
		if t, ok := lastUser[id]; ok {
			row.LastActive = day(t).Format("2006-01-02")
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
