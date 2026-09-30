//go:build unix

package main

import (
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestTourS5cNotificationsPush is the S5c tour's notifications and web push
// area (refactor plan §6.4 S5c, before M9; invariants I4, I8 (the stream's
// head), I9 (the notify: stream key's head only; its live frames are S6's);
// quirk Q14 (null against []); OpenV REQ-109, REQ-132 (issue #132), REQ-143).
// Its golden is testdata/tour/s5c/notifications_push.json. It takes all 13
// routes of notification_handlers.go and push_handlers.go: the inbox and its
// views, paging, the read, flag, clear and delete actions, the live stream's
// head, the account's notification preferences, and web push (the server's
// key, the devices an account subscribes, and one push the server attempts).
//
// Notifications are written by the notifier, a bus subscriber that runs after
// the answer (internal/notify), so no recorded step causes one: each is
// caused by a setup request, and the area polls the inbox with probes until
// the expected count is there before the next setup or a recorded read (the
// bus dispatches on one FIFO goroutine, so the last row is a barrier for the
// ones before it; and the notifier reads who is an admin or an editor when it
// handles the event, so a later setup must not change that before it has).
// Setup, through S5a's and the workspace routes: the owner shares workspace W
// with the member m (org.member_added: "You joined a workspace"), makes m an
// admin of W (org.member_role_changed: m's access_changed, and the admins'
// membership_changed, which m, now an admin, receives too, while the owner,
// who acted, receives nothing), creates project P in W with one requirement,
// adds m to P as an editor (project.member_added: access_changed), mentions
// @tourmember in a note on the requirement (mention) and submits it for
// review (review_requested, to P's editors): six notifications for m, all
// minted before step 1, and none for the owner.
//
// The area walks, in order: the inbox (its Content-Type, its order, newest
// first and then by id, the unread count, the owner's empty inbox written
// null), its views (inbox, flagged, cleared, an unknown one read as the inbox,
// unread only) and its paging (a full page carries next_cursor, the cursor
// "<RFC 3339 time>|<id>" of its last row, which before= continues from, so a
// page that happens to hold the rest still carries one and the page after it
// is empty; each refusal of limit and before); the live stream's head (the
// stream replays nothing, so the tour reads no frame and closes it; it is
// read while no notification is pending); who may call these routes (a
// worker key on each of the 13 answers the handler's 401, a request with no
// session the middleware's); the actions (read by id, scoped to the caller,
// so another account's id, a phantom and a malformed id update nothing; flag
// and unflag, 404 for another account's id and a malformed one; read-all;
// clear, which archives into the cleared view, keeps a flag, and marks the
// rows read; delete what is cleared); the preferences (their defaults, a
// partial update that changes only the keys sent); and web push: the server's
// VAPID public key, a device subscribed, re-subscribed (an upsert on the
// endpoint: the same id and created_at, the keys and user agent refreshed,
// the user agent falling back to the request's own header, and truncated to
// 400 runes, not bytes), a second device and its withdrawal (204 also when
// there is nothing to withdraw), the keys never serialised, a device another
// account posts moving to that account (the upsert refreshes the owner), and
// every refusal of an endpoint (https only, no address literal, no
// credentials, the default port, a push service of the built-in list, whose
// "*.push.apple.com" does not match push.apple.com itself), of the keys and
// of the body. Last, with m opted in and one device on file, a push-eligible
// notification (m's role back to member: access_changed) makes the server
// attempt one push: the recording proxy sees CONNECT fcm.googleapis.com:443
// once and refuses it, the device is marked failed_at and kept, and posting
// it again clears the mark; clearing that notification marks it read. And
// the opt-out: an account with push off (the default) and a device on file
// joins W as setup and is pushed nothing (no CONNECT to web.push.apple.com in
// outbound_requests); it never turns push on, so the answer does not depend
// on when the push worker reads the preference.
//
// The server has a fixed test VAPID pair (generated once with
// cmd/openv-vapid, kept below) and subject, so push is on and its public key
// is the same on every run; the device's keys are a fixed P-256 point and a
// 16-byte secret, which the push library needs to be valid before it sends.
//
// Nondeterminism: ids and minted times (the generic tokens: every
// notification is minted during a setup's polling, <time@before step N>);
// the stream's answer has no Content-Length. Not pinned: the push's delivery
// (the proxy refuses it, and its encrypted payload and VAPID token are
// random), a push service's 404 or 410 that deletes a device and a 2xx that
// stamps last_used_at (they need a push service; the dispatcher's own tests
// have them), notification email (no mail server here; the mail area pins
// its opt-out), the wording of titles and bodies (recorded, but S10 guards
// it, I18), the live "notification" frame on the stream (S6's), and the 200
// cap and default 50 of the inbox's limit, which would take 201
// notifications (X3a's table test).
func TestTourS5cNotificationsPush(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "notifications_push",
		about: "Notifications and web push: the inbox, its views and paging, the read, flag, clear and delete actions, " +
			"the live stream's head, the notification preferences, and web push (the server's key, the devices an " +
			"account subscribes, each refusal, and one push attempted through the recording proxy).",
		run: notificationsPushTour,
		env: map[string]string{
			"OPENV_VAPID_PUBLIC_KEY":  notificationsPushVAPIDPublic,
			"OPENV_VAPID_PRIVATE_KEY": notificationsPushVAPIDPrivate,
			"OPENV_VAPID_SUBJECT":     "mailto:tour@example.com",
		},
	})
}

