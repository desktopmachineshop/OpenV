package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The purchase path: checkout, plan change, portal, and binding a completed
// checkout to its workspace.
//
// The server decides everything the provider is told. A client sends a plan
// name, an interval and a currency; the price comes from the registry, the
// customer from the workspace, the quantity from the member count, the
// trial from the buyer's record. That single rule is what makes price and
// tenant confusion impossible from the outside.

// Buyer is the person starting a checkout.
type Buyer struct {
	ID    string
	Email string
	Name  string
	// TrialUsed says this person has already had their one trial.
	TrialUsed bool
}

// SetUsers attaches the user service the bind path records trials on.
func (s *Service) SetUsers(u Users) { s.users = u }

// SetSeatCounter attaches how a workspace's billed seats are counted:
// members plus pending invitations, the same reading the seat limit uses.
func (s *Service) SetSeatCounter(f func(orgID string) (int, error)) { s.seats = f }

// SetReturnURL sets the app origin the provider's pages return to.
func (s *Service) SetReturnURL(u string) { s.returnURL = strings.TrimRight(u, "/") }

// DefaultReturnURL sets the return origin only if none is configured.
func (s *Service) DefaultReturnURL(u string) {
	if s.returnURL == "" {
		s.SetReturnURL(u)
	}
}

// SetPortalConfig sets the provider's portal configuration id.
func (s *Service) SetPortalConfig(id string) { s.portalConfig = id }

// SetTrialDays sets the first-time buyer's trial; 0 disables it.
func (s *Service) SetTrialDays(days int) { s.trialDays = days }

