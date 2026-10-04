package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
)

// Run failure classes (refactor plan step S15a; OpenV REQ-84, run failure
// classification). testdata/run_failures/outcomes.txt records what the
// runner reports for every terminal outcome of a claimed run: each run is
// claimed from a scripted API by the real Worker.Run, and the golden holds
// the requests it then sent (the exact bytes of its finish or release body)
// and the class that reached the API, with whether the platform retries it.
// testdata/run_failures/classes.txt records the taxonomy behind them:
// every finish site's class and every signal in a CLI's failure text that
// decides one. Refactor step M15a splits Worker.execute into preflight,
// runEnv, buildRunSpec, finishRequestFor and failRun with "messages and
// error classes unchanged": both files staying byte-identical is that proof.
// Neither golden changes in a refactor; a deliberate change regenerates them
// with UPDATE_GOLDEN=1 (the command a failure prints) in a release-noted
// pull request.

const runFailuresDir = "testdata/run_failures"

// failureScenario is one claimed run and how its world behaves. The run's
// id is the scenario's id, so every request the runner sends for it names
// it.
type failureScenario struct {
	id    string
	given string // what the golden says the scenario sets up
	// agent is the claim's agent object as JSON ("null" for none), auth its
	// auth object (empty for user-account auth), project the run's project
	// id (empty for none).
	agent   string
	auth    string
	project string
	// start is the adapter's behaviour; nil when the run must not reach it.
	start func(ctx context.Context, spec RunSpec) (RunHandle, error)
	// The API's answers that differ from defaultAPIAnswer.
	startStatus int
	startBody   string
	repoStatus  int
	cancel      bool // the log push answers cancel_requested
	// prepFile puts a file where the run's workspace directory goes.
	prepFile bool
	// shutdown: the run is still going when the worker shuts down.
	shutdown bool
}

