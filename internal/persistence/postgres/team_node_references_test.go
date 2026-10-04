package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/teams"
)

// A crew points only at its own nodes (#379 bug 91, migration 0052).
// Postgres-gated (OPENV_TEST_DATABASE_URL).

// Migration 0052 on a database that holds what the old schema let through:
// a crew whose entry node is gone, one whose entry node is another crew's,
// and edges from, to and between another crew's nodes. It clears the two
// entry nodes and deletes the three edges, and keeps every other row as it
// was: a crew's own entry node, its own edges, and the updated_at of the
// crews it clears.
func TestMigration52ClearsWhatACrewCannotPointAt(t *testing.T) {
	db := testDB(t)
	var before []Migration
	for _, m := range migrations {
		if m.Version < 52 {
			before = append(before, m)
		}
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0051: %v", err)
	}

	org, agent := uuid.New().String(), uuid.New().String()
	crewA, crewB, crewGone := uuid.New().String(), uuid.New().String(), uuid.New().String()
	a1, a2, b1, b2 := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	keptA, keptB := uuid.New().String(), uuid.New().String()
	fromOther, toOther, betweenOther := uuid.New().String(), uuid.New().String(), uuid.New().String()
	seed := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	seed(`INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'lead', 'Lead', 'claude')`, agent, org)
	for _, c := range []string{crewA, crewB, crewGone} {
		seed(`INSERT INTO agent_teams (id, org_id, name, updated_at) VALUES ($1, $2, 'Crew', $3)`, c, org, rtAt(0))
	}
	for _, n := range []struct{ id, crew string }{{a1, crewA}, {a2, crewA}, {b1, crewB}, {b2, crewB}} {
		seed(`INSERT INTO agent_team_nodes (id, team_id, agent_id, label) VALUES ($1, $2, $3, 'Node')`, n.id, n.crew, agent)
	}
	seed(`UPDATE agent_teams SET entry_node_id = $2 WHERE id = $1`, crewA, a1)                     // its own: kept
	seed(`UPDATE agent_teams SET entry_node_id = $2 WHERE id = $1`, crewB, a1)                     // another crew's: cleared
	seed(`UPDATE agent_teams SET entry_node_id = $2 WHERE id = $1`, crewGone, uuid.New().String()) // gone: cleared
	for _, e := range []struct{ id, crew, from, to string }{
		{keptA, crewA, a1, a2},
		{keptB, crewB, b1, b2},
		{fromOther, crewA, b1, a2},
		{toOther, crewA, a1, b2},
		{betweenOther, crewA, b1, b2},
	} {
		seed(`INSERT INTO agent_team_edges (id, team_id, from_node_id, to_node_id, edge_type) VALUES ($1, $2, $3, $4, 'delegates-to')`,
			e.id, e.crew, e.from, e.to)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewTeamRepository(db)
	for _, c := range []struct {
		crew, entry, what string
	}{
		{crewA, a1, "a crew whose entry node is its own"},
		{crewB, "", "a crew whose entry node was another crew's"},
		{crewGone, "", "a crew whose entry node was gone"},
	} {
		got, err := repo.FindTeamByID(c.crew)
		if err != nil || got == nil {
			t.Fatalf("%s: %v, %v", c.what, got, err)
		}
		if entry := ""; got.EntryNodeID != nil {
			entry = *got.EntryNodeID
			if entry != c.entry {
				t.Errorf("%s has entry node %q, want %q", c.what, entry, c.entry)
			}
		} else if c.entry != "" {
			t.Errorf("%s has no entry node, want %q", c.what, c.entry)
		}
		rtWantTimestamp(t, "updated_at of "+c.what, got.UpdatedAt, rtAt(0))
	}
	for _, c := range []struct {
		id, what string
		kept     bool
	}{
		{keptA, "an edge between a crew's own nodes", true},
		{keptB, "another crew's edge between its own nodes", true},
		{fromOther, "an edge from another crew's node", false},
		{toOther, "an edge to another crew's node", false},
		{betweenOther, "an edge between another crew's nodes", false},
	} {
		if found, err := repo.FindEdgeByID(c.id); err != nil || (found != nil) != c.kept {
			t.Errorf("%s after the migration: %v, %v; want kept %v", c.what, found, err, c.kept)
		}
	}
}

// Whatever takes a crew's entry node clears it, and nothing else of the
// crew: deleting the node, and deleting the agent the node places. A crew
// whose entry node is set deletes as any other, its nodes and edges with it.
func TestACrewsEntryNodeGoesWithTheNode(t *testing.T) {
	db := rtDB(t)
	repo := NewTeamRepository(db)
	org, agentID, other := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedAgent(t, db, agentID, org, "lead")
	rtSeedAgent(t, db, other, org, "writer")

	newCrew := func(name string, agent string) (*teams.Team, *teams.Node) {
		t.Helper()
		crew := &teams.Team{ID: uuid.New().String(), OrgID: org, Name: name, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.SaveTeam(crew); err != nil {
			t.Fatal(err)
		}
		entry := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: agent, Label: "Entry", CreatedAt: rtAt(0)}
		writer := &teams.Node{ID: uuid.New().String(), TeamID: crew.ID, AgentID: other, Label: "Writer", CreatedAt: rtAt(0)}
		for _, n := range []*teams.Node{entry, writer} {
			if err := repo.SaveNode(n); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.SaveEdge(&teams.Edge{ID: uuid.New().String(), TeamID: crew.ID, FromNodeID: entry.ID, ToNodeID: writer.ID,
			EdgeType: teams.EdgeDelegates, CreatedAt: rtAt(0)}); err != nil {
			t.Fatal(err)
		}
		crew.EntryNodeID = &entry.ID
		if err := repo.UpdateTeam(crew); err != nil {
			t.Fatal(err)
		}
		return crew, entry
	}
	wantNoEntry := func(what string, crew *teams.Team) {
		t.Helper()
		got, err := repo.FindTeamByID(crew.ID)
		if err != nil || got == nil || got.EntryNodeID != nil {
			t.Errorf("%s: %v, %v; want the crew with no entry node", what, got, err)
			return
		}
		rtWantSame(t, what, rtTeamSansTimes(got), teams.Team{ID: crew.ID, OrgID: org, Name: crew.Name})
		rtWantTimestamp(t, what+", its updated_at", got.UpdatedAt, rtAt(0))
	}

	byNode, entry := newCrew("By node", agentID)
	if err := repo.DeleteNode(entry.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	wantNoEntry("a crew whose entry node was deleted", byNode)

	byAgent, _ := newCrew("By agent", agentID)
	if err := NewAgentRepository(db).Delete(agentID); err != nil {
		t.Fatalf("delete the agent: %v", err)
	}
	wantNoEntry("a crew whose entry node's agent was deleted", byAgent)

	whole, _ := newCrew("Whole", other)
	if err := repo.DeleteTeam(whole.ID); err != nil {
		t.Fatalf("DeleteTeam of a crew with an entry node: %v", err)
	}
	for _, table := range []string{"agent_team_nodes", "agent_team_edges"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE team_id = $1`, whole.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("a deleted crew's rows in %s: %d (%v), want none", table, n, err)
		}
	}
}
