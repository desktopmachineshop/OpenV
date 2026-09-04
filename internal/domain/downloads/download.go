// Package downloads assembles one project download.
//
// Everything a reader can take away from a project — the JSON an import reads
// back, a CSV for a spreadsheet, ReqIF for DOORS, the PDF and Word
// specifications, and the figures themselves — comes from one prepared
// snapshot, narrowed once by the reader's selection and then handed to the
// renderer for the format they picked. That is the point of this package: the
// filters mean the same thing in every format, because only one piece of code
// applies them.
//
// What differs per format is the renderer and the media type, nothing else.
package downloads

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/reports"
)

// ErrUnsupportedFormat is returned for a format this package cannot render.
// Handlers map it to a 400.
var ErrUnsupportedFormat = errors.New("unsupported download format")

// Format is what the reader chose to take away.
type Format string

const (
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
	FormatReqIF Format = "reqif"
	FormatPDF   Format = "pdf"
	FormatDOCX  Format = "docx"
)

// Formats lists what a download can be rendered as, in the order a chooser
// should offer them.
var Formats = []Format{FormatJSON, FormatCSV, FormatReqIF, FormatPDF, FormatDOCX}

// Supported reports whether a string names a format this package renders.
func Supported(format string) bool {
	for _, f := range Formats {
		if string(f) == format {
			return true
		}
	}
	return false
}

// Request is one download: which project, which snapshot, what to keep, and
// what shape to render it in.
type Request struct {
	ProjectID  string
	BaselineID string
	Format     Format
	Selection  exports.Selection
}

// Result is the file to serve.
type Result struct {
	Data        []byte
	Filename    string
	ContentType string
}

// Options describes what a project offers a download form: the sections a
// reader can pick from, the artifact types present, and the attachment
// categories actually held. It is derived from the project rather than fixed,
// so the form never offers a filter that would return nothing.
type Options struct {
	Sections    []Section               `json:"sections"`
	Types       []TypeCount             `json:"types"`
	Attachments []exports.CategoryCount `json:"attachments"`
}

// Section is a top-level heading a download can be narrowed to.
type Section struct {
	ID string `json:"id"`
	// Ref and Number address the section: REQ-3 and "2" respectively.
	Ref    string `json:"ref,omitempty"`
	Number string `json:"number,omitempty"`
	Title  string `json:"title"`
	// Artifacts counts what sits under it, headings excluded, so a reader can
	// see what picking it costs.
	Artifacts int `json:"artifacts"`
}

// TypeCount is one artifact type and how many of it the project holds.
type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// Service builds downloads.
type Service interface {
	Options(projectID string, baselineID string) (*Options, error)
	Download(req Request) (*Result, error)
}

// DefaultService renders downloads over the export and report services.
type DefaultService struct {
	exportService exports.Service
	reportService reports.Service
	// readFile reads an attachment's bytes off disk. Injected so the archive
	// path is testable without a filesystem.
	readFile func(path string) ([]byte, error)
}

// NewService wires a download service. The report service supplies the
// snapshot load (live or baseline) as well as the document renderers.
func NewService(exportService exports.Service, reportService reports.Service) *DefaultService {
	return &DefaultService{
		exportService: exportService,
		reportService: reportService,
		readFile:      os.ReadFile,
	}
}

// SetFileReader overrides how attachment bytes are read. For tests.
func (s *DefaultService) SetFileReader(read func(path string) ([]byte, error)) {
	s.readFile = read
}

// prepare loads the snapshot for a request and narrows it. Every format goes
// through here, which is what makes "no headings" mean the same thing in a PDF
// and in a CSV.
func (s *DefaultService) prepare(req Request) (*exports.ProjectExport, string, error) {
	if req.ProjectID == "" {
		return nil, "", errors.New("project_id is required")
	}
	data, baselineName, err := s.reportService.LoadReportExport(req.ProjectID, req.BaselineID)
	if err != nil {
		return nil, "", err
	}
	return exports.Apply(data, req.Selection), baselineName, nil
}

// Options reports what this project offers a download form.
func (s *DefaultService) Options(projectID string, baselineID string) (*Options, error) {
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	data, _, err := s.reportService.LoadReportExport(projectID, baselineID)
	if err != nil {
		return nil, err
	}
	return BuildOptions(data), nil
}

