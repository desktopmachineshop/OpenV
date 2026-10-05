package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// respondJSON writes a status and a JSON body, which every handler here does.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// limitAction is what a limitPolicy does with one kind of ?limit= value.
type limitAction uint8

const (
	// limitKeep hands the value on as it is. For a value strconv.Atoi
	// refuses, that is Atoi's own answer: 0, or on overflow the saturated
	// math.MaxInt or math.MinInt, which then meets the policy's low and
	// high actions.
	limitKeep limitAction = iota
	// limitDefault hands on the policy's default instead.
	limitDefault
	// limitMax hands on the policy's maximum instead.
	limitMax
	// limitRefuse answers 400 with the policy's refusal.
	limitRefuse
)

// limitPolicy is how one list route reads its ?limit= parameter (quirk
// Q8). A missing or empty value is def. A value strconv.Atoi refuses meets
// notInt, an integer below 1 meets low, and one above max meets high; a
// policy with max 0 sets no maximum. Any other integer is handed on as it
// is.
type limitPolicy struct {
	def     int
	max     int
	notInt  limitAction
	low     limitAction
	high    limitAction
	refusal string
}

// The seven limit policies (Q8), one per list route, each the rule its
// handler parsed inline before X3c. Where a default or a cap sits below the
// handler, it stays there, and the policy hands that layer what the inline
// parser did.
var (
	// agentRunsLimit (ListAgentRuns) hands on whatever strconv.Atoi
	// answers. The run repository resets <= 0 and > 500 to 100 at the SQL
	// LIMIT.
	agentRunsLimit = limitPolicy{
		def:    0,
		max:    0,
		notInt: limitKeep,
		low:    limitKeep,
		high:   limitKeep,
	}
	// artifactsLimit (ListArtifacts): the default and the maximum are one
	// value, so anything out of range is a full page.
	artifactsLimit = limitPolicy{
		def:    defaultArtifactPageLimit,
		max:    maxArtifactPageLimit,
		notInt: limitKeep,
		low:    limitDefault,
		high:   limitDefault,
	}
	// domainEventsLimit (ListDomainEvents) resets an out-of-range value to
	// 100 instead of capping it at 500. The event repository applies the
	// same rule again.
	domainEventsLimit = limitPolicy{
		def:    100,
		max:    500,
		notInt: limitKeep,
		low:    limitDefault,
		high:   limitDefault,
	}
	// searchLimit (GlobalSearch) caps at its maximum, so an overflowing
	// value is the maximum here, not the default.
	searchLimit = limitPolicy{
		def:    defaultSearchLimit,
		max:    maxSearchLimit,
		notInt: limitKeep,
		low:    limitDefault,
		high:   limitMax,
	}
	// interviewSessionsLimit (ListProjectInterviewSessions) refuses a value
	// that is not an integer and hands on any integer. The interview service
	// makes <= 0 its default of 20 and caps at 100.
	interviewSessionsLimit = limitPolicy{
		def:     0,
		max:     0,
		notInt:  limitRefuse,
		low:     limitKeep,
		high:    limitKeep,
		refusal: "limit must be an integer",
	}
	// notificationsLimit (ListNotifications) refuses a value that is not an
	// integer or is below 1, and caps at its maximum.
	notificationsLimit = limitPolicy{
		def:     defaultNotificationLimit,
		max:     maxNotificationLimit,
		notInt:  limitRefuse,
		low:     limitRefuse,
		high:    limitMax,
		refusal: "limit must be a positive integer",
	}
	// sharedProductsLimit (ListSharedProducts) drops a value that is not an
	// integer, overflow included, and hands on any integer. The
	// shared-product service makes <= 0 its default of 200 and caps at 500.
	sharedProductsLimit = limitPolicy{
		def:    0,
		max:    0,
		notInt: limitDefault,
		low:    limitKeep,
		high:   limitKeep,
	}
)

// parseLimit reads r's ?limit= by policy p. When the policy refuses the
// value, parseLimit has written the 400 and ok is false.
func parseLimit(w http.ResponseWriter, r *http.Request, p limitPolicy) (limit int, ok bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return p.def, true
	}
	n, err := strconv.Atoi(raw)
	switch {
	case err != nil && p.notInt != limitKeep:
		return p.apply(w, p.notInt, n)
	case n < 1:
		return p.apply(w, p.low, n)
	case p.max > 0 && n > p.max:
		return p.apply(w, p.high, n)
	}
	return n, true
}

// apply carries out action a on the value n.
func (p limitPolicy) apply(w http.ResponseWriter, a limitAction, n int) (int, bool) {
	switch a {
	case limitDefault:
		return p.def, true
	case limitMax:
		return p.max, true
	case limitRefuse:
		writeJSONError(w, http.StatusBadRequest, p.refusal)
		return 0, false
	}
	return n, true
}
