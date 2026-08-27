package api

import (
	"net/http/httptest"
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

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.7:5511"
	if got := clientIP(r); got != "192.0.2.7" {
		t.Fatalf("clientIP from RemoteAddr = %q, want 192.0.2.7", got)
	}

	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("clientIP from X-Forwarded-For = %q, want 203.0.113.9", got)
	}
}
