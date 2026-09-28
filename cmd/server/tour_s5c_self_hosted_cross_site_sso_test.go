//go:build unix

package main

import (
	"fmt"
	"testing"
)

// TestTourS5cSelfHostedCrossSiteSso is the S5c tour's self-hosted,
// cross-site and single sign-on area (refactor plan §6.4 S5c, before M9 and
// M14, and for X8 and X12's effective limits; invariants I8 (the session,
// OIDC flow and Google state cookies' attributes under CROSS_SITE_COOKIES,
// and HSTS), I10 (org.invitation_accepted's actor), I13 (Q12's two fallback
// chains); quirks Q5, Q12 and Q18; OpenV REQ-18, REQ-95). Its golden is
// testdata/tour/s5c/self_hosted_cross_site_sso.json.
//
// The server boots as a self-hosted deployment might: three S4b profiles,
// self_hosted (OPENV_SELF_HOSTED), cross_site_cookies (CROSS_SITE_COOKIES) and
// registration_closed (OPENV_REGISTRATION=closed, so admin and owner register
// on a second server, as that profile's own sign-up does), and variables of
// the area's own: OPENV_LIMITS={"max_members":2}, so that a self-hosted
// refusal and its remedy are reachable; PUBLIC_URL=https://api.tour.example
// with FRONTEND_URL unset (Q12); Google's client id and secret, with the
// tour's stand-in for Google (newTourGoogle); CF-Connecting-IP as the
// client's address, which only the Q18 drain's steps send; and an OIDC
// identity provider, the tour's
// stand-in at https://idp.tour.example (newTourIdP), which answers discovery,
// its JWKS (an RSA key made per run) and a token endpoint signing an RS256
// id_token with the claims the area issues per authorization code, the nonce
// the login redirect set among them. The stand-in's first discovery request
// fails, as a provider that is down would (failDiscovery), and some codes
// get a wrong token answer (issueWrong).
//
// The area walks, in order:
//   - the sign-in methods (OIDC on, named TourSSO; registration closed);
//   - the self-hosted deployment's shared workspaces: two made one after the
//     other, both 201, since the self-host plan leaves max_shared_workspaces
//     open (Q5: each answer carries release_channel "" and locked false,
//     which GET /api/v1/orgs later reads as nightly and locked);
//   - the cross-site cookie re-run (I8): an invitation to W2, whose link is on
//     PUBLIC_URL (Q12's first chain: FRONTEND_URL, else PUBLIC_URL); a
//     registration without it, refused since registration is closed; the
//     registration its link opens (which publishes no event for the
//     invitation it accepts), and a sign-in, each setting openv_session
//     with Secure, SameSite=None and Partitioned; a sign-out clearing it with
//     the same attributes. HSTS, which cross-site cookies turn on, is in the
//     standard security headers every answer carries (the golden's
//     standard_security_headers);
//   - the self-host plan's limits: W's effective limits (every count open but
//     max_members, which OPENV_LIMITS sets to 2), a personal workspace's
//     members 1 over every layer; an invitation holding W's second seat; a
//     third person refused with the self-hosted remedy, which names
//     OPENV_LIMITS; the public plans with billing off, and W's billing
//     unavailable;
//   - OIDC sign-on against the stand-in: a start while the provider's
//     discovery fails (502), then the redirect to the provider's authorization
//     endpoint with its state and nonce in cookies (Secure, SameSite=None,
//     Partitioned) and the redirect_uri on PUBLIC_URL; each refusal of the
//     callback in the handler's order (the state, the nonce cookie, the code,
//     the token exchange, a token answer with no id_token, an id_token that
//     does not verify, signed by a key the provider does not publish or
//     issued to another client, the id_token's nonce, claims that do not
//     decode, the email claim, the provider's email_verified, an address a
//     password account holds); then a
//     sign-in that makes the account, accepts W's pending invitation for the
//     provider-verified address (org.invitation_accepted, actor user:<sso>,
//     which S6 had left to a black-box step) and lands on
//     http://localhost:3000 (Q12's second chain: OIDC's FrontendURL is
//     FRONTEND_URL, else localhost:3000, never PUBLIC_URL); the account, its
//     workspaces, W's members and invitations after it, a further seat
//     refused, the account's password change refused (409 no_password), and
//     a password sign-in to it refused as a wrong password is (401); an
//     address nobody invited signing in, since the registration policy is
//     never consulted for single sign-on; and the first account signing in
//     again, publishing nothing;
//   - Google sign-on on the same server: the start, whose state cookie is
//     Secure, SameSite=None and Partitioned and whose redirect_uri is on
//     PUBLIC_URL, and a callback that makes an account (the closed
//     registration is not consulted), sets its cross-site session cookie and
//     lands on http://localhost:3000 (Google's FrontendURL: FRONTEND_URL, else
//     localhost:3000, Q12's second chain);
//   - a share link, whose url is on PUBLIC_URL (the handler's FrontendURL:
//     FRONTEND_URL, else PUBLIC_URL, Q12's first chain);
//   - Q18: ssoIPLimiter drained through OIDC's start from an address of its
//     own and refusing OIDC's callback (sessions_auth drains it through
//     Google's).
//
// Nondeterminism: ids, sessions, tokens and minted times (the generic
// tokens); the OIDC state and nonce (captured from the login's cookies); a
// workspace slug's last 8 hex digits (the area's own pattern). The stand-in's
// requests carry the client's credentials (the test values tour-oidc and x)
// and the codes the area issued; the id_token the stand-in signs never
// reaches the golden, since the server never echoes it.
//
// Not pinned: a real identity provider's behaviour, beyond the requests the
// server sends the stand-in; the id_token's bytes; a self-hosted
// deployment's hosted runners and runner sessions (HOSTED_RUNNERS=off in the
// harness); billing with a provider, which OPENV_SELF_HOSTED turns off even
// when STRIPE_SECRET_KEY is set (S4b's self_hosted_billing profile).
func TestTourS5cSelfHostedCrossSiteSso(t *testing.T) {
	idp := newTourIdP("idp.tour.example", "tour-oidc")
	google := newTourGoogle()
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "self_hosted_cross_site_sso",
		about: "A self-hosted deployment with registration closed, serving a frontend on another site, with an OIDC " +
			"identity provider and Google: the session and flow cookies' cross-site attributes and HSTS, the self-host " +
			"plan's limits and remedy under an OPENV_LIMITS override, billing off, and OIDC and Google sign-on against " +
			"stand-ins, each OIDC refusal and sign-ins that accept a pending invitation and pass the closed " +
			"registration (Q5, Q12's two fallback chains at each of their sites, and Q18's ssoIPLimiter at OIDC's " +
			"call sites).",
		run:      func(tr *tour) { selfHostedCrossSiteSsoTour(tr, idp, google) },
		profiles: []string{"self_hosted", "cross_site_cookies", "registration_closed"},
		env: map[string]string{
			"OPENV_LIMITS":             `{"max_members":2}`,
			"PUBLIC_URL":               "https://api.tour.example",
			"OPENV_OIDC_ISSUER":        idp.issuer(),
			"OPENV_OIDC_CLIENT_ID":     "tour-oidc",
			"OPENV_OIDC_CLIENT_SECRET": "x",
			"OPENV_OIDC_NAME":          "TourSSO",
			"GOOGLE_CLIENT_ID":         "tour-google-client",
			"GOOGLE_CLIENT_SECRET":     "tour-google-secret",
			"OPENV_CLIENT_IP_HEADER":   "CF-Connecting-IP",
		},
		standIns: append([]*tourStandIn{selfHostedCrossSiteSsoProvider(idp)}, google.standIns()...),
	})
}

