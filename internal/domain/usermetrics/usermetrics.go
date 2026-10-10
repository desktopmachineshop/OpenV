// Package usermetrics holds the platform admin's user dashboard: who signed
// up, who is active, who has gone quiet and where they sign in from.
//
// Activity is recorded at most once per user per day: the auth middleware
// calls Service.RecordActivity on every authenticated browser request, and
// the service drops repeats in memory before they reach the database, so
// the cost is one INSERT per user per day per server. The country is the
// two-letter code Cloudflare sends in CF-IPCountry; the IP address itself
// is never stored.
//
// The dashboard is computed on demand from users and user_activity_days;
// nothing is precomputed, so a number is always as fresh as the last
// recorded day.
package usermetrics

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Window lengths the dashboard reports on.
const (
	// LostAfterDays is how long without any activity makes a user lost.
	LostAfterDays = 30
	// SeriesDays is the length of the daily sign-up and activity series.
	SeriesDays = 90
	// CohortMonths is how many monthly sign-up cohorts the retention grid
	// shows, the current month included.
	CohortMonths = 6
)

// DayCount is one day (or, for cohort sizes, one month) and a count.
type DayCount struct {
	Day   time.Time
	Count int
}

// NamedCount is a label and a count.
type NamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CountryCount is the users whose latest known country is Country, and how
// many of them were active in the last LostAfterDays days.
type CountryCount struct {
	Country   string `json:"country"`
	Users     int    `json:"users"`
	Active30d int    `json:"active_30d"`
}

// CohortCell counts the users of one sign-up month active in a later month.
type CohortCell struct {
	Cohort time.Time
	Month  time.Time
	Users  int
}

// UserTotals counts accounts by creation date.
type UserTotals struct {
	Total      int
	New7d      int
	New30d     int
	NewPrev30d int
}

// ActivityTotals counts distinct active users. Lost is every user whose last
// activity is more than LostAfterDays ago; NewlyLost the part of them whose
// last activity fell in the LostAfterDays before that.
type ActivityTotals struct {
	DAU, WAU, MAU   int
	Lost, NewlyLost int
}

// Repository persists activity days and answers the dashboard's
// aggregates. Every time is a UTC calendar day.
type Repository interface {
	// RecordActivity notes that the user was active on day. A known
	// country replaces an unknown one recorded earlier that day; an unknown
	// one never erases a known one.
	RecordActivity(userID string, day time.Time, country string) error
	UserTotals(today time.Time) (UserTotals, error)
	ActivityTotals(today time.Time) (ActivityTotals, error)
	// DailySignups and DailyActive answer the days on or after since that
	// have a non-zero count.
	DailySignups(since time.Time) ([]DayCount, error)
	DailyActive(since time.Time) ([]DayCount, error)
	AuthProviders() ([]NamedCount, error)
	// Countries groups users by the country of their latest activity that
	// had one; activeSince bounds Active30d.
	Countries(activeSince time.Time) ([]CountryCount, error)
	// CohortSizes counts sign-ups per month from since (a month start);
	// CohortActivity counts each of those cohorts' active users per month.
	CohortSizes(since time.Time) ([]DayCount, error)
	CohortActivity(since time.Time) ([]CohortCell, error)
}

// Dashboard is GET /api/v1/admin/metrics/users.
type Dashboard struct {
	GeneratedAt string `json:"generated_at"`
	TotalUsers  int    `json:"total_users"`
	NewUsers7d  int    `json:"new_users_7d"`
	NewUsers30d int    `json:"new_users_30d"`
	// NewUsersPrev30d is the 30 days before NewUsers30d, for the trend.
	NewUsersPrev30d int `json:"new_users_prev_30d"`
	DAU             int `json:"dau"`
	WAU             int `json:"wau"`
	MAU             int `json:"mau"`
	// Stickiness is DAU / MAU, 0 when nobody was active this month.
	Stickiness float64 `json:"stickiness"`
	// LostUsers have no activity in the last 30 days; NewlyLost30d went
	// quiet in the 30 days before that.
	LostUsers     int            `json:"lost_users"`
	NewlyLost30d  int            `json:"newly_lost_30d"`
	LostAfterDays int            `json:"lost_after_days"`
	Signups       []SeriesPoint  `json:"signups"`
	Active        []SeriesPoint  `json:"active"`
	AuthProviders []NamedCount   `json:"auth_providers"`
	Countries     []CountryCount `json:"countries"`
	// UnknownCountry counts users with no recorded country.
	UnknownCountry int      `json:"unknown_country"`
	Cohorts        []Cohort `json:"cohorts"`
}

// SeriesPoint is one day of a daily series, every day present.
type SeriesPoint struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// Cohort is one sign-up month and, for it and each month since, the share
// of its users active in that month (index 0 is the sign-up month).
type Cohort struct {
	Month    string    `json:"month"`
	Size     int       `json:"size"`
	Retained []float64 `json:"retained"`
}

