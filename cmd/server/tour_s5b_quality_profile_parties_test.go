//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestTourS5bQualityProfileParties is the S5b tour's requirement-quality,
// product-profile and reference-parties area (refactor plan §6.4 S5b, before
// M8 splits suite_handlers.go into quality_lint_, product_profile_ and
// reference_party_handlers.go; invariants I3 (guard, lookup and decode
// order), I4, I5 and I10; quirks Q1 and Q19; OpenV REQ-13, REQ-23, REQ-143,
// REQ-147). Its golden is testdata/tour/s5b/quality_profile_parties.json.
//
// The owner shares a workspace named "Tour <Studio> & Co" (its < and & show
// how the parties answer escapes the default party's name) with a plain
// member, and keeps project P there: one heading holding requirements a
// linter judges differently (clean; weak and vague; a placeholder; passive;
// one over-long sentence; one citing REQ-1, which it is linked to, and
// REQ-9, which does not exist), a user need, and a test case, which is not
// linted. A viewer of P, who is no member of the workspace, and an
// outsider show the role gates. Project E and its baseline EB give another
// project's baseline; project C, in the owner's personal workspace, holds a
// requirement that cites a requirement of P and refines it, for the one
// place the project report and the artifact endpoint judge a citation
// differently.
//
// The area walks P's product profile first, since the first read of a
// project's profile creates it (a write on a read, which every export-based
// read also does: the baseline B1 taken after it, the quality report); then
// the quality report (live, from B1, refused baselines) and the artifact
// endpoint; the quality rules of P and of the workspace (the default, a
// project override, the validation refusals, the four decode shapes of an
// empty body, {}, trailing text and a malformed body, and the workspace's
// house style inherited and then overridden, its own two refusals and its
// clearing); and the parties. The profile
// answers are bare encodes (Q1: text/plain, sniffed); the quality, rules
// and parties answers set application/json. None of these writes publishes
// an event, which each write step's empty events list pins.
//
// Nondeterminism: only ids and minted times. P's artifacts sit under one
// heading, so the export (and the report built from it) lists them in the
// order they were created; every map the answers hold (summary, severities,
// labels, the profile's objects) is encoded with its keys sorted.
func TestTourS5bQualityProfileParties(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "quality_profile_parties",
		about: "Requirement quality (the project report, live and from a baseline, and one artifact's lint), the " +
			"quality rule sets of a project and a workspace (defaults, overrides, validation, inheritance), a " +
			"project's reference parties, and its product profile (created by its first read, replaced by a PUT).",
		run: qualityProfilePartiesTour,
	})
}

// qualityProfilePartiesActors are the accounts the area registers beside
// admin and owner.
type qualityProfilePartiesActors struct {
	member, viewer, outsider *tourActor
}

