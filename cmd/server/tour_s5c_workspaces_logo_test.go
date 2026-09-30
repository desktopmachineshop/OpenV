//go:build unix

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestTourS5cWorkspacesLogo is the S5c tour's workspaces area (refactor plan
// §6.4 S5c, before M9, which moves these handlers out of org_handlers.go
// into org_ and org_logo_handlers.go, and before X8 and X12, which read the
// effective limits and plan gates it pins; invariants I3 (a guard's answer
// before a lookup's, and a workspace the caller is not in answered as one no
// row has), I4, I8 (the logo's Cache-Control and
// Content-Disposition), I16 (the logo's file under UPLOADS_DIR); quirks Q1,
// Q5, Q14 and Q19; OpenV REQ-18, REQ-136, REQ-137, REQ-138, REQ-154). Its
// golden is testdata/tour/s5c/workspaces_logo.json.
//
// The area walks, in order: creating a workspace, whose answer is the
// domain's struct with the release channel unresolved (Q5), right beside a
// read that resolves it; each refusal of the create; the workspace list
// (the personal workspace first, then by name, and the deleted ones,
// written null while there are none, Q14); reading a workspace as a member,
// and the guard's answer to a workspace that does not exist (404, as to a
// workspace the caller is not in: I3) and to an id that is not one (the same); the settings a workspace admin changes
// on the single plan (a rename, the monthly budget as a number, null and a
// string, and the release channel and upgrade window that plan locks), with
// a rename sent with a budget refused, which stores nothing either; the
// feature gates on the nightly channel (every one on) and the
// stable-release preview the single plan refuses; the limits under the
// alpha terms, for a shared and a personal workspace; the plan a platform
// admin sets (business), each refusal of it, and what business changes:
// the stable channel, the channel and upgrade window its admin may now
// choose, and the preview a member turns on for itself alone, which a
// platform admin that is no member is refused; the logo in every raster
// format, each replacing the last and its file, each refusal (an image of a
// type other than the one declared among them), removal, and the uploads
// directory at the end; switching the session's active workspace; the
// answers of routes other areas own that this default environment
// makes: billing off (the public plans and every workspace billing route:
// 404 billing_unavailable) and Google sign-on not configured; deleting the
// workspace, its answer and what it hides, and restoring it; and last a
// workspace that does not exist, named by the platform admin, whom the
// workspace guard passes with no role but not with no workspace: the
// guard's 404 answers before any handler reads a body or writes anything.
//
// Actors: admin (the platform admin) sets plans, and reaches what only a
// caller the guards let by reaches (a platform admin passes every workspace
// role check, so it never stands in for a member); owner creates and
// administers W; member is a plain member of W, for the admin-only refusals;
// outsider belongs to no workspace but its own, for the non-member refusals.
//
// Pinned as it is, a bug whose fix is a release-noted bug-fix pull request
// of its own that regenerates this golden (R7): the platform admin's move to
// business puts the workspace on the stable channel with no stable release,
// so every gate is off (the channel and the empty stable release show it,
// and the all-off map is a pattern that a gate on would fail).
//
// Nondeterminism: ids, sessions and minted times (the generic tokens); a
// slug's last 8 hex digits (the area's pattern); the feature map, which
// every New features release adds a key to, pinned as all on for the
// nightly channel and all off for the stable channel with no stable release
// turned on (patterns); and a preview's gates, the stable release a preview
// resolves against and the next stable release, which depend on the stable
// marker the monthly Cut stable release workflow writes in RELEASE_NOTES.md
// (patterns), so that neither a release nor the cut changes the golden. Not
// pinned: a preview's feature values (deliberately, as above); a stable release turned on for a workspace (the
// stable scheduler runs hourly, and no stable release exists yet); the
// purge of a workspace deleted 30 days ago (time-bound); the 500s of a
// failing disk or database.
func TestTourS5cWorkspacesLogo(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "workspaces_logo",
		about: "Workspaces: create (Q5), list, read, update (budget, release channel, upgrade window), the platform " +
			"admin's plan, feature gates and the stable-release preview, limits, the logo in every raster format, " +
			"switching the active workspace, delete and restore; with the billing-off and Google-off answers of " +
			"the default environment.",
		run: workspacesLogoTour,
	})
}

