package postgres

import "database/sql"

// 0023: images attached to an artifact become numbered figures.
//
// A figure carries a reference of its own — "REQ-17-FIG-1" — minted from a
// per-artifact counter that only ever moves forward, so deleting a figure
// does not free its number and no number is ever issued twice. That is the
// same contract artifact refs have (0018), for the same reason: a figure
// number appears in a report, a review comment, or a conversation months
// later, and must still mean the one image it meant then.
//
// attachments keeps holding the CURRENT file; attachment_versions records
// every version including the first, so an earlier drawing stays
// retrievable after it is superseded.
//
// The backfill numbers what is already there, oldest first, so existing
// images get figures rather than sitting outside the scheme. Attachments
// whose artifact never got a ref (soft-deleted rows the 0018 backfill
// skipped) are left alone: there is no artifact reference to build on.
func m0023ArtifactFigures(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE attachments
				ADD COLUMN IF NOT EXISTS figure_ref VARCHAR(64),
				ADD COLUMN IF NOT EXISTS figure_num INT,
				ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1,
				ADD COLUMN IF NOT EXISTS original_filename VARCHAR(512) NOT NULL DEFAULT ''
		`); err != nil {
		return err
	}
	// Unique among live figures, and the index is what stops two
	// concurrent uploads landing on one number if the counter is ever
	// bypassed.
	if _, err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_attachments_figure_ref
				ON attachments(figure_ref) WHERE figure_ref IS NOT NULL
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS attachment_figure_counters (
				artifact_id UUID PRIMARY KEY,
				next_num INT NOT NULL
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS attachment_versions (
				id UUID PRIMARY KEY,
				attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
				version INT NOT NULL,
				filename VARCHAR(512) NOT NULL,
				original_filename VARCHAR(512) NOT NULL DEFAULT '',
				mime_type VARCHAR(128) NOT NULL,
				file_path VARCHAR(1024) NOT NULL,
				file_size INT NOT NULL,
				created_by UUID,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				UNIQUE (attachment_id, version)
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_attachment_versions_attachment
				ON attachment_versions(attachment_id, version DESC)
		`); err != nil {
		return err
	}

	// Number existing images per artifact, oldest first; id breaks ties so
	// the ordering is deterministic when timestamps collide.
	if _, err := tx.Exec(`
			WITH numbered AS (
				SELECT a.id,
				       art.ref AS artifact_ref,
				       ROW_NUMBER() OVER (PARTITION BY a.artifact_id
				                          ORDER BY a.created_at, a.id) AS num
				FROM attachments a
				JOIN artifacts art ON art.id = a.artifact_id AND art.valid_to IS NULL
				WHERE a.figure_ref IS NULL AND art.ref IS NOT NULL AND art.ref <> ''
			)
			UPDATE attachments a
			SET figure_num = n.num,
			    figure_ref = n.artifact_ref || '-FIG-' || n.num,
			    original_filename = CASE WHEN a.original_filename = '' THEN a.filename ELSE a.original_filename END
			FROM numbered n
			WHERE a.id = n.id
		`); err != nil {
		return err
	}
	// Park each counter past the highest number handed out.
	if _, err := tx.Exec(`
			INSERT INTO attachment_figure_counters (artifact_id, next_num)
			SELECT artifact_id, MAX(figure_num) + 1
			FROM attachments
			WHERE figure_num IS NOT NULL
			GROUP BY artifact_id
			ON CONFLICT (artifact_id)
			DO UPDATE SET next_num = GREATEST(attachment_figure_counters.next_num, EXCLUDED.next_num)
		`); err != nil {
		return err
	}
	// Every existing image becomes version 1 of its figure.
	_, err := tx.Exec(`
			INSERT INTO attachment_versions
				(id, attachment_id, version, filename, original_filename, mime_type, file_path, file_size, created_at)
			SELECT gen_random_uuid(), a.id, 1, a.filename, a.original_filename, a.mime_type, a.file_path, a.file_size, a.created_at
			FROM attachments a
			WHERE a.figure_ref IS NOT NULL
			  AND NOT EXISTS (SELECT 1 FROM attachment_versions v WHERE v.attachment_id = a.id AND v.version = 1)
		`)
	return err
}
