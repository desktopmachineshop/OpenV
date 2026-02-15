package postgres

import (
    "database/sql"
    "encoding/json"
    "errors"
    "time"

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
		INSERT INTO artifacts (id, project_id, parent_id, type, title, body, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	err = r.db.QueryRow(
		query,
		artifact.ID,
		artifact.ProjectID,
		artifact.ParentID,
		artifact.Type,
		artifact.Title,
		artifact.Body,
		attributesJSON,
		artifact.Version,
		artifact.ValidFrom,
		artifact.ValidTo,
		artifact.CreatedAt,
		artifact.UpdatedAt,
	).Err()

	return err
}

// FindByID retrieves an artifact by ID
func (r *ArtifactRepository) FindByID(id string) (*artifacts.Artifact, error) {
	artifact := &artifacts.Artifact{}
	var attributesJSON []byte

	query := `
		SELECT id, project_id, parent_id, type, title, body, attributes, version, valid_from, valid_to, created_at, updated_at
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
		SELECT id, project_id, parent_id, type, title, body, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		ORDER BY created_at ASC
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
		SELECT id, project_id, parent_id, type, title, body, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND type = $2 AND valid_to IS NULL
		ORDER BY created_at ASC
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

// Update updates an artifact
func (r *ArtifactRepository) Update(artifact *artifacts.Artifact) error {
	attributesJSON, err := json.Marshal(artifact.Attributes)
	if err != nil {
		return err
	}

	query := `
		UPDATE artifacts 
		SET project_id = $1, parent_id = $2, type = $3, title = $4, body = $5, attributes = $6, version = $7, updated_at = $8
		WHERE id = $9
	`

	result, err := r.db.Exec(
		query,
		artifact.ProjectID,
		artifact.ParentID,
		artifact.Type,
		artifact.Title,
		artifact.Body,
		attributesJSON,
		artifact.Version,
		artifact.UpdatedAt,
		artifact.ID,
	)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return errors.New("artifact not found")
	}

	return nil
}

// Delete soft-deletes an artifact
func (r *ArtifactRepository) Delete(id string) error {
	query := `UPDATE artifacts SET valid_to = $1 WHERE id = $2 AND valid_to IS NULL`
	_, err := r.db.Exec(query, time.Now(), id)
	return err
}
