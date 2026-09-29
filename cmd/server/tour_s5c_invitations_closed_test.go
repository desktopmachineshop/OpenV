//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestTourS5cInvitationsClosed is the S5c tour's workspace-invitation area,
// on a server whose self-service registration is closed (refactor plan §6.4
// S5c, before M9; invariants I3, I4, I8 (Retry-After); quirk Q18; OpenV
// REQ-95, REQ-143). Its golden is testdata/tour/s5c/invitations_closed.json.
//
// Registration is closed (S4b's registration_closed profile), so an
// invitation link is the one door a new account comes through: admin, owner
// and an existing account (existing) are registered on a second server with
// registration open (tourArea.profiles signs them up there), and every
// other account arrives through a recorded registration that carries an
// invite token (tour.adopt). The owner shares two workspaces: W, where it
// acts and reads the events, and W2, whose invitations (setup) are the
// door for the two accounts that later take up an invitation to W signed
// in.
//
// The area walks, in order: the sign-in configuration and policy, which say
// registration is closed, and a registration without a token (403
// registration_closed); then the invitation routes, as W's admin: the list
// while it is empty ([], which the handler writes for no rows), an invitation
// to an address no account has (202, the invitation, its one-time link and
// emailed false, since the server has no mailer; the token is cut out of the
// link), the same through POST /orgs/{id}/members, which takes the same branch,
// a re-invitation of an address (the same invitation, a new link that replaces
// the old), an address an account has (201, the membership, whose created_at is
// the zero time, and org.member_added) and one a member has (409), each refusal
// in the handler's order (an address, a role, a body, the personal workspace, a
// member who is not an admin, no session), the list (pending only, newest
// first), and revocation (204, then 404, and 404 for an id no invitation of the
// workspace has); then the preview a sign-up page shows (the address, the
// workspace, the role and the expiry; the token is trimmed), and its one 404
// for every link that does not work; then the door: a registration with a live
// token and its address (in other capitals: the account is made, verified and
// joins W with the invited role, the answer says "accepted", and
// org.invitation_accepted names the new account as its actor), and the 403 for
// a live token of another address, a revoked, an unknown and a spent one; then
// an account taking up an invitation signed in: 200 and
// org.invitation_accepted, the account its actor again, 200 already_member true
// with no event for an account already in W, and each refusal (a spent link,
// another address's, an unknown one, no session, a worker key in place of the
// session, no JSON Content-Type, a malformed body); the revocation of an
// invitation already taken up (204: the row goes, the membership stays); the
// list again, which holds only what no one took up; and last the two rate-limit
// buckets: inviteLimiter, one per inviting account (the invited admin spends
// its 20, and the owner still invites), and invitePreviewLimiter (Q18), drained
// once through previews and once through share-link lookups, each ending in a
// preview's 429; and, at the very end, the invited admin's add through POST
// /orgs/{id}/members refused by the inviteLimiter budget its invitations spent
// (Q18: both routes invite through one call site).
//
// The server trusts CF-Connecting-IP as the client's address (env), which
// only the steps that name a network send, so the registrations spend their
// own addresses' budgets of five and each preview bucket drained here is one
// of its own (RFC 5737 documentation addresses). Every drain is some twenty
// or sixty requests in well under a second, and each 429 comes soon after
// it, so its Retry-After is the bucket's refill interval or a little less;
// the area's header patterns allow a third of the interval, so that a slow or
// loaded runner does not fail the golden.
//
// Nondeterminism: ids, sessions, invitation tokens (registered from the links
// the answers carry) and minted times (the generic tokens); an invitation's
// expiry, seven days ahead, is <time>; a workspace slug's last 8 hex digits,
// which are its id's first eight (the area's own pattern); three Retry-After
// countdowns. Both doors publish the same org.invitation_accepted as the SSO
// area's OIDC sign-in, the joiner its actor: a registration that took up an
// invitation published none, and the signed-in acceptance published one as
// "system", since the middleware does not run on /api/v1/auth/ paths, until the
// scouts' bug 4 was fixed by a release-noted bug-fix pull request of its own.
// Not pinned: the invitation mail and the hour-long suppression of a re-send,
// which need a mailer (the mail area's, emailed true); an invitation's expiry
// after seven days (time-bound); a single sign-on account getting past the
// closed door (the SSO areas', on an open server); the capped seats an
// invitation charges through checkOrgSeats (the alpha terms cap none; the
// tiers-on area pins them).
func TestTourS5cInvitationsClosed(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "invitations_closed",
		about: "Workspace invitations on a server whose registration is closed: creating, listing and revoking them, " +
			"the preview a sign-up page shows, registration with an invite token (the one door), taking one up signed " +
			"in, and the per-account invitation budget and the preview bucket share links also spend (Q18).",
		run:      invitationsClosedTour,
		profiles: []string{"registration_closed"},
		env:      map[string]string{"OPENV_CLIENT_IP_HEADER": "CF-Connecting-IP"},
		accounts: []tourAccount{{name: "existing", display: "Tour Existing", about: "an account made before the " +
			"invitations (on the second server), whose address no invitation names: added to W outright, then a " +
			"member that is not an admin, and the account that presents another address's link"}},
	})
}

