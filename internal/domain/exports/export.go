package exports

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/attachments"
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
}

// Service defines the export/import service interface
type Service interface {
	ExportProject(projectID string, format ExportFormat) ([]byte, string, error)
	ImportProject(data []byte) (string, error)
	ImportProjectWithOverrides(data []byte, nameOverride string, descOverride string) (string, error)
}

// DefaultService implements the export service
type DefaultService struct {
	artifactService   artifacts.Service
	linkService       links.Service
	attachmentService attachments.Service
	projectRepo       ProjectRepository
	projectService    projects.Service
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
func (s *DefaultService) ImportProject(data []byte) (string, error) {
	return s.ImportProjectWithOverrides(data, "", "")
}

// ImportProjectWithOverrides imports project data and overrides name/description when provided.
func (s *DefaultService) ImportProjectWithOverrides(data []byte, nameOverride string, descOverride string) (string, error) {
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
	
	if err := s.projectService.CreateProject(newProject); err != nil {
		return "", fmt.Errorf("failed to create project: %w", err)
	}
	
	projectID := newProject.ID

	// Map old artifact IDs to new ones
	idMap := make(map[string]string)

	// First pass: Create all artifacts without parent relationships in a single transaction
	// We need to do this in two passes to handle parent-child relationships
	log.Printf("[IMPORT] Starting first pass: creating %d artifacts", len(importData.Artifacts))
	log.Printf("[IMPORT] Beginning transaction for first pass")
	
	// Note: We can't use transactions at the repository level since the service layer
	// handles the transaction. Instead, we'll batch the creates but keep individual
	// transactions for now. Watch the database for bottlenecks.
	
	for i, artifact := range importData.Artifacts {
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
			return "", fmt.Errorf("failed to create artifact %d (%s): %w", i, artifact.Title, err)
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
					return "", fmt.Errorf("failed to update artifact parent: %w", err)
				}
				log.Printf("[IMPORT] Successfully updated artifact %d in second pass", i)
			}
		}
	}
	log.Printf("[IMPORT] Second pass complete. Updated %d artifacts with parent relationships", updateCount)

	// Create links using the new IDs
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
				fmt.Printf("Warning: failed to create link: %v\n", err)
			}
		}
	}

	// Note: Attachments are not imported as the actual image files
	// are not included in the JSON export. Only metadata was exported.

	return projectID, nil
}
