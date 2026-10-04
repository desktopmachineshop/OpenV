package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/metrics"
)

// The layer order buildHTTPHandler builds, outermost first (invariant I6):
//
//	SecurityHeaders, BodyLimit, CORS, Compression, RequestLog, metrics, Auth, router
//
// TestBuildHTTPHandlerLayerOrder proves it in process, with no database, on
// the real middleware around the real routes: each subtest says which layer
// it shows wrapping which, by what a request gets back or by the order in
// which the access-log line, the metrics count and the response reach the
// server's side. Three neighbouring pairs commute, so no request can tell
// their order apart and none is asserted: SecurityHeaders and BodyLimit,
// BodyLimit and CORS (none of the three wraps the response writer or reads
// the body), and CORS and Compression (CORS never wraps the writer, and
// Compression has nothing to do to the empty answer CORS gives a preflight).
// The router has no middleware of its own: CORS answers every OPTIONS before
// it, which is why the OPTIONS-only ContentTypeMiddleware it used to carry was
// removed (#379 question 41).
func TestBuildHTTPHandlerLayerOrder(t *testing.T) {
	// Auth inside metrics, inside RequestLog, inside Compression; the router
	// inside Auth.
	t.Run("auth_metrics_log_compression", func(t *testing.T) {
		chain := newTestChain(t)
		const path = "/api/v1/no-such-route"

		// With gzip offered, Compression holds a short response back until
		// the layers inside it return: the access-log line is written, with
		// the request already counted, before the status reaches the server.
		events, rec := chain.serveRecorded(t, http.MethodGet, path, "unmatched", http.Header{"Accept-Encoding": {"gzip"}, "Origin": {testOrigin}})
		want := []string{"log GET " + path + " 401, counted 1", "status 401", "body"}
		if !slices.Equal(events, want) {
			t.Errorf("with gzip offered: events %q, want %q (log and count inside Compression, count inside the log)", events, want)
		}
		if !strings.Contains(rec.Body.String(), "authentication required") {
			t.Errorf("body %q, want auth's refusal, not the router's 404", rec.Body.String())
		}
		assertSecurityHeaders(t, rec.Header())
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
			t.Errorf("Access-Control-Allow-Origin %q, want %q", got, testOrigin)
		}

		// Without gzip, Compression passes the writer through, so the same
		// refusal reaches the server first and is logged after: the hold-back
		// above is Compression's, around the log.
		chain = newTestChain(t)
		events, _ = chain.serveRecorded(t, http.MethodGet, path, "unmatched", nil)
		want = []string{"status 401", "body", "log GET " + path + " 401, counted 1"}
		if !slices.Equal(events, want) {
			t.Errorf("without gzip: events %q, want %q", events, want)
		}
	})

	// The router inside Auth, metrics and RequestLog: it answers what Auth
	// lets through.
	t.Run("router", func(t *testing.T) {
		chain := newTestChain(t)
		const path = "/api/v1/public/no-such-route"
		events, rec := chain.serveRecorded(t, http.MethodGet, path, "unmatched", nil)
		if rec.Code != http.StatusNotFound || rec.Body.String() != "404 page not found\n" {
			t.Errorf("open unknown path: %d %q, want gorilla's 404", rec.Code, rec.Body.String())
		}
		want := []string{"status 404", "body", "log GET " + path + " 404, counted 1"}
		if !slices.Equal(events, want) {
			t.Errorf("events %q, want %q", events, want)
		}
		assertSecurityHeaders(t, rec.Header())
	})

	// /metrics is a route of the router, open past Auth, counted and logged.
	t.Run("metrics_route", func(t *testing.T) {
		chain := newTestChain(t)
		events, rec := chain.serveRecorded(t, http.MethodGet, "/metrics", "/metrics", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "go_goroutines") {
			t.Fatalf("GET /metrics with no credentials: %d %.200q, want 200 and the scrape", rec.Code, rec.Body.String())
		}
		if want := "log GET /metrics 200, counted 1"; len(events) < 3 || events[0] != "status 200" || events[len(events)-1] != want {
			t.Errorf("events %q, want the status, the body and %q last", events, want)
		}
		assertSecurityHeaders(t, rec.Header())
	})

	// CORS outside RequestLog, metrics, Auth and the router: it answers a
	// preflight that none of them sees. SecurityHeaders outside CORS: that
	// answer has its headers.
	t.Run("preflight", func(t *testing.T) {
		chain := newTestChain(t)
		events, rec := chain.serveRecorded(t, http.MethodOptions, "/api/v1/projects", "unmatched", http.Header{
			"Origin":                        {testOrigin},
			"Access-Control-Request-Method": {"POST"},
			"Accept-Encoding":               {"gzip"},
		})
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Errorf("preflight: %d %q, want an empty 200", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
			t.Errorf("preflight Access-Control-Allow-Origin %q, want %q", got, testOrigin)
		}
		if want := []string{"status 200"}; !slices.Equal(events, want) {
			t.Errorf("preflight events %q, want %q (not logged)", events, want)
		}
		if scrape := chain.scrape(t); strings.Contains(scrape, "http_requests_total{") {
			t.Errorf("preflight counted by metrics:\n%s", scrape)
		}
		assertSecurityHeaders(t, rec.Header())
	})

	// BodyLimit outside Compression, RequestLog and metrics: its reader holds
	// the server's own response writer.
	t.Run("body_limit", func(t *testing.T) {
		chain := newTestChain(t) // OPENV_MAX_BODY_MB=1
		srv := httptest.NewServer(chain.handler)
		defer srv.Close()

		// A body past the cap ends in a MaxBytesError, and net/http closes
		// the connection after the answer only when BodyLimit's reader holds
		// the server's own response writer, not a wrapper of Compression
		// (gzip is offered), RequestLog or metrics. The JSON is unterminated,
		// so a body under the cap, read to its end, is refused too, and the
		// connection stays open.
		for _, tc := range []struct {
			name      string
			size      int
			wantClose bool
		}{
			{"over the cap", 1 << 20, true},
			{"under the cap", 1<<20 - 1024, false},
		} {
			body := `{"email":"` + strings.Repeat("a", tc.size)
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Encoding", "gzip")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest || resp.Close != tc.wantClose {
				t.Errorf("%s (%d bytes): %d, connection closed %v; want 400, closed %v",
					tc.name, len(body), resp.StatusCode, resp.Close, tc.wantClose)
			}
		}
	})

	// A refused CORS_ORIGIN is returned before the settings read after it,
	// which a valid one reads in order.
	t.Run("cors_origin_refused", func(t *testing.T) {
		// envparse warns once per variable and value, so each run has values
		// of its own.
		nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
		t.Setenv("SECURE_COOKIES", "yes-"+nonce)
		t.Setenv("CROSS_SITE_COOKIES", "yes-"+nonce)
		t.Setenv("OPENV_MAX_BODY_MB", "32MB-"+nonce)
		logs := captureLog(t)
		build := func() (http.Handler, error) {
			return buildHTTPHandler(api.NewHandler(api.HandlerDeps{}), metrics.New(), nil, nil, nil, nil, "", func() string { return "" }, "", users.EmailVerificationPolicy{})
		}

		t.Setenv("CORS_ORIGIN", "*")
		h, err := build()
		if !errors.Is(err, api.ErrCORSOriginNotAllowed) || h != nil {
			t.Fatalf("CORS_ORIGIN=*: %v, %v; want no handler and api.ErrCORSOriginNotAllowed", h, err)
		}
		if got := logs.malformed(); len(got) != 0 {
			t.Errorf("CORS_ORIGIN=*: settings read after the refusal: %q", got)
		}

		t.Setenv("CORS_ORIGIN", testOrigin)
		if _, err := build(); err != nil {
			t.Fatal(err)
		}
		want := []string{"SECURE_COOKIES", "CROSS_SITE_COOKIES", "OPENV_MAX_BODY_MB"}
		if got := logs.malformed(); !slices.Equal(got, want) {
			t.Errorf("valid CORS_ORIGIN: malformed settings read %q, want %q", got, want)
		}
	})
}

