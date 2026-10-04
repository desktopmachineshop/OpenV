package runner

import (
	"context"
	"log"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// A sign-in that completes is reported, then its provider is detected again
// (the report sent, the provider added to the providers the next claim
// sends: loginBroker.redetect), and only then does agentd log that the
// sign-in completed. An operator who reads "sign-in completed" in the log
// can count on the provider being in the next claim. Nothing pinned that
// order: moving redetect after the log line in finishLogin passed every
// test (#379 bug 123). These tests watch agentd's log and note, as the
// completed line is written, what the stand-in API has received and which
// providers the worker holds, for each place a sign-in completes: the
// piped flow and the console flow (loginBroker.handleLogin and
// handleInteractiveLogin), and finishLogin, where the pseudo-terminal and
// loopback flows end.

// completedLog notes the state of the world each time agentd logs that a
// sign-in completed.
type completedLog struct {
	mu    sync.Mutex
	state func() string
	notes []string
}

func (l *completedLog) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "sign-in completed") {
		note := strings.TrimSpace(string(p)) + " => " + l.state()
		l.mu.Lock()
		l.notes = append(l.notes, note)
		l.mu.Unlock()
	}
	return len(p), nil
}

func (l *completedLog) Notes() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.notes)
}

// watchCompletedLog points the standard logger, which agentd logs through,
// at a completedLog for the length of the test. The test must not run in
// parallel with others.
func watchCompletedLog(t *testing.T, state func() string) *completedLog {
	t.Helper()
	l := &completedLog{state: state}
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(l)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	return l
}

func TestSignInLogsCompletionAfterTheProviderIsAdded(t *testing.T) {
	skipWithoutPOSIXShell(t)
	for _, c := range []struct {
		name, provider, line string
		run                  func(b *loginBroker, ctx context.Context, login *providers.LoginRequest)
	}{
		{"piped", providers.ProviderCodexCLI, "login l1: codex-cli sign-in completed",
			func(b *loginBroker, ctx context.Context, login *providers.LoginRequest) { b.handleLogin(ctx, login) }},
		{"console", providers.ProviderClaudeCode, "login l1: claude-code interactive sign-in completed",
			func(b *loginBroker, ctx context.Context, login *providers.LoginRequest) { b.handleLogin(ctx, login) }},
		{"finishLogin (pseudo-terminal and loopback)", providers.ProviderClaudeCode, "login l1: claude-code sign-in completed",
			func(b *loginBroker, ctx context.Context, login *providers.LoginRequest) {
				b.finishLogin(ctx, login, nil, nil, newLineTail(1))
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := t.TempDir()
			writeScript(t, bin, "codex", standInCodex)
			writeScript(t, bin, "claude", standInClaude)
			isolateRunnerEnv(t, bin)
			t.Setenv("HOME", t.TempDir())
			api := newFakeAPI(t, nil)
			w := NewWorker(NewClient(api.URL(), "worker-key"), Options{WorkerID: "w-signin"})
			w.adapters = map[string]Adapter{c.provider: &scriptedAdapter{name: c.provider, installed: true}}

			reported := func(calls []apiCall) bool {
				return count(calls, func(c apiCall) bool { return c.is("POST", "/provider-settings/detect") }) > 0
			}
			logs := watchCompletedLog(t, func() string {
				calls := api.Calls()
				s := &signInWorld{t: t}
				progress, _ := s.progressOf("l1", calls)
				last := ""
				if len(progress) > 0 {
					last = progress[len(progress)-1].Status
				}
				return "last progress " + last + ", detection reported " + map[bool]string{true: "yes", false: "no"}[reported(calls)] +
					", providers " + strings.Join(w.snapshotProviders(), ",")
			})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			c.run(w.logins, ctx, &providers.LoginRequest{ID: "l1", Provider: c.provider})

			want := []string{c.line + " => last progress " + providers.LoginCompleted + ", detection reported yes, providers " + c.provider}
			if got := logs.Notes(); !slices.Equal(got, want) {
				t.Errorf("agentd logged the completed sign-in as\n  %q\nwant, after the report and the detection,\n  %q", got, want)
			}
		})
	}
}
