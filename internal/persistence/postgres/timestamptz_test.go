package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// inNewYork makes every later connection of db run in a session whose
// TimeZone is not UTC, as TestUsageDayBucketsAreUTC does, so a test shows
// both that a time keeps its instant and that it answers in UTC anyway.
func inNewYork(t *testing.T, db *sql.DB) {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`ALTER DATABASE %q SET timezone TO 'America/New_York'`, name)); err != nil {
		t.Fatal(err)
	}
	db.SetMaxIdleConns(0)
	var zone string
	if err := db.QueryRow(`SHOW timezone`).Scan(&zone); err != nil || zone != "America/New_York" {
		t.Fatalf("session time zone %q (%v), want America/New_York: the test would prove nothing", zone, err)
	}
}

// sameInstantInUTC fails unless got is want's instant, in UTC.
func sameInstantInUTC(t *testing.T, what string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil || !got.Equal(want) {
		t.Errorf("%s read back as %v, want the instant sent, %v (%s)", what, got, want, want.UTC().Format(time.RFC3339))
		return
	}
	if got.Location() != time.UTC {
		t.Errorf("%s read back in %v, want UTC like every other time the API answers", what, got.Location())
	}
}

// A time a client sends with an offset is stored as the instant it names
// and read back as that instant, in UTC, whatever the database session's
// TimeZone: an evidence bundle's captured_at, a work item's due_date, an
// interview invite's and a workspace invitation's expires_at (#379 bug 4,
// migration 0050). As TIMESTAMP columns they dropped the offset and kept
// the wall clock, so the instant moved by the offset.
func TestAnOffsetTimeReadsBackAsTheSameInstant(t *testing.T) {
	f := newEvidenceFixture(t)
	inNewYork(t, f.db)
	sent := time.Date(2026, 9, 12, 10, 0, 0, 0, time.FixedZone("", 2*60*60)) // 08:00 UTC
	now := time.Now().UTC()

	bundle := &evidence.Bundle{ID: uuid.New().String(), ProjectID: f.projectID, Title: "Sweep",
		CapturedAt: &sent, Conditions: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now}
	if err := f.repo.Create(bundle); err != nil {
		t.Fatal(err)
	}
	stored, err := f.repo.FindByID(bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameInstantInUTC(t, "a bundle's captured_at", stored.CapturedAt, sent)
	listed, err := f.repo.ListByProject(f.projectID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("the project's bundles: %d (%v)", len(listed), err)
	}
	sameInstantInUTC(t, "a listed bundle's captured_at", listed[0].CapturedAt, sent)

	items := NewWorkItemRepository(f.db)
	item := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: f.projectID, Title: "Load test",
		Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser, DueDate: &sent, CreatedAt: now, UpdatedAt: now}
	if err := items.Save(item); err != nil {
		t.Fatal(err)
	}
	gotItem, err := items.FindByID(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameInstantInUTC(t, "a work item's due_date", gotItem.DueDate, sent)

	ivs := NewInterviewRepository(f.db)
	iv := &interviews.Interview{ID: uuid.New().String(), ProjectID: f.projectID, Name: "Users",
		Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := ivs.SaveInterview(iv); err != nil {
		t.Fatal(err)
	}
	invite := &interviews.Invite{ID: uuid.New().String(), InterviewID: iv.ID, TokenHash: uuid.New().String(),
		InviteeLabel: "Next year", ExpiresAt: &sent, CreatedAt: now}
	if err := ivs.SaveInvite(invite); err != nil {
		t.Fatal(err)
	}
	gotInvite, err := ivs.FindInviteByID(invite.ID)
	if err != nil || gotInvite == nil {
		t.Fatalf("read the invite: %v", err)
	}
	sameInstantInUTC(t, "an interview invite's expires_at", gotInvite.ExpiresAt, sent)

	invs := NewInvitationRepository(f.db)
	inv := &invitations.Invitation{ID: uuid.New().String(), OrgID: f.orgID, Email: "dana@example.com",
		Role: "member", TokenHash: uuid.New().String(), ExpiresAt: sent, CreatedAt: now}
	if err := invs.Replace(inv); err != nil {
		t.Fatal(err)
	}
	sameInstantInUTC(t, "a stored workspace invitation's expires_at", &inv.ExpiresAt, sent)
	gotInv, err := invs.FindByID(inv.ID)
	if err != nil || gotInv == nil {
		t.Fatalf("read the invitation: %v", err)
	}
	sameInstantInUTC(t, "a workspace invitation's expires_at", &gotInv.ExpiresAt, sent)
	// The expiry compares as the instant: live a minute before it, not a
	// minute after, where a wall clock two hours late was still live.
	if pending, err := invs.FindPending(f.orgID, "dana@example.com", sent.Add(-time.Minute)); err != nil || pending == nil {
		t.Errorf("the invitation a minute before it expires: %v, %v; want it pending", pending, err)
	}
	if pending, err := invs.FindPending(f.orgID, "dana@example.com", sent.Add(time.Minute)); err != nil || pending != nil {
		t.Errorf("the invitation a minute after it expires: %+v, %v; want none", pending, err)
	}
}

// Migration 0050 reads the times stored before it as the UTC wall clocks the
// server wrote, not in the session's TimeZone, and the evidence list still
// orders a bundle with no capture time by when it was written.
func TestMigration50ReadsStoredTimesAsUTC(t *testing.T) {
	db := testDB(t)
	var before []Migration
	for _, m := range migrations {
		if m.Version < 50 {
			before = append(before, m)
		}
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0049: %v", err)
	}
	inNewYork(t, db)

	org, project, interview := uuid.New().String(), uuid.New().String(), uuid.New().String()
	const wall = "2026-01-15 09:30:00"
	want := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	bundles := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}
	for _, stmt := range []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Lab', $2)`, []interface{}{org, "lab-" + org[:8]}},
		{`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Cabinet')`, []interface{}{project, org}},
		// Captured at 09:30 (written at 09:00), and two with no capture time
		// written at 09:15 and 10:00: newest first, 10:00, the capture, 09:15.
		// Under a New York session an implicit cast of created_at would read
		// those two as 14:15 and 15:00 UTC, after the capture.
		{`INSERT INTO evidence_bundles (id, project_id, ref, title, captured_at, created_at, updated_at)
			VALUES ($1, $2, 'EVD-1', 'Captured', $3, '2026-01-15 09:00:00', '2026-01-15 09:00:00')`,
			[]interface{}{bundles[1], project, wall}},
		{`INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at)
			VALUES ($1, $2, 'EVD-2', 'Written first', '2026-01-15 09:15:00', '2026-01-15 09:15:00')`,
			[]interface{}{bundles[2], project}},
		{`INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at)
			VALUES ($1, $2, 'EVD-3', 'Written last', '2026-01-15 10:00:00', '2026-01-15 10:00:00')`,
			[]interface{}{bundles[0], project}},
		{`INSERT INTO work_items (id, project_id, title, due_date) VALUES ($1, $2, 'Due', $3)`,
			[]interface{}{uuid.New().String(), project, wall}},
		{`INSERT INTO interviews (id, project_id, name) VALUES ($1, $2, 'Users')`, []interface{}{interview, project}},
		{`INSERT INTO interview_invites (id, interview_id, token_hash, expires_at) VALUES ($1, $2, 'h', $3)`,
			[]interface{}{uuid.New().String(), interview, wall}},
		{`INSERT INTO org_invitations (id, org_id, email, role, token_hash, expires_at) VALUES ($1, $2, 'a@example.com', 'member', 'h', $3)`,
			[]interface{}{uuid.New().String(), org, wall}},
	} {
		if _, err := db.Exec(stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed %q: %v", stmt.sql, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, col := range []string{
		`SELECT captured_at FROM evidence_bundles WHERE captured_at IS NOT NULL`,
		`SELECT due_date FROM work_items`,
		`SELECT expires_at FROM interview_invites`,
		`SELECT expires_at FROM org_invitations`,
	} {
		var got time.Time
		if err := db.QueryRow(col).Scan(&got); err != nil {
			t.Fatalf("%s: %v", col, err)
		}
		if !got.Equal(want) {
			t.Errorf("%s read %v after the migration, want the stored wall clock as UTC, %v", col, got, want)
		}
	}

	listed, err := NewEvidenceRepository(db).ListByProject(project)
	if err != nil {
		t.Fatalf("list the bundles: %v", err)
	}
	var order []string
	for _, b := range listed {
		order = append(order, b.ID)
	}
	if fmt.Sprint(order) != fmt.Sprint(bundles) {
		t.Errorf("the bundles list as %v, want %v: newest capture or write first", order, bundles)
	}
}