// The addresses (RFC 5737 documentation ranges) the area's steps send as
// CF-Connecting-IP, each a rate-limit bucket of its own.
const (
	invitationsClosedSignUpNet  = "198.51.100.40" // the first five registrations
	invitationsClosedSignUpNet2 = "198.51.100.41" // the rest
	invitationsClosedPreviewNet = "198.51.100.50" // the recorded previews
	invitationsClosedDrainNet   = "203.0.113.20"  // invitePreviewLimiter drained through previews
	invitationsClosedShareNet   = "203.0.113.21"  // ... through share-link lookups
)

// invitationsClosedFrom sends a request from an address of the area's.
func invitationsClosedFrom(net string) tourOpt { return withHeader("CF-Connecting-IP", net) }

// invitationsClosedInvite is an invitation body.
func invitationsClosedInvite(email, role string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"email":%q,"role":%q}`, email, role))
}

// invitationsClosedSignUp is a registration body carrying an invite token.
func invitationsClosedSignUp(email, name, token string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"email":%q,"password":%q,"name":%q,"invite_token":%q}`, email, tourPassword, name,
		token))
}

// invitationsClosedToken registers the token an invitation's link carries,
// and the invitation's id, from a 202.
func invitationsClosedToken(res *tourResult, name string) {
	res.tr.t.Helper()
	res.captureMatch("invite."+name, "/link", `\?invite=([0-9a-f]{64})$`)
	res.capture("invitation."+name, "/invitation/id")
}

