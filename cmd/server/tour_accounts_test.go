//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// What the API tour gained for S5c, identity and workspace (refactor plan
// §6.4 S5a-S5e), beside tour_mail_test.go and tour_standin_test.go: accounts
// and sessions that recorded steps make, a signed-in request with no
// X-Org-ID, flow cookies, captures from inside a value, waits for what the
// server does after it answers, values whose length varies by design, and
// the server settings an area boots with (S4b's profiles, a second server to
// sign up on, files). Each is inert for an area that does not use it, so no
// S5a or S5b golden changed.
//
// SESSIONS. S5c records registration, login, logout and single sign-on, so
// the accounts they make are recorded steps, not tour.register's setups.
// tour.adopt turns such a step's session cookie into an actor (its id,
// address and personal workspace read back through the API, as setup), and
// tour.session a second session of an account the tour already has: to sign
// one out, to see a password change end the others, or to switch the
// workspace one session is active in (POST /api/v1/orgs/{id}/activate binds
// the session, not the account). Never sign the owner out or change its
// password: it reads the events after every step.
//
// NO X-Org-ID. A signed-in actor's requests carry X-Org-ID, as the frontend
// sends it; noOrgHeader sends one without, so the server resolves the
// workspace itself (the header, the session's active workspace, the
// account's default, its personal one), and the step records acting_in
// "(no X-Org-ID ...)". An owner's step without one reads its events without
// one too, in the workspace the server resolves.

// tourNoOrg is how a step sent with noOrgHeader records acting_in.
const tourNoOrg = "(no X-Org-ID: the server resolves the workspace)"

// noOrgHeader sends a signed-in actor's request with no X-Org-ID.
func noOrgHeader() tourOpt {
	return func(tr *tour, r *tourReq) {
		none := ""
		r.org, r.noOrg = &none, true
	}
}

// withCookie sends a cookie (filled) beside the actor's session, if any: a
// flow cookie such as the OAuth state a sign-on start set. The step lists it
// in its request headers (the session cookie itself never is).
func withCookie(name, value string) tourOpt {
	return func(tr *tour, r *tourReq) {
		c := name + "=" + tr.fill(value)
		if prev := r.header.Get("Cookie"); prev != "" {
			c = prev + "; " + c
		}
		r.header.Set("Cookie", c)
	}
}

// tourAccount is an account an area registers at its start
// (tourArea.accounts), after admin and owner.
type tourAccount struct {
	name, display, about string
}

// actor is an actor of the area's by name.
func (tr *tour) actor(name string) *tourActor {
	tr.t.Helper()
	for _, a := range tr.actors {
		if a.name == name {
			return a
		}
	}
	tr.t.Fatalf("the area has no actor %s", name)
	return nil
}

// sessionToken is the openv_session value an answer set.
func (r *tourResult) sessionToken() string {
	r.tr.t.Helper()
	for _, c := range readSetCookies(r.header) {
		if c.Name == "openv_session" && c.Value != "" {
			return c.Value
		}
	}
	r.tr.t.Fatalf("%s set no openv_session cookie\n%s", r.what(), r.body)
	return ""
}

// adopt makes an actor of the account a recorded step signed in (a
// registration, a login, a sign-on callback, a sign-up by invitation):
// its session is the openv_session cookie the answer set, and its id,
// address and name come from GET /api/v1/auth/me (setup), its personal
// workspace from GET /api/v1/orgs (a probe). name, name.session and
// name.workspace become registered names. The actor acts in its personal
// workspace, like tour.register's. An account whose address the server
// still walls (unverified, with verification required) has no workspace
// yet, and its requests carry no X-Org-ID: once it is verified, tour.home
// reads it.
func (tr *tour) adopt(res *tourResult, name, about string) *tourActor {
	tr.t.Helper()
	a := &tourActor{name: name, about: about, session: res.sessionToken()}
	tr.remember(name+".session", a.session)
	me := tr.setup("who "+name+" is", a, "GET /api/v1/auth/me")
	a.userID, a.email = me.value("/id"), me.value("/email")
	if v, _ := jsonValue(me.body, "/name"); v != nil {
		a.display, _ = v.(string)
	}
	tr.remember(name, a.userID)
	if orgs := tr.probe(a, "GET /api/v1/orgs"); orgs.status == http.StatusOK {
		tr.settleHome(a, orgs)
	}
	tr.actors = append(tr.actors, a)
	return a
}

// home reads, as setup, the personal workspace of an adopted actor that had
// none (its address was walled until verified), registers it as
// name.workspace and moves the actor there.
func (tr *tour) home(a *tourActor) string {
	tr.t.Helper()
	tr.settleHome(a, tr.setup("the workspaces of "+a.name, a, "GET /api/v1/orgs"))
	return a.home
}

func (tr *tour) settleHome(a *tourActor, orgs *tourResult) {
	tr.t.Helper()
	a.home = tr.personalWorkspace(orgs)
	a.org = a.home
	tr.remember(a.name+".workspace", a.home)
}

// personalWorkspace is the personal workspace in a GET /api/v1/orgs answer.
func (tr *tour) personalWorkspace(orgs *tourResult) string {
	tr.t.Helper()
	spans, err := jsonFind(orgs.body, "/orgs/*")
	if err == nil {
		for _, sp := range spans {
			el := orgs.body[sp.start:sp.end]
			if v, _ := jsonValue(el, "/type"); v == "personal" {
				id, _ := jsonValue(el, "/id")
				if s, _ := id.(string); s != "" {
					return s
				}
			}
		}
	}
	tr.t.Fatalf("no personal workspace in %s\n%s", orgs.what(), orgs.body)
	return ""
}

