package workitems

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// Board columns.
const (
	ColumnBacklog    = "backlog"
	ColumnTodo       = "todo"
	ColumnInProgress = "in-progress"
	ColumnReview     = "review"
	ColumnDone       = "done"
)

// Assignee types.
const (
	AssigneeUser  = "user"
	AssigneeAgent = "agent"
	AssigneeTeam  = "team"
)

// Activity kinds.
const (
	KindComment     = "comment"
	KindMoved       = "moved"
	KindAssigned    = "assigned"
	KindRunStarted  = "run-started"
	KindRunFinished = "run-finished"
	KindRunFailed   = "run-failed"
)

// Error definitions
var (
	ErrNotFound      = errors.New("work item not found")
	ErrInvalidColumn = errors.New("invalid board column")
)

// WorkItem is a kanban card on the project board. The board column is stored
// in the board_column DB column but serialized as "column".
type WorkItem struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Column       string     `json:"column"`
	SortOrder    int        `json:"sort_order"`
	AssigneeType string     `json:"assignee_type"`
	AssigneeID   *string    `json:"assignee_id,omitempty"`
	AgentRunID   *string    `json:"agent_run_id,omitempty"`
	ArtifactIDs  []string   `json:"artifact_ids"`
	DueDate      *time.Time `json:"due_date,omitempty"`
	CreatedBy    *string    `json:"created_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Activity is an audit entry on a work item.
type Activity struct {
	ID         string                 `json:"id"`
	WorkItemID string                 `json:"work_item_id"`
	Kind       string                 `json:"kind"`
	Actor      string                 `json:"actor"`
	Content    string                 `json:"content"`
	Payload    map[string]interface{} `json:"payload"`
	CreatedAt  time.Time              `json:"created_at"`
}

// CreateWorkItemRequest is the payload for creating a work item.
type CreateWorkItemRequest struct {
	ProjectID    string     `json:"project_id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Column       string     `json:"column"`
	AssigneeType string     `json:"assignee_type"`
	AssigneeID   *string    `json:"assignee_id,omitempty"`
	ArtifactIDs  []string   `json:"artifact_ids"`
	DueDate      *time.Time `json:"due_date,omitempty"`
}

// UpdateWorkItemRequest is the payload for updating a work item.
type UpdateWorkItemRequest struct {
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	AssigneeType string     `json:"assignee_type"`
	AssigneeID   *string    `json:"assignee_id,omitempty"`
	ArtifactIDs  []string   `json:"artifact_ids"`
	DueDate      *time.Time `json:"due_date,omitempty"`
}

// MoveRequest is the payload for moving a work item on the board.
type MoveRequest struct {
	Column    string `json:"column"`
	SortOrder int    `json:"sort_order"`
}

// ValidColumn reports whether the value is a known board column.
func ValidColumn(column string) bool {
	switch column {
	case ColumnBacklog, ColumnTodo, ColumnInProgress, ColumnReview, ColumnDone:
		return true
	}
	return false
}

// Repository defines persistence operations for work items.
type Repository interface {
	Save(item *WorkItem) error
	Update(item *WorkItem) error
	FindByID(id string) (*WorkItem, error)
	ListByProject(projectID string) ([]*WorkItem, error)
	Delete(id string) error
	SaveActivity(a *Activity) error
	ListActivity(workItemID string) ([]*Activity, error)
	MaxSortOrder(projectID, column string) (int, error)
}

// Service defines work item domain logic.
type Service interface {
	Create(req CreateWorkItemRequest, createdBy *string, actor string) (*WorkItem, error)
	Get(id string) (*WorkItem, error)
	GetWithActivity(id string) (*WorkItem, []*Activity, error)
	ListByProject(projectID string) ([]*WorkItem, error)
	Update(id string, req UpdateWorkItemRequest, actor string) (*WorkItem, error)
	Move(id string, req MoveRequest, actor string) (*WorkItem, error)
	Delete(id string) error
	AddComment(workItemID, content, actor string) (*Activity, error)
	RecordRunActivity(workItemID, kind, content, actor string, payload map[string]interface{}) error
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo Repository
	bus  events.Bus
}

// NewDefaultService creates a new work item service. bus may be nil.
func NewDefaultService(repo Repository, bus events.Bus) *DefaultService {
	return &DefaultService{repo: repo, bus: bus}
}

// Create adds a work item at the end of its column.
func (s *DefaultService) Create(req CreateWorkItemRequest, createdBy *string, actor string) (*WorkItem, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("title is required")
	}
	if req.ProjectID == "" {
		return nil, errors.New("project id is required")
	}

	column := req.Column
	if column == "" {
		column = ColumnBacklog
	}
	if !ValidColumn(column) {
		return nil, ErrInvalidColumn
	}

	assigneeType := req.AssigneeType
	if assigneeType == "" {
		assigneeType = AssigneeUser
	}

	artifactIDs := req.ArtifactIDs
	if artifactIDs == nil {
		artifactIDs = []string{}
	}

	maxOrder, err := s.repo.MaxSortOrder(req.ProjectID, column)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	item := &WorkItem{
		ID:           uuid.New().String(),
		ProjectID:    req.ProjectID,
		Title:        req.Title,
		Description:  req.Description,
		Column:       column,
		SortOrder:    maxOrder + 1,
		AssigneeType: assigneeType,
		AssigneeID:   req.AssigneeID,
		ArtifactIDs:  artifactIDs,
		DueDate:      req.DueDate,
		CreatedBy:    createdBy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.Save(item); err != nil {
		return nil, err
	}

	if item.AssigneeID != nil {
		s.recordActivity(item.ID, KindAssigned, fmt.Sprintf("assigned to %s:%s", item.AssigneeType, *item.AssigneeID), actor, map[string]interface{}{
			"assignee_type": item.AssigneeType,
			"assignee_id":   *item.AssigneeID,
		})
	}

	s.publish(events.WorkItemCreated, item, actor, map[string]interface{}{
		"column": item.Column,
		"title":  item.Title,
	})

	return item, nil
}

