package main

import (
	"context"
	"os"
)

// signals makes the root context and returns its stop for main() to defer.
func (a *app) signals() context.CancelFunc {
	// Root context.
	a.ctx, a.stop = context.WithCancel(context.Background())
	return a.stop
}

// connect opens the store and returns its Close for main() to defer.
func (a *app) connect() func() error {
	// Connect.
	if v := os.Getenv("DSN"); v != "" {
		a.dsn = v
	} else {
		a.dsn = "memory"
	}
	a.port = "8080"
	var err error
	a.db, err = open(a.dsn)
	if err != nil {
		os.Exit(1)
	}
	return a.db.Close
}
