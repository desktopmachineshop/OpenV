package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// What belongs to a project goes with it (#379 bug 86, migration 0053):
// ProjectRepository.Delete in one transaction, and the migration's clean-up
// of what earlier deletes left behind. Postgres-gated
// (OPENV_TEST_DATABASE_URL).

// pdRow is one seeded row, found again by one column's value.
type pdRow struct {
	what, table, column, value string
}

// pdProject is what pdSeedProject put in one project, row by row, and the
// ids a test links across projects.
type pdProject struct {
	id, requirement, testCase string
	rows                      []pdRow
}

// pdWorkspace is what several projects of one workspace share.
type pdWorkspace struct {
	org, user, agent, peopleTeam string
}

func pdSeedWorkspace(t *testing.T, db *sql.DB) pdWorkspace {
	t.Helper()
	w := pdWorkspace{org: uuid.New().String(), user: uuid.New().String(), agent: uuid.New().String(),
		peopleTeam: uuid.New().String()}
	rtSeedOrg(t, db, w.org)
	rtSeedUser(t, db, w.user, "dana-"+w.user+"@example.com", "Dana", "")
	rtSeed(t, db, `INSERT INTO org_teams (id, org_id, name) VALUES ($1, $2, 'Reviewers')`, w.peopleTeam, w.org)
	rtSeedAgent(t, db, w.agent, w.org, "lead-"+w.agent[:8])
	return w
}

