//go:build unix

package main

import (
	"net/http"
	"testing"
)

// TestTourS5bTargetReleases is the S5b tour's target releases area: a
// project's ordered releases ("MVP", "V1.0"), the release range an artifact
// targets through its target_release and target_release_until attributes,
// and each release's progress and burn-up. Its golden is
// testdata/tour/s5b/target_releases.json.
//
// The owner, in its personal (nightly) workspace, keeps project P and
// project Q. P has releases MVP, V1.0 and V2.0; heading H targets MVP, so
// requirement R1 under it inherits MVP onwards; R2 targets V1.0 only (from
// V1.0 until V1.0); R3 targets nothing. Test case TC verifies R1 and passes
// in a run; R1 is approved. The viewer is a project viewer of P.
//
// What the golden shows:
//   - the list is [] before any release, then in position order; a create
//     goes last; a duplicate name (any case) is a 409, an empty name, a date
//     not written YYYY-MM-DD and an unknown status a 400; a viewer's write a
//     403; the reorder needs every release once (400 otherwise);
//   - an artifact write naming a release P does not have, or a last release
//     before its first, is a 400;
//   - the overview counts each release's requirements: MVP holds R1 (ready:
//     approved and verified), V1.0 holds R1 carried over and R2 introduced,
//     V2.0 holds R1 alone; R3 is unassigned;
//   - a release's progress lists its requirements with their range (R1's
//     inherited from H), rollup and latest results, and a burn-up whose last
//     day agrees with the summary;
//   - a release an artifact targets cannot be deleted (409); one nothing
//     targets can (204); a release read through another project is a 404.
func TestTourS5bTargetReleases(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "target_releases",
		about: "Target releases: the list, its edits and order, the release range an artifact targets, and each " +
			"release's progress against its requirements, with a burn-up.",
		run: targetReleasesTour,
	})
}

