package archtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

const aboutText = "Shrink-only ceilings and allowlists for internal/archtest; see README.md there. " +
	"Regenerate with " + regenerateCmd + ", which only lowers or removes entries."

// ratchets mirrors ratchets.json. Every entry is a ceiling that may only fall
// or an allowlist that may only shrink.
type ratchets struct {
	About              string              `json:"about"`
	ImportEdges        map[string][]string `json:"import_edges"`
	LayeringExceptions []string            `json:"layering_exceptions"`
	ClientDomainDeps   map[string][]string `json:"client_domain_deps"`
	DomainReflect      []string            `json:"domain_reflect"`
	FileLines          map[string]int      `json:"file_lines"`
	FuncLines          map[string]int      `json:"func_lines"`
	SideEffectVars     []string            `json:"side_effect_vars"`
	Counts             map[string]int      `json:"counts"`
	EnvReads           map[string]int      `json:"env_reads"`
}

func readRatchets(path string) (*ratchets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r ratchets
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.normalise()
	return &r, nil
}

// normalise replaces nil maps and lists with empty ones, so the file always
// shows every key and an empty list reads [] rather than null.
func (r *ratchets) normalise() {
	for _, mp := range []*map[string][]string{&r.ImportEdges, &r.ClientDomainDeps} {
		if *mp == nil {
			*mp = map[string][]string{}
		}
		for k, v := range *mp {
			if v == nil {
				(*mp)[k] = []string{}
			}
		}
	}
	for _, mp := range []*map[string]int{&r.FileLines, &r.FuncLines, &r.Counts, &r.EnvReads} {
		if *mp == nil {
			*mp = map[string]int{}
		}
	}
	for _, list := range []*[]string{&r.LayeringExceptions, &r.DomainReflect, &r.SideEffectVars} {
		if *list == nil {
			*list = []string{}
		}
	}
}

