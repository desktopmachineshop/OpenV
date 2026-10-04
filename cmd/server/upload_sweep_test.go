//go:build unix

package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"

	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The sweep of the stored files no row names (#379 question 48), against a
// database of its own (OPENV_TEST_DATABASE_URL; skipped when unset) and an
// uploads directory of the test's own, one rule of upload_sweep.go at a
// time.

// sweepFixture is a migrated database, an uploads directory, and the
// instant the boot began.
type sweepFixture struct {
	t     *testing.T
	db    *sql.DB
	dir   string
	began time.Time
	// org is a workspace with a project, artifact an artifact of that
	// project for figure rows to hang off, and bundle an evidence bundle of
	// it.
	org, artifact, bundle string
}

func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	db, err := sql.Open("postgres", freshDatabase(t).url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := postgres.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	f := &sweepFixture{t: t, db: db, dir: t.TempDir(), began: time.Now(),
		org: uuid.New().String(), artifact: uuid.New().String(), bundle: uuid.New().String()}
	project := uuid.New().String()
	f.exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Sweep', $2)`, f.org, "sweep-"+f.org)
	f.exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'P')`, project, f.org)
	f.exec(`INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R')`, f.artifact, project)
	f.exec(`INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at) VALUES ($1, $2, 'EVD-1', 'E', NOW(), NOW())`,
		f.bundle, project)
	return f
}

func (f *sweepFixture) exec(query string, args ...interface{}) {
	f.t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

// file writes a stored file named name in the uploads directory, last
// changed age before the boot began, and answers its path.
func (f *sweepFixture) file(name string, age time.Duration) string {
	f.t.Helper()
	path := filepath.Join(f.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stored bytes"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	at := f.began.Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// figure records a figure whose current file is at path.
func (f *sweepFixture) figure(path string) {
	f.t.Helper()
	f.exec(`INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size) VALUES ($1, $2, 'f.png', 'image/png', $3, 1)`,
		uuid.New().String(), f.artifact, path)
}

// version records an earlier version, at path, of a figure whose current
// file is another, stored now, which it answers.
func (f *sweepFixture) version(path string) string {
	f.t.Helper()
	id, current := uuid.New().String(), f.file(storedName("current.png"), 2*time.Hour)
	f.exec(`INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size, version) VALUES ($1, $2, 'f.png', 'image/png', $3, 1, 2)`,
		id, f.artifact, current)
	f.exec(`INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size) VALUES ($1, $2, 1, 'f.png', 'image/png', $3, 1)`,
		uuid.New().String(), id, path)
	return current
}

// evidence records an evidence file at path.
func (f *sweepFixture) evidence(path string) {
	f.t.Helper()
	f.exec(`INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at) VALUES ($1, $2, 'bench.log', $3, NOW())`,
		uuid.New().String(), f.bundle, path)
}

// sweep runs the sweep as a boot does.
func (f *sweepFixture) sweep() {
	f.t.Helper()
	sweepUnreferencedUploads(f.db, f.dir, f.began)
}

// recorded answers the outcome boot_tasks records for the sweep, and
// whether it records one.
func (f *sweepFixture) recorded() (string, bool) {
	f.t.Helper()
	var outcome string
	switch err := f.db.QueryRow(`SELECT outcome FROM boot_tasks WHERE name = $1`, uploadSweepTask).Scan(&outcome); {
	case err == sql.ErrNoRows:
		return "", false
	case err != nil:
		f.t.Fatal(err)
	}
	return outcome, true
}

func (f *sweepFixture) wantGone(what string, paths ...string) {
	f.t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			f.t.Errorf("%s %s after the sweep: %v, want it removed", what, filepath.Base(p), err)
		}
	}
}

func (f *sweepFixture) wantKept(what string, paths ...string) {
	f.t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			f.t.Errorf("%s %s after the sweep: %v, want it kept", what, filepath.Base(p), err)
		}
	}
}

func storedName(rest string) string { return uuid.New().String() + "_" + rest }

func evidenceName() string { return "evidence-" + uuid.New().String() }

