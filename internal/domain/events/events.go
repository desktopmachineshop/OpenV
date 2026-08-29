package events

import (
	"time"

	"github.com/google/uuid"
)

// Event types emitted across the platform.
const (
	ArtifactCreated  = "artifact.created"
	ArtifactUpdated  = "artifact.updated"
	ArtifactDeleted  = "artifact.deleted"
	// ArtifactStatusChanged carries {from, to} in its payload; the event
	// stream is the sign-off history (issue #127 keeps approval audit in
	// events rather than a dedicated table).
	ArtifactStatusChanged = "artifact.status_changed"
	LinkCreated      = "link.created"
	LinkUpdated      = "link.updated"
	LinkDeleted      = "link.deleted"
	BaselineCaptured = "baseline.captured"
	ChatterCreated   = "chatter.created"
	TestRunRecorded  = "testrun.recorded"
	WorkItemCreated  = "workitem.created"
	WorkItemMoved    = "workitem.moved"
	WorkItemUpdated  = "workitem.updated"
	RunFinished      = "agentrun.finished"
)

// Actor constants; user actors are "user:<id>", agent actors "agent:<run_id>".
const (
	ActorSystem = "system"
)

// Event is a domain event persisted for audit and dispatched to subscribers.
// OrgID is set by callers that know the tenant; the bus backfills it from
// ProjectID for callers that don't.
type Event struct {
	ID        string                 `json:"id"`
	OrgID     string                 `json:"org_id,omitempty"`
	EventType string                 `json:"event_type"`
	ProjectID string                 `json:"project_id,omitempty"`
	EntityID  string                 `json:"entity_id,omitempty"`
	Actor     string                 `json:"actor"`
	Payload   map[string]interface{} `json:"payload"`
	CreatedAt time.Time              `json:"created_at"`
}

// WithOrg returns a copy of the event stamped with the owning org.
func (e Event) WithOrg(orgID string) Event {
	e.OrgID = orgID
	return e
}

// New creates an event with a fresh ID and timestamp.
func New(eventType, projectID, entityID, actor string, payload map[string]interface{}) Event {
	if actor == "" {
		actor = ActorSystem
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return Event{
		ID:        uuid.New().String(),
		EventType: eventType,
		ProjectID: projectID,
		EntityID:  entityID,
		Actor:     actor,
		Payload:   payload,
		CreatedAt: time.Now(),
	}
}

// Bus publishes events to persistent storage and in-process subscribers.
type Bus interface {
	Publish(e Event)
	Subscribe(fn func(Event))
}

// Repository persists domain events.
type Repository interface {
	Save(e Event) error
	// List returns an org's recent events, newest first; the org filter is
	// mandatory, projectID/eventType are optional narrows.
	List(orgID, projectID, eventType string, limit int) ([]Event, error)
}
