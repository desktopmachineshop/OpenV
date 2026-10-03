package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/members"
)

// The project membership repository's round trip, direct roles and
// people-team grants (refactor plan S15b, OpenV REQ-23), pinned as found.
// The helpers are in repository_roundtrip_helpers_test.go.

func rtSeedProject(t *testing.T, db *sql.DB, id, orgID, name string) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name, description) VALUES ($1, $2, $3, '')`, id, orgID, name)
}

func rtSeedPeopleTeam(t *testing.T, db *sql.DB, id, orgID, name string, userIDs ...string) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO org_teams (id, org_id, name) VALUES ($1, $2, $3)`, id, orgID, name)
	for _, u := range userIDs {
		rtSeed(t, db, `INSERT INTO org_team_members (org_team_id, user_id) VALUES ($1, $2)`, id, u)
	}
}

// A membership reads back as saved, its created_at as a TIMESTAMP column
// keeps it (the wall clock, to the microsecond, in lib/pq's zone at offset
// 0), and Find joins no account, so the display fields stay empty. Upsert of
// a membership the project has rewrites the role alone, keeping created_at,
// and stores a role no one defined. An account no row has, a malformed
// account id and a malformed project id are members.ErrUnknownUser; a
// well-formed project no row has is the foreign key's refusal. A membership
// no row has, and a malformed id, read as no membership and no error, and
// Remove of either is no error.
func TestMemberRepositoryRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewMemberRepository(db)
	orgID, projectID, userID := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, orgID)
	rtSeedProject(t, db, projectID, orgID, "Platform")
	rtSeedUser(t, db, userID, "dana@example.com", "Dana", "https://example.com/dana.png")

	saved := &members.Member{ProjectID: projectID, UserID: userID, Role: members.RoleEditor, CreatedAt: rtAt(0).In(rtCEST)}
	if err := repo.Upsert(saved); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := repo.Find(projectID, userID)
	if err != nil || got == nil {
		t.Fatalf("Find: %v, %v", got, err)
	}
	rtWantTimestamp(t, "a membership's created_at, sent at +02:00", got.CreatedAt, saved.CreatedAt)
	rtWantSame(t, "a membership", members.Member{ProjectID: got.ProjectID, UserID: got.UserID, Role: got.Role,
		UserName: got.UserName, UserEmail: got.UserEmail, AvatarURL: got.AvatarURL},
		members.Member{ProjectID: projectID, UserID: userID, Role: members.RoleEditor})

	t.Run("upsert again", func(t *testing.T) {
		if err := repo.Upsert(&members.Member{ProjectID: projectID, UserID: userID, Role: members.RoleOwner,
			CreatedAt: rtAt(30)}); err != nil {
			t.Fatalf("Upsert of a membership the project has: %v", err)
		}
		after, err := repo.Find(projectID, userID)
		if err != nil || after == nil || after.Role != members.RoleOwner {
			t.Fatalf("Find after a second Upsert: %v, %v; want the owner role", after, err)
		}
		rtWantTimestamp(t, "created_at after a second Upsert", after.CreatedAt, saved.CreatedAt)

		if err := repo.Upsert(&members.Member{ProjectID: projectID, UserID: userID, Role: "superuser",
			CreatedAt: rtAt(31)}); err != nil {
			t.Errorf("Upsert of a role no one defined: %v, want it stored", err)
		}
		if after, err := repo.Find(projectID, userID); err != nil || after == nil || after.Role != "superuser" {
			t.Errorf("a role no one defined read back as %v, %v", after, err)
		}
	})

	t.Run("refused upserts", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			if err := repo.Upsert(&members.Member{ProjectID: projectID, UserID: id, Role: members.RoleViewer,
				CreatedAt: rtAt(0)}); err != members.ErrUnknownUser {
				t.Errorf("Upsert of the account %q: %v, want members.ErrUnknownUser", id, err)
			}
		}
		for _, id := range malformedIDs {
			if err := repo.Upsert(&members.Member{ProjectID: id, UserID: userID, Role: members.RoleViewer,
				CreatedAt: rtAt(0)}); err != members.ErrUnknownUser {
				t.Errorf("Upsert in the malformed project %q: %v, want members.ErrUnknownUser", id, err)
			}
		}
		err := repo.Upsert(&members.Member{ProjectID: uuid.New().String(), UserID: userID, Role: members.RoleViewer,
			CreatedAt: rtAt(0)})
		rtWantPQ(t, "Upsert in a project no row has", err, "23503", "project_members_project_id_fkey")
	})

	t.Run("not found", func(t *testing.T) {
		for _, c := range []struct{ project, user string }{
			{projectID, uuid.New().String()},
			{uuid.New().String(), userID},
			{malformed, userID},
			{projectID, "\xff"},
			{"a\x00b", userID},
		} {
			if found, err := repo.Find(c.project, c.user); found != nil || err != nil {
				t.Errorf("Find(%q, %q) = %v, %v; want nil, nil", c.project, c.user, found, err)
			}
		}
	})

	t.Run("remove", func(t *testing.T) {
		if err := repo.Remove(projectID, userID); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if found, err := repo.Find(projectID, userID); found != nil || err != nil {
			t.Errorf("Find after Remove: %v, %v; want nil, nil", found, err)
		}
		for _, c := range []struct{ project, user string }{
			{projectID, userID},
			{malformed, userID},
			{projectID, malformed},
		} {
			if err := repo.Remove(c.project, c.user); err != nil {
				t.Errorf("Remove(%q, %q) of a membership no row has: %v, want no error", c.project, c.user, err)
			}
		}
	})
}

