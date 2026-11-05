package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/savedsearches"
)

// SavedSearchRepository implements savedsearches.Repository (issue #722).
// Every query is keyed by the workspace, and every write by the member too,
// so a member lists only their own searches and those shared in the
// workspace they are in, and changes only their own.
type SavedSearchRepository struct {
	db *sql.DB
}

// NewSavedSearchRepository creates the repository.
func NewSavedSearchRepository(db *sql.DB) *SavedSearchRepository {
	return &SavedSearchRepository{db: db}
}

const savedSearchColumns = `id, user_id, org_id, scope, name, value, shared, created_at`

func scanSavedSearch(row rowScanner) (*savedsearches.SavedSearch, error) {
	s := new(savedsearches.SavedSearch)
	var value []byte
	if err := row.Scan(&s.ID, &s.UserID, &s.OrgID, &s.Scope, &s.Name, &value, &s.Shared, &s.CreatedAt); err != nil {
		return nil, err
	}
	s.Value = value
	return s, nil
}

// Upsert stores a search, keyed on (user, workspace, scope, name): saving
// under a name already used replaces the value and the sharing, and the
// existing row's id and created_at are written back into s.
func (r *SavedSearchRepository) Upsert(s *savedsearches.SavedSearch) error {
	ctx, cancel := stmtCtx()
	defer cancel()
	return r.db.QueryRowContext(ctx, `
		INSERT INTO saved_searches (id, user_id, org_id, scope, name, value, shared, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, org_id, scope, name) DO UPDATE SET
			value = EXCLUDED.value,
			shared = EXCLUDED.shared
		RETURNING id, created_at
	`, s.ID, s.UserID, s.OrgID, s.Scope, s.Name, []byte(s.Value), s.Shared, s.CreatedAt).Scan(&s.ID, &s.CreatedAt)
}

// ListVisible returns the member's own searches in the scope, then those
// others in the workspace shared, each by name.
func (r *SavedSearchRepository) ListVisible(userID, orgID, scope string) ([]*savedsearches.SavedSearch, error) {
	ctx, cancel := stmtCtx()
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+savedSearchColumns+`
		FROM saved_searches
		WHERE org_id = $2 AND scope = $3 AND (user_id = $1 OR shared)
		ORDER BY (user_id = $1) DESC, LOWER(name), created_at
	`, userID, orgID, scope)
	if malformedID(err) {
		return []*savedsearches.SavedSearch{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*savedsearches.SavedSearch{}
	for rows.Next() {
		s, err := scanSavedSearch(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// CountForUser counts the member's own searches in the scope.
func (r *SavedSearchRepository) CountForUser(userID, orgID, scope string) (int, error) {
	ctx, cancel := stmtCtx()
	defer cancel()
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM saved_searches WHERE user_id = $1 AND org_id = $2 AND scope = $3`,
		userID, orgID, scope).Scan(&n)
	if malformedID(err) {
		return 0, nil
	}
	return n, err
}

// DeleteForUser removes one of the member's own searches in the workspace.
func (r *SavedSearchRepository) DeleteForUser(userID, orgID, id string) (int64, error) {
	if !isUUID(id) {
		return 0, nil
	}
	ctx, cancel := stmtCtx()
	defer cancel()
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM saved_searches WHERE id = $1 AND user_id = $2 AND org_id = $3`, id, userID, orgID)
	if malformedID(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
