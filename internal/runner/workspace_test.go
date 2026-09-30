package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// TestExecuteReadsRepoConnectionsWithTheRunToken is the regression test for
// a runner that read a claimed run's repository connections with its own
// worker key. A member's personal runner key reads only the projects its
// member can (OpenV REQ-16), so the runner reads them with the run's own
// token, which reads its project's connections whoever's runner holds the
// run, with the local paths of the member whose runner claimed it, whose
// checkout a personal runner works on (REQ-86).
func TestExecuteReadsRepoConnectionsWithTheRunToken(t *testing.T) {
	rs := newRecordingServer()
	defer rs.srv.Close()

	h := &fakeHandle{events: make(chan RunEvent), waitCh: make(chan struct{})}
	close(h.events)
	close(h.waitCh)
	w := newTestWorker(rs, &fakeAdapter{start: func(context.Context, RunSpec) (RunHandle, error) { return h, nil }})
	w.workspaceBase = t.TempDir()

	claim := testClaim()
	claim.Agent = &agents.Agent{
		Slug: "developer", Name: "Developer", Provider: providers.ProviderClaudeCode, RepoAccess: true,
		AllowedTools: []string{"mcp__openv__*", "Read", "Edit"},
	}
	projectID := "proj-1"
	claim.Run = &agentruns.Run{ID: "r1", Prompt: "hi", ProjectID: &projectID}
	w.execute(context.Background(), claim)

	rs.mu.Lock()
	got := append([]string(nil), rs.repoAuth...)
	rs.mu.Unlock()
	if want := "Bearer " + claim.RunToken; len(got) != 1 || got[0] != want {
		t.Fatalf("the run's repository connections were read with %q, want once with the run's token (%q), "+
			"not the runner's worker key", got, want)
	}
}

// TestPrepareWorkspaceCancelledContext: a context cancelled before (or
// during) prep aborts PrepareWorkspace with the context's error — the
// cancel-during-prep path in worker.go relies on this to stop a clone that
// would otherwise run to completion.
func TestPrepareWorkspaceCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run := &agentruns.Run{ID: "run-cancel-test"}
	_, _, err := PrepareWorkspace(ctx, t.TempDir(), run, &agents.Agent{RepoAccess: true},
		[]*repoconns.RepoConnection{{RemoteURL: "https://example.invalid/repo.git"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareWorkspace with cancelled ctx = %v, want context.Canceled", err)
	}
}

// TestRunGitCtxCancelled: runGitCtx surfaces the context error (wrapped, so
// errors.Is works) instead of a generic git failure.
func TestRunGitCtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runGitCtx(ctx, "", "version"); !errors.Is(err, context.Canceled) {
		t.Fatalf("runGitCtx with cancelled ctx = %v, want context.Canceled", err)
	}
}
