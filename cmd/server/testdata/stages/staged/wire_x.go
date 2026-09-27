package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func (a *app) signals() context.CancelFunc {
	a.ctx, a.stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return a.stop
}

func (a *app) storage() func() error {
	a.port = envOr("PORT", "8080")
	var err error
	a.db, err = openDB(envOr("DSN", ""))
	if err != nil {
		fatal("failed to open the database", err)
	}
	return a.db.Close
}

func (a *app) core() {
	if err := a.db.Ping(); err != nil {
		fatal("failed to reach the database", err)
	}
	a.hub = &hub{}
	a.svc = &service{}
	a.svc.SetHub(a.hub)
	a.lookup = ownerLookup(a.db)
	a.svc.SetOwnerLookup(a.lookup)
	if owner := a.lookup(); owner != "" {
		slog.Info("owner", "id", owner)
	}
	a.svc.AddSubscriber(a.hub)
	a.hub.Subscribe("hooks")
	newNotifier(a.svc).SetMailer(nil).Start(a.hub)
}

func (a *app) jobs() {
	go runPurgeLoop(a.ctx, a.svc)
}

func (a *app) serve() {
	a.srv = newServer(a.ctx, a.port, a.root)
	a.errCh = make(chan error, 1)
	go listen(a.srv, a.port, a.errCh)
}

// openDB is a helper that returns the fallible call main() made in place.
func openDB(dsn string) (*sql.DB, error) {
	return sql.Open("postgres", dsn)
}

// ownerLookup is M3's named form of the closure main() built in place.
func ownerLookup(db *sql.DB) func() string {
	return func() string {
		var id string
		if err := db.QueryRow(`SELECT 1`).Scan(&id); err != nil {
			return ""
		}
		return id
	}
}

// runPurgeLoop is M3's named goroutine body, started with go where the
// function literal was.
func runPurgeLoop(ctx context.Context, svc *service) {
	purge := func() {
		if ids, err := svc.Purge(); err != nil {
			slog.Error("purge failed", "error", err, "purged", len(ids))
		}
	}
	purge()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
			_ = svc.Sweep()
		}
	}
}

// buildHTTPHandler is M2's helper: it returns the CORS error, and main()
// calls fatal on it at the same statement.
func buildHTTPHandler() (http.Handler, error) {
	router := http.NewServeMux()
	router.Handle("/health", http.NotFoundHandler())
	origin, err := url.Parse(envOr("CORS_ORIGIN", "http://localhost:3000"))
	if err != nil {
		return nil, err
	}
	return http.MaxBytesHandler(http.StripPrefix(origin.Path, router), 32<<20), nil
}

func newServer(ctx context.Context, port string, root http.Handler) *http.Server {
	return &http.Server{Addr: ":" + port, Handler: root, BaseContext: func(net.Listener) context.Context { return ctx }}
}

func listen(srv *http.Server, port string, errCh chan<- error) {
	slog.Info("starting server", "port", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
		return
	}
	errCh <- nil
}
