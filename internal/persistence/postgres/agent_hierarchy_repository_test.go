package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
)

// The agent hierarchy (issue 733, migration 0070): an agent's parent, group
// and sort order read back as written, the parent's slug with them, and a
// deleted parent leaves its reports at the top (ON DELETE SET NULL).
func TestAgentHierarchyRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewAgentRepository(db)
	orgID := uuid.New().String()
	lead := &agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: "lead", Name: "Lead", Provider: "claude-code",
		CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := repo.Save(lead); err != nil {
		t.Fatal(err)
	}
	child := &agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: "child", Name: "Child", Provider: "claude-code",
		ParentAgentID: &lead.ID, GroupName: "Verification", SortOrder: 3, CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	if err := repo.Save(child); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindBySlug(orgID, "child")
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentAgentID == nil || *got.ParentAgentID != lead.ID || got.ReportsTo != "lead" ||
		got.GroupName != "Verification" || got.SortOrder != 3 {
		t.Fatalf("child read back with parent %v (%q), group %q, order %d", got.ParentAgentID, got.ReportsTo,
			got.GroupName, got.SortOrder)
	}
	if top, _ := repo.FindByID(lead.ID); top.ParentAgentID != nil || top.ReportsTo != "" || top.GroupName != "" {
		t.Errorf("a top agent read back with a place: %+v", top)
	}
	got.GroupName, got.ParentAgentID = "", nil
	if err := repo.Update(got); err != nil {
		t.Fatal(err)
	}
	var group *string
	if err := db.QueryRow(`SELECT group_name FROM agents WHERE id = $1`, child.ID).Scan(&group); err != nil || group != nil {
		t.Errorf("an empty group is stored as %v, %v; want NULL", group, err)
	}
	got.ParentAgentID = &lead.ID
	if err := repo.Update(got); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(lead.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := repo.FindByID(child.ID); after == nil || after.ParentAgentID != nil || after.ReportsTo != "" {
		t.Errorf("after its parent's delete the child reads %+v, want it at the top", after)
	}
}

// AgentStats counts, per agent, the open and succeeded runs created in the
// window and their cost, and the pending proposals of its runs however old,
// narrowed to a project's runs.
func TestAgentStats(t *testing.T) {
	db := rtDB(t)
	orgID, projectID, otherProject := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, orgID)
	rtSeedProject(t, db, projectID, orgID, "Stats")
	rtSeedProject(t, db, otherProject, orgID, "Elsewhere")
	agentRepo := NewAgentRepository(db)
	a1 := &agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: "a1", Name: "A1", Provider: "claude-code",
		CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	a2 := &agents.Agent{ID: uuid.New().String(), OrgID: orgID, Slug: "a2", Name: "A2", Provider: "claude-code",
		CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	for _, a := range []*agents.Agent{a1, a2} {
		if err := agentRepo.Save(a); err != nil {
			t.Fatal(err)
		}
	}
	runs := NewAgentRunRepository(db)
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -7)
	run := func(agentID, project, status string, created time.Time, cost float64) string {
		t.Helper()
		id := uuid.New().String()
		p := project
		r := &agentruns.Run{ID: id, OrgID: orgID, AgentID: agentID, ProjectID: &p, Status: status, Prompt: "x",
			CreatedAt: created}
		if err := runs.Save(r); err != nil {
			t.Fatalf("Save run: %v", err)
		}
		if cost > 0 {
			rtSeed(t, db, `UPDATE agent_runs SET cost_usd = $2 WHERE id = $1`, id, cost)
		}
		return id
	}
	run(a1.ID, projectID, agentruns.StatusRunning, now, 0)
	run(a1.ID, projectID, agentruns.StatusSucceeded, now, 1.5)
	run(a1.ID, projectID, agentruns.StatusSucceeded, now.AddDate(0, 0, -10), 9) // before the window
	old := run(a2.ID, projectID, agentruns.StatusAwaitingApproval, now.AddDate(0, 0, -30), 0)
	run(a2.ID, otherProject, agentruns.StatusQueued, now, 0) // another project
	for i := 0; i < 2; i++ {
		rtSeed(t, db, `INSERT INTO agent_proposals (id, run_id, project_id, op, status) VALUES ($1, $2, $3, 'create_artifact', 'pending')`,
			uuid.New().String(), old, projectID)
	}

	got, err := runs.AgentStats(orgID, projectID, since)
	if err != nil {
		t.Fatal(err)
	}
	byAgent := map[string]agentruns.AgentStats{}
	for _, s := range got {
		byAgent[s.AgentID] = s
	}
	if s := byAgent[a1.ID]; s.Open != 1 || s.Done != 1 || s.CostUSD != 1.5 || s.PendingProposals != 0 {
		t.Errorf("a1 = %+v, want 1 open, 1 done, $1.50, none pending", s)
	}
	if s := byAgent[a2.ID]; s.Open != 0 || s.Done != 0 || s.PendingProposals != 2 {
		t.Errorf("a2 = %+v, want only its old run's 2 pending proposals", s)
	}
	if all, err := runs.AgentStats(orgID, "", since); err != nil || len(all) != 2 {
		t.Errorf("the whole workspace: %+v, %v", all, err)
	}
	if none, err := runs.AgentStats(orgID, "not-a-uuid", since); err != nil || len(none) != 0 {
		t.Errorf("a malformed project: %+v, %v; want none", none, err)
	}
}
