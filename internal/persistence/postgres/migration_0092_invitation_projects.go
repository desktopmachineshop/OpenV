package postgres

import "database/sql"

// 0092: the projects a workspace invitation also opens (email-invites). A
// project owner who adds somebody with no account to a project makes a
// workspace invitation; this table holds the project and the role it
// grants, written to the project's members when the invitation is
// accepted. A row goes with its invitation (revoked, purged or replaced
// once accepted) and with its project, by its two foreign keys. Keyed by
// invitation id, which a re-invite keeps, so the projects already offered
// survive a resend. Once it has shipped it is frozen (I16): change the
// schema again with a new migration, never by editing this one.
func m0092InvitationProjects(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS org_invitation_projects (
			invitation_id UUID NOT NULL REFERENCES org_invitations(id) ON DELETE CASCADE,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			role VARCHAR(32) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (invitation_id, project_id)
		)
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_org_invitation_projects_project ON org_invitation_projects(project_id)`)
	return err
}
