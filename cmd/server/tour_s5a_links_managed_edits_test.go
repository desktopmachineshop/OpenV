//go:build unix

package main

import "testing"

// TestTourS5aLinksManagedEdits is the S5a tour's links area (refactor plan
// §6.4 S5a; quirks Q3, Q4 and Q14; OpenV REQ-5, REQ-143, REQ-145): the link
// routes, and the managed link edits PUT /api/v1/artifacts/{id} makes with
// pendingLinkAdds and pendingLinkRemoves. Its golden is
// testdata/tour/s5a/links_managed_edits.json.
//
// Project P holds a heading with four children, one of each type a link rule
// names (user-need, requirement, test-case, design-item); project C, filed
// under P, holds one requirement, for the flow-down link (refines) across
// the project boundary. The owner, an ordinary account that owns both
// through its workspace, makes every link but one; the supplier, an editor
// on C and only a viewer on P, makes the refines link, which POST /links
// lets cross into P with viewer rights there, and is refused a derives-from
// link into P and silently denied the same refines link through a managed
// edit.
//
// Creating, updating or deleting a link through /api/v1/links auto-versions
// both endpoints and leaves a chatter note on each; a managed edit versions
// the artifact updated and auto-versions the other ends it touched. The
// steps read the links of versions (links_snapshot) as well as the current
// ones, and the chatter notes, to pin both. The managed edits touch at most
// one artifact besides the one updated: processManagedLinkChanges collects
// the affected ids in a Go map, so with two or more the order they are
// auto-versioned in, and so their updated_at, would change from run to run.
func TestTourS5aLinksManagedEdits(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "links_managed_edits",
		about: "Links (create with the type rules, read, list, update, suspect and confirm, delete, a project's " +
			"linked artifacts, an artifact's links now and per version) and the managed link edits of " +
			"PUT /api/v1/artifacts/{id} (Q3, Q4), with the auto-versions and chatter notes both paths leave.",
		run: linksManagedEditsTour,
	})
}

// linksManagedEditsRationale is the derives-from link's rationale, 119 bytes.
const linksManagedEditsRationale = "Every answer the user needs traced becomes a requirement, so the need is " +
	"met exactly when that requirement is verified."

