//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// TestTourS5bEvidence is the S5b tour's evidence area (refactor plan §6.4
// S5b, before M8 splits suite_handlers.go; invariants I3 (guard, lookup and
// decode order), I4, I5, I8 (the evidence download's headers: the %q-quoted
// Content-Disposition, X-Evidence-SHA256, its own security policy, and
// Range/206, 416 and 304 from http.ServeFile) and I16 (evidence file naming,
// UPLOADS_DIR/evidence-<uuid>); OpenV REQ-13, REQ-23, REQ-143). Its golden is
// testdata/tour/s5b/evidence.json.
//
// The owner, an ordinary account in its personal (nightly) workspace, keeps
// project P, with a requirement and two test cases; run R1 holds a result
// for each (res1, res2) and run R2 one for the first case (res3), all
// recorded as setup (the test runs area pins those routes). Project Q holds
// bundle BQ, which a result of P cites across projects, and project E
// nothing. A viewer of P and an outsider show the role gates. The area walks
// a capture session's life: a bundle created (the refusals of its body and
// guard; the trims, the dropped empty condition key and the numbers a
// conditions map round-trips through interface{}), listed, read and edited
// (a full replace: a captured_at left out is cleared); two files uploaded
// (the refusals of the form; the File JSON with its digest) and downloaded
// (whole, by one and two byte ranges, past the end, conditionally, and
// compressed; by the viewer, the outsider and no session; a phantom and a
// malformed id); results citing bundles (the refusals, a repeat that answers
// a fresh id and time nothing stored, a bundle of another project) and the
// citation reads by result, by run and by bundle; the citations removed; and
// the deletes, which unlink the stored bytes, and a run's delete, which takes
// its citations with it. No evidence route publishes an event; every write
// step records that as an empty events list.
//
// Formats: every answer but the download is respondJSON's (application/json,
// a trailing newline) or writeJSONError's; the lists of an empty project and
// of a result with no citation answer [], and a run with no citation {} (the
// handlers normalise nil). The download is always application/octet-stream
// whatever type the upload declared (that type is only stored, as
// mime_type), carries the stored file's name %q-quoted (so the quote in the
// CSV's name is escaped), its digest, nosniff and a policy that permits
// nothing, and is served by http.ServeFile: Accept-Ranges and Last-Modified
// (the stored file's mtime), a 206 per range, multipart/byteranges for two,
// a 416 through http.Error (its text/plain message in place of the file's
// type and length, and no Last-Modified or Accept-Ranges), a 304 for
// If-Modified-Since its Last-Modified. The 2,000-byte
// file's gzip variant is compressed and loses its Content-Length; the
// 60-byte CSV is below the compressor's floor.
//
// Nondeterminism: only ids and minted times. Each bundle of P carries a
// distinct captured_at, or none (then its created_at, minted by its own
// request), so the list's ORDER BY COALESCE(captured_at, created_at) DESC
// never falls through to the random id; files and citations are each
// written by a request of their own, so their created_at order is fixed. A
// run's citations are a Go map keyed by random result ids, so every read of
// one holds at most one cited result of that run. The captured_at values are
// in the past, before any clock this area runs on, so a bundle without one
// (sorted by when it was created) always lists first. The one stored file
// left at the end is listed in uploads[].
//
// The size limits: the area's server sets the per-file limit
// (OPENV_MAX_EVIDENCE_MB, 200 MB by default) and the workspace's evidence
// storage quota (OPENV_LIMITS' evidence_storage_mb over the plan's, in GB)
// to 1 MB each, which nothing else this area uploads comes near, so that the
// last steps reach both 413s: a file of 1 MiB and one byte is over the
// per-file limit, which is checked first, and a file of exactly 1 MiB is
// within it but over the quota once written, beside what the workspace
// already stores. Each refused file is removed from UPLOADS_DIR (uploads[]
// still lists one file), and the golden records the two uploads' parts by
// size and digest, not their megabyte of text. Not pinned: the quota check
// before the write (CheckQuota with nothing incoming), which a fixed limit
// never fails, since every write the check after it lets through keeps the
// workspace within the limit; the MaxBytesReader 413 of a request over the
// limit and 1 MiB, which a file part alone never reaches (the copy stops
// one byte past the limit) and which answers the per-file text; and the 500s
// that need a failing disk or database.
func TestTourS5bEvidence(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "evidence",
		about: "Evidence bundles (create, list, read, replace, delete), their files (upload, download with Range, " +
			"If-Modified-Since and gzip, delete, and the per-file and workspace limits) and the citations that tie " +
			"test results to them (cite, list by result and by run, uncite), with the role gates of a viewer and an " +
			"outsider.",
		run: evidenceTour,
		env: map[string]string{
			"OPENV_MAX_EVIDENCE_MB": "1",
			"OPENV_LIMITS":          `{"evidence_storage_mb":1}`,
		},
	})
}

