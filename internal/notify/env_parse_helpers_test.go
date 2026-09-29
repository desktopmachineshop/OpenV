package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The env parse helpers (refactor plan S8): every package with an env
// getter carries this file, the same in each apart from its package clause
// (internal/archtest's TestEnvInventory fails when a copy differs), and an
// env_parse_test.go whose TestEnvParse calls its real getters and writes its
// sections of internal/archtest/testdata/env_parse.txt, keyed
// <package>:<func>(<param>), with a variable's name after it where the parse
// depends on the variable. The writers share the file under a lock.

// envParseProbe is the variable each row sets to its input, or unsets. No
// production code reads it; a getter under test is given it as its name.
const envParseProbe = "OPENV_S8_PARSE_PROBE"

// envParseUnset stands for an unset variable among envParseInputs.
const envParseUnset = "\x00unset"

// envParseInputs are the raw values every section tries first, in order.
var envParseInputs = []string{envParseUnset, "", "   ", "TRUE", "true", "1", "0", "-5", "7", " 7 ", "+7", "7x",
	"1e3", "2.5", "Inf", "NaN", "2h", " 2h ", "-1h", "800h", "30d", "a, ,b", ","}

// envParseRows sets the probe to each input in turn, then to each of extra,
// and records what get returns.
func envParseRows(t *testing.T, extra []string, get func() string) []string {
	t.Helper()
	var rows []string
	for _, in := range append(slices.Clone(envParseInputs), extra...) {
		shown := strconv.Quote(in)
		t.Setenv(envParseProbe, "")
		if in == envParseUnset {
			os.Unsetenv(envParseProbe)
			shown = "unset"
		} else {
			t.Setenv(envParseProbe, in)
		}
		rows = append(rows, shown+"\t"+get())
	}
	return rows
}

// envParseResult renders a result: default when it equals the fallback the
// test passed, Go-quoted text, a float in its shortest form, a duration as
// time.Duration prints it, a list or a struct as JSON.
func envParseResult[T any](got, fallback T) string {
	if reflect.DeepEqual(got, fallback) {
		return "default"
	}
	switch v := any(got).(type) {
	case string:
		return strconv.Quote(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case time.Duration:
		return v.String()
	}
	if k := reflect.ValueOf(got).Kind(); k == reflect.Slice || k == reflect.Map || k == reflect.Struct {
		data, err := json.Marshal(got)
		if err != nil {
			return "unrenderable: " + err.Error()
		}
		return string(data)
	}
	return fmt.Sprint(got)
}

// checkEnvParse compares the sections of env_parse.txt whose key starts
// with owner and a colon with sections, or rewrites them when UPDATE_GOLDEN
// is exactly 1.
func checkEnvParse(t *testing.T, owner string, sections map[string][]string) {
	t.Helper()
	path, own := envParseGoldenPath(t), owner+":"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		defer envParseLockTable(t)()
		header, have := envParseReadTable(t, path)
		maps.DeleteFunc(have, func(k string, _ []string) bool { return strings.HasPrefix(k, own) })
		maps.Copy(have, sections)
		var b strings.Builder
		b.WriteString(header)
		for i, k := range slices.Sorted(maps.Keys(have)) {
			if i > 0 || header != "" {
				b.WriteString("\n")
			}
			b.WriteString("[" + k + "]\n" + strings.Join(append(have[k], ""), "\n"))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated the %s sections of internal/archtest/testdata/env_parse.txt", own)
		return
	}
	_, have := envParseReadTable(t, path)
	keys := maps.Clone(sections)
	for k, rows := range have {
		if strings.HasPrefix(k, own) {
			keys[k] = rows
		}
	}
	var diff []string
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		want, got := have[k], sections[k]
		for i := 0; i < len(want) || i < len(got); i++ {
			if i >= len(got) || i >= len(want) || want[i] != got[i] {
				diff = append(diff, fmt.Sprintf("[%s] row %d: golden %s, now %s", k, i+1, envParseAt(want, i), envParseAt(got, i)))
			}
		}
	}
	if len(diff) > 0 {
		t.Errorf("internal/archtest/testdata/env_parse.txt does not match what this package's env getters return:\n  %s\n"+
			"A refactor never changes a golden: a getter that parses differently is a behavior change."+
			" If it is deliberate, regenerate it with:\n  UPDATE_GOLDEN=1 go test ./%s -count=1 -run '^TestEnvParse$'\n"+
			"(only UPDATE_GOLDEN=1 regenerates; any other value compares)", strings.Join(diff, "\n  "), owner)
	}
}

// envParseAt shows row i as input -> result.
func envParseAt(rows []string, i int) string {
	if i < len(rows) {
		return strings.Replace(rows[i], "\t", " -> ", 1)
	}
	return "(no row)"
}

// envParseGoldenPath is env_parse.txt under the module root, the first
// directory above the package's that holds go.mod.
func envParseGoldenPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	for ; err == nil; dir = filepath.Dir(dir) {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return filepath.Join(dir, "internal", "archtest", "testdata", "env_parse.txt")
		}
		if filepath.Dir(dir) == dir {
			err = errors.New("no go.mod above the test's directory")
		}
	}
	t.Fatal(err)
	return ""
}

// envParseReadTable reads env_parse.txt, or nothing when it is missing,
// into its header and its sections' rows.
func envParseReadTable(t *testing.T, path string) (string, map[string][]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	var header []string
	sections, key := map[string][]string{}, ""
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			key = line[1 : len(line)-1]
			sections[key] = []string{}
		case key == "":
			header = append(header, line)
		case line != "":
			sections[key] = append(sections[key], line)
		}
	}
	text := strings.TrimRight(strings.Join(header, "\n"), "\n")
	if text == "" {
		return "", sections
	}
	return text + "\n", sections
}

// envParseLockTable takes the lock every writer of env_parse.txt holds while
// it rewrites the file, a directory in the system's temporary directory, and
// returns its release.
func envParseLockTable(t *testing.T) func() {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "openv-env-parse.lock")
	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if err := os.Mkdir(dir, 0o700); err == nil {
			return func() { os.Remove(dir) }
		}
		if st, err := os.Stat(dir); err == nil && time.Since(st.ModTime()) > time.Minute {
			os.Remove(dir) // left by a run that died holding it
		} else if time.Since(start) > 2*time.Minute {
			t.Fatalf("env_parse.txt is locked by %s; remove it if no test is running", dir)
		}
	}
}