// The area's keys are derived from fixed seeds when the test runs, so that no
// key-shaped literal is committed for the secret scan to flag. They sign and
// encrypt nothing that leaves the machine: the one push the area sends is
// refused by the recording proxy.
//
// A VAPID pair in cmd/openv-vapid's encoding: the private key a P-256 scalar,
// the public key its uncompressed point, both base64url without padding. The
// golden lists the private key by a placeholder (notificationsPushTour).
var notificationsPushVAPIDPublic, notificationsPushVAPIDPrivate = notificationsPushKeyPair("openv tour VAPID key")

// A device's keys as a browser's pushManager.subscribe() hands them over:
// p256dh a P-256 point (65 bytes, 88 base64url characters), auth a 16-byte
// secret (24). Two pairs, so that a re-subscription is seen to change them.
var (
	notificationsPushP256dh  = notificationsPushDeviceKey("openv tour device key 1")
	notificationsPushAuth    = notificationsPushDeviceAuth("openv tour device auth 1")
	notificationsPushP256dh2 = notificationsPushDeviceKey("openv tour device key 2")
	notificationsPushAuth2   = notificationsPushDeviceAuth("openv tour device auth 2")
)

// notificationsPushScalar is a P-256 private key derived from seed.
func notificationsPushScalar(seed string) *ecdh.PrivateKey {
	sum := sha256.Sum256([]byte(seed))
	k, err := ecdh.P256().NewPrivateKey(sum[:])
	if err != nil {
		panic("the tour's push key seed " + seed + " is not a P-256 scalar: " + err.Error())
	}
	return k
}

// notificationsPushKeyPair is a VAPID public and private key derived from seed.
func notificationsPushKeyPair(seed string) (public, private string) {
	k := notificationsPushScalar(seed)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(k.Bytes())
}

// notificationsPushDeviceKey is a p256dh derived from seed, padded as a
// browser pads it.
func notificationsPushDeviceKey(seed string) string {
	return base64.URLEncoding.EncodeToString(notificationsPushScalar(seed).PublicKey().Bytes())
}

// notificationsPushDeviceAuth is a 16-byte auth secret derived from seed.
func notificationsPushDeviceAuth(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return base64.URLEncoding.EncodeToString(sum[:16])
}

// The endpoints the area subscribes: a Chrome device (the one the push goes
// to) and a Safari one.
const (
	notificationsPushDevice = "https://fcm.googleapis.com/fcm/send/tour"
	notificationsPushApple  = "https://web.push.apple.com/tour-device"
)

