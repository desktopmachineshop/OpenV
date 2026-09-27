//go:build unix

package main

import (
	"strings"
	"testing"
)

// TestTourS5aAttachments is the S5a tour's attachments area (refactor plan
// §6.4 S5a; invariants I4, I5, I8 (Range/206 on downloads, Cache-Control,
// Content-Disposition and the per-file security policy) and I16 (upload file
// naming); quirks Q14 and Q19; OpenV REQ-4, REQ-137, REQ-143, REQ-157). Its
// golden is testdata/tour/s5a/attachments.json.
//
// The owner, an ordinary account in its personal (nightly) workspace, keeps
// one project with a requirement, REQ-1, which collects the figures; a
// second requirement, REQ-2, which never gets one (so its list stays null,
// Q14); and a test case, TC-1, with one figure, so the project's list spans
// two artifacts. The area walks a figure's life: the refused uploads, the
// upload, its metadata and download (whole, by byte range, conditional, by
// version), a second version, a rename, a restore, the other formats the
// catalogue accepts (PDF, SVG, STEP) and how each is served, the lists and
// the figure notes, an outsider's and an anonymous request, an id no figure
// has on every route, an artifact restore that redraws the artifact's ref
// under a figure that keeps the old one, and the deletes. An outsider, an
// ordinary account in a workspace of its own, is registered for the
// access steps. No step publishes an event.
//
// Every figure is deleted at the end, so the only file left under
// UPLOADS_DIR is the superseded second version, which a delete leaves on
// disk: the golden's uploads[] then holds one line whatever order the
// random file names would list in.
func TestTourS5aAttachments(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "attachments",
		about: "Attachments, which the API calls figures: upload and its refusals, metadata, download " +
			"(Range, If-Modified-Since, ?version, the per-file security policy), versions and restore, rename, " +
			"the artifact and project lists, the figure notes, and delete.",
		run: attachmentsTour,
	})
}

// attachmentsSTEP is the least a STEP upload's content check accepts: the
// ISO-10303-21 signature, then an empty exchange structure.
const attachmentsSTEP = "ISO-10303-21;\nHEADER;\nENDSEC;\nDATA;\nENDSEC;\nEND-ISO-10303-21;\n"

// attachmentsForm is an upload form: the fields, then one file part.
func attachmentsForm(fields [][2]string, name, contentType, data string) tourOpt {
	return rawBody(multipartForm(fields, tourFormFile{field: "file", name: name, contentType: contentType, data: []byte(data)}))
}

// attachmentsFields is an upload's artifact_id field, filled, and an
// optional title.
func attachmentsFields(tr *tour, artifact string, title ...string) [][2]string {
	f := [][2]string{{"artifact_id", tr.fill(artifact)}}
	for _, t := range title {
		f = append(f, [2]string{"title", t})
	}
	return f
}

