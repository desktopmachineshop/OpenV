package api

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock lets tests drive the limiter's notion of time.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)}
}

func withClock(l *rateLimiter, c *fakeClock) *rateLimiter {
	l.now = c.now
	l.lastCleanup = c.t
	return l
}

func TestRateLimiterBurstThenDeny(t *testing.T) {
	clock := newFakeClock()
	l := withClock(newRateLimiter(5, 20), clock)

	for i := 0; i < 5; i++ {
		if ok, _ := l.allow("invite-1"); !ok {
			t.Fatalf("request %d within burst was denied", i+1)
		}
	}
	ok, retryAfter := l.allow("invite-1")
	if ok {
		t.Fatal("6th request within burst window was allowed")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want > 0", retryAfter)
	}
	// At 20/hour a full token takes 3 minutes.
	if retryAfter > 3*time.Minute+time.Second {
		t.Fatalf("retryAfter = %v, want <= ~3m", retryAfter)
	}
}

func TestRateLimiterRefill(t *testing.T) {
	clock := newFakeClock()
	l := withClock(newRateLimiter(5, 20), clock)

	for i := 0; i < 5; i++ {
		l.allow("k")
	}
	if ok, _ := l.allow("k"); ok {
		t.Fatal("exhausted bucket allowed a request")
	}

	// 20/hour => one token every 3 minutes. Just past that, exactly one
	// request should pass, and the next should be denied again.
	clock.advance(3*time.Minute + time.Second)
	if ok, _ := l.allow("k"); !ok {
		t.Fatal("request after refill interval was denied")
	}
	if ok, _ := l.allow("k"); ok {
		t.Fatal("second request after a single-token refill was allowed")
	}

	// A long idle period refills back up to burst, not beyond.
	clock.advance(24 * time.Hour)
	for i := 0; i < 5; i++ {
		if ok, _ := l.allow("k"); !ok {
			t.Fatalf("request %d after full refill was denied", i+1)
		}
	}
	if ok, _ := l.allow("k"); ok {
		t.Fatal("refill exceeded burst capacity")
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	clock := newFakeClock()
	l := withClock(newRateLimiter(2, 10), clock)

	l.allow("a")
	l.allow("a")
	if ok, _ := l.allow("a"); ok {
		t.Fatal("key a should be exhausted")
	}
	if ok, _ := l.allow("b"); !ok {
		t.Fatal("key b was throttled by key a's usage")
	}
}

func TestRateLimiterCleanupDropsStaleBuckets(t *testing.T) {
	clock := newFakeClock()
	l := withClock(newRateLimiter(5, 20), clock)

	l.allow("stale")
	l.allow("fresh")

	// Beyond staleAfter: "stale" is untouched, "fresh" keeps being used.
	clock.advance(staleAfter)
	l.allow("fresh")

	// Next allow (past the cleanup interval) sweeps the idle bucket.
	clock.advance(cleanupEvery)
	l.allow("other")

	l.mu.Lock()
	_, staleExists := l.buckets["stale"]
	_, freshExists := l.buckets["fresh"]
	size := len(l.buckets)
	l.mu.Unlock()

	if staleExists {
		t.Fatal("stale bucket survived cleanup")
	}
	if !freshExists {
		t.Fatal("recently used bucket was dropped by cleanup")
	}
	if size != 2 { // fresh + other
		t.Fatalf("bucket count = %d, want 2", size)
	}
}

func TestNilRateLimiterAllowsEverything(t *testing.T) {
	var l *rateLimiter
	for i := 0; i < 100; i++ {
		if ok, _ := l.allow("k"); !ok {
			t.Fatal("nil limiter denied a request")
		}
	}
}

func TestClientIPUntrustedIgnoresProxyHeaders(t *testing.T) {
	none := proxyTrust{}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.7:5511"
	if got := clientIPWithTrust(r, none); got != "192.0.2.7" {
		t.Fatalf("clientIP from RemoteAddr = %q, want 192.0.2.7", got)
	}

	// Spoofed headers from a direct client must not shift the key.
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	r.Header.Set("X-Real-IP", "203.0.113.10")
	if got := clientIPWithTrust(r, none); got != "192.0.2.7" {
		t.Fatalf("untrusted clientIP honored a spoofable header: %q, want 192.0.2.7", got)
	}
}

func TestClientIPTrustedHonorsProxyHeaders(t *testing.T) {
	one := proxyTrust{hops: 1}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:5511" // the proxy's address

	// No headers: falls back to the remote address even when trusting.
	if got := clientIPWithTrust(r, one); got != "10.0.0.5" {
		t.Fatalf("trusted clientIP without headers = %q, want 10.0.0.5", got)
	}

	r.Header.Set("X-Real-IP", "203.0.113.10")
	if got := clientIPWithTrust(r, one); got != "203.0.113.10" {
		t.Fatalf("clientIP from X-Real-IP = %q, want 203.0.113.10", got)
	}

	// X-Forwarded-For wins over X-Real-IP. With one trusted hop the client is
	// the entry the single proxy appended — the RIGHTMOST — not a value a
	// client prepended.
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIPWithTrust(r, one); got != "10.0.0.1" {
		t.Fatalf("clientIP from X-Forwarded-For = %q, want 10.0.0.1", got)
	}
}

// A client that prepends a fake X-Forwarded-For entry must not be able to
// choose its own rate-limit key: with the real proxies appending on the right,
// the fake stays on the left and is ignored.
func TestClientIPForwardedForSpoofResisted(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.9:5511" // the innermost proxy
	// Chain: client sent "1.2.3.4"; outer proxy appended the real client
	// (198.51.100.7), inner proxy appended the outer's egress (10.0.0.1).
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.7, 10.0.0.1")

	if got := clientIPWithTrust(r, proxyTrust{hops: 2}); got != "198.51.100.7" {
		t.Fatalf("two trusted hops = %q, want the real client 198.51.100.7", got)
	}
	// Declaring one hop reads only the innermost-appended entry, still never
	// the client-supplied 1.2.3.4.
	if got := clientIPWithTrust(r, proxyTrust{hops: 1}); got != "10.0.0.1" {
		t.Fatalf("one trusted hop = %q, want 10.0.0.1", got)
	}
	// A forged header shorter than the declared chain is not trusted at all.
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIPWithTrust(r, proxyTrust{hops: 2}); got != "10.0.0.9" {
		t.Fatalf("short forged header = %q, want the peer 10.0.0.9", got)
	}
}

// An unforgeable proxy-set header (CF-Connecting-IP) is used verbatim and wins
// over X-Forwarded-For, which is the robust choice behind a CDN.
func TestClientIPTrustedHeaderWins(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.9:5511"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.7")
	r.Header.Set("CF-Connecting-IP", "198.51.100.42")

	trust := proxyTrust{hops: 2, clientHeader: "CF-Connecting-IP"}
	if got := clientIPWithTrust(r, trust); got != "198.51.100.42" {
		t.Fatalf("clientIP did not prefer the trusted header: %q, want 198.51.100.42", got)
	}
	// The header is honored even with no hop count declared.
	if got := clientIPWithTrust(r, proxyTrust{clientHeader: "CF-Connecting-IP"}); got != "198.51.100.42" {
		t.Fatalf("trusted header ignored without hops: %q, want 198.51.100.42", got)
	}
	// When the header is absent the resolver falls back to the hop count.
	r.Header.Del("CF-Connecting-IP")
	if got := clientIPWithTrust(r, trust); got != "1.2.3.4" {
		t.Fatalf("fallback with 2 hops = %q, want 1.2.3.4", got)
	}
}