// ListByProject lists a project's direct memberships with each account's
// name, email and avatar joined from users, by name and then email, which
// breaks a tie on name; people-team grants are not in it. A project with
// none, or no row, lists nil, and a malformed project id is Postgres's
// refusal.
func TestMemberRepositoryListByProject(t *testing.T) {
	db := rtDB(t)
	repo := NewMemberRepository(db)
	orgID, projectID, otherProject, peopleTeam := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, orgID)
	rtSeedProject(t, db, projectID, orgID, "Platform")
	rtSeedProject(t, db, otherProject, orgID, "Other")

	type account struct{ id, email, name, avatar string }
	casey := account{uuid.New().String(), "casey@example.com", "Casey", ""}
	averyB := account{uuid.New().String(), "b.avery@example.com", "Avery", "https://example.com/b.png"}
	nameless := account{uuid.New().String(), "zed@example.com", "", ""}
	averyA := account{uuid.New().String(), "a.avery@example.com", "Avery", ""}
	granted := account{uuid.New().String(), "granted@example.com", "Granted", ""}
	for i, a := range []account{casey, averyB, nameless, averyA, granted} {
		rtSeedUser(t, db, a.id, a.email, a.name, a.avatar)
		if a == granted {
			continue
		}
		// Joined newest first, so that created_at's order is not the name's.
		if err := repo.Upsert(&members.Member{ProjectID: projectID, UserID: a.id, Role: members.RoleViewer,
			CreatedAt: rtAt(-i)}); err != nil {
			t.Fatalf("Upsert %s: %v", a.email, err)
		}
	}
	if err := repo.Upsert(&members.Member{ProjectID: otherProject, UserID: granted.id, Role: members.RoleOwner,
		CreatedAt: rtAt(0)}); err != nil {
		t.Fatal(err)
	}
	rtSeedPeopleTeam(t, db, peopleTeam, orgID, "Reviewers", granted.id)
	if err := repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: projectID, OrgTeamID: peopleTeam, Role: members.RoleEditor,
		CreatedAt: rtAt(0)}); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListByProject(projectID)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	var emails []string
	for _, m := range list {
		emails = append(emails, m.UserEmail)
	}
	rtWantOrder(t, "a project's members", emails, []string{"zed@example.com"}, []string{"a.avery@example.com"},
		[]string{"b.avery@example.com"}, []string{"casey@example.com"})
	if len(list) == 4 {
		got := list[2]
		rtWantTimestamp(t, "a listed membership's created_at", got.CreatedAt, rtAt(-1))
		got.CreatedAt = time.Time{}
		rtWantSame(t, "a listed membership", *got, members.Member{ProjectID: projectID, UserID: averyB.id,
			Role: members.RoleViewer, UserName: "Avery", UserEmail: "b.avery@example.com", AvatarURL: "https://example.com/b.png"})
	}

	empty := uuid.New().String()
	rtSeedProject(t, db, empty, orgID, "Empty")
	list, err = repo.ListByProject(empty)
	rtWantNil(t, "the members of a project with none", list, err)
	list, err = repo.ListByProject(uuid.New().String())
	rtWantNil(t, "the members of a project no row has", list, err)
	list, err = repo.ListByProject(malformed)
	if list != nil {
		t.Errorf("ListByProject of a malformed id listed %v", list)
	}
	rtWantRefused(t, "ListByProject of a malformed id", err)
}

