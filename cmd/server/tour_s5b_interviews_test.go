//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestTourS5bInterviews is the S5b tour's interviews area (refactor plan
// §6.4 S5b, before M8 splits suite_handlers.go; invariants I3, I4, I5, I8
// (Retry-After), I9 (the participant's event stream) and I10; quirks Q1,
// Q8 (the seventh limit parser), Q14 and Q19; OpenV REQ-13, REQ-23,
// REQ-143). Its golden is testdata/tour/s5b/interviews.json.
//
// The owner keeps project P, which holds a persona, a requirement and a
// product profile whose vision and problem statement (not its target users)
// the interviewer's prompt carries, and project Q, which holds a persona of
// its own. A viewer of P shows the role gates. The area walks the management
// routes: interviews created (with the refusals of their body, their persona
// and their guard; the default agent slug finds the workspace's seeded
// requirements-interviewer, an unknown one leaves the interview with no
// agent), a persona set and cleared, listed; invites created (one expired,
// one to expire, sent with an offset, read back as that instant in UTC),
// listed without their token hash, revoked (idempotently); an interview
// closed twice, and refused a new invite. Then the project-wide session list
// and its limit parser (Q8), over 21 sessions of a survey interview. Then
// the participant's side, anonymous, by invite token: the intro (read-only,
// and each refusal of a token: unknown, expired, revoked, interview
// closed), the event stream (which opens an anonymous session, then replays
// the transcript frame by frame), messages (the first names the anonymous
// session; each queues an interviewer run, whose prompt the area reads; an
// interview with no agent answers with a system note), finish (a
// chatter.created event; the intro then answers the ended session, without
// its transcript, and the next message opens a new session), and the three
// rate limits: per invite on messages (spent only once a message is read and
// has content), per network on intros and on streams (spent before the token
// is read), each with the last request its bucket lets through and the 429
// after it, with its Retry-After. Last, two accounts from workspaces of their
// own: an editor of P, whose interview gets P's workspace's interviewer
// rather than its own workspace's (every workspace is seeded with one), and
// an outsider, whom the reads of P's interviews, sessions and
// transcripts refuse.
//
// Every JSON answer but a refusal and a 429 is a bare encode with no
// Content-Type (Q1); P's interviews (I1's long brief), the lists of 20 and
// 21 sessions and the interviewer's run are long enough to be compressed,
// and are then sent with none. An empty list of interviews, invites or an
// interview's sessions answers null (Q14), and so does the transcript of a
// session with no message yet, on its own and inside the intro; the
// project's session list answers [] (its handler normalises). Texts the tour sends with <, > and &
// come back JSON-escaped, in the answers, the stream's frames and the run's
// prompt alike.
//
// Nondeterminism: only ids, tokens and minted times, and three Retry-After
// values, which count down from a bucket's first use (the area's own
// patterns). Every row comes from a request of its own, so no list ties.
// The rate-limit buckets are the area server's own: messages spend a bucket
// per invite (5, then 20 an hour), so no invite here takes more than five
// answered messages; intros and streams spend a bucket per client address
// (20 and 30, refilled at 60 and 120 an hour), which the area's own steps
// stay well inside, and the two per-network 429s are drained on a network of
// their own: the area's server trusts CF-Connecting-IP as the client's
// address (env), which only those requests send. Each bucket is drained in
// some 30 ms, far inside the second its Retry-After pattern allows. The
// queued runs stay queued (HOSTED_RUNNERS=off, no runner key), interview
// runs get no tracking card, and nothing here publishes an event but
// finish; the notifier's notification rows for it are written after the
// answer and read by no step, and send nothing out (no SMTP, no VAPID).
func TestTourS5bInterviews(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "interviews",
		about: "Stakeholder interviews: interviews, personas, invites and sessions as the project's members manage " +
			"them, and the participant's side by invite token (intro, event stream, messages, finish), with its " +
			"rate limits and the interviewer runs each message queues.",
		run: interviewsTour,
		env: map[string]string{"OPENV_CLIENT_IP_HEADER": interviewsNetworkHeader},
	})
}

