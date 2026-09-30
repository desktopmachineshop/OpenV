//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBootProfiles boots the real server once per environment profile of
// refactor plan §6.4 S4b and compares what it saw with
// testdata/boot/<profile>.txt, beside S4a's default.txt (invariants I8, I13
// and I17; quirk Q11; OpenV REQ-18 and REQ-169). Every profile boots through
// S4a's runBootProfile and runs S4a's whole probe list, then the billing
// probes below, then S4a's drain, so a profile's golden differs from
// default.txt only where its setting changes what the server does, and in
// what S4b adds:
//
//   - the workspace's effective limits (GET /api/v1/orgs/{id}/limits, its
//     values only), which the self-hosted, tiers and OPENV_LIMITS settings
//     change;
//   - the billing probes: every billing route (the public catalogue, the
//     workspace's billing read and refresh, checkout, plan change and
//     portal) and the billing_provider_requests_total series on /metrics;
//   - HTTP_PROXY and HTTPS_PROXY at a recordingProxy (harness_test.go), and
//     at the end of the golden every request the server sent through it,
//     all refused there, so no profile's server reaches anything off the
//     machine and each golden says what it tried.
//
// The profiles: one per setting the plan lists, SECURE_COOKIES and
// CROSS_SITE_COOKIES (the session cookie's attributes, HSTS),
// OPENV_SELF_HOSTED, the plan tiers on (a valid
// OPENV_BILLING_GRANDFATHER_BEFORE), OPENV_REGISTRATION=closed, OPENV_LIMITS,
// OPENV_BUILD_SHA (/health and /api/v1/public/build then report the commit)
// and billing on (a test key and a one-entry price map); then two that
// combine settings: billing configured on a self-hosted deployment, which
// keeps it off and says so, and the boot the plan says must still come up, a
// self-hosted deployment with a malformed grandfather date, which it reads
// only when not self-hosted; and one of malformed settings, those the server
// reads at boot and those a request reads, which it must still come up with,
// each at its default and named once in the boot log (internal/envparse;
// #379, question 15), SECURE_COOKIES among them although main reads it twice.
// boot_misconfigured_test.go holds the boots that must not come up.
func TestBootProfiles(t *testing.T) {
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	bin := serverBinary(t)
	for _, p := range s4bProfiles {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			got := runBootProfile(t, bin, p, profilesGoldenHeader)
			checkGolden(t, filepath.Join("testdata", "boot", p.name+".txt"), got, bootProfilesRegenerate)
			took := time.Since(start)
			if took > bootProfileBudget {
				t.Errorf("profile %s took %s to boot, probe and drain; the budget is %s (refactor plan §6.4 S4)",
					p.name, took.Round(time.Millisecond), bootProfileBudget)
			}
			t.Logf("profile %s: booted, probed and drained in %s", p.name, took.Round(time.Millisecond))
		})
	}
}

const bootProfilesRegenerate = testDatabaseURLEnv + "=<server URL> " + updateGoldenEnv +
	"=1 go test ./cmd/server -count=1 -run '^TestBootProfiles$'"

const profilesGoldenHeader = `# The server binary (go build -cover ./cmd/server) booted on a fresh
# database under one environment profile and probed from outside (refactor
# plan §6.4 S4b; invariants I8, I13, I17; quirk Q11). Written by
# TestBootProfiles (cmd/server/boot_profiles_test.go) through the S4a harness
# and probes (cmd/server/{harness,boot_smoke}_test.go), then the S4b billing
# probes; the server's HTTP_PROXY and HTTPS_PROXY are the test's recording
# proxy, which refuses everything. Regenerate with
#   OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootProfiles$'
# against a server with or without the vector extension.
# Normalised: log timestamps dropped; durations, ports, the release version,
# UUIDs, the temporary directory, the session token and its expiry replaced
# by <...>; the Date header dropped; a no-vector server's warning taken out;
# the recording proxy's address shown as <recording proxy port>.

`

