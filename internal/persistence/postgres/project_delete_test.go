package postgres

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/projects"
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
		`INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size) VALUES ($1, $2, 'a.png', 'image/png', $3, 1)`,
		attachment, p.requirement, "/uploads/"+id+"/a.png")
	seed("the attachment's version", "attachment_versions", "attachment_id", attachment,
		`INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
			VALUES ($1, $2, 1, 'a.png', 'image/png', $3, 1)`, n(), attachment, "/uploads/"+id+"/a.png")
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
		`INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at) VALUES ($1, $2, 'log.txt', $3, NOW())`,
		n(), bundle, "/uploads/"+id+"/log.txt")
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

	removed, err := repo.Delete(doomedID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// What it answers for the caller to finish: the project's stored files,
	// each once, and its run, queued and so cancelled (#379 bugs 136, 137).
	if want := []string{"/uploads/" + doomedID + "/a.png", "/uploads/" + doomedID + "/log.txt"}; !reflect.DeepEqual(removed.Files, want) {
		t.Errorf("Delete answered the files %q, want %q", removed.Files, want)
	}
	if want := pdRowValues(doomed, "agent_runs"); !reflect.DeepEqual(removed.CancelledRuns, want) {
		t.Errorf("Delete answered the cancelled runs %q, want %q", removed.CancelledRuns, want)
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
// chatter, attachments and links, in place, answers nothing to remove, and
// cancels no run.
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

	removed, err := repo.Delete(id)
	if err == nil || !strings.Contains(err.Error(), "refused") || removed != nil {
		t.Fatalf("Delete with the project row's delete refused: %+v, %v; want the refusal and nothing removed", removed, err)
	}
	pdWant(t, db, "a project whose delete failed:", p.rows, before, false)
	// Its runs were not cancelled either: the cancel went with the rest.
	for _, r := range pdRowValues(p, "agent_runs") {
		var status string
		if err := db.QueryRow(`SELECT status FROM agent_runs WHERE id = $1`, r).Scan(&status); err != nil || status != "queued" {
			t.Errorf("a run of a project whose delete failed: %q (%v), want it still queued", status, err)
		}
	}
}

// pdRowValues are the values a project's seeded rows of one table were
// found by, in the order they were seeded.
func pdRowValues(p *pdProject, table string) []string {
	var values []string
	for _, r := range p.rows {
		if r.table == table {
			values = append(values, r.value)
		}
	}
	return values
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

// Deleting a project answers the stored file of every row it deleted that
// holds one, each path once and sorted, for the caller to remove once the
// delete has committed (#379 bug 136: the files stayed on disk): every
// version of every figure of every artifact the project ever had, a deleted
// artifact's and a restored version's included, a figure older than figure
// versions with no version row, and every file of every evidence bundle.
// Another project's files are not among them, and its rows stay.
func TestDeletingAProjectAnswersItsStoredFiles(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	w := pdSeedWorkspace(t, db)
	doomedID, keptID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $3, 'Doomed'), ($2, $3, 'Kept')`, doomedID, keptID, w.org)
	n := func() string { return uuid.New().String() }
	figure := func(artifact, current string, versions ...string) {
		t.Helper()
		id := n()
		rtSeed(t, db, `INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size, version)
			VALUES ($1, $2, 'f.png', 'image/png', $3, 1, $4)`, id, artifact, current, max(len(versions), 1))
		for i, path := range versions {
			rtSeed(t, db, `INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
				VALUES ($1, $2, $3, 'f.png', 'image/png', $4, 1)`, n(), id, i+1, path)
		}
	}
	bundle := func(project string, paths ...string) {
		t.Helper()
		id := n()
		rtSeed(t, db, `INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at)
			VALUES ($1, $2, $3, 'Evidence', NOW(), NOW())`, id, project, "EV-"+id[:8])
		for _, path := range paths {
			rtSeed(t, db, `INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at)
				VALUES ($1, $2, 'log.txt', $3, NOW())`, n(), id, path)
		}
	}
	live, gone, kept := n(), n(), n()
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, version, valid_to) VALUES ($1, $2, 'requirement', 'Live', 1, NOW())`, live, doomedID)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, version) VALUES ($1, $2, 'requirement', 'Live', 2)`, live, doomedID)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, valid_to) VALUES ($1, $2, 'requirement', 'Deleted', NOW())`, gone, doomedID)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'Kept')`, kept, keptID)
	// Three versions, the third restoring the first: its file is the first's.
	figure(live, "/d/fig1-v1", "/d/fig1-v1", "/d/fig1-v2", "/d/fig1-v1")
	figure(live, "/d/fig2") // from before figure versions: no version row
	figure(gone, "/d/fig3-v2", "/d/fig3-v1", "/d/fig3-v2")
	bundle(doomedID, "/d/ev1", "/d/ev2")
	bundle(doomedID, "/d/ev3")
	bundle(doomedID)
	figure(kept, "/k/fig", "/k/fig")
	bundle(keptID, "/k/ev")
	keptRows := []pdRow{{"figure", "attachments", "artifact_id", kept}, {"evidence", "evidence_bundles", "project_id", keptID}}
	keptBefore := pdCounts(t, db, keptRows)

	removed, err := repo.Delete(doomedID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	want := []string{"/d/ev1", "/d/ev2", "/d/ev3", "/d/fig1-v1", "/d/fig1-v2", "/d/fig2", "/d/fig3-v1", "/d/fig3-v2"}
	if !reflect.DeepEqual(removed.Files, want) {
		t.Errorf("Delete answered the files %q, want %q", removed.Files, want)
	}
	pdWant(t, db, "the other project's", keptRows, keptBefore, false)
	var left int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM attachment_versions) + (SELECT COUNT(*) FROM evidence_files)`).Scan(&left); err != nil || left != 2 {
		t.Errorf("figure versions and evidence files left: %d (%v), want the other project's 2", left, err)
	}
}

