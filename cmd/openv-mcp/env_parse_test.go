package main

import (
	"os"
	"testing"
)

// TestEnvParse writes cmd/openv-mcp's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what resolveToken returns for each input of one variable, bound to
// the probe, while the other is unset (OPENV_RUN_TOKEN, with
// OPENV_API_TOKEN at a sentinel shown as default) or set (OPENV_API_TOKEN,
// with OPENV_RUN_TOKEN unset). main reads the variables through
// resolveToken(os.Getenv).
func TestEnvParse(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string {
			if vars[name] == envParseProbe {
				return os.Getenv(envParseProbe)
			}
			return vars[name]
		}
	}
	checkEnvParse(t, "cmd/openv-mcp", map[string][]string{
		"cmd/openv-mcp:resolveToken(env) OPENV_RUN_TOKEN": envParseRows(t, nil, func() string {
			got := resolveToken(env(map[string]string{"OPENV_RUN_TOKEN": envParseProbe, "OPENV_API_TOKEN": "fallback"}))
			return envParseResult(got, "fallback")
		}),
		"cmd/openv-mcp:resolveToken(env) OPENV_API_TOKEN": envParseRows(t, nil, func() string {
			return envParseResult(resolveToken(env(map[string]string{"OPENV_API_TOKEN": envParseProbe})), "fallback")
		}),
	})
}
