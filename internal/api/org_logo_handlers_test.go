package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

const logoOrgID = "org-logo"

func logoFixture(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		uploadsDir: t.TempDir(),
		orgService: &fakeOrgService{roles: map[string]map[string]string{
			logoOrgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
		}},
	}
}

// smallPNG encodes a real 4x4 PNG so the content sniff agrees with the
// declared type.
func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// logoUploadReq builds a multipart POST of one "file" part with the given
// declared type, as the requesting user.
func logoUploadReq(t *testing.T, userID, mimeType string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{`form-data; name="file"; filename="logo.bin"`}
	hdr["Content-Type"] = []string{mimeType}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/"+logoOrgID+"/logo", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return logoReqAs(r, userID)
}

func logoReqAs(r *http.Request, userID string) *http.Request {
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return mux.SetURLVars(r, map[string]string{"id": logoOrgID})
}

func decodeOrg(t *testing.T, w *httptest.ResponseRecorder) orgs.Org {
	t.Helper()
	var o orgs.Org
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("decode org: %v (body %q)", err, w.Body.String())
	}
	return o
}

// TestUploadOrgLogoStoresPNG locks in the happy path: an admin's PNG lands
// under uploads/org-logos/<org id>.png, the record points at it, and the
// response reports has_logo:true.
func TestUploadOrgLogoStoresPNG(t *testing.T) {
	h := logoFixture(t)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if o := decodeOrg(t, w); !o.HasLogo {
		t.Fatalf("has_logo = false, want true (body %q)", w.Body.String())
	}
	fake := h.orgService.(*fakeOrgService)
	want := filepath.Join(h.uploadsDir, "org-logos", logoOrgID+".png")
	if fake.logoPath != want || fake.logoMime != "image/png" {
		t.Fatalf("stored (%q, %q), want (%q, image/png)", fake.logoPath, fake.logoMime, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("logo file not written: %v", err)
	}
}

// TestUploadOrgLogoReplacesOtherExtension: uploading a different image type
// drops the previous file so one workspace never keeps two logos on disk.
func TestUploadOrgLogoReplacesOtherExtension(t *testing.T) {
	h := logoFixture(t)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("png upload status = %d (body %q)", w.Code, w.Body.String())
	}
	pngPath := h.orgService.(*fakeOrgService).logoPath

	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	w = httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/gif", gif))
	if w.Code != http.StatusOK {
		t.Fatalf("gif upload status = %d (body %q)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Fatalf("previous png still on disk (stat err %v)", err)
	}
	if got := h.orgService.(*fakeOrgService).logoPath; filepath.Ext(got) != ".gif" {
		t.Fatalf("stored path %q, want .gif", got)
	}
}

// TestUploadOrgLogoRejectsNonImage: a text file is a 400 whether it is
// declared as text or dressed up as a PNG.
func TestUploadOrgLogoRejectsNonImage(t *testing.T) {
	text := []byte("hello, this is not an image at all")
	for _, declared := range []string{"text/plain", "image/png"} {
		t.Run(declared, func(t *testing.T) {
			h := logoFixture(t)
			w := httptest.NewRecorder()
			h.UploadOrgLogo(w, logoUploadReq(t, "admin", declared, text))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
			}
			if h.orgService.(*fakeOrgService).logoPath != "" {
				t.Fatalf("logo recorded for a rejected upload")
			}
		})
	}
}

// TestUploadOrgLogoRejectsSVG: an SVG can carry script, so it is refused
// even though it is an image type the attachment catalog accepts.
func TestUploadOrgLogoRejectsSVG(t *testing.T) {
	h := logoFixture(t)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
	}
}

// TestUploadOrgLogoTooLarge: anything over 2 MiB is a 413 before the bytes
// are inspected or written.
func TestUploadOrgLogoTooLarge(t *testing.T) {
	h := logoFixture(t)
	data := append(smallPNG(t), make([]byte, maxOrgLogoBytes)...)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", data))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %q)", w.Code, w.Body.String())
	}
	if entries, _ := os.ReadDir(filepath.Join(h.uploadsDir, "org-logos")); len(entries) != 0 {
		t.Fatalf("oversize upload left %d file(s) on disk", len(entries))
	}
}

// TestUploadOrgLogoRequiresAdmin: a plain member, a stranger and an
// anonymous caller are refused before anything is read or written.
func TestUploadOrgLogoRequiresAdmin(t *testing.T) {
	cases := []struct {
		name   string
		userID string
		want   int
	}{
		{"member", "member", http.StatusForbidden},
		{"non-member", "stranger", http.StatusNotFound},
		{"unauthenticated", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := logoFixture(t)
			w := httptest.NewRecorder()
			h.UploadOrgLogo(w, logoUploadReq(t, tc.userID, "image/png", smallPNG(t)))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if h.orgService.(*fakeOrgService).logoPath != "" {
				t.Fatalf("logo recorded for a refused caller")
			}
		})
	}
}

