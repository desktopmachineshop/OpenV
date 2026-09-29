//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The golden side of the tour's authorization matrix (tour_matrix_test.go):
// how a matrix is written into its area's golden, and how the rest of the
// tour reads it back with no database: tourCoverage counts each cell's
// status under its row's route (countMatrixCells), and tourChanges names
// each changed cell by section, row and column (matrixChanges).

// render writes the matrix into the golden, normalised in row order.
func (m *tourMatrix) render() *tourMatrixRecord {
	tr := m.tr
	rec := &tourMatrixRecord{Sends: m.sends, Cells: tourMatrixCellGrammar, Sections: []tourMatrixSectionRecord{}}
	for _, c := range m.columns {
		rec.Columns = append(rec.Columns, c.name)
	}
	for _, s := range m.sections {
		sr := tourMatrixSectionRecord{Name: s.name, About: s.about, Rows: []string{}}
		for _, r := range s.rows {
			row, err := matrixRowText(tr.norm, r.key, r.cells)
			if err != nil {
				tr.t.Errorf("the matrix section %q: %v", s.name, err)
			}
			sr.Rows = append(sr.Rows, row)
		}
		for _, e := range s.events {
			where := tr.norm.text(e.workspace)
			for _, ev := range tr.renderEvents(e.raw) {
				var b bytes.Buffer
				enc := json.NewEncoder(&b)
				enc.SetEscapeHTML(false)
				if err := enc.Encode(ev); err != nil {
					tr.t.Errorf("the matrix section %q: an event that does not encode: %v", s.name, err)
				}
				sr.Events = append(sr.Events, where+": "+strings.TrimSuffix(b.String(), "\n"))
			}
		}
		rec.Sections = append(rec.Sections, sr)
	}
	return rec
}

// matrixRowText writes a row, normalised: its key, then its cells, joined by
// tourMatrixSep, which neither may hold (nor a newline), since a reader of the
// golden splits the row there.
func matrixRowText(n *tourNormaliser, key string, cells []tourMatrixCell) (string, error) {
	parts := []string{n.text(key)}
	for _, c := range cells {
		parts = append(parts, c.render(n))
	}
	for i, p := range parts {
		if strings.Contains(p, tourMatrixSep) || strings.Contains(p, "\n") {
			return strings.Join(parts, tourMatrixSep), fmt.Errorf("the row %s: %q (part %d) holds %q or a newline, "+
				"which the row's format keeps for separating its cells", key, p, i, tourMatrixSep)
		}
	}
	return strings.Join(parts, tourMatrixSep), nil
}

// tourMatrixSep separates a row's key and cells.
const tourMatrixSep = " | "

// matrixStatusRE reads a cell's status.
var matrixStatusRE = regexp.MustCompile(`^(\d{3})(?:[ :]|$)`)

// matrixRowParts splits a golden's matrix row into its key, the route the
// key starts with, and its cells' statuses.
func matrixRowParts(row string) (key, route string, statuses []int, err error) {
	parts := strings.Split(row, tourMatrixSep)
	key = parts[0]
	method, rest, ok := strings.Cut(key, " ")
	if !ok {
		return key, "", nil, fmt.Errorf("the row %q does not start with a route", row)
	}
	tmpl, _, _ := strings.Cut(rest, " ")
	route = method + " " + tmpl
	for _, c := range parts[1:] {
		m := matrixStatusRE.FindStringSubmatch(c)
		if m == nil {
			return key, route, nil, fmt.Errorf("the row %q has a cell with no status: %q", key, c)
		}
		s, _ := strconv.Atoi(m[1])
		statuses = append(statuses, s)
	}
	return key, route, statuses, nil
}

// countMatrixCells adds a golden's matrix to a slice's coverage (tourCoverage):
// each cell's status under the route its row's key starts with, and the area
// under that route. A golden without a matrix adds nothing.
func countMatrixCells(t *testing.T, slice, key string, g tourGoldenIndex, known map[string]bool,
	statuses map[string]map[int]bool, areas map[string]map[string]bool) {
	t.Helper()
	if g.Matrix == nil {
		return
	}
	for _, sec := range g.Matrix.Sections {
		for _, row := range sec.Rows {
			_, route, cells, err := matrixRowParts(row)
			switch {
			case err != nil:
				t.Errorf("cmd/server/testdata/tour/%s/%s.json, matrix section %q: %v", slice, key, sec.Name, err)
				continue
			case !known[route]:
				t.Errorf("cmd/server/testdata/tour/%s/%s.json, matrix section %q, holds %q, which is not a route of "+
					"internal/api/testdata/routes.txt", slice, key, sec.Name, route)
				continue
			}
			if statuses[route] == nil {
				statuses[route], areas[route] = map[int]bool{}, map[string]bool{}
			}
			for _, s := range cells {
				statuses[route][s] = true
			}
			areas[route][key] = true
		}
	}
}