const testOrigin = "http://frontend.test"

// testChain is buildHTTPHandler's result around a handler with no services
// (no request here reaches one), with its own metrics and a log recorder.
type testChain struct {
	handler http.Handler
	metrics *metrics.Metrics
	log     *logRecorder
}

func newTestChain(t *testing.T) *testChain {
	t.Helper()
	t.Setenv("CORS_ORIGIN", testOrigin)
	t.Setenv("OPENV_MAX_BODY_MB", "1")
	t.Setenv("OPENV_METRICS_TOKEN", "")
	t.Setenv("SECURE_COOKIES", "")
	t.Setenv("CROSS_SITE_COOKIES", "")
	c := &testChain{metrics: metrics.New(), log: captureLog(t)}
	h, err := buildHTTPHandler(api.NewHandler(api.HandlerDeps{}), c.metrics, nil, nil, nil, nil, "", func() string { return "" }, "", users.EmailVerificationPolicy{})
	if err != nil {
		t.Fatalf("buildHTTPHandler: %v", err)
	}
	c.handler = h
	return c
}

// serveRecorded serves one request in process and returns, in the order they
// happened, the access-log lines (with how many times metrics had counted
// that request, under route, when the line was written) and the status and
// body writes that reached the server's side.
func (c *testChain) serveRecorded(t *testing.T, method, path, route string, header http.Header) ([]string, *httptest.ResponseRecorder) {
	t.Helper()
	var events []string
	c.log.setOnRequest(func(method, path string, status int64) {
		n := c.counted(t, method, route, strconv.FormatInt(status, 10))
		events = append(events, fmt.Sprintf("log %s %s %d, counted %d", method, path, status, n))
	})
	defer c.log.setOnRequest(nil)
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(eventWriter{rec, &events}, req)
	return events, rec
}

