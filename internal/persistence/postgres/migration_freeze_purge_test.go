package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// This file is part (d) of the stored-data freeze (refactor plan step S3,
// invariant I16, quirk Q17; see migration_freeze_test.go for the rest): the
// purge catalogue. PurgeOrg hard-deletes a workspace from a hand-kept list of
// statements (persistence-4). The test records the SQL it sends, through a
// recording database/sql connector rather than by reading the source, so
// moving the list into a named var (M12a), a transaction helper (X13) or
// prepared statements leave it alone, and pins:
//
//   - testdata/purge/statements.txt: the statements, in order, with and
//     without the artifact_embeddings table (the vector migration's table,
//     which PurgeOrg probes for; a stand-in is created where the vector
//     extension is absent, so both CI legs record both sequences), with the
//     transaction each one runs in: a statement sent outside one is logged
//     as "autocommit", and a begin shows a non-default isolation level,
//     read-only and a context deadline, so a purge that stops being one
//     transaction, or gains a timeout, fails;
//   - testdata/purge/catalog.txt: every table, the org_id, project_id or
//     artifact_id columns it has, and how a purge reaches it: deleted by a
//     statement, deleted by an ON DELETE CASCADE foreign key (with every
//     column NOT NULL) from a table that is reached, or not reached. A
//     nullable cascading key does not count: Postgres cascades no row whose
//     key is NULL, so it cannot vouch for every row of the table.
//
// A table with one of those columns that no purge reaches is a gap, and must
// be on purgeGapAllowlist. The allowlist may only shrink: a new gap fails
// until PurgeOrg deletes the table or a NOT NULL cascading foreign key
// reaches it, and an entry that is no longer a gap fails until it is removed.
// Purging more is a data-deleting behavior change, so closing a gap is its
// own release-noted pull request, never part of a refactor.

// purgeGapAllowlist is quirk Q17: the org-, project- or artifact-scoped tables
// a purge leaves rows in today, each with the reason. It may only shrink.
var purgeGapAllowlist = map[string]string{}

// purgeScopeColumns are the columns that tie a row to a workspace.
var purgeScopeColumns = []string{"artifact_id", "org_id", "project_id"}

const (
	purgeStatementsGolden = "testdata/purge/statements.txt"
	purgeCatalogGolden    = "testdata/purge/catalog.txt"
)

