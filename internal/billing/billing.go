// Package billing mirrors a subscription provider's view of each workspace
// into the entitlement the platform enforces (docs/plans/billing-stripe.md).
//
// The provider is the source of truth for money; this package holds none.
// It keeps a registry of which provider price means which plan, reads
// subscriptions on a schedule, and writes one snapshot per workspace through
// orgs.ApplyBillingState. A failed read never changes a plan. With no
// provider configured the package is inert: nothing starts, nothing dials
// out, and the endpoints that would use it answer that billing is
// unavailable.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Intervals a price may recur on.
const (
	IntervalMonth = "month"
	IntervalYear  = "year"
)

// OrgMetadataKey is the metadata key a subscription carries naming the
// workspace it was bought for. It is the discovery key for a subscription
// the platform never learned about; the stored ref is authoritative.
const OrgMetadataKey = "openv_org_id"

// ErrNotFound is what a provider returns for an object it does not have.
var ErrNotFound = errors.New("billing: not found")

// Refusals a purchase can meet. Each maps to one HTTP answer in the API.
var (
	ErrNotConfigured     = errors.New("billing is not available on this deployment")
	ErrUnknownPlan       = errors.New("that plan and interval are not for sale")
	ErrUnknownCurrency   = errors.New("that currency is not offered for this plan")
	ErrCurrencyLocked    = errors.New("this workspace already pays in another currency")
	ErrPricesUnconfirmed = errors.New("prices have not been confirmed with the billing provider yet")
	ErrAlreadySubscribed = errors.New("this workspace already has a live subscription")
	ErrNoSubscription    = errors.New("this workspace has no live subscription")
	ErrGrantedPlan       = errors.New("this workspace is on a plan a platform admin granted; there is nothing to buy")
	ErrPersonalWorkspace = errors.New("a personal workspace is one person; Business is for a shared workspace")
	ErrNoCustomer        = errors.New("this workspace has no billing customer yet")
	ErrSessionMismatch   = errors.New("that checkout belongs to another workspace")
)

// Metadata keys written on provider objects. openv_org_id names the
// workspace; openv_trial_buyer names the person whose one trial a checkout
// spends, so binding the subscription can record it.
const (
	TrialBuyerMetadataKey = "openv_trial_buyer"
	PlanMetadataKey       = "openv_plan"
)

// DefaultTrialDays is the trial a first-time buyer gets, card up front.
const DefaultTrialDays = 14

// Subscription is the slice of a provider subscription the platform reads.
// Only these fields are ever parsed: the plan is decided by PriceID through
// the registry, never by a product's name, nickname or metadata.
type Subscription struct {
	ID         string
	CustomerID string
	// Status is the provider's own status string; MapStatus turns it into a
	// PlanStatus.
	Status            string
	CancelAtPeriodEnd bool
	// CurrentPeriodEnd is zero when the provider reports none.
	CurrentPeriodEnd time.Time
	// ItemID, PriceID and Quantity describe the subscription's single item.
	// ItemCount says how many items the provider actually holds; more than
	// one means somebody did something in the dashboard the platform never
	// does, and the snapshot is not applied.
	ItemID    string
	PriceID   string
	Quantity  int
	ItemCount int
	Currency  string
	Metadata  map[string]string
}

// Price is the slice of a provider price the registry verifies and the
// pricing page renders.
type Price struct {
	ID string
	// Interval is the recurrence ("month", "year"); UsageType is the
	// provider's billing scheme, which must be per-licence.
	Interval  string
	UsageType string
	// Amounts maps a lowercase currency code to the unit amount in that
	// currency's minor unit (pence, cents), the default currency included.
	Amounts     map[string]int64
	TaxBehavior string
}

