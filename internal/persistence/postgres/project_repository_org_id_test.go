package postgres

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestOrgIDForProject pins the event bus's project-to-workspace lookup
// against a real database: the workspace id as canonical text for an id in
// any form the uuid cast reads; "" with no error for a project with no
// workspace (COALESCE), an id no project has, and one that is not a UUID or
// is empty (the $1::uuid cast refuses it); and the error when the database
// cannot be read. cmd/server's TestProjectOrgResolverRule (refactor step
// X7a) is its characterization through the boot's closure.
func TestOrgIDForProject(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewProjectRepository(db)

	org, project, orphan := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug, org_type) VALUES ($1, 'Acme', $2, 'company')`, org, "acme-"+org); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Roadmap'), ($3, NULL, 'Orphan')`,
		project, org, orphan); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name, input, want string
	}{
		{"a project in a workspace", project, org},
		{"its id in upper case", strings.ToUpper(project), org},
		{"its id without hyphens", strings.ReplaceAll(project, "-", ""), org},
		{"a project with no workspace", orphan, ""},
		{"a UUID no project has", uuid.NewString(), ""},
		{"a workspace's id", org, ""},
		{"not a UUID", "not-a-uuid", ""},
		{"a UUID with a character too many", project + "0", ""},
		{"the empty string", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got, err := repo.OrgIDForProject(c.input); got != c.want || err != nil {
				t.Errorf("OrgIDForProject(%q) = %q, %v; want %q, nil", c.input, got, err, c.want)
			}
		})
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.OrgIDForProject(project); got != "" || err == nil {
		t.Fatalf("over a closed database: %q, %v; want \"\" and the error", got, err)
	}
}
