package postgres

import (
	"database/sql"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// purgeFilesSeed puts in workspace orgID a project whose artifact has a
// figure in two versions and whose evidence bundle has a file, gives the
// workspace a logo, and answers every stored file it named, named by tag,
// sorted.
func purgeFilesSeed(t *testing.T, db *sql.DB, orgID, tag string) []string {
	t.Helper()
	projectID, artifactID, attachmentID, bundleID := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	v1, v2 := "/uploads/"+tag+"-fig-v1.png", "/uploads/"+tag+"-fig-v2.png"
	evidence, logo := "/uploads/evidence-"+tag, "/uploads/org-logos/"+tag+".png"
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'P')`, projectID, orgID)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R')`, artifactID, projectID)
	rtSeed(t, db, `INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size, version)
		VALUES ($1, $2, 'f.png', 'image/png', $3, 1, 2)`, attachmentID, artifactID, v2)
	rtSeed(t, db, `INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
		VALUES ($1, $3, 1, 'f.png', 'image/png', $4, 1), ($2, $3, 2, 'f.png', 'image/png', $5, 1)`,
		uuid.New().String(), uuid.New().String(), attachmentID, v1, v2)
	rtSeed(t, db, `INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at) VALUES ($1, $2, 'EVD-1', 'E', NOW(), NOW())`,
		bundleID, projectID)
	rtSeed(t, db, `INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at) VALUES ($1, $2, 'bench.log', $3, NOW())`,
		uuid.New().String(), bundleID, evidence)
	rtSeed(t, db, `UPDATE organizations SET logo_path = $2, logo_mime = 'image/png' WHERE id = $1`, orgID, logo)
	files := []string{evidence, v1, v2, logo}
	sort.Strings(files)
	return files
}

// Purging a workspace answers the stored files of everything it purged
// (#379 bug 143: the purge left every file of the workspace on disk): every
// version of its figures, its evidence files and its logo, each once and
// sorted, and not another workspace's.
func TestPurgingAWorkspaceAnswersItsStoredFiles(t *testing.T) {
	db := rtDB(t)
	repo := NewOrgRepository(db)
	svc := orgs.NewDefaultService(repo)
	doomed, kept, empty := uuid.New().String(), uuid.New().String(), uuid.New().String()
	for _, id := range []string{doomed, kept, empty} {
		rtSeedOrg(t, db, id)
	}
	want := purgeFilesSeed(t, db, doomed, "doomed")
	purgeFilesSeed(t, db, kept, "kept")
	rtSeed(t, db, `UPDATE organizations SET deleted_at = NOW() - INTERVAL '31 days' WHERE id IN ($1, $2)`, doomed, empty)

	purged, err := svc.PurgeExpired(time.Now())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if len(purged.IDs) != 2 {
		t.Errorf("purged %q, want the two expired workspaces", purged.IDs)
	}
	if !reflect.DeepEqual(purged.Files, want) {
		t.Errorf("the purge answered the files %q, want the purged workspace's: %q", purged.Files, want)
	}
	for _, table := range []string{"attachments", "attachment_versions", "evidence_files"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("the kept workspace has no %s rows after the purge", table)
		}
	}
	var logo string
	if err := db.QueryRow(`SELECT logo_path FROM organizations WHERE id = $1`, kept).Scan(&logo); err != nil || logo == "" {
		t.Errorf("the kept workspace's logo after the purge: %q, %v", logo, err)
	}
}
