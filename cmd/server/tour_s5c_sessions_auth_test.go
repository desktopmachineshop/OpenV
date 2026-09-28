//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestTourS5cSessionsAuth is the S5c tour's sessions and sign-in area
// (refactor plan §6.4 S5c, before M9 and M14; invariants I3, I4, I5, I8
// (the openv_session and OAuth state cookies, Cache-Control, Retry-After),
// I13 (Q12's fallbacks); quirks Q1, Q12, Q18 and Q19; OpenV REQ-18, REQ-95,
// REQ-99, REQ-143, REQ-156). Its golden is testdata/tour/s5c/sessions_auth.json.
// It is the worked example the other S5c areas copy: sessions that recorded
// steps make (tour.adopt, tour.session), a signed-in request with no
// X-Org-ID (noOrgHeader), a flow cookie (withCookie), a stand-in the
// recording proxy answers itself (Google), values elided because every
// release changes them (elide), and rate-limit buckets drained from
// addresses of their own.
//
// The area walks, in order: the public and meta routes (/health, the build,
// the release feed and the release notes, whose history and stable marker
// RELEASE_NOTES.md changes with every promotion and cut, so they are elided);
// the sign-in configuration and policy; registration (an account, each
// refusal in the handler's order, an invite token no invitation has, and the
// address's budget of five; the account's name carries <, > and &, which each
// answer that holds it writes JSON-escaped, I4); sign-in (a session of its
// own, each refusal,
// the address in capitals); the account (/auth/me reads the cookie alone),
// and sign-out (the cookie cleared, the session dead, another session of
// the account alive); then how the server resolves the workspace of a
// signed-in request that sends no X-Org-ID (S5a left it to S5c): the
// header when it names a workspace the account is in, else the session's
// active workspace (POST /orgs/{id}/activate binds the session), else the
// account's default (PUT /me/default-workspace, REQ-156), else the personal
// one, a header naming anything else falling through silently, and a default
// that no longer holds once the account leaves the workspace (a recorded
// step, whose org.member_removed is the one event the area's steps publish;
// the notifications the notifier writes for it after the answer are read by
// no step), and the account's deleted workspaces, none, written null (Q14);
// then Google sign-on against the tour's stand-in for Google (the redirect
// and its state cookie, each refusal of the callback, a code Google refuses,
// an account Google makes and signs in, an unverified address, an address a
// password account holds, and a password sign-in to the account Google
// made, refused as a wrong password is); and last the shared rate-limit
// buckets (Q18), each probed at its real call sites from an address of its
// own: authIPLimiter, which
// sign-in, email verification and password-reset confirmation all spend,
// drained through each of them in turn and then refused by the others;
// authAccountLimiter, which failed sign-ins and a wrong current password on
// PUT /me/password both spend, in both directions; and ssoIPLimiter, which
// Google's start and callback share. After them come the callback's two
// userinfo refusals (an answer that does not decode, and an access token
// answered already expired, so the fetch fails before it is sent), and what
// only a server without a mailer answers: a reset request's 409
// reset_email_unavailable (from a drained address too, since the refusal
// comes before the bucket), resend's and address change's 400 (verification
// is not required), a platform admin's reset link for the account Google
// made (409 no_password), and the push configuration with no VAPID keys.
//
// The server trusts CF-Connecting-IP as the client's address (env), which
// only the steps that name a network send, so each bucket drained here is a
// bucket of its own, from a documentation address (RFC 5737), and the
// tour's own requests from 127.0.0.1 keep theirs. The authIPLimiter and
// ssoIPLimiter drains are some twenty or thirty requests in well under a
// second, and each 429 comes right after its drain; the registration budget
// is spent by recorded steps, two of them hashing a password, and the
// account budget by failed sign-ins that each check a hash. So a
// Retry-After is the bucket's refill interval while the drain takes under a
// second, and the area's header patterns allow ten or twenty seconds more, so
// that a slow or loaded runner does not fail the golden. Google's client id and secret are set, and
// PUBLIC_URL and FRONTEND_URL are not, so the redirect_uri Google is sent is
// the server's own address (http://localhost:<port>, written URL-escaped)
// and the callback lands on http://localhost:3000: the defaults at the end
// of Q12's two fallback chains, which with neither variable set read alike
// (the SSO area, with PUBLIC_URL set, tells the two chains apart).
//
// Nondeterminism: ids, sessions, tokens and minted times (the generic
// tokens); a workspace slug's last 8 hex digits, which are its id's first
// eight (the area's own pattern); the release feed's and notes' contents
// (elided with their structure; internal/api's TestGetReleaseBytes pins
// that structure over fixed notes); four Retry-After countdowns. Not pinned: the session's idle and
// absolute expiry (time-bound), the OIDC sign-on's success (the SSO area's,
// against an identity provider stand-in), and Google's own consent screen.
func TestTourS5cSessionsAuth(t *testing.T) {
	google := newTourGoogle()
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "sessions_auth",
		about: "Sessions and sign-in: registration, login, logout and the account, the workspace a signed-in request " +
			"with no X-Org-ID resolves to, the default workspace, Google sign-on against a stand-in for Google, the " +
			"public meta routes, and the rate-limit buckets sign-in, verification, reset and sign-on share (Q18).",
		run: func(tr *tour) { sessionsAuthTour(tr, google) },
		env: map[string]string{
			"OPENV_CLIENT_IP_HEADER": "CF-Connecting-IP",
			"GOOGLE_CLIENT_ID":       "tour-google-client",
			"GOOGLE_CLIENT_SECRET":   "tour-google-secret",
		},
		standIns: google.standIns(),
	})
}

