package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// X13's literal equality for the project repository: every query it builds
// from projectColumns is the literal it replaced, byte for byte (spacing,
// line breaks and tabs included). The statements are read off the wire by
// the recorder in pgkit_test.go, so the test needs no server and checks what
// each method sends, not a copy of it. Delete keeps its hand-rolled
// transaction and its statements, and Create, Update and OrgIDForProject
// keep theirs; none is rebuilt from projectColumns, so none is recorded here.

// The four queries X13 assembles from projectColumns, as the base (5f1ec5a)
// wrote them, byte for byte: each is the literal the method held, ListChildren's
// being the old cols prefix (a "SELECT ..." string) and its own tail joined.
const (
	// oldProjectColumns is the list the base repeated, without the SELECT.
	oldProjectColumns = `id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at`
	// oldProjectCols is the base's cols const, the SELECT and the list.
	oldProjectCols = `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at`
	// oldGetByIDSelect, GetByID
	oldGetByIDSelect = `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at FROM projects WHERE id = $1`
	// oldGetAllSelect, GetAll
	oldGetAllSelect = `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at FROM projects ORDER BY created_at DESC`
	// oldListByOrgSelect, ListByOrg: its list and its FROM are on two lines.
	oldListByOrgSelect = `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at
		FROM projects WHERE org_id = NULLIF($1, '')::uuid ORDER BY created_at DESC`
	// oldListChildrenSelect, ListChildren
	oldListChildrenSelect = `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at FROM projects WHERE parent_project_id = NULLIF($1, '')::uuid ORDER BY created_at ASC`
)

// projectColumns is the old list, and the SELECT in front of it the old cols.
func TestProjectColumnsIsTheOldList(t *testing.T) {
	if projectColumns != oldProjectColumns {
		t.Errorf("projectColumns =\n  %q\nwant\n  %q", projectColumns, oldProjectColumns)
	}
	if "SELECT "+projectColumns != oldProjectCols {
		t.Errorf("SELECT + projectColumns =\n  %q\nwant\n  %q", "SELECT "+projectColumns, oldProjectCols)
	}
}

// Each read sends its old literal and nothing else, and a read that finds no
// row answers as it did: GetByID projects.ErrNotFound, each list an empty,
// non-nil list.
func TestProjectReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewProjectRepository(db)
	id := uuid.New().String()

	if got, err := repo.GetByID(id); got != nil || err != projects.ErrNotFound {
		t.Errorf("GetByID with no row: %v, %v; want nil and projects.ErrNotFound", got, err)
	}
	wantEvents(t, "GetByID", rec.take(), "QUERY "+oldGetByIDSelect)

	reads := []struct {
		name  string
		read  func() ([]*projects.Project, error)
		query string
	}{
		{"GetAll", repo.GetAll, oldGetAllSelect},
		{"ListByOrg", func() ([]*projects.Project, error) { return repo.ListByOrg(id) }, oldListByOrgSelect},
		{"ListChildren", func() ([]*projects.Project, error) { return repo.ListChildren(id) }, oldListChildrenSelect},
	}
	for _, r := range reads {
		list, err := r.read()
		if list == nil || len(list) != 0 || err != nil {
			t.Errorf("%s with no rows: %#v, %v; want an empty, non-nil list and no error", r.name, list, err)
		}
		wantEvents(t, r.name, rec.take(), "QUERY "+r.query)
	}
}
