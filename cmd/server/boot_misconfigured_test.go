//go:build unix

package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBootMisconfigured boots the real server once per fatal configuration
// check in main() and records how it refuses to start (refactor plan §6.4
// S4b; invariant I13, "where fatal checks sit relative to migrations";
// OpenV REQ-169's refusal of a price map that does not parse). Each boot
// goes through S4a's startServer on a fresh database of its own, with the
// recording proxy, and its golden testdata/boot/<name>.txt holds:
//
//   - the boot log up to the exit, normalised as the profiles' are, less the
//     lines logged from boot goroutines (the release announcer's), which may
//     or may not beat the exit;
//   - the exit status, and that the server never answered /health;
//   - whether the migrations ran before the fatal check, read from the
//     boot's own database after the exit: no tables at all, or a
//     schema_migrations ledger that runs from version 1 without a gap (the
//     count is not recorded, so a new migration changes no golden here);
//     for a boot whose DATABASE_URL names no server, that it had none;
//   - what the server sent through the recording proxy (nothing);
//   - for a boot pointed at a stand-in database, whether the password it
//     received was DB_PASSWORD exactly as set.
//
// One boot per fatal check, and one more per condition a check must not
// grow: OPENV_LIMITS twice, on a fresh database and with a DATABASE_URL no
// server answers at, which logs the same refusal only while the check comes
// before postgres.Connect (a connect that succeeds logs nothing, so the
// first boot alone cannot tell); the grandfather date; the billing
// configuration three times, with billing on, with no STRIPE_SECRET_KEY
// (billing off) and on a self-hosted deployment (billing kept off), each
// refusing a price map that does not parse, and once more refusing a trial
// with a unit after its number, which billing's counts, unlike every other
// setting, refuse rather than fall back from (#379, question 15);
// CORS_ORIGIN; and the database credentials, which are used exactly as set
// (#379, question 24): a DATABASE_URL with a line break after it, which does
// not parse; one with a space in front of it, as Railway's has no query,
// which lib/pq would read as key=value settings and whose parse error would
// quote the whole URL; and a DB_PASSWORD with a line break, which a stand-in
// database receives line break and all and refuses, each named once in the
// log, where neither the URL nor the password may appear. The boot that
// must still come up despite a malformed value, self-hosted with a
// malformed grandfather date, is a profile of TestBootProfiles,
// self_hosted_bad_grandfather.
func TestBootMisconfigured(t *testing.T) {
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	bin := serverBinary(t)
	for _, b := range misconfiguredBoots {
		t.Run(b.name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			got := runMisconfiguredBoot(t, bin, b)
			checkGolden(t, filepath.Join("testdata", "boot", b.name+".txt"), got, bootMisconfiguredRegenerate)
			t.Logf("boot %s: refused to start in %s", b.name, time.Since(start).Round(time.Millisecond))
		})
	}
}

const bootMisconfiguredRegenerate = testDatabaseURLEnv + "=<server URL> " + updateGoldenEnv +
	"=1 go test ./cmd/server -count=1 -run '^TestBootMisconfigured$'"

const misconfiguredGoldenHeader = `# The server binary (go build -cover ./cmd/server) booted on a fresh
# database with one malformed setting, and how it refused to start (refactor
# plan §6.4 S4b; invariant I13). Written by TestBootMisconfigured
# (cmd/server/boot_misconfigured_test.go) through the S4a harness
# (cmd/server/harness_test.go); the server's HTTP_PROXY and HTTPS_PROXY are
# the test's recording proxy, which refuses everything. Regenerate with
#   OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootMisconfigured$'
# against a server with or without the vector extension.
# Normalised: log timestamps dropped; durations, ports, the release version,
# UUIDs and the temporary directory replaced by <...>; a no-vector server's
# warning taken out; the recording proxy's address shown as <recording proxy
# port>; lines logged from boot goroutines left out.

`

// misconfiguredBoot is one boot that must refuse to start: the variables it
// sets on top of the harness's, and what is wrong with them. With standIn,
// DB_HOST and DB_PORT point at a stand-in database (boot_standin_db_test.go),
// which must receive exactly DB_PASSWORD as env sets it. secret, when set,
// must appear on no line of the boot's log or stdout.
type misconfiguredBoot struct {
	name    string
	about   string
	env     map[string]string
	standIn bool
	secret  string
}

// unreachableDatabase is a DATABASE_URL no server answers at (nothing
// listens on port 1), for the boot that shows a fatal check comes before
// postgres.Connect: a check made after it would log the failed connect
// instead.
const unreachableDatabase = "postgres://openv@127.0.0.1:1/unreachable?sslmode=disable"

