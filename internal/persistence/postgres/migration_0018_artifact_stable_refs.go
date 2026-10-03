package postgres

import (
	"database/sql"
	"fmt"
)

func m0018ArtifactStableRefs(tx *sql.Tx) error {
	// Stable short refs ("REQ-12") on artifacts, plus the per-project
	// counters that mint them (see artifacts/ref.go and
	// ArtifactRepository.Save). The backfill numbers every project's
	// current artifacts in the stable tree order FindByProjectID uses,
	// then stamps ALL version rows of each artifact so a ref is
	// constant across history; soft-deleted artifacts keep a NULL ref.
	for _, stmt := range []string{
		`ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS ref VARCHAR(32)`,
		`CREATE TABLE IF NOT EXISTS artifact_ref_counters (
				project_id UUID NOT NULL,
				prefix VARCHAR(16) NOT NULL,
				next_num INT NOT NULL,
				PRIMARY KEY (project_id, prefix)
			)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	rows, err := tx.Query(`
			SELECT id, project_id, type FROM artifacts
			WHERE valid_to IS NULL AND ref IS NULL
			ORDER BY project_id, parent_id NULLS FIRST, sort_order ASC, created_at ASC, id ASC
		`)
	if err != nil {
		return err
	}
	type current struct{ id, project, typ string }
	var todo []current
	for rows.Next() {
		var c current
		if err := rows.Scan(&c.id, &c.project, &c.typ); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	counters := map[[2]string]int{}
	for _, c := range todo {
		prefix := backfillRefPrefix(c.typ)
		key := [2]string{c.project, prefix}
		counters[key]++
		ref := fmt.Sprintf("%s-%d", prefix, counters[key])
		if _, err := tx.Exec(
			`UPDATE artifacts SET ref = $1 WHERE id = $2`, ref, c.id,
		); err != nil {
			return err
		}
	}
	for key, n := range counters {
		if _, err := tx.Exec(`
				INSERT INTO artifact_ref_counters (project_id, prefix, next_num)
				VALUES ($1, $2, $3)
				ON CONFLICT (project_id, prefix)
				DO UPDATE SET next_num = GREATEST(artifact_ref_counters.next_num, $3)
			`, key[0], key[1], n+1); err != nil {
			return err
		}
	}

	_, err = tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_artifacts_project_ref
			ON artifacts (project_id, ref) WHERE valid_to IS NULL
		`)
	return err
}
