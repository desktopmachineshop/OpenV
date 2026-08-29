package artifacts

import (
	"strings"
	"testing"
)

func TestSnippet(t *testing.T) {
	t.Run("short body is returned whole", func(t *testing.T) {
		if got := Snippet("the login flow", "login"); got != "the login flow" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("newlines collapse to spaces", func(t *testing.T) {
		if got := Snippet("line one\nlogin\nline three", "login"); got != "line one login line three" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("long body is trimmed around the match with ellipses", func(t *testing.T) {
		body := strings.Repeat("x", 200) + " login " + strings.Repeat("y", 200)
		got := Snippet(body, "LOGIN")
		if !strings.Contains(got, "login") {
			t.Fatalf("snippet %q lost the match", got)
		}
		if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
			t.Errorf("snippet %q should be elided on both sides", got)
		}
		if len([]rune(got)) > 2*snippetRadius+len("login")+2 {
			t.Errorf("snippet is %d runes, want at most the radius window", len([]rune(got)))
		}
	})

	t.Run("title-only match falls back to the body head", func(t *testing.T) {
		body := strings.Repeat("z", 300)
		got := Snippet(body, "absent")
		if !strings.HasSuffix(got, "…") || len([]rune(got)) != 2*snippetRadius+1 {
			t.Errorf("got %q (len %d), want the elided body head", got, len([]rune(got)))
		}
	})

	t.Run("empty body yields empty snippet", func(t *testing.T) {
		if got := Snippet("", "q"); got != "" {
			t.Errorf("got %q", got)
		}
	})
}

func TestLikePattern(t *testing.T) {
	cases := map[string]string{
		"login":  "%login%",
		"100%":   `%100\%%`,
		"a_b":    `%a\_b%`,
		`back\s`: `%back\\s%`,
	}
	for in, want := range cases {
		if got := LikePattern(in); got != want {
			t.Errorf("LikePattern(%q) = %q, want %q", in, got, want)
		}
	}
}
