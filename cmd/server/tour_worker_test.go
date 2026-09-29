//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the API tour gained for S5d, agents and the worker wire (refactor
// plan §6.4 S5a-S5e; invariants I10, I12; quirks Q9, Q10): the requests a
// runner sends, so that an area plays the worker itself and no run ever
// reaches a real model. The server's own side of a run (the queue, the claim
// bytes, the lifecycle, the events it publishes) is what S5d pins; the
// runner's side of the same wire is S7's.
//
// Everything here is new and inert for an area that does not call it. One
// earlier framework file changed for S5d: tour_test.go's reorder numbers the
// ids an unordered array holds, in its sorted order, before a later unordered
// pointer of the same step keys its elements (a cloned crew's edges, which
// differ only in the cloned nodes they join; TestTourOrder). Only a step with
// two unordered pointers sorts differently, and no S5a, S5b or S5c step has
// two, so none of their goldens changed.
//
// CREDENTIALS. A runner authenticates with Authorization: Bearer, never with
// a session, so its actors are bearer actors (tour.bearerActor, S5c): a
// workspace worker key (tour.workerKey), a member's personal runner key
// (tour.runnerKey), a key the server's environment names (WORKER_API_KEY, the
// legacy one: bearerActor with the area's own literal), and a claimed run's
// token (tourResult.runToken), which the claim answer hands out once. Each
// credential is registered as <name.token>, and a key's record id as
// <name.key>.
//
// THE CLAIM. POST /api/v1/agent-runs/claim hands a worker the workspace's
// highest-priority, oldest queued run that it may take (the providers it
// names, a priority floor, a personal key's own runs, a reserved run's grace,
// a hosted runner's repo-access skip). So a run the area left queued (an
// auto-retry, a reserved run, a crew successor, a run-now launch) changes
// which run the next claim takes: claimBody sends exactly what a runner
// sends (runner/client.go's Claim), and tourResult.claimed stops the area
// with the regenerate command unless the claim took the run the area meant.
// tour.queueRun and tour.takeRun are the setup shortcuts other areas use to
// put a run in the state their own steps start from.
//
// TIMES TO COME. A run's hosted_after (a personal runner's grace, 60 s) and
// an auto-retry's next_attempt_at (30 s) are times after the answer. The
// clock writes a time after the tour's last exchange as <time>, but a time
// only 30 s ahead can fall inside a slow area's run and would then be written
// with whichever exchange the clock found there: capture such a value by name
// (tourResult.capture) and note its distance (tourResult.noteSeconds).
//
// AGENT FILES. An agent is a markdown file under OPENV_DATA_DIR/agents/<the
// workspace's id>/ (harnessEnv sets OPENV_DATA_DIR to <tmp>/data), which the
// golden writes as <tmp>/data/agents/<w>/...; tour.agentsDir names that
// directory, for an area that puts a file there before a sync (the S5c
// connector area's fixture pattern).

// tourDefaultProvider is the provider every seeded agent and every agent the
// S5d areas create runs on, so a claim that names it can take any of them.
const tourDefaultProvider = "claude-code"

// workerKey mints a workspace worker key (setup, as the owner: POST
// /api/v1/orgs/{id}/worker-keys, named after name) in a workspace the owner
// administers (filled: "{{w}}"), registers its record id as name.key, and
// returns the key as a bearer actor, its credential registered as name.token.
func (tr *tour) workerKey(name, workspace, about string) *tourActor {
	tr.t.Helper()
	body, _ := json.Marshal(map[string]string{"name": "Tour " + name})
	res := tr.setup("a worker key "+name, tr.owner, "POST /api/v1/orgs/{id}/worker-keys", at("id", workspace),
		rawBody("application/json", body), expect(http.StatusCreated))
	res.capture(name+".key", "/key_record/id")
	return tr.bearerActor(name, res.value("/key"), about)
}

// runnerKey mints a member's personal runner key in a workspace it belongs
// to (setup, as the member: POST /api/v1/orgs/{id}/my-runner-key), registers
// its record id as name.key, and returns the key as a bearer actor, its
// credential registered as name.token. A personal key claims only its
// member's runs and ownerless ones, and marks the member's runner online for
// 30 s after each use.
func (tr *tour) runnerKey(name string, member *tourActor, workspace, about string) *tourActor {
	tr.t.Helper()
	res := tr.setup("the runner key of "+member.name, member, "POST /api/v1/orgs/{id}/my-runner-key",
		at("id", workspace), expect(http.StatusCreated))
	res.capture(name+".key", "/key_record/id")
	return tr.bearerActor(name, res.value("/key"), about)
}