// TestPurgeCatalog is part (d).
func TestPurgeCatalog(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	installed := checkOptionalExtensions(t, db)
	tables := readPurgeTables(t, db)

	rec := newRecordingDB(t, db)
	repo := NewOrgRepository(rec.db)
	org := uuid.New().String()
	var embeddings sql.NullString
	if err := db.QueryRow(`SELECT to_regclass('artifact_embeddings')::text`).Scan(&embeddings); err != nil {
		t.Fatal(err)
	}
	if !embeddings.Valid {
		// No vector extension here: a stand-in lets PurgeOrg take the branch
		// it takes on a database that has the table.
		if _, err := db.Exec(`CREATE TABLE artifact_embeddings (artifact_id UUID PRIMARY KEY)`); err != nil {
			t.Fatalf("create a stand-in artifact_embeddings: %v", err)
		}
	}
	withTable := rec.run(t, org, func() error { return repo.PurgeOrg(org) })
	if _, err := db.Exec(`DROP TABLE artifact_embeddings`); err != nil {
		t.Fatalf("drop artifact_embeddings: %v", err)
	}
	withoutTable := rec.run(t, org, func() error { return repo.PurgeOrg(org) })

	// How a purge reaches each table.
	deleted := map[string]bool{}
	deleteFrom := regexp.MustCompile(`^(?:autocommit )?exec DELETE FROM ([A-Za-z_][A-Za-z0-9_]*)\b`)
	for _, line := range withTable {
		if m := deleteFrom.FindStringSubmatch(line); m != nil {
			deleted[m[1]] = true
		}
	}
	reached := map[string]bool{}
	for name := range deleted {
		reached[name] = true
	}
	for changed := true; changed; {
		changed = false
		for _, tb := range tables {
			if reached[tb.name] {
				continue
			}
			for _, parent := range tb.cascadeFrom {
				if reached[parent] {
					reached[tb.name] = true
					changed = true
					break
				}
			}
		}
	}
	var catalog strings.Builder
	catalog.WriteString(purgeCatalogHeader)
	var gaps []string
	present := map[string]bool{}
	for _, tb := range tables {
		present[tb.name] = true
		scope := "-"
		if len(tb.scope) > 0 {
			scope = strings.Join(tb.scope, ",")
		}
		var how string
		switch {
		case deleted[tb.name]:
			how = "deleted"
		case reached[tb.name]:
			var via []string
			for _, parent := range tb.cascadeFrom {
				if reached[parent] {
					via = append(via, parent)
				}
			}
			how = "cascade from " + strings.Join(via, ",")
		case len(tb.scope) > 0:
			how = "GAP"
			gaps = append(gaps, tb.name)
		default:
			how = "not reached"
		}
		fmt.Fprintf(&catalog, "%s scope %s %s%s\n", tb.name, scope, how, requiresSuffix(tb.requires))
	}

	// The allowlist may only shrink, whatever else happens, and even under
	// UPDATE_GOLDEN=1: a regeneration cannot bless a new gap.
	var problems []string
	for _, name := range gaps {
		if _, ok := purgeGapAllowlist[name]; !ok {
			problems = append(problems, fmt.Sprintf("table %s has %s but a purge never reaches it: PurgeOrg sends no DELETE FROM %s "+
				"and no ON DELETE CASCADE foreign key with NOT NULL columns leads to it from a table that is deleted, so purging "+
				"a workspace leaves its rows behind. Add it to PurgeOrg's list (children before parents) or give it a NOT NULL "+
				"cascading foreign key; "+
				"purgeGapAllowlist (quirk Q17) may only shrink", name, strings.Join(tableByName(tables, name).scope, ", "), name))
		}
	}
	isGap := map[string]bool{}
	for _, name := range gaps {
		isGap[name] = true
	}
	for _, name := range sortedKeys(purgeGapAllowlist) {
		switch {
		case !present[name]:
			problems = append(problems, fmt.Sprintf("purgeGapAllowlist names %s, which no longer exists: remove the entry", name))
		case !isGap[name]:
			problems = append(problems, fmt.Sprintf("%s is no longer a purge gap: remove it from purgeGapAllowlist, which only shrinks "+
				"(and regenerate the catalogue; purging more is a behavior change with a release note)", name))
		}
	}
	if len(problems) > 0 {
		t.Errorf("purge catalogue (quirk Q17):\n  - %s", strings.Join(problems, "\n  - "))
		if s3Updating() {
			t.FailNow() // never record a catalogue the allowlist rejects
		}
	}

	var statements strings.Builder
	statements.WriteString(purgeStatementsHeader)
	statements.WriteString("with artifact_embeddings\n")
	for _, line := range withTable {
		statements.WriteString("  " + line + "\n")
	}
	statements.WriteString("without artifact_embeddings\n")
	for _, line := range withoutTable {
		statements.WriteString("  " + line + "\n")
	}

	regenerate := s3Regenerate("TestPurgeCatalog", true)
	if s3Updating() {
		for _, ext := range sortedKeys(schemaGoldenExtensions) {
			if !installed[ext] {
				t.Fatalf("not regenerating the purge goldens: this server lacks the %s extension; they are recorded where "+
					"every optional extension is installed (%s), so that one file serves both CI legs",
					ext, strings.Join(sortedKeys(schemaGoldenExtensions), ", "))
			}
		}
		s3WriteGolden(t, purgeStatementsGolden, statements.String())
		s3WriteGolden(t, purgeCatalogGolden, catalog.String())
		return
	}
	if want := s3ReadGolden(t, purgeStatementsGolden, regenerate); want != statements.String() {
		t.Errorf("the SQL PurgeOrg sends differs from %s:\n%s\n"+
			"What a purge deletes, and in which order, is stored-data behavior (I16, Q17): a refactor never changes it. "+
			"If this is deliberate, regenerate with:\n  %s",
			s3Shown(purgeStatementsGolden), s3Diff(want, statements.String()), regenerate)
	}
	want := expectedForExtensions(s3ReadGolden(t, purgeCatalogGolden, regenerate), installed)
	if want != catalog.String() {
		t.Errorf("the purge catalogue differs from %s (lines marked @requires(x) are expected only where extension x "+
			"is installed; installed here: %s):\n%s\n"+
			"A new table shows up here: say how a purge reaches it. If this is deliberate, regenerate with:\n  %s",
			s3Shown(purgeCatalogGolden), installedList(installed), s3Diff(want, catalog.String()), regenerate)
	}
}

const purgeStatementsHeader = `# Stored-data freeze, part (d) (refactor plan S3, invariant I16, quirk Q17): the SQL
# OrgRepository.PurgeOrg sends, in order, as a recording database/sql connector sees it, when the
# artifact_embeddings table exists and when it does not. $org is the purged workspace's id. A
# statement sent outside a transaction would read "autocommit exec" or "autocommit query", and a
# begin with a non-default isolation level, read-only or a context deadline would say so.
# Regenerate (a behavior change only) with every S3 golden, against a server with the vector and
# pg_trgm extensions:
# ` + s3RegenerateAll + `
`

