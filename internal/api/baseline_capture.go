package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/plan"
)

// captureBaseline takes a baseline of the project: a snapshot of its
// definition and, with it, where its plan stands and every task's dates,
// so one baseline answers both what changed and how far the project had
// got. The plan is left out for a project that has no task and no
// milestone, unless always. It publishes baseline.captured, and answers
// false after writing the error.
func (h *Handler) captureBaseline(w http.ResponseWriter, r *http.Request, projectID, name string, always bool) (*baselines.Baseline, *plan.Baseline, bool) {
	if name == "" {
		name = fmt.Sprintf("Baseline %s", time.Now().Format("2006-01-02 15:04"))
	}
	var p *plan.Plan
	if h.PlanService != nil {
		loaded, ok := h.loadPlan(w, r, projectID)
		if !ok {
			return nil, nil, false
		}
		if always || len(loaded.Tasks) > 0 || len(loaded.Milestones) > 0 {
			p = loaded
		}
	}

	// The snapshot is the JSON export with the project's attribute
	// definitions beside it (REQ-5): attachment metadata, not their files.
	data, err := h.ExportService.Snapshot(projectID)
	if err != nil {
		respondInternal(w, r, "failed to export project", err)
		return nil, nil, false
	}

	// CurrentUserID is nil when the caller is an automation holding a
	// workspace key rather than a person, which the column records as an
	// unattributed capture instead of blaming somebody.
	baseline, err := h.BaselineService.CreateBaseline(projectID, name, data, CurrentUserID(r))
	if err != nil {
		respondInternal(w, r, "failed to create baseline", err)
		return nil, nil, false
	}
	var planned *plan.Baseline
	if p != nil {
		if planned, err = h.PlanService.SaveBaselineOf(p, name, &baseline.ID, time.Now()); err != nil {
			// Half a baseline would say the plan was not there: take neither.
			_ = h.BaselineService.DeleteBaseline(baseline.ID)
			writePlanError(w, r, err)
			return nil, nil, false
		}
		baseline.Plan, _ = json.Marshal(planned.Moment)
	}

	h.publish(r, events.BaselineCaptured, projectID, baseline.ID, map[string]interface{}{
		"name": name,
	})
	return baseline, planned, true
}
