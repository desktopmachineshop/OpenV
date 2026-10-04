package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// The project repository's round trip, and the export side's project lookup
// over the same table (refactor plan S15b, OpenV REQ-23), pinned as found.
// The helpers are in repository_roundtrip_helpers_test.go.

func rtProjectSansTimes(p *projects.Project) projects.Project {
	c := *p
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	return c
}

func rtProjectNames(list []*projects.Project) []string {
	var names []string
	for _, p := range list {
		names = append(names, p.Name)
	}
	return names
}

// rtWantProjectError fails unless err is an error of the repository's own
// with exactly this text: no sentinel, and nothing errors.Is can tell apart
// from any other error.
func rtWantProjectError(t *testing.T, what string, err error, text string) {
	t.Helper()
	if err == nil || err.Error() != text || errors.Unwrap(err) != nil {
		t.Errorf("%s: %v, want an unwrapped error reading %q", what, err, text)
	}
}

// rtWantWrapped fails unless err starts with the repository's prefix and
// wraps Postgres's refusal of a malformed id.
func rtWantWrapped(t *testing.T, what string, err error, prefix string) {
	t.Helper()
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		t.Errorf("%s: %v, want an error starting %q", what, err, prefix)
	}
	rtWantRefused(t, what, err)
}

// A project reads back every field as saved: its times as TIMESTAMP columns
// keep them (the wall clock, to the microsecond, in lib/pq's zone at offset
// 0), and an empty workspace or parent as NULL that reads back "". Update
// rewrites name, description, agent auth, parent and updated_at, never the
// workspace or created_at. A project no row has is an error reading "project
// not found" from GetByID, Update and Delete alike; a malformed id is that
// error from GetByID, but Postgres's refusal, wrapped, from Update and Delete,
// as are an id a project has, a parent no row has and a malformed workspace.
// Delete takes the project's memberships and team grants and detaches its
// children, and leaves its work items and crews, which have no foreign key.
func TestProjectRepositoryRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	orgID := uuid.New().String()

	parent := &projects.Project{ID: uuid.New().String(), OrgID: orgID, Name: "Platform",
		Description: "The system", AgentAuth: projects.AgentAuthAPIKey, CreatedAt: rtAt(0), UpdatedAt: rtAt(1).In(rtCEST)}
	child := &projects.Project{ID: uuid.New().String(), OrgID: orgID, Name: "Runner", Description: "",
		AgentAuth: projects.AgentAuthUserAccount, ParentProjectID: parent.ID, CreatedAt: rtAt(2), UpdatedAt: rtAt(2)}
	loose := &projects.Project{ID: uuid.New().String(), Name: "No workspace", AgentAuth: projects.AgentAuthUserAccount,
		CreatedAt: rtAt(3), UpdatedAt: rtAt(3)}
	for _, p := range []*projects.Project{parent, child, loose} {
		if err := repo.Create(p); err != nil {
			t.Fatalf("Create %q: %v", p.Name, err)
		}
	}
	for _, p := range []*projects.Project{parent, child, loose} {
		got, err := repo.GetByID(p.ID)
		if err != nil || got == nil {
			t.Fatalf("GetByID %q: %v, %v", p.Name, got, err)
		}
		rtWantTimestamp(t, p.Name+"'s created_at", got.CreatedAt, p.CreatedAt)
		rtWantTimestamp(t, p.Name+"'s updated_at", got.UpdatedAt, p.UpdatedAt)
		rtWantSame(t, "the project "+p.Name, rtProjectSansTimes(got), rtProjectSansTimes(p))
	}
	var orgNull, parentNull bool
	if err := db.QueryRow(`SELECT org_id IS NULL, parent_project_id IS NULL FROM projects WHERE id = $1`, loose.ID).
		Scan(&orgNull, &parentNull); err != nil || !orgNull || !parentNull {
		t.Errorf("a project with no workspace or parent stored NULLs %v %v (%v), want both NULL", orgNull, parentNull, err)
	}

	t.Run("update", func(t *testing.T) {
		changed := *child
		changed.OrgID = uuid.New().String() // not a column Update writes
		changed.Name, changed.Description, changed.AgentAuth = "Runner (renamed)", "Agents", projects.AgentAuthAPIKey
		changed.ParentProjectID = ""
		changed.CreatedAt, changed.UpdatedAt = rtAt(50), rtAt(60).In(rtCEST) // created_at is not written either
		if err := repo.Update(&changed); err != nil {
			t.Fatalf("Update: %v", err)
		}
		after, err := repo.GetByID(child.ID)
		if err != nil || after == nil {
			t.Fatalf("GetByID after update: %v, %v", after, err)
		}
		rtWantTimestamp(t, "created_at after an update", after.CreatedAt, child.CreatedAt)
		rtWantTimestamp(t, "updated_at after an update, sent at +02:00", after.UpdatedAt, changed.UpdatedAt)
		want := rtProjectSansTimes(&changed)
		want.OrgID = orgID
		rtWantSame(t, "an updated project", rtProjectSansTimes(after), want)

		changed.ParentProjectID = parent.ID
		if err := repo.Update(&changed); err != nil {
			t.Fatalf("Update back under the parent: %v", err)
		}
		if after, err := repo.GetByID(child.ID); err != nil || after.ParentProjectID != parent.ID {
			t.Errorf("a project put back under its parent: %v, %v", after, err)
		}

		rtWantProjectError(t, "Update of a project no row has",
			repo.Update(&projects.Project{ID: uuid.New().String(), Name: "Ghost", AgentAuth: projects.AgentAuthUserAccount}),
			"project not found")
		rtWantWrapped(t, "Update of a malformed id",
			repo.Update(&projects.Project{ID: malformed, Name: "x", AgentAuth: projects.AgentAuthUserAccount}), "failed to update project: ")
		bad := changed
		bad.ParentProjectID = uuid.New().String()
		err = repo.Update(&bad)
		rtWantPQ(t, "Update under a parent no row has", err, "23503", "projects_parent_project_id_fkey")
		if err == nil || !strings.HasPrefix(err.Error(), "failed to update project: ") {
			t.Errorf("Update under a parent no row has: %v, want it wrapped as failed to update project", err)
		}
	})

	t.Run("refused creates", func(t *testing.T) {
		for _, c := range []struct {
			what       string
			project    *projects.Project
			code, name string
		}{
			{"Create of an id a project has", &projects.Project{ID: parent.ID, Name: "Again", AgentAuth: projects.AgentAuthUserAccount},
				"23505", "projects_pkey"},
			{"Create under a parent no row has", &projects.Project{ID: uuid.New().String(), Name: "x",
				AgentAuth: projects.AgentAuthUserAccount, ParentProjectID: uuid.New().String()}, "23503", "projects_parent_project_id_fkey"},
		} {
			err := repo.Create(c.project)
			rtWantPQ(t, c.what, err, c.code, c.name)
			if err == nil || !strings.HasPrefix(err.Error(), "failed to create project: ") {
				t.Errorf("%s: %v, want it wrapped as failed to create project", c.what, err)
			}
		}
		rtWantWrapped(t, "Create in a malformed workspace", repo.Create(&projects.Project{ID: uuid.New().String(), Name: "x",
			OrgID: malformed, AgentAuth: projects.AgentAuthUserAccount}), "failed to create project: ")
	})

	t.Run("not found", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			got, err := repo.GetByID(id)
			if got != nil {
				t.Errorf("GetByID(%q) read %v", id, got)
			}
			rtWantProjectError(t, fmt.Sprintf("GetByID(%q)", id), err, "project not found")
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, projects.ErrParentNotFound) {
				t.Errorf("GetByID(%q): %v is a sentinel, want none", id, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		userID, peopleTeam := uuid.New().String(), uuid.New().String()
		rtSeedOrg(t, db, orgID)
		rtSeedUser(t, db, userID, "dana@example.com", "Dana", "")
		rtSeed(t, db, `INSERT INTO org_teams (id, org_id, name) VALUES ($1, $2, 'Reviewers')`, peopleTeam, orgID)
		rtSeed(t, db, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, parent.ID, userID)
		rtSeed(t, db, `INSERT INTO project_team_access (project_id, org_team_id, role) VALUES ($1, $2, 'viewer')`, parent.ID, peopleTeam)
		rtSeed(t, db, `INSERT INTO work_items (id, project_id, title) VALUES ($1, $2, 'Left behind')`, uuid.New().String(), parent.ID)
		rtSeed(t, db, `INSERT INTO agent_teams (id, org_id, project_id, name) VALUES ($1, $2, $3, 'Left behind')`,
			uuid.New().String(), orgID, parent.ID)

		if err := repo.Delete(parent.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if got, err := repo.GetByID(parent.ID); got != nil || err == nil {
			t.Errorf("GetByID after Delete: %v, %v; want project not found", got, err)
		}
		if after, err := repo.GetByID(child.ID); err != nil || after.ParentProjectID != "" {
			t.Errorf("a deleted project's child: %v, %v; want it detached, parent \"\"", after, err)
		}
		for _, c := range []struct {
			table string
			want  int
		}{{"project_members", 0}, {"project_team_access", 0}, {"work_items", 1}, {"agent_teams", 1}} {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM `+c.table+` WHERE project_id = $1`, parent.ID).Scan(&n); err != nil || n != c.want {
				t.Errorf("a deleted project's rows in %s: %d (%v), want %d", c.table, n, err, c.want)
			}
		}
		rtWantProjectError(t, "Delete of a project no row has", repo.Delete(parent.ID), "project not found")
		rtWantWrapped(t, "Delete of a malformed id", repo.Delete(malformed), "failed to delete project: ")
	})
}

// GetAll lists every workspace's projects, and ListByOrg one workspace's,
// newest first by created_at alone (a tie in no set order). ListChildren
// lists the projects under a parent, whatever their workspace, oldest
// first, by created_at alone. Each answers [] when nothing matches,
// ListByOrg for an empty workspace id too (it fails closed, never listing
// the projects with no workspace) and ListChildren for an empty parent id
// (never the top-level projects). A malformed id is Postgres's refusal,
// wrapped.
func TestProjectRepositoryLists(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	orgID, otherOrg := uuid.New().String(), uuid.New().String()

	list, err := repo.GetAll()
	rtWantEmpty(t, "GetAll with no project", list, err)

	parentID, leafID := uuid.New().String(), uuid.New().String()
	for _, c := range []struct {
		id, name, org, parent string
		at                    int
	}{
		{parentID, "oldest", orgID, "", 0},
		{leafID, "middle", orgID, "", 2},
		{"", "second", orgID, "", 1},
		{"", "tie a", orgID, "", 5},
		{"", "tie b", orgID, "", 5},
		{"", "another workspace's", otherOrg, "", 3},
		{"", "no workspace's", "", "", 4},
		{"", "child, later", orgID, parentID, 9},
		{"", "child, sooner", orgID, parentID, 6},
		{"", "child tie a", orgID, parentID, 7},
		{"", "child tie b", otherOrg, parentID, 7},
	} {
		id := c.id
		if id == "" {
			id = uuid.New().String()
		}
		if err := repo.Create(&projects.Project{ID: id, OrgID: c.org, Name: c.name, AgentAuth: projects.AgentAuthUserAccount,
			ParentProjectID: c.parent, CreatedAt: rtAt(c.at), UpdatedAt: rtAt(c.at)}); err != nil {
			t.Fatalf("Create %q: %v", c.name, err)
		}
	}

	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	rtWantOrder(t, "every project", rtProjectNames(all), []string{"child, later"}, []string{"child tie a", "child tie b"},
		[]string{"child, sooner"}, []string{"tie a", "tie b"}, []string{"no workspace's"}, []string{"another workspace's"},
		[]string{"middle"}, []string{"second"}, []string{"oldest"})

	mine, err := repo.ListByOrg(orgID)
	if err != nil {
		t.Fatalf("ListByOrg: %v", err)
	}
	rtWantOrder(t, "a workspace's projects", rtProjectNames(mine), []string{"child, later"}, []string{"child tie a"},
		[]string{"child, sooner"}, []string{"tie a", "tie b"}, []string{"middle"}, []string{"second"}, []string{"oldest"})
	list, err = repo.ListByOrg("")
	rtWantEmpty(t, "ListByOrg with no workspace", list, err)
	list, err = repo.ListByOrg(uuid.New().String())
	rtWantEmpty(t, "ListByOrg of a workspace with no project", list, err)
	list, err = repo.ListByOrg(malformed)
	if list != nil {
		t.Errorf("ListByOrg of a malformed id listed %v", list)
	}
	rtWantWrapped(t, "ListByOrg of a malformed id", err, "failed to query projects: ")

	children, err := repo.ListChildren(parentID)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	rtWantOrder(t, "a project's children", rtProjectNames(children), []string{"child, sooner"},
		[]string{"child tie a", "child tie b"}, []string{"child, later"})
	for _, id := range []string{"", uuid.New().String(), leafID} {
		list, err = repo.ListChildren(id)
		rtWantEmpty(t, fmt.Sprintf("ListChildren(%q)", id), list, err)
	}
	list, err = repo.ListChildren(malformed)
	if list != nil {
		t.Errorf("ListChildren of a malformed id listed %v", list)
	}
	rtWantWrapped(t, "ListChildren of a malformed id", err, "failed to query child projects: ")
}

// The export side's lookup reads a project's id, name and description, and
// a project no row has, or a malformed id, is an error reading "project not
// found", a new value each time, so no caller can compare it to a sentinel.
func TestProjectInfoRepositoryFindByID(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectInfoRepository(db)
	project := &projects.Project{ID: uuid.New().String(), OrgID: uuid.New().String(), Name: "Platform",
		Description: "The system", AgentAuth: projects.AgentAuthUserAccount, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := NewProjectRepository(db).Create(project); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(project.ID)
	if err != nil || got == nil || got.ID != project.ID || got.Name != "Platform" || got.Description != "The system" {
		t.Errorf("FindByID: %+v, %v; want the project's id, name and description", got, err)
	}
	_, first := repo.FindByID(uuid.New().String())
	for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
		got, err := repo.FindByID(id)
		if got != nil {
			t.Errorf("FindByID(%q) read %+v", id, got)
		}
		rtWantProjectError(t, fmt.Sprintf("FindByID(%q)", id), err, "project not found")
		if err == first {
			t.Errorf("FindByID(%q) answered the same error value twice, want a new one each time", id)
		}
	}
}

// The description column takes NULL, which no write of the repository
// stores, and every read of the table reads such a row's description as ""
// (#379 bug 87; it used to fail the read with the scan's error): GetByID and
// the export side's lookup, and every list it falls in.
func TestProjectRepositoryReadsANullDescription(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	orgID, parentID, nullID := uuid.New().String(), uuid.New().String(), uuid.New().String()
	if err := repo.Create(&projects.Project{ID: parentID, OrgID: orgID, Name: "Parent",
		AgentAuth: projects.AgentAuthUserAccount, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}); err != nil {
		t.Fatal(err)
	}
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name, parent_project_id) VALUES ($1, $2, 'Null description', $3)`,
		nullID, orgID, parentID)

	got, err := repo.GetByID(nullID)
	if err != nil || got == nil || got.Name != "Null description" || got.Description != "" {
		t.Errorf("GetByID of a NULL description: %+v, %v; want the project, its description \"\"", got, err)
	}
	for name, c := range map[string]struct {
		list func() ([]*projects.Project, error)
		n    int
	}{
		"GetAll":       {repo.GetAll, 2},
		"ListByOrg":    {func() ([]*projects.Project, error) { return repo.ListByOrg(orgID) }, 2},
		"ListChildren": {func() ([]*projects.Project, error) { return repo.ListChildren(parentID) }, 1},
	} {
		list, err := c.list()
		if err != nil || len(list) != c.n {
			t.Errorf("%s over a NULL description: %d projects, %v; want %d", name, len(list), err, c.n)
			continue
		}
		for _, p := range list {
			if p.ID == nullID && p.Description != "" {
				t.Errorf("%s read a NULL description as %q, want \"\"", name, p.Description)
			}
		}
	}
	info, err := NewProjectInfoRepository(db).FindByID(nullID)
	if err != nil || info == nil || info.Name != "Null description" || info.Description != "" {
		t.Errorf("the export lookup of a NULL description: %+v, %v; want the project, its description \"\"", info, err)
	}
}

// A failure that is not the id's, here a database that fails every
// statement, is handed back wrapped with the method's own prefix (the
// export side's lookup as it came): a read answers no project with it,
// never "project not found", and a list answers nil, not [].
func TestProjectRepositoryHandsBackAFailure(t *testing.T) {
	db := rtClosedDB(t)
	repo := NewProjectRepository(db)
	id := uuid.New().String()
	project := &projects.Project{ID: id, Name: "x", AgentAuth: projects.AgentAuthUserAccount}
	rtWantClosed(t, "Create", repo.Create(project), "failed to create project: ")
	rtWantClosed(t, "Update", repo.Update(project), "failed to update project: ")
	rtWantClosed(t, "Delete", repo.Delete(id), "failed to delete project: ")
	found, err := repo.GetByID(id)
	if found != nil {
		t.Errorf("GetByID on a failing database read %v", found)
	}
	rtWantClosed(t, "GetByID", err, "failed to retrieve project: ")
	for _, c := range []struct {
		name, prefix string
		list         func() ([]*projects.Project, error)
	}{
		{"GetAll", "failed to query projects: ", repo.GetAll},
		{"ListByOrg", "failed to query projects: ", func() ([]*projects.Project, error) { return repo.ListByOrg(id) }},
		{"ListChildren", "failed to query child projects: ", func() ([]*projects.Project, error) { return repo.ListChildren(id) }},
	} {
		list, err := c.list()
		if list != nil {
			t.Errorf("%s on a failing database listed %v", c.name, list)
		}
		rtWantClosed(t, c.name, err, c.prefix)
	}
	info, err := NewProjectInfoRepository(db).FindByID(id)
	if info != nil {
		t.Errorf("the export lookup on a failing database read %+v", info)
	}
	rtWantClosed(t, "the export lookup", err, "")
}
