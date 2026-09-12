package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"

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
// List returns one page of one view, newest first.
//
// The cursor is a keyset on (created_at, id) rather than an OFFSET: the
// history only grows at the top, so an offset would skip or repeat rows as
// notifications arrive between pages. The id breaks ties, which matters
// because a fan-out writes several rows in the same instant.
func (r *NotificationRepository) List(userID string, q notifications.ListQuery) ([]*notifications.Notification, error) {
	query := `
		SELECT id, COALESCE(org_id::text, ''), user_id, type, title, body, entity_ref, read, flagged, cleared_at, created_at
		FROM notifications
		WHERE user_id = $1
	`
	args := []interface{}{userID}
	switch q.View {
	case notifications.ViewFlagged:
		query += ` AND flagged`
	case notifications.ViewCleared:
		query += ` AND cleared_at IS NOT NULL`
	default:
		query += ` AND cleared_at IS NULL`
	}
	if q.UnreadOnly {
		query += ` AND NOT read`
	}
	if q.HasCursor() {
		args = append(args, q.BeforeTime, q.BeforeID)
		query += fmt.Sprintf(` AND (created_at, id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	args = append(args, q.Limit)
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*notifications.Notification
	for rows.Next() {
		n := new(notifications.Notification)
		var entityRef []byte
		var clearedAt sql.NullTime
		if err := rows.Scan(&n.ID, &n.OrgID, &n.UserID, &n.Type, &n.Title, &n.Body, &entityRef, &n.Read, &n.Flagged, &clearedAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		if clearedAt.Valid {
			t := clearedAt.Time
			n.ClearedAt = &t
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
// ClearInbox archives every uncleared row of the user and returns how many
// moved. Scoped by user_id in the statement itself, so a caller can only ever
// clear their own list.
//
// Deliberately total — sparing the unread ones would leave the bell showing a
// count after the member asked for an empty inbox — and deliberately not a
// delete: the rows stay, readable in the cleared view. Marking them read in
// the same statement keeps the badge and the inbox in step, since a cleared
// row never counts towards the badge anyway.
func (r *NotificationRepository) ClearInbox(userID string) (int64, error) {
	res, err := r.db.Exec(`
		UPDATE notifications SET cleared_at = NOW(), read = TRUE
		WHERE user_id = $1 AND cleared_at IS NULL
	`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetFlagged flags or unflags one of the user's own rows, reporting whether
// one matched — an id belonging to somebody else matches nothing, and is
// answered the same way a missing one is.
func (r *NotificationRepository) SetFlagged(userID, id string, flagged bool) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE notifications SET flagged = $3
		WHERE user_id = $1 AND id = $2
	`, userID, id, flagged)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteCleared permanently removes what the user has already cleared. The
// only path in the system that destroys a notification, which is why it takes
// only rows the member has already put out of the way.
func (r *NotificationRepository) DeleteCleared(userID string) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM notifications WHERE user_id = $1 AND cleared_at IS NOT NULL`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountUnread counts unread rows still in the inbox. A cleared row never
// contributes to the bell badge, however it was left.
func (r *NotificationRepository) CountUnread(userID string) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM notifications
		WHERE user_id = $1 AND NOT read AND cleared_at IS NULL
	`, userID).Scan(&count)
	return count, err
}
