//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestTourS5dProposalsEvents is the S5d tour's proposals-and-events area
// (refactor plan §6.4 S5d, before M3 and M7, which split agent_handlers.go;
// invariants I3, I4, I5, I8, I10, I16; quirks Q1, Q8, Q10, Q14, Q19; OpenV
// REQ-143). Its golden is testdata/tour/s5d/proposals_events.json.
//
// The tour plays the runner (tour_worker_test.go), so the seeded test-case
// author, a proposal-mode agent, never reaches a model: its writes are the
// tour's own requests under the run's token, and the golden's empty
// outbound_requests pins that the server calls no provider. In the owner's
// shared workspace W, with a plain member of W that is a viewer of P, the
// area walks:
//   - the draft (POST /projects/{id}/draft-test-cases): the project guard
//     before the body (a viewer's 403), no ids or blank ones (dropped first),
//     an id that is not a UUID, then a run of the test-case author whose
//     prompt names P and each id once (trimmed and deduplicated, never looked
//     up: an id no artifact has launches too), by the owner and by a worker
//     key (no launched_by: requireProjectRole lets a worker of the project's
//     workspace through, and refuses it another workspace's project with 403
//     and a project no one has with 404, where a person gets 403); by the
//     proposal-mode run's own token, since a draft is no write the proposal
//     mode diverts; in a second workspace L whose author was deleted, 404,
//     and still 404 after a sync, since the delete moved the file to .trash;
//   - the run's writes (S5a's routes under the run's token): a test case with
//     a ref, 202 with its own application/json (unlike Q1's bare encodes) and
//     proposal.created as the run; the same ref twice; a verifies link that
//     names the ref; a ref no proposal holds; a write into another project;
//     a status change;
//   - the finish: awaiting_approval, agentrun.finished #1 and the card moved
//     to review (Q10);
//   - the list: stored payloads (I16: the ref lifted into its own field,
//     attributes null), newest first, the filters, a viewer, a member with no
//     project, an admin's list filtered to the workspace (a proposal of the
//     owner's personal workspace is left out, by the query, before its
//     limit) and null when nothing matches, in the workspace too (Q14);
//   - the apply order: a bulk approval applies create_artifact first and
//     create_link last, ids that do not load keep the middle, the applied
//     writes' events are the system's (the artifact's with a version the
//     handler's own create omits, I10), and the run is finalised (Q10's
//     second agentrun.finished);
//   - the reverse through the single routes: a link approved before its
//     test case fails to apply (a sanitised 500, the real error joined to the
//     reviewer's note in the review note), the test case's rejection
//     finalises the run failed (its error is not stored), and the refusals
//     (reviewed, phantom, id x, a viewer after the lookup);
//   - bulk refusals, a key's 401, a viewer's per-id error, a rejection in
//     the client's order, the 100 a request may carry, and the run's cap of
//     100 proposals; a run's token refused the review of its own proposal
//     (403, before the lookup: a run token is the agent, never a person; fixed
//     under R7, it approved it), which the owner then approves; and the
//     viewer's personal runner key refused the approval of a proposal of P
//     (403: a personal key acts with its member's role; fixed under R7, it
//     applied the proposal with no reviewer), which the owner then approves;
//   - the events route, after the viewer's key is refused a requirement in P
//     (403, fixed under R7 as its approval is) that the owner's own personal
//     key, as W's admin, then creates: a page and the member's view of it
//     (the cursor from the raw page, I8), the actor kinds decorated (user,
//     agent, system, worker, worker:user), the filters, the before cursor,
//     its 400 and a cursor no event has ([]), another project's guard and a
//     key's 401;
//   - Q8 in L: GET /events and GET /agent-runs reset a limit of 0, -1, abc or
//     501 to 100 rather than clamp it, and 500 answers all of more than 100;
//   - the other proposal ops, each with an applier of its own, under a fifth
//     run's token (S5a's routes): an update_artifact, one that carries
//     pendingLinkAdds (which the applier refuses, apply_failed, so the run is
//     finalised failed), a delete_link and a delete_artifact, approved in one
//     bulk request that lists the delete_link first, so that their order (the
//     middle bucket's, the client's) and delete_link's place in it are pinned;
//     the applied writes' events (the system's) and the auto-versions of the
//     deleted link's ends;
//   - last, the single approve route's 2xx, which the viewer's key gave
//     before its fix: the owner approves H's proposal in its personal
//     workspace, and a review body that does not decode is ignored.
//
// Every JSON 2xx here is a bare encode (text/plain by sniffing, Q1), but for
// the proposal receipt's 202; errors are application/json. Proposals are
// ordered created_at DESC, events created_at DESC and id DESC, runs
// created_at DESC. The draft's prompt is the handler's text and the claim is
// setup, so no seed prompt is in the golden; the test-case author's name is.
// A workspace runner key has no member, and still writes in any project of
// its workspace (REQ-42's workspace-wide editor rights; the box key's
// requirement in P). Left to the maintainer, and not pinned as a bug: whether
// a personal key reads, and claims ownerless runs in, a project its member
// cannot view (the runner reads a claimed run's repository connections with
// it). Pinned as they are, bugs for release-noted bug-fix pull requests that
// regenerate the golden: a run finalised failed carries "one or more approved
// proposals failed to apply" only in the status it broadcasts, since
// FinalizeApproval stores no error; and a proposal-mode run launches another
// author run through the draft, with no parent and no launcher.
//
// Not pinned: the budget's 402 on the draft (Q9, orchestration_budget), a
// record_test_result proposal, which no route proposes (no handler diverts a
// result into the queue; S6's event payload test drives its applier
// directly), a pendingLinkRemoves proposal, refused by the same check as
// pendingLinkAdds, a concurrent resolver's lost race in FinalizeApproval, and
// a workspace admin's unfiltered list once the other workspaces hold 500
// newer proposals, which the repository's own test pins
// (TestProposalListFiltersByWorkspaceBeforeItsLimit: the workspace filter
// precedes the limit; fixed under R7, the handler filtered 500 rows read
// across every workspace, so the list came back short or empty).
func TestTourS5dProposalsEvents(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "proposals_events",
		about: "Proposals and the events route: the test-case author's draft, a proposal-mode run's writes under its " +
			"token, the finish that awaits approval, the proposal list, bulk and single approval and rejection in " +
			"their apply order, the run's finalisation (Q10), the refusals, the events route's pages, filters, " +
			"cursor and actor kinds, and Q8's limit reset on the events and runs lists.",
		run: proposalsEventsTour,
		accounts: []tourAccount{{name: "member", display: "Tour Member", about: "a plain member of W and a viewer " +
			"of P: the refusals, its view of the events, and the personal runner key it mints"}},
	})
}

