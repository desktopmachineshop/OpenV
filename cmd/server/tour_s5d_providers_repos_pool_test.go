//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
)

// TestTourS5dProvidersReposPool is the S5d tour's area for a workspace's
// provider settings, the CLI sign-in broker, a project's repository
// connections and the runner pool's nodes (refactor plan §6.4 S5d, before
// M3 and M7, which split agent_handlers.go's repo-connection, provider and
// login sections and move the pool key's wiring; invariants I3, I4, I5,
// I12's "logins and pool"; quirks Q1, Q14, Q19; OpenV REQ-143). Its golden
// is testdata/tour/s5d/providers_repos_pool.json.
//
// The server boots with a runner pool key of its own (RUNNER_POOL_KEY, the
// S5c tiers area's pattern, without its tiers profile, so the lease lengths
// are the defaults). The area walks, in the owner's shared workspace W, with
// a plain member that views P and a second member with no role in P:
//   - provider settings: every known provider in display order, the
//     defaults built in memory on each read (a fresh id and updated_at each
//     time) with available_models from the built-in catalog; a PUT echoed,
//     its default_model appended to the models; a second PUT for the same
//     provider, whose echo carries the stored row's id (the upsert updates
//     the first row and returns its id); the refusals (a body that does not
//     decode, an unknown provider, an auth mode, an api_key_env outside the
//     catalogue, whose message lists the whole catalogue, a member, a key);
//     the worker's detection report, one provider per request (a report naming
//     a known and an unknown provider records the known one and answers 400
//     for the other, which internal/api's
//     TestProviderDetectionRecordsEveryKnownProvider pins), merged into
//     last_detected with checked_at (whole seconds), a detected model list
//     ahead of the catalog, a detection of a provider with no stored row
//     storing one, and the refusals (a user, the pool key, an unknown
//     provider, bodies that do not decode or are not a map of objects);
//   - the CLI sign-in broker: a workspace sign-in started (and resumed) by
//     an admin, a personal one by a member, the refusals (a provider with no
//     CLI sign-in, a target, a member's workspace sign-in, a key, a body),
//     the worker's claim (the full request, claimed, updated_at the
//     database's NOW(); 204 when none is left, since a workspace key skips a
//     member's personal sign-in, which the member's runner key takes), the
//     worker's progress (url_ready with the link and what to paste, a status
//     a worker may not report), the user's reads and code (sanitised: no
//     code), the worker's /full read (the code), completion, a code after
//     it, a cancel of a completed request (unchanged), a failure after
//     completion (answered with the request as it is, still completed), a
//     member's personal sign-in private to it and the workspace's admins, a
//     cancel (Cancelled by user.) that a late worker update cannot undo, a
//     plain member's cancel of a workspace sign-in, refused as its start is
//     (the admin cancels it as setup), another workspace's sign-in (404 for
//     the user and for the worker), a phantom id and one that is not a UUID;
//   - repository connections: none (null, Q14), two made by P's owner (the
//     default branch main, credential_strategy host), the refusals, W's
//     worker key refused a connection and a removal as an editor of P is
//     (a workspace key carries an editor's rights, REQ-42, and the owner
//     guard asks more), the member's own local path (trimmed), the
//     list as the owner (no path), as the member (its path), as the member's
//     runner key (the member's paths, through the key's user) and as the
//     workspace key (none); an update (an empty branch back to main), the
//     refusals, deletes, and null again once none is left;
//   - the runner pool: a node registering, the same name again (the same
//     row, reclaimed idle; providers [] in the answer, null once read back),
//     the refusals (an empty name, a fixed message; a body; a key, a user),
//     a beat with no lease; the member's lease (S5c's routes, as setup), the
//     beat that hands the lease's key over once, from memory, with the
//     lease's deadline, and the next without it; the lease's key claiming
//     (204 with nothing queued, then the member's own run, as a personal
//     runner), which stamps the lease's last_activity_at; the lease ended
//     (setup), the beat that sees no assignment, the lease's key refused; a
//     release naming another lease (a no-op), the release, and the
//     refusals (a phantom node, 404 as its beat answers, not registered;
//     a phantom node's beat, 404; ids that are not UUIDs; bodies; a key, a
//     user);
//   - last, the read the runner makes of a claimed run's repository
//     connections, with the run's own token since R7's fix (OpenV REQ-16 and
//     REQ-42; the maintainer's answer to #379's first S5d question): the token
//     of a run in P that the member's runner key claimed reads P's
//     connections with the member's local path, and the token of one the box
//     key claimed with none (a connection and the member's path made again
//     as setup, the runs launched by the box key, so no one's, and claimed as
//     setup).
//
// Every 2xx JSON answer here is a bare encode (text/plain by sniffing, Q1);
// errors are application/json. No step of the area publishes an event: the
// run the lease's key claims has no project, so no card moves, and the last
// section's runs in P are launched and claimed as setup. The golden's empty
// outbound_requests pins that no route here makes a provider call.
//
// Nondeterminism: a provider default's id is minted afresh on each read, so
// the plain answer's and the gzip variant's differ: it is elided (its
// length kept, and checked to be a new random UUID), while its updated_at
// is placed like any minted time; a detection's checked_at is whole seconds
// (tour.wholeSeconds on the list); a sign-in's claim stamps updated_at with
// the database's NOW(); the lease's deadlines are to come (<time>; the
// assignment's distance and the lease's length and idle window are noted),
// and the member's lease read counts seconds_remaining down (an area
// pattern). The provider list holds the built-in model catalog, so this
// golden changes with every catalog edit; the area reads the list twice
// only.
// Pool nodes go offline 45 s after their last beat and the reaper first
// sweeps 30 s after boot, so the pool's steps run last and beat just before
// each read that depends on the node. Not pinned: a sign-in request gone
// stale (15 minutes without progress, then resumed as a new one), a node
// going offline or a lease swept (time-bound), and the lease's hosted-minute
// allowance (the tiers area's).
func TestTourS5dProvidersReposPool(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "providers_repos_pool",
		about: "A workspace's provider settings and the worker's detection report, the CLI sign-in broker between a " +
			"member and a runner, a project's repository connections with each member's local path, and the runner " +
			"pool's nodes: registration, the heartbeat that hands a lease's key over, and the release.",
		run: providersReposPoolTour,
		env: map[string]string{"RUNNER_POOL_KEY": providersReposPoolKey},
		accounts: []tourAccount{
			{name: "member", display: "Tour Member", about: "a plain member of W that views P: its own sign-in, " +
				"its local path, its runner key, and the cloud runner it leases"},
			{name: "member2", display: "Tour Member Two", about: "another plain member of W, with no role in P: " +
				"the member's private sign-in and P's guard"},
		},
	})
}

