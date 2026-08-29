package exports

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// ErrUnsupportedFormat is returned when a project export is requested in a
// format the service cannot produce. Handlers should map it to a 400.
var ErrUnsupportedFormat = errors.New("unsupported export format")

// ExportFormat represents the export file format
type ExportFormat string

const (
	FormatJSON  ExportFormat = "json"
	FormatCSV   ExportFormat = "csv"
	FormatExcel ExportFormat = "excel"
	// FormatReqIF is the OMG ReqIF 1.x interchange format read by DOORS and
	// Polarion (issue #224). Export only for now; import is a fast-follow.
	FormatReqIF ExportFormat = "reqif"
)

// ProjectExport contains all project data for export
type ProjectExport struct {
	ExportedAt     time.Time                 `json:"exported_at"`
	Version        string                    `json:"version"`
	ProjectID      string                    `json:"project_id"`
	ProjectName    string                    `json:"project_name"`
	ProjectDesc    string                    `json:"project_description"`
	Artifacts      []*artifacts.Artifact     `json:"artifacts"`
	Links          []*links.Link             `json:"links"`
	Attachments    []*attachments.Attachment `json:"attachments"`
	ProductProfile *products.ProductProfile  `json:"product_profile,omitempty"`
	// AttributeDefs carries the org/project attribute definitions effective for
	// the project. It is populated only for the ReqIF export (to type enum
	// attributes as ReqIF enumerations); JSON/CSV exports leave it nil so their
	// output is unchanged.
	AttributeDefs []*attributes.Definition `json:"attribute_definitions,omitempty"`
}

// Service defines the export/import service interface
type Service interface {
	ExportProject(projectID string, format ExportFormat) ([]byte, string, error)
	ImportProject(data []byte, orgID string) (string, error)
	ImportProjectWithOverrides(data []byte, nameOverride string, descOverride string, orgID string) (string, error)
	// ImportProjectReqIF imports a ReqIF 1.x document into a new project owned by
	// the given org (issue #238). Malformed ReqIF yields an error the handler
	// maps to 400.
	ImportProjectReqIF(data []byte, orgID string) (string, error)
	ImportArtifactsIntoProject(projectID string, data []byte, markDraft bool) ([]string, error)
}

// DefaultService implements the export service
type DefaultService struct {
	artifactService   artifacts.Service
	linkService       links.Service
	attachmentService attachments.Service
	projectRepo       ProjectRepository
	projectService    projects.Service
	productService    products.Service
	attributeService  attributes.Service
}

// SetProductService wires an optional product profile service. When set,
// exports include the project's product profile and imports restore it.
func (s *DefaultService) SetProductService(ps products.Service) {
	s.productService = ps
}

// SetAttributeService wires an optional attribute-definition service. When set,
// the ReqIF export types enum attributes as ReqIF enumerations using the
// project's effective definitions. Optional: nil degrades gracefully to
// free-form string attributes.
func (s *DefaultService) SetAttributeService(as attributes.Service) {
	s.attributeService = as
}

// ProjectRepository defines methods for retrieving project info
type ProjectRepository interface {
	FindByID(id string) (*ProjectInfo, error)
}

// ProjectInfo contains basic project information
type ProjectInfo struct {
	ID          string
	Name        string
	Description string
}

// NewService creates a new export service
func NewService(
	artifactService artifacts.Service,
	linkService links.Service,
	attachmentService attachments.Service,
	projectRepo ProjectRepository,
	projectService projects.Service,
) *DefaultService {
	return &DefaultService{
		artifactService:   artifactService,
		linkService:       linkService,
		attachmentService: attachmentService,
		projectRepo:       projectRepo,
		projectService:    projectService,
	}
}

