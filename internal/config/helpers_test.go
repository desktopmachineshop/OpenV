package config

import (
	"os"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/envparse"
	"github.com/openv/requirements-platform/internal/notify"
)

// These tests hold each accessor that stands for a helper of another
// package to that helper as it was before refactor step X10b took it out of
// production code (its copy in oracle_test.go): the helper reads the process
// environment, set with t.Setenv, and the accessor a Config loaded from it,
// and the two must return the same value and, where the accessor logs what
// the helper logs, the same lines. The warnings logged once per variable and
// value are left out of that comparison (internal/envparse's and the session
// lifetime's: the second read is quiet by design); pairs_test.go checks
// them.

// inputs are S8's inputs (env_parse.txt's first column), unset first, then
// extra.
func inputs(extra ...string) []parseRow {
	rows := []parseRow{{unset: true, shown: "unset"}}
	for _, in := range append([]string{"", "   ", "TRUE", "true", "1", "0", "-5", "7", " 7 ", "+7", "7x", "1e3", "2.5",
		"Inf", "NaN", "2h", " 2h ", "-1h", "800h", "30d", "a, ,b", ","}, extra...) {
		rows = append(rows, parseRow{input: in, shown: `"` + in + `"`})
	}
	return rows
}

// both runs today's helper, then the accessor on a Config loaded from the
// same environment, each with its log captured.
func both[T any](t *testing.T, helper func() T, accessor func(*Config) T) (want, got T, wantLog, gotLog string) {
	t.Helper()
	buf := captureLog(t)
	want = helper()
	wantLog = withoutOncePerValue(buf.String())
	buf.Reset()
	got = accessor(Load(os.LookupEnv))
	gotLog = withoutOncePerValue(buf.String())
	return want, got, wantLog, gotLog
}

func TestSessionPolicyIsUsers(t *testing.T) {
	for _, name := range []string{"OPENV_SESSION_MAX_AGE", "OPENV_SESSION_IDLE"} {
		for _, in := range inputs("12h", "45m", "0s", "1ns", " 1h30m ", "167h59m", "168h", "168h0m1s", "169h", "719h", "720h", "720h0m1s", "721h") {
			setEnv(t, "OPENV_SESSION_MAX_AGE", true, "")
			setEnv(t, "OPENV_SESSION_IDLE", true, "")
			setEnv(t, name, in.unset, in.input)
			want, got, wantLog, gotLog := both(t, oracleSessionPolicyFromEnv, (*Config).SessionPolicy)
			if got != want || gotLog != wantLog {
				t.Errorf("%s=%s: SessionPolicy() = %+v, logging\n%s\nusers.SessionPolicyFromEnv() = %+v, logging\n%s", name, in.shown, got, gotLog, want, wantLog)
			}
		}
	}
}

func TestRegistrationIsAPIs(t *testing.T) {
	if registrationOpen != api.RegistrationOpen || registrationClosed != api.RegistrationClosed {
		t.Fatalf("the registration policies are %q and %q here, %q and %q in internal/api",
			registrationOpen, registrationClosed, api.RegistrationOpen, api.RegistrationClosed)
	}
	for _, in := range inputs("open", "closed", " Closed ", "CLOSED", "OPEN", "invite", "closed\n") {
		setEnv(t, "OPENV_REGISTRATION", in.unset, in.input)
		want, got, wantLog, gotLog := both(t, oracleRegistrationPolicyFromEnv, (*Config).Registration)
		if got != want || gotLog != wantLog {
			t.Errorf("OPENV_REGISTRATION=%s: Registration() = %q, logging\n%s\napi.RegistrationPolicyFromEnv() = %q, logging\n%s", in.shown, got, gotLog, want, wantLog)
		}
	}
}

func TestEmailVerificationIsNotifys(t *testing.T) {
	quietLog(t)
	on := notify.NewMailer(notify.SMTPSettings{Host: "smtp.example.test", Port: "587"})
	off := notify.NewMailer(notify.SMTPSettings{Port: "587"})
	mailers := map[string]notify.Mailer{"nil": nil, "a nil *SMTPMailer": (*notify.SMTPMailer)(nil), "SMTP on": on, "SMTP off": off}
	for mname, m := range mailers {
		for _, in := range inputs("off", " OFF ", "Off", "on", "no") {
			setEnv(t, "OPENV_EMAIL_VERIFICATION", in.unset, in.input)
			want, got, wantLog, gotLog := both(t,
				func() users.EmailVerificationPolicy { return oracleVerificationPolicyFromEnv(m) },
				func(c *Config) users.EmailVerificationPolicy { return c.EmailVerification(m) })
			if got != want || gotLog != wantLog {
				t.Errorf("%s, OPENV_EMAIL_VERIFICATION=%s: EmailVerification() = %+v, logging\n%s\nnotify.VerificationPolicyFromEnv() = %+v, logging\n%s",
					mname, in.shown, got, gotLog, want, wantLog)
			}
		}
	}
}

func TestVAPIDIsNotifys(t *testing.T) {
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
				want, got, wantLog, gotLog := both(t, oracleVAPIDFromEnv, (*Config).VAPID)
				if got != want || gotLog != wantLog {
					t.Errorf("public %+v, private %+v, subject %+v: VAPID() = %+v, logging\n%s\nnotify.VAPIDFromEnv() = %+v, logging\n%s",
						pub, priv, sub, got, gotLog, want, wantLog)
				}
			}
		}
	}
}