const purgeCatalogHeader = `# Stored-data freeze, part (d) (refactor plan S3, invariant I16, quirk Q17): every table after
# Migrate, the columns that tie its rows to a workspace (org_id, project_id, artifact_id), and how
# PurgeOrg reaches it: "deleted" by a statement, "cascade from" tables that are reached (a NOT NULL
# ON DELETE CASCADE foreign key; a nullable one misses rows whose key is NULL, so it does not count),
# "not reached" (no such column), or GAP: a scoped table a purge leaves rows in, which must be on
# purgeGapAllowlist in migration_freeze_purge_test.go (it may only shrink).
# A line ending in @requires(x) exists only where extension x is installed.
# Regenerate (a behavior change only) with every S3 golden, against a server with the vector and
# pg_trgm extensions:
# ` + s3RegenerateAll + `
`

// purgeTable is one table as the catalogue sees it.
type purgeTable struct {
	name        string
	scope       []string        // which of purgeScopeColumns it has
	cascadeFrom []string        // tables its NOT NULL ON DELETE CASCADE foreign keys reference
	requires    map[string]bool // extensions it exists only with
}

func tableByName(tables []purgeTable, name string) purgeTable {
	for _, tb := range tables {
		if tb.name == name {
			return tb
		}
	}
	return purgeTable{}
}

