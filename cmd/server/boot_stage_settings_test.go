//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// More characterization pins for refactor step X10b (#379, plan X10), the
// ones env_stage_settings_test.go cannot run without a database: the build
// commit, the hosted runner switch and the session lifetime's clamp through
// the real binary (the S4 harness, harness_test.go), and the effect of
// OPENV_RUN_AUTO_RETRY through stage agents run in process. Each skips
// when OPENV_TEST_DATABASE_URL is unset.

// commitOf is the commit an endpoint of the server reports: the "commit"
// field of its JSON body, "" when there is none.
func commitOf(t *testing.T, s *serverProcess, path string) string {
	t.Helper()
	resp, err := s.client().Get(s.base + path)
	if err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, s.output())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s %v\n%s", path, resp.Status, err, raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, raw)
	}
	commit, _ := body["commit"].(string)
	return commit
}

// TestBootBuildCommit pins the commit stage release reads (wire_notify.go)
// and /health and /api/v1/public/build report: OPENV_BUILD_SHA, trimmed,
// over RAILWAY_GIT_COMMIT_SHA, the fallback, trimmed too; neither set, no
// commit.
func TestBootBuildCommit(t *testing.T) {
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	bin := serverBinary(t)
	const build, railway = "x10b-pin-build-sha", "x10b-pin-railway-sha"
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"build_sha_over_railway", map[string]string{"OPENV_BUILD_SHA": build, "RAILWAY_GIT_COMMIT_SHA": railway}, build},
		{"railway_alone", map[string]string{"RAILWAY_GIT_COMMIT_SHA": " " + railway + "\n"}, railway},
		{"blank_build_sha_falls_back", map[string]string{"OPENV_BUILD_SHA": "   ", "RAILWAY_GIT_COMMIT_SHA": railway}, railway},
		{"neither", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := bootServer(t, bin, tc.env)
			for _, path := range []string{"/health", "/api/v1/public/build"} {
				if got := commitOf(t, s, path); got != tc.want {
					t.Errorf("%q: %s reports commit %q, want %q", tc.env, path, got, tc.want)
				}
			}
		})
	}
}

// hostedRunnersOffLine is the line hosting.NewProvisioner logs when
// HOSTED_RUNNERS turns hosted runners off, before it dials Docker.
const hostedRunnersOffLine = `msg="Hosted runners disabled (HOSTED_RUNNERS=off)"`

// TestBootHostedRunnersSwitch pins HOSTED_RUNNERS as the server reads it
// (stage runners, hosting.NewProvisioner): off in any case and with spaces
// around it turns hosted runners off before Docker is dialled; any other
// value leaves them on, so the server dials Docker, here a socket no daemon
// listens on.
func TestBootHostedRunnersSwitch(t *testing.T) {
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	bin := serverBinary(t)
	cases := []struct {
		name, value string
		off         bool
	}{
		{"spaced_upper_case_off", " OFF ", true},
		{"mixed_case_off", "Off", true},
		{"on", "on", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := bootServer(t, bin, map[string]string{
				"HOSTED_RUNNERS": tc.value,
				"DOCKER_HOST":    "unix://" + filepath.Join(t.TempDir(), "no-docker.sock"),
			})
			if got := strings.Contains(string(s.stderr.Bytes()), hostedRunnersOffLine); got != tc.off {
				t.Errorf("HOSTED_RUNNERS=%q: hosted runners switched off before Docker: %v, want %v; boot log:\n%s",
					tc.value, got, tc.off, s.stderr.Bytes())
			}
		})
	}
}

// sessionLifetimeLine is the line stage notify logs with the session policy
// it hands the user service.
var sessionLifetimeLine = regexp.MustCompile(`msg="session lifetime" max_age=(\S+) idle=(\S+)`)

