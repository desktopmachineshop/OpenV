//go:build unix

package main

import (
	"fmt"
	"testing"
)

// TestTourS5aReviewChatterSearch is the S5a tour's review, chatter and
// search area (refactor plan §6.4 S5a; quirks Q8 and Q14; OpenV REQ-4,
// REQ-143, REQ-165): the review state machine one artifact at a time (PUT
// /api/v1/artifacts/{id}/status), the project review round and the review
// queue, the notes feed (chatter) with its @mentions, the workspace search
// with its ref ranking, modes and limits, and the two semantic-search routes
// (duplicates, reindex-embeddings), which with no OPENV_EMBEDDING_API_KEY
// answer that embeddings are off, the same on the PG16 (vector) and PG15
// (no vector) legs. Its golden is testdata/tour/s5a/review_chatter_search.json.
//
// Project P holds one heading with five children (a user need, two
// requirements, a test case and a design item), every title unique; so the
// artifact list the review round walks (ordered by parent_id first, a random
// UUID) has one parent below the root and a fixed order, and "REQ-1" can
// match no REQ-1x. The test case verifies the first requirement, and an edit
// of that requirement makes the link suspect, so the review queue has a
// suspect link; approving the requirement clears it, and editing it again
// demotes it to draft and makes the link suspect again, which is what the
// review round's re-run picks up. Project B holds 51 bulk artifacts for the
// search limits (Q8).
//
// The owner, an ordinary account, owns both through its personal workspace.
// The reviewer, a second account, is a member of P only: first a viewer
// (refused a note, a status change and a review round), then a reviewer, who
// may comment and whom the owner's note names with an @mention, and last an
// editor, refused the owner's reindex.
func TestTourS5aReviewChatterSearch(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "review_chatter_search",
		about: "Review (status transitions one artifact at a time, the project review round and its re-runs, " +
			"the review queue with a suspect link), chatter (notes, @mentions, the auto notes of status changes) " +
			"and search (ref ranking, modes, the Q8 limit, workspace scope), with the semantic-search routes " +
			"answering that embeddings are off.",
		run: reviewChatterSearchTour,
	})
}

// reviewChatterSearchNeed is the user need's body, 297 bytes: "answers"
// falls more than 80 runes from either end, so a search for "answer"
// snippets it with an ellipsis on both sides.
const reviewChatterSearchNeed = "Operators work shifts of twelve hours at a console that shows every alarm the " +
	"plant raises, and they act on what it tells them. They need answers they can trust at the end of a long " +
	"shift, when attention is lowest and a wrong call costs the most, so every figure on the screen has to be traceable."

// reviewChatterSearchCache is the design item's body, 184 bytes, with no
// "answer" in it: its title matches, so its snippet is the head of the body,
// 160 runes and an ellipsis.
const reviewChatterSearchCache = "A read-through cache in front of the query engine keeps the last thousand " +
	"results warm, and it is flushed whenever the plant model changes, so a stale figure never reaches the console."

