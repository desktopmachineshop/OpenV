package archtest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestEnvInventory is the env var inventory of refactor plan S8 (invariants
// I13 and K8): every environment variable the Go code under cmd/ and
// internal/ reads, with the getter it goes through and its default, frozen
// in testdata/env_vars.txt beside the exemptions below. It fails on a read
// whose name does not resolve and no exemption covers, on an env reader
// used as a value, on a stale exemption, and on a read in a file the typed
// build leaves out. It also checks testdata/env_parse.txt (see
// env_parse_check_test.go) and writes its standard-library and comparison
// sections. Where each read sits is a report, not a golden: go test -v logs
// it, and ENV_INVENTORY_REPORT=<file> writes it there. It is not a rule of
// TestArchitecture and never reads UPDATE_RATCHETS.
func TestEnvInventory(t *testing.T) {
	m, err := loadModule()
	if err != nil {
		t.Fatal(err)
	}
	prog, err := loadEnvProgram(m.root, m.path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := newEnvScan(prog).run()
	if err != nil {
		t.Fatal(err)
	}
	bad := res.judge(prog.fset, envExemptions)
	bad = append(bad, envHoles(m, prog.typed, res.getters)...)
	report := envReport(prog, res)
	if testing.Verbose() {
		t.Log(report)
	}
	if p := os.Getenv("ENV_INVENTORY_REPORT"); p != "" {
		if err := os.WriteFile(p, []byte(report), 0o644); err != nil {
			t.Errorf("write the report: %v", err)
		}
	}
	if len(bad) > 0 {
		t.Fatal(envFailure("env var inventory", bad, envInventoryFix, envInventoryRegenerate))
	}
	envCheckGolden(t, envVarsGolden, []byte(renderEnvVars(res, envExemptions)), envInventoryRegenerate)
	checkEnvParseTable(t, m, res)
}

const (
	envUpdateEnv    = "UPDATE_GOLDEN"
	envVarsGolden   = "testdata/env_vars.txt"
	envParseGolden  = "testdata/env_parse.txt"
	envReadmeAnchor = readmeURL + "#env-var-inventory-s8"

	envInventoryRegenerate = envUpdateEnv + "=1 go test ./internal/archtest -count=1 -run '^TestEnvInventory$'"
	envParseRegenerate     = envUpdateEnv + "=1 go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest" +
		" ./cmd/agentd ./cmd/openv-mcp ./cmd/server ./internal/api ./internal/billing ./internal/domain/users ./internal/hosting ./internal/notify"
	envOnlyOne = "(only " + envUpdateEnv + "=1 regenerates; any other value compares)"

	envInventoryFix = "Fix: name the variable with a constant or a constant expression where it is read, or pass the name" +
		" through a getter whose parameter each call fills with one. A read no constant can name, or one that must stay" +
		" where it is when X10 moves configuration to internal/config, is an exemption in envExemptions" +
		" (internal/archtest/env_inventory_test.go) with its reason, argued in review."
)

// envUpdating reports whether goldens are rewritten: only UPDATE_GOLDEN=1.
func envUpdating() bool { return os.Getenv(envUpdateEnv) == "1" }

// envExemptions are the reads the inventory accepts without a name, and the
// named reads K8 keeps where they are. Sorted by id.
var envExemptions = []envExemption{
	{id: "connector-agentd-environ", kind: "environ", at: []string{"cmd/openv-connector:start"},
		reason: "agentd, which the connector starts, inherits the connector's environment plus WORKER_API_KEY, which goes through the environment rather than argv so that other processes cannot read it"},
	{id: "mcp-tool-allowlist", kind: "placement", names: []string{"OPENV_MCP_TOOLS"}, at: []string{"internal/mcp:EnvFilteredTools"},
		reason: "a set but empty value means no tools, which only os.LookupEnv tells apart; openv-mcp applies it to its tool table"},
	{id: "per-request", kind: "placement",
		names: []string{"OPENV_CLIENT_IP_HEADER", "OPENV_MAX_EVIDENCE_MB", "OPENV_MAX_UPLOAD_MB", "OPENV_PUSH_ENDPOINT_HOSTS",
			"OPENV_TRUSTED_PROXY_HOPS", "OPENV_TRUST_PROXY", "UPLOADS_DIR"},
		at: []string{"internal/api:proxyTrustFromEnv", "internal/api:envUploadMB", "internal/api:maxEvidenceBytes",
			"internal/api:pushEndpointHostPatterns", "internal/domain/reports:resolveAttachmentPath"},
		reason: "read on every request (the client address, an upload or evidence size cap, a push endpoint, a report's attachment path), so a changed value applies without a restart"},
	{id: "runner-child-environ", kind: "environ",
		at:     []string{"internal/runner:childEnv"},
		reason: "every process the runner starts (a provider CLI's run, sign-in or version probe, and git) inherits the runner's environment, less the OpenV credentials its host may hold (the runner's own WORKER_API_KEY and RUNNER_POOL_KEY, and OPENV_API_TOKEN, OPENV_EMAIL and OPENV_PASSWORD), with a run's variables layered on top, which is how a provider CLI finds its own settings"},
	{id: "runner-gemini-auth-probe", kind: "placement",
		names: []string{"CLOUD_SHELL", "GEMINI_API_KEY", "GEMINI_CLI_USE_COMPUTE_ADC", "GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL",
			"GOOGLE_GENAI_USE_GCA", "GOOGLE_GENAI_USE_VERTEXAI"},
		at:     []string{"internal/runner:geminiAuthNamed"},
		reason: "per run: before adding GOOGLE_GENAI_USE_GCA, the runner checks each Gemini auth-mode variable the run's own environment leaves unset in the process environment the CLI will inherit"},
	{id: "runner-probe", kind: "placement",
		names: []string{"ANTHROPIC_API_KEY", "CODEX_HOME", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"},
		at: []string{"internal/runner:ClaudeCodeAdapter.Detect", "internal/runner:CodexCLIAdapter.Detect", "internal/runner:GeminiCLIAdapter.Detect",
			"internal/runner:AntigravityAdapter.Detect", "internal/runner:codexAuthPath"},
		reason: "a provider's detection probes, run at registration and on each lease, see the environment the provider CLI will see, HOME included, which a pool lease changes"},
	{id: "runner-provider-api-key", kind: "unresolved", at: []string{"internal/runner:Worker.runEnv"},
		reason: "per run: the claimed run's API-key variable, named by the API or the provider's default and limited by providers.IsAllowedAPIKeyEnv to ANTHROPIC_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY and OPENAI_API_KEY"},
}

const envVarsHeader = `# Environment variables the Go code reads (refactor plan S8, invariants I13
# and K8), found by a go/types scan of the module root and of every package
# under cmd/ and internal/, test files excluded. Written by TestEnvInventory
# (internal/archtest/env_inventory_test.go); regenerate with
#   UPDATE_GOLDEN=1 go test ./internal/archtest -count=1 -run '^TestEnvInventory$'
# (only UPDATE_GOLDEN=1 regenerates; any other value compares).
#
# One row per variable, read and default, sorted, tab-separated:
#   NAME     the variable
#   read     how it is read: os.Getenv or os.LookupEnv directly, or
#            <package>:<func>(<param>) through a getter, a function whose
#            string parameter reaches an env read or whose function-typed
#            parameter a call binds to os.Getenv; with a comparison after it,
#            such as =="true", when the value is only compared with that
#            string
#   default  the getter's fallback where the call passes a constant (text
#            quoted, a duration as time.Duration prints it), (computed) where
#            it passes anything else, - where the read has none
# testdata/env_parse.txt says what each read returns. Where each read sits
# (file:line, function, binaries) is in the report that
# go test -v -run '^TestEnvInventory$' ./internal/archtest logs.
`

const envExemptionsHeader = `# The exemptions: reads no constant names (unresolved, with ? for names;
# environ, the whole environment, *) and named reads that stay where they are
# when X10 moves configuration to internal/config (placement), K8's list.
# exemption	kind	names	reason
`

// renderEnvVars writes env_vars.txt.
func renderEnvVars(res *envResult, exemptions []envExemption) string {
	seen := map[string]bool{}
	var rows []string
	for _, r := range res.rows {
		line := r.name + "\t" + r.read + "\t" + r.def
		if !seen[line] {
			seen[line] = true
			rows = append(rows, line)
		}
	}
	sort.Strings(rows)
	var ex []string
	for _, e := range exemptions {
		names := "?"
		switch e.kind {
		case "environ":
			names = "*"
		case "placement":
			sorted := append([]string{}, e.names...)
			sort.Strings(sorted)
			names = strings.Join(sorted, ",")
		}
		ex = append(ex, e.id+"\t"+e.kind+"\t"+names+"\t"+e.reason)
	}
	sort.Strings(ex)
	return envVarsHeader + strings.Join(rows, "\n") + "\n\n" + envExemptionsHeader + strings.Join(ex, "\n") + "\n"
}

// envReport is the part of the inventory that is not frozen: where each read
// sits, which binaries link it, the getters, the exempt reads.
func envReport(prog *envProgram, res *envResult) string {
	var b strings.Builder
	names := map[string]bool{}
	var lines []string
	for _, r := range res.rows {
		names[r.name] = true
		src := "-"
		if r.at.def != nil {
			src = envSource(prog.fset, r.at.def)
		}
		lines = append(lines, strings.Join([]string{r.name, r.read, r.def, src, r.at.sink, r.at.fn.key,
			envPos(prog.fset, r.at.node), strings.Join(prog.bins[r.at.fn.pkg.name], ",")}, "\t"))
	}
	sort.Strings(lines)
	fmt.Fprintf(&b, "env var inventory report (S8; not frozen): %d reads of %d variables\n", len(lines), len(names))
	b.WriteString("NAME\tread\tdefault\tdefault source\tsink\tfunction\tfile:line\tbinaries\n")
	b.WriteString(strings.Join(lines, "\n") + "\n")
	b.WriteString("getters (function\tdeclared at\treads through):\n")
	for _, g := range res.getters {
		var how []string
		for k, get := range g.gets {
			how = append(how, k+" via "+get.sink)
		}
		for i, form := range g.bound {
			how = append(how, g.params[i].Name()+" bound to "+form)
		}
		sort.Strings(how)
		fmt.Fprintf(&b, "  %s\t%s\t%s\n", g.key, envPos(prog.fset, g.decl), strings.Join(how, "; "))
	}
	b.WriteString("reads with no name (exempt when envExemptions covers the function):\n")
	for _, r := range append(append([]envRead{}, res.unknown...), res.environ...) {
		why := r.name.unknown
		if r.environ {
			why = "the whole environment"
		}
		fmt.Fprintf(&b, "  %s\t%s\t%s\t%s\n", envPos(prog.fset, r.node), r.fn.key, r.read, why)
	}
	return b.String()
}

// envFailure formats one failure that names what failed, how to fix it and
// how to regenerate the golden after a deliberate change.
func envFailure(what string, bad []string, fix, regenerate string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (refactor plan S8) failed: %d problem(s)\n", what, len(bad))
	for _, v := range bad {
		fmt.Fprintf(&b, "  - %s\n", v)
	}
	fmt.Fprintf(&b, "%s\nAfter a deliberate change, regenerate the golden with:\n  %s\n%s\nREADME: %s",
		fix, regenerate, envOnlyOne, envReadmeAnchor)
	return b.String()
}

// envCheckGolden compares got with a golden of this package, or rewrites
// it when UPDATE_GOLDEN is 1.
func envCheckGolden(t *testing.T, path string, got []byte, regenerate string) {
	t.Helper()
	shown := "internal/archtest/" + path
	if envUpdating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s\n%s", shown, err, regenerate, envOnlyOne)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s does not match the code:\n%s"+
			"A refactor never changes a golden: a renamed, dropped or added variable, getter or default is a behavior change."+
			" If it is deliberate, regenerate it with:\n  %s\n%s",
			shown, envLineDiff(string(want), string(got)), regenerate, envOnlyOne)
	}
}

// envLineDiff lists the lines only the golden has (-) and only the code has
// (+), in order, at most 60 of them.
func envLineDiff(want, got string) string {
	count := func(s string) map[string]int {
		c := map[string]int{}
		for _, l := range strings.Split(s, "\n") {
			c[l]++
		}
		return c
	}
	inWant, inGot := count(want), count(got)
	var b strings.Builder
	n := 0
	emit := func(sign string, text string, other map[string]int) {
		for _, l := range strings.Split(text, "\n") {
			if other[l] > 0 {
				other[l]--
				continue
			}
			if n++; n <= 60 {
				fmt.Fprintf(&b, "%s %s\n", sign, l)
			}
		}
	}
	emit("-", want, inGot)
	emit("+", got, inWant)
	if n > 60 {
		fmt.Fprintf(&b, "... and %d more lines\n", n-60)
	}
	if n == 0 {
		b.WriteString("(the same lines in another order)\n")
	}
	return b.String()
}
