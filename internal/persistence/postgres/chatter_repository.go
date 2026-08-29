package postgres

import (
	"database/sql"
	"log"

	"github.com/openv/requirements-platform/internal/domain/chatter"
)

// ChatterRepository implements the chatter.Repository interface
type ChatterRepository struct {
	db *sql.DB
}

// NewChatterRepository creates a new chatter repository
func NewChatterRepository(db *sql.DB) *ChatterRepository {
	return &ChatterRepository{db: db}
}

// Save saves a chatter entry to the database
func (r *ChatterRepository) Save(entry *chatter.ChatterEntry) error {
	query := `
		INSERT INTO chatter (id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.Exec(
		query,
		entry.ID,
		entry.ArtifactID,
		entry.Message,
		entry.IsAutoEntry,
		entry.EntryType,
		entry.CreatedBy,
		entry.AuthorName,
		entry.CreatedAt,
		entry.UpdatedAt,
	)

	if err != nil {
		log.Printf("Error saving chatter entry: %v", err)
		return err
	}

	return nil
}

// FindByArtifactID retrieves all chatter entries for an artifact, ordered by creation date
func (r *ChatterRepository) FindByArtifactID(artifactID string) ([]*chatter.ChatterEntry, error) {
	query := `
		SELECT id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at
		FROM chatter
		WHERE artifact_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*chatter.ChatterEntry
	for rows.Next() {
		entry := new(chatter.ChatterEntry)

		err := rows.Scan(
			&entry.ID,
			&entry.ArtifactID,
			&entry.Message,
			&entry.IsAutoEntry,
			&entry.EntryType,
			&entry.CreatedBy,
			&entry.AuthorName,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// Delete removes a chatter entry
func (r *ChatterRepository) Delete(id string) error {
	query := `DELETE FROM chatter WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}
