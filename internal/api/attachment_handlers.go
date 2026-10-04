package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// registerAttachmentRoutes wires attachments (figures): upload, read,
// rename, download, versions, delete, and the lists per artifact and per
// project.
func (h *Handler) registerAttachmentRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/attachments/upload", h.UploadAttachment).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}", h.GetAttachmentMeta).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}", h.RenameAttachment).Methods("PUT")
	router.HandleFunc("/api/v1/attachments/{id}/download", h.DownloadAttachment).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}/versions", h.UploadAttachmentVersion).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}/versions", h.ListAttachmentVersions).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}/versions/{version}/restore", h.RestoreAttachmentVersion).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}", h.DeleteAttachment).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{artifactID}/attachments", h.ListArtifactAttachments).Methods("GET")
	router.HandleFunc("/api/v1/projects/{projectID}/attachments", h.ListProjectAttachments).Methods("GET")
}

// UploadAttachment attaches a file to an artifact as a numbered figure.
//
// What may be attached is the catalogue in the attachments domain: pictures,
// PDFs and CAD files. The type the platform records comes from there rather
// than from the browser, which reports application/octet-stream for most CAD
// formats and cannot be trusted to name one.
func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	// Before anything reads the form: FormValue parses the whole multipart
	// body and spools its parts to temp files, so the request has to be
	// bounded while the workspace it belongs to is still unknown. The
	// workspace's own limit is applied to the bytes below.
	r.Body = http.MaxBytesReader(w, r.Body, h.uploadRequestCeilingBytes())

	artifactID := r.FormValue("artifact_id")
	if artifactID == "" {
		writeJSONError(w, http.StatusBadRequest, "artifact_id is required")
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(artifactID), members.RoleEditor) {
		return
	}

	limit := h.uploadLimitBytes(h.orgIDForProject(h.projectIDForArtifact(artifactID)))

	file, header, err := r.FormFile("file")
	if err != nil {
		if uploadReadRefused(w, err) {
			return
		}
		writeJSONError(w, http.StatusBadRequest, "Failed to get file from request")
		return
	}
	defer file.Close()

	mimeType, kind, accepted := attachments.AcceptUpload(header.Header.Get("Content-Type"), header.Filename)
	if !accepted {
		writeJSONError(w, http.StatusBadRequest, unsupportedFigureMessage)
		return
	}
	if !h.mayAttachKind(r, artifactID, kind) {
		writeJSONError(w, http.StatusBadRequest, nonImageGateMessage)
		return
	}

	// On-disk names stay UUID-unique: the uploads directory is flat across
	// every project, figure references are only unique within one, and each
	// version of a figure needs a file of its own. The figure's name is what
	// the record carries and what a download is served as.
	storedPath := filepath.Join(h.UploadsDir, fmt.Sprintf("%s_%s", uuid.New().String(), header.Filename))
	head, fileSize, ok := storeUpload(w, r, file, storedPath, limit)
	if !ok {
		return
	}
	if !uploadLooksLikeFigure(mimeType, header.Filename, head) {
		_ = os.Remove(storedPath)
		writeJSONError(w, http.StatusBadRequest, "File content does not match the format its name gives it")
		return
	}

	// The figure reference is built on the artifact's own reference, so the
	// artifact is read before the number is drawn.
	artifactRef := ""
	if a, err := h.ArtifactService.GetArtifact(artifactID); err == nil && a != nil {
		artifactRef = a.Ref
	}

	// An optional title names the figure from the start; it is bounded the
	// same way a rename is.
	title := strings.TrimSpace(r.FormValue("title"))
	if len([]rune(title)) > attachments.MaxTitleLen {
		_ = os.Remove(storedPath)
		writeJSONError(w, http.StatusBadRequest, attachments.ErrTitleTooLong.Error())
		return
	}

	attachment := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID:       artifactID,
		Filename:         header.Filename,
		OriginalFilename: header.Filename,
		Title:            title,
		MimeType:         mimeType,
		FilePath:         storedPath,
		FileSize:         int(fileSize),
	})

	if err := h.AttachmentService.CreateFigure(attachment, artifactRef); err != nil {
		// Clean up file if database save fails
		_ = os.Remove(storedPath)
		respondInternal(w, r, "Failed to save attachment metadata", err)
		return
	}

	// A new figure is an edit of its artifact, as its new file is (REQ-4).
	h.versionArtifactForFigure(artifactID, attachment.ID, "upload")

	if attachment.FigureRef != "" {
		h.logFigureNote(r, artifactID, fmt.Sprintf("Figure %s added (version 1) — %s.",
			attachment.FigureRef, attachment.Name()))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(attachment)
}

