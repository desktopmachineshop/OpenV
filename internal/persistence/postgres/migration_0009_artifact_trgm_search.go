package postgres

import (
	"database/sql"
	"log/slog"
)

// 0009: trigram search index for cross-project artifact search (issue
// #182). gin_trgm_ops makes the ILIKE predicate index-assisted. CREATE
// EXTENSION needs privilege some managed roles lack, so we probe and, when
// it is absent and uncreatable, roll back to a savepoint and skip the
// indexes (search keeps working via the seq-scan fallback) rather than
// bricking boot.
func m0009ArtifactTrgmSearch(tx *sql.Tx) error {
	var haveTrgm bool
	if err := tx.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`,
	).Scan(&haveTrgm); err != nil {
		return err
	}
	if !haveTrgm {
		if _, err := tx.Exec(`SAVEPOINT trgm_ext`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
			if _, rbErr := tx.Exec(`ROLLBACK TO SAVEPOINT trgm_ext`); rbErr != nil {
				return rbErr
			}
			slog.Warn("pg_trgm extension unavailable; skipping artifact trigram search indexes (cross-project search falls back to a sequential scan)",
				slog.Any("error", err))
			return nil
		}
		if _, err := tx.Exec(`RELEASE SAVEPOINT trgm_ext`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifacts_title_trgm
			ON artifacts USING gin (title gin_trgm_ops)
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifacts_body_trgm
			ON artifacts USING gin (body gin_trgm_ops)
		`)
	return err
}
