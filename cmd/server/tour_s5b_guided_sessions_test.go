//go:build unix

package main

import "testing"

// TestTourS5bGuidedSessions is the S5b tour's guided sessions area (refactor
// plan §6.4 S5b, before M8 splits suite_handlers.go into guided_ and
// guided_copilot_handlers.go; invariants I3 (decode, lookup and guard order),
// I4, I5, I9 (the copilot stream's head and frames), I10 and I16 (the guided
// answers keys); quirks Q1, Q14 and Q19; OpenV REQ-13, REQ-23, REQ-143). Its
// golden is testdata/tour/s5b/guided_sessions.json.
//
// The owner, in its personal (nightly) workspace, keeps project P: one
// heading holding a requirement and a user need, which the copilot's prompt
// lists as the project outline, and a product profile whose vision, problem
// statement and target users the prompt quotes. A viewer of P shows the
// role gates, and an outsider, at the end, the refusals of the reads. The
// area walks a session's life: started (the body is decoded before the
// project guard), read and listed; its steps saved (answers merge at the top
// level and are written with sorted keys, numbers through float64); drafts
// materialised (status draft, origin guided-flow unless the draft gives one,
// any type accepted; a link the drafts name is created unchecked, or skipped
// when the store refuses it; a batch that fails part way leaves the drafts
// before the failure in the project but not in the session); the copilot
// chat (kickoff, messages, nudges and the stream's replay); the commit (each
// draft gets a new version meant to approve it, which the status column
// overrides, so it stays a draft, and an automatic note; any parked nudge is
// cleared); and the abandon. Neither a message, a nudge nor the abandon
// checks that the session is still in progress. A second session shows a
// nudge that launches a turn, a message naming as the artifact on screen a
// requirement of another project of P's workspace, which its prompt leaves
// out, and an abandon of a session in progress. In a second workspace whose
// requirements-copilot agent has been deleted, every launch fails: kickoff
// and a message leave a system note in the transcript, a nudge none.
//
// No worker key exists in this area on purpose: runner_online is true only
// while some key of the project's workspace was used within the last 30 s
// (workerOnlineWindow), which would make the answers depend on the clock. So
// every copilot turn stays queued (HOSTED_RUNNERS=off and nothing claims it;
// the reaper fails only claimed or running runs that went silent), and
// kickoff and nudge answer "pending" while one is. The turns' prompts
// (buildGuidedCopilotPrompt and projectChangeRules, which M8 moves) are read
// through GET /api/v1/agent-runs/{id}, an S5d route.
//
// Nondeterminism: only ids and minted times. The chat messages of a session
// are ordered by created_at, each stamped by its own statement; the sessions
// of a project by created_at, each from its own request; the prompt's outline
// by sort order and then title, and every title differs.
func TestTourS5bGuidedSessions(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "guided_sessions",
		about: "Guided product-definition sessions (start, read, list, save a step, materialise drafts, commit, " +
			"abandon) and their V&V Assistant chat (kickoff, messages, nudges, the event stream), with the prompts " +
			"the turns are launched with, and the chat of a workspace whose copilot agent is gone.",
		run: guidedSessionsTour,
	})
}

// guidedSessionsSeed creates project P, its artifacts and profile, and the
// viewer (setup: the artifacts and profile routes are pinned elsewhere).
func guidedSessionsSeed(tr *tour) (viewer *tourActor) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour guided"}`)).capture("p", "/id")
	tr.setup("P's heading, the one parent of its artifacts", o, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{p}}","type":"heading","title":"Machine definition"}`)).capture("heading", "/id")
	tr.setup("a user need, which the drafts derive from", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{heading}}","type":"user-need","title":"Operators stay clear of the spindle",`+
		`"body":"Operators need to stay clear of the spindle while it runs."}`)).capture("need", "/id")
	tr.setup("a requirement", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{heading}}","type":"requirement","title":"Guard interlock",`+
		`"body":"The system shall stop the spindle within 1 s of the guard opening.",`+
		`"attributes":{"verification_method":"test"}}`)).capture("req", "/id")
	tr.setup("P's product profile, which the prompts quote", o, "PUT /api/v1/projects/{id}/profile", at("id", "{{p}}"),
		jsonBody(`{"vision":"A desktop mill that is safe to leave running","problem_statement":"Hobby mills injure `+
			`hands","target_users":"Makers & small workshops"}`))
	viewer = tr.register("viewer", "Tour Viewer", "a viewer of P, from a workspace of its own: may read, not write")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	return viewer
}

