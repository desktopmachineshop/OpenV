package postgres

import "database/sql"

// 0017: pending-proposal artifact references (issue #235). A proposal-mode
// agent can now draft an artifact AND a link to it in one run: it attaches
// a caller-chosen temporary token to the create_artifact proposal, and a
// sibling create_link proposal names that token in from_id/to_id. This
// column holds the artifact proposal's token; the link side keeps its refs
// in the existing payload JSONB (no schema change there). At apply time the
// token resolves to the real artifact id (see proposals.resolveLinkPayload).
// Existing rows backfill to '' (no reference), the same as any proposal
// that mints no token. The column is nullable-free (NOT NULL DEFAULT '')
// so the scan path never deals with a NULL ref.

func m0017AgentProposalRef(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE agent_proposals
			ADD COLUMN ref TEXT NOT NULL DEFAULT ''
		`)
	return err
}
