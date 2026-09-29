package postgres

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/templates"
)

// A template id or key no row has is templates.ErrNotFound, which the API
// answers 404 "template not found": a project from a template that does not
// exist answered 500, since the repository's not-found was a bare error.
// Postgres-gated (OPENV_TEST_DATABASE_URL).
func TestTemplateLookupsReportATemplateThatIsNotThere(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewTemplateRepository(db)

	if _, err := repo.GetByID(uuid.New().String()); !errors.Is(err, templates.ErrNotFound) {
		t.Fatalf("GetByID of an id no template has: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByKey("no-such-template"); !errors.Is(err, templates.ErrNotFound) {
		t.Fatalf("GetByKey of a key no template has: err = %v, want ErrNotFound", err)
	}

	tpl := &templates.Template{ID: uuid.New().String(), Key: "tour-template", Name: "Tour",
		Snapshot: json.RawMessage(`{}`), CreatedAt: time.Now()}
	if err := repo.Create(tpl); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got, err := repo.GetByID(tpl.ID); err != nil || got.ID != tpl.ID {
		t.Fatalf("GetByID of a stored template: %+v, %v", got, err)
	}
	if got, err := repo.GetByKey(tpl.Key); err != nil || got.ID != tpl.ID {
		t.Fatalf("GetByKey of a stored template: %+v, %v", got, err)
	}
}