func linksManagedEditsTour(tr *tour) {
	owner := tr.owner
	supplier := tr.register("supplier", "Tour Supplier",
		"an editor on the child project C and only a viewer on the parent P, for the flow-down link (refines)")

	// Project P, one heading with a child of each linkable type, and project
	// C under it with one requirement. Every title is unique, since chatter
	// notes name artifacts by title.
	tr.setup("project P", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Links tour","description":"The parent project"}`)).capture("p", "/id")
	tr.setup("project C", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Links supplier","description":"The child project"}`)).capture("c", "/id")
	tr.setup("file C under P", owner, "PUT /api/v1/projects/{id}", at("id", "{{c}}"),
		jsonBody(`{"parent_project_id":"{{p}}"}`))
	tr.setup("a heading in P", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"heading",`+
		`"title":"Scope","body":"What the links tour covers."}`)).capture("heading", "/id")
	for _, a := range []struct{ name, typ, title, body string }{
		{"need", "user-need", "Need one", "Users need traceable answers."},
		{"req", "requirement", "Req one", "The system shall trace every answer."},
		{"tc", "test-case", "Test one", "Trace one answer end to end."},
		{"di", "design-item", "Design one", "A trace table keyed by answer."},
	} {
		tr.setup("a "+a.typ+" under the heading", owner, "POST /api/v1/artifacts",
			jsonBody(`{"project_id":"{{p}}","parent_id":"{{heading}}","type":"`+a.typ+`","title":"`+a.title+
				`","body":"`+a.body+`"}`)).capture(a.name, "/id")
	}
	tr.setup("a requirement in C", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{c}}",`+
		`"type":"requirement","title":"Supplier req","body":"The subsystem shall trace its part of every answer."}`)).
		capture("creq", "/id")
	tr.setup("the supplier edits C", owner, "POST /api/v1/projects/{id}/members", at("id", "{{c}}"),
		jsonBody(`{"email":"tour-supplier@example.com","role":"editor"}`))
	tr.setup("the supplier only views P", owner, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-supplier@example.com","role":"viewer"}`))

	// Before any link (Q14: null or []).
	tr.step("P's links before any exists: null (Q14)", owner, "GET /api/v1/links", query("project_id={{p}}"))
	tr.step("links with no project_id", owner, "GET /api/v1/links")
	tr.step("the links of a project that does not exist", owner, "GET /api/v1/links", query("project_id={{phantom}}"),
		note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("the requirement's current links before any exists: []", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{req}}"))
	tr.step("the requirement's version 1 carries no links_snapshot: null (Q14)", owner,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{req}}"), query("version=1"))
	tr.step("P's linked artifacts before any link crosses out of it: []", owner,
		"GET /api/v1/projects/{id}/linked-artifacts", at("id", "{{p}}"))

	// Create, by the rules of links.ValidateLinkType. Each create auto-versions
	// both endpoints (source first) with a chatter note on each, and publishes
	// link.created.
	// The derives-from link carries a rationale of 119 bytes, which keeps the
	// answers that list it with three other links clear of the compressor's
	// 1,400-byte floor whatever their timestamps' lengths.
	tr.step("the requirement derives from the user need, with attributes: link.created", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{req}}","to_id":"{{need}}","type":"derives-from","attributes":{"rationale":"`+
			linksManagedEditsRationale+`"}}`)).capture("l_derives", "/id")
	tr.step("the test case verifies the requirement", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{req}}","type":"verifies"}`)).capture("l_verifies", "/id")
	tr.step("the design item satisfies the requirement", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"}`)).capture("l_satisfies", "/id")
	tr.step("a link type from a source type its rule does not allow", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{req}}","to_id":"{{tc}}","type":"verifies"}`))
	tr.step("a link type to a target type its rule does not allow", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{need}}","type":"verifies"}`))
	tr.step("a link type no rule names", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{req}}","type":"bogus"}`))
	tr.step("a link from an artifact that does not exist", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{phantom}}","to_id":"{{req}}","type":"verifies"}`))
	tr.step("a link to an artifact that does not exist", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{phantom}}","type":"verifies"}`))
	tr.step("a link with a malformed body", owner, "POST /api/v1/links", jsonBody(`{"from_id":`))

	// The flow-down link (REQ-145): refines crosses from C into P with viewer
	// rights on P; any other type needs editor rights on the far project.
	tr.step("the supplier refines P's requirement from C, with viewer rights on P", supplier, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}`),
		note("the FlowDown feature gate passes: a personal workspace is on the nightly channel")).
		capture("l_refines", "/id")
	tr.step("the supplier's derives-from link into P needs editor rights there", supplier, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{creq}}","to_id":"{{need}}","type":"derives-from"}`))
	tr.step("P's linked artifacts: C's requirement, with its project's name and ref", owner,
		"GET /api/v1/projects/{id}/linked-artifacts", at("id", "{{p}}"))
	tr.step("C's linked artifacts: P's requirement", owner, "GET /api/v1/projects/{id}/linked-artifacts",
		at("id", "{{c}}"))
	tr.step("the supplier reads P's linked artifacts as a viewer", supplier,
		"GET /api/v1/projects/{id}/linked-artifacts", at("id", "{{p}}"))
	tr.step("the linked artifacts of a project that does not exist", owner,
		"GET /api/v1/projects/{id}/linked-artifacts", at("id", "{{phantom}}"))

	// Reads.
	tr.step("read a link: its own valid_from, where lists carry the zero time", owner, "GET /api/v1/links/{id}",
		at("id", "{{l_verifies}}"))
	tr.step("read a link that does not exist", owner, "GET /api/v1/links/{id}", at("id", "{{phantom}}"))
	tr.step("P's links, newest first, the one from C included", owner, "GET /api/v1/links", query("project_id={{p}}"))
	tr.step("C's links: the refines link, listed from either end", owner, "GET /api/v1/links", query("project_id={{c}}"))
	tr.step("the requirement's current links: outgoing, then incoming", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{req}}"))
	tr.step("the requirement's version 5, written by the refines link's auto-version: incoming first", owner,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{req}}"), query("version=5"))
	tr.step("a version that is not a number", owner, "GET /api/v1/artifacts/{id}/links", at("id", "{{req}}"),
		query("version=abc"))
	tr.step("a version with trailing text: read as its leading number", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{req}}"), query("version=2x"),
		note("the handler reads the version's leading digits and ignores what follows them"))
	tr.step("a version the artifact never had", owner, "GET /api/v1/artifacts/{id}/links", at("id", "{{req}}"),
		query("version=99"))
	tr.step("the links of an artifact that does not exist", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{phantom}}"), note("no artifact, so no project: the guard answers 404 \"project not found\""))
	tr.step("every create left an auto-version note on the requirement", owner, "GET /api/v1/chatter",
		query("artifact_id={{req}}"))

	// Suspect and confirm: a content edit of an endpoint marks its links
	// suspect; confirming clears the flag and publishes nothing.
	tr.step("edit the user need's body: its links turn suspect", owner, "PUT /api/v1/artifacts/{id}",
		at("id", "{{need}}"), jsonBody(`{"body":"Users need every answer traced."}`))
	tr.step("the derives-from link is suspect", owner, "GET /api/v1/links/{id}", at("id", "{{l_derives}}"))
	tr.step("confirm it: suspect cleared, no event", owner, "PUT /api/v1/links/{id}/confirm", at("id", "{{l_derives}}"))
	tr.step("confirm it again: a harmless no-op", owner, "PUT /api/v1/links/{id}/confirm", at("id", "{{l_derives}}"))
	tr.step("confirm a link that does not exist", owner, "PUT /api/v1/links/{id}/confirm", at("id", "{{phantom}}"))
	tr.step("the link once confirmed", owner, "GET /api/v1/links/{id}", at("id", "{{l_derives}}"))

	// Update: type and attributes replaced as sent, auto-versions both ends,
	// publishes link.updated with the type the link has now (#379 bug 132).
	tr.step("give the verifies link attributes", owner, "PUT /api/v1/links/{id}", at("id", "{{l_verifies}}"),
		jsonBody(`{"type":"verifies","attributes":{"method":"test","witness":"<qa> & co"}}`))
	tr.step("retype the derives-from link to one its rule refuses: stored as sent", owner, "PUT /api/v1/links/{id}",
		at("id", "{{l_derives}}"), jsonBody(`{"type":"verifies","attributes":{"note":"retyped"}}`),
		note("UpdateLink checks no link rule; POST /api/v1/links answers 400 for verifies from a requirement"))
	tr.step("retype it back; attributes left out become null", owner, "PUT /api/v1/links/{id}",
		at("id", "{{l_derives}}"), jsonBody(`{"type":"derives-from"}`))
	tr.step("update a link that does not exist", owner, "PUT /api/v1/links/{id}", at("id", "{{phantom}}"),
		jsonBody(`{"type":"verifies"}`))
	tr.step("update a link with a malformed body", owner, "PUT /api/v1/links/{id}", at("id", "{{l_verifies}}"),
		jsonBody(`[`))

	// Delete: soft, auto-versions both ends, publishes link.deleted.
	tr.step("delete the satisfies link: link.deleted", owner, "DELETE /api/v1/links/{id}", at("id", "{{l_satisfies}}"))
	tr.step("delete it again", owner, "DELETE /api/v1/links/{id}", at("id", "{{l_satisfies}}"),
		note("the link is gone, so its project resolves to nothing and the guard answers 404 \"project not found\""))
	tr.step("delete a link that does not exist", owner, "DELETE /api/v1/links/{id}", at("id", "{{phantom}}"))
	tr.step("read the deleted link", owner, "GET /api/v1/links/{id}", at("id", "{{l_satisfies}}"))
	tr.step("the design item's version 3, written when its last link went: an empty snapshot, []", owner,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{di}}"), query("version=3"),
		note("the automatic version always writes links_snapshot, empty or not (Q4)"))

	// The managed edit (Q3): in one PUT of the design item, a valid add, an
	// invalid add and the removal of a link between two other artifacts.
	tr.step("managed edit of the design item: add satisfies and verifies to the requirement, remove the verifies "+
		"link from the test case", owner, "PUT /api/v1/artifacts/{id}", at("id", "{{di}}"),
		jsonBody(`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}","type":"satisfies"},`+
			`{"from_id":"{{di}}","to_id":"{{req}}","type":"verifies"}],"pendingLinkRemoves":["{{l_verifies}}"]}`),
		note("Q3: the invalid verifies add is skipped without a word; no link.created or link.deleted is "+
			"published, only artifact.updated; the links_snapshot in this answer is the handler's own list of "+
			"links, keys in the order the server writes a link in, not the stored jsonb's")).
		capture("l_managed", "/attributes/links_snapshot/0/id")
	tr.step("the design item's note lists the links requested, the skipped one too (Q3)", owner,
		"GET /api/v1/chatter", query("artifact_id={{di}}"))
	tr.step("the design item's current links: only the satisfies link was made", owner,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{di}}"))
	tr.step("the design item read back: its links_snapshot through jsonb, keys in alphabetical order", owner,
		"GET /api/v1/artifacts/{id}", at("id", "{{di}}"))
	tr.step("the test case's current links: none left", owner, "GET /api/v1/artifacts/{id}/links", at("id", "{{tc}}"))
	tr.step("the test case's version 3 still lists the removed link", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{tc}}"), query("version=3"))
	tr.step("the test case has no version 4: the removed link's source was not auto-versioned", owner,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{tc}}"), query("version=4"))
	tr.step("the test case's notes: nothing from the managed edit", owner, "GET /api/v1/chatter",
		query("artifact_id={{tc}}"))

	// Q4: a managed removal that leaves the artifact no link writes no
	// links_snapshot, so the new version carries the old one forward.
	tr.step("managed edit of the design item: remove its only link", owner, "PUT /api/v1/artifacts/{id}",
		at("id", "{{di}}"), jsonBody(`{"pendingLinkRemoves":["{{l_managed}}"]}`),
		note("Q4: no link remains, so the handler leaves attributes alone and the version carries the previous "+
			"links_snapshot forward, read back from jsonb"))
	tr.step("the design item's current links: none", owner, "GET /api/v1/artifacts/{id}/links", at("id", "{{di}}"))
	tr.step("its version 5 still lists the removed link (Q4)", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{di}}"), query("version=5"))
	tr.step("a managed edit whose entries all fall away: an add with no type, the removal of no link", owner,
		"PUT /api/v1/artifacts/{id}", at("id", "{{di}}"),
		jsonBody(`{"pendingLinkAdds":[{"from_id":"{{di}}","to_id":"{{req}}"}],"pendingLinkRemoves":["{{phantom}}"]}`),
		note("neither entry reaches the note or the link table, yet the update still makes a version"))
	tr.step("the design item's notes: the removal, then a version with nothing listed", owner, "GET /api/v1/chatter",
		query("artifact_id={{di}}"))

	// The supplier's managed edit: the same refines link POST /links let it
	// make needs editor rights on P through this path, and is skipped.
	tr.step("the supplier's managed add of a refines link into P is skipped (Q3)", supplier,
		"PUT /api/v1/artifacts/{id}", at("id", "{{creq}}"),
		jsonBody(`{"pendingLinkAdds":[{"from_id":"{{creq}}","to_id":"{{req}}","type":"refines"}]}`),
		note("a managed edit wants editor rights on the other end's project for every type, where "+
			"POST /api/v1/links lets refines through with viewer rights; the note still lists the add"))
	tr.step("C's requirement: still the one refines link", supplier, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{creq}}"))
	tr.step("C's requirement's notes", owner, "GET /api/v1/chatter", query("artifact_id={{creq}}"))

	// Q4: every auto-version note names the version read before it plus 1.
	tr.step("the requirement's notes: one auto-version per link write touching it (Q4)", owner, "GET /api/v1/chatter",
		query("artifact_id={{req}}"))
	tr.step("the requirement's version 11, its last auto-version: incoming, then outgoing", owner,
		"GET /api/v1/artifacts/{id}/links", at("id", "{{req}}"), query("version=11"))
	tr.step("and no version 12: the notes' numbers are the versions made", owner, "GET /api/v1/artifacts/{id}/links",
		at("id", "{{req}}"), query("version=12"))
	tr.step("P's links at the end", owner, "GET /api/v1/links", query("project_id={{p}}"))
}
