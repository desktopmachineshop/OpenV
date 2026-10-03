package archtest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The API spec drift ratchet (refactor plan step D1). S2's route inventory,
// internal/api/testdata/routes.txt, lists every method and path the router
// serves; docs/api-spec.md documents them by hand, one table row per route.
// The rule counts the routes the inventory lists and no row documents, and
// the count may only fall.

const (
	routeInventoryFile = "internal/api/testdata/routes.txt"
	apiSpecFile        = "docs/api-spec.md"
)

// apiSpecMethodRe is an HTTP method in a row's first cell.
var apiSpecMethodRe = regexp.MustCompile(`^[A-Z]+$`)

// apiSpecAlternationRe is a brace group holding a comma, such as
// {json,csv,pdf}: a row's way of writing several literal paths at once. A
// brace group with no comma ({id}) is a path variable and stays as it is.
var apiSpecAlternationRe = regexp.MustCompile(`\{([^{}]*,[^{}]*)\}`)

// checkAPISpecRoutes counts the routes of routes.txt that docs/api-spec.md
// does not document, against the ceiling api_spec_undocumented_routes.
func checkAPISpecRoutes(c *check) {
	routes, err := os.ReadFile(filepath.Join(c.m.root, filepath.FromSlash(routeInventoryFile)))
	if err != nil {
		c.violation("cannot read the route inventory: %v", err)
		return
	}
	doc, err := os.ReadFile(filepath.Join(c.m.root, filepath.FromSlash(apiSpecFile)))
	if err != nil {
		c.violation("cannot read the API spec: %v", err)
		return
	}
	documented := apiSpecRoutes(string(doc))
	var all, missing []string
	for _, line := range strings.Split(string(routes), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		all = append(all, line)
		if !documented[line] {
			missing = append(missing, line)
		}
	}
	sort.Strings(missing)
	n := len(missing)
	c.baseline("%d of the %d routes in %s have no row in %s", n, len(all), routeInventoryFile, apiSpecFile)
	ceil := c.stored.APISpecUndocumentedRoutes
	switch {
	case c.bootstrap:
		c.next.APISpecUndocumentedRoutes = n
	case n > ceil:
		c.violation("%d routes in %s have no row in %s, above the ceiling of %d. Document each new route: a row "+
			"of a route table whose first cell names the method and whose second the path, as a code span "+
			"(| GET | `/api/v1/...` | ... |). Undocumented now:\n%s",
			n, routeInventoryFile, apiSpecFile, ceil, strings.Join(missing, "\n"))
	case n < ceil:
		c.tighten("%d undocumented routes, below the ceiling of %d: lower it to %d", n, ceil, n)
		c.next.APISpecUndocumentedRoutes = n
	}
}

// apiSpecRoutes returns the routes the spec's tables document, as
// "METHOD /path" in routes.txt's form. A row documents a route when its first
// cell holds the method (several may share a row: GET/HEAD) and its second a
// code span holding the path; a brace group with a comma in the path stands
// for each of its alternatives.
func apiSpecRoutes(doc string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		cells := strings.Split(strings.TrimSpace(line), "|")
		if len(cells) < 4 || cells[0] != "" {
			continue // not a table row: a row starts with | and has two cells or more
		}
		var methods []string
		for _, m := range strings.FieldsFunc(cells[1], func(r rune) bool { return r == '/' || r == ',' || r == ' ' }) {
			if !apiSpecMethodRe.MatchString(m) {
				methods = nil
				break
			}
			methods = append(methods, m)
		}
		path := strings.TrimSpace(cells[2])
		if len(methods) == 0 || len(path) < 3 || !strings.HasPrefix(path, "`/") || !strings.HasSuffix(path, "`") {
			continue
		}
		for _, p := range expandAlternations(strings.Trim(path, "`")) {
			for _, m := range methods {
				out[m+" "+p] = true
			}
		}
	}
	return out
}

// expandAlternations writes out every path a {a,b,c} group stands for.
func expandAlternations(path string) []string {
	loc := apiSpecAlternationRe.FindStringSubmatchIndex(path)
	if loc == nil {
		return []string{path}
	}
	var out []string
	for _, alt := range strings.Split(path[loc[2]:loc[3]], ",") {
		out = append(out, expandAlternations(path[:loc[0]]+strings.TrimSpace(alt)+path[loc[1]:])...)
	}
	return out
}

// TestAPISpecRule proves the rule on a fixture: the row forms it reads, the
// ones it does not, and the ceiling in both directions.
func TestAPISpecRule(t *testing.T) {
	root := writeFixture(t, map[string]string{
		routeInventoryFile: "DELETE /a/{id}\nGET /a\nGET /a/{id}/download/csv\nGET /a/{id}/download/pdf\nGET /b\nHEAD /b\nPOST /a\nPUT /c\n",
		apiSpecFile: "# Spec\n\n" +
			"| Method | Path | Purpose |\n|---|---|---|\n" +
			"| GET | `/a` | list |\n" +
			"| GET/HEAD | `/b` | both, \\| escaped |\n" +
			"| GET | `/a/{id}/download/{csv,pdf}` | one per format |\n" +
			"| DELETE | `/a/{name}` | another variable name is another path |\n" +
			"| POST | /a | no code span |\n" +
			"Text naming PUT `/c` outside a table.\n",
	})
	m := &module{root: root}
	got := runRule(t, m, &ratchets{APISpecUndocumentedRoutes: 2}, checkAPISpecRoutes)
	want := fmt.Sprintf("3 routes in %s have no row in %s, above the ceiling of 2.", routeInventoryFile, apiSpecFile)
	if len(got) != 1 || !strings.HasPrefix(got[0], want) || !strings.HasSuffix(got[0], "Undocumented now:\nDELETE /a/{id}\nPOST /a\nPUT /c") {
		t.Errorf("violations = %q, want %q naming DELETE /a/{id}, POST /a and PUT /c", got, want)
	}
	stored := &ratchets{APISpecUndocumentedRoutes: 5}
	stored.normalise()
	c := &check{t: t, m: m, stored: stored, next: stored.clone()}
	checkAPISpecRoutes(c)
	if len(c.bad) != 0 || c.next.APISpecUndocumentedRoutes != 3 || len(c.loose) != 1 {
		t.Errorf("at a ceiling of 5: violations %q, tightened to %d, loose %q; want none, 3 and one note",
			c.bad, c.next.APISpecUndocumentedRoutes, c.loose)
	}
}