// pdSeedProject puts one row of every kind a project holds in project id
// (and two versions of its requirement), recording each. With orphan set
// the project has no row, as one deleted before migration 0053 had none,
// and the tables that already had a foreign key to projects get nothing.
func pdSeedProject(t *testing.T, db *sql.DB, w pdWorkspace, id string, orphan bool) *pdProject {
	t.Helper()
	p := &pdProject{id: id, requirement: uuid.New().String(), testCase: uuid.New().String()}
	seed := func(what, table, column, value, query string, args ...interface{}) {
		t.Helper()
		rtSeed(t, db, query, args...)
		p.rows = append(p.rows, pdRow{what, table, column, value})
	}
	n := func() string { return uuid.New().String() }

	seed("the requirement's first version", "artifacts", "id", p.requirement,
		`INSERT INTO artifacts (id, project_id, type, title, version, valid_to) VALUES ($1, $2, 'requirement', 'Answer in time', 1, NOW())`,
		p.requirement, id)
	seed("the requirement's current version", "artifacts", "id", p.requirement,
		`INSERT INTO artifacts (id, project_id, type, title, version) VALUES ($1, $2, 'requirement', 'Answer in time', 2)`,
		p.requirement, id)
	seed("the test case", "artifacts", "id", p.testCase,
		`INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'test-case', 'Time the answer')`, p.testCase, id)
	seed("the reference counter", "artifact_ref_counters", "project_id", id,
		`INSERT INTO artifact_ref_counters (project_id, prefix, next_num) VALUES ($1, 'REQ', 2)`, id)
	seed("the chatter", "chatter", "artifact_id", p.requirement,
		`INSERT INTO chatter (id, artifact_id, message) VALUES ($1, $2, 'Looks right')`, n(), p.requirement)
	attachment := n()
	seed("the attachment", "attachments", "id", attachment,
		`INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size) VALUES ($1, $2, 'a.png', 'image/png', '/x/a.png', 1)`,
		attachment, p.requirement)
	seed("the attachment's version", "attachment_versions", "attachment_id", attachment,
		`INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
			VALUES ($1, $2, 1, 'a.png', 'image/png', '/x/a.png', 1)`, n(), attachment)
	seed("the figure counter", "attachment_figure_counters", "artifact_id", p.requirement,
		`INSERT INTO attachment_figure_counters (artifact_id, next_num) VALUES ($1, 2)`, p.requirement)
	link := n()
	seed("the link inside the project", "links", "id", link,
		`INSERT INTO links (id, from_id, to_id, type) VALUES ($1, $2, $3, 'verifies')`, link, p.testCase, p.requirement)
	seed("the inside link's version records", "link_artifacts", "link_id", link,
		`INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 2)`,
		link, p.testCase, p.requirement)
	if tableExists(t, db, "artifact_embeddings") {
		seed("the embedding", "artifact_embeddings", "artifact_id", p.requirement,
			`INSERT INTO artifact_embeddings (artifact_id, artifact_version, embedding, model, content_hash)
				VALUES ($1, 2, array_fill(0.1::real, ARRAY[1536])::vector, 'model', 'hash')`, p.requirement)
	}

	item := n()
	seed("the work item", "work_items", "id", item,
		`INSERT INTO work_items (id, project_id, title) VALUES ($1, $2, 'Trace it')`, item, id)
	seed("the work item's activity", "work_item_activity", "work_item_id", item,
		`INSERT INTO work_item_activity (id, work_item_id, kind) VALUES ($1, $2, 'comment')`, n(), item)
	crew, lead, reviewer := n(), n(), n()
	seed("the crew", "agent_teams", "id", crew,
		`INSERT INTO agent_teams (id, org_id, project_id, name) VALUES ($1, $2, $3, 'Crew')`, crew, w.org, id)
	seed("the crew's nodes", "agent_team_nodes", "team_id", crew,
		`INSERT INTO agent_team_nodes (id, team_id, agent_id, label) VALUES ($1, $3, $4, 'Lead'), ($2, $3, $4, 'Reviewer')`,
		lead, reviewer, crew, w.agent)
	seed("the crew's edge", "agent_team_edges", "team_id", crew,
		`INSERT INTO agent_team_edges (id, team_id, from_node_id, to_node_id, edge_type) VALUES ($1, $2, $3, $4, 'delegates-to')`,
		n(), crew, lead, reviewer)
	rtSeed(t, db, `UPDATE agent_teams SET entry_node_id = $2 WHERE id = $1`, crew, lead)
	testRun, result := n(), n()
	seed("the test run", "test_runs", "id", testRun,
		`INSERT INTO test_runs (id, project_id, name) VALUES ($1, $2, 'Run 1')`, testRun, id)
	seed("the test result", "test_results", "id", result,
		`INSERT INTO test_results (id, run_id, test_case_id, status) VALUES ($1, $2, $3, 'passed')`, result, testRun, p.testCase)
	interview, invite, session := n(), n(), n()
	seed("the interview", "interviews", "id", interview,
		`INSERT INTO interviews (id, project_id, name, persona_artifact_id) VALUES ($1, $2, 'Users', $3)`, interview, id, p.requirement)
	seed("the interview's invite", "interview_invites", "interview_id", interview,
		`INSERT INTO interview_invites (id, interview_id, token_hash) VALUES ($1, $2, $3)`, invite, interview, "hash-"+invite)
	seed("the interview's session", "interview_sessions", "interview_id", interview,
		`INSERT INTO interview_sessions (id, interview_id, invite_id) VALUES ($1, $2, $3)`, session, interview, invite)
	seed("the interview's message", "interview_messages", "session_id", session,
		`INSERT INTO interview_messages (id, session_id, role, content) VALUES ($1, $2, 'participant', 'Faster')`, n(), session)
	guided := n()
	seed("the guided session", "guided_sessions", "id", guided,
		`INSERT INTO guided_sessions (id, org_id, project_id) VALUES ($1, $2, $3)`, guided, w.org, id)
	seed("the guided session's message", "guided_session_messages", "session_id", guided,
		`INSERT INTO guided_session_messages (id, session_id, role, content) VALUES ($1, $2, 'user', 'Hello')`, n(), guided)
	definition := n()
	seed("the project's attribute definition", "attribute_definitions", "id", definition,
		`INSERT INTO attribute_definitions (id, org_id, project_id, key, data_type) VALUES ($1, $2, $3, 'risk', 'text')`,
		definition, w.org, id)
	automation := n()
	seed("the project's automation", "automations", "id", automation,
		`INSERT INTO automations (id, org_id, project_id, name, agent_id, kind) VALUES ($1, $2, $3, 'Nightly', $4, 'scheduled')`,
		automation, w.org, id, w.agent)
	run, proposal := n(), n()
	rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt) VALUES ($1, $2, $3, $4, 'Draft')`,
		run, w.agent, w.org, id)
	seed("the agent proposal", "agent_proposals", "id", proposal,
		`INSERT INTO agent_proposals (id, run_id, project_id, op) VALUES ($1, $2, $3, 'create_artifact')`, proposal, run, id)
	// The run and the event are kept whatever happens to the project; the
	// tests look at them separately.
	p.rows = append(p.rows, pdRow{"the agent run", "agent_runs", "id", run})
	event := n()
	rtSeed(t, db, `INSERT INTO domain_events (id, event_type, org_id, project_id) VALUES ($1, 'artifact.created', $2, $3)`,
		event, w.org, id)
	p.rows = append(p.rows, pdRow{"the activity log's event", "domain_events", "id", event})
	if orphan {
		return p
	}

	seed("the baseline", "baselines", "project_id", id,
		`INSERT INTO baselines (id, project_id, name, snapshot) VALUES ($1, $2, 'B1', '{}')`, n(), id)
	bundle := n()
	seed("the evidence bundle", "evidence_bundles", "id", bundle,
		`INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at) VALUES ($1, $2, 'EV-1', 'Evidence', NOW(), NOW())`,
		bundle, id)
	seed("the evidence citation", "evidence_citations", "bundle_id", bundle,
		`INSERT INTO evidence_citations (id, bundle_id, test_result_id, created_at) VALUES ($1, $2, $3, NOW())`, n(), bundle, result)
	seed("the evidence file", "evidence_files", "bundle_id", bundle,
		`INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at) VALUES ($1, $2, 'log.txt', '/x/log.txt', NOW())`,
		n(), bundle)
	seed("the product profile", "product_profiles", "project_id", id,
		`INSERT INTO product_profiles (project_id) VALUES ($1)`, id)
	connection := n()
	seed("the repository connection", "repo_connections", "id", connection,
		`INSERT INTO repo_connections (id, project_id, name) VALUES ($1, $2, 'Firmware')`, connection, id)
	seed("the repository's local path", "user_repo_paths", "repo_connection_id", connection,
		`INSERT INTO user_repo_paths (user_id, repo_connection_id, local_path) VALUES ($1, $2, '/src/firmware')`, w.user, connection)
	share := n()
	seed("the share link", "project_share_links", "id", share,
		`INSERT INTO project_share_links (id, project_id, token_hash, role) VALUES ($1, $2, $3, 'viewer')`, share, id, "hash-"+share)
	seed("the membership", "project_members", "project_id", id,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, id, w.user)
	seed("the team grant", "project_team_access", "project_id", id,
		`INSERT INTO project_team_access (project_id, org_team_id, role) VALUES ($1, $2, 'viewer')`, id, w.peopleTeam)
	return p
}

// pdLinkAcross links from one project's artifact to another's, with the
// link's version records on both ends, as a crossing refines or verifies
// link has, and returns the rows it seeded.
func pdLinkAcross(t *testing.T, db *sql.DB, what, from, to string) []pdRow {
	t.Helper()
	id := uuid.New().String()
	rtSeed(t, db, `INSERT INTO links (id, from_id, to_id, type) VALUES ($1, $2, $3, 'refines')`, id, from, to)
	rtSeed(t, db, `INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 1)`, id, from, to)
	return []pdRow{{what, "links", "id", id}, {what + "'s version records", "link_artifacts", "link_id", id}}
}

// pdCount counts the rows a seeded row was found by.
func pdCount(t *testing.T, db *sql.DB, r pdRow) int {
	t.Helper()
	var n int
	if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = $1`, r.table, r.column), r.value).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", r.what, err)
	}
	return n
}

