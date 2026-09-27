package postgres

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// This file is part (c) of the stored-data freeze (refactor plan step S3,
// invariant I16, OpenV REQ-89): the schema a database has after each boot
// path, pinned as goldens (see migration_freeze_test.go for parts (a), (b)
// and the harness). Two goldens:
//
//   - testdata/schema/migrate.txt: after Migrate on a fresh database, which is
//     also what the first MigrateAndBackfill leaves (no user yet, so the org
//     backfill stops before promoting any column);
//   - testdata/schema/migrate_and_backfill.txt: after MigrateAndBackfill once a
//     user exists, the production shape (the backfill's PromoteOrgColumns makes
//     nine org_id columns NOT NULL); a further boot must change nothing.
//
// A golden lists extensions, types, sequences, every table with its row
// count, columns (in column order), constraints, indexes and triggers, views,
// functions and the migration ledger, from pg_catalog, sorted by name in byte
// order and without owners, OIDs or extension versions, so the same file holds
// on Postgres 15 and 16 and on any server locale.
//
// Both CI legs run it: postgres:15 has no vector extension and
// pgvector/pgvector:pg15 has it. Rather than keep one golden per leg, every
// line that exists only because an optional extension is installed ends with
// "  @requires(<extension>)": the extension itself, a table with a column of
// one of its types (with all of that table's lines: artifact_embeddings,
// which migration 0016 and reconcileVectorEmbeddings create only when vector
// is present), and a column, index, constraint or trigger that uses one of its
// objects (the pg_trgm indexes of migration 0009). A server without the
// extension expects those lines absent and every other line exactly as
// recorded. Goldens are recorded where every such extension is installed.

const (
	schemaGoldenMigrate  = "testdata/schema/migrate.txt"
	schemaGoldenBackfill = "testdata/schema/migrate_and_backfill.txt"
)

// schemaGoldenExtensions are the optional extensions the migrations install
// when the server offers them and the role may create them, with the migration
// that does it. Boot never fails without them (issue #241), so the schema has
// one shape per set of installed extensions; the goldens record the shape with
// all of them.
var schemaGoldenExtensions = map[string]string{
	"pg_trgm": "0009 artifact_trgm_search",
	"vector":  "0016 pgvector_artifact_embeddings",
}

const schemaRequiresMarker = "  @requires("

