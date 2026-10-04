package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/teams"
)

// The team repository's round trip (refactor plan S15b, OpenV REQ-23): a
// crew, its nodes and its edges, pinned as found. The helpers are in
// repository_roundtrip_helpers_test.go.

// rtTeamSansTimes is a crew with its times zeroed, to compare every other
// field once the times are checked on their own.
func rtTeamSansTimes(team *teams.Team) teams.Team {
	c := *team
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	return c
}

func rtNodeSansTime(n *teams.Node) teams.Node {
	c := *n
	c.CreatedAt = time.Time{}
	return c
}

func rtEdgeSansTime(e *teams.Edge) teams.Edge {
	c := *e
	c.CreatedAt = time.Time{}
	return c
}

func rtTeamNames(list []*teams.Team) []string {
	var names []string
	for _, team := range list {
		names = append(names, team.Name)
	}
	return names
}

// rtSeedAgent is an agent row for a crew node to place (the node's agent_id
// is a foreign key).
func rtSeedAgent(t *testing.T, db *sql.DB, id, orgID, slug string) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, NULLIF($2, '')::uuid, $3, $3, 'claude')`, id, orgID, slug)
}

// A crew reads back every field as saved: its times as TIMESTAMP columns
// keep them (the wall clock, to the microsecond, in lib/pq's zone at offset
// 0), and no project or entry node as NULL that reads back nil. UpdateTeam
// rewrites name, description, project, entry node, the default flag and
// updated_at, never the workspace or created_at, and an id no row has is no
// error. A crew no row has, and a malformed id, read as no crew and no
// error, never a sentinel. An id a crew has, a malformed workspace id, and
// an empty workspace, project or entry node id are Postgres's refusal (#379
// bug 91: an empty workspace became NULL, where an empty project or entry
// node never did); so is a malformed id in UpdateTeam, MarkDefault and
// DeleteTeam, while deleting a crew no row has is no error. An entry node no
// row has is the foreign key's refusal, in SaveTeam and UpdateTeam alike
// (bug 91: it was stored).
func TestTeamRepositoryTeamRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	orgID, projectID, agentID := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedAgent(t, db, agentID, orgID, "lead")

	saved := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Requirements crew",
		Description: "Drafts, reviews and traces", ProjectID: &projectID,
		CreatedAt: rtAt(0), UpdatedAt: rtAt(1).In(rtCEST)}
	if err := repo.SaveTeam(saved); err != nil {
		t.Fatalf("SaveTeam: %v", err)
	}
	got, err := repo.FindTeamByID(saved.ID)
	if err != nil || got == nil {
		t.Fatalf("FindTeamByID: %v, %v", got, err)
	}
	rtWantTimestamp(t, "a crew's created_at", got.CreatedAt, saved.CreatedAt)
	rtWantTimestamp(t, "a crew's updated_at, sent at +02:00", got.UpdatedAt, saved.UpdatedAt)
	if ns := got.CreatedAt.Nanosecond(); ns != 123457000 {
		t.Errorf("created_at kept %d ns of 123456789, want 123457000 (rounded to the microsecond)", ns)
	}
	rtWantSame(t, "a crew", rtTeamSansTimes(got), rtTeamSansTimes(saved))

	bare := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Bare", CreatedAt: rtAt(2), UpdatedAt: rtAt(2)}
	if err := repo.SaveTeam(bare); err != nil {
		t.Fatalf("SaveTeam with no project: %v", err)
	}
	var projectNull, entryNull bool
	if err := db.QueryRow(`SELECT project_id IS NULL, entry_node_id IS NULL FROM agent_teams WHERE id = $1`,
		bare.ID).Scan(&projectNull, &entryNull); err != nil || !projectNull || !entryNull {
		t.Errorf("a crew with no project or entry node stored NULLs %v %v (%v), want both NULL", projectNull, entryNull, err)
	}
	if got, err := repo.FindTeamByID(bare.ID); err != nil || got == nil {
		t.Errorf("FindTeamByID(bare): %v, %v", got, err)
	} else {
		rtWantSame(t, "a crew with no project or entry node", rtTeamSansTimes(got), rtTeamSansTimes(bare))
	}

	t.Run("update", func(t *testing.T) {
		lead := &teams.Node{ID: uuid.New().String(), TeamID: saved.ID, AgentID: agentID, Label: "Lead", CreatedAt: rtAt(3)}
		if err := repo.SaveNode(lead); err != nil {
			t.Fatal(err)
		}
		newEntry := lead.ID
		changed := *got
		changed.OrgID = uuid.New().String() // not a column UpdateTeam writes
		changed.Name, changed.Description = "Renamed crew", ""
		changed.ProjectID, changed.EntryNodeID, changed.IsDefault = nil, &newEntry, true
		changed.CreatedAt, changed.UpdatedAt = rtAt(50), rtAt(60) // created_at is not written either
		if err := repo.UpdateTeam(&changed); err != nil {
			t.Fatalf("UpdateTeam: %v", err)
		}
		after, err := repo.FindTeamByID(saved.ID)
		if err != nil || after == nil {
			t.Fatalf("FindTeamByID after update: %v, %v", after, err)
		}
		rtWantTimestamp(t, "created_at after an update", after.CreatedAt, saved.CreatedAt)
		rtWantTimestamp(t, "updated_at after an update", after.UpdatedAt, rtAt(60))
		want := rtTeamSansTimes(&changed)
		want.OrgID = orgID
		rtWantSame(t, "an updated crew", rtTeamSansTimes(after), want)

		ghost := &teams.Team{ID: uuid.New().String(), Name: "Ghost", UpdatedAt: rtAt(0)}
		if err := repo.UpdateTeam(ghost); err != nil {
			t.Errorf("UpdateTeam of a crew no row has: %v, want no error", err)
		}
		if found, err := repo.FindTeamByID(ghost.ID); found != nil || err != nil {
			t.Errorf("UpdateTeam of a crew no row has wrote one: %v, %v", found, err)
		}
		rtWantRefused(t, "UpdateTeam of a malformed id", repo.UpdateTeam(&teams.Team{ID: malformed, Name: "x"}))

		gone := uuid.New().String()
		changed.EntryNodeID = &gone
		rtWantPQ(t, "UpdateTeam to an entry node no row has", repo.UpdateTeam(&changed), "23503", "agent_teams_entry_node_id_fkey")
		if after, err := repo.FindTeamByID(saved.ID); err != nil || after == nil || after.EntryNodeID == nil || *after.EntryNodeID != newEntry {
			t.Errorf("a crew after a refused entry node: %v, %v; want its entry node kept", after, err)
		}
	})

	t.Run("refused saves", func(t *testing.T) {
		rtWantPQ(t, "SaveTeam of an id a crew has", repo.SaveTeam(&teams.Team{ID: saved.ID, OrgID: orgID, Name: "Again"}),
			"23505", "agent_teams_pkey")
		// An empty workspace, project or entry node id is text Postgres
		// refuses as a uuid: none becomes NULL.
		empty := ""
		rtWantRefused(t, "SaveTeam with no workspace", repo.SaveTeam(&teams.Team{ID: uuid.New().String(), Name: "x"}))
		rtWantRefused(t, "SaveTeam with an empty project id", repo.SaveTeam(&teams.Team{ID: uuid.New().String(), OrgID: orgID,
			Name: "x", ProjectID: &empty}))
		rtWantRefused(t, "SaveTeam with an empty entry node id", repo.SaveTeam(&teams.Team{ID: uuid.New().String(), OrgID: orgID,
			Name: "x", EntryNodeID: &empty}))
		rtWantRefused(t, "SaveTeam with a malformed workspace id", repo.SaveTeam(&teams.Team{ID: uuid.New().String(), Name: "x", OrgID: malformed}))
		gone := uuid.New().String()
		rtWantPQ(t, "SaveTeam with an entry node no row has", repo.SaveTeam(&teams.Team{ID: uuid.New().String(), OrgID: orgID,
			Name: "x", EntryNodeID: &gone}), "23503", "agent_teams_entry_node_id_fkey")
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM agent_teams`).Scan(&n); err != nil || n != 2 {
			t.Errorf("crews after the refused saves: %d (%v), want the two saved", n, err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			if found, err := repo.FindTeamByID(id); found != nil || err != nil {
				t.Errorf("FindTeamByID(%q) = %v, %v; want nil, nil", id, found, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := repo.DeleteTeam(saved.ID); err != nil {
			t.Fatalf("DeleteTeam: %v", err)
		}
		if found, err := repo.FindTeamByID(saved.ID); found != nil || err != nil {
			t.Errorf("FindTeamByID after DeleteTeam: %v, %v; want nil, nil", found, err)
		}
		if err := repo.DeleteTeam(saved.ID); err != nil {
			t.Errorf("DeleteTeam of a crew no row has: %v, want no error", err)
		}
		rtWantRefused(t, "DeleteTeam of a malformed id", repo.DeleteTeam(malformed))
		if found, err := repo.FindTeamByID(bare.ID); found == nil || err != nil {
			t.Errorf("DeleteTeam removed another crew: %v, %v", found, err)
		}
	})
}

// MarkDefault sets the flag alone: it leaves updated_at and every other
// default of the workspace as they were. FindDefaultTeam answers the oldest
// default crew of the workspace by created_at, and no crew and no error for
// a workspace with none, an empty workspace id (even beside a default crew
// with no workspace, a row from before workspaces that SaveTeam no longer
// writes) and a malformed one.
func TestTeamRepositoryDefaultTeam(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	orgID, otherOrg := uuid.New().String(), uuid.New().String()

	if found, err := repo.FindDefaultTeam(orgID); found != nil || err != nil {
		t.Errorf("FindDefaultTeam with no crew: %v, %v; want nil, nil", found, err)
	}
	save := func(name, org string, at int, isDefault bool) *teams.Team {
		team := &teams.Team{ID: uuid.New().String(), OrgID: org, Name: name, IsDefault: isDefault,
			CreatedAt: rtAt(at), UpdatedAt: rtAt(at)}
		if err := repo.SaveTeam(team); err != nil {
			t.Fatalf("SaveTeam %q: %v", name, err)
		}
		return team
	}
	later := save("later", orgID, 5, false)
	earlier := save("earlier", orgID, 1, false)
	save("plain", orgID, 0, false)
	save("another workspace's default", otherOrg, -10, true)
	rtSeed(t, db, `INSERT INTO agent_teams (id, name, is_default, created_at, updated_at) VALUES ($1, 'no workspace''s default', TRUE, $2, $2)`,
		uuid.New().String(), rtAt(-20))

	if found, err := repo.FindDefaultTeam(orgID); found != nil || err != nil {
		t.Errorf("FindDefaultTeam with no default yet: %v, %v; want nil, nil", found, err)
	}
	if err := repo.MarkDefault(later.ID); err != nil {
		t.Fatalf("MarkDefault: %v", err)
	}
	marked, err := repo.FindTeamByID(later.ID)
	if err != nil || marked == nil || !marked.IsDefault {
		t.Fatalf("a marked crew: %v, %v; want is_default", marked, err)
	}
	rtWantTimestamp(t, "updated_at after MarkDefault", marked.UpdatedAt, later.UpdatedAt)
	if found, err := repo.FindDefaultTeam(orgID); err != nil || found == nil || found.ID != later.ID {
		t.Errorf("FindDefaultTeam: %v, %v; want %q", found, err, later.Name)
	}

	if err := repo.MarkDefault(earlier.ID); err != nil {
		t.Fatalf("MarkDefault: %v", err)
	}
	if still, err := repo.FindTeamByID(later.ID); err != nil || still == nil || !still.IsDefault {
		t.Errorf("the first default after a second MarkDefault: %v, %v; want it still flagged", still, err)
	}
	if found, err := repo.FindDefaultTeam(orgID); err != nil || found == nil || found.ID != earlier.ID {
		t.Errorf("FindDefaultTeam of two defaults: %v, %v; want the older, %q", found, err, earlier.Name)
	}

	for _, org := range append([]string{"", uuid.New().String()}, malformedIDs...) {
		if found, err := repo.FindDefaultTeam(org); found != nil || err != nil {
			t.Errorf("FindDefaultTeam(%q) = %v, %v; want nil, nil", org, found, err)
		}
	}
	if err := repo.MarkDefault(uuid.New().String()); err != nil {
		t.Errorf("MarkDefault of a crew no row has: %v, want no error", err)
	}
	rtWantRefused(t, "MarkDefault of a malformed id", repo.MarkDefault(malformed))
}

// ListTeams lists a workspace's crews by created_at alone: with a project,
// the project's own and the workspace-wide ones (no project); with an empty
// project, every crew of the workspace; with a project no row has, or a
// malformed one, the workspace-wide crews. Crews stamped the same instant
// tie, in no set order. An empty workspace id matches nothing, not even a
// crew with no workspace (a row from before workspaces, which SaveTeam no
// longer writes), and a list that matches nothing is nil; a malformed
// workspace id is Postgres's refusal.
func TestTeamRepositoryListTeams(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	orgID, otherOrg, project, otherProject := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()

	// Saved out of order: the order is the query's.
	for _, c := range []struct {
		name, org string
		project   *string
		at        int
	}{
		{"workspace-wide, later", orgID, nil, 3},
		{"the project's", orgID, &project, 1},
		{"another project's", orgID, &otherProject, 2},
		{"workspace-wide, earlier", orgID, nil, 0},
		{"tie, the project's", orgID, &project, 5},
		{"tie, workspace-wide", orgID, nil, 5},
		{"another workspace's", otherOrg, nil, 0},
	} {
		if err := repo.SaveTeam(&teams.Team{ID: uuid.New().String(), OrgID: c.org, Name: c.name, ProjectID: c.project,
			CreatedAt: rtAt(c.at), UpdatedAt: rtAt(c.at)}); err != nil {
			t.Fatalf("SaveTeam %q: %v", c.name, err)
		}
	}
	rtSeed(t, db, `INSERT INTO agent_teams (id, name, created_at, updated_at) VALUES ($1, 'no workspace''s', $2, $2)`,
		uuid.New().String(), rtAt(0))

	all, err := repo.ListTeams(orgID, "")
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	rtWantOrder(t, "the workspace's crews", rtTeamNames(all),
		[]string{"workspace-wide, earlier"}, []string{"the project's"}, []string{"another project's"},
		[]string{"workspace-wide, later"}, []string{"tie, the project's", "tie, workspace-wide"})

	mine, err := repo.ListTeams(orgID, project)
	if err != nil {
		t.Fatalf("ListTeams for a project: %v", err)
	}
	rtWantOrder(t, "a project's crews", rtTeamNames(mine),
		[]string{"workspace-wide, earlier"}, []string{"the project's"}, []string{"workspace-wide, later"},
		[]string{"tie, the project's", "tie, workspace-wide"})

	for _, p := range append([]string{uuid.New().String()}, malformedIDs...) {
		list, err := repo.ListTeams(orgID, p)
		if err != nil {
			t.Errorf("ListTeams for the project %q: %v", p, err)
			continue
		}
		rtWantOrder(t, fmt.Sprintf("the crews of the project %q, which no row has", p), rtTeamNames(list),
			[]string{"workspace-wide, earlier"}, []string{"workspace-wide, later"}, []string{"tie, workspace-wide"})
	}

	list, err := repo.ListTeams("", "")
	rtWantNil(t, "ListTeams with no workspace", list, err)
	list, err = repo.ListTeams(uuid.New().String(), "")
	rtWantNil(t, "ListTeams of a workspace with no crew", list, err)
	list, err = repo.ListTeams(malformed, "")
	if list != nil {
		t.Errorf("ListTeams of a malformed workspace listed %v", list)
	}
	rtWantRefused(t, "ListTeams of a malformed workspace", err)
}

// A node reads back as saved: an empty node type is stored as "agent", a
// human node carries its account's name and avatar from users, an agent node
// none and no user id, and a missing position is stored as {} and reads
// back as an empty map. UpdateNode rewrites type, agent, user, label,
// department and position, never the crew or created_at. Nodes list by
// created_at alone (a tie in no set order), a crew with none lists nil, and
// a node no row has, or a malformed id, reads as no node and no error (not
// teams.ErrNodeNotFound). An agent node with no agent, or naming an agent or
// crew no row has, is Postgres's refusal, as is a malformed id in any write.
// Deleting a node takes the edges that touch it and clears the entry node
// of the crew it was the entry node of, leaving the rest of the crew as it
// was (#379 bug 91: entry_node_id went on naming the node).
func TestTeamRepositoryNodeRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	orgID, agentID, otherAgent, userID := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedAgent(t, db, agentID, orgID, "drafter")
	rtSeedAgent(t, db, otherAgent, orgID, "reviewer")
	rtSeedUser(t, db, userID, "dana@example.com", "Dana Reyes", "https://example.com/dana.png")
	crew := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Crew", CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := repo.SaveTeam(crew); err != nil {
		t.Fatal(err)
	}

	agentNode := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: agentID, Label: "Drafter",
		Department: "Requirements", Position: map[string]interface{}{"org": map[string]interface{}{"x": 10.5, "y": -20.0}},
		CreatedAt: rtAt(3).In(rtCEST)}
	humanNode := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, NodeType: teams.NodeHuman, UserID: &userID,
		Label: "Approver", CreatedAt: rtAt(1)}
	for _, n := range []*teams.Node{agentNode, humanNode} {
		if err := repo.SaveNode(n); err != nil {
			t.Fatalf("SaveNode %q: %v", n.Label, err)
		}
	}

	got, err := repo.FindNodeByID(agentNode.ID)
	if err != nil || got == nil {
		t.Fatalf("FindNodeByID: %v, %v", got, err)
	}
	rtWantTimestamp(t, "a node's created_at, sent at +02:00", got.CreatedAt, agentNode.CreatedAt)
	want := rtNodeSansTime(agentNode)
	want.NodeType = teams.NodeAgent
	rtWantSame(t, "an agent node", rtNodeSansTime(got), want)

	got, err = repo.FindNodeByID(humanNode.ID)
	if err != nil || got == nil {
		t.Fatalf("FindNodeByID(human): %v, %v", got, err)
	}
	want = rtNodeSansTime(humanNode)
	want.UserName, want.UserAvatarURL, want.Position = "Dana Reyes", "https://example.com/dana.png", map[string]interface{}{}
	rtWantSame(t, "a human node", rtNodeSansTime(got), want)

	t.Run("update", func(t *testing.T) {
		changed := *got
		changed.TeamID = uuid.New().String() // not a column UpdateNode writes
		changed.NodeType, changed.AgentID, changed.UserID = teams.NodeAgent, otherAgent, nil
		changed.Label, changed.Department = "Reviewer", "Quality"
		changed.Position = nil
		changed.CreatedAt = rtAt(40) // nor is this
		if err := repo.UpdateNode(&changed); err != nil {
			t.Fatalf("UpdateNode: %v", err)
		}
		after, err := repo.FindNodeByID(humanNode.ID)
		if err != nil || after == nil {
			t.Fatalf("FindNodeByID after update: %v, %v", after, err)
		}
		rtWantTimestamp(t, "a node's created_at after an update", after.CreatedAt, humanNode.CreatedAt)
		want := rtNodeSansTime(&changed)
		want.TeamID, want.UserName, want.UserAvatarURL, want.Position = crew.ID, "", "", map[string]interface{}{}
		rtWantSame(t, "an updated node", rtNodeSansTime(after), want)

		if err := repo.UpdateNode(&teams.Node{ID: uuid.New().String(), AgentID: agentID, Label: "Ghost"}); err != nil {
			t.Errorf("UpdateNode of a node no row has: %v, want no error", err)
		}
		rtWantRefused(t, "UpdateNode of a malformed id", repo.UpdateNode(&teams.Node{ID: malformed, AgentID: agentID, Label: "x"}))
		// Back to a human, for the list below.
		changed.NodeType, changed.AgentID, changed.UserID = teams.NodeHuman, "", &userID
		if err := repo.UpdateNode(&changed); err != nil {
			t.Fatalf("UpdateNode back to a human: %v", err)
		}
	})

	t.Run("refused saves", func(t *testing.T) {
		rtWantPQ(t, "SaveNode of an agent node with no agent",
			repo.SaveNode(&teams.Node{ID: uuid.New().String(), TeamID: crew.ID, Label: "x"}), "23514", "chk_crew_node_identity")
		rtWantPQ(t, "SaveNode of an agent no row has",
			repo.SaveNode(&teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: uuid.New().String(), Label: "x"}),
			"23503", "agent_team_nodes_agent_id_fkey")
		rtWantPQ(t, "SaveNode on a crew no row has",
			repo.SaveNode(&teams.Node{ID: uuid.New().String(), TeamID: uuid.New().String(), AgentID: agentID, Label: "x"}),
			"23503", "agent_team_nodes_team_id_fkey")
		rtWantRefused(t, "SaveNode on a malformed crew id",
			repo.SaveNode(&teams.Node{ID: uuid.New().String(), TeamID: malformed, AgentID: agentID, Label: "x"}))
	})

	t.Run("list", func(t *testing.T) {
		tieA := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: agentID, Label: "tie a", CreatedAt: rtAt(7)}
		tieB := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: otherAgent, Label: "tie b", CreatedAt: rtAt(7)}
		for _, n := range []*teams.Node{tieB, tieA} {
			if err := repo.SaveNode(n); err != nil {
				t.Fatal(err)
			}
		}
		nodes, err := repo.ListNodesByTeam(crew.ID)
		if err != nil {
			t.Fatalf("ListNodesByTeam: %v", err)
		}
		var labels []string
		for _, n := range nodes {
			labels = append(labels, n.Label)
			if n.Label == "Reviewer" && (n.UserName != "Dana Reyes" || n.UserAvatarURL != "https://example.com/dana.png") {
				t.Errorf("a listed human node: %s, want the account's name and avatar", rtJSON(n))
			}
		}
		// The drafter was stamped two minutes after the reviewer, at +02:00:
		// its wall clock, two hours on, is what it sorts by.
		rtWantOrder(t, "a crew's nodes", labels, []string{"Reviewer"}, []string{"tie a", "tie b"}, []string{"Drafter"})

		empty := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Empty", CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.SaveTeam(empty); err != nil {
			t.Fatal(err)
		}
		list, err := repo.ListNodesByTeam(empty.ID)
		rtWantNil(t, "the nodes of a crew with none", list, err)
		list, err = repo.ListNodesByTeam(uuid.New().String())
		rtWantNil(t, "the nodes of a crew no row has", list, err)
		list, err = repo.ListNodesByTeam(malformed)
		if list != nil {
			t.Errorf("ListNodesByTeam of a malformed id listed %v", list)
		}
		rtWantRefused(t, "ListNodesByTeam of a malformed id", err)
	})

	t.Run("not found", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			if found, err := repo.FindNodeByID(id); found != nil || err != nil {
				t.Errorf("FindNodeByID(%q) = %v, %v; want nil, nil", id, found, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		entry := agentNode.ID
		crew.EntryNodeID = &entry
		crew.UpdatedAt = rtAt(9)
		if err := repo.UpdateTeam(crew); err != nil {
			t.Fatal(err)
		}
		edge := &teams.Edge{ID: uuid.New().String(), TeamID: crew.ID, FromNodeID: agentNode.ID, ToNodeID: humanNode.ID,
			EdgeType: teams.EdgeHandsOff, CreatedAt: rtAt(8)}
		if err := repo.SaveEdge(edge); err != nil {
			t.Fatal(err)
		}
		if err := repo.DeleteNode(agentNode.ID); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
		if found, err := repo.FindNodeByID(agentNode.ID); found != nil || err != nil {
			t.Errorf("FindNodeByID after DeleteNode: %v, %v; want nil, nil", found, err)
		}
		if found, err := repo.FindEdgeByID(edge.ID); found != nil || err != nil {
			t.Errorf("an edge from a deleted node: %v, %v; want it gone with the node", found, err)
		}
		if after, err := repo.FindTeamByID(crew.ID); err != nil || after == nil || after.EntryNodeID != nil {
			t.Errorf("the crew whose entry node was deleted: %v, %v; want no entry node", after, err)
		} else {
			rtWantTimestamp(t, "updated_at of the crew whose entry node was deleted", after.UpdatedAt, rtAt(9))
		}
		if err := repo.DeleteNode(agentNode.ID); err != nil {
			t.Errorf("DeleteNode of a node no row has: %v, want no error", err)
		}
		rtWantRefused(t, "DeleteNode of a malformed id", repo.DeleteNode(malformed))
	})
}

