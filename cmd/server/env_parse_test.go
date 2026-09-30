package main

import "testing"

// TestEnvParse writes cmd/server's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envOr, envInt, envBool and envSecret return for each input,
// called with the probe as the name and a sentinel fallback, which a row
// shows as default. The server's other reads parse inline; env_vars.txt
// lists them.
func TestEnvParse(t *testing.T) {
	checkEnvParse(t, "cmd/server", map[string][]string{
		"cmd/server:envOr(key)": envParseRows(t, nil, func() string {
			return envParseResult(envOr(envParseProbe, "fallback"), "fallback")
		}),
		"cmd/server:envInt(key)": envParseRows(t, nil, func() string {
			return envParseResult(envInt(envParseProbe, 42), 42)
		}),
		"cmd/server:envBool(key)": envParseRows(t, nil, func() string {
			return envParseBool(envBool(envParseProbe, false), envBool(envParseProbe, true))
		}),
		"cmd/server:envSecret(key)": envParseRows(t, []string{"key\n", " key"}, func() string {
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
