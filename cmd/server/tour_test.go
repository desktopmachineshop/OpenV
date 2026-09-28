//go:build unix

package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf16"
)

// The API tour (refactor plan §6.4 S5a–S5e; invariants I4, I5, I8, I10
// (JSON types), I16 and quirks Q1, Q8, Q14, Q19). It drives the real server
// binary, built and booted by the S4 harness (harness_test.go), over HTTP
// from outside, and pins what each request answers: the status, whether a
// Content-Type is set (also when the answer is gzipped), the headers, and
// the body as bytes, normalised only where a value changes from run to run
// (tour_normalise_test.go), with a readable parsed view beside it
// (tour_bodies_test.go). After each recorded request it reads the domain
// events the request published (GET /api/v1/events, as the owner) and pins
// their type, actor and payload keys with each value's JSON type and
// normalised value.
//
// AREAS. A slice (s5a, s5b, ...) is split into areas, each in a file of its
// own, cmd/server/tour_<slice>_<key>_test.go, holding one test function
// named TestTour<Slice><Key> (tourTestName: TestTourS5aProjectsTemplates for
// s5a and projects_templates) that calls runTourArea with a tourArea
// literal, plus the area's own helpers, whose names start with the area's
// key in camelCase (projectsTemplatesSeed) so that files added in parallel
// never collide in package main. Nothing registers an area anywhere else:
// runTourArea checks the test's name, and TestTourGoldensAreClaimed reads
// the area files and ties each to its golden. So an area is added without
// editing any shared file.
//
// ONE SERVER PER AREA. Each area boots its own server on a fresh database
// (bootServer), in parallel with the others, so its ids, events, rate-limit
// buckets (5 registrations per server), metrics and uploads are its own.
// The server's environment is the harness's, plus TZ=UTC (server-formatted
// local times match CI and production) and HTTP_PROXY and HTTPS_PROXY at a
// recordingProxy, so no request leaves the machine and the golden ends with
// what the server tried to send (outbound_requests). The database server
// must run in UTC too, and on the test's clock (startTour checks both),
// since some columns are stamped by NOW() and others by Go, lists order them
// together, and the golden places each minted time among the tour's
// exchanges (tourClock in tour_normalise_test.go).
//
// ACTORS. Every area starts with two accounts, registered through the API:
// admin, the first account and so the platform admin (users.go), used only
// for admin setup; and owner, an ordinary account that owns its personal
// workspace, which is on the nightly channel where every feature is on. An
// area may register up to three more (tour.register) and uses tour.anon for
// requests with no session. A signed-in actor's requests carry its session
// cookie and its workspace in X-Org-ID, as the frontend's client does. A
// personal workspace takes no members, so an area that needs a member other
// than the owner creates a shared workspace (tour.sharedWorkspace) and adds
// accounts to it (tour.join); tour.actIn moves an actor between workspaces
// and the actingIn option sends one request in another. The owner's events
// are read in the workspace it acts in, so both keep them with the step that
// published them (an owner's actingIn step reads that workspace's); another
// actor's step in a workspace other than the owner's current one records
// none.
//
// STEPS. tour.step sends one request and records it; tour.setup sends one and
// records nothing, failing on anything but the status it expects (a 2xx by
// default); tour.probe sends one, records nothing and takes any answer. An
// area that stops early (a setup or a capture failed) still reports what its
// steps so far answered differently from the golden, and how to regenerate
// (tour.reportStop). A step names its route exactly as internal/api/testdata/
// routes.txt spells it ("PUT /api/v1/projects/{id}"), with at("id", ...) for
// the path; at the end of the area the server's own /metrics counters must
// show every request under the route the tour declared, which proves the
// template each step names is the one the server matched. Values the tour
// captures (tourResult.capture) are registered under a name, written <name>
// wherever they appear in the golden, and filled into later requests as
// {{name}}. A GET or HEAD is sent twice, without and with Accept-Encoding:
// gzip, and the gzip variant records whether the compressor took it and what
// Content-Type it then carried (Q1); a write is sent once.
//
// GOLDENS. testdata/tour/<slice>/<key>.json, one per area, and
// testdata/tour/<slice>/coverage.txt, the routes the slice's recorded steps
// reached (TestTourCoverage, with no database). Refactor PRs may add goldens
// but never modify one (CONTRIBUTING.md, "Refactor PRs"), so a later slice
// adds its own directory and never edits an earlier slice's files; S5e adds
// the union and the 90% floor. Only UPDATE_GOLDEN=1 regenerates, and every
// failure prints the command. Areas written in parallel each rewrite their
// slice's coverage.txt, so it conflicts when their commits are combined:
// resolve it by regenerating it (tourCoverageRegenerate, no database), never
// by hand.
//
// ADDED FOR S5b, and used by later slices the same way: an event stream
// (text/event-stream, which never ends) is read with the eventStream option,
// frame by frame, and then closed by the tour (tour_stream_test.go); an area
// may set variables of its own in its server's environment (tourArea.env,
// listed in the golden's env); and unordered also sorts the members of an
// object whose keys are random ids. Integrating S5b added two more: a
// pattern scoped to one response header (tour.headerPattern), for a bare
// number such as a Retry-After that a pattern of the whole golden would
// also match in a body or an event payload; and an event stream read with
// eventStream records content_length null when it has none, even when a
// time in its frames makes its length vary. Reviewing S5b added one: a part of
// a multipart body over tourSmallBinary (4 KiB), such as an upload made to
// reach a size limit, is recorded by its size and digest, as a binary body
// over it already was.
//
// ADDED FOR S5c (identity and workspace), each inert for an area that does
// not ask for it, so that no S5a or S5b golden changed; tour_accounts_test.go,
// tour_mail_test.go and tour_standin_test.go hold them:
//   - accounts made by recorded steps: tour.adopt turns a recorded
//     registration's, login's or sign-on's session cookie into an actor, and
//     tour.session a second session of an account; withCookie sends a flow
//     cookie (an OAuth state), and noOrgHeader a signed-in request with no
//     X-Org-ID, which the step records as acting_in "(no X-Org-ID)";
//   - captures from inside a value (tourResult.captureMatch, captureHeader,
//     captureCookie), and waits for what the server does after it answers
//     (tour.await, tour.awaitOutbound, tour.awaitMail);
//   - a value whose length varies by design, kept out of the golden's bytes:
//     elide (a JSON value, such as the release history GET /api/v1/release
//     answers, which every promotion changes) and tour.patternVarying (an
//     area pattern whose token stands for a varying length), both making
//     content_length "<varies>"; the running release's tokens vary in length
//     too, which no earlier golden held in a body;
//   - a HEAD step (its Content-Length is the GET's, with no body), and a
//     GET /health step (the harness's readiness polls count there too);
//   - the server's port written URL-escaped (localhost%3A<port>);
//   - server settings: tourArea.profiles takes an S4b profile's variables,
//     background lines and sign-up (boot_profiles_test.go), tourArea.async
//     awaits an area's own background lines at boot, tourArea.signUpWithout
//     registers admin and owner on a second server without some variables
//     (a closed registration, a mail server that walls unverified
//     accounts), and tourArea.files writes files the environment names as
//     {{files}};
//   - what the server sends out beyond HTTP_PROXY's refusals: a mail catcher
//     (tourArea.mail), which records every mail in outbound_mail, and stand-ins
//     (tourArea.standIns: a payment provider, Google, an identity provider)
//     that the recording proxy answers itself over TLS the server trusts
//     (SSL_CERT_FILE), recording each request under the step it came in
//     (stand_in_requests), with the requests they answer while the server
//     boots awaited before the tour's first (tourArea.bootStandIns).
//
// Integrating S5c added, from what its areas asked for: headerPatternHere, a
// header pattern for the one step given it, so that a fixed value the
// pattern would also match on other steps stays pinned there
// (billing_upstream's Retry-After of 30 beside a refresh bucket's countdown
// from 30); tourResult.note, a note computed from an answer once it is read,
// and tourResult.captureIf, a capture that does not stop the area when the
// answer lacks the value; tour.register counting the area's own recorded and
// probing registrations from the tour's address against the server's limit
// of five (spendsRegistration); the identity provider's failure modes
// (tourIdP.failDiscovery, issueWrong); and the JPEG, GIF and WebP fixtures,
// checked with the others by TestTourFixtures.

// tourArea is one area of a tour slice; see the top of this file.
type tourArea struct {
	slice string // s5a
	key   string // projects_templates: the file, golden and test are named from it
	about string // one line for the golden's header
	run   func(tr *tour)
	// env is what the area sets in its server's environment beyond the
	// harness's and the tour's (a per-request limit made small enough to
	// reach, such as OPENV_MAX_EVIDENCE_MB); the golden lists it. A variable
	// the harness or the tour sets is refused (tourServerEnv).
	env map[string]string

	// The fields below were added for S5c (tour_accounts_test.go,
	// tour_mail_test.go, tour_standin_test.go); an area that leaves them
	// zero boots and renders exactly as before.

	// profiles names S4b profiles (s4bProfiles, boot_profiles_test.go) whose
	// variables the server boots with, whose background lines startTour
	// awaits, and whose sign-up it follows (registration_closed registers
	// admin and owner on a second server); the golden lists them. An area's
	// own env may not set a profile's variable, and the billing profile,
	// which awaits the reconcile's refusals, does not go with a stand-in.
	profiles []string
	// async lists log lines the server writes from goroutines at boot, which
	// startTour awaits before the first request (a profile's own come with
	// it).
	async []string
	// signUpWithout registers admin and owner on a second server booted on
	// the same database without these variables, then stopped: for a server
	// that would refuse them (OPENV_REGISTRATION=closed) or wall them (a mail
	// server makes verification required). The server under test boots first.
	signUpWithout []string
	// accounts are registered after admin and owner, by tour.register (or on
	// the second server of signUpWithout); tour.actor finds them by name.
	accounts []tourAccount
	// files are written, before the server boots, under a directory of the
	// test's own that an env value names as {{files}} (CONNECTOR_DIST_DIR);
	// their bytes live in Go source, as the tour's .gitattributes asks.
	files map[string][]byte
	// mail starts a mail catcher the server sends through (OPENV_SMTP_*),
	// which records every mail in the golden's outbound_mail.
	mail *tourMailSpec
	// standIns are hosts the recording proxy answers itself, over TLS the
	// server trusts, instead of refusing them (tour_standin_test.go).
	standIns []*tourStandIn
	// bootStandIns are the requests the stand-ins answer while the server
	// boots (host -> how many: billing's reconcile at start), which startTour
	// awaits before the tour's first request, so that what they leave (a
	// confirmed price catalogue) is in place, and stamped <time@boot>.
	bootStandIns map[string]int
}

// tourBudget bounds one area: boot, requests and drain, the build excluded.
// The reaper first ticks after 30 s; with no agent runs it does nothing an
// area could see, so an area may run past it, but the budget keeps areas
// small enough to run in parallel within go test's default timeout.
const tourBudget = 60 * time.Second

// tourCoverageRegenerate rewrites every slice's coverage.txt.
const tourCoverageRegenerate = updateGoldenEnv + "=1 go test ./cmd/server -count=1 -run '^TestTourCoverage$'"

// tourMu serialises writing tour goldens and reading them for coverage, so
// areas regenerating in parallel never read a golden half written.
var tourMu sync.Mutex

// runTourArea runs one area: it boots the area's server, runs the area's
// steps, and compares what they answered with the area's golden.
func runTourArea(t *testing.T, a tourArea) {
	t.Helper()
	if want := tourTestName(a.slice, a.key); t.Name() != want {
		t.Fatalf("the tour area %s/%s must run as %s (in cmd/server/tour_%s_%s_test.go), not %s: the golden, the "+
			"regeneration command and CI's check that each golden's test passed all use that name",
			a.slice, a.key, want, a.slice, a.key, t.Name())
	}
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the API tour (it needs a Postgres server)", testDatabaseURLEnv)
	}
	t.Parallel()
	bin := serverBinary(t)
	start := time.Now()
	tr := startTour(t, bin, a)
	ran := false
	defer func() {
		if !ran {
			tr.reportStop()
		}
	}()
	a.run(tr)
	ran = true
	got := tr.finish()
	func() {
		t.Helper()
		tourMu.Lock()
		defer tourMu.Unlock() // also when checkGolden fails the test
		if want, err := os.ReadFile(tourGoldenPath(a.slice, a.key)); err == nil && !updatingGoldens() && !bytes.Equal(want, got) {
			t.Errorf("the tour area %s/%s answered differently from its golden; what changed, step by step "+
				"(the line diff follows):\n%s", a.slice, a.key, tourChanges(want, got, false))
		}
		checkGolden(t, tourGoldenPath(a.slice, a.key), got, strings.Join(tr.regenerate(), "\n  then "))
		if updatingGoldens() {
			writeTourCoverage(t, a.slice)
		}
	}()
	took := time.Since(start)
	if took > tourBudget {
		t.Errorf("the tour area %s/%s took %s to boot, run and drain; the budget is %s: split the area",
			a.slice, a.key, took.Round(time.Millisecond), tourBudget)
	}
	t.Logf("tour area %s/%s: %d recorded steps in %s", a.slice, a.key, len(tr.steps), took.Round(time.Millisecond))
}