// selfHostedCrossSiteSsoProvider is the identity provider's stand-in, whose
// first discovery request answers 503, as a provider that is down would; the
// server retries discovery on each sign-on request until it succeeds.
func selfHostedCrossSiteSsoProvider(idp *tourIdP) *tourStandIn {
	idp.failDiscovery(1)
	return idp.standIn()
}

// selfHostedCrossSiteSsoCallback sends the OIDC callback once, with a query
// and flow cookies.
func selfHostedCrossSiteSsoCallback(tr *tour, title, q string, opts ...tourOpt) *tourResult {
	tr.t.Helper()
	return tr.step(title, tr.anon, "GET /api/v1/auth/oidc/callback", append([]tourOpt{
		once("each single sign-on request spends a token of the address's budget, and a code is exchanged each time"),
		query(q)}, opts...)...)
}

func selfHostedCrossSiteSsoTour(tr *tour, idp *tourIdP, google *tourGoogle) {
	o, anon := tr.owner, tr.anon
	tr.slugPattern()
	onceSSO := once("each single sign-on request spends a token of the address's budget")
	tr.headerPattern("Retry-After", `^(5[0-9]|60)$`, "<retry-after 60 s>", "Retry-After on the 21st single sign-on "+
		"start or callback from one address: ceil(60 s less the seconds since the first of them, ssoIPLimiter's "+
		"refill of 60 an hour), so 60 while they take under a second and no less than 50 within ten")

	// The sign-in methods.
	tr.step("the sign-in methods: OIDC on, under the name the deployment gives it, and registration closed", anon,
		"GET /api/v1/auth/config")

	// Shared workspaces on the self-host plan (Q5).
	w := tr.step("create a shared workspace: the answer says release_channel \"\" and locked false (Q5)", o,
		"POST /api/v1/orgs", jsonBody(`{"name":"Tour Shared"}`),
		note("GET /api/v1/orgs reads the same workspace as nightly and locked, with billing status \"none\" for "+
			"\"\" (a later step)")).capture("w", "/id")
	w2 := tr.step("create a second one: the self-host plan leaves max_shared_workspaces open", o, "POST /api/v1/orgs",
		jsonBody(`{"name":"Tour Second"}`)).capture("w2", "/id")

	// Cross-site cookies (I8), on a deployment whose registration is closed:
	// the invitation's link is the door.
	tr.actIn(o, w2)
	inv := tr.step("invite an address with no account to W2: the link is on PUBLIC_URL, since FRONTEND_URL is unset "+
		"(Q12's first chain)", o, "POST /api/v1/orgs/{id}/invitations", at("id", w2),
		jsonBody(`{"email":"tour-passworder@example.com","role":"member"}`))
	inv.captureMatch("passworder.invite", "/link", `invite=([0-9a-f]{64})`)
	tr.step("register without the link: registration is closed", anon, "POST /api/v1/auth/register",
		jsonBody(`{"email":"tour-passworder@example.com","password":"tour password 1","name":"Tour Passworder"}`))
	reg := tr.step("register with the link: the account joins W2, and its session cookie is Secure, SameSite=None "+
		"and Partitioned", anon, "POST /api/v1/auth/register",
		jsonBody(`{"email":"tour-passworder@example.com","password":"tour password 1","name":"Tour Passworder",`+
			`"invite_token":"{{passworder.invite}}"}`),
		note("the owner reads W2's events, and the sign-up publishes none for the invitation it accepts, where the "+
			"OIDC sign-in below publishes org.invitation_accepted (pinned as it is; the scouts' bug 4)"))
	pw := tr.adopt(reg, "passworder", fmt.Sprintf("an account a recorded registration made by invitation (step %d), "+
		"with a password; the identity provider later asserts its address", reg.step.n))
	login := tr.step("sign in: a new session, its cookie with the same attributes", anon, "POST /api/v1/auth/login",
		jsonBody(`{"email":"tour-passworder@example.com","password":"tour password 1"}`))
	second := tr.session(pw, login, "passworder.second",
		fmt.Sprintf("a second session of passworder's, from a recorded sign-in (step %d), which the area signs out", login.step.n))
	tr.step("sign the second session out: the cookie cleared with the same attributes", second, "POST /api/v1/auth/logout")
	tr.step("the session signed out", second, "GET /api/v1/auth/me")

	// The self-host plan, with OPENV_LIMITS's max_members over it.
	tr.actIn(o, w)
	tr.step("W's effective limits: the self-host plan's, every count open but max_members, which OPENV_LIMITS sets",
		o, "GET /api/v1/orgs/{id}/limits", at("id", w))
	tr.step("the owner's personal workspace: members 1 over every layer, OPENV_LIMITS's included", o,
		"GET /api/v1/orgs/{id}/limits", at("id", "{{owner.workspace}}"), actingIn("{{owner.workspace}}"))
	tr.step("invite an address with no account to W: the invitation holds W's second seat", o,
		"POST /api/v1/orgs/{id}/invitations", at("id", w), jsonBody(`{"email":"sso-user@example.com","role":"member"}`))
	tr.step("W's invitations: the pending one", o, "GET /api/v1/orgs/{id}/invitations", at("id", w))
	tr.step("add a third person: refused, the pending invitation counting as a seat, with the self-hosted remedy "+
		"naming OPENV_LIMITS", o, "POST /api/v1/orgs/{id}/members", at("id", w),
		jsonBody(`{"email":"tour-passworder@example.com","role":"member"}`))
	tr.step("the public plans: billing off on a self-hosted deployment", anon, "GET /api/v1/public/plans")
	tr.step("W's billing: unavailable", o, "GET /api/v1/orgs/{id}/billing", at("id", w))

	// OIDC sign-on against the stand-in identity provider.
	tr.step("start OIDC sign-on while the provider's discovery fails", anon, "GET /api/v1/auth/oidc/login", onceSSO)
	start := tr.step("start it again: discovery answers, and the redirect to the provider carries the state and "+
		"nonce its cookies hold, and a redirect_uri on PUBLIC_URL", anon, "GET /api/v1/auth/oidc/login", onceSSO)
	start.captureCookie("oidc.state", "openv_oidc_state")
	nonce := start.captureCookie("oidc.nonce", "openv_oidc_nonce")
	state := withCookie("openv_oidc_state", "{{oidc.state}}")
	nonceCookie := withCookie("openv_oidc_nonce", "{{oidc.nonce}}")
	verified := func(email, name string) map[string]any {
		return map[string]any{"nonce": nonce, "email": email, "email_verified": true, "name": name}
	}
	idp.issue("tour-code-sso", verified("sso-user@example.com", "Tour SSO User"))
	idp.issueWrong("tour-code-no-id-token", tourIdPNoIDToken, verified("sso-user@example.com", "Tour SSO User"))
	idp.issueWrong("tour-code-foreign-key", tourIdPForeignKey, verified("sso-user@example.com", "Tour SSO User"))
	idp.issueWrong("tour-code-other-client", tourIdPOtherAudience, verified("sso-user@example.com", "Tour SSO User"))
	idp.issue("tour-code-other-nonce", map[string]any{"nonce": "not-the-nonce", "email": "sso-user@example.com",
		"email_verified": true})
	idp.issue("tour-code-bad-claims", map[string]any{"nonce": nonce, "email": 42, "email_verified": true,
		"name": "Tour Bad Claims"})
	idp.issue("tour-code-no-email", map[string]any{"nonce": nonce, "email_verified": true, "name": "No Address"})
	idp.issue("tour-code-unverified", map[string]any{"nonce": nonce, "email": "sso-unverified@example.com",
		"email_verified": false, "name": "Tour Unverified"})
	idp.issue("tour-code-password", verified("tour-passworder@example.com", "Tour Passworder"))
	idp.issue("tour-code-uninvited", verified("sso-uninvited@example.com", "Tour Uninvited"))
	idp.issue("tour-code-sso-again", verified("sso-user@example.com", "Tour SSO User"))

	selfHostedCrossSiteSsoCallback(tr, "a state other than the cookie's", "state=not-the-state&code=tour-code-sso",
		state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "no nonce cookie", "state={{oidc.state}}&code=tour-code-sso", state)
	selfHostedCrossSiteSsoCallback(tr, "no code: refused after the flow cookies are cleared", "state={{oidc.state}}",
		state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "a code the provider refuses: the token exchange fails",
		"state={{oidc.state}}&code=tour-code-refused", state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "a token answer with no id_token",
		"state={{oidc.state}}&code=tour-code-no-id-token", state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an id_token signed by a key the provider's JWKS does not publish: it does not "+
		"verify (the first verification fetches the JWKS)", "state={{oidc.state}}&code=tour-code-foreign-key", state,
		nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an id_token issued to another client: it does not verify",
		"state={{oidc.state}}&code=tour-code-other-client", state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an id_token bound to another nonce",
		"state={{oidc.state}}&code=tour-code-other-nonce", state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an id_token whose claims do not decode: the email claim is a number",
		"state={{oidc.state}}&code=tour-code-bad-claims", state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an id_token with no email claim", "state={{oidc.state}}&code=tour-code-no-email",
		state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an address the provider does not assert as verified",
		"state={{oidc.state}}&code=tour-code-unverified", state, nonceCookie)
	selfHostedCrossSiteSsoCallback(tr, "an address a password account holds: never linked",
		"state={{oidc.state}}&code=tour-code-password", state, nonceCookie)
	ok := selfHostedCrossSiteSsoCallback(tr, "a verified address: the account is made, joins W by its pending "+
		"invitation, and is sent to http://localhost:3000 (Q12's second chain)",
		"state={{oidc.state}}&code=tour-code-sso", state, nonceCookie)
	sso := tr.adopt(ok, "sso", fmt.Sprintf("the account the OIDC sign-on made (step %d)", ok.step.n))
	tr.step("the account the sign-on made", sso, "GET /api/v1/auth/me")
	tr.step("its workspaces: its personal one and W, each on the nightly channel, locked (Q5)", sso, "GET /api/v1/orgs")
	tr.step("W's members: the owner and the account", o, "GET /api/v1/orgs/{id}/members", at("id", w))
	tr.step("W's invitations: none pending", o, "GET /api/v1/orgs/{id}/invitations", at("id", w))
	tr.step("invite another person: refused, W's two seats taken", o, "POST /api/v1/orgs/{id}/invitations", at("id", w),
		jsonBody(`{"email":"sso-third@example.com","role":"member"}`))
	tr.step("the account changes its password: it has none", sso, "PUT /api/v1/me/password",
		jsonBody(`{"current_password":"anything at all","new_password":"tour password 2"}`))
	tr.step("sign in to the account with a password: refused as any wrong password is, since it is bound to the "+
		"identity provider it was made through (OpenV REQ-18)", anon, "POST /api/v1/auth/login",
		jsonBody(`{"email":"sso-user@example.com","password":"tour password 1"}`))
	un := selfHostedCrossSiteSsoCallback(tr, "an address nobody invited: the account is made, since single sign-on "+
		"never consults the closed registration", "state={{oidc.state}}&code=tour-code-uninvited", state, nonceCookie)
	uninvited := tr.adopt(un, "uninvited", fmt.Sprintf("the account an uninvited OIDC sign-on made (step %d)", un.step.n))
	tr.step("its workspaces: its personal one alone", uninvited, "GET /api/v1/orgs")
	selfHostedCrossSiteSsoCallback(tr, "the first address signs in again: a new session, and nothing published",
		"state={{oidc.state}}&code=tour-code-sso-again", state, nonceCookie)

	// Google sign-on on the same server (I8, Q12): its state cookie under
	// cross-site cookies, its redirect_uri on PUBLIC_URL, and its landing on
	// FRONTEND_URL, else localhost:3000, never PUBLIC_URL. The area's
	// requests from 127.0.0.1 have spent 17 of the address's single sign-on
	// budget (20) by now; these two spend the 18th and 19th.
	google.user("tour-google-sso", map[string]any{"email": "google-user@example.com", "email_verified": true,
		"name": "Tour Google"})
	gstart := tr.step("start Google sign-on: its state cookie is Secure, SameSite=None and Partitioned, and the "+
		"redirect_uri is on PUBLIC_URL (I8, Q12)", anon, "GET /api/v1/auth/google", onceSSO)
	gstart.captureCookie("google.state", "openv_oauth_state")
	gok := tr.step("Google's callback: the account is made (single sign-on never consults the closed registration), "+
		"its session cookie cross-site, and it is sent to http://localhost:3000, not PUBLIC_URL (Q12's second chain)",
		anon, "GET /api/v1/auth/google/callback", onceSSO,
		query("state={{google.state}}&code=tour-google-sso"), withCookie("openv_oauth_state", "{{google.state}}"))
	tr.adopt(gok, "googler", fmt.Sprintf("the account Google's sign-on made (step %d)", gok.step.n))

	// The handler's own FrontendURL, which share links are built on:
	// FRONTEND_URL, else PUBLIC_URL (Q12's first chain), unlike the sign-on
	// landings above.
	tr.setup("project P in W", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour Share"}`),
		expect(201)).capture("p", "/id")
	tr.step("mint a share link: its url is on PUBLIC_URL, the handler's FrontendURL with FRONTEND_URL unset (Q12's "+
		"first chain)", o, "POST /api/v1/projects/{id}/share-links", at("id", "{{p}}"), jsonBody(`{"role":"public"}`))

	// Q18: ssoIPLimiter is spent by OIDC's start and its callback too (20),
	// from an address of the area's own (CF-Connecting-IP, which only these
	// steps send).
	fromQ := withHeader("CF-Connecting-IP", "203.0.113.8")
	for i := 1; i < 20; i++ {
		tr.probe(anon, "GET /api/v1/auth/oidc/login", fromQ)
	}
	tr.step("the 20th OIDC sign-on start from address Q: the last its budget allows", anon,
		"GET /api/v1/auth/oidc/login", fromQ, onceSSO, note("19 starts went before it, as probes"))
	selfHostedCrossSiteSsoCallback(tr, "the callback from Q: refused, the bucket the starts spent (Q18)",
		"state={{oidc.state}}&code=tour-code-sso-again", state, nonceCookie, fromQ)
}
