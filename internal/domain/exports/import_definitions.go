package exports

import (
	"log/slog"
	"slices"

	"github.com/openv/requirements-platform/internal/domain/attributes"
)

// restoreDefinitions gives a new project the attribute definitions its
// document records, each as a definition of the project itself: an import
// never changes its workspace's definitions. One the project already has in
// effect from its workspace, alike in everything that types a value
// (inEffect), is left to the workspace's. A definition that does not
// validate is logged and skipped; it does not fail the import.
func (s *DefaultService) restoreDefinitions(projectID, orgID string, defs []*attributes.Definition) {
	effective, err := s.attributeService.EffectiveForProject(orgID, projectID)
	if err != nil {
		slog.Warn("import: failed to read the workspace's attribute definitions", slog.Any("error", err))
	}
	for _, def := range defs {
		if def == nil || inEffect(effective, def) {
			continue
		}
		project := projectID
		if _, err := s.attributeService.CreateDefinition(attributes.CreateDefinitionRequest{
			ProjectID:     &project,
			Key:           def.Key,
			Label:         def.Label,
			DataType:      def.DataType,
			EnumValues:    def.EnumValues,
			AppliesToType: def.AppliesToType,
			Required:      def.Required,
			SortOrder:     def.SortOrder,
		}); err != nil {
			slog.Warn("import: failed to restore an attribute definition",
				slog.String("key", def.Key), slog.Any("error", err))
		}
	}
}

// inEffect reports whether effective holds a definition alike to def: the
// same key for the same type, label, data type, values and requirement.
func inEffect(effective []*attributes.Definition, def *attributes.Definition) bool {
	for _, e := range effective {
		if e != nil && e.Key == def.Key && e.AppliesToType == def.AppliesToType && e.Label == def.Label &&
			e.DataType == def.DataType && e.Required == def.Required && slices.Equal(e.EnumValues, def.EnumValues) {
			return true
		}
	}
	return false
}
