package config

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/hosting"
	"github.com/openv/requirements-platform/internal/notify"
)

// These tests prove the oracles of oracle_test.go the same as the helpers
// they copy, while those helpers exist (refactor step X10b takes them out of
// production code, and these tests with them): each runs the production
// helper, its twin, and then the oracle over the same environment, and holds
// them to the same result and the same log. A warning logged once per
// variable and value is left out of that comparison, since internal/envparse
// keeps one record of them for the whole process, shared by both;
// twinWarnings shows each such warning is the same by setting the twin and
// the oracle values of their own.

// twin runs the helper, then the oracle, each with its log captured, the
// once-per-value warnings left out.
func twin[T any](t *testing.T, helper, oracle func() T) (want, got T, wantLog, gotLog string) {
	t.Helper()
	buf := captureLog(t)
	want = helper()
	wantLog = withoutOncePerValue(buf.String())
	buf.Reset()
	got = oracle()
	gotLog = withoutOncePerValue(buf.String())
	return want, got, wantLog, gotLog
}

// twinRuns numbers the runs of twinWarnings within one test binary, so that
// each run (go test -count=2) sets values no earlier one set.
var twinRuns atomic.Int64

// twinWarnings sets name to value("twin<n>") and runs helper, then to
// value("oracle<n>") and runs oracle, and holds the two to the same
// warnings, the once-per-value ones included: a warning names the variable,
// never the value, so two values of the same kind log the same lines.
func twinWarnings(t *testing.T, name string, value func(side string) string, helper, oracle func()) {
	t.Helper()
	run := twinRuns.Add(1)
	buf := captureLog(t)
	t.Setenv(name, value(fmt.Sprintf("twin%d", run)))
	helper()
	wantLog := warnings(buf.String())
	buf.Reset()
	t.Setenv(name, value(fmt.Sprintf("oracle%d", run)))
	oracle()
	if gotLog := warnings(buf.String()); gotLog != wantLog || wantLog == "" {
		t.Errorf("%s: the oracle logged\n%s\nthe helper\n%s\nwant the same warning", name, gotLog, wantLog)
	}
}

// warnings are the warning lines of log.
func warnings(log string) string {
	var out []string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "level=WARN") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func TestOracleSessionPolicyIsUsers(t *testing.T) {
	for _, name := range []string{"OPENV_SESSION_MAX_AGE", "OPENV_SESSION_IDLE"} {
		for _, in := range inputs("12h", "45m", "0s", "1ns", " 1h30m ", "167h59m", "168h", "168h0m1s", "169h", "719h", "720h", "720h0m1s", "721h") {
			setEnv(t, "OPENV_SESSION_MAX_AGE", true, "")
			setEnv(t, "OPENV_SESSION_IDLE", true, "")
			setEnv(t, name, in.unset, in.input)
			want, got, wantLog, gotLog := twin(t, users.SessionPolicyFromEnv, oracleSessionPolicyFromEnv)
			if got != want || gotLog != wantLog {
				t.Errorf("%s=%s: the oracle = %+v, logging\n%s\nusers.SessionPolicyFromEnv() = %+v, logging\n%s", name, in.shown, got, gotLog, want, wantLog)
			}
		}
		twinWarnings(t, name, func(side string) string { return "30d-" + side },
			func() { users.SessionPolicyFromEnv() }, func() { oracleSessionPolicyFromEnv() })
		// Above the ceiling, by minutes that differ between the sides.
		twinWarnings(t, name, func(side string) string { return fmt.Sprintf("%dh%dm", 400000+twinRuns.Load(), len(side)) },
			func() { users.SessionPolicyFromEnv() }, func() { oracleSessionPolicyFromEnv() })
	}
}

func TestOracleRegistrationIsAPIs(t *testing.T) {
	for _, in := range inputs("open", "closed", " Closed ", "CLOSED", "OPEN", "invite", "closed\n") {
		setEnv(t, "OPENV_REGISTRATION", in.unset, in.input)
		want, got, wantLog, gotLog := twin(t, api.RegistrationPolicyFromEnv, oracleRegistrationPolicyFromEnv)
		if got != want || gotLog != wantLog {
			t.Errorf("OPENV_REGISTRATION=%s: the oracle = %q, logging\n%s\napi.RegistrationPolicyFromEnv() = %q, logging\n%s", in.shown, got, gotLog, want, wantLog)
		}
	}
}