// tourMatrixChangesShown bounds the changed cells matrixChanges names per
// section; the line diff checkGolden prints shows the rows too.
const tourMatrixChangesShown = 60

// matrixChanges names what differs between two renderings of a golden's
// matrix (tourChanges): each changed cell as "matrix <section>: <row>:
// <column>: <old> -> <new>", each added or gone row and section, a section's
// changed events or about, and a changed header field. For an area that stopped early (partial), got holds
// the rows it sent: those are compared, and the golden's later rows are
// counted as not run. Both absent (a golden without a matrix): nothing.
func matrixChanges(want, got json.RawMessage, partial bool) []string {
	if len(want) == 0 && len(got) == 0 {
		return nil
	}
	var a, b tourMatrixRecord
	if (len(want) > 0 && json.Unmarshal(want, &a) != nil) || (len(got) > 0 && json.Unmarshal(got, &b) != nil) {
		return []string{"  matrix (not a matrix)"}
	}
	var out []string
	for _, f := range []struct {
		name string
		a, b []string
	}{{"columns", a.Columns, b.Columns}, {"sends", a.Sends, b.Sends}, {"cells", a.Cells, b.Cells}} {
		if strings.Join(f.a, "\n") != strings.Join(f.b, "\n") && !(partial && len(got) == 0) {
			out = append(out, "  matrix "+f.name)
		}
	}
	columns := b.Columns
	if len(columns) == 0 {
		columns = a.Columns
	}
	old := map[string]tourMatrixSectionRecord{}
	for _, s := range a.Sections {
		old[s.Name] = s
	}
	now := map[string]bool{}
	for _, s := range b.Sections {
		now[s.Name] = true
		prev, ok := old[s.Name]
		if !ok {
			out = append(out, fmt.Sprintf("  matrix: added section %q (%d rows)", s.Name, len(s.Rows)))
			continue
		}
		out = append(out, matrixSectionChanges(s.Name, columns, prev.Rows, s.Rows, partial)...)
		if prev.About != s.About {
			out = append(out, fmt.Sprintf("  matrix %q: about", s.Name))
		}
		if strings.Join(prev.Events, "\n") != strings.Join(s.Events, "\n") {
			out = append(out, fmt.Sprintf("  matrix %q: events (%d, were %d)", s.Name, len(s.Events), len(prev.Events)))
		}
	}
	for _, s := range a.Sections {
		switch {
		case now[s.Name]:
		case partial:
			out = append(out, fmt.Sprintf("  matrix: (not run: section %q, %d rows)", s.Name, len(s.Rows)))
		default:
			out = append(out, fmt.Sprintf("  matrix: gone section %q (%d rows)", s.Name, len(s.Rows)))
		}
	}
	return out
}

// matrixSectionChanges is matrixChanges for one section's rows, by key.
func matrixSectionChanges(section string, columns, want, got []string, partial bool) []string {
	var out []string
	split := func(row string) (string, []string) {
		parts := strings.Split(row, tourMatrixSep)
		return parts[0], parts[1:]
	}
	old := map[string][]string{}
	for _, r := range want {
		k, cells := split(r)
		old[k] = cells
	}
	seen := map[string]bool{}
	cells := 0
	for _, r := range got {
		k, now := split(r)
		seen[k] = true
		prev, ok := old[k]
		if !ok {
			out = append(out, fmt.Sprintf("  matrix %q: added row %s", section, k))
			continue
		}
		for i := 0; i < max(len(prev), len(now)); i++ {
			var p, n, col string
			if i < len(prev) {
				p = prev[i]
			}
			if i < len(now) {
				n = now[i]
			}
			col = fmt.Sprintf("column %d", i+1)
			if i < len(columns) {
				col = columns[i]
			}
			if p == n {
				continue
			}
			if cells++; cells <= tourMatrixChangesShown {
				out = append(out, fmt.Sprintf("  matrix %q: %s: %s: %s -> %s", section, k, col, p, n))
			}
		}
	}
	if cells > tourMatrixChangesShown {
		out = append(out, fmt.Sprintf("  matrix %q: and %d more changed cells", section, cells-tourMatrixChangesShown))
	}
	notRun := 0
	for _, r := range want {
		k, _ := split(r)
		switch {
		case seen[k]:
		case partial:
			notRun++
		default:
			out = append(out, fmt.Sprintf("  matrix %q: gone row %s", section, k))
		}
	}
	if notRun > 0 {
		out = append(out, fmt.Sprintf("  matrix %q: (not run: %d rows)", section, notRun))
	}
	return out
}