// Get retrieves a work item by ID.
func (s *DefaultService) Get(id string) (*WorkItem, error) {
	return s.repo.FindByID(id)
}

// GetWithActivity retrieves a work item and its activity feed.
func (s *DefaultService) GetWithActivity(id string) (*WorkItem, []*Activity, error) {
	item, err := s.repo.FindByID(id)
	if err != nil {
		return nil, nil, err
	}
	activities, err := s.repo.ListActivity(id)
	if err != nil {
		return nil, nil, err
	}
	return item, activities, nil
}

// ListByProject retrieves all work items for a project.
func (s *DefaultService) ListByProject(projectID string) ([]*WorkItem, error) {
	return s.repo.ListByProject(projectID)
}

// Update replaces the editable fields of a work item.
func (s *DefaultService) Update(id string, req UpdateWorkItemRequest, actor string) (*WorkItem, error) {
	item, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("title is required")
	}

	item.Title = req.Title
	item.Description = req.Description
	if req.AssigneeType != "" {
		item.AssigneeType = req.AssigneeType
	}
	item.AssigneeID = req.AssigneeID
	if req.ArtifactIDs != nil {
		item.ArtifactIDs = req.ArtifactIDs
	}
	item.DueDate = req.DueDate
	item.UpdatedAt = time.Now()

	if err := s.repo.Update(item); err != nil {
		return nil, err
	}

	s.publish(events.WorkItemUpdated, item, actor, map[string]interface{}{
		"title": item.Title,
	})

	return item, nil
}

// Move relocates a work item to a column and position.
func (s *DefaultService) Move(id string, req MoveRequest, actor string) (*WorkItem, error) {
	if !ValidColumn(req.Column) {
		return nil, ErrInvalidColumn
	}

	item, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	item.Column = req.Column
	item.SortOrder = req.SortOrder
	item.UpdatedAt = time.Now()

	if err := s.repo.Update(item); err != nil {
		return nil, err
	}

	s.recordActivity(item.ID, KindMoved, fmt.Sprintf("moved to %s", req.Column), actor, map[string]interface{}{
		"column":     req.Column,
		"sort_order": req.SortOrder,
	})

	payload := map[string]interface{}{
		"column":        item.Column,
		"assignee_type": item.AssigneeType,
	}
	if item.AssigneeID != nil {
		payload["assignee_id"] = *item.AssigneeID
	}
	s.publish(events.WorkItemMoved, item, actor, payload)

	return item, nil
}

// Delete removes a work item and its activity.
func (s *DefaultService) Delete(id string) error {
	return s.repo.Delete(id)
}

// AddComment records a comment activity on a work item.
func (s *DefaultService) AddComment(workItemID, content, actor string) (*Activity, error) {
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("comment content is required")
	}
	if _, err := s.repo.FindByID(workItemID); err != nil {
		return nil, err
	}

	activity := newActivity(workItemID, KindComment, content, actor, nil)
	if err := s.repo.SaveActivity(activity); err != nil {
		return nil, err
	}
	return activity, nil
}

// RecordRunActivity records agent-run lifecycle activity on a work item.
func (s *DefaultService) RecordRunActivity(workItemID, kind, content, actor string, payload map[string]interface{}) error {
	return s.repo.SaveActivity(newActivity(workItemID, kind, content, actor, payload))
}

// newActivity builds an activity entry with defaults applied.
func newActivity(workItemID, kind, content, actor string, payload map[string]interface{}) *Activity {
	if actor == "" {
		actor = events.ActorSystem
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return &Activity{
		ID:         uuid.New().String(),
		WorkItemID: workItemID,
		Kind:       kind,
		Actor:      actor,
		Content:    content,
		Payload:    payload,
		CreatedAt:  time.Now(),
	}
}

// recordActivity persists an activity entry, ignoring persistence errors.
func (s *DefaultService) recordActivity(workItemID, kind, content, actor string, payload map[string]interface{}) {
	if err := s.repo.SaveActivity(newActivity(workItemID, kind, content, actor, payload)); err != nil {
		fmt.Printf("Warning: failed to record work item activity: %v\n", err)
	}
}

// publish emits an event if a bus is configured.
func (s *DefaultService) publish(eventType string, item *WorkItem, actor string, payload map[string]interface{}) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.New(eventType, item.ProjectID, item.ID, actor, payload))
}