// notificationsPushSubscription is a POST /me/push-subscriptions body, in
// the browser's shape; an empty user agent is left out.
func notificationsPushSubscription(endpoint, p256dh, auth, userAgent string) tourOpt {
	type keys struct {
		P256dh string `json:"p256dh,omitempty"`
		Auth   string `json:"auth,omitempty"`
	}
	body, err := json.Marshal(struct {
		Endpoint  string `json:"endpoint"`
		Keys      keys   `json:"keys"`
		UserAgent string `json:"user_agent,omitempty"`
	}{endpoint, keys{p256dh, auth}, userAgent})
	if err != nil {
		panic(err)
	}
	return jsonBody(string(body))
}

// notificationsPushEndpoint is a DELETE /me/push-subscriptions body.
func notificationsPushEndpoint(endpoint string) tourOpt {
	body, _ := json.Marshal(map[string]string{"endpoint": endpoint})
	return jsonBody(string(body))
}

// notificationsPushInbox waits, with probes, until a's inbox holds want
// notifications: the notifier writes them after the answer that caused them.
func notificationsPushInbox(tr *tour, a *tourActor, want int, what string) {
	tr.await(what, a, "GET /api/v1/notifications", func(r *tourResult) bool {
		var body struct {
			Notifications []json.RawMessage `json:"notifications"`
		}
		return r.status == 200 && json.Unmarshal(r.body, &body) == nil && len(body.Notifications) == want
	})
}