// readPurgeTables lists the public tables with their scope columns and
// cascading foreign keys, sorted by name. Only a foreign key whose columns are
// all NOT NULL counts: under the default MATCH SIMPLE a row with a NULL in its
// key references nothing, so deleting the parent cascades to no such row.
func readPurgeTables(t *testing.T, db *sql.DB) []purgeTable {
	t.Helper()
	d := newSchemaDumper(t, db)
	byOID := map[uint32]*purgeTable{}
	byName := map[string]*purgeTable{}
	d.query(`SELECT c.oid, c.relname FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')`,
		func(rows *sql.Rows) error {
			var oid uint32
			var name string
			if err := rows.Scan(&oid, &name); err != nil {
				return err
			}
			if d.members[schemaObject{class: "pg_class", oid: oid}] {
				return nil
			}
			tb := &purgeTable{name: name, requires: d.tableRequires(oid)}
			byOID[oid] = tb
			byName[name] = tb
			return nil
		})
	d.query(`
		SELECT a.attrelid, a.attname FROM pg_attribute a
		WHERE a.attnum > 0 AND NOT a.attisdropped AND a.attname = ANY($1)`,
		func(rows *sql.Rows) error {
			var oid uint32
			var col string
			if err := rows.Scan(&oid, &col); err != nil {
				return err
			}
			if tb := byOID[oid]; tb != nil {
				tb.scope = append(tb.scope, col)
			}
			return nil
		}, pq.Array(purgeScopeColumns))
	d.query(`
		SELECT con.conrelid, con.confrelid FROM pg_constraint con
		WHERE con.contype = 'f' AND con.confdeltype = 'c'
			AND NOT EXISTS (SELECT 1 FROM unnest(con.conkey) k
				JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k
				WHERE NOT a.attnotnull)`,
		func(rows *sql.Rows) error {
			var child, parent uint32
			if err := rows.Scan(&child, &parent); err != nil {
				return err
			}
			if tb, p := byOID[child], byOID[parent]; tb != nil && p != nil {
				tb.cascadeFrom = append(tb.cascadeFrom, p.name)
			}
			return nil
		})
	var out []purgeTable
	for _, tb := range byName {
		sort.Strings(tb.scope)
		sort.Strings(tb.cascadeFrom)
		tb.cascadeFrom = uniqueStrings(tb.cascadeFrom)
		out = append(out, *tb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func uniqueStrings(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// A database/sql connector that records the SQL sent through it.

// recordingDB is a second pool on the test database whose connections log
// every statement with its arguments and whether it ran inside a transaction,
// and every transaction boundary, with a begin's non-default options and
// context deadline.
type recordingDB struct {
	db  *sql.DB
	mu  sync.Mutex
	log []string
	org string // an argument equal to it is logged as $org
}

// newRecordingDB opens a recording pool on the database db is connected to.
func newRecordingDB(t *testing.T, db *sql.DB) *recordingDB {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv(TestDatabaseURLEnv))
	if err != nil {
		t.Fatalf("parse %s: %v", TestDatabaseURLEnv, err)
	}
	u.Path = "/" + name
	connector, err := pq.NewConnector(u.String())
	if err != nil {
		t.Fatalf("connector for %s: %v", name, err)
	}
	r := &recordingDB{}
	r.db = sql.OpenDB(recordingConnector{inner: connector, rec: r})
	t.Cleanup(func() { _ = r.db.Close() })
	return r
}

// run records what fn sends, with org standing for the purged workspace.
func (r *recordingDB) run(t *testing.T, org string, fn func() error) []string {
	t.Helper()
	r.mu.Lock()
	r.log, r.org = nil, org
	r.mu.Unlock()
	if err := fn(); err != nil {
		t.Fatalf("PurgeOrg: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

func (r *recordingDB) record(kind, query string, args []driver.NamedValue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	line := kind
	if query != "" {
		q := query
		if strings.ContainsAny(q, "\n\t\r") {
			q = strconv.Quote(q)
		}
		line += " " + q
	}
	if len(args) > 0 {
		var shown []string
		for _, a := range args {
			if s := fmt.Sprint(a.Value); s == r.org {
				shown = append(shown, "$org")
			} else {
				shown = append(shown, strconv.Quote(s))
			}
		}
		line += " args " + strings.Join(shown, " ")
	}
	r.log = append(r.log, line)
}

type recordingConnector struct {
	inner driver.Connector
	rec   *recordingDB
}

func (c recordingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &recordingConn{conn: conn, rec: c.rec}, nil
}

func (c recordingConnector) Driver() driver.Driver { return c.inner.Driver() }

// recordingConn forwards to a pq connection and logs what goes through it.
// A statement sent while this connection holds no open transaction is logged
// with an "autocommit" prefix, so one that leaves the purge's transaction
// shows in the golden. database/sql uses a connection from one goroutine at
// a time, so inTx needs no lock.
type recordingConn struct {
	conn driver.Conn
	rec  *recordingDB
	inTx bool
}

// kind is how a statement of kind k (exec or query) is logged on c.
func (c *recordingConn) kind(k string) string {
	if c.inTx {
		return k
	}
	return "autocommit " + k
}

func (c *recordingConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *recordingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var st driver.Stmt
	var err error
	if p, ok := c.conn.(driver.ConnPrepareContext); ok {
		st, err = p.PrepareContext(ctx, query)
	} else {
		st, err = c.conn.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &recordingStmt{stmt: st, query: query, conn: c}, nil
}

func (c *recordingConn) Close() error { return c.conn.Close() }

func (c *recordingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *recordingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	var tx driver.Tx
	var err error
	if b, ok := c.conn.(driver.ConnBeginTx); ok {
		tx, err = b.BeginTx(ctx, opts)
	} else {
		tx, err = c.conn.Begin()
	}
	if err != nil {
		return nil, err
	}
	begin := "begin"
	if opts.Isolation != driver.IsolationLevel(sql.LevelDefault) {
		begin += " isolation=" + sql.IsolationLevel(opts.Isolation).String()
	}
	if opts.ReadOnly {
		begin += " read-only"
	}
	if _, ok := ctx.Deadline(); ok {
		begin += " deadline"
	}
	c.inTx = true
	c.rec.record(begin, "", nil)
	return recordingTx{tx: tx, conn: c}, nil
}

func (c *recordingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ex, ok := c.conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip // database/sql prepares instead, and the statement records
	}
	res, err := ex.ExecContext(ctx, query, args)
	if err != driver.ErrSkip {
		c.rec.record(c.kind("exec"), query, args)
	}
	return res, err
}

func (c *recordingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := q.QueryContext(ctx, query, args)
	if err != driver.ErrSkip {
		c.rec.record(c.kind("query"), query, args)
	}
	return rows, err
}

func (c *recordingConn) Ping(ctx context.Context) error {
	if p, ok := c.conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *recordingConn) ResetSession(ctx context.Context) error {
	if r, ok := c.conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *recordingConn) IsValid() bool {
	if v, ok := c.conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

type recordingTx struct {
	tx   driver.Tx
	conn *recordingConn
}

func (t recordingTx) Commit() error {
	err := t.tx.Commit()
	t.conn.inTx = false
	t.conn.rec.record("commit", "", nil)
	return err
}

func (t recordingTx) Rollback() error {
	err := t.tx.Rollback()
	t.conn.inTx = false
	t.conn.rec.record("rollback", "", nil)
	return err
}

// recordingStmt logs a prepared statement's executions as the direct ones
// are logged, so preparing a statement first changes nothing in the log.
type recordingStmt struct {
	stmt  driver.Stmt
	query string
	conn  *recordingConn
}

func (s *recordingStmt) Close() error  { return s.stmt.Close() }
func (s *recordingStmt) NumInput() int { return s.stmt.NumInput() }

func (s *recordingStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(args))
}

func (s *recordingStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(args))
}

func (s *recordingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.conn.rec.record(s.conn.kind("exec"), s.query, args)
	if ex, ok := s.stmt.(driver.StmtExecContext); ok {
		return ex.ExecContext(ctx, args)
	}
	return s.stmt.Exec(plainValues(args))
}

func (s *recordingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.conn.rec.record(s.conn.kind("query"), s.query, args)
	if q, ok := s.stmt.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	return s.stmt.Query(plainValues(args))
}

func namedValues(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, a := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	return out
}

func plainValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}
