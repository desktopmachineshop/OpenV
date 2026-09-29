//go:build unix

package main

import (
	"encoding/json"
	"testing"
)

// TestTourS5dWorkerWire is the S5d tour's worker-wire area, the pilot of the
// slice (refactor plan §6.4 S5d, before M3, which moves main.go's closures
// the wire relies on, and M7, which splits agent_handlers.go; invariants I2,
// I3, I4, I5, I9's stream head, I10, I12; quirks Q1, Q10, Q14, Q19; OpenV
// REQ-143). Its golden is testdata/tour/s5d/worker_wire.json.
//
// The tour plays the runner itself, with the requests runner/client.go sends
// (tour_worker_test.go), so no run ever reaches a model: the golden's empty
// outbound_requests pins that the server makes no provider call of its own on
// this wire. The area walks, in the owner's shared workspace W with a plain
// member that has no role in its project P:
//   - the claim's refusals in the handler's order (a session before the
//     body, a body that does not decode) and its 204, with no body and no
//     Content-Type, when nothing is queued, and while a run is queued but the
//     claim names another provider, none, or a priority floor above it, or
//     comes from the member's personal key, which takes only its member's runs
//     and those no one launched (it claims run_b below, which a key launched);
//   - launch: an agent W does not have (before the body is decoded), a body
//     that does not decode, no prompt (after the project's guard), the member
//     in P, a key of another workspace (the agent is looked up in the key's
//     own workspace), a card id that is not a UUID (the driver's text, Q19),
//     then a run in P: queued, attempt 1 of 3, the agent's snapshot
//     (agent_content_hash, model, effort), and the board's tracking card,
//     created by the hooks inside the request (workitem.created, actor
//     agent:<run>, its workspace filled in by the bus's project resolver);
//   - the claim (I12): its keys agent, auth, run, run_token, sorted since the
//     body is a Go map; HTML-escaped as json.Encoder writes it (the agent's
//     prompt carries <, > and &); auth an object, {"mode":"user-account"}; the
//     run claimed by the worker id with heartbeat_at stamped by the database;
//   - start, as a runner sends it (no body, 204), refused to a key of
//     another workspace and to the member's personal key (both 404, as if the
//     run did not exist), and to the run's own token (403: a run token is no
//     worker); started twice, and started while queued (409);
//   - both log shapes: the bare array older runners send and the object with
//     partial_text (which GET shows), null and {} as heartbeats, a body that
//     does not decode, a seq sent twice (the first write stands), an entry
//     with no created_at (stamped by the server) beside one the tour dated,
//     and one with no payload (stored as JSON null, read back as {}, as every
//     log frame of the stream's replay shows it too); the log read plain,
//     after seq 1, and after seq abc (read as 0), and [] for a run with none
//     (Q14's other side);
//   - the stream's replay (I9's head): each log frame in seq order, then one
//     status frame; after seq 1; the status frame alone for a queued run; a
//     run no one has (a JSON 404, not a stream); a key (the handler's 401);
//   - a cooperative cancel (the flag, heard by the next log push, then the
//     worker's cancelled finish and its agentrun.finished, actor agent:<run>),
//     finish refusals (twice, a status no run has, a body that does not
//     decode), the finished run's token refused by the middleware, and a
//     queued cancel, which is final at once and publishes workitem.moved but no
//     agentrun.finished (Q10);
//   - release (I12): {} answers 204 and releases nothing (no worker id
//     matches the claimer's), no body answers 400, the claimer's worker id
//     puts the run back in the queue and clears its answer so far (a setup
//     log push gave it one) and revokes its token, which the middleware then
//     refuses, and the next claim issues a new one;
//   - finish with tokens, cost and exit code; retry refused for a succeeded
//     run; finish refused (409) for a run no worker ever claimed; a launch on
//     a card id no card has (stored as sent: agent_runs.work_item_id has no
//     foreign key; the board's move of that card only logs);
//   - launches by a worker key and by a run's token (201 with no launched_by:
//     the handler has no user check, and the project guard lets a worker of
//     the project's workspace and a run scoped to the project through, a
//     launch with no project the workspace's plan gate alone); the run's
//     launch records the launching run as its parent (parent_run_id), as a
//     delegation does;
//   - the auto-retry of a retryable failure (worker_error): attempt 2 queued
//     behind a 30 s backoff that the claim honours (204), a manual retry
//     claimable at once, and a failure of class agent_error, which is not
//     retried;
//   - a personal runner's reservation: once the member's key has polled, the
//     member's launch is reserved for it (preferred_user_id) for 60 s, the
//     workspace key's claim answers 204, and the personal key takes it;
//   - a project on the workspace's API key: auth {"api_key_env","mode"}, the
//     provider's own variable, then the one W's provider setting names;
//   - the hosted claim, which skips a run whose agent has repository access
//     (the plan flag is on without tiers; its refusal is S5e's over-plan pass);
//   - the WORKER_API_KEY key (M3's bootstrapOrgID closure): with no workspace
//     at boot no key row was registered, so it resolves lazily to the earliest
//     account's personal workspace, admin's, whose runs it claims and whose
//     worker-key list stays null;
//   - the reads: the run guard (the launcher, the project's roles, a
//     workspace admin for an unscoped run), a key's 401, the id "claim" (GET
//     matches {id}), the member's own runs and null when none match (Q14), the
//     filters, an agent_id that is not a UUID (500, the cast fails in SQL),
//     and every run of W, newest first, compressed with no Content-Type (Q1).
//
// Every 2xx JSON answer of the wire is a bare encode (text/plain by sniffing,
// Q1); errors are application/json; the stream is text/event-stream with
// Cache-Control, Connection and X-Accel-Buffering. Nondeterminism: the claim
// takes the workspace's highest-priority, oldest queued run, so the area keeps
// one claimable run at a time and checks each claim (claimed); heartbeat_at
// and a queued cancel's finished_at are the database's NOW(), the rest Go's
// clock, all placed by the tour's clock; an auto-retry's next_attempt_at is 30
// s ahead, so it is captured by name and its distance noted; hosted_after, 60
// s ahead, is <time> with its distance noted. Not pinned: the reaper (its
// first tick is at 30 s and it fails runs silent for 2 minutes), a live
// broadcast on an open stream and its keepalive (S6), the runner's own side of
// the wire and its tolerance of auth null (S7), the hosted claim's plan-flag
// refusal (S5e), a claim handshake that fails after the claim (it needs a
// run whose agent is gone between the claim's two queries), WORKER_API_KEY's
// registration as a key row at boot, which M3 moves with the bootstrapOrgID
// closure (it needs a personal workspace before the server starts, and each
// area boots on a fresh database; boot_steps.txt pins where the call is, not
// what it does), and a log longer than a page of the repository's read
// (2,000 entries): the replay's draining of every page is pinned by
// internal/api's TestStreamAgentRunDrainsEveryLogPage.
func TestTourS5dWorkerWire(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "worker_wire",
		about: "The worker wire, played by the tour as a runner: launch, the claim's bytes and its 204, start, both log " +
			"shapes, the run's log and its stream's replay, a cooperative and a queued cancel, finish, release, retry " +
			"and the auto-retry's backoff, a personal runner's reservation, a project's API-key auth, the hosted skip, " +
			"the legacy WORKER_API_KEY, and the run reads.",
		run: workerWireTour,
		env: map[string]string{
			// The legacy single key, which main.go registers as a key row
			// only if a personal workspace exists at boot (none does here),
			// and otherwise resolves lazily on each request.
			"WORKER_API_KEY": workerWireLegacyKey,
		},
		accounts: []tourAccount{{name: "member", display: "Tour Member", about: "a plain member of W with no role " +
			"in P: the refusals, an unscoped launch, and the personal runner key that reserves its runs"}},
	})
}

