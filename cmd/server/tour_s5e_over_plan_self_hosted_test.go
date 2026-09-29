//go:build unix

package main

import (
	"fmt"
	"net/http"
	"testing"
)

// TestTourS5eOverPlanSelfHosted is part (3) of S5e, the over-plan pass,
// under the S4b self_hosted profile with the limits profile's deployment
// limit, OPENV_LIMITS={"max_projects":7} (refactor plan §6.4 S5e; invariant
// I3's plan gate; quirk Q13; OpenV REQ-143, REQ-18, REQ-113 and REQ-176). Its
// golden is testdata/tour/s5e/over_plan_self_hosted.json; the tiers' pass is
// TestTourS5eOverPlanTiersOn, whose helpers this area shares.
//
// What a self-hosted deployment changes: every workspace's base plan is
// self_host, whatever its row says, so seats, projects and shared
// workspaces are unlimited and every flag is on (the hosted claim passes);
// only OPENV_LIMITS or a workspace's own limits cap anything; the limits say
// self_hosted true; a limit_reached remedy, and a plan_read_only one, name
// OPENV_LIMITS, not the Billing tab; and billing answers 404
// billing_unavailable everywhere. The self_hosted profile alone never makes
// a workspace read-only, so the limits profile's seven projects do here.
//
// The area walks, on W (the owner's shared workspace, where m1 is a member
// and P's editor, m2 a member, and an address invited):
//   - W's limits with four seats, the shape the tiers made read-only:
//     writable, seats unlimited, self_hosted true;
//   - the hosted claim of W's worker key, 204 (every flag on), and W's
//     billing, 404 billing_unavailable;
//   - Q13 at the deployment's 7: a new project refused limit_reached with
//     the OPENV_LIMITS remedy, a template's project and an import passing and
//     pushing W over, read-only;
//   - refusals, 403 plan_read_only, their remedy OPENV_LIMITS: the owner's
//     rename of P and of W, the worker key's artifact in P, and the worker
//     key's revocation; POST /projects still limit_reached, and a template's
//     project still 201;
//   - REQ-113: the export, the JSON and ReqIF downloads, and the JSON and
//     ReqIF imports of the exports, on the read-only workspace;
//   - the always-writable routes: the four billing writes (404
//     billing_unavailable, which the gate would have answered 403 first),
//     the invitation's revocation and m2's removal (W stays read-only: its
//     projects are what it is over), the deletes of the five projects the pass
//     made past the seven (W writable again at seven), and W's delete.
func TestTourS5eOverPlanSelfHosted(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5e",
		key:   "over_plan_self_hosted",
		about: "The over-plan pass on a self-hosted deployment with OPENV_LIMITS={\"max_projects\":7}: seats " +
			"unlimited and every flag on (the hosted claim passes), billing unavailable, Q13 at the deployment's " +
			"seven projects with the OPENV_LIMITS remedy, the read-only workspace's refusals (their remedy " +
			"OPENV_LIMITS too), export and import (REQ-113), and the always-writable routes.",
		run:      overPlanSelfHostedTour,
		profiles: []string{"self_hosted", "limits"},
		accounts: []tourAccount{
			{name: "m1", display: "Tour Member One", about: "a member of W and an editor of P"},
			{name: "m2", display: "Tour Member Two", about: "a third member of W, past the seats the tiers would allow"},
		},
	})
}

// overPlanSelfHostedReads are the export and downloads the area reads on
// its read-only workspace.
var overPlanSelfHostedReads = []struct{ route, query, whole string }{
	{"GET /api/v1/projects/{id}/export", "", "the JSON export's times"},
	{"GET /api/v1/projects/{id}/export", "format=reqif", ""},
	{"GET /api/v1/projects/{id}/download/json", "", ""},
	{"GET /api/v1/projects/{id}/download/reqif", "", "CREATION-TIME and each LAST-CHANGE"},
}