func targetReleasesTour(tr *tour) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour releases"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour releases elsewhere"}`)).
		capture("q", "/id")
	viewer := tr.register("viewer", "Tour Viewer", "a project viewer of P")
	tr.setup("the viewer joins P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(http.StatusCreated))

	list := "GET /api/v1/projects/{id}/releases"
	create := "POST /api/v1/projects/{id}/releases"
	tr.step("P has no release yet: []", o, list, at("id", "{{p}}"))
	tr.step("MVP, with a target date", o, create, at("id", "{{p}}"),
		jsonBody(`{"name":"MVP","description":"The first usable release: the requirements a first customer needs to put the product to work, every one of them approved and verified before it ships.","target_date":"2027-01-31"}`)).
		capture("mvp", "/id")
	tr.step("V1.0, in progress", o, create, at("id", "{{p}}"), jsonBody(`{"name":"V1.0","status":"in_progress"}`)).
		capture("v1", "/id")
	tr.step("V2.0", o, create, at("id", "{{p}}"), jsonBody(`{"name":"V2.0"}`)).capture("v2", "/id")
	tr.step("Q's own release", o, create, at("id", "{{q}}"), jsonBody(`{"name":"MVP"}`),
		note("names are unique within a project, not across projects")).capture("q_mvp", "/id")
	tr.step("a name P already has, in another case: 409", o, create, at("id", "{{p}}"), jsonBody(`{"name":"mvp"}`))
	tr.step("an empty name: 400", o, create, at("id", "{{p}}"), jsonBody(`{"name":"  "}`))
	tr.step("a date not written YYYY-MM-DD: 400", o, create, at("id", "{{p}}"),
		jsonBody(`{"name":"V3.0","target_date":"31/01/2027"}`))
	tr.step("an unknown status: 400", o, create, at("id", "{{p}}"), jsonBody(`{"name":"V3.0","status":"done"}`))
	tr.step("the viewer may not add a release: 403", viewer, create, at("id", "{{p}}"), jsonBody(`{"name":"V3.0"}`))
	tr.step("the viewer reads P's releases, in order", viewer, list, at("id", "{{p}}"))

	update := "PUT /api/v1/projects/{id}/releases/{releaseId}"
	tr.step("MVP is released: released_at is stamped", o, update, at("id", "{{p}}", "releaseId", "{{mvp}}"),
		jsonBody(`{"status":"released"}`))
	tr.step("V2.0 renamed, its description set", o, update, at("id", "{{p}}", "releaseId", "{{v2}}"),
		jsonBody(`{"name":"V2","description":"Later."}`))
	tr.step("a rename onto a name P already has: 409", o, update, at("id", "{{p}}", "releaseId", "{{v2}}"),
		jsonBody(`{"name":"V1.0"}`))
	tr.step("Q's release, through P: 404", o, update, at("id", "{{p}}", "releaseId", "{{q_mvp}}"),
		jsonBody(`{"name":"X"}`))

	order := "PUT /api/v1/projects/{id}/release-order"
	tr.step("an order that leaves a release out: 400", o, order, at("id", "{{p}}"),
		jsonBody(`{"ids":["{{v2}}","{{mvp}}"]}`))
	tr.step("V2 first, then back: the list in the new order", o, order, at("id", "{{p}}"),
		jsonBody(`{"ids":["{{v2}}","{{mvp}}","{{v1}}"]}`))
	tr.step("MVP, V1.0, V2 again", o, order, at("id", "{{p}}"), jsonBody(`{"ids":["{{mvp}}","{{v1}}","{{v2}}"]}`))

	// The artifacts and their targeting.
	art := "POST /api/v1/artifacts"
	tr.step("heading H targets MVP onwards", o, art, jsonBody(`{"project_id":"{{p}}","type":"heading",`+
		`"title":"Core","attributes":{"target_release":"{{mvp}}"}}`)).capture("h", "/id")
	tr.step("R1 under H targets nothing of its own", o, art, jsonBody(`{"project_id":"{{p}}","parent_id":"{{h}}",`+
		`"type":"requirement","title":"Answer in time","attributes":{"verification_method":"test"}}`)).capture("r1", "/id")
	tr.step("R2 targets V1.0 alone", o, art, jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Export the log","attributes":{"target_release":"{{v1}}","target_release_until":"{{v1}}"}}`)).capture("r2", "/id")
	tr.step("R3 targets no release", o, art, jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Look tidy"}`)).capture("r3", "/id")
	tr.step("a target Q's release: 400", o, art, jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Elsewhere","attributes":{"target_release":"{{q_mvp}}"}}`))
	tr.step("a last release before the first: 400", o, art, jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Backwards","attributes":{"target_release":"{{v2}}","target_release_until":"{{mvp}}"}}`))
	tr.step("a last release with no first: 400", o, art, jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Halfway","attributes":{"target_release_until":"{{v1}}"}}`))
	tr.setup("TC", o, art, jsonBody(`{"project_id":"{{p}}","type":"test-case","title":"Time the answer"}`)).
		capture("tc", "/id")
	tr.setup("TC verifies R1", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{r1}}","type":"verifies"}`))
	tr.setup("a run", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"), jsonBody(`{"name":"Run 1"}`)).
		capture("run", "/id")
	tr.setup("TC passes", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{run}}"),
		jsonBody(`{"test_case_id":"{{tc}}","status":"pass"}`))
	tr.setup("R1 in review", o, "PUT /api/v1/artifacts/{id}/status", at("id", "{{r1}}"),
		jsonBody(`{"status":"in_review"}`))
	tr.setup("R1 approved", o, "PUT /api/v1/artifacts/{id}/status", at("id", "{{r1}}"),
		jsonBody(`{"status":"approved"}`))

	overview := "GET /api/v1/projects/{id}/vv/releases"
	progress := "GET /api/v1/projects/{id}/vv/releases/{releaseId}"
	tr.step("the overview: each release's counts, and R3 unassigned", o, overview, at("id", "{{p}}"),
		note("V1.0 carries R1 over from MVP and introduces R2; V2 holds R1 alone, since R2 ends at V1.0"))
	tr.step("MVP's progress: R1, inherited from H, approved and verified", o, progress,
		at("id", "{{p}}", "releaseId", "{{mvp}}"),
		note("a released release's burn-up ends on the day it was released"))
	tr.step("V1.0's progress: R1 carried over, R2 introduced, with no verification method yet", o, progress,
		at("id", "{{p}}", "releaseId", "{{v1}}"))
	tr.step("the viewer reads V1.0's progress", viewer, progress, at("id", "{{p}}", "releaseId", "{{v1}}"))
	tr.step("Q's release, through P: 404", o, progress, at("id", "{{p}}", "releaseId", "{{q_mvp}}"))
	tr.step("Q's overview: one release, nothing in it", o, overview, at("id", "{{q}}"))

	del := "DELETE /api/v1/projects/{id}/releases/{releaseId}"
	tr.step("MVP is still targeted by H: 409", o, del, at("id", "{{p}}", "releaseId", "{{mvp}}"))
	tr.step("V1.0 is still targeted by R2: 409", o, del, at("id", "{{p}}", "releaseId", "{{v1}}"))
	tr.step("the viewer may not delete V2: 403", viewer, del, at("id", "{{p}}", "releaseId", "{{v2}}"))
	tr.step("V2, which nothing targets: 204", o, del, at("id", "{{p}}", "releaseId", "{{v2}}"))
	tr.step("V2 again: 404", o, del, at("id", "{{p}}", "releaseId", "{{v2}}"))
	tr.step("P's releases after the delete", o, list, at("id", "{{p}}"))
}
