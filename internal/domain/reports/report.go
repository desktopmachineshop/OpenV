package reports

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
)

type artifactNode struct {
	artifact *artifacts.Artifact
	children []*artifactNode
}

type linkGroups struct {
	outgoing map[string][]*linksdomain.Link
	incoming map[string][]*linksdomain.Link
}

// Service defines report generation behavior.
type Service interface {
	GenerateProjectReport(projectID string, baselineID string) ([]byte, string, error)
}

// DefaultService generates PDF reports from project snapshots.
type DefaultService struct {
	exportService   exports.Service
	baselineService baselines.Service
}

// NewService creates a new report service.
func NewService(exportService exports.Service, baselineService baselines.Service) *DefaultService {
	return &DefaultService{
		exportService:   exportService,
		baselineService: baselineService,
	}
}

// GenerateProjectReport builds a PDF report for a project or baseline.
func (s *DefaultService) GenerateProjectReport(projectID string, baselineID string) ([]byte, string, error) {
	if projectID == "" {
		return nil, "", errors.New("project_id is required")
	}

	var data exports.ProjectExport
	var baselineName string

	if baselineID != "" && baselineID != "live" {
		baseline, err := s.baselineService.GetBaseline(baselineID)
		if err != nil {
			return nil, "", err
		}
		baselineName = baseline.Name
		if err := json.Unmarshal(baseline.Snapshot, &data); err != nil {
			return nil, "", fmt.Errorf("failed to parse baseline snapshot: %w", err)
		}
	} else {
		jsonData, _, err := s.exportService.ExportProject(projectID, exports.FormatJSON)
		if err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return nil, "", fmt.Errorf("failed to parse export data: %w", err)
		}
	}

	pdf, err := buildReportPDF(&data, baselineName)
	if err != nil {
		return nil, "", err
	}

	filename := reportFilename(data.ProjectName, baselineName)
	return pdf, filename, nil
}

