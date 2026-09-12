// Package reports renders a project snapshot as a document: the PDF
// specification, the Word document, and the V&V status PDF.
//
// Every renderer walks the same reportModel, built once from a narrowed
// exports.ProjectExport: the artifact tree in document order, section numbers,
// qualified titles, each body parsed into doc blocks, figures decoded, links
// sorted, and (when asked for) verification coverage and test evidence. The
// renderers differ only in how they draw; what they say is decided here.
package reports

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/reports/doc"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// Service defines report generation behavior.
//
// The Generate* methods load a project (or baseline) snapshot and render it
// with the default content. The Render* methods render a snapshot the caller
// already has — which is how a download renders the SAME narrowed snapshot as
// a PDF and as a Word file without either renderer knowing what a filter is.
type Service interface {
	GenerateProjectReport(projectID string, baselineID string) ([]byte, string, error)
	GenerateProjectReportDOCX(projectID string, baselineID string) ([]byte, string, error)
	RenderProjectReport(data *exports.ProjectExport, opts RenderOptions) ([]byte, string, error)
	RenderProjectReportDOCX(data *exports.ProjectExport, opts RenderOptions) ([]byte, string, error)
	// LoadReportExport is the snapshot a report reads: the live project, or a
	// baseline's stored snapshot, with what the document should say about it.
	LoadReportExport(projectID string, baselineID string) (*exports.ProjectExport, Snapshot, error)
	GenerateVVReport(projectID string, baselineID string, latest map[string]*vv.TestResult, runs []*vv.TestRun) ([]byte, string, error)
}

// DefaultService generates reports from project snapshots.
type DefaultService struct {
	exportService   exports.Service
	baselineService baselines.Service
}

// NewService creates a new report service.
func NewService(exportService exports.Service, baselineService baselines.Service) *DefaultService {
	return &DefaultService{exportService: exportService, baselineService: baselineService}
}

// loadReportExport resolves the project export snapshot for a report, either
// from a captured baseline or the live project.
func (s *DefaultService) loadReportExport(projectID string, baselineID string) (*exports.ProjectExport, Snapshot, error) {
	var data exports.ProjectExport
	snap := Snapshot{ExportedAt: time.Now()}

	if baselineID != "" && baselineID != "live" {
		// Scoped load: a baseline from another project is baselines.ErrNotFound,
		// so a foreign baseline ID cannot pull another project's snapshot into
		// this project's report.
		baseline, err := s.baselineService.GetProjectBaseline(projectID, baselineID)
		if err != nil {
			return nil, snap, err
		}
		snap.BaselineID = baseline.ID
		snap.BaselineName = baseline.Name
		snap.CapturedAt = baseline.CreatedAt
		if err := json.Unmarshal(baseline.Snapshot, &data); err != nil {
			return nil, snap, fmt.Errorf("failed to parse baseline snapshot: %w", err)
		}
	} else {
		jsonData, _, err := s.exportService.ExportProject(projectID, exports.FormatJSON)
		if err != nil {
			return nil, snap, err
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return nil, snap, fmt.Errorf("failed to parse export data: %w", err)
		}
	}
	return &data, snap, nil
}

// GenerateProjectReport builds the specification PDF for a project or
// baseline with the default content.
func (s *DefaultService) GenerateProjectReport(projectID string, baselineID string) ([]byte, string, error) {
	if projectID == "" {
		return nil, "", errors.New("project_id is required")
	}
	data, snap, err := s.loadReportExport(projectID, baselineID)
	if err != nil {
		return nil, "", err
	}
	return s.RenderProjectReport(data, defaultRenderOptions(snap))
}

// RenderProjectReport builds a PDF from a snapshot the caller prepared.
func (s *DefaultService) RenderProjectReport(data *exports.ProjectExport, opts RenderOptions) ([]byte, string, error) {
	if data == nil {
		return nil, "", errors.New("nothing to report on")
	}
	pdf, err := buildReportPDF(data, opts)
	if err != nil {
		return nil, "", err
	}
	return pdf, reportFilename(data.ProjectName, opts.Snapshot.BaselineName, "pdf"), nil
}