// providersReposPoolKey is the deployment's runner pool key, the bearer the
// pool's nodes present.
const providersReposPoolKey = "tour-pool-key"

// providersReposPoolAgent is the area's agent, with a short prompt, whose
// run the lease's key claims.
func providersReposPoolAgent() string {
	b, _ := json.Marshal(map[string]any{
		"slug": "tour-pool", "name": "Tour Pool", "description": "An agent of the S5d tour.",
		"provider": tourDefaultProvider, "allowed_tools": []string{"get_artifact"}, "write_mode": "direct",
		"system_prompt": "You answer in one line.",
	})
	return string(b)
}

func providersReposPoolTour(tr *tour) {
	o := tr.owner
	m, m2 := tr.actor("member"), tr.actor("member2")
	w := tr.sharedWorkspace("w", "Tour Shared")
	tr.join(m, w, "member")
	tr.join(m2, w, "member")
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Repos"}`)).capture("p", "/id")
	tr.setup("the member views P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(fmt.Sprintf(`{"email":%q,"role":"viewer"}`, m.email)))
	box := tr.workerKey("box", "{{w}}", "a worker key of W (worker id tour-box)")
	runner := tr.runnerKey("runner", m, "{{w}}", "the member's personal runner key in W (worker id tour-runner)")
	pool := tr.bearerActor("pool", providersReposPoolKey, "a runner pool node, presenting the deployment's pool key "+
		"(RUNNER_POOL_KEY) as its bearer")
	providersReposPoolSettings(tr, o, m, box, pool)
	providersReposPoolLogins(tr, o, m, m2, box, runner, pool)
	providersReposPoolRepos(tr, o, m, m2, box, runner)
	providersReposPoolNodes(tr, o, m, box, pool)
	providersReposPoolRunReads(tr, o, m, box, runner)
}

// providersReposPoolSettings walks the provider settings and the worker's
// detection report.
func providersReposPoolSettings(tr *tour, o, m, box, pool *tourActor) {
	const (
		list   = "GET /api/v1/provider-settings"
		put    = "PUT /api/v1/provider-settings"
		detect = "POST /api/v1/provider-settings/detect"
	)
	tr.wholeSeconds(list, "last_detected.checked_at, which RecordDetection formats as RFC 3339 without a fraction")
	providersReposPoolMinted(tr.step("W's provider settings before any is stored: every known provider in display "+
		"order, each a default built on the read, with the catalog's models", o, list,
		providersReposPoolElide("/*/id")), "/*/id")
	tr.step("store claude-code on the API key GOOGLE_API_KEY names, with a model of its own: 200, the row echoed, "+
		"its model after the catalog's", o, put, jsonBody(`{"provider":"claude-code","auth_mode":"api-key",`+
		`"api_key_env":"GOOGLE_API_KEY","default_model":"tour-model","enabled":true}`)).capture("setting1", "/id")
	tr.step("store claude-code again, back on its subscription: the echo carries the stored row's first id (the "+
		"upsert updates on org and provider, and returns the row's id)", o, put,
		jsonBody(`{"provider":"claude-code","auth_mode":"subscription-cli","enabled":false}`))
	tr.step("store with a body that does not decode", o, put, jsonBody(`{`))
	tr.step("store a provider no one knows", o, put, jsonBody(`{"provider":"tour-cli","auth_mode":"api-key"}`))
	tr.step("store an auth mode no provider has", o, put, jsonBody(`{"provider":"codex-cli","auth_mode":"oauth"}`))
	tr.step("store an api_key_env outside the catalogue: the message lists the catalogue", o, put,
		jsonBody(`{"provider":"codex-cli","auth_mode":"api-key","api_key_env":"DATABASE_URL"}`))
	tr.step("a member stores one: the workspace admin guard", m, put,
		jsonBody(`{"provider":"codex-cli","auth_mode":"api-key"}`))
	tr.step("the box key stores one: the guard's 401, a key is no user", box, put,
		jsonBody(`{"provider":"codex-cli","auth_mode":"api-key"}`))

	// The worker's detection report: one provider per request.
	tr.step("the box key reports claude-code: 204, merged into its stored row's last_detected with checked_at", box,
		detect, jsonBody(`{"claude-code":{"version":"x"}}`),
		note("one provider per request: the handler ranges over a Go map, so a report naming a valid and an "+
			"unknown provider would record the valid one or not by the map's order"))
	tr.step("the box key reports gemini-cli, which has no stored row, with the models its CLI listed: 204, a row "+
		"stored with the defaults", box, detect, jsonBody(`{"gemini-cli":{"version":"0.9",`+
		`"models":["gemini-tour",{"id":"gemini-2.5-pro","label":"Gemini 2.5 Pro (detected)"}]}}`))
	tr.step("report nothing: 204, nothing recorded", box, detect, jsonBody(`{}`))
	tr.step("report a provider no one knows", box, detect, jsonBody(`{"tour-cli":{"version":"1"}}`))
	tr.step("report with a body that does not decode", box, detect, jsonBody(`{`))
	tr.step("report a provider's detection as a string, not an object: the decode refuses it", box, detect,
		jsonBody(`{"claude-code":"x"}`))
	tr.step("a signed-in user reports: worker credentials are required", o, detect,
		jsonBody(`{"claude-code":{"version":"x"}}`))
	tr.step("the pool key reports: no workspace, so no worker", pool, detect,
		jsonBody(`{"claude-code":{"version":"x"}}`))
	defaults := []string{"/1/id", "/3/id", "/4/id", "/5/id", "/6/id"}
	opts := []tourOpt{note("checked_at is whole seconds, so it is <time> (tour.wholeSeconds); updated_at is placed " +
		"as usual")}
	for _, p := range defaults {
		opts = append(opts, providersReposPoolElide(p))
	}
	providersReposPoolMinted(tr.step("W's provider settings, as a member (any member reads them): claude-code's "+
		"stored row under its first id (<setting1>) with the detection, gemini-cli's row stored by the report with "+
		"its detected models ahead of the catalog's, and the rest defaults", m, list, opts...), defaults...)
}

// providersReposPoolLease notes on a step answering the member's lease what
// its times to come hold: the lease's length (expires_at less started_at)
// and its idle deadline (deadline less last_activity_at), in whole minutes.
func providersReposPoolLease(res *tourResult) {
	res.tr.t.Helper()
	minutes := func(pointer, from string) int {
		s, err := secondsBetween(res.body, pointer, from)
		if err != nil {
			res.tr.t.Fatalf("%s: %s less %s: %v\n%s", res.what(), pointer, from, err, res.body)
		}
		return int(math.Round(float64(s) / 60))
	}
	res.note(fmt.Sprintf("the lease runs %d minutes (expires_at less started_at), and the runner goes at deadline, "+
		"%d minutes after last_activity_at (the idle window), which seconds_remaining counts down to",
		minutes("/session/expires_at", "/session/started_at"), minutes("/deadline", "/session/last_activity_at")))
}

// providersReposPoolElide keeps the id of a provider setting built on the
// read out of the golden: a random UUID minted afresh on every read, so the
// plain answer's and the gzip variant's differ (the stored rows' ids are
// pinned as they are). providersReposPoolMinted checks what it hides.
func providersReposPoolElide(pointer string) tourOpt {
	return elide(pointer, "<an id minted on the read>", 38, 38, "a setting with no stored row is built on each "+
		"read (defaultSetting) with a fresh random id, which no request can name; the plain answer's and the gzip "+
		"variant's differ, so it is left out, its 36 characters pinned")
}

// providersReposPoolMinted checks the ids a step elided: each a random
// (version 4) UUID, none repeated.
func providersReposPoolMinted(res *tourResult, pointers ...string) {
	res.tr.t.Helper()
	seen := map[string]bool{}
	for _, p := range pointers {
		spans, err := jsonFind(res.body, p)
		if err != nil || len(spans) == 0 {
			res.tr.t.Fatalf("%s holds no %s: %v\n%s", res.what(), p, err, res.body)
		}
		for _, sp := range spans {
			var id string
			err := json.Unmarshal(res.body[sp.start:sp.end], &id)
			if err != nil || !providersReposPoolUUID4.MatchString(id) || seen[id] {
				res.tr.t.Errorf("%s: %s is %s, not a fresh random UUID: a setting with no stored row should be "+
					"built on the read with a new id; if that changed on purpose, change the area, then regenerate "+
					"with:\n  %s", res.what(), p, res.body[sp.start:sp.end], strings.Join(res.tr.regenerate(), "\n  then "))
			}
			seen[id] = true
		}
	}
}

// providersReposPoolUUID4 is a random (version 4) UUID.
var providersReposPoolUUID4 = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// providersReposPoolLogins walks the CLI sign-in broker.
func providersReposPoolLogins(tr *tour, o, m, m2, box, runner, pool *tourActor) {
	const (
		start    = "POST /api/v1/provider-logins"
		claim    = "POST /api/v1/provider-logins/claim"
		get      = "GET /api/v1/provider-logins/{id}"
		code     = "POST /api/v1/provider-logins/{id}/code"
		cancel   = "POST /api/v1/provider-logins/{id}/cancel"
		progress = "POST /api/v1/provider-logins/{id}/progress"
		full     = "GET /api/v1/provider-logins/{id}/full"
	)
	login := func(name string) tourOpt { return at("id", "{{"+name+"}}") }

	tr.step("an admin starts a sign-in of claude-code on W's shared workers (the default target): 201, pending, "+
		"with the copy that waits for agentd", o, start, jsonBody(`{"provider":"claude-code"}`)).capture("login1", "/id")
	tr.step("start it again, naming the target: the same request, resumed (201, the same id)", o, start,
		jsonBody(`{"provider":"claude-code","target":"workspace"}`))
	tr.step("the member starts a sign-in on its own runner: 201, with the personal runner's copy", m, start,
		jsonBody(`{"provider":"claude-code","target":"user"}`)).capture("login2", "/id")
	tr.step("start a provider that signs in with an API key, not a CLI", o, start,
		jsonBody(`{"provider":"anthropic-api"}`))
	tr.step("start with a target no machine has", o, start, jsonBody(`{"provider":"claude-code","target":"x"}`))
	tr.step("the member starts a sign-in on W's shared workers: the workspace admin guard", m, start,
		jsonBody(`{"provider":"codex-cli"}`))
	tr.step("start with a body that does not decode: before the role is checked", m, start, jsonBody(`{`))
	tr.step("the box key starts one: the handler's 401, a key is no user", box, start,
		jsonBody(`{"provider":"claude-code"}`))

	// The worker's side: claim, progress, the full read.
	tr.step("a signed-in user claims: worker credentials are required", o, claim)
	tr.step("the pool key claims: no workspace, so no worker", pool, claim)
	tr.step("the box key claims: the admin's request, whole, claimed, updated_at the database's NOW()", box, claim)
	tr.step("the box key claims again: 204, the member's personal sign-in is not a workspace key's", box, claim)
	tr.step("the member's runner key claims: the member's personal sign-in", runner, claim)
	tr.step("the box key reports the link, and that it waits for a code: 200, the whole request", box, progress,
		login("login1"), jsonBody(`{"status":"url_ready","auth_url":"https://claude.example/oauth?client=tour&state=1",`+
			`"detail":"Open the link and paste the code.","paste_kind":"code"}`),
		note("encoding/json writes the link's & as \\u0026 (I4)"))
	tr.step("report the status pending, which a worker may not set", box, progress, login("login1"),
		jsonBody(`{"status":"pending"}`))
	tr.step("report with a body that does not decode", box, progress, login("login1"), jsonBody(`{`))
	tr.step("the admin reads the request: the link and what to paste", o, get, login("login1"))
	tr.step("the admin pastes the code: 200, sanitised, no code in the answer", o, code, login("login1"),
		jsonBody(`{"code":"tour-code-123"}`))
	tr.step("the admin reads it again: still no code", o, get, login("login1"))
	tr.step("the box key reads it whole: the code, for the CLI's stdin", box, full, login("login1"))
	tr.step("the admin reads it whole: worker credentials are required", o, full, login("login1"))
	tr.step("paste with a body that does not decode", o, code, login("login1"), jsonBody(`{`))
	tr.step("the box key reports completion: the whole request, the code still in it", box, progress, login("login1"),
		jsonBody(`{"status":"completed","detail":"Signed in."}`))
	tr.step("paste a code after completion", o, code, login("login1"), jsonBody(`{"code":"too-late"}`))
	tr.step("cancel the completed request: 200, as it is", o, cancel, login("login1"))
	tr.step("the box key reports a failure after completion: 200, the request as it is, still completed", box,
		progress, login("login1"),
		jsonBody(`{"status":"failed","detail":"The CLI exited."}`))

	// The member's personal sign-in: private to it and W's admins.
	tr.step("the other member reads the member's personal sign-in: 404, as if it did not exist", m2, get,
		login("login2"))
	tr.step("the other member cancels it: 404 too", m2, cancel, login("login2"))
	tr.step("W's admin reads it: admins see every sign-in of the workspace", o, get, login("login2"))
	tr.step("the member reads its own: claimed by its runner", m, get, login("login2"))
	tr.step("the member cancels it: 200, cancelled, Cancelled by user.", m, cancel, login("login2"))
	tr.step("the runner key reports completion after the cancel: 200, the request as it is, the late update "+
		"swallowed", runner, progress, login("login2"), jsonBody(`{"status":"completed","detail":"Signed in."}`))
	tr.step("the member pastes a code into the cancelled request", m, code, login("login2"), jsonBody(`{"code":"x"}`))
	tr.step("the member's runner key reads it whole", runner, full, login("login2"))
	tr.step("the member's runner key starts one: the handler's 401", runner, start,
		jsonBody(`{"provider":"claude-code","target":"user"}`))

	// A workspace sign-in a plain member could not start: it reads it, and
	// cannot cancel it.
	tr.step("the admin starts a sign-in of gemini-cli on W's shared workers", o, start,
		jsonBody(`{"provider":"gemini-cli"}`)).capture("login3", "/id")
	tr.step("the member reads it: a workspace sign-in is any member's to read", m, get, login("login3"))
	tr.step("the member cancels it: 403, the workspace admin guard, as its start's", m, cancel, login("login3"))
	tr.setup("the admin cancels it", o, cancel, login("login3"))
	tr.step("the box key claims: 204, nothing pending is left", box, claim)

	// Another workspace's sign-in, and ids no request has.
	tr.step("the admin starts a sign-in in its personal workspace", o, start, actingIn("{{owner.workspace}}"),
		jsonBody(`{"provider":"codex-cli"}`)).capture("login.home", "/id")
	tr.step("the admin reads it from W: 404, it is not W's", o, get, login("login.home"))
	tr.step("the admin cancels it from W: 404", o, cancel, login("login.home"))
	tr.step("W's box key reads it whole: 404, it is not the key's workspace's", box, full, login("login.home"))
	tr.step("W's box key reports on it: 404", box, progress, login("login.home"), jsonBody(`{"status":"claimed"}`))
	tr.step("a sign-in no one has", o, get, login("phantom"))
	tr.step("a sign-in no one has, read whole", box, full, login("phantom"))
	tr.step("a report on a sign-in no one has", box, progress, login("phantom"), jsonBody(`{"status":"claimed"}`))
	tr.step("a code for a sign-in no one has", o, code, login("phantom"), jsonBody(`{"code":"x"}`))
	tr.step("an id that is not a UUID: 404 all the same (the lookup's error is not told apart)", o, get,
		at("id", "not-a-login"))
}

// providersReposPoolRepos walks a project's repository connections.
func providersReposPoolRepos(tr *tour, o, m, m2, box, runner *tourActor) {
	const (
		list   = "GET /api/v1/projects/{id}/repo-connections"
		create = "POST /api/v1/projects/{id}/repo-connections"
		update = "PUT /api/v1/repo-connections/{id}"
		remove = "DELETE /api/v1/repo-connections/{id}"
		myPath = "PUT /api/v1/repo-connections/{id}/my-path"
	)
	inP := at("id", "{{p}}")
	repo := func(name string) tourOpt { return at("id", "{{"+name+"}}") }

	tr.step("P's repository connections: none, written null (Q14)", o, list, inP)
	tr.step("P's owner connects a repository: 201, the default branch main, credential_strategy host", o, create,
		inP, jsonBody(`{"name":"Tour repo","remote_url":"https://git.example.com/tour.git"}`)).capture("repo1", "/id")
	tr.step("and a second, on a branch of its own", o, create, inP, jsonBody(`{"name":"Tour docs",`+
		`"remote_url":"git@git.example.com:tour/docs.git","default_branch":"trunk"}`)).capture("repo2", "/id")
	tr.step("connect one with no name", o, create, inP, jsonBody(`{"remote_url":"https://git.example.com/x.git"}`))
	tr.step("connect one with no remote_url", o, create, inP, jsonBody(`{"name":"No remote"}`))
	tr.step("connect one with a body that does not decode", o, create, inP, jsonBody(`{`))
	tr.step("the member, a viewer of P, connects one: P's owner guard", m, create, inP,
		jsonBody(`{"name":"Mine","remote_url":"https://git.example.com/mine.git"}`))
	tr.step("the box key connects one: a workspace key is an editor of P, refused P's owner guard as an editor is",
		box, create, inP, jsonBody(`{"name":"Worker repo","remote_url":"https://git.example.com/worker.git"}`))
	tr.step("the box key removes the first: P's owner guard", box, remove, repo("repo1"))

	// Each member's own local path.
	tr.step("the member sets its local path to the first: 200, the connection with my_local_path, trimmed", m,
		myPath, repo("repo1"), jsonBody(`{"local_path":"  /home/member/src/tour  "}`))
	tr.step("set it with a body that does not decode", m, myPath, repo("repo1"), jsonBody(`{`))
	tr.step("the other member, with no role in P, sets one: P's guard", m2, myPath, repo("repo1"),
		jsonBody(`{"local_path":"/home/member2/tour"}`))
	tr.step("set a path for a connection no one has", m, myPath, repo("phantom"), jsonBody(`{"local_path":"/x"}`))
	tr.step("the box key sets one: the handler's 401, a key has no user", box, myPath, repo("repo1"),
		jsonBody(`{"local_path":"/srv/tour"}`))
	tr.step("P's connections as the owner, in the order made: no local path of the member's", o, list, inP)
	tr.step("as the member: its own path on the first", m, list, inP)
	tr.step("as the member's runner key: the member's paths, through the key's user", runner, list, inP)
	tr.step("as the box key: no user, so no path", box, list, inP)
	tr.step("as the other member: P's guard", m2, list, inP)
	tr.step("the member clears its path on the first with a blank one, which trims to empty: 200, no "+
		"my_local_path", m, myPath, repo("repo1"), jsonBody(`{"local_path":"   "}`))
	tr.step("as the member again: no path of its own now", m, list, inP)

	// Updates and deletes.
	tr.step("the owner renames the first and empties its branch: 200, the branch back to main", o, update,
		repo("repo1"), jsonBody(`{"name":"Tour repo (renamed)","default_branch":""}`))
	tr.step("update the second with an empty name", o, update, repo("repo2"), jsonBody(`{"name":""}`))
	tr.step("update the second with an empty remote_url", o, update, repo("repo2"), jsonBody(`{"remote_url":""}`))
	tr.step("update with a body that does not decode", o, update, repo("repo2"), jsonBody(`{`))
	tr.step("the member updates one: P's owner guard", m, update, repo("repo1"), jsonBody(`{"name":"Mine now"}`))
	tr.step("update a connection no one has", o, update, repo("phantom"), jsonBody(`{"name":"x"}`))
	tr.step("update an id that is not a UUID: 404 all the same", o, update, at("id", "not-a-repo"),
		jsonBody(`{"name":"x"}`))
	tr.step("the member removes one: P's owner guard", m, remove, repo("repo1"))
	tr.step("the owner removes the first: 204", o, remove, repo("repo1"))
	tr.step("remove it again: 404", o, remove, repo("repo1"))
	tr.step("the owner removes the second: 204", o, remove, repo("repo2"))
	tr.step("P's connections: none again, null (Q14)", o, list, inP)
}

// providersReposPoolRunReads walks the runner's read of a claimed run's
// repository connections, made with the run's own token (fixed under R7): a
// member's personal key reads only the projects its member can, so the
// runner no longer reads them with its key. The token reads its own
// project's connections with the local path of the member whose personal
// runner key claimed the run, since that is the machine the run is on, and
// none for a run a workspace key holds.
func providersReposPoolRunReads(tr *tour, o, m, box, runner *tourActor) {
	const list = "GET /api/v1/projects/{id}/repo-connections"
	inP := at("id", "{{p}}")
	tr.setup("P's owner connects a repository again", o, "POST /api/v1/projects/{id}/repo-connections", inP,
		jsonBody(`{"name":"Tour repo","remote_url":"https://git.example.com/tour.git"}`), expect(201)).
		capture("repo3", "/id")
	tr.setup("the member sets its local path to it", m, "PUT /api/v1/repo-connections/{id}/my-path",
		at("id", "{{repo3}}"), jsonBody(`{"local_path":"/home/member/src/tour"}`))
	tr.queueRun("prun", box, "tour-pool", `{"project_id":"{{p}}","prompt":"Tidy P."}`)
	prun := tr.takeRun(runner, "tour-runner", "prun", "the token of a run in P that no one launched, claimed by "+
		"the member's runner key, since the member views P")
	tr.step("the run's token reads P's connections, as the runner reads a claimed run's: the local path of the "+
		"member, whose runner key claimed the run", prun, list, inP, note("P's owner connected the repository again, "+
		"the member set its path, the box key launched the run in P and the member's runner key claimed it, all "+
		"just before (setup)"))
	tr.queueRun("brun", box, "tour-pool", `{"project_id":"{{p}}","prompt":"Tidy P again."}`)
	brun := tr.takeRun(box, "tour-box", "brun", "the token of a run in P that no one launched, claimed by the box key")
	tr.step("the token of a run the box key claimed reads P's connections: no member's machine, so no path", brun,
		list, inP, note("the box key launched and claimed the run just before (setup)"))
}

// providersReposPoolNodes walks the runner pool's node routes, with the
// member's lease (S5c's routes) as setup.
func providersReposPoolNodes(tr *tour, o, m, box, pool *tourActor) {
	const (
		register  = "POST /api/v1/runner-pool/nodes"
		heartbeat = "POST /api/v1/runner-pool/nodes/{id}/heartbeat"
		release   = "POST /api/v1/runner-pool/nodes/{id}/release"
		claim     = "POST /api/v1/agent-runs/claim"
	)
	node := at("id", "{{node}}")
	inW := at("id", "{{w}}")
	tr.pattern(`"seconds_remaining":(89[5-9]|900)\b`, "<about 900 s>", "seconds_remaining on a lease: the whole "+
		"seconds to its deadline, 15 minutes (the default idle window) after its last activity, less the seconds "+
		"since, truncated (899 within a second of it)")

	tr.step("a pool node registers: 201, idle, in the default pool", pool, register,
		jsonBody(`{"name":"tour-node","pool":"","providers":["claude","codex"]}`)).capture("node", "/id")
	tr.step("it registers again under the same name, as a restarted node does: the same row, reclaimed idle, its "+
		"providers replaced", pool, register, jsonBody(`{"name":"tour-node","providers":[]}`),
		note("providers is [] here, as decoded; the store keeps a joined string, so it reads back null"))
	tr.step("register with a blank name: the handler's fixed message", pool, register,
		jsonBody(`{"name":"  ","providers":["claude"]}`))
	tr.step("register with a body that does not decode", pool, register, jsonBody(`{`))
	tr.step("the box key registers one: pool credentials are required", box, register,
		jsonBody(`{"name":"tour-box-node"}`))
	tr.step("a signed-in user registers one: the same", o, register, jsonBody(`{"name":"tour-owner-node"}`))
	tr.step("the node beats with no lease: no assignment, the node as stored (providers null)", pool, heartbeat, node)

	// The member's lease, and the key the node picks up.
	tr.setup("the member leases a cloud runner: the node", m, "POST /api/v1/orgs/{id}/runner-session", inW,
		expect(201)).capture("lease", "/session/id")
	hand := tr.step("the node beats: its assignment, with the lease's key, handed out once from memory", pool,
		heartbeat, node, note("the member leased the node just before (setup: POST /api/v1/orgs/{id}/runner-session)"))
	hand.noteExpiry("/assignment/expires_at")
	lease := tr.bearerActor("lease", hand.value("/assignment/worker_key"), fmt.Sprintf("the lease's key, which the "+
		"node got on its beat at step %d: a personal runner key of the member's, bound to the lease (worker id "+
		"tour-lease)", hand.step.n))
	tr.step("the node beats again: the assignment, no key", pool, heartbeat, node)
	tr.step("the lease's key claims with nothing queued: 204, and the lease's idle clock restarts", lease, claim,
		claimBody("tour-lease", tourDefaultProvider))
	tr.setup("the agent tour-pool", o, "POST /api/v1/agents", jsonBody(providersReposPoolAgent())).
		capture("agent", "/id")
	tr.queueRun("mrun", m, "tour-pool", `{"prompt":"Tidy my week."}`)
	taken := tr.step("the lease's key claims the member's run, as the member's personal runner", lease, claim,
		claimBody("tour-lease", tourDefaultProvider), note("the member launched the run just before (setup), with "+
			"no project, so no card moves and no event is published; the lease's key had just been used, so the "+
			"member's runner counted as online and the launch reserved the run for it (preferred_user_id) until "+
			"hosted_after"))
	taken.claimed("mrun").runToken("mrun", "the member's run's token")
	taken.noteSeconds("/run/hosted_after", "/run/created_at")
	read := tr.step("the member's cloud runner: active, its last_activity_at the claim's, on the default lease "+
		"(no tiers): one hour, idle after 15 minutes; the pool's one node leased, so red", m,
		"GET /api/v1/orgs/{id}/runner-session", inW, note("an S5c route, read to pin that a claim by the lease's "+
			"key stamps its last activity (touchRunnerSession)"))
	read.capture("lease.key", "/session/worker_key_id")
	providersReposPoolLease(read)

	// The lease ended, and the node released.
	tr.setup("the member ends its lease", m, "DELETE /api/v1/orgs/{id}/runner-session", inW)
	tr.step("the node beats: no assignment, the node draining the ended lease", pool, heartbeat, node,
		note("the member ended its lease just before (setup: DELETE /api/v1/orgs/{id}/runner-session)"))
	tr.step("the lease's key claims: revoked with the lease, the middleware's 401", lease, claim,
		claimBody("tour-lease", tourDefaultProvider))
	tr.step("the node releases naming another lease: 204, and nothing released", pool, release, node,
		jsonBody(`{"session_id":"{{phantom}}"}`))
	tr.step("the node beats: still draining", pool, heartbeat, node)
	tr.step("release with a body that does not decode", pool, release, node, jsonBody(`{`))
	tr.step("release with no body", pool, release, node)
	tr.step("the node releases the lease it wiped: 204", pool, release, node, jsonBody(`{"session_id":"{{lease}}"}`))
	tr.step("the node beats: idle, no lease", pool, heartbeat, node)
	tr.step("the node releases with {}: 204, idle either way", pool, release, node, jsonBody(`{}`))

	// Nodes no one has, and credentials that are not the pool's.
	tr.step("release a node no one has: 404, not registered, as its beat answers", pool, release,
		at("id", "{{phantom}}"), jsonBody(`{}`))
	tr.step("a node no one has beats: 404, register again", pool, heartbeat, at("id", "{{phantom}}"))
	tr.step("a node id that is not a UUID beats", pool, heartbeat, at("id", "not-a-node"))
	tr.step("release a node id that is not a UUID", pool, release, at("id", "not-a-node"), jsonBody(`{}`))
	tr.step("the box key beats for the node: pool credentials are required", box, heartbeat, node)
	tr.step("the member releases the node: the same", m, release, node, jsonBody(`{}`))
}