// An edge reads back as saved, a missing config stored as {} and read back
// as an empty map. UpdateEdge rewrites the type and config alone, never the
// crew, the nodes or created_at. Edges list by created_at alone (a tie in
// no set order), a crew with none lists nil, and an edge no row has, or a
// malformed id, reads as no edge and no error. An edge the crew has (same
// nodes and type), a node no row has and a malformed id are Postgres's
// refusal, and a node of another crew is the refusal a node no row has is
// (#379 bug 91: an edge between another crew's nodes was stored and
// listed). Deleting a crew takes its nodes and edges.
func TestTeamRepositoryEdgeRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	orgID, agentID := uuid.New().String(), uuid.New().String()
	rtSeedAgent(t, db, agentID, orgID, "drafter")
	newCrew := func(name string) *teams.Team {
		crew := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: name, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.SaveTeam(crew); err != nil {
			t.Fatal(err)
		}
		return crew
	}
	newNode := func(crew *teams.Team, label string) string {
		n := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: agentID, Label: label, CreatedAt: rtAt(0)}
		if err := repo.SaveNode(n); err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	crew, other := newCrew("Crew"), newCrew("Other")
	lead, writer, checker := newNode(crew, "lead"), newNode(crew, "writer"), newNode(crew, "checker")
	otherA, otherB := newNode(other, "a"), newNode(other, "b")

	delegates := &teams.Edge{ID: uuid.New().String(), TeamID: crew.ID, FromNodeID: lead, ToNodeID: writer,
		EdgeType: teams.EdgeDelegates, Config: map[string]interface{}{"max_depth": 2.0, "note": "first pass"},
		CreatedAt: rtAt(4).In(rtCEST)}
	reviews := &teams.Edge{ID: uuid.New().String(), TeamID: crew.ID, FromNodeID: checker, ToNodeID: writer,
		EdgeType: teams.EdgeReviews, CreatedAt: rtAt(2)}
	for _, e := range []*teams.Edge{delegates, reviews} {
		if err := repo.SaveEdge(e); err != nil {
			t.Fatalf("SaveEdge %s: %v", e.EdgeType, err)
		}
	}
	got, err := repo.FindEdgeByID(delegates.ID)
	if err != nil || got == nil {
		t.Fatalf("FindEdgeByID: %v, %v", got, err)
	}
	rtWantTimestamp(t, "an edge's created_at, sent at +02:00", got.CreatedAt, delegates.CreatedAt)
	rtWantSame(t, "an edge", rtEdgeSansTime(got), rtEdgeSansTime(delegates))
	if got, err := repo.FindEdgeByID(reviews.ID); err != nil || got == nil {
		t.Fatalf("FindEdgeByID: %v, %v", got, err)
	} else {
		want := rtEdgeSansTime(reviews)
		want.Config = map[string]interface{}{}
		rtWantSame(t, "an edge saved with no config", rtEdgeSansTime(got), want)
	}

	t.Run("update", func(t *testing.T) {
		changed := *got
		changed.TeamID, changed.FromNodeID, changed.ToNodeID = other.ID, otherA, otherB // none of them written
		changed.EdgeType, changed.Config, changed.CreatedAt = teams.EdgeHandsOff, nil, rtAt(30)
		if err := repo.UpdateEdge(&changed); err != nil {
			t.Fatalf("UpdateEdge: %v", err)
		}
		after, err := repo.FindEdgeByID(delegates.ID)
		if err != nil || after == nil {
			t.Fatalf("FindEdgeByID after update: %v, %v", after, err)
		}
		rtWantTimestamp(t, "an edge's created_at after an update", after.CreatedAt, delegates.CreatedAt)
		want := rtEdgeSansTime(delegates)
		want.EdgeType, want.Config = teams.EdgeHandsOff, map[string]interface{}{}
		rtWantSame(t, "an updated edge", rtEdgeSansTime(after), want)

		if err := repo.UpdateEdge(&teams.Edge{ID: uuid.New().String(), EdgeType: teams.EdgeReviews}); err != nil {
			t.Errorf("UpdateEdge of an edge no row has: %v, want no error", err)
		}
		rtWantRefused(t, "UpdateEdge of a malformed id", repo.UpdateEdge(&teams.Edge{ID: malformed, EdgeType: teams.EdgeReviews}))
	})

	t.Run("refused saves", func(t *testing.T) {
		rtWantPQ(t, "SaveEdge of an edge the crew has", repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID,
			FromNodeID: checker, ToNodeID: writer, EdgeType: teams.EdgeReviews}),
			"23505", "agent_team_edges_team_id_from_node_id_to_node_id_edge_type_key")
		rtWantPQ(t, "SaveEdge from a node no row has", repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID,
			FromNodeID: uuid.New().String(), ToNodeID: writer, EdgeType: teams.EdgeReviews}), "23503", "agent_team_edges_from_node_id_fkey")
		rtWantRefused(t, "SaveEdge from a malformed node id", repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID,
			FromNodeID: malformed, ToNodeID: writer, EdgeType: teams.EdgeReviews}))
		rtWantPQ(t, "SaveEdge from another crew's node", repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID,
			FromNodeID: otherA, ToNodeID: writer, EdgeType: teams.EdgeReviews}), "23503", "agent_team_edges_from_node_id_fkey")
		rtWantPQ(t, "SaveEdge to another crew's node", repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID,
			FromNodeID: lead, ToNodeID: otherB, EdgeType: teams.EdgeReviews}), "23503", "agent_team_edges_to_node_id_fkey")
		rtWantPQ(t, "SaveEdge between another crew's nodes", repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID,
			FromNodeID: otherA, ToNodeID: otherB, EdgeType: teams.EdgeDelegates}), "23503", "")
	})

	otherEdge := &teams.Edge{ID: uuid.New().String(), TeamID: other.ID, FromNodeID: otherA, ToNodeID: otherB,
		EdgeType: teams.EdgeDelegates, CreatedAt: rtAt(2)}
	if err := repo.SaveEdge(otherEdge); err != nil {
		t.Fatalf("SaveEdge between the other crew's own nodes: %v", err)
	}

	t.Run("list", func(t *testing.T) {
		edges, err := repo.ListEdgesByTeam(crew.ID)
		if err != nil {
			t.Fatalf("ListEdgesByTeam: %v", err)
		}
		var ids []string
		for _, e := range edges {
			ids = append(ids, e.ID)
		}
		rtWantOrder(t, "a crew's edges", ids, []string{reviews.ID}, []string{delegates.ID})

		empty := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Empty", CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.SaveTeam(empty); err != nil {
			t.Fatal(err)
		}
		list, err := repo.ListEdgesByTeam(empty.ID)
		rtWantNil(t, "the edges of a crew with none", list, err)
		list, err = repo.ListEdgesByTeam(uuid.New().String())
		rtWantNil(t, "the edges of a crew no row has", list, err)
		list, err = repo.ListEdgesByTeam(malformed)
		if list != nil {
			t.Errorf("ListEdgesByTeam of a malformed id listed %v", list)
		}
		rtWantRefused(t, "ListEdgesByTeam of a malformed id", err)
	})

	t.Run("not found", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			if found, err := repo.FindEdgeByID(id); found != nil || err != nil {
				t.Errorf("FindEdgeByID(%q) = %v, %v; want nil, nil", id, found, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := repo.DeleteEdge(reviews.ID); err != nil {
			t.Fatalf("DeleteEdge: %v", err)
		}
		if found, err := repo.FindEdgeByID(reviews.ID); found != nil || err != nil {
			t.Errorf("FindEdgeByID after DeleteEdge: %v, %v; want nil, nil", found, err)
		}
		if err := repo.DeleteEdge(reviews.ID); err != nil {
			t.Errorf("DeleteEdge of an edge no row has: %v, want no error", err)
		}
		rtWantRefused(t, "DeleteEdge of a malformed id", repo.DeleteEdge(malformed))

		if err := repo.DeleteTeam(other.ID); err != nil {
			t.Fatal(err)
		}
		if found, err := repo.FindEdgeByID(otherEdge.ID); found != nil || err != nil {
			t.Errorf("an edge of a deleted crew: %v, %v; want it gone with it", found, err)
		}
		if err := repo.DeleteTeam(crew.ID); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{lead, writer, checker, otherA} {
			if found, err := repo.FindNodeByID(id); found != nil || err != nil {
				t.Errorf("a node of a deleted crew: %v, %v; want it gone", found, err)
			}
		}
		if found, err := repo.FindEdgeByID(delegates.ID); found != nil || err != nil {
			t.Errorf("an edge of a deleted crew: %v, %v; want it gone", found, err)
		}
	})
}

