package exports

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// ExportFormat represents the export file format
type ExportFormat string

const (
	FormatJSON  ExportFormat = "json"
	FormatCSV   ExportFormat = "csv"
	FormatExcel ExportFormat = "excel"
)

// ProjectExport contains all project data for export
type ProjectExport struct {
	ExportedAt   time.Time                `json:"exported_at"`
	Version      string                   `json:"version"`
	ProjectID    string                   `json:"project_id"`
	ProjectName  string                   `json:"project_name"`
	ProjectDesc  string                   `json:"project_description"`
	Artifacts    []*artifacts.Artifact    `json:"artifacts"`
	Links        []*links.Link            `json:"links"`
	Attachments  []*attachments.Attachment `json:"attachments"`
	ProductProfile *products.ProductProfile `json:"product_profile,omitempty"`
}

// Service defines the export/import service interface
type Service interface {
	ExportProject(projectID string, format ExportFormat) ([]byte, string, error)
	ImportProject(data []byte, orgID string) (string, error)
	ImportProjectWithOverrides(data []byte, nameOverride string, descOverride string, orgID string) (string, error)
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
}

// SetProductService wires an optional product profile service. When set,
// exports include the project's product profile and imports restore it.
func (s *DefaultService) SetProductService(ps products.Service) {
	s.productService = ps
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

	// Get all attachments for all artifacts
	var allAttachments []*attachments.Attachment
	for _, artifact := range artifactList {
		attachmentList, err := s.attachmentService.GetAttachmentsByArtifact(artifact.ID)
		if err != nil {
			// Log error but continue
			continue
		}
		allAttachments = append(allAttachments, attachmentList...)
	}

	// Create export data structure
	exportData := &ProjectExport{
		ExportedAt:   time.Now(),
		Version:      "1.0",
		ProjectID:    project.ID,
		ProjectName:  project.Name,
		ProjectDesc:  project.Description,
		Artifacts:    artifactList,
		Links:        linkList,
		Attachments:  allAttachments,
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
		return nil, "", fmt.Errorf("CSV export not yet implemented")
	case FormatExcel:
		return nil, "", fmt.Errorf("Excel export not yet implemented")
	default:
		return nil, "", fmt.Errorf("unsupported export format: %s", format)
	}
}

// exportJSON exports project data as JSON
func (s *DefaultService) exportJSON(data *ProjectExport) ([]byte, string, error) {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	filename := fmt.Sprintf("project_%s_%s.json", data.ProjectName, time.Now().Format("20060102_150405"))
	return jsonData, filename, nil
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
	log.Printf("[IMPORT] Starting import with %d artifacts", len(importData.Artifacts))

	if nameOverride != "" {
		importData.ProjectName = nameOverride
	}
	if descOverride != "" {
		importData.ProjectDesc = descOverride
	}

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
			log.Printf("[IMPORT] Warning: failed to restore product profile: %v", err)
		}
	}

	if _, err := s.importArtifactsAndLinks(projectID, &importData, false); err != nil {
		return "", err
	}

	// Note: Attachments are not imported as the actual image files
	// are not included in the JSON export. Only metadata was exported.

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

	return s.importArtifactsAndLinks(projectID, &importData, markDraft)
}

