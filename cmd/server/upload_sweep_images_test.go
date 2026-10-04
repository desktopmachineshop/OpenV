//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// The sweep's look into org-logos/ and avatars/ (#379 question 56),
// against a database of its own (OPENV_TEST_DATABASE_URL; skipped when
// unset): every rule of upload_sweep.go, in each of the two directories.

// imageDirs are the two directories, each with how a test gives an owner a
// row naming its file there (path "" for none), and the log's word for the
// files.
var imageDirs = []struct {
	name, what string
	owner      func(f *sweepFixture, path string) string
}{
	{"org-logos", "logo", (*sweepFixture).workspace},
	{"avatars", "profile picture", (*sweepFixture).account},
}

// workspace records a workspace whose logo is at path ("" for none), and
// answers its id.
func (f *sweepFixture) workspace(path string) string {
	f.t.Helper()
	id := uuid.New().String()
	f.exec(`INSERT INTO organizations (id, name, slug, logo_path) VALUES ($1, 'W', $2, $3)`, id, "w-"+id, path)
	return id
}

// account records an account whose profile picture is at path ("" for
// none), and answers its id.
func (f *sweepFixture) account(path string) string {
	f.t.Helper()
	id := uuid.New().String()
	f.exec(`INSERT INTO users (id, email, name, auth_provider, avatar_path, created_at, updated_at)
		VALUES ($1, $2, 'A', 'password', $3, NOW(), NOW())`, id, id+"@example.com", path)
	return id
}

// image writes a file named name in the image directory dir, last changed
// age before the boot, and answers its path.
func (f *sweepFixture) image(dir, name string, age time.Duration) string {
	f.t.Helper()
	return f.file(filepath.Join(dir, name), age)
}

// owned gives the image directory dir an owner whose row names its file
// there, <id>.png stored two hours ago, and answers the file's path.
func (f *sweepFixture) owned(dir string, owner func(*sweepFixture, string) string) string {
	f.t.Helper()
	id := owner(f, "")
	f.retarget(dir, id, filepath.Join(f.dir, dir, id+".png"))
	return f.image(dir, id+".png", 2*time.Hour)
}

// retarget points the row of owner id in dir's table at path.
func (f *sweepFixture) retarget(dir, id, path string) {
	f.t.Helper()
	if dir == "org-logos" {
		f.exec(`UPDATE organizations SET logo_path = $2 WHERE id = $1`, id, path)
		return
	}
	f.exec(`UPDATE users SET avatar_path = $2 WHERE id = $1`, id, path)
}

// oldSymlink makes link point at target, the link itself last changed age
// before the boot, so that only rule 1 keeps it.
func (f *sweepFixture) oldSymlink(target, link string, age time.Duration) {
	f.t.Helper()
	if err := os.Symlink(target, link); err != nil {
		f.t.Fatal(err)
	}
	stamp := unix.NsecToTimeval(f.began.Add(-age).UnixNano())
	if err := unix.Lutimes(link, []unix.Timeval{stamp, stamp}); err != nil {
		f.t.Fatal(err)
	}
}

// oldDir makes the directory path, last changed age before the boot.
func (f *sweepFixture) oldDir(path string, age time.Duration) {
	f.t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chtimes(path, f.began.Add(-age), f.began.Add(-age)); err != nil {
		f.t.Fatal(err)
	}
}

