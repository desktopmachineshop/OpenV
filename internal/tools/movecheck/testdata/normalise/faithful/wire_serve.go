package main

import (
	"fmt"
	"os"
)

// serve checks the count and starts the server's goroutine.
func (a *app) serve() {
	// Serve.
	a.count = len(a.port)
	var extra int
	a.label, extra = fmt.Sprint(a.port, a.count), 1
	fmt.Println("extra", extra)
	var err error
	err = check(a.count)
	if err != nil {
		os.Exit(1)
	}
	go func() { fmt.Println(a.label, a.ctx.Err(), a.db.name) }()
}