// Service records activity and builds the dashboard.
type Service interface {
	// RecordActivity never fails the request it is called from: a failed
	// write is logged and retried on the user's next request.
	RecordActivity(userID, country string)
	Dashboard() (*Dashboard, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
	now  func() time.Time
	// seen holds userID → "day|country" already written this process.
	seen sync.Map
}

// NewDefaultService creates the service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// NormalizeCountry turns a CF-IPCountry value into an ISO 3166-1 alpha-2
// code, or "" for a missing value, Cloudflare's XX (unknown) and T1 (Tor),
// or anything that is not two letters.
func NormalizeCountry(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if len(v) != 2 || v == "XX" || v == "T1" {
		return ""
	}
	for _, c := range v {
		if c < 'A' || c > 'Z' {
			return ""
		}
	}
	return v
}

func day(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// RecordActivity implements Service.
func (s *DefaultService) RecordActivity(userID, country string) {
	if userID == "" {
		return
	}
	country = NormalizeCountry(country)
	today := day(s.now())
	key := today.Format("2006-01-02") + "|" + country
	if prev, ok := s.seen.Load(userID); ok {
		p := prev.(string)
		// Same day and either the same country or nothing new to add.
		if p == key || (country == "" && strings.HasPrefix(p, key)) {
			return
		}
	}
	if err := s.repo.RecordActivity(userID, today, country); err != nil {
		slog.Warn("user activity not recorded", "user_id", userID, "error", err)
		return
	}
	s.seen.Store(userID, key)
}

// Dashboard implements Service.
func (s *DefaultService) Dashboard() (*Dashboard, error) {
	now := s.now()
	today := day(now)
	ut, err := s.repo.UserTotals(today)
	if err != nil {
		return nil, err
	}
	at, err := s.repo.ActivityTotals(today)
	if err != nil {
		return nil, err
	}
	since := today.AddDate(0, 0, -(SeriesDays - 1))
	signups, err := s.repo.DailySignups(since)
	if err != nil {
		return nil, err
	}
	active, err := s.repo.DailyActive(since)
	if err != nil {
		return nil, err
	}
	providers, err := s.repo.AuthProviders()
	if err != nil {
		return nil, err
	}
	countries, err := s.repo.Countries(today.AddDate(0, 0, -(LostAfterDays - 1)))
	if err != nil {
		return nil, err
	}
	cohortStart := monthStart(today).AddDate(0, -(CohortMonths - 1), 0)
	sizes, err := s.repo.CohortSizes(cohortStart)
	if err != nil {
		return nil, err
	}
	cells, err := s.repo.CohortActivity(cohortStart)
	if err != nil {
		return nil, err
	}

	d := &Dashboard{
		GeneratedAt:     now.UTC().Format(time.RFC3339),
		TotalUsers:      ut.Total,
		NewUsers7d:      ut.New7d,
		NewUsers30d:     ut.New30d,
		NewUsersPrev30d: ut.NewPrev30d,
		DAU:             at.DAU,
		WAU:             at.WAU,
		MAU:             at.MAU,
		LostUsers:       at.Lost,
		NewlyLost30d:    at.NewlyLost,
		LostAfterDays:   LostAfterDays,
		Signups:         fillSeries(signups, since, today),
		Active:          fillSeries(active, since, today),
		AuthProviders:   providers,
		Countries:       countries,
		Cohorts:         buildCohorts(sizes, cells, cohortStart, today),
	}
	if at.MAU > 0 {
		d.Stickiness = float64(at.DAU) / float64(at.MAU)
	}
	if d.AuthProviders == nil {
		d.AuthProviders = []NamedCount{}
	}
	if d.Countries == nil {
		d.Countries = []CountryCount{}
	}
	known := 0
	for _, c := range countries {
		known += c.Users
	}
	if d.UnknownCountry = ut.Total - known; d.UnknownCountry < 0 {
		d.UnknownCountry = 0
	}
	sort.SliceStable(d.Countries, func(i, j int) bool {
		if d.Countries[i].Users != d.Countries[j].Users {
			return d.Countries[i].Users > d.Countries[j].Users
		}
		return d.Countries[i].Country < d.Countries[j].Country
	})
	return d, nil
}

// fillSeries answers every day from since to today, zero where counts has
// no entry.
func fillSeries(counts []DayCount, since, today time.Time) []SeriesPoint {
	by := make(map[string]int, len(counts))
	for _, c := range counts {
		by[day(c.Day).Format("2006-01-02")] += c.Count
	}
	out := []SeriesPoint{}
	for d := since; !d.After(today); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		out = append(out, SeriesPoint{Day: k, Count: by[k]})
	}
	return out
}

func monthsBetween(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
}

// buildCohorts answers one row per month from start to today's month, each
// with a share for every month that has begun since it.
func buildCohorts(sizes []DayCount, cells []CohortCell, start, today time.Time) []Cohort {
	size := map[string]int{}
	for _, s := range sizes {
		size[monthStart(s.Day).Format("2006-01")] += s.Count
	}
	active := map[string]map[int]int{}
	for _, c := range cells {
		k := monthStart(c.Cohort).Format("2006-01")
		off := monthsBetween(monthStart(c.Cohort), monthStart(c.Month))
		if off < 0 {
			continue
		}
		if active[k] == nil {
			active[k] = map[int]int{}
		}
		active[k][off] += c.Users
	}
	current := monthStart(today)
	out := []Cohort{}
	for m := start; !m.After(current); m = m.AddDate(0, 1, 0) {
		k := m.Format("2006-01")
		c := Cohort{Month: k, Size: size[k], Retained: []float64{}}
		for off := 0; off <= monthsBetween(m, current); off++ {
			share := 0.0
			if c.Size > 0 {
				share = float64(active[k][off]) / float64(c.Size)
			}
			c.Retained = append(c.Retained, share)
		}
		out = append(out, c)
	}
	return out
}
