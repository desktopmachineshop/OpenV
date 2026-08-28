package postgres

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
)

// TestDatabaseURLEnv names the environment variable that enables the postgres
// integration tests in this package. It must point at a throwaway postgres
// server (URL form, e.g. postgres://postgres:test@localhost:5433/postgres?sslmode=disable);
// each test creates and drops its own database on that server, so the tests
// never share state and `go test ./...` stays green when the variable is
// unset (the tests skip).
const TestDatabaseURLEnv = "OPENV_TEST_DATABASE_URL"

// testDB creates a fresh throwaway database for one test and returns a
// connection to it. Skips the test when OPENV_TEST_DATABASE_URL is unset.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(TestDatabaseURLEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping postgres integration test", TestDatabaseURLEnv)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", TestDatabaseURLEnv, err)
	}

	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("ping %s: %v", TestDatabaseURLEnv, err)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "openv_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}

	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		if _, err := admin.Exec(fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			t.Logf("drop database %s: %v", name, err)
		}
		_ = admin.Close()
	})

	if err := db.Ping(); err != nil {
		t.Fatalf("ping %s: %v", name, err)
	}
	return db
}

// initTestSchema runs the full production schema migration (ledger +
// baseline + numbered migrations) against the test DB, exactly as the
// server does at boot.
func initTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}
