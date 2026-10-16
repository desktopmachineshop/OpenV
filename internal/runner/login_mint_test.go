package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// fakeMintedToken is shaped like a `claude setup-token` token and is
// obviously not one.
const fakeMintedToken = "sk-ant-oat01-runner-test-placeholder-not-a-real-token"

// A token request on a headless runner runs the minting flow, types the
// pasted code in, reads the token the CLI prints and hands it to the token
// route: never to a progress report, which the member's browser reads.
func TestPTYMintHandsTheTokenToTheServerOnly(t *testing.T) {
	if !ptySupported {
		t.Skip("minting needs a pseudo-terminal")
	}
	var mu sync.Mutex
	var progress []string
	saved := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/progress"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			progress = append(progress, body["detail"]+" "+body["auth_url"])
			mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/full"):
			_ = json.NewEncoder(w).Encode(providers.LoginRequest{ID: "l1", Status: providers.LoginAwaitingCode, Code: "the-code"})
		case strings.HasSuffix(r.URL.Path, "/token"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			saved <- body["token"]
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	worker := NewWorker(NewClient(srv.URL, "worker-key"), Options{Headless: true})
	flow := mintTokenFlow
	// The fake CLI prints a link, waits for the code, then prints the token
	// in colour, as the real one draws it.
	flow.command = []string{"sh", "-c",
		`echo https://claude.ai/oauth/authorize?client_id=openv; read code; ` +
			`printf '\033[32mYour OAuth token:\033[0m\r\n` + fakeMintedToken + `\r\n'; sleep 0.2`}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.logins.handlePTYLogin(ctx, &providers.LoginRequest{ID: "l1", Provider: providers.ProviderClaudeCode,
			Purpose: providers.LoginPurposeToken}, flow)
	}()

	select {
	case got := <-saved:
		if got != fakeMintedToken {
			t.Fatalf("saved token = %q", got)
		}
	case <-time.After(30 * time.Second):
		mu.Lock()
		t.Fatalf("no token was handed in; progress: %q", progress)
	}
	waitFor(t, done)
	mu.Lock()
	defer mu.Unlock()
	for _, p := range progress {
		if strings.Contains(p, "placeholder-not-a-real-token") {
			t.Fatalf("a progress report carried the token: %q", p)
		}
	}
}

// Whatever a runner relays or logs shows a credential's kind, not the
// credential.
func TestRedactSecrets(t *testing.T) {
	got := redactSecrets("token: " + fakeMintedToken + " and sk-ant-api03-abcdefgh12345678 done")
	if strings.Contains(got, "placeholder") || strings.Contains(got, "abcdefgh12345678") {
		t.Fatalf("redacted = %q", got)
	}
	if !strings.Contains(got, "sk-ant-oat01-…(hidden)") || !strings.Contains(got, "sk-ant-api03-…(hidden)") {
		t.Fatalf("redacted = %q", got)
	}
	if got := redactSecrets("Open https://claude.ai/oauth/authorize?x=1"); got != "Open https://claude.ai/oauth/authorize?x=1" {
		t.Fatalf("redacted plain text: %q", got)
	}
}

// A runner that cannot read the token refuses a token request rather than
// running an ordinary sign-in in its place.
func TestOnlyAHeadlessRunnerMints(t *testing.T) {
	failed := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/progress") {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["status"] == providers.LoginFailed {
				failed <- body["detail"]
			}
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	worker := NewWorker(NewClient(srv.URL, "worker-key"), Options{Headless: false})
	worker.logins.handleLogin(context.Background(), &providers.LoginRequest{ID: "l1",
		Provider: providers.ProviderClaudeCode, Purpose: providers.LoginPurposeToken})
	select {
	case detail := <-failed:
		if !strings.Contains(detail, "cloud runner") {
			t.Fatalf("detail = %q", detail)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a personal runner did not refuse the token request")
	}
}
