package postgres

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// This file is the export round trip of refactor plan step S9 (invariant
// I15): each JSON export kept under docs/exports/ is imported, as POST
// /api/v1/projects/import imports it, into a fresh database through the real
// repositories and the services wired as cmd/server wires them, and the new
// project is exported again as JSON. The two documents are compared field by
// field, artifacts matched by ref and links by their ends and type, and what
// the comparison found is written to testdata/formats/roundtrip/<file>.txt:
// for each field, how many values came back equal, remapped to the new ids,
// set to the new project's id, stamped with the round trip's clock, set to
// one value (the import's version 1, a link no longer suspect) or changed
// otherwise. So the golden states what a round trip keeps and what the
// importer drops (attachments, which carry no file; link flags and
// attributes; versions and times), and a field that a repository or the
// import mapper stops carrying, or starts carrying, changes it.
//
// Artifact order is compared within each parent only: the export orders a
// project by parent_id first, and the import mints random parent ids, so
// the order of the parents' groups differs from run to run. Link order is
// not compared at all: the import stamps its links in one loop, so their
// created_at, which orders them, can tie.
//
// DB-gated like the rest of the package (OPENV_TEST_DATABASE_URL). The
// golden is the same with or without pgvector: CI runs it on both legs
// (postgres:15 without the extension, pgvector/pgvector:pg15 with it), and
// Postgres 16 with pgvector writes it alike.
// Regenerate, for a deliberate change only, with
//
//	UPDATE_GOLDEN=1 OPENV_TEST_DATABASE_URL=... go test ./internal/persistence/postgres -count=1 -run '^TestExportDocsRoundTrip$'
//
// Only the value 1 regenerates; any other value compares.

const (
	roundTripDocs      = "../../../docs/exports"
	roundTripGoldenDir = "testdata/formats/roundtrip"
	roundTripShownDir  = "internal/persistence/postgres/testdata/formats/roundtrip"
	roundTripCmd       = "UPDATE_GOLDEN=1 OPENV_TEST_DATABASE_URL=<a throwaway server> go test ./internal/persistence/postgres -count=1 -run '^TestExportDocsRoundTrip$'"
	roundTripUpdateEnv = "UPDATE_GOLDEN"
	// roundTripOrg is the workspace the documents are imported into; the
	// projects table keeps org_id without a foreign key.
	roundTripOrg = "5e9a0000-0000-4000-8000-000000000001"
)

// roundTripService wires the export service over the real repositories as
// cmd/server/main.go does: artifacts with their link suspector, links,
// attachments, projects, the product profile and the attribute definitions.
func roundTripService(t *testing.T) exports.Service {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)
	artifactService := artifacts.NewDefaultService(NewArtifactRepository(db))
	linkService := links.NewDefaultService(NewLinkRepository(db))
	linkService.SetArtifactService(artifactService)
	artifactService.SetLinkSuspector(linkService)
	exportService := exports.NewService(artifactService, linkService,
		attachments.NewDefaultService(NewAttachmentRepository(db)), NewProjectInfoRepository(db),
		projects.NewService(NewProjectRepository(db)))
	exportService.SetProductService(products.NewDefaultService(NewProductProfileRepository(db)))
	exportService.SetAttributeService(attributes.NewDefaultService(NewAttributeDefinitionRepository(db), artifacts.ValidType))
	return exportService
}

// TestExportDocsRoundTrip imports each docs/exports/*.json and compares the
// new project's JSON export with it (see the top of this file).
func TestExportDocsRoundTrip(t *testing.T) {
	docs, err := filepath.Glob(filepath.Join(roundTripDocs, "*.json"))
	if err != nil || len(docs) == 0 {
		t.Fatalf("no export documents under docs/exports (%v)", err)
	}
	svc := roundTripService(t)
	want := map[string]bool{}
	for _, doc := range docs {
		name := strings.TrimSuffix(filepath.Base(doc), ".json") + ".txt"
		want[name] = true
		t.Run(filepath.Base(doc), func(t *testing.T) {
			raw, err := os.ReadFile(doc)
			if err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			id, err := svc.ImportProject(raw, roundTripOrg)
			if err != nil {
				t.Fatalf("ImportProject: %v", err)
			}
			exported, _, err := svc.ExportProject(id, exports.FormatJSON)
			if err != nil {
				t.Fatalf("ExportProject: %v", err)
			}
			after := time.Now()
			rt := newRoundTrip(t, raw, exported, id, before, after)
			report := rt.report("docs/exports/" + filepath.Base(doc))
			checkRoundTripGolden(t, name, report)
		})
	}
	// A golden whose document is gone would pin nothing.
	goldens, _ := filepath.Glob(filepath.Join(roundTripGoldenDir, "*.txt"))
	for _, g := range goldens {
		if !want[filepath.Base(g)] && os.Getenv(roundTripUpdateEnv) != "1" {
			t.Errorf("%s/%s has no document under docs/exports; delete it with the document", roundTripShownDir, filepath.Base(g))
		}
	}
}