// badPriceMap is OPENV_STRIPE_PRICES in a shape an operator might guess,
// which is not the JSON array the variable holds.
const badPriceMap = "business_lite:month=price_boot_harness"

// bootDatabasePassword is the password the database-credential boots set:
// in a DATABASE_URL no server answers at, with a line break after it or a
// space in front of it, and as DB_PASSWORD for the stand-in database, with
// a line break after it.
const bootDatabasePassword = "boot-harness-db-password"

var misconfiguredBoots = []misconfiguredBoot{
	{name: "fatal_limits", about: "OPENV_LIMITS written as key=value, not as a JSON object",
		env: map[string]string{"OPENV_LIMITS": "max_projects=5"}},
	{name: "fatal_limits_no_database", about: "the same OPENV_LIMITS with a DATABASE_URL no server answers at, which must be refused before connect",
		env: map[string]string{"OPENV_LIMITS": "max_projects=5", "DATABASE_URL": unreachableDatabase}},
	{name: "fatal_grandfather", about: "OPENV_BILLING_GRANDFATHER_BEFORE a date without a time, not RFC 3339",
		env: map[string]string{"OPENV_BILLING_GRANDFATHER_BEFORE": badGrandfather}},
	{name: "fatal_billing_prices", about: "billing on with an OPENV_STRIPE_PRICES that is not a JSON array",
		env: map[string]string{"STRIPE_SECRET_KEY": testStripeKey, "OPENV_STRIPE_PRICES": badPriceMap}},
	{name: "fatal_billing_prices_no_key", about: "the same OPENV_STRIPE_PRICES with no STRIPE_SECRET_KEY (billing off)",
		env: map[string]string{"OPENV_STRIPE_PRICES": badPriceMap}},
	{name: "fatal_billing_self_hosted", about: "the same billing configuration on a self-hosted deployment",
		env: map[string]string{"OPENV_SELF_HOSTED": "true", "STRIPE_SECRET_KEY": testStripeKey, "OPENV_STRIPE_PRICES": badPriceMap}},
	{name: "fatal_billing_trial_days", about: "OPENV_BILLING_TRIAL_DAYS with a unit after the number, with no STRIPE_SECRET_KEY (billing off)",
		env: map[string]string{"OPENV_BILLING_TRIAL_DAYS": "30d"}},
	{name: "fatal_cors", about: "CORS_ORIGIN the wildcard *",
		env: map[string]string{"CORS_ORIGIN": "*"}},
	{name: "fatal_database_url_line_break", about: "a DATABASE_URL with a password and a line break after it, used exactly as set, which does not parse",
		env:    map[string]string{"DATABASE_URL": "postgres://openv:" + bootDatabasePassword + "@127.0.0.1:1/unreachable?sslmode=disable\n"},
		secret: bootDatabasePassword},
	{name: "fatal_database_url_leading_space", about: "a DATABASE_URL with a password and a space in front of it, used exactly as set, which lib/pq would read as key=value settings, refused before anything is dialled",
		env:    map[string]string{"DATABASE_URL": " postgres://openv:" + bootDatabasePassword + "@127.0.0.1:1/unreachable"},
		secret: bootDatabasePassword},
	{name: "fatal_db_password_line_break", about: "no DATABASE_URL, and a DB_PASSWORD with a line break after it, which the stand-in database receives exactly as set and refuses",
		env:     map[string]string{"DATABASE_URL": "", "DB_PASSWORD": bootDatabasePassword + "\n"},
		standIn: true, secret: bootDatabasePassword},
}

// misconfiguredExitWithin bounds a refused boot: the migrations and the
// wiring before the last fatal check, as a normal boot does them.
const misconfiguredExitWithin = 60 * time.Second