// CheckoutRequest is what a hosted checkout is created from. The server
// fills every field: a client never sends a price, a customer, a quantity or
// a currency.
type CheckoutRequest struct {
	CustomerID string
	PriceID    string
	Quantity   int
	Currency   string
	// TrialDays is 0 for no trial.
	TrialDays int
	// ClientReferenceID is the workspace id, echoed back on the session so
	// a bind can check the session names the workspace it claims.
	ClientReferenceID string
	// Metadata is written on the session and on the subscription it makes.
	Metadata       map[string]string
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

// CheckoutSession is the slice of a provider checkout the platform reads.
type CheckoutSession struct {
	ID                string
	URL               string
	ClientReferenceID string
	CustomerID        string
	// SubscriptionID is empty until the checkout completes.
	SubscriptionID string
	Status         string
	Metadata       map[string]string
}

// Dispute is an open chargeback and the subscription it concerns.
type Dispute struct {
	ID             string
	Status         string
	SubscriptionID string
}

// Provider is what a subscription provider must offer. The Stripe client is
// one implementation; tests use a fake that embeds the interface and
// implements only what a test needs.
type Provider interface {
	Name() string
	GetPrice(ctx context.Context, id string) (*Price, error)
	GetSubscription(ctx context.Context, id string) (*Subscription, error)
	// ListSubscriptions pages through every subscription, terminal ones
	// included, returning the cursor for the next page or "" at the end.
	ListSubscriptions(ctx context.Context, startingAfter string) ([]*Subscription, string, error)
	// CancelSubscription ends a subscription now.
	CancelSubscription(ctx context.Context, id string) error
	// SetCancelAtPeriodEnd schedules (or unschedules) cancellation for the
	// end of the paid period.
	SetCancelAtPeriodEnd(ctx context.Context, id string, on bool) error
	ListOpenDisputes(ctx context.Context) ([]Dispute, error)

	// CreateCustomer makes the provider's customer for a workspace and
	// returns its id. The idempotency key makes a retry return the same
	// customer rather than a second one.
	CreateCustomer(ctx context.Context, name, email string, metadata map[string]string, idempotencyKey string) (string, error)
	// CreateCheckoutSession starts a hosted checkout and returns it with
	// the URL the browser is sent to.
	CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error)
	GetCheckoutSession(ctx context.Context, id string) (*CheckoutSession, error)
	// CreatePortalSession returns the URL of the provider's self-service
	// portal for a customer; configuration may be empty for the default.
	CreatePortalSession(ctx context.Context, customerID, returnURL, configuration string) (string, error)
	// UpdateSubscriptionItem moves a subscription's single item to another
	// price and quantity, prorating — a plan change on one subscription.
	UpdateSubscriptionItem(ctx context.Context, subscriptionID, itemID, priceID string, quantity int) error
	// SetItemQuantity changes the billed quantity, prorating.
	SetItemQuantity(ctx context.Context, itemID string, quantity int) error
}

// Users is the slice of the user service the purchase path needs.
type Users interface {
	MarkBillingTrialUsed(userID string) error
}

// Orgs is the slice of the workspace service the sync path needs.
type Orgs interface {
	Get(id string) (*orgs.Org, error)
	FindOrgByBillingRef(kind, ref string) (*orgs.Org, error)
	ListBillingOrgs(limit int) ([]*orgs.Org, error)
	ApplyBillingState(orgID string, state orgs.BillingState) (bool, error)
	SetBillingCustomer(orgID, customerRef, currency string) error
}

// Metrics is what the package reports; internal/metrics implements it.
type Metrics interface {
	// ProviderRequest counts one call to the provider by operation and
	// HTTP status (0 for a transport failure).
	ProviderRequest(op string, status int)
	// UnknownPrice counts a subscription whose price is not in the
	// registry, or that has more than one item. Alert on any increase.
	UnknownPrice()
	// SyncStaleSeconds is the age of the oldest snapshot across billed
	// workspaces after a reconcile. Alert above three times the interval.
	SyncStaleSeconds(seconds float64)
}

// NoMetrics is the Metrics that reports nothing.
type NoMetrics struct{}

func (NoMetrics) ProviderRequest(string, int) {}
func (NoMetrics) UnknownPrice()               {}
func (NoMetrics) SyncStaleSeconds(float64)    {}

// MapStatus turns a provider status into a PlanStatus. Unrecognised values
// pass through unchanged, and EntitledPlan treats anything it does not
// know as lapsed — the safe direction.
func MapStatus(providerStatus string) string {
	switch providerStatus {
	case "trialing":
		return orgs.PlanStatusTrialing
	case "active":
		return orgs.PlanStatusActive
	case "past_due":
		return orgs.PlanStatusPastDue
	case "unpaid":
		return orgs.PlanStatusUnpaid
	case "paused":
		return orgs.PlanStatusPaused
	case "canceled":
		return orgs.PlanStatusCanceled
	case "incomplete", "incomplete_expired":
		return orgs.PlanStatusIncomplete
	}
	return providerStatus
}

// Config is the environment's billing configuration.
type Config struct {
	// SecretKey is the provider's secret API key; empty means billing off.
	SecretKey string
	// APIVersion pins the provider API version on every request; empty
	// leaves the account's own pinned version in charge.
	APIVersion string
	// Registry is the price map parsed from OPENV_STRIPE_PRICES.
	Registry *Registry
	// ReconcileInterval is how often subscriptions are re-read.
	ReconcileInterval time.Duration
	// ReturnURL is the app origin the provider's pages send people back to;
	// empty means the deployment's frontend URL.
	ReturnURL string
	// PortalConfig is the provider's portal configuration id; empty means
	// the account default.
	PortalConfig string
	// TrialDays is the first-time buyer's trial; 0 disables trials.
	TrialDays int
}

// Enabled reports whether a provider is configured.
func (c Config) Enabled() bool { return c.SecretKey != "" }