// pdCounts reads every row's count, so a test can say what changed.
func pdCounts(t *testing.T, db *sql.DB, rows []pdRow) map[pdRow]int {
	t.Helper()
	counts := map[pdRow]int{}
	for _, r := range rows {
		counts[r] = pdCount(t, db, r)
	}
	return counts
}

// pdWant checks that each row is gone, or still there as it was.
func pdWant(t *testing.T, db *sql.DB, whose string, rows []pdRow, before map[pdRow]int, gone bool) {
	t.Helper()
	for _, r := range rows {
		got := pdCount(t, db, r)
		switch {
		case gone && got != 0:
			t.Errorf("%s %s (%s): %d rows left, want none", whose, r.what, r.table, got)
		case !gone && got != before[r]:
			t.Errorf("%s %s (%s): %d rows, want the %d it had", whose, r.what, r.table, got, before[r])
		}
	}
}

// pdOwned is a project's rows but its agent run and its event, which stay.
func pdOwned(p *pdProject) (owned, kept []pdRow) {
	for _, r := range p.rows {
		if r.table == "agent_runs" || r.table == "domain_events" {
			kept = append(kept, r)
		} else {
			owned = append(owned, r)
		}
	}
	return owned, kept
}

// pdWantRunAndEvent checks that the project's agent run is kept with no
// project, and its event as it was.
func pdWantRunAndEvent(t *testing.T, db *sql.DB, whose, projectID string, kept []pdRow) {
	t.Helper()
	for _, r := range kept {
		var project sql.NullString
		if err := db.QueryRow(fmt.Sprintf(`SELECT project_id::text FROM %s WHERE id = $1`, r.table), r.value).Scan(&project); err != nil {
			t.Errorf("%s %s: %v, want it kept", whose, r.what, err)
			continue
		}
		switch r.table {
		case "agent_runs":
			if project.Valid {
				t.Errorf("%s agent run names project %s, want it kept with none", whose, project.String)
			}
		case "domain_events":
			if project.String != projectID {
				t.Errorf("%s event names project %q, want it kept as it was, naming %s", whose, project.String, projectID)
			}
		}
	}
}

