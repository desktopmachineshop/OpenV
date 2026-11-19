package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// registerProjectCoverRoutes wires a project's cover photo: anyone who can
// see the project fetches it (its Overview and its card in All projects
// show it); editors upload and remove it.
func (h *Handler) registerProjectCoverRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/cover", h.GetProjectCover).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/cover", h.UploadProjectCover).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/cover", h.DeleteProjectCover).Methods("DELETE")
}

// stampCovers marks each project that has a cover photo with when it was
// stored (cover_updated_at). The cover is decoration: a failure to read the
// stamps is logged and the projects are answered without them.
func (h *Handler) stampCovers(r *http.Request, list ...*projects.Project) {
	if h.ProjectCoverService == nil || len(list) == 0 {
		return
	}
	if err := h.ProjectCoverService.Stamp(list...); err != nil {
		slog.Warn("api: failed to read project cover stamps", "path", r.URL.Path, "error", err)
	}
}

// coverUnavailable answers a deployment built without the cover service.
func (h *Handler) coverUnavailable(w http.ResponseWriter) bool {
	if h.ProjectCoverService == nil {
		writeJSONError(w, http.StatusNotFound, "project has no cover photo")
		return true
	}
	return false
}

// GetProjectCover serves the project's cover photo to anyone who can see
// the project; 404 when it has none. The bytes are served as the stored
// type only, never sniffed. The app asks for it with the cover's
// cover_updated_at in the query, so a changed cover is a new URL and the
// old one may be cached for a day.
func (h *Handler) GetProjectCover(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}
	if h.coverUnavailable(w) {
		return
	}
	cover, err := h.ProjectCoverService.Get(id)
	if errors.Is(err, projects.ErrNoCover) {
		writeJSONError(w, http.StatusNotFound, "project has no cover photo")
		return
	}
	if err != nil {
		respondInternal(w, r, "Failed to read cover photo", err)
		return
	}
	w.Header().Set("Content-Type", cover.Mime)
	w.Header().Set("Content-Length", fmt.Sprint(len(cover.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cover.Data)
}

// UploadProjectCover stores the project's cover photo (editor), replacing
// any it had, and answers the project. The multipart field "file" must be a
// PNG, JPEG, GIF or WebP whose bytes are the declared type, and at most
// projects.MaxCoverBytes (413 beyond that).
func (h *Handler) UploadProjectCover(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleEditor) {
		return
	}
	if h.coverUnavailable(w) {
		return
	}
	// The API-wide body cap skips a multipart request, so this handler's
	// own cap is the one in force: set before the form is parsed.
	r.Body = http.MaxBytesReader(w, r.Body, projects.MaxCoverBytes+multipartOverheadBytes)

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
	if _, ok := rasterImageExtensions[mimeType]; !ok {
		writeJSONError(w, http.StatusBadRequest, "Cover photo must be a PNG, JPEG, GIF or WebP image")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, projects.MaxCoverBytes+1))
	if err != nil {
		respondInternal(w, r, "Failed to read file", err)
		return
	}
	if len(data) > projects.MaxCoverBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Cover photo is larger than 5 MB")
		return
	}
	if !uploadIsRasterImage(mimeType, data) {
		writeJSONError(w, http.StatusBadRequest, "File content does not match an image of the declared type")
		return
	}
	if _, err := h.ProjectCoverService.Set(id, mimeType, data); err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			unknownProject.write(w)
			return
		}
		respondInternal(w, r, "Failed to save cover photo", err)
		return
	}
	h.writeProjectWithCover(w, r, id)
}

// DeleteProjectCover removes the project's cover photo (editor) and answers
// the project. A project with no cover is not an error.
func (h *Handler) DeleteProjectCover(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleEditor) {
		return
	}
	if h.coverUnavailable(w) {
		return
	}
	if err := h.ProjectCoverService.Delete(id); err != nil {
		respondInternal(w, r, "Failed to remove cover photo", err)
		return
	}
	h.writeProjectWithCover(w, r, id)
}

// writeProjectWithCover answers the project as it now stands, stamped.
func (h *Handler) writeProjectWithCover(w http.ResponseWriter, r *http.Request, id string) {
	project, err := h.ProjectService.GetProject(id)
	if err != nil {
		unknownProject.write(w)
		return
	}
	h.stampCovers(r, project)
	writeJSONOK(w, project)
}