// reportStop runs when an area stops before its end: a capture or a setup
// request failed (t.Fatalf), so the golden was never compared. It names what
// the steps recorded so far answered differently from the golden, step by
// step, and how to regenerate, so that this failure reads like any other.
func (tr *tour) reportStop() {
	t := tr.t
	if !t.Failed() {
		return
	}
	summary := "  (no golden to compare with yet)"
	if want, err := os.ReadFile(tourGoldenPath(tr.area.slice, tr.area.key)); err == nil {
		summary = tourChanges(want, tr.encode(tr.render()), true)
		if summary == "" {
			summary = "  (none: every step recorded so far answered as the golden says)"
		}
	}
	t.Errorf("the tour area %s/%s stopped after %d recorded steps, at the failure above, so its golden was not "+
		"compared; what the steps so far answered differently from it:\n%s\nIf the change is intended, change the "+
		"area's code where it relied on the old answer, then regenerate with:\n  %s",
		tr.area.slice, tr.area.key, len(tr.steps), summary, strings.Join(tr.regenerate(), "\n  then "))
}

// tourChanges names what differs between two renderings of a golden:
// each step whose fields differ, with the fields' names, then any other
// top-level field; the line diff checkGolden prints shows the values. For
// an area that stopped early (partial), got holds the steps it recorded:
// those are compared, and the golden's later steps and its other fields
// are not.
func tourChanges(want, got []byte, partial bool) string {
	type golden struct {
		Steps []map[string]json.RawMessage `json:"steps"`
	}
	var a, b golden
	var am, bm map[string]json.RawMessage
	if json.Unmarshal(want, &a) != nil || json.Unmarshal(got, &b) != nil ||
		json.Unmarshal(want, &am) != nil || json.Unmarshal(got, &bm) != nil {
		return "  (the golden is not a tour golden)"
	}
	var out []string
	label := func(st map[string]json.RawMessage) string {
		var n int
		var title, route string
		_ = json.Unmarshal(st["n"], &n)
		_ = json.Unmarshal(st["title"], &title)
		_ = json.Unmarshal(st["route"], &route)
		return fmt.Sprintf("step %d, %s (%s)", n, title, route)
	}
	for i := 0; i < max(len(a.Steps), len(b.Steps)); i++ {
		switch {
		case i >= len(a.Steps):
			out = append(out, "  added: "+label(b.Steps[i]))
		case i >= len(b.Steps) && partial:
			out = append(out, fmt.Sprintf("  (not run: the golden's steps %d to %d)", i+1, len(a.Steps)))
			return strings.Join(out, "\n")
		case i >= len(b.Steps):
			out = append(out, "  gone: "+label(a.Steps[i]))
		default:
			var fields []string
			for _, k := range sortedKeys(mergeKeys(a.Steps[i], b.Steps[i])) {
				if !bytes.Equal(a.Steps[i][k], b.Steps[i][k]) {
					fields = append(fields, k)
				}
			}
			if len(fields) > 0 {
				out = append(out, "  "+label(b.Steps[i])+": "+strings.Join(fields, ", "))
			}
		}
	}
	if partial {
		return strings.Join(out, "\n")
	}
	for _, k := range sortedKeys(mergeKeys(am, bm)) {
		if k != "steps" && !bytes.Equal(am[k], bm[k]) {
			out = append(out, "  "+k)
		}
	}
	return strings.Join(out, "\n")
}

func headerKeys(h http.Header) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k := range h {
		out[k] = nil
	}
	return out
}

