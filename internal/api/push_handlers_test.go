package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/notify"
)

// fakePushSubService is an in-memory pushsubs.Service keyed on the endpoint,
// so it reproduces the real repository's idempotency.
type fakePushSubService struct {
	subs         []*pushsubs.Subscription
	subscribeErr error
	listErr      error
	unsubscribed []string
}

func (f *fakePushSubService) Subscribe(s *pushsubs.Subscription) error {
	if f.subscribeErr != nil {
		return f.subscribeErr
	}
	for i, existing := range f.subs {
		if existing.Endpoint == s.Endpoint {
			// The upsert returns the PERSISTED row: an endpoint already on
			// file keeps its identity and its creation time, whatever the
			// caller generated for the candidate.
			s.ID = existing.ID
			s.CreatedAt = existing.CreatedAt
			f.subs[i] = s
			return nil
		}
	}
	f.subs = append(f.subs, s)
	return nil
}

func (f *fakePushSubService) ListForUser(userID string) ([]*pushsubs.Subscription, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*pushsubs.Subscription
	for _, s := range f.subs {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakePushSubService) Unsubscribe(userID, endpoint string) (int64, error) {
	f.unsubscribed = append(f.unsubscribed, userID+" "+endpoint)
	var kept []*pushsubs.Subscription
	var removed int64
	for _, s := range f.subs {
		if s.UserID == userID && s.Endpoint == endpoint {
			removed++
			continue
		}
		kept = append(kept, s)
	}
	f.subs = kept
	return removed, nil
}

func (f *fakePushSubService) Forget(endpoint string) (int64, error) { return 0, nil }
func (f *fakePushSubService) MarkUsed(string, time.Time) error      { return nil }
func (f *fakePushSubService) MarkFailed(string, time.Time) error    { return nil }

func pushReq(method, body string, user *users.User) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/api/v1/me/push-subscriptions", nil)
	} else {
		r = httptest.NewRequest(method, "/api/v1/me/push-subscriptions", strings.NewReader(body))
	}
	r.Header.Set("User-Agent", "TestBrowser/1.0")
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

const validSubscriptionBody = `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc",` +
	`"keys":{"p256dh":"BPublicKey","auth":"AuthSecret"},"user_agent":"Pixel 9"}`

// TestPushEndpointsRequireUser: every endpoint answers 401 without a
// signed-in user, and the service is never reached.
func TestPushEndpointsRequireUser(t *testing.T) {
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc, vapid: notify.VAPIDConfig{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:ops@example.com"}}

	for _, tc := range []struct {
		name string
		do   func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{"config", h.GetPushConfig, pushReq(http.MethodGet, "", nil)},
		{"list", h.ListPushSubscriptions, pushReq(http.MethodGet, "", nil)},
		{"create", h.CreatePushSubscription, pushReq(http.MethodPost, validSubscriptionBody, nil)},
		{"delete", h.DeletePushSubscription, pushReq(http.MethodDelete, `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc"}`, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.do(w, tc.req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
		})
	}
	if len(svc.subs) != 0 || len(svc.unsubscribed) != 0 {
		t.Fatal("the service was reached without an authenticated user")
	}
}

// TestGetPushConfig: the public key is handed out only when a complete VAPID
// configuration is present; otherwise enabled is false and no key leaks.
func TestGetPushConfig(t *testing.T) {
	user := &users.User{ID: "u-1"}

	on := &Handler{vapid: notify.VAPIDConfig{PublicKey: "the-public-key", PrivateKey: "the-private-key", Subject: "mailto:ops@example.com"}}
	w := httptest.NewRecorder()
	on.GetPushConfig(w, pushReq(http.MethodGet, "", user))
	var cfg pushConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !cfg.Enabled || cfg.PublicKey != "the-public-key" {
		t.Fatalf("config = %+v, want enabled with the public key", cfg)
	}
	if strings.Contains(w.Body.String(), "the-private-key") {
		t.Fatal("the private key leaked into the config response")
	}

	off := &Handler{}
	w = httptest.NewRecorder()
	off.GetPushConfig(w, pushReq(http.MethodGet, "", user))
	cfg = pushConfig{}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if cfg.Enabled || cfg.PublicKey != "" {
		t.Fatalf("config = %+v, want disabled with no key", cfg)
	}
}

// TestCreatePushSubscription: a valid body is stored against the SESSION
// user, answers 201, and never echoes the encryption keys.
func TestCreatePushSubscription(t *testing.T) {
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc}
	user := &users.User{ID: "u-session"}

	w := httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost, validSubscriptionBody, user))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
	}
	if len(svc.subs) != 1 {
		t.Fatalf("stored %d subscriptions, want 1", len(svc.subs))
	}
	stored := svc.subs[0]
	if stored.UserID != "u-session" {
		t.Fatalf("stored under user %q, want the session user", stored.UserID)
	}
	if stored.Endpoint != "https://fcm.googleapis.com/fcm/send/abc" || stored.P256dh != "BPublicKey" || stored.Auth != "AuthSecret" {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.UserAgent != "Pixel 9" {
		t.Fatalf("user agent = %q, want the body's value", stored.UserAgent)
	}
	if strings.Contains(w.Body.String(), "AuthSecret") || strings.Contains(w.Body.String(), "BPublicKey") {
		t.Fatalf("the response echoed the encryption keys: %s", w.Body.String())
	}

	// No user_agent in the body: the request header stands in.
	svc.subs = nil
	w = httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost,
		`{"endpoint":"https://fcm.googleapis.com/fcm/send/xyz","keys":{"p256dh":"k","auth":"a"}}`, user))
	if w.Code != http.StatusCreated || svc.subs[0].UserAgent != "TestBrowser/1.0" {
		t.Fatalf("status %d, user agent %q", w.Code, svc.subs[0].UserAgent)
	}
}

