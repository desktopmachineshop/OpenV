package postgres

import "database/sql"

// 0055: the figure counters no artifact has (#379 question 48). A purge
// before #379's bug 138 deleted a workspace's artifacts and figures but not
// the counter each artifact numbers its figures by, so a counter row was
// left for every purged artifact that had a figure, naming an artifact no
// row has. Nothing reads such a row: a counter is read only when a figure
// is added to its artifact. They are deleted. A counter of an artifact that
// has a row, a deleted one's included, stays, so no figure number is ever
// issued twice.
func m0055FigureCountersWithoutArtifact(tx *sql.Tx) error {
	_, err := tx.Exec(`DELETE FROM attachment_figure_counters c
		WHERE NOT EXISTS (SELECT 1 FROM artifacts a WHERE a.id = c.artifact_id)`)
	return err
}
