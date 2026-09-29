//go:build unix

package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestTourS5eOverPlanSelfHosted is part (3) of S5e, the over-plan pass,
// under the S4b self_hosted profile with the limits profile's deployment
// limit, OPENV_LIMITS={"max_projects":7} (refactor plan §6.4 S5e; invariant
// I3's plan gate; OpenV REQ-143, REQ-18, REQ-113, REQ-176 and REQ-177). Its
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
// a workspace read-only, so the limits profile's seven projects do here. No
// route creates a project past them (every project create counts, as quirk
// Q13's fix made it), so the two that take W over are rows written to the
// database as setup: what a deployment holds whose operator lowered
// OPENV_LIMITS below a workspace's projects (overPlanProjectsPastTheLimit).
//
// The area walks, on W (the owner's shared workspace, where m1 is a member
// and P's editor, m2 a member, and an address invited):
//   - W's limits with four seats, the shape the tiers made read-only:
//     writable, seats unlimited, self_hosted true;
//   - the hosted claim of W's worker key, 204 (every flag on), and W's
//     billing, 404 billing_unavailable;
//   - the project maximum at the deployment's 7: a new project, a template's
//     project and an import each refused limit_reached with the OPENV_LIMITS
//     remedy; then two projects past the seven, written as setup, which take
//     W over, read-only;
//   - refusals, 403 plan_read_only, their remedy OPENV_LIMITS: the owner's
//     rename of P and of W, the worker key's artifact in P, the worker
//     key's revocation, a new project and a template's project;
//   - REQ-113: the export, the JSON and ReqIF downloads, on the read-only
//     workspace, and the JSON and ReqIF imports of the exports, which pass the
//     gate (always writable, REQ-177) and are refused by the project count,
//     limit_reached;
//   - the always-writable routes: the four billing writes (404
//     billing_unavailable, which the gate would have answered 403 first),
//     the invitation's revocation and m2's removal (W stays read-only: its
//     projects are what it is over), the deletes of the two projects past the
//     seven (W writable again at seven), and W's delete.
func TestTourS5eOverPlanSelfHosted(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5e",
		key:   "over_plan_self_hosted",
		about: "The over-plan pass on a self-hosted deployment with OPENV_LIMITS={\"max_projects\":7}: seats " +
			"unlimited and every flag on (the hosted claim passes), billing unavailable, every project create " +
			"refused at the deployment's seven projects with the OPENV_LIMITS remedy, the read-only workspace's " +
			"refusals (their remedy OPENV_LIMITS too), export and import (REQ-113), and the always-writable routes.",
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

	// The project maximum at the deployment's seven.
	overPlanFill(tr, 7)
	tr.step("an eighth project is refused limit_reached, the remedy OPENV_LIMITS", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Over one too many"}`),
		note("the owner created projects up to OPENV_LIMITS's seven just before (setups, Tour fill NNN)"))
	tr.step("a project from the default template counts too: refused limit_reached", o,
		"POST /api/v1/templates/{id}/projects", at("id", "{{template}}"), jsonBody(`{"name":"Tour Over past the cap"}`))
	tr.step("an import counts too: refused limit_reached", o, "POST /api/v1/projects/import",
		jsonBody(`{"project_name":"Tour Over imported past the cap"}`))
	overPlanProjectsPastTheLimit(tr, "past1", "past2")
	tr.step("W's limits: nine projects of seven, read-only, over max_projects", o, limits, inW,
		note("<past1> and <past2>, two projects past the seven, were written to the database just before (no "+
			"setup request): what a deployment holds whose operator lowered OPENV_LIMITS below a workspace's "+
			"projects, since no route creates a project past the maximum"))

	// Refusals on the read-only workspace.
	const gate = "403 plan_read_only, its remedy OPENV_LIMITS: "
	tr.step(gate+"rename P", o, "PUT /api/v1/projects/{id}", at("id", "{{p}}"), jsonBody(`{"name":"Tour Over P renamed"}`))
	tr.step(gate+"rename W", o, "PUT /api/v1/orgs/{id}", inW, jsonBody(`{"name":"Tour Over renamed"}`))
	tr.step(gate+"W's worker key creates an artifact in P", k, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Log each answer",`+
			`"body":"The system shall log each answer."}`))
	tr.step(gate+"revoke W's worker key", o, "DELETE /api/v1/orgs/{id}/worker-keys/{keyId}",
		at("id", "{{w}}", "keyId", "{{k.key}}"))
	tr.step(gate+"a new project, the gate before the count", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Over one too many"}`))
	tr.step(gate+"a project from the default template", o, "POST /api/v1/templates/{id}/projects",
		at("id", "{{template}}"), jsonBody(`{"name":"Tour Over from the template"}`))

	// REQ-113, export and import on the read-only workspace.
	export, reqif := overPlanReads(tr, overPlanSelfHostedReads)
	tr.step("always writable, and counted: import P's JSON export into W, refused limit_reached", o,
		"POST /api/v1/projects/import", answerOf(export, "application/json"))
	tr.step("always writable, and counted: import P's ReqIF export into W, refused limit_reached", o,
		"POST /api/v1/projects/import", query("format=reqif"), answerOf(reqif, "application/json"))

	// The always-writable routes bring W back under its limit.
	overPlanBilling(tr, o, "a self-hosted deployment")
	tr.step("always writable: revoke the invitation", o, "DELETE /api/v1/orgs/{id}/invitations/{invId}",
		at("id", "{{w}}", "invId", "{{inv}}"))
	tr.step("always writable: remove m2", o, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{w}}", "userId", "{{m2}}"))
	tr.step("W's limits: still read-only, over max_projects alone", o, limits, inW)
	for i, name := range []string{"past1", "past2"} {
		tr.step(fmt.Sprintf("always writable: delete a project past the seven, from %d projects to %d", 9-i, 8-i),
			o, "DELETE /api/v1/projects/{id}", at("id", "{{"+name+"}}"))
	}
	tr.step("W's limits: seven projects of seven, writable again", o, limits, inW)
	tr.actIn(o, "{{owner.workspace}}")
	tr.step("delete W, from the owner's personal workspace", o, "DELETE /api/v1/orgs/{id}", inW)
}

// overPlanProjectsPastTheLimit writes a project of W for each name straight
// into the database and registers its id under the name: projects W held
// before its operator lowered OPENV_LIMITS below them, which the server
// under test then counts. No route creates a project past the maximum, so
// no request can.
func overPlanProjectsPastTheLimit(tr *tour, names ...string) {
	tr.t.Helper()
	conn, err := sql.Open("postgres", tr.db.url)
	if err != nil {
		tr.t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	for _, name := range names {
		id := uuid.NewString()
		if _, err := conn.Exec(`INSERT INTO projects (id, name, description, org_id) VALUES ($1, $2, '', $3)`,
			id, "Tour Over "+name, tr.id("w")); err != nil {
			tr.t.Fatalf("write the project %s past the limit: %v", name, err)
		}
		tr.remember(name, id)
	}
}
