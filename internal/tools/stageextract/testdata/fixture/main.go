// Command fixture is stageextract's fixture (refactor plan step S14c): a
// main() with the shapes cmd/server's has, split by testdata/fixture.json.
// It prints what it does, so the split program must print the same.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	str "strconv"
)

func main() {
	fmt.Println("start")
	// Root context, and a ticker, both stopped when main returns.
	ctx, cancel := context.WithCancel(context.Background())
	stop := func() {
		fmt.Println("stop")
		cancel()
	}
	tick := newTicker("1h")
	defer stop()
	defer tick.Stop()

	// Settings.
	var dsn string
	if v := os.Getenv("FIXTURE_DSN"); v != "" {
		dsn = v
	} else {
		dsn = "memory"
	}
	port := envOr("FIXTURE_PORT", "8080")
	scratch := port + "/scratch" // used by this range only: it stays a local
	fmt.Println("scratch", scratch)
	limit, err := str.Atoi(envOr("FIXTURE_LIMIT", "3"))
	if err != nil {
		fatal("limit", err)
	}

	// Connect.
	db, err := openStore(dsn)
	if err != nil {
		fatal("connect", err)
	}
	defer db.Close()
	log := newLog()
	defer fmt.Println("deferred with an argument:", port)

	// Services.
	if err := db.Ping(); err != nil {
		fatal("ping", err)
	}
	subs := []string{"first"}
	svc := &service{subs: subs}
	var count = limit * 2
	names := []string{}
	for i := 0; i < limit; i++ {
		names = append(names, fmt.Sprint("n", i))
	}
	lookup := func() string { return db.name + ":" + port }
	svc.Subscribe(lookup())
	label, total := shout(port), count+len(names)
	first, rest := names[0], len(names)-1
	fmt.Println("rest", rest)
	err = errors.New("reset")
	fmt.Println("reset:", err)

	// Jobs.
	const greeting = "hello"
	done := make(chan struct{})
	go func() {
		defer close(done)
		fmt.Fprintln(log, greeting, label, total, first, count, ctx.Err() == nil)
	}()
	<-done
	err = check(total)
	if err != nil {
		fatal("check", err)
	}

	// The tail stays in main.
	fmt.Fprintln(log, "served", svc.subs, names, subs)
	fmt.Print(log.String())
	stop()
}