func mergeKeys(a, b map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// tourTestName is the test function an area runs as: TestTour, then the
// slice and the key in camel case.
func tourTestName(slice, key string) string {
	var b strings.Builder
	b.WriteString("TestTour")
	for _, part := range strings.Split(slice+"_"+key, "_") {
		if part == "" {
			continue
		}
		r := []rune(part)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

func tourGoldenPath(slice, key string) string {
	return filepath.Join("testdata", "tour", slice, key+".json")
}

func tourAreaRegenerate(slice, key string) string {
	return testDatabaseURLEnv + "=<server URL> " + updateGoldenEnv + "=1 go test ./cmd/server -count=1 -run '^" +
		tourTestName(slice, key) + "$'"
}

// ----------------------------------------------------------------------------
// The tour and its actors

// tour is one area's run against its own server.
type tour struct {
	t     *testing.T
	area  tourArea
	s     *serverProcess
	db    testDatabase
	proxy *recordingProxy

	routes map[string]bool // internal/api/testdata/routes.txt
	norm   *tourNormaliser
	names  map[string]string // registry: name -> value
	values map[string]string // value -> name
	legend []string          // what the area's own patterns and kept literals mean

	admin, owner, anon *tourActor
	actors             []*tourActor
	// ownRegistrations counts the registrations the tour's own address has
	// spent the server's per-address registration limit on (registerIPLimiter):
	// tour.register's and any recorded or probing POST /api/v1/auth/register
	// from it (spendsRegistration, tour_accounts_test.go).
	ownRegistrations int

	steps    []*tourStep
	sent     map[string]int  // "METHOD route status" -> requests sent
	seen     map[string]bool // event ids read
	unread   bool            // a setup request was sent since the events were last read
	security []string        // the standard security headers, from the first answer

	clock *tourClock        // when each exchange's answer was read, for the times the server minted
	phase string            // what the exchanges sent now are ("": the requests before the next step)
	whole map[string]string // wholeSeconds: route -> why its times are whole seconds
	env   map[string]string // what the tour set in the server's environment beyond the harness's
	hpat  []tourHeaderPattern

	// Added for S5c (tour_accounts_test.go, tour_mail_test.go,
	// tour_standin_test.go).
	shown    map[string]string // env variable -> how the golden shows its value (a port, a path)
	profiles []string          // the golden's profiles: "<name>: <about>"
	signedUp string            // how admin and owner were registered, when not on the server under test
	mail     *tourMailCatcher  // tourArea.mail
	files    string            // tourArea.files' directory
}

// tourHeaderPattern is an area's pattern for the values of one response
// header only (tour.headerPattern).
type tourHeaderPattern struct {
	header string // canonical
	re     *regexp.Regexp
	token  string
}

// tourActor is who sends a request. An actor acts in its personal
// workspace (home) until the area moves it to another it belongs to
// (tour.actIn, tour.sharedWorkspace, tour.join) or sends one request in
// another (the actingIn option); a step then records the workspace it acted
// in (acting_in). Areas never set org themselves: the owner's events are
// read from the workspace it acts in, and actIn keeps them straight.
type tourActor struct {
	name    string
	email   string
	display string
	about   string
	session string // the openv_session token; "" for anonymous
	bearer  string // an Authorization: Bearer credential (a worker key, a run token) instead of a session
	userID  string
	home    string // the account's personal workspace
	org     string // the workspace sent as X-Org-ID
}

// tourPassword is every tour account's password.
const tourPassword = "tour password 1"

// tourPhantom is an id no row has, for the not-found steps; it is written
// <phantom> in goldens.
const tourPhantom = "00000000-0000-4000-8000-000000000000"

// tourRegisterBudget is how many accounts one server lets the tour register
// from its own address (registerIPLimiter's burst, 5 per address), counting
// the registrations the area records or probes from it (S5c) as well as
// tour.register's.
const tourRegisterBudget = 5

// securityHeaderNames are the headers SecurityHeadersMiddleware sets on
// every answer, which a step records only when they differ from the first
// answer's (setAttachmentSecurityHeaders overrides the policy per file).
var securityHeaderNames = []string{"Content-Security-Policy", "Referrer-Policy", "Strict-Transport-Security",
	"X-Content-Type-Options", "X-Frame-Options"}

// startTour boots the area's server and registers its first two accounts.
func startTour(t *testing.T, bin string, a tourArea) *tour {
	t.Helper()
	clock := &tourClock{start: time.Now()}
	env, err := tourServerEnv(a)
	if err != nil {
		t.Fatalf("the tour area %s/%s: %v", a.slice, a.key, err)
	}
	tr := &tour{t: t, area: a, norm: newTourNormaliser(), names: map[string]string{}, values: map[string]string{},
		sent: map[string]int{}, seen: map[string]bool{}, clock: clock, whole: map[string]string{},
		shown: map[string]string{}}
	tr.norm.clock = clock
	// What the server sends beyond HTTP (files it reads, mail, stand-ins),
	// set up before it boots (tour_accounts_test.go, tour_mail_test.go,
	// tour_standin_test.go).
	env = tr.prepareFiles(env)
	env = tr.prepareMail(env)
	env, tunnels := tr.prepareStandIns(env)
	proxy := startRecordingProxyWith(t, tunnels)
	env = proxy.env(env)
	s, db := bootServer(t, bin, env)
	async, signUpWithout := tr.settings()
	s.waitForAsyncLines(async)
	tr.s, tr.db, tr.proxy, tr.env = s, db, proxy, env
	tr.awaitBootStandIns()
	tr.checkDatabaseZone()
	tr.loadRoutes()
	tr.addServerLiterals(s.tmp, s.port)
	tr.keep("0001-01-01T00:00:00Z", "the zero time, which links_snapshot entries carry as valid_from")
	tr.remember("phantom", tourPhantom)
	tr.anon = &tourActor{name: "anonymous", about: "no session cookie"}
	tr.readRelease()
	accounts := append([]tourAccount{
		{name: "admin", display: "Tour Admin", about: "the first account registered, so the platform admin; used only for admin setup"},
		{name: "owner", display: "Tour Owner", about: "an ordinary account that owns its personal workspace (nightly channel); reads the events after each step"},
	}, a.accounts...)
	if len(signUpWithout) > 0 {
		tr.signUpElsewhere(bin, signUpWithout, accounts)
	} else {
		for _, acc := range accounts {
			tr.register(acc.name, acc.display, acc.about)
		}
	}
	tr.admin, tr.owner = tr.actors[0], tr.actors[1]
	return tr
}

// addServerLiterals registers what differs from run to run about the
// server itself: its temporary directory, its port after 127.0.0.1: and
// localhost:, and the area's files' directory.
func (tr *tour) addServerLiterals(tmp string, port int) {
	tr.norm.addLiteral(tourLiteral{value: tmp, token: "<tmp>", minLen: 8, maxLen: 160})
	p := strconv.Itoa(port)
	for _, host := range []string{"127.0.0.1:", "localhost:"} {
		tr.norm.addLiteral(tourLiteral{value: host + p, token: host + "<port>", minLen: len(host) + 4, maxLen: len(host) + 5})
		// The same, URL-escaped (S5c): a link's query carrying the server's
		// own address (a redirect_uri, a connector's deep link) writes the
		// colon as %3A.
		esc := url.QueryEscape(host)
		tr.norm.addLiteral(tourLiteral{value: esc + p, token: esc + "<port>", minLen: len(esc) + 4, maxLen: len(esc) + 5})
	}
	if tr.files != "" {
		tr.norm.addLiteral(tourLiteral{value: tr.files, token: "<area files>", minLen: 8, maxLen: 160})
	}
}

// tourEnvName is an environment variable's name as an area may set it.
var tourEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// tourServerEnv is what the tour sets in an area's server environment
// beyond the harness's fixed one (harnessEnv) and the recording proxy's
// HTTP_PROXY and HTTPS_PROXY: TZ=UTC, then the area's own variables. An
// area may not set a variable the harness, the proxy or the tour sets, nor
// one that would let a request past the proxy (NO_PROXY, or the lower-case
// spellings Go also reads), so that every area's server keeps the tour's
// guarantees; the golden's env lists the result (tourEnvLines).
//
// Since S5c an area may also take an S4b profile's variables (profiles),
// which its own env may not override, and a value may name the area's files
// ({{files}}, only with tourArea.files). The variables the framework sets for
// a mail catcher (OPENV_SMTP_*) or stand-ins (SSL_CERT_FILE, SSL_CERT_DIR)
// are refused from env, since a mail server or a certificate authority of
// the area's own would reach past the recording proxy or trust more than the
// tour's stand-ins.
func tourServerEnv(a tourArea) (map[string]string, error) {
	taken := map[string]bool{"TZ": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
		"SSL_CERT_FILE": true, "SSL_CERT_DIR": true}
	for _, kv := range harnessEnv(testDatabase{}, 0, "", nil) {
		k, _, _ := strings.Cut(kv, "=")
		taken[k] = true
	}
	env := map[string]string{"TZ": "UTC"}
	profileOf := map[string]string{}
	for _, name := range a.profiles {
		p, ok := s4bProfile(name)
		if !ok {
			return nil, fmt.Errorf("profiles: %q is not an S4b profile (s4bProfiles, boot_profiles_test.go: %s)", name,
				strings.Join(s4bProfileNames(), ", "))
		}
		for _, k := range sortedKeys(p.env) {
			if prev := profileOf[k]; prev != "" && env[k] != p.env[k] {
				return nil, fmt.Errorf("profiles %s and %s both set %s, to different values", prev, name, k)
			}
			env[k], profileOf[k] = p.env[k], name
		}
	}
	for _, k := range sortedKeys(a.env) {
		switch {
		case !tourEnvName.MatchString(k):
			return nil, fmt.Errorf("env %q: an area sets upper-case variables only (the lower-case proxy variables "+
				"would let a request past the recording proxy)", k)
		case taken[k]:
			return nil, fmt.Errorf("env %s: the harness or the tour sets it, and an area may not change it", k)
		case strings.HasPrefix(k, "OPENV_SMTP_"):
			return nil, fmt.Errorf("env %s: mail goes to the tour's mail catcher (tourArea.mail), which sets the "+
				"OPENV_SMTP_ variables; a mail server of the area's own would be dialled directly, past the proxy", k)
		case profileOf[k] != "":
			return nil, fmt.Errorf("env %s: profile %s sets it; an area may not change a profile's variable", k, profileOf[k])
		case strings.Contains(a.env[k], "{{files}}") && len(a.files) == 0:
			return nil, fmt.Errorf("env %s names {{files}}, but the area has no files", k)
		}
		env[k] = a.env[k]
	}
	return env, nil
}

// tourEnvLines lists a server environment as the golden's env shows it:
// "K=V", sorted, the recording proxy's address written as a placeholder
// (its port differs from run to run).
func tourEnvLines(env map[string]string) []string { return tourEnvLinesShown(env, nil) }

// tourEnvLinesShown is tourEnvLines with the values the tour wrote as
// placeholders (shown: a mail catcher's port, the area's files, the test
// certificate authority's path), since they differ from run to run.
func tourEnvLinesShown(env, shown map[string]string) []string {
	out := []string{"(beyond the harness's fixed environment, cmd/server/harness_test.go)"}
	for _, k := range sortedKeys(env) {
		v := env[k]
		if k == "HTTP_PROXY" || k == "HTTPS_PROXY" {
			v = proxyPlaceholder
		}
		if s, ok := shown[k]; ok {
			v = s
		}
		out = append(out, k+"="+v)
	}
	return out
}

// checkDatabaseZone fails unless the database server runs in UTC and on the
// test's clock.
func (tr *tour) checkDatabaseZone() {
	conn, err := sql.Open("postgres", tr.db.url)
	if err != nil {
		tr.t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var zone string
	if err := conn.QueryRow("SHOW TimeZone").Scan(&zone); err != nil {
		tr.t.Fatalf("read the database server's time zone: %v", err)
	}
	if zone != "UTC" && zone != "Etc/UTC" {
		tr.t.Fatalf("the database server runs in time zone %s; the tour needs UTC (set timezone = 'UTC' in its "+
			"postgresql.conf, as CI's images and Railway's have it), since some columns are stamped by NOW() and "+
			"others by the server's Go clock, and lists order them together", zone)
	}
	// Some times are stamped by the database (NOW()), and the golden places
	// every time among the tour's exchanges by the test's clock (tourClock).
	before := time.Now()
	var now time.Time
	if err := conn.QueryRow("SELECT clock_timestamp()").Scan(&now); err != nil {
		tr.t.Fatalf("read the database server's clock: %v", err)
	}
	if after := time.Now(); now.Before(before) || now.After(after) {
		tr.t.Fatalf("the database server's clock read %s during a query the test sent at %s and saw answered at "+
			"%s; the tour needs a database server on the test's clock (on the same host, as CI's service "+
			"containers and a local cluster are), since the golden places the times it stamps among the tour's "+
			"requests", now.UTC().Format(time.RFC3339Nano), before.UTC().Format(time.RFC3339Nano),
			after.UTC().Format(time.RFC3339Nano))
	}
}

func (tr *tour) loadRoutes() {
	data, err := os.ReadFile(filepath.Join(moduleRoot(tr.t), "internal", "api", "testdata", "routes.txt"))
	if err != nil {
		tr.t.Fatalf("read the route inventory: %v", err)
	}
	tr.routes = map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		tr.routes[l] = true
	}
}

// readRelease registers the running release so that no golden holds it: a
// promotion adds a RELEASE_NOTES.md section, which the server reports.
func (tr *tour) readRelease() {
	r := tr.setup("the running release", tr.anon, "GET /api/v1/public/release")
	for _, h := range securityHeaderNames {
		for _, v := range r.header.Values(h) {
			tr.security = append(tr.security, h+": "+v)
		}
	}
	for _, f := range []struct{ pointer, token string }{{"/version", "<release-version>"}, {"/stable", "<stable-version>"}} {
		v, _ := jsonValue(r.body, f.pointer)
		if s, _ := v.(string); s != "" && tr.values[s] == "" {
			tr.values[s] = f.token
			// A promotion may lengthen the version (0.9.9 to 0.10.0), so an
			// answer that holds it has a length that varies (no S5a or S5b
			// answer holds it).
			tr.norm.addPattern(tourPattern{class: f.token, flat: f.token, minLen: len("0.0.0"), maxLen: len("99.99.99"),
				re: regexp.MustCompile(`(?:^|[^0-9.])(` + regexp.QuoteMeta(s) + `)(?:$|[^0-9.])`)})
		}
	}
}

// register creates an account through the API (setup, not recorded: S5c
// pins registration), with the session its answer sets and the workspace it
// is active in, its personal one. name, name.session and name.workspace
// become registered names; display is the account's name and about says
// what the area uses it for. One server registers at most five accounts,
// admin and owner among them.
func (tr *tour) register(name, display, about string) *tourActor {
	tr.t.Helper()
	if tr.ownRegistrations >= tourRegisterBudget {
		tr.t.Fatalf("a tour area registers at most %d accounts from the tour's own address (the server's registration "+
			"limit per address); admin and owner are two of them, and the area's own POST /api/v1/auth/register "+
			"requests from that address, recorded or probing, count too: %d so far", tourRegisterBudget,
			tr.ownRegistrations)
	}
	a := &tourActor{name: name, email: "tour-" + name + "@example.com", display: display, about: about}
	r := tr.setup("register "+name, tr.anon, "POST /api/v1/auth/register", rawBody("application/json", tourSignUpBody(a)))
	return tr.enrol(a, r)
}

// tourSignUpBody is what tour.register sends for an account.
func tourSignUpBody(a *tourActor) []byte {
	body, _ := json.Marshal(map[string]string{"email": a.email, "password": tourPassword, "name": a.display})
	return body
}

// enrol makes a registered account an actor: its session from the answer's
// Set-Cookie, its id, and the workspace it is active in, its personal one.
// On a server that requires a verified address (a mail catcher, with
// verification on), it first follows the verification link the catcher
// received (tour_mail_test.go), since every route but the auth ones walls an
// unverified account.
func (tr *tour) enrol(a *tourActor, r *tourResult) *tourActor {
	tr.t.Helper()
	for _, c := range readSetCookies(r.header) {
		if c.Name == "openv_session" {
			a.session = c.Value
		}
	}
	if a.session == "" {
		tr.t.Fatalf("registering %s set no session cookie\n%s", a.name, r.body)
	}
	a.userID = r.value("/id")
	tr.remember(a.name, a.userID)
	tr.remember(a.name+".session", a.session)
	if v, _ := jsonValue(r.body, "/email_verified"); v == false {
		tr.verifyByMail(a)
	}
	a.org = tr.setup("the workspaces of "+a.name, a, "GET /api/v1/orgs").value("/active_org")
	a.home = a.org
	tr.remember(a.name+".workspace", a.org)
	tr.actors = append(tr.actors, a)
	return a
}

// bearerActor is an actor that authenticates with Authorization: Bearer
// (a workspace runner key, an agent run's token); the credential is
// registered as name.token.
func (tr *tour) bearerActor(name, credential, about string) *tourActor {
	tr.t.Helper()
	tr.remember(name+".token", credential)
	a := &tourActor{name: name, about: about, bearer: credential}
	tr.actors = append(tr.actors, a)
	return a
}

// actIn moves an actor to another workspace it belongs to, from its next
// request on: the requests carry that workspace (filled) in X-Org-ID, and
// each step records it as acting_in. The owner's events are read from the
// workspace it acts in, so moving the owner first reads what is left unread
// in the workspace it leaves (a setup's events, left out) and then marks as
// read what is already in the one it enters (published while the owner was
// elsewhere, so no step recorded them): however often the owner moves, a
// step's events are those published after the step before it in the
// owner's workspace. For one request in another workspace, use actingIn.
func (tr *tour) actIn(a *tourActor, workspace string) {
	tr.t.Helper()
	org := tr.fill(workspace)
	if a == tr.owner && a.org != org {
		tr.readEvents()
		a.org = org
		tr.readEvents()
		return
	}
	a.org = org
}

// sharedWorkspace creates a workspace the owner shares (setup: POST
// /api/v1/orgs, named display), registers its id under name and moves the
// owner there (actIn). The owner's personal workspace takes no members (POST
// /api/v1/orgs/{id}/members answers 400), so any area that needs a member
// who is not the owner, such as a plain member or an admin that is not the
// owner, works in one; join adds the members.
func (tr *tour) sharedWorkspace(name, display string) string {
	tr.t.Helper()
	body, _ := json.Marshal(map[string]string{"name": display})
	id := tr.setup("the owner's shared workspace "+name, tr.owner, "POST /api/v1/orgs",
		rawBody("application/json", body), expect(http.StatusCreated)).capture(name, "/id")
	tr.actIn(tr.owner, id)
	return id
}

// join adds an account to a workspace the owner administers, with a
// workspace role (setup, as the owner: POST /api/v1/orgs/{id}/members), and
// moves the account there (actIn).
func (tr *tour) join(a *tourActor, workspace, role string) {
	tr.t.Helper()
	org := tr.fill(workspace)
	body, _ := json.Marshal(map[string]string{"email": a.email, "role": role})
	tr.setup("add "+a.name+" to a shared workspace as "+role, tr.owner, "POST /api/v1/orgs/{id}/members",
		at("id", org), rawBody("application/json", body), actingIn(org), expect(http.StatusCreated))
	tr.actIn(a, org)
}

func readSetCookies(h http.Header) []*http.Cookie {
	return (&http.Response{Header: h}).Cookies()
}

// remember registers a value under a name: it is written <name> in the
// golden and filled in for {{name}}. A name and a value are each registered
// once per area.
func (tr *tour) remember(name, value string) {
	tr.t.Helper()
	if !tourNameRE.MatchString(name) {
		tr.t.Fatalf("a registered name is lower case letters, digits and _ . : - (%q)", name)
	}
	if value == "" {
		tr.t.Fatalf("registering %s: the value is empty", name)
	}
	if prev, ok := tr.names[name]; ok && prev != value {
		tr.t.Fatalf("the name %s is already registered, for another value", name)
	}
	if prev, ok := tr.values[value]; ok && prev != "<"+name+">" {
		tr.t.Fatalf("the value of %s is already registered as %s", name, prev)
	}
	tr.names[name] = value
	tr.values[value] = "<" + name + ">"
	tr.norm.addLiteral(tourLiteral{value: value, token: "<" + name + ">"})
}

var tourNameRE = regexp.MustCompile(`^[a-z][a-z0-9_.:-]*$`)

// keep leaves a literal as it is wherever it appears, before any pattern
// runs: a timestamp the tour itself sends, whose round trip is the point.
// The golden is normalised once the area has run, so a kept literal applies
// to every step of it, those before the call too, and adds a line to the
// golden's legend (normalised); call it before the area ends.
func (tr *tour) keep(value, why string) {
	tr.norm.addLiteral(tourLiteral{value: value, token: value})
	tr.legend = append(tr.legend, fmt.Sprintf("%s kept as it is: %s", value, why))
}

// pattern adds an area's own token after the generic ones: every match of
// re (its first group, when it has one) becomes token.
func (tr *tour) pattern(re, token, why string) {
	tr.norm.addPattern(tourPattern{class: token, re: regexp.MustCompile(re), flat: token})
	tr.legend = append(tr.legend, fmt.Sprintf("%s: %s (the area's own pattern %s)", token, why, re))
}

// headerPattern is pattern for the values of one response header only: in
// that header, every match of re (its first group, when it has one) in the
// normalised value becomes token, and nothing else in the golden is
// touched. It is for a value a pattern of the whole golden would also match
// elsewhere, such as a Retry-After that counts down, a bare number that a
// body or an event payload may hold too. It adds a line to the golden's
// legend.
func (tr *tour) headerPattern(header, re, token, why string) {
	tr.hpat = append(tr.hpat, tourHeaderPattern{header: http.CanonicalHeaderKey(header), re: regexp.MustCompile(re),
		token: token})
	tr.legend = append(tr.legend, fmt.Sprintf("%s: %s (the area's own pattern %s, in the %s header only)", token, why,
		re, http.CanonicalHeaderKey(header)))
}

// headerPatternHere is tour.headerPattern for the step it is given to only:
// for a header whose value a pattern would also match, on other steps, where
// the value is fixed and should stay pinned (billing_upstream's fixed
// Retry-After of 30 beside a refresh bucket's countdown from 30). The step's
// patterns apply before the area's. It adds a line to the golden's legend
// once, however many steps name it.
func headerPatternHere(header, re, token, why string) tourOpt {
	return func(tr *tour, r *tourReq) {
		name := http.CanonicalHeaderKey(header)
		r.hpat = append(r.hpat, tourHeaderPattern{header: name, re: regexp.MustCompile(re), token: token})
		line := fmt.Sprintf("%s: %s (the area's own pattern %s, in the %s header of the steps that name it only)",
			token, why, re, name)
		if !contains(tr.legend, line) {
			tr.legend = append(tr.legend, line)
		}
	}
}

// headerValue applies the area's header patterns to a normalised value of
// the response header name.
func (tr *tour) headerValue(name, v string) string { return applyHeaderPatterns(tr.hpat, name, v) }

// applyHeaderPatterns applies header patterns to a normalised value of the
// response header name.
func applyHeaderPatterns(patterns []tourHeaderPattern, name, v string) string {
	for _, p := range patterns {
		if p.header != name {
			continue
		}
		var b strings.Builder
		last := 0
		for _, m := range p.re.FindAllStringSubmatchIndex(v, -1) {
			start, end := m[0], m[1]
			if len(m) >= 4 && m[2] >= 0 {
				start, end = m[2], m[3]
			}
			b.WriteString(v[last:start])
			b.WriteString(p.token)
			last = end
		}
		b.WriteString(v[last:])
		v = b.String()
	}
	return v
}

// wholeSeconds declares that a route writes its times to the whole second
// (an export's RFC 3339 without a fraction): on its steps, a time with no
// fraction stays <time>, since a time cut to the second falls in an earlier
// exchange by chance (tourClock). A time with a fraction is still placed,
// and so is one in the events. It adds a line to the golden's legend.
func (tr *tour) wholeSeconds(route, why string) {
	tr.t.Helper()
	if !tr.routes[route] {
		tr.t.Fatalf("wholeSeconds: %q is not a route of internal/api/testdata/routes.txt", route)
	}
	if tr.whole[route] == "" {
		tr.legend = append(tr.legend, fmt.Sprintf("%s writes whole seconds (%s): its times without a fraction are <time>", route, why))
	}
	tr.whole[route] = why
}

// id is a registered value.
func (tr *tour) id(name string) string {
	tr.t.Helper()
	v, ok := tr.names[name]
	if !ok {
		tr.t.Fatalf("%s is not a registered name", name)
	}
	return v
}

var fillRE = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

// fill replaces each {{name}} by the value registered under name.
func (tr *tour) fill(s string) string {
	tr.t.Helper()
	return fillRE.ReplaceAllStringFunc(s, func(m string) string { return tr.id(m[2 : len(m)-2]) })
}

// ----------------------------------------------------------------------------
// Requests

// tourReq is one request as built from a route and its options.
type tourReq struct {
	method, route, path string
	query               string
	header              http.Header
	body                []byte
	sendBody            bool
	once                string // why a GET is sent only once ("": sent twice)
	expect              int    // setup: the status expected (0: any 2xx)
	unordered           [][2]string
	notes               []string
	rawPath             string
	org                 *string     // actingIn: the workspace sent as X-Org-ID instead of the actor's
	bodyFrom            int         // answerOf: the step whose answer the body is
	stream              *tourStream // eventStream: an event stream, read frame by frame (tour_stream_test.go)
	noOrg               bool        // noOrgHeader: a signed-in request sent with no X-Org-ID (tour_accounts_test.go)
	elide               []tourElision
	hpat                []tourHeaderPattern // headerPatternHere: header patterns of this step only
}

// tourOpt shapes a request.
type tourOpt func(tr *tour, r *tourReq)

// at fills the route's path variables: pairs of name and value, the value
// filled ({{name}}).
func at(pairs ...string) tourOpt {
	return func(tr *tour, r *tourReq) {
		if len(pairs)%2 != 0 {
			tr.t.Fatal("at takes pairs of variable and value")
		}
		for i := 0; i < len(pairs); i += 2 {
			v := tr.fill(pairs[i+1])
			pattern := "{" + pairs[i] + "}"
			if !strings.Contains(r.path, pattern) {
				tr.t.Fatalf("the route %s has no variable %s", r.route, pattern)
			}
			r.path = strings.ReplaceAll(r.path, pattern, url.PathEscape(v))
		}
	}
}

// query sets the query string, filled.
func query(q string) tourOpt {
	return func(tr *tour, r *tourReq) { r.query = tr.fill(q) }
}

// jsonBody sends a JSON body (Content-Type: application/json), filled.
func jsonBody(s string) tourOpt {
	return func(tr *tour, r *tourReq) {
		r.body, r.sendBody = []byte(tr.fill(s)), true
		r.header.Set("Content-Type", "application/json")
	}
}

// rawBody sends bytes as they are, with a Content-Type unless it is "".
func rawBody(contentType string, b []byte) tourOpt {
	return func(tr *tour, r *tourReq) {
		r.body, r.sendBody = b, true
		if contentType != "" {
			r.header.Set("Content-Type", contentType)
		}
	}
}

// answerOf sends, as the body, what an earlier recorded step answered, byte
// for byte, with contentType (none when ""): a round trip of an export into
// an import. The golden names the step instead of repeating the bytes, which
// that step shows.
func answerOf(res *tourResult, contentType string) tourOpt {
	return func(tr *tour, r *tourReq) {
		if res.step == nil {
			tr.t.Fatalf("answerOf takes a recorded step's answer; a setup request's answer is in no golden")
		}
		r.body, r.sendBody, r.bodyFrom = res.body, true, res.step.n
		if contentType != "" {
			r.header.Set("Content-Type", contentType)
		}
	}
}

// actingIn sends this one request with another workspace (filled) in
// X-Org-ID, which the step records as acting_in; the actor stays where it
// is. For the owner, the step's events are read in that workspace (step
// moves the owner there for the one request). To move an actor for the
// requests that follow, use tour.actIn.
func actingIn(workspace string) tourOpt {
	return func(tr *tour, r *tourReq) {
		org := tr.fill(workspace)
		r.org = &org
	}
}

// withHeader sets a request header, filled.
func withHeader(name, value string) tourOpt {
	return func(tr *tour, r *tourReq) { r.header.Set(name, tr.fill(value)) }
}

// once sends a GET without its gzip variant, for a reason the golden gives
// (a GET with a side effect, or a rate-limited route).
func once(why string) tourOpt {
	return func(tr *tour, r *tourReq) { r.once = why }
}

// unordered sorts the elements of the array a JSON pointer names ("*"
// stands for every member or element) before normalising, keeping their
// bytes, by their text normalised with the step's sort key (tour.sortKey: an
// id the golden or the rest of the answer numbered keeps its number, one
// first seen in the array has none): only for an order the server leaves to
// chance (a random id or a timestamp tie), and the golden says why. Never
// use it to hide an order the server fixes. A pointer that names an object
// sorts its members the same way, each by its "name":value text: for a Go
// map keyed by random ids, which encoding/json writes in the order of its
// sorted keys, so in an order that changes from run to run.
func unordered(pointer, why string) tourOpt {
	return func(tr *tour, r *tourReq) { r.unordered = append(r.unordered, [2]string{pointer, why}) }
}

// note adds a line of explanation to the step.
func note(text string) tourOpt {
	return func(tr *tour, r *tourReq) { r.notes = append(r.notes, text) }
}

// expect is the status a setup request must answer (default: any 2xx).
func expect(status int) tourOpt {
	return func(tr *tour, r *tourReq) { r.expect = status }
}

// rawPath sends a path that matches no route, for a step declared
// "METHOD unmatched".
func rawPath(p string) tourOpt {
	return func(tr *tour, r *tourReq) { r.rawPath = tr.fill(p) }
}

var routeVarRE = regexp.MustCompile(`\{[^{}]+\}`)

// build makes a request from a route of routes.txt and its options.
func (tr *tour) build(route string, opts []tourOpt) *tourReq {
	tr.t.Helper()
	method, tmpl, ok := strings.Cut(route, " ")
	if !ok || (!tr.routes[route] && tmpl != "unmatched") {
		tr.t.Fatalf("%q is not a route of internal/api/testdata/routes.txt (spell it as that file does, "+
			"\"METHOD /api/v1/...{id}\", or \"METHOD unmatched\" with rawPath)", route)
	}
	r := &tourReq{method: method, route: route, path: tmpl, header: http.Header{}}
	for _, o := range opts {
		o(tr, r)
	}
	if tmpl == "unmatched" {
		if r.rawPath == "" {
			tr.t.Fatalf("a step declared %q needs rawPath", route)
		}
		r.path = r.rawPath
	} else if r.rawPath != "" {
		tr.t.Fatalf("rawPath is only for a step declared \"METHOD unmatched\" (%s)", route)
	}
	if left := routeVarRE.FindString(r.path); left != "" {
		tr.t.Fatalf("%s: the path variable %s is not filled (at(%q, ...))", route, left, left[1:len(left)-1])
	}
	return r
}

// tourExchange is one request sent and its answer, the body decoded from
// gzip when it came gzipped.
type tourExchange struct {
	status  int
	header  http.Header
	body    []byte
	gzipped bool
}

// exchange sends a request as an actor, with extra headers, and notes it
// under its route for the /metrics check.
func (tr *tour) exchange(a *tourActor, r *tourReq, extra http.Header) *tourExchange {
	tr.t.Helper()
	target := tr.s.base + r.path
	if r.query != "" {
		target += "?" + r.query
	}
	var body io.Reader = http.NoBody
	if r.sendBody {
		body = bytes.NewReader(r.body)
	}
	ctx, cancel := probeContext()
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.method, target, body)
	if err != nil {
		tr.t.Fatal(err)
	}
	for k, vs := range r.header {
		req.Header[k] = vs
	}
	for k, vs := range extra {
		req.Header[k] = vs
	}
	if r.noOrg && a.session == "" {
		tr.t.Fatalf("%s %s: noOrgHeader is for an actor with a session; %s sends no X-Org-ID anyway", r.method, r.path, a.name)
	}
	if a.session != "" {
		// A flow cookie the step sends (withCookie) goes after the session's.
		cookie := "openv_session=" + a.session
		if extra := r.header.Get("Cookie"); extra != "" {
			cookie += "; " + extra
		}
		req.Header.Set("Cookie", cookie)
		if org := r.workspace(a); org != "" {
			req.Header.Set("X-Org-ID", org)
		}
	}
	if a.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+a.bearer)
	}
	resp, err := tr.s.client().Do(req)
	if err != nil {
		tr.t.Fatalf("%s %s: %v\n%s", r.method, r.path, err, tr.s.output())
	}
	defer func() { _ = resp.Body.Close() }()
	if tr.spendsRegistration(r) {
		tr.ownRegistrations++
	}
	var data []byte
	if r.stream != nil && isEventStream(resp.Header) {
		// An event stream never ends: read the frames the area expects, then
		// close the connection (tour_stream_test.go).
		data = tr.readStreamAnswer(r, resp.Body, cancel)
	} else if data, err = io.ReadAll(resp.Body); err != nil {
		tr.t.Fatalf("read the answer to %s %s: %v\n%s", r.method, r.path, err, tr.s.output())
	}
	tr.mark()
	route := r.route
	if _, tmpl, _ := strings.Cut(route, " "); tmpl == "unmatched" {
		route = r.method + " unmatched"
	}
	tr.sent[route+" "+strconv.Itoa(resp.StatusCode)]++
	ex := &tourExchange{status: resp.StatusCode, header: resp.Header, body: data}
	if resp.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			tr.t.Fatalf("%s %s: a gzip answer that does not decode: %v", r.method, r.path, err)
		}
		if ex.body, err = io.ReadAll(zr); err != nil {
			tr.t.Fatalf("%s %s: a gzip answer that does not decode: %v", r.method, r.path, err)
		}
		ex.gzipped = true
	}
	return ex
}

