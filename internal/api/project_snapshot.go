package api

import (
	"errors"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/snapshot"
)

// snapshotSources are where snapshot.Load reads for a handler: a baseline
// through the baseline service, scoped to the project the caller was
// authorized for (a baseline from another project is indistinguishable from
// a missing one, baselines.ErrNotFound, so IDs cannot be probed across
// projects), and the live project through the export service's JSON export.
func (h *Handler) snapshotSources() snapshot.Options {
	return snapshot.Options{
		Baseline: func(projectID, baselineID string) (*snapshot.Baseline, error) {
			baseline, err := h.BaselineService.GetProjectBaseline(projectID, baselineID)
			if err != nil {
				return nil, err
			}
			return snapshotBaseline(baseline), nil
		},
		Live: func(projectID string) ([]byte, error) {
			raw, _, err := h.ExportService.ExportProject(projectID, exports.FormatJSON)
			return raw, err
		},
	}
}

// heldBaseline is snapshot.Load's source for a baseline the handler has
// read already, so that Load decodes it without reading it again.
func heldBaseline(baseline *baselines.Baseline) snapshot.Options {
	return snapshot.Options{Baseline: func(string, string) (*snapshot.Baseline, error) {
		return snapshotBaseline(baseline), nil
	}}
}

// snapshotBaseline is a baseline as snapshot.Load reads it.
func snapshotBaseline(b *baselines.Baseline) *snapshot.Baseline {
	return &snapshot.Baseline{ID: b.ID, Name: b.Name, CreatedAt: b.CreatedAt, Snapshot: b.Snapshot}
}

// projectExport loads the live export or a baseline snapshot as a DTO:
// baselineID "" or "live" is the live project.
func (h *Handler) projectExport(projectID, baselineID string) (*exports.ProjectExport, error) {
	if baselineID == "live" {
		baselineID = ""
	}
	data, _, err := snapshot.Load(projectID, baselineID, h.snapshotSources())
	var bad *snapshot.DecodeError
	if baselineID != "" && errors.As(err, &bad) {
		return nil, fmt.Errorf("failed to parse baseline snapshot: %w", err)
	}
	return data, err
}