// workspacesLogoMaxBytes is the logo's own cap (maxOrgLogoBytes, 2 MiB), and
// workspacesLogoFormCap the request's (that plus multipartOverheadBytes, 1
// MiB), which http.MaxBytesReader enforces while the form is read.
const (
	workspacesLogoMaxBytes = 2 << 20
	workspacesLogoFormCap  = workspacesLogoMaxBytes + 1<<20
)

// workspacesLogoFile sends a multipart form whose "file" part holds data,
// declared as contentType.
func workspacesLogoFile(name, contentType string, data []byte) tourOpt {
	return rawBody(multipartForm(nil, tourFormFile{field: "file", name: name, contentType: contentType, data: data}))
}

// workspacesLogoPadded is a PNG's first bytes followed by zeros, n bytes in
// all: a file whose size is the point, not its pixels.
func workspacesLogoPadded(n int) []byte {
	return append([]byte(tourPNG[:16]), bytes.Repeat([]byte{0}, n-16)...)
}

// workspacesLogoGrace is how long a deleted workspace stays restorable
// (orgs.DeletionGraceDays), which DELETE /api/v1/orgs/{id} answers as
// purge_after less deleted_at.
const workspacesLogoGrace = 30 * 24 * time.Hour

// workspacesLogoPurgeCheck checks what the golden cannot show of a delete's
// answer: purge_after, a time to come and so written <time>, is deleted_at
// plus the grace period exactly.
func workspacesLogoPurgeCheck(tr *tour, res *tourResult) {
	tr.t.Helper()
	at := func(pointer string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, res.value(pointer))
		if err != nil {
			tr.t.Fatalf("%s of %s is not an RFC 3339 time: %v", pointer, res.what(), err)
		}
		return v
	}
	if got := at("/purge_after").Sub(at("/deleted_at")); got != workspacesLogoGrace {
		tr.t.Errorf("%s: purge_after is %s after deleted_at; a deleted workspace stays restorable for %s "+
			"(orgs.DeletionGraceDays), which the golden, writing purge_after as <time>, does not show; if the change "+
			"is intended, change workspacesLogoGrace and regenerate with:\n  %s", res.what(), got, workspacesLogoGrace,
			strings.Join(tr.regenerate(), "\n  then "))
	}
}

