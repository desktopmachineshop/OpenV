package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// The agent runs, the runner leases and the runner keys stamp the times
// they hand the database in UTC (#379 bug 140, the class of bug 93 that
// TestTheServicesStampTimesInUTC pins for five other services). Their
// columns are TIMESTAMP: Postgres keeps the wall clock lib/pq sends and
// drops its offset, every read takes that wall clock as UTC, and the SQL
// beside them stamps and compares NOW() as a UTC wall clock. A time.Now() in
// the server's own zone moved by the zone's offset on its way in, and what
// the services decide from it moved with it. Each service is driven through
// its real repositories with time.Local two hours east of UTC and then five
// hours west: each stamp is stored as the UTC wall clock of the moment it was
// taken, each time a service answers is the instant a later read answers,
// and each decision is the one a server in UTC makes:
//   - a run reserved for its launcher's runner stays reserved for the grace
//     window (west, a workspace runner took it at once);
//   - an auto-retry waits out its backoff (west, it was claimable at once);
//   - the reaper fails a run silent for longer than its window, and only
//     such a run (east, it failed every run claimed since its last sweep;
//     west, a lost worker's claimed run waited five hours more);
//   - a node that registered a moment ago is online (west, the pool counted
//     none), and a lease a moment old has used no minutes (west, it was
//     billed its whole length at once);
//   - the reaper's sweep, handed the server's local now, leaves a live lease
//     alone, and the launch's online check, handed a local cutoff, sees a
//     runner that polled a moment ago, and not one silent for longer.

// arsWest is an offset a time.Now() on a server west of UTC would carry.
var arsWest = time.FixedZone("EST", -5*60*60)

// arsInZone makes time.Local zone until the test ends.
func arsInZone(t *testing.T, zone *time.Location) {
	t.Helper()
	prev := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = prev })
}

// arsAgents serves the run service's agent lookup from this package's
// AgentRepository, as agents' FileService does in cmd/server.
type arsAgents struct {
	agents.Service
	repo *AgentRepository
}

func (a arsAgents) Get(id string) (*agents.Agent, error) { return a.repo.FindByID(id) }

