package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The billing writers (migration 45): one subscription can never entitle two
// workspaces, an older read never overwrites a newer one, a granted plan is
// never moved, and the first move onto a channel-choosing plan pins the
// workspace to nightly so nothing gated by the stable channel disappears.

func seedBillingOrg(t *testing.T, f *claimFixture, plan string) string {
	t.Helper()
	id := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO organizations (id, name, slug, org_type, plan) VALUES ($1, 'Billing Org', $2, 'company', $3)`, id, "billing-"+id[:8], plan); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBillingColumnsRoundTrip(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	id := seedBillingOrg(t, f, orgs.PlanSingle)

	if err := repo.SetBillingCustomer(id, "cus_1", "gbp"); err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	applied, err := repo.ApplyBillingState(id, orgs.BillingState{
		Plan: orgs.PlanBusiness, Status: orgs.PlanStatusActive, Interval: "month", Seats: 7,
		PeriodEnd: &end, CancelAtPeriodEnd: true, SubscriptionRef: "sub_1", ItemRef: "si_1",
		ReadAt: time.Now().UTC(),
	})
	if err != nil || !applied {
		t.Fatalf("apply: applied=%v err=%v", applied, err)
	}
	o, err := repo.FindOrgByID(id)
	if err != nil {
		t.Fatal(err)
	}
	b := o.Billing
	if o.BilledPlan != orgs.PlanBusiness || b.Status != orgs.PlanStatusActive || b.Interval != "month" || b.Seats != 7 ||
		!b.CancelAtPeriodEnd || b.CustomerRef != "cus_1" || b.SubscriptionRef != "sub_1" || b.ItemRef != "si_1" || b.Currency != "gbp" ||
		b.PeriodEnd == nil || !b.PeriodEnd.Equal(end) || b.SyncedAt == nil {
		t.Fatalf("round trip lost something: plan=%q billing=%+v", o.BilledPlan, b)
	}
	if o.EntitledPlan() != orgs.PlanBusiness {
		t.Fatalf("entitled to %q", o.EntitledPlan())
	}

	// Finders.
	if got, err := repo.FindOrgByBillingRef(orgs.BillingRefSubscription, "sub_1"); err != nil || got == nil || got.ID != id {
		t.Fatalf("by subscription: %v %v", got, err)
	}
	if got, err := repo.FindOrgByBillingRef(orgs.BillingRefCustomer, "cus_1"); err != nil || got == nil || got.ID != id {
		t.Fatalf("by customer: %v %v", got, err)
	}
	if got, _ := repo.FindOrgByBillingRef(orgs.BillingRefSubscription, ""); got != nil {
		t.Fatal("an empty ref matched a row")
	}
	list, err := repo.ListBillingOrgs(10)
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("ListBillingOrgs = %v, %v", list, err)
	}

	// Clearing keeps the customer and marks the lapse.
	if err := repo.ClearBillingSubscription(id); err != nil {
		t.Fatal(err)
	}
	o, _ = repo.FindOrgByID(id)
	if o.Billing.SubscriptionRef != "" || o.Billing.CustomerRef != "cus_1" || o.Billing.Status != orgs.PlanStatusCanceled {
		t.Fatalf("after clear: %+v", o.Billing)
	}
	if o.EntitledPlan() != orgs.PlanSingle {
		t.Fatalf("a cleared subscription still entitles %q", o.EntitledPlan())
	}
	if err := repo.SetGrandfathered(id, true); err != nil {
		t.Fatal(err)
	}
	o, _ = repo.FindOrgByID(id)
	if !o.Billing.Grandfathered {
		t.Fatal("grandfathered flag did not stick")
	}
}

func TestOneSubscriptionCannotEntitleTwoWorkspaces(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	a := seedBillingOrg(t, f, orgs.PlanSingle)
	b := seedBillingOrg(t, f, orgs.PlanSingle)
	now := time.Now().UTC()

	state := orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusActive, SubscriptionRef: "sub_shared", ItemRef: "si", ReadAt: now}
	if _, err := repo.ApplyBillingState(a, state); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyBillingState(b, state); err == nil {
		t.Fatal("the same subscription was attached to a second workspace")
	}
	if err := repo.SetBillingCustomer(a, "cus_shared", "gbp"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetBillingCustomer(b, "cus_shared", "gbp"); err == nil {
		t.Fatal("the same customer was attached to a second workspace")
	}
}

func TestAnOlderReadNeverOverwritesANewerOne(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	id := seedBillingOrg(t, f, orgs.PlanSingle)
	newer := time.Now().UTC()
	older := newer.Add(-time.Minute)

	if applied, err := repo.ApplyBillingState(id, orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1", ReadAt: newer}); err != nil || !applied {
		t.Fatalf("first write: %v %v", applied, err)
	}
	applied, err := repo.ApplyBillingState(id, orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusCanceled, SubscriptionRef: "sub_1", ReadAt: older})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("a snapshot read before the last sync was applied")
	}
	o, _ := repo.FindOrgByID(id)
	if o.Billing.Status != orgs.PlanStatusActive {
		t.Fatalf("the older snapshot won: %+v", o.Billing)
	}
	// An equal read time is applied: the same snapshot re-read is a no-op
	// write, not a lost one.
	if applied, _ := repo.ApplyBillingState(id, orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusPastDue, SubscriptionRef: "sub_1", ReadAt: newer}); !applied {
		t.Fatal("a snapshot with the same read time was refused")
	}
}

func TestAGrantWinsOverASubscription(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	for _, plan := range []string{orgs.PlanEnterprise, orgs.PlanOpenSource} {
		id := seedBillingOrg(t, f, plan)
		if _, err := repo.ApplyBillingState(id, orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusActive, SubscriptionRef: "sub_" + plan, ReadAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		o, _ := repo.FindOrgByID(id)
		if o.BilledPlan != plan {
			t.Errorf("a subscription moved a %s grant to %q", plan, o.BilledPlan)
		}
		if o.Billing.Status != orgs.PlanStatusActive {
			t.Errorf("the snapshot's other columns were not written on a %s grant: %+v", plan, o.Billing)
		}
	}
}

func TestFirstMoveOntoAChoosingPlanPinsNightly(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	now := time.Now().UTC()

	// Single → Business with no override: nightly is written, so the
	// workspace's channel does not silently become stable.
	id := seedBillingOrg(t, f, orgs.PlanSingle)
	if _, err := repo.ApplyBillingState(id, orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusActive, SubscriptionRef: "sub_a", ReadAt: now}); err != nil {
		t.Fatal(err)
	}
	o, _ := repo.FindOrgByID(id)
	if o.ReleaseChannelOverride != orgs.ChannelNightly || o.ReleaseChannel != orgs.ChannelNightly {
		t.Fatalf("channel after the flip: override=%q effective=%q, want nightly", o.ReleaseChannelOverride, o.ReleaseChannel)
	}
	// A second apply on an already-choosing plan leaves the override alone,
	// as does an admin's own choice of stable.
	if err := repo.SetReleaseChannel(id, orgs.ChannelStable); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyBillingState(id, orgs.BillingState{Plan: orgs.PlanBusiness, Status: orgs.PlanStatusActive, SubscriptionRef: "sub_a", ReadAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	o, _ = repo.FindOrgByID(id)
	if o.ReleaseChannelOverride != orgs.ChannelStable {
		t.Fatalf("a later apply rewrote the admin's choice: %q", o.ReleaseChannelOverride)
	}

	// Single → Business Lite is nightly-only to nightly-only: no override.
	lite := seedBillingOrg(t, f, orgs.PlanSingle)
	if _, err := repo.ApplyBillingState(lite, orgs.BillingState{Plan: orgs.PlanBusinessLite, Status: orgs.PlanStatusActive, SubscriptionRef: "sub_b", ReadAt: now}); err != nil {
		t.Fatal(err)
	}
	o, _ = repo.FindOrgByID(lite)
	if o.ReleaseChannelOverride != "" {
		t.Fatalf("a nightly-only plan got an override: %q", o.ReleaseChannelOverride)
	}
}

func TestUpdateOrgWritesOnlyTheName(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	id := seedBillingOrg(t, f, orgs.PlanBusiness)
	o, _ := repo.FindOrgByID(id)
	// A stale in-memory copy must not put its plan back.
	o.BilledPlan = orgs.PlanSingle
	o.Name = "Renamed"
	o.Limits = map[string]interface{}{orgs.LimitMaxMembers: 1}
	if err := repo.UpdateOrg(o); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindOrgByID(id)
	if got.Name != "Renamed" || got.BilledPlan != orgs.PlanBusiness || len(got.Limits) != 0 {
		t.Fatalf("UpdateOrg wrote more than the name: %+v", got)
	}
}

// The grandfather step (Phase 4): a workspace created before the announced
// date keeps the alpha terms through its own limits, deleted ones included,
// a key the operator already pinned is kept, a workspace created after it
// is untouched, and a second run changes nothing.
func TestGrandfatherBeforeKeepsTheAlphaTerms(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	// A date in the past, so the fixture's own workspace (created now) is
	// on the "after" side with the one seeded there.
	cutoff := time.Now().UTC().Add(-24 * time.Hour)

	before := seedBillingOrg(t, f, orgs.PlanSingle)
	pinned := seedBillingOrg(t, f, orgs.PlanSingle)
	deleted := seedBillingOrg(t, f, orgs.PlanSingle)
	after := seedBillingOrg(t, f, orgs.PlanSingle)
	for _, id := range []string{before, pinned, deleted} {
		if _, err := f.db.Exec(`UPDATE organizations SET created_at = $2 WHERE id = $1`, id, cutoff.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`UPDATE organizations SET limits = '{"max_projects": 7}' WHERE id = $1`, pinned); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE organizations SET deleted_at = NOW() WHERE id = $1`, deleted); err != nil {
		t.Fatal(err)
	}

	n, err := repo.GrandfatherBefore(cutoff, orgs.AlphaTerms())
	if err != nil || n != 3 {
		t.Fatalf("GrandfatherBefore = %d, %v; want 3", n, err)
	}

	// FindOrgByID skips deleted rows, so read every row the same direct way.
	read := func(id string) *orgs.Org {
		rows, err := f.db.Query(`SELECT `+orgColumns+` FROM organizations WHERE id = $1`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("no row for %s", id)
		}
		o, err := scanOrg(rows)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}

	for _, id := range []string{before, deleted} {
		o := read(id)
		if !o.Billing.Grandfathered {
			t.Errorf("%s not marked grandfathered", id)
		}
		if v, _ := orgs.LimitInt(o.Limits, orgs.LimitMaxMembers); v != 0 {
			t.Errorf("%s members override = %d, want 0 (unlimited)", id, v)
		}
		if !orgs.Allowed(o.Limits, orgs.LimitTeams) {
			t.Errorf("%s lost teams", id)
		}
	}
	if o := read(pinned); !o.Billing.Grandfathered {
		t.Error("pinned workspace not grandfathered")
	} else if v, _ := orgs.LimitInt(o.Limits, orgs.LimitMaxProjects); v != 7 {
		t.Errorf("the operator's pin was overwritten: max_projects = %d", v)
	} else if v, _ := orgs.LimitInt(o.Limits, orgs.LimitMaxMembers); v != 0 {
		t.Errorf("pinned workspace did not get the other terms: %d", v)
	}
	if o := read(after); o.Billing.Grandfathered || len(o.Limits) != 0 {
		t.Errorf("a workspace created after the date was touched: %+v", o.Limits)
	}

	// Idempotent: the same boot again changes nothing.
	if n, err := repo.GrandfatherBefore(cutoff, orgs.AlphaTerms()); err != nil || n != 0 {
		t.Fatalf("second run changed %d rows, %v", n, err)
	}
}
