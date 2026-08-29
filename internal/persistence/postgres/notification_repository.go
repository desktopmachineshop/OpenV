package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/lib/pq"
	"github.com/openv/requirements-platform/internal/domain/notifications"
)

// NotificationRepository implements notifications.Repository. Every query is
// keyed by user_id so callers can only ever touch their own rows.
type NotificationRepository struct {
	db *sql.DB
}

// NewNotificationRepository creates the repository.
func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// Insert stores one notification.
func (r *NotificationRepository) Insert(n *notifications.Notification) error {
	entityRef, err := json.Marshal(n.EntityRef)
	if err != nil {
		return err
	}
	var orgID interface{}
	if n.OrgID != "" {
		orgID = n.OrgID
	}
	_, err = r.db.Exec(`
		INSERT INTO notifications (id, org_id, user_id, type, title, body, entity_ref, read, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, n.ID, orgID, n.UserID, n.Type, n.Title, n.Body, entityRef, n.Read, n.CreatedAt)
	return err
}

// ListForUser returns the user's notifications, newest first.
func (r *NotificationRepository) ListForUser(userID string, unreadOnly bool, limit int) ([]*notifications.Notification, error) {
	query := `
		SELECT id, COALESCE(org_id::text, ''), user_id, type, title, body, entity_ref, read, created_at
		FROM notifications
		WHERE user_id = $1
	`
	if unreadOnly {
		query += ` AND NOT read`
	}
	query += ` ORDER BY created_at DESC LIMIT $2`

	rows, err := r.db.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*notifications.Notification
	for rows.Next() {
		n := new(notifications.Notification)
		var entityRef []byte
		if err := rows.Scan(&n.ID, &n.OrgID, &n.UserID, &n.Type, &n.Title, &n.Body, &entityRef, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		if len(entityRef) > 0 {
			if err := json.Unmarshal(entityRef, &n.EntityRef); err != nil {
				n.EntityRef = map[string]interface{}{}
			}
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

// MarkRead marks the given ids read for that user only; ids belonging to
// other users are silently skipped. Returns rows updated.
func (r *NotificationRepository) MarkRead(userID string, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := r.db.Exec(`
		UPDATE notifications SET read = TRUE
		WHERE user_id = $1 AND NOT read AND id = ANY($2::uuid[])
	`, userID, pq.Array(ids))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkAllRead marks every unread row of the user read.
func (r *NotificationRepository) MarkAllRead(userID string) (int64, error) {
	res, err := r.db.Exec(`
		UPDATE notifications SET read = TRUE
		WHERE user_id = $1 AND NOT read
	`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountUnread returns the user's unread count (the bell badge).
func (r *NotificationRepository) CountUnread(userID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND NOT read
	`, userID).Scan(&count)
	return count, err
}
