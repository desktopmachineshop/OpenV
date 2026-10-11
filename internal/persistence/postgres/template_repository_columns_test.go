package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/templates"
)

// The template repository's columns (refactor plan X13's characterization for
// template_repository.go, in the manner of the chatter repository's), pinned as
// found: the eight columns GetByID and GetByKey read, each holding a value no
// other does, so a swap shows; the seven List reads (the same list without the
// snapshot, which List leaves nil); what Create writes into each column, an
// empty workspace id as NULL; the NULLs the columns may hold once the schema is
// loosened, and the scan error that names each place, for each read that scans
// them; what each read answers for a row no one has, an empty list and a
// malformed id; and a database that fails. The existing tests
// (template_repository_test.go, malformed_id_test.go, malformed_parent_list_test.go
// and workspace_content_utc_stamps_test.go) pin the not-found sentinel, a
// malformed id and the stamps, not these columns.
//
// The three reads and Create send one statement each, so there is no
// transaction to pin. Every test that reads or writes rows gets its own
// database through rtDB; none takes a lock, so none needs NOWAIT.

// rtTplSeed inserts a template by column name, the way no method of the
// repository does (it is the row the reads are pinned on). A blank org stores
// NULL, a built-in's.
func rtTplSeed(t *testing.T, db *sql.DB, tpl *templates.Template) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO templates (id, org_id, template_key, name, description, snapshot, is_default, created_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8)`,
		tpl.ID, tpl.OrgID, tpl.Key, tpl.Name, tpl.Description, []byte(tpl.Snapshot), tpl.IsDefault, tpl.CreatedAt)
}

// rtTplOf is a template whose every field differs from the one for any other
// n, so a swap of two columns in a scan or an insert shows. Its created_at is
// rtAt(n).
func rtTplOf(org string, n int) *templates.Template {
	return &templates.Template{
		ID:          uuid.New().String(),
		OrgID:       org,
		Key:         fmt.Sprintf("key-%d", n),
		Name:        fmt.Sprintf("name %d", n),
		Description: fmt.Sprintf("description %d", n),
		Snapshot:    json.RawMessage(fmt.Sprintf(`{"artifacts": [{"title": "snapshot %d"}], "n": %d}`, n, n)),
		IsDefault:   n%2 == 1,
		CreatedAt:   rtAt(n),
	}
}

// rtTplWantSame fails unless got is sent, field by field, with the snapshot
// compared as the JSON it holds (jsonb normalises its text) and the time as a
// TIMESTAMP column hands it back, so a swap names the field it hit. A
// snapshot is left out of the comparison when sent has none: List does not
// read it.
func rtTplWantSame(t *testing.T, what string, got, sent *templates.Template, snapshot bool) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no template, want %s", what, sent.ID)
	}
	if got.ID != sent.ID {
		t.Errorf("%s id = %q, want %q", what, got.ID, sent.ID)
	}
	if got.OrgID != sent.OrgID {
		t.Errorf("%s org_id = %q, want %q", what, got.OrgID, sent.OrgID)
	}
	if got.Key != sent.Key {
		t.Errorf("%s template_key = %q, want %q", what, got.Key, sent.Key)
	}
	if got.Name != sent.Name {
		t.Errorf("%s name = %q, want %q", what, got.Name, sent.Name)
	}
	if got.Description != sent.Description {
		t.Errorf("%s description = %q, want %q", what, got.Description, sent.Description)
	}
	if snapshot {
		rtTplWantJSON(t, what+" snapshot", got.Snapshot, sent.Snapshot)
	} else if got.Snapshot != nil {
		t.Errorf("%s snapshot = %s, want none (the list does not read it)", what, got.Snapshot)
	}
	if got.IsDefault != sent.IsDefault {
		t.Errorf("%s is_default = %v, want %v", what, got.IsDefault, sent.IsDefault)
	}
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
}

// rtTplWantJSON fails unless two JSON texts decode to the same value.
func rtTplWantJSON(t *testing.T, what string, got, want []byte) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: %q does not decode: %v", what, got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("%s: wanted %q does not decode: %v", what, want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %s, want %s", what, got, want)
	}
}

// GetByID and GetByKey read back every column, each with a value no other
// column holds, from a row written by SQL rather than by Create, so the reads
// are pinned on their own: a workspace template (org_id set) and a built-in
// (org_id NULL, read as "").
func TestTemplateReadsBackEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewTemplateRepository(db)
	org := uuid.New().String()
	own, builtin := rtTplOf(org, 1), rtTplOf("", 2)
	rtTplSeed(t, db, own)
	rtTplSeed(t, db, builtin)

	for _, c := range []struct {
		name string
		want *templates.Template
	}{{"a workspace template", own}, {"a built-in", builtin}} {
		got, err := repo.GetByID(c.want.ID)
		if err != nil {
			t.Fatalf("GetByID of %s: %v", c.name, err)
		}
		rtTplWantSame(t, "GetByID of "+c.name, got, c.want, true)

		got, err = repo.GetByKey(c.want.Key)
		if err != nil {
			t.Fatalf("GetByKey of %s: %v", c.name, err)
		}
		rtTplWantSame(t, "GetByKey of "+c.name, got, c.want, true)
	}
}

// Both lookups find their row among others by their own key alone: the id or
// the key, not the other.
func TestTemplateLookupsKeyOnTheirOwnColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewTemplateRepository(db)
	a, b := rtTplOf("", 1), rtTplOf("", 2)
	rtTplSeed(t, db, a)
	rtTplSeed(t, db, b)

	if got, err := repo.GetByID(b.ID); err != nil || got.Key != b.Key {
		t.Errorf("GetByID(b) = %+v, %v; want b", got, err)
	}
	if got, err := repo.GetByKey(a.Key); err != nil || got.ID != a.ID {
		t.Errorf("GetByKey(a) = %+v, %v; want a", got, err)
	}
	// A key is no id, and an id is no key.
	if _, err := repo.GetByKey(a.ID); err != templates.ErrNotFound {
		t.Errorf("GetByKey of an id = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByID(a.Key); err != templates.ErrNotFound {
		t.Errorf("GetByID of a key = %v, want ErrNotFound (a malformed id is no row)", err)
	}
}

// List reads seven columns, the snapshot not among them (left nil), for the
// workspace's own templates and the built-ins, and no other workspace's: the
// built-ins and the workspace's default first, then newest first. The seven it
// reads are each held by a value no other holds.
func TestTemplateListReadsBackItsSevenColumns(t *testing.T) {
	db := rtDB(t)
	repo := NewTemplateRepository(db)
	org, other := uuid.New().String(), uuid.New().String()
	// n odd is a default. Created at rtAt(n): the order is is_default DESC,
	// created_at DESC.
	ownOld, ownNew := rtTplOf(org, 2), rtTplOf(org, 4)
	ownDefault, builtinDefault := rtTplOf(org, 3), rtTplOf("", 5)
	builtinPlain := rtTplOf("", 6)
	foreign := rtTplOf(other, 7)
	for _, tpl := range []*templates.Template{ownOld, ownNew, ownDefault, builtinDefault, builtinPlain, foreign} {
		rtTplSeed(t, db, tpl)
	}

	got, err := repo.List(org)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []*templates.Template{builtinDefault, ownDefault, builtinPlain, ownNew, ownOld}
	if len(got) != len(want) {
		t.Fatalf("List listed %s, want %d templates", rtJSON(got), len(want))
	}
	for i, w := range want {
		rtTplWantSame(t, fmt.Sprintf("List[%d]", i), got[i], w, false)
	}

	// An empty workspace id lists the built-ins alone.
	got, err = repo.List("")
	if err != nil || len(got) != 2 || got[0].ID != builtinDefault.ID || got[1].ID != builtinPlain.ID {
		t.Errorf("List of the empty workspace id = %s, %v; want the two built-ins", rtJSON(got), err)
	}
}

// List answers nil, never an empty list, and no error, for a workspace with
// no template and no built-in to show, for one no row has, and for a malformed
// id (a nil list too).
func TestTemplateListAnswersNilForNone(t *testing.T) {
	db := rtDB(t)
	repo := NewTemplateRepository(db)

	got, err := repo.List(uuid.New().String())
	rtWantNil(t, "List of a workspace with no template", got, err)
	got, err = repo.List("")
	rtWantNil(t, "List of the empty workspace id with no built-in", got, err)
	for _, id := range malformedIDs {
		got, err = repo.List(id)
		rtWantNil(t, fmt.Sprintf("List of the malformed id %q", id), got, err)
	}
}

// Create writes every field into its own column: read back by column name,
// the stored row is what was sent, an empty workspace id stored as NULL, and
// the three reads answer the same.
func TestTemplateCreateWritesEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewTemplateRepository(db)
	org := uuid.New().String()

	type stored struct {
		org         sql.NullString
		key         sql.NullString
		name        string
		description sql.NullString
		snapshot    []byte
		isDefault   bool
		createdAt   time.Time
	}
	read := func(id string) stored {
		t.Helper()
		var s stored
		err := db.QueryRow(`SELECT org_id::text, template_key, name, description, snapshot, is_default, created_at
			FROM templates WHERE id = $1`, id).Scan(&s.org, &s.key, &s.name, &s.description, &s.snapshot, &s.isDefault, &s.createdAt)
		if err != nil {
			t.Fatalf("reading the stored template %s: %v", id, err)
		}
		return s
	}

	for _, c := range []struct {
		name string
		sent *templates.Template
	}{
		{"a workspace template", rtTplOf(org, 1)},
		{"a built-in", rtTplOf("", 2)},
	} {
		sent := c.sent
		sent.CreatedAt = sent.CreatedAt.In(rtCEST) // the wall clock is stored, the offset dropped
		if err := repo.Create(sent); err != nil {
			t.Fatalf("Create of %s: %v", c.name, err)
		}

		s := read(sent.ID)
		if s.org.String != sent.OrgID || s.org.Valid != (sent.OrgID != "") {
			t.Errorf("%s: stored org_id = %v, want %q (NULL for none)", c.name, s.org, sent.OrgID)
		}
		if s.key.String != sent.Key || !s.key.Valid {
			t.Errorf("%s: stored template_key = %v, want %q", c.name, s.key, sent.Key)
		}
		if s.name != sent.Name {
			t.Errorf("%s: stored name = %q, want %q", c.name, s.name, sent.Name)
		}
		if s.description.String != sent.Description || !s.description.Valid {
			t.Errorf("%s: stored description = %v, want %q", c.name, s.description, sent.Description)
		}
		rtTplWantJSON(t, c.name+": stored snapshot", s.snapshot, sent.Snapshot)
		if s.isDefault != sent.IsDefault {
			t.Errorf("%s: stored is_default = %v, want %v", c.name, s.isDefault, sent.IsDefault)
		}
		rtWantTimestamp(t, c.name+": stored created_at", s.createdAt, sent.CreatedAt)

		got, err := repo.GetByID(sent.ID)
		if err != nil {
			t.Fatalf("GetByID after Create of %s: %v", c.name, err)
		}
		rtTplWantSame(t, "GetByID after Create of "+c.name, got, sent, true)
	}
}

// A column the schema lets hold NULL, or one a test has let take it, fails the
// read that scans it, naming the column and its place in the list: template_key
// is the third, name the fourth, description the fifth, then the snapshot (the
// sixth of GetByID's and GetByKey's eight, not in List's seven, which reads a
// row with a NULL one), is_default and created_at (the seventh and eighth, or
// the sixth and seventh of List's). Every read returns the scan's error as it
// came, with no template and, for List, no list. A NULL org_id is not an error:
// it reads as "", a built-in's.
func TestTemplateNullColumnsFailTheScanNamingItsPlace(t *testing.T) {
	const str = "converting NULL to string is unsupported"
	const flag = "sql/driver: couldn't convert <nil> (<nil>) into type bool"
	const raw = "unsupported Scan, storing driver.Value type <nil> into type *json.RawMessage"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	for _, c := range []struct {
		column    string
		wholeAt   int    // the column's place in GetByID's and GetByKey's list
		listAt    int    // its place in List's, 0 for none
		message   string // the scan error, empty when a NULL reads without one
		dropCheck bool   // the column is NOT NULL, to be loosened
	}{
		{column: "template_key", wholeAt: 3, listAt: 3, message: str},
		{column: "name", wholeAt: 4, listAt: 4, message: str, dropCheck: true},
		{column: "description", wholeAt: 5, listAt: 5, message: str},
		{column: "snapshot", wholeAt: 6, message: raw, dropCheck: true},
		{column: "is_default", wholeAt: 7, listAt: 6, message: flag, dropCheck: true},
		{column: "created_at", wholeAt: 8, listAt: 7, message: stamp, dropCheck: true},
		{column: "org_id", wholeAt: 2, listAt: 2},
	} {
		t.Run(c.column, func(t *testing.T) {
			db := rtDB(t)
			repo := NewTemplateRepository(db)
			org := uuid.New().String()
			tpl := rtTplOf(org, 1)
			rtTplSeed(t, db, tpl)
			if c.dropCheck {
				rtSeed(t, db, `ALTER TABLE templates ALTER COLUMN `+c.column+` DROP NOT NULL`)
			}
			rtSeed(t, db, `UPDATE templates SET `+c.column+` = NULL WHERE id = $1`, tpl.ID)

			wantFor := func(at int) string {
				return fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", at-1, c.column, c.message)
			}
			if c.message == "" {
				// A NULL org_id reads without an error, as "", a built-in's, by every read.
				got, err := repo.GetByID(tpl.ID)
				if err != nil || got == nil || got.OrgID != "" {
					t.Fatalf("GetByID over a NULL org_id = %s, %v; want a template with no org_id", rtJSON(got), err)
				}
				list, err := repo.List(org)
				if err != nil || len(list) != 1 || list[0].OrgID != "" {
					t.Fatalf("List over a NULL org_id = %s, %v; want the template with no org_id", rtJSON(list), err)
				}
				return
			}

			got, err := repo.GetByID(tpl.ID)
			if got != nil || err == nil || err.Error() != wantFor(c.wholeAt) {
				t.Errorf("GetByID over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, wantFor(c.wholeAt))
			}
			if c.column != "template_key" { // GetByKey finds its row by the key
				got, err = repo.GetByKey(tpl.Key)
				if got != nil || err == nil || err.Error() != wantFor(c.wholeAt) {
					t.Errorf("GetByKey over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, wantFor(c.wholeAt))
				}
			}
			list, err := repo.List(org)
			if c.listAt == 0 { // List does not read the column
				if err != nil || len(list) != 1 || list[0].ID != tpl.ID {
					t.Errorf("List over a NULL %s = %s, %v; want the template, which it reads without the column", c.column, rtJSON(list), err)
				}
				return
			}
			if list != nil || err == nil || err.Error() != wantFor(c.listAt) {
				t.Errorf("List over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(list), err, wantFor(c.listAt))
			}
		})
	}
}

// The reads and Create hand a failing database's error back as it came, with
// no template, no list and no write; a malformed id's refusal is the lists'
// nil and the lookups' not-found, as before. It needs no server.
func TestTemplateHandsBackADatabaseFailure(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewTemplateRepository(db)

	rec.failOn = "FROM templates"
	if got, err := repo.GetByID(uuid.New().String()); got != nil || err != errRecordedStatement {
		t.Errorf("GetByID over a failing database = %s, %v; want nil and the database's error as it came", rtJSON(got), err)
	}
	if got, err := repo.GetByKey("key"); got != nil || err != errRecordedStatement {
		t.Errorf("GetByKey over a failing database = %s, %v; want nil and the database's error as it came", rtJSON(got), err)
	}
	if got, err := repo.List(uuid.New().String()); got != nil || err != errRecordedStatement {
		t.Errorf("List over a failing database = %s, %v; want nil and the database's error as it came", rtJSON(got), err)
	}

	rec.failOn = "INSERT INTO templates"
	if err := repo.Create(rtTplOf("", 1)); err != errRecordedStatement {
		t.Errorf("Create over a failing database = %v; want the database's error as it came", err)
	}
}