// roundTrip holds the two documents and what relates them.
type roundTrip struct {
	t             *testing.T
	in, out       map[string]interface{}
	newProject    string
	before, after time.Time
	// oldToNew maps the document's artifact ids to the new project's, by ref;
	// oldRef and newRef name an artifact id by its ref.
	oldToNew       map[string]string
	oldRef, newRef map[string]string
	// tally counts, per section and field, how each value came back.
	tally map[string]map[string]map[string]int
	// setTo collects, per section, field and outcome ("changed" or
	// "added"), the values a field came back as, to tell "set to one value"
	// from "changed".
	setTo map[string]map[string]map[string]map[string]bool
}

func decodeDoc(t *testing.T, raw []byte, what string) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode the %s: %v", what, err)
	}
	return doc
}

func newRoundTrip(t *testing.T, in, out []byte, newProject string, before, after time.Time) *roundTrip {
	return &roundTrip{
		t: t, in: decodeDoc(t, in, "document"), out: decodeDoc(t, out, "export"), newProject: newProject,
		before: before.Add(-time.Second), after: after.Add(time.Second),
		oldToNew: map[string]string{}, oldRef: map[string]string{}, newRef: map[string]string{},
		tally: map[string]map[string]map[string]int{}, setTo: map[string]map[string]map[string]map[string]bool{},
	}
}