// interviewsNetworkHeader is the header the area's server takes a client's
// address from, when a request sends it; interviewsNetwork is the address the
// per-network rate-limit steps send, so that they drain a bucket of their own.
const (
	interviewsNetworkHeader = "CF-Connecting-IP"
	interviewsNetwork       = "203.0.113.7"
)

// interviewsSurveySize is how many sessions the survey interview holds: one
// more than the project session list's default page of 20.
const interviewsSurveySize = 21

// interviewsSeed creates the projects, artifacts, profile and viewer the
// area reads (setup; other areas pin those routes).
func interviewsSeed(tr *tour) (viewer *tourActor) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour interviews"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour other interviews"}`)).capture("q", "/id")
	tr.setup("P's persona", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"persona",`+
		`"title":"Design engineer","body":"Designs fixtures for small machine shops, on a laptop between jobs."}`)).
		capture("persona", "/id")
	tr.setup("P's requirement", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}","type":"requirement",`+
		`"title":"Export in time","body":"The system shall export a part within 2 s."}`)).capture("req", "/id")
	tr.setup("Q's persona", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{q}}","type":"persona",`+
		`"title":"Shop owner","body":"Runs the shop."}`)).capture("persona_q", "/id")
	tr.setup("P's product profile: its vision and problem statement go into the interviewer's prompt", o,
		"PUT /api/v1/projects/{id}/profile", at("id", "{{p}}"), jsonBody(`{"vision":"Fixtures designed in minutes.",`+
			`"problem_statement":"Fixture design takes a day per part.","target_users":"Machinists (not in the prompt)"}`))
	viewer = tr.register("viewer", "Tour Viewer", "a viewer of P, from a workspace of its own: may read, not write")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	return viewer
}

// interviewsSurvey creates interview I2 in P and, for each of n invites, one
// participant message, so P holds n sessions, each started by a request of
// its own (setup: the steps below pin these routes).
func interviewsSurvey(tr *tour, n int) {
	o := tr.owner
	tr.setup("interview I2, a survey", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Survey","brief":"One question."}`), expect(201)).capture("i2", "/id")
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("r%02d", i)
		inv := tr.setup("a survey invite", o, "POST /api/v1/interviews/{id}/invites", at("id", "{{i2}}"),
			jsonBody(fmt.Sprintf(`{"invitee_label":"Respondent %02d"}`, i)), expect(201))
		inv.capture(name+".invite", "/invite/id")
		inv.capture(name+".token", "/token")
		tr.setup("a survey answer, which opens the invite's session", tr.anon,
			"POST /api/v1/public/interviews/{token}/messages", at("token", "{{"+name+".token}}"),
			jsonBody(fmt.Sprintf(`{"participant_name":"Respondent %02d","content":"Answer %02d."}`, i, i)),
			expect(200)).capture(name+".session", "/session/id")
	}
}

// interviewsInvite records an invite's creation and registers its id under
// name and its token under name.token.
func interviewsInvite(tr *tour, title, interview, name, body string) {
	r := tr.step(title, tr.owner, "POST /api/v1/interviews/{id}/invites", at("id", "{{"+interview+"}}"), jsonBody(body))
	r.capture(name, "/invite/id")
	r.capture(name+".token", "/token")
}