// TestBootSessionLifetimeClamp pins the session lifetime stage notify
// computes (users.SessionPolicyFromEnv) just above and at its ceilings: a
// second over 720h or 168h clamps to the ceiling with a warning naming the
// variable, and the ceiling itself is kept with none. The warning's wording
// is not pinned.
func TestBootSessionLifetimeClamp(t *testing.T) {
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	bin := serverBinary(t)
	cases := []struct {
		name, maxAge, idle string
		warnMaxAge         bool
		warnIdle           bool
	}{
		{"max_age_just_above", "720h0m1s", "168h", true, false},
		{"idle_just_above", "720h", "168h0m1s", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := bootServer(t, bin, map[string]string{"OPENV_SESSION_MAX_AGE": tc.maxAge, "OPENV_SESSION_IDLE": tc.idle})
			logged := string(s.stderr.Bytes())
			m := sessionLifetimeLine.FindStringSubmatch(logged)
			if m == nil {
				t.Fatalf("no session lifetime line in the boot log:\n%s", logged)
			}
			for i, want := range []time.Duration{users.DefaultSessionMaxAge, users.DefaultSessionIdle} {
				if got, err := time.ParseDuration(m[i+1]); err != nil || got != want {
					t.Errorf("OPENV_SESSION_MAX_AGE=%s OPENV_SESSION_IDLE=%s: the session policy's %s is %s, want %s",
						tc.maxAge, tc.idle, []string{"max_age", "idle"}[i], m[i+1], want)
				}
			}
			for name, want := range map[string]bool{"OPENV_SESSION_MAX_AGE": tc.warnMaxAge, "OPENV_SESSION_IDLE": tc.warnIdle} {
				if got := warningsNaming(logged, name) > 0; got != want {
					t.Errorf("OPENV_SESSION_MAX_AGE=%s OPENV_SESSION_IDLE=%s: a warning naming %s logged: %v, want %v; boot log:\n%s",
						tc.maxAge, tc.idle, name, got, want, logged)
				}
			}
		})
	}
}

// TestStageAgentsRunAutoRetry pins the effect of OPENV_RUN_AUTO_RETRY as
// stage agents reads it (wire_agents.go): by default, and set true, a run
// that fails with a retryable class is launched again; set false, in any
// case and with spaces around it, or 0, the failure stands. The test runs
// the real stages storage, workspace and agents in process, as main() calls
// them, over a database of its own, once per value, and drives one run per
// value through the run service stage agents built: launched, claimed and
// failed as provider_unavailable.
func TestStageAgentsRunAutoRetry(t *testing.T) {
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Stage workspace reads the first two, stage agents the third; unset,
	// whatever the shell running the test has.
	t.Setenv("OPENV_BILLING_GRANDFATHER_BEFORE", "")
	t.Setenv("RUNNER_POOL_KEY", "")
	t.Setenv("OPENV_RUN_MAX_ATTEMPTS", "")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	agentsDir, uploadsDir := t.TempDir(), t.TempDir()
	cases := []struct {
		value   string
		retried bool
	}{
		{envParseUnset, true},
		{"", true},
		{"true", true},
		{"false", false},
		{" FALSE ", false},
		{"0", false},
	}
	for i, tc := range cases {
		shown := shownEnv(tc.value)
		t.Setenv("OPENV_RUN_AUTO_RETRY", "")
		if tc.value == envParseUnset {
			os.Unsetenv("OPENV_RUN_AUTO_RETRY")
		} else {
			t.Setenv("OPENV_RUN_AUTO_RETRY", tc.value)
		}

		a := &app{db: conn, agentsDir: agentsDir, uploadsDir: uploadsDir}
		a.storage()
		a.workspace()
		// A workspace of this value's own, which stage agents seeds.
		userID, now := uuid.NewString(), time.Now().UTC()
		must(a.userRepo.SaveUser(&users.User{ID: userID, Email: fmt.Sprintf("retry-%d@example.test", i), Name: "retry",
			AuthProvider: users.ProviderPassword, CreatedAt: now, UpdatedAt: now}))
		org, err := a.orgService.CreateOrg(fmt.Sprintf("Retry %d", i), orgs.TypeCompany, userID)
		must(err)
		a.agents()

		list, err := a.agentService.List(org.ID)
		must(err)
		if len(list) == 0 {
			t.Fatalf("OPENV_RUN_AUTO_RETRY=%s: stage agents seeded no agent into the workspace", shown)
		}
		run, _, err := a.runService.Launch(agentruns.LaunchRequest{OrgID: org.ID, AgentID: list[0].ID,
			Prompt: "x10b pin: fail once", LaunchedBy: &userID})
		must(err)
		claimed, err := a.runService.Claim("x10b-pin-worker", org.ID, "", []string{list[0].Provider}, 0, false)
		must(err)
		if claimed == nil || claimed.ID != run.ID {
			t.Fatalf("OPENV_RUN_AUTO_RETRY=%s: the claim took %+v, want the run just launched (%s)", shown, claimed, run.ID)
		}
		_, err = a.runService.Finish(run.ID, agentruns.FinishRequest{Status: agentruns.StatusFailed,
			ErrorClass: agentruns.ErrorClassProviderUnavailable, Error: "x10b pin: the provider is unavailable"})
		must(err)

		var retries int
		must(conn.QueryRow(`SELECT COUNT(*) FROM agent_runs WHERE retried_from_run_id = $1`, run.ID).Scan(&retries))
		if got := retries > 0; got != tc.retried {
			t.Errorf("OPENV_RUN_AUTO_RETRY=%s: a run failed as provider_unavailable was launched again: %v (%d runs), want %v",
				shown, got, retries, tc.retried)
		}
	}
}