// TestSchemaGolden is part (c): the schema after Migrate and after
// MigrateAndBackfill with a seeded user, on whichever CI leg runs it.
func TestSchemaGolden(t *testing.T) {
	t.Run("migrate", func(t *testing.T) {
		db := testDB(t)
		if err := Migrate(db); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		checkSchemaGolden(t, db, schemaGoldenMigrate, "after Migrate on a fresh database", true)
	})
	t.Run("migrate_and_backfill", func(t *testing.T) {
		db := testDB(t)
		agentsDir := t.TempDir()
		// The first boot of an empty database: the backfill finds no user.
		if err := MigrateAndBackfill(db, agentsDir); err != nil {
			t.Fatalf("first MigrateAndBackfill: %v", err)
		}
		checkSchemaGolden(t, db, schemaGoldenMigrate, "after the first MigrateAndBackfill on a fresh database", false)

		// A user signs up; the next boot backfills a personal org for them and
		// promotes the org_id columns.
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, 'seed@example.com', 'Seed')`,
			uuid.New().String()); err != nil {
			t.Fatalf("seed a user: %v", err)
		}
		if err := MigrateAndBackfill(db, agentsDir); err != nil {
			t.Fatalf("MigrateAndBackfill with a seeded user: %v", err)
		}
		checkSchemaGolden(t, db, schemaGoldenBackfill, "after MigrateAndBackfill with a seeded user", true)

		// Every boot re-runs the baseline, the reconcile and the backfill; one
		// more changes nothing.
		if err := MigrateAndBackfill(db, agentsDir); err != nil {
			t.Fatalf("third MigrateAndBackfill: %v", err)
		}
		checkSchemaGolden(t, db, schemaGoldenBackfill, "after a further MigrateAndBackfill", false)
	})
}

// checkSchemaGolden compares the database's schema with a golden, or, under
// UPDATE_GOLDEN=1 and when owner is set, rewrites the golden. A check that
// does not own its golden only compares.
func checkSchemaGolden(t *testing.T, db *sql.DB, path, when string, owner bool) {
	t.Helper()
	installed := checkOptionalExtensions(t, db)
	got := schemaGoldenHeader(path) + dumpSchema(t, db)
	regenerate := s3Regenerate("TestSchemaGolden", true)
	if s3Updating() && owner {
		var missing []string
		for ext := range schemaGoldenExtensions {
			if !installed[ext] {
				missing = append(missing, ext)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Fatalf("not regenerating %s: this server lacks the %s extension(s). The schema goldens are recorded where every "+
				"optional extension is installed (%s), so that one file serves both CI legs; "+
				"run the regenerate command against pgvector/pgvector:pg15 or another server with them",
				s3Shown(path), strings.Join(missing, ", "), strings.Join(sortedKeys(schemaGoldenExtensions), ", "))
		}
		s3WriteGolden(t, path, got)
		return
	}
	if s3Updating() {
		t.Logf("%s: this check compares only (another check of TestSchemaGolden writes the file)", s3Shown(path))
	}
	want := expectedForExtensions(s3ReadGolden(t, path, regenerate), installed)
	if want != got {
		t.Fatalf("the schema %s differs from %s (lines marked @requires(x) are expected only where extension x is installed; "+
			"installed here: %s):\n%s\n"+
			"The schema is stored data every deployed database has (I16): a refactor never changes it. A new migration "+
			"does, and so does any change to the baseline, the runner, the reconcile or the org backfill. If this is "+
			"deliberate, regenerate with:\n  %s",
			when, s3Shown(path), installedList(installed), s3Diff(want, got), regenerate)
	}
}

// schemaGoldenHeader is the comment block at the top of a schema golden.
func schemaGoldenHeader(path string) string {
	what := "after Migrate on a fresh database (also what the first MigrateAndBackfill leaves, with no user yet)"
	if path == schemaGoldenBackfill {
		what = "after MigrateAndBackfill once a user exists (the production shape; a further boot changes nothing)"
	}
	return "# Stored-data freeze, part (c) (refactor plan S3, invariant I16; OpenV REQ-89): the schema\n" +
		"# " + what + ".\n" +
		"# From pg_catalog, sorted by name: extensions, types, sequences, tables (row count, columns in\n" +
		"# column order, constraints, indexes, triggers), views, functions and the migration ledger.\n" +
		"# A line ending in @requires(x) exists only where extension x is installed; the plain postgres:15\n" +
		"# CI leg (no vector) expects those lines absent and every other line as written.\n" +
		"# Regenerate (a behavior change only) with every S3 golden, so that a new migration's body is frozen too,\n" +
		"# against a server with the vector and pg_trgm extensions:\n" +
		"# " + s3RegenerateAll + "\n"
}

// expectedForExtensions is a golden as a server with the given extensions
// installed must reproduce it: the lines that require another extension are
// dropped.
func expectedForExtensions(golden string, installed map[string]bool) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(golden, "\n") {
		if line == "" {
			continue
		}
		if i := strings.LastIndex(line, schemaRequiresMarker); i >= 0 && !strings.HasPrefix(line, "#") {
			keep := true
			for _, ext := range strings.Split(strings.TrimSuffix(strings.TrimRight(line[i+len(schemaRequiresMarker):], "\r\n"), ")"), ",") {
				if !installed[ext] {
					keep = false
				}
			}
			if !keep {
				continue
			}
		}
		b.WriteString(line)
	}
	return b.String()
}

func installedList(installed map[string]bool) string {
	var names []string
	for n, ok := range installed {
		if ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkOptionalExtensions returns the installed extensions, and fails if an
// optional extension that the server offers and the role may create is
// missing: the migration that installs it skipped although it could not
// have had to, so the no-extension expectation must not hide it.
func checkOptionalExtensions(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	installed := map[string]bool{}
	rows, err := db.Query(`SELECT extname FROM pg_extension`)
	if err != nil {
		t.Fatalf("list extensions: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		installed[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, ext := range sortedKeys(schemaGoldenExtensions) {
		if installed[ext] {
			continue
		}
		var creatable bool
		err := db.QueryRow(`
			SELECT (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)
				OR ((v.trusted OR NOT v.superuser) AND has_database_privilege(current_database(), 'CREATE'))
			FROM pg_available_extensions a
			JOIN pg_available_extension_versions v ON v.name = a.name AND v.version = a.default_version
			WHERE a.name = $1`, ext).Scan(&creatable)
		if err == sql.ErrNoRows {
			continue // the server does not offer it
		}
		if err != nil {
			t.Fatalf("check extension %s: %v", ext, err)
		}
		if creatable {
			t.Errorf("migration %s did not install the %s extension, although this server offers it and the role may create it",
				schemaGoldenExtensions[ext], ext)
		}
	}
	return installed
}

// schemaObject identifies a catalog object for extension-dependency lookups.
type schemaObject struct {
	class string // pg_class, pg_constraint, pg_attrdef, pg_trigger, pg_proc, pg_type
	oid   uint32
	subID int32
}

// schemaDumper collects the dump of one database.
type schemaDumper struct {
	t       *testing.T
	db      *sql.DB
	members map[schemaObject]bool            // objects an extension owns (not dumped)
	deps    map[schemaObject]map[string]bool // extensions whose objects an object uses
}

// query runs a catalog query and hands each row to scan.
func (d *schemaDumper) query(q string, scan func(rows *sql.Rows) error, args ...interface{}) {
	d.t.Helper()
	rows, err := d.db.Query(q, args...)
	if err != nil {
		d.t.Fatalf("schema dump: %v\n%s", err, q)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			d.t.Fatalf("schema dump: %v\n%s", err, q)
		}
	}
	if err := rows.Err(); err != nil {
		d.t.Fatalf("schema dump: %v\n%s", err, q)
	}
}

// requires lists the extensions the objects use, as a line suffix.
func (d *schemaDumper) requires(objs ...schemaObject) map[string]bool {
	out := map[string]bool{}
	for _, o := range objs {
		for ext := range d.deps[o] {
			out[ext] = true
		}
	}
	return out
}

func requiresSuffix(exts ...map[string]bool) string {
	all := map[string]bool{}
	for _, m := range exts {
		for e := range m {
			all[e] = true
		}
	}
	if len(all) == 0 {
		return ""
	}
	names := make([]string, 0, len(all))
	for e := range all {
		names = append(names, e)
	}
	sort.Strings(names)
	return schemaRequiresMarker + strings.Join(names, ",") + ")"
}

// schemaLine is one line of the dump with the key it sorts by.
type schemaLine struct {
	key  string
	text string
}

func sortLines(lines []schemaLine) []string {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].key < lines[j].key })
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

// newSchemaDumper reads which objects the installed extensions own, and
// which objects use an extension's objects.
func newSchemaDumper(t *testing.T, db *sql.DB) *schemaDumper {
	t.Helper()
	d := &schemaDumper{t: t, db: db, members: map[schemaObject]bool{}, deps: map[schemaObject]map[string]bool{}}
	d.query(`
		SELECT d.classid::regclass::text, d.objid
		FROM pg_depend d
		WHERE d.refclassid = 'pg_extension'::regclass AND d.deptype = 'e'`,
		func(rows *sql.Rows) error {
			var o schemaObject
			if err := rows.Scan(&o.class, &o.oid); err != nil {
				return err
			}
			d.members[o] = true
			return nil
		})
	d.query(`
		SELECT d.classid::regclass::text, d.objid, d.objsubid, e.extname
		FROM pg_depend d
		JOIN pg_depend m ON m.classid = d.refclassid AND m.objid = d.refobjid
			AND m.refclassid = 'pg_extension'::regclass AND m.deptype = 'e'
		JOIN pg_extension e ON e.oid = m.refobjid
		WHERE d.deptype <> 'e'`,
		func(rows *sql.Rows) error {
			var o schemaObject
			var ext string
			if err := rows.Scan(&o.class, &o.oid, &o.subID, &ext); err != nil {
				return err
			}
			if d.deps[o] == nil {
				d.deps[o] = map[string]bool{}
			}
			d.deps[o][ext] = true
			return nil
		})
	return d
}

// tableRequires lists the extensions a table exists only with: those whose
// types its columns have.
func (d *schemaDumper) tableRequires(oid uint32) map[string]bool {
	out := map[string]bool{}
	for o, exts := range d.deps {
		if o.class == "pg_class" && o.oid == oid && o.subID > 0 {
			for e := range exts {
				out[e] = true
			}
		}
	}
	return out
}

// dumpSchema renders the public schema of the database.
func dumpSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	d := newSchemaDumper(t, db)
	var out []string
	out = append(out, d.extensions()...)
	out = append(out, d.types()...)
	out = append(out, d.sequences()...)
	out = append(out, d.tables()...)
	out = append(out, d.views()...)
	out = append(out, d.functions()...)
	out = append(out, d.ledger()...)
	return strings.Join(out, "\n") + "\n"
}

func (d *schemaDumper) extensions() []string {
	var lines []schemaLine
	d.query(`SELECT extname FROM pg_extension`, func(rows *sql.Rows) error {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		suffix := ""
		if name != "plpgsql" {
			suffix = requiresSuffix(map[string]bool{name: true})
		}
		lines = append(lines, schemaLine{name, "extension " + name + suffix})
		return nil
	})
	return sortLines(lines)
}

// types lists the user-defined enums, domains, composite and range types.
func (d *schemaDumper) types() []string {
	var lines []schemaLine
	d.query(`
		SELECT t.oid, t.typname, t.typtype,
			CASE t.typtype
				WHEN 'e' THEN (SELECT string_agg(quote_literal(enumlabel), ', ' ORDER BY enumsortorder) FROM pg_enum WHERE enumtypid = t.oid)
				WHEN 'd' THEN format_type(t.typbasetype, t.typtypmod)
					|| CASE WHEN t.typnotnull THEN ' NOT NULL' ELSE '' END
					|| COALESCE(' DEFAULT ' || t.typdefault, '')
					|| COALESCE((SELECT ' ' || string_agg(pg_get_constraintdef(c.oid), ' ' ORDER BY c.conname) FROM pg_constraint c WHERE c.contypid = t.oid), '')
				WHEN 'c' THEN (SELECT string_agg(quote_ident(a.attname) || ' ' || format_type(a.atttypid, a.atttypmod), ', ' ORDER BY a.attnum)
					FROM pg_attribute a WHERE a.attrelid = t.typrelid AND a.attnum > 0 AND NOT a.attisdropped)
				ELSE ''
			END
		FROM pg_type t
		LEFT JOIN pg_class r ON r.oid = t.typrelid
		WHERE t.typnamespace = 'public'::regnamespace
			AND t.typtype IN ('e', 'd', 'c', 'r', 'm')
			AND (t.typtype <> 'c' OR r.relkind = 'c')`,
		func(rows *sql.Rows) error {
			var o schemaObject
			var name, kind, def string
			if err := rows.Scan(&o.oid, &name, &kind, &def); err != nil {
				return err
			}
			o.class = "pg_type"
			if d.members[o] {
				return nil
			}
			kinds := map[string]string{"e": "enum", "d": "domain", "c": "composite", "r": "range", "m": "multirange"}
			lines = append(lines, schemaLine{name, fmt.Sprintf("type %s %s (%s)%s", name, kinds[kind], def, requiresSuffix(d.requires(o)))})
			return nil
		})
	return sortLines(lines)
}

func (d *schemaDumper) sequences() []string {
	var lines []schemaLine
	d.query(`
		SELECT c.oid, c.relname, format_type(s.seqtypid, NULL), s.seqstart, s.seqincrement, s.seqmin, s.seqmax, s.seqcache, s.seqcycle,
			COALESCE((SELECT oc.relname || '.' || a.attname FROM pg_depend dp
				JOIN pg_class oc ON oc.oid = dp.refobjid
				JOIN pg_attribute a ON a.attrelid = dp.refobjid AND a.attnum = dp.refobjsubid
				WHERE dp.classid = 'pg_class'::regclass AND dp.objid = c.oid
					AND dp.refclassid = 'pg_class'::regclass AND dp.deptype IN ('a', 'i')), '')
		FROM pg_class c JOIN pg_sequence s ON s.seqrelid = c.oid
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'S'`,
		func(rows *sql.Rows) error {
			var o schemaObject
			var name, typ, owned string
			var start, inc, min, max, cache int64
			var cycle bool
			if err := rows.Scan(&o.oid, &name, &typ, &start, &inc, &min, &max, &cache, &cycle, &owned); err != nil {
				return err
			}
			o.class = "pg_class"
			if d.members[o] {
				return nil
			}
			text := fmt.Sprintf("sequence %s %s start %d increment %d min %d max %d cache %d", name, typ, start, inc, min, max, cache)
			if cycle {
				text += " cycle"
			}
			if owned != "" {
				text += " owned by " + owned
			}
			lines = append(lines, schemaLine{name, text})
			return nil
		})
	return sortLines(lines)
}

// tableBlock is one table's lines, gathered from several catalog queries.
type tableBlock struct {
	oid                            uint32
	name, head                     string
	requires                       map[string]bool
	columns                        []string
	constraints, indexes, triggers []schemaLine
	rows                           int64
}

func (d *schemaDumper) tables() []string {
	tables := map[uint32]*tableBlock{}
	d.query(`
		SELECT c.oid, c.relname, c.relkind, c.relpersistence
		FROM pg_class c
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')`,
		func(rows *sql.Rows) error {
			tb := &tableBlock{}
			var kind, persistence string
			if err := rows.Scan(&tb.oid, &tb.name, &kind, &persistence); err != nil {
				return err
			}
			if d.members[schemaObject{class: "pg_class", oid: tb.oid}] {
				return nil
			}
			tb.head = "table " + tb.name
			if kind == "p" {
				tb.head += " partitioned"
			}
			if persistence == "u" {
				tb.head += " unlogged"
			}
			tables[tb.oid] = tb
			return nil
		})
	// A table with a column of an extension's type exists only where the
	// extension is installed: every line of it requires the extension.
	for _, tb := range tables {
		tb.requires = d.tableRequires(tb.oid)
	}
	d.query(`
		SELECT a.attrelid, a.attnum, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
			COALESCE(pg_get_expr(ad.adbin, ad.adrelid), ''), COALESCE(ad.oid, 0), a.attidentity, a.attgenerated,
			CASE WHEN a.attcollation <> t.typcollation THEN COALESCE(co.collname, '') ELSE '' END
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		LEFT JOIN pg_collation co ON co.oid = a.attcollation
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attrelid, a.attnum`,
		func(rows *sql.Rows) error {
			var rel, defOID uint32
			var num int32
			var name, typ, def, identity, generated, collation string
			var notNull bool
			if err := rows.Scan(&rel, &num, &name, &typ, &notNull, &def, &defOID, &identity, &generated, &collation); err != nil {
				return err
			}
			tb := tables[rel]
			if tb == nil {
				return nil
			}
			text := "  column " + name + " " + typ
			if collation != "" {
				text += " COLLATE " + collation
			}
			if notNull {
				text += " NOT NULL"
			}
			switch {
			case generated == "s":
				text += " GENERATED ALWAYS AS (" + def + ") STORED"
			case def != "":
				text += " DEFAULT " + def
			}
			switch identity {
			case "a":
				text += " GENERATED ALWAYS AS IDENTITY"
			case "d":
				text += " GENERATED BY DEFAULT AS IDENTITY"
			}
			req := d.requires(schemaObject{class: "pg_class", oid: rel, subID: num}, schemaObject{class: "pg_attrdef", oid: defOID})
			tb.columns = append(tb.columns, text+requiresSuffix(tb.requires, req))
			return nil
		})
	d.query(`
		SELECT conrelid, oid, conname, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE connamespace = 'public'::regnamespace AND conrelid <> 0 AND contype <> 'n'`,
		func(rows *sql.Rows) error {
			var rel, oid uint32
			var name, def string
			if err := rows.Scan(&rel, &oid, &name, &def); err != nil {
				return err
			}
			if tb := tables[rel]; tb != nil {
				req := d.requires(schemaObject{class: "pg_constraint", oid: oid})
				tb.constraints = append(tb.constraints, schemaLine{name, "  constraint " + name + " " + def + requiresSuffix(tb.requires, req)})
			}
			return nil
		})
	d.query(`
		SELECT i.indrelid, i.indexrelid, ic.relname, pg_get_indexdef(i.indexrelid), i.indisvalid
		FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
		WHERE ic.relnamespace = 'public'::regnamespace`,
		func(rows *sql.Rows) error {
			var rel, oid uint32
			var name, def string
			var valid bool
			if err := rows.Scan(&rel, &oid, &name, &def, &valid); err != nil {
				return err
			}
			if tb := tables[rel]; tb != nil {
				if !valid {
					def += " INVALID"
				}
				req := d.requires(schemaObject{class: "pg_class", oid: oid})
				tb.indexes = append(tb.indexes, schemaLine{name, "  index " + name + " " + def + requiresSuffix(tb.requires, req)})
			}
			return nil
		})
	d.query(`
		SELECT tgrelid, oid, tgname, pg_get_triggerdef(oid)
		FROM pg_trigger WHERE NOT tgisinternal`,
		func(rows *sql.Rows) error {
			var rel, oid uint32
			var name, def string
			if err := rows.Scan(&rel, &oid, &name, &def); err != nil {
				return err
			}
			if tb := tables[rel]; tb != nil {
				req := d.requires(schemaObject{class: "pg_trigger", oid: oid})
				tb.triggers = append(tb.triggers, schemaLine{name, "  trigger " + name + " " + def + requiresSuffix(tb.requires, req)})
			}
			return nil
		})

	var order []*tableBlock
	for _, tb := range tables {
		if err := d.db.QueryRow(`SELECT COUNT(*) FROM public.` + pq.QuoteIdentifier(tb.name)).Scan(&tb.rows); err != nil {
			d.t.Fatalf("count rows of %s: %v", tb.name, err)
		}
		order = append(order, tb)
	}
	sort.Slice(order, func(i, j int) bool { return order[i].name < order[j].name })
	var out []string
	for _, tb := range order {
		out = append(out, fmt.Sprintf("%s rows %d%s", tb.head, tb.rows, requiresSuffix(tb.requires)))
		out = append(out, tb.columns...)
		out = append(out, sortLines(tb.constraints)...)
		out = append(out, sortLines(tb.indexes)...)
		out = append(out, sortLines(tb.triggers)...)
	}
	return out
}

