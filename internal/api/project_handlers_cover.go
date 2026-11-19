package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

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
	router.HandleFunc("/api/v1/projects/{id}/cover/generate", h.GenerateProjectCover).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/cover/svg", h.DrawProjectCover).Methods("POST")
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

// DrawProjectCover sets the project's cover from an SVG drawing (editor) and
// answers the project. This is how an agent, which cannot make pixels but
// can write a drawing, makes a cover with no provider key: the server
// renders the drawing (projects.RenderCoverSVG) and stores the PNG; the SVG
// itself is never stored or served. A drawing it cannot render, or one with
// nothing it draws, is answered 400 with the reason, and nothing is stored.
func (h *Handler) DrawProjectCover(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleEditor) {
		return
	}
	if h.coverUnavailable(w) {
		return
	}
	var req struct {
		SVG string `json:"svg"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.SVG) == "" {
		writeJSONError(w, http.StatusBadRequest, "svg is required: the drawing, as SVG source")
		return
	}
	if _, err := h.ProjectCoverService.Draw(id, req.SVG); err != nil {
		switch {
		case errors.Is(err, projects.ErrCoverSVG):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, projects.ErrNotFound):
			unknownProject.write(w)
		default:
			respondInternal(w, r, "Failed to save cover photo", err)
		}
		return
	}
	h.writeProjectWithCover(w, r, id)
}

// noCoverKeyMessage answers a generation the workspace has no key for.
const noCoverKeyMessage = "Generating a cover photo uses the workspace's OpenAI API key: a workspace admin adds one under Workspace settings, AI providers"

// GenerateProjectCover makes the project's cover photo from a prompt
// (editor) and answers the project. The image is generated with the
// workspace's own OpenAI API key (projects.CoverKeyEnv), so the workspace
// pays for it and a workspace with none is answered 409 with what to do.
// Generations are rate-limited per workspace (coverGenLimiter): an agent in
// a loop must not run up the workspace's bill. The prompt is 1 to 1,000
// characters (400 otherwise); the provider's refusal is answered 502 with
// its reason. Nothing is stored unless an image comes back.
func (h *Handler) GenerateProjectCover(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleEditor) {
		return
	}
	if h.coverUnavailable(w) {
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if p := strings.TrimSpace(req.Prompt); p == "" || utf8.RuneCountInString(p) > projects.MaxCoverPromptRunes {
		writeJSONError(w, http.StatusBadRequest, projects.ErrCoverPrompt.Error())
		return
	}
	orgID := h.orgIDForProject(id)
	key := ""
	if h.ProviderKeyService != nil && orgID != "" {
		var err error
		if key, err = h.ProviderKeyService.RevealKey(orgID, projects.CoverKeyEnv); err != nil {
			respondInternal(w, r, "Failed to read the workspace's API key", err)
			return
		}
	}
	if key == "" {
		writeJSONError(w, http.StatusConflict, noCoverKeyMessage)
		return
	}
	if h.coverGenLimiter != nil {
		if ok, retry := h.coverGenLimiter.allow(orgID); !ok {
			writeRateLimited(w, "Too many cover photos generated in this workspace; try again later", retry)
			return
		}
	}
	if _, err := h.ProjectCoverService.Generate(r.Context(), id, key, req.Prompt); err != nil {
		switch {
		case errors.Is(err, projects.ErrCoverPrompt):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, projects.ErrNoCoverGenerator):
			writeJSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, projects.ErrCoverProvider):
			slog.Warn("api: cover generation failed", "project_id", id, "error", err)
			writeJSONError(w, http.StatusBadGateway, err.Error())
		case errors.Is(err, projects.ErrNotFound):
			unknownProject.write(w)
		default:
			respondInternal(w, r, "Failed to save cover photo", err)
		}
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

// workspaceHasKey reports whether the workspace has a pasted provider key
// for keyEnv, from the key list alone: the key itself is not opened.
func (h *Handler) workspaceHasKey(orgID, keyEnv string) bool {
	if h.ProviderKeyService == nil || orgID == "" {
		return false
	}
	keys, err := h.ProviderKeyService.ListKeys(orgID)
	if err != nil {
		return false
	}
	for _, k := range keys {
		if k.KeyEnv == keyEnv {
			return true
		}
	}
	return false
}
