package postgres

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/automations"
)

// automationFixture is a database with one org ready for seeding automations.
type automationFixture struct {
	repo  *AutomationRepository
	orgID string
}

func newAutomationFixture(t *testing.T) *automationFixture {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)

	orgID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Test Org', 'test-org')`, orgID); err != nil {
		t.Fatal(err)
	}
	return &automationFixture{repo: NewAutomationRepository(db), orgID: orgID}
}

// seedDue inserts one enabled scheduled automation already due (next_run_at in
// the past) and returns its id.
func (f *automationFixture) seedDue(t *testing.T, next time.Time) string {
	t.Helper()
	id := uuid.New().String()
	a := &automations.Automation{
		ID:             id,
		OrgID:          f.orgID,
		Name:           "due-" + id,
		Kind:           automations.KindScheduled,
		Enabled:        true,
		PromptTemplate: "run",
		CronExpr:       "* * * * *",
		NextRunAt:      &next,
		EventFilter:    map[string]interface{}{},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := f.repo.Save(a); err != nil {
		t.Fatalf("seed automation: %v", err)
	}
	return id
}

// TestClaimDueScheduledIsExactlyOnce locks in the issue-#179 fix: two
// concurrent claim calls on the same due automation partition the row — exactly
// one wins — so replicated API schedulers never double-fire.
func TestClaimDueScheduledIsExactlyOnce(t *testing.T) {
	f := newAutomationFixture(t)
	past := time.Now().Add(-time.Minute)
	id := f.seedDue(t, past)

	next := time.Now().Add(time.Hour)
	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.repo.ClaimDueScheduled(id, time.Now(), next)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
	wins := 0
	for _, won := range results {
		if won {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("claims won = %d, want exactly 1 (double-fire otherwise)", wins)
	}

	// A follow-up claim now finds the row advanced out of the due window.
	if won, err := f.repo.ClaimDueScheduled(id, time.Now(), next); err != nil || won {
		t.Fatalf("re-claim after advance = %v, %v, want false, nil", won, err)
	}
}

// TestClaimDueScheduledPartitionsTheDueSet locks in that two concurrent
// schedulers sweeping the whole due set split it with zero overlap and zero
// loss: every due automation is claimed by exactly one of them.
func TestClaimDueScheduledPartitionsTheDueSet(t *testing.T) {
	f := newAutomationFixture(t)
	past := time.Now().Add(-time.Minute)
	const n = 25
	ids := make([]string, n)
	for i := range ids {
		ids[i] = f.seedDue(t, past)
	}

	next := time.Now().Add(time.Hour)
	claimAll := func(out map[string]bool, mu *sync.Mutex) func() {
		return func() {
			for _, id := range ids {
				won, err := f.repo.ClaimDueScheduled(id, time.Now(), next)
				if err != nil {
					t.Errorf("claim %s: %v", id, err)
					continue
				}
				if won {
					mu.Lock()
					out[id] = true
					mu.Unlock()
				}
			}
		}
	}

	var mu sync.Mutex
	a := map[string]bool{}
	b := map[string]bool{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); claimAll(a, &mu)() }()
	go func() { defer wg.Done(); claimAll(b, &mu)() }()
	wg.Wait()

	// No overlap: no automation claimed by both sweepers.
	for id := range a {
		if b[id] {
			t.Errorf("automation %s double-claimed by both schedulers", id)
		}
	}
	// Full coverage: every due automation claimed exactly once.
	claimed := map[string]bool{}
	for id := range a {
		claimed[id] = true
	}
	for id := range b {
		claimed[id] = true
	}
	if len(claimed) != n {
		t.Fatalf("claimed %d of %d due automations (some lost)", len(claimed), n)
	}
}

// TestClaimDueScheduledSkipsNotDue locks in that only enabled, scheduled, due
// rows can be claimed: an automation whose next_run_at is still in the future
// is never claimed.
func TestClaimDueScheduledSkipsNotDue(t *testing.T) {
	f := newAutomationFixture(t)
	future := time.Now().Add(time.Hour)
	id := f.seedDue(t, future) // not actually due

	next := time.Now().Add(2 * time.Hour)
	if won, err := f.repo.ClaimDueScheduled(id, time.Now(), next); err != nil || won {
		t.Fatalf("claim of a not-yet-due automation = %v, %v, want false, nil", won, err)
	}
}

// TestClaimDueScheduledLeavesLastRunAt pins the split of bug 74 (issue #379,
// decided under Q35): the claim advances next_run_at and leaves last_run_at
// as it was, and StampLastRun, which the scheduler calls once a run has
// launched, sets last_run_at and nothing else, so an occurrence that
// launched nothing is not shown as run, and a schedule edited between the
// claim and the stamp keeps its new next_run_at.
func TestClaimDueScheduledLeavesLastRunAt(t *testing.T) {
	f := newAutomationFixture(t)
	id := f.seedDue(t, time.Now().Add(-time.Minute))

	// UTC, since the column is a TIMESTAMP without a zone: it keeps the wall
	// clock it is given and reads back as UTC.
	next := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if won, err := f.repo.ClaimDueScheduled(id, time.Now(), next); err != nil || !won {
		t.Fatalf("claim = %v, %v, want true, nil", won, err)
	}
	a, err := f.repo.FindByID(id)
	if err != nil || a == nil {
		t.Fatalf("FindByID: %v, %v", a, err)
	}
	if a.LastRunAt != nil || a.NextRunAt == nil || !a.NextRunAt.Equal(next) {
		t.Fatalf("after the claim: last_run_at %v, next_run_at %v; want NULL and %v", a.LastRunAt, a.NextRunAt, next)
	}

	edited := next.Add(24 * time.Hour)
	a.NextRunAt = &edited
	if err := f.repo.Update(a); err != nil {
		t.Fatal(err)
	}
	launched := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	if err := f.repo.StampLastRun(id, launched); err != nil {
		t.Fatalf("StampLastRun: %v", err)
	}
	a, err = f.repo.FindByID(id)
	if err != nil || a == nil {
		t.Fatalf("FindByID: %v, %v", a, err)
	}
	if a.LastRunAt == nil || !a.LastRunAt.Equal(launched) || a.NextRunAt == nil || !a.NextRunAt.Equal(edited) ||
		!a.Enabled {
		t.Errorf("after the stamp: last_run_at %v, next_run_at %v, enabled %v; want %v, the edited %v, enabled",
			a.LastRunAt, a.NextRunAt, a.Enabled, launched, edited)
	}
}

// TestSwitchOffScheduledIsExactlyOnce pins the switch-off the scheduler makes
// of an automation whose cron expression no longer parses (bug 72 of issue
// #379, decided under Q34): of two concurrent calls on the same due
// automation exactly one wins, and the row is then disabled with no
// next_run_at and its last_run_at untouched, so it is no longer due and a
// further switch-off finds nothing to do.
func TestSwitchOffScheduledIsExactlyOnce(t *testing.T) {
	f := newAutomationFixture(t)
	id := f.seedDue(t, time.Now().Add(-time.Minute))

	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.repo.SwitchOffScheduled(id, time.Now())
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("switch-off %d: %v", i, err)
		}
	}
	if results[0] == results[1] {
		t.Fatalf("switch-offs won = %v, want exactly one", results)
	}
	a, err := f.repo.FindByID(id)
	if err != nil || a == nil {
		t.Fatalf("FindByID: %v, %v", a, err)
	}
	if a.Enabled || a.NextRunAt != nil || a.LastRunAt != nil {
		t.Errorf("after the switch-off: enabled %v, next_run_at %v, last_run_at %v; want false, NULL, NULL",
			a.Enabled, a.NextRunAt, a.LastRunAt)
	}
	if won, err := f.repo.SwitchOffScheduled(id, time.Now()); err != nil || won {
		t.Fatalf("a second switch-off = %v, %v, want false, nil", won, err)
	}
}

// TestSwitchOffScheduledSkipsNotDue pins that the switch-off holds to the
// claim's condition: an automation not yet due is left enabled with its
// next_run_at, as one whose cron an admin has just corrected is.
func TestSwitchOffScheduledSkipsNotDue(t *testing.T) {
	f := newAutomationFixture(t)
	future := time.Now().Add(time.Hour)
	id := f.seedDue(t, future) // not actually due

	if won, err := f.repo.SwitchOffScheduled(id, time.Now()); err != nil || won {
		t.Fatalf("switch-off of a not-yet-due automation = %v, %v, want false, nil", won, err)
	}
	a, err := f.repo.FindByID(id)
	if err != nil || a == nil {
		t.Fatalf("FindByID: %v, %v", a, err)
	}
	if !a.Enabled || a.NextRunAt == nil {
		t.Errorf("after a lost switch-off: enabled %v, next_run_at %v; want both as they were", a.Enabled, a.NextRunAt)
	}
}
