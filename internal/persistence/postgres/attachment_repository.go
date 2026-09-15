package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// figureColumns is the attachment projection every read shares. figure_ref and
// figure_num are NULL on rows whose artifact had no reference to build on, so
// both scan through nullable holders.
const figureColumns = `id, artifact_id, filename, original_filename, title, mime_type, file_path, file_size, figure_ref, figure_num, version, created_at`

// prefixedFigureColumns is figureColumns qualified by a table alias, for the
// queries that join attachments to artifacts and so cannot use bare names.
func prefixedFigureColumns(alias string) string {
	cols := strings.Split(figureColumns, ", ")
	for i, c := range cols {
		cols[i] = alias + "." + c
	}
	return strings.Join(cols, ", ")
}

func scanAttachment(scan func(...interface{}) error) (*attachments.Attachment, error) {
	a := new(attachments.Attachment)
	var figureRef sql.NullString
	var figureNum sql.NullInt64
	if err := scan(
		&a.ID, &a.ArtifactID, &a.Filename, &a.OriginalFilename, &a.Title, &a.MimeType,
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
		INSERT INTO attachments (id, artifact_id, filename, original_filename, title, mime_type, file_path, file_size, version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	if attachment.Version < 1 {
		attachment.Version = 1
	}
	_, err := r.db.Exec(query,
		attachment.ID,
		attachment.ArtifactID,
		attachment.Filename,
		attachment.OriginalFilename,
		attachment.Title,
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

// FindByProjectID retrieves every attachment in a project, joined through the
// artifacts that hold them, in the order a reader meets them: artifact
// document order, then figure number.
//
// It serves cross-artifact figure references, which need the project's figures
// as one list rather than one artifact's at a time.
func (r *AttachmentRepository) FindByProjectID(projectID string) ([]*attachments.Attachment, error) {
	query := `
		SELECT ` + prefixedFigureColumns("a") + `
		FROM attachments a
		JOIN artifacts art ON art.id = a.artifact_id
		WHERE art.project_id = $1 AND art.valid_to IS NULL
		ORDER BY art.sort_order, art.created_at, a.figure_num NULLS LAST, a.created_at
	`
	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to find the project's attachments: %w", err)
	}
	defer rows.Close()

	var result []*attachments.Attachment
	for rows.Next() {
		attachment, err := scanAttachment(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("failed to scan attachment: %w", err)
		}
		result = append(result, attachment)
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
			(id, artifact_id, filename, original_filename, title, mime_type, file_path, file_size, figure_ref, figure_num, version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		attachment.ID, attachment.ArtifactID, attachment.Filename, attachment.OriginalFilename,
		attachment.Title, attachment.MimeType, attachment.FilePath, attachment.FileSize,
		nullString(attachment.FigureRef), nullInt(attachment.FigureNum),
		attachment.Version, attachment.CreatedAt,
	); err != nil {
		return fmt.Errorf("failed to save figure: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO attachment_versions
			(id, attachment_id, version, filename, original_filename, title, mime_type, file_path, file_size, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		uuid.New().String(), attachment.ID, attachment.Version, attachment.Filename,
		attachment.OriginalFilename, attachment.Title, attachment.MimeType, attachment.FilePath,
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
		RETURNING version, figure_ref, title
	`, attachmentID, v.OriginalFilename, v.MimeType, v.FilePath, v.FileSize).Scan(&next, &figureRef, &v.Title); err != nil {
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
	if err := insertVersion(tx, v); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

// Rename records a new title as a new version over the figure's current
// image: the attachment's version advances and the version row copies the
// current file fields, so the history reads as one line per change whether
// the change was the picture or the name.
func (r *AttachmentRepository) Rename(attachmentID, title string, by *string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin figure rename transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	v := &attachments.Version{AttachmentID: attachmentID, Title: title, CreatedBy: by, CreatedAt: time.Now()}
	if err := tx.QueryRow(`
		UPDATE attachments
		SET version = version + 1,
		    title = $2
		WHERE id = $1
		RETURNING version, filename, original_filename, mime_type, file_path, file_size
	`, attachmentID, title).Scan(&v.Version, &v.Filename, &v.OriginalFilename, &v.MimeType, &v.FilePath, &v.FileSize); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to rename figure: %w", err)
	}
	v.ID = uuid.New().String()
	if err := insertVersion(tx, v); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return v.Version, nil
}

func insertVersion(tx *sql.Tx, v *attachments.Version) error {
	if _, err := tx.Exec(`
		INSERT INTO attachment_versions
			(id, attachment_id, version, filename, original_filename, title, mime_type, file_path, file_size, created_by, created_at, restored_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		v.ID, v.AttachmentID, v.Version, v.Filename, v.OriginalFilename, v.Title,
		v.MimeType, v.FilePath, v.FileSize, v.CreatedBy, v.CreatedAt, v.RestoredFrom,
	); err != nil {
		return fmt.Errorf("failed to record figure version: %w", err)
	}
	return nil
}

const versionColumns = `id, attachment_id, version, filename, original_filename, title, mime_type, file_path, file_size, created_by, created_at, restored_from`

func scanVersion(scan func(...interface{}) error) (*attachments.Version, error) {
	v := new(attachments.Version)
	var createdBy sql.NullString
	var restoredFrom sql.NullInt64
	if err := scan(
		&v.ID, &v.AttachmentID, &v.Version, &v.Filename, &v.OriginalFilename, &v.Title,
		&v.MimeType, &v.FilePath, &v.FileSize, &createdBy, &v.CreatedAt, &restoredFrom,
	); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		id := createdBy.String
		v.CreatedBy = &id
	}
	if restoredFrom.Valid {
		n := int(restoredFrom.Int64)
		v.RestoredFrom = &n
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

// Restore brings an older version's image and title back as a new version.
//
// It reuses the older version's stored file rather than copying it: every
// version's file is already kept on disk for the life of the figure and
// nothing deletes them individually, so two version rows pointing at one
// path is cheaper and cannot drift. The consequence is that a restore and a
// re-upload of the same file look identical by path, which is why the new
// row records restored_from.
//
// The whole thing is one transaction: a figure whose attachment row moved
// on without a matching version row would be a figure with no record of
// what it currently shows.
func (r *AttachmentRepository) Restore(attachmentID string, version int, by *string) (*attachments.Version, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin figure restore transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the figure first so a concurrent upload cannot advance the
	// version between the check below and the write.
	var current int
	var figureRef sql.NullString
	if err := tx.QueryRow(`
		SELECT version, figure_ref FROM attachments WHERE id = $1 FOR UPDATE
	`, attachmentID).Scan(&current, &figureRef); err != nil {
		if err == sql.ErrNoRows {
			return nil, attachments.ErrNoSuchVersion
		}
		return nil, fmt.Errorf("failed to read figure for restore: %w", err)
	}
	if version == current {
		return nil, attachments.ErrAlreadyCurrent
	}

	source, err := scanVersion(tx.QueryRow(`
		SELECT `+versionColumns+`
		FROM attachment_versions
		WHERE attachment_id = $1 AND version = $2
	`, attachmentID, version).Scan)
	if err == sql.ErrNoRows {
		return nil, attachments.ErrNoSuchVersion
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the version being restored: %w", err)
	}

	// Title travels with the image. A restore that brought back the drawing
	// but left a later rename in place would show one figure under another
	// figure's name.
	var next int
	if err := tx.QueryRow(`
		UPDATE attachments
		SET version = version + 1,
		    original_filename = $2,
		    title = $3,
		    mime_type = $4,
		    file_path = $5,
		    file_size = $6
		WHERE id = $1
		RETURNING version
	`, attachmentID, source.OriginalFilename, source.Title, source.MimeType,
		source.FilePath, source.FileSize).Scan(&next); err != nil {
		return nil, fmt.Errorf("failed to advance figure version on restore: %w", err)
	}

	// The stored name follows the figure, as it does for an upload, so every
	// version is served under the same name.
	filename := source.Filename
	if figureRef.String != "" {
		filename = attachments.FigureFilename(figureRef.String, source.OriginalFilename)
		if _, err := tx.Exec(`UPDATE attachments SET filename = $2 WHERE id = $1`, attachmentID, filename); err != nil {
			return nil, fmt.Errorf("failed to rename figure on restore: %w", err)
		}
	}

	restored := &attachments.Version{
		ID:               uuid.New().String(),
		AttachmentID:     attachmentID,
		Version:          next,
		Filename:         filename,
		OriginalFilename: source.OriginalFilename,
		Title:            source.Title,
		MimeType:         source.MimeType,
		FilePath:         source.FilePath,
		FileSize:         source.FileSize,
		CreatedBy:        by,
		CreatedAt:        time.Now(),
		RestoredFrom:     &version,
	}
	if err := insertVersion(tx, restored); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return restored, nil
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
