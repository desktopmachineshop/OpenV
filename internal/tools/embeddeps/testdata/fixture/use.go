package api

// server holds a handler in a field, beside a field of its own named like
// one of Handler's.
type server struct {
	h     *Handler
	store Store
}

// Lookup reads the copied fields through the receiver, a local, a
// dereference, a copy and a closure.
func (h *Handler) Lookup(id string) string {
	name := h.userService.Name(id)
	get := h.store.Get
	hh := h
	v := *h
	f := func() string { return hh.buildSHA + v.buildSHA }
	return name + get(id) + f() + (*h).store.Get(id) + h.frontendURL
}

// sha reads one through a value receiver.
func (h Handler) sha() string { return h.buildSHA }

func (s *server) both(id string) string {
	return s.h.store.Get(id) + s.store.Get(id) + s.h.sha()
}