// evidenceCSV is the small upload: 60 bytes of CSV, below the compressor's
// 1,400-byte floor, uploaded as text/csv under a name with quotes in it.
const evidenceCSV = "condition,level_dbv,noise_uv\n" +
	"quiet,-42.0,3.1\n" +
	"loud,-12.5,9.8\n"

// evidenceCSVName is the CSV's name as uploaded: the quotes show %q's escape
// in the download's Content-Disposition.
const evidenceCSVName = `sweep "run 3".csv`

// evidenceLog is the large upload: 40 lines of 50 bytes, 2,000 bytes of
// text, over the compressor's floor, so its download's gzip variant is
// compressed.
var evidenceLog = func() string {
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "sweep step %02d: level -42.0 dBV, noise 3.1 uV, ok.\n", i)
	}
	return b.String()
}()

// evidenceForm is an evidence upload form: one file part, under the field
// name the handler does not read (it takes the first part with a filename).
func evidenceForm(name, contentType, data string) tourOpt {
	return rawBody(multipartForm(nil, tourFormFile{field: "file", name: name, contentType: contentType, data: []byte(data)}))
}

// evidenceDigest is the hex SHA-256 of a fixture, as the upload records it.
func evidenceDigest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// evidenceSeed creates the projects, test cases, runs, results and accounts
// the area reads (setup; other areas pin those routes).
func evidenceSeed(tr *tour) (viewer, outsider *tourActor) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour evidence"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour other evidence"}`)).capture("q", "/id")
	tr.setup("project E, which never gets a bundle", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour empty"}`)).
		capture("e", "/id")
	tr.setup("requirement R", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Stay quiet","body":"The system shall keep its noise floor below 5 uV."}`)).capture("req", "/id")
	for _, tc := range []struct{ name, title string }{
		{"tc1", "Measure the noise floor"},
		{"tc2", "Measure the output level"},
	} {
		tr.setup("test case "+tc.title, o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
			`"type":"test-case","title":"`+tc.title+`","body":"Run the sweep on the rig."}`)).capture(tc.name, "/id")
		tr.setup(tc.title+" verifies R", o, "POST /api/v1/links",
			jsonBody(`{"from_id":"{{`+tc.name+`}}","to_id":"{{req}}","type":"verifies"}`))
	}
	tr.setup("run R1", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Rig campaign"}`)).capture("r1", "/id")
	tr.setup("res1: R1's result for TC1", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc1}}","status":"pass"}`)).capture("res1", "/id")
	tr.setup("res2: R1's result for TC2", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc2}}","status":"fail"}`)).capture("res2", "/id")
	tr.setup("run R2", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Rig campaign, repeat"}`)).capture("r2", "/id")
	tr.setup("res3: R2's result for TC1", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{r2}}"),
		jsonBody(`{"test_case_id":"{{tc1}}","status":"pass"}`)).capture("res3", "/id")
	tr.setup("bundle BQ of project Q", o, "POST /api/v1/projects/{id}/evidence-bundles", at("id", "{{q}}"),
		jsonBody(`{"title":"Shared rig capture","captured_by":"Lab A"}`), expect(201)).capture("bq", "/id")

	viewer = tr.register("viewer", "Tour Viewer", "a viewer of P, from a workspace of its own: may read, not write")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	outsider = tr.register("outsider", "Tour Outsider", "an ordinary account in a workspace of its own, with no "+
		"access to P")
	return viewer, outsider
}

