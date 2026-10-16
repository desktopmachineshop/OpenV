package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	saved := make(chan map[string]any, 1)
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
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			saved <- body
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	worker := NewWorker(NewClient(srv.URL, "worker-key"), Options{Headless: true})
	flow := mintTokenFlow
	flow.signOut = nil // never the real `claude auth logout` in a test
	// The fake CLI prints a link, waits for the code, then prints the token
	// in colour, as the real one draws it.
	flow.command = []string{"sh", "-c",
		`echo https://claude.ai/oauth/authorize?client_id=openv; read code; ` +
			`printf '\033[32mYour OAuth token (valid for 1 year):\033[0m\r\n` + fakeMintedToken + `\r\n'; sleep 0.2`}
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
		if got["token"] != fakeMintedToken {
			t.Fatalf("saved token = %v", got["token"])
		}
		if got["expires_in"] != float64(365*24*3600) {
			t.Fatalf("expires_in = %v, want the year the CLI printed", got["expires_in"])
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

// The lifetime is read from what the CLI printed, in the forms a duration
// takes; anything unreadable is 0, the server's cue for the default.
func TestMintedLifetime(t *testing.T) {
	day := 24 * time.Hour
	for text, want := range map[string]time.Duration{
		"Your OAuth token (valid for 1 year):":   365 * day,
		"Your OAuth token (valid for 30 days):":  30 * day,
		"Your OAuth token (valid for 12h 30m):":  12*time.Hour + 30*time.Minute,
		"Your OAuth token (valid for 2 weeks):":  14 * day,
		"Your OAuth token (valid for a while):":  0,
		"Long-lived token created successfully!": 0,
	} {
		if got := mintedLifetime(text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

// A sign-in that goes quiet after the code relays what its terminal shows,
// credentials hidden, so the member is not left on "code received".
func TestAQuietSignInRelaysItsScreen(t *testing.T) {
	if !ptySupported {
		t.Skip("needs a pseudo-terminal")
	}
	defer func(was time.Duration) { quietAfter = was }(quietAfter)
	quietAfter = time.Second
	details := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/progress"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			select {
			case details <- body["detail"]:
			default:
			}
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/full"):
			_ = json.NewEncoder(w).Encode(providers.LoginRequest{ID: "l1", Status: providers.LoginAwaitingCode, Code: "the-code"})
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	worker := NewWorker(NewClient(srv.URL, "worker-key"), Options{Headless: true})
	flow := mintTokenFlow
	flow.signOut = nil // never the real `claude auth logout` in a test
	flow.command = []string{"sh", "-c", `echo https://claude.ai/oauth/authorize?client_id=openv; read code; ` +
		`printf 'Creating API key for Claude Code… ` + fakeMintedToken + `\r\n'; sleep 20`}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		worker.logins.handlePTYLogin(ctx, &providers.LoginRequest{ID: "l1", Provider: providers.ProviderClaudeCode,
			Purpose: providers.LoginPurposeToken}, flow)
	}()
	// The flow reads quietAfter: it must be over before the deferred reset.
	defer func() { cancel(); <-finished }()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case d := <-details:
			if !strings.Contains(d, "terminal shows") {
				continue
			}
			if !strings.Contains(d, "Creating API key for Claude Code") || strings.Contains(d, "placeholder-not-a-real-token") {
				t.Fatalf("relayed screen = %q", d)
			}
			return
		case <-deadline:
			t.Fatal("the quiet sign-in never relayed its screen")
		}
	}
}

// A headless Claude sign-in signs the CLI out first and runs with the
// Claude credential variables blanked, so neither an old sign-in nor a key
// on the host stands in for the one being made.
func TestAHeadlessSignInStartsFromACleanSlate(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "left-over-token")
	seen := t.TempDir() + "/seen"
	flow := mintTokenFlow
	flow.signOut = []string{"sh", "-c", `printf 'ran:%s' "$CLAUDE_CODE_OAUTH_TOKEN" > ` + seen}
	worker := NewWorker(NewClient("http://127.0.0.1:1", "worker-key"), Options{Headless: true})
	env := childEnv(worker.logins.cleanSlate(context.Background(), &providers.LoginRequest{ID: "l1"}, flow)...)
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the sign-out did not run: %v", err)
	}
	if string(got) != "ran:" {
		t.Fatalf("the sign-out saw the host's token: %q", got)
	}
	last := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		last[k] = v
	}
	for _, name := range claudeCredentialEnv {
		if last[name] != "" {
			t.Errorf("%s reaches the sign-in as %q", name, last[name])
		}
	}
	// Both Claude flows start this way.
	if signIn, _ := flowFor(providers.ProviderClaudeCode); len(signIn.signOut) == 0 || len(mintTokenFlow.signOut) == 0 {
		t.Fatal("a Claude sign-in flow does not sign out first")
	}
}

// timedWriter records each write and when it came.
type timedWriter struct {
	writes []string
	at     []time.Time
}

func (w *timedWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	w.at = append(w.at, time.Now())
	return len(p), nil
}

// The code and its Enter are two writes, apart: a TUI reads one burst as a
// paste and keeps an Enter inside it as text, which left a minted sign-in
// sitting on its masked code in production.
func TestTypeCodePressesEnterSeparately(t *testing.T) {
	w := &timedWriter{}
	if err := typeCode(w, "the-code#state"); err != nil {
		t.Fatal(err)
	}
	if len(w.writes) != 2 || w.writes[0] != "the-code#state" || w.writes[1] != "\r" {
		t.Fatalf("writes = %q", w.writes)
	}
	if gap := w.at[1].Sub(w.at[0]); gap < submitAfter/2 {
		t.Fatalf("Enter came %v after the code", gap)
	}
}

// The relayed screen carries no charset or shift codes.
func TestStripANSIDropsCharsetAndShiftCodes(t *testing.T) {
	if got := stripANSI("\x1b(B\x0fPaste code here\x1b)0\x0e"); got != "Paste code here" {
		t.Fatalf("stripped = %q", got)
	}
}
