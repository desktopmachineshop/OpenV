package orgs

import (
	"errors"
	"fmt"
)

// Hitting a limit.
//
// A refusal is only useful if it answers three questions at once: what stopped
// me, how close am I, and what do I do about it. A bare "limit exceeded"
// answers none of them and sends the person to support.
//
// The third answer depends on who is running the deployment, and getting it
// wrong is worse than saying nothing. Telling a self-hoster to "upgrade your
// plan" is nonsense — there is no plan and nobody to pay; they own the
// hardware and need to know which setting to change. Telling a hosted member
// to edit OPENV_LIMITS is equally useless, because they have no shell. So the
// remedy is chosen from the deployment mode, once, at boot.

// selfHosted records whether this deployment is somebody's own installation.
// The hosted service leaves it false.
var selfHosted bool

// SetSelfHosted tells the domain which remedy to offer when a limit is hit.
// Called once at boot from the deployment configuration.
func SetSelfHosted(v bool) { selfHosted = v }

// SelfHosted reports the configured deployment mode.
func SelfHosted() bool { return selfHosted }

// ErrLimitReached is the sentinel every limit refusal wraps, so handlers can
// map the whole family onto one status code without knowing the keys.
var ErrLimitReached = errors.New("workspace limit reached")

// LimitError is a refusal with its arithmetic attached, so the API can hand
// the frontend a message, a machine-readable key and the numbers behind it
// rather than only prose.
type LimitError struct {
	// Key is the catalogued limit, e.g. "max_members".
	Key string
	// Label is the human name from the catalogue.
	Label string
	// Used is the count at the moment of refusal; Allowed is the ceiling.
	Used    int
	Allowed int
	Unit    Unit
	// Detail is an optional clause explaining what counts towards Used, for
	// limits where that is not obvious.
	Detail string
}

// NewLimitError builds a refusal for one catalogued limit.
func NewLimitError(key string, used, allowed int) *LimitError {
	e := &LimitError{Key: key, Used: used, Allowed: allowed, Label: key}
	if def, ok := Describe(key); ok {
		e.Label = def.Label
		e.Unit = def.Unit
	}
	return e
}

// WithDetail attaches the clause naming what counts towards the total.
func (e *LimitError) WithDetail(detail string) *LimitError {
	e.Detail = detail
	return e
}

// Is makes every LimitError match ErrLimitReached.
func (e *LimitError) Is(target error) bool { return target == ErrLimitReached }

// Error is the whole message a person reads: what stopped them, the numbers,
// and what to do next.
func (e *LimitError) Error() string {
	msg := fmt.Sprintf("%s: this workspace allows %s and already has %s",
		e.Label, e.amount(e.Allowed), e.amount(e.Used))
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return msg + ". " + e.Remedy()
}

// Remedy is the sentence that tells the reader how to raise the ceiling,
// written for whoever is actually able to do it on this deployment.
func (e *LimitError) Remedy() string {
	if selfHosted {
		return fmt.Sprintf(
			"This deployment sets its own limits: raise %s in OPENV_LIMITS to change it everywhere, "+
				"or set it on this workspace alone to change it here.", e.Key)
	}
	return "Upgrade the workspace's plan to raise this limit, or ask a workspace admin to."
}

// amount renders a number in the limit's unit, so a storage refusal does not
// read as a count of megabytes.
func (e *LimitError) amount(n int) string {
	switch e.Unit {
	case UnitMB:
		return fmt.Sprintf("%d MB", n)
	case UnitMinutes:
		return fmt.Sprintf("%d minutes", n)
	case UnitCPUs:
		return fmt.Sprintf("%d CPUs", n)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// CheckCeiling is the whole enforcement pattern in one call: read the limit,
// treat absent or zero as unlimited, and refuse when adding `adding` more
// would cross it. Returns nil when there is room.
//
// Every count limit goes through this so that "unlimited", off-by-one and the
// refusal wording cannot drift apart between call sites.
func CheckCeiling(limits map[string]interface{}, key string, used, adding int) error {
	allowed, capped := Ceiling(limits, key)
	if !capped {
		return nil
	}
	if used+adding > allowed {
		return NewLimitError(key, used, allowed)
	}
	return nil
}
