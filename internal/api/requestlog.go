package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// ctxReqMeta holds a *requestMeta the logging middleware injects before auth
// runs. The auth middleware fills in the resolved identity so the access log
// can attribute the request even though logging wraps outside of auth.
const ctxReqMeta contextKey = "openv-request-meta"

// requestMeta is mutable per-request attribution shared between the logging
// and auth middlewares.
type requestMeta struct {
	orgID  string
	userID string
	actor  string // "user", "worker", or "run" — how the request authenticated
}

// metaFrom returns the request's meta holder, or nil outside the logging
// middleware (e.g. in tests that exercise handlers directly).
func metaFrom(ctx context.Context) *requestMeta {
	m, _ := ctx.Value(ctxReqMeta).(*requestMeta)
	return m
}

// statusRecorder captures the response status for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// Flush forwards to the underlying writer so SSE streaming keeps working
// through the recorder.
func (rec *statusRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// RequestLogMiddleware emits one structured log line per request: method,
// path, status, duration, and org/user attribution when downstream middleware
// resolved it. Wire it outermost (around auth) so rejected requests are
// logged too.
func RequestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		meta := &requestMeta{}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), ctxReqMeta, meta)))

		attrs := []any{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		}
		if meta.orgID != "" {
			attrs = append(attrs, slog.String("org_id", meta.orgID))
		}
		if meta.userID != "" {
			attrs = append(attrs, slog.String("user_id", meta.userID))
		}
		if meta.actor != "" {
			attrs = append(attrs, slog.String("auth", meta.actor))
		}
		switch {
		case rec.status >= 500:
			slog.Error("http request", attrs...)
		case rec.status >= 400:
			slog.Warn("http request", attrs...)
		case r.URL.Path == "/health":
			// Health probes fire constantly; keep them out of INFO.
			slog.Debug("http request", attrs...)
		default:
			slog.Info("http request", attrs...)
		}
	})
}
