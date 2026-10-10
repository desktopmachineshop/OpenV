package config

import (
	"bufio"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestEnvParse writes internal/config's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13): what each of Config's parse helpers returns for each input, called
// on a Config that recorded the probe, with the probe as the name and a
// sentinel fallback, which a row shows as default. Each section is rendered
// exactly as the section of the getter the helper reproduces (cmd/server's,
// internal/api's, internal/notify's, internal/domain/users'), so that a read
// X10b moves into this package keeps its results and only its label moves.
//
// S8's scan sees these helpers as getters only once cmd/server hands Load an
// env reader (refactor step X10b), so a section is written only for a read
// column env_vars.txt lists: until then this test writes and compares
// nothing. After the inventory gains this package's read columns, run
//
//	UPDATE_GOLDEN=1 go test ./internal/config -count=1 -run '^TestEnvParse$'
//
// once env_vars.txt is regenerated (only UPDATE_GOLDEN=1 regenerates; any
// other value compares).
func TestEnvParse(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	const unset = envParseProbe + "_UNSET"
	t.Setenv(unset, "")
	os.Unsetenv(unset)
	fallback := func() []string { return []string{"fallback"} }
	all := map[string][]string{
		"internal/config:Config.text(name)": envParseRows(t, nil, func() string {
			return envParseResult(probed(unset).text(envParseProbe, "fallback"), "fallback")
		}),
		"internal/config:Config.count(name)": envParseRows(t, nil, func() string {
			return envParseResult(probed(unset).count(envParseProbe, 42), 42)
		}),
		"internal/config:Config.boolean(name)": envParseRows(t, nil, func() string {
			return envParseBool(probed(unset).boolean(envParseProbe, false), probed(unset).boolean(envParseProbe, true))
		}),
		"internal/config:Config.onOff(name)": envParseRows(t, []string{"off", " OFF ", "on", "On"}, func() string {
			return envParseBool(probed(unset).onOff(envParseProbe, false), probed(unset).onOff(envParseProbe, true))
		}),
		"internal/config:Config.secret(name)": envParseRows(t, []string{"key\n", " key"}, func() string {
			return envParseResult(probed(unset).secret(envParseProbe, "fallback"), "fallback")
		}),
		"internal/config:Config.credential(name)": envParseRows(t, []string{"key\n", " key"}, func() string {
			return envParseResult(probed(unset).credential(envParseProbe), "\x00no fallback")
		}),
		"internal/config:Config.getenv(name)": envParseRows(t, nil, func() string {
			return strconv.Quote(probed(unset).getenv(envParseProbe))
		}),
		"internal/config:Config.rateLimit(burstVar)": envParseRows(t, nil, func() string {
			return envParseResult(probed(unset).rateLimit(envParseProbe, unset, 42, 4.5).Burst, 42)
		}),
		"internal/config:Config.rateLimit(refillVar)": envParseRows(t, nil, func() string {
			return envParseResult(probed(unset).rateLimit(unset, envParseProbe, 42, 4.5).RefillPerHour, 4.5)
		}),
		"internal/config:Config.typeList(name)": envParseRows(t, []string{" a , b ", "b,a,a", "A"}, func() string {
			return envParseResult(probed(unset).typeList(envParseProbe, fallback), fallback())
		}),
	}
	for name, max := range map[string]time.Duration{"OPENV_SESSION_MAX_AGE": users.DefaultSessionMaxAge,
		"OPENV_SESSION_IDLE": users.DefaultSessionIdle} {
		all["internal/config:Config.sessionLifetime(name) "+name] = envParseRows(t, []string{"719h", "167h59m"}, func() string {
			return envParseResult(probed(unset).sessionLifetime(envParseProbe, max), max)
		})
	}
	listed := inventoryReadColumns(t)
	sections := map[string][]string{}
	for k, rows := range all {
		if base, _, _ := strings.Cut(k, " "); listed[base] {
			sections[k] = rows
		}
	}
	checkEnvParse(t, "internal/config", sections)
}

// probed is a Config that recorded the probe, and the variable named unset,
// as the process environment has them now.
func probed(unset string) *Config {
	c := &Config{vars: map[string]setting{}}
	for _, name := range []string{envParseProbe, unset} {
		raw, set := os.LookupEnv(name)
		c.vars[name] = setting{raw: raw, set: set}
	}
	return c
}

// envParseBool renders a boolean getter's row from its results with each
// fallback: default when each call returned its own fallback, else the value
// both returned (cmd/server's rendering).
func envParseBool(withFalse, withTrue bool) string {
	switch {
	case !withFalse && withTrue:
		return "default"
	case withFalse == withTrue:
		return envParseResult(withFalse, !withFalse)
	}
	return "unrenderable: the fallback inverted the result"
}

// inventoryReadColumns is the set of read columns env_vars.txt lists.
func inventoryReadColumns(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(filepath.Join(filepath.Dir(envParseGoldenPath(t)), "env_vars.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) == 3 && !strings.HasPrefix(cols[0], "#") {
			base, _, _ := strings.Cut(cols[1], " ")
			out[base] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
