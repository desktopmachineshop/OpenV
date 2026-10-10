// Package api is httpio's fixture: handlers in internal/api's shapes,
// rewritten by TestFixture and served before and after by
// TestFixtureAnswersAlike.
package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON answers status with v as a JSON body: Content-Type
// application/json, then the status, then v as json.NewEncoder encodes it
// (HTML escaped, with a trailing newline).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONOK answers v as a JSON body with Content-Type application/json
// and the implicit 200: it calls no WriteHeader, so the first write sends
// the status.
func writeJSONOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONBare answers v as a JSON body with the implicit 200 and no
// Content-Type of its own, so net/http sniffs one from the body (quirk Q1).
func writeJSONBare(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONBareStatus answers status with v as a JSON body and no
// Content-Type of its own (quirk Q1).
func writeJSONBareStatus(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// invalidRequestBody is the message of a request body that does not
// decode, the one "invalid request body" literal of the package.
const invalidRequestBody = "invalid request body"

// decodeJSON decodes the request body into v. When that fails it answers
// 400 {"error":"invalid request body"} and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONMsg(w, r, v, invalidRequestBody)
}

// decodeJSONMsg is decodeJSON for a handler whose 400 says msg (quirk Q19).
func decodeJSONMsg(w http.ResponseWriter, r *http.Request, v any, msg string) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, msg)
		return false
	}
	return true
}