// The sweep removes the stored figures and evidence files no row names,
// last changed more than an hour before the boot, and keeps everything
// else: a file a row names under the path it has now, under the uploads
// directory it was stored in before UPLOADS_DIR changed (relative or
// absolute), or under its uuid with the rest of its name normalised
// otherwise (rule 4); a file not named as the server names a stored upload
// (rule 2); a file changed within the hour (rule 3); and a directory, what
// is in one, a symlink and what it points at (rule 1). It is recorded, and
// a later boot does not sweep again.
func TestTheUploadSweepRemovesOnlyStoredFilesNoRowNames(t *testing.T) {
	f := newSweepFixture(t)
	old := 2 * time.Hour

	current := f.file(storedName("brake.png"), old)
	f.figure(current)
	moved := storedName("brake-v1.png")
	movedPath := f.file(moved, old)
	movedCurrent := f.version("uploads/" + moved) // stored when UPLOADS_DIR was ./uploads
	renamed := uuid.New().String()
	renamedPath := f.file(renamed+"_Bremsé.png", old)
	f.figure("/data/uploads/" + renamed + "_Bremsé.png")
	evidence := f.file(evidenceName(), old)
	f.evidence(evidence)

	orphanFigure := f.file(storedName("deleted project.png"), old)
	orphanEvidence := f.file(evidenceName(), old)
	orphanVersion := f.file(storedName("an earlier version.step"), uploadSweepMargin+time.Minute)

	recent := f.file(storedName("uploading.png"), uploadSweepMargin-time.Minute)
	future := f.file(storedName("clock skew.png"), -time.Hour)
	notOurs := []string{
		f.file("backup.sql", old),
		f.file("notes.txt", old),
		f.file(strings.ToUpper(uuid.New().String())+"_shouting.png", old),
		f.file(uuid.New().String()+"_", old),
		f.file("evidence-"+uuid.New().String()+".log", old),
	}
	ourDir, emptyDir := filepath.Join(f.dir, storedName("a directory.png")), filepath.Join(f.dir, storedName("an empty directory.png"))
	inSubdirectories := []string{
		f.file(filepath.Join("org-logos", f.org+".png"), old),
		f.file(filepath.Join("avatars", uuid.New().String()+".png"), old),
		f.file(filepath.Join(filepath.Base(ourDir), storedName("inside.png")), old),
		ourDir, emptyDir,
	}
	if err := os.Mkdir(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{ourDir, emptyDir} { // old, as a file left behind would be
		if err := os.Chtimes(d, f.began.Add(-old), f.began.Add(-old)); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), storedName("outside.png"))
	if err := os.WriteFile(outside, []byte("not ours"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outside, f.began.Add(-old), f.began.Add(-old)); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.dir, storedName("a link.png"))
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	// The link itself old too, so only rule 1 keeps it.
	stamp := unix.NsecToTimeval(f.began.Add(-old).UnixNano())
	if err := unix.Lutimes(link, []unix.Timeval{stamp, stamp}); err != nil {
		t.Fatal(err)
	}

	f.sweep()
	f.wantGone("a stored file no row names", orphanFigure, orphanEvidence, orphanVersion)
	f.wantKept("a stored file a row names", current, movedPath, movedCurrent, renamedPath, evidence)
	f.wantKept("a stored file changed within the hour", recent, future)
	f.wantKept("a file not named as a stored upload", notOurs...)
	f.wantKept("a directory, or a file in one,", inSubdirectories...)
	f.wantKept("a symlink, or what it points at,", link, outside)
	outcome, ok := f.recorded()
	if !ok || !strings.HasPrefix(outcome, "removed 3 stored files (36 bytes) no row names from ") {
		t.Errorf("the sweep's record: %q, %v; want it recorded as removing the three files", outcome, ok)
	}

	later := f.file(storedName("orphaned after the sweep.png"), old)
	f.sweep()
	f.wantKept("a file no row names, left after the sweep ran,", later)
}

// When the database's paths cannot all be read, nothing is removed and the
// sweep is not recorded, so the next boot sweeps (rule 5).
func TestTheUploadSweepRemovesNothingWhenAPathCannotBeRead(t *testing.T) {
	f := newSweepFixture(t)
	named := f.file(storedName("named.png"), 2*time.Hour)
	f.figure(named)
	orphan := f.file(storedName("orphan.png"), 2*time.Hour)

	f.exec(`ALTER TABLE evidence_files RENAME TO evidence_files_away`)
	f.sweep()
	f.wantKept("a stored file no row names, when the paths could not be read,", orphan)
	if outcome, ok := f.recorded(); ok {
		t.Errorf("a sweep that could not read the paths was recorded: %q", outcome)
	}

	f.exec(`ALTER TABLE evidence_files_away RENAME TO evidence_files`)
	f.sweep()
	f.wantGone("a stored file no row names, at the next boot,", orphan)
	f.wantKept("a stored file a row names", named)
}

