package config

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/envparse"
	"github.com/openv/requirements-platform/internal/notify"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The dependent pairs of refactor plan X10 (a setting read, or fatal, only
// under another's value), and the parses cmd/server writes inline after a
// read, which no package exports for helpers_test.go to compare with: each
// expectation below is what today's statement in cmd/server does, read from
// the code at the base of X10a.

// TestGrandfatherDateOnlyOffSelfHosted pairs OPENV_SELF_HOSTED with
// OPENV_BILLING_GRANDFATHER_BEFORE (stage workspace, wire_workspace.go): the
// date is trimmed and parsed, fatally, only when it is set and the install is
// not self-hosted, so a malformed date on a self-hosted install still boots.
func TestGrandfatherDateOnlyOffSelfHosted(t *testing.T) {
	quietLog(t)
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	type want struct {
		on    bool
		fatal bool
	}
	dates := map[string]want{
		"\x00unset":                 {false, false},
		"":                          {false, false},
		"   ":                       {false, false},
		"2026-01-01T00:00:00Z":      {true, false},
		" 2026-01-01T00:00:00Z\n":   {true, false},
		"2026-01-01T01:00:00+01:00": {true, false},
		"2026-01-01":                {true, true},
		"soon":                      {true, true},
	}
	selfHosted := map[string]bool{"\x00unset": false, "false": false, "true": true, "1": true, " TRUE ": true, "yes": false}
	for sh, isSelfHosted := range selfHosted {
		for date, w := range dates {
			env := map[string]string{}
			if sh != "\x00unset" {
				env["OPENV_SELF_HOSTED"] = sh
			}
			if date != "\x00unset" {
				env["OPENV_BILLING_GRANDFATHER_BEFORE"] = date
			}
			if isSelfHosted {
				w = want{false, false}
			}
			got, on, err := loadFrom(env).GrandfatherBefore()
			if on != w.on || (err != nil) != w.fatal {
				t.Errorf("OPENV_SELF_HOSTED=%q OPENV_BILLING_GRANDFATHER_BEFORE=%q: on %v, err %v; want on %v, fatal %v", sh, date, on, err, w.on, w.fatal)
			}
			if on && err == nil && !got.Equal(cutoff) {
				t.Errorf("OPENV_BILLING_GRANDFATHER_BEFORE=%q: cutoff %v, want %v", date, got, cutoff)
			}
			if err != nil {
				_, want := time.Parse(time.RFC3339, strings.TrimSpace(date))
				if err.Error() != want.Error() {
					t.Errorf("OPENV_BILLING_GRANDFATHER_BEFORE=%q: error %q, want time.Parse's %q, which the fatal line carries", date, err, want)
				}
			}
		}
	}
}

// billingDecision is stage billing's switch (wire_http.go) over Billing()
// and SelfHosted(): fatal on the error, billing kept off with a warning on a
// self-hosted install with a key, on with a key, off without one.
func billingDecision(c *Config) string {
	cfg, err := c.Billing()
	switch {
	case err != nil:
		return "fatal"
	case cfg.Enabled() && c.SelfHosted():
		return "off, warned"
	case cfg.Enabled():
		return "on"
	}
	return "off"
}

// TestBillingConfigIsFatalWhateverTheKeyOrHosting pairs STRIPE_SECRET_KEY
// and OPENV_SELF_HOSTED with the price map and the counts: a malformed one
// is fatal with no key and on a self-hosted install too (S4's
// fatal_billing_* boots), and a self-hosted install with a key keeps billing
// off.
func TestBillingConfigIsFatalWhateverTheKeyOrHosting(t *testing.T) {
	quietLog(t)
	keys := map[string]bool{"\x00unset": false, "": false, "   ": false, "sk_test_x10a": true}
	hosts := map[string]bool{"\x00unset": false, "true": true}
	settings := map[string]bool{ // a billing setting and whether it is malformed
		"": false,
		"OPENV_STRIPE_PRICES=" + `[{"price":"price_b_m","plan":"business","interval":"month"}]`: false,
		"OPENV_STRIPE_PRICES=[]": false,
		"OPENV_STRIPE_PRICES=7":  true,
		"OPENV_STRIPE_PRICES=" + `[{"price":"price_e","plan":"enterprise","interval":"month"}]`: true,
		"OPENV_BILLING_TRIAL_DAYS=30d":        true,
		"OPENV_BILLING_TRIAL_DAYS=0":          false,
		"OPENV_BILLING_MAX_SEATS=0":           true,
		"OPENV_BILLING_RECONCILE_MINUTES=1e3": true,
	}
	for key, keyOn := range keys {
		for host, selfHosted := range hosts {
			for setting, malformed := range settings {
				env := map[string]string{}
				if key != "\x00unset" {
					env["STRIPE_SECRET_KEY"] = key
				}
				if host != "\x00unset" {
					env["OPENV_SELF_HOSTED"] = host
				}
				if name, value, ok := strings.Cut(setting, "="); ok {
					env[name] = value
				}
				want := "off"
				switch {
				case malformed:
					want = "fatal"
				case keyOn && selfHosted:
					want = "off, warned"
				case keyOn:
					want = "on"
				}
				if got := billingDecision(loadFrom(env)); got != want {
					t.Errorf("STRIPE_SECRET_KEY=%q OPENV_SELF_HOSTED=%q %s: stage billing would be %s, want %s", key, host, setting, got, want)
				}
			}
		}
	}
}

// TestSSOSettingsOnlyWithTheirSwitch pairs OPENV_OIDC_ISSUER with the other
// OIDC settings, and GOOGLE_CLIENT_ID with Google's (stage sso,
// wire_sso.go): without the switch the accessor is nil and reads nothing
// else, so a credential with spaces around it is not even warned about.
func TestSSOSettingsOnlyWithTheirSwitch(t *testing.T) {
	// Secrets no earlier run of this test (go test -count=2) set: a warning
	// comes once per variable and value for the life of the process.
	run := configWarningRuns.Add(1)
	oidcSecret, googleSecret := fmt.Sprintf(" oidc-secret-x10a-run%d\n", run), fmt.Sprintf(" google-secret-x10a-run%d\n", run)
	fields := map[string]string{
		"OPENV_OIDC_CLIENT_ID": "oidc-client", "OPENV_OIDC_CLIENT_SECRET": oidcSecret, "OPENV_OIDC_NAME": "Okta",
		"OPENV_OIDC_REDIRECT_URL": "https://api.example.test/cb", "OPENV_OIDC_SCOPES": "openid  email",
		"GOOGLE_CLIENT_SECRET": googleSecret, "PUBLIC_URL": "https://api.example.test", "FRONTEND_URL": "https://app.example.test",
		"PORT": "9090",
	}
	for _, issuer := range []string{"\x00unset", "", "   "} {
		env := map[string]string{}
		for k, v := range fields {
			env[k] = v
		}
		if issuer != "\x00unset" {
			env["OPENV_OIDC_ISSUER"] = issuer
			env["GOOGLE_CLIENT_ID"] = issuer
		}
		log := captureLog(t)
		var reads []string
		c := traced(loadFrom(env), &reads)
		if o := c.OIDC(); o != nil {
			t.Errorf("OPENV_OIDC_ISSUER=%q: OIDC() = %+v, want nil", issuer, o)
		}
		if g := c.GoogleOAuth(); g != nil {
			t.Errorf("GOOGLE_CLIENT_ID=%q: GoogleOAuth() = %+v, want nil", issuer, g)
		}
		if want := []string{"OPENV_OIDC_ISSUER", "GOOGLE_CLIENT_ID"}; !reflect.DeepEqual(reads, want) {
			t.Errorf("with no issuer and no client id, OIDC() and GoogleOAuth() read %v, want only %v", reads, want)
		}
		if log.Len() != 0 {
			t.Errorf("with no issuer and no client id, OIDC() and GoogleOAuth() logged:\n%s", log)
		}
	}
	env := map[string]string{"OPENV_OIDC_ISSUER": " https://idp.example.test ", "GOOGLE_CLIENT_ID": "google-client"}
	for k, v := range fields {
		env[k] = v
	}
	log := captureLog(t)
	c := loadFrom(env)
	wantOIDC := &OIDC{Issuer: "https://idp.example.test", ClientID: "oidc-client", ClientSecret: oidcSecret,
		RedirectURL: "https://api.example.test/cb", Scopes: []string{"openid", "email"}, ProviderName: "Okta", FrontendURL: "https://app.example.test"}
	if got := c.OIDC(); !reflect.DeepEqual(got, wantOIDC) {
		t.Errorf("OIDC() = %+v, want %+v", got, wantOIDC)
	}
	wantGoogle := &GoogleOAuth{ClientID: "google-client", ClientSecret: googleSecret,
		RedirectURL: "https://api.example.test" + googleRedirectPath, FrontendURL: "https://app.example.test"}
	if got := c.GoogleOAuth(); !reflect.DeepEqual(got, wantGoogle) {
		t.Errorf("GoogleOAuth() = %+v, want %+v", got, wantGoogle)
	}
	for _, name := range []string{"OPENV_OIDC_CLIENT_SECRET", "GOOGLE_CLIENT_SECRET"} {
		if n := strings.Count(log.String(), "var="+name); n != 1 {
			t.Errorf("with the switch on, %s, set with spaces around it, is warned about %d times, want once:\n%s", name, n, log)
		}
		if strings.Contains(log.String(), "secret-x10a") {
			t.Errorf("a warning carries the value of %s:\n%s", name, log)
		}
	}
	// With no PUBLIC_URL, the redirects fall back to this server's port.
	delete(env, "PUBLIC_URL")
	delete(env, "OPENV_OIDC_REDIRECT_URL")
	c = loadFrom(env)
	if got := c.OIDC().RedirectURL; got != "http://localhost:9090"+oidcRedirectPath {
		t.Errorf("OIDC().RedirectURL with PORT=9090 and no PUBLIC_URL = %q", got)
	}
	if got := c.GoogleOAuth().RedirectURL; got != "http://localhost:9090"+googleRedirectPath {
		t.Errorf("GoogleOAuth().RedirectURL with PORT=9090 and no PUBLIC_URL = %q", got)
	}
}

// TestOIDCScopes pins OPENV_OIDC_SCOPES (wire_sso.go): read untrimmed, nil
// when unset or empty, otherwise split on spaces, so a value of only
// spaces is an empty, non-nil list.
func TestOIDCScopes(t *testing.T) {
	cases := map[string][]string{
		"\x00unset":                 nil,
		"":                          nil,
		"   ":                       {},
		"openid":                    {"openid"},
		" openid  email\tprofile\n": {"openid", "email", "profile"},
	}
	for in, want := range cases {
		env := map[string]string{"OPENV_OIDC_ISSUER": "https://idp.example.test"}
		if in != "\x00unset" {
			env["OPENV_OIDC_SCOPES"] = in
		}
		got := loadFrom(env).OIDC().Scopes
		if !reflect.DeepEqual(got, want) || (got == nil) != (want == nil) {
			t.Errorf("OPENV_OIDC_SCOPES=%q: Scopes = %#v, want %#v", in, got, want)
		}
	}
}

// TestLogLevel pins OPENV_LOG_LEVEL as initLogging reads it (logging.go),
// which no test pinned before: trimmed and lower-cased, and an unknown value
// keeps info and is reported, for the warning the caller logs, which names
// the variable, never the value (#379, bug 222), so LogLevel does not hand
// the value back.
func TestLogLevel(t *testing.T) {
	cases := map[string]struct {
		level   slog.Level
		unknown bool
	}{
		"\x00unset": {slog.LevelInfo, false},
		"":          {slog.LevelInfo, false},
		"   ":       {slog.LevelInfo, false},
		"info":      {slog.LevelInfo, false},
		" INFO ":    {slog.LevelInfo, false},
		"debug":     {slog.LevelDebug, false},
		"Debug\n":   {slog.LevelDebug, false},
		"warn":      {slog.LevelWarn, false},
		"warning":   {slog.LevelWarn, false},
		"WARNING":   {slog.LevelWarn, false},
		"error":     {slog.LevelError, false},
		"verbose":   {slog.LevelInfo, true},
		" trace ":   {slog.LevelInfo, true},
		"1":         {slog.LevelInfo, true},
	}
	for in, want := range cases {
		env := map[string]string{}
		if in != "\x00unset" {
			env["OPENV_LOG_LEVEL"] = in
		}
		log := captureLog(t)
		level, unknown := loadFrom(env).LogLevel()
		if level != want.level || unknown != want.unknown {
			t.Errorf("OPENV_LOG_LEVEL=%q: LogLevel() = %v, %v; want %v, %v", in, level, unknown, want.level, want.unknown)
		}
		if log.Len() != 0 {
			t.Errorf("OPENV_LOG_LEVEL=%q: LogLevel() logged, before the logger is installed:\n%s", in, log)
		}
	}
}

// TestDefaultPlan pins OPENV_PLAN_DEFAULT with OPENV_SELF_HOSTED (stage
// config): a plan's name wins; anything else keeps the deployment's own
// default and logs one warning naming the variable, never the value.
func TestDefaultPlan(t *testing.T) {
	cases := []struct {
		selfHosted, named string
		want              string
		warns             bool
	}{
		{"", "", "single", false},
		{"true", "", "self_host", false},
		{"", " business ", "business", false},
		{"true", "enterprise", "enterprise", false},
		{"", "open_source", "open_source", false},
		{"", "team", "team", false},
		{"", "platinum-x10a", "single", true},
		{"true", "Business", "self_host", true},
	}
	for _, tc := range cases {
		log := captureLog(t)
		got := loadFrom(map[string]string{"OPENV_SELF_HOSTED": tc.selfHosted, "OPENV_PLAN_DEFAULT": tc.named}).DefaultPlan()
		if got != tc.want {
			t.Errorf("OPENV_SELF_HOSTED=%q OPENV_PLAN_DEFAULT=%q: DefaultPlan() = %q, want %q", tc.selfHosted, tc.named, got, tc.want)
		}
		warned := strings.Contains(log.String(), `msg="ignoring an unknown plan; the deployment's default plan applies" var=OPENV_PLAN_DEFAULT want="one of `)
		if warned != tc.warns || (tc.named != "" && strings.Contains(log.String(), strings.TrimSpace(tc.named)) && tc.warns) {
			t.Errorf("OPENV_PLAN_DEFAULT=%q: warned %v, want %v, never with the value:\n%s", tc.named, warned, tc.warns, log)
		}
	}
}

// TestDatabase pins stage config's database settings: DATABASE_URL, exactly
// as set, wins and no DB_* setting is read; otherwise the DB_* parts, which
// postgres.ConnString quotes.
func TestDatabase(t *testing.T) {
	var reads []string
	c := traced(loadFrom(map[string]string{"DATABASE_URL": "postgres://u:p@db/x ", "DB_PASSWORD": " not read"}), &reads)
	if got := c.DatabaseURL(); got != "postgres://u:p@db/x " {
		t.Errorf("DatabaseURL() = %q, want it exactly as set", got)
	}
	if !reflect.DeepEqual(reads, []string{"DATABASE_URL"}) {
		t.Errorf("DatabaseURL() read %v", reads)
	}
	c = loadFrom(map[string]string{"DB_HOST": " db ", "DB_PASSWORD": " p'w\n"})
	p := c.DatabaseParts()
	if got, want := postgres.ConnString(p.Host, p.Port, p.User, p.Password, p.Name),
		`host='db' port='5432' user='postgres' password=' p\'w`+"\n"+`' dbname='openv' sslmode=disable`; got != want {
		t.Errorf("ConnString(DatabaseParts()) = %q, want %q", got, want)
	}
}

// TestHostedRunnersOff pins hosting.NewProvisioner's switch: HOSTED_RUNNERS,
// trimmed, off in any case. TestFixedWarningsNameTheVariable checks the
// warning any other value gives.
func TestHostedRunnersOff(t *testing.T) {
	quietLog(t)
	for in, want := range map[string]bool{"\x00unset": false, "": false, "off": true, " OFF ": true, "Off": true, "on": false, "0": false, "false": false} {
		env := map[string]string{}
		if in != "\x00unset" {
			env["HOSTED_RUNNERS"] = in
		}
		if got := loadFrom(env).HostedRunnersOff(); got != want {
			t.Errorf("HOSTED_RUNNERS=%q: HostedRunnersOff() = %v, want %v", in, got, want)
		}
	}
}

// TestUploadsDirsStayApart pins Q12's two readings of UPLOADS_DIR: the
// server's, with its default, and the reports', trimmed with none.
func TestUploadsDirsStayApart(t *testing.T) {
	for in, want := range map[string][2]string{
		"\x00unset": {"./uploads", ""},
		"":          {"./uploads", ""},
		"   ":       {"./uploads", ""},
		" /data/u ": {"/data/u", "/data/u"},
	} {
		env := map[string]string{}
		if in != "\x00unset" {
			env["UPLOADS_DIR"] = in
		}
		c := loadFrom(env)
		if got := [2]string{c.UploadsDir(), c.UploadsDirRaw()}; got != want {
			t.Errorf("UPLOADS_DIR=%q: UploadsDir(), UploadsDirRaw() = %q, want %q", in, got, want)
		}
	}
}

// TestCORSOriginStaysAPIsCheck pins that the CORS_ORIGIN check stays
// api.CORSMiddleware's, fed by the accessor: a wildcard or null is refused
// (the boot's "invalid CORS_ORIGIN" exit), and a blank value is the default.
func TestCORSOriginStaysAPIsCheck(t *testing.T) {
	for in, refused := range map[string]bool{"\x00unset": false, "   ": false, "*": true, " * ": true, "null": true, "https://app.example.test": false} {
		env := map[string]string{}
		if in != "\x00unset" {
			env["CORS_ORIGIN"] = in
		}
		_, err := api.CORSMiddleware(loadFrom(env).CORSOrigin(), http.NotFoundHandler())
		if (err != nil) != refused {
			t.Errorf("CORS_ORIGIN=%q: api.CORSMiddleware's error %v, want refused %v", in, err, refused)
		}
	}
}

// TestMaxBodyBytes pins maxRequestBodyBytes (http.go): OPENV_MAX_BODY_MB
// mebibytes, 32 by default, and the default for a size whose bytes do not
// fit in an int64 (#379, bug 224), which wrapped round.
func TestMaxBodyBytes(t *testing.T) {
	quietLog(t)
	for in, want := range map[string]int64{"\x00unset": 32 << 20, "64": 64 << 20, " 1 ": 1 << 20, "32MB": 32 << 20, "0": 32 << 20,
		"8796093022207": envparse.MaxMebibytes << 20, "8796093022208": 32 << 20, "9223372036854775807": 32 << 20} {
		env := map[string]string{}
		if in != "\x00unset" {
			env["OPENV_MAX_BODY_MB"] = in
		}
		if got := loadFrom(env).MaxBodyBytes(); got != want {
			t.Errorf("OPENV_MAX_BODY_MB=%q: MaxBodyBytes() = %d, want %d", in, got, want)
		}
	}
}

// TestEnvparseWarnsOncePerValue pins that an accessor warns as today's getter
// does, through internal/envparse: once per variable and value, naming the
// variable and never the value, however often the setting is read (stage
// config and stage server both read SECURE_COOKIES, for one).
func TestEnvparseWarnsOncePerValue(t *testing.T) {
	// Values no earlier run of this test (go test -count=2) set: a warning
	// comes once per variable and value for the life of the process.
	run := configWarningRuns.Add(1)
	log := captureLog(t)
	c := loadFrom(map[string]string{"SECURE_COOKIES": fmt.Sprintf("yes-x10a-run%d", run),
		"OPENV_RUN_MAX_ATTEMPTS": fmt.Sprintf("zero-x10a-run%d", run), "WORKER_API_KEY": fmt.Sprintf(" key-x10a-run%d", run)})
	for i := 0; i < 3; i++ {
		c.SecureCookies()
		c.RunMaxAttempts()
		c.WorkerAPIKey()
	}
	for _, name := range []string{"SECURE_COOKIES", "OPENV_RUN_MAX_ATTEMPTS", "WORKER_API_KEY"} {
		if n := strings.Count(log.String(), "var="+name); n != 1 {
			t.Errorf("%s malformed and read three times: warned %d times, want once:\n%s", name, n, log)
		}
	}
	// A different value of the same variable is a new pair, and warns.
	loadFrom(map[string]string{"SECURE_COOKIES": fmt.Sprintf("no-x10a-run%d", run)}).SecureCookies()
	if n := strings.Count(log.String(), "var=SECURE_COOKIES"); n != 2 {
		t.Errorf("SECURE_COOKIES read with a second malformed value: warned %d times in all, want twice:\n%s", n, log)
	}
	if strings.Contains(log.String(), "x10a") {
		t.Errorf("a warning carries a value:\n%s", log)
	}
}

// configWarningRuns numbers the runs of the tests that read a malformed or
// padded setting (TestFixedWarningsNameTheVariable among them) within one
// test binary: a warning comes once per variable and value for
// the life of the process, so each run (go test -count=2) sets values of
// its own.
var configWarningRuns atomic.Int64

// spelled is word with the case of its i-th letter flipped where bit i of n
// is set, so n = 0 is word as written.
func spelled(word string, n int64) string {
	b := []byte(word)
	for i := range b {
		if n&(1<<i) != 0 {
			b[i] ^= 'a' - 'A'
		}
	}
	return string(b)
}

// TestFixedWarningsNameTheVariable pins the warnings #379's bugs 222, 224
// and 225 fixed, as the accessors give them: each names the variable and
// never its value (a secret pasted into the wrong variable must not reach
// the log), and the session lifetime's, the body cap's and the off
// switches' come once per variable and value, however often the setting is
// read. The session lifetime's are users.SessionPolicyFromEnv's, line for
// line (its copy in oracle_test.go).
func TestFixedWarningsNameTheVariable(t *testing.T) {
	run := configWarningRuns.Add(1)
	tag := fmt.Sprintf("sk_live_x10a_run%d", run)
	log := captureLog(t)
	checkOnce := func(what, name, value string, want ...string) {
		t.Helper()
		if n := strings.Count(log.String(), "level=WARN"); n != 1 || strings.Count(log.String(), name) != 1 {
			t.Errorf("%s: want one warning naming %s, got:\n%s", what, name, log)
		}
		for _, w := range want {
			if !strings.Contains(log.String(), w) {
				t.Errorf("%s: the warning lacks %s:\n%s", what, w, log)
			}
		}
		if strings.Contains(log.String(), value) {
			t.Errorf("%s: the warning printed the value %q:\n%s", what, value, log)
		}
		log.Reset()
	}

	// Bug 222: the registration policy and the session lifetimes.
	if got := loadFrom(map[string]string{"OPENV_REGISTRATION": "invite-" + tag}).Registration(); got != registrationOpen {
		t.Errorf("OPENV_REGISTRATION=%q: Registration() = %q, want open", "invite-"+tag, got)
	}
	checkOnce("an unknown OPENV_REGISTRATION", "OPENV_REGISTRATION", "invite-"+tag, `want="open or closed"`)

	unusable, above := "30d-"+tag, fmt.Sprintf("%dh", 100000+run)
	setEnv(t, "OPENV_SESSION_MAX_AGE", false, unusable)
	setEnv(t, "OPENV_SESSION_IDLE", false, above)
	oracleSessionPolicyFromEnv()
	wantLog := log.String()
	log.Reset()
	c := Load(os.LookupEnv)
	for range 3 {
		if p := c.SessionPolicy(); p.MaxAge != users.DefaultSessionMaxAge || p.Idle != users.DefaultSessionIdle {
			t.Errorf("SessionPolicy() = %+v, want the defaults", p)
		}
	}
	gotLog := log.String()
	if again := `level=INFO msg="session lifetime" max_age=720h0m0s idle=168h0m0s` + "\n"; gotLog != wantLog+again+again {
		t.Errorf("SessionPolicy() read three times logged\n%s\nwant what users.SessionPolicyFromEnv() logged\n%s\nthen its last line twice", gotLog, wantLog)
	}
	for _, name := range []string{"OPENV_SESSION_MAX_AGE", "OPENV_SESSION_IDLE"} {
		if n := strings.Count(gotLog, "var="+name+" "); n != 1 {
			t.Errorf("SessionPolicy() read three times named %s %d times, want once:\n%s", name, n, gotLog)
		}
	}
	if strings.Count(gotLog, "level=WARN") != 2 || strings.Contains(gotLog, unusable) || strings.Contains(gotLog, above) {
		t.Errorf("SessionPolicy() read three times: want two warnings, and no value, got:\n%s", gotLog)
	}
	log.Reset()

	// Bug 224: a body cap whose bytes do not fit in an int64 (a value
	// TestMaxBodyBytes does not set).
	tooBig := strconv.FormatInt(envparse.MaxMebibytes+1000+run, 10)
	c = loadFrom(map[string]string{"OPENV_MAX_BODY_MB": tooBig})
	for range 3 {
		if got := c.MaxBodyBytes(); got != 32<<20 {
			t.Errorf("OPENV_MAX_BODY_MB=%s: MaxBodyBytes() = %d, want the default", tooBig, got)
		}
	}
	checkOnce("OPENV_MAX_BODY_MB too big for bytes", "OPENV_MAX_BODY_MB", tooBig)

	// Bug 225: an off switch given anything but off, false spelled in a
	// case of this run's (TestHostedRunnersOff sets false as written).
	value := spelled("false", run)
	c = loadFrom(map[string]string{"HOSTED_RUNNERS": value, "OPENV_EMAIL_VERIFICATION": value})
	on := &notify.SMTPMailer{}
	for range 3 {
		if c.HostedRunnersOff() {
			t.Errorf("HOSTED_RUNNERS=%q: HostedRunnersOff() = true", value)
		}
	}
	checkOnce("HOSTED_RUNNERS="+value, "HOSTED_RUNNERS", value, `want="off (any case), or unset"`)
	for range 3 {
		c.EmailVerification(on)
	}
	if n := strings.Count(log.String(), "var=OPENV_EMAIL_VERIFICATION "); n != 1 || strings.Contains(log.String(), value) ||
		!strings.Contains(log.String(), `want="off (any case), or unset"`) {
		t.Errorf("OPENV_EMAIL_VERIFICATION=%q read three times: want one warning naming it, never the value, got:\n%s", value, log)
	}
	log.Reset()
	for _, quiet := range []string{"off", " OFF ", "", "   "} {
		c = loadFrom(map[string]string{"HOSTED_RUNNERS": quiet, "OPENV_EMAIL_VERIFICATION": quiet})
		c.HostedRunnersOff()
		c.EmailVerification(on)
		if strings.Contains(log.String(), "level=WARN") {
			t.Errorf("HOSTED_RUNNERS and OPENV_EMAIL_VERIFICATION=%q warned:\n%s", quiet, log)
		}
	}
}

// TestAccessorsReadTodaysVariables pins, for each accessor, the variables it
// reads and their order, which is the order today's statement reads them in
// and so the order of their warnings in the boot log. The one read today's
// statement does not make is PORT, which the PUBLIC_URL fallbacks re-read
// where cmd/server uses the port stage config read; a text read logs nothing
// and Config never changes, so the value and the log are today's.
func TestAccessorsReadTodaysVariables(t *testing.T) {
	quietLog(t)
	set := map[string]string{
		"OPENV_BILLING_GRANDFATHER_BEFORE": "2026-01-01T00:00:00Z", "GOOGLE_CLIENT_ID": "g", "OPENV_OIDC_ISSUER": "i",
		"OPENV_REGISTRATION": "invite",
	}
	rl := []string{}
	for _, l := range []string{"INTERVIEW_MSG", "INTERVIEW_IP", "INTERVIEW_STREAM", "AUTH_IP", "AUTH_ACCOUNT", "REGISTER_IP", "SSO_IP",
		"VERIFY_RESEND", "PASSWORD_RESET", "INVITE_PREVIEW", "BILLING_REFRESH", "BILLING_WRITE", "INVITE"} {
		rl = append(rl, "OPENV_"+l+"_BURST", "OPENV_"+l+"_REFILL_PER_HOUR")
	}
	cases := []struct {
		accessor string
		call     func(*Config)
		reads    []string
	}{
		{"LogLevel", func(c *Config) { c.LogLevel() }, []string{"OPENV_LOG_LEVEL"}},
		{"DatabaseParts", func(c *Config) { c.DatabaseParts() }, []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"}},
		{"AgentsDir", func(c *Config) { c.AgentsDir() }, []string{"OPENV_DATA_DIR", "AGENTS_DIR"}},
		{"DefaultPlan", func(c *Config) { c.DefaultPlan() }, []string{"OPENV_SELF_HOSTED", "OPENV_PLAN_DEFAULT"}},
		{"DeploymentLimits", func(c *Config) { _, _ = c.DeploymentLimits() }, []string{"OPENV_LIMITS"}},
		{"Embeddings", func(c *Config) { c.Embeddings() }, []string{"OPENV_EMBEDDING_API_KEY", "OPENV_EMBEDDING_BASE_URL", "OPENV_EMBEDDING_MODEL"}},
		{"GrandfatherBefore", func(c *Config) { _, _, _ = c.GrandfatherBefore() }, []string{"OPENV_BILLING_GRANDFATHER_BEFORE", "OPENV_SELF_HOSTED"}},
		{"RunnerContainer", func(c *Config) { c.RunnerContainer() }, []string{"RUNNER_IMAGE", "RUNNER_NETWORK", "RUNNER_API_URL"}},
		{"SMTP", func(c *Config) { c.SMTP() }, []string{"OPENV_SMTP_HOST", "OPENV_SMTP_PORT", "OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD", "OPENV_SMTP_FROM"}},
		{"EmailLinkBase", func(c *Config) { c.EmailLinkBase() }, []string{"PUBLIC_URL", "FRONTEND_URL"}},
		{"SessionPolicy", func(c *Config) { c.SessionPolicy() }, []string{"OPENV_SESSION_MAX_AGE", "OPENV_SESSION_IDLE"}},
		{"Registration (unrecognised)", func(c *Config) { c.Registration() }, []string{"OPENV_REGISTRATION"}},
		{"VAPID", func(c *Config) { c.VAPID() }, []string{"OPENV_VAPID_PUBLIC_KEY", "OPENV_VAPID_PRIVATE_KEY", "OPENV_VAPID_SUBJECT"}},
		{"BuildSHA", func(c *Config) { c.BuildSHA() }, []string{"RAILWAY_GIT_COMMIT_SHA", "OPENV_BUILD_SHA"}},
		{"GoogleOAuth", func(c *Config) { c.GoogleOAuth() }, []string{"GOOGLE_CLIENT_ID", "PORT", "PUBLIC_URL", "GOOGLE_CLIENT_SECRET", "FRONTEND_URL"}},
		{"OIDC", func(c *Config) { c.OIDC() }, []string{"OPENV_OIDC_ISSUER", "PORT", "PUBLIC_URL", "OPENV_OIDC_REDIRECT_URL", "OPENV_OIDC_SCOPES",
			"OPENV_OIDC_CLIENT_ID", "OPENV_OIDC_CLIENT_SECRET", "OPENV_OIDC_NAME", "FRONTEND_URL"}},
		{"Billing", func(c *Config) { _, _ = c.Billing() }, []string{"STRIPE_SECRET_KEY", "OPENV_STRIPE_API_VERSION", "OPENV_BILLING_RETURN_URL",
			"OPENV_BILLING_PORTAL_CONFIG", "OPENV_BILLING_MAX_SEATS", "OPENV_BILLING_TRIAL_DAYS", "OPENV_STRIPE_PRICES", "OPENV_BILLING_RECONCILE_MINUTES"}},
		{"HandlerFrontendURL", func(c *Config) { c.HandlerFrontendURL() }, []string{"PUBLIC_URL", "FRONTEND_URL"}},
		{"PublicAPIURL", func(c *Config) { c.PublicAPIURL() }, []string{"PORT", "PUBLIC_URL"}},
		{"RateLimits", func(c *Config) { c.RateLimits() }, rl},
		{"MaxBodyBytes", func(c *Config) { c.MaxBodyBytes() }, []string{"OPENV_MAX_BODY_MB"}},
	}
	for _, tc := range cases {
		var reads []string
		tc.call(traced(loadFrom(set), &reads))
		if !reflect.DeepEqual(reads, tc.reads) {
			t.Errorf("%s read\n  %v\nwant today's\n  %v", tc.accessor, reads, tc.reads)
		}
	}
}
