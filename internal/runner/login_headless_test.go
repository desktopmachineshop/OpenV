package runner

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
