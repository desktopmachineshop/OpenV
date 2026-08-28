package postgres

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// legacyFixture seeds a pre-tenancy database shape: users, projects with
// owner memberships, agents/runs/automations/guided sessions/events/provider
// settings — all without org ownership — plus a flat agents directory.
type legacyFixture struct {
	userEarly, userLate   string // userEarly registered first => bootstrap owner
	projOwned, projOrphan string // projOwned owned by userLate; projOrphan has no owner
	agentID               string
	runInProject          string
	runOrphan             string
	guidedSession         string
	agentsDir             string
	agentFile             string // flat legacy path of the agent's markdown file
}

func seedLegacy(t *testing.T, db *sql.DB) legacyFixture {
	t.Helper()
	f := legacyFixture{
		userEarly:     uuid.New().String(),
		userLate:      uuid.New().String(),
		projOwned:     uuid.New().String(),
		projOrphan:    uuid.New().String(),
		agentID:       uuid.New().String(),
		runInProject:  uuid.New().String(),
		runOrphan:     uuid.New().String(),
		guidedSession: uuid.New().String(),
		agentsDir:     t.TempDir(),
	}

	mustExec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	mustExec(`INSERT INTO users (id, email, name, created_at) VALUES ($1, 'first@example.com', 'First User', NOW() - INTERVAL '2 days')`, f.userEarly)
	mustExec(`INSERT INTO users (id, email, name, created_at) VALUES ($1, 'second@example.com', '', NOW() - INTERVAL '1 day')`, f.userLate)

	mustExec(`INSERT INTO projects (id, name) VALUES ($1, 'Owned Project')`, f.projOwned)
	mustExec(`INSERT INTO projects (id, name) VALUES ($1, 'Orphan Project')`, f.projOrphan)
	mustExec(`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, f.projOwned, f.userLate)

	f.agentFile = filepath.Join(f.agentsDir, "helper.md")
	if err := os.WriteFile(f.agentFile, []byte("---\nname: Helper\n---\nprompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.agentsDir, ".trash"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO agents (id, slug, name, provider, file_path) VALUES ($1, 'helper', 'Helper', 'claude', $2)`, f.agentID, f.agentFile)

	mustExec(`INSERT INTO agent_runs (id, agent_id, project_id, prompt) VALUES ($1, $2, $3, 'in project')`, f.runInProject, f.agentID, f.projOwned)
	mustExec(`INSERT INTO agent_runs (id, agent_id, prompt) VALUES ($1, $2, 'no project')`, f.runOrphan, f.agentID)

	mustExec(`INSERT INTO guided_sessions (id, project_id) VALUES ($1, $2)`, f.guidedSession, f.projOwned)
	mustExec(`INSERT INTO automations (id, name, agent_id, project_id, kind) VALUES ($1, 'auto', $2, $3, 'manual')`, uuid.New().String(), f.agentID, f.projOwned)
	mustExec(`INSERT INTO agent_teams (id, name, project_id) VALUES ($1, 'crew', $2)`, uuid.New().String(), f.projOwned)
	mustExec(`INSERT INTO domain_events (id, event_type, project_id) VALUES ($1, 'artifact.created', $2)`, uuid.New().String(), f.projOwned)
	mustExec(`INSERT INTO domain_events (id, event_type) VALUES ($1, 'agentrun.finished')`, uuid.New().String())
	mustExec(`INSERT INTO provider_settings (id, provider) VALUES ($1, 'claude')`, uuid.New().String())
	mustExec(`INSERT INTO provider_logins (id, provider) VALUES ($1, 'claude')`, uuid.New().String())

	return f
}

// personalOrg resolves a user's personal org id.
func personalOrg(t *testing.T, db *sql.DB, userID string) string {
	t.Helper()
	var orgID string
	err := db.QueryRow(`
		SELECT o.id FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		WHERE m.user_id = $1 AND o.org_type = 'personal'
	`, userID).Scan(&orgID)
	if err != nil {
		t.Fatalf("personal org for %s: %v", userID, err)
	}
	return orgID
}

func scalarString(t *testing.T, db *sql.DB, query string, args ...interface{}) string {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	return v.String
}

func scalarInt(t *testing.T, db *sql.DB, query string, args ...interface{}) int {
	t.Helper()
	var v int
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	return v
}

