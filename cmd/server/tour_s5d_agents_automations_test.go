//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTourS5dAgentsAutomations is the S5d tour's area for agent definitions
// and automations (refactor plan §6.4 S5d, before M3 and M7, which split
// agent_handlers.go; invariants I3, I4, I5, I10; quirks Q1, Q2, Q14, Q19;
// OpenV REQ-21, REQ-91, REQ-143). Its golden is
// testdata/tour/s5d/agents_automations.json.
//
// No run reaches a model: the area launches runs only through run-now, and
// nothing claims them; the golden's empty outbound_requests pins that the
// server makes no provider call of its own. It walks, in the owner's shared
// workspace W with a plain member m that edits W's project P, an outsider
// with a workspace of its own, and a second shared workspace X:
//   - the agents: W's eight seeds in name order, each with its file under
//     <tmp>, the file's hash and when it was synced (their prompts are in the
//     golden, so a seed's change changes it); create with its defaults
//     (write_mode proposal, 50 turns, 1,800 s) and its refusals (the admin
//     guard before the body, a body that does not decode, a slug taken, a slug
//     that is no slug, no allowed_tools or only blank ones (REQ-91), a
//     write_mode or an effort no agent has, repository access on a provider
//     that cannot confine it); a name with & written \u0026 (I4); the read, by
//     any member; the raw markdown file and its save (a file whose
//     frontmatter names another slug, one with no frontmatter, YAML that does
//     not parse, no allowed_tools, and a save to a slug no agent has, which
//     creates it with 200); the form's update (a body slug that differs, an
//     empty one, which takes the URL's, no allowed_tools, and a slug no agent
//     has, which creates it with 200);
//   - the delete and what it cascades (ON DELETE CASCADE on agent_runs,
//     automations and agent_team_nodes): the agent's queued run and its
//     automation are gone (404), its crew node is gone from the crew's
//     graph, whose entry_node_id still names it (no foreign key), so an
//     automation of that crew answers "team has no entry node"; the run's
//     tracking card stays on P's board; a slug no agent has answers 500
//     (agents.ErrNotFound is unmapped, a Q2-style answer);
//   - the sync, in X once the area deleted X's seeds: X's agents are null
//     (Q14), the seeds stay deleted since their files went to X's .trash, a
//     file written on disk with no allowed_tools is backfilled
//     (mcp__openv__*) and rewritten, a file with no frontmatter makes the
//     sync answer 400 with every error, though the other files synced all the
//     same, and the member's sync is refused;
//   - the automations: create (a manual one's defaults: enabled, 60 s
//     cooldown, 10 runs an hour, event_filter {}; a scheduled one's
//     next_run_at, a cron that never falls due while the area runs, since the
//     scheduler ticks every 30 s; created_by the caller, whatever the body
//     says (fixed under R7); a triggered one only disabled, since the
//     matcher launches runs from the bus's goroutine) and its refusals in
//     order (a body that does not decode, the guard: the member only pinned to
//     P, the outsider nowhere; then the service: no name, a kind no
//     automation has, not exactly one target, a cron robfig does not parse,
//     with its text (Q19), no event type); a target agent no row has (the
//     foreign key's text, Q19) and a crew no row has (stored as sent); the
//     list, newest first, filtered to P, and null when none match (Q14), which
//     the member reads whole; the read's guard (the outsider) and a phantom
//     or non-UUID id (404); the update of next_run_at (disabled, enabled,
//     another cron) and its refusals; run-now's copy (the template rendered,
//     an unknown placeholder empty; an empty template and one of unknown
//     placeholders only fall back to "Manual run of automation: <name>",
//     but one of two placeholders and a space does not, and the run's prompt is
//     that space; a crew's entry node's agent, with team_id and team_node_id;
//     a crew with no entry node, and one no row has; a disabled automation
//     runs all the same) and the tracking card a run in P gets
//     (workitem.created, actor agent:<run>, the launches' only events); and
//     delete (the run it launched keeps its automation_id);
//   - the triggered firings: an enabled automation of W for all of W on
//     artifact.created does not fire on an artifact the outsider creates in
//     its own workspace, since the matcher compares the event's workspace
//     with the automation's (fixed under R7: it compared none, and W's run
//     was scoped to the outsider's project with the outsider's title in its
//     prompt); it fires on an artifact in P, and so does an automation pinned
//     to P, each run scoped to P with its tracking card there. The firings
//     are asynchronous (the bus dispatches from a goroutine, one event at a
//     time), so the area creates both artifacts as setup and awaits the
//     last_run_at of the automation pinned to P, which the matcher stamps
//     after the launch and its tracking card, and after the automation for
//     all of W, which it takes first (agentsAutomationsTrigger).
//
// Every 2xx JSON answer here is a bare encode (text/plain by sniffing, no
// Content-Type once gzipped, Q1); errors are application/json.
// Nondeterminism: none is left to chance. The cron 0 0 1 1 * falls due only
// at a new year, and next_run_at, a time to come, is <time> with its month,
// day and hour noted; the triggered run's times are all minted before the
// step that reads them. Not pinned: a scheduled firing (the scheduler's
// first tick is 30 s after boot, and a cron that falls due within the area
// would race its steps), the matcher's hourly cap and event filter, and a
// provider or Docker, which no route of the area calls.
func TestTourS5dAgentsAutomations(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "agents_automations",
		about: "Agent definitions and automations: the seeded agents, create, read, the raw file, update and delete " +
			"with what it cascades, the sync from disk and its errors, the automations' create, list, read, update " +
			"and delete, run-now's prompt copy and crew targets, and one triggered firing across workspaces.",
		run: agentsAutomationsTour,
		accounts: []tourAccount{
			{name: "member", display: "Tour Member", about: "a plain member of W and an editor of its project P: " +
				"refused the agents' writes, and allowed the automations pinned to P"},
			{name: "outsider", display: "Tour Outsider", about: "an account with a workspace of its own and no place " +
				"in W: refused W's automations, and the author of the artifact that fires W's triggered automation"},
		},
	})
}

