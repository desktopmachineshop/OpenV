package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// TestOrgSoftDeleteLifecycle locks in the workspace deletion contract:
// soft delete hides the org from listings and voids MemberRole (locking it),
// MemberRoleAny still sees the membership (restore path), restore brings it
// back, and the expiry scan only reports orgs past the cutoff.
func TestOrgSoftDeleteLifecycle(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewOrgRepository(db)
	svc := orgs.NewDefaultService(repo)

	userID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, 'del@example.com', 'Del')`, userID); err != nil {
		t.Fatal(err)
	}
	org, err := svc.CreateOrg("Doomed Workspace", orgs.TypeCompany, userID)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	personal, _, err := svc.EnsurePersonalOrg(userID, "Del")
	if err != nil {
		t.Fatalf("EnsurePersonalOrg: %v", err)
	}

	if _, err := svc.DeleteOrg(personal.ID); err != orgs.ErrPersonalOrgDelete {
		t.Fatalf("DeleteOrg(personal) = %v, want ErrPersonalOrgDelete", err)
	}

	deleted, err := svc.DeleteOrg(org.ID)
	if err != nil {
		t.Fatalf("DeleteOrg: %v", err)
	}
	if deleted.DeletedAt == nil {
		t.Fatal("DeleteOrg did not stamp DeletedAt")
	}

	live, err := svc.ListForUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range live {
		if o.ID == org.ID {
			t.Error("deleted org still listed for user")
		}
	}
	if role, _ := svc.RoleInOrg(org.ID, userID); role != "" {
		t.Errorf("RoleInOrg on deleted org = %q, want locked (empty)", role)
	}
	if role, _ := svc.RoleInOrgAny(org.ID, userID); role != orgs.RoleAdmin {
		t.Errorf("RoleInOrgAny on deleted org = %q, want admin", role)
	}
	gone, err := svc.ListDeletedForUser(userID)
	if err != nil || len(gone) != 1 || gone[0].ID != org.ID {
		t.Fatalf("ListDeletedForUser = %v, %v; want the deleted org", gone, err)
	}

	// Not yet expired: a scan dated now must not report it...
	ids, err := repo.ListExpiredDeletedOrgIDs(time.Now().Add(-time.Hour))
	if err != nil || len(ids) != 0 {
		t.Fatalf("expired scan before cutoff = %v, %v; want empty", ids, err)
	}
	// ...but a scan past the grace period must.
	ids, err = repo.ListExpiredDeletedOrgIDs(time.Now().Add(time.Hour))
	if err != nil || len(ids) != 1 || ids[0] != org.ID {
		t.Fatalf("expired scan after cutoff = %v, %v; want [%s]", ids, err, org.ID)
	}

	restored, err := svc.RestoreOrg(org.ID)
	if err != nil {
		t.Fatalf("RestoreOrg: %v", err)
	}
	if restored.DeletedAt != nil {
		t.Fatal("RestoreOrg left DeletedAt set")
	}
	if role, _ := svc.RoleInOrg(org.ID, userID); role != orgs.RoleAdmin {
		t.Errorf("RoleInOrg after restore = %q, want admin", role)
	}
	if _, err := svc.RestoreOrg(org.ID); err != orgs.ErrNotDeleted {
		t.Fatalf("RestoreOrg(live) = %v, want ErrNotDeleted", err)
	}
}

// TestOrgPurge locks in the hard-delete sweep: purging removes the org row
// and its non-cascading dependents (projects, artifacts, links, chatter,
// test runs), while another workspace's data survives untouched.
func TestOrgPurge(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewOrgRepository(db)
	svc := orgs.NewDefaultService(repo)

	userID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, 'purge@example.com', 'Purge')`, userID); err != nil {
		t.Fatal(err)
	}
	doomed, err := svc.CreateOrg("Doomed", orgs.TypeCompany, userID)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := svc.CreateOrg("Kept", orgs.TypeCompany, userID)
	if err != nil {
		t.Fatal(err)
	}

	seedProject := func(orgID string) (projectID, artifactID string) {
		projectID = uuid.New().String()
		artifactID = uuid.New().String()
		otherArtifact := uuid.New().String()
		mustExec := func(q string, args ...interface{}) {
			t.Helper()
			if _, err := db.Exec(q, args...); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		mustExec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'P')`, projectID, orgID)
		mustExec(`INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R1')`, artifactID, projectID)
		mustExec(`INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'test-case', 'T1')`, otherArtifact, projectID)
		mustExec(`INSERT INTO links (id, from_id, to_id, type) VALUES ($1, $2, $3, 'verifies')`, uuid.New().String(), otherArtifact, artifactID)
		mustExec(`INSERT INTO chatter (id, artifact_id, message) VALUES ($1, $2, 'note')`, uuid.New().String(), artifactID)
		mustExec(`INSERT INTO test_runs (id, project_id, name) VALUES ($1, $2, 'run')`, uuid.New().String(), projectID)
		mustExec(`INSERT INTO baselines (id, project_id, name, snapshot) VALUES ($1, $2, 'b1', '{}')`, uuid.New().String(), projectID)
		return projectID, artifactID
	}
	seedProject(doomed.ID)
	keptProject, keptArtifact := seedProject(kept.ID)

	if _, err := svc.DeleteOrg(doomed.ID); err != nil {
		t.Fatal(err)
	}
	// Backdate the soft delete past the grace period, then run the real sweep.
	if _, err := db.Exec(`UPDATE organizations SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, doomed.ID); err != nil {
		t.Fatal(err)
	}
	purged, err := svc.PurgeExpired(time.Now())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if len(purged) != 1 || purged[0] != doomed.ID {
		t.Fatalf("purged = %v, want [%s]", purged, doomed.ID)
	}

	count := func(q string, args ...interface{}) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM organizations WHERE id = $1`, doomed.ID); n != 0 {
		t.Errorf("org row survived purge")
	}
	if n := count(`SELECT COUNT(*) FROM projects WHERE org_id = $1`, doomed.ID); n != 0 {
		t.Errorf("projects survived purge")
	}
	if n := count(`SELECT COUNT(*) FROM artifacts a WHERE NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = a.project_id)`); n != 0 {
		t.Errorf("%d orphaned artifacts after purge", n)
	}
	if n := count(`SELECT COUNT(*) FROM links l WHERE NOT EXISTS (SELECT 1 FROM artifacts a WHERE a.id = l.from_id)`); n != 0 {
		t.Errorf("%d orphaned links after purge", n)
	}
	if n := count(`SELECT COUNT(*) FROM chatter c WHERE NOT EXISTS (SELECT 1 FROM artifacts a WHERE a.id = c.artifact_id)`); n != 0 {
		t.Errorf("%d orphaned chatter rows after purge", n)
	}

	// The other workspace is intact.
	if n := count(`SELECT COUNT(*) FROM projects WHERE id = $1`, keptProject); n != 1 {
		t.Errorf("kept project lost")
	}
	if n := count(`SELECT COUNT(*) FROM artifacts WHERE id = $1`, keptArtifact); n != 1 {
		t.Errorf("kept artifact lost")
	}
	if role, _ := svc.RoleInOrg(kept.ID, userID); role != orgs.RoleAdmin {
		t.Errorf("kept org membership lost")
	}
}
