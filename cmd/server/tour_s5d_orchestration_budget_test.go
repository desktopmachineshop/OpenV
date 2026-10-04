//go:build unix

package main

import (
	"encoding/json"
	"testing"
)

// TestTourS5dOrchestrationBudget is the S5d tour's orchestration and budget
// area (refactor plan §6.4 S5d, before M3, which moves main.go's budget guard
// closure, and M7, which splits agent_handlers.go; invariants I2's
// same-method overlap, I3, I4, I5, I10, I12; quirks Q1, Q9, Q14; OpenV
// REQ-21, REQ-23, REQ-76, REQ-81, REQ-91, REQ-143). Its golden is
// testdata/tour/s5d/orchestration_budget.json.
//
// The tour plays the runner (tour_worker_test.go), so no run reaches a model:
// every run's lifecycle is driven over the worker wire with W's worker key,
// and the golden's empty outbound_requests pins that the server makes no
// provider call of its own. The server boots with OPENV_BUDGET_ENFORCE=true,
// which wires main.go's budget guard (an M3 closure) into the run service.
//
// In the owner's shared workspace W, with m a plain member of W, the area
// builds, as setup through the /crews routes, crew C: Lead (the entry node)
// delegates to Analyst, hands off to Checker (whose edge carries a prompt
// template) and to a human node for m, and is reviewed by Critic; and crew
// C2, whose one node delegates to nobody. Then it walks:
//   - delegation (POST /agent-runs/delegate): a session and a worker key are
//     refused (403: only a run token delegates), a run with no crew node
//     (400), a body that does not decode, a blank prompt, a label no delegate
//     has (the available labels listed, none for C2's run), a run whose crew
//     node was removed (404, the node routes' answer), and the delegation
//     itself, whose label is matched without regard to case: 201 {run_id,
//     status}, and the child inherits the parent's card, which its launch
//     moves back to To Do;
//   - the parent's poll (GET /agent-runs/delegate/{id}): {error, final_text,
//     run_id, status} in a Go map's order, refused to another run's token, a
//     run no one has, and a user;
//   - I2's overlap: GET /agent-runs/delegate/tree, /logs and /stream are
//     answered by the poll (403 for a user, 404 for a run token), which the
//     area's /metrics check proves by counting each under
//     /api/v1/agent-runs/delegate/{id}, not {id}/tree, {id}/logs or
//     {id}/stream (registration order: registerAgentRoutes, in
//     agent_handlers.go, calls registerWorkerDispatchRoutes before
//     registerAgentRunReadRoutes);
//   - the child claimed before an older plain run (priority 10 beats 0),
//     with priority, parent_run_id and team_node_id and no launched_by in its
//     claim bytes; its finish, which the parent's poll then shows; the
//     parent's finish, inside which the run service's synchronous subscriber
//     (the orchestration hooks) launches Checker's run with the edge's
//     template rendered and Critic's with the reviews copy, each successor
//     moving the parent's card back to To Do, and refuses the hand-off to m,
//     who has no role in P and could not open a card there (REQ-23, REQ-81;
//     handoff_reach_test.go pins the rest of who may be handed one): no card
//     is made, the reason is noted at the end of the parent's log and on its
//     card, and m is granted nothing, so m still cannot read the parent's
//     card; the run tree, breadth first, children by created_at; its
//     refusals; and the finished parent's token, revoked (the middleware's
//     401);
//   - the conversational replies S5b left to S5d: the guided copilot's run
//     (priority 20, guided_session_id in its claim bytes) finished with an
//     answer the session's chat then holds, and a failed turn, whose failure
//     text the chat holds instead and which is not retried; the interviewer's
//     run, whose claim bytes carry interview_session_id, the untrusted origin
//     of REQ-91, finished into the session's transcript;
//   - Q9, with W over its budget ($5.00 spent of $1.00): the launch and the
//     test-case drafting answer 402, run-now, a crew launch and a test run's
//     agent run 400, delegation 500, and, beyond Q9's six, a retry 500, all
//     with the guard's text where they show it; a crew run finished over
//     budget launches no agent successor (the tree shows it) and publishes
//     agentrun.successors_skipped, naming them and the budget, with a note on
//     the run saying the same (REQ-76), but still hands off to m, since a
//     card is no run, once the owner has made m a viewer of P (setup); and
//     with the budget cleared a launch, run-now, a crew launch and the retry
//     pass again, so that each refusal above was the guard's. A last step
//     reads the over-budget run's log, which ends with that note.
//
// Every 2xx JSON answer is a bare encode (text/plain by sniffing, and no
// Content-Type once compressed: the run tree, Q1); errors are
// application/json. Nondeterminism: the claim takes the workspace's
// highest-priority, oldest queued run, so the area checks each claim
// (claimed) and cancels the successors it does not claim; successors,
// hand-offs and conversational replies happen inside the finish request, so
// nothing is awaited; the bus subscribers on agentrun.finished (the notifier,
// the budget monitor, the trigger matcher) run after the answer and write
// notifications only, which no step reads; the spend is month to date in
// UTC, so an area that crossed a month boundary would read $0.00. Not
// pinned: the live SSE frames the replies broadcast (S6's), a parked wizard
// nudge's launch on the turn's finish (S5b pins the parking), and the
// automation and crew launch routes' own successes (other S5d areas').
func TestTourS5dOrchestrationBudget(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "orchestration_budget",
		about: "Crew orchestration and the workspace budget: delegation and the parent's poll (with I2's delegate/{id} " +
			"overlap), hand-offs to agents and to a person when a crew run finishes, refused to a person who cannot " +
			"open the project, the run tree, the guided copilot's and the interviewer's replies over the worker wire, " +
			"and Q9's refusals of every launch path once the workspace is over its monthly budget, with the skipped " +
			"successors of a crew run recorded.",
		run: orchestrationBudgetTour,
		env: map[string]string{
			// Wires main.go's budget guard (an M3 closure): launches are
			// refused once W's month-to-date spend reaches its budget.
			"OPENV_BUDGET_ENFORCE": "true",
		},
		accounts: []tourAccount{{name: "member", display: "Tour Member", about: "m, a plain member of W with no role " +
			"in P until the owner makes m a viewer of it before the over-budget finish: the human node of crew C, " +
			"to whom its Lead's hand-off is refused, and then made, and a refusal of the run tree"}},
	})
}

