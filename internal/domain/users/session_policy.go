package users

// Session lifetime (REQ-99 / HAZ-16). A session ends two ways: an absolute
// deadline measured from sign-in, and an idle deadline measured from the last
// request it made. Both are operator-configurable, and both are capped — an
// operator may shorten a session's life, never lengthen it past the ceiling
// the requirement sets, because a stolen cookie's value is exactly how long
// it stays usable.

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// Session lifetime bounds and defaults.
const (
	// DefaultSessionMaxAge is how long a session stays valid from sign-in,
	// and also the ceiling: OPENV_SESSION_MAX_AGE can only shorten it.
	DefaultSessionMaxAge = 30 * 24 * time.Hour
	// DefaultSessionIdle is how long a session survives without a request,
	// and also the ceiling for OPENV_SESSION_IDLE.
	DefaultSessionIdle = 7 * 24 * time.Hour
	// SessionTouchInterval bounds how often a live session writes
	// last_seen_at. Idle expiry has day granularity, so a write per request
	// buys nothing and costs one UPDATE on every authenticated call.
	SessionTouchInterval = time.Minute

	envSessionMaxAge = "OPENV_SESSION_MAX_AGE"
	envSessionIdle   = "OPENV_SESSION_IDLE"
)

// SessionPolicy is the deployment's session lifetime configuration. The zero
// value means the defaults, so a service that was never configured behaves
// exactly as it did before the policy existed.
type SessionPolicy struct {
	// MaxAge is the absolute lifetime, measured from session creation.
	MaxAge time.Duration
	// Idle is how long a session may go unused before it expires.
	Idle time.Duration
}

// Normalized fills in the defaults for zero fields and clamps both values to
// their ceilings, so every read of the policy sees usable durations — a zero
// policy (a service or handler that was never configured) behaves exactly as
// the code did before the policy existed.
func (p SessionPolicy) Normalized() SessionPolicy {
	if p.MaxAge <= 0 || p.MaxAge > DefaultSessionMaxAge {
		p.MaxAge = DefaultSessionMaxAge
	}
	if p.Idle <= 0 || p.Idle > DefaultSessionIdle {
		p.Idle = DefaultSessionIdle
	}
	return p
}

// SessionPolicyFromEnv reads OPENV_SESSION_MAX_AGE and OPENV_SESSION_IDLE
// (Go duration strings, e.g. "12h", "45m"). Anything unparseable or out of
// range falls back to the default, and a value above the ceiling is clamped;
// each of those decisions logs one line, so an operator who typed "30d" — not
// a Go duration — sees why their sessions still last 30 days.
func SessionPolicyFromEnv() SessionPolicy {
	p := SessionPolicy{
		MaxAge: envDuration(envSessionMaxAge, DefaultSessionMaxAge),
		Idle:   envDuration(envSessionIdle, DefaultSessionIdle),
	}
	slog.Info("session lifetime", "max_age", p.MaxAge, "idle", p.Idle)
	return p
}

// envDuration parses one duration variable, clamping it to max.
func envDuration(name string, max time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return max
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("session lifetime: ignoring unusable value", "var", name, "value", raw, "using", max)
		return max
	}
	if d > max {
		slog.Warn("session lifetime: value above the ceiling, clamping", "var", name, "value", d, "using", max)
		return max
	}
	return d
}
