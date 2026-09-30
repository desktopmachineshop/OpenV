//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestTourS5eOverPlanTiersOn is part (3) of S5e, the over-plan pass, under
// the S4b tiers_on profile (refactor plan §6.4 S5e; invariant I3's plan
// read-only gate inside requireProjectRole and requireOrgRole; OpenV
// REQ-143, REQ-18, REQ-113, REQ-176 and REQ-177). Its golden is
// testdata/tour/s5e/over_plan_tiers_on.json; the self-hosted profile's pass
// is TestTourS5eOverPlanSelfHosted, and parts (1) and (2), the phantom and
// real-id matrices, are areas of their own.
//
// A workspace that holds more than its plan allows is read-only: every write
// that passes a project or workspace guard is then refused 403
// plan_read_only, with the limits it is past (over) and the remedy, until it
// is brought back under the plan (limits.go, requireWritable). Reads and
// export never are, and nine writes are exempt (alwaysWritable, the routes
// internal/api/testdata/route_handlers.txt marks). The area pins that by
// behavior, on one workspace W, which the owner administers and where m1 is
// a member and P's editor:
//
//   - W's limits on the single plan (two seats, both taken), then, once W
//     holds four seats (m2 added and an address invited while the platform
//     admin had W on business, then moved back to single), read-only, over
//     max_members: the single plan's members cap (REQ-176);
//   - one refused write or more per guard kind that carries the gate:
//     project:editor (the owner's rename of P; an artifact created by m1, by
//     W's worker key and by a run's token), project:owner, project:viewer,
//     project:reviewer, org:admin, org:member, runner-sessions, scoped-write
//     and run:editor, each 403 plan_read_only; and before them P's viewer's
//     rename, refused by its role first (the guard decides access, then the
//     gate: I3's order);
//   - the project creates and a launch with no project, which ask the gate
//     (requireProjectCreate, requireUnscopedLaunch) and are refused;
//   - the writes that pass a read-only workspace because nothing on their
//     path asks the gate: the launcher's own cancel (requireRunAccess passes
//     a launcher before the project ladder), marking notifications read (a
//     session route), and the worker wire (a claim of the run m1 launched
//     before W went read-only, and a log push);
//   - the hosted runner's claim, refused by the single plan's
//     hosted_automation flag (the refusal the S5d note left to S5e) and
//     taken on Business Lite, where the flag is on (REQ-176), which leaves W
//     read-only, since Business Lite has two seats too;
//   - REQ-113: the export and every document download of P, on the
//     read-only workspace;
//   - the nine always-writable routes, each answering as on a writable
//     workspace: the JSON and ReqIF imports of those exports, the four billing
//     writes (404 billing_unavailable: billingAdmin and RefreshOrgBilling ask
//     requireOrgRole, with its gate, before billingAvailable, so a dropped
//     exemption would answer 403 plan_read_only instead), a project's delete,
//     an invitation's revocation and a member's removal, which bring W back
//     under the plan, and last the workspace's delete;
//   - the project maximum: at the tier's 200, a new project, a project from a
//     template and an import are each refused limit_reached (quirk Q13, that
//     only POST /projects counted, is fixed); once the platform admin has had
//     W on business for a 201st project and moved it back to single, W is
//     read-only over max_projects, and a new project is refused by the gate,
//     plan_read_only, before the count.
//
// ImportProject asks the gate (requireProjectCreate), so its alwaysWritable
// is what lets the imports through on the read-only workspace: dropping it
// turns their 201 into 403 plan_read_only.
//
// Not reachable here: the grandfather step, which runs at boot over the
// workspaces created before OPENV_BILLING_GRANDFATHER_BEFORE, and a tour
// boots on an empty database (internal/persistence/postgres's
// TestGrandfatherBeforeKeepsTheAlphaTerms covers GrandfatherBefore). S5c's
// tiers area pins the rest of the single plan's limits whole (the
// shared-workspace ceiling, the 300 hosted minutes, the teams and budget
// flags).
//
// Pinned as they behave (plan R7), and listed for the maintainer: the member
// and admin writes that only concern the caller's own session, key or lease,
// or that revoke access, are refused on a read-only workspace like any other
// write (activating W, the stable preview, revoking one's own runner key or
// W's worker key, ending a runner lease, revoking a public share link); and
// the launcher's own cancel, which passes because its route asks no gate,
// though ReadOnlyRemedy says every write is refused.
func TestTourS5eOverPlanTiersOn(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5e",
		key:   "over_plan_tiers_on",
		about: "The over-plan pass under the tiers: a workspace over its single plan's seats is read-only (403 " +
			"plan_read_only) for a write of every guard kind that carries the gate, passes the writes that ask no " +
			"gate, the export and every document download (REQ-113) and the nine always-writable routes; the hosted " +
			"claim's plan flag, refused on single and taken on Business Lite (REQ-176); and every project create " +
			"refused at the tier's 200 projects. The grandfather step runs at boot over workspaces created before the " +
			"grandfather date, which " +
			"a tour on an empty database never has: internal/persistence/postgres's " +
			"TestGrandfatherBeforeKeepsTheAlphaTerms covers it.",
		run:      overPlanTiersOnTour,
		profiles: []string{"tiers_on"},
		accounts: []tourAccount{
			{name: "m1", display: "Tour Member One", about: "a member of W and an editor of P, with a personal runner key " +
				"in W that it never uses"},
			{name: "m2", display: "Tour Member Two", about: "the member past the single plan's two seats, and P's viewer"},
		},
	})
}

