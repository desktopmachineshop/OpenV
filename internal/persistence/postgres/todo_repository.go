package postgres

import (
	"database/sql"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/todos"
)

// todoColumns are the columns the to-do repository reads whole, in the order
// scanTodo reads them (refactor plan X13).
const todoColumns = `id, org_id, project_id, text, done, done_at, done_note, assignee_id, due_date, source_artifact_id, source_chatter_id, promoted_work_item_id, created_by, created_at, updated_at`

// todoOrder lists open to-dos first, soonest due first with the undated
// after them, then the done ones, most recently done first; created_at and
// id break the ties, so a list reads in one order every time.
const todoOrder = `ORDER BY done ASC, CASE WHEN done THEN NULL ELSE due_date END ASC NULLS LAST, done_at DESC NULLS LAST, created_at ASC, id ASC`

// TodoRepository implements todos.Repository.
type TodoRepository struct {
	db *sql.DB
}

// NewTodoRepository creates a to-do repository.
func NewTodoRepository(db *sql.DB) *TodoRepository {
	return &TodoRepository{db: db}
}

// Save inserts a new to-do.
func (r *TodoRepository) Save(t *todos.Todo) error {
	_, err := r.db.Exec(`
		INSERT INTO todos (`+todoColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`,
		t.ID, t.OrgID, t.ProjectID, t.Text, t.Done, t.DoneAt, t.DoneNote, t.AssigneeID, t.DueDate,
		t.SourceArtifactID, t.SourceChatterID, t.PromotedWorkItemID, t.CreatedBy, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

// Update writes a to-do's changeable fields. Where it lives, where it was
// raised and who wrote it never change.
func (r *TodoRepository) Update(t *todos.Todo) error {
	res, err := r.db.Exec(`
		UPDATE todos
		SET text = $2, done = $3, done_at = $4, done_note = $5, assignee_id = $6, due_date = $7, promoted_work_item_id = $8, updated_at = $9
		WHERE id = $1
	`, t.ID, t.Text, t.Done, t.DoneAt, t.DoneNote, t.AssigneeID, t.DueDate, t.PromotedWorkItemID, t.UpdatedAt)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return todos.ErrNotFound
	}
	return nil
}

// scanTodo scans one row of todoColumns.
func scanTodo(row rowScanner) (*todos.Todo, error) {
	t := &todos.Todo{}
	if err := row.Scan(
		&t.ID, &t.OrgID, &t.ProjectID, &t.Text, &t.Done, &t.DoneAt, &t.DoneNote, &t.AssigneeID, &t.DueDate,
		&t.SourceArtifactID, &t.SourceChatterID, &t.PromotedWorkItemID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	t.DueDate = inUTC(t.DueDate)
	return t, nil
}

// FindByID returns one to-do, or todos.ErrNotFound.
func (r *TodoRepository) FindByID(id string) (*todos.Todo, error) {
	t, err := scanTodo(r.db.QueryRow(`SELECT `+todoColumns+` FROM todos WHERE id = $1`, id))
	if noRow(err) || malformedID(err) {
		return nil, todos.ErrNotFound
	}
	return t, err
}

// ListByProject lists a project's to-dos in todoOrder.
func (r *TodoRepository) ListByProject(projectID string) ([]*todos.Todo, error) {
	return r.list(`SELECT `+todoColumns+` FROM todos WHERE project_id = $1 `+todoOrder, projectID)
}

// ListForUser lists, in one workspace, the to-dos assigned to a person and
// their personal ones, in todoOrder.
func (r *TodoRepository) ListForUser(orgID, userID string) ([]*todos.Todo, error) {
	return r.list(`
		SELECT `+todoColumns+` FROM todos
		WHERE org_id = $1 AND (assignee_id = $2 OR (project_id IS NULL AND created_by = $2))
		`+todoOrder, orgID, userID)
}

// ListBySourceChatterIDs lists the to-dos raised from the given notes,
// oldest first.
func (r *TodoRepository) ListBySourceChatterIDs(chatterIDs []string) ([]*todos.Todo, error) {
	if chatterIDs = uuidsOnly(chatterIDs); len(chatterIDs) == 0 {
		return nil, nil
	}
	return r.list(`SELECT `+todoColumns+` FROM todos WHERE source_chatter_id = ANY($1) ORDER BY created_at ASC, id ASC`, pq.Array(chatterIDs))
}

func (r *TodoRepository) list(query string, args ...interface{}) ([]*todos.Todo, error) {
	rows, err := r.db.Query(query, args...)
	if malformedID(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*todos.Todo
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Delete removes a to-do.
func (r *TodoRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM todos WHERE id = $1`, id)
	return err
}
