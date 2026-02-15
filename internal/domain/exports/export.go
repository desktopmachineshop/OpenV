package exports

import (
	"encoding/json"
	"fmt"
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

	// First pass: Create all artifacts without parent relationships
	// We need to do this in two passes to handle parent-child relationships
	for _, artifact := range importData.Artifacts {
		oldID := artifact.ID
		
		// Create new artifact with this project ID, but no parent yet
		var sortOrder *int
		if artifact.SortOrder > 0 {
			value := artifact.SortOrder
			sortOrder = &value
		}
		newArtifact := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID:  projectID,
			Type:       artifact.Type,
			Title:      artifact.Title,
			Body:       artifact.Body,
			SortOrder:  sortOrder,
			Attributes: artifact.Attributes,
			ParentID:   nil, // Will set in second pass
		})
		
		if err := s.artifactService.CreateArtifact(newArtifact); err != nil {
			return "", fmt.Errorf("failed to create artifact: %w", err)
		}
		
		// Map old ID to new ID
		idMap[oldID] = newArtifact.ID
	}

	// Second pass: Update parent relationships
	for _, artifact := range importData.Artifacts {
		if artifact.ParentID != nil && *artifact.ParentID != "" {
			newID := idMap[artifact.ID]
			newParentID := idMap[*artifact.ParentID]
			
			if newParentID != "" {
				var sortOrder *int
				if artifact.SortOrder > 0 {
					value := artifact.SortOrder
					sortOrder = &value
				}
				// Update the artifact with the parent relationship
				// Must include all fields to prevent clearing existing data
				_, err := s.artifactService.UpdateArtifact(newID, artifacts.UpdateArtifactRequest{
					ParentID:   &newParentID,
					Type:       artifact.Type,
					Title:      artifact.Title,
					Body:       artifact.Body,
					SortOrder:  sortOrder,
					Attributes: artifact.Attributes,
				})
				if err != nil {
					return "", fmt.Errorf("failed to update artifact parent: %w", err)
				}
			}
		}
	}

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