func TestBackfillOrgsMigratesLegacyDataIdempotently(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	f := seedLegacy(t, db)

	if err := BackfillOrgs(db, f.agentsDir); err != nil {
		t.Fatalf("BackfillOrgs (first run): %v", err)
	}

	// Every user got exactly one personal org; membership role is admin.
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM organizations WHERE org_type = 'personal'`); got != 2 {
		t.Fatalf("personal orgs = %d, want 2", got)
	}
	bootstrap := personalOrg(t, db, f.userEarly)
	lateOrg := personalOrg(t, db, f.userLate)
	if bootstrap == lateOrg {
		t.Fatal("each user must get their own personal org")
	}
	if role := scalarString(t, db, `SELECT role FROM org_members WHERE user_id = $1`, f.userEarly); role != "admin" {
		t.Errorf("personal org role = %q, want admin", role)
	}
	// A user with an empty name gets the email local-part as display name.
	name := scalarString(t, db, `SELECT o.name FROM organizations o JOIN org_members m ON m.org_id = o.id WHERE m.user_id = $1`, f.userLate)
	if name != "second's Space" {
		t.Errorf("org name = %q, want \"second's Space\" (from email local-part)", name)
	}

	// Projects: owned project follows its owner's personal org, ownerless
	// project falls to the bootstrap (earliest user's) org.
	if got := scalarString(t, db, `SELECT org_id::text FROM projects WHERE id = $1`, f.projOwned); got != lateOrg {
		t.Errorf("owned project org = %s, want its owner's personal org %s", got, lateOrg)
	}
	if got := scalarString(t, db, `SELECT org_id::text FROM projects WHERE id = $1`, f.projOrphan); got != bootstrap {
		t.Errorf("orphan project org = %s, want bootstrap %s", got, bootstrap)
	}

	// Project-derivable rows inherit their project's org; rows with no
	// project fall to bootstrap.
	if got := scalarString(t, db, `SELECT org_id::text FROM agent_runs WHERE id = $1`, f.runInProject); got != lateOrg {
		t.Errorf("project run org = %s, want the project's org %s", got, lateOrg)
	}
	if got := scalarString(t, db, `SELECT org_id::text FROM agent_runs WHERE id = $1`, f.runOrphan); got != bootstrap {
		t.Errorf("orphan run org = %s, want bootstrap %s", got, bootstrap)
	}
	if got := scalarString(t, db, `SELECT org_id::text FROM guided_sessions WHERE id = $1`, f.guidedSession); got != lateOrg {
		t.Errorf("guided session org = %s, want %s", got, lateOrg)
	}
	for _, table := range []string{"agent_teams", "automations", "domain_events", "agents", "provider_settings", "provider_logins"} {
		if n := scalarInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE org_id IS NULL`); n != 0 {
			t.Errorf("%s: %d rows left unowned", table, n)
		}
	}

	// Flat agent files moved into the bootstrap org's directory, registry
	// path updated, legacy trash carried along.
	movedFile := filepath.Join(f.agentsDir, bootstrap, "helper.md")
	if _, err := os.Stat(movedFile); err != nil {
		t.Errorf("agent file not moved to %s: %v", movedFile, err)
	}
	if _, err := os.Stat(f.agentFile); !os.IsNotExist(err) {
		t.Errorf("flat agent file still present at %s", f.agentFile)
	}
	if got := scalarString(t, db, `SELECT file_path FROM agents WHERE id = $1`, f.agentID); got != movedFile {
		t.Errorf("agents.file_path = %q, want %q", got, movedFile)
	}
	if info, err := os.Stat(filepath.Join(f.agentsDir, bootstrap, ".trash")); err != nil || !info.IsDir() {
		t.Errorf("legacy .trash not moved into org dir: %v", err)
	}

	// org_id columns were promoted to NOT NULL once everything was owned.
	for _, table := range []string{"projects", "agents", "agent_runs", "guided_sessions", "domain_events", "provider_settings", "provider_logins", "agent_teams", "automations"} {
		nullable := scalarString(t, db, `
			SELECT is_nullable FROM information_schema.columns
			WHERE table_name = $1 AND column_name = 'org_id'
		`, table)
		if nullable != "NO" {
			t.Errorf("%s.org_id is_nullable = %q, want NO after promotion", table, nullable)
		}
	}

	// --- Second run: must be a no-op, not an error or a duplicate. ---
	if err := BackfillOrgs(db, f.agentsDir); err != nil {
		t.Fatalf("BackfillOrgs (second run): %v", err)
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM organizations`); got != 2 {
		t.Errorf("second run changed org count: %d, want 2", got)
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM org_members`); got != 2 {
		t.Errorf("second run changed membership count: %d, want 2", got)
	}
	if got := scalarString(t, db, `SELECT org_id::text FROM projects WHERE id = $1`, f.projOwned); got != lateOrg {
		t.Errorf("second run reassigned owned project: %s", got)
	}
	if got := scalarString(t, db, `SELECT file_path FROM agents WHERE id = $1`, f.agentID); got != movedFile {
		t.Errorf("second run disturbed the agent file path: %q", got)
	}
	if _, err := os.Stat(movedFile); err != nil {
		t.Errorf("second run disturbed the moved agent file: %v", err)
	}
}

func TestBackfillOrgsFreshDatabaseIsNoop(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	if err := BackfillOrgs(db, t.TempDir()); err != nil {
		t.Fatalf("BackfillOrgs on empty DB: %v", err)
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM organizations`); got != 0 {
		t.Errorf("orgs = %d, want 0 on a fresh database", got)
	}
	// Empty tables still promote cleanly (no NULLs to violate NOT NULL)…
	// except nothing was backfilled, so the fresh-DB early return must have
	// fired before promotion: signups will create orgs later.
}

func TestBackfillOrgsNewUserAfterMigrationGetsPersonalOrg(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	f := seedLegacy(t, db)

	if err := BackfillOrgs(db, f.agentsDir); err != nil {
		t.Fatalf("first BackfillOrgs: %v", err)
	}

	// A user registered after the first migration (e.g. restored from an
	// older dump) is picked up by the next boot's backfill.
	newUser := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, 'third@example.com', 'Third')`, newUser); err != nil {
		t.Fatal(err)
	}
	if err := BackfillOrgs(db, f.agentsDir); err != nil {
		t.Fatalf("second BackfillOrgs: %v", err)
	}
	orgID := personalOrg(t, db, newUser)
	if orgID == "" {
		t.Fatal("new user did not get a personal org")
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM organizations WHERE org_type = 'personal'`); got != 3 {
		t.Errorf("personal orgs = %d, want 3", got)
	}
	// Bootstrap org is still the earliest user's, so existing ownership is
	// untouched.
	if got := scalarString(t, db, `SELECT org_id::text FROM agent_runs WHERE id = $1`, f.runOrphan); got != personalOrg(t, db, f.userEarly) {
		t.Errorf("orphan run moved orgs on re-run: %s", got)
	}
}
