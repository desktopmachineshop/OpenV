package stripe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/billing"
)

// The client's contract with Stripe: the headers every request carries, the
// form encoding, what is retried and what never is, and the few fields
// parsed out of each answer.

type recorded struct {
	method, path, query, body, auth, version, idem string
}

func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, rec recorded)) (*httptest.Server, *Client, *[]recorded) {
	t.Helper()
	var log []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		rec := recorded{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body[:n]),
			auth: r.Header.Get("Authorization"), version: r.Header.Get("Stripe-Version"), idem: r.Header.Get("Idempotency-Key")}
		log = append(log, rec)
		handler(w, r, rec)
	}))
	t.Cleanup(srv.Close)
	c := New("sk_test_fake_for_unit_tests", WithBaseURL(srv.URL), WithAPIVersion("2025-08-27.basil"), withSleep(func(time.Duration) {}))
	return srv, c, &log
}

func TestGetPriceCarriesTheHeadersAndMergesCurrencies(t *testing.T) {
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		w.Write([]byte(`{"id":"price_1","currency":"gbp","unit_amount":1200,"tax_behavior":"exclusive",
			"recurring":{"interval":"month","usage_type":"licensed"},
			"currency_options":{"gbp":{"unit_amount":1200},"usd":{"unit_amount":1500},"EUR":{"unit_amount":1400}}}`))
	})
	p, err := c.GetPrice(context.Background(), "price_1")
	if err != nil {
		t.Fatal(err)
	}
	rec := (*log)[0]
	if rec.method != http.MethodGet || rec.path != "/v1/prices/price_1" || !strings.Contains(rec.query, "expand%5B%5D=currency_options") {
		t.Fatalf("request = %+v", rec)
	}
	if rec.auth != "Bearer sk_test_fake_for_unit_tests" || rec.version != "2025-08-27.basil" || rec.idem != "" {
		t.Fatalf("headers = %+v", rec)
	}
	if p.Interval != "month" || p.UsageType != "licensed" || p.TaxBehavior != "exclusive" {
		t.Fatalf("price = %+v", p)
	}
	if p.Amounts["gbp"] != 1200 || p.Amounts["usd"] != 1500 || p.Amounts["eur"] != 1400 || len(p.Amounts) != 3 {
		t.Fatalf("amounts = %v", p.Amounts)
	}
}

func TestARateLimitIsRetriedAndACardErrorIsNot(t *testing.T) {
	var calls int32
	_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
			return
		}
		w.Write([]byte(`{"id":"sub_1","status":"active","items":{"data":[]}}`))
	})
	if _, err := c.GetSubscription(context.Background(), "sub_1"); err != nil {
		t.Fatalf("a 429 then 200 should succeed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}

	calls = 0
	_, c2, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Request-Id", "req_abc")
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":{"type":"card_error","code":"card_declined","message":"Your card was declined."}}`))
	})
	_, err := c2.GetSubscription(context.Background(), "sub_1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 402 || apiErr.Code != "card_declined" || apiErr.RequestID != "req_abc" {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Retryable() || calls != 1 {
		t.Fatalf("a 402 was retried: calls=%d", calls)
	}
	if !strings.Contains(apiErr.Error(), "req_abc") || strings.Contains(apiErr.Error(), "Your card") {
		t.Fatalf("the error names the request id and never the message: %q", apiErr.Error())
	}
}