func workspacesLogoTour(tr *tour) {
	o, admin, anon := tr.owner, tr.admin, tr.anon
	tr.slugPattern()
	// The stable channel's answers first, so that a map the nightly pattern
	// would also match (a preview of a stable release that has every
	// feature) is taken here, by its channel. A stable-channel workspace
	// with no stable release turned on, read by a caller not previewing,
	// has every gate off whatever the release notes say (release.Enabled):
	// that map is pinned as all off; only a preview's gates depend on the
	// stable marker.
	tr.patternVarying(`"channel":"stable","stable_release":"","preview":false,"features":`+
		`(\{(?:"[a-z0-9-]+":false,?)+\}(?:,"next_stable_release":"[^"]*","next_stable_at":"[^"]*")?)`,
		`"<every feature off>"`, len(`{}`), 1250, "every registered feature key with false, and the next stable "+
			"release and when it turns on (omitted while there is none): a stable-channel workspace with no stable "+
			"release turned on, read by a caller not previewing, sees no gated feature (release.Enabled), and every "+
			"New features release adds a key, so the map's length varies (a key on would leave the map unmatched, "+
			"and the golden would show it)")
	tr.patternVarying(`"channel":"stable","stable_release":"[^"]*","preview":true,"features":`+
		`(\{(?:"[a-z0-9-]+":(?:true|false),?)*\}(?:,"next_stable_release":"[^"]*","next_stable_at":"[^"]*")?)`,
		`"<the stable channel's gates>"`, len(`{}`), 1250, "the gates a member previewing the stable channel sees, "+
			"and the next stable release and when it turns on (omitted while there is none), which depend on the "+
			"stable marker the monthly Cut stable release workflow writes in RELEASE_NOTES.md and on every New "+
			"features release: the channel, the stable release and the preview beside them are pinned")
	tr.patternVarying(`"channel":"stable","stable_release":("[^"]*"),"preview":true`,
		`"<the newest stable release, or none before the first cut>"`, len(`""`), len(`"99.99.99"`),
		"the stable release a member's preview resolves against: the newest one RELEASE_NOTES.md marks, \"\" "+
			"until the monthly cut marks one")
	tr.patternVarying(`"features":(\{(?:"[a-z0-9-]+":true,?)*\})`, `"<every feature on>"`, len(`{}`), 1250,
		"every registered feature key (internal/domain/release/features.go) with true: every feature is on for the "+
			"nightly channel, and every New features release adds a key, so the map's length varies (a key off "+
			"would leave the map unmatched, and the golden would show it)")

	m := tr.register("member", "Tour Member", "a plain member of W: the admin-only refusals")
	outsider := tr.register("outsider", "Tour Outsider", "a member of no workspace but its own: the non-member refusals")
	worker := tr.bearerActor("worker", tr.setup("a worker key of the owner's personal workspace", o,
		"POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"), jsonBody(`{"name":"tour worker"}`)).value("/key"),
		"a worker key of the owner's personal workspace, as a bearer")

	// Create (Q5): the create answers the domain's struct as the service
	// built it; a read resolves the channel from the plan.
	created := tr.step("create a workspace: 201, the domain's struct, the release channel unresolved (Q5)", o,
		"POST /api/v1/orgs", jsonBody(`{"name":"  Tour Workspace  "}`),
		note("CreateOrg encodes what orgs.CreateOrg built, setting no Content-Type of its own, so net/http sniffs "+
			"text/plain (Q1): release_channel \"\" and release_channel_locked false, since only the repository's "+
			"scanOrg resolves them from the plan (and sets has_logo), billing's status \"\" where a read says "+
			"\"none\", and the creator's role, which a read of one workspace leaves out; the name is trimmed, and "+
			"the slug is the name's letters and digits, then the id's first 8 hex digits"))
	w := created.capture("w", "/id")
	tr.step("read it back: the channel resolved from the single plan, nightly and locked (Q5)", o, "GET /api/v1/orgs/{id}",
		at("id", w), note(fmt.Sprintf("the same workspace as step %d's answer, read through scanOrg", created.step.n)))
	tr.actIn(o, w)
	tr.step("create with a malformed body", o, "POST /api/v1/orgs", jsonBody(`{`))
	tr.step("create with a blank name: the domain's error, as it is (Q19)", o, "POST /api/v1/orgs",
		jsonBody(`{"name":"   "}`))
	tr.setup("a second shared workspace, created after W and named to sort before it", o, "POST /api/v1/orgs",
		jsonBody(`{"name":"Tour Annex"}`), expect(201)).capture("annex", "/id")
	tr.join(m, w, "member")

	// Listing and reading.
	tr.step("the owner's workspaces: the personal one first, then by name, and the one the request names as active",
		o, "GET /api/v1/orgs")
	tr.step("the owner's deleted workspaces: none, written null (Q14)", o, "GET /api/v1/orgs", query("deleted=true"))
	tr.step("a member reads W", m, "GET /api/v1/orgs/{id}", at("id", w))
	tr.step("read a workspace that does not exist: the guard's 404, before any lookup (I3)", o, "GET /api/v1/orgs/{id}",
		at("id", "{{phantom}}"))
	tr.step("read a workspace by something that is not an id: the same 404", o,
		"GET /api/v1/orgs/{id}", at("id", "not-a-workspace"))
	tr.step("a non-member reads W", outsider, "GET /api/v1/orgs/{id}", at("id", w))

	// Update, on the single plan.
	tr.step("rename W", o, "PUT /api/v1/orgs/{id}", at("id", w), jsonBody(`{"name":" Tour Workspace Renamed "}`),
		note("the name is trimmed; the slug keeps the name it was made from"))
	tr.step("set a monthly budget: a number (workspace_budget is on under the alpha terms)", o, "PUT /api/v1/orgs/{id}",
		at("id", w), jsonBody(`{"monthly_budget_usd":250.5}`))
	tr.step("clear the budget: null, and the key is gone from the answer", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"monthly_budget_usd":null}`))
	tr.step("a negative budget", o, "PUT /api/v1/orgs/{id}", at("id", w), jsonBody(`{"monthly_budget_usd":-1}`))
	tr.step("a rename with a budget that is a string: 400, and nothing stored", o, "PUT /api/v1/orgs/{id}",
		at("id", w), jsonBody(`{"name":"Tour Workspace Again","monthly_budget_usd":"lots"}`),
		note("UpdateOrg checks every part of the request before it writes any, so the refusal leaves the name as "+
			"it was (the next step reads it), as it does for a refused channel or window"))
	tr.step("W as the refused update left it: the name and updated_at of the last update that passed", o,
		"GET /api/v1/orgs/{id}", at("id", w))
	tr.step("choose the stable channel on the single plan: locked", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"release_channel":"stable"}`))
	tr.step("choose an upgrade window on the single plan: locked", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"upgrade_window":{"day":15,"hour":9,"timezone":"Europe/Berlin"}}`))
	tr.step("clear the upgrade window on the single plan: locked as well", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"upgrade_window":null}`))
	tr.step("an upgrade window that is not an object", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"upgrade_window":"soon"}`))
	tr.step("update with a malformed body", o, "PUT /api/v1/orgs/{id}", at("id", w), jsonBody(`{`))
	tr.step("a member updates W: admins only", m, "PUT /api/v1/orgs/{id}", at("id", w), jsonBody(`{"name":"Mine"}`))

	// Feature gates and the preview on the single plan (nightly).
	tr.step("W's feature gates on the nightly channel: every feature on", o, "GET /api/v1/orgs/{id}/features", at("id", w))
	tr.step("the owner's personal workspace: nightly too", o, "GET /api/v1/orgs/{id}/features",
		at("id", "{{owner.workspace}}"))
	tr.step("a non-member reads W's gates", outsider, "GET /api/v1/orgs/{id}/features", at("id", w))
	tr.step("preview the next stable release on the single plan: it always runs nightly", o,
		"PUT /api/v1/orgs/{id}/members/me/preview", at("id", w), jsonBody(`{"enabled":true}`))
	tr.step("preview with a malformed body", o, "PUT /api/v1/orgs/{id}/members/me/preview", at("id", w), jsonBody(`{`))

	// Limits, under the alpha terms.
	tr.step("W's limits under the alpha terms: every count unlimited, the resources capped, every flag included, "+
		"in the catalogue's order", o,
		"GET /api/v1/orgs/{id}/limits", at("id", w))
	tr.step("the owner's personal workspace's limits: one member, fixed, with its own description", o,
		"GET /api/v1/orgs/{id}/limits", at("id", "{{owner.workspace}}"))
	tr.step("a member reads W's limits", m, "GET /api/v1/orgs/{id}/limits", at("id", w))
	tr.step("a non-member reads W's limits", outsider, "GET /api/v1/orgs/{id}/limits", at("id", w))

	// The plan, which only a platform admin sets (REQ-154).
	tr.step("the platform admin moves W to business: the workspace, with an explicit Content-Type", admin,
		"PUT /api/v1/orgs/{id}/plan", at("id", w), jsonBody(`{"plan":"business"}`),
		note("the channel resolves to stable, business's default, with no stable release turned on, so every "+
			"gate is off until one is (the billing path writes a nightly override instead; the all-off map below is "+
			"a pattern that a gate on would fail)"))
	tr.step("a plan that does not exist: the six plans listed", admin, "PUT /api/v1/orgs/{id}/plan", at("id", w),
		jsonBody(`{"plan":"gold"}`))
	tr.step("a workspace that does not exist", admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{phantom}}"),
		jsonBody(`{"plan":"business"}`))
	tr.step("a plan with a malformed body", admin, "PUT /api/v1/orgs/{id}/plan", at("id", w), jsonBody(`{`))
	tr.step("the workspace's own admin sets its plan: platform admins only", o, "PUT /api/v1/orgs/{id}/plan", at("id", w),
		jsonBody(`{"plan":"enterprise"}`))
	tr.step("W on business, read back: stable, and the channel no longer locked", o, "GET /api/v1/orgs/{id}", at("id", w))
	tr.step("W's limits on business, still under the alpha terms", o, "GET /api/v1/orgs/{id}/limits", at("id", w))

	// What business lets its admin choose (REQ-136, REQ-138), and the
	// preview a member takes for itself (REQ-137).
	tr.step("choose the stable channel on business", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"release_channel":"stable"}`))
	tr.step("a channel that does not exist", o, "PUT /api/v1/orgs/{id}", at("id", w), jsonBody(`{"release_channel":"beta"}`))
	tr.step("an upgrade window: the 15th at 09:00 in Berlin", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"upgrade_window":{"day":15,"hour":9,"timezone":"Europe/Berlin"}}`))
	tr.step("a window on a day not every month has", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"upgrade_window":{"day":31,"hour":9,"timezone":"Europe/Berlin"}}`))
	tr.step("a window in a time zone that does not exist", o, "PUT /api/v1/orgs/{id}", at("id", w),
		jsonBody(`{"upgrade_window":{"day":1,"hour":9,"timezone":"Mars/Olympus_Mons"}}`))
	tr.step("clear the window: null", o, "PUT /api/v1/orgs/{id}", at("id", w), jsonBody(`{"upgrade_window":null}`))
	tr.step("W's gates on the stable channel", o, "GET /api/v1/orgs/{id}/features", at("id", w))
	tr.step("the owner previews the next stable release: its gates, preview true", o,
		"PUT /api/v1/orgs/{id}/members/me/preview", at("id", w), jsonBody(`{"enabled":true}`))
	tr.step("the owner's gates with the preview on", o, "GET /api/v1/orgs/{id}/features", at("id", w))
	tr.step("the member's gates: the preview is the owner's alone", m, "GET /api/v1/orgs/{id}/features", at("id", w))
	tr.step("the owner turns the preview off", o, "PUT /api/v1/orgs/{id}/members/me/preview", at("id", w),
		jsonBody(`{"enabled":false}`))
	tr.step("a non-member previews in W", outsider, "PUT /api/v1/orgs/{id}/members/me/preview", at("id", w),
		jsonBody(`{"enabled":true}`))
	tr.step("the platform admin, a member of no shared workspace, previews in W: the guard lets it by, and the "+
		"write finds no membership, so the not-member 403", admin, "PUT /api/v1/orgs/{id}/members/me/preview",
		at("id", w), jsonBody(`{"enabled":true}`),
		note("the preview is kept on the membership: the repository's UPDATE of org_members matches no row and "+
			"answers orgs.ErrNotMember, which the handler answers with its text"))

	// The logo (I8, I16).
	logo := func(title string, a *tourActor, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "POST /api/v1/orgs/{id}/logo", append([]tourOpt{at("id", w)}, opts...)...)
	}
	logo("upload a PNG logo: the workspace, has_logo true", o, workspacesLogoFile("logo.png", "image/png", []byte(tourPNG)))
	tr.step("a member fetches it: the bytes as stored, their stored type, cached privately for five minutes", m,
		"GET /api/v1/orgs/{id}/logo", at("id", w))
	tr.step("a Range request: the whole logo all the same (the handler copies the file, it does not serve ranges)", m,
		"GET /api/v1/orgs/{id}/logo", at("id", w), withHeader("Range", "bytes=0-9"))
	tr.step("a non-member fetches it", outsider, "GET /api/v1/orgs/{id}/logo", at("id", w))
	logo("replace it with a JPEG: stored as .jpg, and the .png removed", o,
		workspacesLogoFile("logo.jpg", "image/jpeg", []byte(tourJPEG)))
	logo("replace it with a GIF", o, workspacesLogoFile("logo.gif", "image/gif", []byte(tourGIF)))
	logo("replace it with a WebP", o, workspacesLogoFile("logo.webp", "image/webp", []byte(tourWebP)))
	tr.step("the owner fetches the WebP, as image/webp", o, "GET /api/v1/orgs/{id}/logo", at("id", w))
	logo("an SVG: refused by its declared type", o, workspacesLogoFile("logo.svg", "image/svg+xml", []byte(tourSVG)))
	logo("a PDF declared as a PNG: its bytes are no image", o, workspacesLogoFile("logo.png", "image/png", []byte(tourPDF)))
	logo("a GIF declared as a PNG: its bytes are an image, but not of the declared type", o,
		workspacesLogoFile("logo.png", "image/png", []byte(tourGIF)))
	tr.step("the logo is still the WebP, as image/webp: the refused upload stored nothing", o,
		"GET /api/v1/orgs/{id}/logo", at("id", w))
	logo("a file of 2 MiB and a byte: over the logo's own cap", o,
		workspacesLogoFile("big.png", "image/png", workspacesLogoPadded(workspacesLogoMaxBytes+1)))
	logo("a request over the logo's cap and the form's allowance: refused while the form is read", o,
		workspacesLogoFile("huge.png", "image/png", workspacesLogoPadded(workspacesLogoFormCap+1)))
	logo("a form with no file", o, rawBody(multipartForm([][2]string{{"note", "no file here"}})))
	logo("a member uploads: admins only", m, workspacesLogoFile("logo.png", "image/png", []byte(tourPNG)))
	tr.step("a member removes it: admins only", m, "DELETE /api/v1/orgs/{id}/logo", at("id", w))
	tr.step("remove the logo: the file, then the record", o, "DELETE /api/v1/orgs/{id}/logo", at("id", w))
	tr.step("fetch it once removed", m, "GET /api/v1/orgs/{id}/logo", at("id", w))
	tr.step("remove it again: nothing to remove, 200 all the same", o, "DELETE /api/v1/orgs/{id}/logo", at("id", w))
	logo("upload the PNG once more: W's only file in the uploads directory at the end", o,
		workspacesLogoFile("logo.png", "image/png", []byte(tourPNG)))

	// The session's active workspace (POST /orgs/{id}/activate binds the
	// session: the sessions area shows how the server resolves a request
	// with no X-Org-ID).
	tr.step("the member makes W its session's active workspace", m, "POST /api/v1/orgs/{id}/activate", at("id", w))
	tr.step("the member's workspaces with no X-Org-ID: active_org is W", m, "GET /api/v1/orgs", noOrgHeader())
	tr.step("activate with a worker key: no account, the guard's 401", worker, "POST /api/v1/orgs/{id}/activate",
		at("id", w))
	tr.step("a non-member activates W", outsider, "POST /api/v1/orgs/{id}/activate", at("id", w))

	// Billing off (the billing area's routes; this environment has no
	// provider) and Google sign-on not configured (the sessions area's).
	tr.step("the public plans with billing off: none for sale", anon, "GET /api/v1/public/plans")
	tr.step("W's billing with billing off", o, "GET /api/v1/orgs/{id}/billing", at("id", w))
	tr.step("refresh W's billing with billing off", o, "POST /api/v1/orgs/{id}/billing/refresh", at("id", w))
	tr.step("check out with billing off", o, "POST /api/v1/orgs/{id}/billing/checkout", at("id", w),
		jsonBody(`{"plan":"business","interval":"month","currency":"eur"}`))
	tr.step("change the plan with billing off", o, "POST /api/v1/orgs/{id}/billing/change", at("id", w),
		jsonBody(`{"plan":"business_lite","interval":"year"}`))
	tr.step("open the billing portal with billing off", o, "POST /api/v1/orgs/{id}/billing/portal", at("id", w))
	tr.step("start Google sign-on with no client configured", anon, "GET /api/v1/auth/google",
		once("a single sign-on request, sent once as the sessions area sends them"))
	tr.step("the Google callback with no client configured", anon, "GET /api/v1/auth/google/callback",
		once("a single sign-on request, sent once as the sessions area sends them"), query("state=s&code=c"))

	// Delete and restore. The owner moves to its personal workspace first,
	// so that it never reads the events of a deleted one.
	tr.actIn(o, "{{owner.workspace}}")
	tr.step("delete the personal workspace: refused", o, "DELETE /api/v1/orgs/{id}", at("id", "{{owner.workspace}}"))
	tr.step("a member deletes W: admins only", m, "DELETE /api/v1/orgs/{id}", at("id", w))
	tr.step("a workspace that does not exist: the guard's 404", o, "DELETE /api/v1/orgs/{id}", at("id", "{{phantom}}"))
	deleted := tr.step("delete W: when, and when it is purged (30 days on)", o, "DELETE /api/v1/orgs/{id}", at("id", w),
		note("the route is alwaysWritable, so a workspace over its plan may still be deleted (S5e's over-plan pass)"),
		note("purge_after is a time to come, so the golden writes <time>; the area checks it is 30 days after "+
			"deleted_at to the nanosecond (workspacesLogoPurgeCheck)"))
	workspacesLogoPurgeCheck(tr, deleted)
	tr.step("read W once deleted: refused, as for a non-member", o, "GET /api/v1/orgs/{id}", at("id", w))
	tr.step("the owner's workspaces: W is gone", o, "GET /api/v1/orgs")
	tr.step("the owner's deleted workspaces: W", o, "GET /api/v1/orgs", query("deleted=true"))
	tr.step("delete W again: refused, since the guard no longer finds the owner's role", o, "DELETE /api/v1/orgs/{id}",
		at("id", w))
	tr.step("a member restores W: admins only", m, "POST /api/v1/orgs/{id}/restore", at("id", w))
	tr.step("restore W", o, "POST /api/v1/orgs/{id}/restore", at("id", w),
		note("the answer is the workspace as the restore stored it: deleted_at gone, and updated_at the restore's "+
			"(the read of W below shows the same)"))
	tr.step("restore it again: not deleted", o, "POST /api/v1/orgs/{id}/restore", at("id", w))
	tr.step("restore a workspace that does not exist: the role lookup answers first", o, "POST /api/v1/orgs/{id}/restore",
		at("id", "{{phantom}}"))
	tr.step("the platform admin restores a workspace that does not exist: no role lookup, so the service's 404", admin,
		"POST /api/v1/orgs/{id}/restore", at("id", "{{phantom}}"))
	tr.step("W restored, read back", o, "GET /api/v1/orgs/{id}", at("id", w))
	tr.step("the owner's workspaces: W is back", o, "GET /api/v1/orgs")

	// A workspace that does not exist, named by the platform admin, whom the
	// workspace guard (orgAccess) passes with no role, but only into a
	// workspace that exists: its 404 answers, which nobody else reaches.
	phantom := at("id", "{{phantom}}")
	nowhere := func(what string) string {
		return "the platform admin " + what + " a workspace that does not exist"
	}
	tr.step(nowhere("reads")+": the guard's 404", admin, "GET /api/v1/orgs/{id}", phantom)
	tr.step(nowhere("renames")+": the guard's 404", admin, "PUT /api/v1/orgs/{id}", phantom,
		jsonBody(`{"name":"Nowhere"}`))
	tr.step(nowhere("deletes")+": the guard's 404", admin, "DELETE /api/v1/orgs/{id}", phantom)
	tr.step(nowhere("reads the gates of")+": 404", admin, "GET /api/v1/orgs/{id}/features", phantom)
	tr.step(nowhere("previews in")+": 404", admin, "PUT /api/v1/orgs/{id}/members/me/preview",
		phantom, jsonBody(`{"enabled":true}`))
	tr.step(nowhere("reads the limits of")+": 404", admin, "GET /api/v1/orgs/{id}/limits", phantom)
	tr.step(nowhere("fetches the logo of")+": 404, not the no-logo one", admin, "GET /api/v1/orgs/{id}/logo", phantom)
	tr.step(nowhere("removes the logo of")+": 404", admin, "DELETE /api/v1/orgs/{id}/logo", phantom)
	tr.step(nowhere("uploads a logo for")+": 404, the workspace looked up before anything is written", admin,
		"POST /api/v1/orgs/{id}/logo", phantom, workspacesLogoFile("logo.png", "image/png", []byte(tourPNG)),
		note("the uploads directory at the end lists W's logo alone"))
	tr.step("the platform admin makes a workspace that does not exist its session's active one: 404, nothing stored",
		admin, "POST /api/v1/orgs/{id}/activate", phantom,
		note("sessions.active_org_id would take any UUID; the guard looks the workspace up before ActivateOrg "+
			"stores anything"))
	tr.step("the platform admin's workspaces with no X-Org-ID, after the refused activation: its personal one", admin,
		"GET /api/v1/orgs", noOrgHeader())
}
