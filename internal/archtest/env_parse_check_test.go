package archtest

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file holds testdata/env_parse.txt together (refactor plan S8): the
// parse table of every read column of env_vars.txt, which X10a reruns
// against internal/config. The getters are unexported helpers of other
// packages, so each such package has an env_parse_test.go whose TestEnvParse
// calls its real helpers and writes its own sections, keyed
// <package>:<func>(<param>), through the helpers of
// env_parse_helpers_test.go, a file each such package carries a copy of.
// TestEnvInventory writes the sections of the standard readers (os.Getenv,
// os.LookupEnv) and of the comparisons (=="true"), and checks the whole: the
// header; a section for every read column, either [<read>] or one
// [<read> <NAME>] per variable where the getter's parse depends on the
// variable; no stale section; every section trying the same inputs first; a
// TestEnvParse in env_parse_test.go that calls each getter; and identical
// copies of env_parse_helpers_test.go. The writers share the file under a
// lock, so one go test run can regenerate every section at once.

// envParseHelpers is the file of helpers every writer of env_parse.txt
// carries, the same in each apart from its package clause.
const envParseHelpers = "env_parse_helpers_test.go"

// envParseInputs are the raw values every section tries first, in order;
// envParseUnset stands for an unset variable. env_parse_helpers_test.go
// holds the same list.
var envParseInputs = []string{envParseUnset, "", "   ", "TRUE", "true", "1", "0", "-5", "7", " 7 ", "+7", "7x",
	"1e3", "2.5", "Inf", "NaN", "2h", " 2h ", "-1h", "800h", "30d", "a, ,b", ","}

const (
	envParseUnset = "\x00unset"
	envParseProbe = "OPENV_S8_PARSE_PROBE"
	envParseLock  = "openv-env-parse.lock"
)

const envParseHeader = `# What each env read returns (refactor plan S8, invariant I13), for X10a to
# rerun against internal/config: one section per read column of
# env_vars.txt, [<read>], or [<read> <NAME>] per variable where the getter's
# parse depends on the variable; [=="true"], [=="1"] and [!=""] are the
# comparisons a read column ends with. Each row is <input><TAB><result>: the
# input is unset or a Go-quoted raw value, and every section tries the same
# inputs first; the result is Go-quoted text, a number, a duration, true or
# false, a JSON list, error: <message>, or default where the getter returned
# the fallback the test passed it, so that X10a substitutes each variable's
# own default. Each package's TestEnvParse (<package>/env_parse_test.go)
# writes its <package>: sections by calling its real helpers, and
# TestEnvInventory the rest; regenerate them all with
#   UPDATE_GOLDEN=1 go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest ./cmd/agentd ./cmd/openv-mcp ./cmd/server ./internal/api ./internal/billing ./internal/domain/users ./internal/hosting ./internal/notify
# (only UPDATE_GOLDEN=1 regenerates; any other value compares).
`

// envParseTable is env_parse.txt: its header and its sections' rows.
type envParseTable struct {
	header   string
	sections map[string][]string
}

func parseEnvParseTable(text string) *envParseTable {
	tbl := &envParseTable{sections: map[string][]string{}}
	var header []string
	key := ""
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			key = line[1 : len(line)-1]
			tbl.sections[key] = []string{}
		case key == "":
			header = append(header, line)
		case line != "":
			tbl.sections[key] = append(tbl.sections[key], line)
		}
	}
	for len(header) > 0 && header[len(header)-1] == "" {
		header = header[:len(header)-1]
	}
	if len(header) > 0 {
		tbl.header = strings.Join(header, "\n") + "\n"
	}
	return tbl
}

func (tbl *envParseTable) render() string {
	keys := make([]string, 0, len(tbl.sections))
	for k := range tbl.sections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(tbl.header)
	for i, k := range keys {
		if i > 0 || tbl.header != "" {
			b.WriteString("\n")
		}
		b.WriteString("[" + k + "]\n")
		for _, row := range tbl.sections[k] {
			b.WriteString(row + "\n")
		}
	}
	return b.String()
}

// envParseArchtestKey reports whether TestEnvInventory writes a section:
// the standard readers' and the comparisons'.
func envParseArchtestKey(key string) bool {
	return strings.HasPrefix(key, "os.") || strings.HasPrefix(key, "syscall.") ||
		strings.HasPrefix(key, "==") || strings.HasPrefix(key, "!=")
}