func TestOracleVerificationPolicyIsNotifys(t *testing.T) {
	mailers := map[string]notify.Mailer{"nil": nil, "a nil *SMTPMailer": (*notify.SMTPMailer)(nil),
		"SMTP on": enabledMailer{}, "SMTP off": &notify.SMTPMailer{}}
	for mname, m := range mailers {
		for _, in := range inputs("off", " OFF ", "Off", "on", "no") {
			setEnv(t, "OPENV_EMAIL_VERIFICATION", in.unset, in.input)
			want, got, wantLog, gotLog := twin(t,
				func() users.EmailVerificationPolicy { return notify.VerificationPolicyFromEnv(m) },
				func() users.EmailVerificationPolicy { return oracleVerificationPolicyFromEnv(m) })
			if got != want || gotLog != wantLog {
				t.Errorf("%s, OPENV_EMAIL_VERIFICATION=%s: the oracle = %+v, logging\n%s\nnotify.VerificationPolicyFromEnv() = %+v, logging\n%s",
					mname, in.shown, got, gotLog, want, wantLog)
			}
		}
	}
	twinWarnings(t, "OPENV_EMAIL_VERIFICATION", func(side string) string { return "nope-" + side },
		func() { notify.VerificationPolicyFromEnv(enabledMailer{}) }, func() { oracleVerificationPolicyFromEnv(enabledMailer{}) })
}

func TestOracleVAPIDIsNotifys(t *testing.T) {
	publics := []parseRow{{unset: true}, {input: ""}, {input: "pub"}, {input: " pub "}}
	privates := []parseRow{{unset: true}, {input: ""}, {input: "   "}, {input: "priv"}, {input: " priv\n"}}
	subjects := []parseRow{{unset: true}, {input: "mailto:ops@example.test"}, {input: " https://example.test "},
		{input: "http://example.test"}, {input: "ops@example.test"}, {input: "   "}}
	for _, pub := range publics {
		for _, priv := range privates {
			for _, sub := range subjects {
				setEnv(t, "OPENV_VAPID_PUBLIC_KEY", pub.unset, pub.input)
				setEnv(t, "OPENV_VAPID_PRIVATE_KEY", priv.unset, priv.input)
				setEnv(t, "OPENV_VAPID_SUBJECT", sub.unset, sub.input)
				want, got, wantLog, gotLog := twin(t, notify.VAPIDFromEnv, oracleVAPIDFromEnv)
				if got != want || gotLog != wantLog {
					t.Errorf("public %+v, private %+v, subject %+v: the oracle = %+v, logging\n%s\nnotify.VAPIDFromEnv() = %+v, logging\n%s",
						pub, priv, sub, got, gotLog, want, wantLog)
				}
			}
		}
	}
	t.Setenv("OPENV_VAPID_PUBLIC_KEY", "pub")
	t.Setenv("OPENV_VAPID_SUBJECT", "mailto:ops@example.test")
	twinWarnings(t, "OPENV_VAPID_PRIVATE_KEY", func(side string) string { return " key-" + side + "\n" },
		func() { notify.VAPIDFromEnv() }, func() { oracleVAPIDFromEnv() })
}

func TestOracleNotificationTypesAreNotifys(t *testing.T) {
	quietLog(t)
	for _, in := range inputs(" a , b ", "b,a,a", "A", "run_finished", "run_finished, review_requested,,") {
		setEnv(t, "OPENV_EMAIL_NOTIFICATION_TYPES", in.unset, in.input)
		setEnv(t, "OPENV_PUSH_NOTIFICATION_TYPES", in.unset, in.input)
		if got, want := oracleEmailTypesFromEnv(), notify.EmailTypesFromEnv(); !reflect.DeepEqual(got, want) {
			t.Errorf("OPENV_EMAIL_NOTIFICATION_TYPES=%s: the oracle = %q, notify.EmailTypesFromEnv() = %q", in.shown, got, want)
		}
		if got, want := oraclePushTypesFromEnv(), notify.PushTypesFromEnv(); !reflect.DeepEqual(got, want) {
			t.Errorf("OPENV_PUSH_NOTIFICATION_TYPES=%s: the oracle = %q, notify.PushTypesFromEnv() = %q", in.shown, got, want)
		}
	}
}

