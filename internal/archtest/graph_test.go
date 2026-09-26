package archtest

import (
	"fmt"
	"slices"
	"strings"
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

// layer places a package in the K7 layering.
func layer(name string) string {
	switch {
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
	case lf == "domain" && lt != "domain":
		return "a domain package may import only other internal/domain packages (plus the standard library and " +
			"third-party modules), never internal/api, internal/persistence or an application service"
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
func checkClientBinaries(c *check) {
	binaries := sortedKeys(c.stored.ClientDomainDeps)
	if c.bootstrap {
		binaries = nil
		for _, name := range c.m.names {
			if layer(name) == "cmd" && name != "cmd/server" && len(c.m.pkgs[name].files) > 0 {
				binaries = append(binaries, name)
			}
		}
	}
	next := map[string][]string{}
	for _, bin := range binaries {
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
