package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// shareOne publishes a product through the service, which is the only path a
// real caller has (sanitization and limits included).
func shareOne(t *testing.T, svc *sharedproducts.DefaultService, name, orgID, userID string) *sharedproducts.Product {
	t.Helper()
	p, err := svc.Publish(sharedproducts.Product{
		Category:    "kitchen appliance",
		Name:        name,
		Description: "A coffee tin that recognises Kevin and locks.",
		Vision:      name + " becomes the reason the bean jar survives a Tuesday.",
		Problem:     "Beans vanish overnight and nobody admits to owning the grinder.",
		TargetUsers: "office workers whose beans keep leaving with Kevin",
	}, orgID, userID)
	if err != nil {
		t.Fatalf("publish %q: %v", name, err)
	}
	return p
}

// TestSharedProductPoolIsCrossTenant is the shape of the feature: two
// different workspaces publish, and each sees the other's product. This is
// the one table where that is correct, so it is worth asserting rather than
// assuming.
func TestSharedProductPoolIsCrossTenant(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	svc := sharedproducts.NewDefaultService(NewSharedProductRepository(db), 0, 0)

	orgA, orgB := uuid.New().String(), uuid.New().String()
	shareOne(t, svc, "Kevinproof", orgA, uuid.New().String())
	shareOne(t, svc, "Crustodian", orgB, uuid.New().String())

	pool, err := svc.List(sharedproducts.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, p := range pool {
		names[p.Name] = true
		// Nothing published crosses a tenant boundary except the joke itself.
		if p.CreatedByOrg != "" || p.CreatedByUser != "" {
			t.Errorf("listing exposed author metadata: %+v", p)
		}
	}
	if !names["Kevinproof"] || !names["Crustodian"] {
		t.Errorf("pool = %v, want both workspaces' products", names)
	}
}

// TestSharedProductDedupeAndRateLimit covers the two SQL-backed guards: the
// unique name key (so one product cannot be flooded in under a hundred
// spellings) and the per-workspace daily allowance.
func TestSharedProductDedupeAndRateLimit(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 3, 0)

	orgID, userID := uuid.New().String(), uuid.New().String()
	shareOne(t, svc, "Kevinproof", orgID, userID)

	// A cosmetic respelling normalizes to the same key and is refused.
	_, err := svc.Publish(sharedproducts.Product{
		Category: "kitchen appliance", Name: "kevin-proof!",
		Description: "A different tin that also locks.",
		Vision:      "Kevin-proof! becomes something else entirely.",
		Problem:     "Beans still vanish overnight, differently.",
		TargetUsers: "office workers who suspect a second Kevin",
	}, orgID, userID)
	if !errors.Is(err, sharedproducts.ErrDuplicate) {
		t.Errorf("respelling error = %v, want ErrDuplicate", err)
	}

	// Fill the workspace's allowance (3), then the next publish is refused.
	shareOne(t, svc, "Crustodian", orgID, userID)
	shareOne(t, svc, "Loafwatch", orgID, userID)
	if _, err := svc.Publish(sharedproducts.Product{
		Category: "toy", Name: "Fourthing",
		Description: "A toy that counts to four.",
		Vision:      "Fourthing becomes the fourth thing.",
		Problem:     "Nobody counts past three.",
		TargetUsers: "children who lose interest immediately after three",
	}, orgID, userID); !errors.Is(err, sharedproducts.ErrRateLimited) {
		t.Errorf("over the daily cap: %v, want ErrRateLimited", err)
	}

	// The cap is per workspace: a different one still publishes.
	shareOne(t, svc, "Fourthing", uuid.New().String(), uuid.New().String())

	// Hidden rows still count against the publisher's allowance, so taking
	// something down does not buy a fresh slot.
	count, err := repo.CountByOrgSince(orgID, time.Now().Add(-24*time.Hour))
	if err != nil || count != 3 {
		t.Errorf("CountByOrgSince = %d, %v; want 3", count, err)
	}
}

