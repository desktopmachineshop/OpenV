package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Service is the sync path: it turns provider subscriptions into workspace
// entitlements and keeps the pricing catalogue confirmed.
type Service struct {
	provider Provider
	orgs     Orgs
	registry *Registry
	catalog  catalog
	metrics  Metrics
	interval time.Duration
	now      func() time.Time
	log      *slog.Logger
}

// New wires a service. A nil provider is the off switch: Enabled reports
// false and every method answers as if nothing is configured.
func New(provider Provider, orgSvc Orgs, registry *Registry, m Metrics) *Service {
	if m == nil {
		m = NoMetrics{}
	}
	if registry == nil {
		registry, _ = ParseRegistry("")
	}
	return &Service{
		provider: provider,
		orgs:     orgSvc,
		registry: registry,
		metrics:  m,
		interval: 5 * time.Minute,
		now:      time.Now,
		log:      slog.Default().With("component", "billing"),
	}
}

// Enabled reports whether a provider is configured.
func (s *Service) Enabled() bool { return s != nil && s.provider != nil }

// PublicPlans is the pricing page's answer, served from the cache.
func (s *Service) PublicPlans() PublicPlans {
	if s == nil {
		return PublicPlans{Currencies: []string{}, Plans: []PublicPlan{}}
	}
	return s.catalog.render(s.Enabled())
}

// RefreshPrices confirms every registered price against the provider: that
// it exists under this key (a test key with live ids, or the reverse,
// answers 404 — price ids carry no mode prefix, so this call is the only way
// to catch it), recurs on the declared interval, and bills per licence. The
// amounts it returns are the only source of the numbers the pricing page
// shows. A failure keeps the last good reading and is not fatal: boot must
// never depend on the provider being up.
func (s *Service) RefreshPrices(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	entries := s.registry.Entries()
	prices := map[string]*Price{}
	for _, e := range entries {
		p, err := s.provider.GetPrice(ctx, e.Price)
		if err != nil {
			return fmt.Errorf("price %s (%s %s): %w", e.Price, e.Plan, e.Interval, err)
		}
		if p.Interval != e.Interval {
			return fmt.Errorf("price %s is registered as %s %s but recurs every %s", e.Price, e.Plan, e.Interval, p.Interval)
		}
		if p.UsageType != "" && p.UsageType != "licensed" {
			return fmt.Errorf("price %s bills by %s; only a per-licence price can be sold", e.Price, p.UsageType)
		}
		prices[e.Price] = p
	}
	s.catalog.set(entries, prices, s.now())
	return nil
}

// RefreshOrg re-reads one workspace's subscription and applies it: the
// post-checkout return page calls this so the entitlement is live before the
// page renders. A provider failure returns the workspace unchanged with the
// error — never a downgrade.
func (s *Service) RefreshOrg(ctx context.Context, orgID string) (*orgs.Org, error) {
	org, err := s.orgs.Get(orgID)
	if err != nil {
		return nil, err
	}
	if !s.Enabled() || org.Billing.SubscriptionRef == "" {
		return org, nil
	}
	readAt := s.now()
	sub, err := s.provider.GetSubscription(ctx, org.Billing.SubscriptionRef)
	if err != nil {
		return org, err
	}
	s.apply(ctx, sub, nil, readAt)
	return s.orgs.Get(orgID)
}

// Reconcile is the self-healing backstop, run at boot and on every tick. It
// lists every subscription the provider holds, applies each, re-reads any
// stored subscription the listing did not mention, marks disputed ones,
// reports staleness, and refreshes the catalogue. Every step that fails
// leaves the last known state in place.
func (s *Service) Reconcile(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	readAt := s.now()

	disputed := map[string]bool{}
	if disputes, err := s.provider.ListOpenDisputes(ctx); err != nil {
		s.log.Warn("could not list disputes; none marked this tick", "error", err)
	} else {
		for _, d := range disputes {
			if d.SubscriptionID != "" {
				disputed[d.SubscriptionID] = true
			}
		}
	}

	seen := map[string]bool{}
	listed := true
	for cursor := ""; ; {
		subs, next, err := s.provider.ListSubscriptions(ctx, cursor)
		if err != nil {
			s.log.Warn("could not list subscriptions; entitlements left as they were", "error", err)
			listed = false
			break
		}
		for _, sub := range subs {
			seen[sub.ID] = true
			s.apply(ctx, sub, disputed, readAt)
		}
		if next == "" {
			break
		}
		cursor = next
	}

	billed, err := s.orgs.ListBillingOrgs(1000)
	if err != nil {
		s.log.Warn("could not list billed workspaces", "error", err)
		return
	}
	var oldest time.Duration
	for _, org := range billed {
		ref := org.Billing.SubscriptionRef
		if listed && !seen[ref] {
			// The provider did not mention a subscription we hold. Ask for
			// it by id rather than guess: a listing can lag a fresh
			// checkout by seconds.
			sub, err := s.provider.GetSubscription(ctx, ref)
			switch {
			case errors.Is(err, ErrNotFound):
				s.log.Warn("stored subscription unknown to the provider; left as is", "org_id", org.ID, "subscription", ref)
			case err != nil:
				s.log.Warn("could not read subscription; left as is", "org_id", org.ID, "subscription", ref, "error", err)
			default:
				s.apply(ctx, sub, disputed, readAt)
			}
		}
		if org.Billing.SyncedAt == nil {
			oldest = s.interval * 3 // never synced reads as already stale
		} else if age := readAt.Sub(*org.Billing.SyncedAt); age > oldest {
			oldest = age
		}
	}
	s.metrics.SyncStaleSeconds(oldest.Seconds())

	if err := s.RefreshPrices(ctx); err != nil {
		s.log.Warn("prices not confirmed; the pricing page keeps its last reading", "error", err)
	}
}