// agentsAutomationsTools is the allowlist the area's agents carry (REQ-91).
const agentsAutomationsTools = `["mcp__openv__get_artifact"]`

// agentsAutomationsAgent is a definition for POST /api/v1/agents with a
// short prompt, so that the golden holds the area's own agents rather than a
// seed's prompt, beyond the one list of W's seeds.
func agentsAutomationsAgent(slug, name string) string {
	return `{"slug":"` + slug + `","name":"` + name + `","description":"An agent of the S5d tour.","provider":` +
		`"claude-code","allowed_tools":` + agentsAutomationsTools + `,"system_prompt":"You answer in one line."}`
}

// agentsAutomationsRaw is the body of PUT /api/v1/agents/{slug}/raw: the
// markdown file as a JSON string.
func agentsAutomationsRaw(content string) tourOpt {
	return literalBody(map[string]string{"content": content})
}

// agentsAutomationsBody is a JSON body from a map, with each string value
// that is a registered name's {{name}} filled in but prompt_template, whose
// own {{placeholders}} jsonBody would try to fill (literalBody).
func agentsAutomationsBody(tr *tour, fields map[string]any) tourOpt {
	tr.t.Helper()
	out := map[string]any{}
	for k, v := range fields {
		if s, ok := v.(string); ok && k != "prompt_template" {
			v = tr.fill(s)
		}
		out[k] = v
	}
	return literalBody(out)
}

// agentsAutomationsWrite writes a file into a workspace's agents directory,
// the fixture a sync reads.
func agentsAutomationsWrite(tr *tour, workspace, name, content string) {
	tr.t.Helper()
	if err := os.WriteFile(filepath.Join(tr.agentsDir(workspace), name), []byte(content), 0o644); err != nil {
		tr.t.Fatalf("write the agent file %s: %v", name, err)
	}
}

// agentsAutomationsNoteNext notes where a scheduled automation's next_run_at
// falls: a time to come, written <time>, whose month, day and hour are what
// the cron chose, and which falls within the coming year.
func agentsAutomationsNoteNext(r *tourResult) {
	r.tr.t.Helper()
	v, err := jsonValue(r.body, "/next_run_at")
	if err != nil {
		r.tr.t.Fatalf("read next_run_at of %s: %v\n%s", r.what(), err, r.body)
	}
	s, _ := v.(string)
	next, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		r.tr.t.Fatalf("next_run_at of %s: %v", r.what(), err)
	}
	now := time.Now()
	within := "within the coming year"
	if !next.After(now) || next.After(now.AddDate(1, 0, 0)) {
		within = "NOT within the coming year"
	}
	r.note(fmt.Sprintf("next_run_at is %s %d at %02d:%02d:%02d UTC, %s", next.UTC().Month(), next.UTC().Day(),
		next.UTC().Hour(), next.UTC().Minute(), next.UTC().Second(), within))
}

