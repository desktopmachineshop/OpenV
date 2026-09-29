package postgres

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// claimFixture is a database with one org and two agents (one with repo
// access) ready for queueing runs.
type claimFixture struct {
	db        *sql.DB
	repo      *AgentRunRepository
	orgID     string
	agentID   string // provider "claude", no repo access
	repoAgent string // provider "claude", repo access
	userA     string
	userB     string
}

func newClaimFixture(t *testing.T) *claimFixture {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)

	f := &claimFixture{
		db:        db,
		repo:      NewAgentRunRepository(db),
		orgID:     uuid.New().String(),
		agentID:   uuid.New().String(),
		repoAgent: uuid.New().String(),
		userA:     uuid.New().String(),
		userB:     uuid.New().String(),
	}
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Test Org', 'test-org')`, f.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'worker', 'Worker', 'claude')`, f.agentID, f.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agents (id, org_id, slug, name, provider, repo_access) VALUES ($1, $2, 'coder', 'Coder', 'claude', TRUE)`, f.repoAgent, f.orgID); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{f.userA, f.userB} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Member')`, u, u+"@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, f.orgID, u); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// project adds a project to the fixture's org.
func (f *claimFixture) project(t *testing.T, name string) string {
	t.Helper()
	id := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, $3)`, id, f.orgID, name); err != nil {
		t.Fatal(err)
	}
	return id
}