// mark notes that an exchange's answer has been read: a time the server
// minted since the mark before was minted during this exchange (tourClock).
// The exchanges between two steps (setups, probes, the owner's event reads)
// share one label, "before step N": how many there are is the area's
// business, not the server's.
func (tr *tour) mark() {
	if tr.clock == nil {
		return
	}
	label := tr.phase
	if label == "" {
		label = fmt.Sprintf("before step %d", len(tr.steps)+1)
	}
	tr.clock.marks = append(tr.clock.marks, tourMark{end: time.Now(), label: label})
}

// workspace is the workspace a request is sent in: actingIn's, or the
// actor's.
func (r *tourReq) workspace(a *tourActor) string {
	if r.org != nil {
		return *r.org
	}
	return a.org
}

// tourStep is a recorded request.
type tourStep struct {
	n      int
	title  string
	actor  *tourActor
	org    string // the X-Org-ID it sent
	req    *tourReq
	plain  *tourExchange
	gzip   *tourExchange // nil: not sent
	events []json.RawMessage
}

// tourResult is what an area reads of an answer.
type tourResult struct {
	tr     *tour
	step   *tourStep // nil for a request the golden does not record
	status int
	header http.Header
	body   []byte
}

// step sends one request and records it: a GET or HEAD twice (without and
// with gzip) unless once, then the events it published.
func (tr *tour) step(title string, a *tourActor, route string, opts ...tourOpt) *tourResult {
	tr.t.Helper()
	r := tr.build(route, opts)
	if r.expect != 0 {
		tr.t.Fatalf("step %q: expect is for setup requests; a step records whatever the server answers", title)
	}
	st := &tourStep{n: len(tr.steps) + 1, title: title, actor: a, org: r.workspace(a), req: r}
	// The owner's events are read where it acts; for a request it sends in
	// another workspace (actingIn), it acts there for this one request, so
	// what the step publishes there is read with it.
	back := ""
	if a == tr.owner && st.org != a.org {
		back = a.org
		tr.actIn(a, st.org)
	}
	if tr.unread {
		tr.readEvents()
	}
	tr.phase = fmt.Sprintf("step %d", st.n)
	st.plain = tr.exchange(a, r, nil)
	if isRead(r.method) && r.once == "" && r.header.Get("Accept-Encoding") == "" {
		st.gzip = tr.exchange(a, r, http.Header{"Accept-Encoding": {"gzip"}})
	}
	tr.phase = fmt.Sprintf("events after step %d", st.n)
	st.events = tr.readEvents()
	tr.phase = ""
	tr.steps = append(tr.steps, st)
	if back != "" {
		tr.actIn(a, back)
	}
	return &tourResult{tr: tr, step: st, status: st.plain.status, header: st.plain.header, body: st.plain.body}
}

// isRead is whether a method is sent twice, plain and with gzip.
func isRead(method string) bool { return method == http.MethodGet || method == http.MethodHead }

// setup sends one request and records nothing; its events are read before
// the next step and left out. Anything but the expected status (any 2xx by
// default) fails the area.
func (tr *tour) setup(title string, a *tourActor, route string, opts ...tourOpt) *tourResult {
	tr.t.Helper()
	r := tr.build(route, opts)
	ex := tr.exchange(a, r, nil)
	if (r.expect != 0 && ex.status != r.expect) || (r.expect == 0 && (ex.status < 200 || ex.status > 299)) {
		tr.t.Fatalf("setup %q (%s as %s) answered %d: %s\n%s", title, route, a.name, ex.status, ex.body, tr.s.output())
	}
	tr.unread = true
	return &tourResult{tr: tr, status: ex.status, header: ex.header, body: ex.body}
}

