package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/plan"
	"github.com/openv/requirements-platform/internal/domain/projects"
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
	router.HandleFunc("/api/v1/projects/{id}/download/excel", h.DownloadExcel).Methods("GET")
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
	opts, err := h.DownloadService.Options(projectID, r.URL.Query().Get("baseline_id"))
	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
		respondInternal(w, r, "failed to read download options", err)
		return
	}
	// A live project with a task or a milestone can take its plan along.
	out := downloadOptions{Options: opts}
	if id := r.URL.Query().Get("baseline_id"); h.PlanService != nil && (id == "" || id == "live") {
		p, ok := h.loadPlan(w, r, projectID)
		if !ok {
			return
		}
		if len(p.Tasks) > 0 || len(p.Milestones) > 0 {
			out.Plan = &downloadPlanOption{Tasks: len(p.Tasks), Milestones: len(p.Milestones)}
		}
	}
	// A project with projects under it the caller can read can take them along.
	if id := r.URL.Query().Get("baseline_id"); id == "" || id == "live" {
		out.Children = len(h.readableDescendants(r, projectID))
	}
	writeJSONOK(w, out)
}

// readableDescendants lists the projects under a project, every level
// down, that the caller may read, a parent before its children. One the
// caller cannot read is left out with everything under it: its place in
// the hierarchy is not the caller's to learn.
func (h *Handler) readableDescendants(r *http.Request, projectID string) []*projects.Project {
	if h.ProjectService == nil {
		return nil
	}
	var out []*projects.Project
	seen := map[string]bool{projectID: true}
	queue := []string{projectID}
	for len(queue) > 0 {
		children, err := h.ProjectService.ListChildren(queue[0])
		queue = queue[1:]
		if err != nil {
			continue
		}
		for _, c := range children {
			if c == nil || seen[c.ID] || !h.hasProjectRole(r, c.ID, members.RoleViewer) {
				continue
			}
			seen[c.ID] = true
			out = append(out, c)
			queue = append(queue, c.ID)
		}
	}
	return out
}

// downloadOptions is what a download form is offered: what the project
// holds, and its plan when it has one to take along.
type downloadOptions struct {
	*downloads.Options
	Plan *downloadPlanOption `json:"plan,omitempty"`
	// Children counts the projects under this one that a download can take along.
	Children int `json:"children,omitempty"`
}

// downloadPlanOption counts what the plan would add to a download.
type downloadPlanOption struct {
	Tasks      int `json:"tasks"`
	Milestones int `json:"milestones"`
}

// DownloadJSON serves the project as the JSON an import reads back.
func (h *Handler) DownloadJSON(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatJSON)
}

// DownloadCSV serves one row per artifact for a spreadsheet.
func (h *Handler) DownloadCSV(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatCSV)
}

