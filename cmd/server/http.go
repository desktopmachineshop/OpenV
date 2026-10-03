package main

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/metrics"
)

// maxRequestBodyBytes is the cap the API places on any single request body.
// OPENV_MAX_BODY_MB overrides the 32 MB default; attachment uploads carry a
// tighter cap of their own (OPENV_MAX_UPLOAD_MB).
func maxRequestBodyBytes() int64 {
	return int64(envInt("OPENV_MAX_BODY_MB", 32)) * 1024 * 1024
}

// buildHTTPHandler builds the API's handler: the router with every route,
// wrapped, outermost first, in SecurityHeaders, BodyLimit, CORS,
// Compression, RequestLog, metrics and Auth (invariant I6, which
// http_test.go pins). It returns api.CORSMiddleware's error for a
// CORS_ORIGIN that would reflect any origin, before it reads the settings
// after that one, and main exits on it.
func buildHTTPHandler(
	handler *api.Handler,
	metricsCollector *metrics.Metrics,
	userService *users.DefaultService,
	runService *agentruns.DefaultService,
	orgService *orgs.DefaultService,
	workerKeyService *workerkeys.DefaultService,
	workerKey string,
	bootstrapOrgID func() string,
	runnerPoolKey string,
	emailVerification users.EmailVerificationPolicy,
) (http.Handler, error) {
	// Router + middleware.
	router := mux.NewRouter()
	router.Use(api.ContentTypeMiddleware)
	handler.RegisterRoutes(router)

	// Prometheus scrape endpoint. Unauthenticated by default (firewall it to an
	// internal network in production); set OPENV_METRICS_TOKEN to require an
	// "Authorization: Bearer <token>" header. Registered as an open path in the
	// auth middleware so scraping is never blocked by session auth.
	router.Handle("/metrics", metricsCollector.Handler(envSecret("OPENV_METRICS_TOKEN", ""))).Methods("GET")

	authMiddleware := api.NewAuthMiddleware(userService, runService, orgService, workerKeyService, workerKey, bootstrapOrgID)
	authMiddleware.SetPoolKey(runnerPoolKey)
	authMiddleware.SetEmailVerificationPolicy(emailVerification)
	// Request logging wraps outside auth so rejected requests are logged too;
	// auth annotates the log line with the resolved org/user. The metrics HTTP
	// middleware sits between them, recording every request (including rejected
	// ones) labelled by mux route template; it is a distinct concern from the
	// access log and does not double-count.
	protected := api.RequestLogMiddleware(
		metricsCollector.HTTPMiddleware(router)(authMiddleware.Wrap(router)),
	)

	// Compression sits outside all of that so it sees the finished response,
	// whichever layer produced it. It leaves event streams alone — the API
	// holds SSE connections open for minutes and a compressor would batch
	// their events instead of delivering them — and skips anything too small
	// to be worth the header. The payload it exists for is a baseline
	// snapshot: a whole project export, which is close to a megabyte of JSON
	// for a real project and about a fifth of that compressed.
	protected = api.CompressionMiddleware(protected)

	// CORS: restricted to the configured frontend origin, with credentials.
	// A wildcard is refused at startup rather than reflected (see
	// api.CORSMiddleware).
	corsHandler, err := api.CORSMiddleware(envOr("CORS_ORIGIN", "http://localhost:3000"), protected)
	if err != nil {
		return nil, err
	}
	// Outermost: cap every request body, then stamp the browser-hardening
	// headers on every response, including CORS preflights and rejections.
	// HSTS follows SECURE_COOKIES, the deployment's declaration that it is
	// only reached over TLS.
	secureDeployment := envBool("SECURE_COOKIES", false) || envBool("CROSS_SITE_COOKIES", false)
	rootHandler := api.SecurityHeadersMiddleware(secureDeployment)(
		api.BodyLimitMiddleware(maxRequestBodyBytes())(corsHandler),
	)
	return rootHandler, nil
}

// newServer is the API's HTTP server: rootHandler on port, with every
// request's context derived from ctx, the signal context.
func newServer(ctx context.Context, port string, rootHandler http.Handler) *http.Server {
	// HTTP server. ReadHeaderTimeout defends against slowloris-style clients
	// holding connections open while trickling headers; IdleTimeout reclaims
	// idle keep-alive connections. ReadTimeout and WriteTimeout deliberately
	// stay 0 (unlimited): the API serves long-lived SSE streams (e.g.
	// /api/v1/agent-runs/{id}/stream, guided chat and interview streams)
	// that hold a response open indefinitely, and a nonzero WriteTimeout
	// is an absolute
	// deadline that would sever every stream after it elapsed. Slow-client
	// abuse on the read side is already bounded by ReadHeaderTimeout plus
	// per-handler request parsing.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Derive request contexts from the signal context so long-lived SSE
		// handlers (which select on r.Context().Done()) exit promptly on
		// shutdown instead of pinning the drain for its full timeout.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	return srv
}
