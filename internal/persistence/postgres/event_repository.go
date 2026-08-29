package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// EventRepository implements events.Repository.
type EventRepository struct {
	db *sql.DB
}

// NewEventRepository creates a new event repository.
func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

// Save persists a domain event.
func (r *EventRepository) Save(e events.Event) error {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO domain_events (id, org_id, event_type, project_id, entity_id, actor, payload, created_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6, $7, $8)
	`, e.ID, e.OrgID, e.EventType, e.ProjectID, e.EntityID, e.Actor, payload, e.CreatedAt)
	return err
}

// List returns an org's recent events, newest first, optionally filtered by
// project and event type. The org filter is mandatory. beforeID is a keyset
// cursor: when non-empty, only events strictly older than the event with that
// ID (ordered by created_at, id) are returned. An unknown cursor matches no
// rows, so a stale cursor degrades to an empty page rather than an error.
func (r *EventRepository) List(orgID, projectID, eventType, beforeID string, limit int) ([]events.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
		SELECT id, COALESCE(org_id::text, ''), event_type, COALESCE(project_id::text, ''), COALESCE(entity_id::text, ''), actor, payload, created_at
		FROM domain_events
		WHERE org_id = NULLIF($1, '')::uuid
		  AND ($2 = '' OR project_id = $2::uuid)
		  AND ($3 = '' OR event_type = $3)
		  AND ($4 = '' OR (created_at, id) < (
		      SELECT created_at, id FROM domain_events WHERE id = $4::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $5
	`
	rows, err := r.db.Query(query, orgID, projectID, eventType, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []events.Event
	for rows.Next() {
		var e events.Event
		var payload []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.EventType, &e.ProjectID, &e.EntityID, &e.Actor, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &e.Payload); err != nil {
				e.Payload = map[string]interface{}{}
			}
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
