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
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/vv"
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
	// Remove code blocks (```...```) - preserve the content inside
	codeBlockRe := regexp.MustCompile("(?s)```[a-zA-Z]*\n?(.*?)\n?```")
	text = codeBlockRe.ReplaceAllString(text, "$1")
	
	// Remove HTML tags (including sup, sub, etc.)
	htmlTagRe := regexp.MustCompile(`<[^>]+>`)
	text = htmlTagRe.ReplaceAllString(text, "")
	
	// Remove horizontal rules (---, ___, ***)
	hrRe := regexp.MustCompile(`(?m)^\s*[-_*]{3,}\s*$`)
	text = hrRe.ReplaceAllString(text, "")
	
	// Convert headers (# ## ###) to text - handle headers without space after #
	headerRe := regexp.MustCompile(`(?m)^#{1,6}\s*(.+?)\s*$`)
	text = headerRe.ReplaceAllString(text, "$1")
	
	// Remove blockquotes (> or >>)
	blockquoteRe := regexp.MustCompile(`(?m)^>+\s*`)
	text = blockquoteRe.ReplaceAllString(text, "")
	
	// Remove task list markers [x] and [ ]
	taskListRe := regexp.MustCompile(`(?m)^(\s*)[-*+]\s+\[[ xX]\]\s+`)
	text = taskListRe.ReplaceAllString(text, "$1- ")
	
	// Convert bold+italic (***text*** or ___text___) first
	boldItalicRe := regexp.MustCompile(`\*\*\*(.+?)\*\*\*|___(.+?)___`)
	text = boldItalicRe.ReplaceAllString(text, "$1$2")
	
	// Convert bold (**text** or __text__) to plain text
	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	text = boldRe.ReplaceAllString(text, "$1$2")
	
	// Convert italic (*text* or _text_) to plain text - be careful not to affect list markers
	italicRe := regexp.MustCompile(`\*([^*\s][^*]*?)\*|_([^_\s][^_]*?)_`)
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
	
	// Convert list markers (- or * or +) to simple dashes
	listRe := regexp.MustCompile(`(?m)^([\s]*)[-*+]\s+`)
	text = listRe.ReplaceAllString(text, "$1- ")
	
	// Convert numbered lists (1. 2. etc) to simple indented text
	numberedListRe := regexp.MustCompile(`(?m)^([\s]*)\d+\.\s+`)
	text = numberedListRe.ReplaceAllString(text, "$1  ")
	
	// Convert strikethrough ~~text~~ to plain text
	strikeRe := regexp.MustCompile(`~~(.+?)~~`)
	text = strikeRe.ReplaceAllString(text, "$1")
	
	// Replace em dashes and special Unicode characters that may not be in Arial font
	text = strings.ReplaceAll(text, "\u2014", "-") // em dash
	text = strings.ReplaceAll(text, "\u2013", "-") // en dash
	text = strings.ReplaceAll(text, "\u201C", "\"") // left double quote
	text = strings.ReplaceAll(text, "\u201D", "\"") // right double quote
	text = strings.ReplaceAll(text, "\u2018", "'") // left single quote
	text = strings.ReplaceAll(text, "\u2019", "'") // right single quote
	
	// Clean up excessive newlines (more than 2 in a row)
	multiNewlineRe := regexp.MustCompile(`\n{3,}`)
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")
	
	// Clean up excessive spaces
	multiSpaceRe := regexp.MustCompile(` {2,}`)
	text = multiSpaceRe.ReplaceAllString(text, " ")
	
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
	GenerateVVReport(projectID string, baselineID string, latest map[string]*vv.TestResult, runs []*vv.TestRun) ([]byte, string, error)
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

	renderProductDefinition(pdf, tr, data.ProductProfile)

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