// quantityFor is the billed quantity for a plan: Business bills per member
// and pending invitation; Lite is one person and is never seat-synced.
func (s *Service) quantityFor(plan, orgID string) int {
	if plan != orgs.PlanBusiness || s.seats == nil {
		return 1
	}
	n, err := s.seats(orgID)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// sellable checks that a plan may be bought for this workspace at all.
func sellable(org *orgs.Org, plan string) error {
	if orgs.GrantedPlan(org.BilledPlan) {
		return ErrGrantedPlan
	}
	if plan == orgs.PlanBusiness && org.OrgType == orgs.TypePersonal {
		return ErrPersonalWorkspace
	}
	return nil
}

// Checkout starts a hosted checkout for a workspace and returns the URL the
// browser is sent to. Refused when billing is off, the plan is not for sale,
// the workspace already has a live subscription, is on a granted plan, is
// personal and asks for Business, or asks for a currency other than the one
// its customer is locked to.
func (s *Service) Checkout(ctx context.Context, org *orgs.Org, buyer Buyer, plan, interval, currency string) (string, error) {
	if !s.Enabled() {
		return "", ErrNotConfigured
	}
	entry, ok := s.registry.Price(plan, interval)
	if !ok {
		return "", ErrUnknownPlan
	}
	if err := sellable(org, plan); err != nil {
		return "", err
	}
	if org.Billing.Live() {
		return "", ErrAlreadySubscribed
	}
	currency = strings.ToLower(strings.TrimSpace(currency))
	if org.Billing.Currency != "" {
		if currency == "" {
			currency = org.Billing.Currency
		} else if currency != org.Billing.Currency {
			return "", ErrCurrencyLocked
		}
	}
	amounts, confirmed := s.catalog.amounts(entry.Price)
	if !confirmed {
		return "", ErrPricesUnconfirmed
	}
	if currency == "" {
		currency = pickCurrency(amounts)
	}
	if _, offered := amounts[currency]; !offered {
		return "", ErrUnknownCurrency
	}

	customer := org.Billing.CustomerRef
	if customer == "" {
		id, err := s.provider.CreateCustomer(ctx, org.Name, buyer.Email,
			map[string]string{OrgMetadataKey: org.ID}, "openv:cust:"+org.ID+":v1")
		if err != nil {
			return "", err
		}
		if err := s.orgs.SetBillingCustomer(org.ID, id, currency); err != nil {
			return "", err
		}
		customer = id
	}

	meta := map[string]string{OrgMetadataKey: org.ID, PlanMetadataKey: plan}
	trial := 0
	if s.trialDays > 0 && !buyer.TrialUsed {
		trial = s.trialDays
		meta[TrialBuyerMetadataKey] = buyer.ID
	}
	bucket := s.now().Unix() / 300
	sess, err := s.provider.CreateCheckoutSession(ctx, CheckoutRequest{
		CustomerID:        customer,
		PriceID:           entry.Price,
		Quantity:          s.quantityFor(plan, org.ID),
		Currency:          currency,
		TrialDays:         trial,
		ClientReferenceID: org.ID,
		Metadata:          meta,
		SuccessURL:        s.returnURL + "/org/settings?tab=billing&checkout=done&session_id={CHECKOUT_SESSION_ID}",
		CancelURL:         s.returnURL + "/org/settings?tab=billing&checkout=cancelled",
		IdempotencyKey:    fmt.Sprintf("openv:co:%s:%s:%s:%s:%d", org.ID, plan, interval, currency, bucket),
	})
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

// pickCurrency is the display default: GBP where offered, else the first
// offered currency alphabetically, so the choice is stable.
func pickCurrency(amounts map[string]int64) string {
	if _, ok := amounts["gbp"]; ok {
		return "gbp"
	}
	best := ""
	for cur := range amounts {
		if best == "" || cur < best {
			best = cur
		}
	}
	return best
}

// BindCheckoutSession attaches a just-completed checkout to its workspace
// and applies the subscription, so the return page shows the entitlement
// before it renders. Nothing in the browser is trusted: the session is read
// from the provider and must name this workspace. A session that is not
// complete yet leaves the workspace as it is. A second live subscription
// arriving for a workspace that already holds one — two admins checking out
// at once — loses: it is cancelled at once and reported, so an operator can
// refund it.
func (s *Service) BindCheckoutSession(ctx context.Context, orgID, sessionID string) (*orgs.Org, error) {
	org, err := s.orgs.Get(orgID)
	if err != nil {
		return nil, err
	}
	if !s.Enabled() {
		return org, ErrNotConfigured
	}
	sess, err := s.provider.GetCheckoutSession(ctx, sessionID)
	if err != nil {
		return org, err
	}
	if sess.ClientReferenceID != orgID {
		s.log.Error("checkout session names another workspace; not bound", "org_id", orgID, "session", sessionID, "names", sess.ClientReferenceID)
		return org, ErrSessionMismatch
	}
	if sess.SubscriptionID == "" {
		return org, nil
	}
	held := org.Billing.SubscriptionRef
	if held != "" && held != sess.SubscriptionID && org.Billing.Live() {
		s.log.Error("a second subscription completed for a workspace that already holds a live one; cancelling the newer — refund it by hand",
			"org_id", orgID, "held", held, "newer", sess.SubscriptionID)
		if err := s.provider.CancelSubscription(ctx, sess.SubscriptionID); err != nil {
			s.log.Error("could not cancel the competing subscription", "subscription", sess.SubscriptionID, "error", err)
		}
		return org, ErrAlreadySubscribed
	}
	readAt := s.now()
	sub, err := s.provider.GetSubscription(ctx, sess.SubscriptionID)
	if err != nil {
		return org, err
	}
	s.apply(ctx, sub, nil, readAt)
	if buyer := sess.Metadata[TrialBuyerMetadataKey]; buyer != "" && s.users != nil {
		if err := s.users.MarkBillingTrialUsed(buyer); err != nil {
			s.log.Error("could not record the buyer's trial", "user_id", buyer, "error", err)
		}
	}
	return s.orgs.Get(orgID)
}

// ChangePlan moves a live subscription to another plan or interval in place
// — one subscription per workspace, prorated by the provider — and applies
// the result.
func (s *Service) ChangePlan(ctx context.Context, org *orgs.Org, plan, interval string) (*orgs.Org, error) {
	if !s.Enabled() {
		return org, ErrNotConfigured
	}
	entry, ok := s.registry.Price(plan, interval)
	if !ok {
		return org, ErrUnknownPlan
	}
	if err := sellable(org, plan); err != nil {
		return org, err
	}
	if !org.Billing.Live() || org.Billing.SubscriptionRef == "" || org.Billing.ItemRef == "" {
		return org, ErrNoSubscription
	}
	if err := s.provider.UpdateSubscriptionItem(ctx, org.Billing.SubscriptionRef, org.Billing.ItemRef, entry.Price, s.quantityFor(plan, org.ID)); err != nil {
		return org, err
	}
	readAt := s.now()
	sub, err := s.provider.GetSubscription(ctx, org.Billing.SubscriptionRef)
	if err != nil {
		return org, err
	}
	s.apply(ctx, sub, nil, readAt)
	return s.orgs.Get(org.ID)
}

// PortalURL opens the provider's self-service portal for a workspace's
// customer: payment method, address, tax id, invoices, cancel at period end.
func (s *Service) PortalURL(ctx context.Context, org *orgs.Org) (string, error) {
	if !s.Enabled() {
		return "", ErrNotConfigured
	}
	if org.Billing.CustomerRef == "" {
		return "", ErrNoCustomer
	}
	return s.provider.CreatePortalSession(ctx, org.Billing.CustomerRef, s.returnURL+"/org/settings?tab=billing", s.portalConfig)
}

var _ = time.Now
