package usermetrics

import (
	"errors"
	"testing"
	"time"
)

type fakeRepo struct {
	writes  []string
	failing bool
	sizes   []DayCount
	cells   []CohortCell
}

func (f *fakeRepo) RecordActivity(userID string, day time.Time, country string) error {
	if f.failing {
		return errors.New("down")
	}
	f.writes = append(f.writes, userID+"|"+day.Format("2006-01-02")+"|"+country)
	return nil
}
func (f *fakeRepo) UserTotals(time.Time) (UserTotals, error) { return UserTotals{Total: 10}, nil }
func (f *fakeRepo) ActivityTotals(time.Time) (ActivityTotals, error) {
	return ActivityTotals{DAU: 2, WAU: 4, MAU: 8, Lost: 2}, nil
}
func (f *fakeRepo) DailySignups(time.Time) ([]DayCount, error) {
	return []DayCount{{Day: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Count: 3}}, nil
}
func (f *fakeRepo) DailyActive(time.Time) ([]DayCount, error)      { return nil, nil }
func (f *fakeRepo) AuthProviders() ([]NamedCount, error)           { return nil, nil }
func (f *fakeRepo) CohortSizes(time.Time) ([]DayCount, error)      { return f.sizes, nil }
func (f *fakeRepo) CohortActivity(time.Time) ([]CohortCell, error) { return f.cells, nil }
func (f *fakeRepo) Countries(time.Time) ([]CountryCount, error) {
	return []CountryCount{{Country: "DE", Users: 1}, {Country: "GB", Users: 3}}, nil
}

func newService(repo Repository, now time.Time) *DefaultService {
	s := NewDefaultService(repo)
	s.now = func() time.Time { return now }
	return s
}

func TestNormalizeCountry(t *testing.T) {
	for in, want := range map[string]string{"gb": "GB", " US ": "US", "XX": "", "T1": "", "": "", "USA": "", "1A": ""} {
		if got := NormalizeCountry(in); got != want {
			t.Errorf("NormalizeCountry(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRecordActivityWritesOncePerDayUnlessCountryLearned(t *testing.T) {
	repo := &fakeRepo{}
	s := newService(repo, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC))
	s.RecordActivity("u1", "")
	s.RecordActivity("u1", "")
	s.RecordActivity("u1", "gb")
	s.RecordActivity("u1", "GB")
	s.RecordActivity("u1", "")
	s.now = func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }
	s.RecordActivity("u1", "")
	want := []string{"u1|2026-10-10|", "u1|2026-10-10|GB", "u1|2026-10-11|"}
	if len(repo.writes) != len(want) {
		t.Fatalf("writes = %v, want %v", repo.writes, want)
	}
	for i := range want {
		if repo.writes[i] != want[i] {
			t.Errorf("write %d = %q, want %q", i, repo.writes[i], want[i])
		}
	}
}

func TestRecordActivityRetriesAfterFailure(t *testing.T) {
	repo := &fakeRepo{failing: true}
	s := newService(repo, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC))
	s.RecordActivity("u1", "")
	repo.failing = false
	s.RecordActivity("u1", "")
	if len(repo.writes) != 1 {
		t.Fatalf("writes = %v, want one after the failure", repo.writes)
	}
}

func TestDashboard(t *testing.T) {
	repo := &fakeRepo{
		sizes: []DayCount{{Day: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Count: 4}},
		cells: []CohortCell{
			{Cohort: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Month: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Users: 4},
			{Cohort: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Month: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Users: 1},
		},
	}
	d, err := newService(repo, time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)).Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if d.Stickiness != 0.25 {
		t.Errorf("stickiness = %v, want 0.25", d.Stickiness)
	}
	if len(d.Signups) != SeriesDays || d.Signups[0].Day != "2026-07-13" || d.Signups[SeriesDays-1].Day != "2026-10-10" {
		t.Errorf("signups span %s..%s (%d days)", d.Signups[0].Day, d.Signups[len(d.Signups)-1].Day, len(d.Signups))
	}
	if d.Signups[SeriesDays-2].Count != 3 {
		t.Errorf("signups on 2026-10-09 = %d, want 3", d.Signups[SeriesDays-2].Count)
	}
	if d.Countries[0].Country != "GB" || d.UnknownCountry != 6 {
		t.Errorf("countries = %+v, unknown %d", d.Countries, d.UnknownCountry)
	}
	if len(d.Cohorts) != CohortMonths || d.Cohorts[0].Month != "2026-05" {
		t.Fatalf("cohorts = %+v", d.Cohorts)
	}
	sep := d.Cohorts[4]
	if sep.Month != "2026-09" || sep.Size != 4 || len(sep.Retained) != 2 || sep.Retained[0] != 1 || sep.Retained[1] != 0.25 {
		t.Errorf("September cohort = %+v", sep)
	}
	if oct := d.Cohorts[5]; len(oct.Retained) != 1 || oct.Retained[0] != 0 {
		t.Errorf("October cohort = %+v", oct)
	}
}
