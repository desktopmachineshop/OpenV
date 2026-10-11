package postgres

import (
	"context"
	"database/sql"
	"strings"
)

// The persistence kit (refactor plan X13): what every repository's reads and
// transactions share. Each repository gives its entity a columns const
// (artifactColumns) and a scan function (scanArtifact) taking a rowScanner;
// each hand-rolled transaction becomes a withTx call. The migration runner
// (migrate_runner.go, migrations.go, applyOnce) keeps its own transactions.

// rowScanner is what a scan function reads one row from: a *sql.Row or a
// *sql.Rows positioned on a row.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// prefixColumns qualifies each column of a columns const (linkColumns, say)
// by a table alias, for the queries that join tables and so cannot use bare
// names: "id, title" under alias "a" is "a.id, a.title". The list is split on
// ", " and joined the same way, so a qualified list keeps the spacing of its
// const.
func prefixColumns(columns, alias string) string {
	cols := strings.Split(columns, ", ")
	for i, c := range cols {
		cols[i] = alias + "." + c
	}
	return strings.Join(cols, ", ")
}

// withTx runs fn in a transaction begun on db with ctx and opts, and commits
// it when fn returns nil. It answers the begin's error, fn's, or the
// commit's, each as it came. Any other way out of fn, an error or a panic,
// rolls the transaction back (the rollback's own error is dropped, and a
// panic goes on once it has run). It adds no timeout: a caller bounds the
// transaction through ctx. db.Begin() is db.BeginTx(context.Background(),
// nil), so a transaction begun that way is withTx(context.Background(), db,
// nil, fn).
func withTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
