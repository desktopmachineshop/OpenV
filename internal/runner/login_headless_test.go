package runner

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// The loopback port is read from the CLI's own redirect_uri, so a vendor that
// picks a different port still works — and a URL without one falls back
// rather than failing.
func TestLoopbackBaseFrom(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "port from redirect_uri",
			url:  "https://auth.openai.com/authorize?client_id=x&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback",
			want: "http://localhost:1455",
		},
		{
			name: "a non-default port is honored",
			url:  "https://auth.openai.com/authorize?redirect_uri=http%3A%2F%2F127.0.0.1%3A8123%2Fcb",
			want: "http://127.0.0.1:8123",
		},
		{name: "no redirect_uri", url: "https://auth.openai.com/authorize?client_id=x", want: defaultLoopbackBase},
		{name: "unparseable", url: "::not a url::", want: defaultLoopbackBase},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := loopbackBaseFrom(tc.url); got != tc.want {
				t.Errorf("loopbackBaseFrom(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// The replay hits the CLI's own listener with the pasted path and query.
func TestReplayLoopbackCallbackForwardsPathAndQuery(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pasted := "http://localhost:1455/auth/callback?code=abc123&state=xyz"
	if err := replayLoopbackCallback(server.URL, pasted); err != nil {
		t.Fatalf("replayLoopbackCallback: %v", err)
	}
	if gotPath != "/auth/callback" {
		t.Errorf("replayed path = %q, want /auth/callback", gotPath)
	}
	if !strings.Contains(gotQuery, "code=abc123") {
		t.Errorf("replayed query = %q, want it to carry the code", gotQuery)
	}
}

// Only the path and query of the paste are used: the host always comes from
// the CLI's own advertised listener, so a paste naming another host cannot
// make the runner fetch it.
func TestReplayLoopbackCallbackIgnoresPastedHost(t *testing.T) {
	var reached bool
	loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer loopback.Close()

	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the replay reached a host named in the pasted value")
		w.WriteHeader(http.StatusOK)
	}))
	defer elsewhere.Close()

	if err := replayLoopbackCallback(loopback.URL, elsewhere.URL+"/auth/callback?code=abc"); err != nil {
		t.Fatalf("replayLoopbackCallback: %v", err)
	}
	if !reached {
		t.Error("the replay did not reach the CLI's own listener")
	}
}

// A paste that is not a redirect gets a usable message back, and no request
// is made at all.
func TestReplayLoopbackCallbackRejectsUnusablePastes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an unusable paste still issued a request")
	}))
	defer server.Close()

	for _, pasted := range []string{"", "just-a-code", "http://localhost:1455/auth/callback"} {
		err := replayLoopbackCallback(server.URL, pasted)
		if err == nil {
			t.Errorf("replayLoopbackCallback(%q) succeeded, want a rejection", pasted)
			continue
		}
		if !strings.Contains(err.Error(), "address") {
			t.Errorf("rejection for %q reads %q; it should tell the member what to paste", pasted, err)
		}
	}
}

// A rejected replay is reported rather than swallowed, so the member can try
// again instead of watching a card that never moves.
func TestReplayLoopbackCallbackReportsRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	err := replayLoopbackCallback(server.URL, "http://localhost:1455/auth/callback?code=stale")
	if err == nil {
		t.Fatal("a 400 from the CLI's listener was reported as success")
	}
}

// URL scraping runs over ANSI-painted TUI output, so the escapes have to come
// off before the URL can be found.
func TestStripANSIExposesTheAuthURL(t *testing.T) {
	painted := "\x1b[1m\x1b[38;5;208mOpen this URL:\x1b[0m \x1b[4mhttps://claude.ai/oauth/authorize?code=1\x1b[0m\r"
	plain := stripANSI(painted)
	if strings.Contains(plain, "\x1b") || strings.Contains(plain, "\r") {
		t.Fatalf("stripANSI left control sequences in %q", plain)
	}
	got := authURLPattern.FindString(plain)
	if got != "https://claude.ai/oauth/authorize?code=1" {
		t.Errorf("scraped URL = %q, want the authorize link", got)
	}
}

// realRetryFrame is what `claude setup-token` actually drew after a code it
// rejected, captured verbatim from the CLI over a pseudo-terminal. It is a
// redraw: carriage returns, cursor moves and colour, and not a single
// newline — which is precisely how a line scanner sat on this message
// without ever seeing it, leaving the sign-in stuck at "code sent".
const realRetryFrame = "\x1b(B\x0f\x1b[2K\x1b[1A\x1b[2K\x1b[G\x1b[1A\r\x1b[1C\x1b[4A" +
	"\x1b[38;5;211mOAuth error: Invalid code. Please make sure the full code was copied" +
	"\r\x1b[2B\x1b[39m\x1b[K\r\x1b[1C\x1b[1B\x1b[38;5;153mPress \x1b[1mEnter\x1b[22m to retry." +
	"\r\x1b[1B\x1b[39m\x1b[K\r\x1b[1B\x1b[K\r\x1b[1A"

