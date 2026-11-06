// OrgRepository's once-only notices to a workspace's admins (migration 72).

package postgres

// ClaimNotice inserts the notice's row and reports whether this caller
// inserted it. The primary key (org_id, notice_key) makes the claim atomic:
// of two callers, on one replica or two, exactly one sees true.
func (r *OrgRepository) ClaimNotice(orgID, key, subject string) (bool, error) {
	res, err := r.db.Exec(`
		INSERT INTO workspace_notices (org_id, notice_key, subject)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, notice_key) DO NOTHING
	`, orgID, key, subject)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecordNotice writes how many admins a claimed notice reached.
func (r *OrgRepository) RecordNotice(orgID, key string, recipients int) error {
	_, err := r.db.Exec(`
		UPDATE workspace_notices SET recipients = $3, sent_at = NOW() WHERE org_id = $1 AND notice_key = $2
	`, orgID, key, recipients)
	return err
}

// ReleaseNotice deletes a claim, so the notice can be claimed again.
func (r *OrgRepository) ReleaseNotice(orgID, key string) error {
	_, err := r.db.Exec(`DELETE FROM workspace_notices WHERE org_id = $1 AND notice_key = $2`, orgID, key)
	return err
}
