package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// Taking a project away used to mean four separate things: a JSON export, a
// CSV export, a PDF report and a Word report, each with its own endpoint, its
// own button and its own idea of what "the project" was. They now share one
// prepared snapshot and one set of filters, and differ only in the renderer at
// the end.
//
// There is still an endpoint per format, because the formats differ in what
// they serve — media type, filename, and whether the reader gets a file or an
// archive of files. What they no longer differ in is what is IN them.

// registerDownloadRoutes wires the download surface: one route per output,
// plus the options a chooser is built from.
func (h *Handler) registerDownloadRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/download/options", h.DownloadOptions).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/download/json", h.DownloadJSON).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/download/csv", h.DownloadCSV).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/download/reqif", h.DownloadReqIF).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/download/pdf", h.DownloadPDF).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/download/docx", h.DownloadDOCX).Methods("GET")
}

// DownloadOptions reports what this project offers a download form: its
// sections, the artifact types in it, and the attachment categories it holds.
func (h *Handler) DownloadOptions(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	opts, err := h.downloadService.Options(projectID, r.URL.Query().Get("baseline_id"))
	if err != nil {
		respondInternal(w, r, "failed to read download options", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(opts)
}

// DownloadJSON serves the project as the JSON an import reads back.
func (h *Handler) DownloadJSON(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatJSON)
}

// DownloadCSV serves one row per artifact for a spreadsheet.
func (h *Handler) DownloadCSV(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatCSV)
}

// DownloadReqIF serves the OMG interchange format read by DOORS and Polarion.
func (h *Handler) DownloadReqIF(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatReqIF)
}

// DownloadPDF serves the specification as a PDF.
func (h *Handler) DownloadPDF(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatPDF)
}

// DownloadDOCX serves the specification as a Word document.
func (h *Handler) DownloadDOCX(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatDOCX)
}

// serveDownload is the shared half: authorize, read the selection off the
// query, render, and serve. Only the format differs between the handlers above.
func (h *Handler) serveDownload(w http.ResponseWriter, r *http.Request, format downloads.Format) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	result, err := h.downloadService.Download(downloads.Request{
		ProjectID:  projectID,
		BaselineID: r.URL.Query().Get("baseline_id"),
		Format:     format,
		Selection:  selectionFromQuery(r),
	})
	if err != nil {
		if errors.Is(err, downloads.ErrUnsupportedFormat) || errors.Is(err, exports.ErrUnsupportedFormat) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondInternal(w, r, "failed to build download", err)
		return
	}

	w.Header().Set("Content-Type", result.ContentType)
	// The filename is quoted; the renderers sanitize it (no quotes,
	// backslashes or control characters).
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", result.Filename))
	w.WriteHeader(http.StatusOK)
	w.Write(result.Data)
}

// selectionFromQuery reads a download's filters off the query string.
//
// Defaults matter here: a request with no filters at all is the whole project
// with its headings and no attachment files, which is exactly what the plain
// export endpoint has always produced. Callers narrow from there.
func selectionFromQuery(r *http.Request) exports.Selection {
	q := r.URL.Query()
	sel := exports.Selection{
		Sections:    csvParam(q.Get("sections")),
		Types:       csvParam(q.Get("types")),
		Attachments: csvParam(q.Get("attachments")),
		// Headings are in unless the caller says otherwise.
		IncludeHeadings: q.Get("headings") != "0" && !strings.EqualFold(q.Get("headings"), "false"),
	}
	return sel
}

// csvParam splits a comma-separated query parameter, dropping the blanks a
// trailing comma or an empty value leaves behind.
func csvParam(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
