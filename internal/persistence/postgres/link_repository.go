package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/links"
)

// LinkRepository implements links.Repository using PostgreSQL
type LinkRepository struct {
	db *sql.DB
}

// NewLinkRepository creates a new link repository
func NewLinkRepository(db *sql.DB) *LinkRepository {
	return &LinkRepository{db: db}
}

// Save inserts a new link
func (r *LinkRepository) Save(link *links.Link) error {
	attributesJSON, err := json.Marshal(link.Attributes)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO links (id, from_id, to_id, type, suspect, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err = r.db.Exec(
		query,
		link.ID,
		link.FromID,
		link.ToID,
		link.Type,
		link.Suspect,
		attributesJSON,
		link.Version,
		link.ValidFrom,
		link.ValidTo,
		link.CreatedAt,
		link.UpdatedAt,
	)

	return err
}

// FindByID retrieves a link by ID (current version only)
func (r *LinkRepository) FindByID(id string) (*links.Link, error) {
	link := &links.Link{}
	var attributesJSON []byte

	query := `
		SELECT id, from_id, to_id, type, suspect, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM links
		WHERE id = $1 AND valid_to IS NULL
	`

	err := r.db.QueryRow(query, id).Scan(
		&link.ID,
		&link.FromID,
		&link.ToID,
		&link.Type,
		&link.Suspect,
		&attributesJSON,
		&link.Version,
		&link.ValidFrom,
		&link.ValidTo,
		&link.CreatedAt,
		&link.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("link not found")
		}
		return nil, err
	}

	if attributesJSON != nil {
		err = json.Unmarshal(attributesJSON, &link.Attributes)
		if err != nil {
			return nil, err
		}
	}

	return link, nil
}