// An account's projects are those it is a member of, directly or through a
// people-team's grant, each once, in no set order; an account with none
// lists nil, and a malformed id is Postgres's refusal. Its roles on a
// project are every role it holds there, the direct one and each team
// grant's, duplicates kept, in no set order; none, or a malformed id on
// either side, is nil and no error.
func TestMemberRepositoryAccess(t *testing.T) {
	db := rtDB(t)
	repo := NewMemberRepository(db)
	orgID, user, outsider := uuid.New().String(), uuid.New().String(), uuid.New().String()
	p1, p2, p3, p4 := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	reviewers, editors, others := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, orgID)
	for _, p := range []string{p1, p2, p3, p4} {
		rtSeedProject(t, db, p, orgID, "Project "+p[:8])
	}
	rtSeedUser(t, db, user, "dana@example.com", "Dana", "")
	rtSeedUser(t, db, outsider, "olu@example.com", "Olu", "")
	rtSeedPeopleTeam(t, db, reviewers, orgID, "Reviewers", user)
	rtSeedPeopleTeam(t, db, editors, orgID, "Editors", user)
	rtSeedPeopleTeam(t, db, others, orgID, "Others", outsider)

	if err := repo.Upsert(&members.Member{ProjectID: p1, UserID: user, Role: members.RoleViewer, CreatedAt: rtAt(0)}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []members.TeamGrant{
		{ProjectID: p1, OrgTeamID: reviewers, Role: members.RoleViewer},
		{ProjectID: p1, OrgTeamID: editors, Role: members.RoleEditor},
		{ProjectID: p2, OrgTeamID: reviewers, Role: members.RoleReviewer},
		{ProjectID: p3, OrgTeamID: editors, Role: members.RoleEditor},
		{ProjectID: p4, OrgTeamID: others, Role: members.RoleOwner},
	} {
		g.CreatedAt = rtAt(0)
		if err := repo.UpsertTeamGrant(&g); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := repo.ListProjectIDsForUser(user)
	if err != nil {
		t.Fatalf("ListProjectIDsForUser: %v", err)
	}
	rtWantOrder(t, "an account's projects", ids, []string{p1, p2, p3})
	nobody := uuid.New().String()
	rtSeedUser(t, db, nobody, "nobody@example.com", "Nobody", "")
	ids, err = repo.ListProjectIDsForUser(nobody)
	rtWantNil(t, "the projects of an account with none", ids, err)
	ids, err = repo.ListProjectIDsForUser(uuid.New().String())
	rtWantNil(t, "the projects of an account no row has", ids, err)
	ids, err = repo.ListProjectIDsForUser(malformed)
	if ids != nil {
		t.Errorf("ListProjectIDsForUser of a malformed id listed %v", ids)
	}
	rtWantRefused(t, "ListProjectIDsForUser of a malformed id", err)

	roles, err := repo.RolesFor(p1, user)
	if err != nil {
		t.Fatalf("RolesFor: %v", err)
	}
	rtWantOrder(t, "an account's roles on a project", roles, []string{members.RoleViewer, members.RoleViewer, members.RoleEditor})
	roles, err = repo.RolesFor(p2, user)
	if err != nil {
		t.Fatalf("RolesFor: %v", err)
	}
	rtWantOrder(t, "an account's roles through one team", roles, []string{members.RoleReviewer})
	for _, c := range []struct{ project, user string }{
		{p4, user},
		{p1, outsider},
		{uuid.New().String(), user},
		{malformed, user},
		{p1, malformed},
		{p1, "\xff"},
	} {
		roles, err := repo.RolesFor(c.project, c.user)
		rtWantNil(t, fmt.Sprintf("RolesFor(%q, %q)", c.project, c.user), roles, err)
	}
}

// A team grant lists with its team's name, its created_at as a TIMESTAMP
// column keeps it; a project's grants list by team name alone (teams with
// one name tie, in no set order). UpsertTeamGrant of a grant the project
// has rewrites the role alone, keeping created_at; a team or project no row
// has is the foreign key's refusal, and a malformed id Postgres's, neither
// mapped to a sentinel. RemoveTeamGrant of a grant no row has, or of a
// malformed id, is no error. A project with no grant, or no row, lists nil,
// and a malformed project id is Postgres's refusal.
func TestMemberRepositoryTeamGrants(t *testing.T) {
	db := rtDB(t)
	repo := NewMemberRepository(db)
	orgID, projectID := uuid.New().String(), uuid.New().String()
	writers, approvers, qualityA, qualityB := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, orgID)
	rtSeedProject(t, db, projectID, orgID, "Platform")
	for _, team := range []struct{ id, name string }{
		{writers, "Writers"}, {qualityA, "Quality"}, {approvers, "Approvers"}, {qualityB, "Quality"},
	} {
		rtSeedPeopleTeam(t, db, team.id, orgID, team.name)
	}
	for i, team := range []string{writers, qualityA, approvers, qualityB} {
		// Granted newest first, so that created_at's order is not the name's.
		if err := repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: projectID, OrgTeamID: team, Role: members.RoleViewer,
			CreatedAt: rtAt(-i).In(rtCEST)}); err != nil {
			t.Fatalf("UpsertTeamGrant: %v", err)
		}
	}

	grants, err := repo.ListTeamGrants(projectID)
	if err != nil {
		t.Fatalf("ListTeamGrants: %v", err)
	}
	var names []string
	for _, g := range grants {
		names = append(names, g.TeamName)
	}
	rtWantOrder(t, "a project's team grants", names, []string{"Approvers"}, []string{"Quality", "Quality"}, []string{"Writers"})
	if len(grants) == 4 {
		got := grants[0]
		rtWantTimestamp(t, "a team grant's created_at, sent at +02:00", got.CreatedAt, rtAt(-2).In(rtCEST))
		got.CreatedAt = time.Time{}
		rtWantSame(t, "a team grant", *got, members.TeamGrant{ProjectID: projectID, OrgTeamID: approvers,
			Role: members.RoleViewer, TeamName: "Approvers"})
	}

	t.Run("upsert again", func(t *testing.T) {
		if err := repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: projectID, OrgTeamID: approvers, Role: members.RoleOwner,
			CreatedAt: rtAt(40)}); err != nil {
			t.Fatalf("UpsertTeamGrant of a grant the project has: %v", err)
		}
		grants, err := repo.ListTeamGrants(projectID)
		if err != nil || len(grants) != 4 || grants[0].OrgTeamID != approvers || grants[0].Role != members.RoleOwner {
			t.Fatalf("grants after a second UpsertTeamGrant: %s, %v; want Approvers first as owner", rtJSON(grants), err)
		}
		rtWantTimestamp(t, "a grant's created_at after a second UpsertTeamGrant", grants[0].CreatedAt, rtAt(-2).In(rtCEST))
	})

	t.Run("refused upserts", func(t *testing.T) {
		rtWantPQ(t, "UpsertTeamGrant of a team no row has", repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: projectID,
			OrgTeamID: uuid.New().String(), Role: members.RoleViewer}), "23503", "project_team_access_org_team_id_fkey")
		rtWantPQ(t, "UpsertTeamGrant in a project no row has", repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: uuid.New().String(),
			OrgTeamID: writers, Role: members.RoleViewer}), "23503", "project_team_access_project_id_fkey")
		rtWantRefused(t, "UpsertTeamGrant of a malformed team id", repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: projectID,
			OrgTeamID: malformed, Role: members.RoleViewer}))
	})

	t.Run("remove", func(t *testing.T) {
		if err := repo.RemoveTeamGrant(projectID, writers); err != nil {
			t.Fatalf("RemoveTeamGrant: %v", err)
		}
		grants, err := repo.ListTeamGrants(projectID)
		if err != nil || len(grants) != 3 {
			t.Errorf("grants after RemoveTeamGrant: %s, %v; want three", rtJSON(grants), err)
		}
		for _, c := range []struct{ project, team string }{
			{projectID, writers},
			{malformed, writers},
			{projectID, malformed},
		} {
			if err := repo.RemoveTeamGrant(c.project, c.team); err != nil {
				t.Errorf("RemoveTeamGrant(%q, %q) of a grant no row has: %v, want no error", c.project, c.team, err)
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		other := uuid.New().String()
		rtSeedProject(t, db, other, orgID, "Other")
		list, err := repo.ListTeamGrants(other)
		rtWantNil(t, "the grants of a project with none", list, err)
		list, err = repo.ListTeamGrants(uuid.New().String())
		rtWantNil(t, "the grants of a project no row has", list, err)
		list, err = repo.ListTeamGrants(malformed)
		if list != nil {
			t.Errorf("ListTeamGrants of a malformed id listed %v", list)
		}
		rtWantRefused(t, "ListTeamGrants of a malformed id", err)
	})
}

// A failure that is not the id's, here a database that fails every
// statement, is handed back as it came by every method: Upsert's is not
// members.ErrUnknownUser, the removals' is no longer no error, a read
// answers no membership or roles with it, and a list answers nil.
func TestMemberRepositoryHandsBackAFailure(t *testing.T) {
	repo := NewMemberRepository(rtClosedDB(t))
	id := uuid.New().String()
	rtWantClosed(t, "Upsert", repo.Upsert(&members.Member{ProjectID: id, UserID: id, Role: members.RoleViewer}), "")
	rtWantClosed(t, "Remove", repo.Remove(id, id), "")
	rtWantClosed(t, "UpsertTeamGrant", repo.UpsertTeamGrant(&members.TeamGrant{ProjectID: id, OrgTeamID: id, Role: members.RoleViewer}), "")
	rtWantClosed(t, "RemoveTeamGrant", repo.RemoveTeamGrant(id, id), "")
	found, err := repo.Find(id, id)
	if found != nil {
		t.Errorf("Find on a failing database read %v", found)
	}
	rtWantClosed(t, "Find", err, "")
	list, err := repo.ListByProject(id)
	if list != nil {
		t.Errorf("ListByProject on a failing database listed %v", list)
	}
	rtWantClosed(t, "ListByProject", err, "")
	ids, err := repo.ListProjectIDsForUser(id)
	if ids != nil {
		t.Errorf("ListProjectIDsForUser on a failing database listed %v", ids)
	}
	rtWantClosed(t, "ListProjectIDsForUser", err, "")
	roles, err := repo.RolesFor(id, id)
	if roles != nil {
		t.Errorf("RolesFor on a failing database listed %v", roles)
	}
	rtWantClosed(t, "RolesFor", err, "")
	grants, err := repo.ListTeamGrants(id)
	if grants != nil {
		t.Errorf("ListTeamGrants on a failing database listed %v", grants)
	}
	rtWantClosed(t, "ListTeamGrants", err, "")
}