// overPlanTiersOn is what the area's phases share.
type overPlanTiersOn struct {
	tr               *tour
	o, admin, m1, m2 *tourActor
	k, run1          *tourActor
	export, reqif    *tourResult
}

func overPlanTiersOnTour(tr *tour) {
	// W's rename answers W, whose slug ends in the first 8 hex digits of its
	// id; the claimed run's agent's slug, which the pattern leaves alone, is
	// not checked.
	tr.slugPattern()
	// The V&V report's filename carries the day it was rendered, as in S5b.
	tr.pattern(`vv-report-.*-(\d{8})\.pdf`, "<yyyymmdd>", "the day the V&V report was rendered, in its filename")
	x := overPlanTiersOnSetup(tr)
	x.overSeats()
	x.refusals()
	x.passes()
	x.hostedClaim()
	x.exports(overPlanDocumentReads)
	x.alwaysWritable()
	x.projectCap()
	tr.actIn(x.o, "{{owner.workspace}}")
	tr.step("delete W, over its plan again, from the owner's personal workspace: always writable", x.o,
		"DELETE /api/v1/orgs/{id}", at("id", "{{w}}"))
}

// overPlanAgent is the direct-mode agent the pass's runs belong to.
func overPlanAgent() string {
	b, _ := json.Marshal(map[string]any{
		"slug": "tour-over", "name": "Tour Over", "description": "The S5e over-plan pass's agent.",
		"provider": tourDefaultProvider, "allowed_tools": []string{"get_artifact"}, "write_mode": "direct",
		"repo_access": false, "system_prompt": "You answer in one line.",
	})
	return string(b)
}

// overPlanMember is the body that adds an account to a workspace or a
// project with a role.
func overPlanMember(a *tourActor, role string) tourOpt {
	b, _ := json.Marshal(map[string]string{"email": a.email, "role": role})
	return rawBody("application/json", b)
}

