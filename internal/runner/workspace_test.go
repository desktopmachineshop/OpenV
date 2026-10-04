package runner

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

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

// TestRunFailsWhenItsRepoConnectionsCannotBeRead is the regression test for
// #379 bug 57: a run that asked for repository access, whose project's
// repository connections could not be read, was only logged, and the run
// went on in an empty workspace and "succeeded" without the code it was
// meant to work on. It now fails as a workspace failure, which is not
// retried, before its start transition and without reaching the adapter.
func TestRunFailsWhenItsRepoConnectionsCannotBeRead(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusForbidden, `{"error":"forbidden","code":"forbidden"}`},
		{http.StatusInternalServerError, `{"error":"internal error","code":"internal"}`},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			api := newFakeAPI(t, func(c apiCall) (int, string) {
				if c.is("GET", "/repo-connections") {
					return tc.status, tc.body
				}
				return 0, ""
			})
			started := false
			adapter := &fakeAdapter{start: func(context.Context, RunSpec) (RunHandle, error) {
				started = true
				return finishedRun(Result{FinalText: "Done."}, nil), nil
			}}
			w := &Worker{
				client:             NewClient(api.URL(), "worker-key"),
				adapters:           map[string]Adapter{providers.ProviderClaudeCode: adapter},
				workerID:           "w-test",
				workspaceBase:      t.TempDir(),
				apiURL:             api.URL(),
				workspaceRetention: time.Hour,
			}
			claim := testClaim()
			claim.Agent = &agents.Agent{
				Slug: "developer", Name: "Developer", Provider: providers.ProviderClaudeCode, RepoAccess: true,
				AllowedTools: []string{"mcp__openv__*", "Read", "Edit"},
			}
			projectID := "proj-1"
			claim.Run = &agentruns.Run{ID: "r1", Prompt: "hi", ProjectID: &projectID}
			w.execute(context.Background(), claim)

			if started {
				t.Error("the run reached the adapter: it ran without its repository")
			}
			calls := api.Calls()
			if n := count(calls, func(c apiCall) bool { return c.is("POST", "/start") }); n != 0 {
				t.Errorf("the run was moved to running (%d start transitions), want it failed before", n)
			}
			finishes := api.match(func(c apiCall) bool { return c.is("POST", "/finish") })
			if len(finishes) != 1 {
				t.Fatalf("finish requests = %d, want 1; calls:\n%s", len(finishes), describeCalls(calls))
			}
			var req agentruns.FinishRequest
			finishes[0].json(t, &req)
			want := "could not read the project's repository connections: api returned " +
				strconv.Itoa(tc.status) + ": " + tc.body
			if req.Status != agentruns.StatusFailed || req.ErrorClass != agentruns.ErrorClassWorkspace || req.Error != want {
				t.Errorf("finish = status %q, class %q, error %q; want failed, %q, %q",
					req.Status, req.ErrorClass, req.Error, agentruns.ErrorClassWorkspace, want)
			}
			if agentruns.IsRetryableClass(req.ErrorClass) {
				t.Errorf("error class %q is retried; a run whose repository cannot be read must not be", req.ErrorClass)
			}
		})
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
