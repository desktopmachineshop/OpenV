//go:build unix

package main

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

// TestTourS5bWorkItems is the S5b tour's work items area: the project board
// (refactor plan §6.4 S5b, before M8 moves these handlers out of
// suite_handlers.go into work_item_handlers.go; invariants I3 (guard, lookup
// and decode order), I4, I5 and I10; quirks Q1, Q14 and Q19; OpenV REQ-13,
// REQ-23, REQ-143). Its golden is testdata/tour/s5b/work_items.json.
//
// The owner, an ordinary account in its personal (nightly) workspace, keeps
// project P, with a requirement R1 and a note on it; project Q, with a note
// of its own; and project E, with nothing. A viewer of P, registered here,
// shows the role gates: it may read and comment (the comment route's
// minimum role is viewer), not create, update, move or delete. An outsider,
// with no access to P, is refused a read and a comment, the comment with a
// malformed body, which the guard refuses before the decode reads it. The area
// walks an item's life: created (the refusals of its body, its title, its
// column, its assignee_id and its source note; the defaults a title alone
// gets; an item raised from a note, with an assignee, artifacts and a due
// date), listed in the board's order, read with its activity, replaced by an
// update (what an omitted field clears and what it keeps), moved,
// commented on and deleted. A workspace runner key and an agent run's token
// write too, and show their actors.
//
// The board drives agents: a person's move of an item assigned to an agent
// into todo makes the orchestration hooks (a bus subscriber) launch a run
// for it, which moves the item into todo as the run (the tracking move) and
// records a run-started activity as "system". The bus dispatches after the
// answer, so what the hooks write lands at no fixed point among the tour's
// exchanges: such a move is sent as setup, and the area polls the item
// (tr.probe, unrecorded) until the hooks have recorded the launch, so every
// time they stamp falls before the next recorded step and their event is
// left out with the setup's. The steps then read what the launch left. That
// nothing is launched by a second move while the item's run is live, by a
// create straight into todo, or by a move sent with a run token or a runner
// key, is read after a barrier: another item's launch, polled for the same
// way. The bus dispatches every event on one goroutine, in order, so once
// the barrier's run-started is there, the hooks have handled every event
// before it.
//
// Every answer of these handlers but a refusal is a bare encode with no
// Content-Type (Q1): sniffed as text/plain, and sent with none once
// compressed, which the board's lists show; the refusals go through
// writeJSONError (application/json). The list of a project with no item
// answers null (Q14), and so does the activity of an item with none.
//
// Nondeterminism: ids and minted times only. Every item comes from a request
// of its own, so the list's last key, created_at, never ties; the activity
// is ordered by created_at, and each entry has a request (or a hook's step)
// of its own.
func TestTourS5bWorkItems(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "work_items",
		about: "Work items on the project board (create, list, read with the activity, update, move, comment, " +
			"delete) as a person, a viewer, a workspace runner key and an agent run's token, and the runs the " +
			"board launches when a person moves an item assigned to an agent into todo.",
		run: workItemsTour,
	})
}

// workItemsSeed creates the projects, the notes, the viewer and the
// outsider the area reads (setup; the artifacts and chatter areas pin those
// routes), and looks up the seeded developer agent.
func workItemsSeed(tr *tour) (viewer, outsider *tourActor) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour board"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour other board"}`)).capture("q", "/id")
	tr.setup("project E, which never gets a work item", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour empty board"}`)).capture("e", "/id")
	tr.setup("R1, the requirement the items point at", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"type":"requirement","title":"Answer in time","body":"The system shall answer within 2 s."}`)).capture("r1", "/id")
	tr.setup("a note on R1", o, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{r1}}","message":"Someone should load test this."}`)).capture("note_p", "/id")
	tr.setup("Q's requirement", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{q}}","type":"requirement",`+
		`"title":"Log each answer","body":"The system shall log each answer."}`)).capture("rq", "/id")
	tr.setup("a note on Q's requirement", o, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{rq}}","message":"A note of the other board."}`)).capture("note_q", "/id")
	viewer = tr.register("viewer", "Tour Viewer", "a viewer of P, from a workspace of its own: may read and "+
		"comment, not write")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	tr.setup("the workspace's agents, for the seeded developer's id (S5d pins the route)", o, "GET /api/v1/agents").
		captureWhere("developer", "", "slug", "developer", "id")
	outsider = tr.register("outsider", "Tour Outsider", "an ordinary account in a workspace of its own, with no "+
		"access to P")
	return viewer, outsider
}

