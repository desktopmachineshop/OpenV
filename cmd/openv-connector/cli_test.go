package main

import (
	"os"
	"path/filepath"
	"testing"
)

// cliName is the command cli_harness_test.go builds and runs.
const cliName = "openv-connector"

// TestCLI snapshots the connector's command line (refactor plan S8,
// invariant I14): the banner, install and uninstall (which only Windows
// supports), the --insecure token, and each openv-connector:// action,
// unpaired and paired. Every scenario stops before the network: unpaired,
// or declining cleartext pairing at the prompt (stdin is empty, which
// answers no), or paired but with no agentd next to the binary, which the
// harness builds alone in its directory; the pairing's API address is a
// closed local port all the same. Each error waits for Enter, which the
// empty stdin gives at once.
func TestCLI(t *testing.T) {
	runCLI(t, []cliScenario{
		{name: "no_args"},
		{name: "insecure_only", args: []string{"--insecure"}},
		{name: "install", args: []string{"install"}},
		{name: "uninstall", args: []string{"uninstall"}},
		{name: "start", args: []string{"openv-connector://start"}},
		{name: "start_org", args: []string{"openv-connector://start?org=org-1"}},
		{name: "pair_missing_code", args: []string{"openv-connector://pair"}},
		{name: "pair_cleartext_declined", args: []string{"openv-connector://pair?code=c0de&api=http://openv.example.test"}},
		{name: "open_unpaired_no_code", args: []string{"openv-connector://open?org=org-1"}},
		{name: "unknown_action", args: []string{"openv-connector://bogus"}},
		{name: "paired_start", setup: cliPaired},
		{name: "paired_start_other_org", args: []string{"openv-connector://start?org=org-2"}, setup: cliPaired},
		{name: "paired_unknown_action", args: []string{"openv-connector://bogus"}, setup: cliPaired},
		{name: "legacy_config_start", setup: cliLegacy},
	})
}

// cliPaired stores one pairing, for org-1, where os.UserConfigDir points on
// Linux and on macOS.
func cliPaired(t *testing.T, home, config string) {
	cliState(t, home, config, `{"active_org":"org-1","pairings":{"org-1":{"api_url":"http://127.0.0.1:9",`+
		`"worker_key":"wk-example","org_id":"org-1","org_name":"Example Workspace","user_name":"Ada"}}}`)
}

// cliLegacy stores a pairing in the flat format of older connectors, which
// loading migrates.
func cliLegacy(t *testing.T, home, config string) {
	cliState(t, home, config, `{"api_url":"http://127.0.0.1:9","worker_key":"wk-example","org_id":"org-1",`+
		`"org_name":"Legacy Workspace","user_name":"Ada"}`)
}

func cliState(t *testing.T, home, config, state string) {
	for _, dir := range []string{config, filepath.Join(home, "Library", "Application Support")} {
		path := filepath.Join(dir, "OpenV", "connector.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(state), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
