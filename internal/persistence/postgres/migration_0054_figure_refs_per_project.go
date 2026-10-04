package postgres

import "database/sql"

// 0054: a figure reference is unique within its project, not across every
// project (#379 bug 142). 0023 made figure_ref unique on its own, but a
// figure reference is built on its artifact's reference ("REQ-1-FIG-1"),
// and an artifact reference is unique only within a project
// (idx_artifacts_project_ref), so a second project's first figure on its
// REQ-1 collided with the first project's REQ-1-FIG-1 and the upload
// failed. An attachment reaches its project only through its artifact, and
// the artifact's counter (attachment_figure_counters) numbers its figures,
// so the index is keyed by the artifact: no two figures of one artifact
// share a reference, and no two live artifacts of a project share the
// reference theirs are built on. Every set of rows the old index allowed
// the new one allows, so building it cannot fail on existing data.
func m0054FigureRefsPerProject(tx *sql.Tx) error {
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_attachments_figure_ref`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_attachments_artifact_figure_ref
			ON attachments(artifact_id, figure_ref) WHERE figure_ref IS NOT NULL`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