func interviewsTour(tr *tour) {
	o, anon := tr.owner, tr.anon
	viewer := interviewsSeed(tr)
	// A brief of 1,124 bytes (1,139 once JSON escapes its <, > and &): the
	// interviewer's run carries it in its prompt, which makes that answer long
	// enough to be compressed whatever its timestamps' lengths.
	brief := strings.Repeat("Ask about the parts they fixture, how long it takes, and what goes wrong. ", 15) +
		"<ask> & listen"
	tr.remember("unknown.token", strings.Repeat("0123456789abcdef", 4))
	tr.keep("2020-01-01T00:00:00Z", "an invite's expiry the tour sends, in the past")
	tr.keep("2099-01-01T00:00:00+01:00", "an invite's expiry the tour sends, to come, with an offset")
	tr.keep("2098-12-31T23:00:00Z", "that expiry read back: the instant sent, in UTC (a TIMESTAMPTZ since "+
		"migration 0050; a TIMESTAMP kept the wall clock and dropped the offset)")
	tr.headerPattern("Retry-After", `^(179|180)$`, "<retry-after 180 s>", "Retry-After on the sixth quick "+
		"message to one invite: ceil(180 s less the seconds since the first of the six), so 180 while they take "+
		"under a second and 179 under two (they take some 30 ms)")
	tr.headerPattern("Retry-After", `^(59|60)$`, "<retry-after 60 s>", "Retry-After on the 21st intro from one "+
		"network: ceil(60 s less the seconds since the first of them), as above")
	tr.headerPattern("Retry-After", `^(29|30)$`, "<retry-after 30 s>", "Retry-After on the 31st stream from one "+
		"network: ceil(30 s less the seconds since the first of them), as above")

	// Interviews: the list of a project with none, then the create's
	// refusals in the handler's order (guard, decode, persona, then the
	// service), and the creates.
	tr.step("the interviews of a project with none: null (Q14)", o, "GET /api/v1/projects/{id}/interviews",
		at("id", "{{q}}"))
	tr.step("create an interview with a malformed body", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{`))
	tr.step("create an interview with no name", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"  ","brief":"blank"}`))
	tr.step("create an interview whose persona no artifact is", o, "POST /api/v1/projects/{id}/interviews",
		at("id", "{{p}}"), jsonBody(`{"name":"Phantom persona","persona_artifact_id":"{{phantom}}"}`))
	tr.step("create an interview with Q's persona", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Other persona","persona_artifact_id":"{{persona_q}}"}`))
	tr.step("create an interview whose persona is a requirement", o, "POST /api/v1/projects/{id}/interviews",
		at("id", "{{p}}"), jsonBody(`{"name":"Not a persona","persona_artifact_id":"{{req}}"}`))
	tr.step("the viewer creates an interview", viewer, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Viewer interview"}`))
	tr.step("create an interview whose guided_session_id is not a UUID: the driver's text (Q19)", o,
		"POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Bad session","guided_session_id":"not-a-uuid"}`))
	i1 := tr.step("create interview I1 for P's persona: the default slug finds the workspace's requirements-interviewer", o,
		"POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Onboarding <interviews> & more","brief":"`+brief+`","persona_artifact_id":"{{persona}}"}`),
		note("the brief makes the interviewer's run long enough to be compressed"))
	i1.capture("i1", "/id")
	i1.capture("interviewer", "/agent_id")
	tr.step("create interview I0 with an agent slug the workspace does not have: created with no agent", o,
		"POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"No interviewer","brief":"Nobody asks.","agent_slug":"no-such-agent"}`)).capture("i0", "/id")

	// The persona: cleared, refused, set again.
	tr.step("clear I1's persona", o, "PUT /api/v1/interviews/{id}/persona", at("id", "{{i1}}"),
		jsonBody(`{"persona_artifact_id":null}`))
	tr.step("set the persona of an interview that does not exist", o, "PUT /api/v1/interviews/{id}/persona",
		at("id", "{{phantom}}"), jsonBody(`{"persona_artifact_id":"{{persona}}"}`))
	tr.step("the viewer sets I1's persona", viewer, "PUT /api/v1/interviews/{id}/persona", at("id", "{{i1}}"),
		jsonBody(`{"persona_artifact_id":"{{persona}}"}`))
	tr.step("set I1's persona with a malformed body", o, "PUT /api/v1/interviews/{id}/persona", at("id", "{{i1}}"),
		jsonBody(`[`))
	tr.step("set I1's persona to a requirement", o, "PUT /api/v1/interviews/{id}/persona", at("id", "{{i1}}"),
		jsonBody(`{"persona_artifact_id":"{{req}}"}`))
	tr.step("set I1's persona again", o, "PUT /api/v1/interviews/{id}/persona", at("id", "{{i1}}"),
		jsonBody(`{"persona_artifact_id":"{{persona}}"}`))
	tr.step("P's interviews, newest first: compressed, and then sent with no Content-Type (Q1)", o,
		"GET /api/v1/projects/{id}/interviews", at("id", "{{p}}"))
	tr.step("the viewer lists P's interviews", viewer, "GET /api/v1/projects/{id}/interviews", at("id", "{{p}}"))
	tr.step("the interviews of a project that does not exist", o, "GET /api/v1/projects/{id}/interviews",
		at("id", "{{phantom}}"), note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))

	// Invites: created (the answer carries the token once; the invite hides
	// its hash), refused, listed and revoked.
	tr.step("I1's invites before any: null (Q14)", o, "GET /api/v1/interviews/{id}/invites", at("id", "{{i1}}"))
	tr.step("I1's sessions before any: null (Q14)", o, "GET /api/v1/interviews/{id}/sessions", at("id", "{{i1}}"))
	tr.step("the sessions of an interview that does not exist", o, "GET /api/v1/interviews/{id}/sessions",
		at("id", "{{phantom}}"))
	tr.step("the invites of an interview that does not exist", o, "GET /api/v1/interviews/{id}/invites",
		at("id", "{{phantom}}"))
	interviewsInvite(tr, "invite Dana to I1: the token, once, and the path the participant opens", "i1", "inv1",
		`{"invitee_label":"Dana <design> & co"}`)
	tr.step("create an invite whose expires_at is not a time: the decode's refusal", o,
		"POST /api/v1/interviews/{id}/invites", at("id", "{{i1}}"), jsonBody(`{"expires_at":"soon"}`))
	tr.step("the viewer creates an invite", viewer, "POST /api/v1/interviews/{id}/invites", at("id", "{{i1}}"),
		jsonBody(`{"invitee_label":"Viewer's guest"}`))
	tr.step("create an invite to an interview that does not exist", o, "POST /api/v1/interviews/{id}/invites",
		at("id", "{{phantom}}"), jsonBody(`{"invitee_label":"Nobody"}`))
	interviewsInvite(tr, "an invite that expired in 2020", "i1", "inv_expired",
		`{"invitee_label":"Late","expires_at":"2020-01-01T00:00:00Z"}`)
	interviewsInvite(tr, "an invite that expires in 2099, sent with an offset: echoed as sent", "i1", "inv_future",
		`{"invitee_label":"Next year","expires_at":"2099-01-01T00:00:00+01:00"}`)
	interviewsInvite(tr, "an invite to revoke", "i1", "inv_revoked", `{"invitee_label":"Withdrawn"}`)
	tr.step("the viewer revokes it", viewer, "POST /api/v1/interview-invites/{id}/revoke", at("id", "{{inv_revoked}}"))
	tr.step("revoke an invite that does not exist", o, "POST /api/v1/interview-invites/{id}/revoke",
		at("id", "{{phantom}}"))
	tr.step("revoke an invite by an id that is not a UUID", o, "POST /api/v1/interview-invites/{id}/revoke",
		at("id", "not-a-uuid"), note("the lookup's driver error is answered as not found, like an id no invite has"))
	tr.step("revoke it", o, "POST /api/v1/interview-invites/{id}/revoke", at("id", "{{inv_revoked}}"))
	tr.step("revoke it again: the same answer", o, "POST /api/v1/interview-invites/{id}/revoke",
		at("id", "{{inv_revoked}}"))
	tr.step("I1's invites, newest first: no token, the 2099 expiry read back as the instant sent, in UTC, one revoked", o,
		"GET /api/v1/interviews/{id}/invites", at("id", "{{i1}}"),
		note("fixed under R7 (#379 bug 4): the TIMESTAMP column kept the wall clock sent and dropped its offset, "+
			"so the instant moved by the offset"))
	tr.step("the viewer lists I1's invites", viewer, "GET /api/v1/interviews/{id}/invites", at("id", "{{i1}}"))

	// Closing: an interview with an invite, closed twice; a closed interview
	// takes no new invite.
	tr.setup("interview Ic, to close", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Closed campaign"}`), expect(201)).capture("ic", "/id")
	closed := tr.setup("an invite to Ic", o, "POST /api/v1/interviews/{id}/invites", at("id", "{{ic}}"),
		jsonBody(`{"invitee_label":"Too late"}`), expect(201))
	closed.capture("inv_closed", "/invite/id")
	closed.capture("inv_closed.token", "/token")
	tr.step("the viewer closes Ic", viewer, "POST /api/v1/interviews/{id}/close", at("id", "{{ic}}"))
	tr.step("close an interview that does not exist", o, "POST /api/v1/interviews/{id}/close", at("id", "{{phantom}}"))
	tr.step("close Ic", o, "POST /api/v1/interviews/{id}/close", at("id", "{{ic}}"))
	tr.step("close Ic again: 200 too, nothing checks the status; updated_at moves", o, "POST /api/v1/interviews/{id}/close",
		at("id", "{{ic}}"))
	tr.step("invite to the closed Ic", o, "POST /api/v1/interviews/{id}/invites", at("id", "{{ic}}"),
		jsonBody(`{"invitee_label":"Later still"}`))

	// The project's sessions, newest first, and the seventh limit parser
	// (Q8): a value that is not an integer is refused; 0 and below mean the
	// default page of 20; above the cap of 100 means 100 (the exact cap would
	// take 101 sessions: X3a's table test). Over the survey's 21 sessions.
	interviewsSurvey(tr, interviewsSurveySize)
	tr.step("P's sessions with a limit that is not an integer", o, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=x"))
	tr.step("P's sessions with no limit: the default page, 20 of the 21", o,
		"GET /api/v1/projects/{id}/interview-sessions", at("id", "{{p}}"))
	tr.step("P's sessions with limit 0: the default page", o, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=0"))
	tr.step("P's sessions with limit -1: the default page", o, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=-1"))
	tr.step("P's sessions with limit 1: the newest", o, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=1"))
	tr.step("P's sessions with limit 500: capped at 100, so all 21", o, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=500"))
	tr.step("the viewer reads P's newest session", viewer, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=1"))
	tr.step("the sessions of a project with no interview: [], not null (the handler normalises)", o,
		"GET /api/v1/projects/{id}/interview-sessions", at("id", "{{q}}"))

	// The participant's side, anonymous: the intro and each refusal of a
	// token. The intros spend the per-address bucket (20), so the refusals
	// are sent once. Sam's invite takes the refused messages below, which
	// must spend nothing of its bucket: its five quick messages at the end
	// would otherwise not all be answered.
	burst := tr.setup("an invite for Sam, who writes quickly", o, "POST /api/v1/interviews/{id}/invites",
		at("id", "{{i1}}"), jsonBody(`{"invitee_label":"Sam"}`), expect(201))
	burst.capture("inv_burst", "/invite/id")
	burst.capture("inv_burst.token", "/token")
	spent := once("each intro spends a token of the per-address bucket")
	tr.step("the intro of Dana's invite: no session yet", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{inv1.token}}"))
	tr.step("I1's sessions after the intro: still null, the intro is read-only", o,
		"GET /api/v1/interviews/{id}/sessions", at("id", "{{i1}}"))
	tr.step("the intro of a token no invite has", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{unknown.token}}"), spent)
	tr.step("the intro of the expired invite", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{inv_expired.token}}"), spent)
	tr.step("the intro of the revoked invite", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{inv_revoked.token}}"), spent)
	tr.step("the intro of an invite to the closed interview", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{inv_closed.token}}"), spent)
	tr.step("a message by a token no invite has", anon, "POST /api/v1/public/interviews/{token}/messages",
		at("token", "{{unknown.token}}"), jsonBody(`{"participant_name":"Dana","content":"Hello"}`))
	tr.step("a message to the closed interview", anon, "POST /api/v1/public/interviews/{token}/messages",
		at("token", "{{inv_closed.token}}"), jsonBody(`{"participant_name":"Dana","content":"Hello"}`))
	tr.step("a message with a malformed body: refused before the invite's bucket is spent", anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv_burst.token}}"), jsonBody(`{"content":`))
	tr.step("a message with no content: refused before the invite's bucket is spent", anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv_burst.token}}"),
		jsonBody(`{"participant_name":"Sam","content":" "}`))
	tr.step("the stream of a token no invite has", anon, "GET /api/v1/public/interviews/{token}/stream",
		at("token", "{{unknown.token}}"), eventStream(0))
	tr.step("the stream of the revoked invite", anon, "GET /api/v1/public/interviews/{token}/stream",
		at("token", "{{inv_revoked.token}}"), eventStream(0))

	// Dana's session: the stream opens it, anonymous, before any message;
	// the first message names it (#205), the second does not rename it.
	tr.step("Dana's stream, before any message: the head, no frame; it opens an anonymous session", anon,
		"GET /api/v1/public/interviews/{token}/stream", at("token", "{{inv1.token}}"), eventStream(0))
	tr.step("I1's sessions: the anonymous one the stream opened", o, "GET /api/v1/interviews/{id}/sessions",
		at("id", "{{i1}}")).capture("s1", "/0/id")
	tr.step("the transcript of Dana's session before any message: null (Q14)", o,
		"GET /api/v1/interview-sessions/{id}/transcript", at("id", "{{s1}}"),
		note("the repository's own nil list, where the intro's transcript before any session is the handler's"))
	tr.step("the intro of Dana's invite: the session, with no message yet, its transcript null (Q14)", anon,
		"GET /api/v1/public/interviews/{token}", at("token", "{{inv1.token}}"), spent)
	tr.step("Dana's first message: it names the session and queues an interviewer run (no event)", anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv1.token}}"),
		jsonBody(`{"participant_name":"Dana","content":"I need <fast> exports & a clear error when a part is too big."}`)).
		capture("m1", "/message/id")
	tr.step("a second message under another name: the session keeps Dana's", anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv1.token}}"),
		jsonBody(`{"participant_name":"Someone else","content":"Also: offline mode."}`)).capture("m2", "/message/id")
	tr.step("the intro of Dana's invite: the session and its transcript", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{inv1.token}}"))
	tr.step("Dana's stream: the transcript replayed, a frame per message", anon,
		"GET /api/v1/public/interviews/{token}/stream", at("token", "{{inv1.token}}"), eventStream(2))
	tr.step("Dana's transcript", o, "GET /api/v1/interview-sessions/{id}/transcript", at("id", "{{s1}}"))
	tr.step("the viewer reads Dana's transcript", viewer, "GET /api/v1/interview-sessions/{id}/transcript",
		at("id", "{{s1}}"))
	tr.step("the transcript of a session that does not exist", o, "GET /api/v1/interview-sessions/{id}/transcript",
		at("id", "{{phantom}}"))
	tr.step("the transcript of a session id that is not a UUID", o, "GET /api/v1/interview-sessions/{id}/transcript",
		at("id", "not-a-uuid"))

	// The interviewer run the second message queued: its prompt (an S5d
	// route, read here for what the interview handler writes into it).
	tr.setup("P's agent runs", o, "GET /api/v1/agent-runs", query("project_id={{p}}")).
		captureWhere("s1.run", "", "interview_session_id", tr.id("s1"), "id")
	tr.step("the interviewer run Dana's second message queued: brief, persona, vision and problem, participant, "+
		"transcript", o, "GET /api/v1/agent-runs/{id}", at("id", "{{s1.run}}"),
		note("an S5d route, read for the prompt the interview handler builds; the run stays queued "+
			"(HOSTED_RUNNERS=off, no runner) and gets no tracking card"))

	// Finishing: with no session, with one (an event), and again; the intro
	// then answers the ended session, without its transcript, and the next
	// message opens a new one.
	tr.step("finish an invite with no session: nothing to finish", anon, "POST /api/v1/public/interviews/{token}/finish",
		at("token", "{{inv_future.token}}"))
	tr.step("finish the expired invite", anon, "POST /api/v1/public/interviews/{token}/finish",
		at("token", "{{inv_expired.token}}"))
	tr.step("Dana finishes: chatter.created, published by no one (system)", anon,
		"POST /api/v1/public/interviews/{token}/finish", at("token", "{{inv1.token}}"))
	tr.step("Dana finishes again: no active session, so nothing to finish", anon,
		"POST /api/v1/public/interviews/{token}/finish", at("token", "{{inv1.token}}"))
	tr.step("the intro once Dana finished: her latest session, completed, without its transcript (#379 bug 214)",
		anon, "GET /api/v1/public/interviews/{token}", at("token", "{{inv1.token}}"), spent,
		note("no session is active, so the invite's latest answers, without its transcript (only an active "+
			"session's is sent), and the page shows its thank-you with no stream"))
	again := tr.step("Dana writes again: a new session", anon, "POST /api/v1/public/interviews/{token}/messages",
		at("token", "{{inv1.token}}"), jsonBody(`{"participant_name":"Dana","content":"One more thing: metric units."}`))
	again.capture("s2", "/session/id")
	again.capture("m3", "/message/id")
	tr.step("I1's sessions, newest first: the new one active, the finished one completed", o,
		"GET /api/v1/interviews/{id}/sessions", at("id", "{{i1}}"))

	// An interview with no agent: the message is kept, and a system note
	// says so.
	noAgent := tr.setup("an invite to I0", o, "POST /api/v1/interviews/{id}/invites", at("id", "{{i0}}"),
		jsonBody(`{"invitee_label":"Lee"}`), expect(201))
	noAgent.capture("inv0", "/invite/id")
	noAgent.capture("inv0.token", "/token")
	lee := tr.step("Lee's message to I0, which has no interviewer: kept, and a system note added", anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv0.token}}"),
		jsonBody(`{"participant_name":"Lee","content":"Hello?"}`))
	lee.capture("s0", "/session/id")
	lee.capture("m0", "/message/id")
	tr.step("Lee's stream: the message and the system note", anon, "GET /api/v1/public/interviews/{token}/stream",
		at("token", "{{inv0.token}}"), eventStream(2))
	tr.step("Lee's transcript", o, "GET /api/v1/interview-sessions/{id}/transcript", at("id", "{{s0}}")).
		capture("note0", "/1/id")

	// The rate limits, each drained quickly so that its Retry-After is
	// taken within a second of the bucket's first use; the last request a
	// bucket still lets through is recorded too, which pins its size. The
	// draining requests are probes, so that a bucket spent sooner shows as a
	// recorded step's answer. Messages: five per invite at once, then one
	// per 180 s; the two refused messages to Sam's invite above spent
	// nothing of it.
	for i := 1; i <= 4; i++ {
		tr.probe(anon, "POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv_burst.token}}"),
			jsonBody(fmt.Sprintf(`{"participant_name":"Sam","content":"Point %d."}`, i)))
	}
	fifth := tr.step("Sam's fifth quick message: still answered, in the session his first opened", anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{inv_burst.token}}"),
		jsonBody(`{"participant_name":"Sam","content":"Point 5."}`),
		note("four messages to this invite went before it, within the second"))
	fifth.capture("s_burst", "/session/id")
	fifth.capture("m_burst", "/message/id")
	tr.step("Sam's sixth quick message: 429", anon, "POST /api/v1/public/interviews/{token}/messages",
		at("token", "{{inv_burst.token}}"), jsonBody(`{"participant_name":"Sam","content":"Point 6."}`))
	// Intros and streams: a bucket per client address, 20 and 30, drained
	// here from a network of their own with a token no invite has: each
	// limiter runs before the token is resolved.
	network := withHeader(interviewsNetworkHeader, interviewsNetwork)
	for i := 1; i <= 19; i++ {
		tr.probe(anon, "GET /api/v1/public/interviews/{token}", at("token", "{{unknown.token}}"), network)
	}
	tr.step("a 20th intro from one network: still let through to the token's check", anon,
		"GET /api/v1/public/interviews/{token}", at("token", "{{unknown.token}}"), network, once("a rate-limited GET"),
		note("19 intros of this token went from this network before it, within the second"))
	tr.step("a 21st intro from one network: 429, even for a valid token", anon, "GET /api/v1/public/interviews/{token}",
		at("token", "{{inv1.token}}"), network, once("a rate-limited GET"))
	for i := 1; i <= 29; i++ {
		tr.probe(anon, "GET /api/v1/public/interviews/{token}/stream", at("token", "{{unknown.token}}"), network)
	}
	tr.step("a 30th stream from one network: still let through to the token's check", anon,
		"GET /api/v1/public/interviews/{token}/stream", at("token", "{{unknown.token}}"), network, eventStream(0),
		note("29 streams of this token went from this network before it, within the second"))
	tr.step("a 31st stream from one network: 429, even for a valid token", anon,
		"GET /api/v1/public/interviews/{token}/stream", at("token", "{{inv1.token}}"), network, eventStream(0))
	tr.step("P's four newest sessions, across its interviews", o, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"), query("limit=4"))
	interviewsOtherWorkspaces(tr)
}

