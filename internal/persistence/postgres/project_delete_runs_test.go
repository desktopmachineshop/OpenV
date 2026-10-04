package postgres

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// What a project's delete leaves its agent runs (#379 bugs 146 and 149): no
// run waits on a review the delete made impossible, and no run names a row
// the delete removed. Postgres-gated (OPENV_TEST_DATABASE_URL).

// A run of the project awaiting approval is cancelled with the project's
// other unfinished runs, as a queued one is: its proposals go with the
// project, so no review could finalise it, and it waited in
// awaiting_approval for ever (#379 bug 146). It is answered among the
// cancelled runs for the caller to announce, and a review that comes after
// finds nothing to finalise. A run awaiting approval in another project
// stays as it was.
func TestDeletingAProjectCancelsItsRunsAwaitingApproval(t *testing.T) {
	db := rtDB(t)
	w := pdSeedWorkspace(t, db)
	runs := NewAgentRunRepository(db)
	doomedID, keptID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $3, 'Doomed'), ($2, $3, 'Kept')`, doomedID, keptID, w.org)
	awaiting := func(project string) string {
		t.Helper()
		id := uuid.New().String()
		// As Finish leaves it: finished, its worker's answer kept, its token
		// revoked, one proposal pending review.
		rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, status, worker_id, finished_at, final_text)
			VALUES ($1, $2, $3, $4, 'Draft', 'awaiting_approval', 'worker-1', NOW() - INTERVAL '1 hour', 'Proposed two requirements')`,
			id, w.agent, w.org, project)
		rtSeed(t, db, `INSERT INTO agent_proposals (id, run_id, project_id, op) VALUES ($1, $2, $3, 'create_artifact')`,
			uuid.New().String(), id, project)
		return id
	}
	doomed, kept := awaiting(doomedID), awaiting(keptID)

	removed, err := NewProjectRepository(db).Delete(doomedID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !reflect.DeepEqual(removed.CancelledRuns, []string{doomed}) {
		t.Errorf("Delete answered the cancelled runs %q, want the run awaiting approval %q", removed.CancelledRuns, doomed)
	}
	run, err := runs.FindByID(doomed)
	if err != nil || run == nil {
		t.Fatalf("the doomed project's run: %v, %v", run, err)
	}
	if run.Status != agentruns.StatusCancelled || !run.CancelRequested || run.FinishedAt == nil || run.RunTokenHash != "" {
		t.Errorf("the doomed project's run awaiting approval: %s, cancel requested %v, finished %v, token %q; want cancelled, requested, finished, none",
			run.Status, run.CancelRequested, run.FinishedAt, run.RunTokenHash)
	}
	if run.FinalText != "Proposed two requirements" || run.Error != "" || run.WorkerID != "worker-1" {
		t.Errorf("the cancelled run's answer %q, error %q, worker %q; want its answer and worker kept, no error",
			run.FinalText, run.Error, run.WorkerID)
	}
	if n, err := runs.CountPendingProposals(doomed); err != nil || n != 0 {
		t.Errorf("the cancelled run's pending proposals: %d (%v), want none", n, err)
	}

	// A review resolved after the delete has nothing left to finalise.
	svc := agentruns.NewDefaultService(runs, nil, nil)
	after, err := svc.FinalizeIfResolved(doomed)
	if err != nil || after.Status != agentruns.StatusCancelled {
		t.Errorf("FinalizeIfResolved after the delete = %+v, %v; want the run left cancelled", after, err)
	}

	other, err := runs.FindByID(kept)
	if err != nil || other == nil || other.Status != agentruns.StatusAwaitingApproval || other.CancelRequested {
		t.Errorf("another project's run awaiting approval after the delete: %+v, %v; want it awaiting approval as it was", other, err)
	}
}

// arRefs is what an agent run names besides its project: the six columns a
// project's delete may leave naming a row no longer there.
type arRefs struct {
	workItem, guided, interview, automation, team, teamNode sql.NullString
}

func arReadRefs(t *testing.T, db *sql.DB, runID string) arRefs {
	t.Helper()
	var r arRefs
	if err := db.QueryRow(`SELECT work_item_id::text, guided_session_id::text, interview_session_id::text, automation_id::text,
			team_id::text, team_node_id::text FROM agent_runs WHERE id = $1`, runID).
		Scan(&r.workItem, &r.guided, &r.interview, &r.automation, &r.team, &r.teamNode); err != nil {
		t.Fatalf("read the references of run %s: %v", runID, err)
	}
	return r
}