func TestAWriteWithoutAKeyIsNeverRepeatedAndAKeyedOneIs(t *testing.T) {
	var calls int32
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	})
	// cancel_at_period_end is a keyed POST: retried, on one key.
	if err := c.SetCancelAtPeriodEnd(context.Background(), "sub_1", true); err == nil {
		t.Fatal("a persistent 502 should fail")
	}
	if calls != maxAttempts {
		t.Fatalf("a keyed write was tried %d times, want %d", calls, maxAttempts)
	}
	rec := (*log)[0]
	if rec.method != http.MethodPost || rec.body != "cancel_at_period_end=true" || !strings.HasPrefix(rec.idem, "openv:cancel-at-end:sub_1:") {
		t.Fatalf("cancel request = %+v", rec)
	}
	if (*log)[1].idem != rec.idem {
		t.Fatal("a retry of one attempt changed its idempotency key")
	}
	// A second attempt is a new intent with a new key: the first answer
	// must not be replayed over it.
	calls = 0
	_ = c.SetCancelAtPeriodEnd(context.Background(), "sub_1", false)
	if (*log)[len(*log)-1].idem == rec.idem || (*log)[len(*log)-1].body != "cancel_at_period_end=false" {
		t.Fatalf("toggling reused a key or lost its value: %+v", (*log)[len(*log)-1])
	}
	// An unkeyed write through do() is sent once.
	calls = 0
	if err := c.do(context.Background(), "test", http.MethodPost, "/v1/things", nil, "", nil); err == nil {
		t.Fatal("expected failure")
	}
	if calls != 1 {
		t.Fatalf("an unkeyed write was retried: %d", calls)
	}
}

func TestA404IsNotFoundAndImmediateCancelIsADelete(t *testing.T) {
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such subscription"}}`))
	})
	if _, err := c.GetSubscription(context.Background(), "sub_missing"); !errors.Is(err, billing.ErrNotFound) {
		t.Fatalf("a 404 should be ErrNotFound: %v", err)
	}
	// Cancelling something already gone is done, not an error.
	if err := c.CancelSubscription(context.Background(), "sub_missing"); err != nil {
		t.Fatalf("delete of a missing subscription: %v", err)
	}
	last := (*log)[len(*log)-1]
	if last.method != http.MethodDelete || last.path != "/v1/subscriptions/sub_missing" {
		t.Fatalf("immediate cancel = %+v", last)
	}
}

func TestListSubscriptionsPagesAndParsesTheItem(t *testing.T) {
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		if strings.Contains(rec.query, "starting_after=sub_a") {
			w.Write([]byte(`{"data":[{"id":"sub_b","customer":"cus_2","status":"canceled","items":{"data":[]}}],"has_more":false}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"sub_a","customer":"cus_1","status":"past_due","cancel_at_period_end":true,"currency":"GBP",
			"metadata":{"openv_org_id":"org-1"},
			"items":{"data":[{"id":"si_1","quantity":4,"current_period_end":1790000000,"price":{"id":"price_bm","currency":"gbp","recurring":{"interval":"month"}}}]}}],"has_more":true}`))
	})
	page1, next, err := c.ListSubscriptions(context.Background(), "")
	if err != nil || len(page1) != 1 || next != "sub_a" {
		t.Fatalf("page 1: %v %q %v", page1, next, err)
	}
	s := page1[0]
	if s.CustomerID != "cus_1" || s.Status != "past_due" || !s.CancelAtPeriodEnd || s.Currency != "gbp" ||
		s.Metadata["openv_org_id"] != "org-1" || s.ItemID != "si_1" || s.PriceID != "price_bm" || s.Quantity != 4 || s.ItemCount != 1 ||
		!s.CurrentPeriodEnd.Equal(time.Unix(1790000000, 0)) {
		t.Fatalf("parsed = %+v", s)
	}
	page2, next, err := c.ListSubscriptions(context.Background(), next)
	if err != nil || len(page2) != 1 || next != "" || page2[0].ItemCount != 0 {
		t.Fatalf("page 2: %v %q %v", page2, next, err)
	}
	first := (*log)[0]
	if !strings.Contains(first.query, "status=all") || !strings.Contains(first.query, "limit=100") {
		t.Fatalf("listing query = %q", first.query)
	}
}

func TestSubscriptionLevelPeriodEndWinsWhenPresent(t *testing.T) {
	_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		w.Write([]byte(`{"id":"sub_1","status":"active","current_period_end":1800000000,
			"items":{"data":[{"id":"si_1","quantity":1,"current_period_end":1790000000,"price":{"id":"p"}}]}}`))
	})
	s, err := c.GetSubscription(context.Background(), "sub_1")
	if err != nil || !s.CurrentPeriodEnd.Equal(time.Unix(1800000000, 0)) {
		t.Fatalf("period end = %v (%v)", s.CurrentPeriodEnd, err)
	}
}

