package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The sweep of the stored files no row names (#379 question 48). Deleting a
// project before #379's bug 136 was fixed, migration 0053's clean-up of the
// rows such deletes left, purging a workspace before bug 143 was, and
// deleting a figure before bug 145 was all took rows and left their files
// in the uploads directory; a purge also left the workspace's logo in
// org-logos/ (bug 158). Once per database, the first boot after the
// migrations removes them, and fails safe: when it cannot tell for certain
// that a file is one of this database's and that no row names it, it keeps
// the file. OPENV_UPLOAD_SWEEP=off skips it and records nothing, so it runs
// at the first boot without the setting (question 55).
//
// It looks in the uploads directory itself, at the figures and evidence
// files the server stores there, and in two of its subdirectories, at the
// files the server names after their owner: org-logos/<workspace id>.<ext>
// and avatars/<account id>.<ext> (question 56). Its rules, each of which
// keeps a file:
//
//  1. Only a regular file directly in one of those three directories is
//     considered: never a directory or anything in one, never a symlink
//     (nor what it points at), never anything else. A subdirectory that is
//     not a directory of its own, a symlink say, is not looked in.
//  2. Only a file named as the server names one is considered:
//     <uuid>_<the uploaded name> for a figure's version and evidence-<uuid>
//     for an evidence file in the uploads directory; <uuid>.<ext> in
//     org-logos/ and avatars/, the uuid lower case and the extension one
//     the logo and picture uploads write (png, jpg, gif, webp).
//  3. A file modified less than uploadSweepMargin before the boot began, or
//     since, stays: it may be an upload whose row another server process
//     has yet to write.
//  4. In the uploads directory, a row names a file by its path as
//     UPLOADS_DIR was when it was stored, absolute or relative, and
//     UPLOADS_DIR may have changed since; so a file is named by a row when
//     the last element of a row's path holds the same uuid, which the
//     server mints once per stored file, whatever the directory before it
//     and the rest of the name (which a file system may have normalised).
//     In org-logos/ and avatars/ a file is kept while its workspace or
//     account has a row, a soft-deleted workspace's included; only a file
//     whose uuid no workspace (no account) has is removed.
//  5. When the database's paths, workspaces and accounts cannot all be
//     read, or a directory cannot be listed, nothing is removed anywhere,
//     and the sweep is not recorded, so the next boot tries again.
//  6. In each directory: when the database names no file there at all (no
//     figure or evidence file; no logo; no picture), nothing there is
//     removed: nothing shows that the directory is this database's.
//  7. In each directory: when more than uploadSweepMaxMissingPercent of the
//     files the database names there are not in it, nothing there is
//     removed: the names look like another directory's (UPLOADS_DIR changed
//     and the files were not moved, say).
//
// Rules 6 and 7 decide for their directory: the sweep is recorded and does
// not run again, as it is when it has removed what it could. Each file
// removed is logged at info with its size, a file that will not go is
// logged and kept, and the sweep ends with one line counting what it did.
// Nothing here fails the boot.

// uploadSweepTask names the sweep in boot_tasks.
const uploadSweepTask = "sweep_unreferenced_uploads"

// uploadSweepSetting turns the sweep off (question 55): a deployment whose
// uploads directory another deployment shares must not sweep it.
const uploadSweepSetting = "OPENV_UPLOAD_SWEEP"

// uploadSweepMargin is how long before the boot a stored file must have
// last changed for the sweep to consider it (rule 3).
const uploadSweepMargin = time.Hour

// uploadSweepMaxMissingPercent is the share of the files the database names
// in a directory that may be missing from it before the sweep takes the
// names for another directory's (rule 7).
const uploadSweepMaxMissingPercent = 10

// storedUploadName matches the names the server stores uploads under (rule
// 2): evidence-<uuid> and <uuid>_<name>, the uuid lower case as
// uuid.New().String() writes it. A name may hold any byte but a slash.
var storedUploadName = regexp.MustCompile(`(?s)^(?:(evidence-` + uuidPattern + `)|(` + uuidPattern + `)_.+)$`)

// storedImageName matches the names of logos and profile pictures (rule 2):
// <uuid>.<ext>, as the API's orgLogoPath and avatarPath write them, with the
// extensions of its rasterImageExtensions.
var storedImageName = regexp.MustCompile(`^(` + uuidPattern + `)\.(?:png|jpg|gif|webp)$`)

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

// storedUploadKey answers the part of a stored upload's name the server
// minted for it, unique to the file (evidence-<uuid>, or the uuid of
// <uuid>_<name>), and whether name is a stored upload's at all.
func storedUploadKey(name string) (string, bool) {
	m := storedUploadName.FindStringSubmatch(name)
	switch {
	case m == nil:
		return "", false
	case m[1] != "":
		return m[1], true
	default:
		return m[2], true
	}
}

// storedImageOwner answers the uuid a logo's or picture's name gives its
// owner, and whether name is one the server writes at all.
func storedImageOwner(name string) (string, bool) {
	if m := storedImageName.FindStringSubmatch(name); m != nil {
		return m[1], true
	}
	return "", false
}

// imageDir is a subdirectory of the uploads directory whose files the
// server names after their owner.
type imageDir struct {
	name   string // the subdirectory
	what   string // its files, in a log line: "logo", "profile picture"
	owners string // their owners, in a log line: "workspace", "account"
	read   func(postgres.StoredFileNames) []postgres.ImageOwner
	tally  func(*sweepTally) *int // the summary's count of removals here
}

// uploadSweepImageDirs are the subdirectories the sweep looks in.
var uploadSweepImageDirs = []imageDir{
	{"org-logos", "logo", "workspace", func(n postgres.StoredFileNames) []postgres.ImageOwner { return n.Workspaces },
		func(t *sweepTally) *int { return &t.logos }},
	{"avatars", "profile picture", "account", func(n postgres.StoredFileNames) []postgres.ImageOwner { return n.Accounts },
		func(t *sweepTally) *int { return &t.avatars }},
}

// sweepUnreferencedUploads runs the sweep of dir, the uploads directory,
// once per database, unless on is false (OPENV_UPLOAD_SWEEP=off): then it
// says so and records nothing. began is when the boot began. A failure is
// logged, and the next boot runs the sweep again.
func sweepUnreferencedUploads(db *sql.DB, dir string, began time.Time, on bool) {
	if !on {
		slog.Info("upload sweep: off (" + uploadSweepSetting + "=off); nothing swept or recorded")
		return
	}
	if _, err := postgres.RunBootTaskOnce(db, uploadSweepTask, func() (string, error) {
		return sweepUploads(dir, began.Add(-uploadSweepMargin), func() (postgres.StoredFileNames, error) {
			return postgres.ReadStoredFileNames(db)
		})
	}); err != nil {
		slog.Warn("upload sweep: nothing removed; the next boot tries again", "dir", dir, "error", err)
	}
}

// sweepCandidate is a file the sweep may remove: its name, the key a row
// would name it by, and its size.
type sweepCandidate struct {
	name, key string
	size      int64
}

// sweepListing is one directory as the sweep found it.
type sweepListing struct {
	dir        string
	unlisted   string          // why the directory was not listed: "" when it was
	keys       map[string]bool // every server-named file's key, or every regular file's name in an image directory
	candidates []sweepCandidate
	recent     int // server-named files changed within the margin
}

// sweepTally is what the sweep did, for its summary line.
type sweepTally struct {
	removed, logos, avatars, failed, recent int
	bytes                                   int64
}

// sweepUploads removes, by the rules above, the stored files last changed
// before cutoff that nothing names read answers, from dir and its image
// subdirectories, and answers what it did. It lists the directories before
// it reads the names, so a file stored meanwhile is either too recent to
// consider or named by a row it reads. An error, a directory it cannot list
// or names it cannot read, comes with nothing removed.
func sweepUploads(dir string, cutoff time.Time, read func() (postgres.StoredFileNames, error)) (string, error) {
	top, err := listDir(dir, cutoff, false)
	if err != nil {
		return "", err
	}
	images := make([]sweepListing, len(uploadSweepImageDirs))
	for i, d := range uploadSweepImageDirs {
		if images[i], err = listDir(filepath.Join(dir, d.name), cutoff, true); err != nil {
			return "", err
		}
	}
	names, err := read()
	if err != nil {
		return "", err // rule 5
	}

	var tally sweepTally
	namedKeys := map[string]bool{}
	for _, p := range names.Paths {
		if key, ok := storedUploadKey(filepath.Base(p)); ok {
			namedKeys[key] = true
		}
	}
	outcomes := []string{sweepTop(top, namedKeys, &tally)}
	for i, d := range uploadSweepImageDirs {
		outcomes = append(outcomes, d.name+": "+sweepImages(images[i], d, d.read(names), &tally))
	}
	outcome := strings.Join(outcomes, "; ")
	slog.Info("upload sweep: done", "dir", dir, "removed", tally.removed, "logos_removed", tally.logos,
		"avatars_removed", tally.avatars, "bytes", tally.bytes, "failed", tally.failed, "kept_recent", tally.recent,
		"named", len(namedKeys))
	return outcome, nil
}

// listDir lists dir for the sweep: each regular file named as the server
// names a stored upload there (image: a logo or picture), keyed by what a
// row names it by, and those changed before cutoff as candidates. A
// directory that does not exist, or is not a directory of its own, is
// listed as empty (only an image directory may be either: the uploads
// directory is made at start-up).
func listDir(dir string, cutoff time.Time, image bool) (sweepListing, error) {
	l := sweepListing{dir: dir, keys: map[string]bool{}}
	if image {
		info, err := os.Lstat(dir)
		switch {
		case os.IsNotExist(err):
			l.unlisted = "there is no " + dir
			return l, nil
		case err != nil:
			return l, fmt.Errorf("failed to look at %s: %w", dir, err)
		case !info.IsDir():
			l.unlisted = dir + " is not a directory of its own" // rule 1: a symlink is not looked in
			return l, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return l, fmt.Errorf("failed to list %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue // rule 1
		}
		var key string
		var ok bool
		if image {
			l.keys[e.Name()] = true // rule 7 counts a row's file by its name
			key, ok = storedImageOwner(e.Name())
		} else {
			key, ok = storedUploadKey(e.Name())
			if ok {
				l.keys[key] = true
			}
		}
		if !ok {
			continue // rule 2
		}
		info, err := e.Info() // the entry's own, as listed: a symlink is not followed
		if err != nil {
			continue
		}
		if !info.ModTime().Before(cutoff) {
			l.recent++ // rule 3
			continue
		}
		l.candidates = append(l.candidates, sweepCandidate{e.Name(), key, info.Size()})
	}
	return l, nil
}

// sweepTop sweeps the uploads directory itself: the figures and evidence
// files no row names.
func sweepTop(l sweepListing, namedKeys map[string]bool, tally *sweepTally) string {
	tally.recent += l.recent
	if len(namedKeys) == 0 { // rule 6
		if len(l.keys) == 0 {
			outcome := fmt.Sprintf("nothing to remove: the database names no figure or evidence file, and %s holds none", l.dir)
			slog.Info("upload sweep: " + outcome)
			return outcome
		}
		outcome := fmt.Sprintf("nothing removed: the database names no figure or evidence file, so nothing shows that %s holds its files; its %d stored files are kept",
			l.dir, len(l.keys))
		slog.Warn("upload sweep: " + outcome)
		return outcome
	}
	missing := 0
	for key := range namedKeys {
		if !l.keys[key] {
			missing++
		}
	}
	if missing*100 > len(namedKeys)*uploadSweepMaxMissingPercent { // rule 7
		outcome := fmt.Sprintf("nothing removed: %d of the %d figures and evidence files the database names are not in %s, so its names look like another directory's",
			missing, len(namedKeys), l.dir)
		slog.Warn("upload sweep: " + outcome)
		return outcome
	}
	removed, bytes, failed := removeCandidates(l, func(c sweepCandidate) bool { return namedKeys[c.key] }, // rule 4
		"a stored file no row names")
	tally.removed += removed
	tally.bytes += bytes
	tally.failed += failed
	return fmt.Sprintf("removed %d stored files (%d bytes) no row names from %s; %d could not be removed, %d changed within %s of the boot were kept",
		removed, bytes, l.dir, failed, l.recent, uploadSweepMargin)
}

// sweepImages sweeps an image directory: the logos (pictures) of workspaces
// (accounts) no row has.
func sweepImages(l sweepListing, d imageDir, owners []postgres.ImageOwner, tally *sweepTally) string {
	tally.recent += l.recent
	if l.unlisted != "" {
		return "nothing to remove: " + l.unlisted
	}
	ids := map[string]bool{}
	named, missing := 0, 0
	for _, o := range owners {
		ids[o.ID] = true
		if o.Path == "" {
			continue
		}
		named++
		if !l.keys[filepath.Base(o.Path)] {
			missing++
		}
	}
	held := len(l.candidates) + l.recent
	if named == 0 { // rule 6
		if held == 0 {
			return fmt.Sprintf("nothing to remove: the database names no %s, and %s holds none", d.what, l.dir)
		}
		outcome := fmt.Sprintf("nothing removed: the database names no %s, so nothing shows that %s holds its %ss; its %d are kept",
			d.what, l.dir, d.what, held)
		slog.Warn("upload sweep: " + outcome)
		return outcome
	}
	if missing*100 > named*uploadSweepMaxMissingPercent { // rule 7
		outcome := fmt.Sprintf("nothing removed: %d of the %d %ss the database names are not in %s, so its names look like another directory's",
			missing, named, d.what, l.dir)
		slog.Warn("upload sweep: " + outcome)
		return outcome
	}
	removed, bytes, failed := removeCandidates(l, func(c sweepCandidate) bool { return ids[c.key] }, // rule 4
		"a "+d.what+" no "+d.owners+" has")
	*d.tally(tally) += removed
	tally.bytes += bytes
	tally.failed += failed
	return fmt.Sprintf("removed %d %ss (%d bytes) no %s has; %d could not be removed, %d changed within %s of the boot were kept",
		removed, d.what, bytes, d.owners, failed, l.recent, uploadSweepMargin)
}

// removeCandidates removes each of l's candidates that kept does not keep,
// once it has checked that it is still a plain file, logging each removal
// as what, and answers how many it removed, their bytes and how many would
// not go.
func removeCandidates(l sweepListing, kept func(sweepCandidate) bool, what string) (removed int, bytes int64, failed int) {
	for _, c := range l.candidates {
		if kept(c) {
			continue
		}
		path := filepath.Join(l.dir, c.name)
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue // gone, or no longer a plain file, since the listing
		}
		if err := os.Remove(path); err != nil {
			failed++
			slog.Warn("upload sweep: failed to remove "+what, "path", path, "error", err)
			continue
		}
		removed++
		bytes += c.size
		slog.Info("upload sweep: removed "+what, "path", path, "bytes", c.size)
	}
	return removed, bytes, failed
}