// GenerateProjectReportDOCX builds the Word document for a project or
// baseline with the default content.
func (s *DefaultService) GenerateProjectReportDOCX(projectID string, baselineID string) ([]byte, string, error) {
	if projectID == "" {
		return nil, "", errors.New("project_id is required")
	}
	data, snap, err := s.loadReportExport(projectID, baselineID)
	if err != nil {
		return nil, "", err
	}
	return s.RenderProjectReportDOCX(data, defaultRenderOptions(snap))
}

// RenderProjectReportDOCX builds a Word document from a prepared snapshot.
func (s *DefaultService) RenderProjectReportDOCX(data *exports.ProjectExport, opts RenderOptions) ([]byte, string, error) {
	if data == nil {
		return nil, "", errors.New("nothing to report on")
	}
	docx, err := buildReportDOCX(data, opts)
	if err != nil {
		return nil, "", err
	}
	return docx, reportFilename(data.ProjectName, opts.Snapshot.BaselineName, "docx"), nil
}

// LoadReportExport exposes the snapshot load so a download can narrow it
// before any renderer sees it.
func (s *DefaultService) LoadReportExport(projectID string, baselineID string) (*exports.ProjectExport, Snapshot, error) {
	if projectID == "" {
		return nil, Snapshot{}, errors.New("project_id is required")
	}
	return s.loadReportExport(projectID, baselineID)
}

// --- Report model ------------------------------------------------------------

type artifactNode struct {
	artifact *artifacts.Artifact
	children []*artifactNode
	// depth is the nesting depth in the tree, 0 for a root.
	depth int
}

// linkRow is one traceability row, already worded for the reader.
type linkRow struct {
	Direction    string // "Incoming" / "Outgoing"
	Relationship string // "verified by", "derives from"
	TargetID     string
	TargetTitle  string
	Suspect      bool
}

// fieldRow is one label/value pair of an artifact's details table.
type fieldRow struct {
	Label string
	Value string
	// Rollup is set on the V&V status row so a renderer can colour it.
	Rollup string
}

// figure is an attachment ready to draw: decoded to a format the renderers
// embed, or a placeholder sentence when it cannot be.
type figure struct {
	Attachment *attachments.Attachment
	Caption    string
	// JPEG holds the original bytes for a JPEG; PNG holds the image re-encoded
	// as PNG for every other decodable format. Exactly one is set unless the
	// figure is a placeholder.
	JPEG, PNG     []byte
	Width, Height int
	Placeholder   string
}

// Renderable reports whether the figure has image bytes to embed.
func (f figure) Renderable() bool { return len(f.JPEG) > 0 || len(f.PNG) > 0 }

// evidenceRow is one test case's latest result.
type evidenceRow struct {
	TestCaseID string
	Ref        string
	Title      string
	Status     string
	ExecutedAt string
	RunName    string
	Notes      string
}

// reportModel is the rendered-report data model shared by every renderer.
type reportModel struct {
	data *exports.ProjectExport
	opts RenderOptions

	roots []*artifactNode
	byID  map[string]*artifacts.Artifact
	// order lists every node in document order; renderers walk it.
	order []*artifactNode

	sectionNumbers map[string]string
	artifactTitles map[string]string
	// refIndex maps a stable ref to its artifact id, figureIndex a figure ref
	// to its attachment, so a citation in a body can become a link.
	refIndex    map[string]string
	figureIndex map[string]*attachments.Attachment

	bodies  map[string][]doc.Block
	figures map[string][]figure
	links   map[string][]linkRow

	fieldLabels map[string]string
	fieldOrder  []string

	coverage      *vv.CoverageReport
	gaps          *vv.GapReport
	coverageByReq map[string]*vv.CoverageEntry
	evidence      []evidenceRow
	runs          []*vv.TestRun

	// counts summarises the snapshot for the cover.
	counts map[string]int
}

