package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	str "strings"
)

// This file is liftmigrations' fixture: a registry in the shapes of
// internal/persistence/postgres/migrations.go, with a runner and helpers
// after it.

// Migration is one numbered schema change.
type Migration struct {
	Version int
	Name    string
	Run     func(tx *sql.Tx) error
	RunDB   func(db *sql.DB) error
}

// String is a method of the registry's element type, which stays with it.
func (m Migration) String() string { return m.Name }

// migrations is the ordered registry.
var migrations = []Migration{
	{Version: 1, Name: "baseline", RunDB: initSchema},

	// 0002: a body that uses fmt, an aliased import and a helper, with a
	// multi-line raw string indented deeper than the code and a comment
	// inside.
	//
	// The comment has two paragraphs.
	{Version: 2, Name: "create_widgets", Run: func(tx *sql.Tx) error {
		// The table.
		if _, err := tx.Exec(`
			CREATE TABLE widgets (
				id INT PRIMARY KEY
			)
		`); err != nil {
			return fmt.Errorf("widgets: %w", err)
		}
		_, err := tx.Exec(str.TrimSpace(widgetSQL(2))) // a trailing comment
		return err
	}},
	// 0003: existing rows read '' (SQL's empty string), which gofmt would
	// turn into a typographic quote in a doc comment.
	{Version: 3, Name: "widget_labels", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE widgets ADD COLUMN label TEXT NOT NULL DEFAULT ''`)
		return err
	}},
	{Version: 4, Name: "no_comment", Run: func(tx *sql.Tx) error {
		// Only a comment inside, and an import nothing else uses.
		return errors.Join()
	}},

	// 0006 was lifted before; 0005 was reserved and never used.
	{Version: 6, Name: "already_named", Run: m0006AlreadyNamed},
	// 0007 and 0008 share a name.
	{Version: 7, Name: "same_name", Run: func(tx *sql.Tx) error { return nil }},
	// Positional entries follow.

	// 0008: positional fields; the comment above it is two groups.
	{8, "same_name", func(tx *sql.Tx) error {
		_, err := tx.Exec(`SELECT 1`)
		return err
	}, nil},
}

// widgetSQL is a helper migration 0002 reaches.
func widgetSQL(n int) string { return str.Repeat("SELECT 1;", n) + suffix }

// suffix is reached through widgetSQL, and by the runner.
const suffix = " "

// lockKey is the runner's.
const lockKey int64 = 42

// Migrate applies the registry.
func Migrate(db *sql.DB) error {
	if _, err := db.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return err
	}
	for _, m := range migrations {
		if err := apply(db, m); err != nil {
			return fmt.Errorf("%s: %w%s", m, err, suffix)
		}
	}
	return nil
}

// apply runs one migration.
func apply(db *sql.DB, m Migration) error {
	if m.RunDB != nil {
		return m.RunDB(db)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	if err := m.Run(tx); err != nil {
		return err
	}
	return tx.Commit()
}