// exec runs a fixture statement.
func (f *claimFixture) exec(t *testing.T, query string, args ...interface{}) {
	t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// runSpec describes one queued run to seed.
type runSpec struct {
	agentID     string // defaults to fixture.agentID
	projectID   *string
	launchedBy  *string
	preferred   *string // sets preferred_user_id + hostedAfter
	hostedAfter time.Time
	priority    int
	age         time.Duration // how long ago the run was created
}

func (f *claimFixture) queueRun(t *testing.T, spec runSpec) string {
	t.Helper()
	agentID := spec.agentID
	if agentID == "" {
		agentID = f.agentID
	}
	run := &agentruns.Run{
		ID:               uuid.New().String(),
		OrgID:            f.orgID,
		AgentID:          agentID,
		ProjectID:        spec.projectID,
		Status:           agentruns.StatusQueued,
		Priority:         spec.priority,
		Prompt:           "do work",
		ArtifactsTouched: []map[string]interface{}{},
		LaunchedBy:       spec.launchedBy,
		CreatedAt:        time.Now().Add(-spec.age),
	}
	if spec.preferred != nil {
		run.PreferredUserID = spec.preferred
		hosted := spec.hostedAfter
		run.HostedAfter = &hosted
	}
	if err := f.repo.Save(run); err != nil {
		t.Fatalf("queue run: %v", err)
	}
	return run.ID
}

func (f *claimFixture) status(t *testing.T, runID string) string {
	t.Helper()
	run, err := f.repo.FindByID(runID)
	if err != nil || run == nil {
		t.Fatalf("FindByID(%s): %v %v", runID, run, err)
	}
	return run.Status
}

var claudeOnly = []string{"claude"}

func TestClaimPersonalRunnerTakesOwnAndOwnerlessRuns(t *testing.T) {
	f := newClaimFixture(t)
	viewed := f.project(t, "Viewed")
	f.exec(t, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'viewer')`, viewed, f.userA)
	mine := f.queueRun(t, runSpec{launchedBy: &f.userA, age: 3 * time.Hour})
	theirs := f.queueRun(t, runSpec{launchedBy: &f.userB, age: 2 * time.Hour})
	ownerless := f.queueRun(t, runSpec{projectID: &viewed, age: time.Hour})

	// Personal runner for userA: oldest of {mine, ownerless} first.
	first, err := f.repo.Claim("w-a", f.orgID, f.userA, claudeOnly, 0, false)
	if err != nil {
		t.Fatalf("claim 1: %v", err)
	}
	if first == nil || first.ID != mine {
		t.Fatalf("first claim = %+v, want the user's own oldest run %s", first, mine)
	}
	if first.Status != agentruns.StatusClaimed || first.WorkerID != "w-a" || first.HeartbeatAt == nil {
		t.Errorf("claimed run = status %s worker %q heartbeat %v, want claimed/w-a/set", first.Status, first.WorkerID, first.HeartbeatAt)
	}

	second, err := f.repo.Claim("w-a", f.orgID, f.userA, claudeOnly, 0, false)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if second == nil || second.ID != ownerless {
		t.Fatalf("second claim = %+v, want the ownerless run %s in a project the user views (board/automation "+
			"work load-shares)", second, ownerless)
	}

	third, err := f.repo.Claim("w-a", f.orgID, f.userA, claudeOnly, 0, false)
	if err != nil {
		t.Fatalf("claim 3: %v", err)
	}
	if third != nil {
		t.Fatalf("third claim = %+v, want nil: another member's run must stay reserved", third)
	}
	if got := f.status(t, theirs); got != agentruns.StatusQueued {
		t.Errorf("other member's run status = %s, want still queued", got)
	}
}

// TestClaimPersonalRunnerTakesOnlyOwnerlessRunsItsUserCouldSee is the
// regression test for personal runners that claimed ownerless (automation,
// board) runs in every project of their workspace: the claim took any run no
// one launched, so a member's runner took, and its agent then worked in, a
// project the member cannot open (OpenV REQ-27, REQ-16, REQ-42). A personal runner
// never takes a run its user could not see: an ownerless run in a project the
// user holds a role in, directly or through a people team, and, for a
// workspace admin, any ownerless run, one with no project too, which only
// workspace admins may see. A workspace runner still takes them all.
func TestClaimPersonalRunnerTakesOnlyOwnerlessRunsItsUserCouldSee(t *testing.T) {
	f := newClaimFixture(t)
	viewed, teamed, hidden := f.project(t, "Viewed"), f.project(t, "Teamed"), f.project(t, "Hidden")
	f.exec(t, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'viewer')`, viewed, f.userA)
	team := uuid.New().String()
	f.exec(t, `INSERT INTO org_teams (id, org_id, name) VALUES ($1, $2, 'Reviewers')`, team, f.orgID)
	f.exec(t, `INSERT INTO org_team_members (org_team_id, user_id) VALUES ($1, $2)`, team, f.userA)
	f.exec(t, `INSERT INTO project_team_access (project_id, org_team_id, role) VALUES ($1, $2, 'reviewer')`, teamed, team)
	// Hidden has roles too, both held by someone else: a direct one and a
	// people team's. The user's runner asks whether its user holds a role
	// there, not whether anyone does.
	f.exec(t, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'editor')`, hidden, f.userB)
	others := uuid.New().String()
	f.exec(t, `INSERT INTO org_teams (id, org_id, name) VALUES ($1, $2, 'Others')`, others, f.orgID)
	f.exec(t, `INSERT INTO org_team_members (org_team_id, user_id) VALUES ($1, $2)`, others, f.userB)
	f.exec(t, `INSERT INTO project_team_access (project_id, org_team_id, role) VALUES ($1, $2, 'viewer')`, hidden, others)

	inHidden := f.queueRun(t, runSpec{projectID: &hidden, age: 4 * time.Hour})
	noProject := f.queueRun(t, runSpec{age: 3 * time.Hour})
	inViewed := f.queueRun(t, runSpec{projectID: &viewed, age: 2 * time.Hour})
	inTeamed := f.queueRun(t, runSpec{projectID: &teamed, age: time.Hour})

	for _, want := range []string{inViewed, inTeamed, ""} {
		got, err := f.repo.Claim("w-a", f.orgID, f.userA, claudeOnly, 0, false)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if want == "" {
			if got != nil {
				t.Fatalf("claim = %s (project %v), want nothing: the user has no role in the rest", got.ID, got.ProjectID)
			}
			break
		}
		if got == nil || got.ID != want {
			t.Fatalf("claim = %+v, want %s, the oldest ownerless run in a project the user holds a role in", got, want)
		}
	}
	for _, id := range []string{inHidden, noProject} {
		if got := f.status(t, id); got != agentruns.StatusQueued {
			t.Errorf("run %s is %s, want still queued for a runner whose user could see it", id, got)
		}
	}

	// A workspace admin sees every run of the workspace, one with no project
	// included, so the admin's personal runner takes what was left.
	f.exec(t, `UPDATE org_members SET role = 'admin' WHERE org_id = $1 AND user_id = $2`, f.orgID, f.userB)
	for _, want := range []string{inHidden, noProject} {
		got, err := f.repo.Claim("w-b", f.orgID, f.userB, claudeOnly, 0, false)
		if err != nil || got == nil || got.ID != want {
			t.Fatalf("the admin's claim = %+v (%v), want %s", got, err, want)
		}
	}
}

// TestClaimRecordsThePersonalRunnersUser pins claimed_by: a personal runner's
// claim records its user, whose local checkout paths the run's token reads
// the project's repository connections with; a workspace runner's records
// none; and a release clears it with the claim.
func TestClaimRecordsThePersonalRunnersUser(t *testing.T) {
	f := newClaimFixture(t)
	run := f.queueRun(t, runSpec{launchedBy: &f.userA})

	got, err := f.repo.Claim("w-a", f.orgID, f.userA, claudeOnly, 0, false)
	if err != nil || got == nil || got.ID != run {
		t.Fatalf("personal claim = %+v (%v), want %s", got, err, run)
	}
	if got.ClaimedBy == nil || *got.ClaimedBy != f.userA {
		t.Fatalf("claimed_by = %v, want the personal runner's user %s", got.ClaimedBy, f.userA)
	}
	if released, err := f.repo.ReleaseClaim(run, "w-a"); err != nil || !released {
		t.Fatalf("release: %v %v", released, err)
	}
	if back, err := f.repo.FindByID(run); err != nil || back.ClaimedBy != nil {
		t.Fatalf("after the release claimed_by = %v (%v), want none", back.ClaimedBy, err)
	}

	got, err = f.repo.Claim("w-box", f.orgID, "", claudeOnly, 0, false)
	if err != nil || got == nil || got.ID != run {
		t.Fatalf("workspace claim = %+v (%v), want %s", got, err, run)
	}
	if got.ClaimedBy != nil {
		t.Fatalf("claimed_by = %s after a workspace key's claim, want none", *got.ClaimedBy)
	}
}

func TestClaimHostedRespectsPersonalReservationUntilGrace(t *testing.T) {
	f := newClaimFixture(t)
	reserved := f.queueRun(t, runSpec{
		launchedBy:  &f.userA,
		preferred:   &f.userA,
		hostedAfter: time.Now().Add(time.Hour),
	})

	// Within the grace window the workspace/hosted runner must not touch it.
	got, err := f.repo.Claim("w-hosted", f.orgID, "", claudeOnly, 0, false)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got != nil {
		t.Fatalf("hosted runner claimed a reserved run inside its grace window: %+v", got)
	}

	// The launcher's own personal runner may claim it immediately.
	own, err := f.repo.Claim("w-a", f.orgID, f.userA, claudeOnly, 0, false)
	if err != nil || own == nil || own.ID != reserved {
		t.Fatalf("personal claim = %+v (%v), want the reserved run", own, err)
	}
	// Put it back for the expiry half of the test.
	if released, err := f.repo.ReleaseClaim(reserved, "w-a"); err != nil || !released {
		t.Fatalf("release: %v %v", released, err)
	}

	// Expire the grace window: hosted may now take it over.
	if _, err := f.db.Exec(`UPDATE agent_runs SET hosted_after = NOW() - INTERVAL '1 second' WHERE id = $1`, reserved); err != nil {
		t.Fatal(err)
	}
	got, err = f.repo.Claim("w-hosted", f.orgID, "", claudeOnly, 0, false)
	if err != nil {
		t.Fatalf("claim after grace: %v", err)
	}
	if got == nil || got.ID != reserved {
		t.Fatalf("post-grace hosted claim = %+v, want %s", got, reserved)
	}
	if got.WorkerID != "w-hosted" {
		t.Errorf("worker = %q, want w-hosted", got.WorkerID)
	}
}

func TestClaimHostedTakesUnreservedRunsFromAnyLauncher(t *testing.T) {
	f := newClaimFixture(t)
	unreserved := f.queueRun(t, runSpec{launchedBy: &f.userB})

	got, err := f.repo.Claim("w-hosted", f.orgID, "", claudeOnly, 0, false)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got == nil || got.ID != unreserved {
		t.Fatalf("claim = %+v, want the unreserved run regardless of launcher", got)
	}
}

func TestClaimPriorityThenAgeOrdering(t *testing.T) {
	f := newClaimFixture(t)
	oldNormal := f.queueRun(t, runSpec{priority: agentruns.PriorityNormal, age: 3 * time.Hour})
	newChild := f.queueRun(t, runSpec{priority: agentruns.PriorityChild, age: 10 * time.Minute})
	oldChild := f.queueRun(t, runSpec{priority: agentruns.PriorityChild, age: time.Hour})

	want := []string{oldChild, newChild, oldNormal} // priority DESC, then oldest first
	for i, expected := range want {
		got, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, 0, false)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if got == nil || got.ID != expected {
			t.Fatalf("claim %d = %+v, want %s", i, got, expected)
		}
	}
}

func TestClaimMinPriorityReservesChildSlots(t *testing.T) {
	f := newClaimFixture(t)
	f.queueRun(t, runSpec{priority: agentruns.PriorityNormal, age: time.Hour})

	got, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, agentruns.PriorityChild, false)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got != nil {
		t.Fatalf("dedicated child slot claimed a normal-priority run: %+v", got)
	}

	child := f.queueRun(t, runSpec{priority: agentruns.PriorityInterview})
	got, err = f.repo.Claim("w-1", f.orgID, "", claudeOnly, agentruns.PriorityChild, false)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got == nil || got.ID != child {
		t.Fatalf("claim = %+v, want the high-priority run", got)
	}
}

func TestClaimFiltersProviderOrgAndRepoAccess(t *testing.T) {
	f := newClaimFixture(t)
	repoRun := f.queueRun(t, runSpec{agentID: f.repoAgent, age: 2 * time.Hour})

	t.Run("provider mismatch", func(t *testing.T) {
		got, err := f.repo.Claim("w-1", f.orgID, "", []string{"codex"}, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("claimed across providers: %+v", got)
		}
	})

	t.Run("other org", func(t *testing.T) {
		got, err := f.repo.Claim("w-1", uuid.New().String(), "", claudeOnly, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("claimed across orgs: %+v", got)
		}
	})

	t.Run("hosted runner refuses repo-access work", func(t *testing.T) {
		got, err := f.repo.Claim("w-hosted", f.orgID, "", claudeOnly, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("excludeRepoAccess claimed a repo-access run: %+v", got)
		}
	})

	t.Run("personal runner takes repo-access work", func(t *testing.T) {
		got, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.ID != repoRun {
			t.Fatalf("claim = %+v, want %s", got, repoRun)
		}
	})
}

func TestClaimSkipsNonQueuedRuns(t *testing.T) {
	f := newClaimFixture(t)
	runID := f.queueRun(t, runSpec{})
	if _, err := f.db.Exec(`UPDATE agent_runs SET status = 'running' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("claimed a non-queued run: %+v", got)
	}
}

