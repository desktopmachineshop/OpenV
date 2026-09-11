package api

// Registration policy (REQ-95 / HAZ-15). A deployment either has a public
// sign-up door or it does not. Open is the default — that is what a
// self-hosted stack, dev and the E2E suite have always had — and closing it
// leaves exactly two ways in: an invitation from a workspace admin, or the
// deployment's single sign-on provider, whose IdP is doing the admitting.

import (
	"log/slog"
	"os"
	"strings"
)

// Registration policy values, as accepted in OPENV_REGISTRATION.
const (
	RegistrationOpen   = "open"
	RegistrationClosed = "closed"

	envRegistration = "OPENV_REGISTRATION"
)

// RegistrationPolicyFromEnv reads OPENV_REGISTRATION, defaulting to open and
// logging one line naming the state — an operator reading the boot log
// should be able to see whether strangers can still sign themselves up.
func RegistrationPolicyFromEnv() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envRegistration))) {
	case RegistrationClosed:
		slog.Info("registration: closed (new accounts arrive by workspace invitation or single sign-on)")
		return RegistrationClosed
	case "", RegistrationOpen:
		slog.Info("registration: open (set OPENV_REGISTRATION=closed to require an invitation)")
		return RegistrationOpen
	default:
		slog.Warn("registration: unrecognised OPENV_REGISTRATION value; leaving registration open",
			"value", os.Getenv(envRegistration))
		return RegistrationOpen
	}
}