// mailerHolds is what notify.MailerFromEnv put in its mailer: its fields are
// unexported, so the test reads them by reflection.
func mailerHolds(m *notify.SMTPMailer) SMTP {
	v := reflect.ValueOf(m).Elem()
	return SMTP{Host: v.FieldByName("host").String(), Port: v.FieldByName("port").String(), User: v.FieldByName("user").String(),
		Password: v.FieldByName("pass").String(), From: v.FieldByName("from").String()}
}

func TestOracleMailerIsNotifys(t *testing.T) {
	quietLog(t)
	vars := []string{"OPENV_SMTP_HOST", "OPENV_SMTP_PORT", "OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD", "OPENV_SMTP_FROM"}
	check := func(what string) {
		t.Helper()
		if got, want := oracleMailerFromEnv(), mailerHolds(notify.MailerFromEnv()); got != want {
			t.Errorf("%s: the oracle = %+v, notify.MailerFromEnv() holds %+v", what, got, want)
		}
	}
	for _, name := range vars {
		for _, in := range inputs("smtp.example.test", " key\n", "ops@example.test") {
			for _, v := range vars {
				setEnv(t, v, true, "")
			}
			setEnv(t, name, in.unset, in.input)
			check(name + "=" + in.shown)
		}
	}
	t.Setenv("OPENV_SMTP_USER", " mailer@example.test")
	for _, from := range []string{"", "   ", "noreply@example.test"} {
		t.Setenv("OPENV_SMTP_FROM", from)
		check("OPENV_SMTP_FROM=" + from + " with a user")
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
	for _, name := range []string{"OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD"} {
		// MailerFromEnv logs its line about the mailer after its reads,
		// which the oracle leaves to notify, so the warnings are compared.
		twinWarnings(t, name, func(side string) string { return " cred-" + side },
			func() { notify.MailerFromEnv() }, func() { oracleMailerFromEnv() })
	}
}

func TestOracleProviderIsEmbeddings(t *testing.T) {
	quietLog(t)
	vars := []string{"OPENV_EMBEDDING_BASE_URL", "OPENV_EMBEDDING_MODEL"}
	for _, name := range vars {
		for _, in := range inputs(" https://embed.example.test/v1// ", "https://embed.example.test/v1", "/") {
			for _, v := range vars {
				setEnv(t, v, true, "")
			}
			setEnv(t, name, in.unset, in.input)
			for _, key := range []string{"", "sk-test", " sk-test\n"} {
				p := embeddings.ProviderFromEnv(key)
				v := reflect.ValueOf(p).Elem()
				want := Embeddings{APIKey: v.FieldByName("apiKey").String(), BaseURL: v.FieldByName("baseURL").String(), Model: p.Model()}
				if got := oracleProviderFromEnv(key); got != want {
					t.Errorf("%s=%s, key %q: the oracle = %+v, embeddings.ProviderFromEnv holds %+v", name, in.shown, key, got, want)
				}
			}
		}
	}
}

func TestOraclePidsLimitIsHostings(t *testing.T) {
	quietLog(t)
	for _, in := range inputs("64", " 512 ", "-1", "banana", "2k", "512.5", "99999999999999999999") {
		setEnv(t, "HOSTED_RUNNER_PIDS_LIMIT", in.unset, in.input)
		if got, want := oraclePidsLimit(), hosting.PidsLimit(); got != want {
			t.Errorf("HOSTED_RUNNER_PIDS_LIMIT=%s: the oracle = %d, hosting.PidsLimit() = %d", in.shown, got, want)
		}
	}
	twinWarnings(t, "HOSTED_RUNNER_PIDS_LIMIT", func(side string) string { return "lots-" + side },
		func() { hosting.PidsLimit() }, func() { oraclePidsLimit() })
}

// TestOracleServerEnvSecretIsSecret holds cmd/server's envSecret, whose copy
// the oracle is (cmd/server is a main package, which no test can import), to
// this package's secret, the accessor helper word for word the same, over
// S8's inputs.
func TestOracleServerEnvSecretIsSecret(t *testing.T) {
	quietLog(t)
	const unset = envParseProbe + "_UNSET"
	for _, in := range inputs(" key", "key\n") {
		setEnv(t, envParseProbe, in.unset, in.input)
		if got, want := oracleServerEnvSecret(envParseProbe, "fallback"), probed(unset).secret(envParseProbe, "fallback"); got != want {
			t.Errorf("%s=%s: the oracle = %q, secret = %q", envParseProbe, in.shown, got, want)
		}
	}
}