// TestCreatePushSubscriptionIdempotent: posting the same endpoint twice
// refreshes the keys instead of adding a device.
func TestCreatePushSubscriptionIdempotent(t *testing.T) {
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc}
	user := &users.User{ID: "u-1"}

	for _, body := range []string{
		validSubscriptionBody,
		`{"endpoint":"https://fcm.googleapis.com/fcm/send/abc","keys":{"p256dh":"Rotated","auth":"RotatedAuth"}}`,
	} {
		w := httptest.NewRecorder()
		h.CreatePushSubscription(w, pushReq(http.MethodPost, body, user))
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
		}
	}
	if len(svc.subs) != 1 {
		t.Fatalf("re-posting the same endpoint created %d rows, want 1", len(svc.subs))
	}
	if svc.subs[0].P256dh != "Rotated" {
		t.Fatalf("keys were not refreshed: %+v", svc.subs[0])
	}
}

// TestCreatePushSubscriptionValidation: malformed bodies are 400s that never
// reach the store; an https-less endpoint is refused outright.
func TestCreatePushSubscriptionValidation(t *testing.T) {
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc}
	user := &users.User{ID: "u-1"}

	for _, tc := range []struct{ name, body string }{
		{"malformed json", `{nope`},
		{"no endpoint", `{"keys":{"p256dh":"k","auth":"a"}}`},
		{"http endpoint", `{"endpoint":"http://push.example.com/a","keys":{"p256dh":"k","auth":"a"}}`},
		{"not a url", `{"endpoint":"file:///etc/passwd","keys":{"p256dh":"k","auth":"a"}}`},
		{"no keys", `{"endpoint":"https://fcm.googleapis.com/fcm/send/a"}`},
		{"one key", `{"endpoint":"https://fcm.googleapis.com/fcm/send/a","keys":{"p256dh":"k"}}`},
		{"endpoint too long", `{"endpoint":"https://fcm.googleapis.com/fcm/send/` + strings.Repeat("x", maxPushEndpointLen) + `","keys":{"p256dh":"k","auth":"a"}}`},
		{"key too long", `{"endpoint":"https://fcm.googleapis.com/fcm/send/a","keys":{"p256dh":"` + strings.Repeat("k", maxPushKeyLen+1) + `","auth":"a"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.CreatePushSubscription(w, pushReq(http.MethodPost, tc.body, user))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
			}
		})
	}
	if len(svc.subs) != 0 {
		t.Fatalf("an invalid body was stored: %+v", svc.subs)
	}

	// An over-long user agent is truncated rather than refused: it is only a
	// label for the device list.
	w := httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost,
		`{"endpoint":"https://fcm.googleapis.com/fcm/send/a","keys":{"p256dh":"k","auth":"a"},"user_agent":"`+
			strings.Repeat("u", maxPushUserAgentLen+50)+`"}`, user))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if len(svc.subs[0].UserAgent) != maxPushUserAgentLen {
		t.Fatalf("user agent length = %d, want %d", len(svc.subs[0].UserAgent), maxPushUserAgentLen)
	}
}

