package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Attachment uploads are the one place a member hands the API a file that
// other members' browsers will later fetch. Three rules keep that honest:
// the file is read up to a cap rather than in full, its bytes must look like
// the image type the uploader declared, and a format that can carry script
// (SVG) is served as a download inside a sandbox rather than rendered on the
// API origin.

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
// carrying something else is refused. Go's sniffer recognises the raster
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

// attachmentDisposition decides whether a download may render inline. Raster
// images are inert; an SVG is a document that can carry script and event
// handlers, and rendered on the API origin it would run them with the
// viewer's session, so it is always handed over as a file.
func attachmentDisposition(mime string) string {
	if mime == "image/svg+xml" {
		return "attachment"
	}
	return "inline"
}

// setAttachmentSecurityHeaders overrides the API-wide policy for a file
// response: the bytes may not be sniffed into another type, and a browser
// that renders the response as a document may load nothing but the image
// itself and, for an SVG, runs inside a sandbox with no script at all.
func setAttachmentSecurityHeaders(w http.ResponseWriter, mime string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if mime == "image/svg+xml" {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'none'")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; frame-ancestors 'none'")
}
