//go:build unix

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTourS5cRunnerKeysConnector is the S5c tour's runners and Agent
// Connector area (refactor plan §6.4 S5c, before M9, which splits
// org_handlers.go's worker-key, runner-key, hosted-runner, worker-status and
// connector sections into files of their own; invariants I3, I4, I5, I8
// (Content-Disposition, Accept-Ranges, Last-Modified, Range/206 and 416,
// If-Modified-Since/304 on the connector download), I15 (the download's
// filenames and media types); quirks Q1, Q14 and Q19; OpenV REQ-143). Its
// golden is testdata/tour/s5c/runner_keys_connector.json.
//
// The area walks, in order, in the owner's shared workspace W with a plain
// member:
//   - worker keys, which only a workspace admin lists, mints and revokes:
//     none yet (null, Q14), three minted (a name trimmed, none, and a
//     workspace key named "hosted-runner", which the fleet view takes for the
//     hosted runner by its name alone), each refusal (a body that does not
//     decode, a name longer than its column, whose database error the answer
//     passes through (Q19), a member), the list newest first, a key used as
//     a bearer (the connector's poll, POST /api/v1/agent-runs/claim, answers
//     204 with no run queued; a workspace route answers the handler's 401,
//     since a key is no member, and stamps last_used_at all the same),
//     revocation (twice: 204 both times), each revocation refusal in the
//     handler's order (an unknown id, an id that is not a UUID, whose
//     database error the answer passes through too, a key of another
//     workspace, a member), and a revoked key refused by the middleware
//     ("invalid token");
//   - the member's personal runner key: none, minted ("<name>'s runner"),
//     offline until used, online once used (last_used_at within the last 30
//     s: a step takes milliseconds, so the reads right after a use are
//     online), minted again (the old key revoked and refused), and a
//     workspace the member is not in;
//   - connector pairing: a one-time code with its deep links (PUBLIC_URL,
//     URL-escaped, and the & JSON-escaped as encoding/json writes it, I4),
//     traded on the public route for a personal key named "personal-runner"
//     (the handler hands the domain no user name), which rotates the member's
//     runner key; the code again, an unknown code, an empty one and a body
//     that does not decode;
//   - the fleet (worker-status): every key of the workspace in key order,
//     revoked ones too, each online if used within the last 30 s (the box
//     key polls again just before, so that the read does not depend on how
//     long the steps between took), with the queue's counts at zero;
//   - the personal runner key revoked, twice (204 both times);
//   - the hosted runner with HOSTED_RUNNERS=off (the harness's): the empty
//     record, and the refusals (the deployment's before the plan flag and
//     the body, then "no hosted runner" for start, stop and delete), with a
//     member refused first by the admin guard;
//   - the usage rollup's window: 30 days by default, 7, 400 capped at 365
//     (a parser outside Q8's seven, X3a), and 0 or text refused;
//   - the transient runner routes, disabled with no RUNNER_POOL_KEY (their
//     other answers are another area's);
//   - the connector download, from the area's own dist directory
//     (CONNECTOR_DIST_DIR={{files}}/dist, fixture bytes written from Go
//     source): the default OS (Windows, the .exe single file), Linux (the
//     single file), macOS (only the legacy zip, the fallback), an unknown
//     OS and one in capitals, a Range (206) and one past the end (416),
//     If-Modified-Since after and before the fixtures' fixed mtime (304,
//     200), HEAD, and an OS whose file is gone (the Linux fixture removed
//     mid-area: 404).
//
// None of these routes publishes an event, so every write step records
// none. The default ./dist is never read: it is gitignored and depends on the
// machine that built it.
//
// Nondeterminism: ids, keys, pairing codes and minted times (the generic
// tokens; each key the area uses as a bearer is registered under its name);
// the pairing code's expiry (<time>, ten minutes ahead); Last-Modified and
// If-Modified-Since (<http-date>: the fixtures' mtime is fixed, 2026-01-01).
// Not pinned: "connector downloads are not configured on this deployment"
// (connectorDistDir "" cannot be reached: envOr falls back to ./dist when the
// variable is empty); the hosted runner's provisioning, start, stop and
// removal (the harness fixes HOSTED_RUNNERS=off, and they need Docker); a
// key going offline 30 s after its last use (time-bound); an expired pairing
// code (ten minutes); and the usage rollup's plan gate, which the tiers area
// pins (the flag is on under the alpha terms).
func TestTourS5cRunnerKeysConnector(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "runner_keys_connector",
		about: "Runners and the Agent Connector: workspace worker keys, a member's personal runner key and its online " +
			"window, connector pairing and the public code exchange, the fleet and queue, the hosted runner switched off, " +
			"the usage window, the transient runner routes switched off, and the connector download (single file, zip " +
			"fallback, Range, If-Modified-Since, HEAD, a missing OS).",
		run: runnerKeysConnectorTour,
		env: map[string]string{
			// The pairing answer's links carry the API's address URL-escaped;
			// without PUBLIC_URL it would be http://localhost:<port>.
			"PUBLIC_URL":         "https://api.tour.example",
			"CONNECTOR_DIST_DIR": "{{files}}/dist",
		},
		files: runnerKeysConnectorDist(t),
	})
}