// orchestrationBudgetAgent is an agent definition with a short prompt, so
// that the claim bytes hold the area's own agent rather than a seeded one,
// whose prompt changes with feature work.
func orchestrationBudgetAgent(slug, name, prompt string) string {
	b, _ := json.Marshal(map[string]any{
		"slug": slug, "name": name, "description": "An agent of the S5d tour.", "provider": tourDefaultProvider,
		"allowed_tools": []string{"get_artifact"}, "write_mode": "direct", "repo_access": false,
		"system_prompt": prompt,
	})
	return string(b)
}

// orchestrationBudgetGuidedPrompt keeps the guided copilot's prompt out of the
// golden at pointer: buildGuidedCopilotPrompt's text, which S5b's
// guided_sessions golden pins (it reads the same run) and which feature work
// rewords; here the point is the run around it.
func orchestrationBudgetGuidedPrompt(pointer string) tourOpt {
	return elide(pointer, "<the copilot's prompt>", len(`""`), tourNoMore, "the guided copilot's turn prompt "+
		"(buildGuidedCopilotPrompt), which S5b's guided_sessions golden pins and feature work rewords")
}

// orchestrationBudgetCard registers under name the id of P's card titled
// title that no name holds yet (setup: GET /api/v1/projects/{id}/work-items,
// an S5b route): a hand-off card, which the finish that creates it does not
// answer with.
func orchestrationBudgetCard(tr *tour, name, title string) {
	tr.t.Helper()
	res := tr.setup("P's cards, for the id of "+name, tr.owner, "GET /api/v1/projects/{id}/work-items", at("id", "{{p}}"))
	var items []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(res.body, &items); err != nil {
		tr.t.Fatalf("P's cards: %v\n%s", err, res.body)
	}
	for _, it := range items {
		if it.Title == title && tr.values[it.ID] == "" {
			tr.remember(name, it.ID)
			return
		}
	}
	tr.t.Fatalf("P holds no card %q that the area has not named yet\n%s", title, res.body)
}

// orchestrationBudgetQueued registers under name the id of W's queued run
// whose field has the value (setup: GET /api/v1/agent-runs?status=queued, as
// W's admin): a run a request launched without answering with its id.
func orchestrationBudgetQueued(tr *tour, name, field, value string) {
	tr.t.Helper()
	tr.setup("W's queued runs, for the id of "+name, tr.owner, "GET /api/v1/agent-runs", query("status=queued")).
		captureWhere(name, "", field, value, "id")
}