// importArtifactsAndLinks performs the shared two-pass artifact import, link
// creation, and link snapshot population for an already existing project.
// It returns the created artifact IDs in import order.
func (s *DefaultService) importArtifactsAndLinks(projectID string, importData *ProjectExport, markDraft bool) ([]string, error) {
	// Map old artifact IDs to new ones
	idMap := make(map[string]string)
	createdIDs := make([]string, 0, len(importData.Artifacts))

	// First pass: Create all artifacts without parent relationships in a single transaction
	// We need to do this in two passes to handle parent-child relationships
	log.Printf("[IMPORT] Starting first pass: creating %d artifacts", len(importData.Artifacts))
	log.Printf("[IMPORT] Beginning transaction for first pass")

	// Note: We can't use transactions at the repository level since the service layer
	// handles the transaction. Instead, we'll batch the creates but keep individual
	// transactions for now. Watch the database for bottlenecks.

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
		oldID := artifact.ID
		
		log.Printf("[IMPORT] Creating artifact %d/%d: %s", i, len(importData.Artifacts), artifact.Title)
		
		// Create new artifact with this project ID, but no parent yet
		// Always set sortOrder to avoid expensive NextSortOrder() database queries during import
		sortOrderVal := artifact.SortOrder
		if sortOrderVal == 0 {
			sortOrderVal = i + 1 // Use position in import as sort order
		}
		
		start := time.Now()
		newArtifact := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID:  projectID,
			Type:       artifact.Type,
			Title:      artifact.Title,
			Body:       artifact.Body,
			SortOrder:  &sortOrderVal,
			Attributes: artifact.Attributes,
			ParentID:   nil, // Will set in second pass
		})
		
		log.Printf("[IMPORT] [Artifact %d] NewArtifact constructed in %v", i, time.Since(start))
		
		start = time.Now()
		if err := s.artifactService.CreateArtifact(newArtifact); err != nil {
			log.Printf("[IMPORT] ERROR creating artifact %d (%s): %v", i, artifact.Title, err)
			return nil, fmt.Errorf("failed to create artifact %d (%s): %w", i, artifact.Title, err)
		}
		elapsed := time.Since(start)
		log.Printf("[IMPORT] Successfully created artifact %d/%d: %s (ID: %s) in %v", i, len(importData.Artifacts), artifact.Title, newArtifact.ID, elapsed)
		
		// Log if this artifact took an unusually long time
		if elapsed > 100*time.Millisecond {
			log.Printf("[IMPORT] WARNING: Artifact %d took %v (slow)", i, elapsed)
		}
		if i%10 == 0 {
			log.Printf("[IMPORT] Created %d artifacts", i)
		}
		
		// Map old ID to new ID
		idMap[oldID] = newArtifact.ID
		createdIDs = append(createdIDs, newArtifact.ID)
	}

	log.Printf("[IMPORT] First pass complete. Starting second pass: updating parent relationships")
	log.Printf("[IMPORT] ID map has %d entries", len(idMap))
	
	// Second pass: Update parent relationships
	updateCount := 0
	for i, artifact := range importData.Artifacts {
		if artifact.ParentID != nil && *artifact.ParentID != "" {
			newID := idMap[artifact.ID]
			newParentID := idMap[*artifact.ParentID]
			
			if newParentID != "" {
				updateCount++
				log.Printf("[IMPORT] Second pass: updating artifact %d (%s) - parent: %d (%s)", i, artifact.Title, i, *artifact.ParentID)
				
				// Always set sortOrder to avoid expensive NextSortOrder() calls
				sortOrderVal := artifact.SortOrder
				if sortOrderVal == 0 {
					sortOrderVal = i + 1
				}
				// Update the artifact with the parent relationship
				// Must include all fields to prevent clearing existing data
				log.Printf("[IMPORT] About to call UpdateArtifact for artifact %d", i)
				_, err := s.artifactService.UpdateArtifact(newID, artifacts.UpdateArtifactRequest{
					ParentID:   &newParentID,
					Type:       artifact.Type,
					Title:      artifact.Title,
					Body:       artifact.Body,
					SortOrder:  &sortOrderVal,
					Attributes: artifact.Attributes,
				})
				log.Printf("[IMPORT] UpdateArtifact returned for artifact %d", i)
				if err != nil {
					log.Printf("[IMPORT] ERROR in second pass for artifact %d: %v", i, err)
					return nil, fmt.Errorf("failed to update artifact parent: %w", err)
				}
				log.Printf("[IMPORT] Successfully updated artifact %d in second pass", i)
			}
		}
	}
	log.Printf("[IMPORT] Second pass complete. Updated %d artifacts with parent relationships", updateCount)

	// Create links using the new IDs
	log.Printf("[IMPORT] Starting link creation: %d links to import", len(importData.Links))
	for _, link := range importData.Links {
		newFromID, fromExists := idMap[link.FromID]
		newToID, toExists := idMap[link.ToID]
		
		// Only create link if both artifacts were imported
		if fromExists && toExists {
			newLink := links.NewLink(links.CreateLinkRequest{
				FromID: newFromID,
				ToID:   newToID,
				Type:   link.Type,
			})
			
			if err := s.linkService.CreateLink(newLink); err != nil {
				// Log error but continue with other links
				log.Printf("[IMPORT] Warning: failed to create link: %v", err)
			}
		}
	}
	log.Printf("[IMPORT] Link creation complete")

	// Populate link snapshots for all imported artifacts
	log.Printf("[IMPORT] Starting link snapshot population for %d artifacts", len(idMap))
	if err := s.populateLinksSnapshotsForImport(idMap); err != nil {
		log.Printf("[IMPORT] Warning: failed to populate link snapshots: %v", err)
		// Don't fail the import, but log the error
	}
	log.Printf("[IMPORT] Link snapshot population complete")

	return createdIDs, nil
}

// populateLinksSnapshotsForImport populates link snapshots for all imported artifacts
// This ensures links are visible when viewing artifact history/versions
func (s *DefaultService) populateLinksSnapshotsForImport(idMap map[string]string) error {
	// For each newly created artifact, fetch its links and store in snapshot
	for _, newID := range idMap {
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)
		
		// Get all incoming links
		incomingLinks, err := s.linkService.GetLinksTo(newID)
		if err == nil {
			for _, link := range incomingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}
		
		// Get all outgoing links
		outgoingLinks, err := s.linkService.GetLinksFrom(newID)
		if err == nil {
			for _, link := range outgoingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}
		
		// If artifact has links, update its snapshot
		if len(allLinks) > 0 {
			artifact, err := s.artifactService.GetArtifact(newID)
			if err != nil {
				log.Printf("[IMPORT] Warning: failed to get artifact %s for snapshot update: %v", newID, err)
				continue
			}
			
			// Update artifact to store links snapshot WITHOUT incrementing version
			// We do this by directly updating the attributes
			if artifact.Attributes == nil {
				artifact.Attributes = make(map[string]interface{})
			}
			artifact.Attributes["links_snapshot"] = allLinks
			
			// Use UpdateArtifact with all existing fields to avoid clearing data
			// The version will increment, but that's OK - this is part of import
			_, err = s.artifactService.UpdateArtifact(newID, artifacts.UpdateArtifactRequest{
				ParentID:   artifact.ParentID,
				Type:       artifact.Type,
				Title:      artifact.Title,
				Body:       artifact.Body,
				SortOrder:  &artifact.SortOrder,
				Attributes: artifact.Attributes,
			})
			if err != nil {
				log.Printf("[IMPORT] Warning: failed to update artifact %s with link snapshot: %v", newID, err)
				continue
			}
		}
	}
	
	return nil
}