func objects(v interface{}) []map[string]interface{} {
	list, _ := v.([]interface{})
	out := make([]map[string]interface{}, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func isString(v interface{}) bool {
	_, ok := v.(string)
	return ok
}

func str(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

// isClock reports whether a value is a time the round trip stamped. A
// TIMESTAMP column hands back the wall clock it was written with as UTC, so
// the wall clock is also read in the local zone.
func (rt *roundTrip) isClock(v interface{}) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return false
	}
	wall := time.Date(ts.Year(), ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second(), ts.Nanosecond(), time.Local)
	for _, c := range []time.Time{ts, wall} {
		if !c.Before(rt.before) && !c.After(rt.after) {
			return true
		}
	}
	return false
}

// count records how one value came back.
func (rt *roundTrip) count(section, field string, in, out interface{}, inOK, outOK bool) {
	how := ""
	switch {
	case !inOK && !outOK:
		return
	case inOK && !outOK:
		how = "dropped"
	case !inOK && outOK:
		how = "added"
	case reflect.DeepEqual(in, out):
		how = "equal"
	case rt.remapped(in, out):
		how = "remapped to the new ids"
	case out == rt.newProject:
		how = "the new project's id"
	case field == "id" && isString(in) && isString(out):
		how = "a new id"
	case rt.isClock(out):
		how = "the round trip's clock"
	}
	if how == "" {
		how = "changed"
	}
	if how == "changed" || how == "added" {
		enc, _ := json.Marshal(out)
		if rt.setTo[section] == nil {
			rt.setTo[section] = map[string]map[string]map[string]bool{}
		}
		if rt.setTo[section][field] == nil {
			rt.setTo[section][field] = map[string]map[string]bool{}
		}
		if rt.setTo[section][field][how] == nil {
			rt.setTo[section][field][how] = map[string]bool{}
		}
		rt.setTo[section][field][how][string(enc)] = true
	}
	if rt.tally[section] == nil {
		rt.tally[section] = map[string]map[string]int{}
	}
	if rt.tally[section][field] == nil {
		rt.tally[section][field] = map[string]int{}
	}
	rt.tally[section][field][how]++
}

// remapped reports whether out is the new id of the document's id in.
func (rt *roundTrip) remapped(in, out interface{}) bool {
	a, aok := in.(string)
	b, bok := out.(string)
	return aok && bok && rt.oldToNew[a] != "" && rt.oldToNew[a] == b
}

// compareObjects counts every key of two objects; keys under descend are
// compared one level down (attributes.priority), and links_snapshot by the
// links it lists.
func (rt *roundTrip) compareObjects(section, prefix string, in, out map[string]interface{}, descend map[string]bool) {
	keys := map[string]bool{}
	for k := range in {
		keys[k] = true
	}
	for k := range out {
		keys[k] = true
	}
	for k := range keys {
		iv, iok := in[k]
		ov, ook := out[k]
		if descend[k] {
			im, _ := iv.(map[string]interface{})
			om, _ := ov.(map[string]interface{})
			if (iok && iv != nil && im == nil) || (ook && ov != nil && om == nil) {
				rt.count(section, prefix+k, iv, ov, iok, ook)
				continue
			}
			rt.compareObjects(section, prefix+k+".", im, om, nil)
			continue
		}
		if prefix+k == "attributes.links_snapshot" && iok && ook {
			rt.countSnapshot(section, iv, ov)
			continue
		}
		rt.count(section, prefix+k, iv, ov, iok, ook)
	}
}

// countSnapshot compares two links_snapshot lists by the links they name:
// each entry's ends, by ref, and its type, as a sorted list. The ids and
// times of the entries are the links', which the import renews.
func (rt *roundTrip) countSnapshot(section string, in, out interface{}) {
	triples := func(v interface{}, ref map[string]string) []string {
		var out []string
		for _, e := range objects(v) {
			from, to := ref[str(e, "from_id")], ref[str(e, "to_id")]
			if from == "" {
				from = "(outside)"
			}
			if to == "" {
				to = "(outside)"
			}
			out = append(out, from+" -"+str(e, "type")+"-> "+to)
		}
		sort.Strings(out)
		return out
	}
	how := "the same links, renewed"
	if !reflect.DeepEqual(triples(in, rt.oldRef), triples(out, rt.newRef)) {
		how = "other links"
	}
	if rt.tally[section] == nil {
		rt.tally[section] = map[string]map[string]int{}
	}
	if rt.tally[section]["attributes.links_snapshot"] == nil {
		rt.tally[section]["attributes.links_snapshot"] = map[string]int{}
	}
	rt.tally[section]["attributes.links_snapshot"][how]++
}

// report compares the documents and writes what it found.
func (rt *roundTrip) report(source string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Round trip of %s (refactor plan S9, invariant I15): imported as\n", source)
	b.WriteString(roundTripHeader)

	inArts, outArts := objects(rt.in["artifacts"]), objects(rt.out["artifacts"])
	newByRef := map[string]map[string]interface{}{}
	for _, a := range outArts {
		newByRef[str(a, "ref")] = a
		rt.newRef[str(a, "id")] = str(a, "ref")
	}
	for _, a := range inArts {
		rt.oldRef[str(a, "id")] = str(a, "ref")
		if n := newByRef[str(a, "ref")]; n != nil && str(a, "ref") != "" {
			rt.oldToNew[str(a, "id")] = str(n, "id")
		}
	}

	// The document's own fields, the product profile one level down.
	top := map[string]bool{"artifacts": true, "links": true}
	inTop, outTop := map[string]interface{}{}, map[string]interface{}{}
	for k, v := range rt.in {
		if !top[k] {
			inTop[k] = v
		}
	}
	for k, v := range rt.out {
		if !top[k] {
			outTop[k] = v
		}
	}
	rt.compareObjects("document", "", inTop, outTop, map[string]bool{"product_profile": true})

	// Artifacts, matched by ref.
	matched := 0
	for _, a := range inArts {
		n := newByRef[str(a, "ref")]
		if n == nil || str(a, "ref") == "" {
			continue
		}
		matched++
		rt.compareObjects("artifacts", "", a, n, map[string]bool{"attributes": true})
	}
	fmt.Fprintf(&b, "document: %d artifacts and %d links in, %d artifacts and %d links out\n",
		len(inArts), len(objects(rt.in["links"])), len(outArts), len(objects(rt.out["links"])))
	rt.write(&b, "document", "the document's own fields")
	fmt.Fprintf(&b, "artifacts: %d matched by ref, %d in unmatched, %d out unmatched\n",
		matched, len(inArts)-matched, len(outArts)-matched)
	b.WriteString(rt.order(inArts, outArts))
	rt.write(&b, "artifacts", "")

	// Links, matched by ends and type; a link with an end outside the
	// document is the import's to skip.
	key := func(l map[string]interface{}, ref map[string]string) (string, bool) {
		from, fok := ref[str(l, "from_id")]
		to, tok := ref[str(l, "to_id")]
		return from + " -" + str(l, "type") + "-> " + to, fok && tok
	}
	outByKey := map[string][]map[string]interface{}{}
	for _, l := range objects(rt.out["links"]) {
		k, _ := key(l, rt.newRef)
		outByKey[k] = append(outByKey[k], l)
	}
	outside, linked, unmatched := 0, 0, 0
	for _, l := range objects(rt.in["links"]) {
		k, ok := key(l, rt.oldRef)
		if !ok {
			outside++
			continue
		}
		if len(outByKey[k]) == 0 {
			unmatched++
			continue
		}
		n := outByKey[k][0]
		outByKey[k] = outByKey[k][1:]
		linked++
		rt.compareObjects("links", "", l, n, nil)
	}
	extra := 0
	for _, rest := range outByKey {
		extra += len(rest)
	}
	fmt.Fprintf(&b, "links: %d matched by ends and type, %d in with an end outside the document, %d in unmatched, %d out unmatched\n",
		linked, outside, unmatched, extra)
	rt.write(&b, "links", "")
	return b.String()
}

// order compares the order of each parent's children with the document's.
func (rt *roundTrip) order(in, out []map[string]interface{}) string {
	groups := func(list []map[string]interface{}, ref map[string]string) map[string][]string {
		g := map[string][]string{}
		for _, a := range list {
			parent := ref[str(a, "parent_id")]
			g[parent] = append(g[parent], str(a, "ref"))
		}
		return g
	}
	gin, gout := groups(in, rt.oldRef), groups(out, rt.newRef)
	same := 0
	for parent, refs := range gin {
		if reflect.DeepEqual(refs, gout[parent]) {
			same++
		}
	}
	return fmt.Sprintf("  order: the children of %d of %d parents (the root among them) in the document's order\n", same, len(gin))
}

// write lists a section's fields, sorted, each with its counts.
func (rt *roundTrip) write(b *strings.Builder, section, title string) {
	if title != "" {
		fmt.Fprintf(b, "%s\n", title)
	}
	fields := make([]string, 0, len(rt.tally[section]))
	for f := range rt.tally[section] {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, f := range fields {
		type entry struct {
			how string
			n   int
		}
		var entries []entry
		for how, n := range rt.tally[section][f] {
			if values := rt.setTo[section][f][how]; len(values) == 1 {
				for v := range values {
					if how == "changed" {
						how = "set to " + v
					} else {
						how = "added as " + v
					}
				}
			}
			entries = append(entries, entry{how, n})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].n != entries[j].n {
				return entries[i].n > entries[j].n
			}
			return entries[i].how < entries[j].how
		})
		parts := make([]string, len(entries))
		for i, e := range entries {
			parts[i] = fmt.Sprintf("%s %d", e.how, e.n)
		}
		fmt.Fprintf(b, "  %s: %s\n", f, strings.Join(parts, ", "))
	}
}

