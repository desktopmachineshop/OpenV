//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestTourS5bSharedProducts is the S5b tour's shared products area (refactor
// plan §6.4 S5b; invariants I3 (guard, decode and service order), I4, I5;
// quirk Q8's shared-products limit policy; OpenV REQ-13, REQ-23, REQ-143):
// the community pool of demo products every workspace reads, the one
// cross-tenant, user-writable surface (internal/api/shared_product_handlers.go,
// internal/domain/sharedproducts). Its golden is
// testdata/tour/s5b/shared_products.json.
//
// At boot the server seeds the 15-entry starter pool
// (internal/seeds/product_concepts.go): random ids, created_at at boot, no
// author. The pool's text is served content, so editing that file changes
// this golden, as a behaviour change should. The owner, a registered member
// and a registered reporter, each in a personal workspace of their own, are
// the three distinct people a report needs to hide an entry (ReportsToHide
// is 3) and the second voter; a moderator, promoted to platform admin by
// setup (the admin account is kept to setup), deletes; a workspace runner
// key of the owner's workspace is a bearer with no person behind it, who may
// read the pool but not publish, vote, report or delete.
//
// The area walks: the seeded pool read with each sort and the limit policy
// (Q8: default 200, capped at 500, a value that is not a number read as 0);
// the publish refusals in the order the handler and Sanitize meet them, and
// what Sanitize scrubs (markup and invisible runes dropped, line breaks
// flattened, whitespace collapsed, length counted in runes); votes, which
// are per person and idempotent; reports, where one person reporting twice
// counts once and the third distinct reporter hides the entry from the list
// and from votes; the platform admin's delete; the per-workspace daily cap of
// 20, which counts hidden rows but not deleted ones and is checked after
// Sanitize and before the duplicate check; and, with the pool grown to 201
// visible entries (setup publishes from workspaces the owner creates), the
// default cut of 200, the cap at 500 answering all 201, and the pool ceiling,
// which the area's environment sets to 201 (OPENV_SHARED_PRODUCT_POOL_LIMIT;
// the default 5,000 is out of reach). Every answer but a 204 carries
// Content-Type: application/json, which these handlers set themselves (no
// Q1). A malformed id answers 404, as an id no entry has (fixed under R7: it
// reached the database as a uuid syntax error and answered 500). No route
// publishes an event.
// The person gates come before the decode and the lookup, which the runner
// key's steps show with a malformed body and an id no entry has. Not shown:
// the publish route's workspace-member gate (org:member in route_guards.txt)
// cannot refuse a signed-in person, since the auth middleware only resolves a
// workspace the account belongs to, and its plan read-only branch needs the
// tiers (S5e's over-plan pass); the cap of 500 itself would take 501 visible
// entries; and a vote older than the 7-day window cannot be cast through the
// API, so top_week lists what top lists.
//
// Nondeterminism: only the seeded and published rows' ids and created_at.
// Every row is written by a request of its own (the seeds one at a time at
// boot), so no two share a created_at and the lists, ordered by created_at
// (after the votes for the top sorts), never tie.
func TestTourS5bSharedProducts(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "shared_products",
		about: "The community pool of demo products shared by every workspace: list (sort and limit), publish " +
			"(sanitising, refusals, the daily cap per workspace and the pool ceiling), vote and unvote, report " +
			"(three distinct people hide an entry) and the platform admin's delete, as people, a runner key and " +
			"no session.",
		run: sharedProductsTour,
		env: map[string]string{sharedProductsPoolLimitEnv: strconv.Itoa(sharedProductsPoolLimit)},
	})
}

// sharedProductsPoolLimitEnv is the pool ceiling the area sets: one entry
// more than the list's default limit, so that the pool can both show the
// default cut and be full.
const (
	sharedProductsPoolLimitEnv = "OPENV_SHARED_PRODUCT_POOL_LIMIT"
	sharedProductsPoolLimit    = 201
	sharedProductsDailyLimit   = 20 // sharedproducts.DefaultDailyOrgLimit, which the area leaves as it is
)

