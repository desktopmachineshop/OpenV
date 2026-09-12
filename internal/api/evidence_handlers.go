package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Evidence bundles: the files and written accounts behind a physical or manual
// test result.
//
// This is a second upload path, beside the figure one, and every rule differs
// because a dataset is not a picture. A figure is an image, small, and shown
// inline; a capture from a rig is any format at all, large, and never
// rendered. Sharing the figure path would have meant loosening its image
// checks for every artifact in the product, which is the opposite of what
// those checks are for.
func (h *Handler) registerEvidenceRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/evidence-bundles", h.ListEvidenceBundles).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/evidence-bundles", h.CreateEvidenceBundle).Methods("POST")
	router.HandleFunc("/api/v1/evidence-bundles/{id}", h.GetEvidenceBundle).Methods("GET")
	router.HandleFunc("/api/v1/evidence-bundles/{id}", h.UpdateEvidenceBundle).Methods("PUT")
	router.HandleFunc("/api/v1/evidence-bundles/{id}", h.DeleteEvidenceBundle).Methods("DELETE")
	router.HandleFunc("/api/v1/evidence-bundles/{id}/files", h.UploadEvidenceFile).Methods("POST")
	router.HandleFunc("/api/v1/evidence-files/{id}/download", h.DownloadEvidenceFile).Methods("GET")
	router.HandleFunc("/api/v1/evidence-files/{id}", h.DeleteEvidenceFile).Methods("DELETE")
	router.HandleFunc("/api/v1/test-results/{id}/citations", h.ListResultCitations).Methods("GET")
	router.HandleFunc("/api/v1/test-results/{id}/citations", h.CiteEvidence).Methods("POST")
	router.HandleFunc("/api/v1/test-results/{id}/citations/{bundleId}", h.UnciteEvidence).Methods("DELETE")
	router.HandleFunc("/api/v1/test-runs/{id}/citations", h.ListRunCitations).Methods("GET")
}

// respondJSON writes a status and a JSON body, which every handler here does.
func respondJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// nextFilePart walks a multipart stream to the first part that carries a file,
// skipping ordinary form fields. Walking rather than parsing the whole form is
// what keeps the upload streamed: ParseMultipartForm would buffer it first.
func nextFilePart(reader *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if part.FileName() != "" {
			return part, nil
		}
		_ = part.Close()
	}
}

// envMaxEvidenceMB caps one uploaded evidence file. It is separate from
// OPENV_MAX_UPLOAD_MB, which governs figures: a 25 MB ceiling is right for an
// image pasted into a requirement and useless for an instrument capture.
const (
	envMaxEvidenceMB     = "OPENV_MAX_EVIDENCE_MB"
	defaultMaxEvidenceMB = 200
)

func maxEvidenceBytes() int64 {
	mb := int64(defaultMaxEvidenceMB)
	if v := os.Getenv(envMaxEvidenceMB); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			mb = n
		}
	}
	return mb * 1024 * 1024
}

// evidenceStorageLimitBytes is the workspace's total evidence allowance, from
// the org limits. Zero means unlimited.
func (h *Handler) evidenceStorageLimitBytes(orgID string) int64 {
	if h.orgService == nil {
		return 0
	}
	org, err := h.orgService.Get(orgID)
	if err != nil || org == nil {
		return 0
	}
	if v, ok := orgs.LimitFloat(org.EffectiveLimits(), orgs.LimitEvidenceStorageMB); ok && v > 0 {
		return int64(v) * 1024 * 1024
	}
	return 0
}

// evidenceBundleChecked loads a bundle and enforces the caller's role on its
// project. It answers 404 for a bundle that does not exist and for one the
// caller may not see, so the endpoint cannot be used to discover that a
// bundle exists in a project they have no access to.
func (h *Handler) evidenceBundleChecked(w http.ResponseWriter, r *http.Request, id, minRole string) *evidence.Bundle {
	if h.evidenceService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return nil
	}
	bundle, err := h.evidenceService.Get(id)
	if errors.Is(err, evidence.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "evidence bundle not found")
		return nil
	}
	if err != nil {
		respondInternal(w, r, "failed to load the evidence bundle", err)
		return nil
	}
	if !h.requireProjectRole(w, r, bundle.ProjectID, minRole) {
		return nil
	}
	return bundle
}