func notificationsPushTour(tr *tour) {
	tr.shown["OPENV_VAPID_PRIVATE_KEY"] = "<the tour's test VAPID private key, derived from a fixed seed>"
	o, anon := tr.owner, tr.anon

	// The fixtures: W with the member, P in W with a requirement, and the
	// six notifications they cause for the member (none for the owner).
	w := tr.sharedWorkspace("w", "Tour Notifications")
	m := tr.register("member", "Tour Member", "a member of W and an editor of P, whose notifications and devices "+
		"the area reads; an admin of W from the setup until the push at the end")
	worker := tr.bearerActor("worker", tr.setup("a worker key of W", o, "POST /api/v1/orgs/{id}/worker-keys",
		at("id", w), jsonBody(`{"name":"tour worker"}`)).value("/key"), "a worker key of W, as a bearer")
	// The notifier reads who is an admin, a member or an editor when it
	// handles an event, after the answer: each cause is awaited before the
	// next setup changes that. The member's join is awaited to its end, the
	// admins' alert, since the notifier tells the member first and reads W's
	// admins after; the role change after the join would otherwise make the
	// member an admin in time to hear of its own arrival. The owner, who
	// sends the join, is the one admin the alert skips, so the quiet account
	// joins as an admin to be the alert's witness: the request that publishes
	// its own join makes it an admin, so the alert of that join reaches it
	// too, and the count it is awaited at does not race.
	// The opt-out: an account with push off (the default) and a device on
	// file joins W, and its notices are never pushed (the golden's
	// outbound_requests would show a CONNECT to web.push.apple.com). It never
	// turns push on, so the answer does not depend on when the push worker
	// reads the preference. No step reads its inbox or its role.
	quiet := tr.register("quiet", "Tour Quiet", "a member of W with push off and a device on file, to whom nothing "+
		"is pushed")
	tr.setup("the quiet account subscribes a device, with push off", quiet, "POST /api/v1/me/push-subscriptions",
		notificationsPushSubscription("https://web.push.apple.com/tour-quiet", notificationsPushP256dh,
			notificationsPushAuth, "Tour Quiet/1.0"), expect(201))
	tr.join(quiet, w, "admin")
	notificationsPushInbox(tr, quiet, 2, "the quiet account's notice that it joined W, and the admins' alert of it")
	tr.join(m, w, "member")
	notificationsPushInbox(tr, m, 1, "the member's notice that it joined W")
	notificationsPushInbox(tr, quiet, 3, "the admins' alert of the member's join, read before the member is an admin")
	tr.setup("make the member an admin of W", o, "PUT /api/v1/orgs/{id}/members/{userId}",
		at("id", w, "userId", "{{member}}"), jsonBody(`{"role":"admin"}`), expect(204))
	notificationsPushInbox(tr, m, 3, "the member's notice of its role, and the admins' alert")
	tr.setup("project P in W", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Notification tour","description":"The project whose changes notify the member"}`)).capture("p", "/id")
	tr.setup("a requirement in P", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Alarm latency","body":"The system shall raise every alarm within 1 s."}`)).capture("req", "/id")
	tr.setup("add the member to P as an editor", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-member@example.com","role":"editor"}`))
	notificationsPushInbox(tr, m, 4, "the member's notice that it was added to P")
	tr.setup("a note on the requirement that mentions the member", o, "POST /api/v1/chatter",
		jsonBody(`{"artifact_id":"{{req}}","message":"@tourmember please check the new latency before the review"}`))
	notificationsPushInbox(tr, m, 5, "the member's mention")
	tr.setup("submit the requirement for review", o, "PUT /api/v1/artifacts/{id}/status", at("id", "{{req}}"),
		jsonBody(`{"status":"in_review"}`))
	notificationsPushInbox(tr, m, 6, "the member's review request")

	// The inbox, its views and its paging.
	inbox := tr.step("the member's inbox: six notifications, newest first, and the unread count", m,
		"GET /api/v1/notifications", note("every notification was minted during the setup's polling, before step 1; "+
			"the order is the notifier's: joined W, the role change (the member's own notice, then the admins' "+
			"alert), added to P, the mention, the review request"))
	for i, name := range []string{"note.review", "note.mention", "note.project", "note.admin", "note.role", "note.joined"} {
		inbox.capture(name, "/notifications/"+string(rune('0'+i))+"/id")
	}
	tr.step("the owner's inbox: the owner caused every one of them, so none; written null (Q14)", o,
		"GET /api/v1/notifications")
	tr.step("the inbox with no session: the middleware's 401", anon, "GET /api/v1/notifications")
	tr.step("unread only: all six", m, "GET /api/v1/notifications", query("unread=true"))
	tr.step("the inbox view by name: the same as none", m, "GET /api/v1/notifications", query("view=inbox"))
	tr.step("the flagged view: none", m, "GET /api/v1/notifications", query("view=flagged"))
	tr.step("the cleared view: none", m, "GET /api/v1/notifications", query("view=cleared"))
	tr.step("a view the server does not know: the inbox", m, "GET /api/v1/notifications", query("view=everything"))
	first := tr.step("one per page: a full page carries next_cursor, its last row's time and id", m,
		"GET /api/v1/notifications", query("limit=1"))
	cursor := first.value("/next_cursor")
	second := tr.step("the next five, from the cursor: the rest, and since the page is full, a cursor again", m,
		"GET /api/v1/notifications", query("limit=5&before="+cursor),
		note("the cursor is sent as the server wrote it, \"<RFC 3339 time>|<id>\""))
	tr.step("the page after the rest: empty, written null, with no cursor", m, "GET /api/v1/notifications",
		query("before="+second.value("/next_cursor")))
	tr.step("a limit of 0", m, "GET /api/v1/notifications", query("limit=0"))
	tr.step("a limit that is not a number", m, "GET /api/v1/notifications", query("limit=abc"))
	tr.step("a cursor with no separator", m, "GET /api/v1/notifications", query("before=abc"))
	tr.step("a cursor whose time does not parse", m, "GET /api/v1/notifications", query("before=yesterday|{{note.joined}}"))

	// The live stream: its head alone, since it replays nothing and no
	// notification is pending.
	tr.step("the member's live stream: the head of an event stream, and no frame", m,
		"GET /api/v1/notifications/stream", eventStream(0))
	tr.step("the live stream with no session: the middleware's 401", anon, "GET /api/v1/notifications/stream")

	// A worker key reaches every one of these handlers, which answer 401: they
	// serve a signed-in person only.
	for _, route := range []string{
		"GET /api/v1/notifications",
		"POST /api/v1/notifications/read",
		"POST /api/v1/notifications/read-all",
		"POST /api/v1/notifications/clear",
		"DELETE /api/v1/notifications/cleared",
		"PUT /api/v1/notifications/{id}/flag",
		"GET /api/v1/notifications/stream",
		"GET /api/v1/me/notification-prefs",
		"PUT /api/v1/me/notification-prefs",
		"GET /api/v1/me/push/config",
		"GET /api/v1/me/push-subscriptions",
		"POST /api/v1/me/push-subscriptions",
		"DELETE /api/v1/me/push-subscriptions",
	} {
		var opts []tourOpt
		if strings.Contains(route, "{id}") {
			opts = append(opts, at("id", "{{note.review}}"))
		}
		tr.step("with a worker key: the handler's 401", worker, route, opts...)
	}

	// Read, by id and all at once.
	tr.step("mark two read: how many changed, and the unread count", m, "POST /api/v1/notifications/read",
		jsonBody(`{"ids":["{{note.review}}","{{note.mention}}"]}`))
	tr.step("mark the same two read again: none changed", m, "POST /api/v1/notifications/read",
		jsonBody(`{"ids":["{{note.review}}","{{note.mention}}"]}`))
	tr.step("the owner marks one of the member's read: scoped to the caller, so none changed", o,
		"POST /api/v1/notifications/read", jsonBody(`{"ids":["{{note.project}}"]}`))
	tr.step("mark a notification that does not exist read: none changed", m, "POST /api/v1/notifications/read",
		jsonBody(`{"ids":["{{phantom}}"]}`))
	tr.step("mark read with an empty list", m, "POST /api/v1/notifications/read", jsonBody(`{"ids":[]}`))
	tr.step("mark read with no list", m, "POST /api/v1/notifications/read", jsonBody(`{}`))
	tr.step("mark read with a malformed body", m, "POST /api/v1/notifications/read", jsonBody(`{"ids":`))
	tr.step("mark read with an id that is not a UUID: none marked, as for an id no notification has", m,
		"POST /api/v1/notifications/read", jsonBody(`{"ids":["not-a-notification"]}`))
	tr.step("unread only: the four left", m, "GET /api/v1/notifications", query("unread=true"))

	// Flags.
	tr.step("flag the mention", m, "PUT /api/v1/notifications/{id}/flag", at("id", "{{note.mention}}"),
		jsonBody(`{"flagged":true}`))
	tr.step("flag with no flagged", m, "PUT /api/v1/notifications/{id}/flag", at("id", "{{note.mention}}"),
		jsonBody(`{}`))
	tr.step("flag with flagged not a boolean", m, "PUT /api/v1/notifications/{id}/flag", at("id", "{{note.mention}}"),
		jsonBody(`{"flagged":"yes"}`))
	tr.step("flag a notification that does not exist", m, "PUT /api/v1/notifications/{id}/flag",
		at("id", "{{phantom}}"), jsonBody(`{"flagged":true}`))
	tr.step("flag an id that is not a UUID: 404, as an id no notification has", m,
		"PUT /api/v1/notifications/{id}/flag", at("id", "not-a-notification"), jsonBody(`{"flagged":true}`))
	tr.step("the owner flags the member's notification: the same 404 as a missing one", o,
		"PUT /api/v1/notifications/{id}/flag", at("id", "{{note.mention}}"), jsonBody(`{"flagged":true}`))
	tr.step("the flagged view: the mention", m, "GET /api/v1/notifications", query("view=flagged"))
	tr.step("mark all read: the four left", m, "POST /api/v1/notifications/read-all")
	tr.step("mark all read again: none", m, "POST /api/v1/notifications/read-all")

	// Clear and delete.
	tr.step("clear the inbox: all six move to the cleared view", m, "POST /api/v1/notifications/clear")
	tr.step("the inbox: empty, written null", m, "GET /api/v1/notifications")
	tr.step("the cleared view: all six, stamped cleared_at", m, "GET /api/v1/notifications", query("view=cleared"))
	tr.step("the flagged view: the mention, cleared and still flagged", m, "GET /api/v1/notifications",
		query("view=flagged"))
	tr.step("unflag the mention", m, "PUT /api/v1/notifications/{id}/flag", at("id", "{{note.mention}}"),
		jsonBody(`{"flagged":false}`))
	tr.step("the flagged view: none", m, "GET /api/v1/notifications", query("view=flagged"))
	tr.step("clear an empty inbox: none moved", m, "POST /api/v1/notifications/clear")
	tr.step("delete what is cleared: all six, for good", m, "DELETE /api/v1/notifications/cleared")
	tr.step("the cleared view: none", m, "GET /api/v1/notifications", query("view=cleared"))
	tr.step("delete what is cleared again: none", m, "DELETE /api/v1/notifications/cleared")

	// The notification preferences.
	tr.step("the member's preferences: the defaults, email on and push off", m, "GET /api/v1/me/notification-prefs")
	tr.step("turn push on alone: email is left as it is", m, "PUT /api/v1/me/notification-prefs",
		jsonBody(`{"push_notifications":true}`))
	tr.step("turn email off alone: push is left as it is", m, "PUT /api/v1/me/notification-prefs",
		jsonBody(`{"email_notifications":false}`))
	tr.step("an update that names neither: nothing changes", m, "PUT /api/v1/me/notification-prefs", jsonBody(`{}`))
	tr.step("an update whose value is not a boolean", m, "PUT /api/v1/me/notification-prefs",
		jsonBody(`{"push_notifications":"yes"}`))
	tr.step("the member's preferences: email off, push on", m, "GET /api/v1/me/notification-prefs")
	tr.step("the owner's preferences: its own, the defaults", o, "GET /api/v1/me/notification-prefs")

	// Web push: the server's key and the member's devices.
	tr.step("the push configuration: on, with the server's VAPID public key", m, "GET /api/v1/me/push/config")
	tr.step("the member's devices: none, written [] (the handler writes nil as [])", m,
		"GET /api/v1/me/push-subscriptions")
	tr.step("subscribe a device: 201, the stored row, never its keys", m, "POST /api/v1/me/push-subscriptions",
		notificationsPushSubscription(notificationsPushDevice, notificationsPushP256dh, notificationsPushAuth,
			"Tour Browser/1.0")).capture("device", "/id")
	tr.step("the member's devices: the one", m, "GET /api/v1/me/push-subscriptions")
	tr.step("subscribe the same endpoint again, with new keys and no user agent: 201, the same id and created_at, "+
		"and the request's own User-Agent", m, "POST /api/v1/me/push-subscriptions",
		notificationsPushSubscription(notificationsPushDevice, notificationsPushP256dh2, notificationsPushAuth2, ""))
	tr.step("subscribe a second device with a user agent of 401 two-byte runes: kept to 400 runes", m,
		"POST /api/v1/me/push-subscriptions", notificationsPushSubscription(notificationsPushApple,
			notificationsPushP256dh2, notificationsPushAuth2, strings.Repeat("é", 401))).capture("device.apple", "/id")
	tr.step("the member's devices: newest first", m, "GET /api/v1/me/push-subscriptions")
	tr.step("withdraw the second device: 204", m, "DELETE /api/v1/me/push-subscriptions",
		notificationsPushEndpoint(notificationsPushApple))
	tr.step("withdraw it again: 204, nothing to withdraw", m, "DELETE /api/v1/me/push-subscriptions",
		notificationsPushEndpoint(notificationsPushApple))
	tr.step("withdraw with no endpoint", m, "DELETE /api/v1/me/push-subscriptions", notificationsPushEndpoint(" "))
	tr.step("withdraw with a malformed body", m, "DELETE /api/v1/me/push-subscriptions", jsonBody(`{"endpoint":`))
	tr.step("the owner's devices: none, the member's are not listed", o, "GET /api/v1/me/push-subscriptions")
	tr.step("the owner withdraws the member's endpoint: 204, and nothing of the member's goes", o,
		"DELETE /api/v1/me/push-subscriptions", notificationsPushEndpoint(notificationsPushDevice))
	tr.step("the member's devices: the one, still there", m, "GET /api/v1/me/push-subscriptions")

	// Each refusal of a subscription, in the handler's order.
	for _, c := range []struct {
		title string
		body  tourOpt
	}{
		{"subscribe with a malformed body", jsonBody(`{"endpoint":`)},
		{"subscribe with no endpoint", notificationsPushSubscription("", notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe an endpoint over 2,048 bytes", notificationsPushSubscription(notificationsPushDevice+"/"+
			strings.Repeat("x", 2048-len(notificationsPushDevice)), notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe with no auth key", notificationsPushSubscription(notificationsPushDevice, notificationsPushP256dh, "", "")},
		{"subscribe with a key over 256 bytes", notificationsPushSubscription(notificationsPushDevice,
			strings.Repeat("A", 257), notificationsPushAuth, "")},
		{"subscribe a plain-http endpoint", notificationsPushSubscription("http://fcm.googleapis.com/fcm/send/tour",
			notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe an endpoint with credentials", notificationsPushSubscription(
			"https://tour:secret@fcm.googleapis.com/fcm/send/tour", notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe an endpoint on another port", notificationsPushSubscription(
			"https://fcm.googleapis.com:8443/fcm/send/tour", notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe an address literal", notificationsPushSubscription("https://127.0.0.1/fcm/send/tour",
			notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe a host that is not a push service", notificationsPushSubscription("https://push.example.org/tour",
			notificationsPushP256dh, notificationsPushAuth, "")},
		{"subscribe push.apple.com itself: *.push.apple.com matches its subdomains only",
			notificationsPushSubscription("https://push.apple.com/tour", notificationsPushP256dh, notificationsPushAuth, "")},
	} {
		tr.step(c.title, m, "POST /api/v1/me/push-subscriptions", c.body)
	}

	// The upsert refreshes the owner too: a device another account posts is
	// that account's from then on.
	tr.step("the owner subscribes the member's endpoint: 201, the same id, now the owner's", o,
		"POST /api/v1/me/push-subscriptions", notificationsPushSubscription(notificationsPushDevice,
			notificationsPushP256dh, notificationsPushAuth, "Tour Owner's Browser"))
	tr.step("the member's devices: none", m, "GET /api/v1/me/push-subscriptions")
	tr.step("the member subscribes it back: the same id, the member's again", m, "POST /api/v1/me/push-subscriptions",
		notificationsPushSubscription(notificationsPushDevice, notificationsPushP256dh, notificationsPushAuth,
			"Tour Browser/1.0"))

	// One push: the member's role back to member (access_changed, which push
	// carries), with push on and one device on file. The recording proxy
	// refuses the push service's CONNECT, and the device is marked failed.
	tr.setup("make the member a member of W again", o, "PUT /api/v1/orgs/{id}/members/{userId}",
		at("id", w, "userId", "{{member}}"), jsonBody(`{"role":"member"}`), expect(204))
	notificationsPushInbox(tr, m, 1, "the member's access_changed notification")
	tr.awaitOutbound("CONNECT fcm.googleapis.com:443", 1)
	tr.await("the device marked failed", m, "GET /api/v1/me/push-subscriptions", func(r *tourResult) bool {
		return r.status == 200 && strings.Contains(string(r.body), `"failed_at"`)
	})
	inbox = tr.step("the member's inbox: the role change, which the server also tried to push", m,
		"GET /api/v1/notifications")
	inbox.capture("note.push", "/notifications/0/id")
	tr.step("the member's devices: the one, marked failed_at when the push service could not be reached, and kept", m,
		"GET /api/v1/me/push-subscriptions")
	tr.step("subscribe it again: the failure mark is cleared", m, "POST /api/v1/me/push-subscriptions",
		notificationsPushSubscription(notificationsPushDevice, notificationsPushP256dh, notificationsPushAuth,
			"Tour Browser/1.0"))
	tr.step("the member's devices: no failed_at", m, "GET /api/v1/me/push-subscriptions")
	tr.step("clear the inbox with the notification unread: it moves, and the count is 0", m,
		"POST /api/v1/notifications/clear")
	tr.step("the cleared view: the notification, read, since clearing marks it read", m, "GET /api/v1/notifications",
		query("view=cleared"))
}