// runnerKeysConnectorSize is the size of each connector fixture, and of the
// executable inside the macOS zip.
const runnerKeysConnectorSize = 48

// runnerKeysConnectorBinary is a fixture executable: an executable format's
// magic number, a label, and NUL bytes up to runnerKeysConnectorSize, so that
// it is recorded as bytes, never as text.
func runnerKeysConnectorBinary(magic, label string) []byte {
	b := make([]byte, runnerKeysConnectorSize)
	if copy(b, magic+label) != len(magic+label) {
		panic("runnerKeysConnectorBinary: the label does not fit")
	}
	return b
}

// runnerKeysConnectorDist is the area's dist directory: a single file for
// Windows and Linux, and for macOS only the legacy zip bundle, stored (not
// compressed) with a fixed modification time, so that its bytes are the
// same on every run and it stays far under the compressor's 1,400 bytes.
func runnerKeysConnectorDist(t *testing.T) map[string][]byte {
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	h := &zip.FileHeader{Name: "openv-connector", Method: zip.Store, Modified: tourFilesTime}
	h.SetMode(0o755)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(runnerKeysConnectorBinary("\xcf\xfa\xed\xfe\x07\x00\x00\x01", "tour connector: darwin")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{
		"dist/openv-connector-windows.exe": runnerKeysConnectorBinary("MZ\x90\x00\x03\x00\x00\x00", "tour connector: windows"),
		"dist/openv-connector-linux":       runnerKeysConnectorBinary("\x7fELF\x02\x01\x01\x00", "tour connector: linux"),
		"dist/openv-connector-darwin.zip":  zipped.Bytes(),
	}
}

// runnerKeysConnectorPoll is the request an Agent Connector polls with: a
// claim, which answers 204 while no run is queued. Any request with a key as
// the bearer stamps the key's last_used_at, which the online flags read.
func runnerKeysConnectorPoll(worker string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"worker_id":%q}`, worker))
}

// runnerKeysConnectorServes checks that a download answered the area's
// fixture byte for byte, with its length as Content-Length: the golden shows
// a zip by its entries and its length as <varies>, which would let a
// download that changed the archive's bytes through.
func runnerKeysConnectorServes(tr *tour, fixture string, res *tourResult) {
	tr.t.Helper()
	want := tr.area.files[fixture]
	if got := res.header.Get("Content-Length"); !bytes.Equal(res.body, want) || got != strconv.Itoa(len(want)) {
		tr.t.Errorf("%s is not %s byte for byte (%d bytes, Content-Length %q; the fixture has %d bytes): the "+
			"download must serve the file as it is on disk; if that changed on purpose, change the area's check, then "+
			"regenerate with:\n  %s", res.what(), fixture, len(res.body), got, len(want),
			strings.Join(tr.regenerate(), "\n  then "))
	}
}

func runnerKeysConnectorTour(tr *tour) {
	o, anon := tr.owner, tr.anon
	w := tr.sharedWorkspace("w", "Tour Shared")
	m := tr.register("member", "Tour Member", "a plain member of W: the personal runner key, pairing, the fleet and "+
		"the usage rollup, and the admin guards' refusals")
	tr.join(m, w, "member")
	inW := at("id", "{{w}}")
	const claim = "POST /api/v1/agent-runs/claim"

	// Worker keys: workspace-wide runner credentials, admin only.
	tr.step("W's worker keys: none yet, written null (Q14)", o, "GET /api/v1/orgs/{id}/worker-keys", inW)
	box := tr.step("mint a worker key: 201, the record and the key, shown once; the name is trimmed", o,
		"POST /api/v1/orgs/{id}/worker-keys", inW, jsonBody(`{"name":"  Tour build box  "}`))
	box.capture("box.key", "/key_record/id")
	boxKey := tr.bearerActor("box", box.value("/key"), fmt.Sprintf("the worker key step %d minted, as a bearer", box.step.n))
	spare := tr.step("mint one with no name: named \"worker\"", o, "POST /api/v1/orgs/{id}/worker-keys", inW,
		jsonBody(`{}`))
	spare.capture("spare.key", "/key_record/id")
	spareKey := tr.bearerActor("spare", spare.value("/key"), fmt.Sprintf("the worker key step %d minted, as a bearer, "+
		"once it is revoked", spare.step.n))
	look := tr.step("mint one named \"hosted-runner\", the name the hosted runner's own key has", o,
		"POST /api/v1/orgs/{id}/worker-keys", inW, jsonBody(`{"name":"hosted-runner"}`))
	look.capture("lookalike.key", "/key_record/id")
	look.capture("lookalike.token", "/key")
	tr.step("mint one with a body that does not decode", o, "POST /api/v1/orgs/{id}/worker-keys", inW, jsonBody(`{`))
	tr.step("mint one with a name over the column's 255 characters: the database's error, passed through (Q19)", o,
		"POST /api/v1/orgs/{id}/worker-keys", inW, jsonBody(`{"name":"`+strings.Repeat("n", 256)+`"}`))
	tr.step("a member mints one: the admin guard", m, "POST /api/v1/orgs/{id}/worker-keys", inW,
		jsonBody(`{"name":"not mine to mint"}`))
	tr.step("W's worker keys, newest first: workspace keys carry no user_id, and none has been used", o,
		"GET /api/v1/orgs/{id}/worker-keys", inW)
	tr.step("a member lists them: the admin guard", m, "GET /api/v1/orgs/{id}/worker-keys", inW)
	tr.step("the key as a bearer, polling for work as a connector does: 204, no run is queued", boxKey, claim,
		runnerKeysConnectorPoll("tour-box"))
	tr.step("the key on a workspace route: the handler's 401 (a key is no member), though the middleware has "+
		"accepted it and stamped its use", boxKey, "GET /api/v1/orgs/{id}/worker-keys", inW)
	tr.step("W's worker keys: the box's last_used_at is the step before's", o, "GET /api/v1/orgs/{id}/worker-keys", inW)
	tr.setup("a worker key of the owner's personal workspace", o, "POST /api/v1/orgs/{id}/worker-keys",
		at("id", "{{owner.workspace}}"), jsonBody(`{"name":"Tour personal box"}`), expect(201)).
		capture("elsewhere.key", "/key_record/id")
	tr.step("revoke the spare key: 204", o, "DELETE /api/v1/orgs/{id}/worker-keys/{keyId}",
		at("id", "{{w}}", "keyId", "{{spare.key}}"))
	tr.step("revoke it again: 204 all the same", o, "DELETE /api/v1/orgs/{id}/worker-keys/{keyId}",
		at("id", "{{w}}", "keyId", "{{spare.key}}"))
	tr.step("revoke a key no workspace has: 400 with the domain's error", o,
		"DELETE /api/v1/orgs/{id}/worker-keys/{keyId}", at("id", "{{w}}", "keyId", "{{phantom}}"))
	tr.step("revoke an id that is not a UUID: the database's error, passed through (Q19)", o,
		"DELETE /api/v1/orgs/{id}/worker-keys/{keyId}", at("id", "{{w}}", "keyId", "not-a-key"))
	tr.step("revoke, under W, a key of the owner's personal workspace: not W's, so not found", o,
		"DELETE /api/v1/orgs/{id}/worker-keys/{keyId}", at("id", "{{w}}", "keyId", "{{elsewhere.key}}"))
	tr.step("a member revokes one: the admin guard", m, "DELETE /api/v1/orgs/{id}/worker-keys/{keyId}",
		at("id", "{{w}}", "keyId", "{{box.key}}"))
	tr.step("the revoked key as a bearer: the middleware's 401", spareKey, claim, runnerKeysConnectorPoll("tour-spare"))
	tr.step("W's worker keys: the spare one revoked", o, "GET /api/v1/orgs/{id}/worker-keys", inW)

	// The member's personal runner key: one per member and workspace.
	tr.step("the member's runner key: none", m, "GET /api/v1/orgs/{id}/my-runner-key", inW)
	mint := tr.step("mint it: 201, named after the member, the key shown once", m, "POST /api/v1/orgs/{id}/my-runner-key", inW)
	mint.capture("runner.key", "/key_record/id")
	runner := tr.bearerActor("runner", mint.value("/key"), fmt.Sprintf("the member's runner key step %d minted, as "+
		"a bearer", mint.step.n))
	tr.step("the runner key before any use: offline", m, "GET /api/v1/orgs/{id}/my-runner-key", inW)
	tr.step("the runner key polls for work: 204, no run of the member's is queued", runner, claim,
		runnerKeysConnectorPoll("tour-runner"))
	tr.step("the runner key: online, since it was used within the last 30 s", m, "GET /api/v1/orgs/{id}/my-runner-key",
		inW, note("online is last_used_at within 30 s of the read; the use was the step before, milliseconds ago"))
	again := tr.step("mint it again: a new key, and the old one revoked", m, "POST /api/v1/orgs/{id}/my-runner-key", inW)
	again.capture("runner2.key", "/key_record/id")
	runner2 := tr.bearerActor("runner2", again.value("/key"), fmt.Sprintf("the member's runner key step %d minted "+
		"in its place, as a bearer", again.step.n))
	tr.step("the old runner key polls: the middleware's 401", runner, claim, runnerKeysConnectorPoll("tour-runner"))
	tr.step("the runner key: the new one, not yet used, so offline", m, "GET /api/v1/orgs/{id}/my-runner-key", inW)
	tr.step("the member's runner key in a workspace it is not in", m, "GET /api/v1/orgs/{id}/my-runner-key",
		at("id", "{{owner.workspace}}"))
	tr.step("mint one in a workspace it is not in", m, "POST /api/v1/orgs/{id}/my-runner-key",
		at("id", "{{owner.workspace}}"))

	// Connector pairing: a one-time code the local connector trades for the
	// member's runner key.
	pair := tr.step("a pairing code for the connector, with its deep links", m, "POST /api/v1/orgs/{id}/connector-pairing",
		inW, note("expires_at is <time>, since it is to come (its distance from the answer is noted below); the "+
			"links carry PUBLIC_URL URL-escaped, and encoding/json writes each & as \\u0026"))
	pair.capture("pairing.code", "/code")
	pair.noteExpiry("/expires_at")
	tr.step("a pairing code in a workspace the member is not in", m, "POST /api/v1/orgs/{id}/connector-pairing",
		at("id", "{{owner.workspace}}"))
	traded := tr.step("the connector trades the code: the member's new runner key, and where it works", anon,
		"POST /api/v1/public/connector/pair", jsonBody(`{"code":"{{pairing.code}}"}`))
	connector := tr.bearerActor("connector", traded.value("/worker_key"), fmt.Sprintf("the runner key the connector "+
		"got for the code at step %d, as a bearer", traded.step.n))
	tr.step("the same code again: spent", anon, "POST /api/v1/public/connector/pair",
		jsonBody(`{"code":"{{pairing.code}}"}`))
	tr.remember("unknown.code", strings.Repeat("0123456789abcdef", 4))
	tr.step("a code no pairing has: the same answer", anon, "POST /api/v1/public/connector/pair",
		jsonBody(`{"code":"{{unknown.code}}"}`))
	tr.step("an empty code", anon, "POST /api/v1/public/connector/pair", jsonBody(`{"code":""}`))
	tr.step("a body that does not decode: the same answer as an empty code", anon, "POST /api/v1/public/connector/pair",
		jsonBody(`{`))
	tr.step("the runner key the pairing replaced polls: the middleware's 401", runner2, claim,
		runnerKeysConnectorPoll("tour-runner"))
	tr.step("the member's runner key: the connector's, named \"personal-runner\" (the exchange passes no user "+
		"name), offline", m, "GET /api/v1/orgs/{id}/my-runner-key", inW).capture("connector.key", "/key_record/id")
	tr.step("the connector polls for work: 204", connector, claim, runnerKeysConnectorPoll("tour-connector"))
	tr.step("the member's runner key: online", m, "GET /api/v1/orgs/{id}/my-runner-key", inW)

	// The fleet: every key of the workspace, the revoked ones too.
	tr.step("the box key polls again", boxKey, claim, runnerKeysConnectorPoll("tour-box"))
	tr.step("W's fleet: every key, newest first, with who holds it, whether it is online (used within 30 s), "+
		"revoked or taken for the hosted runner (by its name), and the queue", m, "GET /api/v1/orgs/{id}/worker-status",
		inW, note("the connector's key and the box key were used in the steps just before, well within the 30 s "+
			"window; the member's first runner key was used too, but is revoked"))
	tr.step("the fleet of a workspace the member is not in", m, "GET /api/v1/orgs/{id}/worker-status",
		at("id", "{{owner.workspace}}"))

	// The personal runner key revoked.
	tr.step("revoke the member's runner key: 204", m, "DELETE /api/v1/orgs/{id}/my-runner-key", inW)
	tr.step("revoke it again, with none left: 204 all the same", m, "DELETE /api/v1/orgs/{id}/my-runner-key", inW)
	tr.step("the member's runner key: none", m, "GET /api/v1/orgs/{id}/my-runner-key", inW)
	tr.step("the connector polls with the revoked key: the middleware's 401", connector, claim,
		runnerKeysConnectorPoll("tour-connector"))

	// The hosted runner, with HOSTED_RUNNERS=off (the harness's).
	tr.step("W's hosted runner: none, and hosted runners off", o, "GET /api/v1/orgs/{id}/hosted-runner", inW)
	tr.step("provision one, with a body that does not decode: refused for the deployment before the plan flag "+
		"and the body", o, "POST /api/v1/orgs/{id}/hosted-runner", inW, jsonBody(`{`))
	tr.step("start it: there is none", o, "POST /api/v1/orgs/{id}/hosted-runner/start", inW)
	tr.step("stop it: there is none", o, "POST /api/v1/orgs/{id}/hosted-runner/stop", inW)
	tr.step("remove it: there is none", o, "DELETE /api/v1/orgs/{id}/hosted-runner", inW)
	tr.step("a member reads it: the admin guard", m, "GET /api/v1/orgs/{id}/hosted-runner", inW)
	tr.step("a member provisions one: the admin guard, before the deployment's refusal", m,
		"POST /api/v1/orgs/{id}/hosted-runner", inW, jsonBody(`{}`))
	tr.step("a member starts it: the admin guard, before the lookup", m, "POST /api/v1/orgs/{id}/hosted-runner/start", inW)

	// The usage rollup's window (?days=).
	tr.step("W's usage: no runs, over the default 30 days", m, "GET /api/v1/orgs/{id}/usage", inW)
	tr.step("over 7 days", m, "GET /api/v1/orgs/{id}/usage", inW, query("days=7"))
	tr.step("over 400 days: capped at 365, which the answer says", m, "GET /api/v1/orgs/{id}/usage", inW,
		query("days=400"))
	tr.step("over 0 days", m, "GET /api/v1/orgs/{id}/usage", inW, query("days=0"))
	tr.step("over days that are not a number", m, "GET /api/v1/orgs/{id}/usage", inW, query("days=abc"))
	tr.step("the usage of a workspace the member is not in", m, "GET /api/v1/orgs/{id}/usage",
		at("id", "{{owner.workspace}}"))

	// The transient runner routes, with no RUNNER_POOL_KEY: disabled.
	tr.step("the member's cloud runner: transient runners are off", m, "GET /api/v1/orgs/{id}/runner-session", inW)
	tr.step("W's runner pool, as its admin: off", o, "GET /api/v1/orgs/{id}/runner-pool", inW)
	tr.step("W's runner pool, as a member: the admin guard", m, "GET /api/v1/orgs/{id}/runner-pool", inW)
	tr.step("lease a cloud runner: refused, transient runners are off", m, "POST /api/v1/orgs/{id}/runner-session", inW)
	tr.step("extend the lease: the same", m, "POST /api/v1/orgs/{id}/runner-session/extend", inW)
	tr.step("end the lease: the same", m, "DELETE /api/v1/orgs/{id}/runner-session", inW)

	// The connector download, from the area's dist directory.
	const download = "GET /api/v1/public/connector/download"
	tr.keep("Thu, 01 Jan 2026 00:00:00 GMT", "the fixtures' mtime (tourFilesTime), which a download's Last-Modified "+
		"carries")
	runnerKeysConnectorServes(tr, "dist/openv-connector-windows.exe", tr.step("download the connector with no os: "+
		"Windows, the .exe single file, as an attachment", anon, download))
	runnerKeysConnectorServes(tr, "dist/openv-connector-windows.exe", tr.step("for Windows, named: the same", anon,
		download, query("os=windows")))
	runnerKeysConnectorServes(tr, "dist/openv-connector-linux", tr.step("for Linux: its single file", anon, download,
		query("os=linux")))
	runnerKeysConnectorServes(tr, "dist/openv-connector-darwin.zip", tr.step("for macOS: no single file, so the "+
		"legacy zip bundle", anon, download, query("os=darwin"), note("a zip is shown by its entries, and its length "+
		"as <varies> (the framework's band for a compressed format); the area also checks that the answer is the "+
		"fixture's bytes and Content-Length, exactly")))
	tr.step("for an OS the connector is not built for", anon, download, query("os=plan9"))
	tr.step("for Linux spelled with a capital: an unknown OS too", anon, download, query("os=Linux"))
	tr.step("the first four bytes (Range): 206 with Content-Range", anon, download, withHeader("Range", "bytes=0-3"))
	tr.step("a range past the end: 416, with the file's size in Content-Range", anon, download,
		withHeader("Range", "bytes=100-200"))
	tr.step("If-Modified-Since in 2100, after the file's mtime: 304, no body", anon, download,
		withHeader("If-Modified-Since", "Fri, 01 Jan 2100 00:00:00 GMT"),
		note("the fixtures' mtime is 2026-01-01T00:00:00Z (tourFilesTime); the tour sent 2100-01-01, written <http-date>"))
	tr.step("If-Modified-Since in 2024, before it: 200, the whole file", anon, download,
		withHeader("If-Modified-Since", "Mon, 01 Jan 2024 00:00:00 GMT"),
		note("the tour sent 2024-01-01, written <http-date>"))
	tr.step("HEAD: the headers and Content-Length, no body", anon, "HEAD /api/v1/public/connector/download")
	if err := os.Remove(filepath.Join(tr.files, "dist", "openv-connector-linux")); err != nil {
		tr.t.Fatalf("remove the Linux fixture: %v", err)
	}
	tr.step("for Linux once its file is gone (and it has no zip either): not available", anon, download,
		query("os=linux"), note("the area removed the Linux fixture from its dist directory before this step"))
}
