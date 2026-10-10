//go:build unix

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestTourS5cMailPasswordAdmin is the S5c tour's mail, password and platform
// admin area (refactor plan §6.4 S5c, before M9; invariants I3, I4, I5, I8
// (Retry-After, the avatar's Cache-Control and Content-Disposition), I16
// (the avatar's file naming), I18 (that a mail went, to whom, with which
// link; S10 owns the wording); quirks Q18 and Q19; OpenV REQ-18, REQ-95,
// REQ-99, REQ-143, REQ-155, REQ-158). Its golden is
// testdata/tour/s5c/mail_password_admin.json.
//
// The server sends mail through the tour's mail catcher (tourArea.mail), so
// email verification is required (auth/config says so) and every password
// account the area's steps register is walled until it follows its link.
// Admin and owner therefore register on a second server booted without the
// OPENV_SMTP_ variables (signUpWithout), where accounts are verified at
// sign-up. The other accounts are recorded registrations (tour.adopt), each
// from an address of the area's own, and each sign-up's mail is awaited
// before the account does anything else, since the link the mail carries is
// minted in a goroutine that would otherwise race the account's next
// request (one link is live per account: a later one deletes it).
//
// The area walks, in order: the sign-in configuration with a mailer; email
// verification (the wall on a protected route, a password change and an
// upload; the account itself, which the open /auth/me still reads; resend's
// refusals in the handler's order: no JSON body, no session; a resend, which
// makes the sign-up link stale; the link confirmed, and not twice; resend and
// change once verified); a change of address (its refusals, one of them
// after the account's resend budget is spent; the confirmation moving the
// address, and sign-in with the address it moved to); the resend budget
// (verifyResendLimiter) that resend and change share (Q18), drained through
// changes alone and through resends and a change; a send the mail server
// refuses (502); a change of address that another account registers before
// the link is followed (409 on confirmation, the account still walled);
// password reset by mail (202 for any address, a mail only for an account;
// a later link replacing an earlier one; the address's budget of three,
// counted on the normalised address; the confirmation's refusals, the reset
// signing the account out everywhere, and the emailed link verifying the
// address); password reset by a platform admin (the link, its refusals, and
// that its confirmation verifies nothing); a password change (its refusals,
// then the change, which ends the account's other sessions; the account is
// also added to a shared workspace as setup, whose notifier mail the area
// awaits, after an account with email notifications off joined it first and
// was mailed nothing of it: the email opt-out); the platform admin's workspace and account lists and the
// platform-admin standing; invitations by mail (emailed true, an unchanged
// invitation mailed within the hour handed back without a second mail, and
// an unverified account's address invited rather than added); the profile
// picture (each raster type replacing the last, the file served as
// stored, to an account that shares no workspace with its owner the 404 of
// an account with no picture, the refusals, an image of a type other than
// the one declared among them, and removal); and last the
// authIPLimiter bucket that a reset request spends only when the server has
// a mailer, drained through reset requests and refused on sign-in,
// confirmation and verification, and drained through sign-ins and refused
// on a reset request (Q18).
//
// Nondeterminism: ids, sessions, tokens (the generic tokens, or names where
// the area captures them) and minted times; a workspace slug's last 8 hex
// digits (the area's pattern, as the sessions area has it); the avatar URL's
// ?v=<unix seconds> (the area's pattern); the Retry-After countdowns of the
// 600 s and 30 s buckets (the area's header patterns); the admin routes'
// whole-second times (wholeSeconds); and when an invitation's mail went
// (last_emailed_at, stamped by the goroutine that sends it, so elided).
// Not pinned: a mail slower than notify.SendWithTimeout (10 s);
// the time-bound expiry of verification (24 h) and reset (1 h, 24 h for an
// admin's) links; ErrLastAdmin, since the caller of a demotion is itself an
// admin, so at least two admins exist whenever one is demoted (only a race of
// two demotions reaches it); the 500s that need a failing database or disk;
// and the S4a-pinned MaxBytesReader 413 of the avatar upload. Pinned in other
// areas: 409 no_password on PUT /me/password (self_hosted_cross_site_sso.json
// 37) and on the admin's reset (sessions_auth.json 95), which need an account
// an identity provider made; resend's and address change's 400 "email
// verification is not required on this server" and the reset request's 409
// reset_email_unavailable (sessions_auth.json 91 to 94), which only a server
// without a mailer answers.
func TestTourS5cMailPasswordAdmin(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "mail_password_admin",
		about: "Mail, passwords and platform admin: email verification and its wall, resend and change of address, " +
			"password reset by mail and by a platform admin, password change, the platform admin's lists and " +
			"standing, the profile picture, and the rate-limit buckets verification mail and reset requests share " +
			"(Q18), with every mail the server sent recorded by the tour's mail catcher.",
		run: mailPasswordAdminTour,
		env: map[string]string{
			"OPENV_CLIENT_IP_HEADER": "CF-Connecting-IP",
		},
		mail:          &tourMailSpec{refuse: []string{"@refused.example"}},
		signUpWithout: []string{"OPENV_SMTP_FROM", "OPENV_SMTP_HOST", "OPENV_SMTP_PORT"},
	})
}