// ExportProject exports a project in the specified format
func (s *DefaultService) ExportProject(projectID string, format ExportFormat) ([]byte, string, error) {
	// Get project info
	project, err := s.projectRepo.FindByID(projectID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get project: %w", err)
	}

	// Get all artifacts
	artifactList, err := s.artifactService.ListArtifacts(projectID, "")
	if err != nil {
		return nil, "", fmt.Errorf("failed to get artifacts: %w", err)
	}

	// Get all links
	linkList, err := s.linkService.GetAllLinks(projectID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get links: %w", err)
	}

	// Get all attachments for every artifact in one batched query (was a
	// per-artifact N+1 — the worst offender on the export/baseline/diff/report
	// substrate). Group in Go and flatten in artifact order so the export's
	// attachment ordering is unchanged.
	artifactIDs := make([]string, 0, len(artifactList))
	for _, artifact := range artifactList {
		if artifact != nil {
			artifactIDs = append(artifactIDs, artifact.ID)
		}
	}
	var allAttachments []*attachments.Attachment
	if len(artifactIDs) > 0 {
		byArtifact, err := s.attachmentService.GetAttachmentsByArtifacts(artifactIDs)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get attachments: %w", err)
		}
		for _, artifact := range artifactList {
			if artifact == nil {
				continue
			}
			allAttachments = append(allAttachments, byArtifact[artifact.ID]...)
		}
	}

	// Create export data structure
	exportData := &ProjectExport{
		ExportedAt:  time.Now(),
		Version:     "1.0",
		ProjectID:   project.ID,
		ProjectName: project.Name,
		ProjectDesc: project.Description,
		Artifacts:   artifactList,
		Links:       linkList,
		Attachments: allAttachments,
	}

	// Attach the product profile when a product service is wired.
	// Errors are ignored: the profile is optional data.
	if s.productService != nil {
		if profile, err := s.productService.GetProfile(projectID); err == nil && profile != nil {
			exportData.ProductProfile = profile
		}
	}

	// Export based on format
	switch format {
	case FormatJSON:
		return s.exportJSON(exportData)
	case FormatCSV:
		return s.exportCSV(exportData)
	case FormatReqIF:
		// Attach the effective attribute definitions so enum attributes export
		// as ReqIF enumerations. Best-effort: definitions are optional fidelity,
		// so failures degrade to free-form string attributes rather than fail
		// the export.
		if s.attributeService != nil {
			orgID := ""
			if s.projectService != nil {
				if p, err := s.projectService.GetProject(projectID); err == nil && p != nil {
					orgID = p.OrgID
				}
			}
			if defs, err := s.attributeService.EffectiveForProject(orgID, projectID); err == nil {
				exportData.AttributeDefs = defs
			}
		}
		return s.exportReqIF(exportData)
	case FormatExcel:
		return nil, "", fmt.Errorf("%w: excel export not yet implemented", ErrUnsupportedFormat)
	default:
		return nil, "", fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
}

// exportJSON exports project data as JSON
func (s *DefaultService) exportJSON(data *ProjectExport) ([]byte, string, error) {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return jsonData, exportFilename(data.ProjectName, "json"), nil
}

// csvHeader is the column layout of a CSV project export: one flat row per
// artifact, with outgoing links folded into a single semicolon-separated
// "type:targetId" column.
var csvHeader = []string{
	"id", "type", "title", "body", "status", "version",
	"parent_id", "links", "created_at", "updated_at",
}

