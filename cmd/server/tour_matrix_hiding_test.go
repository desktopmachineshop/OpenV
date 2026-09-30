//go:build unix

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTourExistenceHiding holds S5e's two matrices to invariant I3's
// existence hiding, with no database (issue #379's decision 10, OpenV
// REQ-17): a resource the caller cannot reach answers exactly as one that
// does not exist, the same status and the same message. For every route
// real_id_reads.json sends with the id of a fixture of W or P in its path,
// the outsider, who reaches nothing of W, must get the very cell
// phantom_matrix.json records for it with an id no row has. The exceptions
// are the routes that do not hide what they name by design, listed in
// tourHidingExempt.
func TestTourExistenceHiding(t *testing.T) {
	phantom := tourMatrixCells(t, "phantom_matrix.json", "phantom ids, body {")
	real := tourMatrixCells(t, "real_id_reads.json", "real ids, GET", "real ids, body {")
	checked := 0
	for route, got := range real {
		want, ok := phantom[route]
		if !ok || tourHidingExempt[route] != "" {
			continue
		}
		checked++
		if got["outsider"] != want["outsider"] {
			t.Errorf("%s: the outsider's answer for a real id of W is %q, for an id no row has %q: the two must be "+
				"one answer, or the refusal tells the outsider the id exists (I3)", route, got["outsider"], want["outsider"])
		}
	}
	if checked < 100 {
		t.Fatalf("compared %d routes, fewer than the real-id matrix sends: are the goldens' sections renamed?", checked)
	}
	for route := range tourHidingExempt {
		if _, ok := real[route]; !ok {
			t.Errorf("tourHidingExempt names %s, which real_id_reads.json does not send", route)
		}
	}
}

// tourHidingExempt are the routes whose resource an outsider may reach, each
// with the reason.
var tourHidingExempt = map[string]string{
	"GET /api/v1/public/interviews/{token}":        "an interview invite opens for whoever holds its link",
	"GET /api/v1/public/interviews/{token}/stream": "an interview invite opens for whoever holds its link",
	"GET /api/v1/public/share/{token}":             "a share link opens for whoever holds it",
	"GET /api/v1/public/share/{token}/page":        "a share link opens for whoever holds it",
	"GET /api/v1/public/share/{token}/preview.png": "a share link opens for whoever holds it",
}

// tourMatrixCells reads the named sections of an S5e matrix golden: each
// row's route (its key without the query or body) mapped to its cells by
// column.
func tourMatrixCells(t *testing.T, golden string, sections ...string) map[string]map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tour", "s5e", golden))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Matrix tourMatrixRecord `json:"matrix"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("%s: %v", golden, err)
	}
	out := map[string]map[string]string{}
	found := 0
	for _, s := range g.Matrix.Sections {
		keep := false
		for _, name := range sections {
			keep = keep || s.Name == name
		}
		if !keep {
			continue
		}
		found++
		for _, row := range s.Rows {
			cells := strings.Split(row, " | ")
			if len(cells) != len(g.Matrix.Columns)+1 {
				t.Fatalf("%s: a row of %d cells for %d columns: %s", golden, len(cells)-1, len(g.Matrix.Columns), row)
			}
			route := cells[0]
			for _, cut := range []string{" ?", " {"} {
				if i := strings.Index(route, cut); i >= 0 {
					route = route[:i]
				}
			}
			byColumn := map[string]string{}
			for i, c := range g.Matrix.Columns {
				byColumn[c] = cells[i+1]
			}
			out[route] = byColumn
		}
	}
	if found != len(sections) {
		t.Fatalf("%s: found %d of the sections %q", golden, found, sections)
	}
	return out
}
