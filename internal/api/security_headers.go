package api

import (
	"net/http"
)

// SecurityHeadersMiddleware stamps the browser-hardening headers on every
// response the API serves. The API answers JSON, streams and file downloads,
// never HTML, so the policy is the strictest one a browser will accept: a
// response may not be framed, may not be sniffed into another type, and may
// not load anything if a browser ever renders it as a document.
//
// hsts adds Strict-Transport-Security. It is the operator's declaration that
// the API is only ever reached over TLS (the same deployments that set
// SECURE_COOKIES): the API cannot tell whether a TLS terminator sits in front
// of it, and sending HSTS over plain HTTP would lock a dev stack's browser
// out of http://localhost.
func SecurityHeadersMiddleware(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