func TestListOpenDisputesFiltersAndFollowsTheInvoice(t *testing.T) {
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		if strings.Contains(rec.query, "starting_after=dp_2") {
			w.Write([]byte(`{"data":[{"id":"dp_3","status":"under_review","charge":{"invoice":{"subscription":"sub_3"}}}],"has_more":false}`))
			return
		}
		w.Write([]byte(`{"data":[
			{"id":"dp_1","status":"needs_response","charge":{"invoice":{"subscription":"sub_1"}}},
			{"id":"dp_2","status":"won","charge":{"invoice":{"subscription":"sub_2"}}}
		],"has_more":true}`))
	})
	got, err := c.ListOpenDisputes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SubscriptionID != "sub_1" || got[1].SubscriptionID != "sub_3" {
		t.Fatalf("disputes = %+v", got)
	}
	if !strings.Contains((*log)[0].query, "expand%5B%5D=data.charge.invoice") {
		t.Fatalf("the charge was not expanded through to the invoice: %q", (*log)[0].query)
	}
}

func TestATransportFailureIsRetriedThenReported(t *testing.T) {
	c := New("sk_test_fake", WithBaseURL("http://127.0.0.1:1"), withSleep(func(time.Duration) {}),
		WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	if _, err := c.GetSubscription(context.Background(), "sub_1"); err == nil {
		t.Fatal("an unreachable host should fail")
	}
	// A cancelled context stops retrying at once.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetSubscription(ctx, "sub_1"); err == nil {
		t.Fatal("a cancelled context should fail")
	}
}

// The purchase calls: what each one tells Stripe, and that the server's
// decisions — tax, tax ids, address, promo codes, trial, metadata on both
// the session and the subscription — are all on the wire.
func TestCreateCheckoutSessionCarriesEveryServerDecision(t *testing.T) {
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		w.Write([]byte(`{"id":"cs_1","url":"https://checkout.stripe.com/c/cs_1","client_reference_id":"org-1","customer":"cus_1","status":"open","metadata":{"openv_org_id":"org-1"}}`))
	})
	sess, err := c.CreateCheckoutSession(context.Background(), billing.CheckoutRequest{
		CustomerID: "cus_1", PriceID: "price_bm", Quantity: 7, Currency: "gbp", TrialDays: 14, ClientReferenceID: "org-1",
		Metadata:   map[string]string{"openv_org_id": "org-1", "openv_trial_buyer": "u1"},
		SuccessURL: "https://app/ok?session_id={CHECKOUT_SESSION_ID}", CancelURL: "https://app/no", IdempotencyKey: "openv:co:1",
	})
	if err != nil || sess.URL != "https://checkout.stripe.com/c/cs_1" || sess.ClientReferenceID != "org-1" || sess.Metadata["openv_org_id"] != "org-1" {
		t.Fatalf("session = %+v %v", sess, err)
	}
	rec := (*log)[0]
	if rec.method != http.MethodPost || rec.path != "/v1/checkout/sessions" || rec.idem != "openv:co:1" {
		t.Fatalf("request = %+v", rec)
	}
	for _, want := range []string{
		"mode=subscription", "customer=cus_1", "client_reference_id=org-1",
		"line_items%5B0%5D%5Bprice%5D=price_bm", "line_items%5B0%5D%5Bquantity%5D=7", "currency=gbp",
		"automatic_tax%5Benabled%5D=true", "tax_id_collection%5Benabled%5D=true", "billing_address_collection=required",
		"customer_update%5Baddress%5D=auto", "allow_promotion_codes=true",
		"subscription_data%5Btrial_period_days%5D=14",
		"metadata%5Bopenv_org_id%5D=org-1", "subscription_data%5Bmetadata%5D%5Bopenv_org_id%5D=org-1",
		"subscription_data%5Bmetadata%5D%5Bopenv_trial_buyer%5D=u1",
		"success_url=https%3A%2F%2Fapp%2Fok%3Fsession_id%3D%7BCHECKOUT_SESSION_ID%7D",
	} {
		if !strings.Contains(rec.body, want) {
			t.Errorf("body lacks %s: %s", want, rec.body)
		}
	}
	// No trial: the field is absent rather than zero.
	_, _ = c.CreateCheckoutSession(context.Background(), billing.CheckoutRequest{CustomerID: "cus_1", PriceID: "p", Quantity: 1})
	if strings.Contains((*log)[1].body, "trial_period_days") {
		t.Fatal("a zero trial was sent")
	}
}