// session makes an actor of another session of an account the tour
// already has, from the recorded step (a login) whose answer set it: the
// same account, id and workspace, and a session of its own, registered as
// name.session. A setup GET /api/v1/auth/me checks it is that account's.
func (tr *tour) session(of *tourActor, res *tourResult, name, about string) *tourActor {
	tr.t.Helper()
	a := &tourActor{name: name, email: of.email, display: of.display, about: about, session: res.sessionToken(),
		userID: of.userID, home: of.home, org: of.home}
	tr.remember(name+".session", a.session)
	if id := tr.setup("whose session "+name+" is", a, "GET /api/v1/auth/me").value("/id"); id != of.userID {
		tr.t.Fatalf("%s's session is not %s's account", name, of.name)
	}
	tr.actors = append(tr.actors, a)
	return a
}

// captureCookie registers, under name, the value of the cookie the answer
// set (an OAuth state).
func (r *tourResult) captureCookie(name, cookie string) string {
	r.tr.t.Helper()
	for _, c := range readSetCookies(r.header) {
		if c.Name == cookie && c.Value != "" {
			r.tr.remember(name, c.Value)
			return c.Value
		}
	}
	r.tr.t.Fatalf("%s set no %s cookie", r.what(), cookie)
	return ""
}

// captureMatch registers, under name, the first group of re (the whole
// match when it has none) in the string at a JSON pointer of the answer:
// a token inside a link.
func (r *tourResult) captureMatch(name, pointer, re string) string {
	r.tr.t.Helper()
	return r.tr.captureIn(name, r.value(pointer), re, fmt.Sprintf("%s of %s", pointer, r.what()))
}

// captureHeader registers, under name, the first group of re (the whole
// match when it has none) in the answer's header: the state in a redirect's
// Location.
func (r *tourResult) captureHeader(name, header, re string) string {
	r.tr.t.Helper()
	return r.tr.captureIn(name, r.header.Get(header), re, fmt.Sprintf("the %s header of %s", header, r.what()))
}

// captureIf is capture for a value the answer may not hold: it registers the
// string at a JSON pointer of the answer under name and says so, or, when
// the answer holds no non-empty string there, registers nothing and says
// not, without failing, so that an area can run on after an answer that
// changed and let the golden's comparison show what did.
func (r *tourResult) captureIf(name, pointer string) (string, bool) {
	r.tr.t.Helper()
	v, err := jsonValue(r.body, pointer)
	if s, ok := v.(string); err == nil && ok && s != "" {
		r.tr.remember(name, s)
		return s, true
	}
	return "", false
}

// note adds a line of explanation to the recorded step an answer came from,
// once the answer is read: what an area computes from it, such as the
// length of a lease between two future times, which the golden writes as
// <time>.
func (r *tourResult) note(text string) {
	r.tr.t.Helper()
	if r.step == nil {
		r.tr.t.Fatalf("note: %s is not a recorded step's", r.what())
	}
	r.step.req.notes = append(r.step.req.notes, text)
}

// spendsRegistration is whether a request spends a token of the server's
// registration limit for the tour's own address (registerIPLimiter, keyed
// by clientIP): a POST /api/v1/auth/register whose body decodes, since the
// handler decodes the body before it asks the limiter, that names no client
// address of its own in the header OPENV_CLIENT_IP_HEADER names (an area
// that sets it may send requests from other addresses), nor, when the area
// declares proxy hops, in X-Forwarded-For or X-Real-IP.
func (tr *tour) spendsRegistration(r *tourReq) bool {
	if r.route != "POST /api/v1/auth/register" || !r.sendBody {
		return false
	}
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		Name        string `json:"name"`
		InviteToken string `json:"invite_token"`
	}
	if json.NewDecoder(bytes.NewReader(r.body)).Decode(&body) != nil {
		return false
	}
	if h := strings.TrimSpace(tr.env["OPENV_CLIENT_IP_HEADER"]); h != "" && strings.TrimSpace(r.header.Get(h)) != "" {
		return false
	}
	if tr.env["OPENV_TRUSTED_PROXY_HOPS"] != "" || tr.env["OPENV_TRUST_PROXY"] == "1" {
		return r.header.Get("X-Forwarded-For") == "" && r.header.Get("X-Real-IP") == ""
	}
	return true
}

func (tr *tour) captureIn(name, text, re, where string) string {
	tr.t.Helper()
	m := regexp.MustCompile(re).FindStringSubmatch(text)
	if m == nil {
		tr.t.Fatalf("%s does not match %s: %q", where, re, text)
	}
	v := m[0]
	if len(m) > 1 {
		v = m[1]
	}
	tr.remember(name, v)
	return v
}

// tourAwaitWithin bounds a wait for what the server does after it answers.
const tourAwaitWithin = 10 * time.Second

