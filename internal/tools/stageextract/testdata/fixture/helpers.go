package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The helpers main() calls, outside main.go so that main.go does not
// import bytes: a field of the bytes.Buffer type makes app.go import it.

func fatal(msg string, err error) {
	fmt.Println("fatal:", msg, err)
	os.Exit(1)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type store struct{ name string }

func openStore(dsn string) (*store, error) {
	if dsn == "" {
		return nil, errors.New("no dsn")
	}
	fmt.Println("open", dsn)
	return &store{name: dsn}, nil
}

func (s *store) Close() error { fmt.Println("close", s.name); return nil }

func (s *store) Ping() error { fmt.Println("ping", s.name); return nil }

type service struct{ subs []string }

func (s *service) Subscribe(name string) {
	s.subs = append(s.subs, name)
	fmt.Println("subscribe", name)
}

type ticker struct{ every string }

func newTicker(every string) *ticker { return &ticker{every: every} }

func (t *ticker) Stop() { fmt.Println("ticker stopped", t.every) }

func newLog() *bytes.Buffer { return &bytes.Buffer{} }

func check(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	fmt.Println("checked", n)
	return nil
}

func shout(s string) string { return strings.ToUpper(s) + "!" }
