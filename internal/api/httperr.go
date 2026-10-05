package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// errorBody is the JSON error envelope every API error response uses. The
// frontend reads err.response.data.error, so the field name is load-bearing.
type errorBody struct {
	Error string `json:"error"`
	// Code is a stable machine-readable reason for the errors a client has to
	// branch on (the message is for people and may change).
	Code string `json:"code,omitempty"`
}

// Machine-readable error codes. A client branches on these; the messages
// beside them are for people and may change.
const (
	// ErrCodeEmailUnverified marks the 403 the auth middleware answers for a
	// session whose account has not yet confirmed its email address.
	ErrCodeEmailUnverified = "email_unverified"
	// ErrCodeRegistrationClosed marks the 403 that registration answers on a
	// deployment with no public sign-up door (REQ-95).
	ErrCodeRegistrationClosed = "registration_closed"
	// ErrCodeInvitationEmailMismatch marks the 403 for accepting an invite
	// link while signed in as some other address. The invited address is
	// deliberately absent from that response, so the code is how the client
	// tells this refusal from an unusable link.
	ErrCodeInvitationEmailMismatch = "invitation_email_mismatch"
	// The three ways a password change refuses (REQ-99). weak_password also
	// answers a registration's and a reset's password under the minimum.
	ErrCodeWeakPassword      = "weak_password"
	ErrCodePasswordIncorrect = "password_incorrect"
	// ErrCodeLimitReached marks a refusal caused by a workspace limit rather
	// than by permissions, so a client can offer the remedy that came with
	// it instead of an access-denied message.
	ErrCodeLimitReached = "limit_reached"
	// ErrCodePlanReadOnly marks a write refused because the workspace holds
	// more than its plan allows; reads and export are never refused.
	ErrCodePlanReadOnly = "plan_read_only"
	ErrCodeNoPassword   = "no_password"
	// ErrCodeResetInvalid answers a reset link that is unknown, spent or
	// expired; ErrCodeResetEmailUnavailable a request for an emailed reset
	// on a deployment with no mailer (REQ-158).
	ErrCodeResetInvalid          = "reset_invalid"
	ErrCodeResetEmailUnavailable = "reset_email_unavailable"
	// ErrCodeBillingUnavailable answers every billing route on a deployment
	// with no billing provider configured — every self-hosted one.
	ErrCodeBillingUnavailable = "billing_unavailable"
	// ErrCodeBillingUpstream marks a 503 where the billing provider did not
	// answer, or has not confirmed the prices yet; the workspace's plan was
	// left exactly as it was.
	ErrCodeBillingUpstream = "billing_upstream"
	// The purchase refusals a client branches on. already_subscribed also
	// answers a platform admin's plan grant over a live subscription.
	ErrCodeUnknownPlan       = "unknown_plan"
	ErrCodeAlreadySubscribed = "already_subscribed"
	ErrCodeNoSubscription    = "no_subscription"
	ErrCodeGrantedPlan       = "granted_plan"
	ErrCodeNoCustomer        = "no_customer"
	ErrCodeCheckoutMismatch  = "checkout_mismatch"
)

// writeJSONErrorCode is writeJSONError with a machine-readable code.
func writeJSONErrorCode(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: message, Code: code})
}

// writeJSONError writes a JSON {"error": message} body with the given status.
// It does no logging; use respondError when there is an underlying error that
// should reach the server log.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: message})
}

// respondError logs the real error with request context and writes a
// sanitized JSON error response. The public message is what the client sees;
// err — which may carry internals like SQL text, file paths, or upstream
// details — only reaches the server log. 5xx responses log at ERROR, 4xx at
// WARN. err may be nil when there is nothing beyond the public message to
// record.
func respondError(w http.ResponseWriter, r *http.Request, status int, publicMsg string, err error) {
	attrs := []any{
		slog.Int("status", status),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	if org := ActiveOrg(r); org != "" {
		attrs = append(attrs, slog.String("org_id", org))
	}
	if user := CurrentUser(r); user != nil {
		attrs = append(attrs, slog.String("user_id", user.ID))
	}
	if status >= 500 {
		slog.Error(publicMsg, attrs...)
	} else {
		slog.Warn(publicMsg, attrs...)
	}
	writeJSONError(w, status, publicMsg)
}

// respondInternal is respondError for the common 500 path: the client gets
// the stable public message, the log gets the real error.
func respondInternal(w http.ResponseWriter, r *http.Request, publicMsg string, err error) {
	respondError(w, r, http.StatusInternalServerError, publicMsg, err)
}

// notFound is the answer to an id no row has: its status and message. The
// guards give exactly this answer to a caller with no access at all, so that
// a refusal tells nothing of whether the id exists (I3, OpenV REQ-17): a
// project no row has and one the caller cannot reach both answer the project
// guard's 404 "project not found", and a workspace likewise "workspace not
// found". A guard that stands for a resource the handler looked up by its
// own id answers that resource's own not-found (missing), the answer the
// lookup gives an id no row has. A caller who reaches the project or
// workspace but lacks the role a write needs still gets 403: the resource
// exists for it. The zero notFound stands for the guard's own answer.
type notFound struct {
	status  int
	message string
}

var (
	unknownProject   = notFound{http.StatusNotFound, "project not found"}
	unknownWorkspace = notFound{http.StatusNotFound, "workspace not found"}
)

// missing is a resource's not-found answer: 404 with its message.
func missing(message string) notFound { return notFound{http.StatusNotFound, message} }

// or is n, or def when n is the zero notFound.
func (n notFound) or(def notFound) notFound {
	if n == (notFound{}) {
		return def
	}
	return n
}

func (n notFound) write(w http.ResponseWriter) { writeJSONError(w, n.status, n.message) }

// respondArtifactLookup answers a failed artifact lookup of a write that
// loads the artifact before its guard: 404 for an id no artifact has, a
// malformed one among them (artifacts.ErrNotFound), and 500 for anything
// else, which a client may retry.
func respondArtifactLookup(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, artifacts.ErrNotFound) {
		respondError(w, r, http.StatusNotFound, "artifact not found", err)
		return
	}
	respondInternal(w, r, "failed to load artifact", err)
}
