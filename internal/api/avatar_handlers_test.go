package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeAvatarUsers is the slice of users.Service the avatar handlers use: a
// lookup by id and the two avatar writes, over an in-memory map.
type fakeAvatarUsers struct {
	users.Service
	byID map[string]*users.User
}

func (f *fakeAvatarUsers) GetByID(id string) (*users.User, error) {
	if u, ok := f.byID[id]; ok {
		copy := *u
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeAvatarUsers) SetAvatar(userID, path, mime, url string) (*users.User, error) {
	u := f.byID[userID]
	u.AvatarPath, u.AvatarMime, u.AvatarURL = path, mime, url
	u.HasAvatar = path != ""
	return f.GetByID(userID)
}

func (f *fakeAvatarUsers) ClearAvatar(userID string) (*users.User, error) {
	return f.SetAvatar(userID, "", "", "")
}

func avatarFixture(t *testing.T) (*Handler, *fakeAvatarUsers) {
	t.Helper()
	svc := &fakeAvatarUsers{byID: map[string]*users.User{
		"u1": {ID: "u1", Email: "u1@example.com", AvatarURL: "https://idp.example/u1.png"},
		"u2": {ID: "u2", Email: "u2@example.com"},
	}}
	return &Handler{uploadsDir: t.TempDir(), userService: svc}, svc
}

func avatarReqAs(r *http.Request, userID string) *http.Request {
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return r
}

// avatarUploadReq builds a multipart POST of one "file" part with the given
// declared type, as the requesting user.
func avatarUploadReq(t *testing.T, userID, mimeType string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{`form-data; name="file"; filename="me.bin"`}
	hdr["Content-Type"] = []string{mimeType}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/me/avatar", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return avatarReqAs(r, userID)
}

func decodeUser(t *testing.T, w *httptest.ResponseRecorder) users.User {
	t.Helper()
	var u users.User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatalf("decode user: %v (body %q)", err, w.Body.String())
	}
	return u
}