// Deleting a project cancels its live agent runs in the same transaction,
// as a cancel does, and revokes their tokens (#379 bug 137: they went on
// with no project, their token reaching nothing): a queued run is cancelled,
// with its token revoked; a claimed or running run is asked to stop, its
// worker kept, and its token revoked at once. A finished run, and one
// awaiting approval, stay as they were. Each answered is in Removed's
// CancelledRuns, and no run of another project, or of no project, is
// touched: the next claims take those, never the cancelled one.
func TestDeletingAProjectCancelsItsLiveRuns(t *testing.T) {
	db := rtDB(t)
	repo := NewProjectRepository(db)
	runs := NewAgentRunRepository(db)
	w := pdSeedWorkspace(t, db)
	doomedID, keptID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $3, 'Doomed'), ($2, $3, 'Kept')`, doomedID, keptID, w.org)

	type run struct {
		id, project, status, worker, token string
	}
	seeded := map[string]*run{}
	seed := func(name, project, status, worker string) *run {
		t.Helper()
		r := &run{id: uuid.New().String(), project: project, status: status, worker: worker, token: "hash-" + name}
		rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, status, worker_id, run_token_hash, heartbeat_at)
			VALUES ($1, $2, $3, NULLIF($4, '')::uuid, 'Work', $5, $6, $7, NOW())`, r.id, w.agent, w.org, project, status, worker, r.token)
		seeded[name] = r
		return r
	}
	queued := seed("queued", doomedID, "queued", "")
	claimed := seed("claimed", doomedID, "claimed", "worker-1")
	running := seed("running", doomedID, "running", "worker-2")
	for _, status := range []string{"succeeded", "failed", "cancelled", "timed_out", "awaiting_approval"} {
		seed(status, doomedID, status, "worker-3")
	}
	otherProject := seed("another project's", keptID, "queued", "")
	noProject := seed("no project's", "", "queued", "")

	removed, err := repo.Delete(doomedID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := append([]string(nil), removed.CancelledRuns...)
	sort.Strings(got)
	want := []string{queued.id, claimed.id, running.id}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Delete answered the cancelled runs %q, want the queued, claimed and running ones %q", got, want)
	}

	for name, r := range seeded {
		var status, worker, token string
		var cancelRequested, finished bool
		if err := db.QueryRow(`SELECT status, worker_id, run_token_hash, cancel_requested, finished_at IS NOT NULL FROM agent_runs WHERE id = $1`, r.id).
			Scan(&status, &worker, &token, &cancelRequested, &finished); err != nil {
			t.Fatalf("the %s run: %v", name, err)
		}
		wantStatus, wantToken, wantCancel, wantFinished := r.status, r.token, false, false
		switch r {
		case queued:
			wantStatus, wantToken, wantCancel, wantFinished = "cancelled", "", true, true
		case claimed, running:
			wantToken, wantCancel = "", true
		}
		if status != wantStatus || worker != r.worker || token != wantToken || cancelRequested != wantCancel || finished != wantFinished {
			t.Errorf("the %s run: status %s, worker %q, token %q, cancel requested %v, finished %v; want %s, %q, %q, %v, %v",
				name, status, worker, token, cancelRequested, finished, wantStatus, r.worker, wantToken, wantCancel, wantFinished)
		}
		found, err := runs.FindByTokenHash(r.token)
		if authenticates := found != nil; err != nil || authenticates != (r == otherProject || r == noProject) {
			t.Errorf("the %s run's token authenticates: %v (%v)", name, authenticates, err)
		}
	}

	var claimedNext []string
	for i := 0; i < 3; i++ {
		next, err := runs.Claim("worker-9", w.org, "", []string{"claude"}, 0, false)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if next != nil {
			claimedNext = append(claimedNext, next.ID)
		}
	}
	sort.Strings(claimedNext)
	wantNext := []string{otherProject.id, noProject.id}
	sort.Strings(wantNext)
	if !reflect.DeepEqual(claimedNext, wantNext) {
		t.Errorf("the claims after the delete took %q, want the other two queued runs %q", claimedNext, wantNext)
	}
}