// tourClaim is the body a runner claims with (runner/client.go, Claim).
type tourClaim struct {
	worker      string
	providers   []string
	minPriority int
	hosted      bool
}

// body is the claim as the runner encodes it: a map, so its keys are
// sorted, and providers an array even when empty.
func (c tourClaim) body() []byte {
	providers := c.providers
	if providers == nil {
		providers = []string{}
	}
	b, _ := json.Marshal(map[string]any{
		"worker_id":    c.worker,
		"providers":    providers,
		"min_priority": c.minPriority,
		"hosted":       c.hosted,
	})
	return b
}

func (c tourClaim) opt() tourOpt { return rawBody("application/json", c.body()) }

// claimBody is what a runner sends to POST /api/v1/agent-runs/claim: its
// worker id, the providers it can run (none: an empty list, which claims
// nothing), no priority floor, not hosted.
func claimBody(workerID string, providers ...string) tourOpt {
	return tourClaim{worker: workerID, providers: providers}.opt()
}

// claimAbove is claimBody with a priority floor: only runs of at least
// minPriority (a delegated child is 10, an interview or guided turn 20).
func claimAbove(workerID string, minPriority int, providers ...string) tourOpt {
	return tourClaim{worker: workerID, providers: providers, minPriority: minPriority}.opt()
}

// hostedClaimBody is claimBody from a hosted runner, which the plan flag
// gates and which never takes a run whose agent has repository access.
func hostedClaimBody(workerID string, providers ...string) tourOpt {
	return tourClaim{worker: workerID, providers: providers, hosted: true}.opt()
}

// claimedProblem says what is wrong with a claim answer that should have
// handed out the run want ("" when it did).
func claimedProblem(status int, body []byte, want string) string {
	if status != http.StatusOK {
		return fmt.Sprintf("it answered %d, not 200 with a run", status)
	}
	got, err := jsonValue(body, "/run/id")
	if err != nil {
		return fmt.Sprintf("it holds no /run/id: %v", err)
	}
	if got != want {
		return fmt.Sprintf("it took run %v", got)
	}
	return ""
}

// claimed stops the area unless the claim answer handed out the run
// registered as run: a claim takes the workspace's highest-priority, oldest
// queued run, so a run the area left queued would otherwise be taken
// silently, and every later step would drive another run than its title
// says. It returns the answer, for runToken.
func (r *tourResult) claimed(run string) *tourResult {
	r.tr.t.Helper()
	want := r.tr.id(run)
	if problem := claimedProblem(r.status, r.body, want); problem != "" {
		r.tr.t.Fatalf("%s should have claimed the run registered as %s (%s), but %s: a claim takes the workspace's "+
			"highest-priority, oldest queued run the worker may take, so a run the area left queued (an auto-retry, a "+
			"reserved run, a successor, a launch by a key or a token) was taken first, or the run was not claimable. "+
			"Keep one claimable run at a time; if the server's order changed on purpose, change the area, then "+
			"regenerate with:\n  %s\n%s", r.what(), run, want, problem, strings.Join(r.tr.regenerate(), "\n  then "), r.body)
	}
	return r
}

// runToken makes a bearer actor of the run token a claim answer handed out,
// registered as name.token: the credential a runner gives the agent's MCP
// tools, which act as the run (actor agent:<run>). A claim mints a new token
// each time, so a run claimed again needs a new name.
func (r *tourResult) runToken(name, about string) *tourActor {
	r.tr.t.Helper()
	return r.tr.bearerActor(name, r.value("/run_token"), about)
}

// queueRun launches a run as setup (POST /api/v1/agents/{slug}/runs as a,
// the body filled, expecting 201) and registers its id as name: for an area
// whose recorded steps start from a queued run. The launch itself is pinned
// by the worker_wire area.
func (tr *tour) queueRun(name string, a *tourActor, slug, body string, opts ...tourOpt) *tourResult {
	tr.t.Helper()
	res := tr.setup("launch the run "+name, a, "POST /api/v1/agents/{slug}/runs",
		append([]tourOpt{at("slug", slug), jsonBody(body), expect(http.StatusCreated)}, opts...)...)
	res.capture(name, "/id")
	return res
}

