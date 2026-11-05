//go:build unix

package main

import (
	"net/http"
	"testing"
)

// TestTourS5bTodos is the S5b tour's to-dos area (issue #731): one paragraph
// to tick off, kept off the board. Its golden is testdata/tour/s5b/todos.json.
//
// The owner, in its personal (nightly) workspace, keeps project P with
// requirement R and a note on it, and project Q with a note of its own. The
// viewer is a project viewer of P.
//
// What the golden shows:
//   - P's list is [] before any to-do; a to-do raised from P's note takes R
//     as its source artifact; one raised from Q's note, a second paragraph,
//     an empty text and an assignee who is not a member of P are a 400; the
//     viewer's add is a 403; the list is open first, soonest due first;
//   - the note's feed shows the to-do beside the note, with its status;
//   - a tick stamps done_at, opening it again clears it; an edit replaces the
//     text, the assignee and the due date;
//   - promoting makes a work item in todo carrying the text and ticks the
//     to-do off as promoted; promoting it again is a 409;
//   - a personal to-do is listed in My to-dos, reads and ticks for its
//     writer, is a 404 to the viewer, and cannot be promoted (400);
//   - a delete is a 204, and the to-do is a 404 after it.
func TestTourS5bTodos(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "todos",
		about: "To-dos: a project's list, one raised from a note, ticking off, editing, promoting to a board task, " +
			"a personal to-do and My to-dos.",
		run: todosTour,
	})
}

func todosTour(tr *tour) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour to-dos"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour to-dos elsewhere"}`)).capture("q", "/id")
	tr.setup("R in P", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Answer in time","body":"The system shall answer within 2 s."}`)).capture("r", "/id")
	tr.setup("a note on R", o, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{r}}","message":"Check the wording of this one."}`)).capture("note_p", "/id")
	tr.setup("Q's requirement", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{q}}","type":"requirement",`+
		`"title":"Log each answer"}`)).capture("rq", "/id")
	tr.setup("a note on Q's requirement", o, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{rq}}","message":"Elsewhere."}`)).capture("note_q", "/id")
	viewer := tr.register("viewer", "Tour Viewer", "a project viewer of P")
	tr.setup("the viewer joins P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(http.StatusCreated))
	tr.keep("2026-03-01T00:00:00Z", "a due date the tour sends in UTC")

	list := "GET /api/v1/projects/{id}/todos"
	create := "POST /api/v1/projects/{id}/todos"
	tr.step("P has no to-do yet: []", o, list, at("id", "{{p}}"))
	tr.step("a to-do raised from the note on R: R is its source", o, create, at("id", "{{p}}"),
		jsonBody(`{"text":"Check the wording of R","source_chatter_id":"{{note_p}}"}`)).capture("t1", "/id")
	tr.step("a to-do due on 1 March, for the owner", o, create, at("id", "{{p}}"),
		jsonBody(`{"text":"Ring the supplier","assignee_id":"{{owner}}","due_date":"2026-03-01T00:00:00Z"}`)).
		capture("t2", "/id")
	tr.step("Q's note: 400", o, create, at("id", "{{p}}"), jsonBody(`{"text":"x","source_chatter_id":"{{note_q}}"}`))
	tr.step("two paragraphs: 400", o, create, at("id", "{{p}}"), jsonBody(`{"text":"one\n\ntwo"}`))
	tr.step("no text: 400", o, create, at("id", "{{p}}"), jsonBody(`{"text":"  "}`))
	tr.step("the viewer may not add one: 403", viewer, create, at("id", "{{p}}"), jsonBody(`{"text":"x"}`))
	tr.step("the viewer reads P's to-dos: the dated one first", viewer, list, at("id", "{{p}}"))
	tr.step("R's notes show the to-do raised from the note", o, "GET /api/v1/chatter", query("artifact_id={{r}}"))

	done := "POST /api/v1/todos/{id}/done"
	tr.step("t1 ticked off: done_at stamped", o, done, at("id", "{{t1}}"), jsonBody(`{"done":true}`))
	tr.step("the viewer may not tick one: 403", viewer, done, at("id", "{{t2}}"), jsonBody(`{"done":true}`))
	tr.step("t1 opened again", o, done, at("id", "{{t1}}"), jsonBody(`{"done":false}`))
	tr.step("t2 edited: new text, nobody, no date", o, "PUT /api/v1/todos/{id}", at("id", "{{t2}}"),
		jsonBody(`{"text":"Ring the supplier about the seals"}`))
	tr.step("t1 read", viewer, "GET /api/v1/todos/{id}", at("id", "{{t1}}"))

	promote := "POST /api/v1/todos/{id}/promote"
	tr.step("t1 promoted: a work item in todo, and t1 done as promoted", o, promote, at("id", "{{t1}}"))
	tr.step("t1 again: 409", o, promote, at("id", "{{t1}}"))

	tr.step("a personal to-do", o, "POST /api/v1/todos", jsonBody(`{"text":"Book the rig"}`)).capture("mine", "/id")
	tr.step("My to-dos: the owner's personal one (t2, edited to nobody, is not listed)", o, "GET /api/v1/todos")
	tr.step("the viewer's My to-dos: none", viewer, "GET /api/v1/todos")
	tr.step("the personal to-do, to the viewer: 404", viewer, "GET /api/v1/todos/{id}", at("id", "{{mine}}"))
	tr.step("a personal to-do has no board: 400", o, promote, at("id", "{{mine}}"))
	tr.step("the personal to-do ticked off", o, done, at("id", "{{mine}}"), jsonBody(`{"done":true}`))

	del := "DELETE /api/v1/todos/{id}"
	tr.step("the viewer may not delete one: 403", viewer, del, at("id", "{{t2}}"))
	tr.step("t2 deleted: 204", o, del, at("id", "{{t2}}"))
	tr.step("t2 after the delete: 404", o, "GET /api/v1/todos/{id}", at("id", "{{t2}}"))
}