// ConfigFromEnv reads the billing configuration. A malformed price map is an
// error the caller should treat as fatal, like OPENV_LIMITS: a typo that
// silently sold nothing would look exactly like a price that does not work.
// The secret key's absence is not an error — it is the off switch.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		SecretKey:         strings.TrimSpace(getenv("STRIPE_SECRET_KEY")),
		APIVersion:        strings.TrimSpace(getenv("OPENV_STRIPE_API_VERSION")),
		ReconcileInterval: 5 * time.Minute,
		ReturnURL:         strings.TrimRight(strings.TrimSpace(getenv("OPENV_BILLING_RETURN_URL")), "/"),
		PortalConfig:      strings.TrimSpace(getenv("OPENV_BILLING_PORTAL_CONFIG")),
		TrialDays:         DefaultTrialDays,
	}
	if raw := strings.TrimSpace(getenv("OPENV_BILLING_TRIAL_DAYS")); raw != "" {
		var days int
		if _, err := fmt.Sscanf(raw, "%d", &days); err != nil || days < 0 {
			return cfg, fmt.Errorf("OPENV_BILLING_TRIAL_DAYS must be a whole number of days, got %q", raw)
		}
		cfg.TrialDays = days
	}
	reg, err := ParseRegistry(getenv("OPENV_STRIPE_PRICES"))
	if err != nil {
		return cfg, fmt.Errorf("OPENV_STRIPE_PRICES: %w", err)
	}
	cfg.Registry = reg
	if raw := strings.TrimSpace(getenv("OPENV_BILLING_RECONCILE_MINUTES")); raw != "" {
		var minutes int
		if _, err := fmt.Sscanf(raw, "%d", &minutes); err != nil || minutes < 1 {
			return cfg, fmt.Errorf("OPENV_BILLING_RECONCILE_MINUTES must be a whole number of minutes, got %q", raw)
		}
		cfg.ReconcileInterval = time.Duration(minutes) * time.Minute
	}
	return cfg, nil
}

// PriceEntry maps one provider price to a plan and an interval.
type PriceEntry struct {
	Price    string `json:"price"`
	Plan     string `json:"plan"`
	Interval string `json:"interval"`
}

// Registry is the price map: which provider price sells which plan for which
// interval. It is the ONLY thing that turns a subscription into a plan.
type Registry struct {
	byPrice map[string]PriceEntry
	byPlan  map[string]PriceEntry // plan + "/" + interval
	entries []PriceEntry
}

// sellablePlans are the plans a price may sell. Everything else — enterprise,
// open source, self-host, the legacy aliases — is granted, never bought, and
// a price map naming one is a misconfiguration rather than a sale.
var sellablePlans = map[string]bool{orgs.PlanBusinessLite: true, orgs.PlanBusiness: true}

// ParseRegistry reads the JSON array OPENV_STRIPE_PRICES holds. An empty
// value is an empty registry: billing may be on with nothing yet for sale.
func ParseRegistry(raw string) (*Registry, error) {
	r := &Registry{byPrice: map[string]PriceEntry{}, byPlan: map[string]PriceEntry{}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return r, nil
	}
	var entries []PriceEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("not a JSON array of {price, plan, interval}: %w", err)
	}
	seenPlan := map[string]bool{}
	for i, e := range entries {
		e.Price, e.Plan, e.Interval = strings.TrimSpace(e.Price), strings.TrimSpace(e.Plan), strings.TrimSpace(e.Interval)
		if e.Price == "" {
			return nil, fmt.Errorf("entry %d has no price id", i)
		}
		if !sellablePlans[e.Plan] {
			return nil, fmt.Errorf("entry %d sells plan %q; only business_lite and business are for sale", i, e.Plan)
		}
		if e.Interval != IntervalMonth && e.Interval != IntervalYear {
			return nil, fmt.Errorf("entry %d has interval %q; want month or year", i, e.Interval)
		}
		if _, dup := r.byPrice[e.Price]; dup {
			return nil, fmt.Errorf("price %s is listed twice", e.Price)
		}
		key := e.Plan + "/" + e.Interval
		if seenPlan[key] {
			return nil, fmt.Errorf("%s %s is sold by two prices; which one wins would be map order", e.Plan, e.Interval)
		}
		seenPlan[key] = true
		r.byPrice[e.Price] = e
		r.byPlan[key] = e
		r.entries = append(r.entries, e)
	}
	sort.Slice(r.entries, func(i, j int) bool {
		if r.entries[i].Plan != r.entries[j].Plan {
			return r.entries[i].Plan < r.entries[j].Plan
		}
		return r.entries[i].Interval < r.entries[j].Interval
	})
	return r, nil
}

// Lookup finds the plan and interval a price sells.
func (r *Registry) Lookup(priceID string) (PriceEntry, bool) {
	if r == nil {
		return PriceEntry{}, false
	}
	e, ok := r.byPrice[priceID]
	return e, ok
}

// Price finds the price that sells a plan for an interval.
func (r *Registry) Price(plan, interval string) (PriceEntry, bool) {
	if r == nil {
		return PriceEntry{}, false
	}
	e, ok := r.byPlan[plan+"/"+interval]
	return e, ok
}

// Entries lists the registry, plan then interval.
func (r *Registry) Entries() []PriceEntry {
	if r == nil {
		return nil
	}
	out := make([]PriceEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Len is how many prices are registered.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.entries)
}
