package main

type app struct {
	name  string
	ready bool
}

// open defers a close, which would then run when open returns instead of
// when main does.
func (a *app) open() {
	f := a.name
	defer println("closing", f)
}

// check returns early, skipping the rest of main's statements only in the
// flattened reading.
func (a *app) check() {
	if a.name == "" {
		return
	}
	a.ready = true
}

// guard recovers, which only works in a deferred call.
func (a *app) guard() {
	if r := recover(); r != nil {
		println(r)
	}
}