// interviewsOtherWorkspaces is what accounts from other workspaces meet: an
// editor of P, whose interview gets the interviewer of P's workspace, not
// of its own (CreateInterview resolves the agent in the project's
// workspace), and an outsider, whom every read of P's interviews, invites,
// sessions and transcripts refuses once the entity is found.
func interviewsOtherWorkspaces(tr *tour) {
	o := tr.owner
	editor := tr.register("editor", "Tour Editor", "an editor of P, from a workspace of its own, with a "+
		"requirements-interviewer of its own: its interview uses P's workspace's")
	tr.setup("the editor joins P as an editor", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-editor@example.com","role":"editor"}`), expect(201))
	tr.step("an editor of P from another workspace creates an interview: the default slug finds P's workspace's "+
		"requirements-interviewer, not its own", editor, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Editor interview"}`))
	outsider := tr.register("outsider", "Tour Outsider", "an ordinary account in a workspace of its own, with no "+
		"access to P")
	tr.step("the outsider lists I1's invites: the interview found, then the guard", outsider,
		"GET /api/v1/interviews/{id}/invites", at("id", "{{i1}}"))
	tr.step("the outsider lists I1's sessions", outsider, "GET /api/v1/interviews/{id}/sessions", at("id", "{{i1}}"))
	tr.step("the outsider lists P's sessions", outsider, "GET /api/v1/projects/{id}/interview-sessions",
		at("id", "{{p}}"))
	tr.step("the outsider reads Dana's transcript", outsider, "GET /api/v1/interview-sessions/{id}/transcript",
		at("id", "{{s1}}"))
}
