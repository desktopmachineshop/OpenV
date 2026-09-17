package api

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/orgs"
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

// How big one uploaded figure may be.
//
// The ceiling is a workspace limit (orgs.LimitMaxUploadMB) rather than one
// number for the whole deployment. A figure is no longer a screenshot: the
// catalogue accepts CAD assemblies and supplier PDFs, and the 25 MB this
// shipped with refused most real geometry, which made the format catalogue a
// promise the uploader could not keep (issue #364).
//
// OPENV_MAX_UPLOAD_MB still wins where an operator set it, so a deployment
// that pinned the old number keeps it, and a self-hosted operator has one
// variable rather than a limits document to edit.
const (
	envMaxUploadMB = "OPENV_MAX_UPLOAD_MB"
	// defaultMaxUploadMB applies only where no workspace could be resolved.
	// It matches the free plan, so the fallback is never more generous than
	// the tier a workspace would actually have got.
	defaultMaxUploadMB = 128
	// maxUploadCeilingMB bounds one HTTP request whatever the workspace is
	// allowed. It is a transport guard, not a plan limit: "unlimited" on a
	// self-hosted deployment means the operator sets no ceiling, not that the
	// API should accept a stream that never ends.
	maxUploadCeilingMB = 8192
	bytesPerMB         = int64(1024 * 1024)
)

// envUploadMB reads the operator's override, and whether it was set to
// something usable.
func envUploadMB() (int64, bool) {
	v := os.Getenv(envMaxUploadMB)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// uploadLimitBytes is the biggest single figure this workspace may upload.
// An unresolvable workspace gets the free plan's ceiling rather than none:
// a limit lookup that failed must not be the reason a cap disappears.
func (h *Handler) uploadLimitBytes(orgID string) int64 {
	if mb, ok := envUploadMB(); ok {
		return mb * bytesPerMB
	}
	if h.orgService != nil && orgID != "" {
		if org, err := h.orgService.Get(orgID); err == nil && org != nil {
			if mb, ok := orgs.LimitFloat(org.EffectiveLimits(), orgs.LimitMaxUploadMB); ok {
				if mb <= 0 {
					return maxUploadCeilingMB * bytesPerMB
				}
				return min(int64(mb), int64(maxUploadCeilingMB)) * bytesPerMB
			}
		}
	}
	return defaultMaxUploadMB * bytesPerMB
}

// multipartOverheadBytes is the room a multipart envelope needs beyond the
// file itself: the boundary lines, the part headers and the other form fields.
// A megabyte is far more than any of them take and keeps the bound from
// refusing a file that is exactly at the limit.
const multipartOverheadBytes = int64(1024 * 1024)

// uploadRequestCeilingBytes bounds an upload request before the handler knows
// which workspace it is for.
//
// Parsing a multipart body spools every part to a temp file, so the bound has
// to be in place before the form is touched — and at that point the only
// honest answer is "no more than any plan could possibly allow". The
// workspace's own, tighter limit is applied to the bytes afterwards.
func (h *Handler) uploadRequestCeilingBytes() int64 {
	if mb, ok := envUploadMB(); ok {
		return mb*bytesPerMB + multipartOverheadBytes
	}
	mb := int64(orgs.MaxPlanUploadMB())
	if mb <= 0 || mb > maxUploadCeilingMB {
		mb = maxUploadCeilingMB
	}
	return mb*bytesPerMB + multipartOverheadBytes
}

// uploadLimitMessage says what the ceiling is, because "too large" without a
// number leaves somebody guessing how much to cut.
func uploadLimitMessage(limit int64) string {
	mb := limit / bytesPerMB
	return "File is larger than this workspace's " + strconv.FormatInt(mb, 10) + " MB upload limit"
}

// uploadHeadBytes is how much of a file the format checks read: Go's content
// sniffer looks at 512 bytes and every magic number in the catalogue is
// shorter than that. It is what makes streaming possible — the checks need
// the start of the file, not the whole of it.
const uploadHeadBytes = 512

// storeUpload streams an uploaded file straight to path, refusing anything
// over limit with a 413 and leaving no partial file behind. It answers the
// file's leading bytes, which is all the format checks need, and its size.
//
// Streamed rather than buffered because the cap is now measured in hundreds
// of megabytes: reading one of those into memory to look at its first 512
// bytes would make the API's footprint the size of the largest file anybody
// happens to be uploading.
func storeUpload(w http.ResponseWriter, r *http.Request, src io.Reader, path string, limit int64) (head []byte, size int64, ok bool) {
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		respondInternal(w, r, "Failed to save file", err)
		return nil, 0, false
	}
	defer dst.Close()

	// One byte past the limit is enough to know the file is over it, and
	// stops a refusal costing the whole transfer.
	buffered := bufio.NewReaderSize(io.LimitReader(src, limit+1), uploadHeadBytes)
	peeked, err := buffered.Peek(uploadHeadBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = os.Remove(path)
		if uploadReadRefused(w, err) {
			return nil, 0, false
		}
		respondInternal(w, r, "Failed to read file", err)
		return nil, 0, false
	}
	head = append([]byte(nil), peeked...)

	size, err = io.Copy(dst, buffered)
	if err != nil {
		_ = os.Remove(path)
		if uploadReadRefused(w, err) {
			return nil, 0, false
		}
		respondInternal(w, r, "Failed to save file", err)
		return nil, 0, false
	}
	if size > limit {
		_ = os.Remove(path)
		writeJSONError(w, http.StatusRequestEntityTooLarge, uploadLimitMessage(limit))
		return nil, 0, false
	}
	return head, size, true
}

// uploadReadRefused answers the one read error that is the uploader's doing
// rather than the server's: the request body running past the cap the handler
// put on it. Reported and true when that is what happened.
func uploadReadRefused(w http.ResponseWriter, err error) bool {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "File is larger than the upload limit")
		return true
	}
	return false
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
