package postgres

import (
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// AttachmentRepository implements attachments.Repository using PostgreSQL
type AttachmentRepository struct {
	db *sql.DB
}

// NewAttachmentRepository creates a new attachment repository
func NewAttachmentRepository(db *sql.DB) attachments.Repository {
	return &AttachmentRepository{db: db}
}

// Save persists an attachment
func (r *AttachmentRepository) Save(attachment *attachments.Attachment) error {
	query := `
		INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.Exec(query,
		attachment.ID,
		attachment.ArtifactID,
		attachment.Filename,
		attachment.MimeType,
		attachment.FilePath,
		attachment.FileSize,
		attachment.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save attachment: %w", err)
	}
	return nil
}

// FindByID retrieves an attachment by ID
func (r *AttachmentRepository) FindByID(id string) (*attachments.Attachment, error) {
	attachment := &attachments.Attachment{}
	query := `
		SELECT id, artifact_id, filename, mime_type, file_path, file_size, created_at
		FROM attachments
		WHERE id = $1
	`
	err := r.db.QueryRow(query, id).Scan(
		&attachment.ID,
		&attachment.ArtifactID,
		&attachment.Filename,
		&attachment.MimeType,
		&attachment.FilePath,
		&attachment.FileSize,
		&attachment.CreatedAt,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to find attachment: %w", err)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return attachment, nil
}

// FindByArtifactID retrieves all attachments for an artifact
func (r *AttachmentRepository) FindByArtifactID(artifactID string) ([]*attachments.Attachment, error) {
	query := `
		SELECT id, artifact_id, filename, mime_type, file_path, file_size, created_at
		FROM attachments
		WHERE artifact_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(query, artifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to find attachments: %w", err)
	}
	defer rows.Close()

	var attachmentList []*attachments.Attachment
	for rows.Next() {
		attachment := new(attachments.Attachment)
		err := rows.Scan(
			&attachment.ID,
			&attachment.ArtifactID,
			&attachment.Filename,
			&attachment.MimeType,
			&attachment.FilePath,
			&attachment.FileSize,
			&attachment.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan attachment: %w", err)
		}
		attachmentList = append(attachmentList, attachment)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("attachment rows error: %w", err)
	}

	return attachmentList, nil
}

// FindByArtifactIDs retrieves attachments for many artifacts in a single
// query, grouped by artifact ID. Within each artifact the ordering matches
// FindByArtifactID (newest first); artifacts with no attachments are absent
// from the returned map.
func (r *AttachmentRepository) FindByArtifactIDs(artifactIDs []string) (map[string][]*attachments.Attachment, error) {
	result := make(map[string][]*attachments.Attachment, len(artifactIDs))
	if len(artifactIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT id, artifact_id, filename, mime_type, file_path, file_size, created_at
		FROM attachments
		WHERE artifact_id = ANY($1)
		ORDER BY artifact_id, created_at DESC
	`
	rows, err := r.db.Query(query, pq.Array(artifactIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to find attachments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		attachment := new(attachments.Attachment)
		if err := rows.Scan(
			&attachment.ID,
			&attachment.ArtifactID,
			&attachment.Filename,
			&attachment.MimeType,
			&attachment.FilePath,
			&attachment.FileSize,
			&attachment.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan attachment: %w", err)
		}
		result[attachment.ArtifactID] = append(result[attachment.ArtifactID], attachment)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("attachment rows error: %w", err)
	}

	return result, nil
}

// Delete removes an attachment
func (r *AttachmentRepository) Delete(id string) error {
	query := `DELETE FROM attachments WHERE id = $1`
	_, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete attachment: %w", err)
	}
	return nil
}