// qualityProfilePartiesSeed creates the workspace, the projects, their
// artifacts and links, and the accounts (setup; the artifacts, links and
// identity areas pin those routes).
func qualityProfilePartiesSeed(tr *tour) qualityProfilePartiesActors {
	o := tr.owner
	var a qualityProfilePartiesActors
	a.member = tr.register("member", "Tour Member", "a plain member of the owner's shared workspace (not an admin), "+
		"with no role on its projects")
	a.viewer = tr.register("viewer", "Tour Viewer", "a viewer of P, from a workspace of its own: not a member of "+
		"the shared workspace")
	a.outsider = tr.register("outsider", "Tour Outsider", "an ordinary account in a workspace of its own, with no "+
		"access to P")
	// From here the owner acts in the shared workspace and reads its events.
	tr.sharedWorkspace("studio", "Tour <Studio> & Co")
	tr.join(a.member, "{{studio}}", "member")

	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour quality"}`)).capture("p", "/id")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	tr.setup("P's heading, the one parent of its other artifacts", o, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{p}}","type":"heading","title":"Operations","body":"What the line needs."}`)).
		capture("heading", "/id")
	// Created in this order, so REQ-1 to REQ-6, NEED-1 and TC-1.
	for _, art := range []struct{ name, typ, title, body string }{
		{"req_clean", "requirement", "Answer time", "The system shall respond within 2 s."},
		{"req_weak", "requirement", "Speed", "The system should be fast for most users."},
		{"req_tbd", "requirement", "Pending decision", "TBD"},
		{"req_passive", "requirement", "Records", "Records shall be kept, and shall be stored for 5 years."},
		{"req_long", "requirement", "Traceable parts", "The system shall record the time, the operator, the " +
			"station, the batch, the shift, the machine, the tool, the program, the material and the order of " +
			"every part it makes on each line."},
		{"req_cites", "requirement", "Cited answers", "The system shall log every answer #REQ-1 times and every " +
			"alarm ##REQ-9 raises, as #REQ-1-FIG-1 shows."},
		{"need", "user-need", "Alarm view", "Users need a fast, simple, intuitive and reliable view of some " +
			"alarms, etc."},
		{"tc", "test-case", "Time the answer", "Measure the answer time on the line."},
	} {
		tr.setup("P's "+art.typ+" "+art.title, o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
			`"parent_id":"{{heading}}","type":"`+art.typ+`","title":"`+art.title+`","body":"`+art.body+`"}`)).
			capture(art.name, "/id")
	}
	tr.setup("REQ-1 decomposes to the requirement that cites it", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{req_clean}}","to_id":"{{req_cites}}","type":"decomposes-to"}`))
	tr.setup("the test case verifies REQ-1", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{req_clean}}","type":"verifies"}`))

	tr.setup("project E, which stays empty", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour empty"}`)).
		capture("e", "/id")
	tr.setup("E's baseline EB", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{e}}"),
		jsonBody(`{"name":"Empty baseline"}`)).capture("eb", "/id")

	// C lives in the owner's personal workspace: a project may be filed
	// under a parent of its own workspace only, and the refines link crosses
	// projects (and workspaces) without it.
	tr.setup("project C, in the owner's personal workspace", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour archive"}`), actingIn("{{owner.workspace}}")).capture("c", "/id")
	tr.setup("C's one requirement, REQ-1 of C, citing P's REQ-4", o, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{c}}","type":"requirement","title":"Archive",`+
			`"body":"The archive shall keep each record #REQ-4 names for 5 years."}`), actingIn("{{owner.workspace}}")).
		capture("c_req", "/id")
	tr.setup("C's requirement refines P's REQ-4", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{c_req}}","to_id":"{{req_passive}}","type":"refines"}`), actingIn("{{owner.workspace}}"))
	return a
}

func qualityProfilePartiesTour(tr *tour) {
	o := tr.owner
	a := qualityProfilePartiesSeed(tr)
	tr.wholeSeconds("GET /api/v1/projects/{id}/export", "the JSON export's times")
	tr.wholeSeconds("GET /api/v1/projects/{id}/ai-map", "its Generated line")
	qualityProfilePartiesProfile(tr, a)
	tr.setup("P's baseline B1, after the profile steps (taking it reads P's profile too)", o,
		"POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"), jsonBody(`{"name":"Tour B1"}`)).capture("b1", "/id")
	qualityProfilePartiesLint(tr, a)
	qualityProfilePartiesRules(tr, a)
	qualityProfilePartiesParties(tr, a)
}

