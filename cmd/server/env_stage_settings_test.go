package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// Characterization pins for the settings refactor step X10b moves into
// internal/config (#379, plan X10). internal/config's own tests hold each
// accessor to today's helper; these hold today's stages to what they compute
// from the environment, by running them in process on a bare app, in
// main()'s order: the logger's level (stage signals), the agents directory
// and the database DSN (stage config), and the single sign-on settings
// (stage sso). boot_stage_settings_test.go holds the pins that need a
// database or the real binary.

// stageSettingVars are the variables stages signals, config and sso read.
// runStages sets each one, to "" unless the test says otherwise, so that the
// shell running the test changes nothing.
var stageSettingVars = []string{
	"OPENV_LOG_LEVEL",
	"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
	"PORT", "UPLOADS_DIR", "OPENV_UPLOAD_SWEEP", "OPENV_DATA_DIR", "AGENTS_DIR", "WORKER_API_KEY",
	"OPENV_SELF_HOSTED", "OPENV_PLAN_DEFAULT", "OPENV_LIMITS",
	"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "PUBLIC_URL", "FRONTEND_URL",
	"OPENV_OIDC_ISSUER", "OPENV_OIDC_REDIRECT_URL", "OPENV_OIDC_SCOPES", "OPENV_OIDC_CLIENT_ID",
	"OPENV_OIDC_CLIENT_SECRET", "OPENV_OIDC_NAME",
}

// stageRun is what a bare app's stages computed and logged.
type stageRun struct {
	a      *app
	logger slog.Handler // the handler stage signals installed
	log    string       // every line the stages logged
}

// runStages sets env over stageSettingVars (envParseUnset unsets a
// variable), then runs stage signals on a bare app, then stages, as main()
// does, with the process's stderr, where the logger stage signals installs
// writes, pointed at a file. It puts back the logger, the log package's
// output, stderr and the deployment policy stage config sets before it
// returns.
func runStages(t *testing.T, env map[string]string, stages ...func(*app)) stageRun {
	t.Helper()
	for _, name := range stageSettingVars {
		t.Setenv(name, "")
	}
	t.Setenv("UPLOADS_DIR", t.TempDir()) // stage config creates it
	for name, value := range env {
		t.Setenv(name, "")
		if value == envParseUnset {
			os.Unsetenv(name)
		} else {
			t.Setenv(name, value)
		}
	}
	out, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	prevLogger, prevOut, prevFlags, prevStderr := slog.Default(), log.Writer(), log.Flags(), os.Stderr
	selfHosted, plan, limits := orgs.SelfHosted(), orgs.DefaultPlan(), orgs.DeploymentLimits()
	defer func() {
		os.Stderr = prevStderr
		slog.SetDefault(prevLogger)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		orgs.SetSelfHosted(selfHosted)
		orgs.SetDefaultPlan(plan)
		orgs.SetDeploymentLimits(limits)
		_ = out.Close()
	}()
	os.Stderr = out

	a := &app{}
	stop := a.signals()
	for _, stage := range stages {
		stage(a)
	}
	stop()
	logged, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return stageRun{a: a, logger: slog.Default().Handler(), log: string(logged)}
}

// shownEnv shows a value runStages sets: unset, or Go-quoted.
func shownEnv(value string) string {
	if value == envParseUnset {
		return "unset"
	}
	return fmt.Sprintf("%q", value)
}

// warningsNaming counts the warnings in logged that name the variable.
func warningsNaming(logged, name string) int {
	n := 0
	for _, line := range strings.Split(logged, "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, name) {
			n++
		}
	}
	return n
}

