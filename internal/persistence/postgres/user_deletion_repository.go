// Removing an account at the identity provider's request (users/deletion.go):
// the account is anonymised and what belonged to the person alone is
// deleted, in one transaction.

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// UserDeletionRepository implements users.AccountDeletionRepository.
type UserDeletionRepository struct {
	db *sql.DB
}

// NewUserDeletionRepository builds the account deletion repository.
func NewUserDeletionRepository(db *sql.DB) *UserDeletionRepository {
	return &UserDeletionRepository{db: db}
}

// deletionMembership is one workspace the account belongs to, with what
// decides whether the deletion may leave or purge it.
type deletionMembership struct {
	orgID, name, role string
	deleted           bool
	members, admins   int
	billing           orgs.Billing
	purge, soleOwner  bool
	liveSubscription  bool
}

// DeleteAccountByEmail anonymises the account the address names; see
// users.AccountDeletionRepository. Table by table:
//
//   - users: the row stays, so the ids that requirements, links, comments,
//     test results, baselines, proposals, runs and events carry resolve to
//     "Deleted user"; email becomes deleted-<id>@deleted.invalid, and the
//     name, avatar, password, verification, notification settings, default
//     workspace, trial marker and platform-admin flag are cleared.
//   - deleted: sessions, worker_keys of the person's own (their runner
//     keys), connected_apps (with their OAuth codes, tokens and workspace
//     grants), connector_pairings, user_provider_tokens, user_repo_paths,
//     user_identities, identity_link_requests, email_verifications,
//     password_resets, push_subscriptions, notifications in their inbox,
//     user_activity_days, crew nodes standing for them (agent_team_nodes),
//     org_team_members, project_members, org_members, and the pending
//     invitations they sent or that were sent to their address.
//   - rewritten: chatter.author_name of their comments; the address in an
//     accepted invitation and in the payload of an "invitation sent" event;
//     the name or address in an evidence bundle's captured_by and in other
//     people's notifications.
//   - unlinked: site_visitors.user_id. Live runner sessions are expired, so
//     the sweeper ends them.
//   - purged: each workspace the person is the only member of, with all it
//     contains (purgeOrgTx).
func (r *UserDeletionRepository) DeleteAccountByEmail(email string, now time.Time) (*users.AccountDeletion, error) {
	var out *users.AccountDeletion
	err := withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		var err error
		out, err = deleteAccountTx(tx, email, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func deleteAccountTx(tx *sql.Tx, email string, now time.Time) (*users.AccountDeletion, error) {
	var (
		userID, name, avatarPath string
		isAdmin                  bool
	)
	err := tx.QueryRow(`SELECT id, name, avatar_path, is_admin FROM users WHERE LOWER(email) = LOWER($1) FOR UPDATE`, email).
		Scan(&userID, &name, &avatarPath, &isAdmin)
	if err == sql.ErrNoRows {
		return &users.AccountDeletion{Status: users.DeletionNotFound}, nil
	}
	if err != nil {
		return nil, err
	}
	out := &users.AccountDeletion{UserID: userID}

	if isAdmin {
		var otherAdmins int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin AND id <> $1`, userID).Scan(&otherAdmins); err != nil {
			return nil, err
		}
		if otherAdmins == 0 {
			out.Status = users.DeletionBlocked
			out.Blocked = &users.DeletionBlock{Kind: users.BlockPlatformAdmin}
			return out, nil
		}
	}

	memberships, err := deletionMemberships(tx, userID)
	if err != nil {
		return nil, err
	}
	if block := deletionBlock(memberships); block != nil {
		out.Status = users.DeletionBlocked
		out.Blocked = block
		return out, nil
	}

	for _, m := range memberships {
		if !m.purge {
			out.LeftWorkspaces = append(out.LeftWorkspaces, m.orgID)
			continue
		}
		files, err := purgeOrgTx(tx, m.orgID)
		if err != nil {
			return nil, err
		}
		out.PurgedWorkspaces = append(out.PurgedWorkspaces, m.orgID)
		out.Files = append(out.Files, files...)
	}

	values := map[string]interface{}{
		"id": userID, "email": email, "placeholder": users.DeletedUserEmail(userID),
		"name": users.DeletedUserName, "now": now,
	}
	for _, stmt := range accountDeletionStatements {
		var args []interface{}
		for _, a := range strings.Fields(stmt.args) {
			args = append(args, values[a])
		}
		if _, err := tx.Exec(stmt.sql, args...); err != nil {
			return nil, fmt.Errorf("delete account: %q: %w", stmt.sql, err)
		}
	}
	// Other people's notifications name the person in their text; a name
	// too short to be told from an ordinary word is left alone.
	if n := strings.TrimSpace(name); len([]rune(n)) >= 4 {
		if _, err := tx.Exec(`
			UPDATE notifications
			SET title = REPLACE(title, $1, $2), body = REPLACE(body, $1, $2)
			WHERE POSITION($1 IN title) > 0 OR POSITION($1 IN body) > 0`, n, users.DeletedUserName); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE evidence_bundles SET captured_by = $2 WHERE LOWER(TRIM(captured_by)) = LOWER($1)`, n, users.DeletedUserName); err != nil {
			return nil, err
		}
	}
	if avatarPath != "" {
		out.Files = append(out.Files, avatarPath)
	}
	out.Status = users.DeletionDeleted
	return out, nil
}

// deletionMemberships reads every workspace the account belongs to and
// decides, for each, whether the deletion purges it or leaves it, and what
// would block either.
func deletionMemberships(tx *sql.Tx, userID string) ([]*deletionMembership, error) {
	rows, err := tx.Query(`
		SELECT o.id, o.name, m.role, o.deleted_at IS NOT NULL,
		       (SELECT COUNT(*) FROM org_members x WHERE x.org_id = o.id),
		       (SELECT COUNT(*) FROM org_members x WHERE x.org_id = o.id AND x.role = $2),
		       o.plan_status, o.plan_cancel_at_period_end
		FROM org_members m JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1
		ORDER BY o.created_at, o.id
		FOR UPDATE OF o`, userID, orgs.RoleAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*deletionMembership
	for rows.Next() {
		m := &deletionMembership{}
		if err := rows.Scan(&m.orgID, &m.name, &m.role, &m.deleted, &m.members, &m.admins,
			&m.billing.Status, &m.billing.CancelAtPeriodEnd); err != nil {
			return nil, err
		}
		m.purge = m.members <= 1
		// A deleted workspace awaits its purge; the person only leaves it
		// when others remain, and blocks nothing.
		if !m.deleted {
			m.soleOwner = !m.purge && m.role == orgs.RoleAdmin && m.admins <= 1
			m.liveSubscription = m.purge && m.billing.Live() && !m.billing.CancelAtPeriodEnd
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// deletionBlock is the first reason the memberships block the deletion,
// naming the first workspace and counting the rest; a subscription is named
// before ownership, since cancelling it is the person's own step.
func deletionBlock(memberships []*deletionMembership) *users.DeletionBlock {
	var block *users.DeletionBlock
	for _, kind := range []string{users.BlockSubscription, users.BlockSoleOwner} {
		for _, m := range memberships {
			hit := (kind == users.BlockSubscription && m.liveSubscription) ||
				(kind == users.BlockSoleOwner && m.soleOwner)
			if !hit {
				continue
			}
			if block == nil {
				block = &users.DeletionBlock{Kind: kind, Workspace: m.name}
			} else {
				block.Others++
			}
		}
		if block != nil {
			return block
		}
	}
	return nil
}

// accountDeletionStatements anonymise the account once its workspaces are
// settled. Each names its arguments from the user id (id), the address,
// normalised (email), its placeholder and now. The users row, whose address
// the invitation statements match, changes last.
var accountDeletionStatements = []struct {
	sql  string
	args string
}{
	{`DELETE FROM sessions WHERE user_id = $1`, "id"},
	{`DELETE FROM worker_keys WHERE user_id = $1`, "id"},
	{`DELETE FROM connected_apps WHERE user_id = $1`, "id"},
	{`DELETE FROM connector_pairings WHERE user_id = $1`, "id"},
	{`DELETE FROM user_provider_tokens WHERE user_id = $1`, "id"},
	{`DELETE FROM user_repo_paths WHERE user_id = $1`, "id"},
	{`DELETE FROM user_identities WHERE user_id = $1`, "id"},
	{`DELETE FROM identity_link_requests WHERE user_id = $1`, "id"},
	{`DELETE FROM email_verifications WHERE user_id = $1`, "id"},
	{`DELETE FROM password_resets WHERE user_id = $1`, "id"},
	{`DELETE FROM push_subscriptions WHERE user_id = $1`, "id"},
	{`DELETE FROM notifications WHERE user_id = $1`, "id"},
	{`DELETE FROM user_activity_days WHERE user_id = $1`, "id"},
	{`DELETE FROM agent_team_nodes WHERE user_id = $1`, "id"},
	{`DELETE FROM org_team_members WHERE user_id = $1`, "id"},
	{`DELETE FROM project_members WHERE user_id = $1`, "id"},
	{`DELETE FROM org_members WHERE user_id = $1`, "id"},
	{`DELETE FROM org_invitations WHERE accepted_at IS NULL AND (invited_by = $1 OR LOWER(email) = LOWER($2))`, "id email"},
	{`UPDATE org_invitations SET email = $2 WHERE LOWER(email) = LOWER($1)`, "email placeholder"},
	{`UPDATE domain_events SET payload = jsonb_set(payload, '{email}', to_jsonb($2::text)) WHERE LOWER(payload->>'email') = LOWER($1)`, "email placeholder"},
	{`UPDATE notifications SET title = REPLACE(title, $1, $2), body = REPLACE(body, $1, $2) WHERE POSITION($1 IN title) > 0 OR POSITION($1 IN body) > 0`, "email placeholder"},
	{`UPDATE evidence_bundles SET captured_by = $2 WHERE LOWER(TRIM(captured_by)) = LOWER($1)`, "email placeholder"},
	{`UPDATE chatter SET author_name = $2 WHERE created_by = $1`, "id name"},
	{`UPDATE site_visitors SET user_id = NULL WHERE user_id = $1`, "id"},
	{`UPDATE runner_sessions SET expires_at = $2 WHERE user_id = $1 AND status IN ('starting', 'active') AND expires_at > $2`, "id now"},
	{`UPDATE users SET email = $2, name = $3, avatar_url = '', avatar_path = '', avatar_mime = '',
		password_hash = NULL, is_admin = FALSE, email_notifications = FALSE, push_notifications = FALSE,
		email_verified = FALSE, email_verified_at = NULL, default_org_id = NULL, billing_trial_used_at = NULL,
		updated_at = $4
	 WHERE id = $1`, "id placeholder name now"},
}