func TestClientIPTrustFromEnv(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.7:5511"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")

	t.Setenv(envTrustProxy, "")
	if got := clientIP(r); got != "192.0.2.7" {
		t.Fatalf("clientIP with %s unset = %q, want 192.0.2.7", envTrustProxy, got)
	}
	t.Setenv(envTrustProxy, "0")
	if got := clientIP(r); got != "192.0.2.7" {
		t.Fatalf("clientIP with %s=0 = %q, want 192.0.2.7", envTrustProxy, got)
	}
	t.Setenv(envTrustProxy, "1")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("clientIP with %s=1 = %q, want 203.0.113.9", envTrustProxy, got)
	}

	// An explicit hop count overrides the legacy flag and counts from the right.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9, 10.0.0.1")
	t.Setenv(envTrustProxy, "1")
	t.Setenv(envTrustedHops, "2")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("clientIP with %s=2 = %q, want 203.0.113.9", envTrustedHops, got)
	}

	// A trusted client-IP header wins over any hop counting.
	t.Setenv(envClientIPHeader, "CF-Connecting-IP")
	r.Header.Set("CF-Connecting-IP", "198.51.100.42")
	if got := clientIP(r); got != "198.51.100.42" {
		t.Fatalf("clientIP ignored %s: %q, want 198.51.100.42", envClientIPHeader, got)
	}
}

