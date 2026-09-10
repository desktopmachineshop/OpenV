package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
)

// REQ-91: an agent whose definition carries no allowlist fails its run before
// any CLI is launched — through the normal finish path, with a message naming
// the agent, and without taking the worker down with it.
func TestExecuteRefusesAgentWithNoAllowedTools(t *testing.T) {
	rs := newRecordingServer()
	defer rs.srv.Close()

	launched := false
	adapter := &fakeAdapter{start: func(context.Context, RunSpec) (RunHandle, error) {
		launched = true
		return nil, nil
	}}
	w := newTestWorker(rs, adapter)
	w.workspaceBase = t.TempDir()

	claim := testClaim()
	claim.Agent.Slug = "no-tools"
	claim.Agent.Name = "Toolless Agent"
	claim.Agent.AllowedTools = nil
	w.execute(context.Background(), claim)

	if launched {
		t.Fatal("the vendor CLI was launched for an agent with no allowlist")
	}
	if rs.hit("finish") != 1 {
		t.Fatalf("finish hits = %d, want 1 (the run must terminate through the normal path)", rs.hit("finish"))
	}
	got := rs.finishBody
	if got.Status != agentruns.StatusFailed {
		t.Errorf("status = %q, want %q", got.Status, agentruns.StatusFailed)
	}
	if got.ErrorClass != agentruns.ErrorClassAgentError {
		t.Errorf("error class = %q, want %q", got.ErrorClass, agentruns.ErrorClassAgentError)
	}
	if agentruns.IsRetryableClass(got.ErrorClass) {
		t.Error("a definition that has to be edited must not be auto-retried")
	}
	for _, want := range []string{"Toolless Agent", "no-tools", "allowed_tools"} {
		if !strings.Contains(got.Error, want) {
			t.Errorf("failure message %q should mention %q", got.Error, want)
		}
	}
}

// The worker labels a run untrusted from BOTH the run's origin and the agent
// definition, and hands that to the adapter — which is what turns
// auto-approval off (REQ-91, HAZ-1).
func TestExecutePassesUntrustedToTheAdapter(t *testing.T) {
	cases := []struct {
		name  string
		agent agents.Agent
		run   *agentruns.Run
		want  bool
	}{
		{
			name:  "plain openv agent",
			agent: agents.Agent{Slug: "vv-engineer", Provider: "fake", AllowedTools: []string{"mcp__openv__*"}},
			want:  false,
		},
		{
			name:  "the interviewer talks to strangers",
			agent: agents.Agent{Slug: agents.InterviewerSlug, Provider: "fake", AllowedTools: []string{"mcp__openv__get_artifact"}},
			want:  true,
		},
		{
			name:  "repo access brings someone else's files in",
			agent: agents.Agent{Slug: "developer", Provider: "fake", RepoAccess: true, AllowedTools: []string{"mcp__openv__*", "Edit"}},
			want:  true,
		},
		{
			name:  "web tools bring someone else's pages in",
			agent: agents.Agent{Slug: "requirements-copilot", Provider: "fake", AllowedTools: []string{"mcp__openv__*", "WebSearch", "WebFetch"}},
			want:  true,
		},
		{
			// Trust is a property of where the run came from, not of one
			// slug. An interview can be bound to any agent (agent_slug on
			// create), and an interview turn's prompt is a participant's own
			// transcript whichever agent is serving it. Reading trust off the
			// definition alone let a workspace point its interviews at an
			// ordinary agent and get an auto-approving run out of a
			// stranger's text.
			name:  "an interview turn is untrusted whatever agent serves it",
			agent: agents.Agent{Slug: "requirements-analyst", Provider: "fake", AllowedTools: []string{"mcp__openv__*"}},
			run:   &agentruns.Run{ID: "r1", Prompt: "hi", InterviewSessionID: strPtr("sess-1")},
			want:  true,
		},
		{
			name:  "an ordinary run of the same agent is trusted",
			agent: agents.Agent{Slug: "requirements-analyst", Provider: "fake", AllowedTools: []string{"mcp__openv__*"}},
			run:   &agentruns.Run{ID: "r1", Prompt: "hi"},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			agent := tc.agent
			claim.Agent = &agent
			if tc.run != nil {
				claim.Run = tc.run
			}
			w.execute(context.Background(), claim)

			if got.Untrusted != tc.want {
				t.Errorf("RunSpec.Untrusted = %v, want %v", got.Untrusted, tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