// agentJSON builds a claim's agent object: provider, name, slug, allowed
// tools and repository access as given, the rest fixed. tools nil leaves
// allowed_tools null. The fixed fields each hold a value of their own (and
// the run's prompt another), so a spec that takes one field for another
// shows in the golden.
func agentJSON(provider, name, slug string, tools []string, repoAccess bool) string {
	raw, err := json.Marshal(map[string]interface{}{
		"id": "agent-1", "org_id": "org-1", "provider": provider, "name": name, "slug": slug,
		"allowed_tools": tools, "repo_access": repoAccess, "model": "model-x", "effort": "high",
		"max_turns": 50, "timeout_seconds": 600, "system_prompt": "Be precise.",
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

var fieldTools = []string{"mcp__openv__*", "Read"}

// The scenarios: every terminal outcome execute and its callers report.
func failureScenarios() []failureScenario {
	claude := agentJSON("claude-code", "Field Agent", "field-agent", fieldTools, false)
	codex := agentJSON("codex-cli", "Field Agent", "field-agent", fieldTools, false)
	ok := func(context.Context, RunSpec) (RunHandle, error) {
		return finishedRun(Result{ExitCode: 0, FinalText: "Done."}, nil), nil
	}
	waitFails := func(code int, err error) func(context.Context, RunSpec) (RunHandle, error) {
		return func(context.Context, RunSpec) (RunHandle, error) {
			return finishedRun(Result{ExitCode: code}, err), nil
		}
	}
	cost := 0.0123
	return []failureScenario{
		// Refused before a workspace is made.
		{id: "no-adapter", given: "the agent's provider, gemini-cli, has no adapter on this runner",
			agent: agentJSON("gemini-cli", "Field Agent", "field-agent", fieldTools, false)},
		{id: "no-adapter-blank-provider", given: "the agent names no provider",
			agent: agentJSON("", "Field Agent", "field-agent", fieldTools, false)},
		{id: "no-tools", given: "the agent's allowed_tools is empty",
			agent: agentJSON("claude-code", "Field Agent", "field-agent", []string{}, false)},
		{id: "no-tools-blank-entries", given: "the agent's allowed_tools holds only blanks, and the agent has a slug and no name",
			agent: agentJSON("claude-code", "", "field-agent", []string{"", "  "}, false)},
		{id: "no-tools-unnamed", given: "the agent's allowed_tools is null, and the agent has neither name nor slug",
			agent: agentJSON("claude-code", "", "", nil, false)},
		{id: "repo-access-unsupported", given: "a codex-cli agent with repository access",
			agent: agentJSON("codex-cli", "Field Agent", "field-agent", fieldTools, true)},
		{id: "null-agent", given: "the claim's agent is null (the worker panics reading it)", agent: "null"},
		// The workspace and the start transition.
		{id: "workspace-prep-fails", given: "a file stands where the run's workspace directory goes",
			agent: claude, prepFile: true},
		{id: "repo-connections-unreadable", given: "a claude-code agent with repository access whose project's connections answer 403",
			agent: agentJSON("claude-code", "Field Agent", "field-agent", fieldTools, true), project: "p-repo-connections-unreadable",
			repoStatus: http.StatusForbidden},
		{id: "start-transition-refused", given: "the API answers the start transition 409",
			agent: claude, startStatus: http.StatusConflict, startBody: `{"error":"run is not claimed by this worker","code":"conflict"}`},
		// The project's API-key auth.
		{id: "api-key-variable-not-allowed", given: "api-key auth naming WORKER_API_KEY, which is not a provider key variable",
			agent: claude, auth: `{"mode":"api-key","api_key_env":"WORKER_API_KEY"}`},
		{id: "api-key-missing", given: "api-key auth with the provider's default variable, ANTHROPIC_API_KEY, unset on the runner host",
			agent: claude, auth: `{"mode":"api-key"}`},
		{id: "api-key-missing-named", given: "api-key auth naming GOOGLE_API_KEY, unset on the runner host",
			agent: claude, auth: `{"mode":"api-key","api_key_env":"GOOGLE_API_KEY"}`},
		{id: "api-key-present", given: "a codex-cli agent on api-key auth naming GEMINI_API_KEY, which is set on the runner host",
			agent: codex, auth: `{"mode":"api-key","api_key_env":"GEMINI_API_KEY"}`, start: ok},
		// The adapter.
		{id: "adapter-start-fails", given: "the adapter cannot launch its CLI",
			agent: claude, start: func(context.Context, RunSpec) (RunHandle, error) {
				return nil, errorString(`exec: "claude": executable file not found in $PATH`)
			}},
		{id: "adapter-refuses-definition", given: "the adapter refuses the definition (an ErrAgentPolicy error)",
			agent: claude, start: func(context.Context, RunSpec) (RunHandle, error) {
				return nil, agentPolicyError(agents.AllowedToolsRequired)
			}},
		{id: "adapter-panics", given: "the adapter panics in Start",
			agent: claude, start: func(context.Context, RunSpec) (RunHandle, error) {
				panic("stand-in adapter panicked in Start")
			}},
		// What the CLI's run ended with.
		{id: "succeeded", given: "the CLI exits 0 with an answer, tokens and a cost",
			agent: claude, start: func(context.Context, RunSpec) (RunHandle, error) {
				return finishedRun(Result{ExitCode: 0, FinalText: "Done.", TokensIn: 1200, TokensOut: 340, CostUSD: &cost}, nil), nil
			}},
		{id: "succeeded-nonzero-exit", given: "the run ends with exit code 2 and no error",
			agent: claude, start: waitFails(2, nil)},
		{id: "agent-fails", given: "the CLI fails with text naming no auth or provider signal",
			agent: claude, start: waitFails(1, errorString("exit status 1: tool Bash failed: no such file or directory"))},
		{id: "agent-fails-auth", given: "the CLI fails with text carrying an auth signal",
			agent: claude, start: waitFails(1, errorString("exit status 1: Invalid API key · Please run /login"))},
		{id: "agent-fails-provider", given: "the CLI fails with text carrying a provider signal",
			agent: claude, start: waitFails(1, errorString(`exit status 1: API Error: 529 {"type":"overloaded_error"}`))},
		{id: "agent-fails-auth-over-provider", given: "the CLI fails with text carrying both an auth and a provider signal",
			agent: claude, start: waitFails(1, errorString("exit status 1: 503 Service Unavailable: not logged in"))},
		{id: "timeout-with-detail", given: "the adapter's watchdog fires with the parser's error",
			agent: claude, start: waitFails(-1, &timeoutError{detail: errorString("stuck waiting on tool result")})},
		{id: "timeout-without-detail", given: "the adapter's watchdog fires before the parser has an error",
			agent: claude, start: waitFails(-1, &timeoutError{})},
		{id: "deadline-exceeded", given: "the run ends with context.DeadlineExceeded itself",
			agent: claude, start: waitFails(-1, context.DeadlineExceeded)},
		{id: "wait-panics", given: "the CLI handle panics in Wait",
			agent: claude, start: func(ctx context.Context, _ RunSpec) (RunHandle, error) {
				h := newHeldRun(ctx)
				h.end(Result{}, nil, true)
				return h, nil
			}},
		{id: "cancelled-by-member", given: "the log push answers cancel_requested while the CLI runs",
			agent: claude, cancel: true, start: func(ctx context.Context, _ RunSpec) (RunHandle, error) {
				return newHeldRun(ctx), nil
			}},
		{id: "worker-shutdown", given: "the worker shuts down while the CLI runs",
			agent: claude, shutdown: true, start: func(ctx context.Context, _ RunSpec) (RunHandle, error) {
				return newHeldRun(ctx), nil
			}},
	}
}

// failureClaim is the claim the API hands the worker for a scenario.
func failureClaim(sc failureScenario) string {
	project := ""
	if sc.project != "" {
		project = `,"project_id":"` + sc.project + `"`
	}
	auth := `{"mode":"user-account"}`
	if sc.auth != "" {
		auth = sc.auth
	}
	return `{"run":{"id":"` + sc.id + `","org_id":"org-1","agent_id":"agent-1"` + project +
		`,"status":"claimed","priority":0,"prompt":"Summarise the open risks."},"agent":` + sc.agent +
		`,"run_token":"rt-` + sc.id + `","auth":` + auth + `}`
}

// TestRunFailureClassesGolden claims every scenario at once (one claim tick
// of a worker with a slot each) and records what the runner reported.
func TestRunFailureClassesGolden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the workspace outcome quotes the operating system's mkdir error and path")
	}
	scenarios := failureScenarios()
	byID := map[string]failureScenario{}
	for _, sc := range scenarios {
		if _, dup := byID[sc.id]; dup {
			t.Fatalf("two scenarios are named %q", sc.id)
		}
		byID[sc.id] = sc
	}

	clearProviderKeys(t)
	t.Setenv("GEMINI_API_KEY", "gemini-key-from-the-runner-host")
	workspaces := t.TempDir()
	for _, sc := range scenarios {
		if sc.prepFile {
			if err := os.WriteFile(filepath.Join(workspaces, sc.id), []byte("not a directory\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	queue := make(chan string, len(scenarios))
	for _, sc := range scenarios {
		queue <- failureClaim(sc)
	}
	api := newFakeAPI(t, func(c apiCall) (int, string) {
		sc, known := byID[runIDOf(c)]
		switch {
		case isClaim(c):
			select {
			case claim := <-queue:
				return http.StatusOK, claim
			default:
				return http.StatusNoContent, ""
			}
		case known && c.is("POST", "/start") && sc.startStatus != 0:
			return sc.startStatus, sc.startBody
		case known && c.is("POST", "/logs") && sc.cancel:
			return http.StatusOK, `{"cancel_requested":true,"status":"running"}`
		case c.is("GET", "/repo-connections"):
			for _, s := range scenarios {
				if s.project != "" && strings.Contains(c.Path, "/"+s.project+"/") && s.repoStatus != 0 {
					return s.repoStatus, `{"error":"forbidden","code":"forbidden"}`
				}
			}
		}
		return 0, ""
	})

	started := newStartLog()
	adapter := &scriptedAdapter{installed: true, start: func(ctx context.Context, spec RunSpec) (RunHandle, error) {
		started.add(spec)
		sc, ok := byID[spec.RunID]
		if !ok || sc.start == nil {
			t.Errorf("run %s reached the adapter, which its scenario does not expect", spec.RunID)
			return nil, errorString("unexpected run")
		}
		return sc.start(ctx, spec)
	}}
	w := NewWorker(NewClient(api.URL(), "worker-key"), Options{
		WorkerID:      "w-failures",
		Concurrency:   len(scenarios),
		WorkspaceBase: workspaces,
		MCPBinary:     "/opt/openv/bin/openv-mcp",
		APIURL:        "https://openv.example",
		Hosted:        true, // no sign-in loop: this test is about runs
	})
	w.adapters = map[string]Adapter{"claude-code": adapter, "codex-cli": adapter}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- w.Run(ctx) }()

	// Every run but the shutdown one reports a terminal state on its own.
	api.await(t, 30*time.Second, "a terminal report from every scenario that ends on its own", func(calls []apiCall) bool {
		for _, sc := range scenarios {
			if sc.shutdown {
				if !started.has(sc.id) {
					return false
				}
				continue
			}
			if count(calls, func(c apiCall) bool { return runIDOf(c) == sc.id && c.is("POST", "/finish") }) == 0 {
				return false
			}
		}
		return true
	})
	cancel()
	select {
	case err := <-ran:
		if err != context.Canceled {
			t.Errorf("Worker.Run returned %v on shutdown, want context.Canceled", err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("Worker.Run did not return after its context ended")
	}

	calls := api.Calls()
	var b strings.Builder
	b.WriteString(outcomesHeader)
	seen := map[string]bool{}
	for _, sc := range scenarios {
		b.WriteString("\n## " + sc.id + "\n")
		b.WriteString("given: " + sc.given + "\n")
		if spec, ok := started.get(sc.id); ok {
			b.WriteString("adapter called with env: " + specEnv(spec) + "\n")
			b.WriteString("adapter called with spec: " + specFields(spec, workspaces) + "\n")
		}
		class := ""
		for _, c := range calls {
			if !scenarioCall(sc, c) {
				continue
			}
			line := c.Method + " " + c.Path
			if c.Auth != "worker-key" {
				line += " (as " + c.Auth + ")"
			}
			if c.is("POST", "/finish") || c.is("POST", "/release") {
				line += " " + strings.ReplaceAll(string(c.Body), workspaces, "<workspaces>")
			}
			b.WriteString(line + "\n")
			if c.is("POST", "/finish") {
				var req agentruns.FinishRequest
				c.json(t, &req)
				class = req.ErrorClass
			}
		}
		switch {
		case class == "":
			b.WriteString("class: none\n")
		case agentruns.IsRetryableClass(class):
			b.WriteString("class: " + class + " (retried)\n")
		default:
			b.WriteString("class: " + class + " (not retried)\n")
		}
		seen[class] = true
	}
	checkGolden(t, filepath.Join(runFailuresDir, "outcomes.txt"), []byte(b.String()), "TestRunFailureClassesGolden")

	// REQ-84 names six classes; every one of them is reported by some outcome.
	for _, class := range []string{agentruns.ErrorClassProviderUnavailable, agentruns.ErrorClassAuth,
		agentruns.ErrorClassWorkspace, agentruns.ErrorClassTimeout, agentruns.ErrorClassAgentError,
		agentruns.ErrorClassWorkerError} {
		if !seen[class] {
			t.Errorf("no scenario reports the error class %q: add the outcome that does", class)
		}
	}
}

const outcomesHeader = `# What the runner reports for each terminal outcome of a claimed run
# (refactor plan step S15a; OpenV REQ-84). Written by TestRunFailureClassesGolden
# in internal/runner, which has the real Worker.Run claim every scenario from a
# stand-in API and records, per run, the requests it sent (log pushes left out),
# each finish or release body byte for byte, and the error class that reached
# the API with whether the platform retries it (agentruns.IsRetryableClass).
# Requests go as the worker key unless marked "(as <credential>)"; "adapter
# called with env" is the environment the worker handed the provider adapter,
# and "adapter called with spec" the rest of what it handed it: answer_rule
# says the system prompt is the agent's own followed by agentruns'
# AnswerLengthRule, and mcp_env=env that the MCP server gets that environment.
# Not covered: a cancel noticed during workspace preparation, which the
# 30-second preparation heartbeat (prepHeartbeatInterval) reports. A refactor
# leaves this file byte-identical; a deliberate change regenerates it with
#   UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run TestRunFailureClassesGolden
`

// scenarioCall reports whether a request belongs to a scenario's run: its
// lifecycle calls (log pushes left out, their number depends on timing) and
// the read of its project's repository connections.
func scenarioCall(sc failureScenario, c apiCall) bool {
	if runIDOf(c) == sc.id {
		return !c.is("POST", "/logs")
	}
	return sc.project != "" && c.is("GET", "/projects/"+sc.project+"/repo-connections")
}

// specEnv renders the environment the worker handed the adapter, sorted.
func specEnv(spec RunSpec) string {
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+spec.Env[k])
	}
	return strings.Join(parts, " ")
}

// specFields renders the rest of the RunSpec the worker handed the adapter:
// its run id and environment are pinned elsewhere (the scenario a spec is
// filed under, and specEnv), the AnswerLengthRule the worker appends to the
// agent's system prompt is named rather than quoted, and the workspace base
// reads <workspaces>.
func specFields(spec RunSpec, workspaces string) string {
	system, rule := spec.SystemPrompt, false
	switch {
	case strings.HasSuffix(system, agentruns.AnswerLengthRule):
		system, rule = strings.TrimSuffix(system, agentruns.AnswerLengthRule), true
	case system == strings.TrimSpace(agentruns.AnswerLengthRule):
		system, rule = "", true
	}
	mcpEnv := "env"
	if !reflect.DeepEqual(spec.MCP.Env, spec.Env) {
		mcpEnv = fmt.Sprintf("%q", spec.MCP.Env)
	}
	tools, _ := json.Marshal(spec.AllowedTools)
	return fmt.Sprintf("workdir=%s prompt=%q system_prompt=%q answer_rule=%v model=%q effort=%q mcp=%s mcp_env=%s "+
		"allowed_tools=%s untrusted=%v repo_access=%v max_turns=%d timeout=%d",
		strings.ReplaceAll(spec.WorkDir, workspaces, "<workspaces>"), spec.Prompt, system, rule, spec.Model,
		spec.Effort, spec.MCP.Command, mcpEnv, tools, spec.Untrusted, spec.RepoAccess, spec.MaxTurns, spec.TimeoutSec)
}

// startLog keeps the RunSpec each run reached the adapter with.
type startLog struct {
	mu    sync.Mutex
	specs map[string]RunSpec
}

func newStartLog() *startLog { return &startLog{specs: map[string]RunSpec{}} }

func (l *startLog) add(spec RunSpec) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.specs[spec.RunID] = spec
}

func (l *startLog) get(id string) (RunSpec, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	spec, ok := l.specs[id]
	return spec, ok
}

func (l *startLog) has(id string) bool {
	_, ok := l.get(id)
	return ok
}

// TestRunFailureTaxonomyGolden pins the taxonomy behind the outcomes: the
// class each finish site maps to, and each word or status code in a CLI's
// failure text that makes a failed run auth or provider_unavailable
// (anything else is agent_error), with the precedence between them.
func TestRunFailureTaxonomyGolden(t *testing.T) {
	sites := []struct {
		name string
		site finishSite
		err  error
	}{
		{"siteNoAdapter", siteNoAdapter, nil},
		{"siteWorkspacePrep", siteWorkspacePrep, nil},
		{"siteStartTransition", siteStartTransition, nil},
		{"siteAPIKeyMissing", siteAPIKeyMissing, nil},
		{"siteAdapterStart", siteAdapterStart, errorString("exec: not found")},
		{"siteAdapterStart", siteAdapterStart, agentPolicyError("refused")},
		{"siteAdapterStart", siteAdapterStart, fmt.Errorf("wrapped: %w", agentPolicyError("refused"))},
		{"siteAgentResult", siteAgentResult, nil},
		{"siteAgentResult", siteAgentResult, errorString("exit status 1")},
		{"siteAgentExit", siteAgentExit, nil},
		{"siteTimeout", siteTimeout, context.DeadlineExceeded},
		{"sitePanic", sitePanic, nil},
		{"siteAgentPolicy", siteAgentPolicy, nil},
		{"(a site with no case)", finishSite(-1), nil},
	}

	// Every finishSite constant in classify.go has a row above.
	declared := finishSiteNames(t)
	covered := map[string]bool{}
	for _, s := range sites {
		covered[s.name] = true
	}
	for _, name := range declared {
		if !covered[name] {
			t.Errorf("finish site %s has no row in TestRunFailureTaxonomyGolden: add one", name)
		}
	}

	var b strings.Builder
	b.WriteString(taxonomyHeader)
	b.WriteString("\n## finish sites (classifySite)\n")
	for _, s := range sites {
		with := "no error"
		if s.err != nil {
			with = fmt.Sprintf("%q", s.err.Error())
			if errors.Is(s.err, ErrAgentPolicy) {
				with += " (ErrAgentPolicy)"
			}
		}
		b.WriteString(fmt.Sprintf("%s, %s -> %s\n", s.name, with, quoteClass(classifySite(s.site, s.err))))
	}

	// The signal lists sorted: their order inside a list decides nothing.
	// Each signal is classified in a CLI's failure text, a status code as the
	// whole text (anywhere else it needs an HTTP word beside it).
	b.WriteString("\n## auth signals, checked first (authSignals)\n")
	for _, s := range sortedSignals(authSignals) {
		b.WriteString(fmt.Sprintf("%q -> %s\n", s, classifyAgentError(errorString(signalProbe(s)))))
	}
	b.WriteString("\n## provider signals, checked second (providerSignals)\n")
	for _, s := range sortedSignals(providerSignals) {
		b.WriteString(fmt.Sprintf("%q -> %s\n", s, classifyAgentError(errorString(signalProbe(s)))))
	}
	// Each after "exit status 1: " and before "; check your API key", a
	// status code after "HTTP".
	b.WriteString("\n## rate-limit and overload signals, outranking a mention of an API key (throttleSignals)\n")
	for _, s := range sortedSignals(throttleSignals) {
		probe := s
		if isStatusCode(s) {
			probe = "HTTP " + s
		}
		b.WriteString(fmt.Sprintf("%q -> %s\n", s, classifyAgentError(errorString("exit status 1: "+probe+"; check your API key"))))
	}

	b.WriteString("\n## failure texts (classifyAgentError: lower-cased, then a whole-word match)\n")
	for _, text := range []string{
		"",
		"exit status 1",
		"Error: 401 Unauthorized",
		"UNAUTHORIZED",
		"Please Log In to continue",
		"API Error: 429 Too Many Requests",
		"upstream connect error: Connection Reset by peer",
		"503 Service Unavailable: not logged in",
		"rate limit exceeded; check your API key",
		"tool Read failed: wrote 403 lines",
		"processed 1500 files before the tool failed",
		"the network tool is disabled for this agent",
		"Credentials file is missing a field the tool expects",
		"panic: index out of range",
	} {
		var err error
		if text != "" {
			err = errorString(text)
		}
		b.WriteString(fmt.Sprintf("%q -> %s\n", text, quoteClass(classifyAgentError(err))))
	}

	b.WriteString("\n## classes the platform retries (agentruns.IsRetryableClass)\n")
	for _, class := range []string{agentruns.ErrorClassProviderUnavailable, agentruns.ErrorClassAuth,
		agentruns.ErrorClassWorkspace, agentruns.ErrorClassTimeout, agentruns.ErrorClassAgentError,
		agentruns.ErrorClassWorkerError, ""} {
		b.WriteString(fmt.Sprintf("%s -> %v\n", quoteClass(class), agentruns.IsRetryableClass(class)))
	}
	checkGolden(t, filepath.Join(runFailuresDir, "classes.txt"), []byte(b.String()), "TestRunFailureTaxonomyGolden")
}

const taxonomyHeader = `# The run failure taxonomy (refactor plan step S15a; OpenV REQ-84): the class
# each runner finish site reports, the signals in a CLI's failure text that
# decide a failed run's class, and which classes the platform retries. Written
# by TestRunFailureTaxonomyGolden in internal/runner; the outcomes these
# produce are in outcomes.txt beside it. A refactor leaves this file
# byte-identical; a deliberate change regenerates it with
#   UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run TestRunFailureTaxonomyGolden
`

// signalProbe is the failure text a signal is classified in: a status code
// as the whole text, any other signal after "exit status 1: ".
func signalProbe(signal string) string {
	if isStatusCode(signal) {
		return signal
	}
	return "exit status 1: " + signal
}

func sortedSignals(signals []string) []string {
	out := append([]string(nil), signals...)
	sort.Strings(out)
	return out
}

func quoteClass(class string) string {
	if class == "" {
		return `""`
	}
	return class
}

// finishSiteNames lists the constants of type finishSite the package
// declares (classify.go today), so a new site cannot go without a row.
func finishSiteNames(t *testing.T) []string {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the runner package: %v", err)
	}
	var names []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				typed := false // a spec with no type repeats the one above it
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if vs.Type != nil {
						id, ok := vs.Type.(*ast.Ident)
						typed = ok && id.Name == "finishSite"
					} else if len(vs.Values) > 0 {
						typed = false
					}
					if typed {
						for _, n := range vs.Names {
							names = append(names, n.Name)
						}
					}
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("found no finishSite constants in the runner package")
	}
	sort.Strings(names)
	return names
}