func (d *schemaDumper) views() []string {
	var lines []schemaLine
	d.query(`
		SELECT c.oid, c.relname, c.relkind, pg_get_viewdef(c.oid)
		FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('v', 'm')`,
		func(rows *sql.Rows) error {
			var o schemaObject
			var name, kind, def string
			if err := rows.Scan(&o.oid, &name, &kind, &def); err != nil {
				return err
			}
			o.class = "pg_class"
			if d.members[o] {
				return nil
			}
			label := "view"
			if kind == "m" {
				label = "materialized view"
			}
			lines = append(lines, schemaLine{name, fmt.Sprintf("%s %s %s", label, name, strconv.Quote(strings.TrimSpace(def)))})
			return nil
		})
	return sortLines(lines)
}

func (d *schemaDumper) functions() []string {
	var lines []schemaLine
	d.query(`
		SELECT p.oid, p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')', p.prokind,
			CASE WHEN p.prokind IN ('f', 'p', 'w') THEN pg_get_functiondef(p.oid) ELSE '' END
		FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace`,
		func(rows *sql.Rows) error {
			var o schemaObject
			var sig, kind, def string
			if err := rows.Scan(&o.oid, &sig, &kind, &def); err != nil {
				return err
			}
			o.class = "pg_proc"
			if d.members[o] {
				return nil
			}
			lines = append(lines, schemaLine{sig, fmt.Sprintf("function %s %s %s%s", sig, kind, strconv.Quote(strings.TrimSpace(def)),
				requiresSuffix(d.requires(o)))})
			return nil
		})
	return sortLines(lines)
}

func (d *schemaDumper) ledger() []string {
	var out []string
	d.query(`SELECT version, name FROM schema_migrations ORDER BY version`, func(rows *sql.Rows) error {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("ledger %04d %s", version, name))
		return nil
	})
	return out
}