// What a node's position and an edge's config read back as when the JSON
// stored is not an object, which no write of the repository stores: an
// array reads as an empty map, and JSON null as a nil map (null again in
// the API).
func TestTeamRepositoryReadsJSONThatIsNoObject(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	orgID, agentID := uuid.New().String(), uuid.New().String()
	rtSeedAgent(t, db, agentID, orgID, "drafter")
	crew := &teams.Team{ID: uuid.New().String(), OrgID: orgID, Name: "Crew", CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := repo.SaveTeam(crew); err != nil {
		t.Fatal(err)
	}
	from := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: agentID, Label: "from", CreatedAt: rtAt(0)}
	to := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: agentID, Label: "to", CreatedAt: rtAt(0)}
	for _, n := range []*teams.Node{from, to} {
		if err := repo.SaveNode(n); err != nil {
			t.Fatal(err)
		}
	}
	edge := &teams.Edge{ID: uuid.New().String(), TeamID: crew.ID, FromNodeID: from.ID, ToNodeID: to.ID,
		EdgeType: teams.EdgeDelegates, CreatedAt: rtAt(0)}
	if err := repo.SaveEdge(edge); err != nil {
		t.Fatal(err)
	}
	rtSeed(t, db, `UPDATE agent_team_nodes SET position = '[1, 2]' WHERE id = $1`, from.ID)
	rtSeed(t, db, `UPDATE agent_team_nodes SET position = 'null' WHERE id = $1`, to.ID)
	rtSeed(t, db, `UPDATE agent_team_edges SET config = '"text"' WHERE id = $1`, edge.ID)

	if got, err := repo.FindNodeByID(from.ID); err != nil || got == nil || got.Position == nil || len(got.Position) != 0 {
		t.Errorf("a position stored as an array: %v, %v; want an empty map", got, err)
	}
	if got, err := repo.FindNodeByID(to.ID); err != nil || got == nil || got.Position != nil {
		t.Errorf("a position stored as JSON null: %v, %v; want a nil map", got, err)
	}
	if got, err := repo.FindEdgeByID(edge.ID); err != nil || got == nil || got.Config == nil || len(got.Config) != 0 {
		t.Errorf("a config stored as a string: %v, %v; want an empty map", got, err)
	}
}

