package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// businessOrg is a workspace holding a live Business subscription billing
// `billed` seats.
func businessOrg(id string, billed int) *orgs.Org {
	return &orgs.Org{ID: id, BilledPlan: orgs.PlanBusiness, Billing: orgs.Billing{
		Status: orgs.PlanStatusActive, Seats: billed, SubscriptionRef: "sub_" + id, ItemRef: "si_sub_" + id, CustomerRef: "cus_" + id,
	}}
}

func seatService(t *testing.T, counts map[string]int, o *fakeOrgs) (*Service, *fakeProvider, *countingMetrics) {
	t.Helper()
	p := &fakeProvider{prices: confirmedPrices()}
	for id := range o.orgs {
		sub := business("sub_"+id, id, "active")
		sub.Quantity = o.orgs[id].Billing.Seats
		p.subs = append(p.subs, sub)
	}
	s, m := newTestService(p, o)
	s.SetSeatCounter(func(orgID string) (int, error) {
		n, ok := counts[orgID]
		if !ok {
			return 0, errors.New("uncounted")
		}
		return n, nil
	})
	return s, p, m
}

func TestSeatChangesCoalesceIntoOnePushPerWorkspace(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": businessOrg("o1", 3), "o2": businessOrg("o2", 3)}}
	s, p, _ := seatService(t, map[string]int{"o1": 5, "o2": 4}, o)

	for i := 0; i < 6; i++ {
		s.SeatsChanged("o1")
	}
	s.SeatsChanged("o2")
	s.SeatsChanged("missing")
	if n := s.FlushSeats(context.Background()); n != 3 {
		t.Fatalf("attempted %d, want 3 (o1 once, o2 once, the unknown one)", n)
	}
	if len(p.quantity) != 2 {
		t.Fatalf("pushes = %v, want one per workspace", p.quantity)
	}
	// The snapshot follows the push without waiting for a tick.
	if o.orgs["o1"].Billing.Seats != 5 || o.orgs["o2"].Billing.Seats != 4 {
		t.Fatalf("seats after push: o1=%d o2=%d", o.orgs["o1"].Billing.Seats, o.orgs["o2"].Billing.Seats)
	}
	// Nothing left to do: a second flush pushes nothing.
	s.SeatsChanged("o1")
	s.FlushSeats(context.Background())
	if len(p.quantity) != 2 {
		t.Fatalf("an unchanged count was pushed: %v", p.quantity)
	}
}

func TestSeatSyncNeverTouchesLiteOrLapsedOrUnbilled(t *testing.T) {
	lite := businessOrg("lite", 1)
	lite.BilledPlan = orgs.PlanBusinessLite
	lapsed := businessOrg("lapsed", 3)
	lapsed.Billing.Status = orgs.PlanStatusCanceled
	noItem := businessOrg("noitem", 3)
	noItem.Billing.ItemRef = ""
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"lite": lite, "lapsed": lapsed, "noitem": noItem}}
	s, p, _ := seatService(t, map[string]int{"lite": 9, "lapsed": 9, "noitem": 9}, o)

	for id := range o.orgs {
		s.SeatsChanged(id)
	}
	s.FlushSeats(context.Background())
	// The provider holds nothing for them either, so a tick leaves each as
	// it is and the drift pass sees the same three.
	p.subs = nil
	s.Reconcile(context.Background())
	if len(p.quantity) != 0 {
		t.Fatalf("pushed for a workspace that is not seat-billed: %v", p.quantity)
	}
}

func TestSeatPushFloorsAtOneAndRefusesAboveTheCeiling(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"empty": businessOrg("empty", 3), "huge": businessOrg("huge", 3)}}
	s, p, m := seatService(t, map[string]int{"empty": 0, "huge": 501}, o)

	s.SeatsChanged("empty")
	s.SeatsChanged("huge")
	s.FlushSeats(context.Background())
	if len(p.quantity) != 1 || p.quantity[0] != "si_sub_empty=1" {
		t.Fatalf("pushes = %v, want the floor of one and nothing for the runaway count", p.quantity)
	}
	if m.refused != 1 {
		t.Fatalf("refusals = %d, want 1", m.refused)
	}
	// A raised ceiling lets it through.
	s.SetMaxSeats(1000)
	s.SeatsChanged("huge")
	s.FlushSeats(context.Background())
	if len(p.quantity) != 2 || p.quantity[1] != "si_sub_huge=501" {
		t.Fatalf("pushes = %v", p.quantity)
	}
}

func TestReconcileRepairsSeatDriftBothWays(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"up": businessOrg("up", 3), "down": businessOrg("down", 3), "same": businessOrg("same", 3)}}
	s, p, m := seatService(t, map[string]int{"up": 8, "down": 2, "same": 3}, o)

	s.Reconcile(context.Background())
	got := map[string]bool{}
	for _, q := range p.quantity {
		got[q] = true
	}
	if len(p.quantity) != 2 || !got["si_sub_up=8"] || !got["si_sub_down=2"] {
		t.Fatalf("pushes = %v", p.quantity)
	}
	if m.drift != 2 {
		t.Fatalf("drift = %d, want 2", m.drift)
	}
	if o.orgs["up"].Billing.Seats != 8 || o.orgs["down"].Billing.Seats != 2 {
		t.Fatalf("snapshots: up=%d down=%d", o.orgs["up"].Billing.Seats, o.orgs["down"].Billing.Seats)
	}
	// The next tick finds nothing to do.
	s.Reconcile(context.Background())
	if len(p.quantity) != 2 || m.drift != 0 {
		t.Fatalf("second tick: pushes=%v drift=%d", p.quantity, m.drift)
	}
}

func TestSeatPushFailureLeavesTheSnapshotForTheNextTick(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": businessOrg("o1", 3)}}
	s, p, _ := seatService(t, map[string]int{"o1": 4}, o)
	p.qtyErr = errors.New("stripe is down")

	s.SeatsChanged("o1")
	s.FlushSeats(context.Background())
	if o.orgs["o1"].Billing.Seats != 3 {
		t.Fatalf("a failed push changed the snapshot: %d", o.orgs["o1"].Billing.Seats)
	}
	p.qtyErr = nil
	s.Reconcile(context.Background())
	if o.orgs["o1"].Billing.Seats != 4 || len(p.quantity) != 2 {
		t.Fatalf("not repaired: seats=%d pushes=%v", o.orgs["o1"].Billing.Seats, p.quantity)
	}
}

func TestSeatsChangedIsSafeWhenBillingIsOff(t *testing.T) {
	var none *Service
	none.SeatsChanged("o1")
	off := New(nil, &fakeOrgs{}, nil, nil)
	off.SeatsChanged("o1")
	if off.FlushSeats(context.Background()) != 0 {
		t.Fatal("a disabled service queued a seat change")
	}
}
