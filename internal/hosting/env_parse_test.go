package hosting

import "testing"

// TestEnvParse writes internal/hosting's section of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envOr returns for each input, called with the probe as the
// name and a sentinel fallback, which a row shows as default.
func TestEnvParse(t *testing.T) {
	checkEnvParse(t, "internal/hosting", map[string][]string{
		"internal/hosting:envOr(key)": envParseRows(t, nil, func() string {
			return envParseResult(envOr(envParseProbe, "fallback"), "fallback")
		}),
	})
}