func invitationsClosedTour(tr *tour) {
	o, anon, e := tr.owner, tr.anon, tr.actor("existing")
	tr.remember("unknown.token", strings.Repeat("fedcba9876543210", 4))
	tr.remember("share.token", strings.Repeat("0a1b2c3d", 8))
	tr.slugPattern()
	tr.headerPattern("Retry-After", `^(5[0-9]|60)$`, "<retry-after 60 s>", "Retry-After on an admin's 21st "+
		"invitation, through either route that invites: ceil(60 s less the seconds since the first of the 21, "+
		"inviteLimiter's refill of 60 an hour), so 60 while they take under a second and no less than 50 within ten")
	tr.headerPattern("Retry-After", `^(1[0-5])$`, "<retry-after 15 s>", "Retry-After on the 61st invitation preview "+
		"or share-link lookup from one address: invitePreviewLimiter refills 240 an hour, as above (no less than 10 "+
		"within five)")

	// W2's invitations are the door for two accounts that later take up an
	// invitation to W signed in; the owner then acts in W.
	w2 := tr.sharedWorkspace("w2", "Tour Door")
	for _, name := range []string{"joiner", "stayer"} {
		invitationsClosedToken(tr.setup("invite "+name+" to W2", o, "POST /api/v1/orgs/{id}/invitations", at("id", w2),
			actingIn(w2), invitationsClosedInvite("tour-"+name+"@example.com", "member"), expect(202)), name+".w2")
	}
	w := tr.sharedWorkspace("w", "Tour Shared")
	inW := at("id", w)

	// The closed door.
	fromSignUp := invitationsClosedFrom(invitationsClosedSignUpNet)
	tr.step("the sign-in methods: registration closed", anon, "GET /api/v1/auth/config")
	tr.step("the registration policy: closed", anon, "GET /api/v1/auth/policy")
	tr.step("register with no invite token: the door is closed", anon, "POST /api/v1/auth/register", fromSignUp,
		jsonBody(`{"email":"tour-walkin@example.com","password":"tour password 1","name":"Tour Walk-in"}`))

	// Invitations, as W's admin (the owner).
	tr.step("W's invitations, none yet: [], which the handler writes for no rows", o,
		"GET /api/v1/orgs/{id}/invitations", inW)
	invite := func(title string, a *tourActor, email, role string, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "POST /api/v1/orgs/{id}/invitations", append([]tourOpt{inW,
			invitationsClosedInvite(email, role)}, opts...)...)
	}
	invitationsClosedToken(invite("invite an address no account has, as an admin: 202, the invitation, its "+
		"one-time link, and emailed false (no mailer); org.invitation_sent", o, "Tour-Invitee@Example.com", "admin",
		note("the address is stored folded to lower case; the event's payload carries it as sent")), "invitee")
	invitationsClosedToken(invite("invite the joiner, as a member", o, "tour-joiner@example.com", "member"), "joiner")
	invitationsClosedToken(invite("invite the stayer, with no role: a member", o, "tour-stayer@example.com", ""),
		"stayer")
	invitationsClosedToken(invite("invite an address the admin will revoke", o, "tour-revoked@example.com", "member"),
		"revoked")
	first := tr.step("invite through the members route: the same branch, the same 202", o,
		"POST /api/v1/orgs/{id}/members", inW, invitationsClosedInvite("tour-pending@example.com", "member"),
		note("the route is the members area's; both routes bring an address in through addOrInviteToOrg"))
	first.captureMatch("invite.pending.first", "/link", `\?invite=([0-9a-f]{64})$`)
	first.capture("invitation.pending", "/invitation/id")
	again := invite("invite the same address again, as an admin: the same invitation with the new role and a new "+
		"link, which replaces the old", o, "tour-pending@example.com", "admin")
	again.captureMatch("invite.pending", "/link", `\?invite=([0-9a-f]{64})$`)
	invite("invite an address an account has: 201, the membership, and org.member_added", o, e.email, "member",
		note("the membership is built in the handler, not read back, so its created_at is the zero time"))
	invite("invite a member: 409", o, e.email, "admin")
	invite("invite something that is not an address", o, "not-an-address", "member")
	invite("invite with a role there is none of", o, "tour-owner-role@example.com", "owner")
	tr.step("invite with a malformed body", o, "POST /api/v1/orgs/{id}/invitations", inW, jsonBody(`{`))
	tr.step("invite into the owner's personal workspace, which takes no members", o,
		"POST /api/v1/orgs/{id}/invitations", at("id", "{{owner.workspace}}"),
		invitationsClosedInvite("tour-personal@example.com", "member"))
	invite("invite as a member that is not an admin", e, "tour-by-member@example.com", "member")
	invite("invite with no session: the middleware's 401", anon, "tour-by-nobody@example.com", "member")
	tr.step("W's pending invitations, newest first", o, "GET /api/v1/orgs/{id}/invitations", inW)
	tr.step("the invitations, as a member that is not an admin", e, "GET /api/v1/orgs/{id}/invitations", inW)
	revoke := func(title string, a *tourActor, id string) *tourResult {
		return tr.step(title, a, "DELETE /api/v1/orgs/{id}/invitations/{invId}", at("id", w, "invId", id))
	}
	revoke("revoke as a member that is not an admin", e, "{{invitation.revoked}}")
	revoke("revoke an invitation: 204, and no event", o, "{{invitation.revoked}}")
	revoke("revoke it again: 404", o, "{{invitation.revoked}}")
	revoke("revoke an invitation of another workspace, W2's: 404, as if there were none", o,
		"{{invitation.joiner.w2}}")
	revoke("revoke an id no invitation has", o, "{{phantom}}")

	// The preview a sign-up page shows; one 404 for every link that does not
	// work.
	fromPreview := invitationsClosedFrom(invitationsClosedPreviewNet)
	preview := func(title, body string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "POST /api/v1/auth/invitations/preview", append([]tourOpt{fromPreview,
			jsonBody(body)}, opts...)...)
	}
	preview("preview a live link: the address, the workspace, the role and the expiry", `{"token":"{{invite.invitee}}"}`)
	preview("preview the link with spaces around it: the token is trimmed", `{"token":" {{invite.invitee}} "}`)
	preview("preview a revoked link", `{"token":"{{invite.revoked}}"}`)
	preview("preview a link the re-invitation replaced", `{"token":"{{invite.pending.first}}"}`)
	preview("preview a token no invitation has", `{"token":"{{unknown.token}}"}`)
	preview("preview with no token", `{"token":""}`)
	preview("preview with a malformed body", `{`)

	// The door: a registration that carries a live link for its own
	// address.
	reg := tr.step("register with the invitee's link and its address, in other capitals: the account is made, "+
		"verified and joins W as an admin, and org.invitation_accepted", anon, "POST /api/v1/auth/register", fromSignUp,
		invitationsClosedSignUp("TOUR-INVITEE@example.COM", "Tour Invitee", "{{invite.invitee}}"),
		note("the address is compared folded; the registration publishes the event the signed-in acceptance "+
			"publishes, with the new account as its actor"))
	invitee := tr.adopt(reg, "invitee", fmt.Sprintf("the account a registration with an invite link made (step %d), "+
		"an admin of W", reg.step.n))
	tr.step("the invitee's workspaces: its personal one and W, as an admin", invitee, "GET /api/v1/orgs")
	tr.step("register with the joiner's live link and another address", anon, "POST /api/v1/auth/register", fromSignUp,
		invitationsClosedSignUp("tour-stranger@example.com", "Tour Stranger", "{{invite.joiner}}"))
	tr.step("register with the revoked link and its address", anon, "POST /api/v1/auth/register", fromSignUp,
		invitationsClosedSignUp("tour-revoked@example.com", "Tour Revoked", "{{invite.revoked}}"))
	tr.step("register with a token no invitation has", anon, "POST /api/v1/auth/register", fromSignUp,
		invitationsClosedSignUp("tour-unknown@example.com", "Tour Unknown", "{{unknown.token}}"))
	fromSignUp2 := invitationsClosedFrom(invitationsClosedSignUpNet2)
	tr.step("register with the invitee's link again, now spent, and its address", anon, "POST /api/v1/auth/register",
		fromSignUp2, invitationsClosedSignUp("tour-invitee@example.com", "Tour Invitee", "{{invite.invitee}}"),
		note("a second address: the first spent its budget of five"))
	joinerReg := tr.step("register with the joiner's link to W2: the account joins W2 alone", anon,
		"POST /api/v1/auth/register", fromSignUp2,
		invitationsClosedSignUp("tour-joiner@example.com", "Tour Joiner", "{{invite.joiner.w2}}"))
	joiner := tr.adopt(joinerReg, "joiner", fmt.Sprintf("an account W2's link made (step %d); W's link is still "+
		"pending for it", joinerReg.step.n))
	stayerReg := tr.step("register with the stayer's link to W2", anon, "POST /api/v1/auth/register", fromSignUp2,
		invitationsClosedSignUp("tour-stayer@example.com", "Tour Stayer", "{{invite.stayer.w2}}"))
	stayer := tr.adopt(stayerReg, "stayer", fmt.Sprintf("an account W2's link made (step %d), then added to W "+
		"outright, while W's link for it is still pending", stayerReg.step.n))
	tr.join(stayer, w, "member")

	// Taking an invitation up, signed in.
	accept := func(title string, a *tourActor, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "POST /api/v1/auth/invitations/accept", opts...)
	}
	accept("the joiner takes up W's link: 200, and org.invitation_accepted", joiner,
		jsonBody(`{"token":"{{invite.joiner}}"}`), note("the event's actor is the joiner, whom the handler names "+
			"itself: the auth middleware does not run on /api/v1/auth/ paths, so the request carries no user for "+
			"Actor to name"))
	accept("the joiner presents the link again: spent", joiner, jsonBody(`{"token":"{{invite.joiner}}"}`))
	accept("the stayer, already in W, takes up its link: 200, already_member true, the role it holds, no event",
		stayer, jsonBody(`{"token":"{{invite.stayer}}"}`))
	accept("an account presents another address's live link: 403", e, jsonBody(`{"token":"{{invite.pending}}"}`))
	accept("an account presents a token no invitation has", e, jsonBody(`{"token":"{{unknown.token}}"}`))
	accept("take a link up with no session", anon, jsonBody(`{"token":"{{invite.pending}}"}`))
	worker := tr.bearerActor("worker", tr.setup("a worker key of the owner's personal workspace", o,
		"POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"), actingIn("{{owner.workspace}}"),
		jsonBody(`{"name":"tour worker"}`)).value("/key"), "a worker key of the owner's personal workspace, as a bearer")
	accept("take a link up with a worker key as the bearer: only the session cookie is read", worker,
		jsonBody(`{"token":"{{invite.pending}}"}`))
	accept("take a link up with no JSON Content-Type", joiner,
		rawBody("text/plain", []byte(tr.fill(`{"token":"{{invite.pending}}"}`))))
	accept("take a link up with a malformed body", joiner, jsonBody(`{`))
	revoke("revoke the invitation the invitee took up: 204, the row goes and the membership stays", o,
		"{{invitation.invitee}}")
	tr.step("W's pending invitations: only the one no one took up", o, "GET /api/v1/orgs/{id}/invitations", inW)

	// inviteLimiter: one bucket per inviting account (20). The invitee, an
	// admin of W, spends its own.
	tr.actIn(invitee, w)
	drainInvite := func(i int) tourOpt {
		return invitationsClosedInvite(fmt.Sprintf("tour-drain-%02d@example.com", i), "member")
	}
	for i := 1; i < 20; i++ {
		tr.probe(invitee, "POST /api/v1/orgs/{id}/invitations", inW, drainInvite(i))
	}
	invite("the invitee's 20th invitation: the last its budget allows", invitee, "tour-drain-20@example.com", "member",
		note("19 invitations, each to another address, went before it, as probes"))
	invite("its 21st: refused", invitee, "tour-drain-21@example.com", "member")
	invite("the owner invites all the same: the budget is the inviting account's", o, "tour-after@example.com",
		"member")

	// Q18: invitePreviewLimiter (60 per address) is spent by invitation
	// previews and share-link lookups alike.
	fromDrain := invitationsClosedFrom(invitationsClosedDrainNet)
	fromShare := invitationsClosedFrom(invitationsClosedShareNet)
	for i := 1; i < 60; i++ {
		tr.probe(anon, "POST /api/v1/auth/invitations/preview", fromDrain, jsonBody(`{"token":"{{unknown.token}}"}`))
	}
	tr.step("the 60th preview from one address: the last its bucket allows", anon,
		"POST /api/v1/auth/invitations/preview", fromDrain, jsonBody(`{"token":"{{invite.pending}}"}`),
		note("59 previews of an unknown token went before it, as probes"))
	tr.step("the 61st: refused, even for a live link", anon, "POST /api/v1/auth/invitations/preview", fromDrain,
		jsonBody(`{"token":"{{invite.pending}}"}`))
	for i := 1; i <= 60; i++ {
		tr.probe(anon, "GET /api/v1/public/share/{token}", fromShare, at("token", "{{share.token}}"))
	}
	tr.step("a preview from an address whose share-link lookups spent the bucket: refused (Q18)", anon,
		"POST /api/v1/auth/invitations/preview", fromShare, jsonBody(`{"token":"{{invite.pending}}"}`),
		note("60 lookups of a share-link token no link has went before it, as probes; each spends the bucket before "+
			"the token is read"))
	tr.step("a preview from an address none of that touched: its own bucket", anon,
		"POST /api/v1/auth/invitations/preview", fromPreview, jsonBody(`{"token":"{{invite.pending}}"}`))

	// Q18: POST /orgs/{id}/members invites through the same call site as
	// POST /orgs/{id}/invitations, so the invited admin's drained
	// inviteLimiter refuses it too.
	tr.step("the invited admin adds a person through the members route: refused, the budget its invitations spent "+
		"(Q18)", invitee, "POST /api/v1/orgs/{id}/members", inW, invitationsClosedInvite("tour-drain-22@example.com",
		"member"))
}
