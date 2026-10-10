package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// probe is the name this package's parse helpers are run under, as S8's
// writers run a getter under OPENV_S8_PARSE_PROBE.
const (
	probe      = "OPENV_S8_PARSE_PROBE"
	probeUnset = probe + "_UNSET"
)

// renderBool shows a boolean read as S8's writers do: default when the read
// returned each fallback it was given, else the value both calls returned.
func renderBool(withFalse, withTrue bool) string {
	switch {
	case !withFalse && withTrue:
		return "default"
	case withFalse == withTrue:
		return render(withFalse)
	}
	return "unrenderable: the fallback inverted the result"
}

// helperSections are the sections of env_parse.txt whose getter this
// package reproduces with one parse helper, each run the way S8's writer
// runs that getter: under a probe name, with a sentinel fallback shown as
// default.
var helperSections = map[string]func(parseRow) string{
	"cmd/server:envOr(key)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).text(probe, "fallback"), "fallback")
	},
	"cmd/server:envInt(key)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).count(probe, 42), 42)
	},
	"cmd/server:envBool(key)": func(r parseRow) string {
		c := probeConfig(probe, r)
		return renderBool(c.boolean(probe, false), c.boolean(probe, true))
	},
	"cmd/server:envSwitch(key)": func(r parseRow) string {
		c := probeConfig(probe, r)
		return renderBool(c.onOff(probe, false), c.onOff(probe, true))
	},
	"cmd/server:envSecret(key)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).secret(probe, "fallback"), "fallback")
	},
	"internal/hosting:envOr(key)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).text(probe, "fallback"), "fallback")
	},
	"internal/notify:envDefault(key)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).text(probe, "fallback"), "fallback")
	},
	"internal/notify:envSecret(key)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).secret(probe, ""), "\x00no fallback")
	},
	"internal/notify:typeListFromEnv(key)": func(r parseRow) string {
		fallback := func() []string { return []string{"fallback"} }
		return renderParse(probeConfig(probe, r).typeList(probe, fallback), fallback())
	},
	"internal/api:newRateLimiterFromEnv(burstVar)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r, probeUnset).rateLimit(probe, probeUnset, 42, 4.5).Burst, 42)
	},
	"internal/api:newRateLimiterFromEnv(refillVar)": func(r parseRow) string {
		return renderParse(probeConfig(probe, r, probeUnset).rateLimit(probeUnset, probe, 42, 4.5).RefillPerHour, 4.5)
	},
	"internal/domain/users:envDuration(name) OPENV_SESSION_MAX_AGE": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).sessionLifetime(probe, users.DefaultSessionMaxAge), users.DefaultSessionMaxAge)
	},
	"internal/domain/users:envDuration(name) OPENV_SESSION_IDLE": func(r parseRow) string {
		return renderParse(probeConfig(probe, r).sessionLifetime(probe, users.DefaultSessionIdle), users.DefaultSessionIdle)
	},
	// What Load records is what the standard readers return.
	"os.Getenv": func(r parseRow) string {
		return render(probeConfig(probe, r).getenv(probe))
	},
	"os.LookupEnv": func(r parseRow) string {
		if v, ok := probeConfig(probe, r).lookup(probe); ok {
			return render(v)
		}
		return "unset"
	},
}

// notRerun are the sections of env_parse.txt that pin reads the server does
// not make, by key prefix, with the reason.
var notRerun = map[string]string{
	// From X10b on: this package's TestEnvParse writes these sections by
	// running these very helpers, and TestAccessorsRerunS8 reruns them per
	// accessor.
	"internal/config:": "this package's own parse helpers, written by its TestEnvParse",
	"cmd/agentd:":      agentdSetting,
	"cmd/openv-mcp:":   mcpSetting,
	`!=""`:             runnerRead + " (only the runner's probes compare a value with \"\")",
}

// TestParseHelpersRerunS8 reruns env_parse.txt's sections of the getters
// this package reproduces against its own parse helpers, exactly as S8's
// writers ran the getters (refactor plan S8, "which X10a runs again"). Every
// section is rerun here, or per variable by TestAccessorsRerunS8, or named
// in notRerun.
func TestParseHelpersRerunS8(t *testing.T) {
	quietLog(t)
	sections := readParseTable(t)
	perVariable := map[string]bool{}
	for _, ac := range accessorCases() {
		perVariable[sectionKey(sections, ac)] = true
	}
	for key, rows := range sections {
		run, ok := helperSections[key]
		if !ok {
			skipped := perVariable[key]
			for prefix := range notRerun {
				skipped = skipped || strings.HasPrefix(key, prefix)
			}
			if !skipped {
				t.Errorf("env_parse.txt [%s] is rerun nowhere: give its getter a helper in helperSections, "+
					"a variable's accessor case, or a reason in notRerun", key)
			}
			continue
		}
		for _, r := range rows {
			if got := run(r); got != r.result {
				t.Errorf("[%s] %s: %s, S8 pinned %s", key, r.shown, got, r.result)
			}
		}
	}
	for key := range helperSections {
		base, name, _ := strings.Cut(key, " ")
		moved := strings.TrimSpace(movedReads[base] + " " + name)
		if _, ok := sections[key]; !ok && (movedReads[base] == "" || sections[moved] == nil) {
			t.Errorf("helperSections reruns [%s], which env_parse.txt no longer has, nor the section [%s] its read moves to", key, moved)
		}
	}
}

