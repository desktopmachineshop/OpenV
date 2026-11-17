package postgres

import "database/sql"

// 0070: the agent hierarchy (issue 733). The Agents page shows a
// workspace's agents as a tree of cards: each agent may report to another
// agent of its workspace (parent_agent_id), sit in a named group
// (group_name, which also colours its card) and take a place among its
// siblings (sort_order). It is display and arrangement only: no agent
// starts another. The agent's markdown file is the source of truth
// (reports_to, group and sort_order in its frontmatter); these columns
// mirror it, the parent resolved from the slug the file names. Deleting the
// parent leaves the child at the top (ON DELETE SET NULL). The rows go with
// their workspace through the purge's agents entry, as before.
func m0070AgentHierarchy(tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS parent_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL`,
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS group_name VARCHAR(120)`,
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_agent_id) WHERE parent_agent_id IS NOT NULL`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