func attachmentsTour(tr *tour) {
	o := tr.owner
	outsider := tr.register("outsider", "Tour Outsider",
		"an ordinary account in a workspace of its own, not a member of the owner's project")
	// 255 and 256 two-byte letters: a figure title is bounded in characters,
	// not bytes (attachments.MaxTitleLen).
	title255 := strings.Repeat("é", 255)
	title256 := strings.Repeat("é", 256)

	tr.setup("the project", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour figures"}`)).capture("project", "/id")
	tr.setup("REQ-1, which collects the figures", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{project}}",`+
		`"type":"requirement","title":"Show the wiring","body":"The system shall show its wiring diagram."}`)).
		capture("req", "/id")
	tr.setup("REQ-2, which never gets a figure", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{project}}",`+
		`"type":"requirement","title":"Stay bare","body":"The system shall carry no figure."}`)).capture("bare", "/id")
	tr.setup("TC-1, which gets one figure", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{project}}",`+
		`"type":"test-case","title":"Inspect the wiring","body":"Compare the unit with its diagram."}`)).capture("tc", "/id")

	// The lists before any figure exists (Q14).
	tr.step("REQ-1's figures before any exists: null (Q14)", o, "GET /api/v1/artifacts/{artifactID}/attachments",
		at("artifactID", "{{req}}"))
	tr.step("the project's figures before any exists: []", o, "GET /api/v1/projects/{projectID}/attachments",
		at("projectID", "{{project}}"))

	// Uploads the server refuses.
	tr.step("upload with no artifact_id", o, "POST /api/v1/attachments/upload",
		attachmentsForm(nil, "fig.png", "image/png", tourPNG))
	tr.step("upload with no file", o, "POST /api/v1/attachments/upload",
		rawBody(multipartForm(attachmentsFields(tr, "{{req}}"))))
	tr.step("upload a text file: not in the catalogue", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "notes.txt", "text/plain", "Some notes.\n"))
	tr.step("upload a .png that holds text: the content sniff refuses it", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "fig.png", "image/png", "hello"))
	tr.step("upload a .step without the STEP signature", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "part.step", "application/octet-stream", "hello"),
		note("a non-image is checked against its format's signature where it has one"))
	tr.step("upload with a title of 256 characters", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}", title256), "fig.png", "image/png", tourPNG),
		note("the title is checked after the file is stored, and the stored file is removed: uploads[] holds no file of this step"))
	tr.step("upload to an artifact that does not exist", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{phantom}}"), "fig.png", "image/png", tourPNG))
	tr.step("an outsider uploads to REQ-1", outsider, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "fig.png", "image/png", tourPNG))

	// The first figure: upload, metadata, download.
	tr.step("upload fig.png to REQ-1: figure REQ-1-FIG-1", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "fig.png", "image/png", tourPNG),
		note("the file is stored as UPLOADS_DIR/<random uuid>_<uploaded name> (I16); the figure is served as "+
			"<figure ref><extension>")).
		capture("fig", "/id")
	tr.step("its metadata", o, "GET /api/v1/attachments/{id}", at("id", "{{fig}}"))
	lastModified := tr.step("download it: served inline, with the raster image policy", o,
		"GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		note("the file is served as a static file: Accept-Ranges, and Last-Modified from the stored file's mtime")).
		header.Get("Last-Modified")
	tr.step("download its first 8 bytes (Range)", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		withHeader("Range", "bytes=0-7"))
	tr.step("download two ranges: multipart/byteranges", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{fig}}"), withHeader("Range", "bytes=0-1,4-5"),
		note("the boundary is 60 random hex characters, in the Content-Type and between the parts"))
	tr.step("download a range past the end", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		withHeader("Range", "bytes=100-200"))
	tr.step("download it again with If-Modified-Since its Last-Modified", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{fig}}"), withHeader("If-Modified-Since", lastModified))

	// A second version.
	tr.step("upload a new version with no file", o, "POST /api/v1/attachments/{id}/versions", at("id", "{{fig}}"),
		rawBody(multipartForm(nil)))
	tr.step("upload a text file as a new version", o, "POST /api/v1/attachments/{id}/versions", at("id", "{{fig}}"),
		attachmentsForm(nil, "notes.txt", "text/plain", "Some notes.\n"))
	tr.step("upload a .png that holds text as a new version", o, "POST /api/v1/attachments/{id}/versions",
		at("id", "{{fig}}"), attachmentsForm(nil, "fig2.png", "image/png", "hello"))
	tr.step("upload fig2.png as version 2: the figure keeps its ref and its served name", o,
		"POST /api/v1/attachments/{id}/versions", at("id", "{{fig}}"),
		attachmentsForm(nil, "fig2.png", "image/png", tourPNG2),
		note("the artifact takes a new version too, through an update that changes nothing it says"))
	tr.step("the versions, newest first", o, "GET /api/v1/attachments/{id}/versions", at("id", "{{fig}}"))
	tr.step("download it: version 2's bytes", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"))
	tr.step("download version 1", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		query("version=1"))
	tr.step("download version 0", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		query("version=0"))
	tr.step("download version x", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		query("version=x"))
	tr.step("download a version the figure never had", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{fig}}"), query("version=99"))

	// Rename (REQ-157).
	tr.step("rename with a malformed body: \"Invalid request body\", capital I (Q19)", o, "PUT /api/v1/attachments/{id}",
		at("id", "{{fig}}"), jsonBody(`{"title":`),
		note("the one site that writes \"Invalid request body\" where the others write \"invalid request body\" (Q19)"))
	tr.step("rename to a title of 256 characters", o, "PUT /api/v1/attachments/{id}", at("id", "{{fig}}"),
		jsonBody(`{"title":"`+title256+`"}`))
	tr.step("rename it: version 3, over version 2's file", o, "PUT /api/v1/attachments/{id}", at("id", "{{fig}}"),
		jsonBody(`{"title":"  Wiring diagram  "}`), note("the title is trimmed"))
	tr.step("rename it to the title it has: a no-op", o, "PUT /api/v1/attachments/{id}", at("id", "{{fig}}"),
		jsonBody(`{"title":"Wiring diagram"}`), note("no new version, no note, and no artifact version"))
	tr.step("REQ-1 after a new figure version and a rename: version 3", o, "GET /api/v1/artifacts/{id}",
		at("id", "{{req}}"), note("an upload of a new figure does not version the artifact; see the artifact "+
			"restore below, which answers version 4 after four more uploads"))
	// The versions are listed again after the restore, not here: three
	// versions come to about 1,300 bytes, which a longer temporary directory
	// on another machine would take over the compressor's 1,400-byte floor.

	// Restore.
	tr.step("restore version x", o, "POST /api/v1/attachments/{id}/versions/{version}/restore",
		at("id", "{{fig}}", "version", "x"))
	tr.step("restore a version the figure never had", o, "POST /api/v1/attachments/{id}/versions/{version}/restore",
		at("id", "{{fig}}", "version", "99"))
	tr.step("restore the current version", o, "POST /api/v1/attachments/{id}/versions/{version}/restore",
		at("id", "{{fig}}", "version", "3"))
	tr.step("restore version 1: version 4, restored_from 1, answering the version", o,
		"POST /api/v1/attachments/{id}/versions/{version}/restore", at("id", "{{fig}}", "version", "1"),
		note("the title travels with the image, so the rename is undone; the restore reuses version 1's file, "+
			"writes no figure note and takes no artifact version"))
	tr.step("its metadata after the restore", o, "GET /api/v1/attachments/{id}", at("id", "{{fig}}"))
	tr.step("the versions after the restore", o, "GET /api/v1/attachments/{id}/versions", at("id", "{{fig}}"))
	tr.step("download it: version 1's bytes again", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"))
	tr.step("REQ-1 after the restore: still version 3", o, "GET /api/v1/artifacts/{id}", at("id", "{{req}}"))

	// The other formats (REQ-137): on the nightly channel every workspace may
	// attach them; each is served as a download under a policy that permits
	// nothing.
	tr.step("upload a PDF: figure 2, kind document", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "spec.pdf", "application/pdf", tourPDF)).
		capture("pdf", "/id")
	tr.step("download the PDF: an attachment, sandboxed", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{pdf}}"))
	tr.step("upload an SVG: figure 3, kind image", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "outline.svg", "image/svg+xml", tourSVG)).
		capture("svg", "/id")
	tr.step("download the SVG: an image, but an attachment, sandboxed", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{svg}}"), note("an SVG can carry script, so it is never rendered on the API's origin"))
	tr.step("upload a STEP model sent as application/octet-stream: figure 4, kind model", o,
		"POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "part.step", "application/octet-stream", attachmentsSTEP),
		note("the extension decides the recorded type, not what the browser declared")).
		capture("step", "/id")
	tr.step("download the STEP model: an attachment, sandboxed", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{step}}"))
	tr.step("upload a PNG with a title: figure 5", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}", "Front view"), "front.png", "image/png", tourPNG)).
		capture("front", "/id")
	tr.step("upload a PNG to TC-1: its own figure 1", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{tc}}"), "bench.png", "image/png", tourPNG)).
		capture("bench", "/id")

	// The lists and the notes.
	tr.step("REQ-1's figures, in figure order", o, "GET /api/v1/artifacts/{artifactID}/attachments",
		at("artifactID", "{{req}}"))
	tr.step("REQ-2's figures: still null (Q14)", o, "GET /api/v1/artifacts/{artifactID}/attachments",
		at("artifactID", "{{bare}}"))
	tr.step("the project's figures: by artifact (sort order, then creation), then figure number", o,
		"GET /api/v1/projects/{projectID}/attachments", at("projectID", "{{project}}"))
	tr.step("REQ-1's notes: one per figure added, new version and rename, newest first", o, "GET /api/v1/chatter",
		query("artifact_id={{req}}"), note("the restore wrote none"))

	// Who else may ask.
	tr.step("an outsider reads the figure's metadata", outsider, "GET /api/v1/attachments/{id}", at("id", "{{fig}}"))
	tr.step("an outsider downloads the figure", outsider, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"))
	tr.step("an outsider lists the project's figures", outsider, "GET /api/v1/projects/{projectID}/attachments",
		at("projectID", "{{project}}"))
	tr.step("a download with no session", tr.anon, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"),
		note("the auth middleware answers before routing; an <img> tag's request carries the session cookie"))

	// An id no figure has.
	tr.step("the metadata of a figure that does not exist", o, "GET /api/v1/attachments/{id}", at("id", "{{phantom}}"))
	tr.step("download a figure that does not exist", o, "GET /api/v1/attachments/{id}/download",
		at("id", "{{phantom}}"))
	tr.step("the versions of a figure that does not exist", o, "GET /api/v1/attachments/{id}/versions",
		at("id", "{{phantom}}"))
	tr.step("a new version of a figure that does not exist", o, "POST /api/v1/attachments/{id}/versions",
		at("id", "{{phantom}}"), attachmentsForm(nil, "fig.png", "image/png", tourPNG))
	tr.step("rename a figure that does not exist", o, "PUT /api/v1/attachments/{id}", at("id", "{{phantom}}"),
		jsonBody(`{"title":"Nothing"}`))
	tr.step("restore a version of a figure that does not exist", o,
		"POST /api/v1/attachments/{id}/versions/{version}/restore", at("id", "{{phantom}}", "version", "1"))
	tr.step("delete a figure that does not exist", o, "DELETE /api/v1/attachments/{id}", at("id", "{{phantom}}"))
	tr.step("the figures of an artifact that does not exist", o, "GET /api/v1/artifacts/{artifactID}/attachments",
		at("artifactID", "{{phantom}}"))
	tr.step("the figures of a project that does not exist", o, "GET /api/v1/projects/{projectID}/attachments",
		at("projectID", "{{phantom}}"))

	// An artifact restore draws the artifact a new ref; its figures keep the
	// old one, and the next figure is numbered on from the artifact's counter
	// under the new ref.
	tr.step("restore REQ-1 to its version 1: the restore draws it a new ref, REQ-3", o,
		"POST /api/v1/artifacts/{id}/restore", at("id", "{{req}}"), jsonBody(`{"version":1}`),
		note("the artifacts area pins this route; it is here for what it does to the figures' refs"))
	tr.step("figure 1 after the artifact's restore: still REQ-1-FIG-1", o, "GET /api/v1/attachments/{id}",
		at("id", "{{fig}}"))
	tr.step("a figure uploaded after the restore: REQ-3-FIG-6", o, "POST /api/v1/attachments/upload",
		attachmentsForm(attachmentsFields(tr, "{{req}}"), "after.png", "image/png", tourPNG)).
		capture("after", "/id")

	// A title is bounded in characters.
	tr.step("rename the SVG to 255 two-byte characters: accepted", o, "PUT /api/v1/attachments/{id}",
		at("id", "{{svg}}"), jsonBody(`{"title":"`+title255+`"}`))

	// Delete.
	tr.step("delete figure 1", o, "DELETE /api/v1/attachments/{id}", at("id", "{{fig}}"),
		note("the current version's file is removed; a superseded version's file stays on disk (uploads[])"))
	tr.step("its metadata once deleted", o, "GET /api/v1/attachments/{id}", at("id", "{{fig}}"))
	tr.step("download it once deleted", o, "GET /api/v1/attachments/{id}/download", at("id", "{{fig}}"))
	tr.step("its versions once deleted", o, "GET /api/v1/attachments/{id}/versions", at("id", "{{fig}}"))
	for _, name := range []string{"pdf", "svg", "step", "front", "bench", "after"} {
		tr.setup("delete "+name, o, "DELETE /api/v1/attachments/{id}", at("id", "{{"+name+"}}"), expect(204))
	}
	tr.step("REQ-1's figures once all are deleted: null again (Q14)", o,
		"GET /api/v1/artifacts/{artifactID}/attachments", at("artifactID", "{{req}}"))
	tr.step("the project's figures once all are deleted: []", o, "GET /api/v1/projects/{projectID}/attachments",
		at("projectID", "{{project}}"))
}