// The profiles' fixed values. The Stripe key is a test-mode key of no
// account (sk_test_ keys never move money), and the price id names nothing:
// the recording proxy refuses every request before it could reach Stripe.
const (
	testStripeKey      = "sk_test_boot_harness_no_such_account"
	testPriceMap       = `[{"price":"price_boot_harness_business_lite_month","plan":"business_lite","interval":"month"}]`
	testGrandfather    = "2026-01-01T00:00:00Z" // before every workspace a boot creates, so the tiers apply to them
	badGrandfather     = "2026-01-01"           // a date without a time: not RFC 3339
	testBuildSHA       = "0123456789abcdef0123456789abcdef01234567"
	testLimits         = `{"max_projects":7}`
	registrationClosed = "closed"
	openedEmail        = "boot-harness-before-closing@example.com" // the account signUpOnOpenServer makes
)

// billingOn is the variables that turn billing on (docs/plans/billing-stripe.md).
func billingOn(env map[string]string) map[string]string {
	out := map[string]string{"STRIPE_SECRET_KEY": testStripeKey, "OPENV_STRIPE_PRICES": testPriceMap}
	for k, v := range env {
		out[k] = v
	}
	return out
}

// billingReconcileMessages are what the reconcile billing.Start runs at once
// logs when the provider does not answer: it lists disputes, then
// subscriptions, then confirms the one registered price, and each call gives
// up after its retries.
var billingReconcileMessages = []string{
	`msg="could not list disputes; none marked this tick"`,
	`msg="could not list subscriptions; entitlements left as they were"`,
	`msg="prices not confirmed; the pricing page keeps its last reading"`,
}

// s4bProfiles are TestBootProfiles' boots. Each sets proxy and runs S4b's
// probes (s4b).
var s4bProfiles = s4b([]bootProfile{
	{name: "secure_cookies", about: "the deployment is reached only over TLS",
		env: map[string]string{"SECURE_COOKIES": "true"}},
	{name: "cross_site_cookies", about: "the frontend is on another site than the API",
		env: map[string]string{"CROSS_SITE_COOKIES": "true"}},
	{name: "self_hosted", about: "a deployment somebody runs themselves",
		env: map[string]string{"OPENV_SELF_HOSTED": "true"}},
	{name: "tiers_on", about: "the plan tiers in force for workspaces created after the grandfather date",
		env: map[string]string{"OPENV_BILLING_GRANDFATHER_BEFORE": testGrandfather}},
	{name: "registration_closed", about: "self-service registration closed",
		env:    map[string]string{"OPENV_REGISTRATION": registrationClosed},
		signUp: signUpOnOpenServer},
	{name: "limits", about: "a deployment-wide limit override",
		env: map[string]string{"OPENV_LIMITS": testLimits}},
	{name: "build_sha", about: "the commit the binary was built from",
		env: map[string]string{"OPENV_BUILD_SHA": testBuildSHA}},
	{name: "billing", about: "billing on: a Stripe test key and a one-entry price map, the provider unreachable",
		env: billingOn(nil), async: billingReconcileMessages},
	{name: "self_hosted_billing", about: "billing configured on a self-hosted deployment",
		env: billingOn(map[string]string{"OPENV_SELF_HOSTED": "true"})},
	{name: "self_hosted_bad_grandfather", about: "a malformed grandfather date on a self-hosted deployment, which does not read it",
		env: map[string]string{"OPENV_SELF_HOSTED": "true", "OPENV_BILLING_GRANDFATHER_BEFORE": badGrandfather}},
	{name: "malformed_settings", about: "settings that break the rule, each kept at its default with one warning",
		env: malformedSettings},
})

