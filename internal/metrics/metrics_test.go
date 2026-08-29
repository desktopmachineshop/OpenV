package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// newTestRouter returns a router with one dynamic route and the /metrics
// endpoint, wrapped in the HTTP-instrumentation middleware.
func newTestRouter(m *Metrics, token string) http.Handler {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}).Methods("GET")
	router.Handle("/metrics", m.Handler(token)).Methods("GET")
	return m.HTTPMiddleware(router)(router)
}

func TestMetricsEndpointExposesCountedRequest(t *testing.T) {
	m := New()
	h := newTestRouter(m, "")

	// A request to a dynamic route should be counted under the route template.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/abc-123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("app request: got status %d, want 200", rec.Code)
	}

	// Scrape /metrics.
	scrape := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	srec := httptest.NewRecorder()
	h.ServeHTTP(srec, scrape)
	if srec.Code != http.StatusOK {
		t.Fatalf("scrape: got status %d, want 200", srec.Code)
	}
	if ct := srec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("scrape: content-type %q, want prometheus text exposition", ct)
	}

	body := srec.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Fatalf("scrape body missing http_requests_total:\n%s", body)
	}
	// The dynamic segment must collapse to the mux template, not the raw path.
	if !strings.Contains(body, `route="/api/v1/projects/{id}"`) {
		t.Fatalf("scrape body missing templated route label:\n%s", body)
	}
	if strings.Contains(body, "abc-123") {
		t.Fatalf("scrape body leaked raw path segment (cardinality blowup):\n%s", body)
	}
	if !strings.Contains(body, "http_request_duration_seconds") {
		t.Fatalf("scrape body missing duration histogram:\n%s", body)
	}
}

func TestRouteTemplateCollapsesDynamicSegments(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/projects/{id}", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/baselines", func(http.ResponseWriter, *http.Request) {}).Methods("GET")

	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/api/v1/projects/abc-123", "/api/v1/projects/{id}"},
		{http.MethodGet, "/api/v1/projects/00000000-0000-0000-0000-000000000000/baselines", "/api/v1/projects/{id}/baselines"},
		{http.MethodGet, "/api/v1/does-not-exist", "unmatched"},
		{http.MethodPost, "/api/v1/projects/abc-123", "unmatched"}, // method mismatch
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		if got := routeTemplate(router, req); got != c.want {
			t.Errorf("routeTemplate(%s %s) = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestMetricsTokenGate(t *testing.T) {
	m := New()
	h := m.Handler("s3cret")

	// No token: rejected.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: got %d, want 401", rec.Code)
	}

	// Correct token: allowed.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: got %d, want 200", rec.Code)
	}
}

func TestAgentRunLifecycleMetrics(t *testing.T) {
	m := New()
	run := &agentruns.Run{ID: "r1", AgentProvider: "anthropic"}

	// queued -> gauge 1
	run.Status = agentruns.StatusQueued
	m.RunStatusChanged(run)
	if v := testutil.ToFloat64(m.queued); v != 1 {
		t.Fatalf("after queue: queued gauge = %v, want 1", v)
	}

	// claimed -> leaves queued (claimed is not a tracked gauge bucket)
	run.Status = agentruns.StatusClaimed
	m.RunStatusChanged(run)
	if v := testutil.ToFloat64(m.queued); v != 0 {
		t.Fatalf("after claim: queued gauge = %v, want 0", v)
	}

	// running -> gauge 1
	run.Status = agentruns.StatusRunning
	m.RunStatusChanged(run)
	if v := testutil.ToFloat64(m.running); v != 1 {
		t.Fatalf("after run: running gauge = %v, want 1", v)
	}

	// succeeded -> running gauge back to 0, counter incremented once
	run.Status = agentruns.StatusSucceeded
	m.RunStatusChanged(run)
	if v := testutil.ToFloat64(m.running); v != 0 {
		t.Fatalf("after finish: running gauge = %v, want 0", v)
	}
	if v := testutil.ToFloat64(m.agentRuns.WithLabelValues("succeeded", "anthropic")); v != 1 {
		t.Fatalf("agent_runs_total{succeeded,anthropic} = %v, want 1", v)
	}

	// A run first seen already terminal (e.g. after a restart) must not push a
	// gauge negative.
	late := &agentruns.Run{ID: "r2", Status: agentruns.StatusFailed, AgentProvider: "openai"}
	m.RunStatusChanged(late)
	if v := testutil.ToFloat64(m.running); v != 0 {
		t.Fatalf("late terminal: running gauge = %v, want 0 (no negative)", v)
	}
	if v := testutil.ToFloat64(m.queued); v != 0 {
		t.Fatalf("late terminal: queued gauge = %v, want 0 (no negative)", v)
	}
	if v := testutil.ToFloat64(m.agentRuns.WithLabelValues("failed", "openai")); v != 1 {
		t.Fatalf("agent_runs_total{failed,openai} = %v, want 1", v)
	}
}

func TestRunWithoutProviderLabelledUnknown(t *testing.T) {
	m := New()
	run := &agentruns.Run{ID: "r3", Status: agentruns.StatusFailed}
	m.RunStatusChanged(run)
	if v := testutil.ToFloat64(m.agentRuns.WithLabelValues("failed", "unknown")); v != 1 {
		t.Fatalf("agent_runs_total{failed,unknown} = %v, want 1", v)
	}
}
