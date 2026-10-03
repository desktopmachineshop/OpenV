package api

import (
	"mime"
	"net/http"
)

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