// A failure that is not the id's, here a database that fails every
// statement, is handed back as it came by every method: a single-row read
// answers no crew, node or edge with the error, never the no row and no
// error of an id no row has, and a list answers nil. A position or config
// JSON cannot encode fails the write with the encoder's error before any
// statement runs.
func TestTeamRepositoryHandsBackAFailure(t *testing.T) {
	repo := NewTeamRepository(rtClosedDB(t))
	id := uuid.New().String()
	rtWantClosed(t, "SaveTeam", repo.SaveTeam(&teams.Team{ID: id, Name: "x"}), "")
	rtWantClosed(t, "UpdateTeam", repo.UpdateTeam(&teams.Team{ID: id, Name: "x"}), "")
	rtWantClosed(t, "DeleteTeam", repo.DeleteTeam(id), "")
	rtWantClosed(t, "MarkDefault", repo.MarkDefault(id), "")
	rtWantClosed(t, "SaveNode", repo.SaveNode(&teams.Node{ID: id, TeamID: id, AgentID: id, Label: "x"}), "")
	rtWantClosed(t, "UpdateNode", repo.UpdateNode(&teams.Node{ID: id, AgentID: id, Label: "x"}), "")
	rtWantClosed(t, "DeleteNode", repo.DeleteNode(id), "")
	rtWantClosed(t, "SaveEdge", repo.SaveEdge(&teams.Edge{ID: id, TeamID: id, FromNodeID: id, ToNodeID: id,
		EdgeType: teams.EdgeDelegates}), "")
	rtWantClosed(t, "UpdateEdge", repo.UpdateEdge(&teams.Edge{ID: id, EdgeType: teams.EdgeDelegates}), "")
	rtWantClosed(t, "DeleteEdge", repo.DeleteEdge(id), "")

	team, err := repo.FindTeamByID(id)
	if team != nil {
		t.Errorf("FindTeamByID on a failing database read %v", team)
	}
	rtWantClosed(t, "FindTeamByID", err, "")
	team, err = repo.FindDefaultTeam(id)
	if team != nil {
		t.Errorf("FindDefaultTeam on a failing database read %v", team)
	}
	rtWantClosed(t, "FindDefaultTeam", err, "")
	node, err := repo.FindNodeByID(id)
	if node != nil {
		t.Errorf("FindNodeByID on a failing database read %v", node)
	}
	rtWantClosed(t, "FindNodeByID", err, "")
	edge, err := repo.FindEdgeByID(id)
	if edge != nil {
		t.Errorf("FindEdgeByID on a failing database read %v", edge)
	}
	rtWantClosed(t, "FindEdgeByID", err, "")

	crews, err := repo.ListTeams(id, "")
	if crews != nil {
		t.Errorf("ListTeams on a failing database listed %v", crews)
	}
	rtWantClosed(t, "ListTeams", err, "")
	nodes, err := repo.ListNodesByTeam(id)
	if nodes != nil {
		t.Errorf("ListNodesByTeam on a failing database listed %v", nodes)
	}
	rtWantClosed(t, "ListNodesByTeam", err, "")
	edges, err := repo.ListEdgesByTeam(id)
	if edges != nil {
		t.Errorf("ListEdgesByTeam on a failing database listed %v", edges)
	}
	rtWantClosed(t, "ListEdgesByTeam", err, "")

	rtWantUnencodable(t, "SaveNode", repo.SaveNode(&teams.Node{ID: id, TeamID: id, AgentID: id, Label: "x", Position: rtUnencodable}))
	rtWantUnencodable(t, "UpdateNode", repo.UpdateNode(&teams.Node{ID: id, AgentID: id, Label: "x", Position: rtUnencodable}))
	rtWantUnencodable(t, "SaveEdge", repo.SaveEdge(&teams.Edge{ID: id, TeamID: id, FromNodeID: id, ToNodeID: id,
		EdgeType: teams.EdgeDelegates, Config: rtUnencodable}))
	rtWantUnencodable(t, "UpdateEdge", repo.UpdateEdge(&teams.Edge{ID: id, EdgeType: teams.EdgeDelegates, Config: rtUnencodable}))
}
