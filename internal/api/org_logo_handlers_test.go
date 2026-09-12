package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
		{"non-member", "stranger", http.StatusForbidden},
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

	// A stranger cannot fetch it.
	w = httptest.NewRecorder()
	h.GetOrgLogo(w, logoReqAs(httptest.NewRequest(http.MethodGet, "/api/v1/orgs/"+logoOrgID+"/logo", nil), "stranger"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger status = %d, want 403", w.Code)
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