func TestCustomerPortalAndChangeCalls(t *testing.T) {
	_, c, log := newServer(t, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		switch rec.path {
		case "/v1/customers":
			w.Write([]byte(`{"id":"cus_9"}`))
		case "/v1/billing_portal/sessions":
			w.Write([]byte(`{"url":"https://billing.stripe.com/p/s"}`))
		default:
			w.Write([]byte(`{"id":"x"}`))
		}
	})
	id, err := c.CreateCustomer(context.Background(), "Acme", "dana@example.com", map[string]string{"openv_org_id": "org-1"}, "openv:cust:org-1:v1")
	if err != nil || id != "cus_9" {
		t.Fatalf("customer = %q %v", id, err)
	}
	if rec := (*log)[0]; rec.idem != "openv:cust:org-1:v1" || !strings.Contains(rec.body, "name=Acme") || !strings.Contains(rec.body, "email=dana%40example.com") || !strings.Contains(rec.body, "metadata%5Bopenv_org_id%5D=org-1") {
		t.Fatalf("customer request = %+v", rec)
	}
	url, err := c.CreatePortalSession(context.Background(), "cus_9", "https://app/back", "bpc_1")
	if err != nil || url != "https://billing.stripe.com/p/s" {
		t.Fatalf("portal = %q %v", url, err)
	}
	if rec := (*log)[1]; rec.idem != "" || !strings.Contains(rec.body, "customer=cus_9") || !strings.Contains(rec.body, "configuration=bpc_1") {
		t.Fatalf("portal request = %+v", rec)
	}
	if err := c.UpdateSubscriptionItem(context.Background(), "sub_1", "si_1", "price_bm", 7); err != nil {
		t.Fatal(err)
	}
	if rec := (*log)[2]; rec.path != "/v1/subscriptions/sub_1" || !strings.HasPrefix(rec.idem, "openv:change:sub_1:") ||
		!strings.Contains(rec.body, "items%5B0%5D%5Bid%5D=si_1") || !strings.Contains(rec.body, "items%5B0%5D%5Bprice%5D=price_bm") ||
		!strings.Contains(rec.body, "items%5B0%5D%5Bquantity%5D=7") || !strings.Contains(rec.body, "proration_behavior=create_prorations") {
		t.Fatalf("change request = %+v", rec)
	}
	if err := c.SetItemQuantity(context.Background(), "si_1", 8); err != nil {
		t.Fatal(err)
	}
	if err := c.SetItemQuantity(context.Background(), "si_1", 8); err != nil {
		t.Fatal(err)
	}
	a, b := (*log)[3], (*log)[4]
	if a.path != "/v1/subscription_items/si_1" || !strings.Contains(a.body, "quantity=8") || a.idem == b.idem || !strings.HasPrefix(a.idem, "openv:seats:si_1:") {
		t.Fatalf("quantity requests = %+v %+v", a, b)
	}
	if _, err := c.GetCheckoutSession(context.Background(), "cs_1"); err != nil {
		t.Fatal(err)
	}
	if rec := (*log)[5]; rec.method != http.MethodGet || rec.path != "/v1/checkout/sessions/cs_1" {
		t.Fatalf("get session = %+v", rec)
	}
}