// TestUploadAvatarStoresPNG locks in the happy path: the PNG lands under
// uploads/avatars/<user id>.png, the record points at it, and the answer
// carries an avatar_url on the API that now replaces the provider's.
func TestUploadAvatarStoresPNG(t *testing.T) {
	h, svc := avatarFixture(t)
	w := httptest.NewRecorder()
	h.UploadAvatar(w, avatarUploadReq(t, "u1", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	u := decodeUser(t, w)
	if !u.HasAvatar {
		t.Fatalf("has_avatar = false, want true (body %q)", w.Body.String())
	}
	if !strings.HasPrefix(u.AvatarURL, "/api/v1/users/u1/avatar?v=") {
		t.Fatalf("avatar_url = %q, want the API path with a version", u.AvatarURL)
	}
	want := filepath.Join(h.uploadsDir, "avatars", "u1.png")
	if svc.byID["u1"].AvatarPath != want || svc.byID["u1"].AvatarMime != "image/png" {
		t.Fatalf("stored (%q, %q), want (%q, image/png)", svc.byID["u1"].AvatarPath, svc.byID["u1"].AvatarMime, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("picture not written: %v", err)
	}
}

// TestUploadAvatarReplacesOtherExtension: a different image type drops the
// previous file so one account never keeps two pictures on disk.
func TestUploadAvatarReplacesOtherExtension(t *testing.T) {
	h, svc := avatarFixture(t)
	w := httptest.NewRecorder()
	h.UploadAvatar(w, avatarUploadReq(t, "u1", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("png upload status = %d (body %q)", w.Code, w.Body.String())
	}
	pngPath := svc.byID["u1"].AvatarPath

	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	w = httptest.NewRecorder()
	h.UploadAvatar(w, avatarUploadReq(t, "u1", "image/gif", gif))
	if w.Code != http.StatusOK {
		t.Fatalf("gif upload status = %d (body %q)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Fatalf("previous png still on disk (stat err %v)", err)
	}
	if got := svc.byID["u1"].AvatarPath; filepath.Ext(got) != ".gif" {
		t.Fatalf("stored path %q, want .gif", got)
	}
}

// TestUploadAvatarRejects: a text file is a 400 whether it is declared as
// text or dressed up as a PNG, an oversized picture is 413, and a request
// with no session is 401. None of them records anything.
func TestUploadAvatarRejects(t *testing.T) {
	text := []byte("hello, this is not an image at all")
	big := append(smallPNG(t), make([]byte, maxAvatarBytes)...)
	for _, tc := range []struct {
		name, user, declared string
		data                 []byte
		want                 int
	}{
		{"text as text", "u1", "text/plain", text, http.StatusBadRequest},
		{"text as png", "u1", "image/png", text, http.StatusBadRequest},
		{"too large", "u1", "image/png", big, http.StatusRequestEntityTooLarge},
		{"no session", "", "image/png", smallPNG(t), http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, svc := avatarFixture(t)
			w := httptest.NewRecorder()
			h.UploadAvatar(w, avatarUploadReq(t, tc.user, tc.declared, tc.data))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if svc.byID["u1"].AvatarPath != "" {
				t.Fatalf("picture recorded for a rejected upload")
			}
		})
	}
}

// TestGetUserAvatarServesStoredBytes: another signed-in member fetches the
// picture as its stored type; an account with none is 404; no session is 401.
func TestGetUserAvatarServesStoredBytes(t *testing.T) {
	h, _ := avatarFixture(t)
	png := smallPNG(t)
	w := httptest.NewRecorder()
	h.UploadAvatar(w, avatarUploadReq(t, "u1", "image/png", png))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d (body %q)", w.Code, w.Body.String())
	}

	get := func(userID, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+target+"/avatar", nil)
		r = mux.SetURLVars(avatarReqAs(r, userID), map[string]string{"id": target})
		w := httptest.NewRecorder()
		h.GetUserAvatar(w, r)
		return w
	}

	w = get("u2", "u1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("served bytes differ from the upload")
	}
	if w := get("u1", "u2"); w.Code != http.StatusNotFound {
		t.Fatalf("no picture: status = %d, want 404", w.Code)
	}
	if w := get("u1", "nobody"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown user: status = %d, want 404", w.Code)
	}
	if w := get("", "u1"); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: status = %d, want 401", w.Code)
	}
}

// TestDeleteAvatarRemovesFileAndRecord: the file is gone, the record is
// cleared, and the answer reports no picture. Deleting when there is none
// is not an error.
func TestDeleteAvatarRemovesFileAndRecord(t *testing.T) {
	h, svc := avatarFixture(t)
	w := httptest.NewRecorder()
	h.UploadAvatar(w, avatarUploadReq(t, "u1", "image/png", smallPNG(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("upload status = %d (body %q)", w.Code, w.Body.String())
	}
	stored := svc.byID["u1"].AvatarPath

	del := func(userID string) *httptest.ResponseRecorder {
		r := avatarReqAs(httptest.NewRequest(http.MethodDelete, "/api/v1/me/avatar", nil), userID)
		w := httptest.NewRecorder()
		h.DeleteAvatar(w, r)
		return w
	}
	w = del("u1")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if u := decodeUser(t, w); u.HasAvatar || u.AvatarURL != "" {
		t.Fatalf("answer still carries a picture: %+v", u)
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatalf("picture still on disk (stat err %v)", err)
	}
	if svc.byID["u1"].AvatarPath != "" {
		t.Fatalf("record still points at %q", svc.byID["u1"].AvatarPath)
	}
	if w := del("u2"); w.Code != http.StatusOK {
		t.Fatalf("delete with no picture: status = %d, want 200", w.Code)
	}
	if w := del(""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: status = %d, want 401", w.Code)
	}
}
