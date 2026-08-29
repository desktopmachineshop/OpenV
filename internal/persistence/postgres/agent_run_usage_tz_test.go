package postgres

import (
	"fmt"
	"testing"
	"time"
)

// TestUsageDayBucketsAreUTC is the issue-#190(b) regression test: Usage must
// bucket runs by true UTC calendar day, independent of the query session's
// timezone. It seeds two runs whose created_at instants share a UTC day
// (2026-03-15) but straddle midnight in a westward zone, forces the database
// session into America/New_York, and asserts both land in the single UTC-day
// bucket 2026-03-15.
//
// This guards against the tempting-but-wrong `(created_at AT TIME ZONE
// 'UTC')::date` rewrite: on a naive TIMESTAMP column that form folds through
// the session timezone and would split these two runs across 2026-03-14 and
// 2026-03-15 under a New York session.
func TestUsageDayBucketsAreUTC(t *testing.T) {
	f := newClaimFixture(t)

	// Pin the database default timezone to a non-UTC zone, then force the pool
	// to hand out fresh connections that pick it up. Every subsequent query —
	// including Usage's — then runs under America/New_York.
	var dbName string
	if err := f.db.QueryRow(`SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if _, err := f.db.Exec(fmt.Sprintf(`ALTER DATABASE %q SET timezone TO 'America/New_York'`, dbName)); err != nil {
		t.Fatalf("ALTER DATABASE timezone: %v", err)
	}
	f.db.SetMaxIdleConns(0)

	// Confirm the session really is non-UTC, otherwise the test proves nothing.
	var tz string
	if err := f.db.QueryRow(`SHOW timezone`).Scan(&tz); err != nil {
		t.Fatalf("SHOW timezone: %v", err)
	}
	if tz != "America/New_York" {
		t.Fatalf("session timezone = %q, want America/New_York (test would be vacuous)", tz)
	}

	// Both instants are on 2026-03-15 in UTC; in New York the first is still
	// 2026-03-14 22:00 and the second is 2026-03-15 01:00.
	utcDay := time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC)  // NY: 2026-03-14 22:00
	utcDay2 := time.Date(2026, 3, 15, 5, 0, 0, 0, time.UTC) // NY: 2026-03-15 01:00
	f.seedUsageRun(t, f.agentID, utcDay, 10, 5, ptr(0.1), "succeeded")
	f.seedUsageRun(t, f.agentID, utcDay2, 20, 7, ptr(0.2), "succeeded")

	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	_, byDay, err := f.repo.Usage(f.orgID, since)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}

	if len(byDay) != 1 {
		t.Fatalf("byDay = %+v, want a single UTC-day bucket (both runs are 2026-03-15 UTC)", byDay)
	}
	if byDay[0].Day != "2026-03-15" {
		t.Errorf("byDay[0].Day = %q, want 2026-03-15 (true UTC day, not the New York local day)", byDay[0].Day)
	}
	if byDay[0].Runs != 2 {
		t.Errorf("byDay[0].Runs = %d, want 2", byDay[0].Runs)
	}
}
