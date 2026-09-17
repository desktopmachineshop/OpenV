package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	for _, hsts := range []bool{false, true} {
		rec := httptest.NewRecorder()
		SecurityHeadersMiddleware(hsts)(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))
		h := rec.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("nosniff missing: %v", h)
		}
		if h.Get("X-Frame-Options") != "DENY" {
			t.Fatalf("frame header missing: %v", h)
		}
		if h.Get("Referrer-Policy") == "" || h.Get("Content-Security-Policy") == "" {
			t.Fatalf("referrer policy or CSP missing: %v", h)
		}
		if got := h.Get("Strict-Transport-Security"); (got != "") != hsts {
			t.Fatalf("hsts=%v but header %q", hsts, got)
		}
	}
}

func TestCORSRefusesWildcardAndEmpty(t *testing.T) {
	for _, origin := range []string{"*", "", "null"} {
		if _, err := CORSMiddleware(origin, okHandler()); err == nil {
			t.Fatalf("origin %q accepted", origin)
		}
	}
}

func TestCORSAllowsOnlyTheConfiguredOrigin(t *testing.T) {
	h, err := CORSMiddleware("https://app.example.com", okHandler())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		origin string
		want   string
	}{
		{"https://app.example.com", "https://app.example.com"},
		{"https://evil.example", ""},
		{"", ""},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
			t.Fatalf("origin %q: allow-origin %q, want %q", tc.origin, got, tc.want)
		}
		if tc.want != "" && rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatalf("credentials not allowed for the configured origin")
		}
	}
	// A preflight from a foreign origin is answered without CORS headers and
	// never reaches the wrapped handler.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign preflight: code %d headers %v", rec.Code, rec.Header())
	}
}

func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n := 0
		for {
			m, err := r.Body.Read(buf)
			n += m
			if err != nil {
				if _, ok := err.(*http.MaxBytesError); ok {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				break
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	h := BodyLimitMiddleware(16)(read)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small")))
	if rec.Code != http.StatusOK {
		t.Fatalf("small body: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", rec.Code)
	}
}

// Issue #364: a MaxBytesReader wrapped around another one enforces the
// tighter of the two, so the JSON-sized cap here used to override whatever an
// upload handler asked for — the figure limit could not be raised past it, and
// the evidence handler's own 200 MB cap had never been reachable. A multipart
// body now passes through untouched, to a handler that bounds it itself.
func TestBodyLimitLeavesFileUploadsToTheirHandler(t *testing.T) {
	var read int
	sink := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		read = int(n)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := BodyLimitMiddleware(16)(sink)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload",
		strings.NewReader(strings.Repeat("x", 100)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || read != 100 {
		t.Fatalf("multipart body was clamped: code=%d read=%d", rec.Code, read)
	}

	// Anything that is not a file upload is still held to the cap.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/artifacts",
		strings.NewReader(strings.Repeat("x", 100)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a JSON body escaped the cap: %d", rec.Code)
	}
}