// exportCSV exports project data as RFC 4180 CSV, one row per artifact.
func (s *DefaultService) exportCSV(data *ProjectExport) ([]byte, string, error) {
	// Fold links into per-artifact "type:targetId" pairs, keyed by source.
	linksByFrom := make(map[string][]string, len(data.Links))
	for _, link := range data.Links {
		if link == nil {
			continue
		}
		linksByFrom[link.FromID] = append(linksByFrom[link.FromID], link.Type+":"+link.ToID)
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write(csvHeader); err != nil {
		return nil, "", fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, artifact := range data.Artifacts {
		if artifact == nil {
			continue
		}

		// Prefer the first-class status column; fall back to the legacy
		// attribute mirror for exports captured before the column existed.
		status := artifact.Status
		if status == "" && artifact.Attributes != nil {
			if v, ok := artifact.Attributes["status"].(string); ok {
				status = v
			}
		}

		parentID := ""
		if artifact.ParentID != nil {
			parentID = *artifact.ParentID
		}

		row := []string{
			artifact.ID,
			artifact.Type,
			artifact.Title,
			artifact.Body,
			status,
			strconv.Itoa(artifact.Version),
			parentID,
			strings.Join(linksByFrom[artifact.ID], ";"),
			artifact.CreatedAt.UTC().Format(time.RFC3339),
			artifact.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(row); err != nil {
			return nil, "", fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, "", fmt.Errorf("failed to write CSV: %w", err)
	}

	return buf.Bytes(), exportFilename(data.ProjectName, "csv"), nil
}

// exportFilename builds the download filename for an export, sanitizing the
// project name so the value is safe inside a quoted Content-Disposition
// filename: quotes, backslashes, path separators, and control characters are
// stripped.
func exportFilename(projectName, extension string) string {
	name := sanitizeFilenameComponent(projectName)
	if name == "" {
		name = "export"
	}
	return fmt.Sprintf("project_%s_%s.%s", name, time.Now().Format("20060102_150405"), extension)
}

// sanitizeFilenameComponent strips characters that are unsafe in a quoted
// Content-Disposition filename or in filesystem names.
func sanitizeFilenameComponent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f: // control characters
		case r == '"' || r == '\\' || r == '/': // header/path breakers
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// ImportProject imports project data from JSON and creates a new project
// owned by the given org.
func (s *DefaultService) ImportProject(data []byte, orgID string) (string, error) {
	return s.ImportProjectWithOverrides(data, "", "", orgID)
}

// ImportProjectWithOverrides imports project data into a new project owned
// by the given org, overriding name/description when provided.
func (s *DefaultService) ImportProjectWithOverrides(data []byte, nameOverride string, descOverride string, orgID string) (string, error) {
	// Parse the JSON
	var importData ProjectExport
	if err := json.Unmarshal(data, &importData); err != nil {
		return "", fmt.Errorf("failed to parse import data: %w", err)
	}
	slog.Debug("import: starting", slog.Int("artifacts", len(importData.Artifacts)))

	if nameOverride != "" {
		importData.ProjectName = nameOverride
	}
	if descOverride != "" {
		importData.ProjectDesc = descOverride
	}

	return s.createProjectFromExport(&importData, orgID)
}

// ImportProjectReqIF parses a ReqIF 1.x document into a ProjectExport and
// imports it into a new project through the same machinery as the JSON import
// (id remapping, parent reconstruction, link creation, version=1). See
// reqif_import.go for the mapping and fidelity notes.
func (s *DefaultService) ImportProjectReqIF(data []byte, orgID string) (string, error) {
	importData, err := parseReqIF(data)
	if err != nil {
		return "", err
	}
	slog.Debug("import: starting reqif", slog.Int("artifacts", len(importData.Artifacts)))
	return s.createProjectFromExport(importData, orgID)
}

// createProjectFromExport creates a new project owned by orgID and populates it
// from a fully-parsed ProjectExport. It is the shared tail of the JSON and
// ReqIF import paths: project creation, optional product-profile restore, and
// the single-pass artifact/link import.
func (s *DefaultService) createProjectFromExport(importData *ProjectExport, orgID string) (string, error) {
	// Create a new project with the imported name and description
	newProject := projects.NewProject(projects.CreateProjectRequest{
		Name:        importData.ProjectName,
		Description: importData.ProjectDesc,
	})
	newProject.OrgID = orgID

	if err := s.projectService.CreateProject(newProject); err != nil {
		return "", fmt.Errorf("failed to create project: %w", err)
	}

	projectID := newProject.ID

	// Restore the product profile when one was exported and a product service
	// is wired. Failure to restore it does not fail the import.
	if importData.ProductProfile != nil && s.productService != nil {
		profile := importData.ProductProfile
		if _, err := s.productService.UpdateProfile(projectID, products.UpdateProfileRequest{
			Vision:           profile.Vision,
			ProblemStatement: profile.ProblemStatement,
			TargetUsers:      profile.TargetUsers,
			Constraints:      profile.Constraints,
			SuccessMetrics:   profile.SuccessMetrics,
			Settings:         profile.Settings,
		}); err != nil {
			slog.Warn("import: failed to restore product profile", slog.Any("error", err))
		}
	}

	if _, err := s.importArtifactsAndLinks(projectID, importData, false, true); err != nil {
		return "", err
	}

	// Note: Attachments are not imported as the actual image files
	// are not included in the JSON/ReqIF export. Only metadata was exported.

	return projectID, nil
}

// ImportArtifactsIntoProject imports the artifacts and links contained in an
// export payload into an existing project. When markDraft is true, each
// created artifact is stamped with Attributes["status"]="draft" and
// Attributes["origin"] defaulting to "import" when absent. It returns the IDs
// of the created artifacts in import order.
func (s *DefaultService) ImportArtifactsIntoProject(projectID string, data []byte, markDraft bool) ([]string, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}

	var importData ProjectExport
	if err := json.Unmarshal(data, &importData); err != nil {
		return nil, fmt.Errorf("failed to parse import data: %w", err)
	}

	// Refs are never preserved when importing into an existing project: the
	// payload's refs may collide with artifacts already there, so these
	// imports draw fresh refs from the project's counters instead.
	return s.importArtifactsAndLinks(projectID, &importData, markDraft, false)
}

// importArtifactsAndLinks imports an export payload's artifacts and links
// into an already existing project and returns the created artifact IDs in
// import order.
//
// New artifact IDs are pre-generated up front so that parent references and
// link endpoints can be remapped BEFORE anything is written: each artifact is
// then inserted exactly once — with its parent and its links_snapshot
// attribute already in place — and stays at version 1. (The previous
// implementation created artifacts parentless, re-parented them via
// UpdateArtifact, and then ran a per-artifact read-links/read-artifact/update
// N+1 sweep to fill snapshots, bumping every imported artifact to version 2+;
// issue #124.)
func (s *DefaultService) importArtifactsAndLinks(projectID string, importData *ProjectExport, markDraft bool, preserveRefs bool) ([]string, error) {
	total := len(importData.Artifacts)
	slog.Debug("import: starting", slog.Int("artifacts", total), slog.Int("links", len(importData.Links)))

	// Pre-generate the old->new ID map. newIDs is positional so duplicate old
	// IDs in a malformed payload still create distinct artifacts (the map's
	// last entry wins for parent/link remapping, as before).
	idMap := make(map[string]string, total)
	newIDs := make([]string, total)
	for i, artifact := range importData.Artifacts {
		newIDs[i] = uuid.New().String()
		idMap[artifact.ID] = newIDs[i]
	}

	// Build the new-ID link objects before any insert. Link rows are written
	// after the artifacts, but their content is fully known now, which lets
	// each artifact carry its links_snapshot from birth. Links referencing
	// artifacts outside the payload are skipped, as before.
	var newLinks []*links.Link
	snapshotByArtifact := make(map[string][]interface{})
	seenPerArtifact := make(map[string]map[string]bool)
	addToSnapshot := func(artifactID string, link *links.Link) {
		if seenPerArtifact[artifactID] == nil {
			seenPerArtifact[artifactID] = make(map[string]bool)
		}
		if seenPerArtifact[artifactID][link.ID] {
			return
		}
		seenPerArtifact[artifactID][link.ID] = true
		snapshotByArtifact[artifactID] = append(snapshotByArtifact[artifactID], link)
	}
	for _, link := range importData.Links {
		newFromID, fromExists := idMap[link.FromID]
		newToID, toExists := idMap[link.ToID]
		if !fromExists || !toExists {
			continue
		}
		newLink := links.NewLink(links.CreateLinkRequest{
			FromID: newFromID,
			ToID:   newToID,
			Type:   link.Type,
		})
		newLinks = append(newLinks, newLink)
		addToSnapshot(newFromID, newLink)
		addToSnapshot(newToID, newLink)
	}

	// Single creation pass: parent already remapped, snapshot already
	// attached, sort order always explicit (avoids per-row NextSortOrder
	// queries). parent_id has no FK, so insert order is free.
	createdIDs := make([]string, 0, total)
	for i, artifact := range importData.Artifacts {
		if markDraft {
			if artifact.Attributes == nil {
				artifact.Attributes = map[string]interface{}{}
			}
			artifact.Attributes["status"] = "draft"
			if _, ok := artifact.Attributes["origin"]; !ok {
				artifact.Attributes["origin"] = "import"
			}
		}
		if snapshot := snapshotByArtifact[newIDs[i]]; len(snapshot) > 0 {
			if artifact.Attributes == nil {
				artifact.Attributes = map[string]interface{}{}
			}
			artifact.Attributes["links_snapshot"] = snapshot
		}

		var parentID *string
		if artifact.ParentID != nil && *artifact.ParentID != "" {
			if newParentID, ok := idMap[*artifact.ParentID]; ok {
				parentID = &newParentID
			}
		}

		sortOrderVal := artifact.SortOrder
		if sortOrderVal == 0 {
			sortOrderVal = i + 1 // Use position in import as sort order
		}

		newArtifact := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID:  projectID,
			ParentID:   parentID,
			Type:       artifact.Type,
			Title:      artifact.Title,
			Body:       artifact.Body,
			SortOrder:  &sortOrderVal,
			Attributes: artifact.Attributes,
		})
		// Adopt the pre-generated ID so links and children agree with it.
		newArtifact.ID = newIDs[i]
		// Into a NEW project the exported stable refs (REQ-12, ...) carry
		// over, so external references to them survive a round-trip; the
		// repository advances the ref counters past each one. Junk that
		// doesn't parse as PREFIX-NUM is dropped and reassigned.
		if preserveRefs {
			if _, _, ok := artifacts.ParseRef(artifact.Ref); ok {
				newArtifact.Ref = artifact.Ref
			}
		}

		start := time.Now()
		if err := s.artifactService.CreateArtifact(newArtifact); err != nil {
			slog.Error("import: failed to create artifact",
				slog.Int("index", i),
				slog.String("title", artifact.Title),
				slog.Any("error", err))
			return nil, fmt.Errorf("failed to create artifact %d (%s): %w", i, artifact.Title, err)
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			slog.Warn("import: artifact creation was slow",
				slog.Int("index", i),
				slog.Duration("elapsed", elapsed))
		}
		createdIDs = append(createdIDs, newArtifact.ID)
	}

	// Persist the links. A failed link is logged and skipped (as before); its
	// snapshot entry then over-reports, which matches the old tolerance for
	// partially failed link imports.
	slog.Debug("import: starting link creation", slog.Int("links", len(newLinks)))
	for _, link := range newLinks {
		if err := s.linkService.CreateLink(link); err != nil {
			slog.Warn("import: failed to create link", slog.Any("error", err))
		}
	}
	slog.Debug("import: complete", slog.Int("artifacts", len(createdIDs)), slog.Int("links", len(newLinks)))

	return createdIDs, nil
}
