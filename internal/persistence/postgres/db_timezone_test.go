package postgres

import (
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// Every connection the server opens keeps time in UTC, whatever zone the
// database would give it (#379 bug 156). The SQL stamps and compares the
// TIMESTAMP columns, which hold UTC wall clocks, with NOW(), a TIMESTAMPTZ
// that Postgres converts to and from a TIMESTAMP in the session's TimeZone:
// a session in another zone stamped that zone's wall clock and compared off
// by its offset. Each case gives a throwaway database a zone the way an
// operator's server can (the database's default, the role's default in it,
// PGTZ where the server runs), checks that a connection opened as before,
// with sql.Open, is in that zone, and then works through Connect, the
// server's own pool:
//   - the session's zone is UTC, NOW() is the UTC wall clock, and so is a
//     column's DEFAULT NOW() and an UPDATE's NOW();
//   - an agent run reserved for its launcher's runner is not taken by a
//     workspace runner inside the grace window (east, it was taken at once);
//     the claim stamps heartbeat_at with the UTC wall clock, and the reaper
//     leaves the run alone a moment later (west, it failed it at once) and
//     fails it once silent for longer than its window (east, nine hours
//     later), stamping finished_at;
//   - an auto-retry waits out its backoff (east, it was claimable at once);
//   - the queue's oldest run is a moment old (west, minus four hours), and
//     cancelling it while queued stamps finished_at;
//   - a runner lease a moment old has used no minutes (east, its whole 30;
//     west, minus four hours of them, which reads as none).
func TestTheConnectionsKeepTimeInUTC(t *testing.T) {
	cases := []struct {
		name string
		zone string
		set  func(t *testing.T, db *sql.DB, database, zone string)
	}{
		{"the database's default", "America/New_York", func(t *testing.T, db *sql.DB, database, zone string) {
			rtSeed(t, db, `ALTER DATABASE `+pq.QuoteIdentifier(database)+` SET timezone TO `+pq.QuoteLiteral(zone))
		}},
		{"the role's default in the database", "Asia/Tokyo", func(t *testing.T, db *sql.DB, database, zone string) {
			rtSeed(t, db, `ALTER ROLE CURRENT_USER IN DATABASE `+pq.QuoteIdentifier(database)+` SET timezone TO `+pq.QuoteLiteral(zone))
		}},
		{"PGTZ", "Asia/Tokyo", func(t *testing.T, _ *sql.DB, _, zone string) {
			t.Setenv("PGTZ", zone)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := tzConnect(t, c.zone, c.set)
			var before time.Time
			start := func() { before = time.Now() }

			var zone string
			if err := db.QueryRow(`SHOW TimeZone`).Scan(&zone); err != nil || zone != "UTC" {
				t.Errorf("a connection of the server's pool runs in %q (%v), want UTC", zone, err)
			}
			start()
			var now time.Time
			if err := db.QueryRow(`SELECT NOW()::timestamp`).Scan(&now); err != nil {
				t.Fatal(err)
			}
			rtStamp(t, "NOW() as a TIMESTAMP", now, time.Time{}, before, time.Now())

			org, user, agent, project := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
			start()
			rtSeedOrg(t, db, org)
			var created time.Time
			if err := db.QueryRow(`SELECT created_at FROM organizations WHERE id = $1`, org).Scan(&created); err != nil {
				t.Fatal(err)
			}
			rtStamp(t, "a column's DEFAULT NOW()", created, time.Time{}, before, time.Now())
			rtSeedUser(t, db, user, "dana@example.com", "Dana", "")
			start()
			if err := users.NewDefaultService(NewUserRepository(db)).SetEmailNotifications(user, false); err != nil {
				t.Fatal(err)
			}
			var updated time.Time
			if err := db.QueryRow(`SELECT updated_at FROM users WHERE id = $1`, user).Scan(&updated); err != nil {
				t.Fatal(err)
			}
			rtStamp(t, "an UPDATE's NOW()", updated, time.Time{}, before, time.Now())

			rtSeed(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, user)
			rtSeed(t, db, `INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'drafter', 'Drafter', 'claude')`, agent, org)
			rtSeedProject(t, db, project, org, "Board")

			t.Run("agent runs", func(t *testing.T) {
				repo := NewAgentRunRepository(db)
				svc := agentruns.NewDefaultService(repo, arsAgents{repo: NewAgentRepository(db)}, nil)
				svc.SetRoutingPolicy(func(string, string) bool { return true }, func(string) int { return 300 })
				claude := []string{"claude"}
				launch := func() *agentruns.Run {
					t.Helper()
					run, _, err := svc.Launch(agentruns.LaunchRequest{OrgID: org, AgentID: agent, ProjectID: &project,
						Prompt: "Draft", LaunchedBy: &user})
					if err != nil {
						t.Fatal(err)
					}
					return run
				}
				find := func(id string) *agentruns.Run {
					t.Helper()
					run, err := repo.FindByID(id)
					if err != nil || run == nil {
						t.Fatalf("FindByID(%s): %v, %v", id, run, err)
					}
					return run
				}
				claim := func(want string) {
					t.Helper()
					got, err := svc.Claim("mine", org, user, claude, 0, false)
					if err != nil || got == nil || got.ID != want {
						t.Fatalf("the launcher's runner claimed %s (%v), want run %s", arsID(got), err, want)
					}
				}

				run := launch()
				if taken, err := svc.Claim("hosted", org, "", claude, 0, false); err != nil || taken != nil {
					t.Errorf("a workspace runner claimed %s (%v) inside the launcher's grace window, want nothing", arsID(taken), err)
					// A workspace runner took it: the launcher's runner claims
					// another.
					if taken != nil {
						run = launch()
					}
				}
				start()
				claim(run.ID)
				rtStamp(t, "a claim's heartbeat_at", *find(run.ID).HeartbeatAt, time.Time{}, before, time.Now())
				if failed, err := svc.FailStale(2 * time.Minute); err != nil || len(failed) != 0 {
					t.Errorf("the reaper failed %v (%v) a moment after their claim, want none", failed, err)
				}
				rtSeed(t, db, `UPDATE agent_runs SET heartbeat_at = heartbeat_at - INTERVAL '3 minutes' WHERE id = $1`, run.ID)
				start()
				if failed, err := svc.FailStale(2 * time.Minute); err != nil || len(failed) != 1 || failed[0] != run.ID {
					t.Errorf("the reaper failed %v (%v) 3 minutes after the claim, want %s", failed, err, run.ID)
				} else {
					rtStamp(t, "a failed run's finished_at", *find(run.ID).FinishedAt, time.Time{}, before, time.Now())
				}

				// A lost worker's failure is retried after a backoff of 30 s.
				lost := launch()
				claim(lost.ID)
				if _, err := svc.Finish(lost.ID, agentruns.FinishRequest{Status: agentruns.StatusFailed, Error: "lost",
					ErrorClass: agentruns.ErrorClassWorkerError}); err != nil {
					t.Fatal(err)
				}
				var retry string
				if err := db.QueryRow(`SELECT id FROM agent_runs WHERE retried_from_run_id = $1`, lost.ID).Scan(&retry); err != nil {
					t.Fatalf("the auto-retry: %v", err)
				}
				if taken, err := svc.Claim("mine", org, user, claude, 0, false); err != nil || taken != nil {
					t.Errorf("the launcher's runner claimed %s (%v) inside the retry's backoff, want nothing", arsID(taken), err)
				}

				// The retry is the oldest queued run, a moment old.
				stats, err := svc.QueueStats(org)
				if err != nil {
					t.Fatal(err)
				}
				if stats.OldestQueuedSeconds < 0 || stats.OldestQueuedSeconds > 60 {
					t.Errorf("the oldest queued run, queued a moment ago, is %d s old, want a moment", stats.OldestQueuedSeconds)
				}
				start()
				if _, err := svc.RequestCancel(retry); err != nil {
					t.Fatal(err)
				}
				if cancelled := find(retry); cancelled.FinishedAt == nil {
					t.Errorf("the cancelled retry is %s with no finished_at, want it cancelled while queued", cancelled.Status)
				} else {
					rtStamp(t, "a cancelled queued run's finished_at", *cancelled.FinishedAt, time.Time{}, before, time.Now())
				}
			})

			t.Run("runner leases", func(t *testing.T) {
				keys := workerkeys.NewDefaultService(NewWorkerKeyRepository(db))
				leases := runnersessions.NewDefaultService(NewRunnerSessionRepository(db), keys)
				if _, err := leases.RegisterNode("", "node-1", []string{"claude"}); err != nil {
					t.Fatal(err)
				}
				if _, created, err := leases.Start(org, user, 30, 15); err != nil || !created {
					t.Fatalf("Start: %v, %v", created, err)
				}
				month := time.Now().UTC()
				month = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
				if used, err := leases.MinutesUsed(org, month); err != nil || used != 0 {
					t.Errorf("the minutes a lease a moment old has used: %d (%v), want 0", used, err)
				}
			})
		})
	}
}

// tzConnect is a fresh database with the production schema whose sessions
// set puts in zone, opened through Connect. It first checks that a
// connection opened as the server opened them before, with sql.Open, is in
// zone, so the case has a zone to overcome.
func tzConnect(t *testing.T, zone string, set func(t *testing.T, db *sql.DB, database, zone string)) *sql.DB {
	t.Helper()
	plain := rtDB(t)
	var database string
	if err := plain.QueryRow(`SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	set(t, plain, database, zone)
	u, err := url.Parse(os.Getenv(TestDatabaseURLEnv))
	if err != nil {
		t.Fatalf("parse %s: %v", TestDatabaseURLEnv, err)
	}
	u.Path = "/" + database

	before, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	var got string
	if err := before.QueryRow(`SHOW TimeZone`).Scan(&got); err != nil || got != zone {
		t.Fatalf("a connection opened with sql.Open runs in %q (%v), want %q: the case sets no zone", got, err, zone)
	}

	db, err := Connect(u.String())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
