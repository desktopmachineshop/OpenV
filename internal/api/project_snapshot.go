package api

import (
	"encoding/json"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/exports"
)

// projectExport loads the live export or a baseline snapshot as a DTO. The
// baseline load is scoped to the project the caller was authorized for: a
// baseline from another project is indistinguishable from a missing one
// (baselines.ErrNotFound), so IDs cannot be probed across projects.
func (h *Handler) projectExport(projectID, baselineID string) (*exports.ProjectExport, error) {
	if baselineID != "" && baselineID != "live" {
		baseline, err := h.BaselineService.GetProjectBaseline(projectID, baselineID)
		if err != nil {
			return nil, err
		}
		var data exports.ProjectExport
		if err := json.Unmarshal(baseline.Snapshot, &data); err != nil {
			return nil, fmt.Errorf("failed to parse baseline snapshot: %w", err)
		}
		return &data, nil
	}
	raw, _, err := h.ExportService.ExportProject(projectID, exports.FormatJSON)
	if err != nil {
		return nil, err
	}
	var data exports.ProjectExport
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}