// qualityProfilePartiesProfile is P's product profile: created by its
// first read, replaced whole by a PUT, cleared by an empty object.
func qualityProfilePartiesProfile(tr *tour, a qualityProfilePartiesActors) {
	o := tr.owner
	tr.step("P's profile, never read before: this GET creates it, empty, and its gzip variant reads the stored copy",
		o, "GET /api/v1/projects/{id}/profile", at("id", "{{p}}"),
		note("created_at and updated_at are minted by this step's first request; the gzip variant, sent second, "+
			"reads the row it stored (the database keeps microseconds, so the two answers' times differ in "+
			"length, not in what they normalise to)"))
	tr.step("the viewer reads it", a.viewer, "GET /api/v1/projects/{id}/profile", at("id", "{{p}}"))
	tr.step("the outsider reads it", a.outsider, "GET /api/v1/projects/{id}/profile", at("id", "{{p}}"))
	tr.step("replace it: every field, objects in the arrays and a settings object", o,
		"PUT /api/v1/projects/{id}/profile", at("id", "{{p}}"),
		jsonBody(`{"vision":"Answer every operator <fast> & plainly","problem_statement":"Operators wait for answers.",`+
			`"target_users":"Line operators; shift leads",`+
			`"constraints":[{"kind":"budget","limit_eur":1.50},{"kind":"weight","max_kg":12,"note":"<carry> & lift"}],`+
			`"success_metrics":[{"metric":"answer time","target_s":2,"p95":true}],`+
			`"settings":{"units":"SI","serial":12345678901234567890,"nested":{"zulu":1,"alpha":[1,2]}}}`),
		note("the objects go through interface{}: their keys come back sorted, 1.50 as 1.5 and "+
			"12345678901234567890 as the float64 12345678901234567000, and JSON escapes the < > &"))
	tr.step("read it back: the stored copy", o, "GET /api/v1/projects/{id}/profile", at("id", "{{p}}"))
	tr.step("P's export carries the profile", o, "GET /api/v1/projects/{id}/export", at("id", "{{p}}"),
		note("an S5a route, read to show the profile in the export (and so in every baseline taken from here on)"))
	tr.step("replace it with a malformed body", o, "PUT /api/v1/projects/{id}/profile", at("id", "{{p}}"),
		jsonBody(`{`))
	tr.step("replace it with constraints that are not an array", o, "PUT /api/v1/projects/{id}/profile",
		at("id", "{{p}}"), jsonBody(`{"vision":"Kept?","constraints":"none"}`),
		note("a field of the wrong JSON type fails the decode like a malformed body"))
	tr.step("the viewer replaces it", a.viewer, "PUT /api/v1/projects/{id}/profile", at("id", "{{p}}"),
		jsonBody(`{"vision":"Viewer vision"}`))
	tr.step("replace the profile of a project that does not exist", o, "PUT /api/v1/projects/{id}/profile",
		at("id", "{{phantom}}"), jsonBody(`{"vision":"Nowhere"}`),
		note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("replace it with an empty object: a full replace clears every field to \"\", [] and {}", o,
		"PUT /api/v1/projects/{id}/profile", at("id", "{{p}}"), jsonBody(`{}`))
}

// qualityProfilePartiesLint is the quality report of a project and one
// artifact's lint, against the default rules (the "shall" convention).
func qualityProfilePartiesLint(tr *tour, a qualityProfilePartiesActors) {
	o := tr.owner
	tr.step("P's quality report: requirements and the user need in export order, the heading and test case left out", o,
		"GET /api/v1/projects/{id}/quality", at("id", "{{p}}"),
		note("the title is joined to the body by two newlines, which the sentence split does not cut at, so the "+
			"long sentence's words and its match start with the title's"))
	tr.step("the report from B1: the same artifacts, from the snapshot", o, "GET /api/v1/projects/{id}/quality",
		at("id", "{{p}}"), query("baseline_id={{b1}}"))
	tr.step("baseline_id=live: the live report", o, "GET /api/v1/projects/{id}/quality", at("id", "{{p}}"),
		query("baseline_id=live"))
	tr.step("the report from a baseline that does not exist", o, "GET /api/v1/projects/{id}/quality",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("the report of P from E's baseline: refused like a missing one", o, "GET /api/v1/projects/{id}/quality",
		at("id", "{{p}}"), query("baseline_id={{eb}}"))
	tr.step("the report from a baseline_id that is not a UUID: any lookup error is not found", o,
		"GET /api/v1/projects/{id}/quality", at("id", "{{p}}"), query("baseline_id=not-a-uuid"))
	tr.step("the report of a project that does not exist", o, "GET /api/v1/projects/{id}/quality",
		at("id", "{{phantom}}"), note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("the report of the empty project: entries [], and every band counted", o,
		"GET /api/v1/projects/{id}/quality", at("id", "{{e}}"))
	tr.step("the outsider reads P's report", a.outsider, "GET /api/v1/projects/{id}/quality", at("id", "{{p}}"))

	tr.step("lint the citing requirement alone: REQ-9 flagged, REQ-1 linked, the figure citation left alone", o,
		"GET /api/v1/artifacts/{id}/quality", at("id", "{{req_cites}}"))
	tr.step("lint the user need", o, "GET /api/v1/artifacts/{id}/quality", at("id", "{{need}}"))
	tr.step("the viewer lints the weak requirement", a.viewer, "GET /api/v1/artifacts/{id}/quality",
		at("id", "{{req_weak}}"))
	tr.step("the outsider lints it", a.outsider, "GET /api/v1/artifacts/{id}/quality", at("id", "{{req_weak}}"))
	tr.step("lint the heading: not a type the linter judges", o, "GET /api/v1/artifacts/{id}/quality",
		at("id", "{{heading}}"))
	tr.step("lint an artifact that does not exist", o, "GET /api/v1/artifacts/{id}/quality", at("id", "{{phantom}}"),
		note("the artifact is looked up before the guard, so a missing one answers 404"))
	tr.step("lint by an id that is not a UUID: any lookup error is not found", o, "GET /api/v1/artifacts/{id}/quality",
		at("id", "not-a-uuid"))

	// The report builds a project's linked refs from its own export's
	// artifacts only, so a link to another project's artifact names no ref
	// there; the artifact endpoint reads each link's other end.
	tr.step("C's report: its citation of P's REQ-4 is flagged unlinked, though C refines it", o,
		"GET /api/v1/projects/{id}/quality", at("id", "{{c}}"), actingIn("{{owner.workspace}}"),
		note("C's export lists the refines link, but REQ-4 is not among C's artifacts, so the report cannot name "+
			"the link's other end"))
	tr.step("the same requirement linted alone: nothing flagged, the link's other end is read", o,
		"GET /api/v1/artifacts/{id}/quality", at("id", "{{c_req}}"), actingIn("{{owner.workspace}}"))
}

// qualityProfilePartiesRules is the quality rule sets: P's override, its
// validation and the decode shapes of its body, then the workspace's house
// style and how P inherits it.
func qualityProfilePartiesRules(tr *tour, a qualityProfilePartiesActors) {
	o := tr.owner
	tr.step("P's rules before anything is set: the defaults, both levels null, and the catalog", o,
		"GET /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"))
	tr.step("the viewer reads P's rules", a.viewer, "GET /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"))
	tr.step("the rules of a project that does not exist", o, "GET /api/v1/projects/{id}/quality-rules",
		at("id", "{{phantom}}"), note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("P writes RFC 2119 and switches weak-word off", o, "PUT /api/v1/projects/{id}/quality-rules",
		at("id", "{{p}}"), jsonBody(`{"convention":"rfc2119","severities":{"weak-word":"off"}}`))
	tr.step("P's report under its rules: should and may are keywords now, shall is off-convention", o,
		"GET /api/v1/projects/{id}/quality", at("id", "{{p}}"))
	tr.step("the report from B1 is judged by today's rules, not those of the day it was taken", o,
		"GET /api/v1/projects/{id}/quality", at("id", "{{p}}"), query("baseline_id={{b1}}"))
	tr.step("P's AI map: its house-style line", o, "GET /api/v1/projects/{id}/ai-map", at("id", "{{p}}"),
		note("an S5a route, read to show the rule set's summary where agents read it"))

	tr.step("an unknown convention: the convention is checked before the rules", o,
		"PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"),
		jsonBody(`{"convention":"iso","severities":{"tone":"loud"}}`))
	tr.step("an unknown rule", o, "PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"),
		jsonBody(`{"severities":{"tone":"warning"}}`))
	tr.step("a severity the rules do not have", o, "PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"),
		jsonBody(`{"convention":"shall","severities":{"weak-word":"loud"}}`))
	tr.step("five bad keys: the first in sorted order is named, whatever the map's order", o,
		"PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"),
		jsonBody(`{"severities":{"weak-word":"loud","zz-tone":"error","passive-voice":"shouty","long-sentence":"x","aa-tone":"off"}}`))
	tr.step("the viewer sets P's rules", a.viewer, "PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"),
		jsonBody(`{"convention":"shall"}`))
	tr.step("an empty body: accepted, and it clears P's override (an end of input is no error here)", o,
		"PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"), jsonBody(``))
	tr.step("text after the JSON value: only the first value is read", o, "PUT /api/v1/projects/{id}/quality-rules",
		at("id", "{{p}}"), jsonBody(`{"convention":"rfc2119"} and then some`))
	tr.step("an empty object clears the override too", o, "PUT /api/v1/projects/{id}/quality-rules",
		at("id", "{{p}}"), jsonBody(`{}`))
	tr.step("a malformed body", o, "PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"), jsonBody(`{`))

	// The workspace's house style: any member reads it, admins set it.
	tr.step("the member reads the workspace's rules: the defaults", a.member, "GET /api/v1/orgs/{id}/quality-rules",
		at("id", "{{studio}}"))
	tr.step("the member sets them", a.member, "PUT /api/v1/orgs/{id}/quality-rules", at("id", "{{studio}}"),
		jsonBody(`{"convention":"rfc2119"}`))
	tr.step("the owner, the workspace's admin, sets RFC 2119 with passive voice an error", o,
		"PUT /api/v1/orgs/{id}/quality-rules", at("id", "{{studio}}"),
		jsonBody(`{"convention":"rfc2119","severities":{"passive-voice":"error"}}`))
	tr.step("P inherits the house style: workspace set, project null", o, "GET /api/v1/projects/{id}/quality-rules",
		at("id", "{{p}}"))
	tr.step("P re-grades passive voice alone: the workspace's convention stays", o,
		"PUT /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"),
		jsonBody(`{"severities":{"passive-voice":"info"}}`))
	tr.step("the viewer, no member of the workspace, reads its rules", a.viewer, "GET /api/v1/orgs/{id}/quality-rules",
		at("id", "{{studio}}"))
	tr.step("the rules of a workspace that does not exist", o, "GET /api/v1/orgs/{id}/quality-rules",
		at("id", "{{phantom}}"), note("no role in a workspace no row has: 404, as for a workspace the caller is not in (I3)"))
	tr.step("the owner sends the house style a malformed body", o, "PUT /api/v1/orgs/{id}/quality-rules",
		at("id", "{{studio}}"), jsonBody(`{`))
	tr.step("the owner sends the house style an unknown convention: the project level's check", o,
		"PUT /api/v1/orgs/{id}/quality-rules", at("id", "{{studio}}"), jsonBody(`{"convention":"iso"}`))
	tr.step("the owner clears the house style with an empty body", o, "PUT /api/v1/orgs/{id}/quality-rules",
		at("id", "{{studio}}"), jsonBody(``))
}

