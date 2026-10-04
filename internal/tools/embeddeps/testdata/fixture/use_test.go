package api

import "testing"

type fakeUsers struct{ Users }

func (fakeUsers) Name(id string) string { return "user " + id + ";" }

type fakeStore struct{ Store }

func (fakeStore) Get(id string) string { return "item " + id + ";" }

func newTestHandler(t *testing.T, opts ...func(*Handler)) *Handler {
	t.Helper()
	h := &Handler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func TestNewHandler(t *testing.T) {
	b := &Billing{}
	h := NewHandler(HandlerDeps{Store: fakeStore{}, UserService: fakeUsers{}, FrontendURL: "https://app.example/",
		BillingService: b, BuildSHA: "abc;", CrossSiteCookies: true})
	want := "user 1;item 1;abc;abc;item 1;https://app.example"
	if got := h.Lookup("1"); got != want {
		t.Errorf("Lookup = %q, want %q", got, want)
	}
	if b.ReturnURL != "https://app.example" || !h.secureCookies || h.limiter.n != 3 {
		t.Errorf("derived values: return URL %q, secure %v, limiter %v", b.ReturnURL, h.secureCookies, h.limiter)
	}
	s := &server{h: h, store: fakeStore{}}
	if got := s.both("2"); got != "item 2;item 2;abc;" {
		t.Errorf("both = %q", got)
	}
}

func TestOptions(t *testing.T) {
	h := newTestHandler(t, func(h *Handler) { h.userService = fakeUsers{} })
	h.store = fakeStore{}
	m := &AuthMiddleware{userService: fakeUsers{}}
	m.store = h.store
	if got := m.Who("3") + h.userService.Name("4"); got != "user 3;item 3;user 4;" {
		t.Errorf("got %q", got)
	}
}