func overPlanSelfHostedTour(tr *tour) {
	o := tr.owner
	m1, m2 := tr.actor("m1"), tr.actor("m2")
	inW := at("id", "{{w}}")
	limits := "GET /api/v1/orgs/{id}/limits"

	tr.sharedWorkspace("w", "Tour Over Self-Hosted")
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Over P"}`),
		expect(http.StatusCreated)).capture("p", "/id")
	tr.join(m1, "{{w}}", "member")
	tr.setup("m1 an editor of P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		overPlanMember(m1, "editor"))
	tr.join(m2, "{{w}}", "member")
	tr.setup("invite an address with no account", o, "POST /api/v1/orgs/{id}/invitations", inW,
		jsonBody(`{"email":"tour-invitee@example.com","role":"member"}`)).capture("inv", "/invitation/id")
	k := tr.workerKey("k", "{{w}}", "W's workspace worker key (a workspace runner)")
	tr.setup("the templates", o, "GET /api/v1/templates").
		captureWhere("template", "", "key", "guided-product-skeleton", "id")

	// Seats, flags and billing.
	tr.step("W's limits with four seats (the owner, m1, m2 and an invitation): self_hosted, seats unlimited, "+
		"writable", o, limits, inW, note("under the tiers the same four seats made W read-only "+
		"(over_plan_tiers_on.json)"))
	tr.step("W's worker key claims as a hosted runner: 204, every flag is on (nothing queued)", k,
		"POST /api/v1/agent-runs/claim", hostedClaimBody("tour-hosted", tourDefaultProvider))
	tr.step("W's billing: 404 billing_unavailable", o, "GET /api/v1/orgs/{id}/billing", inW)

	// Q13 at the deployment's seven.
	overPlanFill(tr, 7)
	tr.step("Q13: an eighth project is refused limit_reached, the remedy OPENV_LIMITS", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Over one too many"}`),
		note("the owner created projects up to OPENV_LIMITS's seven just before (setups, Tour fill NNN)"))
	tr.step("Q13: a project from the default template is not counted: 201, the eighth", o,
		"POST /api/v1/templates/{id}/projects", at("id", "{{template}}"), jsonBody(`{"name":"Tour Over past the cap"}`)).
		capture("past1", "/id")
	tr.step("Q13: an import is not counted either: 201, the ninth", o, "POST /api/v1/projects/import",
		jsonBody(`{"project_name":"Tour Over imported past the cap"}`)).capture("past2", "/project_id")
	tr.step("W's limits: nine projects of seven, read-only, over max_projects", o, limits, inW)

	// Refusals on the read-only workspace.
	const gate = "403 plan_read_only, its remedy OPENV_LIMITS: "
	tr.step(gate+"rename P", o, "PUT /api/v1/projects/{id}", at("id", "{{p}}"), jsonBody(`{"name":"Tour Over P renamed"}`))
	tr.step(gate+"rename W", o, "PUT /api/v1/orgs/{id}", inW, jsonBody(`{"name":"Tour Over renamed"}`))
	tr.step(gate+"W's worker key creates an artifact in P", k, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Log each answer",`+
			`"body":"The system shall log each answer."}`))
	tr.step(gate+"revoke W's worker key", o, "DELETE /api/v1/orgs/{id}/worker-keys/{keyId}",
		at("id", "{{w}}", "keyId", "{{k.key}}"))
	tr.step("Q13: a new project on the read-only workspace: limit_reached, the count's refusal", o,
		"POST /api/v1/projects", jsonBody(`{"name":"Tour Over one too many"}`))
	tr.step("a project from the default template on the read-only workspace: 201, no guard, no gate, no count", o,
		"POST /api/v1/templates/{id}/projects", at("id", "{{template}}"),
		jsonBody(`{"name":"Tour Over from the template"}`)).capture("past3", "/id")

	// REQ-113, export and import on the read-only workspace.
	export, reqif := overPlanReads(tr, overPlanSelfHostedReads)
	tr.step("always writable: import P's JSON export into W", o, "POST /api/v1/projects/import",
		answerOf(export, "application/json")).capture("past4", "/project_id")
	tr.step("always writable: import P's ReqIF export into W", o, "POST /api/v1/projects/import", query("format=reqif"),
		answerOf(reqif, "application/json")).capture("past5", "/project_id")

	// The always-writable routes bring W back under its limit.
	overPlanBilling(tr, o, "a self-hosted deployment")
	tr.step("always writable: revoke the invitation", o, "DELETE /api/v1/orgs/{id}/invitations/{invId}",
		at("id", "{{w}}", "invId", "{{inv}}"))
	tr.step("always writable: remove m2", o, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{w}}", "userId", "{{m2}}"))
	tr.step("W's limits: still read-only, over max_projects alone", o, limits, inW)
	for i, name := range []string{"past1", "past2", "past3", "past4", "past5"} {
		tr.step(fmt.Sprintf("always writable: delete a project made past the seven, from %d projects to %d", 12-i, 11-i),
			o, "DELETE /api/v1/projects/{id}", at("id", "{{"+name+"}}"))
	}
	tr.step("W's limits: seven projects of seven, writable again", o, limits, inW)
	tr.actIn(o, "{{owner.workspace}}")
	tr.step("delete W, from the owner's personal workspace", o, "DELETE /api/v1/orgs/{id}", inW)
}