// renderProductDefinition renders the "Product Definition" section from the
// export's product profile snapshot. No-op when the profile is empty.
func renderProductDefinition(pdf *gofpdf.Fpdf, tr func(string) string, profile *products.ProductProfile) {
	if !productProfileHasContent(profile) {
		return
	}

	ensureSpace(pdf, 20)
	pdf.SetFont("Arial", "B", 13)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 8, "Product Definition", "", 1, "L", false, 0, "")

	renderProfileParagraph := func(label, text string) {
		if text == "" {
			return
		}
		ensureSpace(pdf, 12)
		pdf.SetFont("Arial", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(0, 5, label, "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.SetTextColor(0, 0, 0)
		pdf.MultiCell(0, 5, tr(stripMarkdown(text)), "", "L", false)
		pdf.Ln(1)
	}

	renderProfileParagraph("Vision", profile.Vision)
	renderProfileParagraph("Problem Statement", profile.ProblemStatement)
	renderProfileParagraph("Target Users", profile.TargetUsers)

	if len(profile.SuccessMetrics) > 0 {
		ensureSpace(pdf, 12)
		pdf.SetFont("Arial", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(0, 5, "Success Metrics", "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.SetTextColor(0, 0, 0)
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
			ensureSpace(pdf, 5)
			pdf.SetX(19)
			pdf.MultiCell(0, 5, tr(stripMarkdown(line)), "", "L", false)
		}
		pdf.Ln(1)
	}

	if len(profile.Constraints) > 0 {
		ensureSpace(pdf, 12)
		pdf.SetFont("Arial", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(0, 5, "Constraints", "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.SetTextColor(0, 0, 0)
		for _, constraint := range profile.Constraints {
			text := mapStringValue(constraint, "text", "description", "name", "title")
			if text == "" {
				continue
			}
			ensureSpace(pdf, 5)
			pdf.SetX(19)
			pdf.MultiCell(0, 5, tr("- "+stripMarkdown(text)), "", "L", false)
		}
		pdf.Ln(1)
	}

	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(3)
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
	_, pageHeight := pdf.GetPageSize()
	safeMargin := 20.0 // minimum margin from bottom before forcing new page
	
	// Check if artifact fits on current page
	availableSpace := pageHeight - pdf.GetY() - 15 // 15mm bottom margin
	
	// If artifact is small (fits on one page), keep it together
	if sectionHeight+6 < availableSpace {
		// Fits on current page - render normally
		renderArtifactContent(pdf, tr, node, xStart, indent, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs, linkID)
	} else if sectionHeight+6 < pageHeight-30 {
		// Artifact is medium-sized (fits on one page if started at top)
		// Force to new page
		pdf.AddPage()
		renderArtifactContent(pdf, tr, node, xStart, indent, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs, linkID)
	} else {
		// Artifact is too large for one page - need to split
		// Start on new page with split-rendering logic
		pdf.AddPage()
		renderArtifactContentWithSplitting(pdf, tr, node, xStart, indent, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs, linkID, pageHeight, safeMargin)
	}

	pdf.Ln(2)

	for _, child := range node.children {
		renderArtifactNode(pdf, tr, child, depth+1, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs)
	}
}

// renderArtifactContent renders the core content of an artifact (title, body, table, images)
func renderArtifactContent(
	pdf *gofpdf.Fpdf,
	tr func(string) string,
	node *artifactNode,
	xStart float64,
	indent float64,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
	linkIDs map[string]int,
	linkID int,
) {
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
}

// renderArtifactContentWithSplitting handles artifacts too large for one page
func renderArtifactContentWithSplitting(
	pdf *gofpdf.Fpdf,
	tr func(string) string,
	node *artifactNode,
	xStart float64,
	indent float64,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
	linkIDs map[string]int,
	linkID int,
	pageHeight float64,
	safeMargin float64,
) {
	// For split content, we still render the title and basic content
	// but mark continuation sections properly
	if node.artifact.Type == "heading" {
		pdf.SetFont("Arial", "B", 13)
	} else {
		pdf.SetFont("Arial", "B", 11)
	}

	// Skip title for description artifacts
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

	// For split rendering, call the table renderer with split flag
	if node.artifact.Type != "heading" && node.artifact.Type != "description" {
		renderArtifactDetailsTableWithSplitting(pdf, tr, node, xStart, indent, attachmentMap, linkGroupsByArtifact, artifactTitles, linkIDs, pageHeight, safeMargin)
	}
}

// renderSingleTextField renders a text field that fits on one page
func renderSingleTextField(pdf *gofpdf.Fpdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight float64, label, text string) {
	startY := pdf.GetY()
	
	pdf.SetFont("Arial", "", 9)
	textHeight := calculateTextHeight(pdf, text, valueColWidth, 9)
	cellHeight := textHeight + 2
	
	// Draw complete box border
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.5)
	pdf.Rect(tableX, startY, tableWidth, cellHeight, "")
	
	// Draw vertical column separator
	pdf.SetLineWidth(0.2)
	pdf.Line(tableX+labelColWidth, startY, tableX+labelColWidth, startY+cellHeight)
	
	// Label cell
	pdf.SetXY(tableX, startY)
	pdf.SetTextColor(60, 60, 60)
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(labelColWidth, cellHeight, label, "", 0, "TL", false, 0, "")
	
	// Text value cell
	pdf.SetXY(tableX+labelColWidth, startY)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 9)
	pdf.MultiCell(valueColWidth, rowHeight, text, "", "L", false)
}

// renderSplitTextField renders a text field that spans multiple pages
func renderSplitTextField(pdf *gofpdf.Fpdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight float64, label, text string, pageHeight, safeMargin float64) {
	currentY := pdf.GetY()
	availableHeight := pageHeight - currentY - safeMargin
	
	// Split text into lines
	pdf.SetFont("Arial", "", 9)
	lines := pdf.SplitLines([]byte(text), valueColWidth)
	
	// Calculate how many lines fit on current page
	linesPerPage := int(availableHeight / rowHeight)
	if linesPerPage < 1 {
		// Not enough space, move to next page
		pdf.AddPage()
		renderSingleTextField(pdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight, label, text)
		return
	}
	
	// First part - on current page
	firstPartLines := lines[:linesPerPage]
	firstPartText := strings.Join(convertBytesToStrings(firstPartLines), "\n")
	firstPartHeight := float64(len(firstPartLines)) * rowHeight
	
	// Draw first part (no [continued] tag on first part)
	pdf.SetXY(tableX, currentY)
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.5)
	pdf.Rect(tableX, currentY, tableWidth, firstPartHeight+2, "")
	pdf.SetLineWidth(0.2)
	pdf.Line(tableX+labelColWidth, currentY, tableX+labelColWidth, currentY+firstPartHeight+2)
	
	pdf.SetTextColor(60, 60, 60)
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(labelColWidth, firstPartHeight, label, "", 0, "TL", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 9)
	pdf.SetX(tableX + labelColWidth)
	pdf.MultiCell(valueColWidth, rowHeight, firstPartText, "", "L", false)
	
	currentY = pdf.GetY()
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.2)
	pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
	
	// Second part - on next page
	if len(lines) > linesPerPage {
		pdf.AddPage()
		remainingLines := lines[linesPerPage:]
		remainingText := strings.Join(convertBytesToStrings(remainingLines), "\n")
		renderSingleTextField(pdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight, label+" [continued]", remainingText)
	}
}

