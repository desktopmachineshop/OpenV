package postgres

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// ReadStoredFileNames answers every stored file a row names, each once and
// sorted: a figure's current file and every version's, an evidence file, a
// workspace logo and a profile picture, under the path the row holds (#379
// question 48); and every workspace, a soft-deleted one's included, and
// every account, by id, with its logo or picture (question 56). When it
// cannot read them all it answers the error and nothing else.
func TestReadStoredFileNamesNamesEveryStoredFileAndOwner(t *testing.T) {
	db := rtDB(t)
	org, user, project, artifact, attachment, bundle := uuid.New().String(), uuid.New().String(), uuid.New().String(),
		uuid.New().String(), uuid.New().String(), uuid.New().String()
	deletedOrg, plainUser := uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, org)
	rtSeedOrg(t, db, deletedOrg)
	rtSeed(t, db, `UPDATE organizations SET deleted_at = NOW() WHERE id = $1`, deletedOrg)
	rtSeedUser(t, db, user, "pic@example.com", "Pic", "")
	rtSeedUser(t, db, plainUser, "plain@example.com", "Plain", "")
	rtSeed(t, db, `UPDATE organizations SET logo_path = '/u/org-logos/o.png' WHERE id = $1`, org)
	rtSeed(t, db, `UPDATE users SET avatar_path = 'uploads/avatars/u.png' WHERE id = $1`, user)
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'P')`, project, org)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R')`, artifact, project)
	rtSeed(t, db, `INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size) VALUES ($1, $2, 'f', 'image/png', '/u/v2_f.png', 1)`,
		attachment, artifact)
	rtSeed(t, db, `INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
		VALUES ($1, $3, 1, 'f', 'image/png', 'uploads/v1_f.png', 1), ($2, $3, 2, 'f', 'image/png', '/u/v2_f.png', 1)`,
		uuid.New().String(), uuid.New().String(), attachment)
	rtSeed(t, db, `INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at) VALUES ($1, $2, 'EVD-1', 'E', NOW(), NOW())`,
		bundle, project)
	rtSeed(t, db, `INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at) VALUES ($1, $2, 'b.log', '/u/evidence-1', NOW())`,
		uuid.New().String(), bundle)

	got, err := ReadStoredFileNames(db)
	if err != nil {
		t.Fatalf("ReadStoredFileNames: %v", err)
	}
	want := []string{"/u/evidence-1", "/u/org-logos/o.png", "/u/v2_f.png", "uploads/avatars/u.png", "uploads/v1_f.png"}
	if !reflect.DeepEqual(got.Paths, want) {
		t.Errorf("ReadStoredFileNames paths = %q, want %q", got.Paths, want)
	}
	byID := func(owners []ImageOwner) map[string]string {
		m := map[string]string{}
		for _, o := range owners {
			m[o.ID] = o.Path
		}
		return m
	}
	if w := byID(got.Workspaces); len(w) != 2 || w[org] != "/u/org-logos/o.png" || w[deletedOrg] != "" {
		t.Errorf("ReadStoredFileNames workspaces = %v, want %s with its logo and the soft-deleted %s", got.Workspaces, org, deletedOrg)
	}
	if a := byID(got.Accounts); len(a) != 2 || a[user] != "uploads/avatars/u.png" || a[plainUser] != "" {
		t.Errorf("ReadStoredFileNames accounts = %v, want %s with its picture and %s with none", got.Accounts, user, plainUser)
	}

	for _, table := range []string{"attachment_versions", "users"} {
		rtSeed(t, db, `ALTER TABLE `+table+` RENAME TO `+table+`_away`)
		if got, err := ReadStoredFileNames(db); err == nil || got.Paths != nil || got.Workspaces != nil || got.Accounts != nil {
			t.Errorf("ReadStoredFileNames with %s unreadable = %+v, %v; want nothing and the error", table, got, err)
		}
		rtSeed(t, db, `ALTER TABLE `+table+`_away RENAME TO `+table)
	}
}