// pdWorkspaceRows seeds what a workspace holds outside any project: a crew,
// an automation, an attribute definition, a guided session and an agent
// run, none naming a project.
func pdWorkspaceRows(t *testing.T, db *sql.DB, w pdWorkspace) []pdRow {
	t.Helper()
	crew, automation, definition, guided, run := uuid.New().String(), uuid.New().String(), uuid.New().String(),
		uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO agent_teams (id, org_id, name) VALUES ($1, $2, 'Workspace crew')`, crew, w.org)
	rtSeed(t, db, `INSERT INTO automations (id, org_id, name, agent_id, kind) VALUES ($1, $2, 'Weekly', $3, 'scheduled')`,
		automation, w.org, w.agent)
	rtSeed(t, db, `INSERT INTO attribute_definitions (id, org_id, key, data_type) VALUES ($1, $2, 'cost', 'number')`, definition, w.org)
	rtSeed(t, db, `INSERT INTO guided_sessions (id, org_id) VALUES ($1, $2)`, guided, w.org)
	rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, prompt) VALUES ($1, $2, $3, 'Sweep')`, run, w.agent, w.org)
	return []pdRow{
		{"crew", "agent_teams", "id", crew},
		{"automation", "automations", "id", automation},
		{"attribute definition", "attribute_definitions", "id", definition},
		{"guided session", "guided_sessions", "id", guided},
		{"agent run", "agent_runs", "id", run},
	}
}