// GetAttachmentMeta retrieves attachment metadata
func (h *Handler) GetAttachmentMeta(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.AttachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleViewer, missing("Attachment not found")) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachment)
}

// versionArtifactForFigure takes the artifact carrying a figure to a new
// version after the figure changed: added, given a new file or renamed.
// Nothing the artifact SAYS changed, so this goes through an attribute-free
// update: it does not demote an approved artifact or mark its links suspect.
// A failure is logged rather than failing the figure change that succeeded;
// change names it in the log ("upload", "change", "rename").
func (h *Handler) versionArtifactForFigure(artifactID, attachmentID, change string) {
	if _, err := h.ArtifactService.UpdateArtifact(artifactID, artifacts.UpdateArtifactRequest{}); err != nil {
		slog.Warn("api: failed to version artifact after a figure "+change,
			"artifact_id", artifactID, "attachment_id", attachmentID, "error", err)
	}
}

// logFigureNote records a figure event in the artifact's notes. The feed is
// where a reader looks to find out why a drawing changed, so a failure to write
// one is logged rather than failing the upload that succeeded.
func (h *Handler) logFigureNote(r *http.Request, artifactID, message string) {
	h.logAutoNote(r, artifactID, message, "figure-change")
}

// UploadAttachmentVersion replaces a figure's image with a new version.
//
// The figure keeps its reference and its place in the document; what changes
// is which file it points at. Because the artifact now says something
// different to a reader, the artifact takes a new version too, and the notes
// record the move from one figure version to the next.
func (h *Handler) UploadAttachmentVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	existing, err := h.AttachmentService.GetAttachment(id)
	if err != nil || existing == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}
	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(existing.ArtifactID), members.RoleEditor, missing("Attachment not found")) {
		return
	}

	// The workspace is known here (the attachment named it), so the request
	// is bounded by its own limit rather than by the deployment's ceiling.
	limit := h.uploadLimitBytes(h.orgIDForProject(h.projectIDForArtifact(existing.ArtifactID)))
	r.Body = http.MaxBytesReader(w, r.Body, limit+multipartOverheadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		if uploadReadRefused(w, err) {
			return
		}
		writeJSONError(w, http.StatusBadRequest, "Failed to get file from request")
		return
	}
	defer file.Close()

	mimeType, kind, accepted := attachments.AcceptUpload(header.Header.Get("Content-Type"), header.Filename)
	if !accepted {
		writeJSONError(w, http.StatusBadRequest, unsupportedFigureMessage)
		return
	}
	if !h.mayAttachKind(r, existing.ArtifactID, kind) {
		writeJSONError(w, http.StatusBadRequest, nonImageGateMessage)
		return
	}

	// A new file, not a rewrite of the old one: the superseded version must
	// stay readable.
	storedPath := filepath.Join(h.UploadsDir, fmt.Sprintf("%s_%s", uuid.New().String(), header.Filename))
	head, fileSize, ok := storeUpload(w, r, file, storedPath, limit)
	if !ok {
		return
	}
	if !uploadLooksLikeFigure(mimeType, header.Filename, head) {
		_ = os.Remove(storedPath)
		writeJSONError(w, http.StatusBadRequest, "File content does not match the format its name gives it")
		return
	}

	version := &attachments.Version{
		Filename:         header.Filename,
		OriginalFilename: header.Filename,
		MimeType:         mimeType,
		FilePath:         storedPath,
		FileSize:         int(fileSize),
	}
	if user := CurrentUser(r); user != nil {
		version.CreatedBy = &user.ID
	}

	previous := existing.Version
	next, err := h.AttachmentService.AddVersion(id, version)
	if err != nil {
		_ = os.Remove(storedPath)
		respondInternal(w, r, "Failed to record the new figure version", err)
		return
	}
	if next == 0 {
		_ = os.Remove(storedPath)
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	// The artifact carries the figure, so a new figure version is a new
	// artifact version.
	h.versionArtifactForFigure(existing.ArtifactID, id, "change")

	label := existing.FigureRef
	if label == "" {
		label = existing.Filename
	}
	h.logFigureNote(r, existing.ArtifactID, fmt.Sprintf(
		"Figure %s updated from version %d to %d — %s.", label, previous, next, version.OriginalFilename))

	updated, err := h.AttachmentService.GetAttachment(id)
	if err != nil || updated == nil {
		updated = existing
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(updated)
}

// RenameAttachment gives a figure a title (REQ-157).
//
// The name is part of what the document says under the picture, so it is
// tracked the way the picture is: the figure takes a new version recording
// the title, who set it and when; the artifact takes a new version through
// the same attribute-free update a new image uses (no demotion, no suspect
// links); and the notes record the old and new names. An unchanged title is
// a no-op that writes nothing.
func (h *Handler) RenameAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	existing, err := h.AttachmentService.GetAttachment(id)
	if err != nil || existing == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}
	projectID := h.projectIDForArtifact(existing.ArtifactID)
	if !h.requireProjectRoleFor(w, r, projectID, members.RoleEditor, missing("Attachment not found")) {
		return
	}
	if !h.projectFeatureEnabled(r, projectID, release.FeatureFigureTitles) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}

	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == existing.Title {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(existing)
		return
	}

	// Captured before the rename so the note names what the figure was
	// called, whatever the service hands back afterwards.
	was, previous := existing.Name(), existing.Version
	next, err := h.AttachmentService.RenameFigure(id, title, CurrentUserID(r))
	if errors.Is(err, attachments.ErrTitleTooLong) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		respondInternal(w, r, "Failed to rename the figure", err)
		return
	}
	if next == 0 {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	h.versionArtifactForFigure(existing.ArtifactID, id, "rename")

	label := existing.FigureRef
	if label == "" {
		label = existing.Filename
	}
	now := title
	if now == "" {
		now = existing.OriginalFilename
	}
	h.logFigureNote(r, existing.ArtifactID, fmt.Sprintf(
		"Figure %s renamed (version %d to %d) — %q is now %q.", label, previous, next, was, now))

	updated, err := h.AttachmentService.GetAttachment(id)
	if err != nil || updated == nil {
		updated = existing
		updated.Title = title
		updated.Version = next
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// ListAttachmentVersions returns a figure's version history, newest first.
func (h *Handler) ListAttachmentVersions(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.AttachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}
	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleViewer, missing("Attachment not found")) {
		return
	}

	versions, err := h.AttachmentService.GetVersions(id)
	if err != nil {
		respondInternal(w, r, "Failed to list figure versions", err)
		return
	}
	if versions == nil {
		versions = []*attachments.Version{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

// RestoreAttachmentVersion brings an older version of a figure back as a new
// version. Editor role, like uploading one: it changes what the figure
// shows.
func (h *Handler) RestoreAttachmentVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	version, err := strconv.Atoi(mux.Vars(r)["version"])
	if err != nil || version < 1 {
		writeJSONError(w, http.StatusBadRequest, "version must be a positive whole number")
		return
	}

	attachment, err := h.AttachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}
	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleEditor, missing("Attachment not found")) {
		return
	}

	restored, err := h.AttachmentService.RestoreVersion(id, version, CurrentUserID(r))
	switch {
	case errors.Is(err, attachments.ErrNoSuchVersion):
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("This figure has no version %d", version))
		return
	case errors.Is(err, attachments.ErrAlreadyCurrent):
		// Not an error the member made: they asked for the state the figure
		// is already in. Say so rather than writing a version that changes
		// nothing.
		writeJSONError(w, http.StatusConflict, fmt.Sprintf("Version %d is already the current one", version))
		return
	case err != nil:
		respondInternal(w, r, "Failed to restore the figure version", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(restored)
}

// DownloadAttachment serves the attachment file
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.AttachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	// Session-cookie auth works here too, so <img> tags keep rendering.
	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleViewer, missing("Attachment not found")) {
		return
	}

	// ?version=N serves a superseded version; without it the current one. The
	// version is part of the URL, so a new version is a new URL and the long
	// cache below never serves a stale drawing.
	name, mime, path, size := attachment.Filename, attachment.MimeType, attachment.FilePath, attachment.FileSize
	if raw := r.URL.Query().Get("version"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeJSONError(w, http.StatusBadRequest, "version must be a positive number")
			return
		}
		if n != attachment.Version {
			v, err := h.AttachmentService.GetVersion(id, n)
			if err != nil || v == nil {
				writeJSONError(w, http.StatusNotFound, "That version of the figure was not found")
				return
			}
			name, mime, path, size = v.Filename, v.MimeType, v.FilePath, v.FileSize
		}
	}

	// Set appropriate headers for image serving
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(size))
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	// Saving the image should land a file named for the figure, not for
	// whatever the uploader happened to call it.
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", attachmentDisposition(mime), name))
	setAttachmentSecurityHeaders(w, mime)

	// Serve the file
	http.ServeFile(w, r, path)
}

