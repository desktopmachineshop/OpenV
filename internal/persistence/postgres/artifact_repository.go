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

// ArtifactRepository implements artifacts.Repository using PostgreSQL
type ArtifactRepository struct {
	db *sql.DB
}

// NewArtifactRepository creates a new artifact repository
func NewArtifactRepository(db *sql.DB) *ArtifactRepository {
	return &ArtifactRepository{db: db}
}

// Save inserts a new artifact
func (r *ArtifactRepository) Save(artifact *artifacts.Artifact) error {
	attributesJSON, err := json.Marshal(artifact.Attributes)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO artifacts (id, project_id, parent_id, type, title, body, sort_order, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	// Use context with 5 second timeout for individual artifact inserts
	// This prevents indefinite hangs and provides clear error reporting
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = r.db.ExecContext(
		ctx,
		query,
		artifact.ID,
		artifact.ProjectID,
		artifact.ParentID,
		artifact.Type,
		artifact.Title,
		artifact.Body,
		artifact.SortOrder,
		attributesJSON,
		artifact.Version,
		artifact.ValidFrom,
		artifact.ValidTo,
		artifact.CreatedAt,
		artifact.UpdatedAt,
	)

	return err
}

// FindByID retrieves an artifact by ID
func (r *ArtifactRepository) FindByID(id string) (*artifacts.Artifact, error) {
	artifact := &artifacts.Artifact{}
	var attributesJSON []byte

	query := `
		SELECT id, project_id, parent_id, type, title, body, sort_order, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE id = $1 AND valid_to IS NULL
	`

	err := r.db.QueryRow(query, id).Scan(
		&artifact.ID,
		&artifact.ProjectID,
		&artifact.ParentID,
		&artifact.Type,
		&artifact.Title,
		&artifact.Body,
		&artifact.SortOrder,
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
		SELECT id, project_id, parent_id, type, title, body, sort_order, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifactList []*artifacts.Artifact
	for rows.Next() {
		artifact := new(artifacts.Artifact)
		var attributesJSON []byte

		err := rows.Scan(
			&artifact.ID,
			&artifact.ProjectID,
			&artifact.ParentID,
			&artifact.Type,
			&artifact.Title,
			&artifact.Body,
			&artifact.SortOrder,
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

// FindByProjectAndType retrieves artifacts by project and type
func (r *ArtifactRepository) FindByProjectAndType(projectID string, artifactType string) ([]*artifacts.Artifact, error) {
	query := `
		SELECT id, project_id, parent_id, type, title, body, sort_order, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND type = $2 AND valid_to IS NULL
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC
	`

	rows, err := r.db.Query(query, projectID, artifactType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifactList []*artifacts.Artifact
	for rows.Next() {
		artifact := new(artifacts.Artifact)
		var attributesJSON []byte

		err := rows.Scan(
			&artifact.ID,
			&artifact.ProjectID,
			&artifact.ParentID,
			&artifact.Type,
			&artifact.Title,
			&artifact.Body,
			&artifact.SortOrder,
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

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Mark the current version as archived
	archiveQuery := `UPDATE artifacts SET valid_to = $1 WHERE id = $2 AND valid_to IS NULL`
	_, err = tx.Exec(archiveQuery, artifact.ValidFrom, artifact.ID)
	if err != nil {
		return err
	}

	// Insert the new version
	insertQuery := `
		INSERT INTO artifacts (id, project_id, parent_id, type, title, body, sort_order, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err = tx.Exec(
		insertQuery,
		artifact.ID,
		artifact.ProjectID,
		artifact.ParentID,
		artifact.Type,
		artifact.Title,
		artifact.Body,
		artifact.SortOrder,
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
	query := `UPDATE artifacts SET valid_to = $1 WHERE id = $2 AND valid_to IS NULL`
	_, err := r.db.Exec(query, time.Now(), id)
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

	var next int
	err := r.db.QueryRow(query, projectID, parentID).Scan(&next)
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
	sqlQuery := `
		SELECT id, project_id, type, title, body
		FROM artifacts
		WHERE valid_to IS NULL
		AND project_id = ANY($1)
		AND (title ILIKE $2 OR body ILIKE $2)
		ORDER BY (title ILIKE $2) DESC, updated_at DESC
		LIMIT $3
	`

	rows, err := r.db.Query(sqlQuery, pq.Array(projectIDs), pattern, limit)
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
		SELECT id, project_id, parent_id, type, title, body, sort_order, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE id = $1
		ORDER BY version DESC
	`

	rows, err := r.db.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifactList []*artifacts.Artifact
	for rows.Next() {
		artifact := new(artifacts.Artifact)
		var attributesJSON []byte

		err := rows.Scan(
			&artifact.ID,
			&artifact.ProjectID,
			&artifact.ParentID,
			&artifact.Type,
			&artifact.Title,
			&artifact.Body,
			&artifact.SortOrder,
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