// TestClaimConcurrentWorkersNeverDoubleClaim exercises the FOR UPDATE SKIP
// LOCKED path: concurrent claimers must partition the queue, never sharing a
// run and never blocking each other.
func TestClaimConcurrentWorkersNeverDoubleClaim(t *testing.T) {
	f := newClaimFixture(t)

	const rounds = 5
	for round := 0; round < rounds; round++ {
		runA := f.queueRun(t, runSpec{age: 2 * time.Hour})
		runB := f.queueRun(t, runSpec{age: time.Hour})

		type result struct {
			worker string
			run    *agentruns.Run
			err    error
		}
		results := make(chan result, 2)
		var start sync.WaitGroup
		start.Add(1)
		for _, worker := range []string{"w-1", "w-2"} {
			go func(worker string) {
				start.Wait() // maximize overlap
				run, err := f.repo.Claim(worker, f.orgID, "", claudeOnly, 0, false)
				results <- result{worker, run, err}
			}(worker)
		}
		start.Done()

		claimed := map[string]string{} // runID -> worker
		for i := 0; i < 2; i++ {
			r := <-results
			if r.err != nil {
				t.Fatalf("round %d: worker %s claim error: %v", round, r.worker, r.err)
			}
			if r.run == nil {
				t.Fatalf("round %d: worker %s got nothing; both queued runs should be claimable", round, r.worker)
			}
			if other, dup := claimed[r.run.ID]; dup {
				t.Fatalf("round %d: run %s double-claimed by %s and %s", round, r.run.ID, other, r.worker)
			}
			claimed[r.run.ID] = r.worker
			if r.run.WorkerID != r.worker {
				t.Errorf("round %d: run %s carries worker %q, want %q", round, r.run.ID, r.run.WorkerID, r.worker)
			}
		}
		if len(claimed) != 2 {
			t.Fatalf("round %d: claimed runs = %v, want both %s and %s", round, claimed, runA, runB)
		}
		// The stored rows agree.
		for runID, worker := range claimed {
			run, err := f.repo.FindByID(runID)
			if err != nil || run == nil {
				t.Fatalf("round %d: FindByID(%s): %v", round, runID, err)
			}
			if run.Status != agentruns.StatusClaimed || run.WorkerID != worker {
				t.Errorf("round %d: stored run %s = %s/%q, want claimed/%q", round, runID, run.Status, run.WorkerID, worker)
			}
		}
	}
}

