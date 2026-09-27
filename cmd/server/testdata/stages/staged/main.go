// Package main is ../flat as M2, M3 and M4 would leave it, read (never built)
// by TestBootStepsFollowStages in cmd/server/boot_steps_test.go: locals are
// fields of app, main() calls the a.<stage>() methods of wire_x.go in the
// flat program's order, the stages that open a resource return its cleanup
// for main() to defer at the same point and open it through a helper that
// returns the fallible call (M1), the HTTP chain is a helper that returns
// the CORS error for main() to call fatal on (M2), and the closures and
// goroutine bodies are named functions (M3).
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
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

type app struct {
	ctx    context.Context
	stop   context.CancelFunc
	port   string
	db     *sql.DB
	hub    *hub
	svc    *service
	lookup func() string
	root   http.Handler
	srv    *http.Server
	errCh  chan error
}

func main() {
	a := &app{}
	stop := a.signals()
	defer stop()
	closeDB := a.storage()
	defer closeDB()
	a.core()
	a.jobs()
	var err error
	a.root, err = buildHTTPHandler()
	if err != nil {
		fatal("invalid CORS_ORIGIN", err)
	}
	a.serve()

	select {
	case err := <-a.errCh:
		if err != nil {
			fatal("failed to start server", err)
		}
	case <-a.ctx.Done():
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.srv.Shutdown(shutdownCtx); err != nil {
			_ = a.srv.Close()
		}
		<-a.errCh
	}
}
