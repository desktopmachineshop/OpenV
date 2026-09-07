package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The session cookie's attributes follow the deployment topology: same-site
// deployments (the API proxied on the frontend's origin) get a plain Lax
// cookie; cross-site ones get SameSite=None + Secure + Partitioned, the only
// third-party cookie Chromium-based browsers still store.
func TestSessionCookieAttributesFollowDeploymentTopology(t *testing.T) {
	cases := []struct {
		name          string
		deps          HandlerDeps
		want, notWant []string
	}{
		{
			name:    "same-site plain http",
			deps:    HandlerDeps{},
			want:    []string{"SameSite=Lax", "HttpOnly"},
			notWant: []string{"Secure", "Partitioned", "SameSite=None"},
		},
		{
			name:    "same-site https",
			deps:    HandlerDeps{SecureCookies: true},
			want:    []string{"SameSite=Lax", "Secure"},
			notWant: []string{"Partitioned"},
		},
		{
			name:    "cross-site",
			deps:    HandlerDeps{CrossSiteCookies: true},
			want:    []string{"SameSite=None", "Secure", "Partitioned", "HttpOnly"},
			notWant: []string{"SameSite=Lax"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(tc.deps)
			for _, phase := range []string{"set", "clear"} {
				rec := httptest.NewRecorder()
				if phase == "set" {
					h.setSessionCookie(rec, "token")
				} else {
					h.clearSessionCookie(rec)
				}
				raw := rec.Header().Get("Set-Cookie")
				if !strings.HasPrefix(raw, SessionCookieName+"=") {
					t.Fatalf("%s: unexpected cookie %q", phase, raw)
				}
				for _, w := range tc.want {
					if !strings.Contains(raw, w) {
						t.Errorf("%s: cookie %q lacks %s", phase, raw, w)
					}
				}
				for _, nw := range tc.notWant {
					if strings.Contains(raw, nw) {
						t.Errorf("%s: cookie %q must not carry %s", phase, raw, nw)
					}
				}
				if phase == "clear" && !strings.Contains(raw, "Max-Age=0") {
					t.Errorf("clear: cookie %q does not expire", raw)
				}
			}
		})
	}
}

// Partitioned cookies must also be Secure, or browsers drop them; the cross-
// site mode forces Secure on regardless of SECURE_COOKIES.
func TestCrossSiteCookiesForceSecure(t *testing.T) {
	h := NewHandler(HandlerDeps{CrossSiteCookies: true, SecureCookies: false})
	rec := httptest.NewRecorder()
	h.setSessionCookie(rec, "token")
	c := rec.Result().Cookies()[0]
	if !c.Secure || !c.Partitioned || c.SameSite != http.SameSiteNoneMode {
		t.Fatalf("cross-site cookie not secure+partitioned+none: %+v", c)
	}
}
