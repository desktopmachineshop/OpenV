package postgres

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Removing an account at the identity provider's request: the account is
// anonymised, what was the person's alone goes, the workspaces only they
// were in are purged, shared records stay, and a block changes nothing.

type deletionFixture struct {
	t  *testing.T
	db *sql.DB
}

func (f deletionFixture) exec(q string, args ...interface{}) {
	f.t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f deletionFixture) count(q string, args ...interface{}) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (f deletionFixture) user(email, name string) string {
	id := uuid.New().String()
	f.exec(`INSERT INTO users (id, email, name, auth_provider, password_hash, avatar_url, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, 'password', 'hash', 'https://img.example/a.png', TRUE, NOW(), NOW())`, id, email, name)
	return id
}

func (f deletionFixture) org(name, orgType string, members map[string]string) string {
	id := uuid.New().String()
	f.exec(`INSERT INTO organizations (id, name, slug, org_type) VALUES ($1, $2, $3, $4)`, id, name, "s-"+id[:8], orgType)
	for userID, role := range members {
		f.exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`, id, userID, role)
	}
	return id
}

func (f deletionFixture) project(orgID, author string) (projectID, artifactID string) {
	projectID, artifactID = uuid.New().String(), uuid.New().String()
	f.exec(`INSERT INTO projects (id, name, org_id) VALUES ($1, 'P', $2)`, projectID, orgID)
	f.exec(`INSERT INTO artifacts (id, project_id, type, title, created_by) VALUES ($1, $2, 'requirement', 'R', $3)`, artifactID, projectID, author)
	return projectID, artifactID
}

func TestAccountDeletionAnonymisesAndKeepsSharedRecords(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	f := deletionFixture{t, db}
	repo := NewUserDeletionRepository(db)

	dave := f.user("Dave@Example.com", "Dave Example")
	erin := f.user("erin@example.com", "Erin")
	personal := f.org("Dave's workspace", "personal", map[string]string{dave: "admin"})
	f.project(personal, dave)
	shared := f.org("Acme", "company", map[string]string{dave: "admin", erin: "admin"})
	_, art := f.project(shared, dave)

	f.exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, 'h1', NOW() + INTERVAL '1 day')`, uuid.New().String(), dave)
	f.exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, 'h2', NOW() + INTERVAL '1 day')`, uuid.New().String(), erin)
	f.exec(`INSERT INTO worker_keys (id, org_id, name, key_hash, user_id) VALUES ($1, $2, 'mine', 'k1', $3)`, uuid.New().String(), shared, dave)
	f.exec(`INSERT INTO worker_keys (id, org_id, name, key_hash, created_by) VALUES ($1, $2, 'shared', 'k2', $3)`, uuid.New().String(), shared, dave)
	f.exec(`INSERT INTO user_identities (user_id, provider) VALUES ($1, 'oidc')`, dave)
	f.exec(`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth) VALUES ($1, $2, 'https://push/1', 'p', 'a')`, uuid.New().String(), dave)
	f.exec(`INSERT INTO notifications (id, user_id, type, title) VALUES ($1, $2, 'x', 'mine')`, uuid.New().String(), dave)
	f.exec(`INSERT INTO notifications (id, user_id, type, title, body) VALUES ($1, $2, 'x', 'Dave Example commented', 'by dave@example.com')`, uuid.New().String(), erin)
	f.exec(`INSERT INTO chatter (id, artifact_id, message, created_by, author_name) VALUES ($1, $2, 'looks good', $3, 'Dave Example')`, uuid.New().String(), art, dave)
	f.exec(`INSERT INTO org_invitations (id, org_id, email, token_hash, expires_at, accepted_at) VALUES ($1, $2, 'dave@example.com', 't1', NOW(), NOW())`, uuid.New().String(), shared)
	f.exec(`INSERT INTO org_invitations (id, org_id, email, token_hash, expires_at, invited_by) VALUES ($1, $2, 'frank@example.com', 't2', NOW() + INTERVAL '1 day', $3)`, uuid.New().String(), shared, dave)
	f.exec(`INSERT INTO domain_events (id, event_type, org_id, actor, payload) VALUES ($1, 'org.invitation_sent', $2, 'user:x', '{"email":"dave@example.com","role":"member"}')`, uuid.New().String(), shared)
	f.exec(`INSERT INTO evidence_bundles (id, project_id, ref, title, captured_by, created_at, updated_at)
		SELECT $1, project_id, 'EV-1', 'Bench', 'Dave Example', NOW(), NOW() FROM artifacts WHERE id = $2`, uuid.New().String(), art)

	out, err := repo.DeleteAccountByEmail("dave@example.com", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != users.DeletionDeleted || out.UserID != dave {
		t.Fatalf("outcome = %+v, want deleted", out)
	}
	if len(out.PurgedWorkspaces) != 1 || out.PurgedWorkspaces[0] != personal || len(out.LeftWorkspaces) != 1 || out.LeftWorkspaces[0] != shared {
		t.Errorf("purged %v, left %v; want the personal workspace purged and Acme left", out.PurgedWorkspaces, out.LeftWorkspaces)
	}

	var email, name, avatar string
	var hash sql.NullString
	var verified bool
	if err := db.QueryRow(`SELECT email, name, avatar_url, password_hash, email_verified FROM users WHERE id = $1`, dave).
		Scan(&email, &name, &avatar, &hash, &verified); err != nil {
		t.Fatal(err)
	}
	if email != users.DeletedUserEmail(dave) || name != users.DeletedUserName || avatar != "" || hash.Valid || verified {
		t.Errorf("user row = %q %q %q %v %v, want anonymised", email, name, avatar, hash, verified)
	}
	for q, want := range map[string]int{
		`SELECT COUNT(*) FROM organizations WHERE id = '` + personal + `'`:                              0,
		`SELECT COUNT(*) FROM organizations WHERE id = '` + shared + `'`:                                1,
		`SELECT COUNT(*) FROM artifacts WHERE id = '` + art + `' AND created_by = '` + dave + `'`:       1,
		`SELECT COUNT(*) FROM sessions WHERE user_id = '` + dave + `'`:                                  0,
		`SELECT COUNT(*) FROM sessions WHERE user_id = '` + erin + `'`:                                  1,
		`SELECT COUNT(*) FROM worker_keys WHERE user_id = '` + dave + `'`:                               0,
		`SELECT COUNT(*) FROM worker_keys WHERE name = 'shared'`:                                        1,
		`SELECT COUNT(*) FROM user_identities WHERE user_id = '` + dave + `'`:                           0,
		`SELECT COUNT(*) FROM push_subscriptions WHERE user_id = '` + dave + `'`:                        0,
		`SELECT COUNT(*) FROM notifications WHERE user_id = '` + dave + `'`:                             0,
		`SELECT COUNT(*) FROM org_members WHERE user_id = '` + dave + `'`:                               0,
		`SELECT COUNT(*) FROM org_invitations WHERE email = 'frank@example.com'`:                        0,
		`SELECT COUNT(*) FROM org_invitations WHERE LOWER(email) LIKE '%dave%'`:                         0,
		`SELECT COUNT(*) FROM domain_events WHERE payload::text ILIKE '%dave@example.com%'`:             0,
		`SELECT COUNT(*) FROM chatter WHERE author_name = 'Deleted user'`:                               1,
		`SELECT COUNT(*) FROM notifications WHERE title || body ILIKE '%dave%'`:                         0,
		`SELECT COUNT(*) FROM evidence_bundles WHERE captured_by = 'Deleted user'`:                      1,
		`SELECT COUNT(*) FROM users WHERE LOWER(email) LIKE '%dave%' OR name ILIKE '%dave%'`:            0,
		`SELECT COUNT(*) FROM org_members WHERE org_id = '` + shared + `' AND user_id = '` + erin + `'`: 1,
	} {
		if got := f.count(q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}

	// Idempotent: the address names no account any more.
	again, err := repo.DeleteAccountByEmail("dave@example.com", time.Now().UTC())
	if err != nil || again.Status != users.DeletionNotFound {
		t.Errorf("second call = %+v, %v; want not_found", again, err)
	}
}

func TestAccountDeletionBlocksAndChangesNothing(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	f := deletionFixture{t, db}
	repo := NewUserDeletionRepository(db)

	// The only admin of a workspace others use.
	owner := f.user("owner@example.com", "Owner")
	member := f.user("member@example.com", "Member")
	f.org("Acme", "company", map[string]string{owner: "admin", member: "member"})
	f.org("Beta", "company", map[string]string{owner: "admin", member: "member"})
	f.exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, 'h1', NOW() + INTERVAL '1 day')`, uuid.New().String(), owner)
	out, err := repo.DeleteAccountByEmail("OWNER@example.com", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != users.DeletionBlocked || out.Blocked == nil || out.Blocked.Kind != users.BlockSoleOwner || out.Blocked.Workspace != "Acme" || out.Blocked.Others != 1 {
		t.Fatalf("outcome = %+v (%+v), want blocked by Acme and one more", out, out.Blocked)
	}
	if f.count(`SELECT COUNT(*) FROM users WHERE email = 'owner@example.com' AND name = 'Owner'`) != 1 ||
		f.count(`SELECT COUNT(*) FROM sessions WHERE user_id = $1`, owner) != 1 ||
		f.count(`SELECT COUNT(*) FROM org_members WHERE user_id = $1`, owner) != 2 {
		t.Error("a blocked deletion changed the account")
	}

	// A plain member of the same workspaces is no owner: they just leave.
	if out, err := repo.DeleteAccountByEmail("member@example.com", time.Now().UTC()); err != nil || out.Status != users.DeletionDeleted || len(out.LeftWorkspaces) != 2 {
		t.Errorf("member: %+v, %v; want deleted, leaving both", out, err)
	}

	// A live subscription on a workspace the deletion would purge.
	payer := f.user("payer@example.com", "Payer")
	solo := f.org("Solo Ltd", "company", map[string]string{payer: "admin"})
	f.exec(`UPDATE organizations SET plan_status = 'active' WHERE id = $1`, solo)
	out, err = repo.DeleteAccountByEmail("payer@example.com", time.Now().UTC())
	if err != nil || out.Status != users.DeletionBlocked || out.Blocked.Kind != users.BlockSubscription || out.Blocked.Workspace != "Solo Ltd" {
		t.Fatalf("payer: %+v, %v; want blocked by the subscription", out, err)
	}
	// Cancelled, ending at the period's end: nothing more to bill.
	f.exec(`UPDATE organizations SET plan_cancel_at_period_end = TRUE WHERE id = $1`, solo)
	out, err = repo.DeleteAccountByEmail("payer@example.com", time.Now().UTC())
	if err != nil || out.Status != users.DeletionDeleted || len(out.PurgedWorkspaces) != 1 {
		t.Fatalf("payer after cancelling: %+v, %v; want deleted", out, err)
	}

	// The server's only platform admin.
	admin := f.user("root@example.com", "Root")
	f.exec(`UPDATE users SET is_admin = TRUE WHERE id = $1`, admin)
	out, err = repo.DeleteAccountByEmail("root@example.com", time.Now().UTC())
	if err != nil || out.Status != users.DeletionBlocked || out.Blocked.Kind != users.BlockPlatformAdmin {
		t.Fatalf("platform admin: %+v, %v; want blocked", out, err)
	}

	if out, err := repo.DeleteAccountByEmail("nobody@example.com", time.Now().UTC()); err != nil || out.Status != users.DeletionNotFound {
		t.Errorf("unknown address: %+v, %v; want not_found", out, err)
	}
}