// workItemsAwaitRun polls an item (tr.probe: unrecorded, any answer) until
// the board's hooks have recorded a launch on it (run-started, or
// run-failed when the launch failed), and returns the last answer. The
// hooks record that activity last, after the run and the tracking move, so
// everything they write is in place once it shows.
func workItemsAwaitRun(tr *tour, item string) *tourResult {
	tr.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r := tr.probe(tr.owner, "GET /api/v1/work-items/{id}", at("id", "{{"+item+"}}"))
		if r.status != http.StatusOK {
			tr.t.Fatalf("poll %s for the board's launch: %d %s", item, r.status, r.body)
		}
		if bytes.Contains(r.body, []byte(`"kind":"run-`)) {
			return r
		}
		if time.Now().After(deadline) {
			tr.t.Fatalf("the board's hooks recorded no launch on %s within 10 s:\n%s", item, r.body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// workItemsLaunch moves an item assigned to an agent into todo as the owner
// (setup: the hooks launch its run after the answer), waits for the launch
// and registers the run's id under run.
func workItemsLaunch(tr *tour, title, item, run string) {
	tr.t.Helper()
	tr.setup(title, tr.owner, "POST /api/v1/work-items/{id}/move", at("id", "{{"+item+"}}"),
		jsonBody(`{"column":"todo"}`), expect(http.StatusOK))
	workItemsAwaitRun(tr, item).captureWhere(run, "/activity", "kind", "run-started", "payload/run_id")
}

func workItemsTour(tr *tour) {
	o := tr.owner
	viewer, outsider := workItemsSeed(tr)
	tr.keep("2026-01-15T09:30:00+01:00", "a due date the tour sends with an offset, which the create echoes as sent")
	tr.keep("2026-01-15T09:30:00Z", "that due date read back: due_date is a TIMESTAMP without a time zone, so the "+
		"offset was dropped and the wall clock sent comes back with Z")
	tr.keep("2026-02-01T00:00:00Z", "a due date the tour sends in UTC")

	// Creating, in the handler's order: the guard, the decode, the source
	// note, then the service (title, column, the insert).
	tr.step("the work items of a project with none: null (Q14)", o, "GET /api/v1/projects/{id}/work-items",
		at("id", "{{e}}"))
	tr.step("create with a malformed body", o, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":`))
	tr.step("create with a blank title", o, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"  ","column":"todo"}`))
	tr.step("create in a column the board does not have", o, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), jsonBody(`{"title":"Plan the load test","column":"doing"}`))
	tr.step("create with an assignee_id that is not a UUID: the driver's text (Q19)", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Plan the load test","assignee_id":"not-a-uuid"}`),
		note("nothing checks the id before the insert, which the uuid column refuses"))
	tr.step("create from a note of another project", o, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Plan the load test","source_chatter_id":"{{note_q}}"}`),
		note("the handler checks the note's project, so that this project's item never shows on another's note"))
	tr.step("create from a note that does not exist", o, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Plan the load test","source_chatter_id":"{{phantom}}"}`),
		note("a note the lookup does not find gets the same answer"))
	tr.step("the viewer creates a work item", viewer, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Viewer item"}`))
	tr.step("create in a project that does not exist", o, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{phantom}}"), jsonBody(`{"title":"Nowhere"}`),
		note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("create with a title only, which JSON escapes: the backlog, sort_order 0, assignee_type user with no "+
		"one named, artifact_ids [], created_by: workitem.created", o, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), jsonBody(`{"title":"Draft the <limits> & margins"}`),
		note("sort_order is the column's highest plus 1, and an empty column's highest is -1")).capture("w_first", "/id")
	tr.step("a second item in the backlog: sort_order 1", o, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), jsonBody(`{"title":"Review the margins","description":"Second in the backlog."}`)).
		capture("w_second", "/id")
	tr.step("raise an item from R1's note, in todo, assigned to the owner, with R1 and a due date sent with an "+
		"offset: an assigned activity, and the due date echoed as sent", o, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), jsonBody(`{"title":"Load test the answer","description":"Raised from the note on R1.",`+
			`"column":"todo","assignee_type":"user","assignee_id":"{{owner}}","artifact_ids":["{{r1}}"],`+
			`"due_date":"2026-01-15T09:30:00+01:00","source_chatter_id":"{{note_p}}"}`)).capture("w_note", "/id")
	tr.step("create in done with an empty source_chatter_id, which counts as none", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Close the old ticket","column":"done","source_chatter_id":""}`)).capture("w_done", "/id")
	tr.step("create in review with an assignee_type the board does not know: accepted, nothing checks it", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Check the wording","column":"review","assignee_type":"robot"}`)).capture("w_review", "/id")
	tr.step("create in in-progress, assigned to a team id no team has: accepted, with an assigned activity", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"), jsonBody(`{"title":"Write the timing script",`+
			`"column":"in-progress","assignee_type":"team","assignee_id":"{{phantom}}"}`)).capture("w_progress", "/id")
	tr.step("P's work items: by column name (backlog, done, in-progress, review, todo), then sort_order, then "+
		"created_at; compressed, and then sent with no Content-Type (Q1)", o, "GET /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), note("board_column ASC is a text order, so the columns come alphabetically, not in the "+
			"board's order; these five names sort alike in C and en_US collations"))
	tr.step("the viewer lists P's work items", viewer, "GET /api/v1/projects/{id}/work-items", at("id", "{{p}}"))
	tr.step("list the work items of a project that does not exist", o, "GET /api/v1/projects/{id}/work-items",
		at("id", "{{phantom}}"))

	// Reading one item: the lookup, then the guard.
	tr.step("read the item raised from the note: {activity, item}, keys sorted; the assigned activity; the due "+
		"date read back as the wall clock sent, with Z", o, "GET /api/v1/work-items/{id}", at("id", "{{w_note}}"))
	tr.step("read an item with no activity: activity null", o, "GET /api/v1/work-items/{id}", at("id", "{{w_first}}"))
	tr.step("the viewer reads an item", viewer, "GET /api/v1/work-items/{id}", at("id", "{{w_note}}"))
	tr.step("the outsider reads an item: the item found, then the guard", outsider, "GET /api/v1/work-items/{id}",
		at("id", "{{w_note}}"))
	tr.step("read an item that does not exist", o, "GET /api/v1/work-items/{id}", at("id", "{{phantom}}"))
	tr.step("read an item by an id that is not a UUID", o, "GET /api/v1/work-items/{id}", at("id", "not-a-uuid"),
		note("the lookup's driver error is answered as not found, like an id no item has"))
	tr.step("R1's notes: the note shows the item raised from it, its column and its assignee's name", o,
		"GET /api/v1/chatter", query("artifact_id={{r1}}"),
		note("an S5a route, read to show the link an item raised from a note keeps"))

	// Updating: the lookup, the guard, the decode, then the service.
	tr.step("update an item that does not exist", o, "PUT /api/v1/work-items/{id}", at("id", "{{phantom}}"),
		jsonBody(`{"title":"Nothing"}`))
	tr.step("the viewer updates an item", viewer, "PUT /api/v1/work-items/{id}", at("id", "{{w_note}}"),
		jsonBody(`{"title":"Viewer title"}`))
	tr.step("update with a malformed body", o, "PUT /api/v1/work-items/{id}", at("id", "{{w_note}}"), jsonBody(`[`))
	tr.step("update with a blank title", o, "PUT /api/v1/work-items/{id}", at("id", "{{w_note}}"),
		jsonBody(`{"title":" ","description":"Kept?"}`))
	tr.step("update the note's item with a title only: a full replace, so the description, the assignee_id and "+
		"the due date are cleared, while an omitted assignee_type or artifact_ids keeps the stored one; the column "+
		"and the note stay: workitem.updated", o, "PUT /api/v1/work-items/{id}", at("id", "{{w_note}}"),
		jsonBody(`{"title":"Load test the answer at 2 s"}`))
	tr.step("update it with an empty artifact list, which clears it, another assignee and a due date in UTC: no "+
		"assigned activity", o, "PUT /api/v1/work-items/{id}", at("id", "{{w_note}}"),
		jsonBody(`{"title":"Load test the answer at 2 s","description":"Handed to the viewer.",`+
			`"assignee_type":"user","assignee_id":"{{viewer}}","artifact_ids":[],"due_date":"2026-02-01T00:00:00Z"}`))
	tr.step("read it back: the activity still holds only the creation's assignment", o, "GET /api/v1/work-items/{id}",
		at("id", "{{w_note}}"))

	// Moving: the lookup, the guard, the decode, then the column.
	tr.step("move an item that does not exist", o, "POST /api/v1/work-items/{id}/move", at("id", "{{phantom}}"),
		jsonBody(`{"column":"todo"}`))
	tr.step("the viewer moves an item", viewer, "POST /api/v1/work-items/{id}/move", at("id", "{{w_second}}"),
		jsonBody(`{"column":"todo"}`))
	tr.step("move with a malformed body", o, "POST /api/v1/work-items/{id}/move", at("id", "{{w_second}}"),
		jsonBody(`{"column":`))
	tr.step("move to a column the board does not have", o, "POST /api/v1/work-items/{id}/move",
		at("id", "{{w_second}}"), jsonBody(`{"column":"doing","sort_order":1}`))
	tr.step("move the second backlog item to review at position 5: a moved activity; the sort_order as sent, "+
		"nothing renumbers: workitem.moved, with no assignee_id", o, "POST /api/v1/work-items/{id}/move",
		at("id", "{{w_second}}"), jsonBody(`{"column":"review","sort_order":5}`))
	tr.step("move the note's item, now the viewer's, to done with no sort_order (0): workitem.moved names the "+
		"assignee", o, "POST /api/v1/work-items/{id}/move", at("id", "{{w_note}}"), jsonBody(`{"column":"done"}`))

	// Comments: the lookup, the guard (a viewer may), the decode, then the
	// content.
	tr.step("comment on an item that does not exist", o, "POST /api/v1/work-items/{id}/comments",
		at("id", "{{phantom}}"), jsonBody(`{"content":"Anyone?"}`))
	tr.step("comment with a malformed body", o, "POST /api/v1/work-items/{id}/comments", at("id", "{{w_second}}"),
		jsonBody(`"text"`))
	tr.step("comment with a blank body", o, "POST /api/v1/work-items/{id}/comments", at("id", "{{w_second}}"),
		jsonBody(`{"content":" \n "}`))
	tr.step("comment, with markup that JSON escapes: 201 with the activity, and no event", o,
		"POST /api/v1/work-items/{id}/comments", at("id", "{{w_second}}"),
		jsonBody(`{"content":"Started on <the rig> & the script"}`))
	tr.step("the viewer comments: the route's minimum role is viewer", viewer, "POST /api/v1/work-items/{id}/comments",
		at("id", "{{w_second}}"), jsonBody(`{"content":"Looks right to me."}`))
	tr.step("the outsider comments with a malformed body: 404, the guard before the decode", outsider,
		"POST /api/v1/work-items/{id}/comments", at("id", "{{w_second}}"), jsonBody(`"text"`),
		note("the comment route's guard is the viewer's, so only an account with no access to P shows that it "+
			"runs before the decode"))
	tr.step("read the item: its move, then the two comments, oldest first", o, "GET /api/v1/work-items/{id}",
		at("id", "{{w_second}}"))

	// A workspace runner key writes as worker:<workspace>, with no person
	// behind it; it also claims a run for its token, below.
	key := tr.setup("a workspace runner key", o, "POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"),
		jsonBody(`{"name":"Tour runner"}`), expect(201)).value("/key")
	worker := tr.bearerActor("worker", key, "a workspace runner key of the owner's workspace, as the MCP tools and "+
		"a runner send it: no person behind it")
	tr.step("the runner key creates a work item: no created_by", worker, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), jsonBody(`{"title":"Triage the nightly failures"}`)).capture("w_worker", "/id")

	// The board drives agents. First a run for a token: an item assigned to
	// the developer, moved into todo, whose run the runner key claims (all
	// setup; S5d pins the claim).
	tr.setup("an item assigned to the developer agent, for the run the runner key claims", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"), jsonBody(`{"title":"Implement the timeout",`+
			`"assignee_type":"agent","assignee_id":"{{developer}}"}`)).capture("w_claimed", "/id")
	workItemsLaunch(tr, "the owner moves it into todo, which launches the developer's run", "w_claimed", "run_claimed")
	claim := tr.setup("the runner key claims the queued run", worker, "POST /api/v1/agent-runs/claim",
		jsonBody(`{"worker_id":"tour-runner","providers":["claude-code"]}`), expect(http.StatusOK))
	if claimed := claim.value("/run/id"); claimed != tr.id("run_claimed") {
		tr.t.Fatalf("the runner key claimed run %s, not the board's run (%s)", claimed, tr.id("run_claimed"))
	}
	agent := tr.bearerActor("agent", claim.value("/run_token"), "the token of the developer's run the board "+
		"launched for another item of P, once the runner key claimed it: scoped to P, as editor")

	// The launch, pinned by what it leaves on the item.
	tr.step("create an item assigned to the developer agent: the assigned activity names the agent", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"), jsonBody(`{"title":"Add the retry",`+
			`"description":"Retry once after a timeout.","assignee_type":"agent","assignee_id":"{{developer}}",`+
			`"artifact_ids":["{{r1}}"]}`)).capture("w_agent", "/id")
	workItemsLaunch(tr, "the owner moves it into todo, which launches the developer's run after the answer",
		"w_agent", "run_agent")
	tr.step("read it once the board's hooks have launched its run: the owner's move, the run's tracking move "+
		"into todo at 0, and run-started by system with the run's id", o, "GET /api/v1/work-items/{id}",
		at("id", "{{w_agent}}"), note("the owner's move into todo was sent as setup, and the area polled this item "+
			"until run-started showed: the hooks run after the answer, so their writes, and the workitem.moved "+
			"event the tracking move publishes (shown by the audit step below), land at no fixed point among the "+
			"tour's exchanges"))
	tr.step("move it into todo again while its run is queued: workitem.moved names the agent; no second run "+
		"(read after the barrier below)", o, "POST /api/v1/work-items/{id}/move", at("id", "{{w_agent}}"),
		jsonBody(`{"column":"todo","sort_order":2}`))
	tr.step("create an item assigned to the agent straight into todo: a create is not a move, so no run", o,
		"POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"), jsonBody(`{"title":"Tune the pool",`+
			`"column":"todo","assignee_type":"agent","assignee_id":"{{developer}}"}`)).capture("w_direct", "/id")
	tr.step("the run's token moves it into todo: actor agent:<run>, and only a person's move launches", agent,
		"POST /api/v1/work-items/{id}/move", at("id", "{{w_direct}}"), jsonBody(`{"column":"todo","sort_order":3}`))
	tr.step("the runner key moves it into todo: actor worker:<workspace>, no run either", worker,
		"POST /api/v1/work-items/{id}/move", at("id", "{{w_direct}}"), jsonBody(`{"column":"todo","sort_order":4}`))

	// The barrier: one more launch. The bus dispatches in order, so once its
	// run-started shows, the hooks have handled every move above.
	tr.setup("the barrier: an item assigned to the agent", o, "POST /api/v1/projects/{id}/work-items",
		at("id", "{{p}}"), jsonBody(`{"title":"Barrier item","assignee_type":"agent","assignee_id":"{{developer}}"}`)).
		capture("w_barrier", "/id")
	workItemsLaunch(tr, "the owner moves the barrier into todo, which launches its run", "w_barrier", "run_barrier")
	tr.step("the workspace's three latest events, newest first: the one event the barrier's launch published (its "+
		"tracking move, by the run), then the owner's move and create of the barrier", o, "GET /api/v1/events",
		query("limit=3"), note("the workspace audit, a route another slice pins, read here to show what a launch "+
			"publishes after the answer, which the tour's own event reads leave out with the setup's; the page is "+
			"full, so X-Next-Cursor names its last event"))
	tr.step("read the agent's item: the second move launched nothing", o, "GET /api/v1/work-items/{id}",
		at("id", "{{w_agent}}"))
	tr.step("read the item created in todo: moved by the run's token and the runner key, and no run", o,
		"GET /api/v1/work-items/{id}", at("id", "{{w_direct}}"))

	// Deleting: the lookup, the guard, then the delete; nothing is published.
	tr.step("the viewer deletes an item", viewer, "DELETE /api/v1/work-items/{id}", at("id", "{{w_first}}"))
	tr.step("delete an item: 204, and no event", o, "DELETE /api/v1/work-items/{id}", at("id", "{{w_first}}"))
	tr.step("delete it again", o, "DELETE /api/v1/work-items/{id}", at("id", "{{w_first}}"))
	tr.step("read the deleted item", o, "GET /api/v1/work-items/{id}", at("id", "{{w_first}}"))
	tr.step("P's work items at the end: the board's runs moved theirs into todo and in-progress", o,
		"GET /api/v1/projects/{id}/work-items", at("id", "{{p}}"))
}