// writeEvidenceError maps the domain's errors onto status codes once, so every
// handler answers the same way.
func (h *Handler) writeEvidenceError(w http.ResponseWriter, r *http.Request, verb string, err error) {
	switch {
	case errors.Is(err, evidence.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "evidence bundle not found")
	case errors.Is(err, evidence.ErrFileNotFound):
		writeJSONError(w, http.StatusNotFound, "evidence file not found")
	case errors.Is(err, evidence.ErrInvalid):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, evidence.ErrQuotaExceeded):
		writeJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		respondInternal(w, r, verb, err)
	}
}

// ListEvidenceBundles returns a project's capture sessions, newest first.
func (h *Handler) ListEvidenceBundles(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if h.evidenceService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return
	}
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.evidenceService.List(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list evidence bundles", err)
		return
	}
	if list == nil {
		list = []*evidence.Bundle{}
	}
	respondJSON(w, http.StatusOK, list)
}

// CreateEvidenceBundle records a new capture session.
func (h *Handler) CreateEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if h.evidenceService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return
	}
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req evidence.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var createdBy *string
	if u := CurrentUser(r); u != nil {
		id := u.ID
		createdBy = &id
	}
	bundle, err := h.evidenceService.Create(projectID, req, createdBy)
	if err != nil {
		h.writeEvidenceError(w, r, "failed to create the evidence bundle", err)
		return
	}
	respondJSON(w, http.StatusCreated, bundle)
}

// GetEvidenceBundle returns one bundle with its files and the results citing
// it.
func (h *Handler) GetEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	bundle := h.evidenceBundleChecked(w, r, mux.Vars(r)["id"], members.RoleViewer)
	if bundle == nil {
		return
	}
	respondJSON(w, http.StatusOK, bundle)
}

// UpdateEvidenceBundle edits a bundle's description of what was done. The ref
// and the project are not editable: a citable reference that moved would break
// every document quoting it.
func (h *Handler) UpdateEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if h.evidenceBundleChecked(w, r, id, members.RoleEditor) == nil {
		return
	}
	var req evidence.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bundle, err := h.evidenceService.Update(id, req)
	if err != nil {
		h.writeEvidenceError(w, r, "failed to update the evidence bundle", err)
		return
	}
	respondJSON(w, http.StatusOK, bundle)
}

// DeleteEvidenceBundle removes a bundle, its files and the citations against
// it. This is the destructive path: the results that cited it keep their
// recorded outcome but lose what backed it, so the UI warns with the list
// first.
func (h *Handler) DeleteEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if h.evidenceBundleChecked(w, r, id, members.RoleEditor) == nil {
		return
	}
	files, err := h.evidenceService.Delete(id)
	if err != nil {
		h.writeEvidenceError(w, r, "failed to delete the evidence bundle", err)
		return
	}
	// The rows are gone; the bytes follow. A file that will not unlink is
	// logged rather than failed: the record is what the product is about, and
	// re-reporting a delete that already happened would be worse.
	removeEvidenceFiles(files)
	w.WriteHeader(http.StatusNoContent)
}

