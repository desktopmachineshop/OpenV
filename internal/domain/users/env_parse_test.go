package users

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

// TestEnvParse writes internal/domain/users' sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what envDuration returns for each input. Its fallback is also its
// ceiling, so the parse depends on the variable: one section per variable,
// each called with that variable's own default (SessionPolicyFromEnv pairs
// them), which a row shows as default. The warnings it logs are discarded.
func TestEnvParse(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	sections := map[string][]string{}
	for name, max := range map[string]time.Duration{envSessionMaxAge: DefaultSessionMaxAge, envSessionIdle: DefaultSessionIdle} {
		sections["internal/domain/users:envDuration(name) "+name] = envParseRows(t, []string{"719h", "167h59m"}, func() string {
			return envParseResult(envDuration(envParseProbe, max), max)
		})
	}
	checkEnvParse(t, "internal/domain/users", sections)
}
