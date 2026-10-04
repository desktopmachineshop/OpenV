package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// The services stamp the times they write into TIMESTAMP columns in UTC
// (#379 bug 93). A TIMESTAMP keeps the wall clock it is sent, dropping the
// offset, and every read takes that wall clock as UTC, so a time.Now() in a
// server's local zone moved by the zone's offset on its way in: on a server
// two hours east of UTC, a crew created at 09:00 UTC read back as created at
// 11:00 UTC, two hours in the future. Each service is driven through its
// real repository with time.Local two hours east of UTC: each time it
// stamps is stored as the UTC wall clock of the moment it was stamped, and
// the time it answers is the instant a later read answers.

// rtInCEST makes time.Local two hours east of UTC until the test ends.
func rtInCEST(t *testing.T) {
	t.Helper()
	prev := time.Local
	time.Local = rtCEST
	t.Cleanup(func() { time.Local = prev })
}

// rtStamp checks one stamped time: stored is what a TIMESTAMP column read
// back, answered what the service answered (zero when it answers none), and
// the stamp was taken between before and after.
func rtStamp(t *testing.T, what string, stored, answered, before, after time.Time) {
	t.Helper()
	lo, hi := before.UTC().Add(-time.Microsecond), after.UTC().Add(time.Microsecond)
	if stored.Before(lo) || stored.After(hi) {
		t.Errorf("%s stored the wall clock %s, want the UTC wall clock of when it was stamped, %s to %s",
			what, stored.Format(time.RFC3339Nano), lo.Format(time.RFC3339Nano), hi.Format(time.RFC3339Nano))
	}
	// A TIMESTAMP keeps microseconds, rounded half to even where Go's Round
	// goes away from zero, so the two may part by a microsecond.
	if d := answered.Sub(stored); !answered.IsZero() && (d > time.Microsecond || d < -time.Microsecond) {
		t.Errorf("%s answered %s, but a later read answers %s", what, answered.Format(time.RFC3339Nano),
			stored.Format(time.RFC3339Nano))
	}
}

