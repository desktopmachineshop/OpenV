//go:build unix

package main

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTourS5aShareLinksPublic is the S5a tour's share-links area (refactor
// plan §6.4 S5a; invariants I4, I8; quirks Q14, Q19; OpenV REQ-149, REQ-150,
// REQ-151). Its golden is testdata/tour/s5a/share_links_public.json.
//
// The owner, an ordinary account, shares a project that has a description,
// typed artifacts under one heading, a link and a baseline: the links listed
// before any exists (Q14), minted (public, reviewer, already expired) and
// refused (malformed, an unknown role, no access), listed newest first with
// no token, opened by an anonymous client as JSON, as the unfurler's HTML
// page and as the 1200×630 preview card, taken up by the reviewer account
// and by the owner (who keeps the stronger role), revoked, and then refused
// everywhere with the one 404 every unusable link gets. The token lookups
// and accepts spend one per-address bucket (invitePreviewLimiter, shared with
// invitation previews); the area spends what is left of it and pins the 429
// the share routes answer (no Retry-After). Last, the open-source showcase,
// before and after the admin, the platform admin, puts the owner's
// workspace on the open_source plan: once it is on the plan, the showcase
// lists the project and opens its newest baseline as JSON, as the unfurl
// page and as the preview card.
func TestTourS5aShareLinksPublic(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "share_links_public",
		about: "Share links (list, create, revoke), the public views a link opens (JSON, the unfurl page, the " +
			"preview card), taking a reviewer link up, the 429 once the address's lookups are spent, and the " +
			"open-source showcase before and after the open_source plan.",
		run: shareLinksPublicTour,
	})
}

// shareLinksPublicExpires is a reviewer link's expiry as the tour sends it,
// with an offset; shareLinksPublicExpiresRead is what the database gives
// back (a TIMESTAMP column drops the offset and keeps the wall time).
const (
	shareLinksPublicExpires     = "2099-01-02T03:04:05+02:00"
	shareLinksPublicExpiresRead = "2099-01-02T03:04:05Z"
	shareLinksPublicExpired     = "2001-02-03T04:05:06Z"
)

// shareLinksPublicUnknown is a well-formed token no link has.
var shareLinksPublicUnknown = strings.Repeat("5eed", 16)

