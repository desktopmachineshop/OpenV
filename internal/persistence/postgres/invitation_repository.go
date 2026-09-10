package postgres

import (
	"database/sql"
	"time"

	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// InvitationRepository implements invitations.Repository.
type InvitationRepository struct {
	db *sql.DB
}

// NewInvitationRepository creates a new invitation repository.
func NewInvitationRepository(db *sql.DB) *InvitationRepository {
	return &InvitationRepository{db: db}
}

// invitationEmail folds an address the way every row is written, so a lookup
// is a plain equality against the email column and idx_org_invitations_email
// is actually used. LOWER(i.email) = LOWER($1) would read the same and scan
// the whole table.
func invitationEmail(email string) string { return users.NormalizeEmail(email) }

const invitationColumns = `i.id, i.org_id, i.email, i.role, i.token_hash, i.invited_by, i.expires_at, i.accepted_at, i.created_at`

// invitationJoin carries the display names an invitee sees before they have
// any membership to read them from.
const invitationJoin = `
	FROM org_invitations i
	LEFT JOIN organizations o ON o.id = i.org_id
	LEFT JOIN users u ON u.id = i.invited_by`

const invitationSelect = `SELECT ` + invitationColumns + `, COALESCE(o.name, ''), COALESCE(u.name, '')` + invitationJoin

func scanInvitation(row interface{ Scan(...interface{}) error }) (*invitations.Invitation, error) {
	inv := new(invitations.Invitation)
	var invitedBy sql.NullString
	var acceptedAt sql.NullTime
	err := row.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.TokenHash, &invitedBy,
		&inv.ExpiresAt, &acceptedAt, &inv.CreatedAt, &inv.OrgName, &inv.InvitedByName)
	if err != nil {
		return nil, err
	}
	if invitedBy.Valid {
		v := invitedBy.String
		inv.InvitedBy = &v
	}
	if acceptedAt.Valid {
		t := acceptedAt.Time
		inv.AcceptedAt = &t
	}
	return inv, nil
}

func scanInvitations(rows *sql.Rows) ([]*invitations.Invitation, error) {
	defer rows.Close()
	var result []*invitations.Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, inv)
	}
	return result, rows.Err()
}

// Replace inserts an invitation, overwriting any unaccepted invitation the
// workspace already holds for the address.
//
// It is one statement rather than a delete followed by an insert because
// idx_org_invitations_pending covers EVERY unaccepted row, expired ones
// included: a delete-then-insert would leave a window in which a concurrent
// re-invite has already inserted, and the second insert would fail the
// index. Upserting on that same index instead makes the second writer wait
// and then overwrite, so re-inviting an address is always the newest link
// and never an error. Accepted rows are history and are left alone.
func (r *InvitationRepository) Replace(inv *invitations.Invitation) error {
	_, err := r.db.Exec(`
		INSERT INTO org_invitations (id, org_id, email, role, token_hash, invited_by, expires_at, accepted_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (org_id, email) WHERE accepted_at IS NULL
		DO UPDATE SET
			id = EXCLUDED.id,
			role = EXCLUDED.role,
			token_hash = EXCLUDED.token_hash,
			invited_by = EXCLUDED.invited_by,
			expires_at = EXCLUDED.expires_at,
			created_at = EXCLUDED.created_at
	`, inv.ID, inv.OrgID, invitationEmail(inv.Email), inv.Role, inv.TokenHash, inv.InvitedBy, inv.ExpiresAt, inv.AcceptedAt, inv.CreatedAt)
	return err
}

// ListPending returns the workspace's live invitations, newest first.
func (r *InvitationRepository) ListPending(orgID string, now time.Time) ([]*invitations.Invitation, error) {
	rows, err := r.db.Query(invitationSelect+`
		WHERE i.org_id = $1 AND i.accepted_at IS NULL AND i.expires_at > $2
		ORDER BY i.created_at DESC
	`, orgID, now)
	if err != nil {
		return nil, err
	}
	return scanInvitations(rows)
}

// FindByID returns one invitation, or nil.
func (r *InvitationRepository) FindByID(id string) (*invitations.Invitation, error) {
	inv, err := scanInvitation(r.db.QueryRow(invitationSelect+` WHERE i.id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return inv, err
}

// FindByTokenHash returns the invitation with the given token hash, or nil.
func (r *InvitationRepository) FindByTokenHash(hash string) (*invitations.Invitation, error) {
	inv, err := scanInvitation(r.db.QueryRow(invitationSelect+` WHERE i.token_hash = $1`, hash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return inv, err
}

// ListPendingForEmail returns every workspace's pending invitations for one
// address, oldest first (the order they are accepted in).
func (r *InvitationRepository) ListPendingForEmail(email string, now time.Time) ([]*invitations.Invitation, error) {
	rows, err := r.db.Query(invitationSelect+`
		WHERE i.email = $1 AND i.accepted_at IS NULL AND i.expires_at > $2
		ORDER BY i.created_at
	`, invitationEmail(email), now)
	if err != nil {
		return nil, err
	}
	return scanInvitations(rows)
}

// MarkAccepted stamps accepted_at, reporting whether this caller won the
// claim. The WHERE clause is the claim: only one concurrent acceptance can
// match a row that is still pending.
func (r *InvitationRepository) MarkAccepted(id string, at time.Time) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE org_invitations SET accepted_at = $2
		WHERE id = $1 AND accepted_at IS NULL AND expires_at > $2
	`, id, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Delete removes an invitation.
func (r *InvitationRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM org_invitations WHERE id = $1`, id)
	return err
}

// DeleteExpired removes expired, never-accepted invitations.
func (r *InvitationRepository) DeleteExpired(before time.Time) error {
	_, err := r.db.Exec(`DELETE FROM org_invitations WHERE accepted_at IS NULL AND expires_at < $1`, before)
	return err
}