func reviewChatterSearchTour(tr *tour) {
	owner := tr.owner
	reviewer := tr.register("reviewer", "Tour Reviewer",
		"a member of project P only: a viewer, then a reviewer (named by an @mention in a note), then an editor")

	// Project P: one heading, and under it one child of each of four types
	// (two requirements), so REQ-1 and REQ-2 are the workspace's only
	// requirements.
	tr.setup("project P", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Review tour","description":"The project the review round runs over"}`)).capture("p", "/id")
	tr.setup("a heading in P", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"heading",`+
		`"title":"Scope","body":"What the review tour covers."}`)).capture("heading", "/id")
	for _, a := range []struct{ name, typ, title, body string }{
		{"need", "user-need", "Operator need", reviewChatterSearchNeed},
		{"req1", "requirement", "Answer in time", "The system shall answer every operator query within 1 s."},
		{"req2", "requirement", "Audit trail", "The system shall record 99% of answers in the audit trail within 5 s."},
		{"tc", "test-case", "Check REQ-1 timing", "Measure the answer time of REQ-1 over a thousand queries."},
		{"di", "design-item", "Answer cache", reviewChatterSearchCache},
	} {
		tr.setup("a "+a.typ+" under the heading", owner, "POST /api/v1/artifacts",
			jsonBody(`{"project_id":"{{p}}","parent_id":"{{heading}}","type":"`+a.typ+`","title":"`+a.title+
				`","body":"`+a.body+`"}`)).capture(a.name, "/id")
	}
	tr.setup("the test case verifies the first requirement", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tc}}","to_id":"{{req1}}","type":"verifies"}`)).capture("link", "/id")
	tr.setup("the reviewer views P", owner, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-reviewer@example.com","role":"viewer"}`))

	// Before anything is in review, noted or configured (Q14: null or []).
	tr.step("P's review queue before anything is in review: both lists [], never null", owner,
		"GET /api/v1/projects/{id}/review-queue", at("id", "{{p}}"))
	tr.step("the notes of an artifact that has none: null (Q14)", owner, "GET /api/v1/chatter",
		query("artifact_id={{need}}"))
	tr.step("P's duplicate candidates with no embedding key: enabled false, a note and []", owner,
		"GET /api/v1/projects/{id}/duplicates", at("id", "{{p}}"))
	tr.step("the duplicate candidates of a project that does not exist", owner,
		"GET /api/v1/projects/{id}/duplicates", at("id", "{{phantom}}"),
		note("the project guard runs before any lookup: 403, not 404"))
	tr.step("reindex P's embeddings with no embedding key: 200, nothing queued, no event", owner,
		"POST /api/v1/projects/{id}/reindex-embeddings", at("id", "{{p}}"))
	tr.step("reindex the embeddings of a project that does not exist", owner,
		"POST /api/v1/projects/{id}/reindex-embeddings", at("id", "{{phantom}}"))

	// A content edit of the link's target makes the link suspect.
	tr.setup("edit the first requirement's body", owner, "PUT /api/v1/artifacts/{id}", at("id", "{{req1}}"),
		jsonBody(`{"body":"The system shall answer every operator query within 2 s."}`))
	tr.step("the review queue: the suspect link, with both ends' titles and types", owner,
		"GET /api/v1/projects/{id}/review-queue", at("id", "{{p}}"))
	tr.step("the review queue of a project that does not exist", owner,
		"GET /api/v1/projects/{id}/review-queue", at("id", "{{phantom}}"))

	// One artifact at a time: draft <-> in_review -> approved -> superseded.
	tr.step("submit the second requirement for review: artifact.status_changed", owner,
		"PUT /api/v1/artifacts/{id}/status", at("id", "{{req2}}"), jsonBody(`{"status":"in_review"}`))
	tr.step("approve a draft directly: 409, the \">\" escaped", owner, "PUT /api/v1/artifacts/{id}/status",
		at("id", "{{need}}"), jsonBody(`{"status":"approved"}`))
	tr.step("a status the state machine does not know", owner, "PUT /api/v1/artifacts/{id}/status",
		at("id", "{{need}}"), jsonBody(`{"status":"bogus"}`))
	tr.step("the status of an artifact that does not exist", owner, "PUT /api/v1/artifacts/{id}/status",
		at("id", "{{phantom}}"), jsonBody(`{"status":"in_review"}`),
		note("the handler looks the artifact up before the project guard, so a phantom id answers 404"))
	tr.step("a status change with a malformed body", owner, "PUT /api/v1/artifacts/{id}/status",
		at("id", "{{need}}"), jsonBody(`{"status":`))
	tr.step("the reviewer, a viewer, changes a status: editors only", reviewer, "PUT /api/v1/artifacts/{id}/status",
		at("id", "{{req2}}"), jsonBody(`{"status":"approved"}`))
	tr.step("the review queue: the second requirement in review beside the suspect link", owner,
		"GET /api/v1/projects/{id}/review-queue", at("id", "{{p}}"))
	tr.step("approve the second requirement", owner, "PUT /api/v1/artifacts/{id}/status", at("id", "{{req2}}"),
		jsonBody(`{"status":"approved"}`))
	tr.step("approve it again: approved -> approved is no transition", owner, "PUT /api/v1/artifacts/{id}/status",
		at("id", "{{req2}}"), jsonBody(`{"status":"approved"}`))
	tr.step("submit the test case", owner, "PUT /api/v1/artifacts/{id}/status", at("id", "{{tc}}"),
		jsonBody(`{"status":"in_review"}`))
	tr.step("withdraw it to draft: in_review -> draft", owner, "PUT /api/v1/artifacts/{id}/status", at("id", "{{tc}}"),
		jsonBody(`{"status":"draft"}`))

	// The project review round (REQ-165).
	tr.step("a review round naming a type the catalogue does not have", owner,
		"POST /api/v1/projects/{id}/review-round", at("id", "{{p}}"), jsonBody(`{"types":["requirement","bogus"]}`))
	tr.step("a review round with a malformed body", owner, "POST /api/v1/projects/{id}/review-round",
		at("id", "{{p}}"), jsonBody(`{"types":`))
	tr.step("a review round of a project that does not exist", owner, "POST /api/v1/projects/{id}/review-round",
		at("id", "{{phantom}}"))
	tr.step("the reviewer, a viewer, starts a review round: editors only", reviewer,
		"POST /api/v1/projects/{id}/review-round", at("id", "{{p}}"))
	tr.step("a round of the requirements only: the draft one moves, the approved one stays", owner,
		"POST /api/v1/projects/{id}/review-round", at("id", "{{p}}"), jsonBody(`{"types":["requirement"]}`))
	tr.step("a round with no body: every type of the catalogue; the drafts move in list order", owner,
		"POST /api/v1/projects/{id}/review-round", at("id", "{{p}}"),
		note("moved follows the artifact list: the heading (no parent) first, then its children by sort_order; "+
			"with two parents below the root that order would follow their random ids"))
	tr.step("supersede the approved requirement", owner, "PUT /api/v1/artifacts/{id}/status", at("id", "{{req2}}"),
		jsonBody(`{"status":"superseded"}`))
	tr.step("superseded is terminal", owner, "PUT /api/v1/artifacts/{id}/status", at("id", "{{req2}}"),
		jsonBody(`{"status":"draft"}`))
	tr.step("the round again: nothing is a draft, so nothing moves", owner, "POST /api/v1/projects/{id}/review-round",
		at("id", "{{p}}"))
	tr.step("the review queue: five in review, newest change first, and the suspect link", owner,
		"GET /api/v1/projects/{id}/review-queue", at("id", "{{p}}"))
	tr.step("approve the first requirement: the suspicion on its link is cleared", owner,
		"PUT /api/v1/artifacts/{id}/status", at("id", "{{req1}}"), jsonBody(`{"status":"approved"}`))
	tr.step("the review queue: four in review, no suspect link", owner, "GET /api/v1/projects/{id}/review-queue",
		at("id", "{{p}}"))
	tr.setup("edit the approved requirement's body: back to draft, its link suspect again", owner,
		"PUT /api/v1/artifacts/{id}", at("id", "{{req1}}"),
		jsonBody(`{"body":"The system shall answer every operator query within 3 s."}`))
	tr.step("the round again: only the changed requirement moves", owner, "POST /api/v1/projects/{id}/review-round",
		at("id", "{{p}}"), jsonBody(`{}`))

	// Notes (chatter): the reviewer role may comment.
	tr.step("the reviewer, a viewer, writes a note: reviewers and up only", reviewer, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{req1}}","message":"Viewed only."}`))
	tr.setup("make the reviewer a reviewer on P", owner, "PUT /api/v1/projects/{id}/members/{userId}",
		at("id", "{{p}}", "userId", "{{reviewer}}"), jsonBody(`{"role":"reviewer"}`))
	tr.step("the owner's note names the reviewer, with markup the JSON escapes: chatter.created", owner,
		"POST /api/v1/chatter", jsonBody(`{"artifact_id":"{{req1}}","message":"Please check the new limit, `+
			`@tourreviewer: <b>3 s</b> & no more"}`)).capture("note_owner", "/id")
	tr.step("the reviewer answers, naming the owner and someone who is not a member", reviewer,
		"POST /api/v1/chatter", jsonBody(`{"artifact_id":"{{req1}}","message":"Agreed, @tour-owner; `+
			`@nobody need not look"}`)).capture("note_reviewer", "/id")
	tr.step("a note with a malformed body", owner, "POST /api/v1/chatter", jsonBody(`{"artifact_id":`))
	tr.step("a note on an artifact that does not exist", owner, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{phantom}}","message":"Nobody reads this."}`),
		note("an artifact with no project resolves to no project, which the guard answers 404"))
	tr.step("the notes with no artifact_id", owner, "GET /api/v1/chatter")
	tr.step("the notes of an artifact that does not exist", owner, "GET /api/v1/chatter",
		query("artifact_id={{phantom}}"))
	tr.step("the first requirement's notes, newest first: mentions resolved against P's members", owner,
		"GET /api/v1/chatter", query("artifact_id={{req1}}"),
		note("an entry lists the members it names (by name without spaces, first name or email local part), "+
			"in the order of P's members; @nobody names no member"))
	tr.step("the user need's notes: the round's status change", reviewer, "GET /api/v1/chatter",
		query("artifact_id={{need}}"))

	// Project B: 51 bulk artifacts for the limits (Q8).
	tr.setup("project B", owner, "POST /api/v1/projects", jsonBody(`{"name":"Bulk tour"}`)).capture("b", "/id")
	for i := 1; i <= 51; i++ {
		tr.setup("a bulk artifact", owner, "POST /api/v1/artifacts", jsonBody(fmt.Sprintf(
			`{"project_id":"{{b}}","type":"other","title":"Bulk item %02d"}`, i))).capture(fmt.Sprintf("bulk_%02d", i), "/id")
	}

	// Search: the owner's workspace, P and B.
	tr.step("search with no session", tr.anon, "GET /api/v1/search", query("q=answer"),
		note("the auth middleware answers before routing"))
	tr.step("search with an empty query", owner, "GET /api/v1/search", query("q="))
	tr.step("search by ref: the exact ref first, then a title that names it", owner, "GET /api/v1/search",
		query("q=REQ-1"))
	tr.step("search by ref in lower case, asking for semantic: a ref query always runs keyword", owner,
		"GET /api/v1/search", query("q=req-1&mode=semantic"))
	tr.step("search a phrase: title matches first, then body matches, each newest change first", owner,
		"GET /api/v1/search", query("q=answer"))
	tr.step("the same, mode semantic: no embeddings, so keyword", owner, "GET /api/v1/search",
		query("q=answer&mode=semantic"))
	tr.step("the same, mode hybrid: no embeddings, so keyword", owner, "GET /api/v1/search",
		query("q=answer&mode=hybrid"))
	tr.step("the same, limit 1", owner, "GET /api/v1/search", query("q=answer&limit=1"))
	tr.step("a query that is a LIKE wildcard matches it literally", owner, "GET /api/v1/search", query("q=%25"))
	tr.step("limit 0 means the default, 20 (Q8)", owner, "GET /api/v1/search", query("q=bulk&limit=0"))
	tr.step("limit 51 is capped at 50 (Q8)", owner, "GET /api/v1/search", query("q=bulk&limit=51"))
	tr.step("the reviewer searches: P is not in the reviewer's workspace", reviewer, "GET /api/v1/search",
		query("q=answer"), note("search covers the caller's active workspace only, whatever projects it is a member of"))
	tr.step("the reviewer's search acting in the owner's workspace, of which it is no member", reviewer,
		"GET /api/v1/search", query("q=answer"), actingIn("{{owner.workspace}}"),
		note("an X-Org-ID the caller is no member of falls back to its own workspace"))

	// Reindexing is the project owner's: an editor is refused.
	tr.setup("make the reviewer an editor on P", owner, "PUT /api/v1/projects/{id}/members/{userId}",
		at("id", "{{p}}", "userId", "{{reviewer}}"), jsonBody(`{"role":"editor"}`))
	tr.step("the reviewer, now an editor, reindexes P's embeddings: owners only", reviewer,
		"POST /api/v1/projects/{id}/reindex-embeddings", at("id", "{{p}}"))
}
