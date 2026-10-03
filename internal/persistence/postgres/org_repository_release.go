// OrgRepository's release settings: the channel, the stable release, the
// upgrade window, the lists by effective channel, and a member's early
// switch to the next stable release.

package postgres

import (
	"github.com/lib/pq"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// SetReleaseChannel writes only release_channel ("" is the plan default).
func (r *OrgRepository) SetReleaseChannel(orgID, channel string) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET release_channel = $2, updated_at = NOW() WHERE id = $1
	`, orgID, channel)
	return err
}

// SetStableRelease writes only stable_release.
func (r *OrgRepository) SetStableRelease(orgID, version string) error {
	_, err := r.db.Exec(`UPDATE organizations SET stable_release = $2, updated_at = NOW() WHERE id = $1`, orgID, version)
	return err
}

// SetUpgradeWindow writes only the upgrade window columns.
func (r *OrgRepository) SetUpgradeWindow(orgID string, day, hour int, timezone string) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET upgrade_day = $2, upgrade_hour = $3, upgrade_timezone = $4, updated_at = NOW() WHERE id = $1
	`, orgID, day, hour, timezone)
	return err
}

// effectiveChannelSQL is the channel a row is on, as SQL: an override
// counts on a company plan, otherwise the plan decides (orgs.ChannelForPlan).
// $N is the parameter holding orgs.ChoosablePlans.
const effectiveChannelSQL = `CASE WHEN o.plan = ANY($2) THEN COALESCE(NULLIF(o.release_channel, ''), 'stable') ELSE 'nightly' END`

// ListOrgsByChannel lists live workspaces whose effective channel is channel.
func (r *OrgRepository) ListOrgsByChannel(channel string) ([]*orgs.Org, error) {
	rows, err := r.db.Query(`
		SELECT `+orgColumnsQualified+` FROM organizations o
		WHERE o.deleted_at IS NULL AND `+effectiveChannelSQL+` = $1
		ORDER BY o.created_at
	`, channel, pq.Array(orgs.ChoosablePlans))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*orgs.Org
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListMemberUserIDsByChannel lists the accounts with at least one live
// workspace on channel.
func (r *OrgRepository) ListMemberUserIDsByChannel(channel string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT m.user_id FROM org_members m
		JOIN organizations o ON o.id = m.org_id
		WHERE o.deleted_at IS NULL AND `+effectiveChannelSQL+` = $1
	`, channel, pq.Array(orgs.ChoosablePlans))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MemberPreview reads a member's early switch to the next stable release.
func (r *OrgRepository) MemberPreview(orgID, userID string) (bool, error) {
	var on bool
	err := r.db.QueryRow(`
		SELECT COALESCE(preview_next_stable, FALSE) FROM org_members WHERE org_id = $1 AND user_id = $2
	`, orgID, userID).Scan(&on)
	if noRow(err) {
		return false, nil
	}
	return on, err
}

// SetMemberPreview writes a member's early switch. The switch lives on the
// membership, so an account with none in the workspace (a platform admin,
// whom the workspace guard lets by) is refused with orgs.ErrNotMember
// rather than answered as though it were stored.
func (r *OrgRepository) SetMemberPreview(orgID, userID string, enabled bool) error {
	res, err := r.db.Exec(`
		UPDATE org_members SET preview_next_stable = $3 WHERE org_id = $1 AND user_id = $2
	`, orgID, userID, enabled)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return orgs.ErrNotMember
	}
	return nil
}
