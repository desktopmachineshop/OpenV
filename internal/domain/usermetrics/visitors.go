package usermetrics

// Site visitors: the platform admin's view of who reaches the public site
// and how many of them go on to make an account.
//
// A visitor is one browser, named by a random first-party id the API keeps
// in an HttpOnly cookie. The app sends one beacon per page load; a beacon
// from a browser that is not signed in counts that visitor for the day,
// and one from a signed-in browser links the visitor to the account. A
// visitor converts when the account it is linked to was made after the
// visitor was first seen, so an existing user signing in on a new laptop
// is never counted as a conversion.
//
// What is kept is the visitor's first day, the site page it landed on (a
// page that carries a token is kept as its template, never the token), the
// referring site or utm_source, and Cloudflare's country. No IP address,
// no user agent and no full referrer URL is stored. A browser that sends
// Global Privacy Control or Do Not Track is not counted at all, and
// neither is a user agent that names itself a bot.

import (
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// VisitorTopN bounds each breakdown table of the visitor dashboard.
const VisitorTopN = 10

// Visit is one beacon. UserID is set when the browser is signed in.
type Visit struct {
	VisitorID string
	UserID    string
	Path      string
	Source    string
	Country   string
}

// VisitorTotals counts visitors for the dashboard's headline numbers. Every
// window ends today and is counted in whole UTC days.
type VisitorTotals struct {
	Today, Last7d, Last30d         int
	New7d, New30d, NewPrev30d      int
	Converted30d, ConvertedPrev30d int
	// Signups30d counts every account made in the last 30 days, and
	// TrackedSignups30d those of them a visitor converted into.
	Signups30d, TrackedSignups30d int
}

// VisitorBreakdownRow is one value of a breakdown: the new visitors in the
// window that had it, and how many of them converted.
type VisitorBreakdownRow struct {
	Name        string `json:"name"`
	Visitors    int    `json:"visitors"`
	Conversions int    `json:"conversions"`
}

// Breakdown dimensions a VisitorRepository groups new visitors by.
const (
	BreakdownLandingPage = "landing_page"
	BreakdownSource      = "source"
	BreakdownCountry     = "country"
)

// VisitorRepository persists visitors and answers the dashboard's
// aggregates. Every time is a UTC calendar day.
type VisitorRepository interface {
	// RecordVisit creates the visitor on its first visit, with its landing
	// page, source and country, and notes that it visited on day. A later
	// visit never rewrites what the first one recorded.
	RecordVisit(visitorID string, day time.Time, path, source, country string) error
	// LinkVisitor links a visitor to the account it signed in to. The first
	// account wins; a visitor never recorded is left alone.
	LinkVisitor(visitorID, userID string) error
	VisitorTotals(today time.Time) (VisitorTotals, error)
	// DailyVisitors, DailyNewVisitors and DailyConversions answer the days
	// on or after since that have a non-zero count. A conversion is dated
	// by the day the account was made.
	DailyVisitors(since time.Time) ([]DayCount, error)
	DailyNewVisitors(since time.Time) ([]DayCount, error)
	DailyConversions(since time.Time) ([]DayCount, error)
	// VisitorBreakdown groups the visitors first seen on or after since by
	// one of the Breakdown dimensions, largest first, at most limit rows.
	VisitorBreakdown(dimension string, since time.Time, limit int) ([]VisitorBreakdownRow, error)
}

// VisitorDashboard is GET /api/v1/admin/metrics/visitors.
type VisitorDashboard struct {
	GeneratedAt string `json:"generated_at"`
	// VisitorsToday, 7d and 30d count distinct visitors who came while not
	// signed in on any of those days.
	VisitorsToday int `json:"visitors_today"`
	Visitors7d    int `json:"visitors_7d"`
	Visitors30d   int `json:"visitors_30d"`
	// NewVisitors* count visitors first seen in the window.
	NewVisitors7d      int `json:"new_visitors_7d"`
	NewVisitors30d     int `json:"new_visitors_30d"`
	NewVisitorsPrev30d int `json:"new_visitors_prev_30d"`
	// Conversions30d counts the new visitors of the last 30 days who have
	// made an account since; ConversionRate30d is their share of
	// NewVisitors30d. The Prev fields are the 30 days before, for the trend.
	Conversions30d        int     `json:"conversions_30d"`
	ConversionRate30d     float64 `json:"conversion_rate_30d"`
	ConversionsPrev30d    int     `json:"conversions_prev_30d"`
	ConversionRatePrev30d float64 `json:"conversion_rate_prev_30d"`
	// Signups30d counts every account made in the last 30 days and
	// TrackedSignups30d the part a visitor converted into: the rest signed
	// up from a browser that was not counted (privacy signals, blockers) or
	// through an invitation on another device.
	Signups30d        int                   `json:"signups_30d"`
	TrackedSignups30d int                   `json:"tracked_signups_30d"`
	Visitors          []SeriesPoint         `json:"visitors"`
	NewVisitors       []SeriesPoint         `json:"new_visitors"`
	Conversions       []SeriesPoint         `json:"conversions"`
	LandingPages      []VisitorBreakdownRow `json:"landing_pages"`
	Sources           []VisitorBreakdownRow `json:"sources"`
	Countries         []VisitorBreakdownRow `json:"countries"`
}

// VisitorService records visits and builds the visitor dashboard.
type VisitorService interface {
	// RecordVisit never fails the request it is called from: a failed
	// write is logged and retried on the visitor's next page load.
	RecordVisit(v Visit)
	Dashboard() (*VisitorDashboard, error)
}

// DefaultVisitorService implements VisitorService.
type DefaultVisitorService struct {
	repo VisitorRepository
	now  func() time.Time

	// seen holds the beacons already written today ("v|id" for a visit,
	// "l|id|user" for a link), so a busy visitor costs one write a day per
	// server. It is emptied when the day turns, which also bounds it.
	mu      sync.Mutex
	seenDay string
	seen    map[string]struct{}
}

// NewDefaultVisitorService creates the service.
func NewDefaultVisitorService(repo VisitorRepository) *DefaultVisitorService {
	return &DefaultVisitorService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// firstTime reports whether key is new today, and remembers it.
func (s *DefaultVisitorService) firstTime(today, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seenDay != today || s.seen == nil {
		s.seenDay, s.seen = today, map[string]struct{}{}
	}
	if _, ok := s.seen[key]; ok {
		return false
	}
	s.seen[key] = struct{}{}
	return true
}

func (s *DefaultVisitorService) forget(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, key)
}

// RecordVisit implements VisitorService.
func (s *DefaultVisitorService) RecordVisit(v Visit) {
	if v.VisitorID == "" {
		return
	}
	today := day(s.now())
	todayKey := today.Format("2006-01-02")
	if v.UserID != "" {
		key := "l|" + v.VisitorID + "|" + v.UserID
		if !s.firstTime(todayKey, key) {
			return
		}
		if err := s.repo.LinkVisitor(v.VisitorID, v.UserID); err != nil {
			s.forget(key)
			slog.Warn("site visitor not linked", "visitor_id", v.VisitorID, "user_id", v.UserID, "error", err)
		}
		return
	}
	key := "v|" + v.VisitorID
	if !s.firstTime(todayKey, key) {
		return
	}
	if err := s.repo.RecordVisit(v.VisitorID, today, NormalizeLandingPath(v.Path), v.Source, NormalizeCountry(v.Country)); err != nil {
		s.forget(key)
		slog.Warn("site visit not recorded", "visitor_id", v.VisitorID, "error", err)
	}
}

// Dashboard implements VisitorService.
func (s *DefaultVisitorService) Dashboard() (*VisitorDashboard, error) {
	now := s.now()
	today := day(now)
	t, err := s.repo.VisitorTotals(today)
	if err != nil {
		return nil, err
	}
	since := today.AddDate(0, 0, -(SeriesDays - 1))
	visitors, err := s.repo.DailyVisitors(since)
	if err != nil {
		return nil, err
	}
	newVisitors, err := s.repo.DailyNewVisitors(since)
	if err != nil {
		return nil, err
	}
	conversions, err := s.repo.DailyConversions(since)
	if err != nil {
		return nil, err
	}
	windowStart := today.AddDate(0, 0, -(LostAfterDays - 1))
	breakdowns := map[string][]VisitorBreakdownRow{}
	for _, dim := range []string{BreakdownLandingPage, BreakdownSource, BreakdownCountry} {
		rows, err := s.repo.VisitorBreakdown(dim, windowStart, VisitorTopN)
		if err != nil {
			return nil, err
		}
		if rows == nil {
			rows = []VisitorBreakdownRow{}
		}
		breakdowns[dim] = rows
	}
	d := &VisitorDashboard{
		GeneratedAt:        now.UTC().Format(time.RFC3339),
		VisitorsToday:      t.Today,
		Visitors7d:         t.Last7d,
		Visitors30d:        t.Last30d,
		NewVisitors7d:      t.New7d,
		NewVisitors30d:     t.New30d,
		NewVisitorsPrev30d: t.NewPrev30d,
		Conversions30d:     t.Converted30d,
		ConversionsPrev30d: t.ConvertedPrev30d,
		Signups30d:         t.Signups30d,
		TrackedSignups30d:  t.TrackedSignups30d,
		Visitors:           fillSeries(visitors, since, today),
		NewVisitors:        fillSeries(newVisitors, since, today),
		Conversions:        fillSeries(conversions, since, today),
		LandingPages:       breakdowns[BreakdownLandingPage],
		Sources:            breakdowns[BreakdownSource],
		Countries:          breakdowns[BreakdownCountry],
	}
	d.ConversionRate30d = rate(t.Converted30d, t.New30d)
	d.ConversionRatePrev30d = rate(t.ConvertedPrev30d, t.NewPrev30d)
	return d, nil
}

func rate(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// sitePages are the app's public pages, kept as they are.
var sitePages = map[string]bool{
	"/": true, "/pricing": true, "/how-it-works": true, "/demos": true, "/faq": true,
	"/security": true, "/open-source": true, "/customers": true, "/white-papers": true,
	"/login": true, "/manual": true, "/whats-new": true,
}

// tokenPages are the first segments of paths whose rest is a secret or an
// id: they are kept as their template.
var tokenPages = map[string]string{
	"share":          "/share/:token",
	"s":              "/share/:token",
	"interview":      "/interview/:token",
	"open-source":    "/open-source/:id",
	"verify-email":   "/verify-email",
	"reset-password": "/reset-password",
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

// NormalizeLandingPath turns the path a visitor landed on into what is
// stored: a public page as it is, a manual chapter by its slug, a path
// that carries a token or an id as its template, and anything else (the
// signed-in app, which sends a visitor to sign in) as "/app". The query
// and fragment are always dropped.
func NormalizeLandingPath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.ToLower(strings.TrimSpace(p))
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if p == "" {
		p = "/"
	}
	if sitePages[p] {
		return p
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(segs) == 2 && segs[0] == "manual" && slugRE.MatchString(segs[1]) {
		return p
	}
	if t, ok := tokenPages[segs[0]]; ok {
		return t
	}
	return "/app"
}

var sourceRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,59}$`)

// NormalizeSource names where a visitor came from: the utm_source of the
// landing URL when it has a usable one, else the referring site's host
// without "www.", else "" (direct). A referrer on ownHost is direct.
func NormalizeSource(utmSource, referrer, ownHost string) string {
	if s := strings.ToLower(strings.TrimSpace(utmSource)); sourceRE.MatchString(s) {
		return s
	}
	u, err := url.Parse(strings.TrimSpace(referrer))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	own := strings.TrimPrefix(strings.ToLower(hostOnly(ownHost)), "www.")
	if host == "" || host == own || !sourceRE.MatchString(host) {
		return ""
	}
	return host
}

func hostOnly(h string) string {
	if u, err := url.Parse("//" + h); err == nil {
		return u.Hostname()
	}
	return h
}

var botRE = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|preview|headless|lighthouse|pingdom|monitor|python|curl|wget|httpclient|java/|go-http|okhttp|axios|node-fetch|scrapy|phantomjs|facebookexternalhit|embedly|quora link|whatsapp|playwright|puppeteer`)

// IsBot reports whether a user agent is empty or names a crawler, a link
// previewer, a monitor, a headless browser or a script.
func IsBot(userAgent string) bool {
	return strings.TrimSpace(userAgent) == "" || botRE.MatchString(userAgent)
}