func TestTheServicesStampTimesInUTC(t *testing.T) {
	db := rtDB(t)
	rtInCEST(t)
	org, userID, peopleTeam := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, org)
	rtSeedUser(t, db, userID, "dana@example.com", "Dana", "")
	rtSeed(t, db, `INSERT INTO org_teams (id, org_id, name) VALUES ($1, $2, 'Reviewers')`, peopleTeam, org)

	var before time.Time
	start := func() { before = time.Now() }

	t.Run("projects", func(t *testing.T) {
		repo := NewProjectRepository(db)
		svc := projects.NewService(repo)
		start()
		p := projects.NewProject(projects.CreateProjectRequest{Name: "Platform"})
		p.OrgID = org
		if err := svc.CreateProject(p); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByID(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "a new project's created_at", got.CreatedAt, p.CreatedAt, before, time.Now())
		rtStamp(t, "a new project's updated_at", got.UpdatedAt, p.UpdatedAt, before, time.Now())
		start()
		updated, err := svc.UpdateProject(p.ID, projects.UpdateProjectRequest{Name: "Platform 2"})
		if err != nil {
			t.Fatal(err)
		}
		if got, err = repo.GetByID(p.ID); err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "an updated project's updated_at", got.UpdatedAt, updated.UpdatedAt, before, time.Now())
	})

	project := uuid.New().String()
	rtSeedProject(t, db, project, org, "Board")

	t.Run("members", func(t *testing.T) {
		repo := NewMemberRepository(db)
		svc := members.NewDefaultService(repo)
		start()
		if err := svc.AddMember(project, userID, members.RoleEditor); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Find(project, userID)
		if err != nil || got == nil {
			t.Fatalf("Find: %v, %v", got, err)
		}
		rtStamp(t, "a membership's created_at", got.CreatedAt, time.Time{}, before, time.Now())
		start()
		if err := svc.GrantTeam(project, peopleTeam, members.RoleViewer); err != nil {
			t.Fatal(err)
		}
		grants, err := repo.ListTeamGrants(project)
		if err != nil || len(grants) != 1 {
			t.Fatalf("ListTeamGrants: %v, %v", grants, err)
		}
		rtStamp(t, "a team grant's created_at", grants[0].CreatedAt, time.Time{}, before, time.Now())
	})

	t.Run("work items", func(t *testing.T) {
		repo := NewWorkItemRepository(db)
		svc := workitems.NewDefaultService(repo, nil)
		start()
		item, err := svc.Create(workitems.CreateWorkItemRequest{ProjectID: project, Title: "Card", AssigneeID: &userID}, nil, "user:dana")
		if err != nil {
			t.Fatal(err)
		}
		got, err := repo.FindByID(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "a new card's created_at", got.CreatedAt, item.CreatedAt, before, time.Now())
		rtStamp(t, "a new card's updated_at", got.UpdatedAt, item.UpdatedAt, before, time.Now())
		feed, err := repo.ListActivity(item.ID)
		if err != nil || len(feed) != 1 {
			t.Fatalf("the new card's activity: %v, %v; want its assignment", feed, err)
		}
		rtStamp(t, "a new card's assignment entry", feed[0].CreatedAt, time.Time{}, before, time.Now())

		start()
		updated, err := svc.Update(item.ID, workitems.UpdateWorkItemRequest{Title: "Card 2"}, "user:dana")
		if err != nil {
			t.Fatal(err)
		}
		if got, err = repo.FindByID(item.ID); err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "an updated card's updated_at", got.UpdatedAt, updated.UpdatedAt, before, time.Now())

		start()
		moved, err := svc.Move(item.ID, workitems.MoveRequest{Column: workitems.ColumnDone}, "user:dana")
		if err != nil {
			t.Fatal(err)
		}
		if got, err = repo.FindByID(item.ID); err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "a moved card's updated_at", got.UpdatedAt, moved.UpdatedAt, before, time.Now())

		start()
		comment, err := svc.AddComment(item.ID, "Looks right", "user:dana")
		if err != nil {
			t.Fatal(err)
		}
		if feed, err = repo.ListActivity(item.ID); err != nil {
			t.Fatal(err)
		}
		for _, a := range feed {
			if a.ID == comment.ID {
				rtStamp(t, "a comment's created_at", a.CreatedAt, comment.CreatedAt, before, time.Now())
			}
		}
	})

	t.Run("crews and agents", func(t *testing.T) {
		agentRepo := NewAgentRepository(db)
		agentSvc, err := agents.NewFileService(t.TempDir(), agentRepo)
		if err != nil {
			t.Fatal(err)
		}
		start()
		agent, err := agentSvc.SaveDefinition(org, &agents.Definition{Slug: "drafter", Name: "Drafter", Provider: "claude-code",
			AllowedTools: []string{"mcp__openv__*"}, SystemPrompt: "Draft."})
		if err != nil {
			t.Fatal(err)
		}
		gotAgent, err := agentRepo.FindByID(agent.ID)
		if err != nil || gotAgent == nil || gotAgent.SyncedAt == nil {
			t.Fatalf("FindByID: %v, %v", gotAgent, err)
		}
		rtStamp(t, "a new agent's created_at", gotAgent.CreatedAt, agent.CreatedAt, before, time.Now())
		rtStamp(t, "a new agent's synced_at", *gotAgent.SyncedAt, *agent.SyncedAt, before, time.Now())
		start()
		agent, err = agentSvc.SaveDefinition(org, &agents.Definition{Slug: "drafter", Name: "Drafter 2", Provider: "claude-code",
			AllowedTools: []string{"mcp__openv__*"}, SystemPrompt: "Draft."})
		if err != nil {
			t.Fatal(err)
		}
		if gotAgent, err = agentRepo.FindByID(agent.ID); err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "a resaved agent's updated_at", gotAgent.UpdatedAt, agent.UpdatedAt, before, time.Now())

		repo := NewTeamRepository(db)
		svc := teams.NewDefaultService(repo)
		start()
		crew, err := svc.CreateTeam(org, "Crew", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := repo.FindTeamByID(crew.ID)
		if err != nil || got == nil {
			t.Fatalf("FindTeamByID: %v, %v", got, err)
		}
		rtStamp(t, "a new crew's created_at", got.CreatedAt, crew.CreatedAt, before, time.Now())
		rtStamp(t, "a new crew's updated_at", got.UpdatedAt, crew.UpdatedAt, before, time.Now())

		start()
		lead, err := svc.AddNode(crew.ID, teams.NodeSpec{AgentID: agent.ID, Label: "Lead"})
		if err != nil {
			t.Fatal(err)
		}
		writer, err := svc.AddNode(crew.ID, teams.NodeSpec{AgentID: agent.ID, Label: "Writer"})
		if err != nil {
			t.Fatal(err)
		}
		gotNode, err := repo.FindNodeByID(lead.ID)
		if err != nil || gotNode == nil {
			t.Fatalf("FindNodeByID: %v, %v", gotNode, err)
		}
		rtStamp(t, "a new node's created_at", gotNode.CreatedAt, lead.CreatedAt, before, time.Now())

		start()
		edge, err := svc.AddEdge(crew.ID, lead.ID, writer.ID, teams.EdgeDelegates, nil)
		if err != nil {
			t.Fatal(err)
		}
		gotEdge, err := repo.FindEdgeByID(edge.ID)
		if err != nil || gotEdge == nil {
			t.Fatalf("FindEdgeByID: %v, %v", gotEdge, err)
		}
		rtStamp(t, "a new edge's created_at", gotEdge.CreatedAt, edge.CreatedAt, before, time.Now())

		start()
		renamed := "Crew 2"
		updated, err := svc.UpdateTeam(crew.ID, &renamed, nil, &lead.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got, err = repo.FindTeamByID(crew.ID); err != nil {
			t.Fatal(err)
		}
		rtStamp(t, "an updated crew's updated_at", got.UpdatedAt, updated.UpdatedAt, before, time.Now())

		start()
		clone, err := svc.CloneTeam(crew.ID, "Clone", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, err = repo.FindTeamByID(clone.ID); err != nil || got == nil {
			t.Fatalf("FindTeamByID(clone): %v, %v", got, err)
		}
		rtStamp(t, "a clone's created_at", got.CreatedAt, clone.CreatedAt, before, time.Now())
		edges, err := repo.ListEdgesByTeam(clone.ID)
		if err != nil || len(edges) != 1 {
			t.Fatalf("the clone's edges: %v, %v", edges, err)
		}
		rtStamp(t, "a clone's edge's created_at", edges[0].CreatedAt, time.Time{}, before, time.Now())
	})
}
