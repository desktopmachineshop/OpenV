package api

import "net/http"

// CORSMiddleware restricts credentialed cross-origin requests to the one
// configured frontend origin. A wildcard is refused outright: reflecting the
// caller's Origin together with Allow-Credentials would let any site on the
// internet drive the API with a signed-in member's cookie, so there is no
// deployment where "*" is a safe value.
func CORSMiddleware(origin string, next http.Handler) (http.Handler, error) {
	if origin == "" || origin == "*" || origin == "null" {
		return nil, ErrCORSOriginNotAllowed
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == origin {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Org-ID")
			// Pagination metadata (artifact totals, event cursors) and export
			// filenames ride on response headers; without this the browser
			// hides them from cross-origin scripts.
			h.Set("Access-Control-Expose-Headers", "X-Total-Count, X-Next-Cursor, Content-Disposition")
			h.Set("Access-Control-Max-Age", "3600")
			h.Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

// ErrCORSOriginNotAllowed is returned by CORSMiddleware for an origin value
// that would reflect arbitrary origins with credentials.
var ErrCORSOriginNotAllowed = errCORSOrigin("CORS_ORIGIN must be the exact frontend origin (scheme and host); a wildcard or empty value would allow any site to call the API with a member's credentials")

type errCORSOrigin string

func (e errCORSOrigin) Error() string { return string(e) }
