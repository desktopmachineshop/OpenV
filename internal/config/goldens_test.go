package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The S8 goldens this package's tests read, never write: the env var
// inventory and the parse table (refactor plan S8, invariant I13). They
// stay byte-identical in X10a; these tests hold the accessors to them.
var (
	envVarsGolden  = filepath.Join("..", "archtest", "testdata", "env_vars.txt")
	envParseGolden = filepath.Join("..", "archtest", "testdata", "env_parse.txt")
)

// inventoryRow is one row of env_vars.txt: a variable, how it is read, and
// the default that read passes.
type inventoryRow struct {
	name, read, def string
}

// key is the row's identity: the variable and its read column.
func (r inventoryRow) key() string { return r.name + " " + r.read }

// exemption is one of env_vars.txt's exemptions.
type exemption struct {
	id, kind string
	names    []string
}

// readInventory reads env_vars.txt's rows and exemptions.
func readInventory(t *testing.T) ([]inventoryRow, map[string]exemption) {
	t.Helper()
	data, err := os.ReadFile(envVarsGolden)
	if err != nil {
		t.Fatalf("read the S8 inventory: %v", err)
	}
	var rows []inventoryRow
	exemptions := map[string]exemption{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		switch len(f) {
		case 3:
			rows = append(rows, inventoryRow{name: f[0], read: f[1], def: f[2]})
		case 4:
			exemptions[f[0]] = exemption{id: f[0], kind: f[1], names: strings.Split(f[2], ",")}
		default:
			t.Fatalf("env_vars.txt: a line of %d tab-separated fields: %q", len(f), line)
		}
	}
	if len(rows) == 0 {
		t.Fatal("env_vars.txt has no rows")
	}
	return rows, exemptions
}

// parseRow is one row of an env_parse.txt section: the input (unset or a
// raw value) and the result.
type parseRow struct {
	unset  bool
	input  string
	shown  string // the input as the golden shows it
	result string
}

// readParseTable reads env_parse.txt's sections.
func readParseTable(t *testing.T) map[string][]parseRow {
	t.Helper()
	data, err := os.ReadFile(envParseGolden)
	if err != nil {
		t.Fatalf("read the S8 parse table: %v", err)
	}
	sections := map[string][]parseRow{}
	key := ""
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			key = line[1 : len(line)-1]
			sections[key] = nil
		case key == "" || line == "":
		default:
			shown, result, ok := strings.Cut(line, "\t")
			if !ok {
				t.Fatalf("env_parse.txt [%s]: a row with no tab: %q", key, line)
			}
			row := parseRow{shown: shown, result: result}
			if shown == "unset" {
				row.unset = true
			} else if row.input, err = strconv.Unquote(shown); err != nil {
				t.Fatalf("env_parse.txt [%s]: input %s: %v", key, shown, err)
			}
			sections[key] = append(sections[key], row)
		}
	}
	return sections
}

// render shows a value as env_parse.txt and env_vars.txt do: Go-quoted
// text, a duration as time.Duration prints it, a float in its shortest
// form, a list or a struct as JSON, an error as error: <message>.
func render(v any) string {
	switch x := v.(type) {
	case error:
		return "error: " + x.Error()
	case string:
		return strconv.Quote(x)
	case time.Duration:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	if v == nil {
		return "nil"
	}
	if k := reflect.ValueOf(v).Kind(); k == reflect.Slice || k == reflect.Map || k == reflect.Struct {
		data, err := json.Marshal(v)
		if err != nil {
			return "unrenderable: " + err.Error()
		}
		return string(data)
	}
	return fmt.Sprint(v)
}

// renderParse is render, or default when got is the fallback the caller
// passed, as S8's writers show a getter's sentinel fallback.
func renderParse(got, fallback any) string {
	if reflect.DeepEqual(got, fallback) {
		return "default"
	}
	return render(got)
}

// loadFrom is Load over a fixed environment: each key set to its value.
func loadFrom(env map[string]string) *Config {
	return Load(func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	})
}

// with is env plus name set to row's input, or with name unset.
func with(env map[string]string, name string, row parseRow) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	if !row.unset {
		out[name] = row.input
	}
	return out
}

// probeConfig is a Config holding one variable, name, set to row's input or
// unset, for running a parse helper of this package the way S8's writers
// run a getter: with a probe as its name.
func probeConfig(name string, row parseRow, others ...string) *Config {
	c := &Config{vars: map[string]setting{name: {raw: row.input, set: !row.unset}}}
	for _, o := range others {
		c.vars[o] = setting{}
	}
	return c
}

// quietLog discards slog's default logger for the test.
func quietLog(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// captureLog sends slog's default logger to a buffer for the test, without
// timestamps, and returns it.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// oncePerValueLines are the messages of the warnings logged once per
// variable and value for the whole process: internal/envparse's, and the
// session lifetime's, which users and sessionLifetime each keep apart. A
// second read of the same value, by today's helper and then by an accessor,
// logs nothing, so tests that compare the two logs leave them out and check
// them with values of their own.
var oncePerValueLines = []string{
	`msg="ignoring a malformed setting; its default applies"`,
	`msg="a credential setting has spaces or a line break around it; it is used exactly as set"`,
	`msg="session lifetime: ignoring unusable value"`,
	`msg="session lifetime: value above the ceiling, clamping"`,
}

// withoutOncePerValue is a log with the once-per-value warnings left out.
func withoutOncePerValue(log string) string {
	var keep []string
	for _, line := range strings.Split(log, "\n") {
		drop := false
		for _, m := range oncePerValueLines {
			drop = drop || strings.Contains(line, m)
		}
		if !drop {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

// setEnv sets name to value for the test, or unsets it when unset.
func setEnv(t *testing.T, name string, unset bool, value string) {
	t.Helper()
	t.Setenv(name, value)
	if unset {
		os.Unsetenv(name)
	}
}

// traced is c with its reads recorded into reads.
func traced(c *Config, reads *[]string) *Config {
	c.trace = func(name string) { *reads = append(*reads, name) }
	return c
}