// Download renders one file. When the selection asks for attachments, the
// rendered document and the files travel together in a zip: a specification
// and the drawings it cites are one deliverable.
func (s *DefaultService) Download(req Request) (*Result, error) {
	if !Supported(string(req.Format)) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, req.Format)
	}

	data, baselineName, err := s.prepare(req)
	if err != nil {
		return nil, err
	}

	var (
		body     []byte
		filename string
	)
	switch req.Format {
	case FormatJSON:
		body, filename, err = s.exportService.RenderExport(data, exports.FormatJSON)
	case FormatCSV:
		body, filename, err = s.exportService.RenderExport(data, exports.FormatCSV)
	case FormatReqIF:
		body, filename, err = s.exportService.RenderExport(data, exports.FormatReqIF)
	case FormatPDF:
		body, filename, err = s.reportService.RenderProjectReport(data, baselineName)
	case FormatDOCX:
		body, filename, err = s.reportService.RenderProjectReportDOCX(data, baselineName)
	}
	if err != nil {
		return nil, err
	}

	files := exports.SelectedFiles(data, req.Selection)
	if len(files) == 0 {
		return &Result{Data: body, Filename: filename, ContentType: ContentType(req.Format)}, nil
	}

	archive, err := s.bundle(filename, body, files)
	if err != nil {
		return nil, err
	}
	return &Result{
		Data:        archive,
		Filename:    zipName(filename),
		ContentType: "application/zip",
	}, nil
}

// ContentType is the media type a format is served as.
func ContentType(format Format) string {
	switch format {
	case FormatJSON:
		return "application/json"
	case FormatCSV:
		return "text/csv; charset=utf-8"
	case FormatReqIF:
		// ReqIF is an XML dialect; application/xml is what tools accept.
		return "application/xml; charset=utf-8"
	case FormatPDF:
		return "application/pdf"
	case FormatDOCX:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

// zipName is the archive's name: the document's, with a .zip extension.
func zipName(filename string) string {
	base := strings.TrimSuffix(filename, path.Ext(filename))
	if base == "" {
		base = "download"
	}
	return base + ".zip"
}

// bundle packs the rendered document and the chosen attachment files into one
// archive. Files go under attachments/, named by their figure reference where
// they have one, so a reader can match a citation to a file without opening it.
//
// A file that cannot be read is skipped rather than failing the download: the
// document is what was asked for, and one missing drawing should not deny it.
// The archive records the loss in a manifest so nobody assumes it is complete.
func (s *DefaultService) bundle(filename string, body []byte, files []*attachments.Attachment) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	doc, err := w.Create(filename)
	if err != nil {
		return nil, err
	}
	if _, err := doc.Write(body); err != nil {
		return nil, err
	}

	var missing []string
	used := map[string]int{}
	for _, a := range files {
		if a == nil {
			continue
		}
		content, err := s.readFile(a.FilePath)
		if err != nil {
			missing = append(missing, attachmentName(a))
			continue
		}
		name := uniqueName(used, "attachments/"+attachmentName(a))
		f, err := w.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(content); err != nil {
			return nil, err
		}
	}

	if len(missing) > 0 {
		note, err := w.Create("attachments/MISSING.txt")
		if err != nil {
			return nil, err
		}
		text := "These attachments could not be read and are not in this archive:\n\n" +
			strings.Join(missing, "\n") + "\n"
		if _, err := note.Write([]byte(text)); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// attachmentName is what a file is called inside the archive: its stored name,
// which for a figure is its reference.
func attachmentName(a *attachments.Attachment) string {
	name := strings.TrimSpace(a.Filename)
	if name == "" {
		name = strings.TrimSpace(a.OriginalFilename)
	}
	if name == "" {
		name = a.ID
	}
	// Names come from uploads. Anything that could climb out of the archive
	// directory is flattened rather than trusted.
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(path.Clean("/" + name))
	if name == "." || name == "/" || name == "" {
		name = a.ID
	}
	return name
}

// uniqueName keeps two files with the same name from colliding in the archive.
func uniqueName(used map[string]int, name string) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	ext := path.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), used[name], ext)
}