// probe sends one request and records nothing, like setup, but takes
// whatever the server answers: for requests an area repeats until an answer
// changes (draining a rate-limit bucket). It is declared under its route for
// the /metrics check like any other request.
func (tr *tour) probe(a *tourActor, route string, opts ...tourOpt) *tourResult {
	tr.t.Helper()
	r := tr.build(route, opts)
	if r.expect != 0 {
		tr.t.Fatalf("probe %s: expect is for setup requests; a probe takes whatever the server answers", route)
	}
	ex := tr.exchange(a, r, nil)
	tr.unread = true
	return &tourResult{tr: tr, status: ex.status, header: ex.header, body: ex.body}
}

// what names the answer in a failure: the step it came from, if any.
func (r *tourResult) what() string {
	if r.step == nil {
		return "the answer"
	}
	return fmt.Sprintf("step %d's answer (%q, %s: %d)", r.step.n, r.step.title, r.step.req.route, r.status)
}

// value is the string at a JSON pointer of the answer.
func (r *tourResult) value(pointer string) string {
	r.tr.t.Helper()
	v, err := jsonValue(r.body, pointer)
	if err != nil {
		r.tr.t.Fatalf("read %s of %s: %v\n%s", pointer, r.what(), err, r.body)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		r.tr.t.Fatalf("%s of %s is not a non-empty string: %v\n%s", pointer, r.what(), v, r.body)
	}
	return s
}

// capture registers the string at a JSON pointer of the answer under name.
func (r *tourResult) capture(name, pointer string) string {
	r.tr.t.Helper()
	v := r.value(pointer)
	r.tr.remember(name, v)
	return v
}

// captureWhere registers, under name, field want of the element of the
// array at list whose field has the value equals.
func (r *tourResult) captureWhere(name, list, field, equals, want string) string {
	r.tr.t.Helper()
	spans, err := jsonFind(r.body, list+"/*")
	if err != nil {
		r.tr.t.Fatalf("read %s of %s: %v", list, r.what(), err)
	}
	for _, sp := range spans {
		el := r.body[sp.start:sp.end]
		if v, err := jsonValue(el, "/"+field); err == nil && v == equals {
			sub := &tourResult{tr: r.tr, step: r.step, status: r.status, body: el}
			return sub.capture(name, "/"+want)
		}
	}
	r.tr.t.Fatalf("no element of %s of %s has %s %q\n%s", list, r.what(), field, equals, r.body)
	return ""
}

// readEvents reads the owner's workspace events not read before, oldest
// first. The server stores an event before it answers the request that
// published it (events.DefaultBus.Publish), so they are all there.
func (tr *tour) readEvents() []json.RawMessage {
	tr.t.Helper()
	tr.unread = false
	if tr.owner == nil {
		return nil
	}
	const page = 500
	var fresh []json.RawMessage // newest first
	before := ""
	for {
		q := "limit=" + strconv.Itoa(page)
		if before != "" {
			q += "&before=" + before
		}
		ex := tr.exchange(tr.owner, &tourReq{method: http.MethodGet, route: "GET /api/v1/events", path: "/api/v1/events", query: q}, nil)
		if ex.status != http.StatusOK {
			tr.t.Fatalf("read the events: %d %s", ex.status, ex.body)
		}
		var list []json.RawMessage
		if err := json.Unmarshal(ex.body, &list); err != nil {
			tr.t.Fatalf("read the events: %v\n%s", err, ex.body)
		}
		stop := len(list) < page
		for _, e := range list {
			var head struct{ ID string }
			_ = json.Unmarshal(e, &head)
			if tr.seen[head.ID] {
				stop = true
				break
			}
			fresh = append(fresh, e)
			before = head.ID
		}
		if stop {
			break
		}
	}
	out := make([]json.RawMessage, 0, len(fresh))
	for i := len(fresh) - 1; i >= 0; i-- {
		var head struct{ ID string }
		_ = json.Unmarshal(fresh[i], &head)
		tr.seen[head.ID] = true
		out = append(out, fresh[i])
	}
	return out
}

// ----------------------------------------------------------------------------
// The end of an area

// finish checks the server's counters, stops it and renders the golden.
func (tr *tour) finish() []byte {
	tr.t.Helper()
	tr.phase = "the end of the area"
	if tr.unread {
		tr.readEvents()
	}
	tr.checkMetrics()
	uploads := tr.listUploads()
	mailed := tr.mailSettled()
	code, _, err := tr.s.terminate(30 * time.Second)
	if err != nil {
		tr.t.Fatalf("%v\n%s", err, tr.s.output())
	}
	if code != 0 {
		tr.t.Errorf("the server exited with status %d\n%s", code, tr.s.output())
	}
	tr.checkNoLateMail(mailed)
	if testing.Verbose() {
		logCoverage(tr.t, tr.s)
	}
	g := tr.render()
	g.Uploads = tr.renderUploads(uploads)
	g.Outbound = tr.proxy.summary()
	g.StandIns = tr.renderStandInsOutsideSteps()
	g.Mail = tr.renderMail()
	return tr.encode(g)
}

// encode writes a golden as indented JSON, HTML left unescaped, with every
// invisible character (a byte-order mark, a zero-width space, a direction
// mark, a non-breaking space) written as its JSON escape, which reads back
// as the same string: an editor or a diff would otherwise show nothing where
// it is, or drop it.
func (tr *tour) encode(g *tourGolden) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g); err != nil {
		tr.t.Fatalf("encode the golden: %v", err)
	}
	return escapeInvisible(buf.Bytes())
}

// escapeInvisible writes each invisible rune of an encoded JSON text as a
// \uXXXX escape (two, a surrogate pair, above U+FFFF). encoding/json emits
// such runes only inside strings, where the escape means the same rune.
func escapeInvisible(data []byte) []byte {
	if !bytes.ContainsFunc(data, isInvisible) {
		return data
	}
	var b bytes.Buffer
	for _, r := range string(data) {
		if !isInvisible(r) {
			b.WriteRune(r)
			continue
		}
		for _, u := range utf16.Encode([]rune{r}) {
			fmt.Fprintf(&b, `\u%04x`, u)
		}
	}
	return b.Bytes()
}

// isInvisible is a rune that prints as nothing or as a plain space: a
// format character (Cf: U+FEFF, U+200B, U+200E, U+00AD), a private-use
// one, a line or paragraph separator, or a space other than U+0020.
func isInvisible(r rune) bool {
	return unicode.In(r, unicode.Cf, unicode.Co, unicode.Zl, unicode.Zp) || (unicode.Is(unicode.Zs, r) && r != ' ')
}

// renderUploads normalises the list of stored files. The walk lists them
// in lexical order, and a stored name starts with a random UUID, so they
// are sorted by their text normalised without numbering, and numbered in
// that order, then sorted by the result, which is then the same on every
// run.
func (tr *tour) renderUploads(uploads []string) []string {
	flat := tr.norm.clone(true)
	key := func(s string) string { return flat.text(s) }
	sort.SliceStable(uploads, func(i, j int) bool { return key(uploads[i]) < key(uploads[j]) })
	out := make([]string, 0, len(uploads))
	for _, u := range uploads {
		out = append(out, tr.norm.text(u))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ki, kj := key(out[i]), key(out[j]); ki != kj {
			return ki < kj
		}
		return out[i] < out[j]
	})
	return out
}

var metricsCountRE = regexp.MustCompile(`^http_requests_total\{(.*)\} (\S+)$`)

// checkMetrics compares the server's http_requests_total counters with the
// requests the tour sent: every (method, route, status) the tour declared
// must be counted exactly as often, and nothing else but the harness's
// readiness polls of /health. A step whose declared route is not the
// template the server matched fails here.
func (tr *tour) checkMetrics() {
	tr.t.Helper()
	// A handler's request is counted once it returns, which a client that
	// has read the whole answer may beat by a moment; so read the counters
	// until they settle.
	var problems []string
	deadline := time.Now().Add(5 * time.Second)
	for {
		ex := tr.exchange(tr.anon, &tourReq{method: http.MethodGet, route: "GET /metrics", path: "/metrics"}, nil)
		delete(tr.sent, "GET /metrics 200") // the scrapes themselves are not the tour's requests
		if ex.status != http.StatusOK {
			tr.t.Fatalf("GET /metrics answered %d", ex.status)
		}
		problems = tr.metricsProblems(ex.body)
		if len(problems) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(problems) > 0 {
		tr.t.Errorf("the server's route counters (/metrics http_requests_total) do not match the routes the tour's "+
			"requests declared, so a step names a route template other than the one the server matched:\n%s",
			strings.Join(problems, "\n"))
	}
}

// metricsProblems compares one /metrics page with the requests sent.
func (tr *tour) metricsProblems(page []byte) []string {
	counted := map[string]int{}
	for _, line := range strings.Split(string(page), "\n") {
		m := metricsCountRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		labels := map[string]string{}
		for _, kv := range labelRE.FindAllStringSubmatch(m[1], -1) {
			labels[kv[1]] = kv[2]
		}
		n, _ := strconv.ParseFloat(m[2], 64)
		counted[labels["method"]+" "+labels["route"]+" "+labels["status"]] = int(n)
	}
	var problems []string
	for k, n := range tr.sent {
		// The harness's readiness polls of /health are counted too (S5c
		// records GET /health as a step).
		if counted[k] != n && !(strings.HasPrefix(k, "GET /health ") && counted[k] > n) {
			problems = append(problems, fmt.Sprintf("  %s: the tour sent %d, the server counted %d", k, n, counted[k]))
		}
	}
	for k, n := range counted {
		_, ok := tr.sent[k]
		if !ok && !strings.HasPrefix(k, "GET /health ") && !strings.HasPrefix(k, "GET /metrics ") {
			problems = append(problems, fmt.Sprintf("  %s: the server counted %d the tour did not declare", k, n))
		}
	}
	sort.Strings(problems)
	return problems
}

// listUploads lists what the server wrote under UPLOADS_DIR (I16's upload
// file naming): each file's path, normalised, and size.
func (tr *tour) listUploads() []string {
	root := filepath.Join(tr.s.tmp, "uploads")
	out := []string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, fmt.Sprintf("%s (%d bytes)", filepath.ToSlash(rel), info.Size()))
		return nil
	})
	return out
}

func (tr *tour) regenerate() []string {
	return []string{tourAreaRegenerate(tr.area.slice, tr.area.key) + " (which also rewrites the slice's coverage.txt)",
		tourCoverageRegenerate + " (coverage.txt alone, no database)"}
}

// ----------------------------------------------------------------------------
// The golden

type tourGolden struct {
	Golden          string            `json:"golden"`
	Test            string            `json:"test"`
	About           string            `json:"about"`
	Regenerate      []string          `json:"regenerate"`
	Normalised      []string          `json:"normalised"`
	Env             []string          `json:"env"`
	Profiles        []string          `json:"profiles,omitempty"`
	SignUp          string            `json:"sign_up,omitempty"`
	Actors          []tourActorRecord `json:"actors"`
	SecurityHeaders []string          `json:"standard_security_headers"`
	Steps           []tourStepRecord  `json:"steps"`
	Uploads         []string          `json:"uploads"`
	Outbound        []string          `json:"outbound_requests"`
	// Added for S5c, empty (and so absent) for an area that asks for none.
	StandIns []tourStandInRecord `json:"stand_in_requests_outside_steps,omitempty"`
	Mail     []tourMailRecord    `json:"outbound_mail,omitempty"`
}