// The addresses (RFC 5737 documentation ranges) the area's steps send as
// CF-Connecting-IP, each a rate-limit bucket of its own.
const (
	sessionsAuthSignUpNet = "198.51.100.10" // the registrations and the register budget
	sessionsAuthLoginNet  = "198.51.100.20" // the recorded sign-ins
	sessionsAuthGoogleNet = "198.51.100.30" // Google sign-on
	sessionsAuthNetA      = "203.0.113.1"   // authIPLimiter drained through email verification
	sessionsAuthNetB      = "203.0.113.2"   // ... through password-reset confirmation
	sessionsAuthNetC      = "203.0.113.3"   // ... through sign-in
	sessionsAuthNetD      = "203.0.113.4"   // an address none of that touched
	sessionsAuthNetE      = "203.0.113.5"   // failed sign-ins of one account
	sessionsAuthNetF      = "203.0.113.6"   // that account's reverse direction
	sessionsAuthNetG      = "203.0.113.7"   // ssoIPLimiter drained through Google's start
)

// sessionsAuthFrom sends a request from an address of the area's.
func sessionsAuthFrom(net string) tourOpt { return withHeader("CF-Connecting-IP", net) }

// sessionsAuthCredentials is a sign-in body.
func sessionsAuthCredentials(email, password string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}

// sessionsAuthDrain spends all but the last of an address's authIPLimiter
// tokens (burst 30) with probes, so that the next request is the last the
// bucket lets through.
func sessionsAuthDrain(tr *tour, route string, net string, body func(i int) string) {
	for i := 1; i < 30; i++ {
		tr.probe(tr.anon, route, sessionsAuthFrom(net), jsonBody(body(i)))
	}
}

