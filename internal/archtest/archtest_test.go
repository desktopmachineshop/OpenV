// Package archtest holds the backend's architecture tests: the frozen import
// graph, the layering rules, size budgets, bans and count ratchets that keep
// the refactor plan's conventions from regressing. It has no production code.
// Every ceiling and allowlist lives in ratchets.json; README.md explains each
// rule, why it exists and how to fix a failure.
package archtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode"
)

const (
	ratchetsFile  = "ratchets.json"
	regenerateCmd = "UPDATE_RATCHETS=1 go test ./internal/archtest"
	readmeURL     = "internal/archtest/README.md"
)

// rule is one architecture check. key names the ratchets.json entry it reads
// and tightens ("" for a ban); allow is what a failure says about it.
type rule struct {
	name   string
	anchor string
	key    string
	allow  string
	run    func(c *check)
}

func inRatchets(key, how string) string {
	return fmt.Sprintf("%q in internal/archtest/ratchets.json (%s)", key, how)
}

const (
	setHow     = "entries may be removed, never added"
	ceilingHow = "a ceiling that may only fall"
	banText    = "none: this is a ban, so change the code"
)

var rules = []rule{
	{"import edges", "import-edges", "import_edges", inRatchets("import_edges", setHow), checkImportEdges},
	{"K7 layering", "k7-layering", "layering_exceptions", inRatchets("layering_exceptions", setHow), checkLayering},
	{"client binaries", "client-binaries", "client_domain_deps", inRatchets("client_domain_deps", setHow), checkClientBinaries},
	{"domain reflection", "domain-reflection", "domain_reflect", inRatchets("domain_reflect", setHow), checkDomainReflect},
	{"K14 file size", "k14-file-size", "file_lines", inRatchets("file_lines", "grandfathered files only; ceilings may only fall"), checkFileSizes},
	{"K14 function size", "k14-function-size", "func_lines", inRatchets("func_lines", "grandfathered functions only; ceilings may only fall"), checkFuncSizes},
	{"no init functions", "no-init-functions", "", banText, checkNoInit},
	{"side-effecting package variables", "side-effecting-package-variables", "side_effect_vars", inRatchets("side_effect_vars", setHow), checkPackageVars},
	{"one router", "one-router", "", banText, checkOneRouter},
	{"HandleFunc outside registrars", "handlefunc-outside-registrars", "counts.handle_func_outside_registrars", inRatchets("counts.handle_func_outside_registrars", ceilingHow), checkHandleFuncOutsideRegistrars},
	{"Handler literals in tests", "handler-literals-in-tests", "counts.handler_literals_in_tests", inRatchets("counts.handler_literals_in_tests", ceilingHow), checkHandlerLiterals},
	{"raw JSON encodes", "raw-json-encodes", "counts.raw_json_encodes", inRatchets("counts.raw_json_encodes", ceilingHow), checkRawEncodes},
	{"invalid request body literals", "invalid-request-body-literals", "counts.invalid_request_body_literals", inRatchets("counts.invalid_request_body_literals", ceilingHow), checkInvalidBodyLiterals},
	{"require helpers outside authz", "require-helpers-outside-authz", "counts.require_outside_authz", inRatchets("counts.require_outside_authz", ceilingHow), checkRequireOutsideAuthz},
	{"direct env reads", "direct-env-reads", "env_reads", inRatchets("env_reads", "a ceiling per package that may only fall; a package not listed may read none"), checkEnvReads},
	{"R8 decode errors", "r8-decode-errors", "", "none: the protected types are decodeAliasTypes in decode_test.go, a list that only grows", checkDecodeAliases},
	{"build context", "build-context", "", "none: the Dockerfile's COPY list is the allowlist", checkBuildContext},
}

