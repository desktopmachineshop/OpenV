package orgs

import (
	"errors"
	"time"
)

// Billing and entitlement.
//
// A workspace's plan is two facts, not one: the plan it is billed for
// (BilledPlan — a platform admin's grant or a subscription's tier) and
// whether that billing relationship is in good standing (Billing.Status).
// The entitled plan is what EffectiveLimits resolves, so a lapsed
// subscription drops a workspace to the free tier's limits without anyone
// touching the plan column, and a platform admin's grant — which has no
// billing relationship — is unaffected by any of it.
//
// No money lives here: no prices, no invoices, no card references. The
// provider's object ids are kept so the sync path can find its way back,
// and they are never serialised to a client.

// Billing statuses, mirroring what a subscription provider reports.
const (
	// PlanStatusNone is no billing relationship: every workspace on a
	// self-hosted deployment, every platform-admin grant, and every
	// workspace created before billing existed. The zero value.
	PlanStatusNone     = "none"
	PlanStatusTrialing = "trialing"
	PlanStatusActive   = "active"
	// PlanStatusPastDue is a failed renewal the provider is still retrying.
	// The workspace keeps its paid plan throughout: the provider's own
	// dunning schedule decides when retrying stops, and its terminal action
	// (which must be cancellation) is what ends the entitlement. Two clocks
	// on one fact would only disagree.
	PlanStatusPastDue    = "past_due"
	PlanStatusUnpaid     = "unpaid"
	PlanStatusPaused     = "paused"
	PlanStatusCanceled   = "canceled"
	PlanStatusIncomplete = "incomplete"
	// PlanStatusDisputed is a chargeback in progress on the subscription's
	// payments; treated as lapsed until it resolves.
	PlanStatusDisputed = "disputed"
)

// Billing reference kinds for FindOrgByBillingRef.
const (
	BillingRefCustomer     = "customer"
	BillingRefSubscription = "subscription"
)

// ErrBillingActive is returned when a platform admin tries to grant a plan
// to a workspace whose subscription is still live.
var ErrBillingActive = errors.New("the workspace has a live subscription; cancel it before granting a plan")

// Billing is the mirrored subscription snapshot on a workspace.
type Billing struct {
	// Status is one of the PlanStatus* values; "" reads as PlanStatusNone.
	Status string `json:"status"`
	// Interval is the billing period bought: "month" or "year".
	Interval string `json:"interval,omitempty"`
	// Seats is the quantity the provider currently bills, so the tab can
	// show drift against the member count without a provider call.
	Seats int `json:"seats,omitempty"`
	// PeriodEnd is when the current paid period ends: the renewal date, or
	// the last day of access after a cancellation.
	PeriodEnd *time.Time `json:"period_end,omitempty"`
	// CancelAtPeriodEnd distinguishes "ends on" from "renews on" while the
	// status is still active.
	CancelAtPeriodEnd bool `json:"cancel_at_period_end,omitempty"`
	// Grandfathered marks a workspace that keeps the alpha terms — every
	// count unlimited, every flag on — through its own limit overrides. The
	// overrides enforce it; this says so.
	Grandfathered bool `json:"grandfathered,omitempty"`
	// SyncedAt is when the snapshot was last read from the provider; the
	// staleness metric watches it and nothing acts on it.
	SyncedAt *time.Time `json:"synced_at,omitempty"`

	// Provider object ids and the customer's currency. Never serialised: a
	// member has no use for them and they name a commercial record.
	CustomerRef     string `json:"-"`
	SubscriptionRef string `json:"-"`
	ItemRef         string `json:"-"`
	Currency        string `json:"-"`
}

// Live reports whether the subscription is in a non-terminal state — one
// where the provider may still bill for it.
func (b Billing) Live() bool {
	switch b.Status {
	case PlanStatusTrialing, PlanStatusActive, PlanStatusPastDue:
		return true
	}
	return false
}

// GrantedPlan reports whether plan is one a platform admin grants rather
// than one a subscription buys. A grant wins over a subscription: the sync
// path never moves a workspace off one, and a checkout is refused on one.
func GrantedPlan(plan string) bool {
	return plan == PlanEnterprise || plan == PlanOpenSource
}

// EntitledPlan is the plan whose defaults apply RIGHT NOW: the billed plan
// while the billing relationship is in good standing, the free tier once it
// is not. A workspace with no billing relationship — every workspace on a
// self-hosted deployment, every platform-admin grant, every workspace from
// before billing existed — is on its billed plan. That is what keeps billing
// an optional module rather than a new rule.
//
// An unrecognised status drops to the free tier, mirroring PlanDefaults'
// stance that an unknown plan is never unlimited.
func (o *Org) EntitledPlan() string {
	switch o.Billing.Status {
	case "", PlanStatusNone, PlanStatusActive, PlanStatusTrialing, PlanStatusPastDue:
		return o.BilledPlan
	default:
		return PlanSingle
	}
}

// BillingState is one subscription snapshot as the sync path applies it.
type BillingState struct {
	// Plan is the tier the subscription's price maps to.
	Plan string
	// Status is a PlanStatus* value.
	Status string
	// Interval is "month" or "year".
	Interval string
	// Seats is the billed quantity.
	Seats int
	// PeriodEnd is the current period's end; nil when the provider reports
	// none.
	PeriodEnd *time.Time
	// CancelAtPeriodEnd is the provider's scheduled cancellation flag.
	CancelAtPeriodEnd bool
	// SubscriptionRef and ItemRef are the provider's ids.
	SubscriptionRef string
	ItemRef         string
	// ReadAt is when the snapshot was read from the provider. A snapshot
	// older than the row's last sync is not applied.
	ReadAt time.Time
}