func overPlanTiersOnSetup(tr *tour) *overPlanTiersOn {
	x := &overPlanTiersOn{tr: tr, o: tr.owner, admin: tr.admin, m1: tr.actor("m1"), m2: tr.actor("m2")}
	o := x.o
	tr.sharedWorkspace("w", "Tour Over Plan")
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Over P"}`),
		expect(http.StatusCreated)).capture("p", "/id")
	tr.setup("project Q, to delete", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Over Q"}`),
		expect(http.StatusCreated)).capture("q", "/id")
	tr.join(x.m1, "{{w}}", "member")
	tr.setup("m1 an editor of P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		overPlanMember(x.m1, "editor"))
	tr.setup("requirement A in P", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Answer in time","body":"The system shall answer within 2 s."}`)).capture("a", "/id")
	tr.setup("a card on P's board", o, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Tour Over card"}`)).capture("card", "/id")
	tr.setup("a public share link to P", o, "POST /api/v1/projects/{id}/share-links", at("id", "{{p}}"),
		jsonBody(`{"role":"public","label":"Tour Over link"}`)).capture("sl", "/id")
	tr.setup("a repository connection of P", o, "POST /api/v1/projects/{id}/repo-connections", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour repo","remote_url":"https://git.example.com/tour.git"}`)).capture("rc", "/id")
	tr.setup("the agent tour-over", o, "POST /api/v1/agents", jsonBody(overPlanAgent())).capture("agent", "/id")
	x.k = tr.workerKey("k", "{{w}}", "W's workspace worker key (a workspace runner)")
	tr.runnerKey("rk", x.m1, "{{w}}", "m1's personal runner key in W, which the area never sends, so that m1's "+
		"runner is never online and m1's launch is not reserved for it")
	tr.setup("the templates", o, "GET /api/v1/templates").
		captureWhere("template", "", "key", "guided-product-skeleton", "id")
	tr.queueRun("run1", o, "tour-over", `{"project_id":"{{p}}","prompt":"Summarise P."}`)
	x.run1 = tr.takeRun(x.k, "tour-over", "run1", "the token of a direct-mode run of tour-over in P, launched by "+
		"the owner and claimed by W's worker key: what an agent's tools send")
	tr.queueRun("run2", o, "tour-over", `{"project_id":"{{p}}","prompt":"Summarise P again."}`)
	tr.queueRun("run3", x.m1, "tour-over", `{"prompt":"Summarise W."}`)
	return x
}

// overSeats takes W past the single plan's two seats (REQ-176's members
// cap): on business, m2 takes a third and an invitation a fourth.
func (x *overPlanTiersOn) overSeats() {
	tr, o := x.tr, x.o
	limits := "GET /api/v1/orgs/{id}/limits"
	tr.step("W's limits on the single plan: two seats of two, writable", o, limits, at("id", "{{w}}"))
	tr.setup("the platform admin moves W to business", x.admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"),
		jsonBody(`{"plan":"business"}`))
	tr.join(x.m2, "{{w}}", "member")
	tr.setup("m2 a viewer of P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		overPlanMember(x.m2, "viewer"))
	tr.setup("invite an address with no account", o, "POST /api/v1/orgs/{id}/invitations", at("id", "{{w}}"),
		jsonBody(`{"email":"tour-invitee@example.com","role":"member"}`)).capture("inv", "/invitation/id")
	tr.setup("the platform admin moves W back to single", x.admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"),
		jsonBody(`{"plan":"single"}`))
	tr.step("W's limits: read-only, over max_members, four seats of two (invitations count)", o, limits,
		at("id", "{{w}}"), note("on business, m2 joined W, became P's viewer and an address was invited; then the "+
			"platform admin moved W back to single (setups)"))
}

// refusals sends a write of each guard kind that asks the gate.
func (x *overPlanTiersOn) refusals() {
	tr, o, m1 := x.tr, x.o, x.m1
	inW := at("id", "{{w}}")
	const gate = "403 plan_read_only: "

	// project:editor (and the order of guard and gate).
	tr.step("P's viewer m2 renames P: its role refuses it first (the guard decides access, then the gate)", x.m2,
		"PUT /api/v1/projects/{id}", at("id", "{{p}}"), jsonBody(`{"name":"Tour Over P by its viewer"}`))
	tr.step(gate+"project:editor, the owner renames P", o, "PUT /api/v1/projects/{id}", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour Over P renamed"}`))
	artifact := jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Log each answer",` +
		`"body":"The system shall log each answer."}`)
	tr.step(gate+"project:editor, P's editor m1 creates an artifact in P", m1, "POST /api/v1/artifacts", artifact)
	tr.step(gate+"project:editor, W's worker key creates an artifact in P", x.k, "POST /api/v1/artifacts", artifact)
	tr.step(gate+"project:editor, run1's token creates an artifact in its project", x.run1, "POST /api/v1/artifacts",
		artifact)

	// project:owner, project:viewer, project:reviewer.
	tr.step(gate+"project:owner, mint a share link to P", o, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{p}}"), jsonBody(`{"role":"public","label":"Tour Over second link"}`))
	tr.step(gate+"project:owner, revoke P's public share link", o, "DELETE /api/v1/share-links/{id}",
		at("id", "{{sl}}"))
	tr.step(gate+"project:viewer, m1 comments on P's card", m1, "POST /api/v1/work-items/{id}/comments",
		at("id", "{{card}}"), jsonBody(`{"body":"Still on it."}`))
	tr.step(gate+"project:viewer, m1 sets its own local path of P's repository", m1,
		"PUT /api/v1/repo-connections/{id}/my-path", at("id", "{{rc}}"), jsonBody(`{"local_path":"/home/m1/src/tour"}`))
	tr.step(gate+"project:reviewer, a chatter entry on A", o, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{a}}","message":"Looks right."}`))

	// org:admin.
	tr.step(gate+"org:admin, rename W", o, "PUT /api/v1/orgs/{id}", inW, jsonBody(`{"name":"Tour Over Plan renamed"}`))
	tr.step(gate+"org:admin, revoke W's worker key", o, "DELETE /api/v1/orgs/{id}/worker-keys/{keyId}",
		at("id", "{{w}}", "keyId", "{{k.key}}"))
	tr.step(gate+"org:admin, sync W's agent files", o, "POST /api/v1/agents/sync")
	tr.step(gate+"org:admin, import a workspace-wide crew (no project_id: the workspace admin's guard, before the "+
		"body is read)", o, "POST /api/v1/crews/import", jsonBody(`{"name":"Tour Over crew"}`))

	// org:member, as m1.
	tr.step(gate+"org:member, m1 makes W its session's active workspace", m1, "POST /api/v1/orgs/{id}/activate", inW)
	tr.step(gate+"org:member, m1 revokes its own runner key", m1, "DELETE /api/v1/orgs/{id}/my-runner-key", inW)
	tr.step(gate+"org:member, m1 turns the stable preview on for itself", m1, "PUT /api/v1/orgs/{id}/members/me/preview",
		inW, jsonBody(`{"enabled":true}`))
	tr.step(gate+"org:member, m1 publishes a shared product (W is its active workspace)", m1,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("tour", "Tour Over Product", "d", "v", "p", "t")))
	tr.step(gate+"org:member, m1 asks for a connector pairing code", m1, "POST /api/v1/orgs/{id}/connector-pairing", inW)

	// runner-sessions, scoped-write, run:editor.
	tr.step(gate+"runner-sessions, m1 ends its cloud runner lease (it holds none: the gate answers first)", m1,
		"DELETE /api/v1/orgs/{id}/runner-session", inW)
	tr.step(gate+"scoped-write, a workspace-wide attribute definition", o, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{w}}","key":"risk","label":"Risk","data_type":"text"}`))
	tr.step(gate+"scoped-write, an automation pinned to P", o, "POST /api/v1/automations",
		jsonBody(`{"name":"Tour Over watch","kind":"manual","agent_id":"{{agent}}","project_id":"{{p}}"}`))
	tr.step(gate+"run:editor, m1 cancels run2, which the owner launched in P (the project's ladder)", m1,
		"POST /api/v1/agent-runs/{id}/cancel", at("id", "{{run2}}"))
}

// passes sends the writes a read-only workspace still takes, since nothing
// on their path asks the gate, and in their midst the three that took it
// before they asked the gate too: two project creates and a launch with no
// project.
func (x *overPlanTiersOn) passes() {
	tr, o := x.tr, x.o
	tr.step("the owner cancels run2, which it launched: requireRunAccess passes a launcher before the project's "+
		"ladder, so no gate", o, "POST /api/v1/agent-runs/{id}/cancel", at("id", "{{run2}}"))
	tr.step("mark every notification read: a session route, no workspace, no gate", o,
		"POST /api/v1/notifications/read-all",
		elide("/updated", "<count>", 1, 3, "the notifier writes the joins' notifications after the setups "+
			"answered, so how many it has written by now is not the server's to fix"),
		elide("/unread_count", "<count>", 1, 3, "the same"))
	tr.step("403 plan_read_only: project-create, a project in W (the gate before the count)", o,
		"POST /api/v1/projects", jsonBody(`{"name":"Tour Over new while read-only"}`))
	tr.step("403 plan_read_only: project-create, a project from the default template", o,
		"POST /api/v1/templates/{id}/projects", at("id", "{{template}}"),
		jsonBody(`{"name":"Tour Over from the template"}`))
	tr.step("403 plan_read_only: unscoped-launch, m1 launches tour-over with no project", x.m1,
		"POST /api/v1/agents/{slug}/runs", at("slug", "tour-over"), jsonBody(`{"prompt":"Summarise W."}`))
	tr.step("W's worker key claims, not hosted: the worker wire asks no gate, and takes m1's run, the one queued",
		x.k, "POST /api/v1/agent-runs/claim", claimBody("tour-over", tourDefaultProvider),
		note("m1 launched run3 with no project while W was writable (setup); run2 was cancelled and run1 is "+
			"claimed, so run3 is the one queued run")).
		claimed("run3").runToken("run3", "the token W's worker key's claim of run3 handed out; never sent")
	tr.step("W's worker key pushes run1's log: the worker wire again", x.k, "POST /api/v1/agent-runs/{id}/logs",
		at("id", "{{run1}}"), jsonBody(`{"entries":[{"seq":1,"kind":"text","payload":{"text":"Reading P."}}],`+
			`"partial_text":"P holds"}`))
}

// hostedClaim is the hosted runner's claim against the plan's
// hosted_automation flag, which S5d's note left to S5e.
func (x *overPlanTiersOn) hostedClaim() {
	tr := x.tr
	claim := "POST /api/v1/agent-runs/claim"
	tr.step("W's worker key claims as a hosted runner: the single plan does not include hosted automation", x.k,
		claim, hostedClaimBody("tour-hosted", tourDefaultProvider))
	tr.setup("the platform admin moves W to Business Lite", x.admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"),
		jsonBody(`{"plan":"business_lite"}`))
	tr.step("the same hosted claim on Business Lite, which includes hosted automation (REQ-176): 204, nothing "+
		"queued", x.k, claim, hostedClaimBody("tour-hosted", tourDefaultProvider),
		note("the platform admin moved W to business_lite just before (setup)"))
	tr.step("W's limits on Business Lite: still read-only, two seats there too", x.o, "GET /api/v1/orgs/{id}/limits",
		at("id", "{{w}}"))
	tr.setup("the platform admin moves W back to single", x.admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"),
		jsonBody(`{"plan":"single"}`))
}

// overPlanDocumentReads are the export and document routes of a project
// that the pass reads on a read-only workspace (REQ-113), with the query
// each is sent with and why its times are whole seconds, if they are.
var overPlanDocumentReads = []struct{ route, query, whole string }{
	{"GET /api/v1/projects/{id}/export", "", "the JSON export's times"},
	{"GET /api/v1/projects/{id}/export", "format=reqif", ""},
	{"GET /api/v1/projects/{id}/download/json", "", ""},
	{"GET /api/v1/projects/{id}/download/csv", "", "the CSV's created and updated columns"},
	{"GET /api/v1/projects/{id}/download/excel", "", "the workbook's created and updated cells"},
	{"GET /api/v1/projects/{id}/download/docx", "", "the Word file's core properties"},
	{"GET /api/v1/projects/{id}/download/pdf", "", ""},
	{"GET /api/v1/projects/{id}/download/reqif", "", "CREATION-TIME and each LAST-CHANGE"},
	{"GET /api/v1/projects/{id}/download/options", "", ""},
	{"GET /api/v1/projects/{id}/report", "", "a document's generated and captured times and its core properties"},
	{"GET /api/v1/projects/{id}/vv/report", "", ""},
	{"GET /api/v1/projects/{id}/ai-map", "", "its Generated line"},
}

// overPlanReads reads P's export and documents on a read-only workspace
// (REQ-113), once each (S5a pins them by structure; the 200 is the point
// here), and returns the JSON export and the ReqIF export, for the imports.
func overPlanReads(tr *tour, reads []struct{ route, query, whole string }) (export, reqif *tourResult) {
	tr.t.Helper()
	for _, d := range reads {
		if d.whole != "" {
			tr.wholeSeconds(d.route, d.whole)
		}
		if d.route == "GET /api/v1/projects/{id}/download/excel" {
			tr.keep("2006-09-16T00:00:00Z", "excelize's fixed docProps/core.xml date: a workbook records no export time "+
				"there")
		}
	}
	for _, d := range reads {
		title := "REQ-113 on the read-only workspace: " + d.route[len("GET /api/v1/projects/{id}"):]
		opts := []tourOpt{at("id", "{{p}}"), once("S5a pins the documents by structure; the 200 on a read-only " +
			"workspace is the point here")}
		if d.query != "" {
			title += "?" + d.query
			opts = append(opts, query(d.query))
		}
		res := tr.step(title, tr.owner, d.route, opts...)
		switch {
		case d.route == "GET /api/v1/projects/{id}/export" && d.query == "":
			export = res
		case d.route == "GET /api/v1/projects/{id}/export":
			reqif = res
		}
	}
	return export, reqif
}

func (x *overPlanTiersOn) exports(reads []struct{ route, query, whole string }) {
	x.export, x.reqif = overPlanReads(x.tr, reads)
}

// alwaysWritable walks the nine exempt routes on the read-only workspace.
func (x *overPlanTiersOn) alwaysWritable() {
	tr, o := x.tr, x.o
	inW := at("id", "{{w}}")
	limits := "GET /api/v1/orgs/{id}/limits"
	tr.step("always writable: import P's JSON export into W", o, "POST /api/v1/projects/import",
		answerOf(x.export, "application/json")).capture("imported_json", "/project_id")
	tr.step("always writable: import P's ReqIF export into W", o, "POST /api/v1/projects/import", query("format=reqif"),
		answerOf(x.reqif, "application/json")).capture("imported_reqif", "/project_id")
	overPlanBilling(tr, o, "a hosted deployment with no payment provider")
	tr.step("always writable: delete project Q", o, "DELETE /api/v1/projects/{id}", at("id", "{{q}}"))
	tr.step("always writable: revoke the invitation, one seat fewer", o,
		"DELETE /api/v1/orgs/{id}/invitations/{invId}", at("id", "{{w}}", "invId", "{{inv}}"))
	tr.step("W's limits: three seats of two, still read-only", o, limits, inW)
	tr.step("always writable: remove m2, back to two seats", o, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{w}}", "userId", "{{m2}}"))
	tr.step("W's limits: under the plan, writable again", o, limits, inW)
	tr.step("rename W: writable again", o, "PUT /api/v1/orgs/{id}", inW, jsonBody(`{"name":"Tour Over Plan Renamed"}`))
}

// overPlanBilling sends the four billing writes, each always writable, on
// a deployment with no payment provider (where).
func overPlanBilling(tr *tour, a *tourActor, where string) {
	tr.t.Helper()
	why := note("billingAdmin (checkout, change, portal) and RefreshOrgBilling ask requireOrgRole, with its plan " +
		"gate, before billingAvailable, so without the exemption this would answer 403 plan_read_only: 404 " +
		"billing_unavailable is the exemption at work, and no payment stand-in is needed")
	order := jsonBody(`{"plan":"business","interval":"month"}`)
	inW := at("id", "{{w}}")
	tr.step("always writable: refresh W's billing, "+where, a, "POST /api/v1/orgs/{id}/billing/refresh", inW, why)
	tr.step("always writable: a checkout for W, "+where, a, "POST /api/v1/orgs/{id}/billing/checkout", inW, order, why)
	tr.step("always writable: a plan change for W, "+where, a, "POST /api/v1/orgs/{id}/billing/change", inW, order, why)
	tr.step("always writable: W's billing portal, "+where, a, "POST /api/v1/orgs/{id}/billing/portal", inW, why)
}

// overPlanFill creates projects in W, as setup, until W holds want, reading
// how many it holds from its limits.
func overPlanFill(tr *tour, want int) {
	tr.t.Helper()
	res := tr.setup("W's limits, for the projects it holds", tr.owner, "GET /api/v1/orgs/{id}/limits", at("id", "{{w}}"))
	var limits struct {
		Limits []struct {
			Key  string `json:"key"`
			Used *int   `json:"used"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(res.body, &limits); err != nil {
		tr.t.Fatalf("read W's limits: %v\n%s", err, res.body)
	}
	used := -1
	for _, l := range limits.Limits {
		if l.Key == "max_projects" && l.Used != nil {
			used = *l.Used
		}
	}
	if used < 0 || used > want {
		tr.t.Fatalf("W's limits name no usable max_projects count (%d), or one past %d\n%s", used, want, res.body)
	}
	for n := used + 1; n <= want; n++ {
		tr.setup(fmt.Sprintf("project %d of %d", n, want), tr.owner, "POST /api/v1/projects",
			jsonBody(fmt.Sprintf(`{"name":"Tour fill %03d"}`, n)), expect(http.StatusCreated))
	}
}

// projectCap is the project maximum at the tier's 200 projects, which every
// project create counts toward, and a workspace over it.
func (x *overPlanTiersOn) projectCap() {
	tr, o := x.tr, x.o
	inW := at("id", "{{w}}")
	limits := "GET /api/v1/orgs/{id}/limits"
	overPlanFill(tr, 200)
	tr.step("W's limits: 200 projects of 200, writable", o, limits, inW,
		note("the owner created projects up to the single plan's 200 just before (setups, Tour fill NNN)"))
	tr.step("a new project past the 200 is refused limit_reached, the remedy the Billing tab", o,
		"POST /api/v1/projects", jsonBody(`{"name":"Tour Over one too many"}`))
	tr.step("a project from the default template counts too: refused limit_reached", o,
		"POST /api/v1/templates/{id}/projects", at("id", "{{template}}"), jsonBody(`{"name":"Tour Over past the cap"}`))
	tr.step("an import counts too: refused limit_reached", o, "POST /api/v1/projects/import",
		answerOf(x.export, "application/json"))
	tr.setup("the platform admin moves W to business", x.admin, "PUT /api/v1/orgs/{id}/plan", inW,
		jsonBody(`{"plan":"business"}`))
	tr.setup("the 201st project, on business", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Over past the cap"}`),
		expect(http.StatusCreated))
	tr.setup("the platform admin moves W back to single", x.admin, "PUT /api/v1/orgs/{id}/plan", inW,
		jsonBody(`{"plan":"single"}`))
	tr.step("W's limits: 201 projects of 200, read-only, over max_projects", o, limits, inW,
		note("on business the owner created a 201st project; then the platform admin moved W back to single "+
			"(setups)"))
	tr.step("rename P: 403 plan_read_only, naming Projects", o, "PUT /api/v1/projects/{id}", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour Over P renamed"}`))
	tr.step("a new project on the read-only workspace: 403 plan_read_only, the gate before the count", o,
		"POST /api/v1/projects", jsonBody(`{"name":"Tour Over one too many"}`))
}
