package notify

import (
	"testing"
	"time"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// fakeBudgetOrgs answers Get/ListMembers from fixed data and reproduces the
// real ClaimBudgetAlert dedupe: a claim wins only for a new month or a
// strictly higher threshold, monotonically — exactly the DB contract the
// monitor relies on for "once per threshold per month".
type fakeBudgetOrgs struct {
	org     *orgs.Org
	members []*orgs.Member

	claimMonth     string
	claimThreshold int
	claimCalls     int
}

func (f *fakeBudgetOrgs) Get(id string) (*orgs.Org, error) { return f.org, nil }

func (f *fakeBudgetOrgs) ListMembers(orgID string) ([]*orgs.Member, error) {
	return f.members, nil
}

func (f *fakeBudgetOrgs) ClaimBudgetAlert(orgID, month string, threshold int) (bool, error) {
	f.claimCalls++
	if month != f.claimMonth || threshold > f.claimThreshold {
		f.claimMonth = month
		f.claimThreshold = threshold
		return true, nil
	}
	return false, nil
}

// fakeSpend returns a settable month-to-date spend.
type fakeSpend struct{ spend float64 }

func (f *fakeSpend) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	return f.spend, nil
}

func budget(v float64) *float64 { return &v }

func orgMember(userID, role string) *orgs.Member {
	return &orgs.Member{UserID: userID, Role: role}
}

func runFinished(orgID string) domainevents.Event {
	return domainevents.Event{
		EventType: domainevents.RunFinished,
		OrgID:     orgID,
		EntityID:  "run-1",
		Actor:     "agent:run-1",
		Payload:   map[string]interface{}{"status": "succeeded"},
	}
}

func newBudgetFixture(budgetUSD *float64) (*fakeBudgetOrgs, *fakeSpend, *fakeStore) {
	orgSvc := &fakeBudgetOrgs{
		org: &orgs.Org{ID: "org-1", MonthlyBudgetUSD: budgetUSD},
		members: []*orgs.Member{
			orgMember("u-admin1", orgs.RoleAdmin),
			orgMember("u-admin2", orgs.RoleAdmin),
			orgMember("u-member", orgs.RoleMember),
		},
	}
	return orgSvc, &fakeSpend{}, &fakeStore{}
}

// TestBudgetAlertFiresToAdminsOnce locks in the core rule: crossing 80% alerts
// every admin exactly once (members excluded), and a second finish at the same
// level fires nothing more.
func TestBudgetAlertFiresToAdminsOnce(t *testing.T) {
	orgSvc, spend, store := newBudgetFixture(budget(100))
	spend.spend = 85 // 85% — crosses the 80% mark
	m := NewBudgetMonitor(orgSvc, spend, store, nil)

	m.Handle(runFinished("org-1"))

	if len(store.created) != 2 {
		t.Fatalf("created %d notifications, want 2 (both admins, no members)", len(store.created))
	}
	got := recipients(t, store)
	if got[0] != "u-admin1" || got[1] != "u-admin2" {
		t.Fatalf("recipients = %v, want the two admins", got)
	}
	for _, n := range store.created {
		if n.Type != notifications.TypeBudgetThreshold {
			t.Errorf("type = %q, want %q", n.Type, notifications.TypeBudgetThreshold)
		}
		if n.EntityRef["kind"] != "org_usage" || n.EntityRef["threshold"] != 80 {
			t.Errorf("entity_ref = %v, want kind=org_usage threshold=80", n.EntityRef)
		}
		if n.OrgID != "org-1" {
			t.Errorf("org_id = %q, want org-1", n.OrgID)
		}
	}

	// A second finish still under 100% must not re-alert 80%.
	store.created = nil
	m.Handle(runFinished("org-1"))
	if len(store.created) != 0 {
		t.Fatalf("re-alerted the 80%% threshold: created %d, want 0", len(store.created))
	}
}

// TestBudgetAlertEscalatesTo100 locks in escalation: after an 80% alert, a
// later finish that crosses 100% fires a second, distinct alert.
func TestBudgetAlertEscalatesTo100(t *testing.T) {
	orgSvc, spend, store := newBudgetFixture(budget(100))
	m := NewBudgetMonitor(orgSvc, spend, store, nil)

	spend.spend = 85
	m.Handle(runFinished("org-1"))
	if len(store.created) != 2 {
		t.Fatalf("80%% alert: created %d, want 2", len(store.created))
	}

	store.created = nil
	spend.spend = 105 // now over budget
	m.Handle(runFinished("org-1"))
	if len(store.created) != 2 {
		t.Fatalf("100%% alert: created %d, want 2", len(store.created))
	}
	if store.created[0].EntityRef["threshold"] != 100 {
		t.Errorf("threshold = %v, want 100", store.created[0].EntityRef["threshold"])
	}

	// A further over-budget finish does not re-alert 100%.
	store.created = nil
	m.Handle(runFinished("org-1"))
	if len(store.created) != 0 {
		t.Fatalf("re-alerted 100%%: created %d, want 0", len(store.created))
	}
}

// TestBudgetAlertGoesStraightTo100 locks in that a single finish that lands
// past 100% (never having crossed 80% first) fires exactly one 100% alert and
// no 80% alert.
func TestBudgetAlertGoesStraightTo100(t *testing.T) {
	orgSvc, spend, store := newBudgetFixture(budget(50))
	spend.spend = 200
	m := NewBudgetMonitor(orgSvc, spend, store, nil)

	m.Handle(runFinished("org-1"))
	if len(store.created) != 2 {
		t.Fatalf("created %d, want 2", len(store.created))
	}
	if store.created[0].EntityRef["threshold"] != 100 {
		t.Errorf("threshold = %v, want 100", store.created[0].EntityRef["threshold"])
	}
}

// TestBudgetAlertNoopCases locks in the silent paths: spend under 80%, no
// budget set, and non-RunFinished events all produce nothing.
func TestBudgetAlertNoopCases(t *testing.T) {
	t.Run("under 80% is silent", func(t *testing.T) {
		orgSvc, spend, store := newBudgetFixture(budget(100))
		spend.spend = 40
		m := NewBudgetMonitor(orgSvc, spend, store, nil)
		m.Handle(runFinished("org-1"))
		if len(store.created) != 0 || orgSvc.claimCalls != 0 {
			t.Fatalf("created=%d claims=%d, want 0/0", len(store.created), orgSvc.claimCalls)
		}
	})

	t.Run("no budget set is silent", func(t *testing.T) {
		orgSvc, spend, store := newBudgetFixture(nil)
		spend.spend = 9999
		m := NewBudgetMonitor(orgSvc, spend, store, nil)
		m.Handle(runFinished("org-1"))
		if len(store.created) != 0 {
			t.Fatalf("created %d, want 0 (no budget)", len(store.created))
		}
	})

	t.Run("non-run-finished events are ignored", func(t *testing.T) {
		orgSvc, spend, store := newBudgetFixture(budget(100))
		spend.spend = 200
		m := NewBudgetMonitor(orgSvc, spend, store, nil)
		m.Handle(domainevents.Event{EventType: domainevents.ProposalCreated, OrgID: "org-1"})
		if len(store.created) != 0 {
			t.Fatalf("created %d on a non-finish event, want 0", len(store.created))
		}
	})
}
