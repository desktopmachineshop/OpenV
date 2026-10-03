package api

import "testing"

// newTestHandler builds the Handler a test drives: a zero Handler with each
// option applied in order. It is the one place a test builds a Handler (K6;
// archtest's handler_literals_in_tests ratchet counts any other literal), so
// a new dependency is one option, not an edit to every test.
//
// It keeps exactly what a &Handler{...} literal gave: unlike NewHandler it
// creates no rate limiters, derives no values (cookie SameSite, the trimmed
// frontend URL), reads no environment and leaves billing unwired. An option
// sets the fields a test needs:
//
//	h := newTestHandler(t, func(h *Handler) { h.exportService = fake })
func newTestHandler(t testing.TB, opts ...func(*Handler)) *Handler {
	t.Helper()
	h := &Handler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}