// TestListPushSubscriptionsOwnUserOnly: a member sees only their own devices,
// and the response carries no encryption keys.
func TestListPushSubscriptionsOwnUserOnly(t *testing.T) {
	mine := pushsubs.New("u-1", "https://fcm.googleapis.com/fcm/send/mine", "MyP256", "MyAuth", "Pixel")
	theirs := pushsubs.New("u-2", "https://fcm.googleapis.com/fcm/send/theirs", "TheirP256", "TheirAuth", "iPad")
	svc := &fakePushSubService{subs: []*pushsubs.Subscription{mine, theirs}}
	h := &Handler{pushSubService: svc}

	w := httptest.NewRecorder()
	h.ListPushSubscriptions(w, pushReq(http.MethodGet, "", &users.User{ID: "u-1"}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Subscriptions []struct {
			ID        string `json:"id"`
			Endpoint  string `json:"endpoint"`
			UserAgent string `json:"user_agent"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Subscriptions) != 1 || resp.Subscriptions[0].Endpoint != mine.Endpoint {
		t.Fatalf("listed %d subscriptions: %+v", len(resp.Subscriptions), resp.Subscriptions)
	}
	for _, secret := range []string{"MyP256", "MyAuth", "TheirP256", "TheirAuth"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("the response leaked %q: %s", secret, w.Body.String())
		}
	}

	// A member with no devices gets an empty array, never null.
	w = httptest.NewRecorder()
	h.ListPushSubscriptions(w, pushReq(http.MethodGet, "", &users.User{ID: "u-nobody"}))
	if got := strings.TrimSpace(w.Body.String()); got != `{"subscriptions":[]}` {
		t.Fatalf("empty list = %s", got)
	}
}

// TestDeletePushSubscription: withdrawal is 204 and scoped to the caller;
// withdrawing something already gone is still 204.
func TestDeletePushSubscription(t *testing.T) {
	mine := pushsubs.New("u-1", "https://fcm.googleapis.com/fcm/send/mine", "p", "a", "Pixel")
	theirs := pushsubs.New("u-2", "https://fcm.googleapis.com/fcm/send/theirs", "p", "a", "iPad")
	svc := &fakePushSubService{subs: []*pushsubs.Subscription{mine, theirs}}
	h := &Handler{pushSubService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.DeletePushSubscription(w, pushReq(http.MethodDelete, `{"endpoint":"`+mine.Endpoint+`"}`, user))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %q)", w.Code, w.Body.String())
	}
	if len(svc.subs) != 1 || svc.subs[0].Endpoint != theirs.Endpoint {
		t.Fatalf("remaining subscriptions: %+v", svc.subs)
	}

	// Another member's endpoint is accepted but removes nothing — the delete
	// is keyed on the session user.
	w = httptest.NewRecorder()
	h.DeletePushSubscription(w, pushReq(http.MethodDelete, `{"endpoint":"`+theirs.Endpoint+`"}`, user))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if len(svc.subs) != 1 {
		t.Fatalf("a member deleted another member's device: %+v", svc.subs)
	}

	// A missing endpoint is a 400.
	w = httptest.NewRecorder()
	h.DeletePushSubscription(w, pushReq(http.MethodDelete, `{}`, user))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestPushEndpointsWithoutService: a deployment wired without the push
// service (nil) answers sensibly instead of panicking.
func TestPushEndpointsWithoutService(t *testing.T) {
	h := &Handler{}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.ListPushSubscriptions(w, pushReq(http.MethodGet, "", user))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"subscriptions":[]}` {
		t.Fatalf("list: status %d body %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost, validSubscriptionBody, user))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("create: status = %d, want 503", w.Code)
	}

	w = httptest.NewRecorder()
	h.DeletePushSubscription(w, pushReq(http.MethodDelete, `{"endpoint":"https://fcm.googleapis.com/fcm/send/a"}`, user))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", w.Code)
	}
}

// TestCreatePushSubscriptionStoreError: a store failure is a 500, not a
// silent success.
func TestCreatePushSubscriptionStoreError(t *testing.T) {
	h := &Handler{pushSubService: &fakePushSubService{subscribeErr: errors.New("db down")}}
	w := httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost, validSubscriptionBody, &users.User{ID: "u-1"}))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// TestCreatePushSubscriptionReturnsThePersistedRow: the 201 body describes
// the row that is now on file, not the candidate the handler built. Re-posting
// a known device therefore answers with the SAME id (and created_at) that
// GET /me/push-subscriptions lists, rather than an id belonging to a row that
// was never inserted.
func TestCreatePushSubscriptionReturnsThePersistedRow(t *testing.T) {
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc}
	user := &users.User{ID: "u-1"}

	created := func(body string) pushsubs.Subscription {
		t.Helper()
		w := httptest.NewRecorder()
		h.CreatePushSubscription(w, pushReq(http.MethodPost, body, user))
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
		}
		var got pushsubs.Subscription
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		return got
	}

	first := created(validSubscriptionBody)
	if first.ID == "" {
		t.Fatal("the 201 body carried no id")
	}
	second := created(`{"endpoint":"https://fcm.googleapis.com/fcm/send/abc",` +
		`"keys":{"p256dh":"Rotated","auth":"RotatedAuth"}}`)

	if second.ID != first.ID {
		t.Fatalf("re-posting the same device answered with id %q, want the persisted %q",
			second.ID, first.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at = %v, want the persisted %v", second.CreatedAt, first.CreatedAt)
	}

	// And that is the row GET lists.
	w := httptest.NewRecorder()
	h.ListPushSubscriptions(w, pushReq(http.MethodGet, "", user))
	var listed struct {
		Subscriptions []pushsubs.Subscription `json:"subscriptions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(listed.Subscriptions) != 1 || listed.Subscriptions[0].ID != first.ID {
		t.Fatalf("listed %+v, want the one device with id %q", listed.Subscriptions, first.ID)
	}
}

// TestPushUserAgentIsCutOnARuneBoundary: the device label is free text from
// the browser and routinely non-ASCII. Slicing bytes would cut a multi-byte
// character in half and store invalid UTF-8 (postgres refuses it outright);
// the cut is on a rune boundary.
func TestPushUserAgentIsCutOnARuneBoundary(t *testing.T) {
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc}

	// A two-byte rune sits exactly on the byte-slicing cut.
	agent := strings.Repeat("u", maxPushUserAgentLen-1) + "é" + strings.Repeat("v", 50)
	w := httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost,
		`{"endpoint":"https://fcm.googleapis.com/fcm/send/abc","keys":{"p256dh":"k","auth":"a"},`+
			`"user_agent":"`+agent+`"}`, &users.User{ID: "u-1"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
	}

	stored := svc.subs[0].UserAgent
	if !utf8.ValidString(stored) {
		t.Fatalf("stored user agent is not valid UTF-8: %q", stored)
	}
	if got := utf8.RuneCountInString(stored); got != maxPushUserAgentLen {
		t.Fatalf("user agent = %d runes, want %d", got, maxPushUserAgentLen)
	}
	if !strings.HasSuffix(stored, "é") {
		t.Fatalf("user agent ends %q, want the whole multi-byte rune", stored[len(stored)-4:])
	}

	// A short non-ASCII agent is stored as it is.
	if got := truncateRunes("Mozilla/5.0 (iPhone) — Sam’s phone", maxPushUserAgentLen); got != "Mozilla/5.0 (iPhone) — Sam’s phone" {
		t.Fatalf("a short user agent was altered: %q", got)
	}
}

// TestPushEndpointAllowList: a subscription endpoint is a URL this server
// will later POST to from inside the deployment's network, so it has to name
// a known push service. Anything else — another host, an address literal, a
// non-default port, embedded credentials — is refused, and no DNS is
// resolved: the allow-list is the whole guard.
func TestPushEndpointAllowList(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		want           bool
	}{
		{"chrome", "https://fcm.googleapis.com/fcm/send/abc123", true},
		{"safari", "https://web.push.apple.com/QBcd-EF", true},
		{"edge", "https://wns2-par02p.notify.windows.com/w/?token=abc", true},
		{"firefox", "https://updates.push.services.mozilla.com/wpush/v2/abc", true},
		{"firefox regional", "https://autopush.eu.push.services.mozilla.com/wpush/v2/abc", true},
		{"firefox bare", "https://push.services.mozilla.com/wpush/v2/abc", true},
		{"upper case host", "https://FCM.googleapis.COM/fcm/send/abc", true},
		{"trailing dot", "https://fcm.googleapis.com./fcm/send/abc", true},
		{"explicit 443", "https://fcm.googleapis.com:443/fcm/send/abc", true},

		{"another host", "https://push.example.com/abc", false},
		{"look-alike suffix", "https://fcm.googleapis.com.evil.example/abc", false},
		{"look-alike prefix", "https://notfcm.googleapis.com/abc", false},
		{"wildcard needs a subdomain", "https://push.apple.com/abc", false},
		{"plain http", "http://fcm.googleapis.com/fcm/send/abc", false},
		{"loopback", "https://127.0.0.1/fcm/send/abc", false},
		{"ipv6 loopback", "https://[::1]/fcm/send/abc", false},
		{"link-local metadata", "https://169.254.169.254/latest/meta-data", false},
		{"private range", "https://10.0.0.5/internal", false},
		{"other port", "https://fcm.googleapis.com:8443/fcm/send/abc", false},
		{"credentials", "https://user:secret@fcm.googleapis.com/fcm/send/abc", false},
		{"no host", "https:///fcm/send/abc", false},
		{"not a url", "file:///etc/passwd", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := validatePushEndpoint(tc.endpoint)
			if got := msg == ""; got != tc.want {
				t.Fatalf("validatePushEndpoint(%q) = %q, want allowed=%v", tc.endpoint, msg, tc.want)
			}
		})
	}
}

// TestPushEndpointHostOverride: a self-hosted push service is admitted by
// OPENV_PUSH_ENDPOINT_HOSTS, exact or wildcard — but the override extends the
// list of NAMES only; it can never admit an address literal.
func TestPushEndpointHostOverride(t *testing.T) {
	if validatePushEndpoint("https://push.internal.example/x") == "" {
		t.Fatal("an unlisted host was accepted before the override was set")
	}

	t.Setenv(envPushEndpointHosts, " push.internal.example , *.push.corp.example ,, 127.0.0.1 ")

	for _, allowed := range []string{
		"https://push.internal.example/x",
		"https://node1.push.corp.example/x",
		"https://fcm.googleapis.com/fcm/send/abc", // the built-ins still stand
	} {
		if msg := validatePushEndpoint(allowed); msg != "" {
			t.Fatalf("validatePushEndpoint(%q) = %q, want allowed", allowed, msg)
		}
	}
	for _, refused := range []string{
		"https://push.corp.example/x", // the wildcard needs a subdomain
		"https://other.example/x",     // not listed
		"https://127.0.0.1/x",         // an address literal, listed or not
	} {
		if validatePushEndpoint(refused) == "" {
			t.Fatalf("validatePushEndpoint(%q) was accepted", refused)
		}
	}

	// End to end: the override is what the handler applies.
	svc := &fakePushSubService{}
	h := &Handler{pushSubService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost,
		`{"endpoint":"https://push.internal.example/x","keys":{"p256dh":"k","auth":"a"}}`, user))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 for an overridden host (body %q)", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.CreatePushSubscription(w, pushReq(http.MethodPost,
		`{"endpoint":"https://evil.example/x","keys":{"p256dh":"k","auth":"a"}}`, user))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unlisted host", w.Code)
	}
	if len(svc.subs) != 1 {
		t.Fatalf("stored %d subscriptions, want only the allowed one", len(svc.subs))
	}
}
