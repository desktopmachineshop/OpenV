package api

import (
	"os"
	"testing"
)

// TestEnvParse writes internal/api's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): the burst and the hourly refill a limiter from
// newRateLimiterFromEnv holds for each input of one of its two variables,
// bound to the probe while the other is unset, with sentinel defaults, which
// a row shows as default. NewHandler builds its 13 limiters with it.
func TestEnvParse(t *testing.T) {
	const unset = envParseProbe + "_UNSET"
	t.Setenv(unset, "")
	os.Unsetenv(unset)
	checkEnvParse(t, "internal/api", map[string][]string{
		"internal/api:newRateLimiterFromEnv(burstVar)": envParseRows(t, nil, func() string {
			return envParseResult(newRateLimiterFromEnv(envParseProbe, unset, 42, 4.5).burst, 42)
		}),
		"internal/api:newRateLimiterFromEnv(refillVar)": envParseRows(t, nil, func() string {
			return envParseResult(newRateLimiterFromEnv(unset, envParseProbe, 42, 4.5).refillPerHour, 4.5)
		}),
	})
}
