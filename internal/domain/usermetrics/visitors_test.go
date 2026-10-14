package usermetrics

import (
	"errors"
	"testing"
	"time"
)

type fakeVisitorRepo struct {
	writes  []string
	failing bool
	totals  VisitorTotals
}

func (f *fakeVisitorRepo) RecordVisit(id string, day time.Time, path, source, country string) error {
	if f.failing {
		return errors.New("down")
	}
	f.writes = append(f.writes, "visit|"+id+"|"+day.Format("2006-01-02")+"|"+path+"|"+source+"|"+country)
	return nil
}
func (f *fakeVisitorRepo) LinkVisitor(id, userID string) error {
	if f.failing {
		return errors.New("down")
	}
	f.writes = append(f.writes, "link|"+id+"|"+userID)
	return nil
}
func (f *fakeVisitorRepo) VisitorTotals(time.Time) (VisitorTotals, error) { return f.totals, nil }
func (f *fakeVisitorRepo) DailyVisitors(time.Time) ([]DayCount, error) {
	return []DayCount{{Day: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Count: 5}}, nil
}
func (f *fakeVisitorRepo) DailyNewVisitors(time.Time) ([]DayCount, error) { return nil, nil }
func (f *fakeVisitorRepo) DailyConversions(time.Time) ([]DayCount, error) { return nil, nil }
func (f *fakeVisitorRepo) VisitorBreakdown(dim string, _ time.Time, _ int) ([]VisitorBreakdownRow, error) {
	if dim == BreakdownSource {
		return []VisitorBreakdownRow{{Name: "news.ycombinator.com", Visitors: 4, Conversions: 1}}, nil
	}
	return nil, nil
}

func newVisitorService(repo VisitorRepository, now time.Time) *DefaultVisitorService {
	s := NewDefaultVisitorService(repo)
	s.now = func() time.Time { return now }
	return s
}

func TestNormalizeLandingPathKeepsNoTokens(t *testing.T) {
	for in, want := range map[string]string{
		"":                         "/",
		"/":                        "/",
		"/pricing?plan=business":   "/pricing",
		"/FAQ/":                    "/faq",
		"/manual/getting-started":  "/manual/getting-started",
		"/manual/a/b":              "/app",
		"/share/abc123secret":      "/share/:token",
		"/s/abc123secret":          "/share/:token",
		"/interview/tok#x":         "/interview/:token",
		"/open-source/42":          "/open-source/:id",
		"/reset-password?token=zz": "/reset-password",
		"/projects/123/vv":         "/app",
	} {
		if got := NormalizeLandingPath(in); got != want {
			t.Errorf("NormalizeLandingPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeSource(t *testing.T) {
	cases := []struct{ utm, ref, own, want string }{
		{"LinkedIn", "https://google.com/", "openv.app", "linkedin"},
		{"", "https://www.Google.com/search?q=openv", "openv.app", "google.com"},
		{"", "https://openv.app/pricing", "openv.app", ""},
		{"", "https://www.openv.app/", "openv.app:443", ""},
		{"", "android-app://com.slack", "openv.app", ""},
		{"", "", "openv.app", ""},
		{"bad source!", "", "openv.app", ""},
	}
	for _, c := range cases {
		if got := NormalizeSource(c.utm, c.ref, c.own); got != c.want {
			t.Errorf("NormalizeSource(%q, %q, %q) = %q, want %q", c.utm, c.ref, c.own, got, c.want)
		}
	}
}

func TestIsBot(t *testing.T) {
	for ua, want := range map[string]bool{
		"": true,
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":                    true,
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/120.0": true,
		"curl/8.4.0": true,
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15": false,
	} {
		if got := IsBot(ua); got != want {
			t.Errorf("IsBot(%q) = %v, want %v", ua, got, want)
		}
	}
}

func TestRecordVisitWritesOncePerDayAndLinksSeparately(t *testing.T) {
	repo := &fakeVisitorRepo{}
	s := newVisitorService(repo, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC))
	s.RecordVisit(Visit{VisitorID: "v1", Path: "/pricing?x=1", Source: "google.com", Country: "gb"})
	s.RecordVisit(Visit{VisitorID: "v1", Path: "/faq"})
	s.RecordVisit(Visit{VisitorID: "v1", UserID: "u1"})
	s.RecordVisit(Visit{VisitorID: "v1", UserID: "u1"})
	s.RecordVisit(Visit{VisitorID: ""})
	s.now = func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }
	s.RecordVisit(Visit{VisitorID: "v1", Path: "/"})
	want := []string{
		"visit|v1|2026-10-10|/pricing|google.com|GB",
		"link|v1|u1",
		"visit|v1|2026-10-11|/||",
	}
	if len(repo.writes) != len(want) {
		t.Fatalf("writes = %v, want %v", repo.writes, want)
	}
	for i := range want {
		if repo.writes[i] != want[i] {
			t.Errorf("write %d = %q, want %q", i, repo.writes[i], want[i])
		}
	}
}

func TestRecordVisitRetriesAfterFailure(t *testing.T) {
	repo := &fakeVisitorRepo{failing: true}
	s := newVisitorService(repo, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC))
	s.RecordVisit(Visit{VisitorID: "v1"})
	s.RecordVisit(Visit{VisitorID: "v1", UserID: "u1"})
	repo.failing = false
	s.RecordVisit(Visit{VisitorID: "v1"})
	s.RecordVisit(Visit{VisitorID: "v1", UserID: "u1"})
	if len(repo.writes) != 2 {
		t.Fatalf("writes = %v, want the visit and the link after the failure", repo.writes)
	}
}

func TestVisitorDashboard(t *testing.T) {
	repo := &fakeVisitorRepo{totals: VisitorTotals{Last30d: 120, New30d: 100, Converted30d: 7, NewPrev30d: 0, Signups30d: 9, TrackedSignups30d: 7}}
	d, err := newVisitorService(repo, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)).Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if d.ConversionRate30d != 0.07 || d.ConversionRatePrev30d != 0 {
		t.Errorf("rates = %v, %v; want 0.07 and 0 with no visitors before", d.ConversionRate30d, d.ConversionRatePrev30d)
	}
	if len(d.Visitors) != SeriesDays || d.Visitors[SeriesDays-2].Count != 5 || d.Visitors[SeriesDays-1].Day != "2026-10-10" {
		t.Errorf("visitor series not filled to today: %d points, last %v", len(d.Visitors), d.Visitors[len(d.Visitors)-1])
	}
	if d.LandingPages == nil || d.Countries == nil || len(d.Sources) != 1 {
		t.Errorf("breakdowns = %v / %v / %v; want empty lists, not null, and the one source", d.LandingPages, d.Countries, d.Sources)
	}
	if d.Signups30d != 9 || d.TrackedSignups30d != 7 {
		t.Errorf("signups = %d tracked %d", d.Signups30d, d.TrackedSignups30d)
	}
}
