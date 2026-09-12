package api

// Profile pictures. An account may upload its own picture, which then stands
// in for whatever its identity provider supplied. It is stored the way a
// workspace logo is — one raster file per account under the uploads
// directory — and served from the API to any signed-in member, since a
// picture is shown wherever the account is: member lists, crew nodes, the
// account menu.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
)

func (h *Handler) registerAvatarRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/avatar", h.UploadAvatar).Methods("POST")
	router.HandleFunc("/api/v1/me/avatar", h.DeleteAvatar).Methods("DELETE")
	router.HandleFunc("/api/v1/users/{id}/avatar", h.GetUserAvatar).Methods("GET")
}

// maxAvatarBytes caps a profile picture upload. It is drawn small wherever
// it appears, so a picture larger than this is a mistake, not a need.
const maxAvatarBytes = 2 * 1024 * 1024

// avatarPath is where an account's picture of the given type is stored:
// one file per account under uploads/avatars, named by user id so an upload
// replaces the previous picture of the same type in place.
func (h *Handler) avatarPath(userID, ext string) string {
	return filepath.Join(h.uploadsDir, "avatars", userID+ext)
}

// avatarURL is the path clients fetch an uploaded picture from. It is
// relative to the API origin — the frontend resolves it against its API
// base — and carries the upload time so a browser that cached the previous
// picture under the same path fetches the new one.
func avatarURL(userID string, at time.Time) string {
	return fmt.Sprintf("/api/v1/users/%s/avatar?v=%d", userID, at.Unix())
}

// UploadAvatar stores the caller's profile picture. The multipart field
// "file" must be a PNG, JPEG, GIF or WebP whose bytes match the declared
// type, and at most maxAvatarBytes (413 beyond that). A previous picture of
// another type is removed so one account never leaves two files behind.
// Answers the updated user, whose avatar_url now points at the picture.
func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to get file from request")
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	ext, ok := rasterImageExtensions[mimeType]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "Picture must be a PNG, JPEG, GIF or WebP image")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		respondInternal(w, r, "Failed to read file", err)
		return
	}
	if len(data) > maxAvatarBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Picture is larger than 2 MB")
		return
	}
	if !uploadLooksLikeImage(mimeType, data) {
		writeJSONError(w, http.StatusBadRequest, "File content does not match an image of the declared type")
		return
	}

	dest := h.avatarPath(user.ID, ext)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		respondInternal(w, r, "Failed to save picture", err)
		return
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		respondInternal(w, r, "Failed to save picture", err)
		return
	}
	// The previous picture, if it was stored under another extension, is
	// now orphaned; drop it. The record is read before it is overwritten.
	if prev, err := h.userService.GetByID(user.ID); err == nil && prev != nil && prev.AvatarPath != "" && prev.AvatarPath != dest {
		_ = os.Remove(prev.AvatarPath)
	}
	updated, err := h.userService.SetAvatar(user.ID, dest, mimeType, avatarURL(user.ID, time.Now()))
	if err != nil {
		respondInternal(w, r, "Failed to save picture", err)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

// GetUserAvatar serves an account's uploaded picture to any signed-in
// member; 404 when none is uploaded (a provider-supplied picture is a URL
// elsewhere, not a file here). The bytes are served as the stored type
// only, never sniffed, and cached briefly: the URL changes on every upload,
// so a cached copy is never stale.
func (h *Handler) GetUserAvatar(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	owner, err := h.userService.GetByID(mux.Vars(r)["id"])
	if err != nil || owner == nil || owner.AvatarPath == "" {
		writeJSONError(w, http.StatusNotFound, "user has no uploaded picture")
		return
	}
	f, err := os.Open(owner.AvatarPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "user has no uploaded picture")
			return
		}
		respondInternal(w, r, "Failed to read picture", err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", owner.AvatarMime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

// DeleteAvatar removes the caller's uploaded picture: the file goes first
// (a missing one is not an error), then the record forgets it. The account
// is left without a picture; an identity provider's picture returns at its
// next sign-in. Answers the updated user.
func (h *Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	current, err := h.userService.GetByID(user.ID)
	if err != nil || current == nil {
		respondInternal(w, r, "Failed to remove picture", err)
		return
	}
	if current.AvatarPath != "" {
		if err := os.Remove(current.AvatarPath); err != nil && !os.IsNotExist(err) {
			respondInternal(w, r, "Failed to remove picture", err)
			return
		}
	}
	updated, err := h.userService.ClearAvatar(user.ID)
	if err != nil {
		respondInternal(w, r, "Failed to remove picture", err)
		return
	}
	json.NewEncoder(w).Encode(updated)
}