// proposalsEventsAgent is the agent the Q8 launches run in L: an area agent
// with a short prompt, since each of its runs' bodies carries its name.
func proposalsEventsAgent() string {
	b, _ := json.Marshal(map[string]any{
		"slug": "tour-q8", "name": "Q8", "description": "An agent of the S5d tour.",
		"provider": tourDefaultProvider, "allowed_tools": []string{"get_artifact"}, "write_mode": "direct",
		"system_prompt": "You answer in one line.",
	})
	return string(b)
}

// proposalsEventsTestCase is a test case a proposal-mode run writes into P,
// with a ref when ref is not "".
func proposalsEventsTestCase(title, ref string) string {
	v := map[string]any{"project_id": "{{p}}", "type": "test-case", "title": title,
		"body": "Ask, then expect the answer within 2 s."}
	if ref != "" {
		v["ref"] = ref
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func proposalsEventsTour(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	const (
		draft    = "POST /api/v1/projects/{id}/draft-test-cases"
		list     = "GET /api/v1/proposals"
		bulk     = "POST /api/v1/proposals/bulk"
		approve  = "POST /api/v1/proposals/{id}/approve"
		reject   = "POST /api/v1/proposals/{id}/reject"
		events   = "GET /api/v1/events"
		runs     = "GET /api/v1/agent-runs"
		getRun   = "GET /api/v1/agent-runs/{id}"
		finish   = "POST /api/v1/agent-runs/{id}/finish"
		start    = "POST /api/v1/agent-runs/{id}/start"
		artifact = "POST /api/v1/artifacts"
		link     = "POST /api/v1/links"
		status   = "PUT /api/v1/artifacts/{id}/status"
	)
	on := func(name string) tourOpt { return at("id", "{{"+name+"}}") }
	inL := actingIn("{{l}}")

	// W: projects P and Q, the requirements, the member as P's viewer, the
	// keys.
	tr.sharedWorkspace("w", "Tour Shared")
	tr.join(m, "{{w}}", "member")
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Drafts"}`)).capture("p", "/id")
	tr.setup("project Q, where the member has no role", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Elsewhere"}`)).capture("q", "/id")
	tr.setup("R1", o, artifact, jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Answer in time",`+
		`"body":"The system shall answer within 2 s."}`)).capture("r1", "/id")
	tr.setup("R2", o, artifact, jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Log each answer",`+
		`"body":"The system shall log each answer."}`)).capture("r2", "/id")
	tr.setup("the member views P", o, "POST /api/v1/projects/{id}/members", on("p"),
		jsonBody(`{"email":"tour-member@example.com","role":"viewer"}`), expect(http.StatusCreated))
	box := tr.workerKey("box", "{{w}}", "a worker key of W (worker id tour-box): claims and finishes the "+
		"drafts' runs, and drafts itself")
	runner := tr.runnerKey("runner", m, "{{w}}", "the member's personal runner key in W, which acts with the "+
		"member's role, a viewer's in P: it neither reviews nor writes there")
	own := tr.runnerKey("own", o, "{{w}}", "the owner's personal runner key in W, as W's admin: creates the "+
		"requirement the member's key may not, and is revoked at once, so that no run the owner launches later is "+
		"reserved for it")

	// A proposal outside W: a draft in project H of the owner's personal
	// workspace, whose run proposes one test case, so that W's admin list
	// has something to filter out.
	home := tr.workerKey("home", "{{owner.workspace}}", "a worker key of the owner's personal workspace (worker id "+
		"tour-home), whose run leaves a proposal outside W")
	tr.setup("project H, in the owner's personal workspace", o, "POST /api/v1/projects",
		actingIn("{{owner.workspace}}"), jsonBody(`{"name":"Tour Home"}`)).capture("h", "/id")
	tr.setup("H's requirement", o, artifact, jsonBody(`{"project_id":"{{h}}","type":"requirement",`+
		`"title":"Start fast","body":"The system shall start within 5 s."}`)).capture("rh", "/id")
	tr.setup("draft test cases in H", o, draft, on("h"), jsonBody(`{"requirement_ids":["{{rh}}"]}`),
		expect(http.StatusCreated)).capture("author_h", "/id")
	authorH := tr.takeRun(home, "tour-home", "author_h", "the run token of H's draft")
	tr.setup("H's run proposes a test case", authorH, artifact, jsonBody(`{"project_id":"{{h}}","type":"test-case",`+
		`"title":"Start within 5 s"}`), expect(http.StatusAccepted)).capture("h.proposal", "/proposal_id")

	// L, for Q8: 101 runs of an area agent in LP, each with its tracking
	// card and workitem.created, so more than 100 runs and 100 events.
	tr.sharedWorkspace("l", "Tour Busy")
	tr.setup("project LP", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Busy"}`)).capture("lp", "/id")
	tr.setup("the agent tour-q8", o, "POST /api/v1/agents", jsonBody(proposalsEventsAgent())).capture("q8.agent", "/id")
	for i := 1; i <= 101; i++ {
		tr.setup(fmt.Sprintf("launch %d of 101 in LP", i), o, "POST /api/v1/agents/{slug}/runs", at("slug", "tour-q8"),
			jsonBody(`{"project_id":"{{lp}}","prompt":"n"}`), expect(http.StatusCreated))
	}
	tr.actIn(o, "{{w}}")

	// (a) The draft.
	tr.step("draft with no requirement ids", o, draft, on("p"), jsonBody(`{"requirement_ids":[]}`))
	tr.step("draft with blank ids only: blanks are dropped before the check", o, draft, on("p"),
		jsonBody(`{"requirement_ids":[""," "]}`))
	tr.step("draft with an id that is not a UUID", o, draft, on("p"), jsonBody(`{"requirement_ids":["{{r1}}","R-1"]}`))
	tr.step("draft with a body that does not decode", o, draft, on("p"), jsonBody(`{`))
	tr.step("the member, P's viewer, drafts: 403, before the body is decoded", m, draft, on("p"), jsonBody(`{`))
	first := tr.step("draft for R1 and R2, one padded and repeated: 201, a queued run of the test-case author whose "+
		"prompt names P and each id once, and its tracking card", o, draft, on("p"),
		jsonBody(`{"requirement_ids":["{{r1}}"," ","{{r2}}"," {{r1}} "]}`),
		note("the prompt is the handler's text; the requirement ids are trimmed and deduplicated, in the order sent"))
	first.capture("author", "/id")
	first.capture("author.card", "/work_item_id")
	first.capture("author.agent", "/agent_id")
	second := tr.step("the box key drafts for R2 and an id no artifact has: 201 with no launched_by, since the "+
		"project guard lets a worker of P's workspace through; the ids are never looked up", box, draft, on("p"),
		jsonBody(`{"requirement_ids":["{{r2}}","{{phantom}}"]}`))
	second.capture("author2", "/id")
	second.capture("author2.card", "/work_item_id")
	tr.step("the box key drafts in H, a project of the owner's personal workspace: 403", box, draft, on("h"),
		jsonBody(`{"requirement_ids":["{{rh}}"]}`))
	tr.step("the box key drafts in a project no one has: 404, where a person's answer is 403", box, draft,
		on("phantom"), jsonBody(`{"requirement_ids":["{{r1}}"]}`))
	tr.setup("delete L's test-case author", o, "DELETE /api/v1/agents/{slug}", at("slug", "test-case-author"), inL,
		expect(http.StatusNoContent))
	tr.step("draft in L, whose test-case author was deleted: 404", o, draft, on("lp"), inL,
		jsonBody(`{"requirement_ids":["{{phantom}}"]}`))
	tr.setup("sync L's agents from disk", o, "POST /api/v1/agents/sync", inL)
	tr.step("draft in L after a sync: still 404, the delete moved the author's file to .trash", o, draft, on("lp"),
		inL, jsonBody(`{"requirement_ids":["{{phantom}}"]}`))

	// (b) The run's writes, under its token.
	author := tr.takeRun(box, "tour-box", "author", "the test-case author's run token, from its claim: its writes "+
		"become proposals")
	tr.setup("start the author's run", box, start, on("author"))
	tr.step("the run proposes test case tc1: 202 with its own Content-Type (unlike Q1), and proposal.created as "+
		"the run", author, artifact, jsonBody(proposalsEventsTestCase("Answer within 2 s", "tc1"))).
		capture("a1", "/proposal_id")
	tr.step("the same ref again: 400", author, artifact, jsonBody(proposalsEventsTestCase("Answer again", "tc1")))
	tr.step("the run proposes tc1 verifies R1, naming the test case by its ref: 202", author, link,
		jsonBody(`{"from_id":"tc1","to_id":"{{r1}}","type":"verifies"}`)).capture("l1", "/proposal_id")
	tr.step("a ref no proposal of the run holds: 400", author, link,
		jsonBody(`{"from_id":"tc9","to_id":"{{r1}}","type":"verifies"}`))
	tr.step("a write into Q: 403, the run is scoped to P", author, artifact,
		jsonBody(`{"project_id":"{{q}}","type":"test-case","title":"Elsewhere"}`))
	tr.step("the run changes R1's status: 403", author, status, on("r1"), jsonBody(`{"status":"approved"}`))
	drafted := tr.step("the run's token drafts in P: 201, a run of the test-case author with no launched_by and no "+
		"parent; the draft is no write the proposal mode diverts", author, draft, on("p"),
		jsonBody(`{"requirement_ids":["{{r1}}"]}`))
	drafted.capture("author_t", "/id")
	drafted.capture("author_t.card", "/work_item_id")
	tr.setup("cancel the run the token drafted, so that no later claim takes it", o,
		"POST /api/v1/agent-runs/{id}/cancel", on("author_t"))

	// (c) The finish.
	tr.step("the box key finishes the run as succeeded: awaiting_approval, since two proposals are pending; "+
		"agentrun.finished #1 and the card moved to review (Q10)", box, finish, on("author"),
		jsonBody(`{"status":"succeeded","final_text":"Drafted a test case with a verifies link for R1; R2 needs a load `+
			`profile before a test can be written."}`))

	// (d) The list.
	tr.step("P's proposals: newest first, each payload as stored (the ref in a field of its own, attributes null)",
		o, list, query("project_id={{p}}"))
	tr.step("the pending proposals, as W's admin with no project: H's, in another workspace, filtered out", o, list,
		query("status=pending"))
	tr.step("the author's proposals", o, list, query("run_id={{author}}"))
	tr.step("P's rejected proposals: none, null (Q14)", o, list, query("project_id={{p}}&status=rejected"))
	tr.step("run_id x: 500, the cast to a UUID fails in SQL", o, list, query("run_id=x"))
	tr.step("the member lists P's proposals, as its viewer", m, list, query("project_id={{p}}"))
	tr.step("the member lists with no project: 403", m, list)
	tr.step("the member lists Q's proposals: Q's guard", m, list, query("project_id={{q}}"))
	tr.step("the box key lists: the handler's 401, a key is no user", box, list, query("project_id={{p}}"))
	tr.step("every proposal, as L's admin with no project: none in L, null (Q14), however many other workspaces "+
		"hold", o, list, inL)

	// (e) The apply order.
	tr.step("bulk-approve the link, an id no proposal has and the test case, in that order: 200; the test case "+
		"applies first and the link last, the phantom keeps the middle; the applied writes are the system's; the "+
		"run's last proposal resolved, it is finalised succeeded: agentrun.finished #2 and the card to done (Q10)",
		o, bulk, jsonBody(`{"action":"approve","ids":["{{l1}}","{{phantom}}","{{a1}}"],"note":"Looks right."}`))
	applied := tr.step("the author's proposals: applied, each with the entity it created, reviewed by the owner", o,
		list, query("run_id={{author}}"))
	applied.captureWhere("tc1.artifact", "", "op", "create_artifact", "applied_entity_id")
	applied.captureWhere("tc1.link", "", "op", "create_link", "applied_entity_id")
	tr.step("the author's run: succeeded, finished_at the approval's time", o, getRun, on("author"))

	// (f) The reverse, through the single routes.
	author2 := tr.takeRun(box, "tour-box", "author2", "the second draft's run token")
	tr.setup("the second run proposes its tc1", author2, artifact, jsonBody(proposalsEventsTestCase("Log it", "tc1")),
		expect(http.StatusAccepted)).capture("a2", "/proposal_id")
	tr.setup("the second run proposes tc1 verifies R2", author2, link,
		jsonBody(`{"from_id":"tc1","to_id":"{{r2}}","type":"verifies"}`), expect(http.StatusAccepted)).
		capture("l2", "/proposal_id")
	tr.setup("finish the second run: awaiting_approval", box, finish, on("author2"), jsonBody(`{"status":"succeeded"}`))
	tr.step("approve the link before its test case, with a note: 500, sanitised; the proposal is stored "+
		"apply_failed", o, approve, on("l2"), jsonBody(`{"note":"Link first."}`))
	tr.step("the second run's proposals: the link apply_failed, its review note the reviewer's note and the real "+
		"error", o, list, query("run_id={{author2}}"))
	tr.step("reject the test case: 200; the run's last pending proposal resolved, it is finalised failed, since "+
		"one apply failed: agentrun.finished #2 and the card back to todo (Q10)", o, reject, on("a2"),
		jsonBody(`{"note":"Not this one."}`))
	tr.step("the second run: failed; the finalisation's error is not stored", o, getRun, on("author2"))
	tr.step("approve the link again: 400", o, approve, on("l2"))
	tr.step("approve a proposal no one has: 404", o, approve, on("phantom"))
	tr.step("reject a proposal no one has: 404", o, reject, on("phantom"))
	tr.step("approve the id x: 404, the lookup's error", o, approve, at("id", "x"))
	tr.step("the member approves the reviewed link: 403, the guard runs after the lookup and before the review "+
		"check", m, approve, on("l2"))

	// (g) Bulk refusals, a run's own review refused, and a viewer's key's.
	third := tr.setup("a third draft, for R1", o, draft, on("p"), jsonBody(`{"requirement_ids":["{{r1}}"]}`),
		expect(http.StatusCreated))
	third.capture("author3", "/id")
	third.capture("author3.card", "/work_item_id")
	author3 := tr.takeRun(box, "tour-box", "author3", "the third draft's run token")
	tr.setup("the third run proposes its tc1", author3, artifact, jsonBody(proposalsEventsTestCase("Time it", "tc1")),
		expect(http.StatusAccepted)).capture("a3", "/proposal_id")
	tr.setup("the third run proposes tc1 verifies R1", author3, link,
		jsonBody(`{"from_id":"tc1","to_id":"{{r1}}","type":"verifies"}`), expect(http.StatusAccepted)).
		capture("l3", "/proposal_id")
	tr.setup("the third run proposes a test case it will approve itself", author3, artifact,
		jsonBody(proposalsEventsTestCase("Self-reviewed", "")), expect(http.StatusAccepted)).capture("s3", "/proposal_id")
	tr.setup("the third run proposes a test case the member's key will approve", author3, artifact,
		jsonBody(proposalsEventsTestCase("Key-reviewed", "")), expect(http.StatusAccepted)).capture("k3", "/proposal_id")
	tr.step("the run approves its own proposal: 403, a run token is the agent and reviews no proposal, refused "+
		"before the lookup; the proposal stays pending", author3, approve, on("s3"))
	tr.setup("the owner approves the proposal its run could not", o, approve, on("s3")).
		capture("s3.artifact", "/applied_entity_id")
	tr.step("the member's runner key approves a proposal of P, where the member is only a viewer: 403, a personal "+
		"key acts with its member's role (fixed under R7, it applied the proposal with no reviewer); the proposal "+
		"stays pending", runner, approve, on("k3"), jsonBody(`{`))
	tr.setup("the owner approves the proposal the member's key could not, with a body that does not decode, which "+
		"is ignored", o, approve, on("k3"), jsonBody(`{`), expect(http.StatusOK)).
		capture("k3.artifact", "/applied_entity_id")
	tr.setup("finish the third run: awaiting_approval", box, finish, on("author3"), jsonBody(`{"status":"succeeded"}`))
	tr.step("bulk with a body that does not decode", o, bulk, jsonBody(`{`))
	tr.step("bulk with action x", o, bulk, jsonBody(`{"action":"x","ids":["{{a3}}"]}`))
	tr.step("bulk with no ids", o, bulk, jsonBody(`{"action":"reject","ids":[]}`))
	tr.step("bulk with 101 ids: 400, before any is loaded", o, bulk,
		jsonBody(`{"action":"reject","ids":["{{phantom}}"`+strings.Repeat(`,"{{phantom}}"`, 100)+`]}`))
	tr.step("the box key bulk-rejects: the handler's 401", box, bulk, jsonBody(`{"action":"reject","ids":["{{l3}}"]}`))
	tr.step("the member bulk-rejects the link, as P's viewer: 200, with the refusal per id", m, bulk,
		jsonBody(`{"action":"reject","ids":["{{l3}}"]}`))
	tr.step("bulk-reject the link, an id no proposal has and the test case: 200, in the client's order; the "+
		"run's last proposals resolved and none failed, it is finalised succeeded (Q10)", o, bulk,
		jsonBody(`{"action":"reject","ids":["{{l3}}","{{phantom}}","{{a3}}"]}`))

	// (h) The events route.
	tr.step("the owner creates a requirement in Q: artifact.created as user:<owner>", o, artifact,
		jsonBody(`{"project_id":"{{q}}","type":"requirement","title":"Keep a log",`+
			`"body":"The system shall keep a log."}`)).capture("rq", "/id")
	tr.step("the box key creates a requirement in P: 201, artifact.created as worker:<w>", box, artifact,
		jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Answer politely",`+
			`"body":"The system shall be polite."}`)).capture("r3", "/id")
	brief := jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Answer briefly",` +
		`"body":"The system shall be brief."}`)
	tr.step("the member's runner key creates a requirement in P, which the member only views: 403, a personal key "+
		"acts with its member's role (fixed under R7, it created the requirement)", runner, artifact, brief)
	tr.setup("the owner's runner key creates that requirement in P, as W's admin: artifact.created as "+
		"worker:<w>:user:<owner>", own, artifact, brief, expect(http.StatusCreated)).capture("r4", "/id")
	tr.setup("the owner revokes its runner key", o, "DELETE /api/v1/orgs/{id}/my-runner-key", at("id", "{{w}}"),
		expect(http.StatusNoContent))
	tr.step("W's newest four events, as its admin: X-Next-Cursor, the last one's id, on a full page (I8)", o, events,
		query("limit=4"))
	tr.step("the member's view of the same page: Q's event dropped, the cursor still the raw page's last id", m,
		events, query("limit=4"))
	tr.step("W's artifact.created events, the actor kinds decorated: user, system, worker, and worker for a "+
		"personal key with its member's name", o, events, query("event_type=artifact.created"))
	tr.step("W's proposal.created events: the agent kind, named after the run's agent", o, events,
		query("event_type=proposal.created"))
	tr.step("P's newest three events", o, events, query("project_id={{p}}&limit=3")).
		captureHeader("events.cursor", "X-Next-Cursor", `.+`)
	tr.step("P's next three, before the cursor", o, events, query("project_id={{p}}&limit=3&before={{events.cursor}}"))
	tr.step("before x: 400", o, events, query("before=x"))
	tr.step("before an event no one has: [], never null", o, events, query("before={{phantom}}"))
	tr.step("the member reads Q's events: Q's guard", m, events, query("project_id={{q}}"))
	tr.step("the box key reads the events: the handler's 401", box, events)

	// The run's cap, and the most a bulk request takes.
	tr.setup("a fourth draft, for R2", o, draft, on("p"), jsonBody(`{"requirement_ids":["{{r2}}"]}`),
		expect(http.StatusCreated)).capture("author4", "/id")
	author4 := tr.takeRun(box, "tour-box", "author4", "the fourth draft's run token")
	ids := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		res := tr.setup(fmt.Sprintf("the fourth run's proposal %d of 100", i), author4, artifact,
			jsonBody(proposalsEventsTestCase(fmt.Sprintf("Case %d", i), "")), expect(http.StatusAccepted))
		ids = append(ids, `"`+res.value("/proposal_id")+`"`)
	}
	tr.step("the fourth run's 101st proposal: 400, a run proposes at most 100", author4, artifact,
		jsonBody(proposalsEventsTestCase("Case 101", "")))
	tr.step("bulk-reject the fourth run's 100 proposals: 200, 100 is the most a request takes; the run is still "+
		"claimed, so nothing is finalised", o, bulk, jsonBody(`{"action":"reject","ids":[`+strings.Join(ids, ",")+`]}`))

	// (i) Q8, in L.
	for _, limit := range []string{"0", "-1", "abc", "501"} {
		tr.step(fmt.Sprintf("L's events with limit %s: reset to 100, not clamped (Q8), and X-Next-Cursor the last "+
			"one's id (I8)", limit), o, events, inL, query("limit="+limit))
	}
	tr.step("L's events with limit 500: all 101, and no cursor", o, events, inL, query("limit=500"))
	for _, limit := range []string{"0", "-1", "abc", "501"} {
		tr.step(fmt.Sprintf("LP's runs with limit %s: reset to 100, not clamped (Q8)", limit), o, runs, inL,
			query("project_id={{lp}}&limit="+limit))
	}
	tr.step("LP's runs with limit 500: all 101", o, runs, inL, query("project_id={{lp}}&limit=500"))

	// (j) The other proposal ops: update_artifact, delete_link and
	// delete_artifact share the propose side with create_artifact, but each
	// has an applier of its own. After the Q8 steps, so that no step number
	// cited elsewhere moves.
	tr.setup("TC, a test case of P", o, artifact, jsonBody(`{"project_id":"{{p}}","type":"test-case",`+
		`"title":"Answer within 2 s, measured","body":"Ask, then time the answer."}`)).capture("tc", "/id")
	tr.setup("TC2, a test case of P the run will delete", o, artifact, jsonBody(`{"project_id":"{{p}}",`+
		`"type":"test-case","title":"Answer at once","body":"Ask, then expect no delay."}`)).capture("tc2", "/id")
	tr.setup("TC verifies R1", o, link, jsonBody(`{"from_id":"{{tc}}","to_id":"{{r1}}","type":"verifies"}`)).
		capture("lk", "/id")
	fifth := tr.setup("a fifth draft, for R1", o, draft, on("p"), jsonBody(`{"requirement_ids":["{{r1}}"]}`),
		expect(http.StatusCreated))
	fifth.capture("author5", "/id")
	fifth.capture("author5.card", "/work_item_id")
	author5 := tr.takeRun(box, "tour-box", "author5", "the fifth draft's run token, whose update and delete "+
		"writes become proposals")
	tr.setup("start the fifth run", box, start, on("author5"))
	tr.step("the run renames R1: 202, an update_artifact proposal, its payload the request as the handler decoded "+
		"it", author5, "PUT /api/v1/artifacts/{id}", on("r1"), jsonBody(`{"title":"Answer within 2 s, always"}`)).
		capture("u5", "/proposal_id")
	tr.step("the run adds a managed link to R2 through the update: 202, an update_artifact proposal that carries "+
		"pendingLinkAdds", author5, "PUT /api/v1/artifacts/{id}", on("r2"),
		jsonBody(`{"pendingLinkAdds":[{"from_id":"{{tc}}","to_id":"{{r2}}","type":"verifies"}]}`)).
		capture("m5", "/proposal_id")
	tr.step("the run deletes TC verifies R1: 202, a delete_link proposal with the link as its target", author5,
		"DELETE /api/v1/links/{id}", on("lk")).capture("dl5", "/proposal_id")
	tr.step("the run deletes TC2: 202, a delete_artifact proposal", author5, "DELETE /api/v1/artifacts/{id}",
		on("tc2")).capture("da5", "/proposal_id")
	tr.setup("finish the fifth run: awaiting_approval", box, finish, on("author5"), jsonBody(`{"status":"succeeded"}`))
	tr.step("bulk-approve the link's delete first, then R1's rename, R2's managed link and TC2's delete: 200, in "+
		"the client's order, since none is create_artifact or create_link; the managed link's update fails to "+
		"apply, the rest are the system's writes (the link's delete versions both ends); the run's last proposal "+
		"resolved with one failure, it is finalised failed (Q10)", o, bulk,
		jsonBody(`{"action":"approve","ids":["{{dl5}}","{{u5}}","{{m5}}","{{da5}}"],"note":"Tidy up."}`))
	tr.step("the fifth run's proposals: three applied, the managed link's apply_failed with the applier's refusal "+
		"in its review note", o, list, query("run_id={{author5}}"))
	tr.step("TC after its link's delete: a new version with no link in its snapshot", o, "GET /api/v1/artifacts/{id}",
		on("tc"))
	tr.step("R1 after the approval: two versions on, one for the link's delete and one for its rename", o,
		"GET /api/v1/artifacts/{id}", on("r1"))

	// (k) The single approve route's 2xx, which step 43 gave until the
	// member's key was refused there: the proposal H's run left outside W,
	// approved where it belongs. Last, so that no step number cited
	// elsewhere moves.
	tr.step("the owner approves H's proposal, in its personal workspace: 200, applied, the owner its reviewer; a "+
		"body that does not decode is ignored", o, approve, on("h.proposal"), actingIn("{{owner.workspace}}"),
		jsonBody(`{`)).capture("h.artifact", "/applied_entity_id")
}
