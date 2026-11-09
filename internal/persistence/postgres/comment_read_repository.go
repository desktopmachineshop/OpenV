package postgres

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/commentreads"
)

// CommentReadRepository implements commentreads.Repository: when a member
// last read each artifact's comments. Every query is keyed by the member and
// the workspace, so a member reads and writes only their own read state, and
// only for artifacts of the workspace they are in.
type CommentReadRepository struct {
	db *sql.DB
}

// NewCommentReadRepository creates the repository.
func NewCommentReadRepository(db *sql.DB) *CommentReadRepository {
	return &CommentReadRepository{db: db}
}

// List returns the member's read times for those of artifactIDs they have one
// for in the workspace.
func (r *CommentReadRepository) List(userID, orgID string, artifactIDs []string) ([]commentreads.Read, error) {
	ctx, cancel := stmtCtx()
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `
		SELECT artifact_id, read_at FROM comment_reads
		WHERE user_id = $1 AND org_id = $2 AND artifact_id = ANY($3::uuid[])
		ORDER BY artifact_id
	`, userID, orgID, pq.Array(artifactIDs))
	if malformedID(err) {
		return []commentreads.Read{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []commentreads.Read{}
	for rows.Next() {
		var rd commentreads.Read
		if err := rows.Scan(&rd.ArtifactID, &rd.ReadAt); err != nil {
			return nil, err
		}
		// A TIMESTAMP holding a UTC wall clock (#379 bug 162).
		rd.ReadAt = rd.ReadAt.UTC()
		list = append(list, rd)
	}
	return list, rows.Err()
}

// Set stores each read time, in one transaction. The artifact's project and
// workspace come from the artifact itself, so an id that is not a live
// artifact of the workspace stores nothing.
func (r *CommentReadRepository) Set(userID, orgID string, reads []commentreads.Read) ([]commentreads.Read, error) {
	if !isUUID(userID) || !isUUID(orgID) {
		return []commentreads.Read{}, nil
	}
	stored := []commentreads.Read{}
	err := withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		for _, rd := range reads {
			ctx, cancel := stmtCtx()
			res, err := tx.ExecContext(ctx, `
				INSERT INTO comment_reads (user_id, org_id, project_id, artifact_id, read_at)
				SELECT $1, p.org_id, a.project_id, a.id, $4
				FROM artifacts a JOIN projects p ON p.id = a.project_id
				WHERE a.id = $3 AND a.valid_to IS NULL AND p.org_id = $2
				ON CONFLICT (user_id, artifact_id) DO UPDATE SET read_at = EXCLUDED.read_at
			`, userID, orgID, rd.ArtifactID, rd.ReadAt.UTC())
			cancel()
			if malformedID(err) {
				continue
			}
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				stored = append(stored, commentreads.Read{ArtifactID: rd.ArtifactID, ReadAt: rd.ReadAt.UTC()})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return stored, nil
}
