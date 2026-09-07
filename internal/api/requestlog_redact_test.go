package api

import "testing"

func TestRedactPathHidesInviteTokens(t *testing.T) {
	cases := map[string]string{
		"/api/v1/public/interviews/abc123":          "/api/v1/public/interviews/[token]",
		"/api/v1/public/interviews/abc123/messages": "/api/v1/public/interviews/[token]/messages",
		"/api/v1/public/interviews/abc123/stream":   "/api/v1/public/interviews/[token]/stream",
		"/api/v1/public/interviews/":                "/api/v1/public/interviews/",
		"/api/v1/interviews/abc123/invites":         "/api/v1/interviews/abc123/invites",
		"/api/v1/public/connector/download":         "/api/v1/public/connector/download",
	}
	for in, want := range cases {
		if got := redactPath(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}
