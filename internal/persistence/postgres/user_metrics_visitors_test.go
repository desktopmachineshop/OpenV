package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/usermetrics"
)

// A visitor converts when the account it is linked to was made after it
// was first seen: a sign-in on a new browser by an older account does not
// count, and an account made with no visitor is a sign-up nobody tracked.
func TestVisitorConversions(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserMetricsRepository(db)
	today := time.Now().UTC()
	yesterday := today.AddDate(0, 0, -1)

	user := func(age time.Duration) string {
		id := uuid.NewString()
		if _, err := db.Exec(`INSERT INTO users (id, email, name, auth_provider, created_at, updated_at)
			VALUES ($1, $2, 'U', 'password', NOW() - $3::interval, NOW())`, id, id+"@example.com", age.String()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	converted, browsing, newLaptop := uuid.NewString(), uuid.NewString(), uuid.NewString()
	old := user(100 * 24 * time.Hour)

	must(repo.RecordVisit(browsing, yesterday, "/", "", "GB"))
	must(repo.RecordVisit(browsing, today, "/faq", "ignored", "US"))
	must(repo.RecordVisit(converted, today, "/pricing", "news.ycombinator.com", "DE"))
	must(repo.RecordVisit(converted, today, "/pricing", "news.ycombinator.com", "DE"))
	must(repo.RecordVisit(newLaptop, today, "/", "", ""))
	signedUp := user(0)
	untracked := user(0)
	must(repo.LinkVisitor(converted, signedUp))
	must(repo.LinkVisitor(converted, untracked)) // the first account wins
	must(repo.LinkVisitor(newLaptop, old))
	must(repo.LinkVisitor(uuid.NewString(), old)) // never recorded: nothing to link

	tot, err := repo.VisitorTotals(today)
	must(err)
	want := usermetrics.VisitorTotals{Today: 3, Last7d: 3, Last30d: 3, New7d: 3, New30d: 3,
		Converted30d: 1, Signups30d: 2, TrackedSignups30d: 1}
	if tot != want {
		t.Errorf("totals = %+v\nwant     %+v", tot, want)
	}

	daily, err := repo.DailyVisitors(yesterday)
	must(err)
	if len(daily) != 2 || daily[0].Count != 1 || daily[1].Count != 3 {
		t.Errorf("daily visitors = %+v, want 1 yesterday and 3 today", daily)
	}
	conv, err := repo.DailyConversions(yesterday)
	must(err)
	if len(conv) != 1 || conv[0].Count != 1 {
		t.Errorf("daily conversions = %+v", conv)
	}

	pages, err := repo.VisitorBreakdown(usermetrics.BreakdownLandingPage, yesterday, 10)
	must(err)
	wantPages := []usermetrics.VisitorBreakdownRow{{Name: "/", Visitors: 2}, {Name: "/pricing", Visitors: 1, Conversions: 1}}
	if len(pages) != 2 || pages[0] != wantPages[0] || pages[1] != wantPages[1] {
		t.Errorf("landing pages = %+v, want %+v (the first visit's page, kept)", pages, wantPages)
	}
	countries, err := repo.VisitorBreakdown(usermetrics.BreakdownCountry, yesterday, 10)
	must(err)
	if len(countries) != 3 || countries[0].Name != "" {
		t.Errorf("countries = %+v", countries)
	}
	if _, err := repo.VisitorBreakdown("user_id", yesterday, 10); err == nil {
		t.Error("an unknown breakdown was answered")
	}

	// A deleted account leaves its visitor behind, unlinked.
	must(func() error { _, err := db.Exec(`DELETE FROM users WHERE id = $1`, signedUp); return err }())
	tot, err = repo.VisitorTotals(today)
	must(err)
	if tot.Converted30d != 0 || tot.New30d != 3 {
		t.Errorf("after deleting the account: %+v", tot)
	}
}
