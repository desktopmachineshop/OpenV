package postgres

import (
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/templates"
)

// X13's literal equality for the template repository: the two lookups'
// statements, assembled from templateColumns, are the literals they replaced,
// byte for byte (spacing, line breaks and tabs included), and each method still
// sends its own. The statements are read off the wire by the recorder in
// pgkit_test.go, so the test needs no server and no database, and checks what
// each method sends, not a copy of it. The repository has no transaction: each
// method sends its one statement and nothing else. List's and Create's
// statements are not in this file: they are not replaced.

const (
	// oldTemplateGetByIDSelect, GetByID
	oldTemplateGetByIDSelect = `
		SELECT id, COALESCE(org_id::text, ''), template_key, name, description, snapshot, is_default, created_at
		FROM templates
		WHERE id = $1
	`
	// oldTemplateGetByKeySelect, GetByKey
	oldTemplateGetByKeySelect = `
		SELECT id, COALESCE(org_id::text, ''), template_key, name, description, snapshot, is_default, created_at
		FROM templates
		WHERE template_key = $1
	`
)

// The list the base wrote out, on one line.
const oldTemplateColumns = `id, COALESCE(org_id::text, ''), template_key, name, description, snapshot, is_default, created_at`

// The columns are the old list.
func TestTemplateColumnsAreTheOldList(t *testing.T) {
	if templateColumns != oldTemplateColumns {
		t.Errorf("templateColumns =\n  %q\nwant\n  %q", templateColumns, oldTemplateColumns)
	}
}

// Each lookup sends its old literal and nothing else, and answers as it did
// for no row: templates.ErrNotFound, for a malformed id too (noRow counts
// Postgres's refusal as no row).
func TestTemplateStatementsAreTheOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewTemplateRepository(db)

	got, err := repo.GetByID("00000000-0000-0000-0000-000000000001")
	if got != nil || err != templates.ErrNotFound {
		t.Errorf("GetByID with no row = %v, %v; want nil and templates.ErrNotFound", got, err)
	}
	wantEvents(t, "GetByID", rec.take(), "QUERY "+oldTemplateGetByIDSelect)

	got, err = repo.GetByID("not-a-uuid")
	if got != nil || err != templates.ErrNotFound {
		t.Errorf("GetByID of a malformed id = %v, %v; want nil and templates.ErrNotFound", got, err)
	}
	wantEvents(t, "GetByID of a malformed id", rec.take(), "QUERY "+oldTemplateGetByIDSelect)

	got, err = repo.GetByKey("no-such-key")
	if got != nil || err != templates.ErrNotFound {
		t.Errorf("GetByKey with no row = %v, %v; want nil and templates.ErrNotFound", got, err)
	}
	wantEvents(t, "GetByKey", rec.take(), "QUERY "+oldTemplateGetByKeySelect)
}

// The scan reads its columns in the order of the list, one row, as the recorder
// answers a statement with a row of values: GetByID's row and GetByKey's row
// each land in the fields they land in today. (The scan-error places are pinned
// by template_repository_columns_test.go.)
func TestTemplateScansReadTheirColumnsInOrder(t *testing.T) {
	when := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	db, rec := recorderDB(t)
	repo := NewTemplateRepository(db)
	values := []driver.Value{"the-id", "the-org", "the-key", "the-name", "the-description", []byte(`{"a":1}`), true, when}
	rec.answers = []recAnswer{
		{"WHERE id = $1", values},
		{"WHERE template_key = $1", values},
	}
	want := &templates.Template{ID: "the-id", OrgID: "the-org", Key: "the-key", Name: "the-name",
		Description: "the-description", Snapshot: json.RawMessage(`{"a":1}`), IsDefault: true, CreatedAt: when}

	got, err := repo.GetByID("the-id")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("GetByID = %#v, %v; want %#v", got, err, want)
	}
	got, err = repo.GetByKey("the-key")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("GetByKey = %#v, %v; want %#v", got, err, want)
	}
}
