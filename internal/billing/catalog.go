package billing

import (
	"sort"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The catalogue: the registry's prices as the provider last confirmed them.
//
// The pricing page never calls the provider. It reads this cache, which the
// reconcile tick refreshes and which keeps its last good reading forever, so
// a provider outage never blanks the page and an unauthenticated endpoint
// can never be made to call the provider per request.

// PublicPlans is the pricing page's answer.
type PublicPlans struct {
	// BillingEnabled is false with no provider, and false until the prices
	// have been confirmed against it at least once.
	BillingEnabled bool `json:"billing_enabled"`
	// AsOf is when the amounts were last confirmed.
	AsOf *time.Time `json:"as_of,omitempty"`
	// Currencies lists every currency some price is offered in.
	Currencies []string     `json:"currencies"`
	Plans      []PublicPlan `json:"plans"`
}

// PublicPlan is one sellable plan with its intervals.
type PublicPlan struct {
	Plan string `json:"plan"`
	// PerSeat says the amount is per member; a flat plan bills once.
	PerSeat   bool                   `json:"per_seat"`
	Intervals map[string]PublicPrice `json:"intervals"`
}

// PublicPrice is one interval's amounts, per currency, in minor units.
type PublicPrice struct {
	Amounts     map[string]int64 `json:"amounts"`
	TaxBehavior string           `json:"tax_behavior,omitempty"`
}

// catalog is the cache behind PublicPlans.
type catalog struct {
	mu      sync.RWMutex
	prices  map[string]*Price // by price id
	asOf    time.Time
	ok      bool // at least one full confirmation succeeded
	entries []PriceEntry
}

func (c *catalog) set(entries []PriceEntry, prices map[string]*Price, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries, c.prices, c.asOf, c.ok = entries, prices, at, true
}

// amounts returns a confirmed price's amounts per currency, and whether the
// catalogue has been confirmed at all.
func (c *catalog) amounts(priceID string) (map[string]int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ok {
		return nil, false
	}
	p, ok := c.prices[priceID]
	if !ok {
		return map[string]int64{}, true
	}
	return p.Amounts, true
}

// render builds the public answer from the last confirmed reading.
func (c *catalog) render(enabled bool) PublicPlans {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := PublicPlans{Currencies: []string{}, Plans: []PublicPlan{}}
	if !enabled || !c.ok {
		return out
	}
	out.BillingEnabled = true
	asOf := c.asOf
	out.AsOf = &asOf
	byPlan := map[string]*PublicPlan{}
	currencies := map[string]bool{}
	for _, e := range c.entries {
		p, ok := c.prices[e.Price]
		if !ok {
			continue
		}
		plan := byPlan[e.Plan]
		if plan == nil {
			plan = &PublicPlan{Plan: e.Plan, PerSeat: e.Plan == orgs.PlanBusiness, Intervals: map[string]PublicPrice{}}
			byPlan[e.Plan] = plan
			out.Plans = append(out.Plans, *plan)
		}
		amounts := map[string]int64{}
		for cur, amt := range p.Amounts {
			amounts[cur] = amt
			currencies[cur] = true
		}
		plan.Intervals[e.Interval] = PublicPrice{Amounts: amounts, TaxBehavior: p.TaxBehavior}
	}
	// The slice holds copies taken before the intervals were filled in.
	for i := range out.Plans {
		out.Plans[i] = *byPlan[out.Plans[i].Plan]
	}
	for cur := range currencies {
		out.Currencies = append(out.Currencies, cur)
	}
	sort.Strings(out.Currencies)
	return out
}