// Deleting a project deletes everything that belongs to it alone, in one
// transaction: every version of its artifacts with their chatter,
// attachments, figure counters, embeddings and links, both its own and those
// crossing to or from another project, with the links' version records; its
// work items with their activity, crews with their nodes and edges, test
// runs with their results, interviews, guided sessions, attribute
// definitions, automations and agent proposals; and, as before, its
// baselines, evidence, product profile, repository connections, share links,
// memberships and team grants. Its agent runs stay, for the workspace's
// usage, with no project, its activity log events stay as they are, and its
// child project stays, detached. Another project of the workspace loses only
// the links that crossed to the deleted one, and the workspace's own crews,
// automations, attribute definitions, guided sessions and runs stay
// (#379 bug 86: the work items, crews and artifacts, and all that hung off
// them, outlived the project).
func TestDeletingAProjectDeletesWhatBelongsToIt(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	w := pdSeedWorkspace(t, db)
	doomedID, keptID, childID := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $3, 'Doomed'), ($2, $3, 'Kept')`, doomedID, keptID, w.org)
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name, parent_project_id) VALUES ($1, $2, 'Child', $3)`, childID, w.org, doomedID)
	doomed := pdSeedProject(t, db, w, doomedID, false)
	kept := pdSeedProject(t, db, w, keptID, false)
	crossing := append(pdLinkAcross(t, db, "the link refining the other project's requirement", doomed.requirement, kept.requirement),
		pdLinkAcross(t, db, "the link from the other project's test case", kept.testCase, doomed.requirement)...)
	workspace := pdWorkspaceRows(t, db, w)
	keptBefore := pdCounts(t, db, append(append([]pdRow{}, kept.rows...), workspace...))

	if err := repo.Delete(doomedID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	owned, runAndEvent := pdOwned(doomed)
	pdWant(t, db, "the deleted project's", owned, nil, true)
	pdWantRunAndEvent(t, db, "the deleted project's", doomedID, runAndEvent)
	pdWant(t, db, "the deleted project's", crossing, nil, true)
	pdWant(t, db, "the other project's", kept.rows, keptBefore, false)
	pdWant(t, db, "the workspace's", workspace, keptBefore, false)
	if child, err := repo.GetByID(childID); err != nil || child.ParentProjectID != "" {
		t.Errorf("the deleted project's child: %+v, %v; want it kept, detached", child, err)
	}
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = $1`, doomedID).Scan(&left); err != nil || left != 0 {
		t.Errorf("the deleted project's row: %d (%v), want none", left, err)
	}
}

// The delete is one transaction: a failure at its last statement, the
// project row's, leaves every row it had already deleted, the artifacts'
// chatter, attachments and links, in place.
func TestDeletingAProjectIsAllOrNothing(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	w := pdSeedWorkspace(t, db)
	id := uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Stubborn')`, id, w.org)
	p := pdSeedProject(t, db, w, id, false)
	before := pdCounts(t, db, p.rows)
	rtSeed(t, db, `CREATE FUNCTION refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'refused'; END $$`)
	rtSeed(t, db, `CREATE TRIGGER refuse_delete BEFORE DELETE ON projects FOR EACH ROW EXECUTE FUNCTION refuse_delete()`)

	err := repo.Delete(id)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("Delete with the project row's delete refused: %v, want the refusal", err)
	}
	pdWant(t, db, "a project whose delete failed:", p.rows, before, false)
}

// Migration 0053 on a database holding what deletes before it left: the
// rows of a project no row has, each kind of them, and links between its
// artifacts and a live project's. It deletes exactly those rows, the
// crossing links and their version records included; keeps the orphaned
// agent run, its project cleared, and the activity log's event as it was;
// and leaves every row of the live project and of the workspace as it was.
// After it, a row naming a project no row has is the foreign key's refusal.
func TestMigration53DeletesWhatOutlivedItsProject(t *testing.T) {
	db := testDB(t)
	var before []Migration
	for _, m := range migrations {
		if m.Version < 53 {
			before = append(before, m)
		}
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0052: %v", err)
	}

	w := pdSeedWorkspace(t, db)
	liveID, goneID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Live')`, liveID, w.org)
	live := pdSeedProject(t, db, w, liveID, false)
	gone := pdSeedProject(t, db, w, goneID, true)
	crossing := append(pdLinkAcross(t, db, "the link from the live project", live.requirement, gone.requirement),
		pdLinkAcross(t, db, "the link to the live project", gone.testCase, live.requirement)...)
	workspace := pdWorkspaceRows(t, db, w)
	liveBefore := pdCounts(t, db, append(append([]pdRow{}, live.rows...), workspace...))

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	owned, runAndEvent := pdOwned(gone)
	pdWant(t, db, "the gone project's", owned, nil, true)
	pdWantRunAndEvent(t, db, "the gone project's", goneID, runAndEvent)
	pdWant(t, db, "the gone project's", crossing, nil, true)
	pdWant(t, db, "the live project's", live.rows, liveBefore, false)
	pdWant(t, db, "the workspace's", workspace, liveBefore, false)

	for _, c := range []struct{ table, insert string }{
		{"artifacts", `INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'x')`},
		{"work_items", `INSERT INTO work_items (id, project_id, title) VALUES ($1, $2, 'x')`},
		{"agent_teams", `INSERT INTO agent_teams (id, project_id, name) VALUES ($1, $2, 'x')`},
		{"test_runs", `INSERT INTO test_runs (id, project_id, name) VALUES ($1, $2, 'x')`},
	} {
		_, err := db.Exec(c.insert, uuid.New().String(), goneID)
		rtWantPQ(t, "a row in "+c.table+" for a project no row has", err, "23503", c.table+"_project_id_fkey")
	}
}