// takeRun claims, as setup, the queued run registered as run with a worker
// (claimBody(workerID, tourDefaultProvider)), stops the area unless the claim
// took that run (claimed), and returns the run's token as a bearer actor
// named after the run (its credential <run.token>).
func (tr *tour) takeRun(worker *tourActor, workerID, run, about string) *tourActor {
	tr.t.Helper()
	res := tr.setup("claim the run "+run, worker, "POST /api/v1/agent-runs/claim",
		claimBody(workerID, tourDefaultProvider), expect(http.StatusOK))
	return res.claimed(run).runToken(run, about)
}

// secondsBetween is how many whole seconds (rounded) the RFC 3339 time at
// pointer is after the one at from, both in one JSON answer.
func secondsBetween(body []byte, pointer, from string) (int, error) {
	read := func(p string) (time.Time, error) {
		v, err := jsonValue(body, p)
		if err != nil {
			return time.Time{}, err
		}
		s, ok := v.(string)
		if !ok {
			return time.Time{}, fmt.Errorf("%s is %v, not a string", p, v)
		}
		return time.Parse(time.RFC3339Nano, s)
	}
	a, err := read(pointer)
	if err != nil {
		return 0, err
	}
	b, err := read(from)
	if err != nil {
		return 0, err
	}
	return int(math.Round(a.Sub(b).Seconds())), nil
}

// noteSeconds notes on a recorded step how many whole seconds the time at
// pointer is after the time at from, both in its answer: a time to come
// (hosted_after, next_attempt_at), written <time> or by the name it was
// captured under, whose distance from the moment it was set is the point.
// noteExpiry measures from the answer's Date in whole minutes; the run
// lifecycle's windows are seconds, and the answer holds its own start.
func (r *tourResult) noteSeconds(pointer, from string) {
	r.tr.t.Helper()
	s, err := secondsBetween(r.body, pointer, from)
	if err != nil {
		r.tr.t.Fatalf("noteSeconds: %s of %s: %v\n%s", pointer, r.what(), err, r.body)
	}
	r.note(fmt.Sprintf("%s is %d seconds after %s", pointer, s, from))
}

// agentsDir is the directory the server keeps a workspace's agent files in
// (OPENV_DATA_DIR/agents/<id>, OPENV_DATA_DIR being <tmp>/data under the
// harness), for an area that writes a file there before a sync.
func (tr *tour) agentsDir(workspace string) string {
	tr.t.Helper()
	return filepath.Join(tr.s.tmp, "data", "agents", tr.fill(workspace))
}

// literalBody sends v, encoded by encoding/json, as an application/json body
// with nothing in it filled: for a body that carries a template of the
// server's own (an edge's {{handoff.output}}, an automation's
// {{automation.name}} or {{event.title}}), whose braces jsonBody would read as
// a registered name. A value the body takes from the tour is filled in Go
// before (tour.id, tour.fill).
func literalBody(v any) tourOpt {
	return func(tr *tour, r *tourReq) {
		tr.t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			tr.t.Fatalf("literalBody: %v", err)
		}
		rawBody("application/json", b)(tr, r)
	}
}

// ----------------------------------------------------------------------------
// A check with no database