// workerWireLegacyKey is the area's WORKER_API_KEY.
const workerWireLegacyKey = "tour-legacy-worker-key"

// workerWireLogTime is the time the tour's first log entry carries.
const workerWireLogTime = "2026-01-02T03:04:05.5Z"

// workerWireAgent is an agent definition with a short prompt, so that the
// claim bytes hold the area's own agent rather than a seeded one, whose
// prompt changes with feature work. The prompt carries <, > and &, so that
// the claim's HTML escaping (I4) shows in its bytes.
func workerWireAgent(slug, name string, repoAccess bool) string {
	b, _ := json.Marshal(map[string]any{
		"slug": slug, "name": name, "description": "An agent of the S5d tour.", "provider": tourDefaultProvider,
		"allowed_tools": []string{"get_artifact"}, "write_mode": "direct", "repo_access": repoAccess,
		"system_prompt": "You answer in one line & in plain text, never in <html>.",
	})
	return string(b)
}

func workerWireTour(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	tr.sharedWorkspace("w", "Tour Shared")
	tr.join(m, "{{w}}", "member")
	const (
		launch   = "POST /api/v1/agents/{slug}/runs"
		claim    = "POST /api/v1/agent-runs/claim"
		start    = "POST /api/v1/agent-runs/{id}/start"
		logs     = "POST /api/v1/agent-runs/{id}/logs"
		readLogs = "GET /api/v1/agent-runs/{id}/logs"
		stream   = "GET /api/v1/agent-runs/{id}/stream"
		finish   = "POST /api/v1/agent-runs/{id}/finish"
		release  = "POST /api/v1/agent-runs/{id}/release"
		cancel   = "POST /api/v1/agent-runs/{id}/cancel"
		retry    = "POST /api/v1/agent-runs/{id}/retry"
		get      = "GET /api/v1/agent-runs/{id}"
		list     = "GET /api/v1/agent-runs"
	)
	agent := at("slug", "tour-worker")
	run := func(name string) tourOpt { return at("id", "{{"+name+"}}") }
	as := func(workerID string) tourOpt { return claimBody(workerID, tourDefaultProvider) }

	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Pilot"}`)).capture("p", "/id")
	tr.setup("project K, whose agents run on the workspace's API key", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Keyed","agent_auth":"api-key"}`)).capture("k", "/id")
	tr.setup("the agent tour-worker", o, "POST /api/v1/agents", jsonBody(workerWireAgent("tour-worker", "Tour Worker",
		false))).capture("agent", "/id")
	tr.setup("the agent tour-repo, with repository access", o, "POST /api/v1/agents",
		jsonBody(workerWireAgent("tour-repo", "Tour Repo", true))).capture("repo.agent", "/id")
	box := tr.workerKey("box", "{{w}}", "a worker key of W, as a workspace runner sends it (worker id tour-box, "+
		"tour-hosted for its hosted claim)")
	elsewhere := tr.workerKey("elsewhere", "{{owner.workspace}}", "a worker key of the owner's personal workspace, "+
		"not W's")
	runner := tr.runnerKey("runner", m, "{{w}}", "the member's personal runner key in W (worker id tour-runner)")
	legacy := tr.bearerActor("legacy", workerWireLegacyKey, "the key WORKER_API_KEY names (worker id tour-legacy), "+
		"which no worker-key row holds")

	// (a) The claim's refusals, and the 204 when nothing is queued.
	tr.step("a signed-in user claims: the handler's 403, before the body is decoded", o, claim, jsonBody(`{`))
	tr.step("the box key claims with a body that does not decode", box, claim, jsonBody(`{`))
	tr.step("the box key claims with nothing queued: 204, no body and no Content-Type", box, claim, as("tour-box"),
		note("the body is what the runner's client sends to claim: a map, so its keys are sorted"))

	// (b) Launch: the refusals in the handler's order, then a run in P.
	tr.step("launch an agent W does not have, with a body that does not decode: the lookup comes first", o, launch,
		at("slug", "no-such-agent"), jsonBody(`{`))
	tr.step("launch with a body that does not decode", o, launch, agent, jsonBody(`{`))
	tr.step("launch in P with an empty prompt: the launch's own check, after the project's guard", o, launch, agent,
		jsonBody(`{"project_id":"{{p}}","prompt":""}`))
	tr.step("the member launches in P, where it has no role", m, launch, agent,
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P."}`))
	tr.step("a key of another workspace launches: the agent is looked up in the key's own workspace, which has none",
		elsewhere, launch, agent, jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P."}`))
	tr.step("launch on a card id that is not a UUID: the database's error, passed through (Q19)", o, launch, agent,
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P.","work_item_id":"not-a-card"}`))
	first := tr.step("launch tour-worker in P: 201, queued, attempt 1 of 3, the agent's snapshot, and the board's "+
		"tracking card, which the hooks create inside the request", o, launch, agent,
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P."}`),
		note("agent_content_hash is the agent file's hash (the agent's content_hash); workitem.created's actor is "+
			"the run, and its workspace is filled in from the project by the bus"))
	first.capture("run1", "/id")
	first.capture("run1.card", "/work_item_id")
	tr.step("run1 is queued; a claim for another provider only: 204", box, claim, claimBody("tour-box", "codex-cli"))
	tr.step("a claim naming no provider: 204, an empty list matches none", box, claim, claimBody("tour-box"))
	tr.step("a claim for priority 10 and up: 204, a manual launch is priority 0", box, claim,
		claimAbove("tour-box", 10, tourDefaultProvider))
	tr.step("the member's runner key claims: 204, a personal key takes only its member's runs and ownerless ones",
		runner, claim, as("tour-runner"))

	// (c) The claim (I12).
	claimed := tr.step("the box key claims run1: the agent, how it authenticates, the run and a run token, keys "+
		"sorted (a Go map), auth an object", box, claim, as("tour-box"),
		note("heartbeat_at is the database's NOW(); the run token is handed out here only, freshly minted"))
	run1 := claimed.claimed("run1").runToken("run1", "run1's token, from its claim: what the runner hands the agent's "+
		"tools")

	// (d) Start.
	tr.step("run1's token on the claim route: the handler's 403, a run token is no worker", run1, claim,
		as("tour-run1"))
	tr.step("a key of another workspace starts run1: 404, as if it did not exist", elsewhere, start, run("run1"))
	tr.step("the member's runner key starts run1, which the owner launched: 404 too", runner, start, run("run1"))
	tr.step("run1's token starts run1: the handler's 403", run1, start, run("run1"))
	tr.step("the box key starts run1, with no body as a runner sends it: 204, and the card moves again", box, start,
		run("run1"))
	tr.step("start run1 again: 409", box, start, run("run1"))
	tr.step("start a run no one has: 404", box, start, run("phantom"))

	// (e) Both log shapes.
	tr.keep(workerWireLogTime, "the time the tour's first log entry carries, which the server stores as sent")
	tr.step("push a log batch as the bare array older runners send: 200, whether a cancel is requested", box, logs,
		run("run1"), jsonBody(`[{"seq":1,"kind":"text","payload":{"text":"Reading P."},"created_at":"`+
			workerWireLogTime+`"}]`))
	tr.step("push the object runners send now: an entry with no created_at, one with no payload either, and the "+
		"answer so far", box, logs, run("run1"), jsonBody(`{"entries":[{"seq":2,"kind":"tool_call","payload":`+
		`{"name":"get_artifact","input":{"id":"R-1"}}},{"seq":3,"kind":"system"}],"partial_text":"P holds"}`))
	tr.step("run1: running, with the partial text and the log push's heartbeat", o, get, run("run1"))
	tr.step("push null: a heartbeat only", box, logs, run("run1"), jsonBody(`null`))
	tr.step("push {}: a heartbeat only, the partial text unchanged", box, logs, run("run1"), jsonBody(`{}`))
	tr.step("push a body that does not decode", box, logs, run("run1"), jsonBody(`{`))
	tr.step("push seq 1 again with other text: 200, and the first write stands", box, logs, run("run1"),
		jsonBody(`[{"seq":1,"kind":"text","payload":{"text":"Overwritten?"}}]`))
	tr.step("run1's log, in seq order: the first entry as the tour dated it, the second stamped by the server, the "+
		"third's missing payload read back as {}, not null", o, readLogs, run("run1"))
	tr.step("run1's log after seq 1", o, readLogs, run("run1"), query("after_seq=1"))
	tr.step("run1's log after seq abc: read as 0, the whole log", o, readLogs, run("run1"), query("after_seq=abc"))
	tr.step("the box key reads run1's log: the handler's 401, a key is no user", box, readLogs, run("run1"))
	tr.step("the member reads run1's log: P's guard", m, readLogs, run("run1"))

	// (f) The stream's replay (a live broadcast is S6's).
	tr.step("stream run1: each log frame in seq order, the payload-less entry's payload {}, then the status", o,
		stream, run("run1"), eventStream(4))
	tr.step("stream run1 after seq 1", o, stream, run("run1"), query("after_seq=1"), eventStream(3))
	tr.step("stream a run no one has: a JSON 404, not a stream", o, stream, run("phantom"), eventStream(0))
	tr.step("the box key streams run1: the handler's 401", box, stream, run("run1"), eventStream(0))

	// (g) A cooperative cancel, then finish.
	tr.step("the member cancels run1: P's guard", m, cancel, run("run1"))
	tr.step("the owner cancels run1 while it runs: 200, cancel_requested, still running", o, cancel, run("run1"))
	tr.step("the next log push hears it", box, logs, run("run1"), jsonBody(`{"entries":[],"partial_text":""}`))
	tr.step("the box key finishes run1 as cancelled: agentrun.finished, as the run", box, finish, run("run1"),
		jsonBody(`{"status":"cancelled","error":"cancelled on request"}`))
	tr.step("finish run1 again: 409", box, finish, run("run1"), jsonBody(`{"status":"succeeded"}`))
	tr.step("finish run1 with a status no run has: 409, before the run is read", box, finish, run("run1"),
		jsonBody(`{"status":"bogus"}`))
	tr.step("finish with a body that does not decode", box, finish, run("run1"), jsonBody(`{`))
	tr.step("run1's token, revoked by the finish: the middleware's 401", run1, claim, as("tour-run1"))
	tr.step("cancel the cancelled run1: 200, the run as it is", o, cancel, run("run1"))
	tr.step("the member retries run1: P's guard", m, retry, run("run1"))
	tr.step("retry a run no one has: 404", o, retry, run("phantom"))
	again := tr.step("retry run1: 201, a new run with its agent, project and prompt, launched by the owner, and a "+
		"card of its own", o, retry, run("run1"))
	again.capture("retry1", "/id")
	again.capture("retry1.card", "/work_item_id")

	// (h) A queued run.
	tr.step("start retry1 while it is queued: 409", box, start, run("retry1"))
	tr.step("stream retry1: no log, so the status frame alone", o, stream, run("retry1"), eventStream(1))
	tr.step("retry1's log: [], never null", o, readLogs, run("retry1"))
	tr.step("cancel retry1 while it is queued: cancelled at once; workitem.moved and no agentrun.finished (Q10)", o,
		cancel, run("retry1"), note("finished_at is the database's NOW(); the run's token is revoked with it"))

	// (i) Release (I12).
	tr.queueRun("run2", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Summarise P again."}`).
		capture("run2.card", "/work_item_id")
	run2 := tr.takeRun(box, "tour-box", "run2", "run2's token from its first claim, kept by the release")
	tr.setup("start run2", box, start, run("run2"))
	tr.setup("run2 reports its answer so far", box, logs, run("run2"), jsonBody(`{"entries":[],"partial_text":"P has"}`))
	tr.step("release run2 with {}: 204, but no worker id matches the claimer's, so nothing is released", box,
		release, run("run2"), jsonBody(`{}`))
	tr.step("run2: still running, still tour-box's, with its answer so far", o, get, run("run2"))
	tr.step("release run2 with no body: 400, the worker id is required", box, release, run("run2"))
	tr.step("release run2 as tour-box, as a runner shutting down sends it: 204, queued again", box, release,
		run("run2"), jsonBody(`{"worker_id":"tour-box"}`))
	tr.step("run2: queued, with no worker, heartbeat, start or answer so far", o, get, run("run2"))
	tr.step("run2's first token, revoked by the release: the middleware's 401", run2, claim, as("tour-run2"))
	reclaim := tr.step("the box key claims run2 again: the same run, a new token", box, claim, as("tour-box"))
	reclaim.claimed("run2").runToken("run2.again", "run2's token from its second claim")
	tr.step("run2's first token after the new claim: still the middleware's 401", run2, claim, as("tour-run2"))

	// (j) Finish, and a retry of a success.
	tr.setup("start run2 again", box, start, run("run2"))
	tr.step("finish run2 as succeeded, with its exit code, answer, tokens and cost", box, finish, run("run2"),
		jsonBody(`{"status":"succeeded","exit_code":0,"final_text":"P holds two requirements.","tokens_in":1200,`+
			`"tokens_out":340,"cost_usd":0.0125}`))
	tr.step("retry run2: 409, only a failed, cancelled or timed-out run is retried", o, retry, run("run2"))
	tr.step("launch on a card id no card has: 201, stored as sent (no foreign key), and no card is created or moved",
		o, launch, agent, jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P for card zero.",`+
			`"work_item_id":"{{phantom}}"}`)).capture("run3", "/id")
	tr.step("finish run3, which no worker claimed: 409, only a run a worker holds finishes", box, finish, run("run3"),
		jsonBody(`{"status":"succeeded","final_text":"Nothing to do."}`))
	tr.setup("cancel run3, still queued, so that no later claim takes it", o, cancel, run("run3"))

	// Launches by a key and by a run's token: the handler has no user check.
	tr.step("the box key launches with no project: 201, in the key's workspace, with no launched_by", box, launch,
		agent, jsonBody(`{"prompt":"Tidy W."}`)).capture("run_b", "/id")
	runB := tr.takeRun(runner, "tour-runner", "run_b", "run_b's token, a run with no project that the member's "+
		"runner key claimed, since no one launched it")
	tr.step("the member reads run_b, unscoped and not its own: the workspace admin guard", m, get, run("run_b"))
	tr.step("run_b's token launches in P: 403, the run is not scoped to P", runB, launch, agent,
		jsonBody(`{"project_id":"{{p}}","prompt":"Help."}`))
	tr.step("run_b's token launches with no project: 201, in the run's workspace, with no launched_by and run_b as "+
		"its parent", runB, launch, agent, jsonBody(`{"prompt":"Help with W."}`)).capture("run_t", "/id")
	tr.setup("cancel run_t, so that no later claim takes it", o, cancel, run("run_t"))

	// (k) The auto-retry and its backoff.
	tr.queueRun("run4", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Summarise P, flakily."}`).
		capture("run4.card", "/work_item_id")
	tr.takeRun(box, "tour-box", "run4", "run4's token")
	tr.step("finish run4 as failed with worker_error, a retryable class: attempt 2 is queued with a card of its own",
		box, finish, run("run4"), jsonBody(`{"status":"failed","error":"the provider hung up",`+
			`"error_class":"worker_error"}`))
	queued := tr.step("W's queued runs: attempt 2 alone, retried from run4, claimable from next_attempt_at", o, list,
		query("status=queued"), note("next_attempt_at is written by the name it was captured under, since a time "+
			"30 s ahead can fall inside a slow run of the area"))
	queued.capture("retry4", "/0/id")
	queued.capture("retry4.card", "/0/work_item_id")
	queued.capture("retry4.next_attempt_at", "/0/next_attempt_at")
	queued.noteSeconds("/0/next_attempt_at", "/0/created_at")
	tr.step("the box key claims during the backoff: 204", box, claim, as("tour-box"))
	manual := tr.step("retry run4 by hand: 201, attempt 1 of a chain of its own, claimable at once", o, retry,
		run("run4"))
	manual.capture("retry4m", "/id")
	manual.capture("retry4m.card", "/work_item_id")
	tr.takeRun(box, "tour-box", "retry4m", "the manual retry's token")
	tr.step("finish it as failed with agent_error, a class that is not retried", box, finish, run("retry4m"),
		jsonBody(`{"status":"failed","error":"the agent gave up","error_class":"agent_error"}`))
	tr.step("W's queued runs: still attempt 2 of run4 alone", o, list, query("status=queued"))
	tr.setup("cancel attempt 2 before its backoff elapses, so that no later claim takes it", o, cancel, run("retry4"))

	// (l) A personal runner's reservation.
	tr.step("the member's runner key polls: 204, and the member's runner counts as online for 30 s", runner, claim,
		as("tour-runner"))
	reserved := tr.step("the member launches with no project, which any member may: reserved for its runner "+
		"(preferred_user_id) until hosted_after", m, launch, agent, jsonBody(`{"prompt":"Summarise my week."}`),
		note("hosted_after is <time>, since it is to come"))
	reserved.capture("run5", "/id")
	reserved.noteSeconds("/hosted_after", "/created_at")
	tr.step("the box key claims: 204, run5 is reserved", box, claim, as("tour-box"))
	tr.step("the member's runner key claims run5", runner, claim, as("tour-runner")).claimed("run5")
	tr.step("the member reads run5, which it launched", m, get, run("run5"))
	tr.step("the member's runner key finishes run5: its member's run", runner, finish, run("run5"),
		jsonBody(`{"status":"succeeded","final_text":"Quiet week."}`))

	// (m) A project on the workspace's API key.
	tr.queueRun("run_k1", o, "tour-worker", `{"project_id":"{{k}}","prompt":"Summarise K."}`)
	tr.step("the box key claims K's run: auth api-key, with the provider's own variable", box, claim,
		as("tour-box")).claimed("run_k1")
	tr.setup("W's claude-code setting names GOOGLE_API_KEY", o, "PUT /api/v1/provider-settings",
		jsonBody(`{"provider":"claude-code","auth_mode":"api-key","api_key_env":"GOOGLE_API_KEY","enabled":true}`))
	tr.queueRun("run_k2", o, "tour-worker", `{"project_id":"{{k}}","prompt":"Summarise K again."}`)
	tr.step("the box key claims K's next run: the variable W's provider setting names", box, claim,
		as("tour-box")).claimed("run_k2")

	// (n) The hosted skip.
	tr.queueRun("run_r", o, "tour-repo", `{"project_id":"{{p}}","prompt":"Fix the build."}`)
	tr.queueRun("run_h", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Summarise P, hosted."}`)
	tr.step("a hosted claim: it skips the older run, whose agent has repository access", box, claim,
		hostedClaimBody("tour-hosted", tourDefaultProvider)).claimed("run_h")
	tr.step("a plain claim takes the repository run", box, claim, as("tour-box")).claimed("run_r")

	// (o) The legacy key.
	tr.step("the WORKER_API_KEY key claims: 204; it resolves to the earliest account's personal workspace, admin's, "+
		"where nothing is queued", legacy, claim, as("tour-legacy"))
	tr.setup("admin's own tour-worker, in its personal workspace", tr.admin, "POST /api/v1/agents",
		jsonBody(workerWireAgent("tour-worker", "Tour Worker", false))).capture("admin.agent", "/id")
	tr.queueRun("run_a", tr.admin, "tour-worker", `{"prompt":"Summarise the platform."}`)
	tr.step("the WORKER_API_KEY key claims admin's run", legacy, claim, as("tour-legacy")).claimed("run_a")
	tr.step("the WORKER_API_KEY key starts a run of W: 404", legacy, start, run("run_r"))
	tr.step("admin's worker keys: null (Q14); with no workspace at boot, WORKER_API_KEY was never registered as one",
		tr.admin, "GET /api/v1/orgs/{id}/worker-keys", at("id", "{{admin.workspace}}"),
		note("an S5c route, read to pin what the server's boot left behind"))

	// (p) The reads.
	tr.step("the member reads run1, the owner's run in P: P's guard", m, get, run("run1"))
	tr.step("a run no one has: 404", o, get, run("phantom"))
	tr.step("the id \"claim\": the GET matches {id} (the claim route is a POST), and no run has that id: 404", o, get,
		at("id", "claim"))
	tr.step("the box key reads run1: the handler's 401", box, get, run("run1"))
	tr.step("the member's runs, with no filter: only those it launched", m, list)
	tr.step("the member's queued runs: none, null (Q14)", m, list, query("status=queued"))
	tr.step("the member lists P's runs: P's guard", m, list, query("project_id={{p}}"))
	tr.step("K's runs, newest first", o, list, query("project_id={{k}}"))
	tr.step("tour-repo's runs", o, list, query("agent_id={{repo.agent}}"))
	tr.step("agent_id x: 500, the cast to a UUID fails in SQL", o, list, query("agent_id=x"))
	tr.step("every run of W, as its admin: newest first, compressed and then sent with no Content-Type (Q1)", o, list)
}
