package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/sharelinks"
)

// A share link's expiry closes the link at the instant the client sent,
// whatever offset it was sent with and whatever the database session's
// TimeZone (migration 0051, following #379 bug 4). As a TIMESTAMP column it
// kept the wall clock and dropped the offset: a link sent with +02:00 opened
// two hours past its expiry, and one sent with -05:00 closed five hours
// early. A share link grants anonymous read access, so the late close is
// the one that matters.
func TestAShareLinksExpiryIsTheInstantSent(t *testing.T) {
	f := newEvidenceFixture(t)
	inNewYork(t, f.db)
	repo := NewShareLinkRepository(f.db)
	svc := sharelinks.NewService(repo)
	now := time.Now().Truncate(time.Second) // stored to the microsecond

	// Half an hour ago, written at +02:00: its wall clock is an hour and a
	// half from now in UTC, which the old column kept as the expiry.
	past := now.Add(-30 * time.Minute).In(time.FixedZone("", 2*60*60))
	lapsed, lapsedToken, err := svc.Create(f.projectID, sharelinks.RolePublic, "Lapsed", nil, &past)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Resolve(lapsedToken); err == nil {
		t.Errorf("a link whose +02:00 expiry passed half an hour ago opened (expires_at read back as %v): "+
			"the offset was dropped and the link lived on", got.ExpiresAt)
	}

	// Half an hour from now, written at -05:00: its wall clock is four and a
	// half hours ago in UTC, which the old column closed the link at.
	future := now.Add(30 * time.Minute).In(time.FixedZone("", -5*60*60))
	live, liveToken, err := svc.Create(f.projectID, sharelinks.RoleReviewer, "Live", nil, &future)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Resolve(liveToken)
	if err != nil || got == nil || got.ID != live.ID {
		t.Fatalf("a link whose -05:00 expiry is half an hour away did not open: %+v, %v", got, err)
	}
	sameInstantInUTC(t, "a resolved link's expires_at", got.ExpiresAt, future)

	// The owner's list and a single read answer the same instants, in UTC.
	listed, err := repo.ListByProject(f.projectID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("the project's links: %d (%v)", len(listed), err)
	}
	for _, l := range listed {
		want := map[string]time.Time{lapsed.ID: past, live.ID: future}[l.ID]
		sameInstantInUTC(t, "a listed link's expires_at ("+l.Label+")", l.ExpiresAt, want)
	}
	one, err := repo.Get(lapsed.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameInstantInUTC(t, "a link's expires_at", one.ExpiresAt, past)
}

// An expiry finer than a microsecond is checked as Postgres stores it. Sent
// as 9999-12-31T18:59:59.99999999-05:00 it is in year 9999 in UTC, but the
// column rounds it into 10000-01-01, which no JSON time can hold: the
// owner's list stopped encoding for good, since links are revoked, never
// deleted. The service truncates it to the microsecond first, so the link
// closes no later than sent and the list still answers.
func TestASubMicrosecondExpiryStillLists(t *testing.T) {
	f := newEvidenceFixture(t)
	inNewYork(t, f.db)
	repo := NewShareLinkRepository(f.db)
	svc := sharelinks.NewService(repo)

	var sent struct {
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(`{"expires_at":"9999-12-31T18:59:59.99999999-05:00"}`), &sent); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(f.projectID, sharelinks.RolePublic, "Far", nil, sent.ExpiresAt); err != nil {
		t.Fatalf("a link closing in year 9999 in UTC was refused: %v", err)
	}
	listed, err := svc.List(f.projectID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("the project's links: %d (%v)", len(listed), err)
	}
	if _, err := json.Marshal(listed); err != nil {
		t.Fatalf("the owner's list does not encode, so the Access tab shows no links: %v", err)
	}
	sameInstantInUTC(t, "the listed expiry", listed[0].ExpiresAt, time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC))
}

// Migration 0051 reads the expiries stored before it as the UTC wall clocks
// the app sends and the old reads compared as UTC, not in the session's
// TimeZone, so every existing link closes at the instant it did.
func TestMigration51ReadsStoredExpiriesAsUTC(t *testing.T) {
	db := testDB(t)
	var before []Migration
	for _, m := range migrations {
		if m.Version < 51 {
			before = append(before, m)
		}
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0050: %v", err)
	}
	inNewYork(t, db)

	org, project, link := uuid.New().String(), uuid.New().String(), uuid.New().String()
	for _, stmt := range []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Lab', $2)`, []interface{}{org, "lab-" + org[:8]}},
		{`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Cabinet')`, []interface{}{project, org}},
		{`INSERT INTO project_share_links (id, project_id, token_hash, role, expires_at) VALUES ($1, $2, 'h', 'public', '2026-01-15 09:30:00')`,
			[]interface{}{link, project}},
	} {
		if _, err := db.Exec(stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed %q: %v", stmt.sql, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := NewShareLinkRepository(db).Get(link)
	if err != nil {
		t.Fatal(err)
	}
	sameInstantInUTC(t, "a stored link's expires_at after the migration", got.ExpiresAt, time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC))
}
