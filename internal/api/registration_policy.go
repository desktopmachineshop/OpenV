package api

// Registration policy (REQ-95 / HAZ-15). A deployment either has a public
// sign-up door or it does not. Open is the default — that is what a
// self-hosted stack, dev and the E2E suite have always had — and closing it
// leaves exactly two ways in: an invitation from a workspace admin, or the
// deployment's single sign-on provider, whose IdP is doing the admitting.

// Registration policy values, as accepted in OPENV_REGISTRATION, which stage
// notify of cmd/server reads (internal/config's Registration) and hands the
// handler as HandlerDeps.Registration.
const (
	RegistrationOpen   = "open"
	RegistrationClosed = "closed"
)
