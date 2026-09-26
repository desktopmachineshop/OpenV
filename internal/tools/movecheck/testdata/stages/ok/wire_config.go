package main

type app struct {
	name  string
	db    string
	ready bool
	done  bool
}

// config names the app and returns the cleanup main defers.
func (x *app) config() func() {
	x.name = "demo" // the receiver is renamed to the caller's a
	cleanup := func() { println("stopped") }
	return cleanup
}