func arRef(id string) sql.NullString { return sql.NullString{String: id, Valid: id != ""} }

// A project's delete clears what its runs name among the rows it deletes:
// the card on its board, its guided session and interview session, its
// automation, its crew and the crew's node (#379 bug 149: a run kept naming
// them, and the board hook logged an ERROR "failed to move card" whenever
// the run changed status, as it does when the delete announces its cancel).
// A run of the project keeps what it names outside it, which stays: a
// workspace crew with its node, a whole-workspace automation, another
// project's card and sessions. A run of another project, naming that
// project's rows, keeps them all.
func TestDeletingAProjectClearsWhatItsRunsNameOfIt(t *testing.T) {
	db := rtDB(t)
	w := pdSeedWorkspace(t, db)
	doomedID, keptID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $3, 'Doomed'), ($2, $3, 'Kept')`, doomedID, keptID, w.org)
	n := func() string { return uuid.New().String() }

	// rows seeds a card, a guided session, an interview with a session, an
	// automation and a crew with a node in project, or the workspace's own
	// crew and automation when project is empty, and answers their ids.
	rows := func(project string) arRefs {
		t.Helper()
		r := arRefs{automation: arRef(n()), team: arRef(n()), teamNode: arRef(n())}
		rtSeed(t, db, `INSERT INTO automations (id, org_id, project_id, name, agent_id, kind) VALUES ($1, $2, NULLIF($3, '')::uuid, 'Nightly', $4, 'scheduled')`,
			r.automation.String, w.org, project, w.agent)
		rtSeed(t, db, `INSERT INTO agent_teams (id, org_id, project_id, name) VALUES ($1, $2, NULLIF($3, '')::uuid, 'Crew')`, r.team.String, w.org, project)
		rtSeed(t, db, `INSERT INTO agent_team_nodes (id, team_id, agent_id, label) VALUES ($1, $2, $3, 'Lead')`, r.teamNode.String, r.team.String, w.agent)
		if project == "" {
			return r
		}
		r.workItem, r.guided, r.interview = arRef(n()), arRef(n()), arRef(n())
		interview, invite := n(), n()
		rtSeed(t, db, `INSERT INTO work_items (id, project_id, title) VALUES ($1, $2, 'Trace it')`, r.workItem.String, project)
		rtSeed(t, db, `INSERT INTO guided_sessions (id, org_id, project_id) VALUES ($1, $2, $3)`, r.guided.String, w.org, project)
		rtSeed(t, db, `INSERT INTO interviews (id, project_id, name) VALUES ($1, $2, 'Users')`, interview, project)
		rtSeed(t, db, `INSERT INTO interview_invites (id, interview_id, token_hash) VALUES ($1, $2, $3)`, invite, interview, "hash-"+invite)
		rtSeed(t, db, `INSERT INTO interview_sessions (id, interview_id, invite_id) VALUES ($1, $2, $3)`, r.interview.String, interview, invite)
		return r
	}
	run := func(project string, r arRefs) string {
		t.Helper()
		id := n()
		rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, status, work_item_id, guided_session_id,
				interview_session_id, automation_id, team_id, team_node_id)
			VALUES ($1, $2, $3, $4, 'Work', 'running', $5, $6, $7, $8, $9, $10)`,
			id, w.agent, w.org, project, r.workItem, r.guided, r.interview, r.automation, r.team, r.teamNode)
		return id
	}
	doomedRows, keptRows, workspaceRows := rows(doomedID), rows(keptID), rows("")
	ownRun := run(doomedID, doomedRows)
	// A run of the project naming only rows outside it: the workspace's
	// crew and automation, and (as a launch naming another card can) the
	// other project's card and sessions.
	outside := keptRows
	outside.automation, outside.team, outside.teamNode = workspaceRows.automation, workspaceRows.team, workspaceRows.teamNode
	outsideRun := run(doomedID, outside)
	keptRun := run(keptID, keptRows)

	if _, err := NewProjectRepository(db).Delete(doomedID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	for _, c := range []struct {
		what, run string
		want      arRefs
	}{
		{"a run naming only the project's rows", ownRun, arRefs{}},
		{"a run of the project naming rows outside it", outsideRun, outside},
		{"another project's run", keptRun, keptRows},
	} {
		if got := arReadRefs(t, db, c.run); got != c.want {
			t.Errorf("%s names %+v after the delete, want %+v", c.what, got, c.want)
		}
	}
}