// TestGetOrgLogoServesBytes: a member gets the stored bytes back as the
// stored type, un-sniffable, inline, privately cacheable; without a logo
// the route is a 404.
func TestGetOrgLogoServesBytes(t *testing.T) {
	h := logoFixture(t)

	w := httptest.NewRecorder()
	h.GetOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodGet, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "member"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status without logo = %d, want 404 (body %q)", w.Code, w.Body.String())
	}

	data := smallPNG(t)
	w = httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", data))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d (body %q)", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.GetOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodGet, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "member"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("served %d bytes, want the %d uploaded", w.Body.Len(), len(data))
	}
	for k, want := range map[string]string{
		"Content-Type":           "image/png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, max-age=300",
		"Content-Disposition":    "inline",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	// A stranger cannot fetch it: to it the workspace is not there.
	w = httptest.NewRecorder()
	h.GetOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodGet, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "stranger"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("stranger status = %d, want 404", w.Code)
	}
}

// TestDeleteOrgLogoClears: an admin's DELETE removes the file, forgets the
// record and answers has_logo:false; a member is refused.
func TestDeleteOrgLogoClears(t *testing.T) {
	h := logoFixture(t)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d (body %q)", w.Code, w.Body.String())
	}
	path := h.orgService.(*fakeOrgService).logoPath

	w = httptest.NewRecorder()
	h.DeleteOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodDelete, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "member"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d, want 403", w.Code)
	}

	w = httptest.NewRecorder()
	h.DeleteOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodDelete, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "admin"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if o := decodeOrg(t, w); o.HasLogo {
		t.Fatalf("has_logo = true after delete (body %q)", w.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("logo file still on disk (stat err %v)", err)
	}
	if fake := h.orgService.(*fakeOrgService); fake.logoPath != "" || fake.logoMime != "" {
		t.Fatalf("record not cleared: (%q, %q)", fake.logoPath, fake.logoMime)
	}

	// Deleting again (no file, no record) is still a 200.
	w = httptest.NewRecorder()
	h.DeleteOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodDelete, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "admin"))
	if w.Code != http.StatusOK {
		t.Fatalf("repeat delete status = %d, want 200", w.Code)
	}
}

// Raster images written out by hand, so that each sniffs as its own type
// (http.DetectContentType), beside smallPNG; and two image formats outside
// the four a logo or a picture may be.
const (
	jpegBytes = "\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xff\xd9"
	gifBytes  = "GIF89a\x01\x00\x01\x00\x00\x00\x00;"
	webpBytes = "RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00"
	bmpBytes  = "BM\x3a\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00\x28\x00\x00\x00\x01\x00\x00\x00\x01\x00\x00\x00\x01\x00\x18\x00"
	icoBytes  = "\x00\x00\x01\x00\x01\x00\x01\x01\x00\x00\x01\x00\x20\x00\x30\x00\x00\x00\x16\x00\x00\x00"
)

// mislabelledImages are image bytes declared as a type they are not: each
// is refused by the logo and the picture upload alike (OpenV REQ-129,
// REQ-133), since both serve the bytes as the declared type.
func mislabelledImages(t *testing.T) []struct {
	name, declared string
	data           []byte
} {
	return []struct {
		name, declared string
		data           []byte
	}{
		{"a GIF declared as a PNG", "image/png", []byte(gifBytes)},
		{"a PNG declared as a GIF", "image/gif", smallPNG(t)},
		{"a JPEG declared as a WebP", "image/webp", []byte(jpegBytes)},
		{"a WebP declared as a JPEG", "image/jpeg", []byte(webpBytes)},
		{"a BMP declared as a PNG", "image/png", []byte(bmpBytes)},
		{"an icon declared as a PNG", "image/png", []byte(icoBytes)},
	}
}

// TestUploadOrgLogoBytesMustBeTheDeclaredType: a logo is served as the type
// it was declared, so its bytes must be that type, not merely some image.
// Each of the four types passes as itself.
func TestUploadOrgLogoBytesMustBeTheDeclaredType(t *testing.T) {
	for _, tc := range mislabelledImages(t) {
		t.Run(tc.name, func(t *testing.T) {
			h := logoFixture(t)
			w := httptest.NewRecorder()
			h.UploadOrgLogo(w, logoUploadReq(t, "admin", tc.declared, tc.data))
			if w.Code != http.StatusBadRequest ||
				w.Body.String() != "{\"error\":\"File content does not match an image of the declared type\"}\n" {
				t.Fatalf("status = %d (body %q), want the 400 for content that does not match", w.Code, w.Body.String())
			}
			if h.orgService.(*fakeOrgService).logoPath != "" {
				t.Fatalf("logo recorded for a refused upload")
			}
			if entries, _ := os.ReadDir(filepath.Join(h.uploadsDir, "org-logos")); len(entries) != 0 {
				t.Fatalf("refused upload left %d file(s) on disk", len(entries))
			}
		})
	}
	for declared, data := range map[string][]byte{
		"image/png": smallPNG(t), "image/jpeg": []byte(jpegBytes), "image/gif": []byte(gifBytes), "image/webp": []byte(webpBytes),
	} {
		h := logoFixture(t)
		w := httptest.NewRecorder()
		h.UploadOrgLogo(w, logoUploadReq(t, "admin", declared, data))
		if w.Code != http.StatusOK {
			t.Fatalf("%s as itself: status = %d, want 200 (body %q)", declared, w.Code, w.Body.String())
		}
	}
}