// malformedSettings break internal/envparse's rule, one of each kind the
// server reads at boot: a boolean (SECURE_COOKIES, read twice, and
// OPENV_BUDGET_ENFORCE), a count (OPENV_SHARED_PRODUCT_DAILY_LIMIT, the body
// cap, a run's attempts, 0 among them) and a rate (a refill of Inf, which
// used to switch sign-in throttling off); and the settings a request reads,
// the proxy trust (its hop count and its boolean) and the upload and
// evidence caps, which NewHandler reads once as well, so that the boot log
// names them, not the first request to read one (the register probe, for
// the proxy trust).
var malformedSettings = map[string]string{
	"SECURE_COOKIES":                   "yes",
	"OPENV_BUDGET_ENFORCE":             "on",
	"OPENV_SHARED_PRODUCT_DAILY_LIMIT": "20 a day",
	"OPENV_MAX_BODY_MB":                "32MB",
	"OPENV_RUN_MAX_ATTEMPTS":           "0",
	"OPENV_AUTH_IP_REFILL_PER_HOUR":    "Inf",
	"OPENV_TRUSTED_PROXY_HOPS":         "two",
	"OPENV_TRUST_PROXY":                "on",
	"OPENV_MAX_UPLOAD_MB":              "25 MB",
	"OPENV_MAX_EVIDENCE_MB":            "-1",
}

// s4b gives each profile the recording proxy and S4b's probes.
func s4b(profiles []bootProfile) []bootProfile {
	for i := range profiles {
		profiles[i].proxy = true
		profiles[i].extra = s4bProbes
	}
	return profiles
}

// signUpOnOpenServer is how the registration_closed profile gets the
// account S4a's later probes use, since its own server refuses to register
// one: a second server, booted from the same binary with the same variables
// but registration open, on the same database, registers an account of its
// own (openedEmail, so that a server under test that wrongly let the
// harness's address in fails on its golden, not here) and is stopped. The
// session it issued is a row of that database, so the server under test
// honours it like any account made before the operator closed registration.
// The server under test booted first, on the fresh database, so its boot log
// is a first boot's; the second server's output is not recorded.
func signUpOnOpenServer(r *probeRun) *http.Cookie {
	r.t.Helper()
	env := map[string]string{}
	for k, v := range r.env {
		if k != "OPENV_REGISTRATION" {
			env[k] = v
		}
	}
	var helper *serverProcess
	for attempt := 1; helper == nil; attempt++ {
		s, err := startServer(r.t, serverBinary(r.t), r.db, env)
		switch {
		case err == nil:
			helper = s
		case !errors.Is(err, errPortTaken) || attempt == 3:
			r.t.Fatalf("the second server, with registration open, did not come up: %v\n%s", err, s.output())
		}
	}
	body, _ := json.Marshal(map[string]string{"email": openedEmail, "password": harnessPassword, "name": "Boot Harness"})
	req, err := http.NewRequest("POST", helper.base+"/api/v1/auth/register", bytes.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := helper.client().Do(req)
	if err != nil {
		r.t.Fatalf("register on the second server: %v\n%s", err, helper.output())
	}
	answer, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "openv_session" {
			session = c
		}
	}
	if resp.StatusCode != http.StatusOK || session == nil {
		r.t.Fatalf("register on the second server, with registration open: %s %s and no session cookie\n%s",
			resp.Status, answer, helper.output())
	}
	if _, _, err := helper.terminate(30 * time.Second); err != nil {
		r.t.Fatalf("stop the second server: %v\n%s", err, helper.output())
	}
	r.line("the account and its session come from a second server, booted on the same database with registration open, then stopped")
	return session
}

// billingOrderBody is the plan the checkout and plan-change probes ask for:
// the one the price map sells.
const billingOrderBody = `{"plan":"business_lite","interval":"month"}`

