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
		SELECT work_item_id, project_id, to_char(start_date, 'YYYY-MM-DD'), parent_id, duration_days, effort_days, milestone_key,
			elapsed, to_char(deadline, 'YYYY-MM-DD'), mode, constraint_type, to_char(constraint_date, 'YYYY-MM-DD'),
			fixed_cost, effort_driven, to_char(actual_start, 'YYYY-MM-DD'), to_char(actual_finish, 'YYYY-MM-DD'),
			percent_complete, actual_cost, sort_order
		FROM plan_tasks WHERE project_id = $1 ORDER BY work_item_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*plan.TaskSchedule{}
	for rows.Next() {
		s := &plan.TaskSchedule{}
		var start, parent, milestone, deadline, constraint, actualStart, actualFinish sql.NullString
		var percent, order sql.NullInt64
		var actualCost sql.NullFloat64
		var duration sql.NullInt64
		var effort sql.NullFloat64
		if err := rows.Scan(&s.WorkItemID, &s.ProjectID, &start, &parent, &duration, &effort, &milestone, &s.Elapsed, &deadline, &s.Mode, &s.ConstraintType, &constraint, &s.FixedCost, &s.EffortDriven,
			&actualStart, &actualFinish, &percent, &actualCost, &order); err != nil {
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
		if deadline.Valid {
			s.Deadline = &deadline.String
		}
		if constraint.Valid {
			s.ConstraintDate = &constraint.String
		}
		if actualStart.Valid {
			s.ActualStart = &actualStart.String
		}
		if actualFinish.Valid {
			s.ActualFinish = &actualFinish.String
		}
		if percent.Valid {
			n := int(percent.Int64)
			s.PercentComplete = &n
		}
		if actualCost.Valid {
			s.ActualCost = &actualCost.Float64
		}
		if order.Valid {
			n := int(order.Int64)
			s.SortOrder = &n
		}
		if milestone.Valid && milestone.String != "" {
			s.MilestoneKey = &milestone.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveSchedule inserts or replaces a task's schedule.
func (r *PlanRepository) SaveSchedule(s *plan.TaskSchedule) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_tasks (work_item_id, project_id, start_date, parent_id, duration_days, effort_days, milestone_key,
			elapsed, deadline, mode, constraint_type, constraint_date, fixed_cost, effort_driven,
			actual_start, actual_finish, percent_complete, actual_cost, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, NOW())
		ON CONFLICT (work_item_id) DO UPDATE
		SET start_date = EXCLUDED.start_date, parent_id = EXCLUDED.parent_id,
			duration_days = EXCLUDED.duration_days, effort_days = EXCLUDED.effort_days,
			milestone_key = EXCLUDED.milestone_key, elapsed = EXCLUDED.elapsed, deadline = EXCLUDED.deadline,
			mode = EXCLUDED.mode, constraint_type = EXCLUDED.constraint_type, constraint_date = EXCLUDED.constraint_date,
			fixed_cost = EXCLUDED.fixed_cost, effort_driven = EXCLUDED.effort_driven,
			actual_start = EXCLUDED.actual_start, actual_finish = EXCLUDED.actual_finish,
			percent_complete = EXCLUDED.percent_complete, actual_cost = EXCLUDED.actual_cost,
			updated_at = NOW()`,
		s.WorkItemID, s.ProjectID, nullableString(s.StartDate), nullableString(s.ParentID), optInt(s.DurationDays), optFloat(s.EffortDays),
		nullableString(s.MilestoneKey), s.Elapsed, nullableString(s.Deadline), s.Mode, constraintType(s.ConstraintType),
		nullableString(s.ConstraintDate), s.FixedCost, s.EffortDriven,
		nullableString(s.ActualStart), nullableString(s.ActualFinish), optInt(s.PercentComplete), optFloat(s.ActualCost))
	return err
}

// ListExternalLinks lists the project's links to tasks of other projects:
// those that drive its tasks, then those its tasks drive, each naming the
// other task, its project and its dates: its own, or those its sub-tasks
// span.
func (r *PlanRepository) ListExternalLinks(projectID string) ([]plan.ExternalLink, error) {
	rows, err := r.db.Query(`
		SELECT mine.id, other.id, other.project_id, p.name, other.title,
			to_char(span.first, 'YYYY-MM-DD'), to_char(span.last, 'YYYY-MM-DD'),
			other.board_column = 'done', l.kind, l.lag_days, l.lag_unit, mine.id = l.work_item_id
		FROM plan_external_links l
		JOIN work_items mine ON mine.id IN (l.work_item_id, l.depends_on_id) AND mine.project_id = $1
		JOIN work_items other ON other.id IN (l.work_item_id, l.depends_on_id) AND other.id <> mine.id
		JOIN projects p ON p.id = other.project_id
		LEFT JOIN LATERAL (
			WITH RECURSIVE sub(id) AS (
				SELECT other.id
				UNION ALL
				SELECT t.work_item_id FROM plan_tasks t JOIN sub ON t.parent_id = sub.id
			)
			SELECT MIN(COALESCE(t.start_date, w.due_date::date)) AS first, MAX(w.due_date::date) AS last
			FROM sub JOIN work_items w ON w.id = sub.id LEFT JOIN plan_tasks t ON t.work_item_id = w.id
		) span ON true
		WHERE other.project_id <> $1
		ORDER BY mine.id, other.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.ExternalLink{}
	for rows.Next() {
		var l plan.ExternalLink
		var start, end sql.NullString
		var driven bool
		if err := rows.Scan(&l.LocalID, &l.TaskID, &l.ProjectID, &l.ProjectName, &l.Title, &start, &end, &l.Done, &l.Type, &l.LagDays, &l.LagUnit, &driven); err != nil {
			return nil, err
		}
		if start.Valid {
			l.Start = &start.String
		}
		if end.Valid {
			l.End = &end.String
		}
		l.Direction = plan.ExternalDrives
		if driven {
			l.Direction = plan.ExternalDrivenBy
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SaveExternalLink makes taskID driven by dependsOnID, a task of another
// project, replacing whatever link the pair had either way round.
func (r *PlanRepository) SaveExternalLink(taskID, dependsOnID, kind string, lagDays int, lagUnit string) error {
	return withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM plan_external_links WHERE work_item_id = $2 AND depends_on_id = $1`, taskID, dependsOnID); err != nil {
			return err
		}
		_, err := tx.Exec(`
			INSERT INTO plan_external_links (work_item_id, depends_on_id, kind, lag_days, lag_unit) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (work_item_id, depends_on_id) DO UPDATE SET kind = EXCLUDED.kind, lag_days = EXCLUDED.lag_days, lag_unit = EXCLUDED.lag_unit`,
			taskID, dependsOnID, kind, lagDays, lagUnit)
		return err
	})
}

// DeleteExternalLink removes the link between two tasks of different
// projects, whichever way round it runs.
func (r *PlanRepository) DeleteExternalLink(taskID, otherID string) error {
	res, err := r.db.Exec(`
		DELETE FROM plan_external_links
		WHERE (work_item_id = $1 AND depends_on_id = $2) OR (work_item_id = $2 AND depends_on_id = $1)`, taskID, otherID)
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

// daysOf is the ISO weekdays a stored bit set names, and bitsOf the bit set
// of a list of them.
func daysOf(bits int) []int {
	out := []int{}
	for d := 1; d <= 7; d++ {
		if bits&(1<<(d-1)) != 0 {
			out = append(out, d)
		}
	}
	return out
}

func bitsOf(days []int) int {
	bits := 0
	for _, d := range days {
		if d >= 1 && d <= 7 {
			bits |= 1 << (d - 1)
		}
	}
	return bits
}

const timeOffColumns = `id, assignee_type, assignee_id, to_char(start_date, 'YYYY-MM-DD'), to_char(end_date, 'YYYY-MM-DD'), name`

func (r *PlanRepository) timeOff(query, id string) ([]plan.TimeOff, error) {
	rows, err := r.db.Query(query, id)
	if malformedID(err) {
		return []plan.TimeOff{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.TimeOff{}
	for rows.Next() {
		var t plan.TimeOff
		if err := rows.Scan(&t.ID, &t.Type, &t.AssigneeID, &t.Start, &t.End, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// WorkspaceCalendar answers a workspace's working days, nil until it sets
// them, and its time off in date order.
func (r *PlanRepository) WorkspaceCalendar(orgID string) ([]int, []plan.TimeOff, error) {
	var days []int
	var bits int
	switch err := r.db.QueryRow(`SELECT working_days FROM plan_org_calendar WHERE org_id = $1`, orgID).Scan(&bits); {
	case err == nil:
		days = daysOf(bits)
	case !errors.Is(err, sql.ErrNoRows) && !malformedID(err):
		return nil, nil, err
	}
	off, err := r.timeOff(`SELECT `+timeOffColumns+` FROM plan_time_off WHERE org_id = $1 ORDER BY start_date, id`, orgID)
	return days, off, err
}

// SaveWorkspaceWorkingDays sets the days every plan of the workspace works.
func (r *PlanRepository) SaveWorkspaceWorkingDays(orgID string, days []int) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_org_calendar (org_id, working_days, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (org_id) DO UPDATE SET working_days = EXCLUDED.working_days, updated_at = NOW()`, orgID, bitsOf(days))
	return err
}

// SaveProjectWorkingDays sets the working days of the workspace the
// project is in.
func (r *PlanRepository) SaveProjectWorkingDays(projectID string, days []int) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_org_calendar (org_id, working_days, updated_at)
		SELECT org_id, $2, NOW() FROM projects WHERE id = $1 AND org_id IS NOT NULL
		ON CONFLICT (org_id) DO UPDATE SET working_days = EXCLUDED.working_days, updated_at = NOW()`, projectID, bitsOf(days))
	return err
}

// ListTimeOff lists the time off of the workspace the project is in.
func (r *PlanRepository) ListTimeOff(projectID string) ([]plan.TimeOff, error) {
	return r.timeOff(`
		SELECT `+timeOffColumns+` FROM plan_time_off
		WHERE org_id = (SELECT org_id FROM projects WHERE id = $1) ORDER BY start_date, id`, projectID)
}

// AddTimeOff stores days a person or an agent is away.
func (r *PlanRepository) AddTimeOff(orgID string, t *plan.TimeOff) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_time_off (id, org_id, assignee_type, assignee_id, start_date, end_date, name) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, orgID, t.Type, t.AssigneeID, t.Start, t.End, t.Name)
	if malformedID(err) {
		return plan.ErrResource
	}
	return err
}

// DeleteTimeOff removes a time-off entry of the workspace.
func (r *PlanRepository) DeleteTimeOff(orgID, id string) error {
	res, err := r.db.Exec(`DELETE FROM plan_time_off WHERE id = $1 AND org_id = $2`, id, orgID)
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

// SetOrder numbers tasks among their siblings; 0 clears a task's place. A
// task with no row of the plan yet gets one.
func (r *PlanRepository) SetOrder(projectID string, order map[string]int) error {
	return withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		for id, n := range order {
			if _, err := tx.Exec(`
				INSERT INTO plan_tasks (work_item_id, project_id, sort_order, updated_at) VALUES ($1, $2, $3, NOW())
				ON CONFLICT (work_item_id) DO UPDATE SET sort_order = EXCLUDED.sort_order`, id, projectID, sql.NullInt64{Int64: int64(n), Valid: n > 0}); err != nil {
				return err
			}
		}
		return nil
	})
}

// Settings returns the plan's calendar, critical threshold and scheduling
// settings. A project that never set any has the defaults, and its working
// days are nil (the caller's default, Monday to Friday).
func (r *PlanRepository) Settings(projectID string) (plan.Settings, error) {
	out := plan.Settings{Holidays: []plan.Holiday{}, Scheduling: plan.Scheduling{Mode: plan.ModeManual, ScheduleFrom: plan.FromStart}}
	var bits int
	var start, finish, status, baseline sql.NullString
	var budget sql.NullFloat64
	err := r.db.QueryRow(`
		SELECT working_days, critical_slack_days, scheduling_mode, to_char(project_start, 'YYYY-MM-DD'),
			to_char(project_finish, 'YYYY-MM-DD'), schedule_from, multiple_critical_paths, currency, budget,
			to_char(status_date, 'YYYY-MM-DD'), active_baseline_id
		FROM plan_settings WHERE project_id = $1`, projectID).
		Scan(&bits, &out.CriticalSlackDays, &out.Mode, &start, &finish, &out.ScheduleFrom, &out.MultipleCriticalPaths, &out.Currency, &budget, &status, &baseline)
	switch {
	case malformedID(err):
		return out, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return out, err
	case err == nil:
		out.WorkingDays = daysOf(bits)
		if start.Valid {
			out.ProjectStart = &start.String
		}
		if finish.Valid {
			out.ProjectFinish = &finish.String
		}
		if budget.Valid {
			out.Budget = &budget.Float64
		}
		if status.Valid {
			out.StatusDate = &status.String
		}
		if baseline.Valid {
			out.ActiveBaselineID = &baseline.String
		}
	}
	// The workspace's working days, once it has set them, are the plan's.
	var shared int
	switch err := r.db.QueryRow(`
		SELECT c.working_days FROM plan_org_calendar c JOIN projects p ON p.org_id = c.org_id WHERE p.id = $1`, projectID).Scan(&shared); {
	case err == nil:
		out.WorkingDays = daysOf(shared)
	case !errors.Is(err, sql.ErrNoRows) && !malformedID(err):
		return out, err
	}
	rows, err := r.db.Query(`SELECT to_char(day, 'YYYY-MM-DD'), name FROM plan_holidays WHERE project_id = $1 ORDER BY day`, projectID)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h plan.Holiday
		if err := rows.Scan(&h.Date, &h.Name); err != nil {
			return out, err
		}
		out.Holidays = append(out.Holidays, h)
	}
	return out, rows.Err()
}

// SaveSettings stores the plan's settings whole: the row, with a bit per
// ISO working weekday, and its holidays.
func (r *PlanRepository) SaveSettings(projectID string, s plan.Settings) error {
	bits := bitsOf(s.WorkingDays)
	return withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO plan_settings (project_id, working_days, critical_slack_days, scheduling_mode, project_start,
				project_finish, schedule_from, multiple_critical_paths, currency, budget, status_date, active_baseline_id, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
			ON CONFLICT (project_id) DO UPDATE SET working_days = EXCLUDED.working_days,
				critical_slack_days = EXCLUDED.critical_slack_days, scheduling_mode = EXCLUDED.scheduling_mode,
				project_start = EXCLUDED.project_start, project_finish = EXCLUDED.project_finish,
				schedule_from = EXCLUDED.schedule_from, multiple_critical_paths = EXCLUDED.multiple_critical_paths,
				currency = EXCLUDED.currency, budget = EXCLUDED.budget,
				status_date = EXCLUDED.status_date, active_baseline_id = EXCLUDED.active_baseline_id,
				updated_at = NOW()`,
			projectID, bits, s.CriticalSlackDays, s.Mode, nullableString(s.ProjectStart), nullableString(s.ProjectFinish),
			s.ScheduleFrom, s.MultipleCriticalPaths, s.Currency, optFloat(s.Budget),
			nullableString(s.StatusDate), nullableString(s.ActiveBaselineID)); err != nil {
			if malformedID(err) {
				return plan.ErrNotFound
			}
			return err
		}
		if _, err := tx.Exec(`DELETE FROM plan_holidays WHERE project_id = $1`, projectID); err != nil {
			return err
		}
		for _, h := range s.Holidays {
			if _, err := tx.Exec(`INSERT INTO plan_holidays (project_id, day, name) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				projectID, h.Date, h.Name); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListAssignments lists who each of the project's tasks is assigned to.
func (r *PlanRepository) ListAssignments(projectID string) ([]plan.Assignment, error) {
	rows, err := r.db.Query(`
		SELECT work_item_id, assignee_type, assignee_id, units FROM plan_assignments
		WHERE project_id = $1 ORDER BY work_item_id, assignee_type, assignee_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.Assignment{}
	for rows.Next() {
		var a plan.Assignment
		if err := rows.Scan(&a.WorkItemID, &a.Type, &a.ID, &a.Units); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAssignments replaces who a task is assigned to.
func (r *PlanRepository) SetAssignments(projectID, workItemID string, assignments []plan.Assignment) error {
	return withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM plan_assignments WHERE work_item_id = $1`, workItemID); err != nil {
			return err
		}
		for _, a := range assignments {
			if _, err := tx.Exec(`
				INSERT INTO plan_assignments (work_item_id, project_id, assignee_type, assignee_id, units)
				VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, workItemID, projectID, a.Type, a.ID, a.Units); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListResources lists the daily rate and capacity the project keeps for
// each person or agent that has one.
func (r *PlanRepository) ListResources(projectID string) ([]plan.Resource, error) {
	rows, err := r.db.Query(`
		SELECT assignee_type, assignee_id, daily_rate, capacity FROM plan_resources
		WHERE project_id = $1 ORDER BY assignee_type, assignee_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.Resource{}
	for rows.Next() {
		var res plan.Resource
		if err := rows.Scan(&res.Type, &res.ID, &res.DailyRate, &res.Capacity); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// SaveResource stores a person's or an agent's daily rate and capacity.
func (r *PlanRepository) SaveResource(projectID string, res plan.Resource) error {
	_, err := r.db.Exec(`
		INSERT INTO plan_resources (project_id, assignee_type, assignee_id, daily_rate, capacity) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (project_id, assignee_type, assignee_id) DO UPDATE
		SET daily_rate = EXCLUDED.daily_rate, capacity = EXCLUDED.capacity`, projectID, res.Type, res.ID, res.DailyRate, res.Capacity)
	if malformedID(err) {
		return plan.ErrResource
	}
	return err
}

// ListBaselines lists the project's baselines with their tasks, oldest
// first.
func (r *PlanRepository) ListBaselines(projectID string) ([]*plan.Baseline, error) {
	rows, err := r.db.Query(`SELECT id, project_id, name, tasks, created_at FROM plan_baselines WHERE project_id = $1 ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*plan.Baseline{}
	for rows.Next() {
		b := &plan.Baseline{}
		var tasks []byte
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Name, &tasks, &b.CreatedAt); err != nil {
			return nil, err
		}
		// Tasks of a shape this build does not read count as none.
		if err := json.Unmarshal(tasks, &b.Tasks); err != nil || b.Tasks == nil {
			b.Tasks = []plan.BaselineTask{}
		}
		b.CreatedAt = b.CreatedAt.UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}

// CreateBaseline inserts a baseline.
func (r *PlanRepository) CreateBaseline(b *plan.Baseline) error {
	tasks, err := json.Marshal(b.Tasks)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`INSERT INTO plan_baselines (id, project_id, name, tasks, created_at) VALUES ($1, $2, $3, $4, $5)`,
		b.ID, b.ProjectID, b.Name, tasks, b.CreatedAt.UTC())
	return err
}

// DeleteBaseline removes a baseline of the project.
func (r *PlanRepository) DeleteBaseline(projectID, id string) error {
	res, err := r.db.Exec(`DELETE FROM plan_baselines WHERE id = $1 AND project_id = $2`, id, projectID)
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

// ListDependencies lists the project's dependencies.
func (r *PlanRepository) ListDependencies(projectID string) ([]plan.Dependency, error) {
	rows, err := r.db.Query(`
		SELECT work_item_id, depends_on_id, kind, lag_days, lag_unit FROM plan_dependencies
		WHERE project_id = $1 ORDER BY work_item_id, depends_on_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []plan.Dependency{}
	for rows.Next() {
		var d plan.Dependency
		if err := rows.Scan(&d.WorkItemID, &d.DependsOnID, &d.Type, &d.LagDays, &d.LagUnit); err != nil {
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
			kind, unit := rel.Type, rel.LagUnit
			if kind == "" {
				kind = plan.RelFinishToStart
			}
			if unit == "" {
				unit = plan.LagWorkingDays
			}
			if _, err := tx.Exec(`
				INSERT INTO plan_dependencies (work_item_id, depends_on_id, project_id, kind, lag_days, lag_unit)
				VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, workItemID, rel.DependsOnID, projectID, kind, rel.LagDays, unit); err != nil {
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

// constraintType stores a task with no constraint as "as soon as possible".
func constraintType(t string) string {
	if t == "" {
		return plan.ConstraintASAP
	}
	return t
}
