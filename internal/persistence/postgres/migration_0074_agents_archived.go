package postgres

import "database/sql"

// 0074: an agent can be archived (agents.Agent.Archived): retired from the
// Agents page and from running, with its run history kept, which a delete
// would cascade away. The flat seeded team (FeatureFlatAgentTeam) archives
// the agents it replaces rather than deleting them.
func m0074AgentsArchived(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE agents ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT FALSE`)
	return err
}