// envSplitRead splits a read column into its read and its comparison.
func envSplitRead(read string) (base, cmp string) {
	if i := strings.Index(read, " "); i >= 0 {
		return read[:i], read[i+1:]
	}
	return read, ""
}

// envArchtestSections computes the sections TestEnvInventory owns, calling
// the standard readers with the probe set to each input.
func envArchtestSections(t *testing.T, res *envResult) map[string][]string {
	out := map[string][]string{}
	for _, r := range res.rows {
		base, cmp := envSplitRead(r.read)
		if envParseArchtestKey(base) && out[base] == nil {
			out[base] = envParseRows(t, func() string { return envStdRead(base) })
		}
		if cmp != "" && out[cmp] == nil {
			op, lit := cmp[:2], cmp[2:]
			want, err := strconv.Unquote(lit)
			if err != nil {
				t.Fatalf("read column %q: %v", r.read, err)
			}
			out[cmp] = envParseRows(t, func() string {
				return strconv.FormatBool((os.Getenv(envParseProbe) == want) == (op == "=="))
			})
		}
	}
	return out
}

// envStdRead is what a standard reader returns for the probe.
func envStdRead(fn string) string {
	switch fn {
	case "os.Getenv":
		return strconv.Quote(os.Getenv(envParseProbe))
	case "os.LookupEnv":
		if v, ok := os.LookupEnv(envParseProbe); ok {
			return strconv.Quote(v)
		}
		return "unset"
	}
	return "unsupported: add " + fn + " to envStdRead"
}