// buildReportModel assembles everything the renderers share.
func buildReportModel(data *exports.ProjectExport, opts RenderOptions) *reportModel {
	m := &reportModel{
		data:           data,
		opts:           opts,
		byID:           map[string]*artifacts.Artifact{},
		sectionNumbers: artifacts.SectionNumbers(data.Artifacts),
		artifactTitles: map[string]string{},
		refIndex:       map[string]string{},
		figureIndex:    map[string]*attachments.Attachment{},
		bodies:         map[string][]doc.Block{},
		figures:        map[string][]figure{},
		links:          map[string][]linkRow{},
		fieldLabels:    map[string]string{},
		counts:         map[string]int{},
	}
	if m.opts.Snapshot.ExportedAt.IsZero() {
		m.opts.Snapshot.ExportedAt = time.Now()
	}

	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		m.byID[a.ID] = a
		m.artifactTitles[a.ID] = qualifiedTitle(a, m.sectionNumbers)
		if a.Ref != "" {
			m.refIndex[a.Ref] = a.ID
		}
		m.bodies[a.ID] = doc.Parse(a.Body)
		m.counts[a.Type]++
	}

	// Tree in document order: children keep the snapshot's order, which the
	// export writes by sort_order.
	nodes := map[string]*artifactNode{}
	for _, a := range data.Artifacts {
		if a != nil {
			nodes[a.ID] = &artifactNode{artifact: a}
		}
	}
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		node := nodes[a.ID]
		if a.ParentID != nil && *a.ParentID != "" {
			if parent := nodes[*a.ParentID]; parent != nil && parent != node {
				parent.children = append(parent.children, node)
				continue
			}
		}
		m.roots = append(m.roots, node)
	}
	var walk func(n *artifactNode, depth int, seen map[string]bool)
	walk = func(n *artifactNode, depth int, seen map[string]bool) {
		if seen[n.artifact.ID] {
			return
		}
		seen[n.artifact.ID] = true
		n.depth = depth
		m.order = append(m.order, n)
		for _, c := range n.children {
			walk(c, depth+1, seen)
		}
	}
	seen := map[string]bool{}
	for _, r := range m.roots {
		walk(r, 0, seen)
	}

	// Figures, in upload order, decoded once.
	for _, att := range data.Attachments {
		if att == nil {
			continue
		}
		if att.FigureRef != "" {
			m.figureIndex[att.FigureRef] = att
		}
		if opts.Content.Figures {
			m.figures[att.ArtifactID] = append(m.figures[att.ArtifactID], loadFigure(att))
		}
	}

	// Traceability rows, deduplicated and sorted so two renders agree.
	if opts.Content.Traceability {
		m.links = buildLinkRows(data.Links, m.artifactTitles)
	}

	// Field labels: definitions name custom keys; standard keys have fixed
	// wording.
	for _, f := range exports.Fields(data) {
		m.fieldLabels[f.Key] = f.Label
		m.fieldOrder = append(m.fieldOrder, f.Key)
	}

	// Verification coverage and evidence.
	if opts.Content.VVStatus {
		m.coverage = vv.ComputeCoverage(data, opts.Latest)
		m.gaps = vv.GapAnalysis(data, m.coverage)
		m.coverageByReq = map[string]*vv.CoverageEntry{}
		for i := range m.coverage.Entries {
			e := &m.coverage.Entries[i]
			m.coverageByReq[e.RequirementID] = e
		}
	}
	if opts.Content.TestResults {
		runsByID := map[string]*vv.TestRun{}
		for _, r := range opts.Runs {
			if r != nil {
				runsByID[r.ID] = r
			}
		}
		m.runs = append([]*vv.TestRun(nil), opts.Runs...)
		sort.SliceStable(m.runs, func(i, j int) bool {
			if m.runs[i] == nil || m.runs[j] == nil {
				return m.runs[i] != nil
			}
			return m.runs[i].StartedAt.After(m.runs[j].StartedAt)
		})
		for _, n := range m.order {
			a := n.artifact
			if a.Type != artifacts.TypeTestCase {
				continue
			}
			row := evidenceRow{TestCaseID: a.ID, Ref: a.Ref, Title: a.Title, Status: "not run"}
			if r := opts.Latest[a.ID]; r != nil {
				row.Status = r.Status
				row.Notes = r.Notes
				if r.ExecutedAt != nil {
					row.ExecutedAt = r.ExecutedAt.UTC().Format("2006-01-02 15:04")
				}
				if run := runsByID[r.RunID]; run != nil {
					row.RunName = run.Name
				}
			}
			m.evidence = append(m.evidence, row)
		}
	}
	return m
}

// title returns the display heading for an artifact in this report.
func (m *reportModel) title(a *artifacts.Artifact) string {
	return qualifiedTitle(a, m.sectionNumbers)
}

