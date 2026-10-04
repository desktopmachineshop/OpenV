package api

import (
	"net/http"
	"strings"
)

// Users is a stand-in service.
type Users interface{ Name(id string) string }

// Store is another.
type Store interface{ Get(id string) string }

// Billing stands in for a service NewHandler rewires.
type Billing struct{ ReturnURL string }

// DefaultReturnURL records the return origin.
func (b *Billing) DefaultReturnURL(u string) { b.ReturnURL = u }

// HandlerDeps carries every dependency.
type HandlerDeps struct {
	Store       Store
	UserService Users
	// FrontendURL is trimmed into a private field.
	FrontendURL      string
	SecureCookies    bool
	CrossSiteCookies bool
	BillingService   *Billing
	BuildSHA         string
}

// Handler holds the dependencies.
type Handler struct {
	store Store
	// userService has a doc comment, which goes with the field.
	userService   Users
	frontendURL   string // derived: trimmed
	secureCookies bool
	sameSite      http.SameSite

	limiter *limiter // derived: built here
	// billing copies BillingService under a name of its own.
	billing  *Billing
	buildSHA string // a line comment, which goes with the field
}

type limiter struct{ n int }

// NewHandler creates a handler.
func NewHandler(deps HandlerDeps) *Handler {
	sameSite := http.SameSiteLaxMode
	secure := deps.SecureCookies
	if deps.CrossSiteCookies {
		sameSite = http.SameSiteNoneMode
		secure = true
	}
	h := &Handler{
		store:       deps.Store,
		userService: deps.UserService,
		frontendURL: strings.TrimRight(deps.FrontendURL, "/"),
		// secureCookies is derived from two settings.
		secureCookies: secure,
		sameSite:      sameSite,
		limiter:       &limiter{n: 3},
		// billing is copied as it is.
		billing:  deps.BillingService,
		buildSHA: (deps.BuildSHA),
	}
	if h.billing != nil {
		h.billing.DefaultReturnURL(h.frontendURL)
	}
	return h
}