func shareLinksPublicTour(tr *tour) {
	owner, anon := tr.owner, tr.anon
	reviewer := tr.register("reviewer", "Tour Reviewer",
		"an ordinary account in a workspace of its own; takes up the reviewer link")
	tr.keep(shareLinksPublicExpires, "a reviewer link's expiry as the tour sends it, with an offset")
	tr.keep(shareLinksPublicExpiresRead, "the same expiry read back: the TIMESTAMP column drops the offset and keeps the wall time")
	tr.keep(shareLinksPublicExpired, "the expiry of a link minted already expired")
	tr.keep(shareLinksPublicUnknown, "a well-formed share token that no link has")

	// The shared project: a description long enough to be cut on the page
	// (180 bytes) and on the card (120), characters that JSON and HTML
	// escape, and one artifact of each counted type under a single heading
	// (so the snapshot's order is fixed: artifact lists order by parent_id,
	// a random UUID, first).
	desc := `The tour's shared <spec> & its "quoted" words: what an assessor reads before a review, ` +
		`with enough text that the unfurl page cuts it at a word before 180 bytes and the preview card cuts it ` +
		`sooner, before 120, each with an ellipsis.`
	tr.setup("the shared project", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour <spec> & \"Co's\"","description":"`+strings.ReplaceAll(desc, `"`, `\"`)+`"}`)).
		capture("shared_project", "/id")
	tr.setup("a heading", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{shared_project}}",`+
		`"type":"heading","title":"Scope","body":"What the share shows."}`)).capture("scope_heading", "/id")
	for _, a := range []struct{ name, typ, title, body string }{
		{"need", "user-need", "Know the state", "The assessor needs to know the state of the work."},
		{"answer_req", "requirement", "Answer in time", "The system shall answer within 2 s."},
		{"audit_req", "requirement", "Keep an audit trail", "The system shall record who changed what."},
		{"design", "design-item", "Log every write", "Each write appends to the audit log."},
		{"timing_test", "test-case", "Time the answer", "Measure the answer time."},
		{"persona", "persona", "Assessor", "Reads the specification, never edits it."},
	} {
		tr.setup("a "+a.typ, owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{shared_project}}",`+
			`"parent_id":"{{scope_heading}}","type":"`+a.typ+`","title":"`+a.title+`","body":"`+a.body+`"}`)).
			capture(a.name, "/id")
	}
	tr.setup("the test case verifies the timing requirement", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{timing_test}}","to_id":"{{answer_req}}","type":"verifies"}`)).capture("shared_link", "/id")
	tr.setup("a baseline", owner, "POST /api/v1/projects/{id}/baselines", at("id", "{{shared_project}}"),
		jsonBody(`{"name":"Tour baseline 1"}`)).capture("shared_baseline", "/id")

	// Minting and listing (owner).
	tr.step("the project's links before any exists", owner, "GET /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"))
	tr.step("mint a link with a malformed body", owner, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"), jsonBody(`{"role":`))
	tr.step("mint a link with an expiry that is not a time", owner, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"), jsonBody(`{"role":"public","expires_at":"tomorrow"}`))
	tr.step("mint a link with a role no link has", owner, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"), jsonBody(`{"role":"editor","label":"Nope"}`))
	public := tr.step("mint a public link: the token and the share URL are in this answer only", owner,
		"POST /api/v1/projects/{id}/share-links", at("id", "{{shared_project}}"),
		jsonBody(`{"role":"public","label":"  Customer <copy> & co  "}`),
		note("the label is trimmed; the url is FRONTEND_URL (the default, http://localhost:3000) + /share/ + the token"))
	shareLinksPublicMinted(public, "public")
	review := tr.step("mint a reviewer link with an expiry that carries an offset", owner,
		"POST /api/v1/projects/{id}/share-links", at("id", "{{shared_project}}"),
		jsonBody(`{"role":"reviewer","expires_at":"`+shareLinksPublicExpires+`"}`),
		note("the answer echoes the expiry as sent, offset and all; the list below reads the stored wall time back as UTC"))
	shareLinksPublicMinted(review, "reviewer")
	expired := tr.step("mint a public link that has already expired", owner, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"), jsonBody(`{"role":"public","label":"Expired","expires_at":"`+shareLinksPublicExpired+`"}`),
		note("an expiry in the past is accepted; the link never opens anything"))
	shareLinksPublicMinted(expired, "expired")
	tr.step("the links, newest first: no token, no url", owner, "GET /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"))
	tr.step("the links of a project that does not exist", owner, "GET /api/v1/projects/{id}/share-links",
		at("id", "{{phantom}}"), note("the owner guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("mint a link for a project that does not exist", owner, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{phantom}}"), jsonBody(`{"role":"public"}`))
	tr.step("the links, as an account with no role on the project", reviewer, "GET /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"))
	tr.step("mint a link, as an account with no role on the project", reviewer,
		"POST /api/v1/projects/{id}/share-links", at("id", "{{shared_project}}"), jsonBody(`{"role":"reviewer"}`))
	tr.step("read the project, as an account with no role on it", reviewer, "GET /api/v1/projects/{id}",
		at("id", "{{shared_project}}"), note("what taking the reviewer link up changes; see the same read below"))
	tr.step("the links, with no session", anon, "GET /api/v1/projects/{id}/share-links", at("id", "{{shared_project}}"),
		note("the auth middleware answers before routing"))

	// What a link opens, with no account. Every token lookup from here on
	// spends the address's bucket (burst 60); a GET is sent twice.
	tr.step("open the public link: the live project as a snapshot, counted by type", anon,
		"GET /api/v1/public/share/{token}", at("token", "{{public_token}}"),
		note("counts leave headings out and include every other type; the snapshot is PrepareExport, exported_at "+
			"taken at the request, so the answer is never cached (Cache-Control: no-store)"))
	tr.step("open the reviewer link: the project's name only", anon, "GET /api/v1/public/share/{token}",
		at("token", "{{reviewer_token}}"), note("no snapshot and empty counts: the reviewer signs in and uses the app"))
	tr.step("open the expired link", anon, "GET /api/v1/public/share/{token}", at("token", "{{expired_token}}"))
	tr.step("open a token no link has", anon, "GET /api/v1/public/share/{token}", at("token", shareLinksPublicUnknown))
	tr.step("the public link's unfurl page: Open Graph and Twitter tags, then a refresh into the app", anon,
		"GET /api/v1/public/share/{token}/page", at("token", "{{public_token}}"),
		note("og:image is PUBLIC_URL (the default, http://localhost:<port>) + the preview's path; the refresh goes "+
			"to FRONTEND_URL + /s/ + the token; the description is cut at a word before 180 bytes; the text is "+
			"HTML-escaped as &lt; &gt; &amp; &#34; &#39;"))
	tr.step("the reviewer link's unfurl page", anon, "GET /api/v1/public/share/{token}/page",
		at("token", "{{reviewer_token}}"))
	tr.step("the unfurl page of the expired link", anon, "GET /api/v1/public/share/{token}/page",
		at("token", "{{expired_token}}"))
	tr.step("the public link's preview card", anon, "GET /api/v1/public/share/{token}/preview.png",
		at("token", "{{public_token}}"),
		note("1200×630, drawn from the project's name, workspace, the counts line and the description cut before "+
			"120 bytes; no time is drawn; recorded as its size and a hash of its decoded pixels"))
	tr.step("the reviewer link's preview card", anon, "GET /api/v1/public/share/{token}/preview.png",
		at("token", "{{reviewer_token}}"))
	tr.step("the preview card of a token no link has", anon, "GET /api/v1/public/share/{token}/preview.png",
		at("token", shareLinksPublicUnknown))

	// Taking a reviewer link up (session cookie only; /api/v1/auth/ is open
	// in the middleware, so the 401 is the handler's own).
	tr.step("take the reviewer link up with no session", anon, "POST /api/v1/auth/share/accept",
		jsonBody(`{"token":"{{reviewer_token}}"}`))
	tr.step("take a link up with a malformed body", reviewer, "POST /api/v1/auth/share/accept", jsonBody(`{"token":`),
		note("the body is read before the bucket is spent"))
	tr.step("take the public link up", reviewer, "POST /api/v1/auth/share/accept", jsonBody(`{"token":"{{public_token}}"}`))
	tr.step("take up a token no link has", reviewer, "POST /api/v1/auth/share/accept",
		jsonBody(`{"token":"`+shareLinksPublicUnknown+`"}`))
	tr.step("take up the expired link", reviewer, "POST /api/v1/auth/share/accept", jsonBody(`{"token":"{{expired_token}}"}`))
	tr.step("take the reviewer link up: the account becomes a reviewer of the project", reviewer,
		"POST /api/v1/auth/share/accept", jsonBody(`{"token":"{{reviewer_token}}"}`),
		note("a map, so encoding/json sorts its keys"))
	tr.step("take it up again: nothing changes", reviewer, "POST /api/v1/auth/share/accept",
		jsonBody(`{"token":"{{reviewer_token}}"}`))
	tr.step("the owner takes the reviewer link up and keeps the stronger role", owner, "POST /api/v1/auth/share/accept",
		jsonBody(`{"token":"{{reviewer_token}}"}`))
	tr.step("read the project, as its reviewer now", reviewer, "GET /api/v1/projects/{id}", at("id", "{{shared_project}}"))
	tr.step("the links, as the project's reviewer", reviewer, "GET /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"), note("listing needs the owner role"))

	// Revoking.
	tr.step("revoke a link, as the project's reviewer", reviewer, "DELETE /api/v1/share-links/{id}",
		at("id", "{{public_link}}"))
	tr.step("revoke a link that does not exist", owner, "DELETE /api/v1/share-links/{id}", at("id", "{{phantom}}"))
	tr.step("revoke a link by an id that is not a UUID", owner, "DELETE /api/v1/share-links/{id}", at("id", "not-a-uuid"),
		note("the database refuses the id; the handler answers any lookup error with the same 404"))
	tr.step("revoke the public link", owner, "DELETE /api/v1/share-links/{id}", at("id", "{{public_link}}"))
	tr.step("revoke it again: still 204", owner, "DELETE /api/v1/share-links/{id}", at("id", "{{public_link}}"),
		note("the first revocation's time is kept"))
	tr.step("revoke the reviewer link", owner, "DELETE /api/v1/share-links/{id}", at("id", "{{reviewer_link}}"))
	tr.step("the links once two are revoked: revoked_at appears", owner, "GET /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"))
	tr.step("open the revoked public link", anon, "GET /api/v1/public/share/{token}", at("token", "{{public_token}}"))
	tr.step("the revoked link's unfurl page", anon, "GET /api/v1/public/share/{token}/page",
		at("token", "{{public_token}}"))
	tr.step("the revoked link's preview card", anon, "GET /api/v1/public/share/{token}/preview.png",
		at("token", "{{public_token}}"))
	tr.step("take up the revoked reviewer link", reviewer, "POST /api/v1/auth/share/accept",
		jsonBody(`{"token":"{{reviewer_token}}"}`))

	// The bucket spent: a fresh link, so that only the bucket refuses.
	fresh := tr.setup("a fresh public link", owner, "POST /api/v1/projects/{id}/share-links",
		at("id", "{{shared_project}}"), jsonBody(`{"role":"public","label":"Fresh"}`))
	shareLinksPublicMinted(fresh, "fresh")
	shareLinksPublicDrain(tr)
	spent := "this address's token lookups are spent (the tour drained the bucket through invitation previews, " +
		"which share it, until one answered 429 with Retry-After of 3 s or more)"
	tr.step("open a live link once the bucket is spent", anon, "GET /api/v1/public/share/{token}",
		at("token", "{{fresh_token}}"), note(spent+"; the share routes answer 429 with no Retry-After"))
	tr.step("its unfurl page", anon, "GET /api/v1/public/share/{token}/page", at("token", "{{fresh_token}}"))
	tr.step("its preview card", anon, "GET /api/v1/public/share/{token}/preview.png", at("token", "{{fresh_token}}"))
	tr.step("take a link up once the bucket is spent", reviewer, "POST /api/v1/auth/share/accept",
		jsonBody(`{"token":"{{fresh_token}}"}`))
	tr.step("take a link up with a malformed body once the bucket is spent", reviewer, "POST /api/v1/auth/share/accept",
		jsonBody(`[`), note("the body is read before the bucket: 400, not 429"))

	// The open-source showcase (REQ-151): not rate-limited.
	tr.step("the open-source projects: none", anon, "GET /api/v1/public/open-source/projects",
		note("the showcase does not spend the share bucket"))
	tr.step("a project of a workspace not on the open-source plan", anon, "GET /api/v1/public/open-source/projects/{id}",
		at("id", "{{shared_project}}"))
	plan := tr.setup("the platform admin puts the owner's workspace on the open_source plan", tr.admin,
		"PUT /api/v1/orgs/{id}/plan", at("id", "{{owner.workspace}}"), jsonBody(`{"plan":"open_source"}`))
	if got := plan.value("/plan"); got != "open_source" {
		tr.t.Fatalf("the owner's workspace is on plan %q after the admin set open_source", got)
	}
	openSource := "the workspace is on the open_source plan and the project has a baseline, so the showcase " +
		"publishes the project as of its newest baseline, which it loads by id: the baseline list carries no snapshot"
	tr.step("the open-source projects once the workspace is on the plan", anon,
		"GET /api/v1/public/open-source/projects", note(openSource))
	tr.step("the project on the open-source plan", anon, "GET /api/v1/public/open-source/projects/{id}",
		at("id", "{{shared_project}}"), note(openSource))
	tr.step("its unfurl page", anon, "GET /api/v1/public/open-source/projects/{id}/page", at("id", "{{shared_project}}"))
	tr.step("its preview card", anon, "GET /api/v1/public/open-source/projects/{id}/preview.png",
		at("id", "{{shared_project}}"))
	tr.step("a project that does not exist", anon, "GET /api/v1/public/open-source/projects/{id}", at("id", "{{phantom}}"))
	tr.step("the owner's links at the end", owner, "GET /api/v1/projects/{id}/share-links", at("id", "{{shared_project}}"))
}

var shareLinksPublicTokenRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// shareLinksPublicMinted registers a minted link's id and token as
// <name_link> and <name_token>, after checking that the token is
// users.NewToken's 64 lowercase hex digits: the golden writes it as a name,
// so its shape is checked here.
func shareLinksPublicMinted(r *tourResult, name string) {
	r.tr.t.Helper()
	r.capture(name+"_link", "/id")
	if token := r.capture(name+"_token", "/token"); !shareLinksPublicTokenRE.MatchString(token) {
		r.tr.t.Fatalf("a share token is 64 lowercase hex digits (users.NewToken), not %q", token)
	}
}

// shareLinksPublicDrain spends what is left of this address's token-lookup
// bucket (invitePreviewLimiter: burst 60, one token back every 15 s). It
// sends invitation previews, which draw on the same bucket and, unlike the
// share routes, say in Retry-After how long the next token is away; once
// one answers 429 with Retry-After of 3 s or more, the steps that follow
// have two seconds before a token comes back, where they need milliseconds.
// Otherwise it waits for that token and spends it too. The requests are
// declared under their route for the /metrics check and recorded nowhere.
func shareLinksPublicDrain(tr *tour) {
	tr.t.Helper()
	for round := 0; round < 5; round++ {
		var ex *tourResult
		for i := 0; i < 200; i++ {
			ex = tr.probe(tr.anon, "POST /api/v1/auth/invitations/preview", jsonBody(`{"token":"tour-drain"}`))
			if ex.status == http.StatusTooManyRequests {
				break
			}
			if ex.status != http.StatusNotFound {
				tr.t.Fatalf("drain the share bucket: an invitation preview answered %d: %s", ex.status, ex.body)
			}
		}
		if ex.status != http.StatusTooManyRequests {
			tr.t.Fatalf("drain the share bucket: 200 invitation previews and none answered 429")
		}
		secs, err := strconv.Atoi(ex.header.Get("Retry-After"))
		if err != nil {
			tr.t.Fatalf("drain the share bucket: a 429 with Retry-After %q", ex.header.Get("Retry-After"))
		}
		if secs >= 3 {
			return
		}
		time.Sleep(time.Duration(secs)*time.Second + 200*time.Millisecond)
	}
	tr.t.Fatal("drain the share bucket: five rounds and the next token was always under 3 s away")
}
