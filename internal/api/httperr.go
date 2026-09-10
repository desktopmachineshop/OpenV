package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
	// The three ways a password change refuses (REQ-99).
	ErrCodeWeakPassword      = "weak_password"
	ErrCodePasswordIncorrect = "password_incorrect"
	ErrCodeNoPassword        = "no_password"
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