// apply writes one subscription snapshot to the workspace it belongs to.
//
// The workspace is found by the stored subscription ref first, then by the
// subscription's own metadata. Nothing is ever matched by email or name. A
// live subscription with no workspace — one whose workspace was purged, or
// bought against a workspace that no longer exists — is cancelled at once
// so nobody keeps paying for nothing. A price the registry does not know,
// or a subscription with more than one item, changes nothing and is counted.
func (s *Service) apply(ctx context.Context, sub *Subscription, disputed map[string]bool, readAt time.Time) {
	org, err := s.orgs.FindOrgByBillingRef(orgs.BillingRefSubscription, sub.ID)
	if err != nil {
		s.log.Warn("could not look up subscription", "subscription", sub.ID, "error", err)
		return
	}
	if org == nil {
		if orgID := sub.Metadata[OrgMetadataKey]; orgID != "" {
			candidate, err := s.orgs.Get(orgID)
			if err != nil && !errors.Is(err, orgs.ErrNotFound) {
				s.log.Warn("could not look up workspace named by subscription", "subscription", sub.ID, "org_id", orgID, "error", err)
				return
			}
			if candidate != nil {
				held := candidate.Billing.SubscriptionRef
				if held != "" && held != sub.ID && candidate.Billing.Live() {
					// Two live subscriptions claim one workspace. The
					// stored one is authoritative; this one is a human's
					// problem, loudly.
					s.log.Error("subscription names a workspace that already holds a live subscription; not bound",
						"subscription", sub.ID, "org_id", orgID, "held", held)
					return
				}
				org = candidate
			}
		}
	}
	if org == nil {
		if MapStatus(sub.Status) != orgs.PlanStatusCanceled && MapStatus(sub.Status) != orgs.PlanStatusIncomplete {
			s.log.Error("live subscription belongs to no workspace; cancelling it", "subscription", sub.ID, "status", sub.Status)
			if err := s.provider.CancelSubscription(ctx, sub.ID, false); err != nil {
				s.log.Error("could not cancel orphaned subscription", "subscription", sub.ID, "error", err)
			}
		}
		return
	}

	entry, known := s.registry.Lookup(sub.PriceID)
	if !known || sub.ItemCount != 1 {
		s.metrics.UnknownPrice()
		s.log.Error("subscription does not map to a plan; workspace left as it was",
			"subscription", sub.ID, "org_id", org.ID, "price", sub.PriceID, "items", sub.ItemCount)
		return
	}

	status := MapStatus(sub.Status)
	if disputed[sub.ID] {
		status = orgs.PlanStatusDisputed
	}
	state := orgs.BillingState{
		Plan:              entry.Plan,
		Status:            status,
		Interval:          entry.Interval,
		Seats:             sub.Quantity,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		SubscriptionRef:   sub.ID,
		ItemRef:           sub.ItemID,
		ReadAt:            readAt,
	}
	if !sub.CurrentPeriodEnd.IsZero() {
		end := sub.CurrentPeriodEnd
		state.PeriodEnd = &end
	}
	if _, err := s.orgs.ApplyBillingState(org.ID, state); err != nil {
		// A unique-index violation here is the tenant-confusion guard
		// firing: the subscription is already bound elsewhere.
		s.log.Error("could not apply subscription", "subscription", sub.ID, "org_id", org.ID, "error", err)
	}
}
