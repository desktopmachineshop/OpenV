package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func healthBody(t *testing.T, h *Handler) map[string]string {
	t.Helper()
	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestHealthReportsBuildCommit: a build that knows its commit says so, which
// is what lets the staging smoke gate tell this deployment from a stale one
// (REQ-141).
func TestHealthReportsBuildCommit(t *testing.T) {
	body := healthBody(t, &Handler{buildSHA: "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"})
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
	if body["commit"] != "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c" {
		t.Errorf("commit = %q, want the build SHA", body["commit"])
	}
}

// TestHealthOmitsUnknownCommit: a local `go run` and the compose stack are
// told no commit, and their /health answer stays exactly what it always was —
// a key present but empty would read as a deployment that lost its revision.
func TestHealthOmitsUnknownCommit(t *testing.T) {
	body := healthBody(t, &Handler{})
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
	if _, ok := body["commit"]; ok {
		t.Errorf("commit present on a build that knows none: %v", body)
	}
}

// TestPublicBuildIsReachableThroughTheFrontendProxy: the commit has to be
// readable on the origin the gate uses. The frontend serves its own /health
// (frontend/nginx.conf: `return 200 "healthy"`) and proxies only /api/, so
// the API's /health never reaches a caller on that origin — this route is
// the one the staging smoke gate polls.
func TestPublicBuildReportsCommitUncached(t *testing.T) {
	h := &Handler{buildSHA: "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"}
	w := httptest.NewRecorder()
	h.GetPublicBuild(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/build", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["commit"] != "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c" {
		t.Errorf("commit = %q, want the build SHA", body["commit"])
	}
	// A deploy that has just landed is exactly what the caller is waiting to
	// notice, so this answer must never be served from a cache — unlike the
	// release feed beside it, which is deliberately cacheable.
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// TestPublicBuildOnAnUnidentifiedBuild: an empty commit must read as "this
// build cannot be identified", never as a match. The gate compares against a
// real SHA, which an empty string never equals.
func TestPublicBuildOnAnUnidentifiedBuild(t *testing.T) {
	w := httptest.NewRecorder()
	(&Handler{}).GetPublicBuild(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/build", nil))

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["commit"] != "" {
		t.Errorf("commit = %q, want empty", body["commit"])
	}
}
