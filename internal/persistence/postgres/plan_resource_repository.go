package postgres

import (
	"context"
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/plan"
)

// The plan's resources: the rate and capacity a project keeps for each
// person and agent, its planned resources, and handing one resource's tasks
// to another. Part of PlanRepository (plan_repository.go).

// ListResources lists the daily rate and capacity the project keeps for
// each person or agent that has one.
func (r *PlanRepository) ListResources(projectID string) ([]plan.Resource, error) {
	rows, err := r.db.Query(`
		SELECT assignee_type, assignee_id, daily_rate, capacity, name FROM plan_resources
		WHERE project_id = $1 ORDER BY assignee_type, assignee_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.Resource{}
	for rows.Next() {
		var res plan.Resource
		if err := rows.Scan(&res.Type, &res.ID, &res.DailyRate, &res.Capacity, &res.PlannedName); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// SaveResource stores a person's or an agent's daily rate and capacity, and
// a planned resource's with its name.
func (r *PlanRepository) SaveResource(projectID string, res plan.Resource) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_resources (project_id, assignee_type, assignee_id, daily_rate, capacity, name) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (project_id, assignee_type, assignee_id) DO UPDATE
		SET daily_rate = EXCLUDED.daily_rate, capacity = EXCLUDED.capacity, name = EXCLUDED.name`,
		projectID, res.Type, res.ID, res.DailyRate, res.Capacity, res.PlannedName)
	if malformedID(err) {
		return plan.ErrResource
	}
	return err
}

// RemoveResource forgets a resource of the project and takes it off every
// task of the project it was assigned to.
func (r *PlanRepository) RemoveResource(projectID, kind, id string) error {
	err := withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM plan_assignments WHERE project_id = $1 AND assignee_type = $2 AND assignee_id = $3`, projectID, kind, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM plan_resources WHERE project_id = $1 AND assignee_type = $2 AND assignee_id = $3`, projectID, kind, id)
		return err
	})
	if malformedID(err) {
		return plan.ErrResource
	}
	return err
}

// ReplaceAssignee gives every task of the project assigned to one resource
// to another: at the first one's share, added to the other's on a task
// they are both on (never past the most a task allows). The first resource
// is then forgotten.
func (r *PlanRepository) ReplaceAssignee(projectID string, from, to plan.Assignment) error {
	err := withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO plan_assignments (work_item_id, project_id, assignee_type, assignee_id, units)
			SELECT work_item_id, project_id, $4, $5, units FROM plan_assignments
			WHERE project_id = $1 AND assignee_type = $2 AND assignee_id = $3
			ON CONFLICT (work_item_id, assignee_type, assignee_id) DO UPDATE
			SET units = LEAST(plan_assignments.units + EXCLUDED.units, $6)`,
			projectID, from.Type, from.ID, to.Type, to.ID, plan.MaxUnits); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM plan_assignments WHERE project_id = $1 AND assignee_type = $2 AND assignee_id = $3`, projectID, from.Type, from.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM plan_resources WHERE project_id = $1 AND assignee_type = $2 AND assignee_id = $3`, projectID, from.Type, from.ID)
		return err
	})
	if malformedID(err) {
		return plan.ErrResource
	}
	return err
}
