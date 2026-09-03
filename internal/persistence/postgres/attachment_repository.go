package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// figureColumns is the attachment projection every read shares. figure_ref and
// figure_num are NULL on rows whose artifact had no reference to build on, so
// both scan through nullable holders.
const figureColumns = `id, artifact_id, filename, original_filename, mime_type, file_path, file_size, figure_ref, figure_num, version, created_at`

func scanAttachment(scan func(...interface{}) error) (*attachments.Attachment, error) {
	a := new(attachments.Attachment)
	var figureRef sql.NullString
	var figureNum sql.NullInt64
	if err := scan(
		&a.ID, &a.ArtifactID, &a.Filename, &a.OriginalFilename, &a.MimeType,
		&a.FilePath, &a.FileSize, &figureRef, &figureNum, &a.Version, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	a.FigureRef = figureRef.String
	a.FigureNum = int(figureNum.Int64)
	return a, nil
}

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
		INSERT INTO attachments (id, artifact_id, filename, original_filename, mime_type, file_path, file_size, version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if attachment.Version < 1 {
		attachment.Version = 1
	}
	_, err := r.db.Exec(query,
		attachment.ID,
		attachment.ArtifactID,
		attachment.Filename,
		attachment.OriginalFilename,
		attachment.MimeType,
		attachment.FilePath,
		attachment.FileSize,
		attachment.Version,
		attachment.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save attachment: %w", err)
	}
	return nil
}

// FindByID retrieves an attachment by ID
func (r *AttachmentRepository) FindByID(id string) (*attachments.Attachment, error) {
	query := `SELECT ` + figureColumns + ` FROM attachments WHERE id = $1`
	attachment, err := scanAttachment(r.db.QueryRow(query, id).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find attachment: %w", err)
	}
	return attachment, nil
}

// FindByArtifactID retrieves all attachments for an artifact
func (r *AttachmentRepository) FindByArtifactID(artifactID string) ([]*attachments.Attachment, error) {
	// Figures read in figure order — Figure 1 first — because that is the
	// order a reader cites them in. Unnumbered rows fall to the end.
	query := `
		SELECT ` + figureColumns + `
		FROM attachments
		WHERE artifact_id = $1
		ORDER BY figure_num NULLS LAST, created_at
	`
	rows, err := r.db.Query(query, artifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to find attachments: %w", err)
	}
	defer rows.Close()

	var attachmentList []*attachments.Attachment
	for rows.Next() {
		attachment, err := scanAttachment(rows.Scan)
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
// FindByArtifactID (figure order); artifacts with no attachments are absent
// from the returned map.
func (r *AttachmentRepository) FindByArtifactIDs(artifactIDs []string) (map[string][]*attachments.Attachment, error) {
	result := make(map[string][]*attachments.Attachment, len(artifactIDs))
	if len(artifactIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT ` + figureColumns + `
		FROM attachments
		WHERE artifact_id = ANY($1)
		ORDER BY artifact_id, figure_num NULLS LAST, created_at
	`
	rows, err := r.db.Query(query, pq.Array(artifactIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to find attachments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		attachment, err := scanAttachment(rows.Scan)
		if err != nil {
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

// SaveWithFigureRef stores a new figure, drawing its number from the
// artifact's counter inside the same transaction as the insert.
//
// The counter only ever moves forward — the upsert increments it whether or
// not the number ends up in use — so two concurrent uploads cannot be handed
// the same figure, and deleting a figure does not put its number back in
// circulation. An artifact with no stable reference yields no figure
// reference: there is nothing to build one from, and inventing a bare "FIG-1"
// would collide the moment the artifact got its ref.
func (r *AttachmentRepository) SaveWithFigureRef(attachment *attachments.Attachment, artifactRef string) error {
	if attachment.Version < 1 {
		attachment.Version = 1
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin figure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if artifactRef != "" {
		var num int
		if err := tx.QueryRow(`
			INSERT INTO attachment_figure_counters (artifact_id, next_num)
			VALUES ($1, 2)
			ON CONFLICT (artifact_id)
			DO UPDATE SET next_num = attachment_figure_counters.next_num + 1
			RETURNING next_num - 1
		`, attachment.ArtifactID).Scan(&num); err != nil {
			return fmt.Errorf("failed to allocate figure number: %w", err)
		}
		attachment.FigureNum = num
		attachment.FigureRef = attachments.FormatFigureRef(artifactRef, num)
		attachment.Filename = attachments.FigureFilename(attachment.FigureRef, attachment.OriginalFilename)
	}

	if _, err := tx.Exec(`
		INSERT INTO attachments
			(id, artifact_id, filename, original_filename, mime_type, file_path, file_size, figure_ref, figure_num, version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		attachment.ID, attachment.ArtifactID, attachment.Filename, attachment.OriginalFilename,
		attachment.MimeType, attachment.FilePath, attachment.FileSize,
		nullString(attachment.FigureRef), nullInt(attachment.FigureNum),
		attachment.Version, attachment.CreatedAt,
	); err != nil {
		return fmt.Errorf("failed to save figure: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO attachment_versions
			(id, attachment_id, version, filename, original_filename, mime_type, file_path, file_size, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		uuid.New().String(), attachment.ID, attachment.Version, attachment.Filename,
		attachment.OriginalFilename, attachment.MimeType, attachment.FilePath,
		attachment.FileSize, attachment.CreatedAt,
	); err != nil {
		return fmt.Errorf("failed to record the figure's first version: %w", err)
	}

	return tx.Commit()
}

// AddVersion supersedes a figure's file, keeping the figure reference and the
// earlier versions. The version number comes from the row itself rather than
// from a count, so a concurrent second upload cannot reuse it: the unique
// (attachment_id, version) index rejects the loser.
func (r *AttachmentRepository) AddVersion(attachmentID string, v *attachments.Version) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin figure version transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var figureRef sql.NullString
	var next int
	if err := tx.QueryRow(`
		UPDATE attachments
		SET version = version + 1,
		    original_filename = $2,
		    mime_type = $3,
		    file_path = $4,
		    file_size = $5
		WHERE id = $1
		RETURNING version, figure_ref
	`, attachmentID, v.OriginalFilename, v.MimeType, v.FilePath, v.FileSize).Scan(&next, &figureRef); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to advance figure version: %w", err)
	}

	// The stored name follows the figure, not the upload, so every version of
	// a figure is served under the same name.
	filename := v.Filename
	if figureRef.String != "" {
		filename = attachments.FigureFilename(figureRef.String, v.OriginalFilename)
		if _, err := tx.Exec(`UPDATE attachments SET filename = $2 WHERE id = $1`, attachmentID, filename); err != nil {
			return 0, fmt.Errorf("failed to rename figure: %w", err)
		}
	}

	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	v.ID = uuid.New().String()
	v.AttachmentID = attachmentID
	v.Version = next
	v.Filename = filename
	if _, err := tx.Exec(`
		INSERT INTO attachment_versions
			(id, attachment_id, version, filename, original_filename, mime_type, file_path, file_size, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		v.ID, v.AttachmentID, v.Version, v.Filename, v.OriginalFilename,
		v.MimeType, v.FilePath, v.FileSize, v.CreatedBy, v.CreatedAt,
	); err != nil {
		return 0, fmt.Errorf("failed to record figure version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

const versionColumns = `id, attachment_id, version, filename, original_filename, mime_type, file_path, file_size, created_by, created_at`

func scanVersion(scan func(...interface{}) error) (*attachments.Version, error) {
	v := new(attachments.Version)
	var createdBy sql.NullString
	if err := scan(
		&v.ID, &v.AttachmentID, &v.Version, &v.Filename, &v.OriginalFilename,
		&v.MimeType, &v.FilePath, &v.FileSize, &createdBy, &v.CreatedAt,
	); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		id := createdBy.String
		v.CreatedBy = &id
	}
	return v, nil
}

// ListVersions returns a figure's versions, newest first.
func (r *AttachmentRepository) ListVersions(attachmentID string) ([]*attachments.Version, error) {
	rows, err := r.db.Query(`
		SELECT `+versionColumns+`
		FROM attachment_versions
		WHERE attachment_id = $1
		ORDER BY version DESC
	`, attachmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list figure versions: %w", err)
	}
	defer rows.Close()

	var out []*attachments.Version
	for rows.Next() {
		v, err := scanVersion(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("failed to scan figure version: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("figure version rows error: %w", err)
	}
	return out, nil
}

// FindVersion returns one version of a figure, or nil when there is no such
// version.
func (r *AttachmentRepository) FindVersion(attachmentID string, version int) (*attachments.Version, error) {
	v, err := scanVersion(r.db.QueryRow(`
		SELECT `+versionColumns+`
		FROM attachment_versions
		WHERE attachment_id = $1 AND version = $2
	`, attachmentID, version).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find figure version: %w", err)
	}
	return v, nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