// DeleteAttachment deletes an attachment
func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.AttachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleEditor, missing("Attachment not found")) {
		return
	}

	// Delete file from disk
	if err := os.Remove(attachment.FilePath); err != nil && !os.IsNotExist(err) {
		respondInternal(w, r, "Failed to delete file", err)
		return
	}

	// Delete database record
	if err := h.AttachmentService.DeleteAttachment(id); err != nil {
		respondInternal(w, r, "Failed to delete attachment", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListProjectAttachments lists every figure in a project.
//
// It exists for cross-artifact figure references: the editor has to offer the
// project's figures under "##" before the writer has any idea which artifact
// holds the one they want, and a reader following such a citation has to be
// able to open a figure that hangs on an artifact they are not looking at.
// Fetching them one artifact at a time would be a request per artifact for
// what is one small list.
func (h *Handler) ListProjectAttachments(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["projectID"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	list, err := h.AttachmentService.GetAttachmentsByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list the project's attachments", err)
		return
	}
	if list == nil {
		list = []*attachments.Attachment{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// ListArtifactAttachments lists all attachments for an artifact
func (h *Handler) ListArtifactAttachments(w http.ResponseWriter, r *http.Request) {
	artifactID := mux.Vars(r)["artifactID"]

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(artifactID), members.RoleViewer) {
		return
	}

	attachmentList, err := h.AttachmentService.GetAttachmentsByArtifact(artifactID)
	if err != nil {
		respondInternal(w, r, "failed to list attachments", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachmentList)
}

// unsupportedFigureMessage names what went wrong in terms an uploader can act
// on. The catalogue is long enough that listing it here would be noise, so it
// names the families instead and leaves the detail to the manual.
// nonImageGateMessage answers a workspace that has not received the wider
// catalogue yet. It is deliberately about the release rather than the format:
// the file is fine, the workspace's channel is what has not caught up.
const nonImageGateMessage = "Attaching PDFs and CAD files " + featureGateMessage

const unsupportedFigureMessage = "That file type cannot be attached. Figures may be images (PNG, JPEG, GIF, WebP, SVG, TIFF, BMP), PDFs, or CAD files (STEP, IGES, STL, 3MF, OBJ, glTF, DXF, DWG and the common native formats)."

// mayAttachKind gates the formats beyond images (REQ-137).
//
// Only what may be ATTACHED is gated. Reading is not: a figure a colleague on
// the nightly channel attached opens for everybody, because a gate that made
// an existing file unreadable would be a regression dressed as a release
// policy. An image is always attachable — that is what the feature widened
// FROM, not something it introduced.
func (h *Handler) mayAttachKind(r *http.Request, artifactID string, kind attachments.Kind) bool {
	if kind == attachments.KindImage {
		return true
	}
	return h.projectFeatureEnabled(r, h.projectIDForArtifact(artifactID), release.FeatureAttachmentFormats)
}
