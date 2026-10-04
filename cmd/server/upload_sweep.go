package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The sweep of the stored files no row names (#379 question 48). Deleting a
// project before #379's bug 136 was fixed, migration 0053's clean-up of the
// rows such deletes left, purging a workspace before bug 143 was, and
// deleting a figure before bug 145 was all took rows and left their files
// in the uploads directory. Once per database, the first boot after the
// migrations removes them, and fails safe: when it cannot tell for certain
// that a file is one of this database's and that no row names it, it keeps
// the file. Its rules, each of which keeps a file:
//
//  1. Only a regular file directly in the uploads directory is considered:
//     never a directory or anything in one (the workspace logos and profile
//     pictures live in org-logos/ and avatars/), never a symlink (nor what
//     it points at), never anything else.
//  2. Only a file named as the server names a stored upload is considered:
//     <uuid>_<the uploaded name> for a figure's version, evidence-<uuid>
//     for an evidence file. Anything else someone put there stays.
//  3. A file modified less than uploadSweepMargin before the boot began, or
//     since, stays: it may be an upload whose row another server process
//     has yet to write.
//  4. A row names a file by its path as UPLOADS_DIR was when it was stored,
//     absolute or relative, and UPLOADS_DIR may have changed since; so a
//     file is named by a row when the last element of a row's path holds
//     the same uuid, which the server mints once per stored file, whatever
//     the directory before it and the rest of the name (which a file system
//     may have normalised). A file whose whole name a row ends in is so
//     named too.
//  5. When the database's paths cannot all be read, nothing is removed, and
//     the sweep is not recorded, so the next boot tries again.
//  6. When the database names no figure or evidence file at all, nothing is
//     removed: nothing shows that the directory is this database's.
//  7. When more than uploadSweepMaxMissingPercent of the figures and
//     evidence files the database names are not in the directory, nothing
//     is removed: the names look like another directory's (UPLOADS_DIR
//     changed and the files were not moved, say).
//
// Rules 6 and 7 decide the sweep: it is recorded and does not run again, as
// it is when it has removed what it could. Each file removed is logged at
// info with its size, a file that will not go is logged and kept, and the
// sweep ends with one line counting what it did. Nothing here fails the
// boot.

// uploadSweepTask names the sweep in boot_tasks.
const uploadSweepTask = "sweep_unreferenced_uploads"

// uploadSweepMargin is how long before the boot a stored file must have
// last changed for the sweep to consider it (rule 3).
const uploadSweepMargin = time.Hour

// uploadSweepMaxMissingPercent is the share of the figures and evidence
// files the database names that may be missing from the uploads directory
// before the sweep takes the names for another directory's (rule 7).
const uploadSweepMaxMissingPercent = 10

// storedUploadName matches the names the server stores uploads under (rule
// 2): evidence-<uuid> and <uuid>_<name>, the uuid lower case as
// uuid.New().String() writes it. A name may hold any byte but a slash.
var storedUploadName = regexp.MustCompile(`(?s)^(?:(evidence-` + uuidPattern + `)|(` + uuidPattern + `)_.+)$`)

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

// sweepUnreferencedUploads runs the sweep of dir, the uploads directory,
// once per database: began is when the boot began. A failure is logged,
// and the next boot runs the sweep again.
func sweepUnreferencedUploads(db *sql.DB, dir string, began time.Time) {
	if _, err := postgres.RunBootTaskOnce(db, uploadSweepTask, func() (string, error) {
		return sweepUploads(dir, began.Add(-uploadSweepMargin), func() ([]string, error) {
			return postgres.StoredFileReferences(db)
		})
	}); err != nil {
		slog.Warn("upload sweep: nothing removed; the next boot tries again", "dir", dir, "error", err)
	}
}

// sweepUploads removes from dir the stored uploads last changed before
// cutoff that none of the paths references answers names, by the rules
// above, and answers what it did. It lists the directory before it reads
// the paths, so a file stored meanwhile is either too recent to consider or
// named by a row it reads. An error, a directory it cannot list or paths it
// cannot read, comes with nothing removed.
func sweepUploads(dir string, cutoff time.Time, references func() ([]string, error)) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to list the uploads directory: %w", err)
	}
	type candidate struct {
		name, key string
		size      int64
	}
	var candidates []candidate
	inDir := map[string]bool{} // the keys of every stored upload in dir
	recent := 0
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue // rule 1
		}
		key, ok := storedUploadKey(e.Name())
		if !ok {
			continue // rule 2
		}
		inDir[key] = true
		info, err := e.Info() // the entry's own, as listed: a symlink is not followed
		if err != nil {
			continue
		}
		if !info.ModTime().Before(cutoff) {
			recent++ // rule 3
			continue
		}
		candidates = append(candidates, candidate{e.Name(), key, info.Size()})
	}

	paths, err := references()
	if err != nil {
		return "", err // rule 5
	}
	namedKeys := map[string]bool{}
	for _, p := range paths {
		if key, ok := storedUploadKey(filepath.Base(p)); ok {
			namedKeys[key] = true
		}
	}
	if len(namedKeys) == 0 { // rule 6
		if len(inDir) == 0 {
			outcome := fmt.Sprintf("nothing to remove: the database names no figure or evidence file, and %s holds none", dir)
			slog.Info("upload sweep: " + outcome)
			return outcome, nil
		}
		outcome := fmt.Sprintf("nothing removed: the database names no figure or evidence file, so nothing shows that %s holds its files; its %d stored files are kept",
			dir, len(inDir))
		slog.Warn("upload sweep: " + outcome)
		return outcome, nil
	}
	missing := 0
	for key := range namedKeys {
		if !inDir[key] {
			missing++
		}
	}
	if missing*100 > len(namedKeys)*uploadSweepMaxMissingPercent {
		outcome := fmt.Sprintf("nothing removed: %d of the %d figures and evidence files the database names are not in %s, so its names look like another directory's",
			missing, len(namedKeys), dir)
		slog.Warn("upload sweep: " + outcome)
		return outcome, nil // rule 7
	}

	var removed, failed int
	var bytes int64
	for _, c := range candidates {
		if namedKeys[c.key] {
			continue // rule 4
		}
		path := filepath.Join(dir, c.name)
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue // gone, or no longer a plain file, since the listing
		}
		if err := os.Remove(path); err != nil {
			failed++
			slog.Warn("upload sweep: failed to remove a stored file no row names", "path", path, "error", err)
			continue
		}
		removed++
		bytes += c.size
		slog.Info("upload sweep: removed a stored file no row names", "path", path, "bytes", c.size)
	}
	outcome := fmt.Sprintf("removed %d stored files (%d bytes) no row names from %s; %d could not be removed, %d changed within %s of the boot were kept",
		removed, bytes, dir, failed, recent, uploadSweepMargin)
	slog.Info("upload sweep: done", "dir", dir, "removed", removed, "bytes", bytes, "failed", failed,
		"kept_recent", recent, "named", len(namedKeys))
	return outcome, nil
}