func TestNotificationTypesAreNotifys(t *testing.T) {
	quietLog(t)
	extra := []string{" a , b ", "b,a,a", "A", "run_finished", "run_finished, review_requested,,"}
	for _, in := range inputs(extra...) {
		setEnv(t, "OPENV_EMAIL_NOTIFICATION_TYPES", in.unset, in.input)
		setEnv(t, "OPENV_PUSH_NOTIFICATION_TYPES", in.unset, in.input)
		c := Load(os.LookupEnv)
		if got, want := c.EmailTypes(), oracleEmailTypesFromEnv(); !reflect.DeepEqual(got, want) {
			t.Errorf("OPENV_EMAIL_NOTIFICATION_TYPES=%s: EmailTypes() = %q, notify.EmailTypesFromEnv() = %q", in.shown, got, want)
		}
		if got, want := c.PushTypes(), oraclePushTypesFromEnv(); !reflect.DeepEqual(got, want) {
			t.Errorf("OPENV_PUSH_NOTIFICATION_TYPES=%s: PushTypes() = %q, notify.PushTypesFromEnv() = %q", in.shown, got, want)
		}
	}
}

func TestSMTPIsTheMailers(t *testing.T) {
	quietLog(t)
	vars := []string{"OPENV_SMTP_HOST", "OPENV_SMTP_PORT", "OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD", "OPENV_SMTP_FROM"}
	check := func(what string) {
		t.Helper()
		if got, want := Load(os.LookupEnv).SMTP(), oracleMailerFromEnv(); got != want {
			t.Errorf("%s: SMTP() = %+v, notify.MailerFromEnv() holds %+v", what, got, want)
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
	// OPENV_SMTP_FROM falls back to the user, a credential, exactly as set.
	t.Setenv("OPENV_SMTP_USER", " mailer@example.test")
	for _, from := range []string{"", "   ", "noreply@example.test"} {
		t.Setenv("OPENV_SMTP_FROM", from)
		check("OPENV_SMTP_FROM=" + from + " with a user")
	}
}

func TestEmbeddingsAreTheProviders(t *testing.T) {
	quietLog(t)
	vars := []string{"OPENV_EMBEDDING_API_KEY", "OPENV_EMBEDDING_BASE_URL", "OPENV_EMBEDDING_MODEL"}
	for _, name := range vars {
		for _, in := range inputs(" https://embed.example.test/v1// ", "https://embed.example.test/v1", "/", " key\n") {
			for _, v := range vars {
				setEnv(t, v, true, "")
			}
			setEnv(t, name, in.unset, in.input)
			// cmd/server handed the provider its key through envSecret.
			want := oracleProviderFromEnv(oracleServerEnvSecret("OPENV_EMBEDDING_API_KEY", ""))
			if got := Load(os.LookupEnv).Embeddings(); got != want {
				t.Errorf("%s=%s: Embeddings() = %+v, embeddings.ProviderFromEnv holds %+v", name, in.shown, got, want)
			}
		}
	}
}

func TestHostedRunnerPidsLimitIsHostings(t *testing.T) {
	quietLog(t)
	for _, in := range inputs("64", " 512 ", "-1", "banana", "2k", "512.5", "99999999999999999999") {
		setEnv(t, "HOSTED_RUNNER_PIDS_LIMIT", in.unset, in.input)
		if got, want := Load(os.LookupEnv).HostedRunnerPidsLimit(), oraclePidsLimit(); got != want {
			t.Errorf("HOSTED_RUNNER_PIDS_LIMIT=%s: HostedRunnerPidsLimit() = %d, hosting.PidsLimit() = %d", in.shown, got, want)
		}
	}
}

func TestBillingIsConfigFromEnv(t *testing.T) {
	quietLog(t)
	vars := []string{"STRIPE_SECRET_KEY", "OPENV_STRIPE_API_VERSION", "OPENV_BILLING_RETURN_URL", "OPENV_BILLING_PORTAL_CONFIG",
		"OPENV_BILLING_MAX_SEATS", "OPENV_BILLING_TRIAL_DAYS", "OPENV_STRIPE_PRICES", "OPENV_BILLING_RECONCILE_MINUTES"}
	for _, name := range vars {
		for _, in := range inputs(`[{"price":"price_b_m","plan":"business","interval":"month"}]`, "[]") {
			for _, v := range vars {
				setEnv(t, v, true, "")
			}
			setEnv(t, name, in.unset, in.input)
			want, wantErr := billing.ConfigFromEnv(os.Getenv)
			got, gotErr := Load(os.LookupEnv).Billing()
			if !reflect.DeepEqual(got, want) || errText(gotErr) != errText(wantErr) {
				t.Errorf("%s=%s: Billing() = %+v, %v; billing.ConfigFromEnv(os.Getenv) = %+v, %v", name, in.shown, got, gotErr, want, wantErr)
			}
		}
	}
}

func TestDeploymentLimitsAreParseLimits(t *testing.T) {
	for _, in := range inputs(`{"max_projects":7}`, ` {"max_projects":7} `, `{"max_projects":-1}`, `{"no_such_limit":1}`, `[]`, `{}`,
		`{"zz_no_such_limit":1,"max_members":"five","aa_no_such_limit":2,"max_projects":-1}`) {
		setEnv(t, "OPENV_LIMITS", in.unset, in.input)
		// Stage config reads it through envOr, which trims.
		want, wantErr := orgs.ParseLimits(envOrToday("OPENV_LIMITS", ""))
		got, gotErr := Load(os.LookupEnv).DeploymentLimits()
		if !reflect.DeepEqual(got, want) || errText(gotErr) != errText(wantErr) {
			t.Errorf("OPENV_LIMITS=%s: DeploymentLimits() = %v, %v; orgs.ParseLimits = %v, %v", in.shown, got, gotErr, want, wantErr)
		}
	}
}

// envOrToday is cmd/server's envOr, word for word, for a test that hands
// its value to a helper.
func envOrToday(key, fallback string) string {
	return envparse.Text(os.Getenv(key), fallback)
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