func buildReportPDF(data *exports.ProjectExport, baselineName string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, 8, data.ProjectName, "", 1, "C", false, 0, "")

	if data.ProjectDesc != "" {
		pdf.SetFont("Arial", "", 10)
		pdf.MultiCell(0, 5, data.ProjectDesc, "", "C", false)
	}

	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(120, 120, 120)
	if baselineName != "" {
		pdf.CellFormat(0, 5, fmt.Sprintf("Baseline: %s", baselineName), "", 1, "C", false, 0, "")
	}
	pdf.CellFormat(0, 5, fmt.Sprintf("Generated: %s", time.Now().Format("2006-01-02 15:04")), "", 1, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	attachmentMap := map[string][]*attachments.Attachment{}
	for _, attachment := range data.Attachments {
		attachmentMap[attachment.ArtifactID] = append(attachmentMap[attachment.ArtifactID], attachment)
	}

	artifactTitles := map[string]string{}
	for _, artifact := range data.Artifacts {
		artifactTitles[artifact.ID] = artifact.Title
	}

	linkGroupsByArtifact := buildLinkGroups(data.Links)

	linkIDs := map[string]int{}
	for _, artifact := range data.Artifacts {
		linkIDs[artifact.ID] = pdf.AddLink()
	}

	nodes := make(map[string]*artifactNode)
	var roots []*artifactNode
	for _, artifact := range data.Artifacts {
		nodes[artifact.ID] = &artifactNode{artifact: artifact}
	}
	for _, artifact := range data.Artifacts {
		node := nodes[artifact.ID]
		if artifact.ParentID != nil && *artifact.ParentID != "" {
			if parent := nodes[*artifact.ParentID]; parent != nil {
				parent.children = append(parent.children, node)
				continue
			}
		}
		roots = append(roots, node)
	}

	for _, node := range roots {
		renderArtifactNode(pdf, node, 0, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func renderArtifactNode(
	pdf *gofpdf.Fpdf,
	node *artifactNode,
	depth int,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
	linkIDs map[string]int,
) {
	indent := float64(depth) * 6
	xStart := 15 + indent

	linkID := linkIDs[node.artifact.ID]

	ensureSpace(pdf, 18)

	if node.artifact.Type == "heading" {
		pdf.SetFont("Arial", "B", 13)
	} else {
		pdf.SetFont("Arial", "B", 11)
	}

	yStart := pdf.GetY()
	pdf.SetX(xStart)
	if linkID > 0 {
		pdf.MultiCell(0, 6, node.artifact.Title, "", "L", false)
		pdf.SetLink(linkID, yStart, -1)
	} else {
		pdf.MultiCell(0, 6, node.artifact.Title, "", "L", false)
	}

	if node.artifact.Type != "heading" {
		pdf.SetFont("Arial", "", 9)
		pdf.SetTextColor(120, 120, 120)
		pdf.SetX(xStart)
		pdf.CellFormat(0, 4, fmt.Sprintf("%s - v%d", node.artifact.Type, node.artifact.Version), "", 1, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}

	if node.artifact.Body != "" {
		pdf.SetFont("Arial", "", 10)
		pdf.SetX(xStart)
		pdf.MultiCell(0, 5, node.artifact.Body, "", "L", false)
	}

	if len(node.artifact.Attributes) > 0 {
		attributesJSON, err := json.MarshalIndent(node.artifact.Attributes, "", "  ")
		if err == nil {
			pdf.SetFont("Arial", "", 8)
			pdf.SetTextColor(90, 90, 90)
			pdf.SetX(xStart)
			pdf.MultiCell(0, 4, string(attributesJSON), "", "L", false)
			pdf.SetTextColor(0, 0, 0)
		}
	}

	attachments := attachmentMap[node.artifact.ID]
	for _, attachment := range attachments {
		imageType := imageTypeFromMime(attachment.MimeType, attachment.FilePath)
		if imageType == "" {
			continue
		}

		imagePath, ok := resolveAttachmentPath(attachment.FilePath)
		if !ok {
			continue
		}

		options := gofpdf.ImageOptions{ImageType: imageType, ReadDpi: true}
		info := pdf.RegisterImageOptions(imagePath, options)
		if info == nil {
			continue
		}

		maxWidth := 170.0 - indent
		width := maxWidth
		height := width * info.Height() / info.Width()
		if height > 90 {
			height = 90
			width = height * info.Width() / info.Height()
		}

		ensureSpace(pdf, height+6)
		pdf.SetX(xStart)
		pdf.ImageOptions(imagePath, xStart, pdf.GetY(), width, height, false, options, 0, "")
		pdf.Ln(height + 4)
	}

	if groups, ok := linkGroupsByArtifact[node.artifact.ID]; ok {
		renderLinkGroup(pdf, xStart, "Outgoing", groups.outgoing, artifactTitles, linkIDs)
		renderLinkGroup(pdf, xStart, "Incoming", groups.incoming, artifactTitles, linkIDs)
	}

	pdf.Ln(2)

	for _, child := range node.children {
		renderArtifactNode(pdf, child, depth+1, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs)
	}
}

func buildLinkGroups(linkList []*linksdomain.Link) map[string]linkGroups {
	groups := make(map[string]linkGroups)
	for _, link := range linkList {
		outgoing, ok := groups[link.FromID]
		if !ok {
			outgoing = linkGroups{outgoing: map[string][]*linksdomain.Link{}, incoming: map[string][]*linksdomain.Link{}}
		}
		outgoing.outgoing[link.Type] = append(outgoing.outgoing[link.Type], link)
		groups[link.FromID] = outgoing

		incoming, ok := groups[link.ToID]
		if !ok {
			incoming = linkGroups{outgoing: map[string][]*linksdomain.Link{}, incoming: map[string][]*linksdomain.Link{}}
		}
		incoming.incoming[link.Type] = append(incoming.incoming[link.Type], link)
		groups[link.ToID] = incoming
	}
	return groups
}

func renderLinkGroup(
	pdf *gofpdf.Fpdf,
	xStart float64,
	label string,
	linksByType map[string][]*linksdomain.Link,
	artifactTitles map[string]string,
	linkIDs map[string]int,
) {
	if len(linksByType) == 0 {
		return
	}

	ensureSpace(pdf, 10)
	pdf.SetFont("Arial", "B", 9)
	pdf.SetTextColor(60, 60, 60)
	pdf.SetX(xStart)
	pdf.CellFormat(0, 5, fmt.Sprintf("%s Links", label), "", 1, "L", false, 0, "")
	
	for linkType, items := range linksByType {
		pdf.SetFont("Arial", "", 8)
		pdf.SetTextColor(90, 90, 90)
		pdf.SetX(xStart)
		pdf.CellFormat(0, 4, fmt.Sprintf("- %s (%d)", linkType, len(items)), "", 1, "L", false, 0, "")

		for _, link := range items {
			targetID := link.ToID
			if label == "Incoming" {
				targetID = link.FromID
			}
			title := artifactTitles[targetID]
			if title == "" {
				title = targetID
			}

			pdf.SetX(xStart + 4)
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "", 9)
			linkID := linkIDs[targetID]
			pdf.CellFormat(0, 4, fmt.Sprintf("- %s", title), "", 1, "L", false, linkID, "")
		}
	}

	pdf.SetTextColor(0, 0, 0)
}

func ensureSpace(pdf *gofpdf.Fpdf, needed float64) {
	_, pageH := pdf.GetPageSize()
	if pdf.GetY()+needed > pageH-15 {
		pdf.AddPage()
	}
}

func reportFilename(projectName string, baselineName string) string {
	sanitizedProject := sanitizeFilename(projectName)
	timestamp := time.Now().Format("20060102_150405")
	if baselineName == "" {
		return fmt.Sprintf("project_report_%s_%s.pdf", sanitizedProject, timestamp)
	}
	sanitizedBaseline := sanitizeFilename(baselineName)
	return fmt.Sprintf("project_report_%s_%s_%s.pdf", sanitizedProject, sanitizedBaseline, timestamp)
}

func sanitizeFilename(value string) string {
	if value == "" {
		return "project"
	}
	value = strings.TrimSpace(value)
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	value = re.ReplaceAllString(value, "_")
	return strings.Trim(value, "_")
}

func imageTypeFromMime(mimeType string, filePath string) string {
	if strings.HasPrefix(mimeType, "image/") {
		subtype := strings.TrimPrefix(mimeType, "image/")
		subtype = strings.ToUpper(subtype)
		subtype = strings.ReplaceAll(subtype, "JPEG", "JPG")
		return subtype
	}
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filePath), "."))
	ext = strings.ReplaceAll(ext, "JPEG", "JPG")
	return ext
}

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

		uploadsDir := os.Getenv("UPLOADS_DIR")
		if uploadsDir != "" {
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