// qualifiedTitle renders the heading a reader sees, prefixed with whichever
// address applies to that artifact:
//
//	1.2 Background          — a section, numbered by position
//	REQ-12 Brake within 2 m — everything else, addressed by its stable ref
func qualifiedTitle(artifact *artifacts.Artifact, sectionNumbers map[string]string) string {
	if artifact == nil {
		return ""
	}
	if number := sectionNumbers[artifact.ID]; number != "" {
		return strings.TrimSpace(number + " " + artifact.Title)
	}
	if artifact.Ref != "" {
		return strings.TrimSpace(artifact.Ref + " " + artifact.Title)
	}
	return artifact.Title
}

// headingLevel is the outline level of a node: headings nest by their tree
// depth; everything else sits one level under its nearest heading.
func (m *reportModel) headingLevel(n *artifactNode) int {
	level := n.depth + 1
	if level > 6 {
		level = 6
	}
	return level
}

// isProse reports whether an artifact renders as prose alone (no details
// table): headings and descriptions.
func isProse(a *artifacts.Artifact) bool {
	return a.Type == artifacts.TypeHeading || a.Type == artifacts.TypeDescription
}

// fieldRows lists the details a non-heading artifact shows, in order:
// reference, type, version, then the attributes the content asks for, then
// verification status and the latest result when evidence was requested.
func (m *reportModel) fieldRows(a *artifacts.Artifact) []fieldRow {
	rows := []fieldRow{}
	if a.Ref != "" {
		rows = append(rows, fieldRow{Label: "Reference", Value: a.Ref})
	}
	rows = append(rows,
		fieldRow{Label: "Type", Value: typeLabel(a.Type)},
		fieldRow{Label: "Version", Value: fmt.Sprintf("v%d", a.Version)},
	)
	for _, key := range m.fieldOrder {
		if !m.opts.Content.ShowsField(key) {
			continue
		}
		v, ok := a.Attributes[key]
		if !ok {
			continue
		}
		text := attributeText(v)
		if text == "" {
			continue
		}
		rows = append(rows, fieldRow{Label: m.fieldLabels[key], Value: text})
	}
	if m.coverageByReq != nil && a.Type == artifacts.TypeRequirement {
		if e := m.coverageByReq[a.ID]; e != nil {
			rows = append(rows, fieldRow{Label: "V&V rollup", Value: rollupLabel(e.Rollup), Rollup: e.Rollup})
		}
	}
	if m.opts.Content.TestResults && a.Type == artifacts.TypeTestCase {
		for _, e := range m.evidence {
			if e.TestCaseID == a.ID {
				value := e.Status
				if e.ExecutedAt != "" {
					value += " · " + e.ExecutedAt
				}
				if e.RunName != "" {
					value += " · " + e.RunName
				}
				rows = append(rows, fieldRow{Label: "Latest result", Value: value, Rollup: e.Status})
				break
			}
		}
	}
	return rows
}

// typeLabel words an artifact type: "test-case" → "Test case".
func typeLabel(t string) string {
	return exports.FieldLabel(strings.ReplaceAll(t, "-", " "))
}

// rollupLabel words a rollup state for print.
func rollupLabel(r string) string {
	return strings.ReplaceAll(r, "-", " ")
}

// attributeText prints an attribute value: strings as they are, numbers and
// booleans plainly, lists joined, anything else as JSON.
func attributeText(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := attributeText(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case []string:
		return strings.Join(t, ", ")
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

// buildLinkRows turns the link list into per-artifact rows: incoming first,
// then outgoing, each group ordered by relationship then target title, with
// exact duplicates removed. Map iteration never reaches a renderer.
func buildLinkRows(list []*linksdomain.Link, titles map[string]string) map[string][]linkRow {
	out := map[string][]linkRow{}
	seen := map[string]bool{}
	for _, l := range list {
		if l == nil {
			continue
		}
		key := l.FromID + ":" + l.ToID + ":" + l.Type
		if seen[key] {
			continue
		}
		seen[key] = true
		out[l.FromID] = append(out[l.FromID], linkRow{
			Direction: "Outgoing", Relationship: linkTypeLabelForDirection(l.Type, false),
			TargetID: l.ToID, TargetTitle: titleOr(titles, l.ToID), Suspect: l.Suspect,
		})
		out[l.ToID] = append(out[l.ToID], linkRow{
			Direction: "Incoming", Relationship: linkTypeLabelForDirection(l.Type, true),
			TargetID: l.FromID, TargetTitle: titleOr(titles, l.FromID), Suspect: l.Suspect,
		})
	}
	for id, rows := range out {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Direction != rows[j].Direction {
				return rows[i].Direction == "Incoming"
			}
			if rows[i].Relationship != rows[j].Relationship {
				return rows[i].Relationship < rows[j].Relationship
			}
			if rows[i].TargetTitle != rows[j].TargetTitle {
				return rows[i].TargetTitle < rows[j].TargetTitle
			}
			return rows[i].TargetID < rows[j].TargetID
		})
		out[id] = rows
	}
	return out
}

