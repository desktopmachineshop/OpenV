package postgres

import "database/sql"

// 0075: the project plan (internal/domain/plan), the Gantt over the board.
// A plan's tasks are the board's work items, so nothing here copies one:
// plan_tasks adds what a schedule needs and the board does not hold (a start
// date, and the parent task of a sub-task), plan_dependencies the
// finish-to-start links between tasks, and plan_task_todos which task a
// to-do belongs to, so ticking to-dos off moves the task's progress.
// plan_milestones holds the milestones a team adds and, for a target
// release's milestone (release_id), only its completion criteria: the
// release keeps its own name and date. Every row carries a NOT NULL
// project_id that cascades, so the rows go with their project and with a
// purged workspace (Q17). It runs once, inside the transaction that records
// it, so no CREATE INDEX CONCURRENTLY. Once it has shipped it is frozen
// (I16).
func m0075Plan(tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS plan_tasks (
			work_item_id UUID PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			start_date DATE,
			parent_id UUID REFERENCES work_items(id) ON DELETE SET NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_tasks_project ON plan_tasks(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_tasks_parent ON plan_tasks(parent_id) WHERE parent_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS plan_dependencies (
			work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			depends_on_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			PRIMARY KEY (work_item_id, depends_on_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_dependencies_project ON plan_dependencies(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_dependencies_depends_on ON plan_dependencies(depends_on_id)`,
		`CREATE TABLE IF NOT EXISTS plan_task_todos (
			todo_id UUID PRIMARY KEY REFERENCES todos(id) ON DELETE CASCADE,
			work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_task_todos_project ON plan_task_todos(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_task_todos_work_item ON plan_task_todos(work_item_id)`,
		`CREATE TABLE IF NOT EXISTS plan_milestones (
			id UUID PRIMARY KEY,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			release_id UUID REFERENCES target_releases(id) ON DELETE CASCADE,
			name VARCHAR(200) NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			target_date DATE,
			criteria JSONB NOT NULL DEFAULT '[]',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_milestones_project ON plan_milestones(project_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_milestones_release ON plan_milestones(release_id) WHERE release_id IS NOT NULL`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