// convertBytesToStrings converts [][]byte to []string
func convertBytesToStrings(lines [][]byte) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = string(line)
	}
	return result
}

func renderArtifactDetailsTableWithSplitting(
	pdf *gofpdf.Fpdf,
	tr func(string) string,
	node *artifactNode,
	xStart float64,
	indent float64,
	attachmentMap map[string][]*attachments.Attachment,
	linkGroupsByArtifact map[string]linkGroups,
	artifactTitles map[string]string,
	linkIDs map[string]int,
	pageHeight float64,
	safeMargin float64,
) {
	// Render the table row by row, checking for page breaks
	tableX := xStart
	tableWidth := 170.0 - indent
	labelColWidth := 50.0
	valueColWidth := tableWidth - labelColWidth
	rowHeight := 5.0
	
	// Description field
	if node.artifact.Body != "" {
		plainText := stripMarkdown(node.artifact.Body)
		text := tr(plainText)
		descriptionHeight := calculateTextHeight(pdf, text, valueColWidth, 9)
		availableHeight := pageHeight - pdf.GetY() - safeMargin
		
		if descriptionHeight > availableHeight && availableHeight > 15 {
			// Text is too long for current page - split it
			renderSplitTextField(pdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight, 
				"Description:", text, pageHeight, safeMargin)
		} else if descriptionHeight > availableHeight {
			// Not enough space on current page - move to next page
			pdf.AddPage()
			renderSingleTextField(pdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight, 
				"Description:", text)
		} else {
			// Fits on current page
			renderSingleTextField(pdf, tableX, tableWidth, labelColWidth, valueColWidth, rowHeight, 
				"Description:", text)
		}
	}
	
	// Type and Version rows
	for _, fieldLabel := range []string{"Type:", "Version:"} {
		if pdf.GetY()+4 > pageHeight-safeMargin {
			pdf.AddPage()
		}
		
		startY := pdf.GetY()
		rowHeight := 5.0
		
		// Draw borders
		pdf.SetDrawColor(200, 200, 200)
		pdf.SetLineWidth(0.5)
		pdf.Rect(tableX, startY, tableWidth, rowHeight, "")
		pdf.SetLineWidth(0.2)
		pdf.Line(tableX+labelColWidth, startY, tableX+labelColWidth, startY+rowHeight)
		
		// Draw label
		pdf.SetXY(tableX, startY)
		pdf.SetTextColor(60, 60, 60)
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(labelColWidth, rowHeight, fieldLabel, "", 0, "CM", false, 0, "")
		
		// Draw value
		pdf.SetXY(tableX+labelColWidth, startY)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Arial", "", 9)
		
		var fieldValue string
		if fieldLabel == "Type:" {
			fieldValue = tr(node.artifact.Type)
		} else {
			fieldValue = fmt.Sprintf("v%d", node.artifact.Version)
		}
		pdf.CellFormat(valueColWidth, rowHeight, fieldValue, "", 0, "CM", false, 0, "")
		
		// Draw bottom border
		pdf.SetDrawColor(200, 200, 200)
		pdf.SetLineWidth(0.2)
		pdf.Line(tableX, startY+rowHeight, tableX+tableWidth, startY+rowHeight)
		
		pdf.SetY(startY + rowHeight)
	}
	
	// Links section
	if groups, ok := linkGroupsByArtifact[node.artifact.ID]; ok {
		if len(groups.incoming) > 0 {
			incomingHeight := calculateLinkRowHeight(pdf, valueColWidth, groups.incoming, artifactTitles, true)
			
			if pdf.GetY()+incomingHeight > pageHeight-safeMargin {
				pdf.AddPage()
			}
			
			pdf.SetXY(tableX, pdf.GetY())
			pdf.SetTextColor(60, 60, 60)
			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(labelColWidth, incomingHeight, "Incoming Links:", "", 0, "TL", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "", 9)
			renderLinksWithHyperlinks(pdf, tableX+labelColWidth, pdf.GetY(), valueColWidth, groups.incoming, artifactTitles, linkIDs, true)
			currentY := pdf.GetY()
			pdf.SetDrawColor(200, 200, 200)
			pdf.SetLineWidth(0.2)
			pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
		}
		
		if len(groups.outgoing) > 0 {
			outgoingHeight := calculateLinkRowHeight(pdf, valueColWidth, groups.outgoing, artifactTitles, false)
			
			if pdf.GetY()+outgoingHeight > pageHeight-safeMargin {
				pdf.AddPage()
			}
			
			pdf.SetXY(tableX, pdf.GetY())
			pdf.SetTextColor(60, 60, 60)
			pdf.SetFont("Arial", "B", 9)
			pdf.CellFormat(labelColWidth, outgoingHeight, "Outgoing Links:", "", 0, "TL", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
			pdf.SetFont("Arial", "", 9)
			renderLinksWithHyperlinks(pdf, tableX+labelColWidth, pdf.GetY(), valueColWidth, groups.outgoing, artifactTitles, linkIDs, false)
			currentY := pdf.GetY()
			pdf.SetDrawColor(200, 200, 200)
			pdf.SetLineWidth(0.2)
			pdf.Line(tableX, currentY, tableX+tableWidth, currentY)
		}
	}
	
	// Images
	for _, attachment := range attachmentMap[node.artifact.ID] {
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
		width, height := calculateImageSize(info, maxWidth)
		imageRowHeight := height + 4
		
		if pdf.GetY()+imageRowHeight > pageHeight-safeMargin {
			pdf.AddPage()
		}
		
		pdf.SetXY(tableX, pdf.GetY())
		pdf.SetTextColor(60, 60, 60)
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(labelColWidth, imageRowHeight, "Image:", "", 0, "TL", false, 0, "")
		
		imageY := pdf.GetY() + 2.0
		imageX := tableX + labelColWidth + 2.0
		pdf.ImageOptions(imagePath, imageX, imageY, width, height, false, options, 0, "")
		
		pdf.SetY(pdf.GetY() + imageRowHeight)
		pdf.SetDrawColor(200, 200, 200)
		pdf.SetLineWidth(0.2)
		pdf.Line(tableX, pdf.GetY(), tableX+tableWidth, pdf.GetY())
	}
	
	// Reset
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetLineWidth(0.2)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Arial", "", 10)
	pdf.Ln(2)
}

// calculateTextHeight estimates the height needed to render text with wrapping
func calculateTextHeight(pdf *gofpdf.Fpdf, text string, width float64, fontSize float64) float64 {
	pdf.SetFont("Arial", "", fontSize)
	lines := pdf.SplitLines([]byte(text), width)
	return float64(len(lines)) * 5.0 // 5mm per line
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
