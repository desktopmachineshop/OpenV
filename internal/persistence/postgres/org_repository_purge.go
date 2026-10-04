// OrgRepository's workspace deletion: soft delete and restore, the lists of
// deleted workspaces, and the hard purge.

package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// SoftDeleteOrg stamps deleted_at, hiding and locking the workspace.
func (r *OrgRepository) SoftDeleteOrg(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE organizations SET deleted_at = $2, updated_at = $2 WHERE id = $1`, id, at)
	return err
}

// RestoreOrg clears deleted_at.
func (r *OrgRepository) RestoreOrg(id string) error {
	_, err := r.db.Exec(`UPDATE organizations SET deleted_at = NULL, updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ListDeletedOrgsForUser returns the user's soft-deleted orgs with role.
func (r *OrgRepository) ListDeletedOrgsForUser(userID string) ([]*orgs.Org, error) {
	rows, err := r.db.Query(`
		SELECT `+orgColumnsQualified+`, m.role FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		WHERE m.user_id = $1 AND o.deleted_at IS NOT NULL
		ORDER BY o.deleted_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*orgs.Org
	for rows.Next() {
		var role string
		o, err := scanOrg(rows, &role)
		if err != nil {
			return nil, err
		}
		o.Role = role
		result = append(result, o)
	}
	return result, rows.Err()
}

// ListExpiredDeletedOrgIDs returns orgs soft-deleted before the cutoff.
func (r *OrgRepository) ListExpiredDeletedOrgIDs(before time.Time) ([]string, error) {
	rows, err := r.db.Query(`SELECT id FROM organizations WHERE deleted_at IS NOT NULL AND deleted_at < $1`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// PurgeOrg hard-deletes an organization and everything it contains, in one
// transaction. Most org- and project-scoped tables predate foreign keys, so
// the dependents that don't cascade are deleted explicitly, children before
// the artifacts/projects they hang off. Attachment rows go with their
// artifacts; the files on disk are not touched here (same as artifact
// deletion elsewhere in the app). The rows keyed by an artifact id, which no
// foreign key can reference, go with the workspace's artifacts, and a link's
// version records with the link (#379 bug 138: the figure counters, and a
// link's record at an end in another workspace, were left behind).
func (r *OrgRepository) PurgeOrg(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// artifact_embeddings only exists when the pgvector migration ran.
	var embeddingsTable sql.NullString
	if err := tx.QueryRow(`SELECT to_regclass('artifact_embeddings')::text`).Scan(&embeddingsTable); err != nil {
		return err
	}
	if embeddingsTable.Valid {
		if _, err := tx.Exec(`DELETE FROM artifact_embeddings WHERE artifact_id IN (`+orgArtifacts+`)`, id); err != nil {
			return err
		}
	}

	for _, stmt := range purgeOrgStatements {
		if _, err := tx.Exec(stmt, id); err != nil {
			return fmt.Errorf("purge org %s: %q: %w", id, stmt, err)
		}
	}
	return tx.Commit()
}

// orgProjects and orgArtifacts select a workspace's projects and their
// artifacts, the workspace id being $1, for PurgeOrg's statements.
const orgProjects = `SELECT id FROM projects WHERE org_id = $1`
const orgArtifacts = `SELECT DISTINCT id FROM artifacts WHERE project_id IN (` + orgProjects + `)`

// purgeOrgStatements is PurgeOrg's list, sent in this order after the
// artifact_embeddings statement, each with the workspace id as $1: children
// before the artifacts and projects they hang off, the workspace itself
// last. TestPurgeCatalog (migration_freeze_purge_test.go) pins the order.
var purgeOrgStatements = []string{
	`DELETE FROM chatter WHERE artifact_id IN (` + orgArtifacts + `)`,
	`DELETE FROM attachments WHERE artifact_id IN (` + orgArtifacts + `)`, // versions cascade
	`DELETE FROM attachment_figure_counters WHERE artifact_id IN (` + orgArtifacts + `)`,
	`DELETE FROM link_artifacts WHERE artifact_id IN (` + orgArtifacts + `) OR link_id IN (SELECT id FROM links WHERE from_id IN (` + orgArtifacts + `) OR to_id IN (` + orgArtifacts + `))`,
	`DELETE FROM links WHERE from_id IN (` + orgArtifacts + `) OR to_id IN (` + orgArtifacts + `)`,
	`DELETE FROM test_runs WHERE project_id IN (` + orgProjects + `)`,  // test_results cascade
	`DELETE FROM work_items WHERE project_id IN (` + orgProjects + `)`, // activity cascades
	`DELETE FROM interviews WHERE project_id IN (` + orgProjects + `)`, // invites/sessions/messages cascade
	`DELETE FROM attribute_definitions WHERE org_id = $1 OR project_id IN (` + orgProjects + `)`,
	`DELETE FROM artifact_ref_counters WHERE project_id IN (` + orgProjects + `)`,
	`DELETE FROM artifacts WHERE project_id IN (` + orgProjects + `)`,
	`DELETE FROM projects WHERE org_id = $1`, // baselines, product_profiles, repo_connections, project_members, team access cascade
	`DELETE FROM agent_runs WHERE org_id = $1`,
	`DELETE FROM agents WHERE org_id = $1`,      // remaining runs/proposals/team nodes cascade
	`DELETE FROM agent_teams WHERE org_id = $1`, // nodes/edges cascade
	`DELETE FROM automations WHERE org_id = $1`,
	`DELETE FROM guided_sessions WHERE org_id = $1`, // messages cascade
	`DELETE FROM domain_events WHERE org_id = $1`,
	`DELETE FROM notifications WHERE org_id = $1`,
	`DELETE FROM provider_settings WHERE org_id = $1`,
	`DELETE FROM provider_logins WHERE org_id = $1`,
	`DELETE FROM templates WHERE org_id = $1`,
	`DELETE FROM organizations WHERE id = $1`, // members, teams, worker keys, pairings, hosted workers cascade
}
