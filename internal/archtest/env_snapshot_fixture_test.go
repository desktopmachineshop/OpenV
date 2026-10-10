package archtest

import (
	"sort"
	"strings"
	"testing"
)

// TestEnvScanSnapshot proves the env scan's snapshot rule (envSnapshots,
// refactor plan X10) on a fixture module with internal/config's shape: Load
// records a table through a function-typed parameter, Config.lookup reads
// one recorded variable back, the helpers read through it with a default,
// and Config.getenv is handed as a value to a function that takes a getenv,
// as Config.Billing hands it to billing.ConfigFromEnv. While nothing binds
// Load's parameter, the snapshot reads nothing and the bound function's
// rows are the ones its other caller gives it. Once a call hands Load
// os.LookupEnv, the read of each accessor used outside the package, called
// or handed on as a value, and of what it calls in turn (URL reads PORT), is
// a row through the helper it calls, with the default it passes; an
// accessor nothing outside uses (Raw) reads nothing; Load's reads of its
// table are no rows; and the value reader binds as os.Getenv does, which is
// no escape.
func TestEnvScanSnapshot(t *testing.T) {
	scan := func(t *testing.T, tool string) ([]string, []string) {
		t.Helper()
		files := map[string]string{
			"go.mod":                    "module example.com/fx\n\ngo 1.25\n",
			"internal/bill/bill.go":     envSnapshotBill,
			"internal/config/config.go": envSnapshotConfig,
			"cmd/tool/main.go":          tool,
		}
		root := writeFixture(t, files)
		prog := envFixtureProgram(t, root, [][2]string{
			{"example.com/fx/internal/bill", "internal/bill/bill.go"},
			{"example.com/fx/internal/config", "internal/config/config.go"},
			{"example.com/fx/cmd/tool", "cmd/tool/main.go"},
		})
		res, err := newEnvScan(prog).run()
		if err != nil {
			t.Fatal(err)
		}
		var rows []string
		for _, r := range res.rows {
			rows = append(rows, r.name+"\t"+r.read+"\t"+r.def)
		}
		sort.Strings(rows)
		return rows, res.judge(prog.fset, nil)
	}
	check := func(t *testing.T, got, want []string, what string) {
		t.Helper()
		if g, w := strings.Join(got, "\n"), strings.Join(want, "\n"); g != w {
			t.Errorf("%s:\n%s\nwant:\n%s", what, g, w)
		}
	}

	t.Run("unbound", func(t *testing.T) {
		rows, bad := scan(t, envSnapshotToolUnbound)
		check(t, rows, []string{
			"BILL_KEY\tinternal/bill:FromEnv(getenv)\t-",
			"DIRECT\tos.Getenv\t-",
		}, "rows")
		check(t, bad, nil, "violations")
	})
	t.Run("bound", func(t *testing.T) {
		rows, bad := scan(t, envSnapshotToolBound)
		check(t, rows, []string{
			"BILL_KEY\tinternal/bill:FromEnv(getenv)\t-",
			"CORS\tinternal/config:Config.text(name)\t\"http://localhost\"",
			"DIRECT\tos.Getenv\t-",
			"PORT\tinternal/config:Config.text(name)\t\"8080\"",
			"URL\tinternal/config:Config.text(name)\t(computed)",
		}, "rows")
		check(t, bad, nil, "violations")
	})
}

const envSnapshotBill = `package bill

// FromEnv reads its settings through the getenv it is handed.
func FromEnv(getenv func(string) string) string { return getenv("BILL_KEY") }
`

const envSnapshotConfig = `package config

import "example.com/fx/internal/bill"

var names = []string{"PORT", "CORS", "URL", "RAW", "BILL_KEY"}

type setting struct {
	raw string
	set bool
}

type Config struct{ vars map[string]setting }

func Load(lookup func(string) (string, bool)) *Config {
	c := &Config{vars: map[string]setting{}}
	for _, name := range names {
		raw, set := lookup(name)
		c.vars[name] = setting{raw: raw, set: set}
	}
	return c
}

func (c *Config) lookup(name string) (string, bool) {
	s := c.vars[name]
	return s.raw, s.set
}

func (c *Config) getenv(name string) string {
	raw, _ := c.lookup(name)
	return raw
}

func (c *Config) text(name, def string) string {
	if v := c.getenv(name); v != "" {
		return v
	}
	return def
}

func (c *Config) Port() string { return c.text("PORT", "8080") }
func (c *Config) CORS() string { return c.text("CORS", "http://localhost") }
func (c *Config) URL() string  { return c.text("URL", "http://localhost:"+c.Port()) }
func (c *Config) Raw() string  { return c.getenv("RAW") }
func (c *Config) Bill() string { return bill.FromEnv(c.getenv) }
`

const envSnapshotToolUnbound = `package main

import (
	"os"

	"example.com/fx/internal/bill"
)

func main() {
	_ = bill.FromEnv(os.Getenv)
	_ = os.Getenv("DIRECT")
}
`

const envSnapshotToolBound = `package main

import (
	"os"

	"example.com/fx/internal/bill"
	"example.com/fx/internal/config"
)

func main() {
	_ = bill.FromEnv(os.Getenv)
	_ = os.Getenv("DIRECT")
	c := config.Load(os.LookupEnv)
	_ = c.CORS()
	url := c.URL
	_ = url()
}
`
