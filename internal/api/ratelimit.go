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

// Throttling for the billing refresh. The post-checkout return page calls
// it once, and a person staring at a slow page calls it again; it re-reads
// the workspace's subscription from the provider, so it is bounded per
// workspace to keep a stuck client from spending the provider's rate limit.
const (
	envBillingRefreshBurst  = "OPENV_BILLING_REFRESH_BURST"
	envBillingRefreshRefill = "OPENV_BILLING_REFRESH_REFILL_PER_HOUR"

	defaultBillingRefreshBurst  = 10
	defaultBillingRefreshRefill = 120.0

	// The purchase writes — checkout, plan change, portal — each create a
	// provider object, so a compromised admin session cannot spam them.
	envBillingWriteBurst  = "OPENV_BILLING_WRITE_BURST"
	envBillingWriteRefill = "OPENV_BILLING_WRITE_REFILL_PER_HOUR"

	defaultBillingWriteBurst  = 5
	defaultBillingWriteRefill = 20.0
)

// Throttling for the credential endpoints. bcrypt makes each password guess
// expensive for the server but nothing stopped a client from making them
// back to back; these buckets do. Sign-in charges every attempt against the
// client address and every FAILED attempt against the account, so a shared
// office address is not locked out by one colleague's typo while a targeted
// account still closes after a handful of wrong passwords. Registration and
// the single sign-on start/callback charge per address only.
//
// Defaults (overridable via environment variables):
//
//	OPENV_AUTH_IP_BURST                 = 30  sign-in attempts instantly per address
//	OPENV_AUTH_IP_REFILL_PER_HOUR       = 120 sign-in attempts/hour steady state
//	OPENV_AUTH_ACCOUNT_BURST            = 5   failed sign-ins instantly per account
//	OPENV_AUTH_ACCOUNT_REFILL_PER_HOUR  = 20  failed sign-ins/hour steady state
//	OPENV_REGISTER_IP_BURST             = 5   registrations instantly per address
//	OPENV_REGISTER_IP_REFILL_PER_HOUR   = 10  registrations/hour steady state
//	OPENV_SSO_IP_BURST                  = 20  SSO starts or callbacks instantly per address
//	OPENV_SSO_IP_REFILL_PER_HOUR        = 60  SSO starts or callbacks/hour steady state
//	OPENV_VERIFY_RESEND_BURST           = 3   verification mails instantly per account
//	OPENV_VERIFY_RESEND_REFILL_PER_HOUR = 6   verification mails/hour steady state
//	OPENV_INVITE_PREVIEW_BURST          = 60  invite-link previews instantly per address
//	OPENV_INVITE_PREVIEW_REFILL_PER_HOUR = 240 invite-link previews/hour steady state
//	OPENV_INVITE_BURST                  = 20  invitations instantly per inviting account
//	OPENV_INVITE_REFILL_PER_HOUR        = 60  invitations/hour steady state
//
// Creating an invitation sends mail to an address the sender chose, so the
// endpoint is a relay: without a bound, one admin account — or one stolen
// admin session — could point the deployment's SMTP credentials at a list
// and spend its sending reputation. The bucket is keyed on the INVITING
// ACCOUNT rather than the address: an admin's browser and their scripted
// bulk invite share one budget wherever they run from, and one admin
// working through a shared office address cannot exhaust their colleagues'.
//
// Previewing an invite link gets its own, deliberately generous bucket
// instead of drawing on the sign-in one. It is not a credential attempt: the
// token is 256 bits, so the limit only bounds database lookups, and the page
// re-previews on every reload, second tab, or return to the link after
// signing out. Charging that against sign-in attempts would let opening an
// invitation lock somebody out of the account they were invited to use —
// and a shared office address, where several colleagues open their links the
// same morning, would run the sign-in bucket down for everyone.
const (
	envAuthIPBurst             = "OPENV_AUTH_IP_BURST"
	envAuthIPRefill            = "OPENV_AUTH_IP_REFILL_PER_HOUR"
	envAuthAccountBurst        = "OPENV_AUTH_ACCOUNT_BURST"
	envAuthAccountRefill       = "OPENV_AUTH_ACCOUNT_REFILL_PER_HOUR"
	envRegisterIPBurst         = "OPENV_REGISTER_IP_BURST"
	envRegisterIPRefill        = "OPENV_REGISTER_IP_REFILL_PER_HOUR"
	envSSOIPBurst              = "OPENV_SSO_IP_BURST"
	envSSOIPRefill             = "OPENV_SSO_IP_REFILL_PER_HOUR"
	envVerifyResendBurst       = "OPENV_VERIFY_RESEND_BURST"
	envVerifyResendRefill      = "OPENV_VERIFY_RESEND_REFILL_PER_HOUR"
	envPasswordResetBurst      = "OPENV_PASSWORD_RESET_BURST"
	envPasswordResetRefill     = "OPENV_PASSWORD_RESET_REFILL_PER_HOUR"
	envInvitePreviewBurst      = "OPENV_INVITE_PREVIEW_BURST"
	envInvitePreviewRefill     = "OPENV_INVITE_PREVIEW_REFILL_PER_HOUR"
	envInviteBurst             = "OPENV_INVITE_BURST"
	envInviteRefill            = "OPENV_INVITE_REFILL_PER_HOUR"
	defaultAuthIPBurst         = 30
	defaultAuthIPRefill        = 120.0
	defaultAuthAccountBurst    = 5
	defaultAuthAccountRefill   = 20.0
	defaultRegisterIPBurst     = 5
	defaultRegisterIPRefill    = 10.0
	defaultSSOIPBurst          = 20
	defaultSSOIPRefill         = 60.0
	defaultVerifyResendBurst   = 3
	defaultVerifyResendRefill  = 6.0
	defaultPasswordResetBurst  = 3
	defaultPasswordResetRefill = 6.0
	defaultInvitePreviewBurst  = 60
	defaultInvitePreviewRefill = 240.0
	defaultInviteBurst         = 20
	defaultInviteRefill        = 60.0
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

// check reports whether key currently has a token without consuming one, so
// a caller can refuse a request that would fail anyway and charge the bucket
// only once the outcome is known (see penalize). A nil limiter allows
// everything.
func (l *rateLimiter) check(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refillLocked(key)
	if b.tokens >= 1 {
		return true, 0
	}
	waitHours := (1 - b.tokens) / l.refillPerHour
	return false, time.Duration(waitHours * float64(time.Hour))
}

// penalize consumes one token for key, letting it go negative-free: a bucket
// already empty stays empty and its wait simply restarts. Used to charge a
// failed sign-in against the account it targeted.
func (l *rateLimiter) penalize(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refillLocked(key)
	if b.tokens >= 1 {
		b.tokens--
	} else {
		b.tokens = 0
	}
}

// refillLocked returns key's bucket after crediting the tokens elapsed time
// has earned it, creating a full bucket for an unseen key. Called with l.mu
// held.
func (l *rateLimiter) refillLocked(key string) *bucket {
	now := l.now()
	l.maybeCleanupLocked(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
		return b
	}
	if elapsed := now.Sub(b.last).Hours(); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.refillPerHour)
		b.last = now
	}
	return b
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
//
// X-Forwarded-For grows LEFT to RIGHT: every proxy appends the address it saw
// the connection arrive from, so the entries a client can forge are the
// leftmost ones (whatever it sent before the first trusted proxy appended the
// real peer). Trusting the leftmost entry therefore trusts the attacker — the
// bug this resolver is written to avoid. The trustworthy client address is the
// entry N hops in from the RIGHT, where N is the number of proxies between the
// app and the internet.
//
//	OPENV_TRUSTED_PROXY_HOPS  number of trusted proxies in front of the app.
//	                          The client is the X-Forwarded-For entry this many
//	                          hops from the right; 0 or unset trusts no header
//	                          and keys on the TCP peer address.
//	OPENV_CLIENT_IP_HEADER    a single header the OUTERMOST trusted proxy sets
//	                          to the real client and that a client cannot forge
//	                          THROUGH it — Cloudflare's CF-Connecting-IP, Akamai's
//	                          True-Client-IP. When set it wins, which is the
//	                          robust choice behind a CDN that rewrites it on
//	                          every request regardless of chain depth.
//	OPENV_TRUST_PROXY=1       legacy alias for one trusted hop; still honored,
//	                          now counting from the right like the rest, so a
//	                          single trusted proxy keeps working and extra
//	                          client-supplied entries no longer shift the key.
const (
	envTrustProxy     = "OPENV_TRUST_PROXY"
	envTrustedHops    = "OPENV_TRUSTED_PROXY_HOPS"
	envClientIPHeader = "OPENV_CLIENT_IP_HEADER"
)

