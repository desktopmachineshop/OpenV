package postgres

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// StoredFileReferences answers every stored file a row names, each once and
// sorted: a figure's current file and every version's, an evidence file, a
// workspace logo and a profile picture, under the path the row holds; and an
// error, with no path, when it cannot read them all (#379 question 48).
func TestStoredFileReferencesNamesEveryStoredFile(t *testing.T) {
	db := rtDB(t)
	org, user, project, artifact, attachment, bundle := uuid.New().String(), uuid.New().String(), uuid.New().String(),
		uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, org)
	rtSeedUser(t, db, user, "pic@example.com", "Pic", "")
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

	got, err := StoredFileReferences(db)
	if err != nil {
		t.Fatalf("StoredFileReferences: %v", err)
	}
	want := []string{"/u/evidence-1", "/u/org-logos/o.png", "/u/v2_f.png", "uploads/avatars/u.png", "uploads/v1_f.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StoredFileReferences = %q, want %q", got, want)
	}

	rtSeed(t, db, `ALTER TABLE attachment_versions RENAME TO attachment_versions_away`)
	if got, err := StoredFileReferences(db); err == nil || got != nil {
		t.Errorf("StoredFileReferences with a table it cannot read = %q, %v; want no path and the error", got, err)
	}
}
