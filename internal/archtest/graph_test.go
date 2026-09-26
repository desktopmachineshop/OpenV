package archtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

const edgeArrow = " -> "

// checkImportEdges freezes the production import graph between the module's
// packages: an edge that is not listed fails, and a listed edge that is gone
// can be removed.
func checkImportEdges(c *check) {
	var current []string
	for _, from := range sortedKeys(c.m.edges) {
		for _, to := range c.m.edges[from] {
			current = append(current, from+edgeArrow+to)
		}
	}
	pkgs := 0
	for _, name := range c.m.names {
		if len(c.m.pkgs[name].files) > 0 {
			pkgs++
		}
	}
	c.baseline("%d packages with production code, %d internal import edges", pkgs, len(current))
	next := c.judgeSet(flatten(c.stored.ImportEdges), current,
		func(e string) string {
			return fmt.Sprintf("new import edge %s: the edge list may only shrink, so edges may be removed, never added", e)
		},
		func(e string) string { return fmt.Sprintf("edge %s is gone: remove it from import_edges", e) })
	c.next.ImportEdges = unflatten(next)
}

// typesLeaves are the types-only leaf packages outside internal/domain that
// domain code may import (K7). Each holds wire types and imports no package
// of this module. P3 creates internal/workerproto, and
// agentruns.FinishRequest becomes an alias of its type.
var typesLeaves = setOf([]string{"internal/workerproto"})

// layer places a package in the K7 layering.
func layer(name string) string {
	switch {
	case typesLeaves[name]:
		return "leaf"
	case name == rootName:
		return "root"
	case strings.HasPrefix(name, "cmd/"):
		return "cmd"
	case under(name, "internal/api"):
		return "api"
	case under(name, "internal/persistence"):
		return "persistence"
	case under(name, "internal/domain"):
		return "domain"
	default:
		return "service" // application services and infrastructure
	}
}

// layeringRule returns why an import edge breaks K7, or "".
func layeringRule(from, to string) string {
	lf, lt := layer(from), layer(to)
	switch {
	case lf == "leaf":
		return "a types-only leaf package imports no package of this module"
	case lf == "domain" && lt != "domain" && lt != "leaf":
		return "a domain package may import only other internal/domain packages and the types-only leaves in typesLeaves " +
			"(plus the standard library and third-party modules), never internal/api, internal/persistence or an application service"
	case lf == "persistence" && lt != "domain" && lt != "persistence":
		return "internal/persistence may import only internal/domain packages"
	case lf == "api" && lt == "persistence":
		return "internal/api reaches storage through domain interfaces, never internal/persistence"
	}
	return ""
}

// checkLayering applies K7 to every production edge; today's exceptions, if
// any, are listed in layering_exceptions.
func checkLayering(c *check) {
	why := map[string]string{}
	var bad []string
	for _, from := range sortedKeys(c.m.edges) {
		for _, to := range c.m.edges[from] {
			if r := layeringRule(from, to); r != "" {
				e := from + edgeArrow + to
				why[e] = r
				bad = append(bad, e)
			}
		}
	}
	c.baseline("%d production edges break K7", len(bad))
	c.next.LayeringExceptions = c.judgeSet(c.stored.LayeringExceptions, bad,
		func(e string) string { return fmt.Sprintf("%s: %s", e, why[e]) },
		func(e string) string {
			return fmt.Sprintf("%s no longer exists: remove it from layering_exceptions", e)
		})
}

// checkClientBinaries freezes the domain packages linked into each binary
// other than cmd/server: they run on operators' machines, and every server
// type they link is wire contract that deployed runners are not rebuilt for.
// A binary under cmd/ without an entry fails until one is added by hand.
func checkClientBinaries(c *check) {
	binaries := sortedKeys(c.stored.ClientDomainDeps)
	for _, name := range c.m.names {
		if layer(name) == "cmd" && name != "cmd/server" && len(c.m.pkgs[name].files) > 0 {
			binaries = append(binaries, name)
		}
	}
	next := map[string][]string{}
	for _, bin := range sortedUnique(binaries) {
		if p := c.m.pkgs[bin]; p == nil || len(p.files) == 0 {
			c.tighten("%s no longer exists: remove it from client_domain_deps", bin)
			continue
		}
		parent := linked(c.m.edges, bin)
		var doms []string
		for _, name := range sortedKeys(parent) {
			if layer(name) == "domain" {
				doms = append(doms, name)
			}
		}
		c.baseline("%s links %d domain packages: %s", bin, len(doms), strings.Join(doms, ", "))
		if _, listed := c.stored.ClientDomainDeps[bin]; !listed && !c.bootstrap {
			c.violation("new client binary %s links %d domain packages (%s): add it to client_domain_deps by hand, in review, "+
				"with the domain packages it links", bin, len(doms), strings.Join(doms, ", "))
			continue
		}
		next[bin] = c.judgeSet(c.stored.ClientDomainDeps[bin], doms,
			func(d string) string {
				return fmt.Sprintf("%s now links %s (%s); a client binary's domain packages may only shrink", bin, d, chain(parent, d))
			},
			func(d string) string { return fmt.Sprintf("%s no longer links %s: remove it from its list", bin, d) })
	}
	c.next.ClientDomainDeps = next
}