type tourActorRecord struct {
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	Account   string `json:"account_name,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	About     string `json:"about"`
}

type tourRequestRecord struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Query   string   `json:"query,omitempty"`
	Headers []string `json:"headers,omitempty"`
	Body    any      `json:"body,omitempty"`
}

type tourStepRecord struct {
	N             int               `json:"n"`
	Title         string            `json:"title"`
	Actor         string            `json:"actor"`
	ActingIn      string            `json:"acting_in,omitempty"`
	Route         string            `json:"route"`
	Request       tourRequestRecord `json:"request"`
	Notes         []string          `json:"notes,omitempty"`
	Status        int               `json:"status"`
	ContentType   *string           `json:"content_type"`
	Headers       []string          `json:"headers"`
	Security      any               `json:"security_headers"`
	ContentLength any               `json:"content_length"`
	tourBodyView
	Unordered []string           `json:"unordered,omitempty"`
	Elided    []string           `json:"elided,omitempty"`
	Gzip      any                `json:"gzip"`
	Events    *[]tourEventRecord `json:"events,omitempty"`
	// StandIns are the requests the server sent to the area's stand-ins
	// while it answered this step (S5c), in the order they came.
	StandIns []tourStandInRecord `json:"stand_in_requests,omitempty"`
}

type tourEventRecord struct {
	Type    string            `json:"type"`
	Actor   string            `json:"actor"`
	Project string            `json:"project,omitempty"`
	Entity  string            `json:"entity,omitempty"`
	Payload map[string]string `json:"payload"`
}

type tourGzipRecord struct {
	ContentEncoding string  `json:"content_encoding"`
	ContentType     *string `json:"content_type"`
	Vary            *string `json:"vary"`
}

// tourLegend explains the tokens every golden uses.
var tourLegend = []string{
	"<name>: a value the tour registered under that name (an account's id, its .session token and .workspace; an id a step captured)",
	"<uuid:N>, <hex64:N>: another random UUID (version 4) or 64-hex token, numbered by first appearance in this golden",
	"<time@step N>: an RFC 3339 timestamp the server minted during step N (<time@before step N>: during a request the tour sent after step N-1 and records no step for; <time@boot>: before the tour's first request)",
	"<time>: any other RFC 3339 timestamp (one the tour sent, one to come such as an expiry, or a whole second on a route the area declared to write whole seconds); <datetime>: a wall-clock YYYY-MM-DD hh:mm[:ss]; <date>: YYYY-MM-DD",
	"<http-date>, <pdf-date>, _<stamp> (a filename's _YYYYMMDD_hhmmss), <boundary> (a multipart boundary)",
	"<tmp>: the test's temporary directory; 127.0.0.1:<port>, localhost:<port>: the server's own port",
	"<release-version>, <stable-version>: the release the server reports (GET /api/v1/public/release)",
	"version-5 and nil UUIDs are not random and stay as they are",
	"bodies are the server's bytes otherwise: key order, escapes, null against [] and the trailing newline are kept",
	"a GET or HEAD is sent twice: plain, then with Accept-Encoding: gzip (the gzip field); a write is sent once",
	"a signed-in actor's requests carry its session cookie and X-Org-ID: its workspace, or acting_in when the step names another; neither header is listed per request",
	"events: those the step published, oldest first (ties on created_at in the order of their lines), each payload value as its JSON type, then its JSON text, normalised",
}

// render normalises every step, in order, into the golden.
func (tr *tour) render() *tourGolden {
	g := &tourGolden{
		Golden:          filepath.ToSlash(filepath.Join("cmd/server", tourGoldenPath(tr.area.slice, tr.area.key))),
		Test:            tourTestName(tr.area.slice, tr.area.key),
		About:           tr.area.about,
		Regenerate:      tr.regenerate(),
		Normalised:      append(append([]string(nil), tourLegend...), tr.legend...),
		Env:             tourEnvLinesShown(tr.env, tr.shown),
		Profiles:        tr.profiles,
		SignUp:          tr.signedUp,
		SecurityHeaders: tr.security,
		Steps:           []tourStepRecord{},
		Uploads:         []string{},
	}
	for _, a := range append(append([]*tourActor(nil), tr.actors...), tr.anon) {
		rec := tourActorRecord{Name: a.name, Email: a.email, Account: a.display, About: a.about}
		if a.home != "" {
			rec.Workspace = tr.norm.text(a.home)
		}
		g.Actors = append(g.Actors, rec)
	}
	for _, st := range tr.steps {
		g.Steps = append(g.Steps, tr.renderStep(st))
	}
	return g
}

func (tr *tour) renderStep(st *tourStep) tourStepRecord {
	n := tr.norm
	r := st.req
	// A route that writes whole seconds: its times without a fraction stay
	// <time> (tour.wholeSeconds); its events' times are the database's.
	n.whole = tr.whole[r.route] != ""
	defer func() { n.whole = false }()
	rec := tourStepRecord{N: st.n, Title: st.title, Actor: st.actor.name, Route: r.route, Notes: r.notes,
		Status: st.plain.status}
	switch {
	case r.noOrg:
		rec.ActingIn = tourNoOrg
	case st.org != st.actor.home:
		rec.ActingIn = n.text(st.org)
	}
	rec.Request = tourRequestRecord{Method: r.method, Path: n.text(r.path), Query: n.text(r.query)}
	for _, k := range sortedKeys(r.header) {
		for _, v := range r.header[k] {
			rec.Request.Headers = append(rec.Request.Headers, k+": "+n.text(v))
		}
	}
	switch {
	case r.bodyFrom > 0:
		rec.Request.Body = fmt.Sprintf("(what step %d answered, byte for byte)", r.bodyFrom)
	case r.sendBody && len(r.body) > 0:
		if isText(r.body) && !strings.HasPrefix(r.header.Get("Content-Type"), "multipart/") {
			rec.Request.Body = n.text(string(r.body))
		} else if v, err := viewBody(n, r.header.Get("Content-Type"), r.body); err == nil {
			rec.Request.Body = v
		} else {
			tr.t.Errorf("step %d: the request body: %v", st.n, err)
		}
	}
	plain := st.plain
	key := tr.sortKey(st, plain.body)
	body := tr.elideBody(st, tr.reorder(st, key, plain.body))
	if ct := plain.header.Get("Content-Type"); ct != "" || len(plain.header.Values("Content-Type")) > 0 {
		v := n.text(ct)
		rec.ContentType = &v
	}
	rec.Headers = []string{}
	var sec []string
	for _, k := range sortedKeys(plain.header) {
		switch {
		case k == "Date" || k == "Content-Length" || k == "Transfer-Encoding" || k == "Content-Type":
			continue
		case contains(securityHeaderNames, k):
			for _, v := range plain.header[k] {
				sec = append(sec, k+": "+v)
			}
			continue
		}
		for _, v := range plain.header[k] {
			rec.Headers = append(rec.Headers, k+": "+tr.headerValue(k, applyHeaderPatterns(r.hpat, k, n.text(v))))
		}
	}
	if strings.Join(sec, "\n") == strings.Join(tr.security, "\n") {
		rec.Security = "standard"
	} else {
		rec.Security = sec
	}
	lo, hi := bodyBand(n, plain.body)
	if len(r.elide) > 0 {
		lo, hi = tr.elidedBand(n, st, plain.body)
	}
	// A HEAD answer carries the GET's Content-Length and no body.
	if cl := plain.header.Get("Content-Length"); cl != "" && cl != strconv.Itoa(len(plain.body)) && r.method != http.MethodHead {
		tr.t.Errorf("step %d (%s): Content-Length %s but the body has %d bytes", st.n, r.route, cl, len(plain.body))
	}
	switch cl := plain.header.Get("Content-Length"); {
	case cl == "" && r.stream != nil && isEventStream(plain.header):
		// An event stream is flushed before its handler returns, so it never
		// has a Content-Length, whatever its frames hold: null is pinned even
		// when a time in a frame makes their length vary.
		rec.ContentLength = nil
	case lo != hi:
		rec.ContentLength = "<varies>"
	case cl == "":
		rec.ContentLength = nil
	default:
		rec.ContentLength, _ = strconv.Atoi(cl)
	}
	view, err := tr.view(n, plain, body)
	if err != nil {
		tr.t.Errorf("step %d (%s): %v", st.n, r.route, err)
	}
	rec.tourBodyView = view
	for _, u := range r.unordered {
		rec.Unordered = append(rec.Unordered, fmt.Sprintf("%s: %s", u[0], u[1]))
	}
	for _, e := range r.elide {
		rec.Elided = append(rec.Elided, fmt.Sprintf("%s: %s (%s)", e.pointer, e.token, e.why))
	}
	rec.Gzip = tr.renderGzip(st, key, body, lo, hi)
	n.whole = false
	if events := tr.renderEvents(st.events); len(events) > 0 || !isRead(r.method) {
		rec.Events = &events
	}
	rec.StandIns = tr.renderStandInRequests(fmt.Sprintf("step %d", st.n))
	return rec
}

// view shows an answer's body: a 206's single range of a file as bytes (a
// multipart answer's ranges are shown part by part), anything else by its
// kind (viewBody), such as a 416's text, which carries a Content-Range too.
func (tr *tour) view(n *tourNormaliser, ex *tourExchange, body []byte) (tourBodyView, error) {
	if ex.status == http.StatusPartialContent && ex.header.Get("Content-Range") != "" && len(body) > 0 {
		return viewPartial(body), nil
	}
	return viewBody(n, ex.header.Get("Content-Type"), body)
}

// sortKey is the key a step's unordered pointers sort by, fixed once for
// the step, before its answer is numbered, so that the plain answer and its
// gzip variant sort the same way: a value the golden numbered in an earlier
// step keeps its number, and so does one the answer holds outside the
// unordered arrays (numbered first, in the key only, so that a link naming
// an artifact of the same answer is told apart by it); a value first seen in
// an unordered element is written without a number, so the order does not
// depend on which random id came first. nil when the step sorts nothing.
func (tr *tour) sortKey(st *tourStep, body []byte) *tourNormaliser {
	if len(st.req.unordered) == 0 || !json.Valid(body) {
		return nil
	}
	key := tr.norm.clone(false)
	var spans []jsonSpan
	for _, u := range st.req.unordered {
		found, err := jsonFind(body, u[0])
		if err != nil {
			break // reorder reports it
		}
		spans = append(spans, found...)
	}
	// Blank each named array or object (the outermost, where one holds
	// another) and number what is left; the spans are disjoint then, so
	// blanking from the end of the body back keeps the earlier spans where
	// they are. The blanked text is only numbered, never parsed, so an object
	// is blanked as [] too.
	var outer []jsonSpan
	for _, sp := range spans {
		inside := false
		for _, o := range spans {
			if o != sp && o.start <= sp.start && sp.end <= o.end {
				inside = true
			}
		}
		if !inside && !slices.Contains(outer, sp) {
			outer = append(outer, sp)
		}
	}
	sort.Slice(outer, func(i, j int) bool { return outer[i].start > outer[j].start })
	rest := append([]byte(nil), body...)
	for _, sp := range outer {
		rest = append(append(append([]byte(nil), rest[:sp.start]...), "[]"...), rest[sp.end:]...)
	}
	key.text(string(rest))
	key.flatNew = true
	return key
}

// reorder applies a step's unordered pointers to a body, sorting by the
// elements' text normalised with the step's sort key (sortKey), so the order
// does not depend on which random id came first.
func (tr *tour) reorder(st *tourStep, key *tourNormaliser, body []byte) []byte {
	if len(st.req.unordered) == 0 {
		return body
	}
	if !json.Valid(body) || key == nil {
		tr.t.Errorf("step %d: unordered applies to JSON answers only", st.n)
		return body
	}
	for _, u := range st.req.unordered {
		out, err := jsonReorder(body, u[0], func(b []byte) string { return key.text(string(b)) })
		if err != nil {
			tr.t.Errorf("step %d: unordered %s: %v", st.n, u[0], err)
			return body
		}
		body = out
	}
	return body
}

// renderGzip records the gzip variant of a GET, after checking that it
// carries the same answer and that the plain answer's length cannot fall on
// either side of the compressor's floor on another run.
func (tr *tour) renderGzip(st *tourStep, key *tourNormaliser, plainBody []byte, lo, hi int) any {
	r := st.req
	switch {
	case r.once != "":
		return "not sent: " + r.once
	case !isRead(r.method):
		return "not sent: a write is sent once, without Accept-Encoding"
	case st.gzip == nil:
		return "not sent: the step set its own Accept-Encoding"
	}
	gz := st.gzip
	if tourStraddles(lo, hi) {
		tr.t.Errorf("step %d (%s): the answer is %d bytes and could be anything from %d to %d on another run, "+
			"either side of the compressor's %d-byte floor, so whether its gzip variant is compressed (and carries "+
			"a Content-Type, Q1) would change from run to run: resize the fixture so it stays clear of the floor",
			st.n, r.route, len(plainBody), lo, hi, compressFloor)
	}
	if gz.status != st.plain.status {
		tr.t.Errorf("step %d (%s): %d without gzip but %d with it", st.n, r.route, st.plain.status, gz.status)
	}
	// The same view of both bodies: a document rendered twice differs in its
	// bytes (dates, font subsets) but not in what the golden shows of it.
	cmp := tr.norm.clone(false)
	gzView := *gz
	if gz.header.Get("Content-Type") == "" {
		// Q1: a compressed answer the handler gave no type is read as the
		// plain one was.
		gzView.header = gz.header.Clone()
		gzView.header.Set("Content-Type", st.plain.header.Get("Content-Type"))
	}
	va, erra := tr.view(cmp, st.plain, plainBody)
	vb, errb := tr.view(cmp, &gzView, tr.elideBody(st, tr.reorder(st, key, gz.body)))
	a, _ := json.MarshalIndent(va, "", "  ")
	b, _ := json.MarshalIndent(vb, "", "  ")
	if erra != nil || errb != nil || !bytes.Equal(a, b) {
		tr.t.Errorf("step %d (%s): the gzip variant's body differs from the plain one's:\n%s", st.n, r.route,
			lineDiff(string(a), string(b)))
	}
	// Every other header must be the plain answer's. The bodies were
	// compared above; a time in them may differ in length between the two
	// answers, and so may Content-Length. A compressed answer adds
	// Content-Encoding and Vary and may lose its Content-Type, which the
	// record below shows.
	skip := map[string]bool{"Date": true, "Content-Length": true}
	if gz.gzipped {
		skip["Content-Type"], skip["Content-Encoding"], skip["Vary"] = true, true, true
	}
	for k := range mergeKeys(headerKeys(st.plain.header), headerKeys(gz.header)) {
		a, b := strings.Join(st.plain.header[k], "\n"), strings.Join(gz.header[k], "\n")
		if !skip[k] && tr.headerValue(k, cmp.text(a)) != tr.headerValue(k, cmp.text(b)) {
			tr.t.Errorf("step %d (%s): the gzip variant's %s differs from the plain answer's (%q against %q)",
				st.n, r.route, k, b, a)
		}
	}
	if !gz.gzipped {
		return "not compressed: the same answer as without Accept-Encoding"
	}
	rec := tourGzipRecord{ContentEncoding: gz.header.Get("Content-Encoding")}
	if vs := gz.header.Values("Content-Type"); len(vs) > 0 {
		v := tr.norm.text(vs[0])
		rec.ContentType = &v
	}
	if vs := gz.header.Values("Vary"); len(vs) > 0 {
		v := strings.Join(vs, ", ")
		rec.Vary = &v
	}
	return rec
}

// renderEvents reduces a step's events to type, actor, project, entity and
// payload key -> JSON type and normalised value, oldest first; events
// stamped with the same time are put in the order of their rendered lines
// (with the numbers the golden gave their ids already), since the server
// breaks that tie by random id.
func (tr *tour) renderEvents(raw []json.RawMessage) []tourEventRecord {
	type ev struct {
		at  string
		key string
		e   struct {
			EventType string         `json:"event_type"`
			Actor     string         `json:"actor"`
			ProjectID string         `json:"project_id"`
			EntityID  string         `json:"entity_id"`
			Payload   map[string]any `json:"payload"`
			CreatedAt string         `json:"created_at"`
		}
	}
	flat := tr.norm.sortKey()
	evs := make([]*ev, 0, len(raw))
	for _, r := range raw {
		e := &ev{}
		d := json.NewDecoder(bytes.NewReader(r))
		d.UseNumber()
		if err := d.Decode(&e.e); err != nil {
			tr.t.Errorf("an event that does not decode: %v\n%s", err, r)
			continue
		}
		e.at = e.e.CreatedAt
		rec := tr.eventRecord(flat, e.e.EventType, e.e.Actor, e.e.ProjectID, e.e.EntityID, e.e.Payload)
		line, _ := json.Marshal(rec)
		e.key = string(line)
		evs = append(evs, e)
	}
	// The server's order across times is kept; each run of equal times is
	// sorted by its lines.
	for i := 0; i < len(evs); {
		j := i + 1
		for j < len(evs) && evs[j].at == evs[i].at {
			j++
		}
		sort.SliceStable(evs[i:j], func(a, b int) bool { return evs[i+a].key < evs[i+b].key })
		i = j
	}
	out := []tourEventRecord{}
	for _, e := range evs {
		out = append(out, tr.eventRecord(tr.norm, e.e.EventType, e.e.Actor, e.e.ProjectID, e.e.EntityID, e.e.Payload))
	}
	return out
}

// eventRecord renders one event: each payload value as its JSON type, then
// its JSON text, normalised. Other code reads the values (automation
// triggers match on them, the notifier on "to", the activity log shows
// them), so they are pinned too. The values are normalised in key order, so
// an id first seen in them is numbered the same way on every run.
func (tr *tour) eventRecord(n *tourNormaliser, typ, actor, project, entity string, payload map[string]any) tourEventRecord {
	rec := tourEventRecord{Type: typ, Actor: n.text(actor), Project: n.text(project), Entity: n.text(entity),
		Payload: map[string]string{}}
	for _, k := range sortedKeys(payload) {
		var raw bytes.Buffer
		enc := json.NewEncoder(&raw)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(payload[k]); err != nil {
			tr.t.Errorf("an event's payload value %s does not encode: %v", k, err)
		}
		rec.Payload[k] = jsonType(payload[k]) + " " + n.text(strings.TrimSuffix(raw.String(), "\n"))
	}
	return rec
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Coverage, and the areas each golden belongs to (no database)

// tourGoldenIndex is what coverage and the claim check read of a golden.
type tourGoldenIndex struct {
	Test  string `json:"test"`
	Steps []struct {
		Route  string `json:"route"`
		Status int    `json:"status"`
	} `json:"steps"`
}

// tourSlices lists the slice directories under testdata/tour.
func tourSlices(t *testing.T) []string {
	entries, err := os.ReadDir(filepath.Join("testdata", "tour"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// tourSliceGoldens reads a slice's area goldens, by area key.
func tourSliceGoldens(t *testing.T, slice string) map[string]tourGoldenIndex {
	files, err := filepath.Glob(filepath.Join("testdata", "tour", slice, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]tourGoldenIndex{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var g tourGoldenIndex
		if err := json.Unmarshal(data, &g); err != nil {
			t.Fatalf("cmd/server/%s is not a tour golden: %v", filepath.ToSlash(f), err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = g
	}
	return out
}

// readRouteList is routes.txt in its order.
func readRouteList(t *testing.T) []string {
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "api", "testdata", "routes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// tourCovered is whether a route's recorded statuses reached its handler:
// any status but 401, which the auth middleware answers before routing.
func tourCovered(statuses map[int]bool) bool {
	for s := range statuses {
		if s != http.StatusUnauthorized {
			return true
		}
	}
	return false
}

// tourSucceeded is whether a recorded step got a 2xx or 3xx from the route:
// its handler did its work, beyond a guard's, a decode's or a lookup's
// refusal. A phantom-id matrix (S5e) reaches nearly every route with
// refusals alone, so the plan's floor counts these.
func tourSucceeded(statuses map[int]bool) bool {
	for s := range statuses {
		if s >= 200 && s < 400 {
			return true
		}
	}
	return false
}

// tourCoverage renders a slice's coverage.txt from its goldens and returns
// the routes it reaches and those with a 2xx or 3xx answer.
func tourCoverage(t *testing.T, slice string, routes []string) ([]byte, map[string]bool, map[string]bool) {
	goldens := tourSliceGoldens(t, slice)
	known := map[string]bool{}
	for _, r := range routes {
		known[r] = true
	}
	statuses := map[string]map[int]bool{}
	areas := map[string]map[string]bool{}
	for _, key := range sortedKeys(goldens) {
		for _, st := range goldens[key].Steps {
			if !known[st.Route] {
				if !strings.HasSuffix(st.Route, " unmatched") {
					t.Errorf("cmd/server/testdata/tour/%s/%s.json records %q, which is not a route of "+
						"internal/api/testdata/routes.txt", slice, key, st.Route)
				}
				continue
			}
			if statuses[st.Route] == nil {
				statuses[st.Route], areas[st.Route] = map[int]bool{}, map[string]bool{}
			}
			statuses[st.Route][st.Status] = true
			areas[st.Route][key] = true
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `# The routes the %s slice of the API tour reached (refactor plan §6.4
# S5a-S5e), derived by TestTourCoverage (cmd/server/tour_test.go) from the
# area goldens beside this file, with no database. Regenerate with
#   %s
# (regenerating an area rewrites it too). A line is a route of
# internal/api/testdata/routes.txt, in that file's order, that a recorded
# step sent: the statuses it answered, then the areas that recorded it. A
# route that answered only 401 is marked "not counted": the auth middleware
# answers that before routing, so its handler never ran. A route reached
# that answered only errors is marked "errors only": its handler ran, but
# only as far as a guard's, a decode's or a lookup's refusal, and the plan's
# 90%% floor counts only the routes with a 2xx or 3xx answer.

`, strings.ToUpper(slice[:1])+slice[1:], tourCoverageRegenerate)
	covered, succeeded := map[string]bool{}, map[string]bool{}
	for _, r := range routes {
		ss := statuses[r]
		if ss == nil {
			continue
		}
		var codes []string
		for _, s := range sortedInts(ss) {
			codes = append(codes, strconv.Itoa(s))
		}
		line := fmt.Sprintf("%s: %s; %s", r, strings.Join(codes, ", "), strings.Join(sortedKeys(areas[r]), ", "))
		switch {
		case !tourCovered(ss):
			line += " (not counted)"
		case !tourSucceeded(ss):
			covered[r] = true
			line += " (errors only)"
		default:
			covered[r], succeeded[r] = true, true
		}
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\n%s: %d of %d routes reached (%.1f%%), %d with a 2xx or 3xx answer (%.1f%%)\n", slice,
		len(covered), len(routes), 100*float64(len(covered))/float64(len(routes)), len(succeeded),
		100*float64(len(succeeded))/float64(len(routes)))
	return []byte(b.String()), covered, succeeded
}

func sortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// writeTourCoverage checks, or rewrites under UPDATE_GOLDEN=1, a slice's
// coverage.txt, and returns the routes it reaches and those with a 2xx or
// 3xx answer.
func writeTourCoverage(t *testing.T, slice string) (map[string]bool, map[string]bool) {
	got, covered, succeeded := tourCoverage(t, slice, readRouteList(t))
	checkGolden(t, filepath.Join("testdata", "tour", slice, "coverage.txt"), got, tourCoverageRegenerate)
	return covered, succeeded
}

// TestTourCoverage checks each slice's coverage.txt against its area
// goldens, with no database, and logs the unions across slices: the routes
// reached, and those with a 2xx or 3xx answer, which S5e holds to the
// plan's 90% of the 341 routes.
func TestTourCoverage(t *testing.T) {
	tourMu.Lock()
	defer tourMu.Unlock()
	routes := readRouteList(t)
	reached, succeeded := map[string]bool{}, map[string]bool{}
	for _, slice := range tourSlices(t) {
		c, s := writeTourCoverage(t, slice)
		for r := range c {
			reached[r] = true
		}
		for r := range s {
			succeeded[r] = true
		}
	}
	t.Logf("the API tour reaches %d of the %d routes (%.1f%%) across its slices, %d of them (%.1f%%) with a 2xx "+
		"or 3xx answer", len(reached), len(routes), 100*float64(len(reached))/float64(len(routes)), len(succeeded),
		100*float64(len(succeeded))/float64(len(routes)))
}

var tourAreaFileRE = regexp.MustCompile(`^tour_(s\d+[a-z]?)_([a-z0-9_]+)_test\.go$`)

// TestTourGoldensAreClaimed checks, with no database, that every tour
// golden belongs to exactly one area and every area has its golden: it reads
// each cmd/server/tour_<slice>_<key>_test.go for the one TestTour function
// that calls runTourArea with that slice and key, so that CI's check that
// each golden's test passed covers every golden, and no area needs a list
// anyone edits.
func TestTourGoldensAreClaimed(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	type area struct{ file, test string }
	areas := map[string]area{} // "slice/key"
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		m := tourAreaFileRE.FindStringSubmatch(f)
		var found []string
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "runTourArea" {
					return true
				}
				slice, key := tourAreaLiteral(call)
				found = append(found, fd.Name.Name+" "+slice+" "+key)
				return true
			})
		}
		switch {
		case m == nil && len(found) > 0:
			t.Errorf("%s calls runTourArea, but a tour area lives in a file of its own, "+
				"cmd/server/tour_<slice>_<key>_test.go", f)
		case m == nil:
		case len(found) != 1:
			t.Errorf("%s must hold exactly one test function calling runTourArea; it has %d", f, len(found))
		default:
			parts := strings.Fields(found[0])
			if len(parts) != 3 || parts[1] != m[1] || parts[2] != m[2] {
				t.Errorf("%s: its area must be tourArea{slice: %q, key: %q, ...} as string literals (found %q)",
					f, m[1], m[2], found[0])
				continue
			}
			if want := tourTestName(m[1], m[2]); parts[0] != want {
				t.Errorf("%s: the area's test function must be named %s, not %s", f, want, parts[0])
			}
			areas[m[1]+"/"+m[2]] = area{file: f, test: parts[0]}
		}
	}
	onDisk := map[string]bool{}
	for _, slice := range tourSlices(t) {
		for key, g := range tourSliceGoldens(t, slice) {
			id := slice + "/" + key
			onDisk[id] = true
			a, ok := areas[id]
			switch {
			case !ok:
				t.Errorf("cmd/server/testdata/tour/%s.json is written by no area: delete it with the area that wrote it, "+
					"or add cmd/server/tour_%s_%s_test.go back", id, slice, key)
			case g.Test != a.test:
				t.Errorf("cmd/server/testdata/tour/%s.json names test %q, but its area runs as %s; regenerate it with:\n  %s",
					id, g.Test, a.test, tourAreaRegenerate(slice, key))
			}
		}
	}
	for _, id := range sortedKeys(areas) {
		if !onDisk[id] {
			slice, key, _ := strings.Cut(id, "/")
			t.Errorf("the tour area %s (%s) has no golden cmd/server/testdata/tour/%s.json; create it with:\n  %s",
				id, areas[id].file, id, tourAreaRegenerate(slice, key))
		}
	}
}

// tourAreaLiteral reads slice and key from runTourArea(t, tourArea{...}).
func tourAreaLiteral(call *ast.CallExpr) (slice, key string) {
	if len(call.Args) != 2 {
		return "", ""
	}
	lit, ok := call.Args[1].(*ast.CompositeLit)
	if !ok {
		return "", ""
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, _ := kv.Key.(*ast.Ident)
		val, _ := kv.Value.(*ast.BasicLit)
		if name == nil || val == nil || val.Kind != token.STRING {
			continue
		}
		s, _ := strconv.Unquote(val.Value)
		switch name.Name {
		case "slice":
			slice = s
		case "key":
			key = s
		}
	}
	return slice, key
}

// TestTourRequests checks, with no database, how the tour builds a request
// from a route and its options, the helpers that later slices rely on (a
// bearer actor, an area's own pattern), and the golden's encoding: its
// escaped invisible characters, its upload list in the same order whatever
// order the walk found the files in, and the step summary of an area that
// stopped early.
func TestTourRequests(t *testing.T) {
	tr := &tour{t: t, norm: newTourNormaliser(), names: map[string]string{}, values: map[string]string{}}
	tr.loadRoutes()
	const p = "8b0c2f47-6a3e-4c1d-9f2a-5e7b1c3d4a6f"
	tr.remember("p", p)
	check := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: got %q, want %q", what, got, want)
		}
	}

	r := tr.build("GET /api/v1/projects/{id}", []tourOpt{at("id", "{{p}}"), query("q={{p}}"),
		withHeader("X-Tour", "{{p}}"), once("a side effect"), unordered("/list", "a random order"), note("a note"),
		actingIn("{{p}}")})
	check("the path", r.path, "/api/v1/projects/"+p)
	check("the query", r.query, "q="+p)
	check("the header", r.header.Get("X-Tour"), p)
	check("once", r.once, "a side effect")
	check("unordered", fmt.Sprint(r.unordered), "[[/list a random order]]")
	check("the notes", strings.Join(r.notes, "|"), "a note")
	check("the workspace sent", r.workspace(&tourActor{org: "home"}), p)
	check("the workspace sent without actingIn", tr.build("GET /api/v1/projects", nil).workspace(&tourActor{org: "home"}), "home")

	r = tr.build("GET unmatched", []tourOpt{rawPath("/api/v1/nowhere/{{p}}")})
	check("an unmatched path", r.path, "/api/v1/nowhere/"+p)

	r = tr.build("POST /api/v1/projects", []tourOpt{jsonBody(`{"parent":"{{p}}"}`), expect(http.StatusCreated)})
	check("the body", string(r.body), `{"parent":"`+p+`"}`)
	check("the body's type", r.header.Get("Content-Type"), "application/json")
	check("expect", strconv.Itoa(r.expect), "201")

	step := &tourStep{n: 7}
	r = tr.build("POST /api/v1/projects/import", []tourOpt{answerOf(&tourResult{tr: tr, step: step, body: []byte("{}")},
		"application/json")})
	check("answerOf's body", string(r.body), "{}")
	check("answerOf's step", strconv.Itoa(r.bodyFrom), "7")

	w := tr.bearerActor("worker", "tour-worker-credential", "a workspace runner key")
	check("the bearer", w.bearer, "tour-worker-credential")
	check("the bearer's name", tr.id("worker.token"), "tour-worker-credential")

	tr.pattern(`run #(\d+)`, "<run>", "a run number")
	check("an area's pattern", tr.norm.text("run #42 of "+p+" by tour-worker-credential"), "run #<run> of <p> by <worker.token>")

	// A header's own pattern: that header's values only (any spelling of its
	// name), its group replaced, and a legend line; a body, another header
	// and the gzip comparison's other values are left as they are.
	tr.headerPattern("retry-after", `^(59|60)$`, "<retry-after 60 s>", "a countdown")
	tr.headerPattern("X-Tour-Id", `id=(\d+)`, "<n>", "a counter")
	check("a header's pattern", tr.headerValue("Retry-After", "59"), "<retry-after 60 s>")
	check("a header's pattern, a value it does not match", tr.headerValue("Retry-After", "61"), "61")
	check("a header's pattern, in another header", tr.headerValue("X-Retry", "60"), "60")
	check("a header's pattern, in a body", tr.norm.text("60"), "60")
	check("a header's pattern, its group", tr.headerValue("X-Tour-Id", "id=12; id=3"), "id=<n>; id=<n>")
	check("a header's pattern, its legend", tr.legend[len(tr.legend)-2], "<retry-after 60 s>: a countdown (the area's "+
		"own pattern ^(59|60)$, in the Retry-After header only)")

	// An area's own server environment: TZ=UTC and its variables, listed in
	// the golden with the recording proxy's address as a placeholder; an
	// area with none lists what every S5a golden lists. A variable the
	// harness or the tour sets, NO_PROXY, and a lower-case name are refused.
	withProxy := func(env map[string]string) map[string]string {
		env["HTTP_PROXY"], env["HTTPS_PROXY"] = "http://127.0.0.1:1", "http://127.0.0.1:1"
		return env
	}
	env, err := tourServerEnv(tourArea{})
	if err != nil {
		t.Fatal(err)
	}
	check("the env of an area with none", strings.Join(tourEnvLines(withProxy(env)), "|"),
		"(beyond the harness's fixed environment, cmd/server/harness_test.go)|HTTPS_PROXY="+proxyPlaceholder+
			"|HTTP_PROXY="+proxyPlaceholder+"|TZ=UTC")
	env, err = tourServerEnv(tourArea{env: map[string]string{"OPENV_MAX_EVIDENCE_MB": "1", "A_KNOB": "x y"}})
	if err != nil {
		t.Fatal(err)
	}
	check("the env of an area with its own", strings.Join(tourEnvLines(withProxy(env)), "|"),
		"(beyond the harness's fixed environment, cmd/server/harness_test.go)|A_KNOB=x y|HTTPS_PROXY="+
			proxyPlaceholder+"|HTTP_PROXY="+proxyPlaceholder+"|OPENV_MAX_EVIDENCE_MB=1|TZ=UTC")
	for _, k := range []string{"PORT", "UPLOADS_DIR", "HOSTED_RUNNERS", "DATABASE_URL", "TZ", "HTTP_PROXY", "HTTPS_PROXY",
		"NO_PROXY", "no_proxy", "http_proxy", "Tz"} {
		if _, err := tourServerEnv(tourArea{env: map[string]string{k: "x"}}); err == nil {
			t.Errorf("an area may set %s in its server's environment", k)
		}
	}

	// The golden's encoding: an invisible rune becomes its JSON escape and
	// reads back as itself.
	raw := []byte("\"a\ufeffb\u00a0c\u200bd\U000e0001\"\n")
	enc := escapeInvisible(raw)
	check("escaped", string(enc), `"a\ufeffb\u00a0c\u200bd\udb40\udc01"`+"\n")
	var a, b string
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(enc, &b) != nil || a != b {
		t.Errorf("the escaped text reads back as %q, not %q", b, a)
	}

	// A multipart body's parts: one within tourSmallBinary as its text, one
	// past it by its size and digest, so that an upload made to reach a size
	// limit does not write its megabyte into the golden.
	big := bytes.Repeat([]byte("Z"), tourSmallBinary+1)
	ct, form := multipartForm([][2]string{{"note", "short"}},
		tourFormFile{field: "file", name: "big.txt", contentType: "text/plain", data: big})
	v, err := viewBody(tr.norm, ct, form)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := json.Marshal(v.Summary)
	check("a multipart body's parts", string(parts), `[{"headers":["Content-Disposition: form-data; name=\"note\""],`+
		`"body":"short"},{"headers":["Content-Disposition: form-data; name=\"file\"; filename=\"big.txt\"",`+
		`"Content-Type: text/plain"],"digest":{"size":4097,`+
		`"sha256":"252a3370c1b76874608df4bddcec49b44df2367ea4b618aa7a5acb4b35709c97"}}]`)

	// The upload list, whatever order the files were found in.
	u := []string{"1f6a1e57-2c1b-4e0a-9d3c-7b8a6f5e4d3c", "2a7b2f68-3d2c-4f1b-8e4d-8c9b7a6f5e4d",
		"0e5f0d46-1b0a-4d9f-ac2b-6a7f5e4d3c2b"}
	tr.remember("known", u[1])
	listed := []string{u[0] + "_fig.png (75 bytes)", u[1] + "_fig.png (75 bytes)", u[2] + "_fig.png (75 bytes)",
		u[0] + "_doc.pdf (9 bytes)"}
	base := tr.norm
	var first []string
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}} {
		tr.norm = base.clone(false)
		var in []string
		for _, i := range order {
			in = append(in, listed[i])
		}
		got := tr.renderUploads(in)
		if first == nil {
			first = got
			check("the uploads", strings.Join(got, ", "),
				"<known>_fig.png (75 bytes), <uuid:1>_doc.pdf (9 bytes), <uuid:1>_fig.png (75 bytes), <uuid:2>_fig.png (75 bytes)")
		}
		check(fmt.Sprintf("the uploads found in the order %v", order), strings.Join(got, ", "), strings.Join(first, ", "))
	}

	// An area that stopped early is compared step by step up to where it
	// stopped.
	golden := []byte(`{"steps":[{"n":1,"title":"one","route":"GET /a","status":200},` +
		`{"n":2,"title":"two","route":"GET /b","status":200},{"n":3,"title":"three","route":"GET /c","status":200}],` +
		`"uploads":["x"]}`)
	stopped := []byte(`{"steps":[{"n":1,"title":"one","route":"GET /a","status":200},` +
		`{"n":2,"title":"two","route":"GET /b","status":403}]}`)
	check("an early stop", tourChanges(golden, stopped, true),
		"  step 2, two (GET /b): status\n  (not run: the golden's steps 3 to 3)")
	check("a full run", tourChanges(golden, stopped, false),
		"  step 2, two (GET /b): status\n  gone: step 3, three (GET /c)\n  uploads")
}

