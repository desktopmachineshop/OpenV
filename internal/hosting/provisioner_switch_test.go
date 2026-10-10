package hosting

import (
	"bytes"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestNewProvisionerHostedRunnersSwitch pins the switch NewProvisioner
// takes, the characterization pin refactor step X10b's pin pull request
// (#570) added: off turns hosted runners off without dialling Docker; on,
// NewProvisioner dials Docker, here a stand-in daemon that answers the ping
// and counts the requests it gets. Which values of HOSTED_RUNNERS are off,
// read by cmd/server since X10b, internal/config's TestHostedRunnersOffPin
// pins, case for case.
func TestNewProvisionerHostedRunnersSwitch(t *testing.T) {
	var requests atomic.Int64
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Api-Version", "1.47")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(daemon.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+daemon.Listener.Addr().String())
	for _, name := range []string{"DOCKER_API_VERSION", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"} {
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

	for _, off := range []bool{true, false} {
		before := requests.Load()
		p := NewProvisioner(Settings{
			Off:       off,
			Container: func() Container { return Container{Image: "openv-worker:latest", APIURL: "http://api:8080"} },
			PidsLimit: func() int64 { return defaultPidsLimit },
		})
		if d, ok := p.(*dockerProvisioner); ok {
			d.cli.Close()
		}
		dialled := requests.Load() > before
		if p.Enabled() == off || dialled == off {
			t.Errorf("off %v: NewProvisioner enabled %v, dialled Docker %v; want both %v; log:\n%s",
				off, p.Enabled(), dialled, !off, logged.String())
		}
	}
}
