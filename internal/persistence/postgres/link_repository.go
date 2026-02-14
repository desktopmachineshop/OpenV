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
		INSERT INTO links (id, from_id, to_id, type, attributes, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	err = r.db.QueryRow(
		query,
		link.ID,
		link.FromID,
		link.ToID,
		link.Type,
		attributesJSON,
		link.Version,
		link.CreatedAt,
		link.UpdatedAt,
	).Err()

	return err
}

// FindByID retrieves a link by ID
func (r *LinkRepository) FindByID(id string) (*links.Link, error) {
	link := &links.Link{}
	var attributesJSON []byte

	query := `
		SELECT id, from_id, to_id, type, attributes, version, created_at, updated_at
		FROM links
		WHERE id = $1
	`

	err := r.db.QueryRow(query, id).Scan(
		&link.ID,
		&link.FromID,
		&link.ToID,
		&link.Type,
		&attributesJSON,
		&link.Version,
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

// FindByFromID retrieves all links from an artifact
func (r *LinkRepository) FindByFromID(fromID string) ([]*links.Link, error) {
	query := `
		SELECT id, from_id, to_id, type, attributes, version, created_at, updated_at
		FROM links
		WHERE from_id = $1
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

// FindByToID retrieves all links to an artifact
func (r *LinkRepository) FindByToID(toID string) ([]*links.Link, error) {
	query := `
		SELECT id, from_id, to_id, type, attributes, version, created_at, updated_at
		FROM links
		WHERE to_id = $1
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

// FindAll retrieves all links in a project
func (r *LinkRepository) FindAll(projectID string) ([]*links.Link, error) {
	query := `
		SELECT l.id, l.from_id, l.to_id, l.type, l.attributes, l.version, l.created_at, l.updated_at
		FROM links l
		INNER JOIN artifacts a ON l.from_id = a.id
		WHERE a.project_id = $1
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

// Update updates a link
func (r *LinkRepository) Update(link *links.Link) error {
	attributesJSON, err := json.Marshal(link.Attributes)
	if err != nil {
		return err
	}

	query := `
		UPDATE links
		SET type = $1, attributes = $2, version = $3, updated_at = $4
		WHERE id = $5
	`

	_, err = r.db.Exec(
		query,
		link.Type,
		attributesJSON,
		link.Version,
		link.UpdatedAt,
		link.ID,
	)

	return err
}

// Delete deletes a link
func (r *LinkRepository) Delete(id string) error {
	query := `DELETE FROM links WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}