func guidedSessionsTour(tr *tour) {
	o := tr.owner
	viewer := guidedSessionsSeed(tr)

	// Starting and reading a session: the body is decoded before the
	// project guard, and the session lookup comes before the guard.
	tr.step("list sessions with no project_id", o, "GET /api/v1/guided-sessions",
		note("the project guard answers an empty project id as not found"))
	tr.step("P's sessions before any: null (Q14)", o, "GET /api/v1/guided-sessions", query("project_id={{p}}"))
	tr.step("the sessions of a project that does not exist", o, "GET /api/v1/guided-sessions",
		query("project_id={{phantom}}"))
	tr.step("start a session with a malformed body naming a project that does not exist", o,
		"POST /api/v1/guided-sessions", jsonBody(`{"project_id":"{{phantom}}"`),
		note("the body is decoded before the project guard, so a malformed body answers 400 whatever it names"))
	tr.step("start a session with no project_id", o, "POST /api/v1/guided-sessions", jsonBody(`{}`))
	tr.step("start a session on a project that does not exist", o, "POST /api/v1/guided-sessions",
		jsonBody(`{"project_id":"{{phantom}}"}`), note("the project guard runs before any lookup: 403, not 404"))
	tr.step("the viewer starts a session", viewer, "POST /api/v1/guided-sessions", jsonBody(`{"project_id":"{{p}}"}`))
	tr.step("start session S on P: step 0, answers {} and draft_artifact_ids []", o, "POST /api/v1/guided-sessions",
		jsonBody(`{"project_id":"{{p}}"}`)).capture("s", "/id")
	tr.step("read S", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))
	tr.step("the viewer reads S", viewer, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))
	tr.step("read a session that does not exist", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{phantom}}"))
	tr.step("read a session by an id that is not a UUID", o, "GET /api/v1/guided-sessions/{id}",
		at("id", "not-a-uuid"), note("the lookup's driver error is answered as not found, like an id no session has"))
	tr.step("the viewer lists P's sessions", viewer, "GET /api/v1/guided-sessions", query("project_id={{p}}"))

	// Saving steps: the answers merge at the top level.
	tr.step("the viewer saves a step", viewer, "PUT /api/v1/guided-sessions/{id}/step", at("id", "{{s}}"),
		jsonBody(`{"step":1,"answers":{"step_1":{}}}`))
	tr.step("save a step with a malformed body", o, "PUT /api/v1/guided-sessions/{id}/step", at("id", "{{s}}"),
		jsonBody(`{"step":`))
	tr.step("save a step of a session that does not exist", o, "PUT /api/v1/guided-sessions/{id}/step",
		at("id", "{{phantom}}"), jsonBody(`{"step":1,"answers":{}}`))
	tr.step("save step 1: the answers come back with sorted keys, HTML escaped, numbers through float64", o,
		"PUT /api/v1/guided-sessions/{id}/step", at("id", "{{s}}"),
		jsonBody(`{"step":1,"answers":{"step_1":{"vision":"A mill <safe> & quiet","problem_statement":"Hands",`+
			`"target_users":"Makers"},"budget_eur":1250.50,"serial":12345678901234567890}}`),
		note("the answers decode into interface{}: 1250.50 is written 1250.5 and 12345678901234567890 as "+
			"12345678901234567000, and JSONB keeps what was written"))
	tr.step("save step 2: step_1 and the other keys stay, step_2 is added and current_step moves", o,
		"PUT /api/v1/guided-sessions/{id}/step", at("id", "{{s}}"),
		jsonBody(`{"step":2,"answers":{"step_2":{"personas":[{"id":"p-1","name":"Maker Mo"}]},"budget_eur":900}}`))
	tr.step("save step 3 with no answers: only current_step moves", o, "PUT /api/v1/guided-sessions/{id}/step",
		at("id", "{{s}}"), jsonBody(`{"step":3}`))
	tr.step("read S back: the answers as stored", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))

	// Drafts.
	tr.step("the viewer materialises drafts", viewer, "POST /api/v1/guided-sessions/{id}/drafts", at("id", "{{s}}"),
		jsonBody(`{"drafts":[]}`))
	tr.step("materialise drafts with a malformed body", o, "POST /api/v1/guided-sessions/{id}/drafts",
		at("id", "{{s}}"), jsonBody(`{"drafts":{}}`))
	tr.step("materialise drafts in a session that does not exist", o, "POST /api/v1/guided-sessions/{id}/drafts",
		at("id", "{{phantom}}"), jsonBody(`{"drafts":[]}`))
	tr.step("a body with no drafts: artifact_ids [], never null", o, "POST /api/v1/guided-sessions/{id}/drafts",
		at("id", "{{s}}"), jsonBody(`{}`))
	drafts := tr.step("materialise a requirement with three links and a hazard with its own origin and status", o,
		"POST /api/v1/guided-sessions/{id}/drafts", at("id", "{{s}}"),
		jsonBody(`{"drafts":[{"type":"requirement","title":"Warn before the spindle starts",`+
			`"body":"The system shall sound a warning 2 s before the spindle starts.","parent_id":"{{heading}}",`+
			`"attributes":{"verification_method":"test"},"links":[{"type":"derives-from","to_id":"{{need}}"},`+
			`{"type":"derives-from","to_id":"{{phantom}}"},{"type":"derives-from","to_id":"not-a-uuid"}]},`+
			`{"type":"hazard","title":"Spindle starts with the guard open","body":"Hand injury.",`+
			`"parent_id":"{{heading}}","attributes":{"severity":"critical","origin":"assistant","status":"approved"}}]}`),
		note("the drafts' links are written with no check, so the one to an id no artifact has is created too, and "+
			"one the store refuses (a to_id that is not a UUID) is skipped: nothing in the answer says so; and "+
			"neither the drafts nor their links publish an event"))
	drafts.capture("draft_req", "/artifact_ids/0")
	drafts.capture("draft_hazard", "/artifact_ids/1")
	tr.step("the drafted hazard: status draft whatever the draft said, and the origin it gave kept", o,
		"GET /api/v1/artifacts/{id}", at("id", "{{draft_hazard}}"),
		note("an S5a route, read to show what a draft is created as; origin is guided-flow only when a draft "+
			"gives none"))
	tr.step("the drafted requirement's links: the one to the user need and the one to an id no artifact has", o,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{draft_req}}"),
		note("an S5a route, read to show the links the drafts created, newest first"))
	tr.step("materialise a draft of a type no artifact has: accepted as it is, nothing checks the type", o,
		"POST /api/v1/guided-sessions/{id}/drafts", at("id", "{{s}}"),
		jsonBody(`{"drafts":[{"type":"widget","title":"Not a type","parent_id":"{{heading}}"}]}`)).
		capture("draft_widget", "/artifact_ids/0")
	tr.step("materialise a valid draft and one whose parent_id is not a UUID: 400, the driver's text (Q19)", o,
		"POST /api/v1/guided-sessions/{id}/drafts", at("id", "{{s}}"),
		jsonBody(`{"drafts":[{"type":"requirement","title":"Brake within 1 s","body":"The system shall brake.",`+
			`"parent_id":"{{heading}}"},{"type":"requirement","title":"Misplaced","parent_id":"not-a-uuid"}]}`),
		note("the drafts are created one by one, and the first failure answers 400 with its text before the "+
			"session is updated: the draft created before it stays in the project, and the session does not "+
			"list it"))
	tr.step("S's draft_artifact_ids: the three drafts, not the one the failed batch left", o,
		"GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))
	tr.step("P's requirements: the draft the failed batch left is in the project, a draft", o,
		"GET /api/v1/artifacts", query("project_id={{p}}&type=requirement"),
		note("an S5a route, read to show what the failed batch left")).
		captureWhere("orphan", "", "title", "Brake within 1 s", "id")

	// The copilot chat. No runner key exists, so runner_online is false and
	// every turn launched stays queued.
	tr.step("S's transcript before any message: [], never null", o, "GET /api/v1/guided-sessions/{id}/messages",
		at("id", "{{s}}"))
	tr.step("the viewer reads S's transcript", viewer, "GET /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"))
	tr.step("the transcript of a session that does not exist", o, "GET /api/v1/guided-sessions/{id}/messages",
		at("id", "{{phantom}}"))
	tr.step("S's chat stream with nothing to replay: the head alone", o, "GET /api/v1/guided-sessions/{id}/chat/stream",
		at("id", "{{s}}"), eventStream(0))
	tr.step("the viewer kicks off S's chat", viewer, "POST /api/v1/guided-sessions/{id}/chat/kickoff",
		at("id", "{{s}}"), jsonBody(`{"step":3}`))
	tr.step("kick off with a malformed body", o, "POST /api/v1/guided-sessions/{id}/chat/kickoff", at("id", "{{s}}"),
		jsonBody(`{"step":"3"}`))
	tr.step("kick off a session that does not exist", o, "POST /api/v1/guided-sessions/{id}/chat/kickoff",
		at("id", "{{phantom}}"), jsonBody(`{}`))
	tr.step("kick off S's empty chat on wizard step 3: launched, and no runner online", o,
		"POST /api/v1/guided-sessions/{id}/chat/kickoff", at("id", "{{s}}"),
		jsonBody(`{"step":3,"state":{"step_3":{"needs":[{"id":"n-1","capability":"Stay clear"}]}}}`),
		note("the launch publishes no event"))
	tr.step("S carries the kickoff's run in agent_run_id", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}")).
		capture("run_kickoff", "/agent_run_id")
	tr.step("the kickoff's run: queued, its prompt beside the wizard (step, state, outline, an opening turn)", o,
		"GET /api/v1/agent-runs/{id}", at("id", "{{run_kickoff}}"),
		note("an S5d route, read to pin buildGuidedCopilotPrompt and projectChangeRules, which M8 moves"))
	tr.step("kick off again while that run is queued: pending", o, "POST /api/v1/guided-sessions/{id}/chat/kickoff",
		at("id", "{{s}}"), jsonBody(`{"step":3}`))

	// Nudges while a turn is in flight are parked on the session.
	tr.step("the viewer nudges S's chat", viewer, "POST /api/v1/guided-sessions/{id}/chat/nudge", at("id", "{{s}}"),
		jsonBody(`{"step":3,"event":"saved step 3"}`))
	tr.step("nudge with a malformed body", o, "POST /api/v1/guided-sessions/{id}/chat/nudge", at("id", "{{s}}"),
		jsonBody(`{"event":3}`))
	tr.step("nudge a session that does not exist", o, "POST /api/v1/guided-sessions/{id}/chat/nudge",
		at("id", "{{phantom}}"), jsonBody(`{}`))
	tr.step("nudge while the kickoff's run is queued: pending, the nudge parked", o,
		"POST /api/v1/guided-sessions/{id}/chat/nudge", at("id", "{{s}}"),
		jsonBody(`{"step":3,"state":{"step_3":{"needs":[]}},"event":"saved step 3"}`))
	tr.step("S's pending_nudge: step, state and event", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))
	tr.step("nudge again with a blank event and no state: the parked nudge is replaced", o,
		"POST /api/v1/guided-sessions/{id}/chat/nudge", at("id", "{{s}}"), jsonBody(`{"step":4,"event":"  "}`))
	tr.step("S's pending_nudge: the newest, its blank event written \"updated the wizard\", no state", o,
		"GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))

	// Messages: each launches a turn, with no in-flight check.
	tr.step("the viewer posts a message", viewer, "POST /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"),
		jsonBody(`{"content":"Hello"}`))
	tr.step("post a message with a malformed body", o, "POST /api/v1/guided-sessions/{id}/messages",
		at("id", "{{s}}"), jsonBody(`{"content":`))
	tr.step("post a message with blank content", o, "POST /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"),
		jsonBody(`{"content":" \n "}`))
	tr.step("post a message to a session that does not exist", o, "POST /api/v1/guided-sessions/{id}/messages",
		at("id", "{{phantom}}"), jsonBody(`{"content":"Hello"}`))
	tr.step("post a message about the requirement on screen: the message and runner_online", o,
		"POST /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"),
		jsonBody(`{"content":"Is 1 s <fast> enough & testable?","artifact_id":"{{req}}"}`),
		note("it launches another turn although the kickoff's is still queued; no event is published"))
	tr.step("S's agent_run_id is the message's run now", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}")).
		capture("run_message", "/agent_run_id")
	tr.step("the message's run: its prompt beside the project (the artifact on screen, the transcript)", o,
		"GET /api/v1/agent-runs/{id}", at("id", "{{run_message}}"), note("an S5d route, read to pin the prompt"))
	tr.step("kick off once the chat holds a message: skipped", o, "POST /api/v1/guided-sessions/{id}/chat/kickoff",
		at("id", "{{s}}"), jsonBody(`{"step":4}`))
	tr.step("S's transcript: the message", o, "GET /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"))
	tr.step("S's chat stream: one frame per message, replayed", o, "GET /api/v1/guided-sessions/{id}/chat/stream",
		at("id", "{{s}}"), eventStream(1))
	tr.step("the viewer's chat stream", viewer, "GET /api/v1/guided-sessions/{id}/chat/stream", at("id", "{{s}}"),
		eventStream(1))
	tr.step("the chat stream asked for with gzip: not compressed", o, "GET /api/v1/guided-sessions/{id}/chat/stream",
		at("id", "{{s}}"), withHeader("Accept-Encoding", "gzip"), eventStream(1),
		note("the compressor leaves text/event-stream alone: no Content-Encoding"))
	tr.step("the chat stream of a session that does not exist", o, "GET /api/v1/guided-sessions/{id}/chat/stream",
		at("id", "{{phantom}}"), eventStream(0))

	// Commit: the drafts are written again, with a note each, and the parked
	// nudge is cleared.
	tr.step("the viewer commits S", viewer, "POST /api/v1/guided-sessions/{id}/commit", at("id", "{{s}}"))
	tr.step("commit a session that does not exist", o, "POST /api/v1/guided-sessions/{id}/commit",
		at("id", "{{phantom}}"))
	tr.step("commit S: committed, the parked nudge cleared", o, "POST /api/v1/guided-sessions/{id}/commit",
		at("id", "{{s}}"), note("the drafts' new versions and notes publish no event"))
	tr.step("P's requirements after the commit: the drafted one at version 2 but still a draft, the failed "+
		"batch's untouched", o, "GET /api/v1/artifacts", query("project_id={{p}}&type=requirement"),
		note("an S5a route. The commit writes each draft's attributes with status approved, but UpdateArtifact "+
			"rewrites that attribute from the status column, so the drafts stay drafts: a new version and the "+
			"automatic note are all the commit leaves (a bug, pinned as it is)"))
	tr.step("the drafted requirement's notes: the commit's automatic note", o, "GET /api/v1/chatter",
		query("artifact_id={{draft_req}}"), note("an S5a route, read to show what the commit writes beside a draft"))
	tr.step("commit S again", o, "POST /api/v1/guided-sessions/{id}/commit", at("id", "{{s}}"))
	tr.step("save a step of the committed S", o, "PUT /api/v1/guided-sessions/{id}/step", at("id", "{{s}}"),
		jsonBody(`{"step":8,"answers":{}}`))
	tr.step("materialise drafts in the committed S", o, "POST /api/v1/guided-sessions/{id}/drafts", at("id", "{{s}}"),
		jsonBody(`{"drafts":[]}`))
	tr.step("post a message to the committed S: accepted, and a turn launched (no status check)", o,
		"POST /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"), jsonBody(`{"content":"One more thing"}`))
	tr.step("S's agent_run_id: the turn launched after the commit", o, "GET /api/v1/guided-sessions/{id}",
		at("id", "{{s}}")).capture("run_after_commit", "/agent_run_id")
	tr.step("nudge the committed S while that turn is queued: pending, parked on a closed session", o,
		"POST /api/v1/guided-sessions/{id}/chat/nudge", at("id", "{{s}}"), jsonBody(`{"step":8,"event":"committed"}`))
	tr.step("the committed S with a pending_nudge", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))
	tr.step("the viewer abandons S", viewer, "POST /api/v1/guided-sessions/{id}/abandon", at("id", "{{s}}"))
	tr.step("abandon a session that does not exist", o, "POST /api/v1/guided-sessions/{id}/abandon",
		at("id", "{{phantom}}"))
	tr.step("abandon the committed S: abandoned (no status check), the parked nudge cleared", o,
		"POST /api/v1/guided-sessions/{id}/abandon", at("id", "{{s}}"))
	tr.step("read S after the abandon", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}"))

	// A second session: a nudge with no turn in flight launches one.
	tr.step("start session T on P", o, "POST /api/v1/guided-sessions", jsonBody(`{"project_id":"{{p}}"}`)).
		capture("t", "/id")
	tr.step("nudge T with no turn in flight: launched", o, "POST /api/v1/guided-sessions/{id}/chat/nudge",
		at("id", "{{t}}"), jsonBody(`{"step":2,"state":{"step_2":{"personas":[{"id":"p-1","name":"Maker Mo"}]}},`+
			`"event":"saved step 2"}`))
	tr.step("T carries the nudge's run, and no pending_nudge", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{t}}")).
		capture("run_nudge", "/agent_run_id")
	tr.step("the nudge's run: its prompt with the event", o, "GET /api/v1/agent-runs/{id}", at("id", "{{run_nudge}}"),
		note("an S5d route, read to pin the prompt"))
	// The artifact on screen goes into the prompt only when it is the
	// session's project's (launchGuidedTurn): nothing checks the caller's
	// rights on artifact_id, so that comparison alone keeps another
	// project's artifact out of a prompt P's members read. Q is the
	// owner's, in P's workspace, and P's viewer cannot read it: the caller
	// can read Q's requirement and it is in the session's workspace, so a
	// weaker check (a role of the caller's on the artifact's project, or the
	// artifact in the session's workspace) would let it in, and this step
	// tells those from the comparison. The copilot prompt's unit test does
	// not see how the artifact is chosen, so O2's rewrite of
	// launchGuidedTurn relies on this step.
	tr.setup("the owner's project Q, in P's workspace", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour guided, another of the workspace's"}`)).capture("q", "/id")
	tr.setup("a requirement of Q, which P's viewer cannot read", o, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{q}}","type":"requirement","title":"Q's own interlock",`+
			`"body":"The system shall keep Q's interlock private."}`)).capture("q_req", "/id")
	tr.step("post a message to T naming Q's requirement as the artifact on screen", o,
		"POST /api/v1/guided-sessions/{id}/messages", at("id", "{{t}}"),
		jsonBody(`{"content":"What about this one?","artifact_id":"{{q_req}}"}`),
		note("the artifact is another project's: accepted, and the turn is launched without it"))
	tr.step("T's agent_run_id is the message's run", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{t}}")).
		capture("run_foreign", "/agent_run_id")
	tr.step("the message's run: no artifact on screen in its prompt, since Q's requirement is not T's project's", o,
		"GET /api/v1/agent-runs/{id}", at("id", "{{run_foreign}}"),
		note("an S5d route, read to pin launchGuidedTurn's same-project check on the artifact on screen"))
	tr.step("abandon T in progress", o, "POST /api/v1/guided-sessions/{id}/abandon", at("id", "{{t}}"))
	tr.step("materialise drafts in the abandoned T", o, "POST /api/v1/guided-sessions/{id}/drafts", at("id", "{{t}}"),
		jsonBody(`{"drafts":[]}`))
	tr.step("P's sessions, newest first", o, "GET /api/v1/guided-sessions", query("project_id={{p}}"))

	// An outsider: each read of a session finds it, then refuses.
	outsider := tr.register("outsider", "Tour Outsider", "an ordinary account in a workspace of its own, with no "+
		"access to P")
	tr.step("the outsider reads S: the session found, then the guard", outsider, "GET /api/v1/guided-sessions/{id}",
		at("id", "{{s}}"))
	tr.step("the outsider reads S's transcript", outsider, "GET /api/v1/guided-sessions/{id}/messages",
		at("id", "{{s}}"))
	tr.step("the outsider opens S's chat stream", outsider, "GET /api/v1/guided-sessions/{id}/chat/stream",
		at("id", "{{s}}"), eventStream(0))

	guidedSessionsUnavailable(tr)
}