func titleOr(titles map[string]string, id string) string {
	if t := titles[id]; t != "" {
		return t
	}
	return id
}

type linkTypeLabel struct {
	label        string
	inverseLabel string
}

var linkTypeLabels = buildLinkTypeLabels()

func buildLinkTypeLabels() map[string]linkTypeLabel {
	labels := make(map[string]linkTypeLabel)
	for _, rule := range linksdomain.GetLinkTypeRules() {
		labels[rule.Type] = linkTypeLabel{label: rule.Label, inverseLabel: rule.InverseLabel}
	}
	return labels
}

func linkTypeLabelForDirection(linkType string, isIncoming bool) string {
	labels, ok := linkTypeLabels[linkType]
	if !ok {
		return linkType
	}
	if isIncoming {
		if labels.inverseLabel != "" {
			return labels.inverseLabel
		}
		return labels.label
	}
	if labels.label != "" {
		return labels.label
	}
	return linkType
}

// --- Figures -----------------------------------------------------------------

// maxFigurePixels bounds the decoded size so a huge upload cannot exhaust
// memory while rendering; anything larger is rejected with a placeholder.
const maxFigurePixels = 40_000_000

// loadFigure reads an attachment and prepares it for embedding. A missing
// file, an undecodable image or an SVG becomes a placeholder rather than an
// error: one drawing must never deny the document.
func loadFigure(att *attachments.Attachment) figure {
	f := figure{Attachment: att, Caption: figureCaption(att)}
	mime := strings.ToLower(strings.TrimSpace(att.MimeType))
	if strings.HasPrefix(mime, "image/svg") || strings.EqualFold(filepath.Ext(att.FilePath), ".svg") {
		f.Placeholder = f.Caption + " is an SVG drawing; open it from the attachments archive."
		return f
	}
	path, ok := resolveAttachmentPath(att.FilePath)
	if !ok {
		f.Placeholder = f.Caption + " could not be read from storage."
		return f
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		f.Placeholder = f.Caption + " could not be read from storage."
		return f
	}
	return decodeFigure(f, raw)
}

// decodeFigure fills in the image bytes for a figure from its file contents.
func decodeFigure(f figure, raw []byte) figure {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		f.Placeholder = f.Caption + " is not an image the document can embed."
		return f
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxFigurePixels {
		f.Placeholder = f.Caption + " is too large to embed."
		return f
	}
	f.Width, f.Height = cfg.Width, cfg.Height
	if format == "jpeg" {
		f.JPEG = raw
		return f
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		f.Placeholder = f.Caption + " is not an image the document can embed."
		return f
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		f.Placeholder = f.Caption + " could not be converted for the document."
		return f
	}
	f.PNG = buf.Bytes()
	return f
}

// logoFigure prepares the workspace logo the same way as a figure.
func logoFigure(ws Workspace) (figure, bool) {
	if len(ws.Logo) == 0 {
		return figure{}, false
	}
	f := decodeFigure(figure{Caption: ws.Name}, ws.Logo)
	if !f.Renderable() {
		return figure{}, false
	}
	return f, true
}

// figureCaption is the line under a figure: its reference and the name the
// file was uploaded under, so a citation in the text finds its picture.
func figureCaption(att *attachments.Attachment) string {
	name := strings.TrimSpace(att.OriginalFilename)
	if name == "" {
		name = strings.TrimSpace(att.Filename)
	}
	if att.FigureRef != "" {
		if name != "" {
			return "Figure " + att.FigureRef + " — " + name
		}
		return "Figure " + att.FigureRef
	}
	if name != "" {
		return "Image — " + name
	}
	return "Image"
}

// encodeJPEGQuality is used when a renderer needs JPEG bytes from a decoded
// image (not currently; kept beside decodeFigure for symmetry).
var _ = jpeg.Encode