// await sends a request as a probe until until holds, for what the server
// writes after it answers (a notification a bus subscriber writes, an
// invitation marked as mailed): a recorded read before it would race it. It
// fails after tourAwaitWithin, with the last answer.
//
// A subscriber reads the state it acts on when it handles an event, not
// when the event was published: the notifier reads who is an admin or an
// editor then. So a setup that changes that state (a role) must await what
// the previous cause produces first; otherwise the subscriber may see the
// later state (a member made an admin in time to be alerted of its own
// arrival), a flake that shows only on some runs.
func (tr *tour) await(what string, a *tourActor, route string, until func(*tourResult) bool, opts ...tourOpt) *tourResult {
	tr.t.Helper()
	deadline := time.Now().Add(tourAwaitWithin)
	for {
		r := tr.probe(a, route, opts...)
		if until(r) {
			return r
		}
		if time.Now().After(deadline) {
			tr.t.Fatalf("waited %s for %s (%s as %s); the last answer: %d %s", tourAwaitWithin, what, route, a.name,
				r.status, r.body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// awaitOutbound waits until the recording proxy has seen n requests whose
// summary starts with prefix ("CONNECT fcm.googleapis.com:443"): one the
// server sends from a goroutine after it answers (a web push), so that the
// golden's outbound_requests does not depend on when the area ends.
func (tr *tour) awaitOutbound(prefix string, n int) {
	tr.t.Helper()
	deadline := time.Now().Add(tourAwaitWithin)
	for {
		tr.proxy.mu.Lock()
		got := 0
		for _, s := range tr.proxy.seen {
			if strings.HasPrefix(s, prefix) {
				got++
			}
		}
		tr.proxy.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			tr.t.Fatalf("waited %s for %d outbound requests %s...; the proxy saw %d: %v", tourAwaitWithin, n, prefix,
				got, tr.proxy.summary())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ----------------------------------------------------------------------------
// Values whose length varies by design

// tourElision is a JSON value a step keeps out of the golden (elide).
type tourElision struct {
	pointer, token, why string
	minLen, maxLen      int // the band of raw bytes the value takes
}

// tourNoMore is elide's maxLen for a value with no upper bound.
const tourNoMore = math.MaxInt32

// elide keeps a JSON value of the answer out of the golden: every value the
// pointer names ("*" for every member or element) is written as the JSON
// string "<token>", in the plain answer and its gzip variant, and the step
// lists why. It is for a value that changes by design outside any refactor
// (the release history GET /api/v1/release answers, which every promotion
// adds to, or a stable marker the monthly cut writes): the rest of the
// bytes, the key order among them, stay pinned. minLen and maxLen are the
// band of bytes the value takes (tourNoMore for no upper bound), so that the
// gzip check can tell whether the answer can fall on either side of the
// compressor's floor; the step's content_length is "<varies>". A pointer
// that names no value fails the step. It also serves a value the server
// stamps after it answers, which a step can therefore hold or not by
// chance, such as an invitation's last_emailed_at, stamped by the goroutine
// that sends its mail: the step says why, and the mail itself is pinned in
// outbound_mail.
func elide(pointer, token string, minLen, maxLen int, why string) tourOpt {
	return func(tr *tour, r *tourReq) {
		if !strings.HasPrefix(token, "<") || !strings.HasSuffix(token, ">") || strings.ContainsAny(token, "\"\\") {
			tr.t.Fatalf("elide %s: a token is written <like this>, with no quote or backslash (%q)", pointer, token)
		}
		if minLen < 1 || maxLen < minLen {
			tr.t.Fatalf("elide %s: the band %d to %d is not one", pointer, minLen, maxLen)
		}
		r.elide = append(r.elide, tourElision{pointer: pointer, token: token, why: why, minLen: minLen, maxLen: maxLen})
	}
}

// elideBody writes a step's elided values as their tokens.
func (tr *tour) elideBody(st *tourStep, body []byte) []byte {
	for _, e := range st.req.elide {
		out, _, err := jsonReplace(body, e.pointer, []byte(strconv.Quote(e.token)))
		if err != nil {
			tr.t.Errorf("step %d (%s): elide %s: %v", st.n, st.req.route, e.pointer, err)
			return body
		}
		body = out
	}
	return body
}

// elidedBand is the band of raw lengths an answer with elided values stands
// for: the rest's band, plus each value's.
func (tr *tour) elidedBand(n *tourNormaliser, st *tourStep, raw []byte) (int, int) {
	body, addLo, addHi := raw, 0, 0
	for _, e := range st.req.elide {
		tok := strconv.Quote(e.token)
		out, count, err := jsonReplace(body, e.pointer, []byte(tok))
		if err != nil {
			return bodyBand(n, raw) // elideBody reports it
		}
		body, addLo = out, addLo+count*(e.minLen-len(tok))
		if e.maxLen == tourNoMore || addHi == tourNoMore {
			addHi = tourNoMore
		} else {
			addHi += count * (e.maxLen - len(tok))
		}
	}
	lo, hi := bodyBand(n, body)
	if addHi == tourNoMore {
		return lo + addLo, tourNoMore
	}
	return lo + addLo, hi + addHi
}

// jsonReplace writes repl in place of every value a pointer names, and
// says how many there were; none is an error.
func jsonReplace(data []byte, pointer string, repl []byte) ([]byte, int, error) {
	spans, err := jsonFind(data, pointer)
	if err != nil {
		return nil, 0, err
	}
	if len(spans) == 0 {
		return nil, 0, fmt.Errorf("%s names no value of the answer", pointer)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	out := append([]byte(nil), data...)
	for _, sp := range spans {
		out = append(append(append([]byte(nil), out[:sp.start]...), repl...), out[sp.end:]...)
	}
	return out, len(spans), nil
}

// patternVarying is pattern for a value whose length varies by design (the
// feature map, which gains a key with every New features release): every
// match of re (its first group, when it has one) becomes token and stands
// for minLen to maxLen bytes, so an answer that holds it records
// content_length "<varies>" and the gzip check uses that band. It adds a
// line to the golden's legend.
func (tr *tour) patternVarying(re, token string, minLen, maxLen int, why string) {
	if minLen < 0 || maxLen < minLen || maxLen == 0 {
		tr.t.Fatalf("patternVarying %s: the band %d to %d is not one", token, minLen, maxLen)
	}
	tr.norm.addPattern(tourPattern{class: token, re: regexp.MustCompile(re), flat: token, minLen: minLen, maxLen: maxLen})
	tr.legend = append(tr.legend, fmt.Sprintf("%s: %s (the area's own pattern %s, standing for %d to %d bytes)", token,
		why, re, minLen, maxLen))
}

// ----------------------------------------------------------------------------
// Server settings: S4b's profiles, a second server to sign up on, files

// s4bProfile is the S4b profile of that name.
func s4bProfile(name string) (bootProfile, bool) {
	for _, p := range s4bProfiles {
		if p.name == name {
			return p, true
		}
	}
	return bootProfile{}, false
}

func s4bProfileNames() []string {
	var out []string
	for _, p := range s4bProfiles {
		out = append(out, p.name)
	}
	return out
}

// settings reads the area's profiles for the golden, and returns the
// background lines to await at boot and the variables to sign up without:
// a profile's own, then the area's. A profile that makes its account on a
// second server (registration_closed) signs up without its variables. The
// billing profile's lines are the reconcile's failures against the refusing
// proxy, which a stand-in for the provider would answer instead, so the two
// do not go together: an area with a Stripe stand-in sets billing's
// variables in its own env.
func (tr *tour) settings() (async, signUpWithout []string) {
	for _, name := range tr.area.profiles {
		p, _ := s4bProfile(name) // tourServerEnv refused an unknown name
		if len(p.async) > 0 && len(tr.area.standIns) > 0 {
			tr.t.Fatalf("profile %s awaits the log lines its provider's refusals write, which the area's stand-ins "+
				"would answer instead: set the profile's variables in env, and await the stand-in's boot requests "+
				"(tourArea.bootStandIns)", name)
		}
		tr.profiles = append(tr.profiles, p.name+": "+p.about)
		async = append(async, p.async...)
		if p.signUp != nil {
			signUpWithout = append(signUpWithout, sortedKeys(p.env)...)
		}
	}
	return append(async, tr.area.async...), append(signUpWithout, tr.area.signUpWithout...)
}

// signUpElsewhere registers the area's first accounts on a second server,
// booted from the same binary on the same database without some variables,
// then stopped (tourArea.signUpWithout; S4b's signUpOnOpenServer): for a
// server that would refuse them or wall them. The server under test booted
// first, on the fresh database; the second server's requests go to a
// recording proxy of its own, which refuses them and is not recorded, and
// its requests are not the tour's (neither steps nor counted in /metrics).
// The sessions it issued are rows of the database, which the server under
// test honours; the rest of each account (its workspace, and its address's
// verification when the server under test walls it) is read there.
func (tr *tour) signUpElsewhere(bin string, without []string, accounts []tourAccount) {
	t := tr.t
	t.Helper()
	for _, k := range without {
		if _, ok := tr.env[k]; !ok {
			t.Fatalf("signUpWithout names %s, which the server's environment does not set", k)
		}
	}
	env := map[string]string{}
	for k, v := range tr.env {
		if !contains(without, k) {
			env[k] = v
		}
	}
	env = startRecordingProxy(t).env(env)
	var helper *serverProcess
	for attempt := 1; helper == nil; attempt++ {
		s, err := startServer(t, bin, tr.db, env)
		switch {
		case err == nil:
			helper = s
		case !errors.Is(err, errPortTaken) || attempt == 3:
			t.Fatalf("the second server, without %s, did not come up: %v\n%s", strings.Join(without, ", "), err, s.output())
		}
	}
	var names []string
	for _, acc := range accounts {
		a := &tourActor{name: acc.name, email: "tour-" + acc.name + "@example.com", display: acc.display, about: acc.about}
		req, err := http.NewRequest(http.MethodPost, helper.base+"/api/v1/auth/register", bytes.NewReader(tourSignUpBody(a)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := helper.client().Do(req)
		if err != nil {
			t.Fatalf("register %s on the second server: %v\n%s", a.name, err, helper.output())
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("register %s on the second server, without %s: %s %s\n%s", a.name, strings.Join(without, ", "),
				resp.Status, body, helper.output())
		}
		tr.enrol(a, &tourResult{tr: tr, status: resp.StatusCode, header: resp.Header, body: body})
		names = append(names, a.name)
	}
	if _, _, err := helper.terminate(30 * time.Second); err != nil {
		t.Fatalf("stop the second server: %v\n%s", err, helper.output())
	}
	tr.signedUp = fmt.Sprintf("%s registered on a second server, booted on the same database after the server under "+
		"test with %s unset, then stopped (its own requests are not recorded); the sessions it issued are rows of "+
		"that database, which the server under test honours", strings.Join(names, ", "), strings.Join(without, ", "))
}

// tourFilesTime is the modification time of every area file, so that a
// Last-Modified or an If-Modified-Since answer does not depend on when the
// test ran.
var tourFilesTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// prepareFiles writes the area's files under a directory of the test's own
// and fills {{files}} in the environment with it; the golden shows the
// directory as <area files>.
func (tr *tour) prepareFiles(env map[string]string) map[string]string {
	t := tr.t
	t.Helper()
	if len(tr.area.files) == 0 {
		return env
	}
	dir := filepath.Join(t.TempDir(), "files")
	for _, p := range sortedKeys(tr.area.files) {
		if p == "" || filepath.IsAbs(p) || strings.Contains(p, "..") || strings.Contains(p, "\\") {
			t.Fatalf("files: %q is not a relative slash-separated path", p)
		}
		path := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, tr.area.files[p], 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, tourFilesTime, tourFilesTime); err != nil {
			t.Fatal(err)
		}
	}
	tr.files = dir
	for _, k := range sortedKeys(env) {
		if v := env[k]; strings.Contains(v, "{{files}}") {
			env[k] = strings.ReplaceAll(v, "{{files}}", dir)
			tr.shown[k] = strings.ReplaceAll(v, "{{files}}", "<area files>")
		}
	}
	return env
}

// ----------------------------------------------------------------------------
// A check with no database

// TestTourAccountOptions checks, with no database, what S5c added to the
// tour's requests, against a server that answers as the auth routes do: an
// account adopted from a recorded sign-in, and another session of it; a
// request with no X-Org-ID and one with a flow cookie beside the session,
// as sent and as recorded; the captures; the server's address written
// URL-escaped; an elided value and a pattern of varying length, with the
// bands the gzip check uses; a HEAD step; the settings an area takes from an
// S4b profile, and the variables an area may not set; the area's files; and
// the /metrics check, which the readiness polls of /health do not upset.
func TestTourAccountOptions(t *testing.T) {
	const (
		userID = "3c8d4e5f-6a7b-4c9d-8e1f-2a3b4c5d6e7f"
		home   = "4d9e5f6a-7b8c-4d0e-9f2a-3b4c5d6e7f8a"
		shared = "5e0f6a7b-8c9d-4e1f-8a3b-4c5d6e7f8a9b"
	)
	session := strings.Repeat("ab", 32)
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			logins++ // a session of its own per sign-in
			http.SetCookie(w, &http.Cookie{Name: "openv_session", Value: strings.Repeat(fmt.Sprintf("%02x", 0xa0+logins), 32), Path: "/"})
			_, _ = io.WriteString(w, `{"id":"`+userID+`"}`+"\n")
		case "/api/v1/auth/me":
			_, _ = io.WriteString(w, `{"id":"`+userID+`","email":"tour-x@example.com","name":"Tour X"}`+"\n")
		case "/api/v1/orgs":
			_, _ = io.WriteString(w, `{"active_org":"`+shared+`","orgs":[{"id":"`+shared+`","type":"company"},`+
				`{"id":"`+home+`","type":"personal"}]}`+"\n")
		case "/api/v1/users":
			// What the server saw: the cookies and the workspace header.
			_, _ = fmt.Fprintf(w, "cookie=%s org=%q\n", r.Header.Get("Cookie"), r.Header.Get("X-Org-ID"))
		case "/api/v1/public/connector/download":
			w.Header().Set("Content-Length", "48")
			w.Header().Set("Content-Type", "application/octet-stream")
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, strings.Repeat("x", 48))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	tr := &tour{t: t, s: &serverProcess{base: srv.URL}, norm: newTourNormaliser(), names: map[string]string{},
		values: map[string]string{}, sent: map[string]int{}, seen: map[string]bool{}, whole: map[string]string{},
		shown: map[string]string{}, clock: &tourClock{start: time.Now()}}
	tr.norm.clock = tr.clock
	tr.loadRoutes()
	tr.anon = &tourActor{name: "anonymous"}
	check := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\n got %q\nwant %q", what, got, want)
		}
	}

	// An account adopted from a recorded sign-in: its id, address and name
	// read back, its personal workspace picked from the list, not the active
	// one; then another session of it.
	login := tr.step("sign in", tr.anon, "POST /api/v1/auth/login", jsonBody(`{}`))
	session = login.sessionToken()
	x := tr.adopt(login, "x", "adopted")
	check("the adopted account", fmt.Sprint(x.userID, " ", x.email, " ", x.display, " ", x.home, " ", x.org, " ", x.session),
		userID+" tour-x@example.com Tour X "+home+" "+home+" "+session)
	check("its names", tr.norm.text(userID+" "+home+" "+session), "<x> <x.workspace> <x.session>")
	second := tr.session(x, tr.step("sign in again", tr.anon, "POST /api/v1/auth/login", jsonBody(`{}`)), "x.second",
		"another session")
	check("another session", fmt.Sprint(second.userID, " ", second.home, " ", second.email, " ", second.session != session),
		userID+" "+home+" tour-x@example.com true")

	// No X-Org-ID, and a flow cookie beside the session: what is sent, and
	// what the step records (the flow cookie, never the session cookie).
	res := tr.step("no header", x, "GET /api/v1/users", noOrgHeader(), withCookie("openv_oauth_state", "{{x.workspace}}"),
		withCookie("second", "2"))
	check("sent with no X-Org-ID", string(res.body), "cookie=openv_session="+session+"; openv_oauth_state="+home+"; second=2 org=\"\"\n")
	rec := tr.renderStep(tr.steps[len(tr.steps)-1])
	check("its acting_in", rec.ActingIn, tourNoOrg)
	check("its request headers", strings.Join(rec.Request.Headers, "|"), "Cookie: openv_oauth_state=<x.workspace>; second=2")
	res = tr.step("the header as usual", x, "GET /api/v1/users")
	check("sent with X-Org-ID", string(res.body), "cookie=openv_session="+session+" org=\""+home+"\"\n")
	res = tr.step("anonymous, with a flow cookie", tr.anon, "GET /api/v1/users", withCookie("openv_oauth_state", "s"))
	check("an anonymous flow cookie", string(res.body), "cookie=openv_oauth_state=s org=\"\"\n")

	// A HEAD answer carries the GET's Content-Length and no body.
	res = tr.step("a HEAD", tr.anon, "HEAD /api/v1/public/connector/download")
	if rec := tr.renderStep(tr.steps[len(tr.steps)-1]); rec.ContentLength != 48 || rec.Kind != "empty" || len(res.body) != 0 {
		t.Errorf("a HEAD step's record: %v %s", rec.ContentLength, rec.Kind)
	}

	// Captures from inside a value.
	cap := &tourResult{tr: tr, header: http.Header{"Location": {"https://idp/authorize?state=" + strings.Repeat("cd", 32) + "&x=1"},
		"Set-Cookie": {"openv_oidc_nonce=" + strings.Repeat("ef", 32) + "; Path=/"}},
		body: []byte(`{"link":"http://localhost:3000/login?invite=` + strings.Repeat("01", 32) + `"}`)}
	check("captureMatch", cap.captureMatch("invite", "/link", `invite=([0-9a-f]{64})`), strings.Repeat("01", 32))
	check("captureHeader", cap.captureHeader("state", "Location", `state=([0-9a-f]{64})`), strings.Repeat("cd", 32))
	check("captureCookie", cap.captureCookie("nonce", "openv_oidc_nonce"), strings.Repeat("ef", 32))
	check("the captures' names", tr.norm.text(string(cap.body)), `{"link":"http://localhost:3000/login?invite=<invite>"}`)

	// The server's address, URL-escaped, and the band it stands for.
	tr.addServerLiterals("/tmp/TourAccounts001", 40123)
	text, lo, hi := tr.norm.normalise("http%3A%2F%2Flocalhost%3A40123%2Fx http://127.0.0.1:40123/y 127.0.0.1%3A40123")
	check("the escaped port", text, "http%3A%2F%2Flocalhost%3A<port>%2Fx http://127.0.0.1:<port>/y 127.0.0.1%3A<port>")
	if hi-lo != 3 {
		t.Errorf("the escaped port's band: %d to %d", lo, hi)
	}

	// An elided value: its token in place, the rest's bytes kept, a band
	// from the value's own; a pattern of varying length likewise.
	st := &tourStep{n: 9, req: &tourReq{route: "GET /api/v1/release", elide: []tourElision{
		{pointer: "/releases", token: "<every release>", minLen: compressFloor, maxLen: tourNoMore},
		{pointer: "/stable", token: "<stable>", minLen: 2, maxLen: 10}}}}
	body := []byte(`{"version":"1.2.3","releases":[{"v":"1"},{"v":"0"}],"stable":""}` + "\n")
	check("elided", string(tr.elideBody(st, body)), `{"version":"1.2.3","releases":"<every release>","stable":"<stable>"}`+"\n")
	if lo, hi := tr.elidedBand(tr.norm, st, body); lo != len(`{"version":"1.2.3","releases":,"stable":}`)+1+compressFloor+2 || hi != tourNoMore {
		t.Errorf("the elided band: %d to %d", lo, hi)
	}
	st.req.elide = st.req.elide[1:]
	if lo, hi := tr.elidedBand(tr.norm, st, body); hi-lo != 8 {
		t.Errorf("a bounded elided band: %d to %d", lo, hi)
	}
	st.req.elide = []tourElision{{pointer: "/missing", token: "<x>", minLen: 1, maxLen: 1}}
	if _, _, err := jsonReplace(body, "/missing", []byte(`"x"`)); err == nil {
		t.Error("jsonReplace names no value, and does not say so")
	}
	tr.patternVarying(`"features":(\{[^}]*\})`, "<features>", 2, 400, "a map that grows")
	if text, lo, hi := tr.norm.normalise(`{"features":{"a":true,"b":true}}`); text != `{"features":<features>}` || hi-lo != 398 {
		t.Errorf("a pattern of varying length: %s %d to %d", text, lo, hi)
	}

	// Settings from S4b's profiles: their variables (which the area may not
	// override), their background lines, and their sign-up.
	env, err := tourServerEnv(tourArea{profiles: []string{"secure_cookies", "billing"}, env: map[string]string{"A": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	check("a profile's variables", fmt.Sprint(env), fmt.Sprint(map[string]string{"A": "1", "OPENV_STRIPE_PRICES": testPriceMap,
		"SECURE_COOKIES": "true", "STRIPE_SECRET_KEY": testStripeKey, "TZ": "UTC"}))
	for _, a := range []tourArea{
		{profiles: []string{"no_such_profile"}},
		{profiles: []string{"secure_cookies"}, env: map[string]string{"SECURE_COOKIES": "false"}},
		{env: map[string]string{"OPENV_SMTP_HOST": "mail.example"}},
		{env: map[string]string{"SSL_CERT_FILE": "/etc/ca.pem"}},
		{env: map[string]string{"CONNECTOR_DIST_DIR": "{{files}}/dist"}},
	} {
		if _, err := tourServerEnv(a); err == nil {
			t.Errorf("an area may boot with %v %v", a.profiles, a.env)
		}
	}
	tr.area = tourArea{profiles: []string{"registration_closed", "billing"}, async: []string{"own"}, signUpWithout: []string{"X"}}
	async, without := tr.settings()
	check("the settings' background lines", strings.Join(async, "|"), strings.Join(append(billingReconcileMessages, "own"), "|"))
	check("the settings' sign-up", strings.Join(without, "|"), "OPENV_REGISTRATION|X")
	check("the golden's profiles", strings.Join(tr.profiles, "|"),
		"registration_closed: self-service registration closed|billing: billing on: a Stripe test key and a one-entry price map, the provider unreachable")

	// The area's files: written with a fixed time, named in the environment
	// and shown as <area files>.
	tr.area = tourArea{files: map[string][]byte{"dist/a.bin": {0, 1, 2}}}
	env = tr.prepareFiles(map[string]string{"CONNECTOR_DIST_DIR": "{{files}}/dist"})
	info, err := os.Stat(filepath.Join(env["CONNECTOR_DIST_DIR"], "a.bin"))
	if err != nil || info.Size() != 3 || !info.ModTime().Equal(tourFilesTime) {
		t.Errorf("the area's file: %v %v", info, err)
	}
	check("the files as the golden shows them", strings.Join(tourEnvLinesShown(env, tr.shown), "|"),
		"(beyond the harness's fixed environment, cmd/server/harness_test.go)|CONNECTOR_DIST_DIR=<area files>/dist")

	// A step's own header pattern, before the area's, and on that step only;
	// its legend line once.
	tr.headerPattern("Retry-After", `^(59|60)$`, "<retry-after 60 s>", "the area's")
	st = &tourStep{n: 10, req: tr.build("GET /api/v1/users", []tourOpt{headerPatternHere("Retry-After", `^(29|30)$`,
		"<retry-after 30 s>", "a countdown"), headerPatternHere("Retry-After", `^(29|30)$`, "<retry-after 30 s>", "a countdown")})}
	check("a step's header pattern", applyHeaderPatterns(st.req.hpat, "Retry-After", "29"), "<retry-after 30 s>")
	check("the area's, on that step", tr.headerValue("Retry-After", applyHeaderPatterns(st.req.hpat, "Retry-After", "60")),
		"<retry-after 60 s>")
	check("another step's fixed value", tr.headerValue("Retry-After", "30"), "30")
	legend := 0
	for _, l := range tr.legend {
		if strings.HasPrefix(l, "<retry-after 30 s>: a countdown") {
			legend++
		}
	}
	check("the step pattern's legend lines", strconv.Itoa(legend), "1")

	// A note added once the answer is read, and a capture that may find
	// nothing.
	res = tr.step("a lease", x, "GET /api/v1/users")
	res.note("computed from the answer")
	check("a note after the answer", strings.Join(tr.renderStep(tr.steps[len(tr.steps)-1]).Notes, "|"),
		"computed from the answer")
	lease := &tourResult{tr: tr, body: []byte(`{"session":{"id":"6f1a2b3c-4d5e-4f60-8a7b-9c0d1e2f3a4b"},"none":null}`)}
	if v, ok := lease.captureIf("lease", "/session/id"); !ok || v != "6f1a2b3c-4d5e-4f60-8a7b-9c0d1e2f3a4b" ||
		tr.norm.text(v) != "<lease>" {
		t.Errorf("captureIf on a lease: %q %v", v, ok)
	}
	for _, pointer := range []string{"/none", "/missing", "/session"} {
		if v, ok := lease.captureIf("nothing", pointer); ok || v != "" {
			t.Errorf("captureIf %s: %q %v", pointer, v, ok)
		}
	}

	// The registrations the tour's own address spends: a body that decodes,
	// from no address of the request's own.
	tr.env = map[string]string{"OPENV_CLIENT_IP_HEADER": "CF-Connecting-IP"}
	for _, c := range []struct {
		opts  []tourOpt
		spent bool
	}{
		{[]tourOpt{jsonBody(`{"email":"a@example.com"}`)}, true},
		{[]tourOpt{jsonBody(`{`)}, false},
		{[]tourOpt{jsonBody(`{"email":1}`)}, false},
		{[]tourOpt{jsonBody(`{}`), withHeader("CF-Connecting-IP", "203.0.113.9")}, false},
		{[]tourOpt{jsonBody(`{}`), withHeader("X-Forwarded-For", "203.0.113.9")}, true},
		{nil, false},
	} {
		r := tr.build("POST /api/v1/auth/register", c.opts)
		if got := tr.spendsRegistration(r); got != c.spent {
			t.Errorf("spendsRegistration(%s %v): %v", r.body, r.header, got)
		}
	}
	tr.env = map[string]string{"OPENV_TRUSTED_PROXY_HOPS": "1"}
	if tr.spendsRegistration(tr.build("POST /api/v1/auth/register", []tourOpt{jsonBody(`{}`),
		withHeader("X-Forwarded-For", "203.0.113.9")})) {
		t.Error("a forwarded registration spends the tour's own address's budget when the area declares proxy hops")
	}
	if tr.spendsRegistration(tr.build("POST /api/v1/auth/login", []tourOpt{jsonBody(`{}`)})) {
		t.Error("a sign-in spends the registration budget")
	}
	tr.env = nil

	// /metrics: the readiness polls of /health count beside the tour's own.
	tr.sent = map[string]int{"GET /health 200": 1, "GET /api/v1/users 200": 2}
	page := []byte(`http_requests_total{method="GET",route="/health",status="200"} 2` + "\n" +
		`http_requests_total{method="GET",route="/api/v1/users",status="200"} 3` + "\n")
	check("the /metrics check", strings.Join(tr.metricsProblems(page), "|"), "  GET /api/v1/users 200: the tour sent 2, the server counted 3")

	// slugPattern's check: a workspace's slug ends in its id's first 8 hex
	// digits; an agent's slug, which the pattern leaves alone, is not checked.
	var answer any
	_ = json.Unmarshal([]byte(`{"id":"`+shared+`","slug":"tour-w-5e0f6a7b","agent":{"id":"`+home+`","slug":"tour-over"},`+
		`"orgs":[{"id":"`+home+`","slug":"tour-home-5e0f6a7b"}]}`), &answer)
	n, bad := slugMismatches(answer)
	check("the slugs checked and those that do not end in their id", fmt.Sprint(n, bad),
		"2 [{"+home+" tour-home-5e0f6a7b -4d9e5f6a}]")
}

// ----------------------------------------------------------------------------
// Values the golden writes as tokens, checked where the area knows them

// slugPattern writes a workspace slug's last 8 hex digits as <id8>, which
// the id's own token does not cover, and checks, once the area has run,
// that every answer object holding an id and a slug the pattern rewrites
// (one ending in 8 hex digits, tourSlugRE) has a slug ending in the first 8
// hex digits of that id (orgs.makeSlug), which the token hides. A slug the
// pattern leaves alone, such as an agent's, is in the golden as it is.
func (tr *tour) slugPattern() {
	tr.pattern(`"slug":"[a-z0-9-]*-([0-9a-f]{8})"`, "<id8>", "a workspace slug ends in the first 8 hex digits of its "+
		"id (orgs.makeSlug), which the id's own token does not cover")
	tr.t.Cleanup(func() {
		if tr.t.Failed() {
			return
		}
		checked, bad := 0, 0 // at most three slugs are reported
		for _, st := range tr.steps {
			if st.plain == nil {
				continue
			}
			var v any
			if json.Unmarshal(st.plain.body, &v) != nil {
				continue
			}
			n, mismatches := slugMismatches(v)
			checked += n
			for _, mm := range mismatches {
				if bad == 3 {
					break
				}
				bad++
				tr.t.Errorf("step %d, %s: the slug %q does not end in %q, the first 8 hex digits of the "+
					"workspace's id %s (orgs.makeSlug), which the golden, writing them as <id8>, does not "+
					"show; if the change is intended, change tour.slugPattern and regenerate with:\n  %s",
					st.n, st.title, mm.slug, mm.want, mm.id, strings.Join(tr.regenerate(), "\n  then "))
			}
		}
		if checked == 0 {
			tr.t.Errorf("tour.slugPattern: no recorded answer holds a workspace's id and slug, so the check checked " +
				"nothing; drop the call")
		}
	})
}

// tourSlugRE is a slug tour.slugPattern's pattern rewrites: one that ends in
// a hyphen and 8 hex digits, as orgs.makeSlug's do.
var tourSlugRE = regexp.MustCompile(`^[a-z0-9-]*-[0-9a-f]{8}$`)

// tourSlugMismatch is an object whose slug does not end in its id's digits.
type tourSlugMismatch struct{ id, slug, want string }

// slugMismatches walks a decoded answer for the objects holding an id and a
// slug that tourSlugRE matches, and returns how many it checked and those
// whose slug does not end in "-" and the first 8 hex digits of their id.
func slugMismatches(v any) (int, []tourSlugMismatch) {
	checked := 0
	var bad []tourSlugMismatch
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			id, _ := x["id"].(string)
			slug, okSlug := x["slug"].(string)
			if hex := strings.ReplaceAll(id, "-", ""); okSlug && tourSlugRE.MatchString(slug) && len(hex) >= 8 {
				checked++
				if want := "-" + hex[:8]; !strings.HasSuffix(slug, want) {
					bad = append(bad, tourSlugMismatch{id: id, slug: slug, want: want})
				}
			}
			for _, k := range sortedKeys(x) {
				walk(x[k])
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
	return checked, bad
}

// noteExpiry notes on a recorded step how many whole minutes after the
// answer's Date the time at pointer is: an expiry (a pairing code's, a reset
// link's), a time to come the golden writes as <time>, whose distance from
// the answer is the point.
func (r *tourResult) noteExpiry(pointer string) {
	r.tr.t.Helper()
	date, err := http.ParseTime(r.header.Get("Date"))
	if err != nil {
		r.tr.t.Fatalf("noteExpiry: %s has no Date header: %v", r.what(), err)
	}
	at, err := time.Parse(time.RFC3339Nano, r.value(pointer))
	if err != nil {
		r.tr.t.Fatalf("noteExpiry: %s of %s is not an RFC 3339 time: %v", pointer, r.what(), err)
	}
	// Date has whole seconds, so round to the nearest minute.
	r.note(fmt.Sprintf("%s is %d minutes after the answer's Date", pointer, int(math.Round(at.Sub(date).Minutes()))))
}
