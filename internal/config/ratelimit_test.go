package config

import "testing"

// A rate limit's settings follow internal/envparse's rule (#379, question
// 15): a refill of Inf used to parse as +Inf, which refilled every bucket at
// once and switched the limit off, and a burst with spaces round it was
// ignored. Now Inf keeps the default refill, so the limit still throttles,
// and the burst is trimmed. internal/api's test of newRateLimiterFromEnv,
// held here against RateLimits, which reads the pair once refactor step X10b
// hands the handler values.
func TestRateLimitKeepsThrottlingOnAnInfiniteRefill(t *testing.T) {
	quietLog(t)
	for _, refill := range []string{"Inf", "+Inf", "Infinity", "NaN", "0", "-1"} {
		l := loadFrom(map[string]string{"OPENV_AUTH_IP_BURST": " 2 ", "OPENV_AUTH_IP_REFILL_PER_HOUR": refill}).RateLimits().AuthIP
		if l.Burst != 2 || l.RefillPerHour != 120 {
			t.Errorf("burst %q, refill %q: burst %v, refill %v; want 2 and the default 120", " 2 ", refill, l.Burst, l.RefillPerHour)
		}
	}
	// A finite rate in any float form still counts.
	if l := loadFrom(map[string]string{"OPENV_AUTH_IP_REFILL_PER_HOUR": "1e3"}).RateLimits().AuthIP; l.RefillPerHour != 1000 {
		t.Errorf("refill 1e3 = %v, want 1000", l.RefillPerHour)
	}
}