// In org-logos/ and avatars/ the sweep removes a logo (a profile picture)
// whose workspace (account) has no row, last changed more than an hour
// before the boot (#379 question 56), in each of the four types the uploads
// write; and keeps every other file: one whose owner has a row, the file
// its row names or another (rule 4), a soft-deleted workspace's logo among
// them; one changed within the hour (rule 3); one not named as the server
// names a logo or picture (rule 2); and a symlink, what it points at, a
// directory and what is in one (rule 1). Each removal is logged and
// counted on the summary line.
func TestTheUploadSweepRemovesLogosAndPicturesNoRowOwns(t *testing.T) {
	f := newSweepFixture(t)
	log := captureSweepLog(t)
	old := 2 * time.Hour
	outside := t.TempDir()
	type kept struct{ what, path string }
	var keep []kept
	var gone []string
	for _, d := range imageDirs {
		named := f.owned(d.name, d.owner)
		other := d.owner(f, "")
		keep = append(keep,
			kept{"a " + d.what + " its row names", named},
			kept{"a " + d.what + " of an owner with a row that names none", f.image(d.name, other+".jpg", old)},
			kept{"a " + d.what + " changed within the hour", f.image(d.name, uuid.New().String()+".png", uploadSweepMargin-time.Minute)},
			kept{"a " + d.what + " changed after the boot began", f.image(d.name, uuid.New().String()+".png", -time.Minute)},
		)
		for _, ext := range []string{".png", ".jpg", ".gif", ".webp"} {
			gone = append(gone, f.image(d.name, uuid.New().String()+ext, old))
		}
		for _, name := range []string{
			strings.ToUpper(uuid.New().String()) + ".png", uuid.New().String() + ".PNG", uuid.New().String() + ".bmp",
			uuid.New().String() + ".svg", uuid.New().String() + ".png.bak", uuid.New().String(), "logo.png", "notes.txt",
			uuid.New().String() + "_figure.png", "evidence-" + uuid.New().String(),
		} {
			keep = append(keep, kept{"a file not named as a " + d.what, f.image(d.name, name, old)})
		}
		target := filepath.Join(outside, uuid.New().String()+".png")
		if err := os.WriteFile(target, []byte("not ours"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(f.dir, d.name, uuid.New().String()+".png")
		f.oldSymlink(target, link, old)
		ownDir := filepath.Join(f.dir, d.name, uuid.New().String()+".png")
		inside := f.file(filepath.Join(d.name, filepath.Base(ownDir), uuid.New().String()+".png"), old)
		f.oldDir(ownDir, old)
		emptyDir := filepath.Join(f.dir, d.name, uuid.New().String()+".gif")
		f.oldDir(emptyDir, old)
		keep = append(keep, kept{"a symlink", link}, kept{"what a symlink points at", target},
			kept{"a directory", ownDir}, kept{"a file in a directory", inside}, kept{"an empty directory", emptyDir})
	}
	deleted := f.workspace(filepath.Join(f.dir, "org-logos", "x.png"))
	f.exec(`UPDATE organizations SET deleted_at = NOW() WHERE id = $1`, deleted)
	f.retarget("org-logos", deleted, filepath.Join(f.dir, "org-logos", deleted+".webp"))
	keep = append(keep, kept{"a soft-deleted workspace's logo", f.image("org-logos", deleted+".webp", old)})

	f.sweep()
	f.wantGone("a logo or picture no row owns", gone...)
	for _, k := range keep {
		f.wantKept(k.what, k.path)
	}
	for _, p := range gone {
		if !strings.Contains(log.String(), p) {
			t.Errorf("the log does not name %s as removed:\n%s", p, log)
		}
	}
	if !strings.Contains(log.String(), " logos_removed=4 avatars_removed=4 ") {
		t.Errorf("the summary line does not count four logos and four pictures removed:\n%s", log)
	}
	outcome, ok := f.recorded()
	if !ok || !strings.Contains(outcome, "org-logos: removed 4 logos (") || !strings.Contains(outcome, "avatars: removed 4 profile pictures (") {
		t.Errorf("the sweep's record: %q, %v; want four logos and four pictures removed", outcome, ok)
	}
}

// An org-logos/ (avatars/) that is not a directory of its own, a symlink to
// another directory say, is not looked in (rule 1): what is there stays,
// and the other directory is swept as ever.
func TestTheUploadSweepLooksInNoSymlinkedImageDirectory(t *testing.T) {
	for _, d := range imageDirs {
		t.Run(d.name, func(t *testing.T) {
			f := newSweepFixture(t)
			elsewhere := t.TempDir()
			f.owned(d.name, d.owner) // the owner's file lands in the linked directory
			if err := os.Rename(filepath.Join(f.dir, d.name), filepath.Join(elsewhere, d.name)); err != nil {
				t.Fatal(err)
			}
			f.oldSymlink(filepath.Join(elsewhere, d.name), filepath.Join(f.dir, d.name), 2*time.Hour)
			orphan := filepath.Join(elsewhere, d.name, uuid.New().String()+".png")
			if err := os.WriteFile(orphan, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			old := f.began.Add(-2 * time.Hour)
			if err := os.Chtimes(orphan, old, old); err != nil {
				t.Fatal(err)
			}
			for _, o := range imageDirs {
				if o.name != d.name {
					f.owned(o.name, o.owner)
				}
			}
			f.sweep()
			f.wantKept("a "+d.what+" no row owns, in a symlinked directory,", orphan)
			if outcome, _ := f.recorded(); !strings.Contains(outcome, d.name+": nothing to remove: "+filepath.Join(f.dir, d.name)+" is not a directory of its own") {
				t.Errorf("the sweep's record: %q, want %s not looked in", outcome, d.name)
			}
		})
	}
}

// When the workspaces or accounts cannot be read, nothing is removed in any
// directory and the sweep is not recorded, so the next boot sweeps (rule
// 5).
func TestTheUploadSweepRemovesNothingWhenAnOwnerCannotBeRead(t *testing.T) {
	for _, table := range []string{"organizations", "users"} {
		t.Run(table, func(t *testing.T) {
			f := newSweepFixture(t)
			var orphans []string
			for _, d := range imageDirs {
				f.owned(d.name, d.owner)
				orphans = append(orphans, f.image(d.name, uuid.New().String()+".png", 2*time.Hour))
			}
			f.figure(f.file(storedName("named.png"), 2*time.Hour))
			orphans = append(orphans, f.file(storedName("orphan.png"), 2*time.Hour))

			f.exec(`ALTER TABLE ` + table + ` RENAME TO ` + table + `_away`)
			f.sweep()
			f.wantKept("a file no row names, when the "+table+" could not be read,", orphans...)
			if outcome, ok := f.recorded(); ok {
				t.Errorf("a sweep that could not read the %s was recorded: %q", table, outcome)
			}
			f.exec(`ALTER TABLE ` + table + `_away RENAME TO ` + table)
			f.sweep()
			f.wantGone("a file no row names, at the next boot,", orphans...)
		})
	}
}

// When the database names no logo (no profile picture), nothing shows that
// org-logos/ (avatars/) is its own, and nothing there is removed (rule 6);
// the other directory is swept as ever.
func TestTheUploadSweepRemovesNoImageWhenTheDatabaseNamesNone(t *testing.T) {
	for _, d := range imageDirs {
		t.Run(d.name, func(t *testing.T) {
			f := newSweepFixture(t)
			d.owner(f, "")
			kept := f.image(d.name, uuid.New().String()+".png", 2*time.Hour)
			var swept string
			for _, o := range imageDirs {
				if o.name != d.name {
					f.owned(o.name, o.owner)
					swept = f.image(o.name, uuid.New().String()+".png", 2*time.Hour)
				}
			}
			f.sweep()
			f.wantKept("a "+d.what+", when the database names none,", kept)
			f.wantGone("a file no row owns in the other directory", swept)
			want := fmt.Sprintf("%s: nothing removed: the database names no %s, so nothing shows that %s holds its %ss; its 1 are kept",
				d.name, d.what, filepath.Join(f.dir, d.name), d.what)
			if outcome, _ := f.recorded(); !strings.Contains(outcome, want) {
				t.Errorf("the sweep's record: %q, want %q", outcome, want)
			}
		})
	}
}

// When more than one in ten of the logos (pictures) the database names are
// not in org-logos/ (avatars/), its names look like another directory's,
// and nothing there is removed (rule 7). One in ten missing is still this
// directory.
func TestTheUploadSweepRemovesNoImageWhenTheNamesAreAnotherDirectorys(t *testing.T) {
	for _, d := range imageDirs {
		for _, c := range []struct {
			missing int
			swept   bool
		}{{1, true}, {2, false}, {10, false}} {
			t.Run(fmt.Sprintf("%s/%d in ten missing", d.name, c.missing), func(t *testing.T) {
				f := newSweepFixture(t)
				for i := 0; i < 10; i++ {
					if i < c.missing {
						id := d.owner(f, "")
						f.retarget(d.name, id, filepath.Join("/srv/old-uploads", d.name, id+".png"))
						continue
					}
					f.owned(d.name, d.owner)
				}
				orphan := f.image(d.name, uuid.New().String()+".png", 2*time.Hour)
				f.sweep()
				if c.swept {
					f.wantGone("a "+d.what+" no row owns", orphan)
					return
				}
				f.wantKept("a "+d.what+" no row owns, the names being another directory's,", orphan)
				want := fmt.Sprintf("%s: nothing removed: %d of the 10 %ss the database names are not in %s", d.name, c.missing, d.what,
					filepath.Join(f.dir, d.name))
				if outcome, _ := f.recorded(); !strings.Contains(outcome, want) {
					t.Errorf("the sweep's record: %q, want %q...", outcome, want)
				}
			})
		}
	}
}

// storedImageOwner knows the names the logo and picture uploads write, and
// nothing else (rule 2).
func TestStoredImageOwner(t *testing.T) {
	id := "0f8c2a3e-6a54-4c1b-9d7e-2b1f0c9e8a71"
	for name, want := range map[string]string{
		id + ".png":                              id,
		id + ".jpg":                              id,
		id + ".gif":                              id,
		id + ".webp":                             id,
		id + ".jpeg":                             "",
		id + ".PNG":                              "",
		id + ".svg":                              "",
		id + ".png.bak":                          "",
		id:                                       "",
		strings.ToUpper(id) + ".png":             "",
		"x" + id + ".png":                        "",
		id + "_x.png":                            "",
		"{" + id + "}.png":                       "",
		strings.ReplaceAll(id, "-", "") + ".png": "",
	} {
		got, ok := storedImageOwner(name)
		if got != want || ok != (want != "") {
			t.Errorf("storedImageOwner(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}