// envParseRows sets the probe to each input in turn and records get.
func envParseRows(t *testing.T, get func() string) []string {
	var rows []string
	for _, in := range envParseInputs {
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

// checkEnvParseTable writes or compares TestEnvInventory's sections of
// env_parse.txt, then checks the table against the inventory.
func checkEnvParseTable(t *testing.T, m *module, res *envResult) {
	own := envArchtestSections(t, res)
	if envUpdating() {
		unlock := envLockParseTable(t)
		defer unlock()
		tbl := &envParseTable{sections: map[string][]string{}}
		if data, err := os.ReadFile(envParseGolden); err == nil {
			tbl = parseEnvParseTable(string(data))
		}
		for k := range tbl.sections {
			if envParseArchtestKey(k) {
				delete(tbl.sections, k)
			}
		}
		for k, rows := range own {
			tbl.sections[k] = rows
		}
		tbl.header = envParseHeader
		if err := os.WriteFile(envParseGolden, []byte(tbl.render()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated its sections of internal/archtest/%s; run again without %s to check the whole table", envParseGolden, envUpdateEnv)
		return
	}
	data, err := os.ReadFile(envParseGolden)
	if err != nil {
		t.Fatalf("read golden internal/archtest/%s: %v\nCreate it with:\n  %s\n%s", envParseGolden, err, envParseRegenerate, envOnlyOne)
	}
	tbl := parseEnvParseTable(string(data))
	bad := envParseProblems(tbl, own, res)
	bad = append(bad, envParseWriters(m, res)...)
	if len(bad) > 0 {
		t.Error(envFailure("env parse table", bad, envParseFix, envParseRegenerate))
	}
}

const envParseFix = "Fix: a new getter needs a TestEnvParse in its package's env_parse_test.go that calls it and writes its section," +
	" beside a copy of env_parse_helpers_test.go from another package (only the package clause differs); a getter whose" +
	" parse depends on the variable writes one section per variable."

// envParseProblems compares TestEnvInventory's own sections and checks
// that the table has exactly one set of sections per read column.
func envParseProblems(tbl *envParseTable, own map[string][]string, res *envResult) []string {
	var bad []string
	if tbl.header != envParseHeader {
		bad = append(bad, "the header is not the one TestEnvInventory writes")
	}
	for k, rows := range own {
		if strings.Join(tbl.sections[k], "\n") != strings.Join(rows, "\n") {
			bad = append(bad, fmt.Sprintf("[%s] differs from what %s returns now:\n%s", k, k,
				envLineDiff(strings.Join(tbl.sections[k], "\n"), strings.Join(rows, "\n"))))
		}
	}
	names := map[string][]string{} // read -> variables
	for _, r := range res.rows {
		base, cmp := envSplitRead(r.read)
		names[base] = append(names[base], r.name)
		if cmp != "" {
			names[cmp] = nil
		}
	}
	want := map[string]bool{}
	for base, vars := range names {
		if _, whole := tbl.sections[base]; whole || len(vars) == 0 {
			want[base] = true
			if !whole {
				bad = append(bad, fmt.Sprintf("no section [%s] for the read column %s of env_vars.txt", base, base))
			}
			continue
		}
		var missing []string
		for _, v := range vars {
			want[base+" "+v] = true
			if _, ok := tbl.sections[base+" "+v]; !ok && !slices.Contains(missing, v) {
				missing = append(missing, v)
			}
		}
		switch {
		case len(missing) == 0:
		case len(missing) == len(slices.Compact(slices.Sorted(slices.Values(vars)))):
			bad = append(bad, fmt.Sprintf("no section [%s], nor one [%s <NAME>] per variable, for the read column %s of env_vars.txt", base, base, base))
		default:
			bad = append(bad, fmt.Sprintf("no section [%s <NAME>] for %s, read through %s", base, strings.Join(missing, ", "), base))
		}
	}
	for k, rows := range tbl.sections {
		if !want[k] {
			bad = append(bad, fmt.Sprintf("[%s] is not a read column of env_vars.txt, or not one read the way its key says (stale)", k))
		}
		for i, in := range envParseInputs {
			shown := strconv.Quote(in)
			if in == envParseUnset {
				shown = "unset"
			}
			if i >= len(rows) || !strings.HasPrefix(rows[i], shown+"\t") {
				bad = append(bad, fmt.Sprintf("[%s] row %d does not try the input %s: every section tries the same inputs first", k, i+1, shown))
				break
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// envParseWriters checks the per-package writers: each getter in a read
// column is called by a TestEnvParse in env_parse_test.go of its package,
// and every copy of env_parse_helpers_test.go is the same.
func envParseWriters(m *module, res *envResult) []string {
	var bad []string
	used := map[string]bool{}
	for _, r := range res.rows {
		base, _ := envSplitRead(r.read)
		used[base] = true
	}
	for _, g := range res.getters {
		called := false
		for k := range used {
			called = called || strings.HasPrefix(k, g.key+"(")
		}
		if called && !envParseCalls(m.pkgs[g.pkg.name], g.decl.Name.Name) {
			bad = append(bad, fmt.Sprintf("%s: no TestEnvParse in %s/env_parse_test.go calls %s", g.key, g.pkg.name, g.decl.Name.Name))
		}
	}
	var ref, refFile string
	for _, name := range m.names {
		for _, f := range m.pkgs[name].tests {
			if f.base != envParseHelpers {
				continue
			}
			src, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(f.rel)))
			if err != nil {
				return append(bad, err.Error())
			}
			_, body, _ := strings.Cut(string(src), "\n") // all but the package clause
			switch {
			case ref == "":
				ref, refFile = body, f.rel
			case body != ref:
				bad = append(bad, f.rel+" differs from "+refFile+" below its package clause: keep every copy the same")
			}
		}
	}
	return bad
}

// envParseCalls reports whether p's env_parse_test.go declares TestEnvParse
// and refers to name.
func envParseCalls(p *pkg, name string) bool {
	if p == nil {
		return false
	}
	for _, f := range p.tests {
		if f.base != "env_parse_test.go" {
			continue
		}
		declares, refers := false, false
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				declares = declares || (x.Recv == nil && x.Name.Name == "TestEnvParse")
			case *ast.Ident:
				refers = refers || x.Name == name
			}
			return true
		})
		return declares && refers
	}
	return false
}

// envLockParseTable takes the lock every writer of env_parse.txt holds while
// it rewrites the file, so that the packages of one go test run can
// regenerate their sections at once.
func envLockParseTable(t *testing.T) func() {
	dir := filepath.Join(os.TempDir(), envParseLock)
	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if err := os.Mkdir(dir, 0o700); err == nil {
			return func() { os.Remove(dir) }
		}
		if st, err := os.Stat(dir); err == nil && time.Since(st.ModTime()) > time.Minute {
			os.Remove(dir) // left by a run that died holding it
			continue
		}
		if time.Since(start) > 2*time.Minute {
			t.Fatalf("%s is locked by %s; remove it if no test is running", envParseGolden, dir)
		}
	}
}
