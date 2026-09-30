//go:build unix

package main

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// TestTourS5cTiersSecureRunnerSessions is the S5c tour's area for the plan
// tiers, secure cookies and the transient runners (refactor plan §6.4 S5c,
// before M9 and M14, and the guard X8 and X12 rely on for the effective
// limits and plan gates of workspace routes; invariants I3, I4, I5 and I8
// (the openv_session and Google state cookies' attributes under
// SECURE_COOKIES, HSTS); quirk Q5;
// OpenV REQ-18, REQ-143, REQ-154). Its golden is
// testdata/tour/s5c/tiers_secure_runner_sessions.json.
//
// The server boots under two S4b profiles, tiers_on (the grandfather date,
// which turns the plan tiers on for every workspace created after it, so for
// every workspace of the tour) and secure_cookies, and with a runner pool key
// of its own, which wires the transient runners.
//
// The area walks, in order: the session cookie under SECURE_COOKIES (a
// recorded registration's and a sign-in's cookies carry Secure beside
// HttpOnly and SameSite=Lax, and no Partitioned, which only a cross-site
// deployment's carry; a sign-out clears the cookie with Secure too; HSTS is
// among the standard security headers every answer carries); then the tiers
// under the single plan, the one a new workspace starts on: the limits of a
// personal workspace and of a shared one, whole (two seats, one shared
// workspace per account, 200 projects, 300 hosted minutes a month, and
// hosted automation, teams and the workspace budget not included); the
// owner's first shared workspace W made by a recorded POST /orgs, whose
// answer carries release_channel "" and locked false (Q5, pinned as it is),
// and a second one refused by the account's own ceiling; the seats (a member
// takes the second, and an invitation or a member past it are refused, the
// refusal counting invitations not yet accepted); the flags (a team, a
// budget and the usage rollup refused; a plain rename still passes, while a
// rename sent with a budget is refused with the budget and stores nothing);
// the hosted runner, which answers that hosted runners are off on
// this deployment before it reads the flag, so the flag's refusal cannot be
// reached; the platform admin's plan change to business, where a team, the
// usage rollup and a project's grant to the team pass, and back to single,
// where the grant is refused again and what was granted stays readable (the
// plan change's answer keeps W on nightly, as a checkout does, since the
// R7 fix for #379's question 19 that regenerated it); and a workspace over
// its plan (three members and an invitation on business, moved back to
// single): read-only, its limits saying which limit it is past, a rename
// refused with plan_read_only, and so is a runner lease, while revoking the
// invitation and removing a member, the always-writable writes, pass and
// bring it back under the plan. Then the transient runners, member side: the
// member's lease before the pool has a node (none, then 503 with the same
// payload), and once a pool node has registered (setup, as the pool): a lease
// of the single plan's length, the lease read back, extended, listed for the
// workspace's admin and refused to a member, ended, then extending and
// ending with none; the pool key and an outsider on the member routes; and a
// lease on the business plan, four hours long. Last, a workspace over its
// plan is deleted: DeleteOrg is always writable too.
//
// A lease's expires_at and deadline are times to come, written <time>, so
// the length of each lease and its idle window are pinned in the step's
// notes, measured from the answer's own times (tiersSecureRunnerSessionsLease):
// 60 minutes on the single plan (its runner_session_minutes, under the 300
// hosted minutes left) and 240 on business. seconds_remaining counts down to
// the deadline, which is the idle window's end (15 minutes on single, 30 on
// business), the earlier of the two, not the lease's; an area pattern pins it
// within five seconds.
//
// Not pinned: the hosted-minutes exhaustion 403 (it needs 300 minutes of
// leases used in the month), a lease shortened to what is left of the month's
// allowance (the same), a lease's idle expiry and the sweep (time-bound: 15
// minutes at least), the pool node's heartbeat, which hands the lease's worker
// key over, and its release (S5d's worker wire), the over-plan pass over every
// route (S5e's matrix), the project ceiling's 403 (200 projects), a plan's
// gates on the stable channel's features, and the hosted runner's own flag
// refusal (hosted runners are off in every tour server, and the handler
// answers so before it reads the flag).
func TestTourS5cTiersSecureRunnerSessions(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "tiers_secure_runner_sessions",
		about: "Plan tiers, secure cookies and transient runners: the session cookie under SECURE_COOKIES, the single " +
			"plan's limits, seats, flags and read-only state under the tiers (X8, X12), a platform admin's plan " +
			"changes, the always-writable writes, and a member's cloud runner lease from the runner pool.",
		run:      tiersSecureRunnerSessionsTour,
		profiles: []string{"tiers_on", "secure_cookies"},
		env: map[string]string{
			"RUNNER_POOL_KEY":      tiersSecureRunnerSessionsPoolKey,
			"GOOGLE_CLIENT_ID":     "tour-google-client",
			"GOOGLE_CLIENT_SECRET": "tour-google-secret",
		},
		accounts: []tourAccount{
			{name: "m1", display: "Tour Member One", about: "a member of W, which leases a cloud runner there"},
			{name: "m2", display: "Tour Member Two", about: "the member W's seats refuse on the single plan, " +
				"and that takes a third seat on business"},
		},
	})
}