// proxyTrust is the operator's declaration of what sits in front of the app.
type proxyTrust struct {
	hops         int    // trusted proxies between the app and the internet
	clientHeader string // a proxy-set header naming the real client, if any
}

// proxyTrustFromEnv reads the trust declaration. An explicit hop count wins;
// the legacy OPENV_TRUST_PROXY=1 is honored as a single hop.
func proxyTrustFromEnv() proxyTrust {
	t := proxyTrust{clientHeader: strings.TrimSpace(os.Getenv(envClientIPHeader))}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(envTrustedHops))); err == nil && n > 0 {
		t.hops = n
	} else if os.Getenv(envTrustProxy) == "1" {
		t.hops = 1
	}
	return t
}

// clientIP extracts the requesting client's IP under the deployment's declared
// proxy trust (see proxyTrust). With no trust declared it returns the TCP peer
// address, so a direct client cannot shift its own rate-limit key by sending a
// forwarding header.
func clientIP(r *http.Request) string {
	return clientIPWithTrust(r, proxyTrustFromEnv())
}

// clientIPWithTrust is clientIP with the trust declaration injected for tests.
func clientIPWithTrust(r *http.Request, t proxyTrust) string {
	// An unforgeable proxy-set header (CF-Connecting-IP and the like) is the
	// most robust source: the outermost proxy overwrites it on every request,
	// so its value never depends on counting a variable-length header.
	if t.clientHeader != "" {
		if v := strings.TrimSpace(r.Header.Get(t.clientHeader)); v != "" {
			return stripPort(v)
		}
	}
	if t.hops > 0 {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			// The rightmost entry was appended by the proxy nearest the app;
			// step in one place per trusted hop to reach the address the
			// outermost trusted proxy saw. Entries left of that are
			// client-supplied and ignored.
			if idx := len(parts) - t.hops; idx >= 0 {
				if ip := strings.TrimSpace(parts[idx]); ip != "" {
					return stripPort(ip)
				}
			}
			// Fewer hops arrived than declared: the request did not traverse
			// the expected chain, so nothing in the header is trustworthy.
			return stripPort(r.RemoteAddr)
		}
		// A single trusted proxy that forwards X-Real-IP instead.
		if t.hops == 1 {
			if rip := strings.TrimSpace(r.Header.Get("X-Real-IP")); rip != "" {
				return stripPort(rip)
			}
		}
	}
	return stripPort(r.RemoteAddr)
}

// stripPort returns s without a trailing :port. A bare IP (the usual
// X-Forwarded-For entry) and a bracketed IPv6 literal both pass through
// unchanged; only a host:port pair is trimmed.
func stripPort(s string) string {
	s = strings.TrimSpace(s)
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
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
