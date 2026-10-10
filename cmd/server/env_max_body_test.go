package main

import (
	"bytes"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openv/requirements-platform/internal/config"
	"github.com/openv/requirements-platform/internal/envparse"
)

// maxBodyRuns numbers the runs of the test below within one test binary:
// internal/envparse names a variable once per value for the life of the
// process, so each run (go test -count=2) sets a value of its own.
var maxBodyRuns atomic.Int64

// An OPENV_MAX_BODY_MB whose bytes do not fit in an int64 keeps the 32 MiB
// default, with one warning naming the variable, never the value, however
// often it is read (#379, bug 224). 8796093022208 MiB is 2^63 bytes, which
// wrapped round to a negative cap, so every request with a body was
// refused. The largest size that fits is taken as set.
func TestMaxBodyMBTooBigForBytesKeepsTheDefault(t *testing.T) {
	tooBig := strconv.FormatInt(envparse.MaxMebibytes+maxBodyRuns.Add(1), 10)
	var log bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Setenv("OPENV_MAX_BODY_MB", tooBig)
	for range 3 {
		if got := config.Load(os.LookupEnv).MaxBodyBytes(); got != 32<<20 {
			t.Errorf("OPENV_MAX_BODY_MB=%s: cap %d, want the default %d", tooBig, got, 32<<20)
		}
	}
	if n := strings.Count(log.String(), "level=WARN"); n != 1 || !strings.Contains(log.String(), "var=OPENV_MAX_BODY_MB ") {
		t.Errorf("OPENV_MAX_BODY_MB=%s read three times: want one warning naming the variable, got:\n%s", tooBig, log.String())
	}
	if strings.Contains(log.String(), tooBig) {
		t.Errorf("the warning printed the value:\n%s", log.String())
	}

	log.Reset()
	t.Setenv("OPENV_MAX_BODY_MB", strconv.FormatInt(envparse.MaxMebibytes, 10))
	if got, want := config.Load(os.LookupEnv).MaxBodyBytes(), envparse.MaxMebibytes<<20; got != want {
		t.Errorf("OPENV_MAX_BODY_MB=%d: cap %d, want %d", envparse.MaxMebibytes, got, want)
	}
	if log.Len() != 0 {
		t.Errorf("OPENV_MAX_BODY_MB=%d, which fits, logged:\n%s", envparse.MaxMebibytes, log.String())
	}
}