// When more than one in ten of the figures and evidence files the database
// names are not in the uploads directory, the names look like another
// directory's, and nothing is removed (rule 7); the sweep is recorded with
// why. One in ten missing is still this directory.
func TestTheUploadSweepRemovesNothingWhenTheNamesAreAnotherDirectorys(t *testing.T) {
	for _, c := range []struct {
		name    string
		missing int
		swept   bool
	}{
		{"one in ten missing", 1, true},
		{"two in ten missing", 2, false},
		{"every one missing", 10, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newSweepFixture(t)
			for i := 0; i < 10; i++ {
				name := storedName(fmt.Sprintf("figure %d.png", i))
				if i < c.missing {
					f.figure(filepath.Join("/srv/old-uploads", name))
					continue
				}
				f.figure(f.file(name, 2*time.Hour))
			}
			orphan := f.file(storedName("orphan.png"), 2*time.Hour)
			f.sweep()
			outcome, ok := f.recorded()
			if c.swept {
				f.wantGone("a stored file no row names", orphan)
				return
			}
			f.wantKept("a stored file no row names, the names being another directory's,", orphan)
			want := fmt.Sprintf("nothing removed: %d of the 10 figures and evidence files the database names are not in ", c.missing)
			if !ok || !strings.HasPrefix(outcome, want) {
				t.Errorf("the sweep's record: %q, %v; want %q...", outcome, ok, want)
			}
		})
	}
}

// When the database names no figure or evidence file, nothing shows that
// the uploads directory is its own (a new database beside another's
// uploads, say), and nothing is removed (rule 6); the sweep is recorded.
func TestTheUploadSweepRemovesNothingWhenTheDatabaseNamesNoFile(t *testing.T) {
	f := newSweepFixture(t)
	f.exec(`UPDATE organizations SET logo_path = $2, logo_mime = 'image/png' WHERE id = $1`, f.org,
		f.file(filepath.Join("org-logos", f.org+".png"), 2*time.Hour))
	orphans := []string{f.file(storedName("another database's.png"), 2*time.Hour), f.file(evidenceName(), 2*time.Hour)}
	f.sweep()
	f.wantKept("a stored file, when the database names none,", orphans...)
	if outcome, ok := f.recorded(); !ok || !strings.HasPrefix(outcome, "nothing removed: the database names no figure or evidence file") ||
		!strings.HasSuffix(outcome, "its 2 stored files are kept") {
		t.Errorf("the sweep's record: %q, %v; want it recorded as removing nothing of two files", outcome, ok)
	}
}

// A file is checked again just before it is removed: one that is no longer
// a plain file, a directory having taken its name since the listing, say,
// is kept, and the others still go.
func TestTheUploadSweepChecksAFileAgainBeforeRemovingIt(t *testing.T) {
	f := newSweepFixture(t)
	f.figure(f.file(storedName("named.png"), 2*time.Hour))
	orphan := f.file(storedName("orphan.png"), 2*time.Hour)
	stubborn := f.file(storedName("stubborn.png"), 2*time.Hour)
	// The listing's answer stays, but the file is no longer one to remove
	// by the time the sweep comes to it: it is checked again first.
	outcome, err := sweepUploads(f.dir, f.began.Add(-uploadSweepMargin), func() ([]string, error) {
		if err := os.Remove(stubborn); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stubborn, 0o755); err != nil {
			t.Fatal(err)
		}
		return postgres.StoredFileReferences(f.db)
	})
	if err != nil {
		t.Fatalf("sweepUploads: %v", err)
	}
	f.wantGone("a stored file no row names", orphan)
	f.wantKept("a directory that took a stored file's name since the listing", stubborn)
	if !strings.HasPrefix(outcome, "removed 1 stored files") {
		t.Errorf("the sweep answered %q, want one file removed", outcome)
	}
}

// storedUploadKey knows the server's names for stored uploads, and nothing
// else (rule 2).
func TestStoredUploadKey(t *testing.T) {
	id := "0f8c2a3e-6a54-4c1b-9d7e-2b1f0c9e8a71"
	for name, want := range map[string]string{
		id + "_brake.png":           id,
		id + "_":                    "",
		id + "_with\nline break":    id,
		"evidence-" + id:            "evidence-" + id,
		"evidence-" + id + "_x.png": "",
		"evidence-" + id + ".log":   "",
		id:                          "",
		strings.ToUpper(id) + "_x":  "",
		"x" + id + "_brake.png":     "",
		"brake.png":                 "",
	} {
		got, ok := storedUploadKey(name)
		if got != want || ok != (want != "") {
			t.Errorf("storedUploadKey(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}