// scrape is what metrics exposes, read from its own handler, not through the
// chain.
func (c *testChain) scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c.metrics.Handler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

// counted is metrics' http_requests_total for one label set.
func (c *testChain) counted(t *testing.T, method, route, status string) int {
	t.Helper()
	prefix := fmt.Sprintf("http_requests_total{method=%q,route=%q,status=%q} ", method, route, status)
	for _, line := range strings.Split(c.scrape(t), "\n") {
		if v, ok := strings.CutPrefix(line, prefix); ok {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				t.Fatalf("metrics line %q: %v", line, err)
			}
			return int(n)
		}
	}
	return 0
}

// eventWriter is the server's side of a response: it records when the status
// and each body write reach it.
type eventWriter struct {
	*httptest.ResponseRecorder
	events *[]string
}

func (w eventWriter) WriteHeader(code int) {
	*w.events = append(*w.events, fmt.Sprintf("status %d", code))
	w.ResponseRecorder.WriteHeader(code)
}

func (w eventWriter) Write(p []byte) (int, error) {
	if !w.headerSent() {
		w.WriteHeader(http.StatusOK)
	}
	*w.events = append(*w.events, "body")
	return w.ResponseRecorder.Write(p)
}

func (w eventWriter) headerSent() bool {
	for _, e := range *w.events {
		if strings.HasPrefix(e, "status ") {
			return true
		}
	}
	return false
}

// logRecorder is the slog default while a test runs. It hands each access-log
// line to onRequest as it is written, and keeps the variables of the
// malformed-setting warnings.
type logRecorder struct {
	mu        sync.Mutex
	warned    []string
	onRequest func(method, path string, status int64)
}

func (l *logRecorder) setOnRequest(f func(method, path string, status int64)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onRequest = f
}

// captureLog installs a logRecorder as the slog default until the test ends.
// slog.SetDefault also points the log package at it, and setting the old
// default back does not undo that, so both are restored.
func captureLog(t *testing.T) *logRecorder {
	t.Helper()
	l := &logRecorder{}
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(l))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return l
}

func (l *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecorder) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logRecorder) WithGroup(string) slog.Handler            { return l }

func (l *logRecorder) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value
		return true
	})
	switch r.Message {
	case "http request":
		l.mu.Lock()
		onRequest := l.onRequest
		l.mu.Unlock()
		if onRequest != nil {
			onRequest(attrs["method"].String(), attrs["path"].String(), attrs["status"].Int64())
		}
	case "ignoring a malformed setting; its default applies":
		l.mu.Lock()
		l.warned = append(l.warned, attrs["var"].String())
		l.mu.Unlock()
	}
	return nil
}

// malformed lists the variables warned about so far, in order.
func (l *logRecorder) malformed() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warned...)
}

func assertSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	for k, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s %q, want %q", k, got, want)
		}
	}
}