func agentsAutomationsTour(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	tr.sharedWorkspace("x", "Tour Emptied")
	tr.sharedWorkspace("w", "Tour Shared")
	tr.join(m, "{{w}}", "member")
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Agents"}`)).capture("p", "/id")
	tr.setup("the member edits P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-member@example.com","role":"editor"}`))

	agentsAutomationsAgents(tr)
	agentsAutomationsDelete(tr)
	agentsAutomationsSync(tr)
	agentsAutomationsAutomations(tr)
	agentsAutomationsTrigger(tr)
}

const (
	agentsAutomationsList   = "GET /api/v1/agents"
	agentsAutomationsCreate = "POST /api/v1/agents"
	agentsAutomationsSyncs  = "POST /api/v1/agents/sync"
	agentsAutomationsGet    = "GET /api/v1/agents/{slug}"
	agentsAutomationsPut    = "PUT /api/v1/agents/{slug}"
	agentsAutomationsDel    = "DELETE /api/v1/agents/{slug}"
	agentsAutomationsRawGet = "GET /api/v1/agents/{slug}/raw"
	agentsAutomationsRawPut = "PUT /api/v1/agents/{slug}/raw"
)

// agentsAutomationsAgents walks the agent definitions: list, create, read,
// the raw file and the form's update.
func agentsAutomationsAgents(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	slug := func(s string) tourOpt { return at("slug", s) }

	// (a) The seeds.
	tr.step("W's agents: the eight seeded when W was created, in name order, each with its file under <tmp>, the "+
		"file's hash and when it was synced", o, agentsAutomationsList,
		note("V&V is written V\\u0026V: the encoder escapes HTML (I4); the seeds' prompts are in this answer, so a "+
			"change to a seed changes this golden"))

	// (b) Create.
	tr.step("the member creates an agent: the admin guard, before the body is decoded", m, agentsAutomationsCreate,
		jsonBody(`{`))
	tr.step("create with a body that does not decode", o, agentsAutomationsCreate, jsonBody(`{`))
	tr.step("create tour-auto with no write_mode, turns or timeout: 201, with the defaults (proposal, 50 turns, "+
		"1,800 s), & written \\u0026", o, agentsAutomationsCreate,
		jsonBody(`{"slug":"tour-auto","name":"Tour Auto & Co","description":"The automations' agent.",`+
			`"provider":"claude-code","allowed_tools":`+agentsAutomationsTools+`,"system_prompt":"You answer in one line."}`),
		note("content_hash is the SHA-256 of the markdown file the server wrote, file_path that file")).
		capture("auto.agent", "/id")
	tr.step("create tour-auto again: 409", o, agentsAutomationsCreate,
		jsonBody(agentsAutomationsAgent("tour-auto", "Tour Auto Again")))
	tr.step("create a slug with a capital and an underscore: 400", o, agentsAutomationsCreate,
		jsonBody(agentsAutomationsAgent("Tour_Auto", "Tour Bad Slug")))
	tr.step("create with no allowed_tools: 400, REQ-91's wording", o, agentsAutomationsCreate,
		jsonBody(`{"slug":"tour-open","name":"Tour Open","provider":"claude-code","system_prompt":"x"}`))
	tr.step("create with only a blank tool: 400, a blank entry is no tool", o, agentsAutomationsCreate,
		jsonBody(`{"slug":"tour-open","name":"Tour Open","provider":"claude-code","allowed_tools":[" "],`+
			`"system_prompt":"x"}`))
	tr.step("create with a write_mode no agent has: 400", o, agentsAutomationsCreate,
		jsonBody(`{"slug":"tour-open","name":"Tour Open","provider":"claude-code","allowed_tools":`+
			agentsAutomationsTools+`,"write_mode":"auto","system_prompt":"x"}`))
	tr.step("create with an effort no provider has: 400", o, agentsAutomationsCreate,
		jsonBody(`{"slug":"tour-open","name":"Tour Open","provider":"claude-code","allowed_tools":`+
			agentsAutomationsTools+`,"effort":"extreme","system_prompt":"x"}`))
	tr.step("create with repository access on codex-cli: 400, only claude-code confines edits per tool", o,
		agentsAutomationsCreate, jsonBody(`{"slug":"tour-open","name":"Tour Open","provider":"codex-cli",`+
			`"allowed_tools":`+agentsAutomationsTools+`,"repo_access":true,"system_prompt":"x"}`))

	// (c) Read.
	tr.step("tour-auto", o, agentsAutomationsGet, slug("tour-auto"))
	tr.step("the member reads tour-auto: any member reads an agent", m, agentsAutomationsGet, slug("tour-auto"))
	tr.step("an agent W does not have: 404", o, agentsAutomationsGet, slug("no-such-agent"))

	// (d) The raw file.
	tr.step("tour-auto's file: its YAML frontmatter, then the prompt", o, agentsAutomationsRawGet, slug("tour-auto"))
	tr.step("the file of an agent W does not have: 404", o, agentsAutomationsRawGet, slug("no-such-agent"))
	tr.step("the file of a slug that is no slug: 404, the path is refused before any read", o,
		agentsAutomationsRawGet, slug("Not_A_Slug"))
	const front = "---\nslug: tour-auto\nname: Tour Auto & Co\ndescription: Saved as a file.\nprovider: claude-code\n" +
		"effort: high\nallowed_tools:\n    - mcp__openv__get_artifact\n---\nYou answer in one line, from the file.\n"
	tr.step("the member saves tour-auto's file: the admin guard", m, agentsAutomationsRawPut, slug("tour-auto"),
		agentsAutomationsRaw(front))
	tr.step("save tour-auto's file with a body that does not decode", o, agentsAutomationsRawPut, slug("tour-auto"),
		jsonBody(`{`))
	tr.step("save tour-auto's file: 200, the agent as synced from it (a new hash, effort high, the default "+
		"write_mode)", o, agentsAutomationsRawPut, slug("tour-auto"), agentsAutomationsRaw(front))
	tr.step("save a file whose frontmatter names tour-other at tour-auto: 400", o, agentsAutomationsRawPut,
		slug("tour-auto"), agentsAutomationsRaw("---\nslug: tour-other\nname: Tour Other\nprovider: claude-code\n"+
			"allowed_tools:\n    - mcp__openv__get_artifact\n---\nx\n"))
	tr.step("save a file with no frontmatter: 400", o, agentsAutomationsRawPut, slug("tour-auto"),
		agentsAutomationsRaw("You answer in one line.\n"))
	tr.step("save a file whose frontmatter is not YAML: 400, the YAML parser's text", o, agentsAutomationsRawPut,
		slug("tour-auto"), agentsAutomationsRaw("---\nslug: [tour-auto\n---\nx\n"))
	tr.step("save a file with no allowed_tools: 400, a save is validated in full (REQ-91)", o,
		agentsAutomationsRawPut, slug("tour-auto"), agentsAutomationsRaw("---\nslug: tour-auto\nname: Tour Auto\n"+
			"provider: claude-code\n---\nx\n"))
	tr.step("save the file of tour-raw, which W does not have: 200, not 201, and the agent exists", o,
		agentsAutomationsRawPut, slug("tour-raw"), agentsAutomationsRaw("---\nslug: tour-raw\nname: Tour Raw\n"+
			"provider: claude-code\nallowed_tools:\n    - mcp__openv__get_artifact\n---\nMade from a file.\n"))

	// (e) The form's update.
	tr.step("the member updates tour-auto: the admin guard, before the body is decoded", m, agentsAutomationsPut,
		slug("tour-auto"), jsonBody(`{`))
	tr.step("update tour-auto with a body that does not decode", o, agentsAutomationsPut, slug("tour-auto"),
		jsonBody(`{`))
	tr.step("update tour-auto with the slug tour-other in the body: 400", o, agentsAutomationsPut, slug("tour-auto"),
		jsonBody(agentsAutomationsAgent("tour-other", "Tour Other")))
	tr.step("update tour-auto with no slug in the body: it takes the URL's; 200, write_mode direct, the file "+
		"rewritten", o, agentsAutomationsPut, slug("tour-auto"),
		jsonBody(`{"name":"Tour Auto & Co","description":"Edited in the form.","provider":"claude-code",`+
			`"allowed_tools":`+agentsAutomationsTools+`,"write_mode":"direct","system_prompt":"You answer in one line."}`))
	tr.step("update tour-auto with no allowed_tools: 400, an update may not take the allowlist away (REQ-91)", o,
		agentsAutomationsPut, slug("tour-auto"), jsonBody(`{"name":"Tour Auto","provider":"claude-code",`+
			`"system_prompt":"x"}`))
	tr.step("update tour-put, which W does not have: 200, and the agent exists", o, agentsAutomationsPut,
		slug("tour-put"), jsonBody(agentsAutomationsAgent("", "Tour Put")))
}

// agentsAutomationsDelete deletes an agent that has a queued run, an
// automation and a crew node, the crew's entry, and an automation of that
// crew, and reads what is left.
func agentsAutomationsDelete(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	slug := func(s string) tourOpt { return at("slug", s) }

	tr.setup("the agent tour-doomed", o, agentsAutomationsCreate,
		jsonBody(agentsAutomationsAgent("tour-doomed", "Tour Doomed"))).capture("doomed.agent", "/id")
	tr.queueRun("doomed.run", o, "tour-doomed", `{"project_id":"{{p}}","prompt":"Summarise P before you go."}`).
		capture("doomed.card", "/work_item_id")
	tr.setup("an automation of tour-doomed", o, "POST /api/v1/automations",
		jsonBody(`{"name":"Tour Doomed Watch","kind":"manual","agent_id":"{{doomed.agent}}"}`)).
		capture("doomed.auto", "/id")
	tr.setup("the crew D", o, "POST /api/v1/crews", jsonBody(`{"name":"Tour Doomed Crew"}`)).capture("crew.d", "/id")
	tr.setup("tour-doomed on D", o, "POST /api/v1/crews/{id}/nodes", at("id", "{{crew.d}}"),
		jsonBody(`{"agent_id":"{{doomed.agent}}","label":"Doomed lead"}`)).capture("crew.d.node", "/id")
	tr.setup("tour-doomed's node is D's entry", o, "PUT /api/v1/crews/{id}", at("id", "{{crew.d}}"),
		jsonBody(`{"entry_node_id":"{{crew.d.node}}"}`))
	tr.setup("an automation of D", o, "POST /api/v1/automations",
		jsonBody(`{"name":"Tour Doomed Crew Run","kind":"manual","team_id":"{{crew.d}}"}`)).capture("crew.d.auto", "/id")

	tr.step("the member deletes tour-doomed: the admin guard", m, agentsAutomationsDel, slug("tour-doomed"))
	tr.step("delete tour-doomed: 204, and its file goes to W's .trash", o, agentsAutomationsDel, slug("tour-doomed"))
	tr.step("tour-doomed: 404", o, agentsAutomationsGet, slug("tour-doomed"))
	tr.step("tour-doomed's queued run: 404, deleted with the agent (ON DELETE CASCADE)", o,
		"GET /api/v1/agent-runs/{id}", at("id", "{{doomed.run}}"))
	tr.step("tour-doomed's automation: 404, deleted with the agent (ON DELETE CASCADE)", o,
		"GET /api/v1/automations/{id}", at("id", "{{doomed.auto}}"))
	tr.step("the crew D: tour-doomed's node is gone (ON DELETE CASCADE), nodes and edges null (Q14), and "+
		"entry_node_id still names the node, which has no foreign key", o, "GET /api/v1/crews/{id}",
		at("id", "{{crew.d}}"))
	tr.step("run D's automation now: 400, the entry node is gone", o, "POST /api/v1/automations/{id}/run-now",
		at("id", "{{crew.d.auto}}"))
	tr.step("the run's tracking card: it stays on P's board, assigned to the agent that is gone", o,
		"GET /api/v1/work-items/{id}", at("id", "{{doomed.card}}"),
		note("an S5b route, read to pin what the cascade leaves"))
	tr.step("delete tour-doomed again: 500, agents.ErrNotFound is unmapped (Q2)", o, agentsAutomationsDel,
		slug("tour-doomed"))
	tr.step("delete a slug that is no slug: 500 too", o, agentsAutomationsDel, slug("Not_A_Slug"))
}

// agentsAutomationsSync deletes X's seeds, then syncs X from files the area
// writes into X's agents directory.
func agentsAutomationsSync(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	inX := actingIn("{{x}}")

	seeds := tr.setup("X's seeded agents", o, agentsAutomationsList, inX)
	var listed []struct{ Slug string }
	if err := json.Unmarshal(seeds.body, &listed); err != nil || len(listed) == 0 {
		tr.t.Fatalf("X's seeded agents: %v\n%s", err, seeds.body)
	}
	for _, a := range listed {
		tr.setup("delete X's seed "+a.Slug, o, agentsAutomationsDel, at("slug", a.Slug), inX,
			expect(http.StatusNoContent))
	}
	tr.step(fmt.Sprintf("X's agents, once the area deleted its %d seeds: null (Q14)", len(listed)), o,
		agentsAutomationsList, inX)
	tr.step("sync X: 200, null; the seeds stay deleted, their files went to X's .trash", o, agentsAutomationsSyncs,
		inX)

	agentsAutomationsWrite(tr, "{{x}}", "tour-disk.md", "---\nslug: tour-disk\nname: Tour Disk\n"+
		"provider: claude-code\n---\nWritten on disk, with no allowlist.\n")
	tr.step("sync X with tour-disk.md on disk, a file with no allowed_tools: the agent, its allowlist backfilled "+
		"to mcp__openv__* (REQ-91)", o, agentsAutomationsSyncs, inX,
		note("the area wrote <tmp>/data/agents/<x>/tour-disk.md before this step"))
	tr.step("tour-disk's file: rewritten by the sync with the backfilled allowlist", o, agentsAutomationsRawGet,
		at("slug", "tour-disk"), inX)
	agentsAutomationsWrite(tr, "{{x}}", "broken.md", "No frontmatter here.\n")
	tr.step("sync X with broken.md on disk too: 400, each file's error", o, agentsAutomationsSyncs, inX,
		note("the area wrote <tmp>/data/agents/<x>/broken.md before this step"))
	tr.step("tour-disk: synced by the sync that answered 400 all the same (synced_at)", o, agentsAutomationsGet,
		at("slug", "tour-disk"), inX)
	if err := os.Remove(filepath.Join(tr.agentsDir("{{x}}"), "broken.md")); err != nil {
		tr.t.Fatalf("remove broken.md: %v", err)
	}
	tr.step("sync X once broken.md is gone: 200", o, agentsAutomationsSyncs, inX,
		note("the area removed broken.md before this step"))
	tr.step("the member syncs W: the admin guard", m, agentsAutomationsSyncs)
}

const (
	agentsAutomationsAutoList   = "GET /api/v1/automations"
	agentsAutomationsAutoCreate = "POST /api/v1/automations"
	agentsAutomationsAutoGet    = "GET /api/v1/automations/{id}"
	agentsAutomationsAutoPut    = "PUT /api/v1/automations/{id}"
	agentsAutomationsAutoDel    = "DELETE /api/v1/automations/{id}"
	agentsAutomationsRunNow     = "POST /api/v1/automations/{id}/run-now"
)

// agentsAutomationsAutomations walks the automations: create, list, read,
// update, run-now and delete.
func agentsAutomationsAutomations(tr *tour) {
	o, m, out := tr.owner, tr.actor("member"), tr.actor("outsider")
	auto := func(name string) tourOpt { return at("id", "{{"+name+"}}") }
	body := func(fields map[string]any) tourOpt { return agentsAutomationsBody(tr, fields) }

	tr.setup("the crew E", o, "POST /api/v1/crews", jsonBody(`{"name":"Tour Entry Crew"}`)).capture("crew.e", "/id")
	tr.setup("tour-auto on E", o, "POST /api/v1/crews/{id}/nodes", at("id", "{{crew.e}}"),
		jsonBody(`{"agent_id":"{{auto.agent}}","label":"Lead"}`)).capture("crew.e.node", "/id")
	tr.setup("tour-auto's node is E's entry", o, "PUT /api/v1/crews/{id}", at("id", "{{crew.e}}"),
		jsonBody(`{"entry_node_id":"{{crew.e.node}}"}`))
	tr.setup("the crew H, with no node and so no entry", o, "POST /api/v1/crews",
		jsonBody(`{"name":"Tour Headless Crew"}`)).capture("crew.h", "/id")

	// (a) Create.
	tr.step("create an automation with a body that does not decode", o, agentsAutomationsAutoCreate, jsonBody(`{`))
	tr.step("a manual automation of tour-auto for all of W: 201, enabled, a 60 s cooldown, 10 runs an hour, "+
		"event_filter {}", o, agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Manual", "kind": "manual",
		"agent_id": "{{auto.agent}}", "prompt_template": "Run {{automation.name}} now{{unknown}}"})).
		capture("a.manual", "/id")
	yearly := tr.step("a scheduled automation on 0 0 1 1 *, whose body names the member as created_by: next_run_at "+
		"is to come, and created_by is the owner, the caller, whatever the body says", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Yearly", "kind": "scheduled", "agent_id": "{{auto.agent}}",
			"cron_expr": "0 0 1 1 *", "created_by": "{{member}}"}))
	yearly.capture("a.yearly", "/id")
	agentsAutomationsNoteNext(yearly)
	tr.step("a scheduled automation on the cron nope: 400, robfig's text passed through (Q19)", o,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Bad Cron", "kind": "scheduled",
			"agent_id": "{{auto.agent}}", "cron_expr": "nope"}))
	tr.step("a scheduled automation with no cron: 400", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour No Cron", "kind": "scheduled", "agent_id": "{{auto.agent}}"}))
	tr.step("a triggered automation with no event type: 400", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour No Event", "kind": "triggered", "agent_id": "{{auto.agent}}"}))
	tr.step("a triggered automation on workitem.created, disabled: 201; the matcher takes only enabled ones", o,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Dormant", "kind": "triggered",
			"agent_id": "{{auto.agent}}", "event_type": "workitem.created", "enabled": false,
			"event_filter": map[string]any{"column": "todo"}}),
		note("disabled, since run-now's launches in P publish workitem.created and the matcher would launch a run "+
			"from the bus's goroutine")).capture("a.dormant", "/id")
	tr.step("an automation with no target: 400", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Aimless", "kind": "manual"}))
	tr.step("an automation with an agent and a crew: 400", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Both", "kind": "manual", "agent_id": "{{auto.agent}}",
			"team_id": "{{crew.e}}"}))
	tr.step("an automation of a kind no automation has: 400", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Odd", "kind": "hourly", "agent_id": "{{auto.agent}}"}))
	tr.step("an automation with no name: 400", o, agentsAutomationsAutoCreate,
		body(map[string]any{"kind": "manual", "agent_id": "{{auto.agent}}"}))
	tr.step("an automation of an agent no row has: 400, the foreign key's refusal as the driver words it (Q19)", o,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Ghost Agent", "kind": "manual",
			"agent_id": "{{phantom}}"}))
	tr.step("an automation of a crew no row has: 201, stored as sent (team_id has no foreign key)", o,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Ghost Crew", "kind": "manual",
			"team_id": "{{phantom}}"})).capture("a.ghost", "/id")
	tr.step("an automation of the crew E", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Crew Run", "kind": "manual", "team_id": "{{crew.e}}",
			"prompt_template": "Lead {{automation.name}}."})).capture("a.crew", "/id")
	tr.step("an automation of the crew H", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Headless Run", "kind": "manual", "team_id": "{{crew.h}}"})).
		capture("a.headless", "/id")
	tr.step("an automation whose template is an unknown placeholder alone", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Blank", "kind": "manual", "agent_id": "{{auto.agent}}",
			"prompt_template": "{{unknown}}"})).capture("a.blank", "/id")
	tr.step("an automation whose template is two unknown placeholders and a space", o, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Space", "kind": "manual", "agent_id": "{{auto.agent}}",
			"prompt_template": "{{unknown}} {{ other }}"})).capture("a.space", "/id")
	tr.step("the member creates one for all of W: the workspace admin guard", m, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Member Wide", "kind": "manual", "agent_id": "{{auto.agent}}"}))
	tr.step("the member creates one pinned to P, with no template: 201, P's editor guard", m,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Member Pinned", "kind": "manual",
			"agent_id": "{{auto.agent}}", "project_id": "{{p}}"})).capture("a.member", "/id")
	tr.step("the outsider creates one pinned to P: P's guard", out, agentsAutomationsAutoCreate,
		body(map[string]any{"name": "Tour Outsider Pinned", "kind": "manual", "agent_id": "{{auto.agent}}",
			"project_id": "{{p}}"}))

	// (b) The list.
	tr.step("W's automations: newest first", o, agentsAutomationsAutoList)
	tr.step("the member reads W's automations: every one, those for all of W too", m, agentsAutomationsAutoList)
	tr.step("P's automations", o, agentsAutomationsAutoList, query("project_id={{p}}"))
	tr.step("the automations of a project no row has: null (Q14)", o, agentsAutomationsAutoList,
		query("project_id={{phantom}}"))
	tr.step("X's automations: null (Q14)", o, agentsAutomationsAutoList, actingIn("{{x}}"))

	// (c) Read.
	tr.step("the member reads the manual automation, which is for all of W", m, agentsAutomationsAutoGet,
		auto("a.manual"))
	tr.step("the outsider reads it: the workspace guard", out, agentsAutomationsAutoGet, auto("a.manual"))
	tr.step("an automation no row has: 404", o, agentsAutomationsAutoGet, auto("phantom"))
	tr.step("the id nope: 404, the UUID cast fails and any error is not found", o, agentsAutomationsAutoGet,
		at("id", "nope"))

	// (d) Update.
	tr.step("the member updates the manual automation, which is for all of W: the admin guard", m,
		agentsAutomationsAutoPut, auto("a.manual"), jsonBody(`{"name":"Tour Taken"}`))
	tr.step("update an automation no row has: 404", o, agentsAutomationsAutoPut, auto("phantom"),
		jsonBody(`{"name":"Tour Nothing"}`))
	tr.step("update with a body that does not decode: 400, after the lookup and the guard", o,
		agentsAutomationsAutoPut, auto("a.manual"), jsonBody(`{`))
	tr.step("disable the yearly automation: next_run_at goes", o, agentsAutomationsAutoPut, auto("a.yearly"),
		jsonBody(`{"enabled":false}`))
	enabled := tr.step("enable it again: next_run_at is computed again", o, agentsAutomationsAutoPut,
		auto("a.yearly"), jsonBody(`{"enabled":true}`))
	agentsAutomationsNoteNext(enabled)
	july := tr.step("change its cron to 0 0 1 7 *: next_run_at follows", o, agentsAutomationsAutoPut,
		auto("a.yearly"), jsonBody(`{"cron_expr":"0 0 1 7 *"}`))
	agentsAutomationsNoteNext(july)
	tr.step("change its cron to nope while it is enabled: 400, robfig's text (Q19)", o, agentsAutomationsAutoPut,
		auto("a.yearly"), jsonBody(`{"cron_expr":"nope"}`))
	tr.step("rename the manual automation to \"\": 400", o, agentsAutomationsAutoPut, auto("a.manual"),
		jsonBody(`{"name":""}`))
	tr.step("clear its agent, leaving no target: 400", o, agentsAutomationsAutoPut, auto("a.manual"),
		jsonBody(`{"agent_id":""}`))
	tr.step("the member renames its automation pinned to P: 200", m, agentsAutomationsAutoPut, auto("a.member"),
		jsonBody(`{"name":"Tour Member Run"}`))
	tr.step("disable the yearly automation and give it the cron nope in one update: 200, the cron stored "+
		"unparsed, since a disabled schedule is not computed", o, agentsAutomationsAutoPut, auto("a.yearly"),
		jsonBody(`{"enabled":false,"cron_expr":"nope"}`))

	// (e) Run now.
	tr.step("the member runs the manual automation now, which is for all of W: the admin guard", m,
		agentsAutomationsRunNow, auto("a.manual"))
	tr.step("run an automation no row has: 404", o, agentsAutomationsRunNow, auto("phantom"))
	tr.step("run the manual automation now: 201, the template rendered with {{unknown}} empty, no project and so "+
		"no card", o, agentsAutomationsRunNow, auto("a.manual")).capture("a.manual.run", "/id")
	tr.step("the member runs its automation in P now: an empty template's copy, launched by the member, and P's "+
		"tracking card (workitem.created, as the run)", m, agentsAutomationsRunNow, auto("a.member"))
	tr.step("run the automation of {{unknown}} alone: the empty rendering falls back to the same copy", o,
		agentsAutomationsRunNow, auto("a.blank"))
	tr.step("run the automation of two unknown placeholders and a space: the prompt is that space, since only "+
		"an empty rendering falls back", o, agentsAutomationsRunNow, auto("a.space"))
	tr.step("run the automation of the crew E: its entry node's agent, with team_id and team_node_id", o,
		agentsAutomationsRunNow, auto("a.crew"))
	tr.step("run the automation of the crew H: 400, it has no entry node", o, agentsAutomationsRunNow,
		auto("a.headless"))
	tr.step("run the automation of a crew no row has: 400, the lookup's text", o, agentsAutomationsRunNow,
		auto("a.ghost"))
	tr.step("run the disabled triggered automation now: 201, run-now does not read enabled", o,
		agentsAutomationsRunNow, auto("a.dormant"))

	// (f) Delete.
	tr.step("the member deletes the manual automation, which is for all of W: the admin guard", m,
		agentsAutomationsAutoDel, auto("a.manual"))
	tr.step("the member deletes its automation pinned to P: 204", m, agentsAutomationsAutoDel, auto("a.member"))
	tr.step("the member's automation: 404", m, agentsAutomationsAutoGet, auto("a.member"))
	tr.step("delete an automation no row has: 404", o, agentsAutomationsAutoDel, auto("phantom"))
	tr.step("delete the manual automation: 204", o, agentsAutomationsAutoDel, auto("a.manual"))
	tr.step("the run it launched: it keeps its automation_id (no foreign key)", o, "GET /api/v1/agent-runs/{id}",
		at("id", "{{a.manual.run}}"))
}

// agentsAutomationsTrigger pins the triggered firings: an automation of W
// for all of W, which an artifact the outsider creates in its own workspace
// does not fire, and both it and one pinned to P, fired by an artifact in P.
// The matcher runs on the bus's goroutine, one event at a time, and takes the
// automations of an event oldest first, so the area creates both artifacts
// as setup, the outsider's first, and awaits the last_run_at of the one
// pinned to P: by then the outsider's event, and whatever it fired, is done,
// and so is the firing of the automation for all of W on the artifact in P.
// Were the outsider's event to fire that automation again, its cooldown (60
// s) would hold back its firing on the artifact in P, and the golden would
// change rather than the await time out.
func agentsAutomationsTrigger(tr *tour) {
	o, out := tr.owner, tr.actor("outsider")
	body := func(fields map[string]any) tourOpt { return agentsAutomationsBody(tr, fields) }
	tr.setup("the agent tour-trigger", o, agentsAutomationsCreate,
		jsonBody(agentsAutomationsAgent("tour-trigger", "Tour Trigger"))).capture("trigger.agent", "/id")
	tr.step("an enabled triggered automation of tour-trigger for all of W, on artifact.created", o,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Watch", "kind": "triggered",
			"agent_id": "{{trigger.agent}}", "event_type": "artifact.created",
			"prompt_template": "Look at {{event.title}} ({{event.artifact_type}}) in project {{project.id}}."})).
		capture("a.watch", "/id")
	tr.step("an enabled triggered automation of tour-trigger pinned to P, on artifact.created", o,
		agentsAutomationsAutoCreate, body(map[string]any{"name": "Tour Watch P", "kind": "triggered",
			"agent_id": "{{trigger.agent}}", "event_type": "artifact.created", "project_id": "{{p}}",
			"prompt_template": "Review {{event.title}} in P."})).capture("a.watch.p", "/id")
	tr.setup("the outsider's project, in its own workspace", out, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Elsewhere"}`)).capture("elsewhere.p", "/id")
	tr.setup("the outsider's requirement, which publishes artifact.created in the outsider's workspace", out,
		"POST /api/v1/artifacts", jsonBody(`{"project_id":"{{elsewhere.p}}","type":"requirement",`+
			`"title":"Outsider roadmap","body":"The system shall stay private."}`)).capture("elsewhere.req", "/id")
	tr.setup("a requirement in P, which publishes artifact.created in W after the outsider's", o,
		"POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
			`"title":"Tour barrier","body":"The system shall fire once."}`)).capture("barrier.req", "/id")
	tr.await("the firing on the requirement in P, stamped as last_run_at of the automation pinned to P", o,
		agentsAutomationsAutoGet, func(r *tourResult) bool {
			v, err := jsonValue(r.body, "/last_run_at")
			return r.status == http.StatusOK && err == nil && v != nil
		}, at("id", "{{a.watch.p}}"))
	const setupNote = "the area created the outsider's requirement, then one in P, as setup, and awaited " +
		"last_run_at of the automation pinned to P, which the matcher stamps after the launch and its tracking card"
	tr.step("the automation for all of W: last_run_at, from the requirement in P; the outsider's requirement, in "+
		"another workspace, did not fire it", o, agentsAutomationsAutoGet, at("id", "{{a.watch}}"), note(setupNote))
	tr.step("the automation pinned to P: last_run_at, from the requirement in P", o, agentsAutomationsAutoGet,
		at("id", "{{a.watch.p}}"))
	tr.step("tour-trigger's runs, newest first: the run of the automation pinned to P, then the run of the one for "+
		"all of W, both fired by the requirement in P, scoped to P with their tracking cards there and its title in "+
		"their prompts; the outsider's requirement fired none", o, "GET /api/v1/agent-runs",
		query("agent_id={{trigger.agent}}"),
		note("the matcher compares an event's workspace with the automation's, and its project with the "+
			"automation's when the automation is pinned to one; a run of an automation for all of W takes the "+
			"event's project, which is in W"))
}