// pdDeleteBehind starts deleting the project while tx, another
// transaction, holds a lock the delete needs, waits until the delete waits
// on it, commits tx, and answers what the delete answered.
func pdDeleteBehind(t *testing.T, db *sql.DB, tx *sql.Tx, id string) *projects.Removed {
	t.Helper()
	type answer struct {
		removed *projects.Removed
		err     error
	}
	done := make(chan answer, 1)
	go func() {
		removed, err := NewProjectRepository(db).Delete(id)
		done <- answer{removed, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case a := <-done:
			t.Fatalf("the delete did not wait for the other transaction: %+v, %v", a.removed, a.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the delete never waited for the other transaction")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	a := <-done
	if a.err != nil {
		t.Fatalf("Delete: %v", a.err)
	}
	return a.removed
}

// A run a worker claims while the delete reaches the project's runs is
// still asked to stop, its token revoked: the delete cancels the queued runs
// first, and finds the claimed one when it reaches the live ones. In the
// other order the run, queued when the live runs were read and claimed
// before the queued ones were, would escape both (#379 bug 137).
func TestDeletingAProjectStopsARunClaimedMeanwhile(t *testing.T) {
	db := rtDB(t)
	w := pdSeedWorkspace(t, db)
	projectID, runID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Doomed')`, projectID, w.org)
	rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, run_token_hash) VALUES ($1, $2, $3, $4, 'Work', 'hash')`,
		runID, w.agent, w.org, projectID)

	// A claim under way: it holds the queued run, as the claim's FOR UPDATE
	// does, and has made it claimed, but has not committed.
	claim, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Rollback()
	if _, err := claim.Exec(`UPDATE agent_runs SET status = 'claimed', worker_id = 'worker-1', heartbeat_at = NOW() WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	removed := pdDeleteBehind(t, db, claim, projectID)

	var status, token string
	var cancelRequested bool
	if err := db.QueryRow(`SELECT status, run_token_hash, cancel_requested FROM agent_runs WHERE id = $1`, runID).
		Scan(&status, &token, &cancelRequested); err != nil {
		t.Fatal(err)
	}
	if status != "claimed" || token != "" || !cancelRequested {
		t.Errorf("a run claimed while the delete ran: %s, token %q, cancel requested %v; want claimed, no token, asked to stop",
			status, token, cancelRequested)
	}
	if !reflect.DeepEqual(removed.CancelledRuns, []string{runID}) {
		t.Errorf("Delete answered the cancelled runs %q, want the claimed run", removed.CancelledRuns)
	}
}

// A run launched into the project just before its delete, still being
// written when the delete starts, is cancelled with the rest: its insert
// holds the project's row, which the delete waits for before it reads the
// project's runs.
func TestDeletingAProjectCancelsARunLaunchedMeanwhile(t *testing.T) {
	db := rtDB(t)
	w := pdSeedWorkspace(t, db)
	projectID, runID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Doomed')`, projectID, w.org)

	launch, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Rollback()
	if _, err := launch.Exec(`INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, run_token_hash) VALUES ($1, $2, $3, $4, 'Work', 'hash')`,
		runID, w.agent, w.org, projectID); err != nil {
		t.Fatal(err)
	}
	removed := pdDeleteBehind(t, db, launch, projectID)

	var status, token string
	if err := db.QueryRow(`SELECT status, run_token_hash FROM agent_runs WHERE id = $1`, runID).Scan(&status, &token); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || token != "" {
		t.Errorf("a run launched as the delete began: %s, token %q; want cancelled, no token", status, token)
	}
	if !reflect.DeepEqual(removed.CancelledRuns, []string{runID}) {
		t.Errorf("Delete answered the cancelled runs %q, want the launched run", removed.CancelledRuns)
	}
}

// A file added to one of the project's evidence bundles, or a new version of
// one of its figures, while the delete begins is answered with the rest: the
// delete locks the bundles before it reads their files, so it waits for the
// file being added, and a figure's new version holds the figure's row, which
// the delete reads its current file from once the version is in.
func TestDeletingAProjectAnswersAFileAddedMeanwhile(t *testing.T) {
	for _, c := range []struct {
		name, path string
		add        func(tx *sql.Tx, attachment, bundle string) error
	}{
		{"an evidence file", "/d/ev-new", func(tx *sql.Tx, _, bundle string) error {
			_, err := tx.Exec(`INSERT INTO evidence_files (id, bundle_id, filename, file_path, created_at)
				VALUES ($1, $2, 'new.log', '/d/ev-new', NOW())`, uuid.New().String(), bundle)
			return err
		}},
		{"a figure's version", "/d/fig-v2", func(tx *sql.Tx, attachment, _ string) error {
			if _, err := tx.Exec(`UPDATE attachments SET version = 2, file_path = '/d/fig-v2' WHERE id = $1`, attachment); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
				VALUES ($1, $2, 2, 'f.png', 'image/png', '/d/fig-v2', 1)`, uuid.New().String(), attachment)
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			db := rtDB(t)
			w := pdSeedWorkspace(t, db)
			projectID, artifact, attachment, bundle := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
			rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Doomed')`, projectID, w.org)
			rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R')`, artifact, projectID)
			rtSeed(t, db, `INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size) VALUES ($1, $2, 'f.png', 'image/png', '/d/fig-v1', 1)`,
				attachment, artifact)
			rtSeed(t, db, `INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
				VALUES ($1, $2, 1, 'f.png', 'image/png', '/d/fig-v1', 1)`, uuid.New().String(), attachment)
			rtSeed(t, db, `INSERT INTO evidence_bundles (id, project_id, ref, title, created_at, updated_at) VALUES ($1, $2, 'EV-1', 'E', NOW(), NOW())`,
				bundle, projectID)

			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := c.add(tx, attachment, bundle); err != nil {
				t.Fatal(err)
			}
			removed := pdDeleteBehind(t, db, tx, projectID)
			found := false
			for _, f := range removed.Files {
				found = found || f == c.path
			}
			if !found {
				t.Errorf("Delete answered the files %q, without %s added as it began, %s", removed.Files, c.name, c.path)
			}
		})
	}
}
