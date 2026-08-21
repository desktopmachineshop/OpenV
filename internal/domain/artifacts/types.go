package artifacts

// ArtifactTypeDef describes one artifact type for UIs and validation.
// This catalog is the single Go-side source of truth for the type
// vocabulary; the frontend fetches it via /api/v1/meta/artifact-types.
type ArtifactTypeDef struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// Artifact type values.
const (
	TypeHeading     = "heading"
	TypeDescription = "description"
	TypeRequirement = "requirement"
	TypeUserNeed    = "user-need"
	TypePersona     = "persona"
	TypeTestCase    = "test-case"
	TypeHazard      = "hazard"
	TypeDesignItem  = "design-item"
	TypeOther       = "other"
)

var typeCatalog = []ArtifactTypeDef{
	{Value: TypeHeading, Label: "Heading", Description: "Structural section heading", Color: "#7f8c8d"},
	{Value: TypeDescription, Label: "Description", Description: "Narrative or informative text", Color: "#95a5a6"},
	{Value: TypePersona, Label: "Persona", Description: "A type of user or stakeholder of the product", Color: "#8e44ad"},
	{Value: TypeUserNeed, Label: "User Need", Description: "Something a persona needs from the product", Color: "#2980b9"},
	{Value: TypeRequirement, Label: "Requirement", Description: "A verifiable statement of what the system shall do", Color: "#3498db"},
	{Value: TypeDesignItem, Label: "Design Item", Description: "A design element that satisfies requirements", Color: "#16a085"},
	{Value: TypeTestCase, Label: "Test Case", Description: "A procedure that verifies requirements or validates needs", Color: "#27ae60"},
	{Value: TypeHazard, Label: "Hazard", Description: "A potential source of harm to be mitigated", Color: "#e74c3c"},
	{Value: TypeOther, Label: "Other", Description: "Anything that doesn't fit the other types", Color: "#bdc3c7"},
}

// TypeCatalog returns the artifact type vocabulary.
func TypeCatalog() []ArtifactTypeDef {
	return typeCatalog
}

// ValidType reports whether a type value is in the catalog.
func ValidType(value string) bool {
	for _, t := range typeCatalog {
		if t.Value == value {
			return true
		}
	}
	return false
}
