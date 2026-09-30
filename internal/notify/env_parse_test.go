package notify

import "testing"

// TestEnvParse writes internal/notify's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envDefault (which trims) and typeListFromEnv return for each
// input, called with the probe as the name and a sentinel fallback, which a
// row shows as default.
func TestEnvParse(t *testing.T) {
	fallback := func() []string { return []string{"fallback"} }
	checkEnvParse(t, "internal/notify", map[string][]string{
		"internal/notify:envDefault(key)": envParseRows(t, nil, func() string {
			return envParseResult(envDefault(envParseProbe, "fallback"), "fallback")
		}),
		"internal/notify:typeListFromEnv(key)": envParseRows(t, []string{" a , b ", "b,a,a", "A"}, func() string {
			return envParseResult(typeListFromEnv(envParseProbe, fallback), fallback())
		}),
	})
}