// UploadEvidenceFile streams one file into the bundle.
//
// Streamed, not buffered: the figure path reads the whole upload into memory,
// which is fine for a 25 MB image and not for a 200 MB capture. The bytes go
// straight to disk through a hash, so the API's memory does not scale with
// the size of somebody's dataset.
func (h *Handler) UploadEvidenceFile(w http.ResponseWriter, r *http.Request) {
	bundleID := mux.Vars(r)["id"]
	bundle := h.evidenceBundleChecked(w, r, bundleID, members.RoleEditor)
	if bundle == nil {
		return
	}

	limit := maxEvidenceBytes()
	// MaxBytesReader bounds the whole request, so a client cannot stream
	// unbounded data at the server regardless of what the multipart headers
	// claim.
	r.Body = http.MaxBytesReader(w, r.Body, limit+1024*1024)

	reader, err := r.MultipartReader()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}
	part, err := nextFilePart(reader)
	if err != nil || part == nil {
		writeJSONError(w, http.StatusBadRequest, "no file in the request")
		return
	}
	defer part.Close()

	// The quota is checked before the write and again against the real size
	// after it. Before, so an upload that obviously cannot fit is refused
	// without spending the disk; after, because the declared size is not
	// evidence of anything and only the bytes that landed are.
	orgID := h.orgIDForProject(bundle.ProjectID)
	storageLimit := h.evidenceStorageLimitBytes(orgID)
	if err := h.evidenceService.CheckQuota(bundle.ProjectID, 0, storageLimit); err != nil {
		h.writeEvidenceError(w, r, "failed to check the evidence storage limit", err)
		return
	}

	filename := filepath.Base(part.FileName())
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "evidence"
	}
	// Flat directory, UUID-prefixed name: evidence filenames are the
	// uploader's and are not unique across projects, and the stored name must
	// never be able to escape the uploads directory.
	storedPath := filepath.Join(h.uploadsDir, "evidence-"+uuid.New().String())
	dst, err := os.Create(storedPath)
	if err != nil {
		respondInternal(w, r, "failed to store the evidence file", err)
		return
	}

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(part, limit+1))
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(storedPath)
		if copyErr != nil && strings.Contains(copyErr.Error(), "request body too large") {
			writeJSONError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("that file is larger than the %s evidence upload limit",
					evidence.HumanBytes(limit)))
			return
		}
		respondInternal(w, r, "failed to store the evidence file", errors.Join(copyErr, closeErr))
		return
	}
	if written > limit {
		_ = os.Remove(storedPath)
		writeJSONError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("that file is larger than the %s evidence upload limit", evidence.HumanBytes(limit)))
		return
	}
	if err := h.evidenceService.CheckQuota(bundle.ProjectID, written, storageLimit); err != nil {
		_ = os.Remove(storedPath)
		h.writeEvidenceError(w, r, "failed to check the evidence storage limit", err)
		return
	}

	var uploadedBy *string
	if u := CurrentUser(r); u != nil {
		id := u.ID
		uploadedBy = &id
	}
	file := &evidence.File{
		Filename: filename,
		// The declared type is recorded for display only. Nothing is served
		// back inline, so an untrue Content-Type cannot be used to have the
		// browser render something as a document.
		MimeType:   part.Header.Get("Content-Type"),
		FilePath:   storedPath,
		FileSize:   written,
		SHA256:     hex.EncodeToString(hash.Sum(nil)),
		UploadedBy: uploadedBy,
	}
	if err := h.evidenceService.AddFile(bundleID, file); err != nil {
		_ = os.Remove(storedPath)
		h.writeEvidenceError(w, r, "failed to record the evidence file", err)
		return
	}
	respondJSON(w, http.StatusCreated, file)
}

// DownloadEvidenceFile hands the bytes over as a file, never as a document.
//
// An evidence file can be any format, including ones that carry script, so
// unlike a figure it is always Content-Disposition: attachment under a
// default-src 'none' policy with nosniff. There is no case where rendering one
// on the API origin is the right thing.
func (h *Handler) DownloadEvidenceFile(w http.ResponseWriter, r *http.Request) {
	if h.evidenceService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return
	}
	file, err := h.evidenceService.GetFile(mux.Vars(r)["id"])
	if err != nil {
		h.writeEvidenceError(w, r, "failed to load the evidence file", err)
		return
	}
	bundle := h.evidenceBundleChecked(w, r, file.BundleID, members.RoleViewer)
	if bundle == nil {
		return
	}

	writeEvidenceDownloadHeaders(w, file)
	http.ServeFile(w, r, file.FilePath)
}

// DeleteEvidenceFile removes one file from a bundle.
func (h *Handler) DeleteEvidenceFile(w http.ResponseWriter, r *http.Request) {
	if h.evidenceService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return
	}
	file, err := h.evidenceService.GetFile(mux.Vars(r)["id"])
	if err != nil {
		h.writeEvidenceError(w, r, "failed to load the evidence file", err)
		return
	}
	if h.evidenceBundleChecked(w, r, file.BundleID, members.RoleEditor) == nil {
		return
	}
	removed, err := h.evidenceService.DeleteFile(file.ID)
	if err != nil {
		h.writeEvidenceError(w, r, "failed to delete the evidence file", err)
		return
	}
	removeEvidenceFiles([]*evidence.File{removed})
	w.WriteHeader(http.StatusNoContent)
}

// ListResultCitations returns the bundles one result rests on.
func (h *Handler) ListResultCitations(w http.ResponseWriter, r *http.Request) {
	resultID := mux.Vars(r)["id"]
	if !h.evidenceResultAllowed(w, r, resultID, members.RoleViewer) {
		return
	}
	list, err := h.evidenceService.CitationsForResult(resultID)
	if err != nil {
		respondInternal(w, r, "failed to list the result's evidence", err)
		return
	}
	if list == nil {
		list = []*evidence.Citation{}
	}
	respondJSON(w, http.StatusOK, list)
}