// qualityProfilePartiesParties is P's reference parties (REQ-147): the
// workspace's own party first, then what P stores. The shared workspace is
// on the nightly channel, like every tour workspace, so the owners gate
// (FeatureOwners) lets each write through and its stable-channel 403 is not
// reached.
func qualityProfilePartiesParties(tr *tour, a qualityProfilePartiesActors) {
	o := tr.owner
	tr.step("P's parties: only the workspace's own, first and default, its name escaped", o,
		"GET /api/v1/projects/{id}/parties", at("id", "{{p}}"))
	tr.step("store parties: the workspace's name with a note, and a default entry sent in", o,
		"PUT /api/v1/projects/{id}/parties", at("id", "{{p}}"),
		jsonBody(`{"parties":[{"name":"  Gear Works  ","note":" Main and nose gear "},`+
			`{"name":"tour <studio> & co","note":"Prime contractor"},{"name":"Ghost","note":"sent as default","default":true},`+
			`{"name":"Avionics"}]}`),
		note("names and notes are trimmed; the workspace's name, matched case-insensitively, gives its note to the "+
			"default entry and is not listed again; an entry sent as default is dropped"))
	tr.step("the viewer reads them", a.viewer, "GET /api/v1/projects/{id}/parties", at("id", "{{p}}"))
	tr.step("the parties share P's settings with its rules: P's override is still there", o,
		"GET /api/v1/projects/{id}/quality-rules", at("id", "{{p}}"))
	tr.step("a party listed twice, in another case", o, "PUT /api/v1/projects/{id}/parties", at("id", "{{p}}"),
		jsonBody(`{"parties":[{"name":"Avionics"},{"name":"AVIONICS"}]}`))
	tr.step("a party with a blank name", o, "PUT /api/v1/projects/{id}/parties", at("id", "{{p}}"),
		jsonBody(`{"parties":[{"name":"Avionics"},{"name":"   ","note":"nameless"}]}`))
	tr.step("101 parties, one over the limit", o, "PUT /api/v1/projects/{id}/parties", at("id", "{{p}}"),
		jsonBody(qualityProfilePartiesMany(101)))
	tr.step("a malformed body", o, "PUT /api/v1/projects/{id}/parties", at("id", "{{p}}"), jsonBody(`{"parties":`))
	tr.step("an empty body: refused here, unlike the quality rules", o, "PUT /api/v1/projects/{id}/parties",
		at("id", "{{p}}"), jsonBody(``))
	tr.step("the viewer stores parties", a.viewer, "PUT /api/v1/projects/{id}/parties", at("id", "{{p}}"),
		jsonBody(`{"parties":[{"name":"Viewer party"}]}`))
	tr.step("the parties of a project that does not exist", o, "GET /api/v1/projects/{id}/parties",
		at("id", "{{phantom}}"))
	tr.step("an empty list clears P's parties: the workspace's own is left", o, "PUT /api/v1/projects/{id}/parties",
		at("id", "{{p}}"), jsonBody(`{"parties":[]}`))
}

// qualityProfilePartiesMany is a parties body with n distinct parties.
func qualityProfilePartiesMany(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"name":"Party %03d"}`, i+1)
	}
	return `{"parties":[` + strings.Join(parts, ",") + `]}`
}