// s4bProbes follow S4a's probes in every S4b profile: the account's
// workspace, its effective limits (what the self-hosted, tiers and
// OPENV_LIMITS settings change), then every billing route and the billing
// provider calls /metrics counted. The billing answers are recorded whole,
// normalised, whatever their length, since the workspace id in them would
// change a digest from run to run.
var s4bProbes = []probe{
	{name: "workspace", title: "GET /api/v1/orgs (the account's active workspace, which the probes below use)", run: func(r *probeRun) {
		resp, data := r.send(r.withSession(r.request("GET", "/api/v1/orgs", nil)))
		r.recordHeadAs(resp, false)
		r.line("body not recorded (the account's workspaces; S5c pins them)")
		var list struct {
			ActiveOrg string `json:"active_org"`
		}
		if err := json.Unmarshal(data, &list); err != nil || list.ActiveOrg == "" {
			r.t.Fatalf("listing the account's workspaces gave no active one: %s %s\n%s", resp.Status, data, r.s.output())
		}
		r.org = list.ActiveOrg
	}},
	{name: "workspace-limits", title: "GET /api/v1/orgs/{id}/limits: the effective values, not the copy", run: func(r *probeRun) {
		resp, data := r.send(r.withSession(r.request("GET", "/api/v1/orgs/"+r.org+"/limits", nil)))
		// Only the status: the size of the copy left out decides whether
		// the answer is chunked, and the headers are S4a's probes' to pin.
		r.line("status " + resp.Status)
		r.line("body summarised: the plan, then each limit's key and value, in the answer's order (labels and descriptions left out)")
		for _, l := range limitsSummary(r, data) {
			r.line("  " + l)
		}
	}},
	{name: "billing-plans", title: "GET /api/v1/public/plans with no session", run: func(r *probeRun) {
		r.recordWhole(r.send(r.request("GET", "/api/v1/public/plans", nil)))
	}},
	{name: "billing-read", title: "GET /api/v1/orgs/{id}/billing", run: func(r *probeRun) {
		r.recordWhole(r.send(r.withSession(r.request("GET", "/api/v1/orgs/"+r.org+"/billing", nil))))
	}},
	{name: "billing-refresh", title: "POST /api/v1/orgs/{id}/billing/refresh with no body", run: func(r *probeRun) {
		r.recordWhole(r.send(r.withSession(r.request("POST", "/api/v1/orgs/"+r.org+"/billing/refresh", nil))))
	}},
	{name: "billing-checkout", title: "POST /api/v1/orgs/{id}/billing/checkout " + billingOrderBody, run: func(r *probeRun) {
		r.billingWrite("checkout", billingOrderBody)
	}},
	{name: "billing-change", title: "POST /api/v1/orgs/{id}/billing/change " + billingOrderBody, run: func(r *probeRun) {
		r.billingWrite("change", billingOrderBody)
	}},
	{name: "billing-portal", title: "POST /api/v1/orgs/{id}/billing/portal", run: func(r *probeRun) {
		r.billingWrite("portal", "")
	}},
	{name: "billing-metrics", title: "GET /metrics: the billing provider calls the server counted", run: func(r *probeRun) {
		resp, body := r.send(r.request("GET", "/metrics", nil))
		r.line("status " + resp.Status)
		r.line("billing_provider_requests_total (operation, HTTP status; 0 is a transport failure), with counts:")
		series := billingRequestSeries(body)
		if len(series) == 0 {
			r.line("  (no series: the server made no call to a billing provider)")
		}
		for _, s := range series {
			r.line("  " + s)
		}
	}},
}

