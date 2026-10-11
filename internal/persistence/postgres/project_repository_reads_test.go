package postgres

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// The project repository's reads (refactor plan X13's characterization for
// the project repository, in the manner of X13a's), pinned as found: every
// column read back through each read that scans the project's eight (the
// existing round trip reads them all back through GetByID alone and the
// lists only by name), the NULLs the COALESCEs turn into "" through each
// read, and the place of each column in the scan, which a scan error names.
// X13 moves the column list into projectColumns and the four scans into
// scanProject; these tests, with project_repository_roundtrip_test.go and
// project_repository_org_id_test.go, are what holds the repository still
// while it does. Delete keeps its hand-rolled transaction, so no new test
// of it is needed here (project_delete_test.go pins it). The helpers are in
// repository_roundtrip_helpers_test.go.

// rtWantProject fails unless got is sent as a project reads back: every
// field but the times equal, and each time the TIMESTAMP column's wall
// clock of the time sent.
func rtWantProject(t *testing.T, what string, got, sent *projects.Project) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtProjectSansTimes(got), rtProjectSansTimes(sent))
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
}

// rtWantProjectList fails unless list holds exactly the projects of want, in
// the order given, each read back whole.
func rtWantProjectList(t *testing.T, what string, list []*projects.Project, err error, want ...*projects.Project) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var wantNames []string
	for _, p := range want {
		wantNames = append(wantNames, p.Name)
	}
	if got := rtProjectNames(list); fmt.Sprint(got) != fmt.Sprint(wantNames) {
		t.Errorf("%s listed %q, want %q", what, got, wantNames)
		return
	}
	for i := range list {
		rtWantProject(t, what+" "+want[i].Name, list[i], want[i])
	}
}

// Every column Create writes reads back through GetByID and through each of
// the three lists, whole: the workspace, the name, the description, the
// agent auth, the parent and the two TIMESTAMP stamps (the wall clock sent,
// its offset dropped, to the microsecond). Each column holds a value no
// other does, so a swap of two in a scan shows.
func TestProjectRepositoryReadsBackEveryColumnThroughEveryRead(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	orgID := uuid.New().String()

	parent := &projects.Project{ID: uuid.New().String(), OrgID: orgID, Name: "Platform", Description: "The system",
		AgentAuth: projects.AgentAuthAPIKey, CreatedAt: rtAt(-200).In(rtCEST), UpdatedAt: rtAt(1).In(rtCEST)}
	first := &projects.Project{ID: uuid.New().String(), OrgID: orgID, Name: "Runner", Description: "Runs agents",
		AgentAuth: projects.AgentAuthUserAccount, ParentProjectID: parent.ID, CreatedAt: rtAt(10), UpdatedAt: rtAt(13).In(rtCEST)}
	second := &projects.Project{ID: uuid.New().String(), OrgID: orgID, Name: "Console", Description: "",
		AgentAuth: projects.AgentAuthAPIKey, ParentProjectID: parent.ID, CreatedAt: rtAt(20).In(rtCEST), UpdatedAt: rtAt(22)}
	for _, p := range []*projects.Project{parent, first, second} {
		if err := repo.Create(p); err != nil {
			t.Fatalf("Create %q: %v", p.Name, err)
		}
		got, err := repo.GetByID(p.ID)
		if err != nil {
			t.Fatalf("GetByID %q: %v", p.Name, err)
		}
		rtWantProject(t, "GetByID "+p.Name, got, p)
	}

	// Newest created_at first (by the wall clock a +02:00 stamp keeps, which
	// is why the parent, stamped at +02:00, is made old enough to come last).
	list, err := repo.GetAll()
	rtWantProjectList(t, "GetAll", list, err, second, first, parent)
	list, err = repo.ListByOrg(orgID)
	rtWantProjectList(t, "ListByOrg", list, err, second, first, parent)
	// Oldest created_at first.
	list, err = repo.ListChildren(parent.ID)
	rtWantProjectList(t, "ListChildren", list, err, first, second)
}

