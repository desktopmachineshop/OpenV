package templates

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
)

const (
	guidedSkeletonTemplateKey = "guided-product-skeleton"

	// RetiredCncMillTemplateKey identifies the Desktop CNC Mill example that
	// used to ship as a bundled default. It is no longer seeded; migration
	// 0020 deletes the row from databases that were seeded before it was
	// retired (SeedDefaults only ever adds, never removes).
	RetiredCncMillTemplateKey = "example-cnc-mill"
)

// TemplateData wraps project export with metadata for file-based templates
type TemplateData struct {
	Key         string                    `json:"key"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Source      string                    `json:"source"`
	ExportedAt  time.Time                 `json:"exported_at"`
	Version     string                    `json:"version"`
	Artifacts   []*artifacts.Artifact     `json:"artifacts"`
	Links       []*linksdomain.Link       `json:"links"`
	Attachments []*attachments.Attachment `json:"attachments"`
}

// DefaultTemplates returns bundled templates.
func DefaultTemplates() ([]*Template, error) {
	guidedSnapshot, err := buildGuidedProductSkeletonSnapshot()
	if err != nil {
		return nil, err
	}

	return []*Template{
		{
			ID:          uuid.New().String(),
			Key:         guidedSkeletonTemplateKey,
			Name:        "Guided Product Skeleton",
			Description: "Starter structure showing personas, user needs, derived requirements, and validating test cases.",
			Snapshot:    json.RawMessage(guidedSnapshot),
			IsDefault:   true,
			CreatedAt:   time.Now(),
		},
	}, nil
}

// buildGuidedProductSkeletonSnapshot builds a small starter project that
// demonstrates the product-discovery vocabulary: personas, user needs,
// requirements derived from needs ("derives-from"), and test cases that both
// verify requirements and validate user needs ("validates").
func buildGuidedProductSkeletonSnapshot() ([]byte, error) {
	now := time.Now()
	artifactsList := make([]*artifacts.Artifact, 0)
	linksList := make([]*linksdomain.Link, 0)

	newArtifact := func(id, parentID, artType, title, body string, attrs map[string]interface{}) *artifacts.Artifact {
		var parent *string
		if parentID != "" {
			parent = &parentID
		}
		if attrs == nil {
			attrs = map[string]interface{}{}
		}
		return &artifacts.Artifact{
			ID:         id,
			ProjectID:  "guided-skeleton",
			ParentID:   parent,
			Type:       artType,
			Title:      title,
			Body:       body,
			Attributes: attrs,
			Version:    1,
			ValidFrom:  now,
			ValidTo:    nil,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	newLink := func(fromID, toID, linkType string) *linksdomain.Link {
		return &linksdomain.Link{
			ID:         uuid.New().String(),
			FromID:     fromID,
			ToID:       toID,
			Type:       linkType,
			Attributes: map[string]interface{}{},
			Version:    1,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	root := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(root, "", "heading", "Guided Product Skeleton",
		"Starter structure produced by guided product discovery: who the product serves, what they need, and how those needs are met and validated.", nil))

	// Personas.
	personasHeading := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(personasHeading, root, "heading", "Personas",
		"Who the product serves.", nil))
	workshopLead := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(workshopLead, personasHeading, "persona", "Workshop Lead",
		"Runs a small prototyping workshop. Comfortable with tools, short on time; wants predictable results without babysitting equipment.", nil))

	// User needs.
	needsHeading := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(needsHeading, root, "heading", "User Needs",
		"Problems and outcomes the personas care about, in their own words.", nil))
	needUnattended := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(needUnattended, needsHeading, "user-need", "Run jobs unattended",
		"As a workshop lead, I need long jobs to run safely without supervision so I can work on other tasks.", nil))
	needQuickSetup := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(needQuickSetup, needsHeading, "user-need", "Set up a job quickly",
		"As a workshop lead, I need to go from design file to running job in minutes, not hours.", nil))

	// Requirements derived from user needs.
	requirementsHeading := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(requirementsHeading, root, "heading", "Requirements",
		"System requirements derived from the user needs above.", nil))
	reqAutoShutdown := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(reqAutoShutdown, requirementsHeading, "requirement", "Automatic fault shutdown",
		"The system shall detect fault conditions and shut down safely within 2 seconds without operator intervention.",
		map[string]interface{}{
			"verification_method": "test",
			"verification_status": "unverified",
		}))

	// Verification & validation.
	verificationHeading := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(verificationHeading, root, "heading", "Verification",
		"Test cases that verify requirements and validate user needs.", nil))
	testFaultShutdown := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(testFaultShutdown, verificationHeading, "test-case", "Fault shutdown test",
		"Inject a simulated fault during an unattended job and verify the system shuts down safely within 2 seconds.",
		map[string]interface{}{
			"verification_method": "test",
		}))

	linksList = append(linksList,
		// Requirement derives from a user need.
		newLink(reqAutoShutdown, needUnattended, "derives-from"),
		// Test case verifies the requirement...
		newLink(testFaultShutdown, reqAutoShutdown, "verifies"),
		// ...and validates the originating user need.
		newLink(testFaultShutdown, needUnattended, "validates"),
	)

	// needQuickSetup is intentionally left without a derived requirement so the
	// gap analysis view has something to show out of the box.
	_ = needQuickSetup

	exportData := exports.ProjectExport{
		ExportedAt:  now,
		Version:     "1.0",
		ProjectID:   "guided-skeleton",
		ProjectName: "Guided Product Skeleton",
		ProjectDesc: "Starter structure with personas, user needs, derived requirements, and validating test cases.",
		Artifacts:   artifactsList,
		Links:       linksList,
		Attachments: []*attachments.Attachment{},
	}

	return json.MarshalIndent(exportData, "", "  ")
}

// TemplateSummary represents template metadata without the full snapshot
type TemplateSummary struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "database" or "file"
	IsDefault   bool   `json:"is_default"`
}

// LoadFileBasedTemplates discovers and returns all file-based templates from the examples directory
func LoadFileBasedTemplates(examplesDir string) ([]*TemplateSummary, error) {
	var templates []*TemplateSummary

	// Check if examples directory exists
	info, err := os.Stat(examplesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist yet, return empty list
			return templates, nil
		}
		return nil, fmt.Errorf("failed to stat examples directory: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("examples path is not a directory")
	}

	// List subdirectories in examples
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read examples directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectFile := filepath.Join(examplesDir, entry.Name(), "project.json")
		data, err := os.ReadFile(projectFile)
		if err != nil {
			log.Printf("Warning: failed to read template %s: %v", entry.Name(), err)
			continue
		}

		// Parse the template data
		var templateData TemplateData
		if err := json.Unmarshal(data, &templateData); err != nil {
			log.Printf("Warning: failed to parse template %s: %v", entry.Name(), err)
			continue
		}

		// Not every examples/ subdirectory is a template: plain project
		// exports (project_id/project_name shape, meant for Import Project)
		// parse cleanly but carry no key or name, and would otherwise show
		// up as a blank, unusable row in the picker.
		if templateData.Key == "" || templateData.Name == "" {
			continue
		}

		// Create summary (using a hash of key as ID for consistency)
		summary := &TemplateSummary{
			ID:          uuid.NewSHA1(uuid.NameSpaceOID, []byte(templateData.Key)).String(),
			Key:         templateData.Key,
			Name:        templateData.Name,
			Description: templateData.Description,
			Source:      "file",
			IsDefault:   true, // File-based templates are considered defaults
		}

		templates = append(templates, summary)
	}

	return templates, nil
}

// GetFileBasedTemplateSnapshot loads the full snapshot for a file-based template
// It accepts either the template key or the derived ID
func GetFileBasedTemplateSnapshot(examplesDir string, keyOrID string) ([]byte, error) {
	// Find the matching template directory by key or ID
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read examples directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectFile := filepath.Join(examplesDir, entry.Name(), "project.json")
		data, err := os.ReadFile(projectFile)
		if err != nil {
			continue
		}

		var templateData TemplateData
		if err := json.Unmarshal(data, &templateData); err != nil {
			continue
		}
		// Skip non-template exports (see LoadFileBasedTemplates) so an empty
		// keyOrID can never match one.
		if templateData.Key == "" || templateData.Name == "" {
			continue
		}

		// Check if this matches by key or by ID
		derivedID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(templateData.Key)).String()
		if templateData.Key == keyOrID || derivedID == keyOrID {
			// Convert TemplateData back to ProjectExport format for the snapshot
			export := exports.ProjectExport{
				ExportedAt:  templateData.ExportedAt,
				Version:     templateData.Version,
				ProjectID:   templateData.Key,
				ProjectName: templateData.Name,
				ProjectDesc: templateData.Description,
				Artifacts:   templateData.Artifacts,
				Links:       templateData.Links,
				Attachments: templateData.Attachments,
			}

			return json.MarshalIndent(export, "", "  ")
		}
	}

	return nil, fmt.Errorf("template not found: %s", keyOrID)
}