// TestTourWorker checks, with no database, what S5d added: the claim bodies
// as a runner sends them, a literal body, the key and token actors and how
// the golden writes them, the claim check's verdicts, the seconds between two
// times of an answer, and the agents directory.
func TestTourWorker(t *testing.T) {
	tr := &tour{t: t, norm: newTourNormaliser(), names: map[string]string{}, values: map[string]string{},
		s: &serverProcess{tmp: "/tmp/TestTourWorker001"}}
	tr.loadRoutes()
	check := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\n got %s\nwant %s", what, got, want)
		}
	}
	const claim = "POST /api/v1/agent-runs/claim"

	// The claim bodies: the runner's map, keys sorted, providers an array.
	for _, c := range []struct {
		opt  tourOpt
		want string
	}{
		{claimBody("tour-box", "claude-code"), `{"hosted":false,"min_priority":0,"providers":["claude-code"],"worker_id":"tour-box"}`},
		{claimBody("tour-box"), `{"hosted":false,"min_priority":0,"providers":[],"worker_id":"tour-box"}`},
		{claimAbove("tour-box", 10, "claude-code", "codex-cli"),
			`{"hosted":false,"min_priority":10,"providers":["claude-code","codex-cli"],"worker_id":"tour-box"}`},
		{hostedClaimBody("tour-hosted", "claude-code"), `{"hosted":true,"min_priority":0,"providers":["claude-code"],"worker_id":"tour-hosted"}`},
	} {
		r := tr.build(claim, []tourOpt{c.opt})
		check("a claim body", string(r.body), c.want)
		check("its type", r.header.Get("Content-Type"), "application/json")
	}

	// A literal body: encoded as it is, a server template's braces and the
	// encoder's HTML escaping kept, nothing filled.
	r := tr.build("POST /api/v1/crews/{id}/edges", []tourOpt{at("id", "run1"),
		literalBody(map[string]any{"config": map[string]string{"prompt_template": "After {{handoff.run_id}} & <x>"}})})
	check("a literal body", string(r.body), `{"config":{"prompt_template":"After {{handoff.run_id}} \u0026 \u003cx\u003e"}}`)
	check("its type", r.header.Get("Content-Type"), "application/json")

	// A run token from a claim answer: a bearer actor, its token registered
	// and written by name; the claim check's verdicts.
	const run, other = "5e0f6a7b-8c9d-4e1f-8a3b-4c5d6e7f8a9b", "6f1a2b3c-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	tr.remember("run1", run)
	token := strings.Repeat("7c", 32)
	body := []byte(`{"agent":{"slug":"tour-worker"},"auth":{"mode":"user-account"},"run":{"id":"` + run +
		`","status":"claimed"},"run_token":"` + token + `"}` + "\n")
	res := &tourResult{tr: tr, status: http.StatusOK, body: body}
	a := res.claimed("run1").runToken("run1", "the run's token")
	check("the token's bearer", a.bearer, token)
	check("the token's name", tr.norm.text(string(body)), `{"agent":{"slug":"tour-worker"},"auth":{"mode":"user-account"},`+
		`"run":{"id":"<run1>","status":"claimed"},"run_token":"<run1.token>"}`+"\n")
	check("a claim of the run", claimedProblem(http.StatusOK, body, run), "")
	check("a claim of another run", claimedProblem(http.StatusOK, body, other), "it took run "+run)
	check("an empty claim", claimedProblem(http.StatusNoContent, nil, run), "it answered 204, not 200 with a run")
	check("a claim with no run", claimedProblem(http.StatusOK, []byte(`{}`), run)[:len("it holds no /run/id")],
		"it holds no /run/id")

	// Seconds between two times of an answer, rounded: a grace of 60 s set
	// a moment after the run was stamped, a backoff of 30 s set a moment
	// before, and times with and without a fraction or in another zone.
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"created_at":"2026-09-28T12:00:00.123456789Z","hosted_after":"2026-09-28T12:01:00.123460001Z"}`, 60},
		{`{"created_at":"2026-09-28T12:00:00.5Z","hosted_after":"2026-09-28T12:00:30.499999Z"}`, 30},
		{`{"created_at":"2026-09-28T14:00:00+02:00","hosted_after":"2026-09-28T12:00:05Z"}`, 5},
	} {
		got, err := secondsBetween([]byte(c.body), "/hosted_after", "/created_at")
		if err != nil || got != c.want {
			t.Errorf("secondsBetween(%s): %d %v, want %d", c.body, got, err, c.want)
		}
	}
	for _, bad := range []string{`{"created_at":"2026-09-28T12:00:00Z"}`, `{"created_at":"x","hosted_after":"x"}`,
		`{"created_at":1,"hosted_after":2}`} {
		if _, err := secondsBetween([]byte(bad), "/hosted_after", "/created_at"); err == nil {
			t.Errorf("secondsBetween(%s) reads no error", bad)
		}
	}
	step := &tourStep{n: 3, req: &tourReq{}}
	(&tourResult{tr: tr, step: step, body: []byte(`{"a":"2026-09-28T12:00:00Z","b":"2026-09-28T12:00:30Z"}`)}).
		noteSeconds("/b", "/a")
	check("the note", strings.Join(step.req.notes, "|"), "/b is 30 seconds after /a")

	// The agents directory, under the harness's OPENV_DATA_DIR.
	tr.remember("w", other)
	check("the agents directory", tr.agentsDir("{{w}}"), "/tmp/TestTourWorker001/data/agents/"+other)
}
