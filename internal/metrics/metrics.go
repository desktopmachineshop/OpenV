// Package metrics exposes a Prometheus /metrics endpoint for the platform:
// HTTP request counters/latency, agent-run lifecycle counters and gauges, and
// SSE connection counts. Metric cardinality is deliberately bounded — routes
// collapse to their mux path template (never the raw path), and only low-
// cardinality labels (method, status, run status, provider) are used. There
// are no per-project, per-user, or per-agent labels.
package metrics

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// Metrics owns a private Prometheus registry and the platform collectors. It
// implements agentruns.Subscriber so run lifecycle transitions update the run
// counter and the queued/running gauges.
type Metrics struct {
	reg *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	agentRuns    *prometheus.CounterVec

	queued  prometheus.Gauge
	running prometheus.Gauge

	// lastState tracks the last-observed status of in-flight runs so a
	// transition can decrement the gauge bucket the run is leaving. Runs are
	// dropped once they reach a terminal (or awaiting_approval) status, so the
	// map is bounded by the number of live runs. Only states this process
	// itself recorded are ever decremented, so the gauges can never go
	// negative across a restart — at worst they under-count runs that were
	// already in flight at boot, self-healing as those runs cycle.
	mu        sync.Mutex
	lastState map[string]string
}

var _ agentruns.Subscriber = (*Metrics)(nil)

// New builds the registry and registers all collectors, including the standard
// Go runtime and process collectors.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg:       reg,
		lastState: map[string]string{},
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method, route template, and status code.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds by method, route template, and status code.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		agentRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_runs_total",
			Help: "Agent runs that reached a terminal status, by status and provider.",
		}, []string{"status", "provider"}),
		queued: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "agent_runs_queued",
			Help: "Agent runs currently queued, observed via status transitions.",
		}),
		running: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "agent_runs_running",
			Help: "Agent runs currently running, observed via status transitions.",
		}),
	}
	reg.MustRegister(m.httpRequests, m.httpDuration, m.agentRuns, m.queued, m.running)
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

// Handler returns the /metrics HTTP handler. When token is non-empty the
// handler requires an "Authorization: Bearer <token>" header (compared in
// constant time); when empty the endpoint is unauthenticated and production
// deployments should firewall it to an internal network. Either way it must
// NOT be placed behind the session-auth middleware, which would break scraping.
func (m *Metrics) Handler(token string) http.Handler {
	h := promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
	if token == "" {
		return h
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// WatchSSEConnections registers a gauge that reports the current SSE listener
// count by calling fn on each scrape.
func (m *Metrics) WatchSSEConnections(fn func() int) {
	m.reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sse_active_connections",
		Help: "Currently connected SSE listener channels.",
	}, func() float64 { return float64(fn()) }))
}

// metricsRecorder captures the response status and forwards Flush so wrapped
// SSE streams keep working.
type metricsRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rec *metricsRecorder) WriteHeader(status int) {
	if !rec.wroteHeader {
		rec.status = status
		rec.wroteHeader = true
	}
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *metricsRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware returns middleware that records request count and latency
// labelled by method, route template, and status. It resolves the route via
// the mux router's matcher (not the raw path) so dynamic segments collapse to
// the template and cardinality stays bounded; unmatched requests are labelled
// "unmatched". It is a separate concern from RequestLogMiddleware and does not
// double-count.
func (m *Metrics) HTTPMiddleware(router *mux.Router) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &metricsRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			route := routeTemplate(router, r)
			status := strconv.Itoa(rec.status)
			m.httpRequests.WithLabelValues(r.Method, route, status).Inc()
			m.httpDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
		})
	}
}

// routeTemplate returns the mux path template that matches r (e.g.
// "/api/v1/projects/{id}"), or "unmatched" when no route matches. Using the
// template rather than r.URL.Path keeps the "route" label cardinality bounded
// by the number of registered routes.
func routeTemplate(router *mux.Router, r *http.Request) string {
	var match mux.RouteMatch
	if router.Match(r, &match) && match.Route != nil {
		if t, err := match.Route.GetPathTemplate(); err == nil {
			return t
		}
	}
	return "unmatched"
}

// RunLogsAppended satisfies agentruns.Subscriber; log volume is not metered.
func (m *Metrics) RunLogsAppended(_ *agentruns.Run, _ []agentruns.LogEntry) {}

// RunStatusChanged updates the queued/running gauges on every transition and
// increments agent_runs_total once a run reaches a terminal status.
func (m *Metrics) RunStatusChanged(run *agentruns.Run) {
	next := run.Status

	m.mu.Lock()
	prev := m.lastState[run.ID]
	if prev != next {
		switch prev {
		case agentruns.StatusQueued:
			m.queued.Dec()
		case agentruns.StatusRunning:
			m.running.Dec()
		}
		switch next {
		case agentruns.StatusQueued:
			m.queued.Inc()
		case agentruns.StatusRunning:
			m.running.Inc()
		}
	}
	if isTracked(next) {
		m.lastState[run.ID] = next
	} else {
		// Terminal or awaiting_approval: stop tracking so the map stays bounded.
		delete(m.lastState, run.ID)
	}
	m.mu.Unlock()

	if terminalStatus[next] {
		provider := run.AgentProvider
		if provider == "" {
			provider = "unknown"
		}
		m.agentRuns.WithLabelValues(next, provider).Inc()
	}
}

// terminalStatus is the set of statuses that count as a finished run.
var terminalStatus = map[string]bool{
	agentruns.StatusSucceeded: true,
	agentruns.StatusFailed:    true,
	agentruns.StatusCancelled: true,
	agentruns.StatusTimedOut:  true,
}

// isTracked reports whether a run in this status should stay in lastState. Only
// pre-terminal states are tracked; awaiting_approval is an absorbing state that
// resolves through a later terminal transition, so it is dropped like a
// terminal status.
func isTracked(status string) bool {
	if terminalStatus[status] || status == agentruns.StatusAwaitingApproval {
		return false
	}
	return true
}