// NewHandler's buckets take the settings HandlerDeps.RateLimits carries,
// which stage handlers of cmd/server reads (internal/config's RateLimits,
// whose tests hold a refill of Inf to the default, so the limit still
// throttles), and a zero field takes the bucket's default.
func TestRateLimiterOrTakesItsSettingsAndDefaults(t *testing.T) {
	clock := newFakeClock()
	l := withClock(newRateLimiterOr(RateLimit{Burst: 2, RefillPerHour: 20}, 5, 20), clock)
	if l.burst != 2 || l.refillPerHour != 20 {
		t.Errorf("burst 2, refill 20: burst %v, refill %v", l.burst, l.refillPerHour)
	}
	l.allow("k")
	l.allow("k")
	clock.advance(time.Second)
	if ok, _ := l.allow("k"); ok {
		t.Error("a third request a second after a burst of 2 was allowed, so the limit is off")
	}
	if l := newRateLimiterOr(RateLimit{}, 5, 20); l.burst != 5 || l.refillPerHour != 20 {
		t.Errorf("no settings: burst %v, refill %v; want the defaults 5 and 20", l.burst, l.refillPerHour)
	}
	if l := newRateLimiterOr(RateLimit{RefillPerHour: 1000}, 5, 20); l.burst != 5 || l.refillPerHour != 1000 {
		t.Errorf("refill 1000 alone: burst %v, refill %v; want the default 5 and 1000", l.burst, l.refillPerHour)
	}
}

// OPENV_TRUST_PROXY is a boolean like every other (#379, question 15): true
// in any case declares the one trusted hop that 1 does, where only the exact
// text 1 used to, and a malformed value, or a hop count that is not a whole
// number above 0, declares nothing.
func TestClientIPTrustProxyIsABoolean(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.7:5511"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	for value, want := range map[string]string{
		"true": "203.0.113.9", "TRUE": "203.0.113.9", " 1 ": "203.0.113.9",
		"false": "192.0.2.7", "0": "192.0.2.7", "yes": "192.0.2.7", "": "192.0.2.7",
	} {
		t.Setenv(envTrustProxy, value)
		if got := clientIP(r); got != want {
			t.Errorf("clientIP with %s=%q = %q, want %q", envTrustProxy, value, got, want)
		}
	}
	t.Setenv(envTrustProxy, "")
	for _, hops := range []string{"0", "-1", "two", "1.5"} {
		t.Setenv(envTrustedHops, hops)
		if got := clientIP(r); got != "192.0.2.7" {
			t.Errorf("clientIP with %s=%q = %q, want the peer 192.0.2.7", envTrustedHops, hops, got)
		}
	}
}

// A setting a request reads on every call is read once by NewHandler too, so
// a malformed one is named in the boot log, as #379's question 15 decided,
// rather than first when a request reads it: only sign-in, registration,
// SSO, password reset, invitations and interviews read the proxy trust, so a
// mistyped hop count, which leaves every client behind the proxy on the
// proxy's own rate-limit bucket, could go unreported for as long as nobody
// signed in. The requests that read it again add no second warning.
// bootSettingRuns numbers the runs of
// TestNewHandlerNamesAMalformedPerRequestSettingAtBoot in this process.
var bootSettingRuns atomic.Int64

func TestNewHandlerNamesAMalformedPerRequestSettingAtBoot(t *testing.T) {
	// Values no other test, and no earlier run of this one (go test
	// -count=2), sets: internal/envparse warns once per variable and value
	// for the life of the process.
	tag := fmt.Sprintf(" (TestNewHandlerNamesAMalformedPerRequestSettingAtBoot, run %d)", bootSettingRuns.Add(1))
	malformed := map[string]string{
		envTrustedHops:   "two" + tag,
		envTrustProxy:    "yes" + tag,
		envMaxUploadMB:   "25 MB" + tag,
		envMaxEvidenceMB: "-1" + tag,
	}
	for name, value := range malformed {
		t.Setenv(name, value)
	}
	// slog.SetDefault also points the log package at the new handler, and
	// setting the old one back does not undo that, so both are restored.
	var logged bytes.Buffer
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	NewHandler(HandlerDeps{})
	atBoot := logged.String()
	for name := range malformed {
		if n := strings.Count(atBoot, "var="+name+" "); n != 1 {
			t.Errorf("NewHandler named %s %d times, want once; log:\n%s", name, n, atBoot)
		}
	}
	if strings.Contains(atBoot, tag) {
		t.Errorf("a warning printed a value:\n%s", atBoot)
	}

	// The per-request reads still happen per request, and stay quiet.
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "192.0.2.7:5511"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(r); got != "192.0.2.7" {
		t.Errorf("clientIP = %q, want the peer 192.0.2.7: a malformed trust declares no proxy", got)
	}
	if _, ok := envUploadMB(); ok {
		t.Errorf("envUploadMB took a malformed %s", envMaxUploadMB)
	}
	if got := maxEvidenceBytes(); got != defaultMaxEvidenceMB*bytesPerMB {
		t.Errorf("maxEvidenceBytes = %d, want the default %d", got, defaultMaxEvidenceMB*bytesPerMB)
	}
	if later := strings.TrimPrefix(logged.String(), atBoot); strings.Contains(later, "malformed setting") {
		t.Errorf("a request's read warned again:\n%s", later)
	}
}