func sessionsAuthTour(tr *tour, google *tourGoogle) {
	o, anon := tr.owner, tr.anon
	tr.remember("unknown.token", strings.Repeat("0123456789abcdef", 4))
	tr.slugPattern()
	tr.headerPattern("Retry-After", `^(3[45][0-9]|360)$`, "<retry-after 360 s>", "Retry-After on the sixth "+
		"registration from one address: ceil(360 s less the seconds since the first of the six, registerIPLimiter's "+
		"refill of 10 an hour), so 360 while they take under a second and no less than 340 within twenty (the six "+
		"are recorded steps, two of them hashing a password, not a back-to-back drain)")
	tr.headerPattern("Retry-After", `^(2[0-9]|30)$`, "<retry-after 30 s>", "Retry-After on the 31st sign-in, "+
		"verification or reset attempt from one address: authIPLimiter refills 120 an hour, as above (no less than "+
		"20 within ten)")
	tr.headerPattern("Retry-After", `^(1[67][0-9]|180)$`, "<retry-after 180 s>", "Retry-After once one account "+
		"has failed five times: authAccountLimiter refills 20 an hour, as above (no less than 160 within twenty: "+
		"each failure checks a password hash)")
	tr.headerPattern("Retry-After", `^(5[0-9]|60)$`, "<retry-after 60 s>", "Retry-After on the 21st single sign-on "+
		"start or callback from one address: ssoIPLimiter refills 60 an hour, as above (no less than 50 within ten)")

	// Public and meta routes.
	tr.step("the health check", anon, "GET /health")
	tr.step("the commit this binary names: none, since the harness sets no OPENV_BUILD_SHA", anon, "GET /api/v1/public/build")
	tr.step("the release feed, which dedicated deployments poll, cached publicly for five minutes", anon,
		"GET /api/v1/public/release",
		elide("/stable", "<stable release>", len(`""`), len(`"99.99.99"`), "the newest stable release, \"\" until the monthly cut designates one"),
		elide("/stable_since", "<stable since>", len(`""`), len(`"2026-01-01"`), "the day it was designated, \"\" until then"))
	tr.step("the release notes, with no session", anon, "GET /api/v1/release")
	tr.step("the release notes a member reads: the running release and every earlier one, never cached", o,
		"GET /api/v1/release",
		elide("/notes", "<the release's notes>", len("[]"), tourNoMore, "RELEASE_NOTES.md's current release, which every promotion replaces"),
		elide("/categories", "<the release's notes by group>", len("[]"), tourNoMore, "the same, grouped"),
		elide("/markdown", "<the release's section>", len(`""`), tourNoMore, "the same, as written"),
		elide("/releases", "<every released version>", compressFloor, tourNoMore, "the history, newest first, which every promotion "+
			"adds to (far over 1,400 bytes: the answer is always compressed)"),
		elide("/stable", "<the stable release or null>", len("null"), tourNoMore, "null until the monthly cut designates one"))

	// The sign-in methods and the registration policy.
	tr.step("the sign-in methods: Google on, OIDC off, no mail server", anon, "GET /api/v1/auth/config")
	tr.step("the registration policy and the password rule", anon, "GET /api/v1/auth/policy")
	tr.step("OIDC sign-in, not configured here", anon, "GET /api/v1/auth/oidc/login")
	tr.step("the OIDC callback, not configured here", anon, "GET /api/v1/auth/oidc/callback")

	// Registration, from an address of its own: the handler decodes, then
	// spends the address's budget (5), then applies the policy, then
	// registers.
	fromSignUp := sessionsAuthFrom(sessionsAuthSignUpNet)
	reg := tr.step("register an account: the account, and a session cookie", anon, "POST /api/v1/auth/register",
		fromSignUp, jsonBody(`{"email":"Tour-Newcomer@Example.com ","password":"tour password 1","name":" Tour <Newcomer> & Co "}`),
		note("the address is stored trimmed and lower-cased, the name trimmed, and the answer writes its <, > and & "+
			"as the JSON escapes of U+003C, U+003E and U+0026, as encoding/json does by default"))
	newcomer := tr.adopt(reg, "newcomer", fmt.Sprintf("the account a recorded registration made (step %d)", reg.step.n))
	tr.step("register the same address again", anon, "POST /api/v1/auth/register", fromSignUp,
		jsonBody(`{"email":"tour-newcomer@example.com","password":"tour password 1","name":"Again"}`))
	tr.step("register with a password under 8 characters", anon, "POST /api/v1/auth/register", fromSignUp,
		jsonBody(`{"email":"tour-short@example.com","password":"short","name":"Short"}`))
	tr.step("register with a malformed body: refused before the address's budget is spent", anon,
		"POST /api/v1/auth/register", fromSignUp, jsonBody(`{`))
	tr.step("register with no address", anon, "POST /api/v1/auth/register", fromSignUp,
		jsonBody(`{"email":"not-an-address","password":"tour password 1"}`))
	tr.step("register with an invite token no invitation has: the account is made all the same, and the answer "+
		"says the token was invalid", anon, "POST /api/v1/auth/register", fromSignUp,
		jsonBody(`{"email":"tour-uninvited@example.com","password":"tour password 1","name":"Tour Uninvited",`+
			`"invite_token":"{{unknown.token}}"}`))
	tr.step("a sixth registration from the address: its budget of five is spent", anon, "POST /api/v1/auth/register",
		fromSignUp, jsonBody(`{"email":"tour-sixth@example.com","password":"tour password 1","name":"Sixth"}`))

	// Sign-in.
	fromLogin := sessionsAuthFrom(sessionsAuthLoginNet)
	login := tr.step("sign in: the account, and a new session", anon, "POST /api/v1/auth/login", fromLogin,
		sessionsAuthCredentials("tour-newcomer@example.com", tourPassword))
	second := tr.session(newcomer, login, "newcomer.second",
		fmt.Sprintf("a second session of newcomer's, from a recorded sign-in (step %d), which the area signs out", login.step.n))
	tr.step("sign in with the address in capitals: the same account", anon, "POST /api/v1/auth/login", fromLogin,
		sessionsAuthCredentials("TOUR-NEWCOMER@EXAMPLE.COM", tourPassword))
	tr.step("sign in with a wrong password", anon, "POST /api/v1/auth/login", fromLogin,
		sessionsAuthCredentials("tour-newcomer@example.com", "not the password"))
	tr.step("sign in with an address no account has", anon, "POST /api/v1/auth/login", fromLogin,
		sessionsAuthCredentials("tour-nobody@example.com", tourPassword))
	tr.step("sign in with a malformed body", anon, "POST /api/v1/auth/login", fromLogin, jsonBody(`{`))

	// The account and sign-out.
	worker := tr.bearerActor("worker", tr.setup("a worker key of the owner's personal workspace", o,
		"POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"), jsonBody(`{"name":"tour worker"}`)).value("/key"), "a worker key of the owner's personal workspace, as a bearer")
	tr.step("the signed-in account", newcomer, "GET /api/v1/auth/me")
	tr.step("the account with no session", anon, "GET /api/v1/auth/me")
	tr.step("the account with a worker key as the bearer: only the cookie is read", worker, "GET /api/v1/auth/me")
	tr.step("sign the second session out: 204, and a cookie that clears it", second, "POST /api/v1/auth/logout")
	tr.step("the account, from the session signed out", second, "GET /api/v1/auth/me")
	tr.step("a protected route from the session signed out: the middleware's 401", second, "GET /api/v1/orgs")
	tr.step("the first session is still signed in", newcomer, "GET /api/v1/auth/me")
	tr.step("sign out with no session: 204 all the same", anon, "POST /api/v1/auth/logout")

	// The workspace of a request with no X-Org-ID. The owner shares W with
	// the member, whose sessions of its own show the order the server
	// resolves in.
	w := tr.sharedWorkspace("w", "Tour Shared")
	m := tr.register("member", "Tour Member", "a member of W; its sessions show the workspace a request with no "+
		"X-Org-ID resolves to")
	tr.join(m, w, "member")
	tr.step("the owner's workspaces with no X-Org-ID: the server resolves one", o, "GET /api/v1/orgs", noOrgHeader())
	mLogin := func(title string) *tourResult {
		return tr.step(title, anon, "POST /api/v1/auth/login", fromLogin, sessionsAuthCredentials(m.email, tourPassword))
	}
	r2 := mLogin("the member signs in again: a session of its own")
	m2 := tr.session(m, r2, "member.second", fmt.Sprintf("a session of the member's from a recorded sign-in (step %d)", r2.step.n))
	tr.step("with no X-Org-ID: the personal workspace, since the session has none active and the member chose no default",
		m2, "GET /api/v1/orgs", noOrgHeader())
	tr.step("make W the session's active workspace", m2, "POST /api/v1/orgs/{id}/activate", at("id", "{{w}}"))
	tr.step("with no X-Org-ID: W, the session's", m2, "GET /api/v1/orgs", noOrgHeader())
	tr.step("naming its personal workspace: the header wins", m2, "GET /api/v1/orgs")
	tr.step("naming a workspace that does not exist: the session's, silently", m2, "GET /api/v1/orgs", actingIn("{{phantom}}"))
	tr.step("naming a workspace it is not in: the same", m2, "GET /api/v1/orgs", actingIn("{{owner.workspace}}"))
	tr.step("naming something that is not an id: the same", m2, "GET /api/v1/orgs", actingIn("not-a-workspace"))
	tr.step("the workspace's members with no X-Org-ID: W's", m2, "GET /api/v1/users", noOrgHeader())
	tr.step("make a workspace it is not in active", m2, "POST /api/v1/orgs/{id}/activate", at("id", "{{owner.workspace}}"))
	tr.step("make a workspace active with a worker key: no account, the guard's 401", worker,
		"POST /api/v1/orgs/{id}/activate", at("id", "{{owner.workspace}}"))

	// The default workspace (REQ-156).
	tr.step("the member's default workspace: none chosen", m, "GET /api/v1/me/default-workspace")
	tr.step("choose W", m, "PUT /api/v1/me/default-workspace", jsonBody(`{"org_id":"{{w}}"}`))
	tr.step("choose the personal workspace: stored as none", m, "PUT /api/v1/me/default-workspace",
		jsonBody(`{"org_id":"{{member.workspace}}"}`))
	tr.step("choose a workspace that does not exist", m, "PUT /api/v1/me/default-workspace",
		jsonBody(`{"org_id":"{{phantom}}"}`))
	tr.step("choose a workspace the member is not in: the same answer", m, "PUT /api/v1/me/default-workspace",
		jsonBody(`{"org_id":"{{owner.workspace}}"}`))
	tr.step("choose something that is not an id: the lookup's error is a 500 (Q19)", m,
		"PUT /api/v1/me/default-workspace", jsonBody(`{"org_id":"not-a-workspace"}`))
	tr.step("choose with a malformed body", m, "PUT /api/v1/me/default-workspace", jsonBody(`{`))
	tr.step("choose W again", m, "PUT /api/v1/me/default-workspace", jsonBody(`{"org_id":" {{w}} "}`),
		note("the id is trimmed"))
	tr.step("the member's default workspace: W", m, "GET /api/v1/me/default-workspace")
	tr.step("the default workspace with a worker key: the handler's 401", worker, "GET /api/v1/me/default-workspace")
	tr.step("choose with no session: the middleware's 401", anon, "PUT /api/v1/me/default-workspace",
		jsonBody(`{"org_id":"{{w}}"}`))
	r3 := mLogin("the member signs in once more")
	m3 := tr.session(m, r3, "member.third", fmt.Sprintf("a session of the member's from a recorded sign-in (step %d)", r3.step.n))
	tr.step("a fresh session with no X-Org-ID: W, the member's default (REQ-156)", m3, "GET /api/v1/orgs", noOrgHeader())
	tr.step("the member leaves W: org.member_removed, self true", m, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{w}}", "userId", "{{member}}"))
	tr.step("the default no longer holds: the personal workspace", m3, "GET /api/v1/orgs", noOrgHeader())
	tr.step("nor does the other session's active W", m2, "GET /api/v1/orgs", noOrgHeader())
	tr.step("the stored default is still W", m, "GET /api/v1/me/default-workspace")
	tr.step("the member's deleted workspaces: none, written null (Q14)", m3, "GET /api/v1/orgs", noOrgHeader(),
		query("deleted=true"))
	tr.step("the workspace's members with no X-Org-ID: the personal workspace's", m3, "GET /api/v1/users", noOrgHeader())

	// Google sign-on, against the tour's stand-in for Google's token and
	// userinfo endpoints. The callback is sent once: each request spends a
	// token of the address's single sign-on budget, and a code is
	// exchanged each time.
	fromGoogle := sessionsAuthFrom(sessionsAuthGoogleNet)
	onceSSO := once("each single sign-on request spends a token of the address's budget")
	google.user("tour-code-verified", map[string]any{"email": "Tour-Googler@Example.com", "email_verified": true,
		"name": "Tour Googler", "picture": "https://lh3.googleusercontent.com/a/tour-googler"})
	google.user("tour-code-unverified", map[string]any{"email": "tour-unverified@example.com", "email_verified": false,
		"name": "Tour Unverified"})
	google.user("tour-code-password", map[string]any{"email": "tour-newcomer@example.com", "email_verified": true,
		"name": "Tour Newcomer"})
	start := tr.step("start Google sign-on: a redirect to Google's consent screen, its state in a cookie", anon,
		"GET /api/v1/auth/google", fromGoogle, onceSSO)
	start.captureCookie("google.state", "openv_oauth_state")
	callback := func(title, q string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "GET /api/v1/auth/google/callback", append([]tourOpt{fromGoogle, onceSSO,
			query(q)}, opts...)...)
	}
	state := withCookie("openv_oauth_state", "{{google.state}}")
	callback("the callback with no state cookie", "state={{google.state}}&code=tour-code-verified")
	callback("a state other than the cookie's", "state=another&code=tour-code-verified", state)
	callback("no code", "state={{google.state}}", state)
	callback("a code Google refuses: the token exchange fails", "state={{google.state}}&code=tour-code-refused", state)
	ok := callback("a code Google accepts: the account is made and signed in, and sent to the frontend",
		"state={{google.state}}&code=tour-code-verified", state)
	googler := tr.adopt(ok, "googler", fmt.Sprintf("the account Google's sign-on made (step %d)", ok.step.n))
	tr.step("the account Google made", googler, "GET /api/v1/auth/me")
	callback("an address Google does not vouch for", "state={{google.state}}&code=tour-code-unverified", state)
	callback("an address a password account holds: never linked", "state={{google.state}}&code=tour-code-password", state)
	tr.step("sign in with a password to the account Google made: refused as any wrong password is, since it is "+
		"bound to Google (OpenV REQ-18)", anon, "POST /api/v1/auth/login",
		jsonBody(`{"email":"tour-googler@example.com","password":"tour password 1"}`))

	// Q18: authIPLimiter is spent by sign-in, email verification and
	// password-reset confirmation alike. Each address's bucket (30) is
	// drained through one of them, and the others are then refused.
	bogusToken := func(int) string { return `{"token":"{{unknown.token}}"}` }
	bogusReset := func(int) string { return `{"token":"{{unknown.token}}","new_password":"tour password 2"}` }
	unknownLogin := func(i int) string {
		return fmt.Sprintf(`{"email":"tour-drain-%02d@example.com","password":"tour password 1"}`, i)
	}
	fromA, fromB, fromC := sessionsAuthFrom(sessionsAuthNetA), sessionsAuthFrom(sessionsAuthNetB), sessionsAuthFrom(sessionsAuthNetC)
	sessionsAuthDrain(tr, "POST /api/v1/auth/verify-email", sessionsAuthNetA, bogusToken)
	tr.step("the 30th email verification from address A: the last its sign-in budget allows (Q18)", anon,
		"POST /api/v1/auth/verify-email", fromA, jsonBody(bogusToken(30)),
		note("29 verifications with the same unknown token went before it, as probes"))
	tr.step("sign in from A, with the right password: refused, the bucket verification spent", anon,
		"POST /api/v1/auth/login", fromA, sessionsAuthCredentials("tour-newcomer@example.com", tourPassword))
	tr.step("confirm a password reset from A: refused by the same bucket", anon, "POST /api/v1/auth/password-reset/confirm",
		fromA, jsonBody(bogusReset(0)))
	tr.step("verify from A with a malformed body: decoded before the bucket, so 400", anon,
		"POST /api/v1/auth/verify-email", fromA, jsonBody(`{`))
	sessionsAuthDrain(tr, "POST /api/v1/auth/password-reset/confirm", sessionsAuthNetB, bogusReset)
	tr.step("the 30th reset confirmation from address B: the last its budget allows", anon,
		"POST /api/v1/auth/password-reset/confirm", fromB, jsonBody(bogusReset(30)),
		note("29 confirmations with the same unknown token went before it, as probes"))
	tr.step("verify from B: refused, the bucket reset confirmation spent", anon, "POST /api/v1/auth/verify-email",
		fromB, jsonBody(bogusToken(0)))
	tr.step("sign in from B: refused too", anon, "POST /api/v1/auth/login", fromB,
		sessionsAuthCredentials("tour-newcomer@example.com", tourPassword))
	sessionsAuthDrain(tr, "POST /api/v1/auth/login", sessionsAuthNetC, unknownLogin)
	tr.step("the 30th sign-in from address C: the last its budget allows", anon, "POST /api/v1/auth/login", fromC,
		jsonBody(unknownLogin(30)), note("29 sign-ins, each to another unknown address (so that no account's own "+
			"budget closes first), went before it, as probes"))
	tr.step("confirm a reset from C: refused, the bucket sign-in spent", anon, "POST /api/v1/auth/password-reset/confirm",
		fromC, jsonBody(bogusReset(0)))
	tr.step("verify from C: refused too", anon, "POST /api/v1/auth/verify-email", fromC, jsonBody(bogusToken(0)))
	tr.step("verify from address D: its own bucket, untouched", anon, "POST /api/v1/auth/verify-email",
		sessionsAuthFrom(sessionsAuthNetD), jsonBody(bogusToken(0)))

	// Q18: authAccountLimiter is spent by failed sign-ins and by a wrong
	// current password on PUT /me/password, for the account (5).
	locked := tr.register("locked", "Tour Locked", "an account whose own budget failed sign-ins spend")
	fromE := sessionsAuthFrom(sessionsAuthNetE)
	for i := 1; i < 5; i++ {
		tr.probe(anon, "POST /api/v1/auth/login", fromE, sessionsAuthCredentials(locked.email, "not the password"))
	}
	tr.step("the fifth failed sign-in to one account", anon, "POST /api/v1/auth/login", fromE,
		sessionsAuthCredentials(locked.email, "not the password"), note("four failed sign-ins went before it, as probes"))
	tr.step("the account changes its password, with the right current one: refused, since failed sign-ins spent "+
		"the account's budget (Q18)", locked, "PUT /api/v1/me/password",
		jsonBody(`{"current_password":"tour password 1","new_password":"tour password 2"}`))
	tr.step("the account signs in with the right password: refused as well", anon, "POST /api/v1/auth/login", fromE,
		sessionsAuthCredentials(locked.email, tourPassword))
	reverse := tr.register("reverse", "Tour Reverse", "an account whose own budget wrong current passwords spend")
	wrongCurrent := jsonBody(`{"current_password":"not the password","new_password":"tour password 2"}`)
	for i := 1; i < 5; i++ {
		tr.probe(reverse, "PUT /api/v1/me/password", wrongCurrent)
	}
	tr.step("the fifth wrong current password", reverse, "PUT /api/v1/me/password", wrongCurrent,
		note("four went before it, as probes"))
	tr.step("the account signs in with the right password: refused, since the wrong current passwords spent its "+
		"budget (Q18)", anon, "POST /api/v1/auth/login", sessionsAuthFrom(sessionsAuthNetF),
		sessionsAuthCredentials(reverse.email, tourPassword))

	// Q18: ssoIPLimiter is spent by Google's start and its callback (20).
	fromG := sessionsAuthFrom(sessionsAuthNetG)
	for i := 1; i < 20; i++ {
		tr.probe(anon, "GET /api/v1/auth/google", fromG)
	}
	tr.step("the 20th Google sign-on start from address G: the last its budget allows", anon,
		"GET /api/v1/auth/google", fromG, onceSSO, note("19 starts went before it, as probes"))
	tr.step("the callback from G: refused, the bucket the starts spent (Q18)", anon, "GET /api/v1/auth/google/callback",
		fromG, onceSSO, query("state={{google.state}}&code=tour-code-verified"), state)

	// Google's two userinfo refusals, from the Google address (its 10th and
	// 11th single sign-on requests of 20).
	google.user("tour-code-undecodable", map[string]any{"email": 42, "email_verified": true})
	google.expiredToken("tour-code-expired")
	callback("Google's userinfo does not decode: the email is a number", "state={{google.state}}&code=tour-code-undecodable",
		state)
	callback("an access token Google answers already expired: the userinfo fetch fails before a request is sent",
		"state={{google.state}}&code=tour-code-expired", state)

	// What only a server without a mailer answers (this area's has none).
	tr.step("ask for a reset link on a server that cannot send mail: 409 reset_email_unavailable", anon,
		"POST /api/v1/auth/password-reset", jsonBody(`{"email":"tour-newcomer@example.com"}`))
	tr.step("the same from address A, whose authIPLimiter is drained: still 409, since with no mailer a reset request "+
		"is refused before it spends a token (Q18)", anon, "POST /api/v1/auth/password-reset", fromA,
		jsonBody(`{"email":"tour-newcomer@example.com"}`))
	tr.step("resend the verification mail on a server that requires no verification: 400", o,
		"POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("change the address to verify on that server: the same 400", o, "POST /api/v1/auth/verify-email/change",
		jsonBody(`{"email":"tour-elsewhere@example.com"}`))
	tr.step("a platform admin's reset link for the account Google made: 409 no_password", tr.admin,
		"POST /api/v1/admin/users/{id}/password-reset", at("id", "{{googler}}"))
	tr.step("the push configuration on a server with no VAPID key pair: off", o, "GET /api/v1/me/push/config")
}
