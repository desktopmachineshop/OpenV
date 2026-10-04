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

// TestMinutesAlertStatesTheUseNotTheThreshold is bug 65's rule applied to
// cloud runner minutes (bug 128): the 80% alert once ended "(80%)", which
// read as the share used when 85% had been. The body gives the minutes used
// against the allowance and nothing else.
func TestMinutesAlertStatesTheUseNotTheThreshold(t *testing.T) {
	base, _, store := newBudgetFixture(nil)
	base.org.Limits = map[string]interface{}{orgs.LimitHostedRunnerMinutesMonth: 100}
	NewMinutesMonitor(&fakeMinutesOrgs{fakeBudgetOrgs: base}, &fakeMinutes{used: 85}, store, nil).Check("org-1")
	if len(store.created) == 0 {
		t.Fatal("no alert was stored")
	}
	want := "This month's leased cloud runner time has reached 85 of the 100 minutes the workspace's plan allows."
	for _, n := range store.created {
		if n.Body != want {
			t.Errorf("an admin reads\n  %q\nwant\n  %q", n.Body, want)
		}
	}
}
