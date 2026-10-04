package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerOrgLogoRoutes wires the workspace logo: any member may fetch it
// (it is shown in the app and on download cover pages); admins upload and
// remove it.
func (h *Handler) registerOrgLogoRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/logo", h.GetOrgLogo).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/logo", h.UploadOrgLogo).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/logo", h.DeleteOrgLogo).Methods("DELETE")
}

// maxOrgLogoBytes caps a workspace logo upload. A logo is a small raster
// image for a cover page, not an attachment, so it gets its own limit rather
// than the attachment cap.
const maxOrgLogoBytes = 2 * 1024 * 1024

// rasterImageExtensions maps the MIME types accepted for a workspace logo or
// a profile picture to the extension the file is stored under. Only inert
// raster formats are accepted: an SVG can carry script and is never rendered
// on the API origin.
var rasterImageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// uploadIsRasterImage reports whether data is an image of the declared type,
// one of rasterImageExtensions: the bytes must sniff as that very type
// (http.DetectContentType), not merely as some image, since a workspace logo
// and a profile picture are served as the type they were stored under and
// never sniffed. A GIF declared as a PNG is refused, and so is a BMP or an
// icon declared as any of the four.
func uploadIsRasterImage(declared string, data []byte) bool {
	if _, ok := rasterImageExtensions[declared]; !ok || len(data) == 0 {
		return false
	}
	return http.DetectContentType(data) == declared
}

// orgLogoPath is where a workspace's logo of the given type is stored:
// one file per workspace under uploads/org-logos, named by org id so an
// upload replaces the previous logo of the same type in place.
func (h *Handler) orgLogoPath(orgID, ext string) string {
	return filepath.Join(h.UploadsDir, "org-logos", orgID+ext)
}

// UploadOrgLogo stores a workspace logo (admin). The multipart field "file"
// must be a PNG, JPEG, GIF or WebP whose bytes are the declared type, and
// at most maxOrgLogoBytes (413 beyond that). A previous logo of another
// type is removed so one workspace never leaves two files behind.
func (h *Handler) UploadOrgLogo(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	// The workspace is looked up before anything is read or written: it
	// names the logo this upload replaces, and a file written for a
	// workspace that does not exist would stay on disk with no record of it.
	prev, err := h.OrgService.Get(orgID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "workspace not found", err)
		return
	}
	// The API-wide body cap skips a multipart request so that an upload
	// handler's own cap is the one in force, which means setting it here
	// before the form is parsed and spooled to disk.
	r.Body = http.MaxBytesReader(w, r.Body, maxOrgLogoBytes+multipartOverheadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		if uploadReadRefused(w, err) {
			return
		}
		writeJSONError(w, http.StatusBadRequest, "Failed to get file from request")
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	ext, ok := rasterImageExtensions[mimeType]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "Logo must be a PNG, JPEG, GIF or WebP image")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxOrgLogoBytes+1))
	if err != nil {
		respondInternal(w, r, "Failed to read file", err)
		return
	}
	if len(data) > maxOrgLogoBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Logo is larger than 2 MB")
		return
	}
	if !uploadIsRasterImage(mimeType, data) {
		writeJSONError(w, http.StatusBadRequest, "File content does not match an image of the declared type")
		return
	}

	dest := h.orgLogoPath(orgID, ext)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		respondInternal(w, r, "Failed to save logo", err)
		return
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		respondInternal(w, r, "Failed to save logo", err)
		return
	}
	// The logo this one replaces is the one on record now, not when the
	// request arrived: another upload may have recorded its own while this
	// body was being received.
	if cur, err := h.OrgService.Get(orgID); err == nil {
		prev = cur
	}
	org, err := h.OrgService.SetLogo(orgID, dest, mimeType)
	if err != nil {
		// Unrecorded, the new file is an orphan unless it replaced the
		// recorded logo in place.
		if dest != prev.LogoPath {
			_ = os.Remove(dest)
		}
		respondInternal(w, r, "Failed to save logo", err)
		return
	}
	// The previous logo, if it was stored under another extension, is now
	// orphaned; drop it. The record was read before it was overwritten.
	if prev.LogoPath != "" && prev.LogoPath != dest {
		_ = os.Remove(prev.LogoPath)
	}
	json.NewEncoder(w).Encode(org)
}

// GetOrgLogo serves the workspace logo to any member; 404 when none is set.
// The bytes are served as the stored type only, never sniffed, and cached
// briefly per user so a settings page or report preview does not refetch it
// on every render.
func (h *Handler) GetOrgLogo(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	org, err := h.OrgService.Get(orgID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "workspace not found", err)
		return
	}
	if org.LogoPath == "" {
		writeJSONError(w, http.StatusNotFound, "workspace has no logo")
		return
	}
	f, err := os.Open(org.LogoPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "workspace has no logo")
			return
		}
		respondInternal(w, r, "Failed to read logo", err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", org.LogoMime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

// DeleteOrgLogo removes the workspace logo (admin): the file goes first
// (a missing one is not an error), then the record forgets it.
func (h *Handler) DeleteOrgLogo(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	org, err := h.OrgService.Get(orgID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "workspace not found", err)
		return
	}
	if org.LogoPath != "" {
		if err := os.Remove(org.LogoPath); err != nil && !os.IsNotExist(err) {
			respondInternal(w, r, "Failed to remove logo", err)
			return
		}
	}
	org, err = h.OrgService.ClearLogo(orgID)
	if err != nil {
		respondInternal(w, r, "Failed to remove logo", err)
		return
	}
	json.NewEncoder(w).Encode(org)
}
