// The stored files the database names: the figures' and their versions',
// the evidence files', the workspace logos' and the profile pictures' paths
// in the uploads directory. A delete that takes such rows answers their
// paths, gathered here from its DELETE ... RETURNING statements, for the
// caller to remove once the delete has committed; this package never
// touches the files themselves.

package postgres

import (
	"database/sql"
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
