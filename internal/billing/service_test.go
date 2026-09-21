package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The sync path: a failed read never changes a plan; an unknown price never
// changes a plan; a subscription is bound by its stored ref, then by its
// own metadata, never by anything else; and a live subscription with no
// workspace is cancelled so nobody keeps paying for nothing.

// fakeProvider embeds the interface so a method a test never scripted
// panics loudly rather than answering a zero value.
type fakeProvider struct {
	Provider
	prices    map[string]*Price
	priceErr  error
	subs      []*Subscription
	listErr   error
	getErr    error
	disputes  []Dispute
	cancelled []string
	scheduled []string
	getCalls  []string
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) GetPrice(_ context.Context, id string) (*Price, error) {
	if f.priceErr != nil {
		return nil, f.priceErr
	}
	p, ok := f.prices[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (f *fakeProvider) GetSubscription(_ context.Context, id string) (*Subscription, error) {
	f.getCalls = append(f.getCalls, id)
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, s := range f.subs {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, ErrNotFound
}

func (f *fakeProvider) ListSubscriptions(_ context.Context, after string) ([]*Subscription, string, error) {
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	// Two subscriptions a page, so pagination is exercised.
	start := 0
	if after != "" {
		for i, s := range f.subs {
			if s.ID == after {
				start = i + 1
			}
		}
	}
	end := start + 2
	if end > len(f.subs) {
		end = len(f.subs)
	}
	page := f.subs[start:end]
	next := ""
	if end < len(f.subs) && len(page) > 0 {
		next = page[len(page)-1].ID
	}
	return page, next, nil
}

func (f *fakeProvider) CancelSubscription(_ context.Context, id string) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}

func (f *fakeProvider) SetCancelAtPeriodEnd(_ context.Context, id string, on bool) error {
	f.scheduled = append(f.scheduled, id+"="+map[bool]string{true: "on", false: "off"}[on])
	return nil
}

func (f *fakeProvider) ListOpenDisputes(context.Context) ([]Dispute, error) { return f.disputes, nil }

// fakeOrgs is an in-memory Orgs.
type fakeOrgs struct {
	orgs    map[string]*orgs.Org
	applied []orgs.BillingState
}

func (f *fakeOrgs) Get(id string) (*orgs.Org, error) {
	o, ok := f.orgs[id]
	if !ok {
		return nil, orgs.ErrNotFound
	}
	c := *o
	return &c, nil
}

func (f *fakeOrgs) FindOrgByBillingRef(kind, ref string) (*orgs.Org, error) {
	for _, o := range f.orgs {
		if (kind == orgs.BillingRefSubscription && o.Billing.SubscriptionRef == ref) ||
			(kind == orgs.BillingRefCustomer && o.Billing.CustomerRef == ref) {
			c := *o
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeOrgs) ListBillingOrgs(limit int) ([]*orgs.Org, error) {
	var out []*orgs.Org
	for _, o := range f.orgs {
		if o.Billing.SubscriptionRef != "" {
			c := *o
			out = append(out, &c)
		}
	}
	return out, nil
}

func (f *fakeOrgs) ApplyBillingState(orgID string, st orgs.BillingState) (bool, error) {
	o, ok := f.orgs[orgID]
	if !ok {
		return false, orgs.ErrNotFound
	}
	// The unique index, in miniature.
	for id, other := range f.orgs {
		if id != orgID && other.Billing.SubscriptionRef == st.SubscriptionRef {
			return false, errors.New("duplicate key value violates unique constraint")
		}
	}
	f.applied = append(f.applied, st)
	if !orgs.GrantedPlan(o.BilledPlan) {
		o.BilledPlan = st.Plan
	}
	o.Billing.Status, o.Billing.Interval, o.Billing.Seats = st.Status, st.Interval, st.Seats
	o.Billing.SubscriptionRef, o.Billing.ItemRef = st.SubscriptionRef, st.ItemRef
	o.Billing.PeriodEnd, o.Billing.CancelAtPeriodEnd = st.PeriodEnd, st.CancelAtPeriodEnd
	at := st.ReadAt
	o.Billing.SyncedAt = &at
	return true, nil
}

type countingMetrics struct {
	NoMetrics
	unknown int
	stale   float64
}

func (m *countingMetrics) UnknownPrice()              { m.unknown++ }
func (m *countingMetrics) SyncStaleSeconds(s float64) { m.stale = s }

var testRegistry, _ = ParseRegistry(`[
	{"price":"price_bm","plan":"business","interval":"month"},
	{"price":"price_lm","plan":"business_lite","interval":"month"}]`)

func business(id, orgID string, status string) *Subscription {
	return &Subscription{ID: id, Status: status, PriceID: "price_bm", ItemID: "si_" + id, Quantity: 3, ItemCount: 1,
		Metadata: map[string]string{OrgMetadataKey: orgID}, CurrentPeriodEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func newTestService(p *fakeProvider, o *fakeOrgs) (*Service, *countingMetrics) {
	m := &countingMetrics{}
	s := New(p, o, testRegistry, m)
	s.now = func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }
	return s, m
}

func TestDisabledServiceIsInert(t *testing.T) {
	s := New(nil, &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1"}}}, nil, nil)
	if s.Enabled() {
		t.Fatal("no provider should mean disabled")
	}
	if pp := s.PublicPlans(); pp.BillingEnabled || len(pp.Plans) != 0 || pp.Currencies == nil {
		t.Fatalf("disabled plans = %+v", pp)
	}
	s.Reconcile(context.Background()) // must not panic on the nil provider
	if err := s.RefreshPrices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o, err := s.RefreshOrg(context.Background(), "o1"); err != nil || o.ID != "o1" {
		t.Fatalf("RefreshOrg disabled: %v %v", o, err)
	}
	var none *Service
	if pp := none.PublicPlans(); pp.BillingEnabled {
		t.Fatal("a nil service sold something")
	}
}

func TestReconcileAppliesAndBindsByMetadata(t *testing.T) {
	p := &fakeProvider{subs: []*Subscription{business("sub_1", "o1", "active")}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", BilledPlan: orgs.PlanSingle}}}
	s, m := newTestService(p, o)
	s.Reconcile(context.Background())

	got := o.orgs["o1"]
	if got.BilledPlan != orgs.PlanBusiness || got.Billing.Status != orgs.PlanStatusActive || got.Billing.SubscriptionRef != "sub_1" ||
		got.Billing.ItemRef != "si_sub_1" || got.Billing.Seats != 3 || got.Billing.Interval != IntervalMonth || got.Billing.PeriodEnd == nil {
		t.Fatalf("not applied: plan=%q billing=%+v", got.BilledPlan, got.Billing)
	}
	if got.EntitledPlan() != orgs.PlanBusiness {
		t.Fatal("entitled plan did not follow")
	}
	if m.unknown != 0 || len(p.cancelled) != 0 {
		t.Fatalf("unknown=%d cancelled=%v", m.unknown, p.cancelled)
	}
	if m.stale != 0 {
		t.Fatalf("a just-synced workspace reads stale: %v", m.stale)
	}
}

func TestAFailedListingChangesNothing(t *testing.T) {
	p := &fakeProvider{listErr: errors.New("stripe is down")}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", BilledPlan: orgs.PlanBusiness,
		Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1"}}}}
	s, m := newTestService(p, o)
	s.Reconcile(context.Background())
	if len(o.applied) != 0 || o.orgs["o1"].Billing.Status != orgs.PlanStatusActive {
		t.Fatalf("a failed listing wrote: %+v", o.applied)
	}
	if len(p.getCalls) != 0 {
		t.Fatalf("a failed listing should not fall back to per-org reads it cannot trust either: %v", p.getCalls)
	}
	// Never synced: reads as stale, so the alert fires on a broken setup.
	if m.stale == 0 {
		t.Fatal("an unsynced workspace should read as stale")
	}
}

func TestAnUnknownPriceOrExtraItemLeavesThePlanAlone(t *testing.T) {
	unknown := business("sub_u", "o1", "active")
	unknown.PriceID = "price_someone_made_in_the_dashboard"
	multi := business("sub_m", "o2", "active")
	multi.ItemCount = 2
	p := &fakeProvider{subs: []*Subscription{unknown, multi}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{
		"o1": {ID: "o1", BilledPlan: orgs.PlanBusinessLite},
		"o2": {ID: "o2", BilledPlan: orgs.PlanBusinessLite},
	}}
	s, m := newTestService(p, o)
	s.Reconcile(context.Background())
	if len(o.applied) != 0 || o.orgs["o1"].BilledPlan != orgs.PlanBusinessLite || o.orgs["o2"].BilledPlan != orgs.PlanBusinessLite {
		t.Fatalf("an unmappable subscription was applied: %+v", o.applied)
	}
	if m.unknown != 2 {
		t.Fatalf("unknown price count = %d, want 2", m.unknown)
	}
	if len(p.cancelled) != 0 {
		t.Fatalf("an unmappable subscription was cancelled: %v", p.cancelled)
	}
}

func TestAnOrphanedLiveSubscriptionIsCancelled(t *testing.T) {
	live := business("sub_live", "purged-org", "active")
	dead := business("sub_dead", "purged-org", "canceled")
	p := &fakeProvider{subs: []*Subscription{live, dead}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{}}
	s, _ := newTestService(p, o)
	s.Reconcile(context.Background())
	if len(p.cancelled) != 1 || p.cancelled[0] != "sub_live" {
		t.Fatalf("cancelled = %v, want just the live orphan", p.cancelled)
	}
}

func TestAConflictingBindIsRefused(t *testing.T) {
	second := business("sub_2", "o1", "active")
	p := &fakeProvider{subs: []*Subscription{second}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", BilledPlan: orgs.PlanBusiness,
		Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1"}}}}
	s, _ := newTestService(p, o)
	s.Reconcile(context.Background())
	if o.orgs["o1"].Billing.SubscriptionRef != "sub_1" || len(o.applied) != 0 {
		t.Fatalf("the second subscription took over: %+v", o.orgs["o1"].Billing)
	}
	// Once the held subscription is over, the new one may bind.
	o.orgs["o1"].Billing.Status = orgs.PlanStatusCanceled
	s.Reconcile(context.Background())
	if o.orgs["o1"].Billing.SubscriptionRef != "sub_2" {
		t.Fatalf("a re-subscription did not bind: %+v", o.orgs["o1"].Billing)
	}
}

func TestADisputeMarksTheWorkspaceLapsed(t *testing.T) {
	p := &fakeProvider{subs: []*Subscription{business("sub_1", "o1", "active")}, disputes: []Dispute{{ID: "dp_1", Status: "needs_response", SubscriptionID: "sub_1"}}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", BilledPlan: orgs.PlanSingle}}}
	s, _ := newTestService(p, o)
	s.Reconcile(context.Background())
	got := o.orgs["o1"]
	if got.Billing.Status != orgs.PlanStatusDisputed || got.EntitledPlan() != orgs.PlanSingle {
		t.Fatalf("disputed: %+v entitled %q", got.Billing, got.EntitledPlan())
	}
	// The dispute closes; the next tick restores the provider's status.
	p.disputes = nil
	s.Reconcile(context.Background())
	if o.orgs["o1"].Billing.Status != orgs.PlanStatusActive {
		t.Fatalf("after the dispute: %+v", o.orgs["o1"].Billing)
	}
}

func TestAStoredSubscriptionTheListingMissedIsReRead(t *testing.T) {
	// The listing is empty (say it lags a fresh checkout) but the stored
	// subscription exists when asked for by id.
	sub := business("sub_1", "o1", "trialing")
	p := &fakeProvider{subs: []*Subscription{sub}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", BilledPlan: orgs.PlanSingle, Billing: orgs.Billing{SubscriptionRef: "sub_1"}}}}
	s, _ := newTestService(p, o)
	s.provider = &listlessProvider{fakeProvider: p}
	s.Reconcile(context.Background())
	if o.orgs["o1"].Billing.Status != orgs.PlanStatusTrialing {
		t.Fatalf("the missed subscription was not re-read: %+v", o.orgs["o1"].Billing)
	}
}

// listlessProvider lists nothing but answers a direct read.
type listlessProvider struct{ *fakeProvider }

func (l *listlessProvider) ListSubscriptions(context.Context, string) ([]*Subscription, string, error) {
	return nil, "", nil
}

func TestRefreshOrgNeverDowngradesOnAFailedRead(t *testing.T) {
	p := &fakeProvider{getErr: errors.New("timeout")}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", BilledPlan: orgs.PlanBusiness,
		Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1"}}}}
	s, _ := newTestService(p, o)
	got, err := s.RefreshOrg(context.Background(), "o1")
	if err == nil {
		t.Fatal("a failed read should be reported")
	}
	if got == nil || got.Billing.Status != orgs.PlanStatusActive || len(o.applied) != 0 {
		t.Fatalf("a failed read changed the workspace: %+v", got)
	}

	// And a good read applies.
	p.getErr = nil
	p.subs = []*Subscription{business("sub_1", "o1", "past_due")}
	got, err = s.RefreshOrg(context.Background(), "o1")
	if err != nil || got.Billing.Status != orgs.PlanStatusPastDue || got.EntitledPlan() != orgs.PlanBusiness {
		t.Fatalf("refresh: %v %+v", err, got.Billing)
	}
	// A workspace with no subscription is returned as is, no provider call.
	o.orgs["o2"] = &orgs.Org{ID: "o2"}
	calls := len(p.getCalls)
	if _, err := s.RefreshOrg(context.Background(), "o2"); err != nil || len(p.getCalls) != calls {
		t.Fatalf("an unsubscribed workspace reached the provider: %v", err)
	}
}

func TestRefreshPricesConfirmsAndKeepsLastGood(t *testing.T) {
	p := &fakeProvider{prices: map[string]*Price{
		"price_bm": {ID: "price_bm", Interval: IntervalMonth, UsageType: "licensed", Amounts: map[string]int64{"gbp": 1200, "usd": 1500, "eur": 1400}, TaxBehavior: "exclusive"},
		"price_lm": {ID: "price_lm", Interval: IntervalMonth, UsageType: "licensed", Amounts: map[string]int64{"gbp": 900}},
	}}
	s, _ := newTestService(p, &fakeOrgs{orgs: map[string]*orgs.Org{}})

	// Enabled but unconfirmed: nothing is for sale yet.
	if pp := s.PublicPlans(); pp.BillingEnabled {
		t.Fatal("unconfirmed prices were offered")
	}
	if err := s.RefreshPrices(context.Background()); err != nil {
		t.Fatal(err)
	}
	pp := s.PublicPlans()
	if !pp.BillingEnabled || pp.AsOf == nil || len(pp.Plans) != 2 {
		t.Fatalf("plans = %+v", pp)
	}
	if got := pp.Currencies; len(got) != 3 || got[0] != "eur" || got[1] != "gbp" || got[2] != "usd" {
		t.Fatalf("currencies = %v", got)
	}
	var biz PublicPlan
	for _, pl := range pp.Plans {
		if pl.Plan == orgs.PlanBusiness {
			biz = pl
		}
	}
	if !biz.PerSeat || biz.Intervals[IntervalMonth].Amounts["usd"] != 1500 || biz.Intervals[IntervalMonth].TaxBehavior != "exclusive" {
		t.Fatalf("business = %+v", biz)
	}
	for _, pl := range pp.Plans {
		if pl.Plan == orgs.PlanBusinessLite && pl.PerSeat {
			t.Fatal("Lite is flat, not per seat")
		}
	}

	// A later failure keeps the last reading.
	p.priceErr = errors.New("stripe is down")
	if err := s.RefreshPrices(context.Background()); err == nil {
		t.Fatal("a failed refresh should be reported")
	}
	if again := s.PublicPlans(); !again.BillingEnabled || len(again.Plans) != 2 {
		t.Fatalf("the last good reading was lost: %+v", again)
	}

	// A price recurring on the wrong interval, or billed by usage, is
	// refused: it would sell something other than what was declared.
	p.priceErr = nil
	p.prices["price_bm"].Interval = IntervalYear
	if err := s.RefreshPrices(context.Background()); err == nil {
		t.Fatal("an interval mismatch was accepted")
	}
	p.prices["price_bm"].Interval = IntervalMonth
	p.prices["price_bm"].UsageType = "metered"
	if err := s.RefreshPrices(context.Background()); err == nil {
		t.Fatal("a metered price was accepted")
	}
}

// Deleting a workspace schedules its subscription to end with the paid
// period; restoring it takes that back. Neither touches a workspace with no
// live subscription, and neither runs without a provider.
func TestDeleteAndRestoreScheduleAndResumeCancellation(t *testing.T) {
	p := &fakeProvider{}
	s, _ := newTestService(p, &fakeOrgs{orgs: map[string]*orgs.Org{}})
	live := &orgs.Org{ID: "o1", Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1"}}
	lapsed := &orgs.Org{ID: "o2", Billing: orgs.Billing{Status: orgs.PlanStatusCanceled, SubscriptionRef: "sub_2"}}
	none := &orgs.Org{ID: "o3"}

	for _, o := range []*orgs.Org{live, lapsed, none, nil} {
		s.OnWorkspaceDeleted(context.Background(), o)
	}
	if len(p.scheduled) != 1 || p.scheduled[0] != "sub_1=on" {
		t.Fatalf("scheduled = %v", p.scheduled)
	}
	s.OnWorkspaceRestored(context.Background(), live)
	s.OnWorkspaceRestored(context.Background(), lapsed)
	if len(p.scheduled) != 2 || p.scheduled[1] != "sub_1=off" {
		t.Fatalf("scheduled = %v", p.scheduled)
	}
	off := New(nil, &fakeOrgs{}, nil, nil)
	off.OnWorkspaceDeleted(context.Background(), live) // must not touch the nil provider
}

func TestStalenessReportsTheOldestSnapshot(t *testing.T) {
	old := time.Date(2026, 9, 21, 11, 0, 0, 0, time.UTC) // an hour before now
	p := &fakeProvider{subs: []*Subscription{business("sub_1", "o1", "active")}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{
		"o1": {ID: "o1", BilledPlan: orgs.PlanBusiness, Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1", SyncedAt: &old}},
		"o2": {ID: "o2", BilledPlan: orgs.PlanBusiness, Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_gone", SyncedAt: &old}},
	}}
	s, m := newTestService(p, o)
	s.Reconcile(context.Background())
	// o1 was re-synced; o2's subscription is unknown to the provider and
	// was left alone, so it is the stale one: an hour.
	if m.stale != 3600 {
		t.Fatalf("stale = %v, want 3600", m.stale)
	}
	if o.orgs["o2"].Billing.Status != orgs.PlanStatusActive {
		t.Fatal("a subscription unknown to the provider was changed")
	}
}