// The addresses (RFC 5737 documentation ranges) the area's steps send as
// CF-Connecting-IP, each a rate-limit bucket of its own.
const (
	mailPasswordAdminSignUpNetA = "198.51.100.41" // four registrations (registerIPLimiter's burst is five)
	mailPasswordAdminSignUpNetB = "198.51.100.42" // four more
	mailPasswordAdminVerifyNet  = "198.51.100.43" // the recorded verifications
	mailPasswordAdminResetNet   = "198.51.100.44" // the recorded reset requests and confirmations
	mailPasswordAdminLoginNet   = "198.51.100.45" // the recorded sign-ins
	mailPasswordAdminDrainA     = "203.0.113.41"  // authIPLimiter drained through reset requests
	mailPasswordAdminDrainB     = "203.0.113.42"  // ... through sign-ins
)

// Image fixtures beside the shared tourPNG, tourSVG and tourPDF, written out
// by hand so that no encoder decides their bytes; each sniffs as its type
// (http.DetectContentType), which the avatar upload checks against the
// declared one.
const (
	// mailPasswordAdminJPEG is a JPEG's start of image, a JFIF APP0 segment
	// and its end of image, 22 bytes.
	mailPasswordAdminJPEG = "\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xff\xd9"
	// mailPasswordAdminGIF is a 1x1 transparent GIF89a, 43 bytes.
	mailPasswordAdminGIF = "GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00," +
		"\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;"
	// mailPasswordAdminWebP is a 1x1 lossless WebP (RIFF, WEBP, VP8L), 34
	// bytes.
	mailPasswordAdminWebP = "RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00"
)

// mailPasswordAdminFrom sends a request from an address of the area's.
func mailPasswordAdminFrom(net string) tourOpt { return withHeader("CF-Connecting-IP", net) }

// mailPasswordAdminCredentials is a sign-in body.
func mailPasswordAdminCredentials(email, password string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}

// mailPasswordAdminPicture is an avatar upload of one file part.
func mailPasswordAdminPicture(name, contentType, data string) tourOpt {
	return rawBody(multipartForm(nil, tourFormFile{field: "file", name: name, contentType: contentType, data: []byte(data)}))
}

// mailPasswordAdminVerifyLink and mailPasswordAdminResetLink capture the
// token of a mailed or answered link.
const (
	mailPasswordAdminVerifyLink = `verify-email\?token=([0-9a-f]{64})`
	mailPasswordAdminResetLink  = `reset-password\?token=([0-9a-f]{64})`
)

// mailPasswordAdminSignUp registers an account as a recorded step, from an
// address of the area's, adopts it, and awaits its sign-up mail (or the
// catcher's refusal of it), capturing the link's token as name.signup when
// the mail went. had is how many mails the address already holds.
func mailPasswordAdminSignUp(tr *tour, name, display, email, net, title string, had int) *tourActor {
	tr.t.Helper()
	reg := tr.step(title, tr.anon, "POST /api/v1/auth/register", mailPasswordAdminFrom(net),
		jsonBody(fmt.Sprintf(`{"email":%q,"password":%q,"name":%q}`, email, tourPassword, display)))
	a := tr.adopt(reg, name, fmt.Sprintf("an account a recorded registration made (step %d), unverified until it "+
		"follows a link", reg.step.n))
	if m := tr.awaitMail(email, had+1)[had]; m.refused == "" {
		m.capture(name+".signup", mailPasswordAdminVerifyLink)
	}
	return a
}

