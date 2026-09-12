package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/evidence"
)

// EvidenceRepository stores evidence bundles, their files and the citations
// that tie them to recorded test results.
type EvidenceRepository struct {
	db *sql.DB
}

// NewEvidenceRepository creates the repository.
func NewEvidenceRepository(db *sql.DB) *EvidenceRepository {
	return &EvidenceRepository{db: db}
}

// bundleColumns is the select list shared by every single-bundle read.
const bundleColumns = `id, project_id, ref, title, summary, captured_at,
	captured_by, conditions, created_by, created_at, updated_at`

func scanBundle(row interface{ Scan(...interface{}) error }) (*evidence.Bundle, error) {
	b := &evidence.Bundle{}
	var conditionsJSON []byte
	var createdBy sql.NullString
	if err := row.Scan(&b.ID, &b.ProjectID, &b.Ref, &b.Title, &b.Summary, &b.CapturedAt,
		&b.CapturedBy, &conditionsJSON, &createdBy, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		id := createdBy.String
		b.CreatedBy = &id
	}
	b.Conditions = map[string]interface{}{}
	if len(conditionsJSON) > 0 {
		if err := json.Unmarshal(conditionsJSON, &b.Conditions); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// Create mints the bundle's reference and stores it in one transaction, so a
// number is never drawn for a row that is not written.
func (r *EvidenceRepository) Create(b *evidence.Bundle) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if b.Ref == "" {
		// The same per-project counter the artifact references use, under the
		// EVD prefix. RETURNING next_num - 1 hands back the number this call
		// claimed, which is what makes concurrent creates safe.
		var n int
		if err := tx.QueryRow(`
			INSERT INTO artifact_ref_counters (project_id, prefix, next_num)
			VALUES ($1, $2, 2)
			ON CONFLICT (project_id, prefix)
			DO UPDATE SET next_num = artifact_ref_counters.next_num + 1
			RETURNING next_num - 1
		`, b.ProjectID, evidence.RefPrefix).Scan(&n); err != nil {
			return err
		}
		b.Ref = fmt.Sprintf("%s-%d", evidence.RefPrefix, n)
	}

	conditionsJSON, err := json.Marshal(b.Conditions)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO evidence_bundles
			(id, project_id, ref, title, summary, captured_at, captured_by,
			 conditions, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, b.ID, b.ProjectID, b.Ref, b.Title, b.Summary, b.CapturedAt, b.CapturedBy,
		conditionsJSON, b.CreatedBy, b.CreatedAt, b.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// FindByID returns one bundle, or (nil, nil) when no row matches.
func (r *EvidenceRepository) FindByID(id string) (*evidence.Bundle, error) {
	b, err := scanBundle(r.db.QueryRow(`SELECT `+bundleColumns+` FROM evidence_bundles WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListByProject returns the project's bundles newest capture first, each
// carrying its file count and total size so the list view needs no second
// query. A bundle whose capture date was never recorded sorts by when it was
// written, which is the closest thing to the truth available.
func (r *EvidenceRepository) ListByProject(projectID string) ([]*evidence.Bundle, error) {
	rows, err := r.db.Query(`
		SELECT b.id, b.project_id, b.ref, b.title, b.summary, b.captured_at,
		       b.captured_by, b.conditions, b.created_by, b.created_at, b.updated_at,
		       COUNT(f.id), COALESCE(SUM(f.file_size), 0)
		FROM evidence_bundles b
		LEFT JOIN evidence_files f ON f.bundle_id = b.id
		WHERE b.project_id = $1
		GROUP BY b.id
		ORDER BY COALESCE(b.captured_at, b.created_at) DESC, b.id DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*evidence.Bundle
	for rows.Next() {
		b := &evidence.Bundle{}
		var conditionsJSON []byte
		var createdBy sql.NullString
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Ref, &b.Title, &b.Summary, &b.CapturedAt,
			&b.CapturedBy, &conditionsJSON, &createdBy, &b.CreatedAt, &b.UpdatedAt,
			&b.FileCount, &b.TotalSize); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			id := createdBy.String
			b.CreatedBy = &id
		}
		b.Conditions = map[string]interface{}{}
		if len(conditionsJSON) > 0 {
			if err := json.Unmarshal(conditionsJSON, &b.Conditions); err != nil {
				return nil, err
			}
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Update replaces a bundle's editable fields. The ref and the project are not
// among them: a citable reference that moved would break every document that
// quoted it.
func (r *EvidenceRepository) Update(b *evidence.Bundle) error {
	conditionsJSON, err := json.Marshal(b.Conditions)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(`
		UPDATE evidence_bundles
		SET title = $2, summary = $3, captured_at = $4, captured_by = $5,
		    conditions = $6, updated_at = $7
		WHERE id = $1
	`, b.ID, b.Title, b.Summary, b.CapturedAt, b.CapturedBy, conditionsJSON, b.UpdatedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return evidence.ErrNotFound
	}
	return nil
}

// Delete removes the bundle and returns the file rows it held, so the caller
// can remove their bytes from disk once the transaction has committed. Files
// and citations go with it by ON DELETE CASCADE.
func (r *EvidenceRepository) Delete(id string) ([]*evidence.File, error) {
	files, err := r.ListFiles(id)
	if err != nil {
		return nil, err
	}
	res, err := r.db.Exec(`DELETE FROM evidence_bundles WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, evidence.ErrNotFound
	}
	return files, nil
}

// AddFile records one uploaded file against a bundle.
func (r *EvidenceRepository) AddFile(f *evidence.File) error {
	_, err := r.db.Exec(`
		INSERT INTO evidence_files
			(id, bundle_id, filename, mime_type, file_path, file_size, sha256, uploaded_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, f.ID, f.BundleID, f.Filename, f.MimeType, f.FilePath, f.FileSize, f.SHA256, f.UploadedBy, f.CreatedAt)
	return err
}

const fileColumns = `id, bundle_id, filename, mime_type, file_path, file_size,
	sha256, uploaded_by, created_at`

func scanFile(row interface{ Scan(...interface{}) error }) (*evidence.File, error) {
	f := &evidence.File{}
	var uploadedBy sql.NullString
	if err := row.Scan(&f.ID, &f.BundleID, &f.Filename, &f.MimeType, &f.FilePath,
		&f.FileSize, &f.SHA256, &uploadedBy, &f.CreatedAt); err != nil {
		return nil, err
	}
	if uploadedBy.Valid {
		id := uploadedBy.String
		f.UploadedBy = &id
	}
	return f, nil
}

// FindFileByID returns one file row, or (nil, nil) when no row matches.
func (r *EvidenceRepository) FindFileByID(id string) (*evidence.File, error) {
	f, err := scanFile(r.db.QueryRow(`SELECT `+fileColumns+` FROM evidence_files WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ListFiles returns a bundle's files in upload order.
func (r *EvidenceRepository) ListFiles(bundleID string) ([]*evidence.File, error) {
	rows, err := r.db.Query(`SELECT `+fileColumns+` FROM evidence_files WHERE bundle_id = $1 ORDER BY created_at, id`, bundleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*evidence.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteFile removes one file's row and returns it, so the caller can remove
// the bytes. (nil, nil) when no row matched.
func (r *EvidenceRepository) DeleteFile(id string) (*evidence.File, error) {
	f, err := scanFile(r.db.QueryRow(`DELETE FROM evidence_files WHERE id = $1 RETURNING `+fileColumns, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// AddCitation records a result's claim on a bundle. A repeat of a citation
// that already exists reports ErrAlreadyCited rather than failing: the caller
// asked for a state that already holds.
func (r *EvidenceRepository) AddCitation(c *evidence.Citation) error {
	_, err := r.db.Exec(`
		INSERT INTO evidence_citations (id, bundle_id, test_result_id, note, created_at)
		VALUES ($1,$2,$3,$4,$5)
	`, c.ID, c.BundleID, c.TestResultID, c.Note, c.CreatedAt)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return evidence.ErrAlreadyCited
	}
	return err
}

// RemoveCitation drops one claim. Removing a citation never touches the
// bundle: the evidence still exists, this result simply no longer rests on it.
func (r *EvidenceRepository) RemoveCitation(bundleID, testResultID string) error {
	_, err := r.db.Exec(`
		DELETE FROM evidence_citations WHERE bundle_id = $1 AND test_result_id = $2
	`, bundleID, testResultID)
	return err
}

// citationSelect joins through the result to the test case and the run, so a
// citation can be rendered without the caller resolving four more ids.
const citationSelect = `
	SELECT c.id, c.bundle_id, c.test_result_id, c.note, c.created_at,
	       b.ref, b.title,
	       r.test_case_id,
	       COALESCE(a.title, ''), COALESCE(a.ref, ''),
	       r.run_id, COALESCE(tr.name, '')
	FROM evidence_citations c
	JOIN evidence_bundles b ON b.id = c.bundle_id
	JOIN test_results r ON r.id = c.test_result_id
	-- artifacts is temporal (PRIMARY KEY (id, version)), so this join must be
	-- pinned to the live row or one citation would come back once per version
	-- of its test case.
	LEFT JOIN artifacts a ON a.id = r.test_case_id AND a.valid_to IS NULL
	LEFT JOIN test_runs tr ON tr.id = r.run_id
`

func scanCitations(rows *sql.Rows) ([]*evidence.Citation, error) {
	defer rows.Close()
	var out []*evidence.Citation
	for rows.Next() {
		c := &evidence.Citation{}
		if err := rows.Scan(&c.ID, &c.BundleID, &c.TestResultID, &c.Note, &c.CreatedAt,
			&c.BundleRef, &c.BundleTitle, &c.TestCaseID, &c.TestCaseTitle, &c.TestCaseRef,
			&c.RunID, &c.RunName); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCitationsForBundle returns the results that cite a bundle — the "what
// does this capture support?" direction, which is what makes a bundle worth
// having its own page.
func (r *EvidenceRepository) ListCitationsForBundle(bundleID string) ([]*evidence.Citation, error) {
	rows, err := r.db.Query(citationSelect+` WHERE c.bundle_id = $1 ORDER BY c.created_at, c.id`, bundleID)
	if err != nil {
		return nil, err
	}
	return scanCitations(rows)
}

// ListCitationsForResult returns the bundles one result cites.
func (r *EvidenceRepository) ListCitationsForResult(testResultID string) ([]*evidence.Citation, error) {
	rows, err := r.db.Query(citationSelect+` WHERE c.test_result_id = $1 ORDER BY c.created_at, c.id`, testResultID)
	if err != nil {
		return nil, err
	}
	return scanCitations(rows)
}

// ListCitationsForRun returns every citation in a run keyed by test result id,
// in one query, so the run grid renders its evidence column without a request
// per row.
func (r *EvidenceRepository) ListCitationsForRun(runID string) (map[string][]*evidence.Citation, error) {
	rows, err := r.db.Query(citationSelect+` WHERE r.run_id = $1 ORDER BY c.created_at, c.id`, runID)
	if err != nil {
		return nil, err
	}
	list, err := scanCitations(rows)
	if err != nil {
		return nil, err
	}
	out := map[string][]*evidence.Citation{}
	for _, c := range list {
		out[c.TestResultID] = append(out[c.TestResultID], c)
	}
	return out, nil
}

// StorageUsedByOrg totals every evidence file in the workspace. The quota is
// measured per workspace rather than per project because the disk it protects
// is one shared volume.
func (r *EvidenceRepository) StorageUsedByOrg(orgID string) (int64, error) {
	var total int64
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(f.file_size), 0)
		FROM evidence_files f
		JOIN evidence_bundles b ON b.id = f.bundle_id
		JOIN projects p ON p.id = b.project_id
		WHERE p.org_id = $1
	`, orgID).Scan(&total)
	return total, err
}

// ProjectForResult resolves the project a recorded result sits in, by way of
// its run. ("", nil) when no such result exists.
func (r *EvidenceRepository) ProjectForResult(testResultID string) (string, error) {
	var projectID string
	err := r.db.QueryRow(`
		SELECT tr.project_id
		FROM test_results res
		JOIN test_runs tr ON tr.id = res.run_id
		WHERE res.id = $1
	`, testResultID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return projectID, err
}

// ProjectOrg resolves the workspace a project belongs to.
func (r *EvidenceRepository) ProjectOrg(projectID string) (string, error) {
	var orgID sql.NullString
	err := r.db.QueryRow(`SELECT org_id FROM projects WHERE id = $1`, projectID).Scan(&orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return orgID.String, nil
}
