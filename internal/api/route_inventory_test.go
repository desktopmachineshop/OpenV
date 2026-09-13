package api

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// The HTTP surface is a compatibility promise (REQ-143): the API stays
// backward compatible from one stable release to the next, and a removal is
// announced in two consecutive stable releases before it happens. This test
// pins the registered routes to testdata/routes.txt so that a route cannot
// disappear by accident. Adding a route is fine and only needs the file
// regenerated (UPDATE_ROUTES=1 go test ./internal/api -run TestRouteInventory);
// removing one fails until the file is regenerated deliberately, which is
// the moment to write the deprecation note in RELEASE_NOTES.md.
func TestRouteInventoryIsBackwardCompatible(t *testing.T) {
	h := &Handler{}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	var lines []string
	err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return nil // a subrouter or matcher without a path
		}
		methods, err := route.GetMethods()
		if err != nil || len(methods) == 0 {
			methods = []string{"ANY"}
		}
		for _, m := range methods {
			lines = append(lines, m+" "+path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(lines)
	lines = dedupe(lines)
	current := strings.Join(lines, "\n") + "\n"

	file := filepath.Join("testdata", "routes.txt")
	if os.Getenv("UPDATE_ROUTES") != "" {
		if err := os.WriteFile(file, []byte(current), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		return
	}
	pinned, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_ROUTES=1 to create it)", file, err)
	}
	if bytes.Equal(pinned, []byte(current)) {
		return
	}
	have := map[string]bool{}
	for _, l := range lines {
		have[l] = true
	}
	var removed []string
	for _, l := range strings.Split(strings.TrimSpace(string(pinned)), "\n") {
		if l != "" && !have[l] {
			removed = append(removed, l)
		}
	}
	if len(removed) > 0 {
		t.Fatalf("routes removed from the API surface:\n  %s\nA removal must be announced in two consecutive stable releases first (REQ-143). "+
			"Once it is, regenerate with UPDATE_ROUTES=1 go test ./internal/api -run TestRouteInventory",
			strings.Join(removed, "\n  "))
	}
	t.Fatalf("routes were added; regenerate the inventory with UPDATE_ROUTES=1 go test ./internal/api -run TestRouteInventory")
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, l := range sorted {
		if i == 0 || l != sorted[i-1] {
			out = append(out, l)
		}
	}
	return out
}

// TestRouteInventoryFileIsSorted keeps the file diff-friendly.
func TestRouteInventoryFileIsSorted(t *testing.T) {
	pinned, err := os.ReadFile(filepath.Join("testdata", "routes.txt"))
	if err != nil {
		t.Skip("no inventory yet")
	}
	lines := strings.Split(strings.TrimSpace(string(pinned)), "\n")
	if !sort.StringsAreSorted(lines) {
		t.Fatalf("testdata/routes.txt is not sorted: %s", fmt.Sprint(lines[:3]))
	}
}
