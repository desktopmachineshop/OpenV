// Command faithful is ../base as internal/tools/stageextract splits it
// (refactor step M4): main() makes the app, calls the stages of the
// wire_*.go files in order and defers the cleanups they return.
package main

import (
	"fmt"
)

type store struct{ name string }

func open(dsn string) (*store, error) { return &store{name: dsn}, nil }

func (s *store) Close() error { return nil }

func check(n int) error { return nil }

func main() {
	a := &app{}
	stop := a.signals()
	defer stop()
	closeDB := a.connect()
	defer closeDB()
	a.serve()

	fmt.Println("serving", a.port, a.dsn, a.count, a.label)
}
