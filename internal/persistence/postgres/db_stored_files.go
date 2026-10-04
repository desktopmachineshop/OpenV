// The stored files the database names: the figures' and their versions',
// the evidence files', the workspace logos' and the profile pictures' paths
// in the uploads directory. A delete that takes such rows answers their
// paths, gathered here from its DELETE ... RETURNING statements, for the
// caller to remove once the delete has committed, and the boot's sweep of
// the files no row names reads every path (StoredFileReferences); this
// package never touches the files themselves.

package postgres

import (
	"database/sql"
	"fmt"
	"sort"
)

// storedFiles gathers the stored files a delete's statements answer, each
// path once.
type storedFiles struct {
	seen  map[string]bool
	paths []string
}

// collect runs stmt in tx, a statement that answers one stored file path a
// row (a DELETE ... RETURNING file_path), and adds each path it answers but
// an empty one, which names no file.
func (f *storedFiles) collect(tx *sql.Tx, stmt string, args ...interface{}) error {
	rows, err := tx.Query(stmt, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		if path == "" || f.seen[path] {
			continue
		}
		if f.seen == nil {
			f.seen = map[string]bool{}
		}
		f.seen[path] = true
		f.paths = append(f.paths, path)
	}
	return rows.Err()
}

// sorted answers the paths gathered, sorted; nil when there are none.
func (f *storedFiles) sorted() []string {
	sort.Strings(f.paths)
	return f.paths
}

// storedFileReferences selects the path of every stored file a row names:
// each figure's current file and every version's, each evidence file, each
// workspace logo and each uploaded profile picture. A path two rows name
// comes twice.
const storedFileReferences = `
	SELECT file_path FROM attachments
	UNION ALL SELECT file_path FROM attachment_versions
	UNION ALL SELECT file_path FROM evidence_files
	UNION ALL SELECT logo_path FROM organizations WHERE logo_path <> ''
	UNION ALL SELECT avatar_path FROM users WHERE COALESCE(avatar_path, '') <> ''`

// StoredFileReferences answers the path of every stored file a row of the
// database names, as the rows hold it: absolute or relative as UPLOADS_DIR
// was when the file was stored, each path once, sorted. It reads them in
// one statement, so they are one snapshot of the database. Any failure is
// returned and no path with it: a caller that deletes the files no row
// names must delete nothing when it cannot read every name (#379 question
// 48).
func StoredFileReferences(db *sql.DB) ([]string, error) {
	rows, err := db.Query(storedFileReferences)
	if err != nil {
		return nil, fmt.Errorf("failed to read the stored files the database names: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("failed to read the stored files the database names: %w", err)
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read the stored files the database names: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}