func orchestrationBudgetTour(tr *tour) {
	o, m := tr.owner, tr.actor("member")
	tr.sharedWorkspace("w", "Tour Shared")
	tr.join(m, "{{w}}", "member")
	const (
		claim      = "POST /api/v1/agent-runs/claim"
		start      = "POST /api/v1/agent-runs/{id}/start"
		finish     = "POST /api/v1/agent-runs/{id}/finish"
		delegate   = "POST /api/v1/agent-runs/delegate"
		poll       = "GET /api/v1/agent-runs/delegate/{id}"
		tree       = "GET /api/v1/agent-runs/{id}/tree"
		cancel     = "POST /api/v1/agent-runs/{id}/cancel"
		retry      = "POST /api/v1/agent-runs/{id}/retry"
		launch     = "POST /api/v1/agents/{slug}/runs"
		crewLaunch = "POST /api/v1/crews/{id}/runs"
	)
	run := func(name string) tourOpt { return at("id", "{{"+name+"}}") }
	as := claimBody("tour-box", tourDefaultProvider)

	// Project P, the area's agents, W's worker key.
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Orchestra"}`)).capture("p", "/id")
	for _, a := range []struct{ name, slug, display, prompt string }{
		{"agent.lead", "tour-lead", "Tour Lead", "You plan and delegate."},
		{"agent.analyst", "tour-analyst", "Tour Analyst", "You analyse what you are given."},
		{"agent.checker", "tour-checker", "Tour Checker", "You check a teammate's work."},
		{"agent.critic", "tour-critic", "Tour Critic", "You review a teammate's work."},
		{"agent.worker", "tour-worker", "Tour Worker", "You answer in one line."},
	} {
		tr.setup("the agent "+a.slug, o, "POST /api/v1/agents", jsonBody(orchestrationBudgetAgent(a.slug, a.display,
			a.prompt))).capture(a.name, "/id")
	}
	box := tr.workerKey("box", "{{w}}", "a worker key of W, as a workspace runner sends it (worker id tour-box)")

	// Crew C, built through the /crews routes (the crews area pins them).
	tr.setup("crew C", o, "POST /api/v1/crews", jsonBody(`{"name":"Tour Crew","description":"Plans a release."}`),
		expect(201)).capture("c", "/id")
	for _, n := range []struct{ name, body string }{
		{"node.lead", `{"agent_id":"{{agent.lead}}","label":"Lead"}`},
		{"node.analyst", `{"agent_id":"{{agent.analyst}}","label":"Analyst"}`},
		{"node.checker", `{"agent_id":"{{agent.checker}}","label":"Checker"}`},
		{"node.critic", `{"agent_id":"{{agent.critic}}","label":"Critic"}`},
		{"node.m", `{"node_type":"human","user_id":"{{member}}","label":"Member M"}`},
	} {
		tr.setup("C's node "+n.name, o, "POST /api/v1/crews/{id}/nodes", at("id", "{{c}}"), jsonBody(n.body),
			expect(201)).capture(n.name, "/id")
	}
	tr.setup("C's entry node: Lead", o, "PUT /api/v1/crews/{id}", at("id", "{{c}}"),
		jsonBody(`{"entry_node_id":"{{node.lead}}"}`))
	edge := func(name, to, edgeType string, config map[string]any) {
		body := map[string]any{"from_node_id": tr.id("node.lead"), "to_node_id": tr.id(to), "edge_type": edgeType}
		if config != nil {
			body["config"] = config
		}
		tr.setup("C's edge "+name, o, "POST /api/v1/crews/{id}/edges", at("id", "{{c}}"), literalBody(body),
			expect(201)).capture(name, "/id")
	}
	edge("edge.analyst", "node.analyst", "delegates-to", nil)
	edge("edge.checker", "node.checker", "hands-off-to",
		map[string]any{"prompt_template": "Continue after {{handoff.run_id}}: {{handoff.output}}"})
	edge("edge.m", "node.m", "hands-off-to", nil)
	edge("edge.critic", "node.critic", "reviews", nil)

	// Crew C2, whose one node delegates to nobody.
	tr.setup("crew C2", o, "POST /api/v1/crews", jsonBody(`{"name":"Tour Solo"}`), expect(201)).capture("c2", "/id")
	tr.setup("C2's node Solo", o, "POST /api/v1/crews/{id}/nodes", at("id", "{{c2}}"),
		jsonBody(`{"agent_id":"{{agent.worker}}","label":"Solo"}`), expect(201)).capture("node.solo", "/id")
	tr.setup("C2's entry node: Solo", o, "PUT /api/v1/crews/{id}", at("id", "{{c2}}"),
		jsonBody(`{"entry_node_id":"{{node.solo}}"}`))

	// (a) C's run in P, claimed: the parent, lead. Then C2's run and a plain
	// run give two tokens more, and a plain run is left queued, older than
	// the child to come.
	lead := tr.setup("launch C in P: its run starts at Lead", o, crewLaunch, at("id", "{{c}}"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Plan the release of P."}`), expect(201))
	lead.capture("lead", "/id")
	lead.capture("lead.card", "/work_item_id")
	leadTok := tr.step("the box key claims C's run: a crew run's claim bytes, with team_id and team_node_id", box,
		claim, as).claimed("lead").runToken("lead", "the token of lead, C's run at its entry node Lead")
	tr.setup("start lead", box, start, run("lead"))
	tr.setup("launch C2 in P", o, crewLaunch, at("id", "{{c2}}"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Tidy P."}`), expect(201)).capture("solo", "/id")
	soloTok := tr.takeRun(box, "tour-box", "solo", "the token of solo, C2's run, whose node delegates to nobody")
	tr.queueRun("plain", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Summarise P."}`)
	plainTok := tr.takeRun(box, "tour-box", "plain", "the token of plain, a run launched outside any crew")
	tr.queueRun("spent", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Estimate P's cost."}`)

	// (b) Delegation: the refusals in the handler's order, then a child.
	tr.step("a signed-in user delegates: 403, only a run token delegates", o, delegate,
		jsonBody(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	tr.step("a worker key delegates: 403 the same", box, delegate,
		jsonBody(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	tr.step("plain's token delegates: 400, its run has no crew node, checked before the body", plainTok, delegate,
		jsonBody(`{`))
	tr.step("lead's token with a body that does not decode", leadTok, delegate, jsonBody(`{`))
	tr.step("lead's token with a blank prompt: checked before the label", leadTok, delegate,
		jsonBody(`{"role_label":"x","prompt":" \n "}`))
	tr.step("lead's token names a delegate Lead does not have: the labels it has", leadTok, delegate,
		jsonBody(`{"role_label":"x","prompt":"Analyse P."}`))
	tr.step("solo's token names Analyst: C2's node has no delegates, so none are listed", soloTok, delegate,
		jsonBody(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	tr.setup("remove C2's node Solo while solo runs", o, "DELETE /api/v1/crew-nodes/{id}", at("id", "{{node.solo}}"),
		expect(204))
	tr.step("solo's token once its crew node is gone: 404, the run still names the node (no foreign key)", soloTok,
		delegate, jsonBody(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	tr.step("lead's token delegates to analyst, matched without regard to case: 201, the child queued", leadTok,
		delegate, jsonBody(`{"role_label":"analyst","prompt":"Analyse P's test gaps."}`),
		note("the child inherits lead's card, and its launch moves that card back to To Do, as agent:<child>")).
		capture("child", "/run_id")

	// (c) The parent's poll.
	tr.step("lead polls its child: queued, a Go map's keys in order", leadTok, poll, run("child"))
	tr.step("solo's token polls lead's child: not its delegated run", soloTok, poll, run("child"))
	tr.step("lead polls a run no one has: 404", leadTok, poll, run("phantom"))
	tr.step("a signed-in user polls: 403, only a run token polls", o, poll, run("child"))

	// (d) I2's overlap: delegate/{id} is registered before {id}/tree,
	// {id}/logs and {id}/stream, so it answers these paths.
	for _, tail := range []string{"tree", "logs", "stream"} {
		tr.step("GET /api/v1/agent-runs/delegate/"+tail+" as a user: the poll's 403, not the "+tail+
			" route's", o, poll, at("id", tail), note("/metrics counts it under /api/v1/agent-runs/delegate/{id}"))
		tr.step("GET /api/v1/agent-runs/delegate/"+tail+" with a run token: the poll's 404, no run has the id "+
			tail, leadTok, poll, at("id", tail))
	}

	// (e) The child, then the parent's finish and its hand-offs.
	tr.step("the box key claims: the child, priority 10, before spent, an older run of priority 0", box, claim, as,
		note("the child's claim bytes carry priority 10, parent_run_id and team_node_id, and no launched_by")).
		claimed("child").runToken("child", "the token of child, lead's delegated run at Analyst")
	tr.setup("start child", box, start, run("child"))
	tr.step("the box key finishes child with its answer", box, finish, run("child"),
		jsonBody(`{"status":"succeeded","final_text":"P lacks two tests."}`))
	tr.step("lead polls its child: succeeded, with its answer", leadTok, poll, run("child"))
	tr.step("the box key finishes lead: Checker's and Critic's runs are launched inside the request, and the "+
		"hand-off to m, who has no role in P, is refused", box, finish, run("lead"),
		jsonBody(`{"status":"succeeded","final_text":"Plan: add the two tests, then ship."}`),
		note("hands-off-to edges first, then reviews, each in the order the edges were made; each successor "+
			"inherits lead's card and moves it back to To Do as itself; m could not open a card in P, so none is made"))
	tr.step("lead's log: the refused hand-off, noted at its end, since m has no role in P (REQ-23)", o,
		"GET /api/v1/agent-runs/{id}/logs", run("lead"),
		note("the tour sent no log for lead, so the note is its first entry"))
	tr.step("m reads lead's card: P's guard, since m has no role in P and the refused hand-off granted none", m,
		"GET /api/v1/work-items/{id}", at("id", "{{lead.card}}"))
	tr.step("lead's card: its activity records the refused hand-off to Member M", o, "GET /api/v1/work-items/{id}",
		at("id", "{{lead.card}}"), note("the child's and the successors' moves are logged on it as theirs"))
	res := tr.step("lead's tree: lead, then its children by created_at (child, Checker's, Critic's)", o, tree,
		run("lead"), note("Checker's prompt is its edge's template rendered; Critic's is the reviews copy"))
	res.captureWhere("checker", "", "team_node_id", tr.id("node.checker"), "id")
	res.captureWhere("critic", "", "team_node_id", tr.id("node.critic"), "id")
	tr.step("child's tree: child alone", o, tree, run("child"))
	tr.step("m reads lead's tree: P's guard", m, tree, run("lead"))
	tr.step("the tree of a run no one has: 404", o, tree, run("phantom"))
	tr.step("the box key reads lead's tree: the handler's 401, a key is no user", box, tree, run("lead"))
	tr.step("lead's token, revoked by its finish: the middleware's 401", leadTok, delegate,
		jsonBody(`{"role_label":"Analyst","prompt":"Analyse P again."}`))
	tr.setup("cancel Checker's queued run, so that no later claim takes it", o, cancel, run("checker"))
	tr.setup("cancel Critic's queued run, so that no later claim takes it", o, cancel, run("critic"))

	// (f) The conversational replies: the guided copilot's and the
	// interviewer's runs, played over the worker wire.
	tr.setup("W's requirements-copilot, redefined with a short prompt", o, "PUT /api/v1/agents/{slug}",
		at("slug", "requirements-copilot"), jsonBody(orchestrationBudgetAgent("requirements-copilot", "Tour Copilot",
			"You ask one question."))).capture("agent.copilot", "/id")
	tr.setup("guided session S on P", o, "POST /api/v1/guided-sessions", jsonBody(`{"project_id":"{{p}}"}`)).
		capture("s", "/id")
	tr.setup("kick off S's chat on step 1", o, "POST /api/v1/guided-sessions/{id}/chat/kickoff", at("id", "{{s}}"),
		jsonBody(`{"step":1}`))
	tr.setup("S's copilot run", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}")).
		capture("copilot", "/agent_run_id")
	tr.step("the box key claims: the copilot's run, priority 20, with guided_session_id", box, claim, as,
		orchestrationBudgetGuidedPrompt("/run/prompt")).
		claimed("copilot").runToken("copilot", "the token of copilot, S's first turn")
	tr.setup("start copilot", box, start, run("copilot"))
	tr.step("the box key finishes copilot: its answer is delivered to S's chat", box, finish, run("copilot"),
		jsonBody(`{"status":"succeeded","final_text":"Who operates the mill?"}`), orchestrationBudgetGuidedPrompt("/prompt"))
	tr.step("S's chat: the copilot's answer", o, "GET /api/v1/guided-sessions/{id}/messages", at("id", "{{s}}"),
		note("an S5b route, read to show the reply"))
	tr.setup("a message to S, which launches another turn", o, "POST /api/v1/guided-sessions/{id}/messages",
		at("id", "{{s}}"), jsonBody(`{"content":"A machinist."}`))
	tr.setup("S's second copilot run", o, "GET /api/v1/guided-sessions/{id}", at("id", "{{s}}")).
		capture("copilot2", "/agent_run_id")
	tr.takeRun(box, "tour-box", "copilot2", "the token of copilot2, S's second turn")
	tr.step("the box key finishes copilot2 as failed, retryable: the failure text is delivered, and a guided "+
		"turn is not retried", box, finish, run("copilot2"),
		jsonBody(`{"status":"failed","error":"the provider hung up","error_class":"provider_unavailable"}`),
		orchestrationBudgetGuidedPrompt("/prompt"))
	tr.step("S's chat: the answer, the message, the failure text", o, "GET /api/v1/guided-sessions/{id}/messages",
		at("id", "{{s}}"))
	tr.step("W's queued runs: spent alone, so copilot2's retryable failure queued no retry (a guided turn's "+
		"failure is its session's to answer)", o, "GET /api/v1/agent-runs", query("status=queued"))
	tr.setup("interview I on P, served by tour-worker", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour discovery","brief":"Ask about night runs.","agent_slug":"tour-worker"}`),
		expect(201)).capture("i", "/id")
	inv := tr.setup("an invite to I", o, "POST /api/v1/interviews/{id}/invites", at("id", "{{i}}"),
		jsonBody(`{"invitee_label":"Operator"}`), expect(201))
	inv.capture("invite", "/invite/id")
	inv.capture("invite.token", "/token")
	tr.setup("the participant's first message, which opens a session and launches the interviewer", tr.anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{invite.token}}"),
		jsonBody(`{"participant_name":"Dana","content":"I run the mill at night."}`)).capture("is", "/session/id")
	orchestrationBudgetQueued(tr, "interviewer", "interview_session_id", tr.id("is"))
	tr.step("the box key claims: the interviewer's run, priority 20, with interview_session_id, the untrusted "+
		"origin (REQ-91)", box, claim, as).claimed("interviewer").runToken("interviewer", "the token of interviewer")
	tr.setup("start interviewer", box, start, run("interviewer"))
	tr.step("the box key finishes interviewer: its answer is delivered to the transcript", box, finish,
		run("interviewer"), jsonBody(`{"status":"succeeded","final_text":"What do you check before a night run?"}`))
	tr.step("the session's transcript: the participant's message, the interviewer's answer", o,
		"GET /api/v1/interview-sessions/{id}/transcript", at("id", "{{is}}"),
		note("an S5b route, read to show the reply"))

	// (g) Q9. Setup, before the budget is set: the launch paths' targets, a
	// failed run, a run that cost $5.00, and a second run of C.
	tr.setup("a manual automation of tour-worker in P", o, "POST /api/v1/automations",
		jsonBody(`{"name":"Tour nightly","agent_id":"{{agent.worker}}","project_id":"{{p}}","kind":"manual",`+
			`"prompt_template":"Summarise P nightly."}`), expect(201)).capture("auto", "/id")
	tr.setup("a requirement in P", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Night runs","body":"The system shall stop the spindle when the enclosure opens."}`)).
		capture("req", "/id")
	tr.setup("a test case in P", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"test-case",`+
		`"title":"Open the enclosure","body":"Open it while the spindle runs; it stops."}`)).capture("tc", "/id")
	tr.setup("a test run in P", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour night run"}`), expect(201)).capture("testrun", "/id")
	tr.takeRun(box, "tour-box", "spent", "the token of spent")
	tr.setup("finish spent at a cost of $5.00", box, finish, run("spent"),
		jsonBody(`{"status":"succeeded","final_text":"About five dollars.","cost_usd":5}`))
	tr.queueRun("failed", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Fail."}`)
	tr.takeRun(box, "tour-box", "failed", "the token of failed")
	tr.setup("finish failed as failed, a class that is not retried", box, finish, run("failed"),
		jsonBody(`{"status":"failed","error":"the agent gave up","error_class":"agent_error"}`))
	lead2 := tr.setup("launch C in P again", o, crewLaunch, at("id", "{{c}}"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Plan the next release of P."}`), expect(201))
	lead2.capture("lead2", "/id")
	lead2.capture("lead2.card", "/work_item_id")
	lead2Tok := tr.takeRun(box, "tour-box", "lead2", "the token of lead2, C's second run, claimed before the budget")
	tr.setup("W's monthly budget: $1.00, below the $5.00 spent", o, "PUT /api/v1/orgs/{id}", at("id", "{{w}}"),
		jsonBody(`{"monthly_budget_usd":1}`))

	tr.step("launch tour-worker over budget: 402 (Q9)", o, launch, at("slug", "tour-worker"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P."}`))
	tr.step("draft test cases over budget: 402 (Q9)", o, "POST /api/v1/projects/{id}/draft-test-cases",
		at("id", "{{p}}"), jsonBody(`{"requirement_ids":["{{req}}"]}`), note("an S5d route, whose other answers the proposals_events area pins"))
	tr.step("run the automation now over budget: 400 (Q9)", o, "POST /api/v1/automations/{id}/run-now",
		at("id", "{{auto}}"))
	tr.step("launch C over budget: 400 (Q9)", o, crewLaunch, at("id", "{{c}}"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Plan it again."}`))
	tr.step("launch an agent on the test run over budget: 400 (Q9)", o, "POST /api/v1/test-runs/{id}/agent-run",
		at("id", "{{testrun}}"), jsonBody(`{"agent_slug":"tour-worker"}`), note("an S5b route"))
	tr.step("lead2 delegates over budget: 500 (Q9)", lead2Tok, delegate,
		jsonBody(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	tr.step("retry the failed run over budget: 500, beyond Q9's six", o, retry, run("failed"))
	tr.setup("the owner makes m a viewer of P, so that a hand-off may go to m", o, "POST /api/v1/projects/{id}/members",
		at("id", "{{p}}"), jsonBody(`{"email":"tour-member@example.com","role":"viewer"}`), expect(201))
	tr.step("the box key finishes lead2 over budget: 200, no agent successor is launched, an event names them and "+
		"the budget (REQ-76), and m, now a viewer of P, still gets a card", box, finish, run("lead2"),
		jsonBody(`{"status":"succeeded","final_text":"Plan: ship as is."}`),
		note("the owner made m a viewer of P just before (setup: POST /api/v1/projects/{id}/members)"))
	orchestrationBudgetCard(tr, "handoff2", "Handoff from Lead")
	tr.step("lead2's tree: lead2 alone, no successor", o, tree, run("lead2"))
	tr.setup("W's monthly budget: none", o, "PUT /api/v1/orgs/{id}", at("id", "{{w}}"),
		jsonBody(`{"monthly_budget_usd":null}`))
	after := tr.step("launch tour-worker with the budget cleared: 201", o, launch, at("slug", "tour-worker"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P, within budget."}`))
	after.capture("after", "/id")
	after.capture("after.card", "/work_item_id")
	for _, again := range []struct {
		name, title, route string
		opts               []tourOpt
	}{
		{"after.auto", "run the automation now with the budget cleared: 201, so its 400 above was the guard's",
			"POST /api/v1/automations/{id}/run-now", []tourOpt{at("id", "{{auto}}")}},
		{"after.crew", "launch C with the budget cleared: 201, so its 400 above was the guard's", crewLaunch,
			[]tourOpt{at("id", "{{c}}"), jsonBody(`{"project_id":"{{p}}","prompt":"Plan it again."}`)}},
		{"after.retry", "retry the failed run with the budget cleared: 201, so its 500 above was the guard's", retry,
			[]tourOpt{run("failed")}},
	} {
		res := tr.step(again.title, o, again.route, again.opts...)
		res.capture(again.name, "/id")
		res.capture(again.name+".card", "/work_item_id")
	}
	tr.step("lead2's log: the note that Checker and Critic were not launched, and why (REQ-76)", o,
		"GET /api/v1/agent-runs/{id}/logs", run("lead2"),
		note("the tour sent no log for lead2, so the note is its only entry"))
}
