package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestEnvScanForms proves the env scan (refactor plan S8) on a fixture
// module: the getter forms it resolves (a wrapper of a wrapper, a constant
// expression, a function-typed parameter bound to os.Getenv, two names with
// their defaults, a range over a constant table, a comparison, a template
// os.ExpandEnv expands, a getter method called directly) and the ones it
// refuses (a local assigned twice, a package variable, a getter fed a
// non-constant, a getter that reassigns its name parameter or takes its
// address, a function literal's parameter, a mutable table, an env reader, a
// getter or an interface method a getter implements used as a value, a call
// through such an interface method, a getter no followed call reaches, a
// read at package initialisation, the whole environment without an
// exemption, stale exemptions, and reads in a file the typed build leaves
// out).
func TestEnvScanForms(t *testing.T) {
	files := map[string]string{
		"go.mod":                      "module example.com/fx\n\ngo 1.25\n",
		"internal/lib/lib.go":         envFixtureLib,
		"internal/lib/lib_windows.go": envFixtureWindows,
		"cmd/tool/main.go":            envFixtureTool,
	}
	root := writeFixture(t, files)
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	prog := envFixtureProgram(t, root, [][2]string{
		{"example.com/fx/internal/lib", "internal/lib/lib.go"},
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
	wantRows := []string{
		"APP_COUNT\tinternal/lib:Int(key)\t3",
		"APP_LOADED\tinternal/lib:Load(getenv)\t-",
		"APP_TABLE_B\tos.Getenv !=\"\"\t-",
		"BURST\tinternal/lib:Pair(burstVar)\t5",
		"COMPUTED\tinternal/lib:Env(key)\t(computed)",
		"DIRECT\tinternal/lib:OSSource.Get(key)\t-",
		"EXP_A\tos.ExpandEnv\t-",
		"EXP_B\tos.ExpandEnv\t-",
		"FLAG\tos.Getenv ==\"true\"\t-",
		"LOADED\tinternal/lib:Load(getenv)\t-",
		"PLAIN\tinternal/lib:Env(key)\t\"dflt\"",
		"REFILL\tinternal/lib:Pair(refillVar)\t2.5",
		"RETENTION\tinternal/lib:Dur(key)\t1h30m0s",
		"TABLE_A\tos.Getenv !=\"\"\t-",
	}
	if got, want := strings.Join(rows, "\n"), strings.Join(wantRows, "\n"); got != want {
		t.Errorf("rows:\n%s\nwant:\n%s", got, want)
	}
	exemptions := []envExemption{
		{id: "fx-environ", kind: "environ", at: []string{"internal/lib:Bad"}},
		{id: "fx-place", kind: "placement", names: []string{"PLAIN", "GONE"}, at: []string{"cmd/tool:main"}},
		{id: "fx-stale", kind: "unresolved", at: []string{"internal/lib:Nowhere"}},
	}
	bad := append(res.judge(prog.fset, exemptions), envHoles(m, prog.typed, res.getters)...)
	sort.Strings(bad)
	at := func(file, marker string) string {
		return fmt.Sprintf("%s:%d", file, envFixtureLine(t, files[file], marker))
	}
	lib, tool, win := "internal/lib/lib.go", "cmd/tool/main.go", "internal/lib/lib_windows.go"
	want := []string{
		at(lib, "twice") + ": os.Getenv in internal/lib:Bad: the variable's name does not resolve: the local k is assigned 2 times",
		at(lib, "pkgvar") + ": os.Getenv in internal/lib:Bad: the variable's name does not resolve: the package variable name",
		at(lib, "fed") + ": internal/lib:Env(key) in internal/lib:Bad: the variable's name does not resolve: the expression strings.ToUpper(s)",
		at(lib, "mutable") + ": os.Getenv in internal/lib:Mutable: the variable's name does not resolve: the range variable k over mutable, which is used other than by range statements",
		at(lib, "reassigned") + ": os.Getenv in internal/lib:Alias: the variable's name does not resolve: the parameter key is reassigned",
		at(lib, "param-addressed") + ": os.Getenv in internal/lib:Pinned: the variable's name does not resolve: the parameter key has its address taken",
		at(lib, "closure-param") + ": os.Getenv in internal/lib:Bad: the variable's name does not resolve: the parameter k of a function literal, whose calls the scan does not follow",
		at(lib, "iface-only") + ": the getter internal/lib:hiddenSource.Get reads the environment, but no call of it is followed: a call through" +
			" an interface or a function value hides the names it reads (call it directly), and a getter nothing calls goes",
		at(tool, "iface-call") + ": lib.Source.Get in cmd/tool:main: the variable's name does not resolve: the call goes through an interface method" +
			" that the getters internal/lib:OSSource.Get, internal/lib:hiddenSource.Get implement, so the scan cannot tell which function reads the name: call the getter directly",
		at(tool, "iface-new") + ": lib.Source.Get in cmd/tool:main: the variable's name does not resolve: the call goes through an interface method" +
			" that the getters internal/lib:OSSource.Get, internal/lib:hiddenSource.Get implement, so the scan cannot tell which function reads the name: call the getter directly",
		at(tool, "iface-value") + ": src.Get is used as a value, which the scan cannot follow (an escape)",
		at(lib, "escape") + ": os.Getenv is used as a value, which the scan cannot follow (an escape)",
		at(lib, "init") + ": os.Getenv is called outside a function declaration, where the scan does not look",
		at(tool, "getter-value") + ": lib.Env is used as a value, which the scan cannot follow (an escape)",
		at(win, "win-read") + ": os.Getenv in a file this build of the scan leaves out (" + envBuildNote + ")",
		at(win, "win-read") + ": a call of the getter Env in a file this build of the scan leaves out (" + envBuildNote + ")",
		"exemption fx-place (placement) names GONE, which none of cmd/tool:main reads: a stale entry goes",
		"exemption fx-stale (unresolved) matches 0 reads in internal/lib:Nowhere, not exactly one: a stale exemption goes, a second read needs its own reasoning",
	}
	sort.Strings(want)
	if got, w := strings.Join(bad, "\n"), strings.Join(want, "\n"); got != w {
		t.Errorf("violations:\n%s\nwant:\n%s", got, w)
	}
}

// envFixtureProgram type-checks fixture packages, each an import path and
// the one file under root that holds it, dependencies first, with the
// standard library from the module's own go list.
func envFixtureProgram(t *testing.T, root string, pkgs [][2]string) *envProgram {
	t.Helper()
	dir, err := os.Getwd()
	for err == nil {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			break
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = filepath.Dir(dir)
	}
	l, err := listEnvPackages(dir)
	if err != nil {
		t.Fatal(err)
	}
	prog := &envProgram{fset: token.NewFileSet(), bins: map[string][]string{}, typed: map[string]bool{}}
	im := newEnvImporter(prog.fset, l)
	for _, pkg := range pkgs {
		path, rel := pkg[0], pkg[1]
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(prog.fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		p, err := im.check(prog.fset, path, filepath.ToSlash(filepath.Dir(rel)), []*ast.File{af})
		if err != nil {
			t.Fatal(err)
		}
		prog.pkgs = append(prog.pkgs, p)
		prog.typed[rel] = true
	}
	return prog
}

// envFixtureLine is the line of the fixture that ends with // <marker>.
func envFixtureLine(t *testing.T, src, marker string) int {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		if strings.HasSuffix(line, "// "+marker) {
			return i + 1
		}
	}
	t.Fatalf("no line ends with // %s", marker)
	return 0
}

const envFixtureLib = `package lib

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const Prefix = "APP_"

func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Int(key string, fallback int) int {
	if n, err := strconv.Atoi(Env(key, "")); err == nil {
		return n
	}
	return fallback
}

func Pair(burstVar, refillVar string, burst int, refill float64) (int, float64) {
	_ = os.Getenv(burstVar)
	_, _ = os.LookupEnv(refillVar)
	return burst, refill
}

func Dur(key string, d time.Duration) time.Duration {
	_ = Env(key, "")
	return d
}

func Load(getenv func(string) string) string { return getenv("LOADED") + getenv(Prefix+"LOADED") }

var authKeys = []string{"TABLE_A", Prefix + "TABLE_B"}

func AnyAuth() bool {
	for _, k := range authKeys {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

var mutable = []string{"MUT"}

func Mutate() { mutable[0] = "OTHER" }

func Mutable() {
	for _, k := range mutable {
		_ = os.Getenv(k) // mutable
	}
}

func Alias(key string) string {
	if key == "OLD" {
		key = "NEW"
	}
	return os.Getenv(key) // reassigned
}

func Pinned(key string) string {
	p := &key
	_ = p
	return os.Getenv(key) // param-addressed
}

type Source interface{ Get(key string) string }

type OSSource struct{}

func (OSSource) Get(key string) string { return os.Getenv(key) }

type hiddenSource struct{}

func (hiddenSource) Get(key string) string { return Env(key, "") } // iface-only

func NewSource() Source { return hiddenSource{} }

var name = "PKGVAR"

func Bad(flag bool, s string) {
	k := "ONE"
	if flag {
		k = "TWO"
	}
	_ = os.Getenv(k) // twice
	_ = os.Getenv(name) // pkgvar
	_ = Env(strings.ToUpper(s), "") // fed
	get := os.Getenv // escape
	_ = get
	_ = os.Environ()
	_ = os.ExpandEnv("$EXP_A/${EXP_B}")
	_ = os.Getenv("FLAG") == "true"
	lit := func(k string) string {
		if k == "" {
			k = "DEF"
		}
		return os.Getenv(k) // closure-param
	}
	_ = lit("X")
}

var atInit = os.Getenv("AT_INIT") // init
`

const envFixtureWindows = `package lib

import "os"

func winHome() string { return os.Getenv("USERPROFILE") + Env("WIN", "") } // win-read
`

const envFixtureTool = `package main

import (
	"os"
	"time"

	"example.com/fx/internal/lib"
)

const retention = 90 * time.Minute

func use(f func(string, string) string) {}

func main() {
	_ = lib.Env("PLAIN", "dflt")
	_ = lib.Int(lib.Prefix+"COUNT", 3)
	lib.Pair("BURST", "REFILL", 5, 2.5)
	_ = lib.Dur("RETENTION", retention)
	_ = lib.Load(os.Getenv)
	_ = lib.AnyAuth()
	lib.Mutable()
	_ = lib.Env("COMPUTED", os.Args[0])
	use(lib.Env) // getter-value
	_ = lib.Alias("OLD")
	_ = lib.Pinned("PINNED")
	_ = lib.OSSource{}.Get("DIRECT")
	var src lib.Source = lib.OSSource{}
	_ = src.Get("HIDDEN") // iface-call
	_ = lib.NewSource().Get("ALSO_HIDDEN") // iface-new
	get := src.Get // iface-value
	_ = get
}
`
