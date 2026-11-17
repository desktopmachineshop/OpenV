package postgres

import "database/sql"

// 0071: to-dos (internal/domain/todos, issue #731). A to-do is one paragraph
// to tick off, kept apart from the board's work items: a project's, which
// its members share, or a personal one with no project, which only its
// writer sees. The note it was raised from, and its artifact, are kept as
// back-references: the note goes blank when it is deleted (artifacts are
// versioned, so the artifact id is a plain column), and so does the board
// task a promoted to-do became. Rows go with their workspace and their
// project through the cascading foreign keys (Q17). It runs once, inside the
// transaction that records it, so no CREATE INDEX CONCURRENTLY. Once it has
// shipped it is frozen (I16).
func m0071Todos(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS todos (
				id UUID PRIMARY KEY,
				org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
				project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
				text VARCHAR(4000) NOT NULL,
				done BOOLEAN NOT NULL DEFAULT FALSE,
				done_at TIMESTAMP,
				done_note VARCHAR(32) NOT NULL DEFAULT '',
				assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
				due_date TIMESTAMPTZ,
				source_artifact_id UUID,
				source_chatter_id UUID REFERENCES chatter(id) ON DELETE SET NULL,
				promoted_work_item_id UUID REFERENCES work_items(id) ON DELETE SET NULL,
				created_by UUID REFERENCES users(id) ON DELETE SET NULL,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_todos_project ON todos(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_todos_org_assignee ON todos(org_id, assignee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_todos_org_created_by ON todos(org_id, created_by) WHERE project_id IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_todos_source_chatter ON todos(source_chatter_id)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
