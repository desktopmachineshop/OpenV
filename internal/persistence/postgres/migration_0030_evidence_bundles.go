package postgres

import "database/sql"

// 0030: evidence bundles — what a physical or manual test produced
// (REQ-121).
//
// A bundle is one capture session, owned by the PROJECT rather than by a
// run, because one long run on a rig routinely answers several test cases
// at once and may still stand when the campaign is repeated later. The
// many-to-many that expresses this is evidence_citations.
//
// A citation targets test_results.id rather than a test case. That id is
// stable across re-records — the result upsert is
// ON CONFLICT (run_id, test_case_id) DO UPDATE, so editing a status or a
// note keeps the row — which is exactly the property a citation needs, and
// exactly what the old evidence array on the result did NOT have: it was
// overwritten on every upsert.
//
// evidence_files is deliberately not attachments. An attachment is a
// numbered figure that must hang off an artifact and must be an image;
// a dataset is none of those. The sha256 is recorded at upload so the
// bytes can be checked against the record years later.
//
// Bundle refs (EVD-1) come from artifact_ref_counters, the existing
// per-project counter keyed by (project_id, prefix). Reusing it rather
// than adding a parallel table means the workspace-deletion sweep that
// already clears those counters clears these too.
func m0030EvidenceBundles(tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS evidence_bundles (
				id UUID PRIMARY KEY,
				project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				ref VARCHAR(32) NOT NULL,
				title VARCHAR(200) NOT NULL,
				summary TEXT NOT NULL DEFAULT '',
				captured_at TIMESTAMP,
				captured_by VARCHAR(200) NOT NULL DEFAULT '',
				conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
				created_by UUID,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL
			)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_evidence_bundles_ref
				ON evidence_bundles (project_id, ref)`,
		// The list view orders by capture time and falls back to when the
		// row was written, for a bundle whose capture date nobody
		// recorded.
		`CREATE INDEX IF NOT EXISTS idx_evidence_bundles_project
				ON evidence_bundles (project_id, COALESCE(captured_at, created_at) DESC)`,

		`CREATE TABLE IF NOT EXISTS evidence_files (
				id UUID PRIMARY KEY,
				bundle_id UUID NOT NULL REFERENCES evidence_bundles(id) ON DELETE CASCADE,
				filename VARCHAR(512) NOT NULL,
				mime_type VARCHAR(255) NOT NULL DEFAULT '',
				file_path TEXT NOT NULL,
				file_size BIGINT NOT NULL DEFAULT 0,
				sha256 VARCHAR(64) NOT NULL DEFAULT '',
				uploaded_by UUID,
				created_at TIMESTAMP NOT NULL
			)`,
		`CREATE INDEX IF NOT EXISTS idx_evidence_files_bundle
				ON evidence_files (bundle_id, created_at)`,

		`CREATE TABLE IF NOT EXISTS evidence_citations (
				id UUID PRIMARY KEY,
				bundle_id UUID NOT NULL REFERENCES evidence_bundles(id) ON DELETE CASCADE,
				test_result_id UUID NOT NULL REFERENCES test_results(id) ON DELETE CASCADE,
				note TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMP NOT NULL
			)`,
		// One claim per pair: citing the same bundle twice from one
		// result is the state the caller already has, not a second fact.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_evidence_citations_pair
				ON evidence_citations (bundle_id, test_result_id)`,
		// The run view resolves citations by result, so that direction
		// gets its own index.
		`CREATE INDEX IF NOT EXISTS idx_evidence_citations_result
				ON evidence_citations (test_result_id)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