func TestTheAgentRunsStampTimesInUTC(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := rtDB(t)
			arsInZone(t, zone)
			org, user, agent, project := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
			rtSeedOrg(t, db, org)
			rtSeedUser(t, db, user, "dana@example.com", "Dana", "")
			rtSeed(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, user)
			rtSeed(t, db, `INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'drafter', 'Drafter', 'claude')`, agent, org)
			rtSeedProject(t, db, project, org, "Board")

			var before time.Time
			start := func() { before = time.Now() }

			t.Run("runs", func(t *testing.T) {
				repo := NewAgentRunRepository(db)
				svc := agentruns.NewDefaultService(repo, arsAgents{repo: NewAgentRepository(db)}, nil)
				const grace = 300 * time.Second
				svc.SetRoutingPolicy(func(string, string) bool { return true }, func(string) int { return int(grace / time.Second) })
				claude := []string{"claude"}
				find := func(id string) *agentruns.Run {
					t.Helper()
					run, err := repo.FindByID(id)
					if err != nil || run == nil {
						t.Fatalf("FindByID(%s): %v, %v", id, run, err)
					}
					return run
				}
				launch := func() *agentruns.Run {
					t.Helper()
					run, _, err := svc.Launch(agentruns.LaunchRequest{OrgID: org, AgentID: agent, ProjectID: &project,
						Prompt: "Draft", LaunchedBy: &user})
					if err != nil {
						t.Fatal(err)
					}
					return run
				}
				// claim has the launcher's runner claim the run it should take
				// next, want.
				claim := func(want string) {
					t.Helper()
					got, err := svc.Claim("mine", org, user, claude, 0, false)
					if err != nil || got == nil || got.ID != want {
						t.Fatalf("the launcher's runner claimed %s (%v), want run %s", arsID(got), err, want)
					}
				}

				start()
				run := launch()
				got := find(run.ID)
				rtStamp(t, "a launched run's created_at", got.CreatedAt, run.CreatedAt, before, time.Now())
				rtStamp(t, "a launched run's hosted_after", *got.HostedAfter, *run.HostedAfter, before.Add(grace), time.Now().Add(grace))
				// Reserved for the launcher's runner for the grace window: a
				// workspace runner takes it only after.
				if taken, err := svc.Claim("hosted", org, "", claude, 0, false); err != nil || taken != nil {
					t.Errorf("a workspace runner claimed %s (%v) inside the launcher's %s grace window, want nothing", arsID(taken), err, grace)
				} else {
					claim(run.ID)
				}

				start()
				if err := svc.MarkRunning(run.ID); err != nil {
					t.Fatal(err)
				}
				got = find(run.ID)
				rtStamp(t, "a running run's started_at", *got.StartedAt, time.Time{}, before, time.Now())
				rtStamp(t, "a running run's heartbeat_at", *got.HeartbeatAt, time.Time{}, before, time.Now())

				// A worker's entry carries no time (the server stamps it) or
				// one it sent with its own offset.
				sent := rtAt(0).In(rtCEST)
				start()
				if _, err := svc.AppendLogs(run.ID, []agentruns.LogEntry{
					{Seq: 1, Kind: agentruns.LogText, Payload: map[string]interface{}{"text": "Drafting"}},
					{Seq: 2, Kind: agentruns.LogText, Payload: map[string]interface{}{"text": "Sent"}, CreatedAt: sent},
				}, "Draft so far"); err != nil {
					t.Fatal(err)
				}
				got = find(run.ID)
				rtStamp(t, "a log push's heartbeat_at", *got.HeartbeatAt, time.Time{}, before, time.Now())
				logs, err := svc.Logs(run.ID, 0)
				if err != nil || len(logs) != 2 {
					t.Fatalf("the run's log: %v, %v", logs, err)
				}
				rtStamp(t, "a log entry's created_at", logs[0].CreatedAt, time.Time{}, before, time.Now())
				rtStamp(t, "a log entry's created_at, sent at +02:00", logs[1].CreatedAt, time.Time{}, sent, sent)

				start()
				if err := svc.Heartbeat(run.ID); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a heartbeat's heartbeat_at", *find(run.ID).HeartbeatAt, time.Time{}, before, time.Now())

				start()
				if err := svc.NoteRun(run.ID, agentruns.NoteHandOffRefused, "Refused", nil); err != nil {
					t.Fatal(err)
				}
				if logs, err = svc.Logs(run.ID, 2); err != nil || len(logs) != 1 {
					t.Fatalf("the run's note: %v, %v", logs, err)
				}
				rtStamp(t, "a note's created_at", logs[0].CreatedAt, time.Time{}, before, time.Now())

				// A lost worker's failure is retried after a backoff of 30 s
				// (the first retry's).
				const backoff = 30 * time.Second
				start()
				finished, err := svc.Finish(run.ID, agentruns.FinishRequest{Status: agentruns.StatusFailed, Error: "lost",
					ErrorClass: agentruns.ErrorClassWorkerError})
				if err != nil {
					t.Fatal(err)
				}
				after := time.Now()
				rtStamp(t, "a finished run's finished_at", *find(run.ID).FinishedAt, *finished.FinishedAt, before, after)
				var retryID string
				if err := db.QueryRow(`SELECT id FROM agent_runs WHERE retried_from_run_id = $1`, run.ID).Scan(&retryID); err != nil {
					t.Fatalf("the auto-retry: %v", err)
				}
				retry := find(retryID)
				rtStamp(t, "an auto-retry's created_at", retry.CreatedAt, time.Time{}, before, after)
				rtStamp(t, "an auto-retry's hosted_after", *retry.HostedAfter, time.Time{}, before.Add(grace), after.Add(grace))
				rtStamp(t, "an auto-retry's next_attempt_at", *retry.NextAttemptAt, time.Time{}, before.Add(backoff), after.Add(backoff))
				if taken, err := svc.Claim("mine", org, user, claude, 0, false); err != nil || taken != nil {
					t.Errorf("the launcher's runner claimed %s (%v) inside the retry's %s backoff, want nothing", arsID(taken), err, backoff)
				}

				// A run whose proposals are all reviewed finishes.
				approval := launch()
				claim(approval.ID)
				if err := svc.MarkRunning(approval.ID); err != nil {
					t.Fatal(err)
				}
				proposal := uuid.New().String()
				rtSeed(t, db, `INSERT INTO agent_proposals (id, run_id, project_id, op, status) VALUES ($1, $2, $3, 'create_link', 'pending')`,
					proposal, approval.ID, project)
				if awaiting, err := svc.Finish(approval.ID, agentruns.FinishRequest{Status: agentruns.StatusSucceeded}); err != nil ||
					awaiting.Status != agentruns.StatusAwaitingApproval {
					t.Fatalf("finish with a pending proposal: %v, %v", awaiting, err)
				}
				rtSeed(t, db, `UPDATE agent_proposals SET status = 'applied' WHERE id = $1`, proposal)
				start()
				resolved, err := svc.FinalizeIfResolved(approval.ID)
				if err != nil || resolved.Status != agentruns.StatusSucceeded {
					t.Fatalf("FinalizeIfResolved: %v, %v", resolved, err)
				}
				rtStamp(t, "a resolved run's finished_at", *find(approval.ID).FinishedAt, *resolved.FinishedAt, before, time.Now())

				// The reaper's window is 2 minutes of silence: a run claimed a
				// moment ago is live, and one whose claim was 3 minutes ago,
				// with no word since, is not.
				silent := launch()
				claim(silent.ID)
				if failed, err := svc.FailStale(2 * time.Minute); err != nil || len(failed) != 0 {
					t.Errorf("the reaper failed %v (%v) a moment after their claim, want none", failed, err)
				}
				rtSeed(t, db, `UPDATE agent_runs SET heartbeat_at = heartbeat_at - INTERVAL '3 minutes' WHERE id = $1`, silent.ID)
				if failed, err := svc.FailStale(2 * time.Minute); err != nil || len(failed) != 1 || failed[0] != silent.ID {
					t.Errorf("the reaper failed %v (%v) 3 minutes after the claim, want %s", failed, err, silent.ID)
				}
			})

			t.Run("runner leases and keys", func(t *testing.T) {
				keyRepo := NewWorkerKeyRepository(db)
				keys := workerkeys.NewDefaultService(keyRepo)
				keys.SetPairingRepository(keyRepo)
				leaseRepo := NewRunnerSessionRepository(db)
				leases := runnersessions.NewDefaultService(leaseRepo, keys)
				findKey := func(id string) *workerkeys.Key {
					t.Helper()
					key, err := keyRepo.FindByID(id)
					if err != nil || key == nil {
						t.Fatalf("FindByID(%s): %v, %v", id, key, err)
					}
					return key
				}
				findLease := func(id string) *runnersessions.Session {
					t.Helper()
					lease, err := leaseRepo.FindSessionByID(id)
					if err != nil || lease == nil {
						t.Fatalf("FindSessionByID(%s): %v, %v", id, lease, err)
					}
					return lease
				}

				start()
				code, expires, err := keys.CreatePairing(org, user)
				if err != nil {
					t.Fatal(err)
				}
				var stored time.Time
				if err := db.QueryRow(`SELECT expires_at FROM connector_pairings WHERE org_id = $1`, org).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a pairing code's expires_at", stored, expires, before.Add(workerkeys.PairingTTL), time.Now().Add(workerkeys.PairingTTL))
				if _, _, err := keys.ExchangePairing(code, "Dana"); err != nil {
					t.Errorf("a pairing code a moment old: %v, want it to pair", err)
				}

				start()
				key, token, err := keys.Create(org, "Dana's connector", &user, &user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a runner key's created_at", findKey(key.ID).CreatedAt, key.CreatedAt, before, time.Now())
				start()
				if _, err := keys.Resolve(token); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a runner key's last_used_at", *findKey(key.ID).LastUsedAt, time.Time{}, before, time.Now())
				// The launch's online check (cmd/server) asks for a poll in
				// the last 30 s of its own clock.
				if online, err := keys.HasOnlinePersonalRunner(org, user, time.Now().Add(-30*time.Second)); err != nil || !online {
					t.Errorf("a runner that polled a moment ago is online: %v (%v), want true", online, err)
				}
				rtSeed(t, db, `UPDATE worker_keys SET last_used_at = last_used_at - INTERVAL '31 seconds' WHERE id = $1`, key.ID)
				if online, err := keys.HasOnlinePersonalRunner(org, user, time.Now().Add(-30*time.Second)); err != nil || online {
					t.Errorf("a runner that polled 31 s ago is online: %v (%v), want false", online, err)
				}

				start()
				node, err := leases.RegisterNode("", "node-1", []string{"claude"})
				if err != nil {
					t.Fatal(err)
				}
				gotNode, err := leaseRepo.FindNodeByID(node.ID)
				if err != nil || gotNode == nil {
					t.Fatalf("FindNodeByID: %v, %v", gotNode, err)
				}
				rtStamp(t, "a node's created_at", gotNode.CreatedAt, node.CreatedAt, before, time.Now())
				rtStamp(t, "a node's last_seen_at", gotNode.LastSeenAt, node.LastSeenAt, before, time.Now())
				if counts, err := leases.Counts(""); err != nil || counts.Total != 1 {
					t.Errorf("the pool's online nodes a moment after one registered: %+v (%v), want 1", counts, err)
				}

				// A 30-minute lease that lapses after 15 idle minutes.
				const idle = 15 * time.Minute
				start()
				lease, created, err := leases.Start(org, user, 30, int(idle/time.Minute))
				if err != nil || !created {
					t.Fatalf("Start: %v, %v, %v", lease, created, err)
				}
				startedFrom, startedTo := before, time.Now()
				got := findLease(lease.ID)
				rtStamp(t, "a lease's started_at", got.StartedAt, lease.StartedAt, startedFrom, startedTo)
				rtStamp(t, "a lease's expires_at", got.ExpiresAt, lease.ExpiresAt, startedFrom.Add(30*time.Minute), startedTo.Add(30*time.Minute))
				rtStamp(t, "a lease's last_activity_at", got.LastActivityAt, lease.LastActivityAt, startedFrom, startedTo)
				rtStamp(t, "a lease key's created_at", findKey(*lease.WorkerKeyID).CreatedAt, time.Time{}, startedFrom, startedTo)
				now := time.Now().UTC()
				if used, err := leases.MinutesUsed(org, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)); err != nil || used != 0 {
					t.Errorf("the minutes a lease a moment old has used: %d (%v), want 0", used, err)
				}

				// The beat that hands the node its lease tells it when the
				// lease lapses: idle minutes after the last activity.
				start()
				_, assignment, err := leases.Heartbeat(node.ID)
				if err != nil || assignment == nil {
					t.Fatalf("the node's heartbeat: %v, %v", assignment, err)
				}
				rtStamp(t, "the lapse a node is told of", assignment.ExpiresAt, time.Time{}, startedFrom.Add(idle), startedTo.Add(idle))
				rtStamp(t, "an active lease's last_activity_at", findLease(lease.ID).LastActivityAt, time.Time{}, before, time.Now())
				rtStamp(t, "a heartbeat's last_seen_at", arsNode(t, leaseRepo, node.ID).LastSeenAt, time.Time{}, before, time.Now())

				start()
				if err := leases.Touch(lease.ID); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a touched lease's last_activity_at", findLease(lease.ID).LastActivityAt, time.Time{}, before, time.Now())

				start()
				extended, err := leases.Extend(lease.ID, 60)
				if err != nil {
					t.Fatal(err)
				}
				got = findLease(lease.ID)
				rtStamp(t, "an extended lease's expires_at", got.ExpiresAt, extended.ExpiresAt, before.Add(time.Hour), time.Now().Add(time.Hour))
				rtStamp(t, "an extended lease's last_activity_at", got.LastActivityAt, extended.LastActivityAt, before, time.Now())

				// The reaper sweeps with its own time.Now().
				if ended, err := leases.Sweep(time.Now()); err != nil || len(ended) != 0 {
					t.Errorf("the sweep ended %d leases (%v) while the one lease was live, want none", len(ended), err)
				}

				start()
				ended, err := leases.End(lease.ID, runnersessions.EndReasonUser)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an ended lease's ended_at", *findLease(lease.ID).EndedAt, *ended.EndedAt, before, time.Now())
			})
		})
	}
}

// arsID names a claimed run in a failure message.
func arsID(run *agentruns.Run) string {
	if run == nil {
		return "nothing"
	}
	return "run " + run.ID
}

func arsNode(t *testing.T, repo *RunnerSessionRepository, id string) *runnersessions.Node {
	t.Helper()
	node, err := repo.FindNodeByID(id)
	if err != nil || node == nil {
		t.Fatalf("FindNodeByID(%s): %v, %v", id, node, err)
	}
	return node
}
