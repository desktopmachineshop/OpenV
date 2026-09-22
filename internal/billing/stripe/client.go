// Package stripe is the platform's Stripe client: nine calls, form-encoded
// in and JSON out, behind the billing.Provider interface. It is hand-rolled
// rather than the vendor SDK so that the API version is one setting, the
// retry and idempotency rules live in one function, and only the fields the
// platform reads are ever parsed.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/billing"
)

// DefaultBaseURL is Stripe's API origin.
const DefaultBaseURL = "https://api.stripe.com"

// maxAttempts bounds retries of one call. Only a rate limit, a server error
// or a transport failure is retried, and only on calls that are safe to
// repeat: reads, and writes carrying an idempotency key.
const maxAttempts = 3

// APIError is a non-2xx answer from Stripe.
type APIError struct {
	Status    int
	Type      string
	Code      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("stripe: %d %s/%s (request %s)", e.Status, e.Type, e.Code, e.RequestID)
}

// Unwrap lets errors.Is(err, billing.ErrNotFound) work on a 404.
func (e *APIError) Unwrap() error {
	if e.Status == http.StatusNotFound {
		return billing.ErrNotFound
	}
	return nil
}

// Retryable reports whether the same request may be sent again.
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Client talks to one Stripe account.
type Client struct {
	key     string
	base    string
	version string
	http    *http.Client
	metrics billing.Metrics
	sleep   func(time.Duration)
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client somewhere else (tests).
func WithBaseURL(base string) Option {
	return func(c *Client) { c.base = strings.TrimRight(base, "/") }
}

// WithAPIVersion pins the Stripe-Version header on every request.
func WithAPIVersion(v string) Option { return func(c *Client) { c.version = v } }

// WithMetrics reports every call.
func WithMetrics(m billing.Metrics) Option { return func(c *Client) { c.metrics = m } }

// WithHTTPClient replaces the transport (tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func withSleep(f func(time.Duration)) Option { return func(c *Client) { c.sleep = f } }

// New makes a client for a secret key.
func New(secretKey string, opts ...Option) *Client {
	c := &Client{
		key:     secretKey,
		base:    DefaultBaseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
		metrics: billing.NoMetrics{},
		sleep:   time.Sleep,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name implements billing.Provider.
func (c *Client) Name() string { return "stripe" }

// do sends one request, retrying what is safe to retry, and decodes the
// answer into out. Every call goes through here, so no call site can
// improvise its own headers, retries or error handling. The response body
// is never logged: it carries names, addresses and card details.
func (c *Client) do(ctx context.Context, op, method, path string, form url.Values, idempotencyKey string, out interface{}) error {
	var body io.Reader
	target := c.base + path
	if form != nil {
		if method == http.MethodGet || method == http.MethodDelete {
			target += "?" + form.Encode()
		} else {
			body = strings.NewReader(form.Encode())
		}
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if body != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		if body != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if c.version != "" {
			req.Header.Set("Stripe-Version", c.version)
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			c.metrics.ProviderRequest(op, 0)
			lastErr = err
			if ctx.Err() != nil {
				return err
			}
			c.backoff(attempt)
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		c.metrics.ProviderRequest(op, resp.StatusCode)
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil {
				return nil
			}
			return json.Unmarshal(data, out)
		}
		apiErr := &APIError{Status: resp.StatusCode, RequestID: resp.Header.Get("Request-Id")}
		var parsed apiErrorBody
		if json.Unmarshal(data, &parsed) == nil {
			apiErr.Type, apiErr.Code, apiErr.Message = parsed.Error.Type, parsed.Error.Code, parsed.Error.Message
		}
		lastErr = apiErr
		// A write without an idempotency key is never repeated: the first
		// attempt may have succeeded on Stripe's side.
		safe := method == http.MethodGet || idempotencyKey != ""
		if !apiErr.Retryable() || !safe {
			return apiErr
		}
		c.backoff(attempt)
	}
	return lastErr
}

func (c *Client) backoff(attempt int) {
	if attempt >= maxAttempts {
		return
	}
	base := 500 * time.Millisecond << uint(attempt-1)
	c.sleep(base + time.Duration(rand.Int63n(int64(base/2))))
}

// GetPrice implements billing.Provider. currency_options only comes back
// when expanded, and it is what makes one price serve every currency.
func (c *Client) GetPrice(ctx context.Context, id string) (*billing.Price, error) {
	var p price
	form := url.Values{}
	form.Add("expand[]", "currency_options")
	if err := c.do(ctx, "get_price", http.MethodGet, "/v1/prices/"+url.PathEscape(id), form, "", &p); err != nil {
		return nil, err
	}
	return convertPrice(&p), nil
}

func convertPrice(p *price) *billing.Price {
	out := &billing.Price{ID: p.ID, TaxBehavior: p.TaxBehavior, Amounts: map[string]int64{}}
	if p.Recurring != nil {
		out.Interval, out.UsageType = p.Recurring.Interval, p.Recurring.UsageType
	}
	if p.UnitAmount != nil && p.Currency != "" {
		out.Amounts[strings.ToLower(p.Currency)] = *p.UnitAmount
	}
	for cur, opt := range p.CurrencyOptions {
		if opt.UnitAmount != nil {
			out.Amounts[strings.ToLower(cur)] = *opt.UnitAmount
		}
	}
	return out
}

// GetSubscription implements billing.Provider.
func (c *Client) GetSubscription(ctx context.Context, id string) (*billing.Subscription, error) {
	var s subscription
	if err := c.do(ctx, "get_subscription", http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, "", &s); err != nil {
		return nil, err
	}
	return convertSubscription(&s), nil
}

func convertSubscription(s *subscription) *billing.Subscription {
	out := &billing.Subscription{
		ID:                s.ID,
		CustomerID:        s.Customer,
		Status:            s.Status,
		CancelAtPeriodEnd: s.CancelAtPeriodEnd,
		Currency:          strings.ToLower(s.Currency),
		Metadata:          s.Metadata,
		ItemCount:         len(s.Items.Data),
	}
	if len(s.Items.Data) > 0 {
		item := s.Items.Data[0]
		out.ItemID, out.PriceID, out.Quantity = item.ID, item.Price.ID, item.Quantity
		// Newer API versions carry the period on the item; older ones on
		// the subscription. Read whichever is present.
		if item.CurrentPeriodEnd > 0 {
			out.CurrentPeriodEnd = time.Unix(item.CurrentPeriodEnd, 0).UTC()
		}
	}
	if s.CurrentPeriodEnd > 0 {
		out.CurrentPeriodEnd = time.Unix(s.CurrentPeriodEnd, 0).UTC()
	}
	return out
}

// ListSubscriptions implements billing.Provider: every status, 100 a page.
func (c *Client) ListSubscriptions(ctx context.Context, startingAfter string) ([]*billing.Subscription, string, error) {
	form := url.Values{}
	form.Set("status", "all")
	form.Set("limit", "100")
	if startingAfter != "" {
		form.Set("starting_after", startingAfter)
	}
	var page listPage[subscription]
	if err := c.do(ctx, "list_subscriptions", http.MethodGet, "/v1/subscriptions", form, "", &page); err != nil {
		return nil, "", err
	}
	out := make([]*billing.Subscription, 0, len(page.Data))
	for i := range page.Data {
		out = append(out, convertSubscription(&page.Data[i]))
	}
	next := ""
	if page.HasMore && len(page.Data) > 0 {
		next = page.Data[len(page.Data)-1].ID
	}
	return out, next, nil
}

// CancelSubscription implements billing.Provider: a DELETE, idempotent by
// nature, and already-gone is done rather than an error.
func (c *Client) CancelSubscription(ctx context.Context, id string) error {
	err := c.do(ctx, "cancel_subscription", http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), nil, "", nil)
	if errors.Is(err, billing.ErrNotFound) {
		return nil
	}
	return err
}

// SetCancelAtPeriodEnd implements billing.Provider. The idempotency key is
// fresh per attempt on purpose: Stripe replays a reused key's ORIGINAL
// answer for a day without re-executing, so a deterministic key would make
// on → off → on stick at off. The key only has to cover one retry burst.
func (c *Client) SetCancelAtPeriodEnd(ctx context.Context, id string, on bool) error {
	form := url.Values{}
	form.Set("cancel_at_period_end", strconv.FormatBool(on))
	key := "openv:cancel-at-end:" + id + ":" + uuid.NewString()
	return c.do(ctx, "set_cancel_at_period_end", http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), form, key, nil)
}

// openDisputeStatuses are the statuses under which a chargeback is still
// being fought, and the payment is in doubt.
var openDisputeStatuses = map[string]bool{
	"needs_response": true, "under_review": true,
	"warning_needs_response": true, "warning_under_review": true,
}

// ListOpenDisputes implements billing.Provider. Stripe does not filter
// disputes by status server-side, so every page is read and filtered here;
// the charge is expanded through to its invoice, the only route from a
// dispute to a subscription.
func (c *Client) ListOpenDisputes(ctx context.Context) ([]billing.Dispute, error) {
	var out []billing.Dispute
	for cursor := ""; ; {
		form := url.Values{}
		form.Set("limit", "100")
		form.Add("expand[]", "data.charge.invoice")
		if cursor != "" {
			form.Set("starting_after", cursor)
		}
		var page listPage[dispute]
		if err := c.do(ctx, "list_disputes", http.MethodGet, "/v1/disputes", form, "", &page); err != nil {
			return nil, err
		}
		for _, d := range page.Data {
			if !openDisputeStatuses[d.Status] {
				continue
			}
			sub := ""
			if d.Charge != nil && d.Charge.Invoice != nil {
				sub = d.Charge.Invoice.Subscription
			}
			out = append(out, billing.Dispute{ID: d.ID, Status: d.Status, SubscriptionID: sub})
		}
		if !page.HasMore || len(page.Data) == 0 {
			break
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return out, nil
}

// CreateCustomer implements billing.Provider.
func (c *Client) CreateCustomer(ctx context.Context, name, email string, metadata map[string]string, idempotencyKey string) (string, error) {
	form := url.Values{}
	form.Set("name", name)
	if email != "" {
		form.Set("email", email)
	}
	for k, v := range metadata {
		form.Set("metadata["+k+"]", v)
	}
	var out customer
	if err := c.do(ctx, "create_customer", http.MethodPost, "/v1/customers", form, idempotencyKey, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// CreateCheckoutSession implements billing.Provider. Tax is calculated by
// the provider, business tax ids are collected for reverse charge, the
// billing address is required because tax determination needs it, and
// promotion codes are allowed so a discount never needs a deploy.
func (c *Client) CreateCheckoutSession(ctx context.Context, req billing.CheckoutRequest) (*billing.CheckoutSession, error) {
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("customer", req.CustomerID)
	form.Set("client_reference_id", req.ClientReferenceID)
	form.Set("line_items[0][price]", req.PriceID)
	form.Set("line_items[0][quantity]", strconv.Itoa(req.Quantity))
	if req.Currency != "" {
		form.Set("currency", req.Currency)
	}
	form.Set("success_url", req.SuccessURL)
	form.Set("cancel_url", req.CancelURL)
	form.Set("allow_promotion_codes", "true")
	form.Set("automatic_tax[enabled]", "true")
	form.Set("tax_id_collection[enabled]", "true")
	form.Set("billing_address_collection", "required")
	form.Set("customer_update[address]", "auto")
	form.Set("customer_update[name]", "auto")
	if req.TrialDays > 0 {
		form.Set("subscription_data[trial_period_days]", strconv.Itoa(req.TrialDays))
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", v)
		form.Set("subscription_data[metadata]["+k+"]", v)
	}
	var out checkoutSession
	if err := c.do(ctx, "create_checkout_session", http.MethodPost, "/v1/checkout/sessions", form, req.IdempotencyKey, &out); err != nil {
		return nil, err
	}
	return convertCheckoutSession(&out), nil
}

func convertCheckoutSession(s *checkoutSession) *billing.CheckoutSession {
	return &billing.CheckoutSession{
		ID: s.ID, URL: s.URL, ClientReferenceID: s.ClientReferenceID, CustomerID: s.Customer,
		SubscriptionID: s.Subscription, Status: s.Status, Metadata: s.Metadata,
	}
}

// GetCheckoutSession implements billing.Provider.
func (c *Client) GetCheckoutSession(ctx context.Context, id string) (*billing.CheckoutSession, error) {
	var out checkoutSession
	if err := c.do(ctx, "get_checkout_session", http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(id), nil, "", &out); err != nil {
		return nil, err
	}
	return convertCheckoutSession(&out), nil
}

// CreatePortalSession implements billing.Provider.
func (c *Client) CreatePortalSession(ctx context.Context, customerID, returnURL, configuration string) (string, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("return_url", returnURL)
	if configuration != "" {
		form.Set("configuration", configuration)
	}
	var out portalSession
	// Unkeyed on purpose: a portal session is worthless if lost and a
	// duplicate costs nothing, so a retry would only ever hurt.
	if err := c.do(ctx, "create_portal_session", http.MethodPost, "/v1/billing_portal/sessions", form, "", &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// UpdateSubscriptionItem implements billing.Provider: a plan change on one
// subscription, prorated. Fresh key per attempt, as for every toggle.
func (c *Client) UpdateSubscriptionItem(ctx context.Context, subscriptionID, itemID, priceID string, quantity int) error {
	form := url.Values{}
	form.Set("items[0][id]", itemID)
	form.Set("items[0][price]", priceID)
	form.Set("items[0][quantity]", strconv.Itoa(quantity))
	form.Set("proration_behavior", "create_prorations")
	key := "openv:change:" + subscriptionID + ":" + uuid.NewString()
	return c.do(ctx, "update_subscription", http.MethodPost, "/v1/subscriptions/"+url.PathEscape(subscriptionID), form, key, nil)
}

// SetItemQuantity implements billing.Provider. The key is fresh per attempt
// on purpose: a deterministic key carrying the quantity would be replayed
// for a day, so 7 → 8 → 7 would stick at 8. Setting a quantity is naturally
// idempotent; the key only has to cover one retry burst.
func (c *Client) SetItemQuantity(ctx context.Context, itemID string, quantity int) error {
	form := url.Values{}
	form.Set("quantity", strconv.Itoa(quantity))
	form.Set("proration_behavior", "create_prorations")
	key := "openv:seats:" + itemID + ":" + uuid.NewString()
	return c.do(ctx, "set_item_quantity", http.MethodPost, "/v1/subscription_items/"+url.PathEscape(itemID), form, key, nil)
}

var _ billing.Provider = (*Client)(nil)