// encode renders the canonical file: two-space indent, sorted map keys (the
// rules keep lists sorted), no HTML escaping, trailing newline.
func (r *ratchets) encode() []byte {
	r.normalise()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (r *ratchets) clone() *ratchets {
	var c ratchets
	if err := json.Unmarshal(r.encode(), &c); err != nil {
		panic(err)
	}
	c.normalise()
	return &c
}

// writeRatchets finishes a run: without UPDATE_RATCHETS it only says whether
// the file could be tightened; with it, it writes the tightened file, and
// refuses when any rule failed, because that is the only way an entry could
// need raising or adding.
func writeRatchets(t *testing.T, mode string, ok bool, stored, next *ratchets) {
	before, after := stored.encode(), next.encode()
	switch mode {
	case "":
		if !bytes.Equal(before, after) {
			t.Logf("%s can be tightened: run %s", ratchetsFile, regenerateCmd)
		}
		return
	case "1", "bootstrap":
	default:
		t.Fatalf("UPDATE_RATCHETS=%q: use 1 to lower or remove entries (bootstrap only creates a missing file)", mode)
	}
	if !ok {
		t.Fatalf("UPDATE_RATCHETS=%s: %s was not written because rules failed above. Regenerating only lowers "+
			"or removes entries, never raises or adds one, so fix the code (%s#regenerating)", mode, ratchetsFile, readmeURL)
	}
	if bytes.Equal(before, after) {
		t.Logf("%s is already tight", ratchetsFile)
		return
	}
	if err := os.WriteFile(ratchetsFile, after, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", ratchetsFile)
}

// headroom is R6's allowance above a grandfathered size: 10%, at most 150
// lines, so that a feature PR touching a giant is never blocked.
func headroom(n int) int { return min(n/10, 150) }

// judgeSet checks current against an allowlist and returns the tightened
// list (the current items). why explains an item that is not allowed; gone
// words an entry that can be removed.
func (c *check) judgeSet(stored, current []string, why, gone func(string) string) []string {
	current = sortedUnique(current)
	if c.bootstrap {
		return current
	}
	allowed, have := setOf(stored), setOf(current)
	next := []string{}
	for _, it := range current {
		if allowed[it] {
			next = append(next, it)
		} else {
			c.violation("%s", why(it))
		}
	}
	for _, it := range sortedUnique(stored) {
		if !have[it] {
			c.tighten("%s", gone(it))
		}
	}
	return next
}

// judgeCeilings checks sizes against a budget. Items over it need an entry
// in stored, and may not exceed it; an entry falls to the current size plus
// headroom, and goes once its item is back within budget or gone. where,
// when set, gives each key's file:line for messages.
func (c *check) judgeCeilings(stored, current map[string]int, where map[string]string, budget int, noun string) map[string]int {
	next := map[string]int{}
	var over []string
	for _, k := range sortedKeys(current) {
		n := current[k]
		ceil, listed := stored[k]
		label := k
		if where[k] != "" {
			label = k + " (" + where[k] + ")"
		}
		switch {
		case n <= budget:
			if listed {
				c.tighten("%s is %d lines, within the %d-line budget: remove its entry", k, n, budget)
			}
			continue
		case c.bootstrap:
			next[k] = n + headroom(n)
		case !listed:
			c.violation("%s is %d lines; a %s may have at most %d, so split it. Only the %ss in %q are grandfathered, and none is added",
				label, n, noun, budget, noun, c.rule.key)
		case n > ceil:
			c.violation("%s is %d lines, above its grandfathered ceiling of %d; shrink it back under the ceiling", label, n, ceil)
			next[k] = ceil
		default:
			next[k] = min(ceil, n+headroom(n))
			if next[k] < ceil {
				c.tighten("%s: ceiling %d can fall to %d (%d lines plus headroom)", k, ceil, next[k], n)
			}
		}
		over = append(over, fmt.Sprintf("%s %d", k, n))
	}
	for _, k := range sortedKeys(stored) {
		if _, ok := current[k]; !ok {
			c.tighten("%s no longer exists: remove its entry", k)
		}
	}
	c.baseline("%d %ss over the %d-line budget: %s", len(over), noun, budget, strings.Join(over, ", "))
	return next
}

// hit is one occurrence a count ratchet counts.
type hit struct {
	pkg string // package name
	pos string // file:line
}

// judgeCount checks a count against its ceiling in the counts map (0 when
// absent) and records the tightened ceiling. next starts as a copy of the
// stored file, so a count at or above its ceiling leaves the entry as it is.
func (c *check) judgeCount(name, what string, hits []hit) {
	n := len(hits)
	c.baseline("%d %s (%d files)", n, what, len(byFile(hits)))
	ceil := c.stored.Counts[name]
	switch {
	case c.bootstrap:
		c.next.Counts[name] = n
	case n > ceil:
		c.violation("%d %s, above the ceiling of %d. By file, now:\n%s", n, what, ceil, fileCounts(hits))
	case n < ceil:
		c.tighten("%s: %d, below the ceiling of %d: lower it to %d", name, n, ceil, n)
		c.next.Counts[name] = n
	}
}

// judgePackages checks per-package counts: a package's count may not pass
// its ceiling, and a package not listed may have none.
func (c *check) judgePackages(stored map[string]int, hits []hit, what string) map[string]int {
	byPkg := map[string][]hit{}
	for _, h := range hits {
		byPkg[h.pkg] = append(byPkg[h.pkg], h)
	}
	next := map[string]int{}
	for _, p := range sortedKeys(byPkg) {
		n, ceil := len(byPkg[p]), stored[p]
		switch {
		case c.bootstrap:
			next[p] = n
		case n > ceil:
			var at []string
			for _, h := range byPkg[p] {
				at = append(at, h.pos)
			}
			c.violation("%s: %d %s, above its ceiling of %d:\n%s", p, n, what, ceil, strings.Join(at, "\n"))
			if ceil > 0 {
				next[p] = ceil
			}
		default:
			next[p] = n
			if n < ceil {
				c.tighten("%s: %d, below the ceiling of %d: lower it to %d", p, n, ceil, n)
			}
		}
	}
	for _, p := range sortedKeys(stored) {
		if _, ok := byPkg[p]; !ok {
			c.tighten("%s has none left: remove its entry", p)
		}
	}
	return next
}

func byFile(hits []hit) map[string]int {
	counts := map[string]int{}
	for _, h := range hits {
		counts[h.pos[:strings.LastIndex(h.pos, ":")]]++
	}
	return counts
}

func fileCounts(hits []hit) string {
	counts := byFile(hits)
	var lines []string
	for _, f := range sortedKeys(counts) {
		lines = append(lines, fmt.Sprintf("%s: %d", f, counts[f]))
	}
	return strings.Join(lines, "\n")
}

func sortedKeys[V any](mp map[string]V) []string {
	keys := make([]string, 0, len(mp))
	for k := range mp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnique(items []string) []string {
	out := []string{}
	for _, it := range sortedKeys(setOf(items)) {
		out = append(out, it)
	}
	return out
}

func setOf(items []string) map[string]bool {
	set := map[string]bool{}
	for _, it := range items {
		set[it] = true
	}
	return set
}
