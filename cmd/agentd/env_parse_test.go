package main

import (
	"testing"
	"time"
)

// TestEnvParse writes cmd/agentd's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envOr, envIntOr and envDurationOr return for each input,
// called with the probe as the name and a sentinel fallback, which a row
// shows as default. Their values become flag defaults; cli_test.go pins
// that through agentd -h.
func TestEnvParse(t *testing.T) {
	checkEnvParse(t, "cmd/agentd", map[string][]string{
		"cmd/agentd:envOr(key)": envParseRows(t, nil, func() string {
			return envParseResult(envOr(envParseProbe, "fallback"), "fallback")
		}),
		"cmd/agentd:envIntOr(key)": envParseRows(t, nil, func() string {
			return envParseResult(envIntOr(envParseProbe, 42), 42)
		}),
		"cmd/agentd:envDurationOr(key)": envParseRows(t, nil, func() string {
			return envParseResult(envDurationOr(envParseProbe, 90*time.Minute), 90*time.Minute)
		}),
	})
}
