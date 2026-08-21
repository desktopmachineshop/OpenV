package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// WorkItemRepository implements the workitems.Repository interface.
// The board column lives in the board_column SQL column.
type WorkItemRepository struct {
	db *sql.DB
}

// NewWorkItemRepository creates a new work item repository
func NewWorkItemRepository(db *sql.DB) *WorkItemRepository {
	return &WorkItemRepository{db: db}
}

// Save inserts a new work item
func (r *WorkItemRepository) Save(item *workitems.WorkItem) error {
	artifactIDs := item.ArtifactIDs
	if artifactIDs == nil {
		artifactIDs = []string{}
	}
	artifactIDsJSON, err := json.Marshal(artifactIDs)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO work_items (id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	_, err = r.db.Exec(
		query,
		item.ID,
		item.ProjectID,
		item.Title,
		item.Description,
		item.Column,
		item.SortOrder,
		item.AssigneeType,
		item.AssigneeID,
		item.AgentRunID,
		artifactIDsJSON,
		item.DueDate,
		item.CreatedBy,
		item.CreatedAt,
		item.UpdatedAt,
	)

	return err
}

// Update updates an existing work item
func (r *WorkItemRepository) Update(item *workitems.WorkItem) error {
	artifactIDs := item.ArtifactIDs
	if artifactIDs == nil {
		artifactIDs = []string{}
	}
	artifactIDsJSON, err := json.Marshal(artifactIDs)
	if err != nil {
		return err
	}

	query := `
		UPDATE work_items
		SET title = $2, description = $3, board_column = $4, sort_order = $5, assignee_type = $6, assignee_id = $7, agent_run_id = $8, artifact_ids = $9, due_date = $10, updated_at = $11
		WHERE id = $1
	`

	_, err = r.db.Exec(
		query,
		item.ID,
		item.Title,
		item.Description,
		item.Column,
		item.SortOrder,
		item.AssigneeType,
		item.AssigneeID,
		item.AgentRunID,
		artifactIDsJSON,
		item.DueDate,
		item.UpdatedAt,
	)

	return err
}

// scanWorkItem scans a single work item row
func scanWorkItem(scan func(dest ...interface{}) error) (*workitems.WorkItem, error) {
	item := &workitems.WorkItem{}
	var artifactIDsJSON []byte

	err := scan(
		&item.ID,
		&item.ProjectID,
		&item.Title,
		&item.Description,
		&item.Column,
		&item.SortOrder,
		&item.AssigneeType,
		&item.AssigneeID,
		&item.AgentRunID,
		&artifactIDsJSON,
		&item.DueDate,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	item.ArtifactIDs = []string{}
	if len(artifactIDsJSON) > 0 {
		if err := json.Unmarshal(artifactIDsJSON, &item.ArtifactIDs); err != nil {
			return nil, err
		}
	}

	return item, nil
}

// FindByID retrieves a work item by ID
func (r *WorkItemRepository) FindByID(id string) (*workitems.WorkItem, error) {
	query := `
		SELECT id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, created_by, created_at, updated_at
		FROM work_items
		WHERE id = $1
	`

	row := r.db.QueryRow(query, id)
	item, err := scanWorkItem(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, workitems.ErrNotFound
		}
		return nil, err
	}

	return item, nil
}

// ListByProject retrieves all work items for a project ordered by board position
func (r *WorkItemRepository) ListByProject(projectID string) ([]*workitems.WorkItem, error) {
	query := `
		SELECT id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, created_by, created_at, updated_at
		FROM work_items
		WHERE project_id = $1
		ORDER BY board_column ASC, sort_order ASC, created_at ASC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*workitems.WorkItem
	for rows.Next() {
		item, err := scanWorkItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// Delete removes a work item; activity cascades via foreign key
func (r *WorkItemRepository) Delete(id string) error {
	query := `DELETE FROM work_items WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

// SaveActivity inserts a work item activity entry
func (r *WorkItemRepository) SaveActivity(a *workitems.Activity) error {
	payload := a.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO work_item_activity (id, work_item_id, kind, actor, content, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = r.db.Exec(
		query,
		a.ID,
		a.WorkItemID,
		a.Kind,
		a.Actor,
		a.Content,
		payloadJSON,
		a.CreatedAt,
	)

	return err
}

// ListActivity retrieves the activity feed for a work item, oldest first
func (r *WorkItemRepository) ListActivity(workItemID string) ([]*workitems.Activity, error) {
	query := `
		SELECT id, work_item_id, kind, actor, content, payload, created_at
		FROM work_item_activity
		WHERE work_item_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(query, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []*workitems.Activity
	for rows.Next() {
		activity := new(workitems.Activity)
		var payloadJSON []byte

		err := rows.Scan(
			&activity.ID,
			&activity.WorkItemID,
			&activity.Kind,
			&activity.Actor,
			&activity.Content,
			&payloadJSON,
			&activity.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		activity.Payload = map[string]interface{}{}
		if len(payloadJSON) > 0 {
			if err := json.Unmarshal(payloadJSON, &activity.Payload); err != nil {
				return nil, err
			}
		}

		activities = append(activities, activity)
	}

	return activities, rows.Err()
}

// MaxSortOrder returns the highest sort_order in a column, or -1 when empty
func (r *WorkItemRepository) MaxSortOrder(projectID, column string) (int, error) {
	query := `
		SELECT COALESCE(MAX(sort_order), -1)
		FROM work_items
		WHERE project_id = $1 AND board_column = $2
	`

	var max int
	err := r.db.QueryRow(query, projectID, column).Scan(&max)
	return max, err
}
