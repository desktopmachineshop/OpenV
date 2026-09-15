package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// Attachment uploads are the one place a member hands the API a file that
// other members' browsers will later fetch. Three rules keep that honest:
// the file is read up to a cap rather than in full, its bytes must look like
// the format the uploader named, and only an inert picture is ever rendered
// on the API origin — every other kind, a PDF and a CAD model included, is
// handed over as a download under a policy that permits nothing.
//
// That last rule is why widening the catalogue beyond images did not widen
// the attack surface with it. A PDF carries script and a CAD file is opaque;
// neither is ever a document on this origin, so neither can run anything with
// a viewer's session. The app previews them by fetching the bytes and
// rendering them in its own origin, where the API's headers cannot be
// mistaken for permission.

// envMaxUploadMB caps one uploaded file; OPENV_MAX_UPLOAD_MB overrides the
// default of 25 MB.
const (
	envMaxUploadMB     = "OPENV_MAX_UPLOAD_MB"
	defaultMaxUploadMB = 25
)

// maxUploadBytes resolves the per-file upload cap from the environment.
func maxUploadBytes() int64 {
	mb := int64(defaultMaxUploadMB)
	if v := os.Getenv(envMaxUploadMB); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			mb = n
		}
	}
	return mb * 1024 * 1024
}

// readUpload reads an uploaded part up to the configured cap. A file over
// the cap is answered with 413 and false; a read failure with 500 and false.
func readUpload(w http.ResponseWriter, r *http.Request, file io.Reader) ([]byte, bool) {
	limit := maxUploadBytes()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "File is larger than the upload limit")
			return nil, false
		}
		respondInternal(w, r, "Failed to read file", err)
		return nil, false
	}
	if int64(len(data)) > limit {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "File is larger than the upload limit")
		return nil, false
	}
	return data, true
}

// uploadLooksLikeImage checks the file's leading bytes against the type the
// uploader declared, so that a file named and labelled as an image but
// carrying something else is refused. It stays image-only because the avatar
// and workspace-logo uploads depend on it: those are rendered as pictures and
// have no business accepting a CAD file. Go's sniffer recognises the raster
// formats the catalog accepts except TIFF, which is matched by its magic
// number; an SVG has no magic number and sniffs as XML or plain text, so it
// is accepted when the text opens with an svg or xml element.
func uploadLooksLikeImage(declared string, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	sniffed := http.DetectContentType(data)
	switch declared {
	case "image/svg+xml":
		if !strings.HasPrefix(sniffed, "text/xml") && !strings.HasPrefix(sniffed, "text/plain") && !strings.HasPrefix(sniffed, "image/svg+xml") {
			return false
		}
		head := strings.ToLower(string(bytes.TrimLeft(data[:min(len(data), 512)], " \t\r\n\xef\xbb\xbf")))
		return strings.HasPrefix(head, "<svg") || strings.HasPrefix(head, "<?xml") || strings.HasPrefix(head, "<!doctype svg") || strings.HasPrefix(head, "<!--")
	case "image/tiff":
		return bytes.HasPrefix(data, []byte("II*\x00")) || bytes.HasPrefix(data, []byte("MM\x00*"))
	default:
		return strings.HasPrefix(sniffed, "image/")
	}
}

// uploadLooksLikeFigure checks an uploaded figure against the format its name
// claims. An image is held to the strict sniff above, because an image is the
// one kind that will later be rendered on this origin; every other kind is
// checked against its signature where it has one, which catches a mis-named
// file rather than a malicious one. Nothing rests on it: the serving policy
// below is what makes a non-image safe, whatever its bytes turn out to be.
func uploadLooksLikeFigure(mime, filename string, data []byte) bool {
	if attachments.IsImage(mime) {
		return uploadLooksLikeImage(mime, data)
	}
	return attachments.ContentMatchesFormat(filename, data)
}

// attachmentDisposition decides whether a download may render inline.
//
// Inline is an allowlist of one: a raster image, which is inert. An SVG is a
// document that can carry script and event handlers; a PDF can carry script
// too; a CAD file is bytes no browser should be guessing at. Rendered on the
// API origin any of them would run with the viewer's session, so everything
// but a raster image is handed over as a file.
func attachmentDisposition(mime string) string {
	if attachments.IsImage(mime) && mime != "image/svg+xml" {
		return "inline"
	}
	return "attachment"
}

// setAttachmentSecurityHeaders overrides the API-wide policy for a file
// response: the bytes may not be sniffed into another type, and a browser
// that renders the response as a document may load nothing but the image
// itself — and for anything that is not an inert picture, nothing at all,
// inside a sandbox with no script.
//
// The API-wide X-Frame-Options: DENY is left standing on purpose. The app
// previews a PDF or a model by fetching its bytes with the member's session
// and rendering them from its own origin, so nothing here ever needs to be
// framed, and relaxing the header to allow it would be trading a hard
// guarantee for a convenience the app does not need.
func setAttachmentSecurityHeaders(w http.ResponseWriter, mime string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if attachments.IsImage(mime) && mime != "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; frame-ancestors 'none'")
		return
	}
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'none'")
}