// DownloadExcel serves the project as an .xlsx workbook: a sheet per artifact
// type, the traceability links, and a cover naming the snapshot.
func (h *Handler) DownloadExcel(w http.ResponseWriter, r *http.Request) {
	h.serveDownload(w, r, downloads.FormatExcel)
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

	// ?plan=1 takes the project's plan and its tasks with the document, as
	// a file beside it in the format nearest the document's. The plan is
	// the one that stands, so a baseline's download has none.
	var extra []downloads.ExtraFile
	baselineID := r.URL.Query().Get("baseline_id")
	if wantsPlan(r) && h.PlanService != nil && (baselineID == "" || baselineID == "live") {
		out, ok := h.renderPlan(w, r, projectID, planFormatFor(format))
		if !ok {
			return
		}
		extra = append(extra, downloads.ExtraFile{Name: "plan/" + out.Filename, Data: out.Data})
	}

	// ?children=1 takes the projects under this one along, each as its own
	// download in the same format with the same selection, in a folder of
	// its name. They are the projects as they stand, so a baseline's
	// download has none.
	if wantsFlag(r, "children") && (baselineID == "" || baselineID == "live") {
		files, ok := h.childDownloads(w, r, projectID, format)
		if !ok {
			return
		}
		extra = append(extra, files...)
	}

	result, err := h.DownloadService.Download(downloads.Request{
		ProjectID:  projectID,
		BaselineID: baselineID,
		Format:     format,
		Selection:  selectionFromQuery(r),
		Coverage:   h.flowDownCoverage,
		Extra:      extra,
	})
	if err != nil {
		if errors.Is(err, downloads.ErrUnsupportedFormat) || errors.Is(err, exports.ErrUnsupportedFormat) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		// A baseline no row has, a malformed id or another project's among
		// them, answers as the report and the V&V reads answer it.
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
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

// wantsPlan reports whether a download asks for the plan beside it.
func wantsPlan(r *http.Request) bool { return wantsFlag(r, "plan") }

// wantsFlag reports whether a query parameter is switched on.
func wantsFlag(r *http.Request, name string) bool {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}

// childDownloads renders each project under a project the caller may read
// as the download the request asks for, at projects/<name>/ in the
// archive; with ?plan=1 each takes its own plan too. A child that fails to
// render fails the download: an archive that silently lacked a project
// would read as complete.
func (h *Handler) childDownloads(w http.ResponseWriter, r *http.Request, projectID string, format downloads.Format) ([]downloads.ExtraFile, bool) {
	var files []downloads.ExtraFile
	used := map[string]bool{}
	for _, child := range h.readableDescendants(r, projectID) {
		var extra []downloads.ExtraFile
		if wantsPlan(r) && h.PlanService != nil {
			out, ok := h.renderPlan(w, r, child.ID, planFormatFor(format))
			if !ok {
				return nil, false
			}
			extra = append(extra, downloads.ExtraFile{Name: "plan/" + out.Filename, Data: out.Data})
		}
		result, err := h.DownloadService.Download(downloads.Request{
			ProjectID: child.ID,
			Format:    format,
			Selection: selectionFromQuery(r),
			Coverage:  h.flowDownCoverage,
			Extra:     extra,
		})
		if err != nil {
			respondInternal(w, r, "failed to build download", err)
			return nil, false
		}
		files = append(files, downloads.ExtraFile{Name: "projects/" + childFolder(child, used) + "/" + result.Filename, Data: result.Data})
	}
	return files, true
}

// childFolder is the folder a child project's download sits in: its name,
// made safe for a path, and told apart from another of the same name.
func childFolder(p *projects.Project, used map[string]bool) string {
	name := strings.Map(func(c rune) rune {
		if c == '/' || c == '\\' || c == ':' || c < 0x20 {
			return '-'
		}
		return c
	}, strings.TrimSpace(p.Name))
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "project"
	}
	folder := name
	for n := 2; used[strings.ToLower(folder)]; n++ {
		folder = fmt.Sprintf("%s (%d)", name, n)
	}
	used[strings.ToLower(folder)] = true
	return folder
}

// planFormatFor is the plan's format beside a document of a format: data
// beside data, a PDF beside a document a person reads, and Microsoft
// Project's XML beside ReqIF, each the interchange format of its kind.
func planFormatFor(format downloads.Format) string {
	switch format {
	case downloads.FormatJSON:
		return plan.FormatJSON
	case downloads.FormatCSV:
		return plan.FormatCSV
	case downloads.FormatExcel:
		return plan.FormatExcel
	case downloads.FormatReqIF:
		return plan.FormatMSProject
	default:
		return plan.FormatPDF
	}
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
		Owners:      csvParam(q.Get("owners")),
		Attachments: csvParam(q.Get("attachments")),
		// Headings are in unless the caller says otherwise.
		IncludeHeadings: q.Get("headings") != "0" && !strings.EqualFold(q.Get("headings"), "false"),
		Content:         exports.DefaultContent(),
	}

	// types: absent keeps the template's; "all" keeps every type, whatever
	// the template keeps, so a reader who ticks every type gets every type;
	// otherwise the listed types. No artifact type is called "all".
	allTypes := len(sel.Types) == 1 && strings.EqualFold(sel.Types[0], "all")
	if allTypes {
		sel.Types = nil
	}

	// A template is a starting point: it sets the types and the content
	// switches, and anything the query names explicitly wins over it.
	if key := strings.TrimSpace(q.Get("template")); key != "" {
		if tmpl, ok := exports.TemplateByKey(key); ok {
			sel.Content = tmpl.Content
			sel.Content.Template = tmpl.Key
			if !allTypes && sel.Types == nil && tmpl.Types != nil {
				sel.Types = append([]string(nil), tmpl.Types...)
			}
		} else {
			sel.Content.Template = key
		}
	}

	flag := func(name string, current bool) bool {
		v := strings.TrimSpace(q.Get(name))
		if v == "" {
			return current
		}
		return v != "0" && !strings.EqualFold(v, "false")
	}
	sel.Content.Traceability = flag("traceability", sel.Content.Traceability)
	sel.Content.Figures = flag("figures", sel.Content.Figures)
	sel.Content.TOC = flag("toc", sel.Content.TOC)
	sel.Content.TestResults = flag("results", sel.Content.TestResults)
	sel.Content.VVStatus = flag("vv", sel.Content.VVStatus)

	// fields: absent keeps the template's choice; "all" shows every attribute;
	// "none" shows none; otherwise the listed keys.
	if _, present := q["fields"]; present {
		raw := strings.TrimSpace(q.Get("fields"))
		switch {
		case strings.EqualFold(raw, "all"):
			sel.Content.AllFields, sel.Content.Fields = true, nil
		case strings.EqualFold(raw, "none"), raw == "":
			sel.Content.AllFields, sel.Content.Fields = false, nil
		default:
			sel.Content.AllFields, sel.Content.Fields = false, csvParam(raw)
		}
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