func resolveAttachmentPath(original string) (string, bool) {
	if original == "" {
		return "", false
	}
	paths := []string{original}
	if strings.Contains(original, "\\") {
		paths = append(paths, strings.ReplaceAll(original, "\\", "/"))
	}
	if !filepath.IsAbs(original) {
		if abs, err := filepath.Abs(original); err == nil {
			paths = append(paths, abs)
		}
		if uploadsDir := os.Getenv("UPLOADS_DIR"); uploadsDir != "" {
			paths = append(paths, filepath.Join(uploadsDir, original))
			paths = append(paths, filepath.Join(uploadsDir, filepath.Base(original)))
		}
	}
	for _, candidate := range paths {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

// --- Cover and product profile ----------------------------------------------

// coverLine is one label/value pair the cover prints under the title.
type coverLine struct{ Label, Value string }

// coverLines describes the snapshot and the content choices.
func (m *reportModel) coverLines() []coverLine {
	c := m.opts.Content
	lines := []coverLine{}
	if m.opts.Workspace.Name != "" {
		lines = append(lines, coverLine{"Workspace", m.opts.Workspace.Name})
	}
	lines = append(lines, coverLine{"Generated", m.opts.Snapshot.ExportedAt.UTC().Format("2006-01-02 15:04 UTC")})
	if c.Template != "" {
		name := c.Template
		if t, ok := exports.TemplateByKey(c.Template); ok {
			name = t.Name
		}
		lines = append(lines, coverLine{"Template", name})
	}
	lines = append(lines, coverLine{"Contents", m.contentsSummary()})
	var carries []string
	if c.AllFields {
		carries = append(carries, "all fields")
	} else if len(c.Fields) > 0 {
		labels := make([]string, 0, len(c.Fields))
		for _, k := range c.Fields {
			if l := m.fieldLabels[k]; l != "" {
				labels = append(labels, l)
			} else {
				labels = append(labels, exports.FieldLabel(k))
			}
		}
		carries = append(carries, "fields: "+strings.Join(labels, ", "))
	} else {
		carries = append(carries, "no custom fields")
	}
	if c.Traceability {
		carries = append(carries, "traceability")
	}
	if c.Figures {
		carries = append(carries, "figures")
	}
	if c.VVStatus {
		carries = append(carries, "V&V status")
	}
	if c.TestResults {
		carries = append(carries, "test results")
	}
	lines = append(lines, coverLine{"Includes", strings.Join(carries, "; ")})
	return lines
}

// contentsSummary counts what the document holds: "241 artifacts: 110
// requirements, 50 test cases, …".
func (m *reportModel) contentsSummary() string {
	total := 0
	for _, n := range m.counts {
		total += n
	}
	order := []string{
		artifacts.TypeHeading, artifacts.TypeUserNeed, artifacts.TypeRequirement, artifacts.TypeDesignItem,
		artifacts.TypeTestCase, artifacts.TypeHazard, artifacts.TypePersona, artifacts.TypeDescription, artifacts.TypeOther,
	}
	var parts []string
	seen := map[string]bool{}
	add := func(t string) {
		if n := m.counts[t]; n > 0 && !seen[t] {
			seen[t] = true
			label := strings.ToLower(typeLabel(t))
			if n != 1 {
				label = pluralType(label)
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	for _, t := range order {
		add(t)
	}
	var rest []string
	for t := range m.counts {
		if !seen[t] {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	for _, t := range rest {
		add(t)
	}
	noun := "artifacts"
	if total == 1 {
		noun = "artifact"
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d %s", total, noun)
	}
	return fmt.Sprintf("%d %s: %s", total, noun, strings.Join(parts, ", "))
}

func pluralType(label string) string {
	switch {
	case strings.HasSuffix(label, "y"):
		return label[:len(label)-1] + "ies"
	case strings.HasSuffix(label, "s"):
		return label
	}
	return label + "s"
}

// mapStringValue reads a string value from a generic map, "" if absent.
func mapStringValue(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// productProfileHasContent reports whether any renderable field is non-empty.
func productProfileHasContent(profile *products.ProductProfile) bool {
	if profile == nil {
		return false
	}
	return profile.Vision != "" || profile.ProblemStatement != "" || profile.TargetUsers != "" ||
		len(profile.SuccessMetrics) > 0 || len(profile.Constraints) > 0
}

// profileSection is the product definition as blocks, so both renderers
// print it the same way.
type profileField struct {
	Label  string
	Blocks []doc.Block
}

func profileFields(profile *products.ProductProfile) []profileField {
	if !productProfileHasContent(profile) {
		return nil
	}
	var out []profileField
	add := func(label, text string) {
		if strings.TrimSpace(text) != "" {
			out = append(out, profileField{Label: label, Blocks: doc.Parse(text)})
		}
	}
	add("Vision", profile.Vision)
	add("Problem statement", profile.ProblemStatement)
	add("Target users", profile.TargetUsers)
	if len(profile.SuccessMetrics) > 0 {
		list := doc.List{}
		for _, metric := range profile.SuccessMetrics {
			name := mapStringValue(metric, "name", "title", "metric")
			if name == "" {
				name = "(unnamed metric)"
			}
			line := name
			if target := mapStringValue(metric, "target"); target != "" {
				line += ": " + target
			}
			if current := mapStringValue(metric, "current"); current != "" {
				line += fmt.Sprintf(" (%s)", current)
			}
			list.Items = append(list.Items, doc.ListItem{Blocks: []doc.Block{doc.Paragraph{Inlines: []doc.Inline{{Text: line}}}}})
		}
		out = append(out, profileField{Label: "Success metrics", Blocks: []doc.Block{list}})
	}
	if len(profile.Constraints) > 0 {
		list := doc.List{}
		for _, constraint := range profile.Constraints {
			text := mapStringValue(constraint, "text", "description", "name", "title")
			if text == "" {
				continue
			}
			list.Items = append(list.Items, doc.ListItem{Blocks: []doc.Block{doc.Paragraph{Inlines: []doc.Inline{{Text: text}}}}})
		}
		if len(list.Items) > 0 {
			out = append(out, profileField{Label: "Constraints", Blocks: []doc.Block{list}})
		}
	}
	return out
}

// --- Filenames and text helpers ---------------------------------------------

// stripMarkdown flattens a markdown body to plain text through the document
// model, for places that print one line (a table cell, a truncated title).
func stripMarkdown(text string) string {
	return doc.PlainText(doc.Parse(text))
}

func reportFilename(projectName string, baselineName string, ext string) string {
	sanitizedProject := sanitizeFilename(projectName)
	timestamp := time.Now().Format("20060102_150405")
	if baselineName == "" {
		return fmt.Sprintf("project_report_%s_%s.%s", sanitizedProject, timestamp, ext)
	}
	sanitizedBaseline := sanitizeFilename(baselineName)
	return fmt.Sprintf("project_report_%s_%s_%s.%s", sanitizedProject, sanitizedBaseline, timestamp, ext)
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeFilename(value string) string {
	if value == "" {
		return "project"
	}
	value = unsafeFilenameChars.ReplaceAllString(strings.TrimSpace(value), "_")
	return strings.Trim(value, "_")
}

// rollupColor returns the fill colour for a rollup or result state:
// pass green, fail red, blocked amber, everything else grey.
func rollupColor(rollup string) (r, g, b int) {
	switch rollup {
	case vv.RollupPass:
		return 39, 174, 96
	case vv.RollupFail:
		return 231, 76, 60
	case vv.RollupBlocked:
		return 243, 156, 18
	default:
		return 150, 150, 150
	}
}

// rollupDisplayOrder controls the summary/legend ordering of rollup states.
var rollupDisplayOrder = []string{
	vv.RollupPass,
	vv.RollupFail,
	vv.RollupBlocked,
	vv.RollupUnrun,
	vv.RollupUncovered,
	vv.RollupVerifiedManually,
	vv.RollupMethodMissing,
}

// gapSections are the gap lists a V&V section prints, in order.
func gapSections(gaps *vv.GapReport) []struct {
	Label string
	IDs   []string
} {
	if gaps == nil {
		return nil
	}
	return []struct {
		Label string
		IDs   []string
	}{
		{"Requirements without a verification method", gaps.RequirementsWithoutMethod},
		{"Requirements without a test case", gaps.RequirementsWithoutTestCase},
		{"Unverified (demonstration, analysis, inspection)", gaps.RequirementsUnverified},
		{"Requirements with failing tests", gaps.RequirementsFailing},
		{"Orphan test cases (verify nothing)", gaps.OrphanTestCases},
		{"User needs without a derived requirement", gaps.NeedsWithoutRequirement},
		{"Unmitigated hazards", gaps.HazardsUnmitigated},
	}
}