const roundTripHeader = `# POST /api/v1/projects/import imports it (ImportProject) into a fresh
# database, then exported again as JSON (GET /api/v1/projects/{id}/export).
# Per field: how many values came back equal, remapped to the new ids, as the
# new project's id, as a new id, stamped with the round trip's clock, set to
# one value, changed otherwise, dropped or added. Artifacts are matched by ref, links by
# their ends and type; links_snapshot is compared by the links it names.
# A refactor never changes this file; for a deliberate change regenerate it
# with
#   UPDATE_GOLDEN=1 OPENV_TEST_DATABASE_URL=<a throwaway server> go test ./internal/persistence/postgres -count=1 -run '^TestExportDocsRoundTrip$'
`

// checkRoundTripGolden compares a report with its golden, or rewrites it
// under UPDATE_GOLDEN=1.
func checkRoundTripGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(roundTripGoldenDir, name)
	shown := roundTripShownDir + "/" + name
	if os.Getenv(roundTripUpdateEnv) == "1" {
		if err := os.MkdirAll(roundTripGoldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with:\n  %s", shown, err, roundTripCmd)
	}
	if bytes.Equal(want, []byte(got)) {
		return
	}
	t.Errorf("%s does not match what an import and a JSON export now do:\n%s\n"+
		"A refactor never changes this file. If the change is deliberate, regenerate it with:\n  %s\n"+
		"(only %s=1 regenerates; any other value compares)",
		shown, s9LineDiff(string(want), got), roundTripCmd, roundTripUpdateEnv)
}

// s9LineDiff lists the lines only one side has, golden first.
func s9LineDiff(want, got string) string {
	inGot := map[string]int{}
	for _, l := range strings.Split(got, "\n") {
		inGot[l]++
	}
	inWant := map[string]int{}
	for _, l := range strings.Split(want, "\n") {
		inWant[l]++
	}
	var out []string
	for _, l := range strings.Split(want, "\n") {
		if inGot[l] == 0 {
			out = append(out, "- "+l)
		} else {
			inGot[l]--
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if inWant[l] == 0 {
			out = append(out, "+ "+l)
		} else {
			inWant[l]--
		}
	}
	return strings.Join(out, "\n")
}
