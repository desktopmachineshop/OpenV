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
			results[i], errs[i] = f.repo.ClaimDueScheduled(id, time.Now(), &next)
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
	if won, err := f.repo.ClaimDueScheduled(id, time.Now(), &next); err != nil || won {
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
				won, err := f.repo.ClaimDueScheduled(id, time.Now(), &next)
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
	if won, err := f.repo.ClaimDueScheduled(id, time.Now(), &next); err != nil || won {
		t.Fatalf("claim of a not-yet-due automation = %v, %v, want false, nil", won, err)
	}
}
