package api

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rate limiting for the public (token-only) interview endpoints. Every
// participant message enqueues a priority LLM run, so a leaked invite token
// must not translate into unbounded provider spend. Limits are enforced with
// small in-memory token buckets — no external dependencies — which is
// adequate for a single API instance (the deployment model today; see
// docker-compose.yml).
//
// Defaults (overridable via environment variables):
//
//	OPENV_INTERVIEW_MSG_BURST               = 5   messages instantly per invite
//	OPENV_INTERVIEW_MSG_REFILL_PER_HOUR     = 20  messages/hour steady state
//	OPENV_INTERVIEW_IP_BURST                = 20  intro GETs instantly per IP
//	OPENV_INTERVIEW_IP_REFILL_PER_HOUR      = 60  intro GETs/hour steady state
//	OPENV_INTERVIEW_STREAM_BURST            = 30  SSE connects instantly per IP
//	OPENV_INTERVIEW_STREAM_REFILL_PER_HOUR  = 120 SSE connects/hour steady state
//
// SSE stream connects get their own, more generous per-IP bucket:
// EventSource clients auto-reconnect after every network hiccup, NAT timeout,
// or laptop sleep, so a legitimate participant reconnects far more often than
// they load the intro page. Charging reconnects against the intro bucket
// would lock a flaky-network participant out of the whole interview; the
// separate bucket still bounds connection floods without letting stream
// churn consume the intro budget.
const (
	envInterviewMsgBurst     = "OPENV_INTERVIEW_MSG_BURST"
	envInterviewMsgRefill    = "OPENV_INTERVIEW_MSG_REFILL_PER_HOUR"
	envInterviewIPBurst      = "OPENV_INTERVIEW_IP_BURST"
	envInterviewIPRefill     = "OPENV_INTERVIEW_IP_REFILL_PER_HOUR"
	envInterviewStreamBurst  = "OPENV_INTERVIEW_STREAM_BURST"
	envInterviewStreamRefill = "OPENV_INTERVIEW_STREAM_REFILL_PER_HOUR"

	defaultInterviewMsgBurst     = 5
	defaultInterviewMsgRefill    = 20.0
	defaultInterviewIPBurst      = 20
	defaultInterviewIPRefill     = 60.0
	defaultInterviewStreamBurst  = 30
	defaultInterviewStreamRefill = 120.0
)

// cleanupEvery bounds how often a limiter sweeps stale buckets, and
// staleAfter is how long a bucket must sit untouched before the sweep drops
// it (any bucket idle that long has refilled to burst, so dropping it is
// behavior-neutral).
const (
	cleanupEvery = 10 * time.Minute
	staleAfter   = 2 * time.Hour
)

// bucket is one key's token-bucket state.
type bucket struct {
	tokens float64   // current tokens, <= burst
	last   time.Time // last refill timestamp
}

// rateLimiter is a keyed token-bucket rate limiter safe for concurrent use.
// Each key gets `burst` instant requests, then refills at refillPerHour.
type rateLimiter struct {
	mu            sync.Mutex
	buckets       map[string]*bucket
	burst         float64
	refillPerHour float64
	lastCleanup   time.Time
	now           func() time.Time // injectable clock for tests
}

// newRateLimiter creates a limiter allowing `burst` immediate requests per
// key with a steady-state refill of refillPerHour tokens per hour.
func newRateLimiter(burst int, refillPerHour float64) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	if refillPerHour <= 0 {
		refillPerHour = 1
	}
	return &rateLimiter{
		buckets:       map[string]*bucket{},
		burst:         float64(burst),
		refillPerHour: refillPerHour,
		lastCleanup:   time.Now(),
		now:           time.Now,
	}
}

// newRateLimiterFromEnv builds a limiter from a pair of environment
// variables, falling back to the given defaults when unset or invalid.
func newRateLimiterFromEnv(burstVar, refillVar string, defBurst int, defRefill float64) *rateLimiter {
	burst := defBurst
	if v := os.Getenv(burstVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			burst = n
		}
	}
	refill := defRefill
	if v := os.Getenv(refillVar); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			refill = f
		}
	}
	return newRateLimiter(burst, refill)
}

// allow consumes one token for key when available. It returns whether the
// request may proceed and, when it may not, roughly how long until the next
// token becomes available. A nil limiter allows everything (so handlers
// constructed without limiters — e.g. in unrelated tests — keep working).
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.maybeCleanupLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		elapsed := now.Sub(b.last).Hours()
		if elapsed > 0 {
			b.tokens = math.Min(l.burst, b.tokens+elapsed*l.refillPerHour)
			b.last = now
		}
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	waitHours := (1 - b.tokens) / l.refillPerHour
	return false, time.Duration(waitHours * float64(time.Hour))
}

// maybeCleanupLocked drops buckets untouched long enough to have fully
// refilled. Called with l.mu held, at most once per cleanupEvery.
func (l *rateLimiter) maybeCleanupLocked(now time.Time) {
	if now.Sub(l.lastCleanup) < cleanupEvery {
		return
	}
	l.lastCleanup = now
	for key, b := range l.buckets {
		if now.Sub(b.last) >= staleAfter {
			delete(l.buckets, key)
		}
	}
}

// envTrustProxy gates whether clientIP honors proxy-supplied headers.
// X-Forwarded-For / X-Real-IP are ordinary request headers any direct client
// can set, so trusting them unconditionally would let a caller evade the
// per-IP limits (or pollute another client's bucket) by spoofing a header.
// Set OPENV_TRUST_PROXY=1 only when the API is deployed behind a reverse
// proxy that overwrites these headers (see docs/operations.md); any other
// value — including unset — keys limits on the TCP peer address.
const envTrustProxy = "OPENV_TRUST_PROXY"

// clientIP extracts the requesting client's IP: the usual proxy headers when
// the operator has declared them trustworthy (OPENV_TRUST_PROXY=1), otherwise
// the connection's remote address.
func clientIP(r *http.Request) string {
	return clientIPTrusting(r, os.Getenv(envTrustProxy) == "1")
}

// clientIPTrusting is clientIP with the trust decision injected for tests.
func clientIPTrusting(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First hop is the original client.
			if first, _, ok := strings.Cut(xff, ","); ok {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(xff)
		}
		if rip := r.Header.Get("X-Real-IP"); rip != "" {
			return strings.TrimSpace(rip)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// writeRateLimited answers 429 with a JSON body the interview chat UI can
// show verbatim, plus a Retry-After hint.
func writeRateLimited(w http.ResponseWriter, message string, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
