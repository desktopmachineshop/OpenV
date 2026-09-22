package notify

import (
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

type fakeMinutesOrgs struct {
	*fakeBudgetOrgs
}

func (f *fakeMinutesOrgs) ClaimMinutesAlert(orgID, month string, threshold int) (bool, error) {
	return f.ClaimBudgetAlert(orgID, month, threshold)
}

type fakeMinutes struct{ used int }

func (f *fakeMinutes) MinutesUsed(orgID string, since time.Time) (int, error) { return f.used, nil }

// Crossing 80% tells every admin once, crossing 100% once more, and a
// workspace with no ceiling is never told anything.
func TestMinutesAlertsAdminsOncePerThreshold(t *testing.T) {
	base, _, store := newBudgetFixture(nil)
	base.org.Limits = map[string]interface{}{orgs.LimitHostedRunnerMinutesMonth: 100}
	orgSvc := &fakeMinutesOrgs{fakeBudgetOrgs: base}
	minutes := &fakeMinutes{used: 50}
	m := NewMinutesMonitor(orgSvc, minutes, store, nil)

	m.Check("org-1")
	if len(store.created) != 0 {
		t.Fatalf("alerted at 50%%: %d", len(store.created))
	}
	minutes.used = 85
	m.Check("org-1")
	m.Check("org-1")
	if len(store.created) != 2 {
		t.Fatalf("80%% alerts = %d, want one per admin", len(store.created))
	}
	if n := store.created[0]; n.Type != notifications.TypeHostedMinutes || n.OrgID != "org-1" {
		t.Fatalf("notification: %+v", n)
	}
	minutes.used = 120
	m.Check("org-1")
	if len(store.created) != 4 {
		t.Fatalf("100%% alerts = %d, want two more", len(store.created))
	}

	base.org.Limits = nil
	m.Check("org-1")
	if len(store.created) != 4 {
		t.Fatal("an uncapped workspace was alerted")
	}
	var none *MinutesMonitor
	none.Check("org-1")
}