// sharedProductsJSON writes a publish body with the six fields in the order
// the wizard sends them, each a JSON string written as encoding/json does
// with HTML escaping off: a tab, a line break or a control rune goes as its
// escape, and everything else, invisible runes included, as it is.
func sharedProductsJSON(category, name, description, vision, problem, targetUsers string) string {
	fields := [][2]string{{"category", category}, {"name", name}, {"description", description},
		{"vision", vision}, {"problem", problem}, {"target_users", targetUsers}}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(f[1])
		parts = append(parts, `"`+f[0]+`":`+strings.TrimSuffix(b.String(), "\n"))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// sharedProductsPlain is a publish body that passes Sanitize unchanged,
// under a name of the caller's.
func sharedProductsPlain(name string) string {
	return sharedProductsJSON("tour", name, "A product the tour publishes.", "Fill the pool.", "The pool needs rows.",
		"the tour")
}

func sharedProductsTour(tr *tour) {
	o := tr.owner
	member := tr.register("member", "Tour Member", "an ordinary account in its personal workspace: the second "+
		"voter and a reporter; its workspace publishes too")
	reporter := tr.register("reporter", "Tour Reporter", "an ordinary account in its personal workspace: the third "+
		"distinct reporter")
	moderator := tr.register("moderator", "Tour Moderator", "an account promoted to platform admin by setup: "+
		"deletes entries")
	tr.setup("promote the moderator to platform admin", tr.admin, "PUT /api/v1/admin/users/{id}/admin",
		at("id", "{{moderator}}"), jsonBody(`{"is_admin":true}`))
	key := tr.setup("a workspace runner key", o, "POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"),
		jsonBody(`{"name":"Tour runner"}`), expect(201)).value("/key")
	worker := tr.bearerActor("worker", key, "a workspace runner key of the owner's workspace: no person behind it, "+
		"so it may read the pool but not publish, vote, report or delete")

	// The seeded pool: the sorts, and the limit policy (Q8).
	tr.step("the pool with no session: every route sits behind authentication", tr.anon,
		"GET /api/v1/shared-products")
	seeded := tr.step("the seeded pool: 15 entries, newest first, no votes", o, "GET /api/v1/shared-products",
		note("the starter pool is seeded one row at a time at boot, in the order of product_concepts.go, so the "+
			"first entry there is the oldest and comes last"))
	seeded.captureWhere("yesterdaily", "", "name", "Yesterdaily", "id")
	seeded.captureWhere("crustodian", "", "name", "Crustodian", "id")
	tr.step("limit=1: one row", o, "GET /api/v1/shared-products", query("limit=1"))
	tr.step("limit=0: the default limit (200), so all 15", o, "GET /api/v1/shared-products", query("limit=0"))
	tr.step("limit=-3: the default limit too", o, "GET /api/v1/shared-products", query("limit=-3"))
	tr.step("limit=abc: not a number, read as 0, so the default limit; not refused", o, "GET /api/v1/shared-products",
		query("limit=abc"))
	tr.step("limit=501: capped at 500, so all 15", o, "GET /api/v1/shared-products", query("limit=501"),
		note("the default and the cap themselves are pinned once the pool holds 201 entries, at the end of the area"))
	tr.step("sort=top before any vote: only voted entries, so an empty list, [] not null", o,
		"GET /api/v1/shared-products", query("sort=top"))
	tr.step("sort=top_week before any vote: []", o, "GET /api/v1/shared-products", query("sort=top_week"))
	tr.step("sort=recent&limit=2: the default order, named", o, "GET /api/v1/shared-products",
		query("sort=recent&limit=2"))
	tr.step("sort=best: refused rather than read as recent", o, "GET /api/v1/shared-products", query("sort=best"))
	tr.step("sort=Top: the names are matched exactly", o, "GET /api/v1/shared-products", query("sort=Top"))

	// Publishing: the person gate, the decode, then Sanitize, field by field
	// in the wizard's order (each field empty, too long, a link, the
	// marker), then the dedupe key, then (once the pool is fuller) the
	// daily cap and the ceiling, then the duplicate check.
	tr.step("the runner key publishes, with a malformed body: refused, no person answers for the row", worker,
		"POST /api/v1/shared-products", jsonBody(`{"name":`),
		note("the person gate comes before the decode, so the body is never read"))
	tr.step("publish a malformed body", o, "POST /api/v1/shared-products", jsonBody(`{"name":`))
	tr.step("publish with no target_users", o, "POST /api/v1/shared-products",
		jsonBody(`{"category":"tour","name":"Half Product","description":"d","vision":"v","problem":"p"}`))
	tr.step("publish a category of markup and spaces only: stripped to nothing, so missing", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("<> `` \t", "Bare Product", "d", "v", "p", "t")))
	tr.step("publish a name with no letter or digit: its dedupe key is empty, so missing", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("tour", "!!! ???", "d", "v", "p", "t")))
	tr.step("publish a name of 61 runes: too long", o, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON("tour", strings.Repeat("Größenwahn ", 5)+"Größen", "d", "v", "p", "t")))
	tr.step("publish a description with a www. host", o, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON("tour", "Linked Product", "Order at www.example.com today", "v", "p", "t")))
	tr.step("publish target users naming a bare domain", o, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON("tour", "Domain Product", "d", "v", "p", "readers of toaster.dev")))
	tr.step("publish a problem carrying the suggestion marker, in any case", o, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON("tour", "Marked Product", "d", "v", "An OpenV-Suggestion block", "t")))
	tr.step("publish a link in the category and no name: the category's link is found first", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("see https://x", "", "d", "v", "p", "t")),
		note("Sanitize checks each field in full, in the wizard's order, before the next"))
	tr.step("publish a case, space and punctuation variant of the seeded Yesterdaily: a duplicate", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("tour", "  yester-DAILY! ", "d", "v", "p", "t")),
		note("the pool dedupes on the name's letters and digits, lower-cased"))
	tr.step("publish P1: Sanitize strips < > and backticks, flattens tabs and line breaks, drops invisible and "+
		"control runes, collapses spaces; & is escaped in the answer", o, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON(
			"Kitchen\tgadgets",
			"  Toast<er>  `Pro`​ ",
			"Browns bread\nto order;\r\nR&D says \"it's fine\"",
			"Every kitchen‮ toasts on cue\u0007",
			"Toast   is\t\tnever   ready",
			"people with bread")),
		note("a no-break space is not a printable rune to Sanitize, so it is dropped, not turned into a space"),
		note("created_at is minted by the server's Go clock in UTC, with nanoseconds")).capture("p1", "/id")
	tr.step("publish P2 with a name of exactly 60 runes (72 bytes): the length is counted in runes", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("tour",
			strings.Repeat("Größenwahn ", 5)+"Größe", "A long name.", "Be named.", "Names are short.", "the tour"))).
		capture("p2", "/id")
	tr.step("the member publishes P1's name in upper case from its own workspace: the pool is one list", member,
		"POST /api/v1/shared-products", jsonBody(sharedProductsJSON("tour", "TOASTER pro", "d", "v", "p", "t")))
	tr.step("the member publishes P3", member, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON("hardware", "Snooze Loom", "A loom that weaves while you sleep.",
			"Every nap makes a scarf.", "Naps produce nothing.", "people who nap"))).capture("p3", "/id")
	tr.step("the newest four: P3, P2, P1, then the newest seed, with created_at as the database stored it", o,
		"GET /api/v1/shared-products", query("limit=4"))

	// Votes: a person's, counted once, and answered with the entry's counts.
	tr.step("the runner key votes, for an entry that does not exist: refused", worker,
		"PUT /api/v1/shared-products/{id}/vote", at("id", "{{phantom}}"),
		note("the person gate comes before the lookup, so an id no entry has answers 403, not 404"))
	tr.step("the runner key withdraws a vote for an entry that does not exist: refused", worker,
		"DELETE /api/v1/shared-products/{id}/vote", at("id", "{{phantom}}"))
	tr.step("vote for an entry that does not exist", o, "PUT /api/v1/shared-products/{id}/vote", at("id", "{{phantom}}"))
	tr.step("vote for an id that is not a UUID: 404, as an id no entry has", o,
		"PUT /api/v1/shared-products/{id}/vote", at("id", "not-a-uuid"),
		note("the store reads an id that is not a UUID as one no row has"))
	tr.step("the owner votes for P1", o, "PUT /api/v1/shared-products/{id}/vote", at("id", "{{p1}}"))
	tr.step("the owner votes for P1 again: the same counts, one vote per person", o,
		"PUT /api/v1/shared-products/{id}/vote", at("id", "{{p1}}"))
	tr.step("the member votes for P1: two", member, "PUT /api/v1/shared-products/{id}/vote", at("id", "{{p1}}"))
	tr.step("the member votes for its own P3", member, "PUT /api/v1/shared-products/{id}/vote", at("id", "{{p3}}"))
	tr.step("the owner votes for the seeded Yesterdaily", o, "PUT /api/v1/shared-products/{id}/vote",
		at("id", "{{yesterdaily}}"))
	tr.step("sort=top as the owner: most votes first, then newest; voted is the owner's own vote", o,
		"GET /api/v1/shared-products", query("sort=top"))
	tr.step("sort=top_week as the member: the votes of the last 7 days by the database's clock; voted is the "+
		"member's", member, "GET /api/v1/shared-products", query("sort=top_week"))
	tr.step("sort=top as the runner key: voted is false on every row", worker, "GET /api/v1/shared-products",
		query("sort=top"))
	tr.step("the owner withdraws its vote for P1", o, "DELETE /api/v1/shared-products/{id}/vote", at("id", "{{p1}}"))
	tr.step("the owner withdraws it again: the same counts", o, "DELETE /api/v1/shared-products/{id}/vote",
		at("id", "{{p1}}"))
	tr.step("the reporter withdraws a vote it never cast for P3: P3's counts as they are", reporter,
		"DELETE /api/v1/shared-products/{id}/vote", at("id", "{{p3}}"))
	tr.step("withdraw a vote for an entry that does not exist", o, "DELETE /api/v1/shared-products/{id}/vote",
		at("id", "{{phantom}}"))
	tr.step("sort=top with one vote each: newest first", o, "GET /api/v1/shared-products", query("sort=top"))

	// Reports: per person; the third distinct reporter hides the entry.
	tr.step("the runner key reports an entry that does not exist: refused before the lookup", worker,
		"POST /api/v1/shared-products/{id}/report", at("id", "{{phantom}}"))
	tr.step("report an entry that does not exist", o, "POST /api/v1/shared-products/{id}/report",
		at("id", "{{phantom}}"))
	tr.step("report an id that is not a UUID: 404, as an id no entry has", o, "POST /api/v1/shared-products/{id}/report",
		at("id", "not-a-uuid"))
	tr.step("the owner reports P2", o, "POST /api/v1/shared-products/{id}/report", at("id", "{{p2}}"))
	tr.step("the owner reports P2 again: counted once", o, "POST /api/v1/shared-products/{id}/report",
		at("id", "{{p2}}"))
	tr.step("the member reports P2: two distinct reporters", member, "POST /api/v1/shared-products/{id}/report",
		at("id", "{{p2}}"))
	tr.step("the owner votes for P2: still votable after three reports by two people", o,
		"PUT /api/v1/shared-products/{id}/vote", at("id", "{{p2}}"))
	tr.step("the reporter reports P2: the third distinct reporter hides it", reporter,
		"POST /api/v1/shared-products/{id}/report", at("id", "{{p2}}"))
	tr.step("the newest three: P2 is left out", o, "GET /api/v1/shared-products", query("limit=3"))
	tr.step("sort=top: P2 is left out, its vote notwithstanding", o, "GET /api/v1/shared-products", query("sort=top"))
	tr.step("the member votes for the hidden P2: not found", member, "PUT /api/v1/shared-products/{id}/vote",
		at("id", "{{p2}}"))
	tr.step("the owner withdraws its vote for the hidden P2: not found either", o,
		"DELETE /api/v1/shared-products/{id}/vote", at("id", "{{p2}}"))
	tr.step("the moderator reports the hidden P2: accepted", moderator, "POST /api/v1/shared-products/{id}/report",
		at("id", "{{p2}}"))

	// The platform admin's delete.
	tr.step("the runner key deletes an entry that does not exist: refused before the lookup", worker,
		"DELETE /api/v1/shared-products/{id}", at("id", "{{phantom}}"))
	tr.step("the owner deletes P3: not a platform admin", o, "DELETE /api/v1/shared-products/{id}", at("id", "{{p3}}"))
	tr.step("the moderator deletes an id that is not a UUID: 404, as an id no entry has", moderator, "DELETE /api/v1/shared-products/{id}",
		at("id", "not-a-uuid"))
	tr.step("the moderator deletes P3", moderator, "DELETE /api/v1/shared-products/{id}", at("id", "{{p3}}"))
	tr.step("the moderator deletes P3 again", moderator, "DELETE /api/v1/shared-products/{id}", at("id", "{{p3}}"))
	tr.step("the member votes for the deleted P3", member, "PUT /api/v1/shared-products/{id}/vote", at("id", "{{p3}}"))
	tr.step("sort=top once P3 is gone", o, "GET /api/v1/shared-products", query("sort=top"))

	// The daily cap: 20 rows per workspace in 24 hours. The owner's
	// workspace holds P1 and the hidden P2; 18 more make 20.
	for i := 1; i <= sharedProductsDailyLimit-2; i++ {
		tr.setup(fmt.Sprintf("the owner's workspace publishes filler %d of 18", i), o, "POST /api/v1/shared-products",
			jsonBody(sharedProductsPlain(fmt.Sprintf("Tour filler %03d", i))), expect(201))
	}
	tr.step("the owner's 21st publication in 24 hours: refused, the hidden P2 counts", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsPlain("Late Product")))
	tr.step("a link from a workspace at its cap: Sanitize answers first", o, "POST /api/v1/shared-products",
		jsonBody(sharedProductsJSON("tour", "Late Link", "Order at www.example.com", "v", "p", "t")))
	tr.step("a duplicate from a workspace at its cap: the cap answers before the duplicate check", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsPlain("Yesterdaily")))
	tr.step("the moderator deletes the hidden P2", moderator, "DELETE /api/v1/shared-products/{id}", at("id", "{{p2}}"))
	tr.step("the owner publishes the late product again: a deleted row no longer counts", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsPlain("Late Product")))

	// The default cut and the ceiling: grow the pool to 201 visible entries
	// from workspaces the owner creates for it (setup).
	sharedProductsFill(tr, sharedProductsPoolLimit)
	tr.step("the pool of 201: the default limit answers the newest 200, so the oldest seed is cut", o,
		"GET /api/v1/shared-products")
	tr.step("limit=501 with 201 entries: capped at 500, not reset to the default, so all 201", o,
		"GET /api/v1/shared-products", query("limit=501"),
		note("the cap of 500 itself would take 501 visible entries"))
	tr.step("the owner's workspace, at its cap again, publishes into a full pool: the cap answers first", o,
		"POST /api/v1/shared-products", jsonBody(sharedProductsPlain("Crowded Product")))
	tr.step("the member's workspace, under its cap, publishes into the full pool: the ceiling", member,
		"POST /api/v1/shared-products", jsonBody(sharedProductsPlain("Crowded Product")),
		note("the ceiling counts the visible pool; the area set it to 201 ("+sharedProductsPoolLimitEnv+")"))
}

// sharedProductsFill publishes (setup) until the visible pool holds want
// entries, 20 from each workspace the owner creates for it.
func sharedProductsFill(tr *tour, want int) {
	tr.t.Helper()
	var rows []json.RawMessage
	r := tr.probe(tr.owner, "GET /api/v1/shared-products", query("limit=500"))
	if err := json.Unmarshal(r.body, &rows); err != nil || r.status != 200 {
		tr.t.Fatalf("read the visible pool: %d %s", r.status, r.body)
	}
	n := 0
	for ws := 1; len(rows)+n < want; ws++ {
		org := tr.setup(fmt.Sprintf("the owner's pool workspace %d", ws), tr.owner, "POST /api/v1/orgs",
			jsonBody(fmt.Sprintf(`{"name":"Tour pool %d"}`, ws)), expect(201)).value("/id")
		for i := 0; i < sharedProductsDailyLimit && len(rows)+n < want; i++ {
			n++
			tr.setup(fmt.Sprintf("pool workspace %d publishes entry %d", ws, n), tr.owner, "POST /api/v1/shared-products",
				jsonBody(sharedProductsPlain(fmt.Sprintf("Tour pool entry %03d", n))), actingIn(org), expect(201))
		}
	}
}
