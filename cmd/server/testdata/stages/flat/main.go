// Package main is a synthetic stand-in for cmd/server's main(), read (never
// built) by TestBootStepsFollowStages in cmd/server/boot_steps_test.go. It
// has the shapes main() has today, flat: a root context and its deferred
// stop, a fatal helper after error checks, a deferred db.Close, setters and
// subscriptions, a closure called in an if statement, a setter chain ending
// in Start, a purge goroutine with a ticker, the HTTP chain with its CORS
// error, the listen goroutine and the serve-and-shutdown select.
// ../staged holds the same program as M2, M3 and M4 would leave it.
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

type hub struct{}

func (h *hub) Subscribe(name string) {}

type service struct{}

func (s *service) SetHub(h *hub)                  {}
func (s *service) SetOwnerLookup(f func() string) {}
func (s *service) AddSubscriber(h *hub)           {}
func (s *service) Purge() ([]string, error)       { return nil, nil }
func (s *service) Sweep() error                   { return nil }

type notifier struct{}

func newNotifier(s *service) *notifier        { return &notifier{} }
func (n *notifier) SetMailer(m any) *notifier { return n }
func (n *notifier) Start(h *hub)              {}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port := envOr("PORT", "8080")
	db, err := sql.Open("postgres", envOr("DSN", ""))
	if err != nil {
		fatal("failed to open the database", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fatal("failed to reach the database", err)
	}
	h := &hub{}
	svc := &service{}
	svc.SetHub(h)
	lookup := func() string {
		var id string
		if err := db.QueryRow(`SELECT 1`).Scan(&id); err != nil {
			return ""
		}
		return id
	}
	svc.SetOwnerLookup(lookup)
	if owner := lookup(); owner != "" {
		slog.Info("owner", "id", owner)
	}
	svc.AddSubscriber(h)
	h.Subscribe("hooks")
	newNotifier(svc).SetMailer(nil).Start(h)

	go func() {
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
	}()

	router := http.NewServeMux()
	router.Handle("/health", http.NotFoundHandler())
	origin, err := url.Parse(envOr("CORS_ORIGIN", "http://localhost:3000"))
	if err != nil {
		fatal("invalid CORS_ORIGIN", err)
	}
	root := http.MaxBytesHandler(http.StripPrefix(origin.Path, router), 32<<20)

	srv := &http.Server{Addr: ":" + port, Handler: root, BaseContext: func(net.Listener) context.Context { return ctx }}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting server", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			fatal("failed to start server", err)
		}
	case <-ctx.Done():
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
		}
		<-errCh
	}
}
