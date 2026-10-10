package config

import (
	"bytes"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// The hosted runners' settings tests, which internal/hosting held while
// hosting.NewProvisioner read HOSTED_RUNNERS and hosting.PidsLimit
// HOSTED_RUNNER_PIDS_LIMIT (refactor step X10b moved those reads here, to
// HostedRunnersOff and HostedRunnerPidsLimit; hosting's own tests hold what
// the provisioner does with the values). Each reads the process
// environment, set with t.Setenv, through a Config loaded from it.

// TestHostedRunnersOffPin pins HOSTED_RUNNERS as the provisioner's switch,
// case for case as the characterization pin refactor step X10b's pin pull
// request (#570) added beside hosting.NewProvisioner: trimmed and in any
// case, off turns hosted runners off; any other value, or none, leaves them
// on (internal/hosting's TestNewProvisionerHostedRunnersSwitch pins that on
// dials Docker and off does not).
func TestHostedRunnersOffPin(t *testing.T) {
	quietLog(t)
	cases := []struct {
		value string
		off   bool
	}{
		{"off", true},
		{" OFF ", true},
		{"Off", true},
		{"\toFF\n", true},
		{"\x00unset", false},
		{"", false},
		{"   ", false},
		{"on", false},
		{"0", false},
		{"FALSE", false},
		{"no", false},
		{"of", false},
		{"offline", false},
		{"o ff", false},
	}
	for _, tc := range cases {
		unset := tc.value == "\x00unset"
		if unset {
			tc.value = ""
		}
		setEnv(t, "HOSTED_RUNNERS", unset, tc.value)
		if got := Load(os.LookupEnv).HostedRunnersOff(); got != tc.off {
			t.Errorf("HOSTED_RUNNERS=%q: HostedRunnersOff() = %v, want %v", tc.value, got, tc.off)
		}
	}
}

// hostedRunnersRuns numbers the runs of the test below within one test
// binary: internal/envparse names a variable once per value for the life of
// the process, so each run (go test -count=2) spells its value differently.
var hostedRunnersRuns atomic.Int64

// hostedRunnersOffset keeps the spellings of false the test below sets
// (spelled, pairs_test.go) apart from those the other tests of this package
// set.
const hostedRunnersOffset = 7

// Only off switches hosted runners off (#379, bug 225). Any other value,
// false among them, leaves them on, as before, but is no longer passed over
// in silence: the log names the variable and the value it takes once,
// however often it is read, never the value it has. A blank value is silent.
func TestHostedRunnersWarnsOnAValueItDoesNotTake(t *testing.T) {
	var logged bytes.Buffer
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	read := func() {
		t.Helper()
		if Load(os.LookupEnv).HostedRunnersOff() {
			t.Fatalf("HOSTED_RUNNERS=%q: HostedRunnersOff() = true, want false", os.Getenv("HOSTED_RUNNERS"))
		}
	}

	value := spelled("false", hostedRunnersRuns.Add(1)+hostedRunnersOffset)
	t.Setenv("HOSTED_RUNNERS", value)
	read()
	read()
	warning := `level=WARN msg="ignoring a malformed setting; its default applies" var=HOSTED_RUNNERS want="off (any case), or unset"`
	if n := strings.Count(logged.String(), "var=HOSTED_RUNNERS "); n != 1 || !strings.Contains(logged.String(), warning) {
		t.Errorf("HOSTED_RUNNERS=%q read twice: want the one warning\n%s\ngot:\n%s", value, warning, logged.String())
	}
	if strings.Contains(logged.String(), value) {
		t.Errorf("the warning printed the value:\n%s", logged.String())
	}

	for _, blank := range []string{"", "   "} {
		logged.Reset()
		t.Setenv("HOSTED_RUNNERS", blank)
		read()
		if strings.Contains(logged.String(), "level=WARN") {
			t.Errorf("HOSTED_RUNNERS=%q warned:\n%s", blank, logged.String())
		}
	}
}

func TestHostedRunnerPidsLimit(t *testing.T) {
	quietLog(t)
	// The pids cgroup counts THREADS, not processes. A node CLI's libuv pool
	// and V8 workers, a toolchain build and a test run all draw on the same
	// allowance, so a cap sized as if it were a process count (256) sat close
	// enough to a real workload's ceiling to abort runs — visible only as a
	// fork failure deep inside a vendor CLI. It still has to stop a fork bomb,
	// so it is raised, not removed.
	const defaultPidsLimit = 1024
	cases := []struct {
		env  string
		want int64
	}{
		{"", defaultPidsLimit},
		{"64", 64},
		{" 512 ", 512},
		{"0", 0},
		{"-1", 0},
		{"banana", defaultPidsLimit}, // never silently unlimited
		{"   ", defaultPidsLimit},
		{"2k", defaultPidsLimit},
		{"1e3", defaultPidsLimit},
		{"512.5", defaultPidsLimit},
	}
	for _, tc := range cases {
		// An empty value takes the same path as an unset one.
		t.Setenv("HOSTED_RUNNER_PIDS_LIMIT", tc.env)
		if got := Load(os.LookupEnv).HostedRunnerPidsLimit(); got != tc.want {
			t.Errorf("HOSTED_RUNNER_PIDS_LIMIT=%q: HostedRunnerPidsLimit() = %d, want %d", tc.env, got, tc.want)
		}
	}
}
