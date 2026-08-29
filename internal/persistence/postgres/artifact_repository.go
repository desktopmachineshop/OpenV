package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// stmtTimeout bounds a single database statement so a stuck query cannot hang
// a request indefinitely. Save originally set this only for itself; the whole
// repository now shares it so no method is unbounded.
const stmtTimeout = 5 * time.Second

// stmtCtx returns a context bounded by stmtTimeout for one statement. Callers
// must defer the returned cancel.
func stmtCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), stmtTimeout)
}

// ArtifactRepository implements artifacts.Repository using PostgreSQL
type ArtifactRepository struct {
	db *sql.DB
}

// NewArtifactRepository creates a new artifact repository
func NewArtifactRepository(db *sql.DB) *ArtifactRepository {
	return &ArtifactRepository{db: db}
}

// Save inserts a new artifact, assigning its stable ref (e.g. "REQ-12")
// when the caller did not supply one. Assignment and insert share one
// transaction so a failed insert never burns a counter value into a
// half-created artifact.
func (r *ArtifactRepository) Save(artifact *artifacts.Artifact) error {
	attributesJSON, err := json.Marshal(artifact.Attributes)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO artifacts (id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	// Bound the insert so a stuck query can't hang the request indefinitely.
	ctx, cancel := stmtCtx()
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := ensureRef(ctx, tx, artifact); err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		query,
		artifact.ID,
		artifact.ProjectID,
		artifact.ParentID,
		artifact.Type,
		artifact.Ref,
		artifact.Title,
		artifact.Body,
		artifact.SortOrder,
		artifact.Status,
		attributesJSON,
		artifact.Version,
		artifact.ValidFrom,
		artifact.ValidTo,
		artifact.CreatedAt,
		artifact.UpdatedAt,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ensureRef fills artifact.Ref inside tx. An empty ref draws the next
// number from the per-project, per-prefix counter (the upsert is atomic, so
// concurrent creates cannot mint the same ref). A caller-supplied ref (e.g.
// a project import preserving its exported refs) is kept as-is, but a
// parseable PREFIX-NUM advances the counter past it so later assignments
// cannot collide; the partial unique index on (project_id, ref) backstops
// anything unparseable.
func ensureRef(ctx context.Context, tx *sql.Tx, artifact *artifacts.Artifact) error {
	if artifact.Ref == "" {
		prefix := artifacts.RefPrefix(artifact.Type)
		var n int
		err := tx.QueryRowContext(ctx, `
			INSERT INTO artifact_ref_counters (project_id, prefix, next_num)
			VALUES ($1, $2, 2)
			ON CONFLICT (project_id, prefix)
			DO UPDATE SET next_num = artifact_ref_counters.next_num + 1
			RETURNING next_num - 1
		`, artifact.ProjectID, prefix).Scan(&n)
		if err != nil {
			return err
		}
		artifact.Ref = artifacts.FormatRef(prefix, n)
		return nil
	}
	if prefix, n, ok := artifacts.ParseRef(artifact.Ref); ok {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO artifact_ref_counters (project_id, prefix, next_num)
			VALUES ($1, $2, $3)
			ON CONFLICT (project_id, prefix)
			DO UPDATE SET next_num = GREATEST(artifact_ref_counters.next_num, $3)
		`, artifact.ProjectID, prefix, n+1)
		return err
	}
	return nil
}

// FindByID retrieves an artifact by ID
func (r *ArtifactRepository) FindByID(id string) (*artifacts.Artifact, error) {
	artifact := &artifacts.Artifact{}
	var attributesJSON []byte
	// ref is NULL on rows the 0018 backfill never touched (soft-deleted
	// artifacts), so it scans through a NullString everywhere.
	var ref sql.NullString

	query := `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE id = $1 AND valid_to IS NULL
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&artifact.ID,
		&artifact.ProjectID,
		&artifact.ParentID,
		&artifact.Type,
		&ref,
		&artifact.Title,
		&artifact.Body,
		&artifact.SortOrder,
		&artifact.Status,
		&attributesJSON,
		&artifact.Version,
		&artifact.ValidFrom,
		&artifact.ValidTo,
		&artifact.CreatedAt,
		&artifact.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("artifact not found")
		}
		return nil, err
	}

	artifact.Ref = ref.String

	if attributesJSON != nil {
		err = json.Unmarshal(attributesJSON, &artifact.Attributes)
		if err != nil {
			return nil, err
		}
	}

	return artifact, nil
}

// FindByProjectID retrieves all artifacts for a project
func (r *ArtifactRepository) FindByProjectID(projectID string) ([]*artifacts.Artifact, error) {
	query := `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectArtifacts(rows)
}

// FindByProjectAndType retrieves artifacts by project and type
func (r *ArtifactRepository) FindByProjectAndType(projectID string, artifactType string) ([]*artifacts.Artifact, error) {
	query := `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND type = $2 AND valid_to IS NULL
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	rows, err := r.db.QueryContext(ctx, query, projectID, artifactType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectArtifacts(rows)
}

// FindByProjectAndStatus retrieves a project's current artifacts in the given
// review status, newest change first (issue #183: the review queue lists
// in_review artifacts). Served by the idx_artifacts_project_status partial
// index over live rows.
func (r *ArtifactRepository) FindByProjectAndStatus(projectID string, status string) ([]*artifacts.Artifact, error) {
	query := `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND status = $2 AND valid_to IS NULL
		ORDER BY updated_at DESC, id ASC
	`

	rows, err := r.db.Query(query, projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectArtifacts(rows)
}

// FindPageByProject returns one page of a project's current artifacts;
// artifactType "" means all types. The ordering matches FindByProjectID with
// id as a final tiebreaker so offset pages are stable even when sibling
// sort_order/created_at values collide.
func (r *ArtifactRepository) FindPageByProject(projectID string, artifactType string, limit, offset int) ([]*artifacts.Artifact, error) {
	query := `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		AND ($2 = '' OR type = $2)
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC, id ASC
		LIMIT $3 OFFSET $4
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	rows, err := r.db.QueryContext(ctx, query, projectID, artifactType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectArtifacts(rows)
}

// CountByProject counts a project's current artifacts (type "" = all).
func (r *ArtifactRepository) CountByProject(projectID string, artifactType string) (int, error) {
	ctx, cancel := stmtCtx()
	defer cancel()

	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		AND ($2 = '' OR type = $2)
	`, projectID, artifactType).Scan(&count)
	return count, err
}

// collectArtifacts scans rows produced by the standard 15-column artifact
// SELECT into a slice.
func collectArtifacts(rows *sql.Rows) ([]*artifacts.Artifact, error) {
	var artifactList []*artifacts.Artifact
	for rows.Next() {
		artifact := new(artifacts.Artifact)
		var attributesJSON []byte
		var ref sql.NullString

		err := rows.Scan(
			&artifact.ID,
			&artifact.ProjectID,
			&artifact.ParentID,
			&artifact.Type,
			&ref,
			&artifact.Title,
			&artifact.Body,
			&artifact.SortOrder,
			&artifact.Status,
			&attributesJSON,
			&artifact.Version,
			&artifact.ValidFrom,
			&artifact.ValidTo,
			&artifact.CreatedAt,
			&artifact.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}
		artifact.Ref = ref.String

		if attributesJSON != nil {
			err = json.Unmarshal(attributesJSON, &artifact.Attributes)
			if err != nil {
				return nil, err
			}
		}

		artifactList = append(artifactList, artifact)
	}

	return artifactList, rows.Err()
}

// Update updates an artifact by archiving the old version and creating a new one
func (r *ArtifactRepository) Update(artifact *artifacts.Artifact) error {
	attributesJSON, err := json.Marshal(artifact.Attributes)
	if err != nil {
		return err
	}

	ctx, cancel := stmtCtx()
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Mark the current version as archived
	archiveQuery := `UPDATE artifacts SET valid_to = $1 WHERE id = $2 AND valid_to IS NULL`
	_, err = tx.ExecContext(ctx, archiveQuery, artifact.ValidFrom, artifact.ID)
	if err != nil {
		return err
	}

	// Insert the new version. The ref rides along unchanged from the loaded
	// current version — a ref is stable across an artifact's history. NULLIF
	// keeps a still-unset ref NULL rather than "" so the partial unique
	// index never sees two empty-string refs in one project.
	insertQuery := `
		INSERT INTO artifacts (id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err = tx.ExecContext(
		ctx,
		insertQuery,
		artifact.ID,
		artifact.ProjectID,
		artifact.ParentID,
		artifact.Type,
		artifact.Ref,
		artifact.Title,
		artifact.Body,
		artifact.SortOrder,
		artifact.Status,
		attributesJSON,
		artifact.Version,
		artifact.ValidFrom,
		artifact.ValidTo,
		artifact.CreatedAt,
		artifact.UpdatedAt,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Delete soft-deletes an artifact
func (r *ArtifactRepository) Delete(id string) error {
	ctx, cancel := stmtCtx()
	defer cancel()

	query := `UPDATE artifacts SET valid_to = $1 WHERE id = $2 AND valid_to IS NULL`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

// NextSortOrder returns the next sort order value for a parent group.
func (r *ArtifactRepository) NextSortOrder(projectID string, parentID *string) (int, error) {
	query := `
		SELECT COALESCE(MAX(sort_order), 0) + 1
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		AND parent_id IS NOT DISTINCT FROM $2
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	var next int
	err := r.db.QueryRowContext(ctx, query, projectID, parentID).Scan(&next)
	if err != nil {
		return 0, err
	}

	return next, nil
}

// SearchInProjects finds current artifacts whose title or body contains the
// query (case-insensitive) within the given projects. Title matches rank
// before body-only matches; ties break on most recently updated. ProjectName
// is left empty — the API layer resolves it.
func (r *ArtifactRepository) SearchInProjects(projectIDs []string, query string, limit int) ([]*artifacts.SearchHit, error) {
	if len(projectIDs) == 0 {
		return []*artifacts.SearchHit{}, nil
	}

	pattern := artifacts.LikePattern(query)
	// The two ILIKE predicates are backed by the pg_trgm GIN indexes
	// idx_artifacts_{title,body}_trgm (migration 0008): gin_trgm_ops supports
	// ILIKE directly, so the planner bitmap-ORs the two index scans instead of
	// sequentially scanning artifacts. Queries shorter than a trigram, or a
	// database where the pg_trgm extension could not be created, fall back to a
	// sequential scan — the query stays correct either way.
	sqlQuery := `
		SELECT id, project_id, type, title, body
		FROM artifacts
		WHERE valid_to IS NULL
		AND project_id = ANY($1)
		AND (title ILIKE $2 OR body ILIKE $2)
		ORDER BY (title ILIKE $2) DESC, updated_at DESC
		LIMIT $3
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	rows, err := r.db.QueryContext(ctx, sqlQuery, pq.Array(projectIDs), pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := []*artifacts.SearchHit{}
	for rows.Next() {
		hit := new(artifacts.SearchHit)
		var body string
		if err := rows.Scan(&hit.ArtifactID, &hit.ProjectID, &hit.Type, &hit.Title, &body); err != nil {
			return nil, err
		}
		hit.Snippet = artifacts.Snippet(body, query)
		hits = append(hits, hit)
	}

	return hits, rows.Err()
}

// FindVersionsByID retrieves all versions of an artifact (including historical ones)
func (r *ArtifactRepository) FindVersionsByID(id string) ([]*artifacts.Artifact, error) {
	query := `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE id = $1
		ORDER BY version DESC
	`

	ctx, cancel := stmtCtx()
	defer cancel()

	rows, err := r.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectArtifacts(rows)
}