// FindByFromID retrieves all current links from an artifact (valid_to IS NULL)
func (r *LinkRepository) FindByFromID(fromID string) ([]*links.Link, error) {
	query := `
		SELECT id, from_id, to_id, type, suspect, attributes, version, created_at, updated_at
		FROM links
		WHERE from_id = $1 AND valid_to IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, fromID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var linkList []*links.Link
	for rows.Next() {
		link := new(links.Link)
		var attributesJSON []byte

		err := rows.Scan(
			&link.ID,
			&link.FromID,
			&link.ToID,
			&link.Type,
			&link.Suspect,
			&attributesJSON,
			&link.Version,
			&link.CreatedAt,
			&link.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		if attributesJSON != nil {
			err = json.Unmarshal(attributesJSON, &link.Attributes)
			if err != nil {
				return nil, err
			}
		}

		linkList = append(linkList, link)
	}

	return linkList, rows.Err()
}

// FindByToID retrieves all current links to an artifact (valid_to IS NULL)
func (r *LinkRepository) FindByToID(toID string) ([]*links.Link, error) {
	query := `
		SELECT id, from_id, to_id, type, suspect, attributes, version, created_at, updated_at
		FROM links
		WHERE to_id = $1 AND valid_to IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, toID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var linkList []*links.Link
	for rows.Next() {
		link := new(links.Link)
		var attributesJSON []byte

		err := rows.Scan(
			&link.ID,
			&link.FromID,
			&link.ToID,
			&link.Type,
			&link.Suspect,
			&attributesJSON,
			&link.Version,
			&link.CreatedAt,
			&link.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		if attributesJSON != nil {
			err = json.Unmarshal(attributesJSON, &link.Attributes)
			if err != nil {
				return nil, err
			}
		}

		linkList = append(linkList, link)
	}

	return linkList, rows.Err()
}

// FindAll retrieves all current links in a project
func (r *LinkRepository) FindAll(projectID string) ([]*links.Link, error) {
	query := `
		SELECT l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.created_at, l.updated_at
		FROM links l
		INNER JOIN artifacts a ON l.from_id = a.id AND a.valid_to IS NULL
		WHERE a.project_id = $1 AND l.valid_to IS NULL
		ORDER BY l.created_at DESC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var linkList []*links.Link
	for rows.Next() {
		link := new(links.Link)
		var attributesJSON []byte

		err := rows.Scan(
			&link.ID,
			&link.FromID,
			&link.ToID,
			&link.Type,
			&link.Suspect,
			&attributesJSON,
			&link.Version,
			&link.CreatedAt,
			&link.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		if attributesJSON != nil {
			err = json.Unmarshal(attributesJSON, &link.Attributes)
			if err != nil {
				return nil, err
			}
		}

		linkList = append(linkList, link)
	}

	return linkList, rows.Err()
}

// FindSuspectByProject returns the live suspect links whose from or to
// artifact belongs to the project, joined with those artifacts' current
// titles and types (issue #183). Both endpoints are inner-joined against the
// live artifact rows: a suspect link needs both endpoints to still exist for
// a reviewer to act on it, and the join supplies the human-readable labels
// the review queue shows. Newest-changed first so freshly invalidated links
// surface at the top.
func (r *LinkRepository) FindSuspectByProject(projectID string) ([]*links.SuspectLink, error) {
	query := `
		SELECT l.id, l.type, l.updated_at,
		       fa.id, fa.title, fa.type,
		       ta.id, ta.title, ta.type
		FROM links l
		INNER JOIN artifacts fa ON fa.id = l.from_id AND fa.valid_to IS NULL
		INNER JOIN artifacts ta ON ta.id = l.to_id AND ta.valid_to IS NULL
		WHERE l.valid_to IS NULL AND l.suspect
		  AND (fa.project_id = $1 OR ta.project_id = $1)
		ORDER BY l.updated_at DESC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*links.SuspectLink{}
	for rows.Next() {
		sl := new(links.SuspectLink)
		if err := rows.Scan(
			&sl.ID,
			&sl.Type,
			&sl.UpdatedAt,
			&sl.FromID,
			&sl.FromTitle,
			&sl.FromType,
			&sl.ToID,
			&sl.ToTitle,
			&sl.ToType,
		); err != nil {
			return nil, err
		}
		list = append(list, sl)
	}

	return list, rows.Err()
}

// Update updates a link
func (r *LinkRepository) Update(link *links.Link) error {
	attributesJSON, err := json.Marshal(link.Attributes)
	if err != nil {
		return err
	}

	// Guard on valid_to IS NULL so an update never resurrects or mutates a
	// soft-deleted link (Delete tombstones by setting valid_to).
	query := `
		UPDATE links
		SET type = $1, suspect = $2, attributes = $3, version = $4, updated_at = $5
		WHERE id = $6 AND valid_to IS NULL
	`

	_, err = r.db.Exec(
		query,
		link.Type,
		link.Suspect,
		attributesJSON,
		link.Version,
		link.UpdatedAt,
		link.ID,
	)

	return err
}

// SetSuspect sets the suspect flag on one live link.
func (r *LinkRepository) SetSuspect(id string, suspect bool) error {
	query := `UPDATE links SET suspect = $1, updated_at = NOW() WHERE id = $2 AND valid_to IS NULL`
	_, err := r.db.Exec(query, suspect, id)
	return err
}

// SetSuspectByArtifact sets the suspect flag on every live link touching
// the given artifact (either endpoint). updated_at is deliberately left
// untouched: suspicion is derived review metadata, not a link edit.
func (r *LinkRepository) SetSuspectByArtifact(artifactID string, suspect bool) error {
	query := `
		UPDATE links SET suspect = $1
		WHERE (from_id = $2 OR to_id = $2) AND valid_to IS NULL AND suspect <> $1
	`
	_, err := r.db.Exec(query, suspect, artifactID)
	return err
}

// Delete soft-deletes a link by closing its validity interval, mirroring the
// artifact soft-delete. A hard DELETE would orphan link_artifacts rows (there
// are no FKs), and every read path already filters valid_to IS NULL, so a
// tombstoned link disappears from all queries while its history and any
// link_artifacts references stay intact. Only currently-live links are
// tombstoned; deleting an already-deleted link is a no-op.
func (r *LinkRepository) Delete(id string) error {
	query := `UPDATE links SET valid_to = NOW() WHERE id = $1 AND valid_to IS NULL`
	_, err := r.db.Exec(query, id)
	return err
}

// FindByFromIDForVersion retrieves links from an artifact at a specific version
func (r *LinkRepository) FindByFromIDForVersion(fromID string, version int) ([]*links.Link, error) {
	query := `
		SELECT DISTINCT l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.created_at, l.updated_at
		FROM links l
		WHERE l.from_id = $1
		AND EXISTS (
			SELECT 1 FROM link_artifacts 
			WHERE link_id = l.id 
			AND artifact_id = $1 
			AND artifact_version = $2
		)
		ORDER BY l.created_at DESC
	`

	rows, err := r.db.Query(query, fromID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var linkList []*links.Link
	for rows.Next() {
		link := new(links.Link)
		var attributesJSON []byte

		err := rows.Scan(
			&link.ID,
			&link.FromID,
			&link.ToID,
			&link.Type,
			&link.Suspect,
			&attributesJSON,
			&link.Version,
			&link.CreatedAt,
			&link.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		if attributesJSON != nil {
			err = json.Unmarshal(attributesJSON, &link.Attributes)
			if err != nil {
				return nil, err
			}
		}

		linkList = append(linkList, link)
	}

	// If no versioned links found, return empty (they'll see the difference)
	return linkList, rows.Err()
}

// FindByToIDForVersion retrieves links to an artifact at a specific version
func (r *LinkRepository) FindByToIDForVersion(toID string, version int) ([]*links.Link, error) {
	query := `
		SELECT DISTINCT l.id, l.from_id, l.to_id, l.type, l.suspect, l.attributes, l.version, l.created_at, l.updated_at
		FROM links l
		WHERE l.to_id = $1
		AND EXISTS (
			SELECT 1 FROM link_artifacts 
			WHERE link_id = l.id 
			AND artifact_id = $1 
			AND artifact_version = $2
		)
		ORDER BY l.created_at DESC
	`

	rows, err := r.db.Query(query, toID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var linkList []*links.Link
	for rows.Next() {
		link := new(links.Link)
		var attributesJSON []byte

		err := rows.Scan(
			&link.ID,
			&link.FromID,
			&link.ToID,
			&link.Type,
			&link.Suspect,
			&attributesJSON,
			&link.Version,
			&link.CreatedAt,
			&link.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		if attributesJSON != nil {
			err = json.Unmarshal(attributesJSON, &link.Attributes)
			if err != nil {
				return nil, err
			}
		}

		linkList = append(linkList, link)
	}

	// If no versioned links found, return empty (they'll see the difference)
	return linkList, rows.Err()
}

// RecordLinkForArtifactVersion records a link association with an artifact version
func (r *LinkRepository) RecordLinkForArtifactVersion(linkID string, artifactID string, artifactVersion int) error {
	query := `
		INSERT INTO link_artifacts (link_id, artifact_id, artifact_version)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`
	_, err := r.db.Exec(query, linkID, artifactID, artifactVersion)
	return err
}
