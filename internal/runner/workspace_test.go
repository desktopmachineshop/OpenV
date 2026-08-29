package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

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