// The columns that take NULL read as "" through every read that scans them:
// the workspace, the description and the parent, each COALESCEd. The row
// below has no workspace and no parent (GetByID, GetAll); another has a
// workspace, no parent and no description (ListByOrg); a third has a parent,
// no workspace and no description (ListChildren). The description's NULL
// through every read is also pinned by TestProjectRepositoryReadsANullDescription.
func TestProjectRepositoryReadsNullColumnsThroughEveryRead(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	orgID, parentID := uuid.New().String(), uuid.New().String()

	bare := uuid.New().String()
	inOrg := uuid.New().String()
	underParent := uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name, description, agent_auth, parent_project_id, created_at, updated_at) VALUES
		($1, NULL, 'Bare', NULL, 'api-key', NULL, $4, $5),
		($2, $6, 'In a workspace', NULL, 'user-account', NULL, $7, $7),
		($3, NULL, 'Under a parent', NULL, 'api-key', $8, $9, $9),
		($8, NULL, 'The parent', 'Described', 'user-account', NULL, $10, $10)`,
		bare, inOrg, underParent, rtAt(0), rtAt(1), orgID, rtAt(2), parentID, rtAt(3), rtAt(-5))

	wantBare := &projects.Project{ID: bare, Name: "Bare", AgentAuth: projects.AgentAuthAPIKey, CreatedAt: rtAt(0), UpdatedAt: rtAt(1)}
	wantInOrg := &projects.Project{ID: inOrg, OrgID: orgID, Name: "In a workspace", AgentAuth: projects.AgentAuthUserAccount,
		CreatedAt: rtAt(2), UpdatedAt: rtAt(2)}
	wantUnder := &projects.Project{ID: underParent, Name: "Under a parent", AgentAuth: projects.AgentAuthAPIKey,
		ParentProjectID: parentID, CreatedAt: rtAt(3), UpdatedAt: rtAt(3)}
	wantParent := &projects.Project{ID: parentID, Name: "The parent", Description: "Described",
		AgentAuth: projects.AgentAuthUserAccount, CreatedAt: rtAt(-5), UpdatedAt: rtAt(-5)}

	for _, want := range []*projects.Project{wantBare, wantInOrg, wantUnder, wantParent} {
		got, err := repo.GetByID(want.ID)
		if err != nil {
			t.Fatalf("GetByID %q: %v", want.Name, err)
		}
		rtWantProject(t, "GetByID "+want.Name, got, want)
	}
	list, err := repo.GetAll()
	rtWantProjectList(t, "GetAll", list, err, wantUnder, wantInOrg, wantBare, wantParent)
	list, err = repo.ListByOrg(orgID)
	rtWantProjectList(t, "ListByOrg", list, err, wantInOrg)
	list, err = repo.ListChildren(parentID)
	rtWantProjectList(t, "ListChildren", list, err, wantUnder)
}

// A column the schema forbids NULL, here one a test has let take it, fails
// the read that scans it, naming the column and its place among the eight
// scanned: the name 2, the agent auth 4, created_at 6 and updated_at 7. Each
// read wraps the scan's error with its own prefix and answers no project
// and no list (nil, not []), the lists whole even when only one row fails.
func TestProjectRepositoryScanErrorsNameTheColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	orgID, parentID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name, created_at, updated_at) VALUES ($1, $2, 'Parent', $3, $3)`,
		parentID, orgID, rtAt(0))
	for _, column := range []string{"name", "agent_auth", "created_at", "updated_at"} {
		rtSeed(t, db, `ALTER TABLE projects ALTER COLUMN `+column+` DROP NOT NULL`)
	}

	for _, c := range []struct {
		column string
		want   string // the scan's error
	}{
		{"name", `sql: Scan error on column index 2, name "name": converting NULL to string is unsupported`},
		{"agent_auth", `sql: Scan error on column index 4, name "agent_auth": converting NULL to string is unsupported`},
		{"created_at", `sql: Scan error on column index 6, name "created_at": unsupported Scan, storing driver.Value type <nil> into type *time.Time`},
		{"updated_at", `sql: Scan error on column index 7, name "updated_at": unsupported Scan, storing driver.Value type <nil> into type *time.Time`},
	} {
		id := uuid.New().String()
		values := map[string]string{"name": "'Broken'", "agent_auth": "'user-account'", "created_at": "NOW()", "updated_at": "NOW()"}
		values[c.column] = "NULL"
		rtSeed(t, db, fmt.Sprintf(`INSERT INTO projects (id, org_id, parent_project_id, name, agent_auth, created_at, updated_at)
			VALUES ($1, $2, $3, %s, %s, %s, %s)`, values["name"], values["agent_auth"], values["created_at"], values["updated_at"]),
			id, orgID, parentID)

		if got, err := repo.GetByID(id); got != nil || err == nil || err.Error() != "failed to retrieve project: "+c.want {
			t.Errorf("GetByID of a row with a NULL %s: %v, %v; want failed to retrieve project: %s", c.column, got, err, c.want)
		}
		for name, list := range map[string]func() ([]*projects.Project, error){
			"GetAll":       repo.GetAll,
			"ListByOrg":    func() ([]*projects.Project, error) { return repo.ListByOrg(orgID) },
			"ListChildren": func() ([]*projects.Project, error) { return repo.ListChildren(parentID) },
		} {
			got, err := list()
			if got != nil || err == nil || err.Error() != "failed to scan project: "+c.want {
				t.Errorf("%s over a row with a NULL %s: %v, %v; want nil and failed to scan project: %s", name, c.column, got, err, c.want)
			}
		}
		rtSeed(t, db, `DELETE FROM projects WHERE id = $1`, id)
	}
}
