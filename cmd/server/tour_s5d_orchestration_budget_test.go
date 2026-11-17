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
// walks (crews, their delegation, hand-offs and successors, went with issue
// 732: an agent run is a single run):
//   - a plain run claimed and finished over the worker wire: its finish
//     launches nothing more, and its tree is the run alone; its refusals;
//     and the finished run's token, revoked (the middleware's 401);
//   - the conversational replies S5b left to S5d: the guided copilot's run
//     (priority 20, guided_session_id in its claim bytes) finished with an
//     answer the session's chat then holds, and a failed turn, whose failure
//     text the chat holds instead and which is not retried; the interviewer's
//     run, whose claim bytes carry interview_session_id, the untrusted origin
//     of REQ-91, finished into the session's transcript;
//   - Q9, with W over its budget ($5.00 spent of $1.00): the launch and the
//     test-case drafting answer 402, run-now and a test run's agent run 400,
//     and, beyond Q9's, a retry 500, all with the guard's text where they
//     show it; and with the budget cleared a launch, run-now and the retry
//     pass again, so that each refusal above was the guard's.
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
// automation launch routes' own successes (other S5d areas').
func TestTourS5dOrchestrationBudget(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "orchestration_budget",
		about: "Run orchestration and the workspace budget: a single run's finish and its tree, the guided " +
			"copilot's and the interviewer's replies over the worker wire, and Q9's refusals of every launch path " +
			"once the workspace is over its monthly budget.",
		run: orchestrationBudgetTour,
		env: map[string]string{
			// Wires main.go's budget guard (an M3 closure): launches are
			// refused once W's month-to-date spend reaches its budget.
			"OPENV_BUDGET_ENFORCE": "true",
		},
		accounts: []tourAccount{{name: "member", display: "Tour Member", about: "m, a plain member of W with no role " +
			"in P: a refusal of the run tree"}},
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
		claim  = "POST /api/v1/agent-runs/claim"
		start  = "POST /api/v1/agent-runs/{id}/start"
		finish = "POST /api/v1/agent-runs/{id}/finish"
		tree   = "GET /api/v1/agent-runs/{id}/tree"
		retry  = "POST /api/v1/agent-runs/{id}/retry"
		launch = "POST /api/v1/agents/{slug}/runs"
		logs   = "GET /api/v1/agent-runs/{id}/logs"
	)
	run := func(name string) tourOpt { return at("id", "{{"+name+"}}") }
	as := claimBody("tour-box", tourDefaultProvider)

	// Project P, the area's agent, W's worker key.
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Orchestra"}`)).capture("p", "/id")
	tr.setup("the agent tour-worker", o, "POST /api/v1/agents", jsonBody(orchestrationBudgetAgent("tour-worker",
		"Tour Worker", "You answer in one line."))).capture("agent.worker", "/id")
	box := tr.workerKey("box", "{{w}}", "a worker key of W, as a workspace runner sends it (worker id tour-box)")

	// (a) A plain run, finished: nothing follows it.
	tr.queueRun("plain", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Summarise P."}`)
	plainTok := tr.step("the box key claims plain: a run's claim bytes", box, claim, as).claimed("plain").
		runToken("plain", "the token of plain")
	tr.setup("start plain", box, start, run("plain"))
	tr.queueRun("spent", o, "tour-worker", `{"project_id":"{{p}}","prompt":"Estimate P's cost."}`)
	tr.step("the box key finishes plain: no other run is launched, since an agent run is a single run", box, finish,
		run("plain"), jsonBody(`{"status":"succeeded","final_text":"P is on track."}`))
	tr.step("plain's tree: plain alone", o, tree, run("plain"))
	tr.step("m reads plain's tree: P's guard", m, tree, run("plain"))
	tr.step("the tree of a run no one has: 404", o, tree, run("phantom"))
	tr.step("the box key reads plain's tree: the handler's 401, a key is no user", box, tree, run("plain"))
	tr.step("plain's token, revoked by its finish: the middleware's 401", plainTok, logs, run("plain"))

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
	// failed run and a run that cost $5.00.
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
	tr.setup("W's monthly budget: $1.00, below the $5.00 spent", o, "PUT /api/v1/orgs/{id}", at("id", "{{w}}"),
		jsonBody(`{"monthly_budget_usd":1}`))

	tr.step("launch tour-worker over budget: 402 (Q9)", o, launch, at("slug", "tour-worker"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P."}`))
	tr.step("draft test cases over budget: 402 (Q9)", o, "POST /api/v1/projects/{id}/draft-test-cases",
		at("id", "{{p}}"), jsonBody(`{"requirement_ids":["{{req}}"]}`), note("an S5d route, whose other answers the proposals_events area pins"))
	tr.step("run the automation now over budget: 400 (Q9)", o, "POST /api/v1/automations/{id}/run-now",
		at("id", "{{auto}}"))
	tr.step("launch an agent on the test run over budget: 400 (Q9)", o, "POST /api/v1/test-runs/{id}/agent-run",
		at("id", "{{testrun}}"), jsonBody(`{"agent_slug":"tour-worker"}`), note("an S5b route"))
	tr.step("retry the failed run over budget: 500, beyond Q9's", o, retry, run("failed"))
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
		{"after.retry", "retry the failed run with the budget cleared: 201, so its 500 above was the guard's", retry,
			[]tourOpt{run("failed")}},
	} {
		res := tr.step(again.title, o, again.route, again.opts...)
		res.capture(again.name, "/id")
		res.capture(again.name+".card", "/work_item_id")
	}
}
