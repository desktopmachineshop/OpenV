package main

import (
	"testing"
	"time"
)

// TestEnvParse writes cmd/agentd's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envOr, envIntOr, envDurationOr, envBool and envSecret return
// for each input, called with the probe as the name and a sentinel
// fallback, which a row shows as default. The first four's values become
// flag defaults, which cli_test.go pins through agentd -h; envSecret's, the
// keys, are read after parsing.
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
		"cmd/agentd:envBool(key)": envParseRows(t, nil, func() string {
			return envParseBool(envBool(envParseProbe, false), envBool(envParseProbe, true))
		}),
		"cmd/agentd:envSecret(key)": envParseRows(t, []string{"key\n", " key"}, func() string {
			return envParseResult(envSecret(envParseProbe, "fallback"), "fallback")
		}),
	})
}

// envParseBool renders a boolean getter's row from its results with each
// fallback: default when each call returned its own fallback, else the value
// both returned.
func envParseBool(withFalse, withTrue bool) string {
	switch {
	case !withFalse && withTrue:
		return "default"
	case withFalse == withTrue:
		return envParseResult(withFalse, !withFalse)
	}
	return "unrenderable: the fallback inverted the result"
}