// sectionKey is the section of env_parse.txt that pins ac's read: the read
// column's own, or the variable's where the parse depends on it; once X10b
// moves the read into this package (movedReads) and the section with it,
// the getter's section it moved to.
func sectionKey(sections map[string][]parseRow, ac accessorCase) string {
	for _, read := range []string{ac.read, movedReads[ac.read]} {
		if read == "" {
			continue
		}
		if _, ok := sections[read]; ok {
			return read
		}
		if _, ok := sections[read+" "+ac.name]; ok {
			return read + " " + ac.name
		}
	}
	return ac.read + " " + ac.name
}

// TestAccessorsRerunS8 reruns, for each accessor case, the section of
// env_parse.txt that pins its read, with the variable set to each input
// (and the case's condition set): the accessor's value must be the
// section's, its own default where the section says default. Where the
// accessor parses further than the read, the read is what is compared, and
// the accessor must still take every input without panicking.
func TestAccessorsRerunS8(t *testing.T) {
	quietLog(t)
	sections := readParseTable(t)
	for _, ac := range accessorCases() {
		key := sectionKey(sections, ac)
		rows, ok := sections[key]
		if !ok {
			t.Errorf("%s: env_parse.txt has no section [%s] nor [%s]", ac.accessor, ac.read, key)
			continue
		}
		get, def := ac.get, ac.def
		if ac.pinned != nil {
			get, def = ac.pinned, ac.pinned(loadFrom(ac.with))
		}
		for _, r := range rows {
			c := loadFrom(with(ac.with, ac.name, r))
			want := r.result
			if want == "default" {
				want = render(def)
			}
			if got := render(get(c)); got != want {
				t.Errorf("%s with %s=%s: %s, S8 pinned %s ([%s] %s)", ac.accessor, ac.name, r.shown, got, want, key, r.result)
			}
			_ = ac.get(c)
		}
	}
}

// TestLoadRecordsTheLookup pins Load: it asks the lookup for every name, once
// each and in names' order, reads nothing else (an environment variable the
// lookup does not return is unset), and records the value and whether it
// was set exactly.
func TestLoadRecordsTheLookup(t *testing.T) {
	t.Setenv("PORT", "9999")
	var asked []string
	c := Load(func(name string) (string, bool) {
		asked = append(asked, name)
		switch name {
		case "OPENV_LIMITS":
			return "", true
		case "DB_PASSWORD":
			return " p\n", true
		}
		return "", false
	})
	if strings.Join(asked, ",") != strings.Join(names, ",") {
		t.Errorf("Load asked for %v, want names in order %v", asked, names)
	}
	if got := c.Port(); got != "8080" {
		t.Errorf("Port() = %q with PORT set in the process but not by the lookup, want the default 8080: Load must read only through its lookup", got)
	}
	if v, ok := c.lookup("OPENV_LIMITS"); v != "" || !ok {
		t.Errorf("OPENV_LIMITS recorded as %q, %v; want set and empty", v, ok)
	}
	if v, ok := c.lookup("DB_PASSWORD"); v != " p\n" || !ok {
		t.Errorf("DB_PASSWORD recorded as %q, %v; want exactly as the lookup returned it", v, ok)
	}
	if v, ok := c.lookup("PORT"); v != "" || ok {
		t.Errorf("PORT recorded as %q, %v; want unset", v, ok)
	}
	defer func() {
		if recover() == nil {
			t.Error("reading a variable Load does not read did not panic")
		}
	}()
	c.getenv("OPENV_NOT_A_SETTING")
}

// TestConfigIsTheSameEachRead pins that an accessor read twice returns the
// same value: Config never changes after Load.
func TestConfigIsTheSameEachRead(t *testing.T) {
	quietLog(t)
	c := loadFrom(map[string]string{"OPENV_SESSION_IDLE": "12h", "OPENV_RUN_MAX_ATTEMPTS": "7", "OPENV_LIMITS": `{"max_projects":7}`})
	if a, b := c.SessionPolicy(), c.SessionPolicy(); a != b || a.Idle != 12*time.Hour {
		t.Errorf("SessionPolicy() = %+v then %+v", a, b)
	}
	if a, b := c.RunMaxAttempts(), c.RunMaxAttempts(); a != b || a != 7 {
		t.Errorf("RunMaxAttempts() = %d then %d", a, b)
	}
}

// TestNamesIsOnlyRangedOver pins what lets S8's inventory name every
// variable Load reads once cmd/server hands it os.LookupEnv: production
// code uses names only as the operand of a range statement (the scan
// resolves the element of such a table, and of no other).
func TestNamesIsOnlyRangedOver(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	idents, ranged := 0, 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.RangeStmt:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "names" {
					ranged++
				}
			case *ast.Ident:
				if x.Name == "names" {
					idents++
				}
			}
			return true
		})
	}
	// Every identifier spelled names is the declaration or a range operand.
	if ranged != 1 || idents != ranged+1 {
		t.Errorf("names appears %d times beside its declaration, %d of them as a range statement's operand; "+
			"production code may only range over it", idents-1, ranged)
	}
}
