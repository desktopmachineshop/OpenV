package main

import "testing"

// TestEnvParse writes cmd/server's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envOr and envInt return for each input, called with the probe
// as the name and a sentinel fallback, which a row shows as default. The
// server's other reads parse inline; env_vars.txt lists them.
func TestEnvParse(t *testing.T) {
	checkEnvParse(t, "cmd/server", map[string][]string{
		"cmd/server:envOr(key)": envParseRows(t, nil, func() string {
			return envParseResult(envOr(envParseProbe, "fallback"), "fallback")
		}),
		"cmd/server:envInt(key)": envParseRows(t, nil, func() string {
			return envParseResult(envInt(envParseProbe, 42), 42)
		}),
	})
}
