package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	a := &app{}
	stop := a.signals()
	defer stop()
	a.config()
	closeDB := a.connect()
	defer closeDB()
	a.storage()
	a.core()
	a.workspace()
	a.runners()
	a.projects()
	a.agents()
	a.realtime()
	a.notify()
	a.release()
	a.jobs()
	a.sso()
	a.billing()
	a.handlers()
	a.server()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting server", "port", a.port)
		if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	case <-a.ctx.Done():
		a.stop() // restore default signal behavior: a second Ctrl-C kills immediately
		slog.Info("shutdown signal received; draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("graceful shutdown incomplete", "error", err)
			_ = a.srv.Close()
		}
		<-errCh // wait for ListenAndServe to return
		slog.Info("server stopped")
	}
}
