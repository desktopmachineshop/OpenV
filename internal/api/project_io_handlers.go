package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// ExportProject exports a project in the specified format
func (h *Handler) ExportProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}

	// Get format from query parameter (default to JSON)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	exportFormat := exports.ExportFormat(format)

	// Reject formats the export service cannot produce before doing any work.
	var contentType string
	switch exportFormat {
	case exports.FormatJSON:
		contentType = "application/json"
	case exports.FormatCSV:
		contentType = "text/csv; charset=utf-8"
	case exports.FormatExcel:
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case exports.FormatReqIF:
		// ReqIF is an XML dialect; application/xml is the widely accepted media
		// type for it (the registered application/reqif+xml is not universal).
		contentType = "application/xml; charset=utf-8"
	default:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unsupported export format: %s", format))
		return
	}

	// Export project
	data, filename, err := h.exportService.ExportProject(id, exportFormat)
	if err != nil {
		if errors.Is(err, exports.ErrUnsupportedFormat) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondInternal(w, r, "failed to export project", err)
		return
	}

	// Set appropriate headers. The filename is quoted; the export service
	// sanitizes it (no quotes, backslashes, or control characters).
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ImportProject imports project data from uploaded JSON file and creates a new project
func (h *Handler) ImportProject(w http.ResponseWriter, r *http.Request) {
	// A project create like any other (requireProjectCreate): the import
	// counts toward the project maximum, and its route's alwaysWritable
	// passes the read-only gate (REQ-177).
	orgID, ok := h.requireProjectCreate(w, r)
	if !ok {
		return
	}

	// Read the uploaded file
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	// Import and create new project. The default format is JSON; ReqIF is
	// selected by ?format=reqif or sniffed from an XML/ReqIF payload (issue
	// #238). A malformed ReqIF or JSON is a client error (400), not a 500.
	var projectID string
	if isReqIFImport(r, data) {
		projectID, err = h.exportService.ImportProjectReqIF(data, orgID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to import ReqIF: %v", err))
			return
		}
	} else {
		projectID, err = h.exportService.ImportProject(data, orgID)
		if errors.Is(err, exports.ErrMalformedImport) {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to import JSON: %v", err))
			return
		}
		if err != nil {
			respondInternal(w, r, "failed to import project", err)
			return
		}
	}

	// Creator becomes the project owner (mirrors CreateProject).
	if user := CurrentUser(r); user != nil && h.memberService != nil {
		if err := h.memberService.AddMember(projectID, user.ID, members.RoleOwner); err != nil {
			slog.Warn("api: failed to add creator as project owner", "project_id", projectID, "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status":     "success",
		"message":    "Project imported successfully",
		"project_id": projectID,
	})
}

// isReqIFImport reports whether an import request carries a ReqIF document
// rather than the default JSON. It honours an explicit ?format=reqif (or an
// XML/ReqIF Content-Type) and otherwise sniffs the payload: JSON exports start
// with '{', ReqIF is XML whose root (after any declaration/BOM) is <REQ-IF>.
func isReqIFImport(r *http.Request, data []byte) bool {
	if strings.EqualFold(r.URL.Query().Get("format"), "reqif") {
		return true
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "reqif") {
		return true
	}
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	if strings.HasPrefix(trimmed, "{") {
		return false // JSON export
	}
	upper := strings.ToUpper(trimmed)
	return strings.Contains(upper, "<REQ-IF")
}

// GenerateReport generates a PDF report for a project or baseline.
func (h *Handler) GenerateReport(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	baselineID := r.URL.Query().Get("baseline_id")

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "pdf"
	}

	var (
		data        []byte
		filename    string
		contentType string
		err         error
	)
	switch format {
	case "pdf":
		contentType = "application/pdf"
		data, filename, err = h.reportService.GenerateProjectReport(projectID, baselineID)
	case "docx":
		contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		data, filename, err = h.reportService.GenerateProjectReportDOCX(projectID, baselineID)
	default:
		respondError(w, r, http.StatusBadRequest, "unsupported report format", fmt.Errorf("unsupported report format: %q", format))
		return
	}

	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
		respondInternal(w, r, "failed to generate project report", err)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
