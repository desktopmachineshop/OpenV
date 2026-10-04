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

// TestOrgRestoreAnswersTheRestoredWorkspace: a restore answers the workspace
// as the restore stored it. It used to answer the workspace as read before
// the write, deleted_at dropped, so its updated_at was the delete's while the
// row held the restore's.
func TestOrgRestoreAnswersTheRestoredWorkspace(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	svc := orgs.NewDefaultService(NewOrgRepository(db))

	userID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, 'restore@example.com', 'Restore')`, userID); err != nil {
		t.Fatal(err)
	}
	org, err := svc.CreateOrg("Restored Workspace", orgs.TypeCompany, userID)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	deleted, err := svc.DeleteOrg(org.ID)
	if err != nil {
		t.Fatalf("DeleteOrg: %v", err)
	}
	restored, err := svc.RestoreOrg(org.ID)
	if err != nil {
		t.Fatalf("RestoreOrg: %v", err)
	}
	stored, err := svc.Get(org.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if restored.DeletedAt != nil || stored.DeletedAt != nil {
		t.Fatalf("deleted_at after restore: answered %v, stored %v; want neither", restored.DeletedAt, stored.DeletedAt)
	}
	if !restored.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatalf("restore answered updated_at %s, the row holds %s", restored.UpdatedAt, stored.UpdatedAt)
	}
	if !restored.UpdatedAt.After(*deleted.DeletedAt) {
		t.Fatalf("restore answered updated_at %s, not after the delete at %s", restored.UpdatedAt, *deleted.DeletedAt)
	}
}

// TestOrgPurge locks in the hard-delete sweep: purging removes the org row
// and its non-cascading dependents (projects, artifacts, links, chatter,
// test runs, figure counters), while another workspace's data survives
// untouched. A link between the two workspaces' artifacts goes with the
// purged one, and so do its version records at both ends (#379 bug 138:
// the figure counters, and the version record at the kept end, were left
// behind with no artifact or link to belong to).
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
		link := uuid.New().String()
		mustExec(`INSERT INTO links (id, from_id, to_id, type) VALUES ($1, $2, $3, 'verifies')`, link, otherArtifact, artifactID)
		mustExec(`INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 1)`,
			link, otherArtifact, artifactID)
		mustExec(`INSERT INTO chatter (id, artifact_id, message) VALUES ($1, $2, 'note')`, uuid.New().String(), artifactID)
		mustExec(`INSERT INTO attachment_figure_counters (artifact_id, next_num) VALUES ($1, 2)`, artifactID)
		mustExec(`INSERT INTO test_runs (id, project_id, name) VALUES ($1, $2, 'run')`, uuid.New().String(), projectID)
		mustExec(`INSERT INTO baselines (id, project_id, name, snapshot) VALUES ($1, $2, 'b1', '{}')`, uuid.New().String(), projectID)
		return projectID, artifactID
	}
	_, doomedArtifact := seedProject(doomed.ID)
	keptProject, keptArtifact := seedProject(kept.ID)
	crossing := uuid.New().String()
	for _, q := range []string{
		`INSERT INTO links (id, from_id, to_id, type) VALUES ($1, $2, $3, 'refines')`,
		`INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 1)`,
	} {
		if _, err := db.Exec(q, crossing, keptArtifact, doomedArtifact); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

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
	if len(purged.IDs) != 1 || purged.IDs[0] != doomed.ID {
		t.Fatalf("purged = %v, want [%s]", purged.IDs, doomed.ID)
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
	if n := count(`SELECT COUNT(*) FROM attachment_figure_counters c WHERE NOT EXISTS (SELECT 1 FROM artifacts a WHERE a.id = c.artifact_id)`); n != 0 {
		t.Errorf("%d orphaned figure counters after purge", n)
	}
	if n := count(`SELECT COUNT(*) FROM link_artifacts la WHERE NOT EXISTS (SELECT 1 FROM links l WHERE l.id = la.link_id)`); n != 0 {
		t.Errorf("%d version records of a purged link left after purge", n)
	}
	if n := count(`SELECT COUNT(*) FROM links WHERE id = $1`, crossing); n != 0 {
		t.Errorf("the link from the kept workspace to the purged one survived purge")
	}

	// The other workspace is intact.
	if n := count(`SELECT COUNT(*) FROM projects WHERE id = $1`, keptProject); n != 1 {
		t.Errorf("kept project lost")
	}
	if n := count(`SELECT COUNT(*) FROM artifacts WHERE id = $1`, keptArtifact); n != 1 {
		t.Errorf("kept artifact lost")
	}
	if n := count(`SELECT COUNT(*) FROM attachment_figure_counters WHERE artifact_id = $1`, keptArtifact); n != 1 {
		t.Errorf("kept figure counter lost")
	}
	if n := count(`SELECT COUNT(*) FROM link_artifacts la JOIN links l ON l.id = la.link_id
		JOIN artifacts a ON a.id = l.to_id WHERE a.project_id = $1`, keptProject); n != 2 {
		t.Errorf("kept link's version records: %d, want 2", n)
	}
	if role, _ := svc.RoleInOrg(kept.ID, userID); role != orgs.RoleAdmin {
		t.Errorf("kept org membership lost")
	}
}