// tiersSecureRunnerSessionsPoolKey is the deployment's runner pool key, the
// bearer the pool's nodes present.
const tiersSecureRunnerSessionsPoolKey = "tour-pool-key"

// tiersSecureRunnerSessionsLease adds to a step answering a runner lease a
// note pinning what its future times hold: the lease's length (expires_at
// less last_activity_at, which Start and Extend both set to the same now)
// and its idle window (deadline less last_activity_at), in whole minutes.
// An answer that holds no lease gets a note saying so, so that the area
// runs on and the golden's comparison shows what changed.
func tiersSecureRunnerSessionsLease(tr *tour, res *tourResult) {
	tr.t.Helper()
	timeAt := func(pointer string) (time.Time, bool) {
		v, _ := jsonValue(res.body, pointer)
		s, _ := v.(string)
		tm, err := time.Parse(time.RFC3339Nano, s)
		return tm, err == nil
	}
	last, ok1 := timeAt("/session/last_activity_at")
	expires, ok2 := timeAt("/session/expires_at")
	deadline, ok3 := timeAt("/deadline")
	text := "(no lease in the answer)"
	if ok1 && ok2 && ok3 {
		minutes := func(d time.Duration) int { return int(math.Round(d.Minutes())) }
		text = fmt.Sprintf("the lease runs %d minutes (expires_at less last_activity_at), and the runner goes at "+
			"deadline, %d minutes after last_activity_at (the idle window, the earlier of the two), which "+
			"seconds_remaining counts down to", minutes(expires.Sub(last)), minutes(deadline.Sub(last)))
	}
	res.note(text)
}

