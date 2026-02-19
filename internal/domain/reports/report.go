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

// stripMarkdown converts markdown text to plain text for PDF rendering
// Handles common markdown syntax: headers, bold, italic, code, lists, links
func stripMarkdown(text string) string {
	// Remove code blocks (```...```)
	codeBlockRe := regexp.MustCompile("(?s)```[a-zA-Z]*\n?(.*?)\n?```")
	text = codeBlockRe.ReplaceAllString(text, "$1")
	
	// Remove HTML tags (including sup, sub, etc.)
	htmlTagRe := regexp.MustCompile(`<[^>]+>`)
	text = htmlTagRe.ReplaceAllString(text, "")
	
	// Remove horizontal rules (---, ___, ***)
	hrRe := regexp.MustCompile(`(?m)^\s*[-_*]{3,}\s*$`)
	text = hrRe.ReplaceAllString(text, "")
	
	// Convert headers (# ## ###) to text with spacing - handle headers without space after #
	headerRe := regexp.MustCompile(`(?m)^#{1,6}\s*(.+?)\s*$`)
	text = headerRe.ReplaceAllString(text, "$1")
	
	// Remove blockquotes (> or >>)
	blockquoteRe := regexp.MustCompile(`(?m)^>+\s*`)
	text = blockquoteRe.ReplaceAllString(text, "")
	
	// Convert bold+italic (***text*** or ___text___) first
	boldItalicRe := regexp.MustCompile(`\*\*\*(.+?)\*\*\*|___(.+?)___`)
	text = boldItalicRe.ReplaceAllString(text, "$1$2")
	
	// Convert bold (**text** or __text__) to plain text
	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	text = boldRe.ReplaceAllString(text, "$1$2")
	
	// Convert italic (*text* or _text_) to plain text
	italicRe := regexp.MustCompile(`\*(.+?)\*|_(.+?)_`)
	text = italicRe.ReplaceAllString(text, "$1$2")
	
	// Convert inline code (`code`) to plain text
	inlineCodeRe := regexp.MustCompile("`([^`]+)`")
	text = inlineCodeRe.ReplaceAllString(text, "$1")
	
	// Remove image syntax ![alt](url) - images are handled separately in PDF
	imageRe := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	text = imageRe.ReplaceAllString(text, "")
	
	// Convert links [text](url) to just text
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRe.ReplaceAllString(text, "$1")
	
	// Convert list markers (- or * or 1.) to simple bullets
	listRe := regexp.MustCompile(`(?m)^[\s]*[-*+]\s+`)
	text = listRe.ReplaceAllString(text, "- ")
	
	numberedListRe := regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`)
	text = numberedListRe.ReplaceAllString(text, "  ")
	
	// Convert strikethrough ~~text~~ to plain text
	strikeRe := regexp.MustCompile(`~~(.+?)~~`)
	text = strikeRe.ReplaceAllString(text, "$1")
	
	// Clean up excessive newlines (more than 2 in a row)
	multiNewlineRe := regexp.MustCompile(`\n{3,}`)
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")
	
	return strings.TrimSpace(text)
}

type linkTypeLabel struct {
	label        string
	inverseLabel string
}

var linkTypeLabels = buildLinkTypeLabels()

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

	// Use UTF-8 encoding translator for special characters
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, 8, tr(data.ProjectName), "", 1, "C", false, 0, "")

	if data.ProjectDesc != "" {
		pdf.SetFont("Arial", "", 10)
		plainDesc := stripMarkdown(data.ProjectDesc)
		pdf.MultiCell(0, 5, tr(plainDesc), "", "C", false)
		pdf.Ln(2)
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
		renderArtifactNode(pdf, tr, node, 0, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func buildLinkTypeLabels() map[string]linkTypeLabel {
	labels := make(map[string]linkTypeLabel)
	for _, rule := range linksdomain.GetLinkTypeRules() {
		labels[rule.Type] = linkTypeLabel{
			label:        rule.Label,
			inverseLabel: rule.InverseLabel,
		}
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

func renderArtifactNode(
	pdf *gofpdf.Fpdf,
	tr func(string) string,
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

	sectionHeight := calculateArtifactSectionHeight(pdf, node, xStart, indent, attachmentMap, linkGroupsByArtifact, artifactTitles)
	ensureSpace(pdf, sectionHeight+6)

	if node.artifact.Type == "heading" {
		pdf.SetFont("Arial", "B", 13)
	} else {
		pdf.SetFont("Arial", "B", 11)
	}

	// Skip title for description artifacts, render only for headings and other types
	if node.artifact.Type != "description" {
		yStart := pdf.GetY()
		pdf.SetX(xStart)
		if linkID > 0 {
			pdf.MultiCell(0, 6, tr(node.artifact.Title), "", "L", false)
			pdf.SetLink(linkID, yStart, -1)
		} else {
			pdf.MultiCell(0, 6, tr(node.artifact.Title), "", "L", false)
		}
	}

	// For headings and descriptions: just display body text and images (no table)
	if node.artifact.Type == "heading" || node.artifact.Type == "description" {
		if node.artifact.Body != "" {
			pdf.SetFont("Arial", "", 10)
			pdf.SetX(xStart)
			// Convert markdown to plain text for PDF rendering
			plainText := stripMarkdown(node.artifact.Body)
			pdf.MultiCell(0, 5, tr(plainText), "", "L", false)
		}
	} else {
		// For other artifact types: render details in table format with embedded links and images
		renderArtifactDetailsTable(pdf, tr, node, xStart, indent, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs)
	}

	// For headings and descriptions, render images below the title/description
	if node.artifact.Type == "heading" || node.artifact.Type == "description" {
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
	}

	pdf.Ln(2)

	for _, child := range node.children {
		renderArtifactNode(pdf, tr, child, depth+1, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs)
	}
}

func renderArtifactDetailsTable(
	pdf *gofpdf.Fpdf,
	tr func(string) string,
	node *artifactNode,
	xStart float64,
	indent float64,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
	linkIDs map[string]int,
) {
	ensureSpace(pdf, 20)

	tableX := xStart
	tableWidth := 170.0 - indent
	labelColWidth := 50.0
	valueColWidth := tableWidth - labelColWidth

	// Table border color (light grey)
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "B", 9)

	tableStartY := pdf.GetY()
	rowHeight := 5.0
	attachments := attachmentMap[node.artifact.ID]

	totalHeight, descriptionHeight := calculateArtifactDetailsTableHeight(pdf, node.artifact, valueColWidth, attachmentMap, linkGroupsByArtifact, artifactTitles)

	// Draw outer border (thicker)
	pdf.SetLineWidth(0.5)
	pdf.Rect(tableX, tableStartY, tableWidth, totalHeight, "")
	// Draw label/value column separator
	pdf.SetLineWidth(0.2)
	pdf.Line(tableX+labelColWidth, tableStartY, tableX+labelColWidth, tableStartY+totalHeight)

	// Draw inner lines (thinner)
	pdf.SetLineWidth(0.2)
	currentY := tableStartY

	// Body field
	if node.artifact.Body != "" {
		pdf.SetXY(tableX, currentY)
		pdf.SetTextColor(60, 60, 60)
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(labelColWidth, descriptionHeight, "Description:", "", 0, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Arial", "", 9)
		pdf.SetX(tableX + labelColWidth)
		// Convert markdown to plain text for PDF rendering
		plainText := stripMarkdown(node.artifact.Body)
		pdf.MultiCell(valueColWidth, rowHeight, tr(plainText), "", "L", false)
		currentY = currentY + descriptionHeight
		pdf.SetDrawColor(200, 200, 200)
		pdf.SetLineWidth(0.2)
		pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
	}

	// Type field
	pdf.SetXY(tableX, currentY)
	pdf.SetTextColor(60, 60, 60)
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(labelColWidth, rowHeight, "Type:", "", 0, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 9)
	pdf.SetX(tableX + labelColWidth)
	pdf.CellFormat(valueColWidth, rowHeight, tr(node.artifact.Type), "", 1, "L", false, 0, "")
	currentY = pdf.GetY()
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.2)
	pdf.Line(tableX, currentY, tableX+tableWidth, currentY)

	// Version field
	pdf.SetXY(tableX, currentY)
	pdf.SetTextColor(60, 60, 60)
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(labelColWidth, rowHeight, "Version:", "", 0, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 9)
	pdf.SetX(tableX + labelColWidth)
	pdf.CellFormat(valueColWidth, rowHeight, fmt.Sprintf("v%d", node.artifact.Version), "", 1, "L", false, 0, "")
	currentY = pdf.GetY()
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.2)
	pdf.Line(tableX, currentY, tableX+tableWidth, currentY)

	// No idea what attributes are so what was this doing?
	// // Attributes (excluding links_snapshot)
	// if len(node.artifact.Attributes) > 0 {
	// 	filteredAttributes := make(map[string]interface{})
	// 	for key, value := range node.artifact.Attributes {
	// 		if key != "links_snapshot" {
	// 			filteredAttributes[key] = value
	// 		}
	// 	}

	// 	if len(filteredAttributes) > 0 {
	// 		attributesJSON, _ := json.MarshalIndent(filteredAttributes, "", "  ")
	// 		pdf.SetXY(tableX, currentY)
	// 		pdf.SetTextColor(60, 60, 60)
	// 		pdf.CellFormat(labelColWidth, 4, "Attributes:", "", 0, "L", false, 0, "")
	// 		pdf.SetTextColor(80, 80, 80)
	// 		pdf.SetFont("Arial", "", 8)
	// 		pdf.SetX(tableX + labelColWidth)
	// 		pdf.MultiCell(valueColWidth, 4, string(attributesJSON), "", "L", false)
	// 		currentY = pdf.GetY()
	// 		pdf.SetDrawColor(200, 200, 200)
	// 		pdf.SetLineWidth(0.2)
	// 		pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
	// 	}
	// }

	// Incoming Links row
	if groups, ok := linkGroupsByArtifact[node.artifact.ID]; ok {
		if len(groups.incoming) > 0 {
			incomingHeight := calculateLinkRowHeight(pdf, valueColWidth, groups.incoming, artifactTitles, true)
			pdf.SetXY(tableX, currentY)
			pdf.SetTextColor(60, 60, 60)
			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(labelColWidth, incomingHeight, "Incoming Links:", "", 0, "TL", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "", 9)
			renderLinksWithHyperlinks(pdf, tableX+labelColWidth, currentY, valueColWidth, groups.incoming, artifactTitles, linkIDs, true)
			currentY = currentY + incomingHeight
			pdf.SetDrawColor(200, 200, 200)
			pdf.SetLineWidth(0.2)
			pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
		}

		// Outgoing Links row
		if len(groups.outgoing) > 0 {
			outgoingHeight := calculateLinkRowHeight(pdf, valueColWidth, groups.outgoing, artifactTitles, false)
			pdf.SetXY(tableX, currentY)
			pdf.SetTextColor(60, 60, 60)
			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(labelColWidth, outgoingHeight, "Outgoing Links:", "", 0, "TL", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "", 9)
			renderLinksWithHyperlinks(pdf, tableX+labelColWidth, currentY, valueColWidth, groups.outgoing, artifactTitles, linkIDs, false)
			currentY = currentY + outgoingHeight
			pdf.SetDrawColor(200, 200, 200)
			pdf.SetLineWidth(0.2)
			pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
		}
	}

	// Images
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

		// Calculate image size with constraints
		maxWidth := valueColWidth - 4 // Leave 2mm padding on each side
		width, height := calculateImageSize(info, maxWidth)

		// Row height for image cell
		imageRowHeight := height + 4 // 2mm padding top/bottom

		pdf.SetXY(tableX, currentY)
		pdf.SetTextColor(60, 60, 60)
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(labelColWidth, imageRowHeight, "Image:", "", 0, "TL", false, 0, "")

		// Center image vertically in its cell with padding
		imageY := currentY + 2.0
		imageX := tableX + labelColWidth + 2.0
		pdf.ImageOptions(imagePath, imageX, imageY, width, height, false, options, 0, "")

		currentY = currentY + imageRowHeight
		pdf.SetDrawColor(200, 200, 200)
		pdf.SetLineWidth(0.2)
		pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
	}

	// Reset
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetLineWidth(0.2)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 10)
	pdf.SetY(currentY)
	pdf.Ln(2)
}

func calculateArtifactSectionHeight(
	pdf *gofpdf.Fpdf,
	node *artifactNode,
	xStart float64,
	indent float64,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
) float64 {
	sectionHeight := 0.0
	pageW, _ := pdf.GetPageSize()
	_, _, rightMargin, _ := pdf.GetMargins()
	contentWidth := pageW - rightMargin - xStart
	if contentWidth <= 0 {
		contentWidth = 170.0 - indent
	}

	if node.artifact.Type != "description" && node.artifact.Title != "" {
		lineHeight := 6.0
		fontSize := 11.0
		if node.artifact.Type == "heading" {
			fontSize = 13.0
		}
		pdf.SetFont("Arial", "B", fontSize)
		sectionHeight += estimateWrappedTextHeight(pdf, node.artifact.Title, contentWidth, lineHeight)
	}

	if node.artifact.Type == "heading" || node.artifact.Type == "description" {
		if node.artifact.Body != "" {
			pdf.SetFont("Arial", "", 10)
			// Convert markdown to plain text before estimating height
			plainText := stripMarkdown(node.artifact.Body)
			sectionHeight += estimateWrappedTextHeight(pdf, plainText, contentWidth, 5.0)
		}
	} else {
		tableWidth := 170.0 - indent
		labelColWidth := 50.0
		valueColWidth := tableWidth - labelColWidth
		tableHeight, _ := calculateArtifactDetailsTableHeight(pdf, node.artifact, valueColWidth, attachmentMap, linkGroupsByArtifact, artifactTitles)
		sectionHeight += tableHeight
	}

	return sectionHeight
}

func calculateArtifactDetailsTableHeight(
	pdf *gofpdf.Fpdf,
	artifact *artifacts.Artifact,
	valueColWidth float64,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
) (float64, float64) {
	rowHeight := 5.0
	descriptionHeight := 0.0
	if artifact.Body != "" {
		pdf.SetFont("Arial", "", 9)
		// Convert markdown to plain text before calculating height
		plainText := stripMarkdown(artifact.Body)
		wrapped := pdf.SplitLines([]byte(plainText), valueColWidth)
		if len(wrapped) == 0 {
			descriptionHeight = rowHeight
		} else {
			descriptionHeight = float64(len(wrapped)) * rowHeight
		}
	}

	var totalHeight float64
	if artifact.Body != "" {
		totalHeight += descriptionHeight
	}
	totalHeight += rowHeight * 2 // Type and Version

	if len(artifact.Attributes) > 0 {
		filteredAttributes := make(map[string]interface{})
		for key, value := range artifact.Attributes {
			if key != "links_snapshot" {
				filteredAttributes[key] = value
			}
		}
		if len(filteredAttributes) > 0 {
			attributesJSON, _ := json.MarshalIndent(filteredAttributes, "", "  ")
			lines := strings.Count(string(attributesJSON), "\n") + 1
			totalHeight += float64(lines) * 4.0
		}
	}

	if groups, ok := linkGroupsByArtifact[artifact.ID]; ok {
		if len(groups.incoming) > 0 {
			incomingHeight := calculateLinkRowHeight(pdf, valueColWidth, groups.incoming, artifactTitles, true)
			totalHeight += incomingHeight
		}
		if len(groups.outgoing) > 0 {
			outgoingHeight := calculateLinkRowHeight(pdf, valueColWidth, groups.outgoing, artifactTitles, false)
			totalHeight += outgoingHeight
		}
	}

	attachments := attachmentMap[artifact.ID]
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
		maxWidth := valueColWidth - 4
		_, height := calculateImageSize(info, maxWidth)
		totalHeight += height + 4
	}

	return totalHeight, descriptionHeight
}

func estimateWrappedTextHeight(pdf *gofpdf.Fpdf, text string, width float64, lineHeight float64) float64 {
	if text == "" {
		return 0
	}
	wrapped := pdf.SplitLines([]byte(text), width)
	if len(wrapped) == 0 {
		return lineHeight
	}
	return float64(len(wrapped)) * lineHeight
}

func renderLinksWithHyperlinks(pdf *gofpdf.Fpdf, xStart float64, yStart float64, width float64, linksByType map[string][]*linksdomain.Link, artifactTitles map[string]string, linkIDs map[string]int, isIncoming bool) {
	currentY := yStart + linkRowPadding
	lineHeight := linkLineHeight

	lines := buildLinkLines(linksByType, artifactTitles, linkIDs, isIncoming)
	for _, line := range lines {
		if line.isHeader {
			pdf.SetTextColor(60, 60, 60)
			pdf.SetFont("Arial", "U", 9)
		} else {
			if line.linkID > 0 {
				pdf.SetTextColor(0, 100, 200)
			} else {
				pdf.SetTextColor(0, 0, 0)
			}
			pdf.SetFont("Arial", "", 9)
		}

		wrapped := pdf.SplitLines([]byte(line.text), width)
		for _, w := range wrapped {
			pdf.SetXY(xStart, currentY)
			pdf.CellFormat(width, lineHeight, string(w), "", 1, "L", false, line.linkID, "")
			currentY += lineHeight
		}
	}

	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 9)
}

type linkLine struct {
	text     string
	linkID   int
	isHeader bool
}

func buildLinkLines(linksByType map[string][]*linksdomain.Link, artifactTitles map[string]string, linkIDs map[string]int, isIncoming bool) []linkLine {
	var lines []linkLine
	for linkType, items := range linksByType {
		label := linkTypeLabelForDirection(linkType, isIncoming)
		lines = append(lines, linkLine{
			text:     fmt.Sprintf("%s (%d)", label, len(items)),
			isHeader: true,
		})
		for _, link := range items {
			targetID := link.ToID
			if isIncoming {
				targetID = link.FromID
			}
			title := artifactTitles[targetID]
			if title == "" {
				title = targetID
			}
			linkID := 0
			if linkIDs != nil {
				linkID = linkIDs[targetID]
			}
			lines = append(lines, linkLine{
				text:   "  - " + title,
				linkID: linkID,
			})
		}
	}
	return lines
}

func calculateLinkRowHeight(pdf *gofpdf.Fpdf, width float64, linksByType map[string][]*linksdomain.Link, artifactTitles map[string]string, isIncoming bool) float64 {
	lines := buildLinkLines(linksByType, artifactTitles, nil, isIncoming)
	if len(lines) == 0 {
		return 0
	}

	// Use the same font metrics as the renderer to avoid undercounting.
	pdf.SetFont("Arial", "", 9)
	var lineCount float64
	for _, line := range lines {
		wrapped := pdf.SplitLines([]byte(line.text), width)
		if len(wrapped) == 0 {
			lineCount += 1
			continue
		}
		lineCount += float64(len(wrapped))
	}

	return (lineCount * linkLineHeight) + (linkRowPadding * 2)
}

const (
	linkLineHeight = 3.5
	linkRowPadding = 1.0
	imageMaxHeight = 35.0
)

func calculateImageSize(info *gofpdf.ImageInfoType, maxWidth float64) (float64, float64) {
	width := maxWidth
	height := width * info.Height() / info.Width()
	if height > imageMaxHeight {
		height = imageMaxHeight
		width = height * info.Width() / info.Height()
	}
	if width > maxWidth {
		width = maxWidth
		height = width * info.Height() / info.Width()
	}
	return width, height
}

func formatLinksForTable(linksByType map[string][]*linksdomain.Link) string {
	var result []string
	for linkType, items := range linksByType {
		result = append(result, fmt.Sprintf("%s (%d)", linkType, len(items)))
	}
	return strings.Join(result, ", ")
}

func buildLinkGroups(linkList []*linksdomain.Link) map[string]linkGroups {
	groups := make(map[string]linkGroups)
	
	// Track unique link relationships to avoid duplicates
	// Key format: "fromID:toID:type"
	seenLinks := make(map[string]bool)
	
	for _, link := range linkList {
		// Create unique key for this link relationship
		linkKey := fmt.Sprintf("%s:%s:%s", link.FromID, link.ToID, link.Type)
		
		// Skip if we've already seen this exact relationship
		if seenLinks[linkKey] {
			continue
		}
		seenLinks[linkKey] = true
		
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