func evidenceTour(tr *tour) {
	o := tr.owner
	viewer, outsider := evidenceSeed(tr)
	if len(evidenceCSV) != 60 || len(evidenceLog) != 2000 {
		tr.t.Fatalf("the evidence fixtures are %d and %d bytes, not 60 and 2,000", len(evidenceCSV), len(evidenceLog))
	}
	tr.keep(evidenceDigest(evidenceCSV), "the SHA-256 of the 60-byte CSV the area uploads, in the file JSON and "+
		"X-Evidence-SHA256")
	tr.keep(evidenceDigest(evidenceLog), "the SHA-256 of the 2,000-byte log the area uploads, in the file JSON and "+
		"X-Evidence-SHA256")
	tr.keep("2026-09-12T10:00:00+02:00", "a captured_at the tour sends with an offset, which the create echoes as sent")
	tr.keep("2026-09-12T10:00:00Z", "that captured_at as read back: the column is TIMESTAMP (no zone), which keeps "+
		"the wall time and drops the offset")
	tr.keep("2026-09-01T08:30:00Z", "a captured_at the tour sends in UTC, echoed and read back alike")
	conditions51 := make([]string, 51)
	for i := range conditions51 {
		conditions51[i] = fmt.Sprintf(`"k%02d":%d`, i+1, i+1)
	}

	// Creating a bundle: the guard, the decode, then the service's checks.
	tr.step("the bundles of a project with none: [], the handler normalises nil", o,
		"GET /api/v1/projects/{id}/evidence-bundles", at("id", "{{e}}"))
	tr.step("create a bundle with a malformed body", o, "POST /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"), jsonBody(`{"title":`))
	tr.step("create a bundle whose captured_at is not RFC 3339: the decoder refuses it", o,
		"POST /api/v1/projects/{id}/evidence-bundles", at("id", "{{p}}"),
		jsonBody(`{"title":"Sweep","captured_at":"yesterday"}`))
	tr.step("create a bundle with a blank title", o, "POST /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"), jsonBody(`{"title":"   "}`))
	tr.step("create a bundle with a title of 201 characters", o, "POST /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"), jsonBody(`{"title":"`+strings.Repeat("T", 201)+`"}`))
	tr.step("create a bundle with 51 conditions", o, "POST /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"), jsonBody(`{"title":"Sweep","conditions":{`+strings.Join(conditions51, ",")+`}}`))
	tr.step("the viewer creates a bundle", viewer, "POST /api/v1/projects/{id}/evidence-bundles", at("id", "{{p}}"),
		jsonBody(`{"title":"Viewer capture"}`))
	tr.step("the outsider creates a bundle", outsider, "POST /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"), jsonBody(`{"title":"Outsider capture"}`))
	tr.step("create B1: EVD-1, the title, captured_by and conditions trimmed, an empty key dropped", o,
		"POST /api/v1/projects/{id}/evidence-bundles", at("id", "{{p}}"),
		jsonBody(`{"title":"  Noise sweep, run 3  ","summary":"Swept the input from quiet to loud on bench 4 in `+
			`three passes, logging the level and the noise at each step; the quiet rows back TC1.",`+
			`"captured_at":"2026-09-12T10:00:00+02:00","captured_by":"  Lab B  ","conditions":{" rig ":"  Bench 4 ",`+
			`"":"dropped","ambient_c":21.50,"counter":12345678901234567890,"calibrated":true,"channels":[1,2]}}`),
		note("the ref comes from the project's artifact_ref_counters under the EVD prefix; captured_at is echoed as "+
			"sent, offset and all; the conditions' numbers pass through interface{}, so 21.50 comes back 21.5 and "+
			"12345678901234567890 as 12345678901234567000; files and citations are left out (omitempty), and "+
			"file_count and total_size are 0")).
		capture("b1", "/id")
	tr.step("create B2 with no captured_at and no conditions: EVD-2, conditions {}", o,
		"POST /api/v1/projects/{id}/evidence-bundles", at("id", "{{p}}"),
		jsonBody(`{"title":"Visual inspection","summary":"No file: the account is the evidence."}`)).
		capture("b2", "/id")
	tr.step("P's bundles: B2 first (no captured_at, so its created_at, today), then B1 (captured on 12 September)", o,
		"GET /api/v1/projects/{id}/evidence-bundles", at("id", "{{p}}"),
		note("ordered by COALESCE(captured_at, created_at) DESC, then id DESC; a list entry carries file_count and "+
			"total_size, never files"))
	tr.step("read B1: captured_at as the column holds it, the wall time with no offset", o,
		"GET /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("the viewer reads B1", viewer, "GET /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("the outsider reads B1: 403, not the 404 evidenceBundleChecked's comment promises", outsider,
		"GET /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"),
		note("the handler's doc comment says a bundle the caller may not see answers 404; the project guard answers "+
			"403"))
	tr.step("the viewer reads BQ, a bundle of a project it is not a member of", viewer,
		"GET /api/v1/evidence-bundles/{id}", at("id", "{{bq}}"))
	tr.step("read a bundle that does not exist", o, "GET /api/v1/evidence-bundles/{id}", at("id", "{{phantom}}"))
	tr.step("read a bundle by an id that is not a UUID: the driver's error, answered 500", o,
		"GET /api/v1/evidence-bundles/{id}", at("id", "not-a-uuid"))
	tr.step("the outsider lists P's bundles", outsider, "GET /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"))

	// Replacing a bundle: every field is sent back.
	tr.step("replace B2 with a malformed body", o, "PUT /api/v1/evidence-bundles/{id}", at("id", "{{b2}}"),
		jsonBody(`[`))
	tr.step("replace B2 with a blank title", o, "PUT /api/v1/evidence-bundles/{id}", at("id", "{{b2}}"),
		jsonBody(`{"title":""}`))
	tr.step("the viewer replaces B2", viewer, "PUT /api/v1/evidence-bundles/{id}", at("id", "{{b2}}"),
		jsonBody(`{"title":"Viewer edit"}`))
	tr.step("replace a bundle that does not exist", o, "PUT /api/v1/evidence-bundles/{id}", at("id", "{{phantom}}"),
		jsonBody(`{"title":"Nothing"}`))
	tr.step("replace B2, now captured on 1 September: updated_at moves, the ref stays", o,
		"PUT /api/v1/evidence-bundles/{id}", at("id", "{{b2}}"),
		jsonBody(`{"title":"Visual inspection","summary":"Looked at the solder joints.","captured_at":"2026-09-01T08:30:00Z",`+
			`"captured_by":"QA","conditions":{"lamp":"ring light"}}`))
	tr.step("P's bundles: B1 (12 September), then B2 (1 September)", o, "GET /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"))
	tr.step("replace B2 leaving out captured_at, summary and conditions: a full replace clears them", o,
		"PUT /api/v1/evidence-bundles/{id}", at("id", "{{b2}}"), jsonBody(`{"title":"Visual inspection"}`))
	tr.step("B2 read back: no captured_at, an empty summary, conditions {}", o, "GET /api/v1/evidence-bundles/{id}",
		at("id", "{{b2}}"))

	// Uploading files: the guard, the form, then the store.
	tr.step("upload with a JSON body", o, "POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b1}}"),
		jsonBody(`{"file":"sweep.csv"}`))
	tr.step("upload a form with no file part", o, "POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b1}}"),
		rawBody(multipartForm([][2]string{{"note", "no file here"}})),
		note("the handler walks the parts to the first with a filename, skipping ordinary fields"))
	tr.step("the viewer uploads to B1", viewer, "POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b1}}"),
		evidenceForm(evidenceCSVName, "text/csv", evidenceCSV))
	tr.step("upload to a bundle that does not exist", o, "POST /api/v1/evidence-bundles/{id}/files",
		at("id", "{{phantom}}"), evidenceForm(evidenceCSVName, "text/csv", evidenceCSV))
	tr.step("upload the 60-byte CSV to B1: the File JSON, its digest and the declared type", o,
		"POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b1}}"),
		evidenceForm(evidenceCSVName, "text/csv", evidenceCSV),
		note("stored flat as UPLOADS_DIR/evidence-<random uuid> (I16), never under the uploader's name")).
		capture("csv", "/id")
	tr.step("upload the 2,000-byte log to B1 under a path: the name is reduced to its last element", o,
		"POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b1}}"),
		evidenceForm("captures/bench-4/sweep-log.txt", "text/plain", evidenceLog),
		note("mime/multipart's FileName and the handler's filepath.Base both keep only the last element")).
		capture("log", "/id")
	tr.step("upload the same CSV to B2: another stored file, the same digest", o,
		"POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b2}}"),
		evidenceForm("inspection.csv", "", evidenceCSV),
		note("the part declares no Content-Type, so mime_type is empty")).capture("csv2", "/id")
	tr.step("read B1: its files in upload order, file_count 2 and total_size 2,060", o,
		"GET /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("P's bundles with their file counts and sizes", o, "GET /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"))

	// Downloading: always an attachment, always application/octet-stream,
	// served by http.ServeFile.
	lastModified := tr.step("download the CSV: application/octet-stream, the name %q-quoted, its digest and policy", o,
		"GET /api/v1/evidence-files/{id}/download", at("id", "{{csv}}"),
		note("the declared text/csv is only stored (mime_type); the served type is always application/octet-stream, "+
			"and Accept-Ranges and Last-Modified (the stored file's mtime) come from http.ServeFile")).
		header.Get("Last-Modified")
	tr.step("download its first 10 bytes (Range)", o, "GET /api/v1/evidence-files/{id}/download",
		at("id", "{{csv}}"), withHeader("Range", "bytes=0-9"))
	tr.step("download two ranges: multipart/byteranges", o, "GET /api/v1/evidence-files/{id}/download",
		at("id", "{{csv}}"), withHeader("Range", "bytes=0-8,29-33"),
		note("the boundary is 60 random hex characters, in the Content-Type and between the parts"))
	tr.step("download a range past the end: 416 through http.Error", o, "GET /api/v1/evidence-files/{id}/download",
		at("id", "{{csv}}"), withHeader("Range", "bytes=100-200"),
		note("http.Error replaces the file's type and Content-Length with its message's; ServeFile's error path "+
			"drops Last-Modified"))
	tr.step("download it with If-Modified-Since its Last-Modified: 304", o,
		"GET /api/v1/evidence-files/{id}/download", at("id", "{{csv}}"), withHeader("If-Modified-Since", lastModified))
	tr.step("download the 2,000-byte log: its gzip variant is compressed and loses its Content-Length", o,
		"GET /api/v1/evidence-files/{id}/download", at("id", "{{log}}"))
	tr.step("the viewer downloads the CSV", viewer, "GET /api/v1/evidence-files/{id}/download", at("id", "{{csv}}"))
	tr.step("the outsider downloads the CSV: 403, not the 404 the doc comment claims", outsider,
		"GET /api/v1/evidence-files/{id}/download", at("id", "{{csv}}"))
	tr.step("download with no session", tr.anon, "GET /api/v1/evidence-files/{id}/download", at("id", "{{csv}}"),
		note("the auth middleware answers before routing"))
	tr.step("download a file that does not exist", o, "GET /api/v1/evidence-files/{id}/download",
		at("id", "{{phantom}}"))
	tr.step("download by an id that is not a UUID: the driver's error, answered 500", o,
		"GET /api/v1/evidence-files/{id}/download", at("id", "not-a-uuid"),
		note("evidence and citation lookups send a malformed id's driver error to respondInternal, where the test run "+
			"lookups answer 404 for any error"))

	// Citing: the result's guard, the decode, bundle_id, the bundle's
	// guard, then the service.
	tr.step("res1's citations before any: [], the handler normalises nil", o,
		"GET /api/v1/test-results/{id}/citations", at("id", "{{res1}}"))
	tr.step("R2's citations before any: {}", o, "GET /api/v1/test-runs/{id}/citations", at("id", "{{r2}}"))
	tr.step("cite with a malformed body", o, "POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"),
		jsonBody(`{"bundle_id":`))
	tr.step("cite with no bundle_id", o, "POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"),
		jsonBody(`{"note":"which one?"}`))
	tr.step("cite with a note of 2,001 characters", o, "POST /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"), jsonBody(`{"bundle_id":"{{b1}}","note":"`+strings.Repeat("n", 2001)+`"}`),
		note("the service's ErrInvalid, so the message starts \"invalid evidence bundle\" though the note is the "+
			"citation's"))
	tr.step("cite a bundle that does not exist", o, "POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"),
		jsonBody(`{"bundle_id":"{{phantom}}"}`))
	tr.step("cite from a result that does not exist", o, "POST /api/v1/test-results/{id}/citations",
		at("id", "{{phantom}}"), jsonBody(`{"bundle_id":"{{b1}}"}`))
	tr.step("cite from a result id that is not a UUID: 500", o, "POST /api/v1/test-results/{id}/citations",
		at("id", "not-a-uuid"), jsonBody(`{"bundle_id":"{{b1}}"}`))
	tr.step("the viewer cites B1 from res1", viewer, "POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"),
		jsonBody(`{"bundle_id":"{{b1}}"}`))
	tr.step("the outsider cites B1 from res1", outsider, "POST /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"), jsonBody(`{"bundle_id":"{{b1}}"}`))
	tr.step("res1 cites B1: 201, the note trimmed, no denormalised fields", o,
		"POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"),
		jsonBody(`{"bundle_id":"{{b1}}","note":"  The quiet rows.  "}`)).capture("cite1", "/id")
	tr.step("res1 cites B1 again: 201 with a fresh id and time that were never stored", o,
		"POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"),
		jsonBody(`{"bundle_id":"{{b1}}","note":"A second note, dropped"}`),
		note("the unique pair refuses the insert, and the service answers ErrAlreadyCited with the citation it "+
			"built: an id and created_at no row holds, and the stored note is unchanged"))
	tr.step("res1 cites BQ, a bundle of project Q: the caller needs only to see Q", o,
		"POST /api/v1/test-results/{id}/citations", at("id", "{{res1}}"), jsonBody(`{"bundle_id":"{{bq}}"}`)).
		capture("cite_bq", "/id")
	tr.step("res1's citations: denormalised, oldest first", o, "GET /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"))
	tr.step("the viewer reads res1's citations", viewer, "GET /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"))
	tr.step("the outsider reads res1's citations", outsider, "GET /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"))
	tr.step("the citations of a result that does not exist", o, "GET /api/v1/test-results/{id}/citations",
		at("id", "{{phantom}}"))
	tr.step("R1's citations, keyed by result: res1 alone", o, "GET /api/v1/test-runs/{id}/citations",
		at("id", "{{r1}}"), note("a Go map keyed by result id; R1's other result, res2, cites nothing, so the "+
			"map holds one key and its order cannot vary"))
	tr.step("the viewer reads R1's citations", viewer, "GET /api/v1/test-runs/{id}/citations", at("id", "{{r1}}"))
	tr.step("the outsider reads R1's citations", outsider, "GET /api/v1/test-runs/{id}/citations",
		at("id", "{{r1}}"))
	tr.step("the citations of a run that does not exist", o, "GET /api/v1/test-runs/{id}/citations",
		at("id", "{{phantom}}"))
	tr.step("the citations of a run id that is not a UUID: 404 like any lookup error", o,
		"GET /api/v1/test-runs/{id}/citations", at("id", "not-a-uuid"))
	tr.step("res3, in R2, cites B1: one capture, results in two runs", o, "POST /api/v1/test-results/{id}/citations",
		at("id", "{{res3}}"), jsonBody(`{"bundle_id":"{{b1}}","note":"Same capture, repeat run"}`)).
		capture("cite3", "/id")
	tr.step("R2's citations: res3's", o, "GET /api/v1/test-runs/{id}/citations", at("id", "{{r2}}"))
	tr.step("read B1: its files and the results citing it, oldest first", o, "GET /api/v1/evidence-bundles/{id}",
		at("id", "{{b1}}"))
	tr.step("read BQ: the result of P citing it", o, "GET /api/v1/evidence-bundles/{id}", at("id", "{{bq}}"))

	// Uncite: idempotent, and blind to the bundle.
	tr.step("the viewer uncites BQ from res1", viewer, "DELETE /api/v1/test-results/{id}/citations/{bundleId}",
		at("id", "{{res1}}", "bundleId", "{{bq}}"))
	tr.step("uncite BQ from res1", o, "DELETE /api/v1/test-results/{id}/citations/{bundleId}",
		at("id", "{{res1}}", "bundleId", "{{bq}}"))
	tr.step("uncite it again: 204, the state asked for holds", o,
		"DELETE /api/v1/test-results/{id}/citations/{bundleId}", at("id", "{{res1}}", "bundleId", "{{bq}}"))
	tr.step("uncite a bundle that does not exist: 204, nothing looks the bundle up", o,
		"DELETE /api/v1/test-results/{id}/citations/{bundleId}", at("id", "{{res1}}", "bundleId", "{{phantom}}"))
	tr.step("uncite by a bundle id that is not a UUID: the driver's error, answered 500", o,
		"DELETE /api/v1/test-results/{id}/citations/{bundleId}", at("id", "{{res1}}", "bundleId", "not-a-uuid"))
	tr.step("uncite from a result that does not exist", o, "DELETE /api/v1/test-results/{id}/citations/{bundleId}",
		at("id", "{{phantom}}", "bundleId", "{{b1}}"))
	tr.step("res1's citations after the uncite: B1's alone", o, "GET /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"))

	// Deletes: a file, a run (its results' citations go with it), a bundle
	// (its files' bytes and its citations go with it).
	tr.step("the viewer deletes the CSV", viewer, "DELETE /api/v1/evidence-files/{id}", at("id", "{{csv}}"))
	tr.step("delete the CSV: its bytes leave UPLOADS_DIR", o, "DELETE /api/v1/evidence-files/{id}", at("id", "{{csv}}"))
	tr.step("delete it again", o, "DELETE /api/v1/evidence-files/{id}", at("id", "{{csv}}"))
	tr.step("download it once deleted", o, "GET /api/v1/evidence-files/{id}/download", at("id", "{{csv}}"))
	tr.step("delete a file by an id that is not a UUID: 500", o, "DELETE /api/v1/evidence-files/{id}",
		at("id", "not-a-uuid"))
	tr.setup("delete run R1 (the test runs area pins the route)", o, "DELETE /api/v1/test-runs/{id}",
		at("id", "{{r1}}"), expect(204))
	tr.step("read B1 once R1 is deleted: res1's citation went with its result, res3's stays", o,
		"GET /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("res1's citations once R1 is deleted: the result is gone", o, "GET /api/v1/test-results/{id}/citations",
		at("id", "{{res1}}"))
	tr.step("the viewer deletes B1", viewer, "DELETE /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("delete B1: its log's bytes leave UPLOADS_DIR, its citations go", o, "DELETE /api/v1/evidence-bundles/{id}",
		at("id", "{{b1}}"))
	tr.step("delete B1 again", o, "DELETE /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("read B1 once deleted", o, "GET /api/v1/evidence-bundles/{id}", at("id", "{{b1}}"))
	tr.step("download the log once its bundle is deleted", o, "GET /api/v1/evidence-files/{id}/download",
		at("id", "{{log}}"))
	tr.step("R2's citations once B1 is deleted: {} again", o, "GET /api/v1/test-runs/{id}/citations",
		at("id", "{{r2}}"))
	tr.step("P's bundles at the end: B2 and its one file", o, "GET /api/v1/projects/{id}/evidence-bundles",
		at("id", "{{p}}"), note("B2's CSV is the one stored file left (uploads[])"))

	// The limits, both 1 MB in this area's server; the workspace stores B2's
	// 60-byte CSV.
	tr.step("upload 1 MiB and one byte to B2: over the per-file limit, 413, and the bytes written removed", o,
		"POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b2}}"),
		evidenceForm("sweep-full.txt", "text/plain", strings.Repeat("Z", 1<<20+1)),
		note("the copy reads one byte past the limit and stops; the per-file check comes before the quota's"))
	tr.step("upload exactly 1 MiB to B2: within the per-file limit, over the workspace's quota once written, 413, "+
		"the stored file removed", o, "POST /api/v1/evidence-bundles/{id}/files", at("id", "{{b2}}"),
		evidenceForm("sweep-full.txt", "text/plain", strings.Repeat("Z", 1<<20)),
		note("the quota check after the write counts the bytes that landed with what the workspace already stores, "+
			"and its error's text is the answer"))
	tr.step("read B2 once both are refused: its one file", o, "GET /api/v1/evidence-bundles/{id}", at("id", "{{b2}}"))
}