func mailPasswordAdminTour(tr *tour) {
	o, admin, anon := tr.owner, tr.admin, tr.anon
	tr.remember("unknown.token", strings.Repeat("fedcba9876543210", 4))
	tr.slugPattern()
	tr.pattern(`avatar\?v=(\d+)`, "<unix>", "an uploaded picture's URL carries the upload's time in Unix seconds "+
		"(avatarURL), so a browser that cached the last picture fetches the new one")
	tr.headerPattern("Retry-After", `^(59[0-9]|600)$`, "<retry-after 600 s>", "Retry-After once one account has asked "+
		"for three verification mails, or one address for three reset mails: verifyResendLimiter and "+
		"passwordResetLimiter refill 6 an hour, so ceil(600 s less the seconds since the first of the three), 600 "+
		"while they take under a second and no less than 590 within ten")
	tr.headerPattern("Retry-After", `^(2[0-9]|30)$`, "<retry-after 30 s>", "Retry-After on the 31st sign-in, "+
		"verification, reset request or reset confirmation from one address: authIPLimiter refills 120 an hour, "+
		"as above (no less than 20 within ten)")
	tr.wholeSeconds("GET /api/v1/admin/users", "adminUser formats created_at as 2006-01-02T15:04:05Z")
	tr.wholeSeconds("PUT /api/v1/admin/users/{id}/admin", "the same adminUser")
	worker := tr.bearerActor("worker", tr.setup("a worker key of the owner's personal workspace", o,
		"POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"), jsonBody(`{"name":"tour worker"}`)).value("/key"),
		"a worker key of the owner's personal workspace, as a bearer")
	fromVerify := mailPasswordAdminFrom(mailPasswordAdminVerifyNet)
	fromReset := mailPasswordAdminFrom(mailPasswordAdminResetNet)
	fromLogin := mailPasswordAdminFrom(mailPasswordAdminLoginNet)
	verify := func(title, token string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "POST /api/v1/auth/verify-email", append([]tourOpt{fromVerify,
			jsonBody(`{"token":"{{` + token + `}}"}`)}, opts...)...)
	}

	tr.step("the sign-in methods with a mail server: verification required, reset by mail offered", anon,
		"GET /api/v1/auth/config")

	// Email verification: the wall, resend and confirmation.
	v := mailPasswordAdminSignUp(tr, "verifier", "Tour Verifier", "tour-verifier@example.com",
		mailPasswordAdminSignUpNetA, "register an account: signed in, unverified, and a link mailed in the background", 0)
	tr.step("the unverified account on a protected route: the wall", v, "GET /api/v1/orgs")
	tr.step("the unverified account changes its password: the wall again, before the handler", v,
		"PUT /api/v1/me/password", jsonBody(`{"current_password":"tour password 1","new_password":"tour password 2"}`))
	tr.step("the unverified account uploads a picture: the wall", v, "POST /api/v1/me/avatar",
		mailPasswordAdminPicture("me.png", "image/png", tourPNG))
	tr.step("the account itself, which the open auth route reads from the cookie", v, "GET /api/v1/auth/me")
	tr.step("resend with no JSON body declared", v, "POST /api/v1/auth/verify-email/resend", rawBody("", []byte(`{}`)),
		note("the cookie-authenticated POST must declare application/json, which forces a CORS preflight"))
	tr.step("resend with no session", anon, "POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("resend with a worker key as the bearer: only the cookie is read", worker,
		"POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("resend: a new link, mailed before the answer", v, "POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.awaitMail(v.email, 2)[1].capture("verifier.resent", mailPasswordAdminVerifyLink)
	verify("confirm the sign-up link: the resend replaced it", "verifier.signup",
		note("one link is live per account: minting one deletes the account's unused ones"))
	verify("confirm the resent link, with no session: the account, verified", "verifier.resent")
	verify("confirm it again: a link works once", "verifier.resent")
	verify("confirm an unknown token", "unknown.token")
	tr.step("resend once verified", v, "POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("change the address once verified", v, "POST /api/v1/auth/verify-email/change",
		jsonBody(`{"email":"tour-verifier-2@example.com"}`))
	tr.home(v)
	tr.step("the verified account on a protected route: the wall is gone", v, "GET /api/v1/orgs")

	// A change of address, and the budget of verification mails that change
	// and resend share (verifyResendLimiter, 3 per account, Q18).
	m := mailPasswordAdminSignUp(tr, "mover", "Tour Mover", "tour-mover@example.com", mailPasswordAdminSignUpNetA,
		"register an account that corrects its address", 0)
	change := func(title, body string, opts ...tourOpt) *tourResult {
		return tr.step(title, m, "POST /api/v1/auth/verify-email/change", append([]tourOpt{jsonBody(body)}, opts...)...)
	}
	change("change to an empty address: refused before the budget", `{"email":"  "}`)
	tr.step("change with no JSON body declared", m, "POST /api/v1/auth/verify-email/change",
		rawBody("text/plain", []byte(`{"email":"tour-moved@example.com"}`)))
	change("change with a malformed body", `{`)
	change("change to the owner's address: taken, after a token of the budget is spent", `{"email":"tour-owner@example.com"}`)
	change("change to something that is not an address: refused after a second token", `{"email":"not-an-address"}`)
	change("change to a new address, in capitals: the third token, and a link mailed there", `{"email":" Tour-Moved@Example.com "}`,
		note("the address is stored trimmed and lower-cased; the account's own address does not change until the "+
			"link is followed"))
	tr.awaitMail("tour-moved@example.com", 1)[0].capture("mover.change", mailPasswordAdminVerifyLink)
	tr.step("resend: refused, since the changes spent the budget resend draws on (Q18)", m,
		"POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	verify("confirm the sign-up link: the change's link replaced it", "mover.signup")
	verify("confirm the change's link: the address moves", "mover.change")
	tr.step("resend once verified: already verified, which is checked before the spent budget", m,
		"POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("sign in with the address the link moved it to", anon, "POST /api/v1/auth/login", fromLogin,
		mailPasswordAdminCredentials("tour-moved@example.com", tourPassword))
	tr.step("sign in with the old address: no such account now", anon, "POST /api/v1/auth/login", fromLogin,
		mailPasswordAdminCredentials("tour-mover@example.com", tourPassword))

	th := mailPasswordAdminSignUp(tr, "throttled", "Tour Throttled", "tour-throttled@example.com",
		mailPasswordAdminSignUpNetA, "register an account whose verification mails reach the budget", 0)
	tr.step("resend: the first token", th, "POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("resend: the second", th, "POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("change the address: the third", th, "POST /api/v1/auth/verify-email/change",
		jsonBody(`{"email":"tour-throttled-2@example.com"}`))
	tr.step("resend: refused, the budget spent (Q18)", th, "POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.step("change: refused by the same budget", th, "POST /api/v1/auth/verify-email/change",
		jsonBody(`{"email":"tour-throttled-3@example.com"}`))
	sent := tr.awaitMail(th.email, 3)
	sent[1].capture("throttled.resent1", mailPasswordAdminVerifyLink)
	sent[2].capture("throttled.resent2", mailPasswordAdminVerifyLink)
	tr.awaitMail("tour-throttled-2@example.com", 1)[0].capture("throttled.change", mailPasswordAdminVerifyLink)

	// A send the mail server refuses.
	f := mailPasswordAdminSignUp(tr, "refused", "Tour Refused", "tour-refused@refused.example",
		mailPasswordAdminSignUpNetA, "register an address the mail server refuses: the account all the same, the "+
			"sign-up mail's failure only logged", 0)
	tr.step("resend: the mail server refuses the recipient, so the send fails", f,
		"POST /api/v1/auth/verify-email/resend", jsonBody(`{}`))
	tr.awaitMail(f.email, 2)

	// A change of address another account takes before the link is followed.
	c := mailPasswordAdminSignUp(tr, "claimed", "Tour Claimed", "tour-claimed-first@example.com",
		mailPasswordAdminSignUpNetB, "register an account that moves to an address another account then takes", 0)
	tr.step("change to a free address: a link mailed there", c, "POST /api/v1/auth/verify-email/change",
		jsonBody(`{"email":"tour-claimed@example.com"}`))
	tr.awaitMail("tour-claimed@example.com", 1)[0].capture("claimed.change", mailPasswordAdminVerifyLink)
	mailPasswordAdminSignUp(tr, "claimer", "Tour Claimer", "tour-claimed@example.com", mailPasswordAdminSignUpNetB,
		"register the address the change's link names: a pending change does not hold it", 1)
	tr.step("change to it again: taken now", c, "POST /api/v1/auth/verify-email/change",
		jsonBody(`{"email":"tour-claimed@example.com"}`))
	verify("confirm the change's link: the address is another account's now", "claimed.change",
		note("ConsumeEmailVerification rolls back on the unique address, so the account stays unverified"))
	tr.step("the account is still walled", c, "GET /api/v1/orgs")

	// Password reset by mail (REQ-158).
	r := mailPasswordAdminSignUp(tr, "resetter", "Tour Resetter", "tour-resetter@example.com",
		mailPasswordAdminSignUpNetB, "register an account that resets its password by mail", 0)
	request := func(title, body string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "POST /api/v1/auth/password-reset", append([]tourOpt{fromReset, jsonBody(body)}, opts...)...)
	}
	request("ask for a reset link: 202, the mail sent after the answer", `{"email":"tour-resetter@example.com"}`)
	tr.awaitMail(r.email, 2)[1].capture("resetter.reset1", mailPasswordAdminResetLink)
	request("ask again: a new link", `{"email":"tour-resetter@example.com"}`)
	tr.awaitMail(r.email, 3)[2].capture("resetter.reset2", mailPasswordAdminResetLink)
	request("ask with the address in capitals: the same address, a third link", `{"email":" TOUR-RESETTER@Example.com "}`,
		note("the answer names the address trimmed and lower-cased"))
	tr.awaitMail(r.email, 4)[3].capture("resetter.reset3", mailPasswordAdminResetLink)
	request("a fourth request for the address: its budget of three is spent", `{"email":"tour-resetter@example.com"}`)
	request("ask for an address no account has: 202 all the same, and no mail", `{"email":"tour-nobody@example.com"}`)
	request("ask with no address", `{"email":"not-an-address"}`)
	request("ask with a malformed body", `{`)
	confirm := func(title, token, password string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "POST /api/v1/auth/password-reset/confirm", append([]tourOpt{fromReset,
			jsonBody(fmt.Sprintf(`{"token":"{{%s}}","new_password":%q}`, token, password))}, opts...)...)
	}
	confirm("confirm the first link: a later one replaced it", "resetter.reset1", "tour password 2")
	confirm("confirm the second: replaced too", "resetter.reset2", "tour password 2")
	confirm("confirm the third with a password under 8 characters", "resetter.reset3", "short",
		note("the length is checked before the link is spent"))
	confirm("confirm the third: the password is set", "resetter.reset3", "tour password 2")
	confirm("confirm it again: a link works once", "resetter.reset3", "tour password 3")
	tr.step("the session the registration opened: signed out, as every session of the account is", r,
		"GET /api/v1/auth/me")
	tr.step("sign in with the old password", anon, "POST /api/v1/auth/login", fromLogin,
		mailPasswordAdminCredentials(r.email, tourPassword))
	back := tr.step("sign in with the new password: the address is verified now, since the emailed link reached it",
		anon, "POST /api/v1/auth/login", fromLogin, mailPasswordAdminCredentials(r.email, "tour password 2"))
	rs := tr.session(r, back, "resetter.second", fmt.Sprintf("a session of resetter's from a recorded sign-in (step %d)",
		back.step.n))
	tr.step("a protected route with the new session: no wall", rs, "GET /api/v1/orgs",
		note("the session has no workspace of the tour's to send, so the server resolves the personal one"))

	// Password reset by a platform admin.
	h := mailPasswordAdminSignUp(tr, "handed", "Tour Handed", "tour-handed@example.com", mailPasswordAdminSignUpNetB,
		"register an account a platform admin issues a reset link for", 0)
	issue := func(title string, a *tourActor, id string) *tourResult {
		return tr.step(title, a, "POST /api/v1/admin/users/{id}/password-reset", at("id", id))
	}
	issue("a reset link issued by someone who is not a platform admin", o, "{{handed}}")
	issue("a reset link issued with a worker key as the bearer", worker, "{{handed}}")
	issue("a reset link for an account that does not exist", admin, "{{phantom}}")
	issue("a reset link for something that is not an id: 404, as an id no account has", admin, "not-an-id")
	link := issue("a reset link issued by the platform admin: the link, shown once, and when it expires", admin, "{{handed}}")
	link.captureMatch("handed.reset", "/link", mailPasswordAdminResetLink)
	link.noteExpiry("/expires_at")
	confirm("confirm the admin's link", "handed.reset", "tour password 2")
	hb := tr.step("sign in with the new password: still unverified, since an admin's link proves nothing of the mailbox",
		anon, "POST /api/v1/auth/login", fromLogin, mailPasswordAdminCredentials(h.email, "tour password 2"))
	hs := tr.session(h, hb, "handed.second", fmt.Sprintf("a session of handed's from a recorded sign-in (step %d)", hb.step.n))
	tr.step("a protected route with the new session: the wall", hs, "GET /api/v1/orgs")

	// Password change (REQ-99).
	p := tr.register("changer", "Tour Changer", "a verified account (its link followed as setup) that changes its "+
		"password, is made a platform admin and back, and uploads a picture")
	w := tr.setup("the owner's shared workspace W", o, "POST /api/v1/orgs", jsonBody(`{"name":"Tour Shared"}`),
		expect(201)).capture("w", "/id")
	// The opt-out: an account with email notifications off joins W first,
	// and is mailed nothing of it (outbound_mail would show "You joined a
	// workspace"). The notifier mails on the bus's one goroutine, in order,
	// so the changer's join mail awaited below comes after whatever the muted
	// account's join would have sent.
	muted := tr.register("muted", "Tour Muted", "a verified account that turns email notifications off and joins W, "+
		"and is mailed nothing of it")
	tr.setup("the muted account turns email notifications off", muted, "PUT /api/v1/me/notification-prefs",
		jsonBody(`{"email_notifications":false}`), expect(200))
	tr.join(muted, w, "member")
	tr.join(p, w, "member")
	// The notifier mails the new member after the answer (a bus subscriber):
	// awaited, so that the golden's outbound_mail does not depend on when
	// the area ends.
	tr.awaitMail(p.email, 2)
	gone := tr.setup("a shared workspace the owner then deletes", o, "POST /api/v1/orgs", jsonBody(`{"name":"Tour Gone"}`),
		expect(201)).capture("gone", "/id")
	tr.setup("delete it", o, "DELETE /api/v1/orgs/{id}", at("id", gone))
	pl := tr.step("the changer signs in elsewhere: a second session", anon, "POST /api/v1/auth/login", fromLogin,
		mailPasswordAdminCredentials(p.email, tourPassword))
	p2 := tr.session(p, pl, "changer.second", fmt.Sprintf("a second session of changer's, from a recorded sign-in (step %d)",
		pl.step.n))
	password := func(title string, a *tourActor, body string) *tourResult {
		return tr.step(title, a, "PUT /api/v1/me/password", jsonBody(body))
	}
	password("change the password with a malformed body", p, `{`)
	password("change it with a wrong current password", p, `{"current_password":"not the password","new_password":"tour password 2"}`)
	password("change it to one under 8 characters", p, `{"current_password":"tour password 1","new_password":"short"}`)
	password("change it with no session: the middleware's 401", anon, `{"current_password":"tour password 1","new_password":"tour password 2"}`)
	password("change it with a worker key as the bearer: no account, the handler's 401", worker,
		`{"current_password":"tour password 1","new_password":"tour password 2"}`)
	password("change it: 204, and every other session of the account ends", p,
		`{"current_password":"tour password 1","new_password":"tour password 2"}`)
	tr.step("the session that changed it is still signed in", p, "GET /api/v1/auth/me")
	tr.step("the other session is signed out", p2, "GET /api/v1/auth/me")
	tr.step("a protected route from the other session: the middleware's 401", p2, "GET /api/v1/orgs")
	tr.step("sign in with the old password", anon, "POST /api/v1/auth/login", fromLogin,
		mailPasswordAdminCredentials(p.email, tourPassword))
	tr.step("sign in with the new one", anon, "POST /api/v1/auth/login", fromLogin,
		mailPasswordAdminCredentials(p.email, "tour password 2"))

	// The platform admin (REQ-155).
	tr.step("every workspace, as someone who is not a platform admin", o, "GET /api/v1/admin/workspaces")
	tr.step("every workspace, with a worker key as the bearer", worker, "GET /api/v1/admin/workspaces")
	tr.step("every live workspace with its member count, oldest first: the deleted one is left out", admin,
		"GET /api/v1/admin/workspaces")
	tr.step("every account, as someone who is not a platform admin", o, "GET /api/v1/admin/users")
	// Each daily series is 90 {day, count} points, a two-digit count at most
	// in a tour, which keeps the answer clear of the compressor's floor.
	dashboardSeriesMin := 2 + 90*len(`{"day":"2026-01-01","count":0}`) + 89
	dashboardSeriesMax := 2 + 90*len(`{"day":"2026-01-01","count":99}`) + 89
	tr.step("the user dashboard, as someone who is not a platform admin", o, "GET /api/v1/admin/metrics/users")
	tr.step("the user dashboard: totals, active and lost users, sign-in methods, countries and cohorts", admin,
		"GET /api/v1/admin/metrics/users",
		elide("/generated_at", "<now>", len(`"2026-01-01T00:00:00Z"`), len(`"2026-01-01T00:00:00Z"`), "the time the dashboard was built"),
		elide("/signups", "<90 days of sign-ups>", dashboardSeriesMin, dashboardSeriesMax, "a daily series ending today, so its days move with the calendar"),
		elide("/active", "<90 days of active users>", dashboardSeriesMin, dashboardSeriesMax, "a daily series ending today, so its days move with the calendar"),
		elide("/cohorts", "<six monthly cohorts>", len("[]"), tourNoMore, "monthly rows ending this month, so their months move with the calendar"))
	setAdmin := func(title string, a *tourActor, id, body string) *tourResult {
		return tr.step(title, a, "PUT /api/v1/admin/users/{id}/admin", at("id", id), jsonBody(body))
	}
	setAdmin("make an account a platform admin, as someone who is not one", o, "{{changer}}", `{"is_admin":true}`)
	setAdmin("make the changer a platform admin", admin, "{{changer}}", `{"is_admin":true}`)
	setAdmin("again: unchanged, and no error", admin, "{{changer}}", `{"is_admin":true}`)
	setAdmin("with a malformed body", admin, "{{changer}}", `{`)
	setAdmin("an account that does not exist", admin, "{{phantom}}", `{"is_admin":true}`)
	setAdmin("something that is not an id: 404, as an id no account has", admin, "not-an-id", `{"is_admin":true}`)
	setAdmin("the admin removes its own standing: refused, another admin must", admin, "{{admin}}", `{"is_admin":false}`)
	tr.step("every account, as the new platform admin: admins first, then by name", p, "GET /api/v1/admin/users")
	setAdmin("remove the changer's standing", admin, "{{changer}}", `{"is_admin":false}`)
	setAdmin("remove the standing of an account that has none: unchanged", admin, "{{owner}}", `{"is_admin":false}`)
	tr.step("every account: one admin again", admin, "GET /api/v1/admin/users")

	// Invitations by mail: with a mailer an invitation is mailed after the
	// answer (emailed true), and an unchanged one whose link was delivered
	// within the hour is handed back without a second mail; an address whose
	// account is unverified is invited, not added.
	tr.step("invite an address to W: the invitation, its link, and emailed true, the mail sent after the answer", o,
		"POST /api/v1/orgs/{id}/invitations", at("id", "{{w}}"), actingIn("{{w}}"),
		jsonBody(`{"email":"tour-invitee@example.com","role":"member"}`))
	tr.awaitMail("tour-invitee@example.com", 1)[0].capture("invitee.invite", `invite=([0-9a-f]{64})`)
	tr.await("the invitation marked as mailed", o, "GET /api/v1/orgs/{id}/invitations", func(r *tourResult) bool {
		return strings.Contains(string(r.body), `"last_emailed_at"`)
	}, at("id", "{{w}}"), actingIn("{{w}}"))
	tr.step("invite it again, unchanged: the invitation handed back, no link and no mail", o,
		"POST /api/v1/orgs/{id}/invitations", at("id", "{{w}}"), actingIn("{{w}}"),
		jsonBody(`{"email":"tour-invitee@example.com","role":"member"}`),
		elide("/invitation/last_emailed_at", "<when the mail went>", len(`"2026-01-01T00:00:00Z"`),
			len(`"2026-01-01T00:00:00.123456789Z"`), "stamped by the goroutine that mailed the link, after the "+
				"invitation's answer, so its exchange is not fixed"))
	tr.step("add the unverified handed account as a member: invited instead, since nobody has proved the mailbox", o,
		"POST /api/v1/orgs/{id}/members", at("id", "{{w}}"), actingIn("{{w}}"),
		jsonBody(`{"email":"tour-handed@example.com","role":"member"}`))
	tr.awaitMail(h.email, 2)[1].capture("handed.invite", `invite=([0-9a-f]{64})`)
	tr.await("the member invitation marked as mailed", o, "GET /api/v1/orgs/{id}/invitations", func(r *tourResult) bool {
		return strings.Count(string(r.body), `"last_emailed_at"`) == 2
	}, at("id", "{{w}}"), actingIn("{{w}}"))

	// The profile picture: each raster type replaces the last.
	avatar := func(title string, a *tourActor, id string, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "GET /api/v1/users/{id}/avatar", append([]tourOpt{at("id", id)}, opts...)...)
	}
	upload := func(title string, a *tourActor, opt tourOpt, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "POST /api/v1/me/avatar", append([]tourOpt{opt}, opts...)...)
	}
	avatar("the owner's picture before any upload", o, "{{owner}}")
	upload("upload a PNG: the account, its avatar_url the picture's", o, mailPasswordAdminPicture("me.png", "image/png", tourPNG))
	avatar("the picture: as stored, never sniffed, cached privately for a day", o, "{{owner}}")
	upload("upload a JPEG: it replaces the PNG, whose file is removed", o,
		mailPasswordAdminPicture("me.jpg", "image/jpeg", mailPasswordAdminJPEG))
	avatar("the JPEG", o, "{{owner}}")
	upload("upload a GIF", o, mailPasswordAdminPicture("me.gif", "image/gif", mailPasswordAdminGIF))
	avatar("the GIF", o, "{{owner}}")
	upload("upload a WebP", o, mailPasswordAdminPicture("me.webp", "image/webp", mailPasswordAdminWebP))
	avatar("the WebP", o, "{{owner}}")
	avatar("the WebP with a Range header: ignored, the whole file", o, "{{owner}}", withHeader("Range", "bytes=0-9"))
	avatar("the owner's picture read by an account that shares no workspace with it: the 404 of an account with no "+
		"picture, since only the owner, W's members and a platform admin may know of it", v, "{{owner}}")
	avatar("the picture with a worker key as the bearer: no account, the handler's 401", worker, "{{owner}}")
	avatar("the picture with no session: the middleware's 401", anon, "{{owner}}")
	avatar("the picture of an account that uploaded none", o, "{{muted}}")
	avatar("the picture of an account that does not exist", o, "{{phantom}}")
	avatar("the picture of something that is not an id: the same 404", o, "not-an-id")
	upload("upload an SVG: not a raster type", o, mailPasswordAdminPicture("me.svg", "image/svg+xml", tourSVG))
	upload("upload a PDF declared as a PNG: its bytes are not an image", o, mailPasswordAdminPicture("me.png", "image/png", tourPDF))
	upload("upload with no file part", o, rawBody(multipartForm([][2]string{{"name", "me"}})))
	upload("upload a JSON body", o, jsonBody(`{"file":"me.png"}`))
	big := append([]byte(tourPNG), bytes.Repeat([]byte{0}, 2<<20+1-len(tourPNG))...)
	upload("upload 2 MiB and one byte", o, rawBody(multipartForm(nil, tourFormFile{field: "file", name: "big.png",
		contentType: "image/png", data: big})), note("the part is recorded by its size and digest"))
	upload("upload a PNG declared as a GIF: refused, since the bytes must sniff as the declared type",
		p, mailPasswordAdminPicture("me.gif", "image/gif", tourPNG))
	avatar("the changer's picture: none, the refused upload stored nothing", p, "{{changer}}")
	tr.setup("the changer uploads its PNG as a PNG", p, "POST /api/v1/me/avatar",
		mailPasswordAdminPicture("me.png", "image/png", tourPNG))
	tr.step("remove the picture: the account without one, the file removed", p, "DELETE /api/v1/me/avatar")
	avatar("the removed picture", p, "{{changer}}")
	tr.step("remove it again: the same answer", p, "DELETE /api/v1/me/avatar")
	tr.step("remove a picture with a worker key as the bearer: the handler's 401", worker, "DELETE /api/v1/me/avatar")

	// Q18: a reset request spends authIPLimiter (burst 30) when the server
	// has a mailer, which sign-in, verification and reset confirmation also
	// spend. Each drain asks for addresses no account has, so no address's
	// own budget closes first and no mail goes.
	fromA, fromB := mailPasswordAdminFrom(mailPasswordAdminDrainA), mailPasswordAdminFrom(mailPasswordAdminDrainB)
	nobody := func(i int) string { return fmt.Sprintf(`{"email":"tour-drain-%02d@example.com"}`, i) }
	for i := 1; i < 30; i++ {
		tr.probe(anon, "POST /api/v1/auth/password-reset", fromA, jsonBody(nobody(i)))
	}
	tr.step("the 30th reset request from address A: the last its budget allows", anon, "POST /api/v1/auth/password-reset",
		fromA, jsonBody(nobody(30)), note("29 requests, each for another address no account has, went before it, as probes"))
	tr.step("sign in from A, with the right password: refused, the bucket the reset requests spent (Q18)", anon,
		"POST /api/v1/auth/login", fromA, mailPasswordAdminCredentials(p.email, "tour password 2"))
	tr.step("confirm a reset from A: refused by the same bucket", anon, "POST /api/v1/auth/password-reset/confirm", fromA,
		jsonBody(`{"token":"{{unknown.token}}","new_password":"tour password 2"}`))
	tr.step("verify from A: refused too", anon, "POST /api/v1/auth/verify-email", fromA,
		jsonBody(`{"token":"{{unknown.token}}"}`))
	tr.step("a reset request from A: refused by the same bucket, before the address's own budget", anon, "POST /api/v1/auth/password-reset", fromA, jsonBody(nobody(31)))
	unknownLogin := func(i int) string {
		return fmt.Sprintf(`{"email":"tour-drain-%02d@example.com","password":"tour password 1"}`, i)
	}
	for i := 1; i < 30; i++ {
		tr.probe(anon, "POST /api/v1/auth/login", fromB, jsonBody(unknownLogin(i)))
	}
	tr.step("the 30th sign-in from address B: the last its budget allows", anon, "POST /api/v1/auth/login", fromB,
		jsonBody(unknownLogin(30)), note("29 sign-ins, each to another address no account has, went before it, as probes"))
	tr.step("a reset request from B: refused, the bucket the sign-ins spent (Q18)", anon,
		"POST /api/v1/auth/password-reset", fromB, jsonBody(`{"email":"tour-resetter@example.com"}`))
}