// guidedSessionsUnavailable is the chat of a workspace whose
// requirements-copilot agent has been deleted: every launch fails.
func guidedSessionsUnavailable(tr *tour) {
	o := tr.owner
	tr.sharedWorkspace("w2", "Tour no copilot")
	tr.setup("project P2 in w2", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour guided elsewhere"}`)).
		capture("p2", "/id")
	tr.setup("delete w2's requirements-copilot agent (an S5d route)", o, "DELETE /api/v1/agents/{slug}",
		at("slug", "requirements-copilot"), expect(204))
	tr.step("start session U on P2", o, "POST /api/v1/guided-sessions", jsonBody(`{"project_id":"{{p2}}"}`)).
		capture("u", "/id")
	tr.step("kick off U's chat: unavailable, and a system note", o, "POST /api/v1/guided-sessions/{id}/chat/kickoff",
		at("id", "{{u}}"), jsonBody(`{"step":1}`))
	tr.step("post a message to U: accepted, and a second system note", o, "POST /api/v1/guided-sessions/{id}/messages",
		at("id", "{{u}}"), jsonBody(`{"content":"Anyone there?"}`))
	tr.step("nudge U: unavailable, and no note", o, "POST /api/v1/guided-sessions/{id}/chat/nudge", at("id", "{{u}}"),
		jsonBody(`{"step":1,"event":"saved step 1"}`))
	tr.step("U's transcript: the note, the message, the note", o, "GET /api/v1/guided-sessions/{id}/messages",
		at("id", "{{u}}"))
	tr.step("U's chat stream: three frames", o, "GET /api/v1/guided-sessions/{id}/chat/stream", at("id", "{{u}}"),
		eventStream(3))
	tr.step("U has no agent_run_id", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{u}}"))
}
