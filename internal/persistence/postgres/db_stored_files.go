// The stored files the database names: the figures' and their versions',
// the evidence files', the workspace logos' and the profile pictures' paths
// in the uploads directory. A delete that takes such rows answers their
// paths, gathered here from its DELETE ... RETURNING statements, for the
// caller to remove once the delete has committed, and the boot's sweep of
// the files no row names reads every path and every workspace and account
// (ReadStoredFileNames); this package never touches the files themselves.

package postgres

import (
	"context"
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

// ImageOwner is a workspace or an account: its id, as Postgres writes a
// uuid (lower case, hyphenated), and the path of the logo or profile
// picture its row names, "" when none.
type ImageOwner struct {
	ID, Path string
}

// StoredFileNames is what the database names in the uploads directory
// (#379 questions 48 and 56).
type StoredFileNames struct {
	// Paths is every stored file path a row names, as the rows hold it:
	// absolute or relative as UPLOADS_DIR was when the file was stored,
	// each once, sorted.
	Paths []string
	// Workspaces and Accounts are every workspace, a soft-deleted one's
	// included, and every account, sorted by id, with the path of its logo
	// or profile picture.
	Workspaces, Accounts []ImageOwner
}

// ReadStoredFileNames reads every stored file path a row names and every
// workspace and account, in one read-only transaction, so that they are
// one snapshot of the database. Any failure is returned with nothing else:
// a caller that deletes the files no row names must delete nothing when it
// cannot read every name (#379 question 48).
func ReadStoredFileNames(db *sql.DB) (StoredFileNames, error) {
	names, err := readStoredFileNames(db)
	if err != nil {
		return StoredFileNames{}, fmt.Errorf("failed to read the stored files the database names: %w", err)
	}
	return names, nil
}

func readStoredFileNames(db *sql.DB) (StoredFileNames, error) {
	var names StoredFileNames
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return names, err
	}
	defer func() { _ = tx.Rollback() }()

	seen := map[string]bool{}
	if err := scanRows(tx, storedFileReferences, func(rows *sql.Rows) error {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		if !seen[path] {
			seen[path] = true
			names.Paths = append(names.Paths, path)
		}
		return nil
	}); err != nil {
		return names, err
	}
	sort.Strings(names.Paths)
	for _, owners := range []struct {
		query string
		into  *[]ImageOwner
	}{
		{`SELECT id::text, logo_path FROM organizations ORDER BY id`, &names.Workspaces},
		{`SELECT id::text, COALESCE(avatar_path, '') FROM users ORDER BY id`, &names.Accounts},
	} {
		if err := scanRows(tx, owners.query, func(rows *sql.Rows) error {
			var o ImageOwner
			if err := rows.Scan(&o.ID, &o.Path); err != nil {
				return err
			}
			*owners.into = append(*owners.into, o)
			return nil
		}); err != nil {
			return names, err
		}
	}
	return names, tx.Commit()
}

// scanRows runs query in tx and hands each row to scan.
func scanRows(tx *sql.Tx, query string, scan func(*sql.Rows) error) error {
	rows, err := tx.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