// TestUploadOrgLogoUnknownWorkspace: a platform admin's upload for a
// workspace that does not exist is refused 404 before anything is written.
// The workspace guard refuses it, since it lets a platform admin by only
// into a workspace that exists; the handler's own lookup, which names the
// logo an upload replaces, refused it while the guard let the admin by for
// any id. Before either, the upload wrote the file first, answered 500 when
// the record could not be saved, and left the file behind.
func TestUploadOrgLogoUnknownWorkspace(t *testing.T) {
	h := logoFixture(t)
	fake := h.orgService.(*fakeOrgService)
	fake.missing = map[string]bool{logoOrgID: true}
	r := logoUploadReq(t, "", "image/png", smallPNG(t))
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true}))
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, r)
	if w.Code != http.StatusNotFound || w.Body.String() != "{\"error\":\"workspace not found\"}\n" {
		t.Fatalf("status = %d (body %q), want 404 workspace not found", w.Code, w.Body.String())
	}
	if entries, _ := os.ReadDir(filepath.Join(h.uploadsDir, "org-logos")); len(entries) != 0 {
		t.Fatalf("an upload for a workspace that does not exist left %d file(s) on disk", len(entries))
	}
}

// TestUploadOrgLogoUnrecordedLeavesNoOrphan: when the record cannot be
// saved, the upload is a 500 and leaves the logo on record as it was: a
// new file of another type is removed rather than orphaned, and the
// recorded logo's file stays.
func TestUploadOrgLogoUnrecordedLeavesNoOrphan(t *testing.T) {
	h := logoFixture(t)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("png upload status = %d (body %q)", w.Code, w.Body.String())
	}
	fake := h.orgService.(*fakeOrgService)
	pngPath := fake.logoPath
	fake.setLogoErr = errors.New("database unavailable")

	w = httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/gif", []byte(gifBytes)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
	}
	if names := logoFiles(h); len(names) != 1 || names[0] != filepath.Base(pngPath) || fake.logoPath != pngPath {
		t.Fatalf("files %v and record %q after an unrecorded upload; want the recorded %s alone",
			names, fake.logoPath, filepath.Base(pngPath))
	}
}

// TestUploadOrgLogoConcurrentUploadLeavesNoOrphan: the logo an upload
// replaces is the one on record when its own is recorded, not the one on
// record when its request arrived. Upload A (a JPEG) is still sending its
// body when upload B (a GIF) replaces the recorded PNG; A must then remove
// B's GIF, not the PNG that B already removed, so that A's JPEG alone is
// left, on record.
func TestUploadOrgLogoConcurrentUploadLeavesNoOrphan(t *testing.T) {
	h := logoFixture(t)
	w := httptest.NewRecorder()
	h.UploadOrgLogo(w, logoUploadReq(t, "admin", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("png upload status = %d (body %q)", w.Code, w.Body.String())
	}

	// A's body goes through a pipe: once its first bytes are read, the
	// handler has looked the workspace up and waits for the rest.
	ra := logoUploadReq(t, "admin", "image/jpeg", []byte(jpegBytes))
	body, err := io.ReadAll(ra.Body)
	if err != nil {
		t.Fatalf("read upload A's body: %v", err)
	}
	pr, pw := io.Pipe()
	ra.Body, ra.ContentLength = pr, -1
	wa := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer pr.Close() // a handler that stops reading must not block the writes below
		h.UploadOrgLogo(wa, ra)
	}()
	send := func(p []byte) {
		if _, err := pw.Write(p); err != nil {
			<-done
			t.Fatalf("upload A stopped reading its body: %v (status %d, body %q)", err, wa.Code, wa.Body.String())
		}
	}
	send(body[:10])

	wb := httptest.NewRecorder()
	h.UploadOrgLogo(wb, logoUploadReq(t, "admin", "image/gif", []byte(gifBytes)))

	send(body[10:])
	pw.Close()
	<-done
	if wa.Code != http.StatusOK || wb.Code != http.StatusOK {
		t.Fatalf("statuses A=%d (body %q) B=%d (body %q), want 200 and 200",
			wa.Code, wa.Body.String(), wb.Code, wb.Body.String())
	}
	want := logoOrgID + ".jpg"
	recorded := filepath.Base(h.orgService.(*fakeOrgService).logoPath)
	if names := logoFiles(h); len(names) != 1 || names[0] != want || recorded != want {
		t.Fatalf("files %v and record %q after two uploads at once; want %s alone, on record", names, recorded, want)
	}
}

// logoFiles lists the file names under uploads/org-logos.
func logoFiles(h *Handler) []string {
	entries, _ := os.ReadDir(filepath.Join(h.uploadsDir, "org-logos"))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