// ListRunCitations returns a run's citations keyed by test result id, so the
// run grid renders its evidence column in one request rather than one per row.
func (h *Handler) ListRunCitations(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if h.evidenceService == nil || h.vvService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return
	}
	run, err := h.vvService.GetRun(runID)
	if err != nil || run == nil {
		writeJSONError(w, http.StatusNotFound, "test run not found")
		return
	}
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleViewer) {
		return
	}
	byResult, err := h.evidenceService.CitationsForRun(runID)
	if err != nil {
		respondInternal(w, r, "failed to list the run's evidence", err)
		return
	}
	if byResult == nil {
		byResult = map[string][]*evidence.Citation{}
	}
	respondJSON(w, http.StatusOK, byResult)
}

// CiteEvidence records that a result rests on a bundle.
func (h *Handler) CiteEvidence(w http.ResponseWriter, r *http.Request) {
	resultID := mux.Vars(r)["id"]
	if !h.evidenceResultAllowed(w, r, resultID, members.RoleEditor) {
		return
	}
	var req struct {
		BundleID string `json:"bundle_id"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BundleID == "" {
		writeJSONError(w, http.StatusBadRequest, "bundle_id is required")
		return
	}
	// The bundle has to be one the caller may edit, or citing would be a way
	// to attach evidence from a project they have no part in.
	if h.evidenceBundleChecked(w, r, req.BundleID, members.RoleViewer) == nil {
		return
	}
	citation, err := h.evidenceService.Cite(resultID, req.BundleID, req.Note)
	if err != nil {
		h.writeEvidenceError(w, r, "failed to cite the evidence", err)
		return
	}
	respondJSON(w, http.StatusCreated, citation)
}

// UnciteEvidence drops a result's claim on a bundle. The bundle is untouched:
// this says "this outcome no longer rests on that capture", not "delete it".
func (h *Handler) UnciteEvidence(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.evidenceResultAllowed(w, r, vars["id"], members.RoleEditor) {
		return
	}
	if err := h.evidenceService.Uncite(vars["id"], vars["bundleId"]); err != nil {
		h.writeEvidenceError(w, r, "failed to remove the citation", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// evidenceResultAllowed resolves a test result to its project and enforces the
// caller's role there.
func (h *Handler) evidenceResultAllowed(w http.ResponseWriter, r *http.Request, resultID, minRole string) bool {
	if h.evidenceService == nil || h.vvService == nil {
		writeJSONError(w, http.StatusNotFound, "evidence is not configured on this server")
		return false
	}
	projectID, err := h.evidenceService.ProjectForResult(resultID)
	if err != nil {
		respondInternal(w, r, "failed to resolve the test result", err)
		return false
	}
	if projectID == "" {
		writeJSONError(w, http.StatusNotFound, "test result not found")
		return false
	}
	return h.requireProjectRole(w, r, projectID, minRole)
}

// removeEvidenceFiles unlinks stored bytes after their rows have gone. A
// failure is logged, not returned: the record is already correct, and telling
// the caller their delete failed would be false.
func removeEvidenceFiles(files []*evidence.File) {
	for _, f := range files {
		if f == nil || f.FilePath == "" {
			continue
		}
		if err := os.Remove(f.FilePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("evidence: failed to remove a file from disk", "file_id", f.ID, "error", err)
		}
	}
}

// writeEvidenceDownloadHeaders is the whole serving policy for an evidence
// file, in one place because it is a security decision rather than a detail.
//
// The declared type is ignored: an evidence file may be any format, including
// ones that carry script, so it is handed over as an opaque download under a
// policy that permits nothing and may not be sniffed into something else.
// There is no case where rendering one on the API origin is right, which is
// why — unlike a figure — there is no inline branch to get wrong.
func writeEvidenceDownloadHeaders(w http.ResponseWriter, file *evidence.File) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(file.FileSize, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'none'")
	// The recorded digest travels with the bytes, so a downloader can check
	// what they got against what the record says.
	if file.SHA256 != "" {
		w.Header().Set("X-Evidence-SHA256", file.SHA256)
	}
}