// TestStageSignalsLogLevel pins OPENV_LOG_LEVEL as stage signals reads it
// (initLogging, logging.go): trimmed and in any case, debug, warn or
// warning, error, and info for info, nothing or anything else, which also
// draws a warning naming the variable. The level is read from the logger the
// stage installs; the warning's wording is not pinned.
func TestStageSignalsLogLevel(t *testing.T) {
	cases := []struct {
		in      string
		level   slog.Level
		unknown bool
	}{
		{envParseUnset, slog.LevelInfo, false},
		{"", slog.LevelInfo, false},
		{"   ", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{" INFO ", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"Debug\n", slog.LevelDebug, false},
		{" DEBUG ", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"WARN", slog.LevelWarn, false},
		{" Warn ", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"\tWARNING\n", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{" Error ", slog.LevelError, false},
		{"verbose", slog.LevelInfo, true},
		{" trace ", slog.LevelInfo, true},
		{"1", slog.LevelInfo, true},
		{"err", slog.LevelInfo, true},
		{"warn ing", slog.LevelInfo, true},
	}
	levels := []slog.Level{slog.LevelDebug - 1, slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	for _, tc := range cases {
		shown := shownEnv(tc.in)
		run := runStages(t, map[string]string{"OPENV_LOG_LEVEL": tc.in})
		for _, l := range levels {
			if got, want := run.logger.Enabled(context.Background(), l), l >= tc.level; got != want {
				t.Errorf("OPENV_LOG_LEVEL=%s: the logger stage signals installed has %v enabled: %v, want %v (level %v)",
					shown, l, got, want, tc.level)
			}
		}
		if got := warningsNaming(run.log, "OPENV_LOG_LEVEL") > 0; got != tc.unknown {
			t.Errorf("OPENV_LOG_LEVEL=%s: a warning naming OPENV_LOG_LEVEL logged: %v, want %v; log:\n%s", shown, got, tc.unknown, run.log)
		}
	}
}

// TestStageSSOOIDCScopes pins OPENV_OIDC_SCOPES as stage sso reads it with
// OPENV_OIDC_ISSUER set (wire_sso.go): untrimmed, nil when unset or empty,
// otherwise split on white space as strings.Fields splits, so a value of
// only spaces is an empty list that is not nil, and a comma separates
// nothing. Without the issuer there is no OIDC configuration.
func TestStageSSOOIDCScopes(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{envParseUnset, nil},
		{"", nil},
		{"   ", []string{}},
		{"openid", []string{"openid"}},
		{" openid  email\tprofile\n", []string{"openid", "email", "profile"}},
		{"openid,email", []string{"openid,email"}},
	}
	for _, tc := range cases {
		run := runStages(t, map[string]string{"OPENV_OIDC_ISSUER": "https://idp.example.test", "OPENV_OIDC_SCOPES": tc.in},
			(*app).config, (*app).sso)
		cfg := run.a.oidcConfig
		if cfg == nil {
			t.Errorf("OPENV_OIDC_SCOPES=%s with OPENV_OIDC_ISSUER set: stage sso built no OIDC configuration", shownEnv(tc.in))
			continue
		}
		if !reflect.DeepEqual(cfg.Scopes, tc.want) || (cfg.Scopes == nil) != (tc.want == nil) {
			t.Errorf("OPENV_OIDC_SCOPES=%s: stage sso's Scopes = %#v, want %#v", shownEnv(tc.in), cfg.Scopes, tc.want)
		}
	}
	run := runStages(t, map[string]string{"OPENV_OIDC_SCOPES": "openid email"}, (*app).config, (*app).sso)
	if run.a.oidcConfig != nil {
		t.Errorf("OPENV_OIDC_SCOPES set with no OPENV_OIDC_ISSUER: stage sso built %+v, want no OIDC configuration", run.a.oidcConfig)
	}
}

// ssoSecretRuns numbers the runs of TestStageSSOReadsNoSecretWithoutItsSwitch
// in this process: internal/envparse names a credential once per value for
// the life of the process, so each run (go test -count=2) sets values no
// earlier run set.
var ssoSecretRuns atomic.Int64

// TestStageSSOReadsNoSecretWithoutItsSwitch pins that stage sso reads
// GOOGLE_CLIENT_SECRET only with GOOGLE_CLIENT_ID set, and
// OPENV_OIDC_CLIENT_SECRET only with OPENV_OIDC_ISSUER set (wire_sso.go):
// without its switch, a secret with spaces around it draws no warning, and
// with it, the same kind of value draws one naming the variable and is kept
// exactly as set.
func TestStageSSOReadsNoSecretWithoutItsSwitch(t *testing.T) {
	n := ssoSecretRuns.Add(1)
	for i, off := range []string{envParseUnset, "", "   "} {
		google := fmt.Sprintf(" x10b-pin-google-secret-%d-%d\n", n, i)
		oidc := fmt.Sprintf(" x10b-pin-oidc-secret-%d-%d\n", n, i)
		run := runStages(t, map[string]string{
			"GOOGLE_CLIENT_ID": off, "GOOGLE_CLIENT_SECRET": google,
			"OPENV_OIDC_ISSUER": off, "OPENV_OIDC_CLIENT_SECRET": oidc,
		}, (*app).config, (*app).sso)
		if run.a.googleOAuth != nil || run.a.oidcConfig != nil {
			t.Errorf("GOOGLE_CLIENT_ID and OPENV_OIDC_ISSUER %s: stage sso built Google %+v and OIDC %+v, want neither",
				shownEnv(off), run.a.googleOAuth, run.a.oidcConfig)
		}
		for _, name := range []string{"GOOGLE_CLIENT_SECRET", "OPENV_OIDC_CLIENT_SECRET"} {
			if got := warningsNaming(run.log, name); got != 0 {
				t.Errorf("GOOGLE_CLIENT_ID and OPENV_OIDC_ISSUER %s: %d warnings name %s, set with spaces around it, want none; log:\n%s",
					shownEnv(off), got, name, run.log)
			}
		}
	}

	google := fmt.Sprintf(" x10b-pin-google-secret-%d-on\n", n)
	oidc := fmt.Sprintf(" x10b-pin-oidc-secret-%d-on\n", n)
	run := runStages(t, map[string]string{
		"GOOGLE_CLIENT_ID": "x10b-pin-google-client", "GOOGLE_CLIENT_SECRET": google,
		"OPENV_OIDC_ISSUER": "https://idp.example.test", "OPENV_OIDC_CLIENT_SECRET": oidc,
	}, (*app).config, (*app).sso)
	if run.a.googleOAuth == nil || run.a.googleOAuth.ClientSecret != google {
		t.Errorf("with GOOGLE_CLIENT_ID set, stage sso's Google configuration is %+v, want the secret exactly as set", run.a.googleOAuth)
	}
	if run.a.oidcConfig == nil || run.a.oidcConfig.ClientSecret != oidc {
		t.Errorf("with OPENV_OIDC_ISSUER set, stage sso's OIDC configuration is %+v, want the secret exactly as set", run.a.oidcConfig)
	}
	for _, name := range []string{"GOOGLE_CLIENT_SECRET", "OPENV_OIDC_CLIENT_SECRET"} {
		if got := warningsNaming(run.log, name); got != 1 {
			t.Errorf("with its switch set, %d warnings name %s, set with spaces around it, want 1; log:\n%s", got, name, run.log)
		}
	}
}

// TestStageConfigAgentsDirDefault pins AGENTS_DIR as stage config reads it
// (wire_config.go): trimmed, and by default OPENV_DATA_DIR (trimmed, default
// ./data) with "/agents" appended to it as text, so a data directory ending
// in a slash keeps it.
func TestStageConfigAgentsDirDefault(t *testing.T) {
	cases := []struct{ dataDir, agentsDir, want string }{
		{envParseUnset, envParseUnset, "./data/agents"},
		{"", "", "./data/agents"},
		{"/srv/openv", envParseUnset, "/srv/openv/agents"},
		{" /srv/openv\n", "   ", "/srv/openv/agents"},
		{"/srv/openv/", "", "/srv/openv//agents"},
		{"/srv/openv", "/srv/agents", "/srv/agents"},
		{envParseUnset, " /srv/agents\n", "/srv/agents"},
	}
	for _, tc := range cases {
		run := runStages(t, map[string]string{"OPENV_DATA_DIR": tc.dataDir, "AGENTS_DIR": tc.agentsDir}, (*app).config)
		if run.a.agentsDir != tc.want {
			t.Errorf("OPENV_DATA_DIR=%s AGENTS_DIR=%s: stage config's agents directory is %q, want %q",
				shownEnv(tc.dataDir), shownEnv(tc.agentsDir), run.a.agentsDir, tc.want)
		}
	}
}

// dbPartsRuns numbers the runs of TestStageConfigReadsNoDBPartWithDatabaseURL
// in this process, for the same reason as ssoSecretRuns.
var dbPartsRuns atomic.Int64

// TestStageConfigReadsNoDBPartWithDatabaseURL pins stage config's database
// settings (wire_config.go): with DATABASE_URL set, the DSN is DATABASE_URL
// exactly as set and no DB_* setting is read, so a DB_PORT that is no port
// does nothing and a DB_PASSWORD with spaces around it draws no warning;
// without it, the same DB_* settings make the DSN, and the password is
// named.
func TestStageConfigReadsNoDBPartWithDatabaseURL(t *testing.T) {
	const databaseURL = "postgres://x10b-pin@db.example.test:6543/openv?sslmode=require"
	password := fmt.Sprintf(" x10b-pin-db-password-%d\n", dbPartsRuns.Add(1))
	env := map[string]string{
		"DATABASE_URL": databaseURL,
		"DB_HOST":      "db-host.example.test", "DB_PORT": "not-a-port", "DB_USER": "x10b",
		"DB_PASSWORD": password, "DB_NAME": "x10b_pin",
	}
	run := runStages(t, env, (*app).config)
	if run.a.dsn != databaseURL {
		t.Errorf("with DATABASE_URL set, stage config's DSN is %q, want DATABASE_URL exactly as set, whatever DB_* say", run.a.dsn)
	}
	if got := warningsNaming(run.log, "DB_PASSWORD"); got != 0 {
		t.Errorf("with DATABASE_URL set, %d warnings name DB_PASSWORD, which stage config must not read; log:\n%s", got, run.log)
	}

	// The control: unset, the same parts make the DSN, the bad port with
	// them, and DB_PASSWORD, read now, is named.
	env["DATABASE_URL"] = envParseUnset
	run = runStages(t, env, (*app).config)
	if want := postgres.ConnString("db-host.example.test", "not-a-port", "x10b", password, "x10b_pin"); run.a.dsn != want {
		t.Errorf("with no DATABASE_URL, stage config's DSN is %q, want %q", run.a.dsn, want)
	}
	if got := warningsNaming(run.log, "DB_PASSWORD"); got != 1 {
		t.Errorf("with no DATABASE_URL, %d warnings name DB_PASSWORD, set with spaces around it, want 1; log:\n%s", got, run.log)
	}
}
