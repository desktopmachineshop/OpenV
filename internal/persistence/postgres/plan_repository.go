package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/plan"
)

// PlanRepository implements plan.Repository: what the plan adds to the
// board (migration 0075).
type PlanRepository struct {
	db *sql.DB
}

// NewPlanRepository creates the repository.
func NewPlanRepository(db *sql.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

// ListSchedules lists the project's task schedules.
func (r *PlanRepository) ListSchedules(projectID string) ([]*plan.TaskSchedule, error) {
	rows, err := r.db.Query(`
		SELECT work_item_id, project_id, to_char(start_date, 'YYYY-MM-DD'), parent_id, duration_days, effort_days
		FROM plan_tasks WHERE project_id = $1 ORDER BY work_item_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*plan.TaskSchedule{}
	for rows.Next() {
		s := &plan.TaskSchedule{}
		var start, parent sql.NullString
		var duration sql.NullInt64
		var effort sql.NullFloat64
		if err := rows.Scan(&s.WorkItemID, &s.ProjectID, &start, &parent, &duration, &effort); err != nil {
			return nil, err
		}
		if duration.Valid {
			d := int(duration.Int64)
			s.DurationDays = &d
		}
		if effort.Valid {
			s.EffortDays = &effort.Float64
		}
		if start.Valid {
			s.StartDate = &start.String
		}
		if parent.Valid {
			s.ParentID = &parent.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveSchedule inserts or replaces a task's schedule.
func (r *PlanRepository) SaveSchedule(s *plan.TaskSchedule) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_tasks (work_item_id, project_id, start_date, parent_id, duration_days, effort_days, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (work_item_id) DO UPDATE
		SET start_date = EXCLUDED.start_date, parent_id = EXCLUDED.parent_id,
			duration_days = EXCLUDED.duration_days, effort_days = EXCLUDED.effort_days, updated_at = NOW()`,
		s.WorkItemID, s.ProjectID, nullableString(s.StartDate), nullableString(s.ParentID), optInt(s.DurationDays), optFloat(s.EffortDays))
	return err
}

// ListDependencies lists the project's dependencies.
func (r *PlanRepository) ListDependencies(projectID string) ([]plan.Dependency, error) {
	rows, err := r.db.Query(`
		SELECT work_item_id, depends_on_id, kind FROM plan_dependencies
		WHERE project_id = $1 ORDER BY work_item_id, depends_on_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.Dependency{}
	for rows.Next() {
		var d plan.Dependency
		if err := rows.Scan(&d.WorkItemID, &d.DependsOnID, &d.Type); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDependencies replaces a task's relationships.
func (r *PlanRepository) SetDependencies(projectID, workItemID string, relationships []plan.Dependency) error {
	return withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM plan_dependencies WHERE work_item_id = $1`, workItemID); err != nil {
			return err
		}
		for _, rel := range relationships {
			kind := rel.Type
			if kind == "" {
				kind = plan.RelFinishToStart
			}
			if _, err := tx.Exec(`
				INSERT INTO plan_dependencies (work_item_id, depends_on_id, project_id, kind)
				VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, workItemID, rel.DependsOnID, projectID, kind); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListTodoTasks maps each attached to-do of the project to its task.
func (r *PlanRepository) ListTodoTasks(projectID string) (map[string]string, error) {
	rows, err := r.db.Query(`SELECT todo_id, work_item_id FROM plan_task_todos WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var todoID, workItemID string
		if err := rows.Scan(&todoID, &workItemID); err != nil {
			return nil, err
		}
		out[todoID] = workItemID
	}
	return out, rows.Err()
}

// AttachTodo puts a to-do on a task, moving it from any other.
func (r *PlanRepository) AttachTodo(projectID, todoID, workItemID string) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_task_todos (todo_id, work_item_id, project_id) VALUES ($1, $2, $3)
		ON CONFLICT (todo_id) DO UPDATE SET work_item_id = EXCLUDED.work_item_id`, todoID, workItemID, projectID)
	return err
}

const planMilestoneColumns = `id, project_id, release_id, name, description, to_char(target_date, 'YYYY-MM-DD'), criteria, created_at, updated_at`

func scanPlanMilestone(s rowScanner) (*plan.Milestone, error) {
	m := &plan.Milestone{}
	var release, date sql.NullString
	var criteria []byte
	if err := s.Scan(&m.ID, &m.ProjectID, &release, &m.Name, &m.Description, &date, &criteria, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	if release.Valid {
		m.ReleaseID = &release.String
	}
	if date.Valid {
		m.TargetDate = &date.String
	}
	// Criteria of a shape this build does not read count as none.
	if err := json.Unmarshal(criteria, &m.Criteria); err != nil || m.Criteria == nil {
		m.Criteria = []plan.Criterion{}
	}
	m.CreatedAt = m.CreatedAt.UTC()
	m.UpdatedAt = m.UpdatedAt.UTC()
	return m, nil
}

// ListMilestones lists the project's stored milestones, oldest first.
func (r *PlanRepository) ListMilestones(projectID string) ([]*plan.Milestone, error) {
	rows, err := r.db.Query(`SELECT `+planMilestoneColumns+` FROM plan_milestones WHERE project_id = $1 ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*plan.Milestone{}
	for rows.Next() {
		m, err := scanPlanMilestone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMilestone returns plan.ErrNotFound when no milestone has the id.
func (r *PlanRepository) GetMilestone(id string) (*plan.Milestone, error) {
	m, err := scanPlanMilestone(r.db.QueryRow(`SELECT `+planMilestoneColumns+` FROM plan_milestones WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) || malformedID(err) {
		return nil, plan.ErrNotFound
	}
	return m, err
}

func criteriaJSON(m *plan.Milestone) ([]byte, error) {
	c := m.Criteria
	if c == nil {
		c = []plan.Criterion{}
	}
	return json.Marshal(c)
}

// CreateMilestone inserts a milestone.
func (r *PlanRepository) CreateMilestone(m *plan.Milestone) error {
	criteria, err := criteriaJSON(m)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO plan_milestones (id, project_id, release_id, name, description, target_date, criteria, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		m.ID, m.ProjectID, nullableString(m.ReleaseID), m.Name, m.Description, nullableString(m.TargetDate),
		criteria, m.CreatedAt.UTC(), m.UpdatedAt.UTC())
	return err
}

// UpdateMilestone writes a milestone's editable fields.
func (r *PlanRepository) UpdateMilestone(m *plan.Milestone) error {
	criteria, err := criteriaJSON(m)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(`
		UPDATE plan_milestones SET name = $2, description = $3, target_date = $4, criteria = $5, updated_at = $6
		WHERE id = $1`,
		m.ID, m.Name, m.Description, nullableString(m.TargetDate), criteria, m.UpdatedAt.UTC())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return plan.ErrNotFound
	}
	return nil
}

// DeleteMilestone removes a milestone.
func (r *PlanRepository) DeleteMilestone(id string) error {
	res, err := r.db.Exec(`DELETE FROM plan_milestones WHERE id = $1`, id)
	if malformedID(err) {
		return plan.ErrNotFound
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return plan.ErrNotFound
	}
	return nil
}

// optInt and optFloat pass a NULL for a value that is not set.
func optInt(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func optFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