// TestTourOrder checks, with no database, that what the tour sorts comes out
// the same whatever order the server sent it in: an unordered array whose
// elements differ only in ids an earlier step numbered, or in ids the same
// answer holds outside the array (links to the artifacts beside them), and
// events stamped with the same time.
func TestTourOrder(t *testing.T) {
	const (
		a, b = "1a6b2c3d-4e5f-4a7b-8c9d-0e1f2a3b4c5d", "2b7c3d4e-5f6a-4b8c-9d0e-1f2a3b4c5d6e"
		x, y = "3c8d4e5f-6a7b-4c9d-8e1f-2a3b4c5d6e7f", "4d9e5f6a-7b8c-4d0e-9f2a-3b4c5d6e7f8a"
	)
	render := func(prior string, body func(first, second [2]string) string, pointer string) []string {
		var out []string
		for _, order := range [][2][2]string{{{x, a}, {y, b}}, {{y, b}, {x, a}}} {
			tr := &tour{t: t, norm: newTourNormaliser()}
			tr.norm.text(prior) // numbered by an earlier step
			raw := []byte(body(order[0], order[1]))
			st := &tourStep{n: 1, req: &tourReq{unordered: [][2]string{{pointer, "the test"}}}}
			key := tr.sortKey(st, raw)
			out = append(out, tr.norm.text(string(tr.reorder(st, key, raw))))
		}
		return out
	}
	links := func(first, second [2]string) string {
		return fmt.Sprintf(`{"links":[{"id":%q,"to":%q},{"id":%q,"to":%q}]}`, first[0], first[1], second[0], second[1])
	}
	if got := render(a+" "+b, links, "/links"); got[0] != got[1] {
		t.Errorf("links to ids an earlier step numbered sort by the server's order:\n%s\n%s", got[0], got[1])
	}
	beside := func(first, second [2]string) string {
		return fmt.Sprintf(`{"artifacts":[{"id":%q},{"id":%q}],"links":[{"id":%q,"to":%q},{"id":%q,"to":%q}]}`,
			a, b, first[0], first[1], second[0], second[1])
	}
	if got := render("", beside, "/links"); got[0] != got[1] {
		t.Errorf("links to the artifacts of the same answer sort by the server's order:\n%s\n%s", got[0], got[1])
	}

	// An object keyed by random ids (a Go map, which encoding/json writes in
	// the order of its keys): members first seen in it, told apart by their
	// values, and members with equal values whose ids an earlier step
	// numbered.
	for _, c := range []struct{ prior, first, second, want string }{
		{"", fmt.Sprintf(`%q:"pass"`, x), fmt.Sprintf(`%q:"fail"`, y),
			`{"entries":[{"latest_results":{"<uuid:1>":"fail","<uuid:2>":"pass"}}]}`},
		{b + " " + a, fmt.Sprintf(`%q:"pass"`, a), fmt.Sprintf(`%q:"pass"`, b),
			`{"entries":[{"latest_results":{"<uuid:1>":"pass","<uuid:2>":"pass"}}]}`},
	} {
		var got []string
		for _, order := range [][2]string{{c.first, c.second}, {c.second, c.first}} {
			tr := &tour{t: t, norm: newTourNormaliser()}
			tr.norm.text(c.prior)
			raw := []byte(`{"entries":[{"latest_results":{` + order[0] + `,` + order[1] + `}}]}`)
			st := &tourStep{n: 1, req: &tourReq{unordered: [][2]string{{"/entries/*/latest_results", "the test"}}}}
			got = append(got, tr.norm.text(string(tr.reorder(st, tr.sortKey(st, raw), raw))))
		}
		if got[0] != got[1] || got[0] != c.want {
			t.Errorf("an object keyed by random ids, sorted:\n%s\n%s\nwant %s", got[0], got[1], c.want)
		}
	}

	// Events stamped with the same time, in either order.
	var events []string
	for _, order := range [][2]string{{a, b}, {b, a}} {
		tr := &tour{t: t, norm: newTourNormaliser()}
		tr.norm.text(a + " " + b)
		var raw []json.RawMessage
		for i, id := range order {
			raw = append(raw, json.RawMessage(fmt.Sprintf(`{"id":"e%d","event_type":"artifact.updated","actor":"tour",`+
				`"entity_id":%q,"payload":{"version":2},"created_at":"2026-09-27T12:00:00.5Z"}`, i, id)))
		}
		var line bytes.Buffer
		enc := json.NewEncoder(&line)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(tr.renderEvents(raw))
		events = append(events, strings.TrimSpace(line.String()))
	}
	if events[0] != events[1] {
		t.Errorf("events stamped with the same time keep the server's order:\n%s\n%s", events[0], events[1])
	}
	want := `[{"type":"artifact.updated","actor":"tour","entity":"<uuid:1>","payload":{"version":"number 2"}},` +
		`{"type":"artifact.updated","actor":"tour","entity":"<uuid:2>","payload":{"version":"number 2"}}]`
	if events[0] != want {
		t.Errorf("events:\n got %s\nwant %s", events[0], want)
	}

	// Coverage counts a route reached by any answer but 401, and as
	// succeeded only with a 2xx or 3xx.
	for _, c := range []struct {
		statuses           []int
		reached, succeeded bool
	}{{[]int{401}, false, false}, {[]int{401, 404}, true, false}, {[]int{403, 500}, true, false},
		{[]int{404, 204}, true, true}, {[]int{302}, true, true}} {
		ss := map[int]bool{}
		for _, s := range c.statuses {
			ss[s] = true
		}
		if tourCovered(ss) != c.reached || tourSucceeded(ss) != c.succeeded {
			t.Errorf("coverage of %v: reached %t, succeeded %t", c.statuses, tourCovered(ss), tourSucceeded(ss))
		}
	}
}