// runMisconfiguredBoot boots b, waits for the server to exit, and writes
// its golden text.
func runMisconfiguredBoot(t *testing.T, bin string, b misconfiguredBoot) []byte {
	proxy := startRecordingProxy(t)
	db := freshDatabase(t)
	env, shown := b.env, b.env
	var standIn *standInDatabase
	if b.standIn {
		standIn = startStandInDatabase(t)
		env, shown = withEnv(b.env, standIn.env()), withEnv(b.env, map[string]string{
			"DB_HOST": "127.0.0.1", "DB_PORT": standInDatabasePortPlaceholder})
	}
	s, err := startServer(t, bin, db, proxy.env(env))
	if err == nil {
		t.Fatalf("the server came up and answered /health; with %v it must refuse to start. "+
			"UPDATE_GOLDEN=1 does not change this: if main() no longer refuses it on purpose, that changes "+
			"what the server does (invariant I13), and the boot leaves misconfiguredBoots "+
			"(cmd/server/boot_misconfigured_test.go), with its golden testdata/boot/%s.txt, in the same change\n%s",
			b.env, b.name, s.output())
	}
	select {
	case <-s.done:
	case <-time.After(misconfiguredExitWithin):
		t.Fatalf("the server neither answered /health nor exited within %s: %v\n%s", misconfiguredExitWithin, err, s.output())
	}
	ran := migrationsRan(t, db, b)
	want := 0
	if ran.yes {
		want = 1
	}
	var boot []string
	for _, l := range withoutVectorWarning(t, s, db, want) {
		if !isAsync(l.text, nil) {
			boot = append(boot, l.text)
		}
	}
	if b.secret != "" {
		for _, out := range []string{string(s.stderr.Bytes()), string(s.stdout.Bytes())} {
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, b.secret) {
					t.Errorf("the boot printed a credential it was given: %s", l)
				}
			}
		}
	}

	var w goldenWriter
	w.WriteString(misconfiguredGoldenHeader)
	w.profileHead("misconfigured boot", b.name, b.about, shown, true)
	w.section("boot log, in order (lines logged from boot goroutines left out)", boot)
	w.section("stdout", stdoutLines(s))
	w.section("exit", []string{
		fmt.Sprintf("exit status %d", s.cmd.ProcessState.ExitCode()),
		"the server exited before it answered /health",
	})
	w.section("the boot's database after the exit", []string{"migrations ran before the exit: " + ran.text})
	w.section("outbound requests (the recording proxy, after exit)", proxy.summary())
	if standIn != nil {
		w.section("the stand-in database (after exit)", []string{standInSummary(t, standIn, b.env["DB_PASSWORD"])})
	}
	return []byte(w.String())
}

// withEnv is env with more set over it, in a new map.
func withEnv(env, more map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

// standInSummary says what the stand-in database received: one password,
// DB_PASSWORD exactly as the boot set it, or the test fails.
func standInSummary(t *testing.T, standIn *standInDatabase, want string) string {
	t.Helper()
	got := standIn.received()
	if len(got) != 1 || got[0] != want {
		t.Errorf("the stand-in database received the passwords %q; want one, DB_PASSWORD exactly as set, %q", got, want)
		return fmt.Sprintf("received %d passwords, not DB_PASSWORD exactly as set", len(got))
	}
	return "received one password, DB_PASSWORD exactly as set, line break and all, and refused it"
}

// migrationState is what a refused boot left in its database.
type migrationState struct {
	yes  bool
	text string
}

// migrationsRan reads the boot's database after the server exited: whether
// it has any table, and if so whether the schema_migrations ledger runs from
// version 1 without a gap, as MigrateAndBackfill leaves it when it returns.
// A boot that sets DATABASE_URL itself never handed the server the fresh
// database, so there is nothing of its to read.
func migrationsRan(t *testing.T, db testDatabase, b misconfiguredBoot) migrationState {
	t.Helper()
	if b.standIn {
		return migrationState{text: "no (the server was pointed at a stand-in database: DB_HOST and DB_PORT above)"}
	}
	if _, own := b.env["DATABASE_URL"]; own {
		return migrationState{text: "no (the server was given no reachable database: DATABASE_URL above)"}
	}
	conn, err := sql.Open("postgres", db.url)
	if err != nil {
		t.Fatalf("open the boot's database: %v", err)
	}
	defer conn.Close()
	var tables int
	if err := conn.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&tables); err != nil {
		t.Fatalf("count the boot's tables: %v", err)
	}
	if tables == 0 {
		return migrationState{text: "no (the database has no tables)"}
	}
	var ledger bool
	if err := conn.QueryRow(`SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&ledger); err != nil {
		t.Fatalf("look for the ledger: %v", err)
	}
	if !ledger {
		return migrationState{yes: true, text: "in part (the database has tables but no schema_migrations ledger)"}
	}
	var first, last, n int
	if err := conn.QueryRow(`SELECT COALESCE(min(version), 0), COALESCE(max(version), 0), count(*) FROM schema_migrations`).
		Scan(&first, &last, &n); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if n > 0 && first == 1 && last == n {
		return migrationState{yes: true, text: "yes (the schema_migrations ledger runs from version 1 without a gap)"}
	}
	return migrationState{yes: true, text: fmt.Sprintf("in part (the schema_migrations ledger holds %d versions, %d to %d)", n, first, last)}
}