// The rejection is recovered from that frame and reads as a human sentence,
// so the member is told what went wrong instead of watching a dead card.
func TestTUIErrorReadsTheRealRejection(t *testing.T) {
	got := tuiError(realRetryFrame)
	if !strings.Contains(got, "Invalid code") {
		t.Fatalf("tuiError = %q, want the CLI's rejection message", got)
	}
	if strings.Contains(got, "\x1b") || strings.Contains(got, "[38;5;") {
		t.Errorf("tuiError leaked terminal escapes: %q", got)
	}
	if got != "OAuth error: Invalid code. Please make sure the full code was copied" {
		t.Errorf("tuiError = %q, want the message on its own", got)
	}
}

// The prompt a healthy flow shows is not an error: a card that cried failure
// at the paste prompt would be worse than one that says nothing.
func TestTUIErrorIgnoresHealthyOutput(t *testing.T) {
	healthy := "\x1b[2GPaste\x1b[8Gcode\x1b[13Ghere\x1b[18Gif\x1b[21Gprompted\x1b[30G>\r\r\n"
	if got := tuiError(healthy); got != "" {
		t.Errorf("tuiError(healthy prompt) = %q, want \"\"", got)
	}
}

// A TUI redraw carries its lines on carriage returns; visibleLines has to
// recover them, or everything downstream reads one run-on line.
func TestVisibleLinesSplitsOnCarriageReturns(t *testing.T) {
	lines := visibleLines(realRetryFrame)
	var joined []string
	for _, l := range lines {
		joined = append(joined, l)
	}
	if len(joined) < 2 {
		t.Fatalf("visibleLines returned %d lines (%q), want the message and the retry prompt", len(joined), joined)
	}
	var sawMessage, sawRetry bool
	for _, l := range joined {
		if strings.Contains(l, "Invalid code") {
			sawMessage = true
		}
		if strings.Contains(l, "to retry") {
			sawRetry = true
		}
	}
	if !sawMessage || !sawRetry {
		t.Errorf("visibleLines lost content: %q", joined)
	}
}

// The last frame wins: a TUI redraws, so an older frame still sitting in the
// buffer must not be reported over what is on screen now.
func TestTUIErrorPrefersTheLatestFrame(t *testing.T) {
	buffer := realRetryFrame + "\r\x1b[38;5;211mOAuth error: Code expired\r\n"
	if got := tuiError(buffer); !strings.Contains(got, "expired") {
		t.Errorf("tuiError = %q, want the most recent frame's message", got)
	}
}

// The screen buffer is bounded, and keeps the newest output — the end is
// where the current frame is.
func TestScreenBufferKeepsTheTail(t *testing.T) {
	b := newScreenBuffer(64)
	b.add(strings.Repeat("x", 200))
	b.add("OAuth error: Invalid code")
	if got := b.String(); len(got) > 64 {
		t.Errorf("screen buffer grew to %d bytes, want at most 64", len(got))
	}
	if !strings.Contains(b.String(), "Invalid code") {
		t.Errorf("screen buffer dropped the newest output: %q", b.String())
	}
	b.reset()
	if b.String() != "" {
		t.Errorf("reset left %q behind; a stale error must not be blamed on a new attempt", b.String())
	}
}

// The Claude Code sign-in must be `auth login`, not `setup-token`. Both drive
// the same paste-back flow over a terminal, but setup-token asks only for
// user:inference and leaves the CLI signed out — every run after it dies on
// "Not logged in", which is exactly what happened in production.
func TestClaudeCodeSignsInRatherThanMintingAToken(t *testing.T) {
	flow, ok := flowFor(providers.ProviderClaudeCode)
	if !ok {
		t.Fatal("no sign-in flow for claude-code")
	}
	got := strings.Join(flow.command, " ")
	if got != "claude auth login" {
		t.Errorf("claude sign-in command = %q, want \"claude auth login\"", got)
	}
	if !flow.interactive {
		t.Error("the claude sign-in is an Ink TUI and renders nothing over pipes; it must be marked interactive")
	}
}

// The CLI's own answer decides whether it is signed in. Anything else is a
// guess, and the guess it replaced ("~/.claude exists") reported a signed-out
// runner as ready.
func TestParseClaudeAuthStatus(t *testing.T) {
	cases := []struct {
		name       string
		out        string
		wantLogged bool
		wantMethod string
		wantOK     bool
	}{
		{
			name:       "signed in",
			out:        `{"loggedIn": true, "authMethod": "oauth_token", "apiProvider": "firstParty"}`,
			wantLogged: true, wantMethod: "oauth_token", wantOK: true,
		},
		{
			name:   "signed out",
			out:    `{"loggedIn": false, "authMethod": ""}`,
			wantOK: true, wantMethod: "unknown method",
		},
		{
			name:       "leading noise before the json",
			out:        "warning: something\n{\"loggedIn\": true, \"authMethod\": \"api_key\"}",
			wantLogged: true, wantMethod: "api_key", wantOK: true,
		},
		// An older CLI answers "unknown command", and a caller that read
		// that as "signed out" would hide a working runner.
		{name: "not json", out: "error: unknown command 'auth'"},
		{name: "empty", out: ""},
		// JSON without the field is not an answer either.
		{name: "json missing the field", out: `{"apiProvider": "firstParty"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loggedIn, method, ok := parseClaudeAuthStatus(tc.out)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if loggedIn != tc.wantLogged {
				t.Errorf("loggedIn = %v, want %v", loggedIn, tc.wantLogged)
			}
			if method != tc.wantMethod {
				t.Errorf("method = %q, want %q", method, tc.wantMethod)
			}
		})
	}
}
