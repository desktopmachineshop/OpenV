package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

// workerBuiltSpec produces a RunSpec the way a real run does: an agent
// definition that has been through Validate (which is what every persisted
// agent has been through — the API handlers on save, parseSyncedFile on sync),
// handed to the worker, whose spec the fake adapter captures.
//
// Going through the worker rather than writing a RunSpec by hand is the whole
// point. A hand-built spec is free to leave MaxTurns at 0, and that is exactly
// how the codex/gemini adapters came to be untestably broken: they refused any
// spec with MaxTurns > 0, Validate coerces every definition to 50, and the
// adapter tests never noticed because none of them built a spec the way a run
// does.
func workerBuiltSpec(t *testing.T, def agents.Definition) RunSpec {
	t.Helper()
	if err := def.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if def.MaxTurns <= 0 {
		t.Fatalf("Validate left MaxTurns at %d; this test exists because it does not", def.MaxTurns)
	}

	rs := newRecordingServer()
	defer rs.srv.Close()

	var got RunSpec
	h := &fakeHandle{events: make(chan RunEvent), waitCh: make(chan struct{})}
	close(h.events)
	close(h.waitCh)
	adapter := &fakeAdapter{start: func(_ context.Context, spec RunSpec) (RunHandle, error) {
		got = spec
		return h, nil
	}}
	w := newTestWorker(rs, adapter)
	w.workspaceBase = t.TempDir()

	claim := testClaim()
	claim.Agent = &agents.Agent{
		Slug:           def.Slug,
		Name:           def.Name,
		Provider:       "fake",
		Model:          def.Model,
		Effort:         def.Effort,
		AllowedTools:   def.AllowedTools,
		WriteMode:      def.WriteMode,
		RepoAccess:     def.RepoAccess,
		MaxTurns:       def.MaxTurns,
		TimeoutSeconds: def.TimeoutSeconds,
		SystemPrompt:   def.SystemPrompt,
	}
	w.execute(context.Background(), claim)
	if got.RunID == "" {
		t.Fatal("the worker never reached the adapter")
	}
	return got
}

// stubCLIsOnPath puts no-op executables named after the vendor CLIs at the
// front of PATH, so an adapter's Start really launches a process instead of
// failing on a missing binary.
func stubCLIsOnPath(t *testing.T, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stubs are POSIX-only")
	}
	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// REQ-91 regression: every provider must actually start a run for an agent
// built the way the platform builds them. MaxTurns is a documented no-op on
// codex and gemini, not a refusal — before this, a definition's MaxTurns of 50
// failed the run at Start, so no persisted codex or gemini agent could run at
// all and none of the sandbox / OPENV_MCP_TOOLS work below it was ever
// reached.
func TestWorkerBuiltSpecStartsOnEveryProvider(t *testing.T) {
	spec := workerBuiltSpec(t, agents.Definition{
		Slug:         "field-agent",
		Name:         "Field Agent",
		Provider:     "codex-cli",
		AllowedTools: []string{"mcp__openv__*", "Read", "Bash(git *)"},
	})
	if spec.MaxTurns != 50 {
		t.Fatalf("RunSpec.MaxTurns = %d, want the 50 Validate defaults to", spec.MaxTurns)
	}

	stubCLIsOnPath(t, "codex", "gemini")
	for _, adapter := range []Adapter{&CodexCLIAdapter{}, &GeminiCLIAdapter{}} {
		t.Run(adapter.Name(), func(t *testing.T) {
			runSpec := spec
			runSpec.WorkDir = t.TempDir()
			handle, err := adapter.Start(context.Background(), runSpec)
			if err != nil {
				t.Fatalf("Start refused a spec the worker built: %v", err)
			}
			handle.Cancel()
			_, _ = handle.Wait()
		})
	}
}

// An agent with repository access is refused on codex and gemini — neither CLI
// can confine edits per tool, so it would run either unconfined or (being
// untrusted, because a clone's files are content nobody in the workspace
// wrote) unable to edit anything, which silently defeats the agent. The
// refusal is agent policy, so the run is not auto-retried. claude-code, whose
// allowlist names the editing tools one at a time, is unaffected.
func TestRepoAccessRefusedOnCLIsThatCannotConfineEdits(t *testing.T) {
	spec := workerBuiltSpec(t, agents.Definition{
		Slug:         "developer",
		Name:         "Developer",
		Provider:     "codex-cli",
		RepoAccess:   true,
		AllowedTools: []string{"mcp__openv__*", "Read", "Edit", "Write", "Bash(git *)"},
	})
	if !spec.RepoAccess {
		t.Fatal("the worker did not carry the definition's repo access into the spec")
	}
	if !spec.Untrusted {
		t.Fatal("a repo-access agent must still be marked untrusted")
	}

	stubCLIsOnPath(t, "codex", "gemini", "claude")
	for _, tc := range []struct {
		adapter Adapter
		refused bool
	}{
		{&CodexCLIAdapter{}, true},
		{&GeminiCLIAdapter{}, true},
		{&ClaudeCodeAdapter{}, false},
	} {
		t.Run(tc.adapter.Name(), func(t *testing.T) {
			runSpec := spec
			runSpec.WorkDir = t.TempDir()
			handle, err := tc.adapter.Start(context.Background(), runSpec)
			if handle != nil {
				handle.Cancel()
				_, _ = handle.Wait()
			}
			if !tc.refused {
				if err != nil {
					t.Fatalf("%s must still run a repo-access agent: %v", tc.adapter.Name(), err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s started a repo-access agent it cannot confine", tc.adapter.Name())
			}
			if !errors.Is(err, ErrAgentPolicy) {
				t.Errorf("refusal must be agent policy (never auto-retried): %v", err)
			}
			if class := classifySite(siteAdapterStart, err); class != "agent_error" {
				t.Errorf("error class = %q, want agent_error so the run is not retried", class)
			}
		})
	}
}