// limitsSummary reads a GET /orgs/{id}/limits answer down to its values.
func limitsSummary(r *probeRun, data []byte) []string {
	var body struct {
		Plan          string   `json:"plan"`
		EntitledPlan  string   `json:"entitled_plan"`
		PlanStatus    string   `json:"plan_status"`
		Grandfathered bool     `json:"grandfathered"`
		SelfHosted    bool     `json:"self_hosted"`
		ReadOnly      bool     `json:"read_only"`
		OverPlan      []string `json:"over_plan"`
		Limits        []struct {
			Key       string `json:"key"`
			Limit     int    `json:"limit"`
			Unlimited bool   `json:"unlimited"`
			Used      *int   `json:"used"`
			Fixed     bool   `json:"fixed"`
			Included  *bool  `json:"included"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		r.t.Fatalf("the workspace limits are not the JSON expected: %v\n%s", err, data)
	}
	out := []string{fmt.Sprintf("plan %s, entitled %s, status %s, grandfathered %t, self-hosted %t, read-only %t, over plan %v",
		body.Plan, body.EntitledPlan, body.PlanStatus, body.Grandfathered, body.SelfHosted, body.ReadOnly, body.OverPlan)}
	for _, l := range body.Limits {
		var v string
		switch {
		case l.Included != nil && *l.Included:
			v = "included"
		case l.Included != nil:
			v = "not included"
		case l.Unlimited:
			v = "unlimited"
		default:
			v = strconv.Itoa(l.Limit)
		}
		if l.Fixed {
			v += " (fixed)"
		}
		if l.Used != nil {
			v += fmt.Sprintf(", used %d", *l.Used)
		}
		out = append(out, l.Key+": "+v)
	}
	return out
}

// billingWrite posts to one of the workspace's billing write routes.
func (r *probeRun) billingWrite(route, body string) {
	req := r.request("POST", "/api/v1/orgs/"+r.org+"/billing/"+route, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	r.recordWhole(r.send(r.withSession(req)))
}

// recordWhole records an answer's head and its whole body, normalised and
// quoted, however long it is.
func (r *probeRun) recordWhole(resp *http.Response, body []byte) {
	r.recordHead(resp)
	if len(body) == 0 {
		r.line("body empty")
		return
	}
	r.line("body " + strconv.Quote(r.s.normaliseText(string(body))))
}

// billingRequestSeries lists the billing_provider_requests_total samples of
// a Prometheus text page, sorted, with their values.
func billingRequestSeries(body []byte) []string {
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "billing_provider_requests_total{") {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// TestBootGoldensAreClaimed checks, with no database, that every golden
// under testdata/boot/ is written by exactly one boot and that every boot
// has one, so no golden goes unchecked and CI's check that each golden's
// subtest passed (ci.yml, "Boot harness ran") covers every boot.
func TestBootGoldensAreClaimed(t *testing.T) {
	owner := map[string]string{}
	claim := func(name, test string) {
		if prev, dup := owner[name]; dup {
			t.Errorf("boot %s is named twice (%s and %s): its golden would be written twice; rename one in "+
				"bootProfiles (boot_smoke_test.go), s4bProfiles (boot_profiles_test.go) or misconfiguredBoots "+
				"(boot_misconfigured_test.go)", name, prev, test)
		}
		owner[name] = test
	}
	for _, p := range bootProfiles {
		claim(p.name, "TestBootSmoke")
	}
	for _, p := range s4bProfiles {
		claim(p.name, "TestBootProfiles")
	}
	for _, b := range misconfiguredBoots {
		claim(b.name, "TestBootMisconfigured")
	}
	files, err := filepath.Glob(filepath.Join("testdata", "boot", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".txt")
		onDisk[name] = true
		if _, ok := owner[name]; !ok {
			t.Errorf("cmd/server/testdata/boot/%s.txt is written by no boot: delete it with the boot that wrote it, "+
				"or add its boot back to bootProfiles, s4bProfiles or misconfiguredBoots", name)
		}
	}
	names := make([]string, 0, len(owner))
	for name := range owner {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !onDisk[name] {
			t.Errorf("boot %s (%s) has no golden cmd/server/testdata/boot/%s.txt; create it with:\n  %s=<server URL> %s=1 go test ./cmd/server -count=1 -run '^%s$'",
				name, owner[name], name, testDatabaseURLEnv, updateGoldenEnv, owner[name])
		}
	}
}