// TestClaimConcurrentSingleRunClaimedOnce: with one queued run, exactly one
// of two racing workers wins; the other gets nil, not an error.
func TestClaimConcurrentSingleRunClaimedOnce(t *testing.T) {
	f := newClaimFixture(t)

	for round := 0; round < 5; round++ {
		runID := f.queueRun(t, runSpec{})
		type result struct {
			run *agentruns.Run
			err error
		}
		results := make(chan result, 2)
		var start sync.WaitGroup
		start.Add(1)
		for _, worker := range []string{"w-1", "w-2"} {
			go func(worker string) {
				start.Wait()
				run, err := f.repo.Claim(worker, f.orgID, "", claudeOnly, 0, false)
				results <- result{run, err}
			}(worker)
		}
		start.Done()

		var wins int
		for i := 0; i < 2; i++ {
			r := <-results
			if r.err != nil {
				t.Fatalf("round %d: claim error: %v", round, r.err)
			}
			if r.run != nil {
				if r.run.ID != runID {
					t.Fatalf("round %d: claimed unexpected run %s", round, r.run.ID)
				}
				wins++
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d workers claimed the single run, want exactly 1", round, wins)
		}
		// Drain: mark it finished so the next round starts clean.
		if _, err := f.db.Exec(`UPDATE agent_runs SET status = 'succeeded' WHERE id = $1`, runID); err != nil {
			t.Fatal(err)
		}
	}
}
