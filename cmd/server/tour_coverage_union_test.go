//go:build unix

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The API tour's coverage across its slices (refactor plan §6.4 S5a-S5e,
// whose exit S5e holds: coverage.txt at least 90% of the 341 routes). Each
// slice's coverage.txt (tourCoverage, tour_test.go) lists the routes its
// recorded steps and matrix cells reached; testdata/tour/coverage.txt is
// their union, in routes.txt's order, every route listed: the slices that
// reached it and those where it answered a 2xx or 3xx. TestTourCoverage
// checks it, or rewrites it under UPDATE_GOLDEN=1, as does regenerating any
// area, and fails when fewer than 90% of the routes answered a 2xx or 3xx
// in some slice, naming those that did not. A route reached by errors alone
// counts as reached but not toward the floor, so S5e's phantom-id matrix,
// which reaches nearly every route with refusals, cannot meet it by itself.

// tourFloorPercent is the plan's floor: the share of routes.txt that must
// answer a 2xx or 3xx somewhere in the tour.
const tourFloorPercent = 90

// tourUnionCoveragePath is the union's file.
var tourUnionCoveragePath = filepath.Join("testdata", "tour", "coverage.txt")

// writeTourUnionCoverage checks, or rewrites under UPDATE_GOLDEN=1, the
// union coverage file from every slice's goldens, and fails below the floor.
// The caller holds tourMu.
func writeTourUnionCoverage(t *testing.T) {
	t.Helper()
	routes := readRouteList(t)
	reachedBy, succeededBy := map[string][]string{}, map[string][]string{}
	for _, slice := range tourSlices(t) {
		_, covered, succeeded := tourCoverage(t, slice, routes)
		for r := range covered {
			reachedBy[r] = append(reachedBy[r], slice)
		}
		for r := range succeeded {
			succeededBy[r] = append(succeededBy[r], slice)
		}
	}
	got, missing := renderTourUnionCoverage(routes, reachedBy, succeededBy)
	checkGolden(t, tourUnionCoveragePath, got, tourCoverageRegenerate)
	if len(missing) > 0 && (len(routes)-len(missing))*100 < len(routes)*tourFloorPercent {
		t.Errorf("the API tour answers %d of the %d routes with a 2xx or 3xx (%.1f%%), below the plan's floor of "+
			"%d%% (refactor plan §6.4 S5a-S5e); the routes no recorded step or matrix cell got a 2xx or 3xx from:\n  %s\n"+
			"Add steps that reach them, then regenerate the areas and the coverage with:\n  %s", len(routes)-len(missing),
			len(routes), 100*float64(len(routes)-len(missing))/float64(len(routes)), tourFloorPercent,
			strings.Join(missing, "\n  "), tourCoverageRegenerate)
	}
}

// renderTourUnionCoverage renders the union file, and lists the routes with
// no 2xx or 3xx answer in any slice. A slice's routes are sorted by name.
func renderTourUnionCoverage(routes []string, reachedBy, succeededBy map[string][]string) ([]byte, []string) {
	var b strings.Builder
	fmt.Fprintf(&b, `# The routes the API tour reached, across its slices (refactor plan §6.4
# S5a-S5e), derived by TestTourCoverage (cmd/server/tour_coverage_union_test.go)
# from every slice's area goldens, with no database. Regenerate with
#   %s
# (regenerating an area rewrites it too). A line is a route of
# internal/api/testdata/routes.txt, in that file's order: the slices whose
# recorded steps or matrix cells reached it (an answer other than 401, which
# the auth middleware gives before routing), then those where it answered a
# 2xx or 3xx. A route reached with errors alone is marked "errors only", one
# no slice reached "not reached". The plan's floor, which TestTourCoverage
# holds: at least %d%% of the routes answer a 2xx or 3xx in some slice.

`, tourCoverageRegenerate, tourFloorPercent)
	var missing []string
	reached := 0
	for _, r := range routes {
		by, ok := reachedBy[r], succeededBy[r]
		switch {
		case len(by) == 0:
			fmt.Fprintf(&b, "%s: not reached\n", r)
		case len(ok) == 0:
			fmt.Fprintf(&b, "%s: %s (errors only)\n", r, strings.Join(sortedStrings(by), ", "))
		default:
			fmt.Fprintf(&b, "%s: %s; 2xx or 3xx in %s\n", r, strings.Join(sortedStrings(by), ", "),
				strings.Join(sortedStrings(ok), ", "))
		}
		if len(by) > 0 {
			reached++
		}
		if len(ok) == 0 {
			missing = append(missing, r)
		}
	}
	n := len(routes)
	fmt.Fprintf(&b, "\nthe tour: %d of %d routes reached (%.1f%%), %d with a 2xx or 3xx answer (%.1f%%); the floor is "+
		"%d%%, %d routes\n", reached, n, 100*float64(reached)/float64(n), n-len(missing),
		100*float64(n-len(missing))/float64(n), tourFloorPercent, (n*tourFloorPercent+99)/100)
	return []byte(b.String()), missing
}

func sortedStrings(list []string) []string {
	m := map[string]bool{}
	for _, s := range list {
		m[s] = true
	}
	return sortedKeys(m)
}