// checkDomainReflect lists the domain packages that import reflect. Domain
// code calls its collaborators through typed interfaces; X9 removes the last
// reflective call (links -> artifacts).
func checkDomainReflect(c *check) {
	var users []string
	for _, name := range c.m.names {
		if layer(name) != "domain" {
			continue
		}
		for _, f := range c.m.pkgs[name].files {
			if slices.Contains(f.paths, "reflect") {
				users = append(users, name)
				break
			}
		}
	}
	c.baseline("%d domain packages import reflect: %s", len(users), strings.Join(users, ", "))
	c.next.DomainReflect = c.judgeSet(c.stored.DomainReflect, users,
		func(p string) string {
			return fmt.Sprintf("%s imports reflect: declare a typed interface for what it calls instead", p)
		},
		func(p string) string {
			return fmt.Sprintf("%s no longer imports reflect: remove it from domain_reflect", p)
		})
}

func flatten(edges map[string][]string) []string {
	var out []string
	for _, from := range sortedKeys(edges) {
		for _, to := range edges[from] {
			out = append(out, from+edgeArrow+to)
		}
	}
	return out
}

func unflatten(items []string) map[string][]string {
	out := map[string][]string{}
	for _, it := range items {
		from, to, _ := strings.Cut(it, edgeArrow)
		out[from] = append(out[from], to)
	}
	for _, from := range sortedKeys(out) {
		slices.Sort(out[from])
	}
	return out
}

// TestLayeringRule pins K7, including the types-only leaf that domain code
// may import (P3's internal/workerproto) and that imports no module package.
func TestLayeringRule(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		broken   bool
	}{
		{"internal/domain/agentruns", "internal/domain/events", false},
		{"internal/domain/agentruns", "internal/workerproto", false},
		{"internal/api", "internal/workerproto", false},
		{"internal/runner", "internal/workerproto", false},
		{"internal/workerproto", "internal/domain/agentruns", true},
		{"internal/workerproto", "internal/runner", true},
		{"internal/domain/agentruns", "internal/runner", true},
		{"internal/domain/projects", "internal/api", true},
		{"internal/persistence/postgres", "internal/domain/users", false},
		{"internal/persistence/postgres", "internal/workerproto", true},
		{"internal/api", "internal/persistence/postgres", true},
	} {
		if got := layeringRule(tc.from, tc.to); (got != "") != tc.broken {
			t.Errorf("layeringRule(%s -> %s) = %q, want broken = %v", tc.from, tc.to, got, tc.broken)
		}
	}
}

// TestClientBinaryDiscovery proves on a fixture that a binary under cmd/
// without a client_domain_deps entry fails, that a listed binary is judged
// against its entry, and that a types-only leaf is not a domain package.
func TestClientBinaryDiscovery(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":                        "module example.com/fixture\n\ngo 1.25\n",
		"cmd/server/main.go":            "package main\n\nimport _ \"example.com/fixture/internal/api\"\n\nfunc main() {}\n",
		"cmd/agentd/main.go":            "package main\n\nimport (\n\t_ \"example.com/fixture/internal/domain/a\"\n\t_ \"example.com/fixture/internal/workerproto\"\n)\n\nfunc main() {}\n",
		"cmd/tool/main.go":              "package main\n\nimport _ \"example.com/fixture/internal/api\"\n\nfunc main() {}\n",
		"internal/api/api.go":           "package api\n\nimport _ \"example.com/fixture/internal/domain/b\"\n",
		"internal/domain/a/a.go":        "package a\n",
		"internal/domain/b/b.go":        "package b\n",
		"internal/workerproto/proto.go": "package workerproto\n",
	})
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	stored := &ratchets{ClientDomainDeps: map[string][]string{"cmd/agentd": {"internal/domain/a"}}}
	got := runRule(t, m, stored, checkClientBinaries)
	if len(got) != 1 || !strings.HasPrefix(got[0], "new client binary cmd/tool links 1 domain packages (internal/domain/b): add it to client_domain_deps by hand") {
		t.Errorf("violations = %q, want only the new client binary cmd/tool", got)
	}
}
