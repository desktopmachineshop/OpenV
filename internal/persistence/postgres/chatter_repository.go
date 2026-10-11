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

// chatterColumns is the list the reads select and the INSERT writes, in the
// order scanChatter reads it (refactor plan X13).
const chatterColumns = `id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at`

// scanChatter scans one row of chatterColumns.
func scanChatter(row rowScanner) (*chatter.ChatterEntry, error) {
	entry := new(chatter.ChatterEntry)
	err := row.Scan(
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
	return entry, nil
}

// Save saves a chatter entry to the database
func (r *ChatterRepository) Save(entry *chatter.ChatterEntry) error {
	query := `
		INSERT INTO chatter (` + chatterColumns + `)
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
// FindByID retrieves one chatter entry.
func (r *ChatterRepository) FindByID(id string) (*chatter.ChatterEntry, error) {
	query := `
		SELECT ` + chatterColumns + `
		FROM chatter
		WHERE id = $1
	`

	entry, err := scanChatter(r.db.QueryRow(query, id))
	if noRow(err) {
		return nil, chatter.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func (r *ChatterRepository) FindByArtifactID(artifactID string) ([]*chatter.ChatterEntry, error) {
	query := `
		SELECT ` + chatterColumns + `
		FROM chatter
		WHERE artifact_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, artifactID)
	if malformedID(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*chatter.ChatterEntry
	for rows.Next() {
		entry, err := scanChatter(rows)

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