// TestArchitecture runs every rule against the module and compares the
// result with ratchets.json. UPDATE_RATCHETS=1 rewrites the file with every
// entry tightened to the tree, and refuses when a rule fails, so it can only
// lower or remove entries. UPDATE_RATCHETS=bootstrap creates a missing file.
func TestArchitecture(t *testing.T) {
	m, err := loadModule()
	if err != nil {
		t.Fatal(err)
	}
	mode := os.Getenv("UPDATE_RATCHETS")
	stored, err := readRatchets(ratchetsFile)
	switch {
	case mode == "bootstrap" && !errors.Is(err, fs.ErrNotExist):
		t.Fatalf("UPDATE_RATCHETS=bootstrap only creates a missing %s; to lower or remove entries run %s", ratchetsFile, regenerateCmd)
	case mode == "bootstrap":
		stored = &ratchets{}
		stored.normalise()
	case err != nil:
		t.Fatalf("%v\n%s holds every ceiling and allowlist (%s#ratchetsjson); restore it from git", err, ratchetsFile, readmeURL)
	}
	next := stored.clone()
	next.About = aboutText
	ok := true
	for _, r := range rules {
		ok = t.Run(r.name, func(t *testing.T) {
			c := &check{t: t, rule: r, m: m, stored: stored, next: next, bootstrap: mode == "bootstrap"}
			r.run(c)
			c.report()
		}) && ok
	}
	writeRatchets(t, mode, ok, stored, next)
}

// check collects one rule's findings and turns them into a single failure
// that names the rule, the allowlist, the regenerate command and the README.
type check struct {
	t         *testing.T
	rule      rule
	m         *module
	stored    *ratchets
	next      *ratchets
	bootstrap bool
	bad       []string
	loose     []string
}

// violation records something the rule forbids.
func (c *check) violation(format string, args ...any) {
	c.bad = append(c.bad, fmt.Sprintf(format, args...))
}

// tighten records an entry that can be lowered or removed. That is allowed,
// so it is logged, not failed; the regenerate command writes it.
func (c *check) tighten(format string, args ...any) {
	c.loose = append(c.loose, fmt.Sprintf(format, args...))
}

// baseline logs what the rule measured (visible with go test -v).
func (c *check) baseline(format string, args ...any) {
	c.t.Logf("baseline: %s", fmt.Sprintf(format, args...))
}

func (c *check) report() {
	if len(c.loose) > 0 {
		sort.Strings(c.loose)
		c.t.Logf("%s can be tightened (allowed; %s writes it):\n  - %s",
			c.rule.key, regenerateCmd, strings.Join(c.loose, "\n  - "))
	}
	if len(c.bad) > 0 {
		c.t.Error(c.failure())
	}
}

// TestReadmeHasEveryRule keeps the README sections that failures point to
// in step with the rules.
func TestReadmeHasEveryRule(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			anchors[githubAnchor(h)] = true
		}
	}
	want := []string{"ratchetsjson", "regenerating"}
	for _, r := range rules {
		want = append(want, r.anchor)
	}
	for _, a := range want {
		if !anchors[a] {
			t.Errorf("README.md has no section for #%s", a)
		}
	}
}

// githubAnchor is the anchor GitHub gives a heading: lower case, spaces
// as hyphens, punctuation other than hyphens and underscores dropped.
func githubAnchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (c *check) failure() string {
	var b strings.Builder
	fmt.Fprintf(&b, "archtest rule %q failed: %d violation(s)\n", c.rule.name, len(c.bad))
	for _, v := range c.bad {
		fmt.Fprintf(&b, "  - %s\n", strings.ReplaceAll(v, "\n", "\n    "))
	}
	fmt.Fprintf(&b, "Allowlist: %s\n", c.rule.allow)
	fmt.Fprintf(&b, "Regenerate: %s (it only lowers or removes entries; it refuses to raise or add one)\n", regenerateCmd)
	fmt.Fprintf(&b, "README: %s#%s", readmeURL, c.rule.anchor)
	return b.String()
}
