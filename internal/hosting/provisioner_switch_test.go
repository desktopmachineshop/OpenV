package hosting

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

// TestNewProvisionerHostedRunnersSwitch pins HOSTED_RUNNERS as
// NewProvisioner reads it, a characterization pin for refactor step X10b
// (#379, plan X10): trimmed and in any case, off turns hosted runners off
// without dialling Docker; any other value, or none, leaves them on, so
// NewProvisioner dials Docker, here a stand-in daemon that answers the ping
// and counts the requests it gets.
func TestNewProvisionerHostedRunnersSwitch(t *testing.T) {
	var requests atomic.Int64
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Api-Version", "1.47")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(daemon.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+daemon.Listener.Addr().String())
	for _, name := range []string{"DOCKER_API_VERSION", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY", "HOSTED_RUNNER_PIDS_LIMIT"} {
		t.Setenv(name, "")
	}
	// slog.SetDefault also points the log package at the new handler, and
	// setting the old one back does not undo that, so both are restored.
	var logged bytes.Buffer
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	cases := []struct {
		value string
		off   bool
	}{
		{"off", true},
		{" OFF ", true},
		{"Off", true},
		{"\toFF\n", true},
		{envParseUnset, false},
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
		shown := fmt.Sprintf("%q", tc.value)
		t.Setenv("HOSTED_RUNNERS", "")
		if tc.value == envParseUnset {
			os.Unsetenv("HOSTED_RUNNERS")
			shown = "unset"
		} else {
			t.Setenv("HOSTED_RUNNERS", tc.value)
		}
		before := requests.Load()
		p := NewProvisioner()
		if d, ok := p.(*dockerProvisioner); ok {
			d.cli.Close()
		}
		dialled := requests.Load() > before
		if p.Enabled() == tc.off || dialled == tc.off {
			t.Errorf("HOSTED_RUNNERS=%s: NewProvisioner() enabled %v, dialled Docker %v; want both %v; log:\n%s",
				shown, p.Enabled(), dialled, !tc.off, logged.String())
		}
	}
}