// TestSharedProductReportingAndTakedown: distinct reporters hide an entry for
// everyone, one account cannot, and an admin delete removes it along with its
// reports.
func TestSharedProductReportingAndTakedown(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 0, 0)

	product := shareOne(t, svc, "Kevinproof", uuid.New().String(), uuid.New().String())
	loud := uuid.New().String()

	// One account, many clicks: the entry stays visible to everyone.
	for i := 0; i < sharedproducts.ReportsToHide*2; i++ {
		if err := svc.Report(product.ID, loud); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	pool, _ := svc.List(sharedproducts.ListOptions{})
	if len(pool) != 1 {
		t.Fatalf("one account hid an entry by reporting repeatedly (pool = %d)", len(pool))
	}

	// Distinct reporters do hide it.
	for i := 1; i < sharedproducts.ReportsToHide; i++ {
		if err := svc.Report(product.ID, uuid.New().String()); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	if pool, _ = svc.List(sharedproducts.ListOptions{}); len(pool) != 0 {
		t.Errorf("entry still visible after %d distinct reporters", sharedproducts.ReportsToHide)
	}

	// Admin takedown removes the row (and its reports cascade).
	if err := svc.Delete(product.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var reports int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shared_product_reports WHERE product_id = $1`, product.ID).Scan(&reports); err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if reports != 0 {
		t.Errorf("%d reports outlived the deleted product", reports)
	}
	if err := svc.Delete(product.ID); !errors.Is(err, sharedproducts.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
	if err := svc.Report(product.ID, loud); !errors.Is(err, sharedproducts.ErrNotFound) {
		t.Errorf("report on a deleted product = %v, want ErrNotFound", err)
	}
}

// TestSharedProductVotingIsPerPerson: one account is one vote however often
// it presses, withdrawing gives the vote back, and the denormalised column
// the leaderboard orders by never drifts from the rows it summarises.
func TestSharedProductVotingIsPerPerson(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 0, 0)

	product := shareOne(t, svc, "Kevinproof", uuid.New().String(), uuid.New().String())
	keen, quiet := uuid.New().String(), uuid.New().String()

	// One account, many clicks: one vote.
	for i := 0; i < 4; i++ {
		counts, err := svc.Vote(product.ID, keen)
		if err != nil {
			t.Fatalf("vote: %v", err)
		}
		if counts.Votes != 1 || counts.VotesWeek != 1 || !counts.Voted {
			t.Fatalf("press %d = %+v, want 1/1/voted", i, counts)
		}
	}
	if counts, err := svc.Vote(product.ID, quiet); err != nil || counts.Votes != 2 {
		t.Errorf("second voter = %+v, %v; want 2 votes", counts, err)
	}

	// The stored column matches the rows, which is what the top ordering
	// reads without touching the vote table.
	assertVoteColumn(t, db, product.ID, 2)

	// Withdrawing is idempotent in the same way.
	for i := 0; i < 3; i++ {
		counts, err := svc.Unvote(product.ID, keen)
		if err != nil {
			t.Fatalf("unvote: %v", err)
		}
		if counts.Votes != 1 || counts.Voted {
			t.Fatalf("unvote %d = %+v, want 1 vote and voted=false", i, counts)
		}
	}
	assertVoteColumn(t, db, product.ID, 1)

	// Each caller sees their own vote and nobody else's.
	pool, err := svc.List(sharedproducts.ListOptions{ViewerID: quiet})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pool) != 1 || !pool[0].Voted || pool[0].Votes != 1 || pool[0].VotesWeek != 1 {
		t.Errorf("voter's view = %+v", pool[0])
	}
	if pool, err = svc.List(sharedproducts.ListOptions{ViewerID: keen}); err != nil || pool[0].Voted {
		t.Errorf("withdrawn voter still sees voted=true: %+v, %v", pool[0], err)
	}
	// A caller with no session user (a workspace runner key) reads the counts
	// but never carries a vote of their own.
	if pool, err = svc.List(sharedproducts.ListOptions{}); err != nil || pool[0].Voted || pool[0].Votes != 1 {
		t.Errorf("anonymous view = %+v, %v", pool[0], err)
	}

	// A deleted product's votes go with it, and it can no longer be voted on.
	if err := svc.Delete(product.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var votes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shared_product_votes WHERE product_id = $1`, product.ID).Scan(&votes); err != nil {
		t.Fatalf("count votes: %v", err)
	}
	if votes != 0 {
		t.Errorf("%d votes outlived the deleted product", votes)
	}
	if _, err := svc.Vote(product.ID, quiet); !errors.Is(err, sharedproducts.ErrNotFound) {
		t.Errorf("vote on a deleted product = %v, want ErrNotFound", err)
	}
}

// TestSharedProductVotesExcludeHidden: an entry reports have hidden is out of
// every list and out of reach of a vote — nothing can quietly climb a
// leaderboard it is not eligible for.
func TestSharedProductVotesExcludeHidden(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 0, 0)

	hidden := shareOne(t, svc, "Kevinproof", uuid.New().String(), uuid.New().String())
	visible := shareOne(t, svc, "Crustodian", uuid.New().String(), uuid.New().String())

	// Vote for both while they are visible, then hide one.
	for i := 0; i < sharedproducts.ReportsToHide; i++ {
		voter := uuid.New().String()
		if _, err := svc.Vote(hidden.ID, voter); err != nil {
			t.Fatalf("vote: %v", err)
		}
		if err := svc.Report(hidden.ID, voter); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	if _, err := svc.Vote(visible.ID, uuid.New().String()); err != nil {
		t.Fatalf("vote: %v", err)
	}

	for _, sort := range []sharedproducts.Sort{sharedproducts.SortRecent, sharedproducts.SortTop, sharedproducts.SortTopWeek} {
		pool, err := svc.List(sharedproducts.ListOptions{Sort: sort})
		if err != nil {
			t.Fatalf("list %s: %v", sort, err)
		}
		for _, p := range pool {
			if p.ID == hidden.ID {
				t.Errorf("sort %s listed a hidden entry with %d votes", sort, p.Votes)
			}
		}
	}
	if _, err := svc.Vote(hidden.ID, uuid.New().String()); !errors.Is(err, sharedproducts.ErrNotFound) {
		t.Errorf("vote on a hidden entry = %v, want ErrNotFound", err)
	}
}

// TestSharedProductTopSorts is the leaderboard contract: most votes first,
// the weekly list counting only the rolling window, and nothing unvoted
// dressed up as "top".
func TestSharedProductTopSorts(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 0, 0)

	// Nothing is voted for yet, so both leaderboards are empty rather than a
	// second random list wearing a filter's name.
	old := shareOne(t, svc, "Kevinproof", uuid.New().String(), uuid.New().String())
	fresh := shareOne(t, svc, "Crustodian", uuid.New().String(), uuid.New().String())
	shareOne(t, svc, "Loafwatch", uuid.New().String(), uuid.New().String())
	for _, sort := range []sharedproducts.Sort{sharedproducts.SortTop, sharedproducts.SortTopWeek} {
		if pool, err := svc.List(sharedproducts.ListOptions{Sort: sort}); err != nil || len(pool) != 0 {
			t.Errorf("unvoted %s = %d rows, %v; want none", sort, len(pool), err)
		}
	}

	// The old favourite: three votes, all of them cast a month ago.
	for i := 0; i < 3; i++ {
		if _, err := svc.Vote(old.ID, uuid.New().String()); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}
	if _, err := db.Exec(
		`UPDATE shared_product_votes SET created_at = NOW() - INTERVAL '30 days' WHERE product_id = $1`, old.ID,
	); err != nil {
		t.Fatalf("age the votes: %v", err)
	}
	// This week's favourite: two votes, both just now.
	for i := 0; i < 2; i++ {
		if _, err := svc.Vote(fresh.ID, uuid.New().String()); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}

	// All time counts every vote ever cast.
	top, err := svc.List(sharedproducts.ListOptions{Sort: sharedproducts.SortTop, Limit: 5})
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(top) != 2 || top[0].ID != old.ID || top[0].Votes != 3 || top[1].ID != fresh.ID {
		t.Errorf("top = %v", describePool(top))
	}
	// The weekly window excludes the month-old votes, so the order flips and
	// the old favourite drops out entirely.
	week, err := svc.List(sharedproducts.ListOptions{Sort: sharedproducts.SortTopWeek, Limit: 5})
	if err != nil {
		t.Fatalf("top_week: %v", err)
	}
	if len(week) != 1 || week[0].ID != fresh.ID || week[0].VotesWeek != 2 || week[0].Votes != 2 {
		t.Errorf("top_week = %v", describePool(week))
	}
	// The all-time list still reports the old favourite's weekly count as
	// zero rather than repeating its total.
	if top[0].VotesWeek != 0 {
		t.Errorf("month-old votes counted as this week's: %+v", top[0])
	}

	// The default ordering is untouched: newest first, every row visible,
	// vote counts carried along.
	recent, err := svc.List(sharedproducts.ListOptions{})
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 3 || recent[0].Name != "Loafwatch" {
		t.Errorf("recent = %v", describePool(recent))
	}
	if recent[2].ID != old.ID || recent[2].Votes != 3 {
		t.Errorf("recent list lost the vote counts: %v", describePool(recent))
	}

	// A limit applies to the leaderboard too.
	if one, err := svc.List(sharedproducts.ListOptions{Sort: sharedproducts.SortTop, Limit: 1}); err != nil || len(one) != 1 || one[0].ID != old.ID {
		t.Errorf("top limit 1 = %v, %v", describePool(one), err)
	}
}

// assertVoteColumn checks the denormalised count against the expected number
// of vote rows.
func assertVoteColumn(t *testing.T, db *sql.DB, id string, want int) {
	t.Helper()
	var stored, rows int
	if err := db.QueryRow(
		`SELECT p.votes, (SELECT COUNT(*) FROM shared_product_votes v WHERE v.product_id = p.id)
		 FROM shared_products p WHERE p.id = $1`, id,
	).Scan(&stored, &rows); err != nil {
		t.Fatalf("read votes: %v", err)
	}
	if stored != want || rows != want {
		t.Errorf("votes column = %d, vote rows = %d; want %d of each", stored, rows, want)
	}
}

// describePool renders a pool as name/votes pairs for a failure message.
func describePool(pool []*sharedproducts.Product) []string {
	out := []string{}
	for _, p := range pool {
		out = append(out, fmt.Sprintf("%s(all=%d week=%d)", p.Name, p.Votes, p.VotesWeek))
	}
	return out
}
