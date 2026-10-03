// Command base is movecheck's fixture of a main() before refactor step M4
// splits it into stages; ../faithful is the same program as
// internal/tools/stageextract splits it.
package main

import (
	"context"
	"fmt"
	"os"
)

type store struct{ name string }

func open(dsn string) (*store, error) { return &store{name: dsn}, nil }

func (s *store) Close() error { return nil }

func check(n int) error { return nil }

func main() {
	// Root context.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// Connect.
	var dsn string
	if v := os.Getenv("DSN"); v != "" {
		dsn = v
	} else {
		dsn = "memory"
	}
	port := "8080"
	db, err := open(dsn)
	if err != nil {
		os.Exit(1)
	}
	defer db.Close()

	// Serve.
	var count = len(port)
	label, extra := fmt.Sprint(port, count), 1
	fmt.Println("extra", extra)
	err = check(count)
	if err != nil {
		os.Exit(1)
	}
	go func() { fmt.Println(label, ctx.Err(), db.name) }()

	fmt.Println("serving", port, dsn, count, label)
}
