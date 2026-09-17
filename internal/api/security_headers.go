package api

import (
	"mime"
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

// BodyLimitMiddleware caps every request body at maxBytes so that a client
// cannot make the API read an unbounded body into memory. Handlers that read
// the body see a *http.MaxBytesError once the cap is passed, and the JSON
// decoders already answer that with a 400.
//
// A FILE UPLOAD is exempt, and has to be: this cap is sized for JSON, and a
// MaxBytesReader wrapped around another one enforces the tighter of the two,
// so an upload handler asking for more than maxBytes would have been silently
// refused at this number instead (issue #364 — the figure limit could not be
// raised past it, and the evidence handler's own 200 MB cap had never actually
// been reachable). Every multipart handler in this API sets its own
// MaxBytesReader before it parses anything, so the exemption hands a request
// to a handler that bounds it, not to one that does not: see UploadAttachment,
// UploadAttachmentVersion, UploadEvidenceFile, UploadAvatar and UploadOrgLogo.
func BodyLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && maxBytes > 0 && !isFileUpload(r) {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isFileUpload reports whether a request carries a multipart body, which is
// the only shape a file reaches this API in.
func isFileUpload(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "multipart/form-data"
}