func tiersSecureRunnerSessionsTour(tr *tour) {
	o, admin, anon := tr.owner, tr.admin, tr.anon
	m1, m2 := tr.actor("m1"), tr.actor("m2")
	tr.slugPattern()
	tr.pattern(`"seconds_remaining":(89[5-9]|900)\b`, "<about 900 s>", "seconds_remaining on a single-plan lease: "+
		"the whole seconds to its deadline, 15 minutes (the idle window) after the lease started or was extended, "+
		"less the seconds since, truncated (899 within a second of it)")
	tr.pattern(`"seconds_remaining":(179[5-9]|1800)\b`, "<about 1800 s>", "seconds_remaining on a business-plan "+
		"lease: the same, to a deadline 30 minutes on")

	// The session cookie under SECURE_COOKIES (I8). The fifth registration
	// from 127.0.0.1: admin, owner, m1 and m2 took four of the address's five.
	reg := tr.step("register an account: its session cookie carries Secure, HttpOnly and SameSite=Lax, and no "+
		"Partitioned", anon, "POST /api/v1/auth/register",
		jsonBody(`{"email":"tour-secure@example.com","password":"tour password 1","name":"Tour Secure"}`),
		note("the fifth registration from the tour's address, the last its budget allows: admin, owner, m1 and m2 "+
			"were the first four"))
	tr.adopt(reg, "secure", fmt.Sprintf("the account a recorded registration made (step %d)", reg.step.n))
	login := tr.step("m1 signs in: a session of its own, with the same attributes", anon, "POST /api/v1/auth/login",
		jsonBody(fmt.Sprintf(`{"email":%q,"password":%q}`, m1.email, tourPassword)))
	second := tr.session(m1, login, "m1.second", fmt.Sprintf("a second session of m1's, from a recorded sign-in "+
		"(step %d), which the area signs out", login.step.n))
	tr.step("sign the second session out: the cookie that clears it carries Secure too", second,
		"POST /api/v1/auth/logout")
	tr.step("the session signed out", second, "GET /api/v1/auth/me")

	// The tiers, on the single plan every new workspace starts on.
	tr.step("the owner's personal workspace's limits, whole: one seat (fixed), one shared workspace, 200 projects, "+
		"300 hosted minutes, and no hosted automation, teams or workspace budget", o,
		"GET /api/v1/orgs/{id}/limits", at("id", "{{owner.workspace}}"))
	created := tr.step("the owner creates W: release_channel \"\" and locked false (Q5)", o, "POST /api/v1/orgs",
		jsonBody(`{"name":"Tour Tiers"}`))
	w := created.capture("w", "/id")
	tr.actIn(o, w)
	tr.setup("project P in W", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour tiers"}`)).capture("p", "/id")
	tr.step("W's limits, whole: two seats, the owner's one taken", o, "GET /api/v1/orgs/{id}/limits", at("id", "{{w}}"))
	tr.step("a second shared workspace: past the account's own ceiling of one", o, "POST /api/v1/orgs",
		jsonBody(`{"name":"Tour Tiers Two"}`))

	// Seats.
	tr.step("add m1 to W: the second seat", o, "POST /api/v1/orgs/{id}/members", at("id", "{{w}}"),
		jsonBody(fmt.Sprintf(`{"email":%q,"role":"member"}`, m1.email)))
	tr.actIn(m1, w)
	tr.step("invite an address with no account: no seat left, invitations counted", o,
		"POST /api/v1/orgs/{id}/invitations", at("id", "{{w}}"),
		jsonBody(`{"email":"tour-invitee@example.com","role":"member"}`))
	tr.step("add m2: the same refusal", o, "POST /api/v1/orgs/{id}/members", at("id", "{{w}}"),
		jsonBody(fmt.Sprintf(`{"email":%q,"role":"member"}`, m2.email)))

	// Flags.
	tr.step("create a team: the single plan does not include teams", o, "POST /api/v1/orgs/{id}/teams",
		at("id", "{{w}}"), jsonBody(`{"name":"Tour Team","description":"people"}`))
	tr.step("set a monthly budget: the single plan does not include the workspace budget", o, "PUT /api/v1/orgs/{id}",
		at("id", "{{w}}"), jsonBody(`{"monthly_budget_usd":100}`))
	tr.step("rename W: a plain rename is no budget, and passes", o, "PUT /api/v1/orgs/{id}", at("id", "{{w}}"),
		jsonBody(`{"name":"Tour Tiers Renamed"}`))
	tr.step("rename W and set a budget in one request: the budget is refused, and the rename with it", o,
		"PUT /api/v1/orgs/{id}", at("id", "{{w}}"), jsonBody(`{"name":"Tour Tiers Budgeted","monthly_budget_usd":100}`),
		note("UpdateOrg checks the budget's flag, with every other part of the request, before it writes any"))
	tr.step("W as it now is: the name of the plain rename, which the refused request left", o, "GET /api/v1/orgs/{id}",
		at("id", "{{w}}"))
	tr.step("the workspace's usage rollup: the workspace budget's flag again", o, "GET /api/v1/orgs/{id}/usage",
		at("id", "{{w}}"))
	tr.step("create a hosted runner: hosted runners are off on this deployment, which the handler answers before "+
		"it reads the hosted_automation flag", o, "POST /api/v1/orgs/{id}/hosted-runner", at("id", "{{w}}"),
		jsonBody(`{}`))

	// The platform admin moves W between plans (REQ-154).
	tr.step("the platform admin moves W to business", admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"),
		jsonBody(`{"plan":"business"}`))
	team := tr.step("create a team: business includes teams", o, "POST /api/v1/orgs/{id}/teams", at("id", "{{w}}"),
		jsonBody(`{"name":"Tour Team","description":"people"}`))
	team.capture("team", "/id")
	tr.step("the workspace's usage rollup: business includes the workspace budget", o, "GET /api/v1/orgs/{id}/usage",
		at("id", "{{w}}"))
	grant := jsonBody(`{"org_team_id":"{{team}}","role":"editor"}`)
	tr.step("grant the team P", o, "PUT /api/v1/projects/{id}/team-access", at("id", "{{p}}"), grant)
	tr.step("the platform admin moves W back to single", admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"),
		jsonBody(`{"plan":"single"}`))
	tr.step("grant the team P again: refused, teams are not on the single plan", o,
		"PUT /api/v1/projects/{id}/team-access", at("id", "{{p}}"), grant)
	tr.step("what was granted stays readable: reads are never refused", o, "GET /api/v1/projects/{id}/team-access",
		at("id", "{{p}}"))

	// Over plan: three members and an invitation on business, then single.
	tr.setup("W to business", admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"), jsonBody(`{"plan":"business"}`))
	tr.step("on business, add m2: a third seat", o, "POST /api/v1/orgs/{id}/members", at("id", "{{w}}"),
		jsonBody(fmt.Sprintf(`{"email":%q,"role":"member"}`, m2.email)))
	tr.step("and invite an address with no account: a fourth", o, "POST /api/v1/orgs/{id}/invitations",
		at("id", "{{w}}"), jsonBody(`{"email":"tour-invitee@example.com","role":"member"}`)).
		capture("invitation", "/invitation/id")
	tr.setup("W back to single", admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"), jsonBody(`{"plan":"single"}`))
	tr.step("W's limits: read-only, over its seats", o, "GET /api/v1/orgs/{id}/limits", at("id", "{{w}}"),
		note("the platform admin moved W back to the single plan just before (setup)"))
	tr.step("rename W: refused, the workspace is read-only", o, "PUT /api/v1/orgs/{id}", at("id", "{{w}}"),
		jsonBody(`{"name":"Tour Tiers Over"}`))
	tr.step("m1 leases a cloud runner: refused by the same gate, before the pool is asked", m1,
		"POST /api/v1/orgs/{id}/runner-session", at("id", "{{w}}"))
	tr.step("revoke the invitation: always writable, and one seat fewer, still over", o,
		"DELETE /api/v1/orgs/{id}/invitations/{invId}", at("id", "{{w}}", "invId", "{{invitation}}"))
	tr.step("remove m2: always writable, and back to two seats", o, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{w}}", "userId", "{{m2}}"))
	tr.step("W's limits: under the plan again", o, "GET /api/v1/orgs/{id}/limits", at("id", "{{w}}"))
	tr.step("rename W: writable again", o, "PUT /api/v1/orgs/{id}", at("id", "{{w}}"), jsonBody(`{"name":"Tour Tiers"}`))

	// Transient runners, the member side, on the single plan.
	session := "POST /api/v1/orgs/{id}/runner-session"
	inW := at("id", "{{w}}")
	tr.step("m1's cloud runner: none, and a pool with no node", m1, "GET /api/v1/orgs/{id}/runner-session", inW)
	tr.step("m1 leases one: nothing in the pool to lease, 503 with the same payload", m1, session, inW)
	pool := tr.bearerActor("pool", tiersSecureRunnerSessionsPoolKey, "a runner pool node, presenting the "+
		"deployment's pool key (RUNNER_POOL_KEY) as its bearer")
	tr.setup("a pool node registers", pool, "POST /api/v1/runner-pool/nodes",
		jsonBody(`{"name":"tour-node-1","pool":"","providers":["claude"]}`), expect(201)).capture("node1", "/id")
	lease := tr.step("m1 leases one: the node, on the single plan's lease", m1, session, inW,
		note("a pool node registered just before (setup)"))
	tiersSecureRunnerSessionsLease(tr, lease)
	lease.captureIf("lease", "/session/id") // when the answer holds a lease (the golden compares the rest)
	read, extend, end := "GET /api/v1/orgs/{id}/runner-session", "POST /api/v1/orgs/{id}/runner-session/extend",
		"DELETE /api/v1/orgs/{id}/runner-session"
	tiersSecureRunnerSessionsLease(tr, tr.step("m1's cloud runner: the lease", m1, read, inW))
	tiersSecureRunnerSessionsLease(tr, tr.step("m1 leases again: the lease it holds, no second node", m1, session, inW))
	tiersSecureRunnerSessionsLease(tr, tr.step("m1 extends it", m1, extend, inW))
	tr.step("the workspace's runner pool, for W's admin: the pool's counts and W's leases", o,
		"GET /api/v1/orgs/{id}/runner-pool", inW)
	tr.step("the same, for a member: admins only", m1, "GET /api/v1/orgs/{id}/runner-pool", inW)
	tr.step("the pool key on a member route: no account, the guard's 401", pool, read, inW)
	tr.step("m2's cloud runner in W, which it has left", m2, read, inW)
	tr.step("m1 ends its lease", m1, end, inW)
	tr.step("extend with no lease", m1, extend, inW)
	tr.step("end with no lease: the payload all the same", m1, end, inW)
	tr.step("m1's cloud runner: none again, and the node still draining", m1, read, inW)

	// A lease on the business plan.
	tr.setup("W to business, for its lease", admin, "PUT /api/v1/orgs/{id}/plan", inW, jsonBody(`{"plan":"business"}`))
	tr.setup("a second pool node registers", pool, "POST /api/v1/runner-pool/nodes",
		jsonBody(`{"name":"tour-node-2","pool":"","providers":["claude","codex"]}`), expect(201)).capture("node2", "/id")
	tiersSecureRunnerSessionsLease(tr, tr.step("m1 leases one on the business plan: its longer lease and idle window",
		m1, session, inW, note("the platform admin moved W to business, and a second pool node registered, just "+
			"before (setups)")))
	tr.step("the workspace's runner pool: the second node leased, the first draining", o,
		"GET /api/v1/orgs/{id}/runner-pool", inW)

	// DeleteOrg is always writable: W over its plan again, then deleted.
	tr.setup("m2 back in W on business: a third seat", o, "POST /api/v1/orgs/{id}/members", inW,
		jsonBody(fmt.Sprintf(`{"email":%q,"role":"member"}`, m2.email)), expect(201))
	tr.setup("W back to single: over its plan", admin, "PUT /api/v1/orgs/{id}/plan", inW, jsonBody(`{"plan":"single"}`))
	tr.step("W's limits: read-only again", o, "GET /api/v1/orgs/{id}/limits", inW,
		note("m2 was added back on business and W moved to single just before (setups)"))
	tr.actIn(o, "{{owner.workspace}}")
	tr.step("delete W, from the owner's personal workspace: always writable", o, "DELETE /api/v1/orgs/{id}", inW)

	// Google's state cookie under SECURE_COOKIES alone (I8): Secure, with
	// SameSite=Lax and no Partitioned (the cross-site area has the other
	// case). No stand-in is needed: the start only redirects.
	tr.step("start Google sign-on: its state cookie is Secure and SameSite=Lax under SECURE_COOKIES (I8)", anon,
		"GET /api/v1/auth/google", once("each single sign-on request spends a token of the address's budget"))
}
