package postgres

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/automation"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/events"
)

// triggerAgents serves the run service's agent lookup from this package's
// AgentRepository (cmd/server wires agents' FileService, whose Get reads the
// same repository).
type triggerAgents struct {
	agents.Service
	repo *AgentRepository
}

func (a triggerAgents) Get(id string) (*agents.Agent, error) { return a.repo.FindByID(id) }

// TestTriggeredRunOnAWorkspaceEvent pins what a triggered automation does on
// an event with no project (issue #379, bug 84: the automations form now
// offers the workspace member events, which carry none). The real trigger
// matcher, subscribed to the real event bus, with the real run service over
// this package's repositories: org.member_added, published as
// publishOrgEvent publishes it (no project, the workspace stamped), fires an
// automation for the whole workspace, and its run launches with no project
// (agent_runs.project_id NULL; such a run is its workspace admins' alone, as
// AgentRunRepository.Claim and requireRunAccess have it), the event as its
// trigger, and last_run_at stamped. An automation pinned to a project never
// fires on it: the matcher's project scope compares the event's project,
// which is none.
func TestTriggeredRunOnAWorkspaceEvent(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	orgID, agentID, projectID := uuid.New().String(), uuid.New().String(), uuid.New().String()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Test Org', 'test-org')`, []any{orgID}},
		{`INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'greeter', 'Greeter', 'claude-code')`,
			[]any{agentID, orgID}},
		{`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Fuel pump')`, []any{projectID, orgID}},
	} {
		if _, err := db.Exec(stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	autoRepo := NewAutomationRepository(db)
	created := time.Now().Add(-time.Hour)
	save := func(name string, project *string) string {
		t.Helper()
		id := uuid.New().String()
		created = created.Add(time.Second) // the matcher takes them in creation order
		a := &automations.Automation{ID: id, OrgID: orgID, Name: name, AgentID: &agentID, ProjectID: project,
			Kind: automations.KindTriggered, Enabled: true, EventType: domainevents.OrgMemberAdded,
			PromptTemplate: "Welcome {{event.user_id}} as {{event.role}} ({{project.id}})",
			EventFilter:    map[string]interface{}{}, CooldownSeconds: 60, MaxRunsPerHour: 10,
			CreatedAt: created, UpdatedAt: created}
		if err := autoRepo.Save(a); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		return id
	}
	pinned := save("Pinned to the project", &projectID)
	workspace := save("For the whole workspace", nil)

	bus := events.NewBus(NewEventRepository(db), nil)
	runService := agentruns.NewDefaultService(NewAgentRunRepository(db), triggerAgents{repo: NewAgentRepository(db)}, bus)
	automation.NewTriggerMatcher(autoRepo, runService, nil).Start(bus)
	newcomer := uuid.New().String()
	e := domainevents.New(domainevents.OrgMemberAdded, "", newcomer, "user:"+uuid.New().String(),
		map[string]interface{}{"user_id": newcomer, "role": "member"}).WithOrg(orgID)
	bus.Publish(e)

	// The bus dispatches on a goroutine of its own; the matcher stamps the
	// workspace automation, which it takes last, after its launch.
	deadline := time.Now().Add(30 * time.Second)
	for {
		a, err := autoRepo.FindByID(workspace)
		if err != nil {
			t.Fatal(err)
		}
		if a.LastRunAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the workspace automation was not stamped: it launched no run on org.member_added")
		}
		time.Sleep(20 * time.Millisecond)
	}

	type runRow struct {
		org, status, prompt string
		project, trigger    sql.NullString
	}
	runsOf := func(automationID string) []runRow {
		t.Helper()
		rows, err := db.Query(`SELECT org_id, status, prompt, project_id, trigger_event_id FROM agent_runs
			WHERE automation_id = $1`, automationID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []runRow
		for rows.Next() {
			var r runRow
			if err := rows.Scan(&r.org, &r.status, &r.prompt, &r.project, &r.trigger); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := runsOf(workspace)
	if len(got) != 1 {
		t.Fatalf("the workspace automation launched %d runs, want one", len(got))
	}
	want := runRow{org: orgID, status: agentruns.StatusQueued, prompt: "Welcome " + newcomer + " as member ()",
		trigger: sql.NullString{String: e.ID, Valid: true}}
	if got[0] != want {
		t.Errorf("its run is %+v, want %+v: queued in the workspace with no project, the event as trigger", got[0], want)
	}
	if n := len(runsOf(pinned)); n != 0 {
		t.Errorf("the automation pinned to a project launched %d runs on an event with no project, want none", n)
	}
	if a, err := autoRepo.FindByID(pinned); err != nil || a.LastRunAt != nil {
		t.Errorf("the pinned automation: %v, last_run_at %v, want never stamped", err, a.LastRunAt)
	}
}
