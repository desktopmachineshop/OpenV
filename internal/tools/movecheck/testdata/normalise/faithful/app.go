package main

import (
	"context"
)

// app holds the locals of main() that more than one of its stages uses.
type app struct {
	ctx   context.Context
	stop  context.CancelFunc
	dsn   string
	port  string
	db    *store
	count int
	label string
}
