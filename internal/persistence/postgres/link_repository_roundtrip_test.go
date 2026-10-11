package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// The link repository's round trip (refactor plan X13's characterization for
// link_repository.go, in the manner of X13a's), pinned as found: every column
// of a link read back through each read that scans it (FindByID, the two
// live lists, FindAll and the two per-version lists), the three shapes of
// the attributes column, the scan errors that name a column's place, what a
// read answers for a row no one has and for an empty list, the suspect
// review queue's nine columns, and what each write puts in the row. The
// existing tests (link_valid_from_test.go, link_suspect_test.go,
// link_delete_test.go, link_artifacts_test.go, review_queue_test.go) read
// back valid_from, valid_to and the suspect flag, and no other column of a
// link through any read; none pins a column's place in a scan or the
// attributes' NULL. X13 gives the link a linkColumns list and a scan
// function in place of the six copies of each; these tests are what holds the
// repository still while it does. The repository has no transaction, so
// there is none to pin. The helpers are in
// repository_roundtrip_helpers_test.go.

func rtLinkSansTimes(l *links.Link) links.Link {
	c := *l
	c.ValidFrom, c.ValidTo, c.CreatedAt, c.UpdatedAt = time.Time{}, nil, time.Time{}, time.Time{}
	return c
}

// rtWantLink fails unless got is sent as a link reads back: every field but
// the times equal, and each time the TIMESTAMP column's wall clock of the
// time sent.
func rtWantLink(t *testing.T, what string, got, sent *links.Link) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtLinkSansTimes(got), rtLinkSansTimes(sent))
	rtWantTimestamp(t, what+" valid_from", got.ValidFrom, sent.ValidFrom)
	rtWantOptionalTimestamp(t, what+" valid_to", got.ValidTo, sent.ValidTo)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
}

// rtWantLinkList fails unless list holds exactly the links of want, in the
// order given, each read back whole.
func rtWantLinkList(t *testing.T, what string, list []*links.Link, err error, want ...*links.Link) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var got, wantIDs []string
	for _, l := range list {
		got = append(got, l.ID)
	}
	for _, l := range want {
		wantIDs = append(wantIDs, l.ID)
	}
	if strings.Join(got, "|") != strings.Join(wantIDs, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantIDs)
		return
	}
	for i := range list {
		rtWantLink(t, fmt.Sprintf("%s link %d", what, i), list[i], want[i])
	}
}

// rtLinkWorld is a fresh database with one project and a repository over it.
type rtLinkWorld struct {
	t       *testing.T
	db      *sql.DB
	repo    *LinkRepository
	project string
}

func newRTLinkWorld(t *testing.T) *rtLinkWorld {
	t.Helper()
	db := rtDB(t)
	w := &rtLinkWorld{t: t, db: db, repo: NewLinkRepository(db), project: uuid.New().String()}
	seedProjects(t, db, w.project)
	return w
}

// artifact saves a current artifact of the type and title in the project
// (project "" is the world's) and answers its id.
func (w *rtLinkWorld) artifact(project, typ, title string) string {
	w.t.Helper()
	if project == "" {
		project = w.project
	}
	seedProjects(w.t, w.db, project)
	a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: typ, Title: title})
	if err := NewArtifactRepository(w.db).Save(a); err != nil {
		w.t.Fatalf("save artifact %q: %v", title, err)
	}
	return a.ID
}

// save saves the link and records it against version 1 of each end, which is
// what the per-version reads answer from.
func (w *rtLinkWorld) save(l *links.Link) *links.Link {
	w.t.Helper()
	if err := w.repo.Save(l); err != nil {
		w.t.Fatalf("Save %s: %v", l.ID, err)
	}
	for _, end := range []string{l.FromID, l.ToID} {
		if err := w.repo.RecordLinkForArtifactVersion(l.ID, end, 1); err != nil {
			w.t.Fatalf("RecordLinkForArtifactVersion %s: %v", l.ID, err)
		}
	}
	return l
}

// rtLinkRead is one read of the repository that answers links.
type rtLinkRead struct {
	name string
	// scans names the columns the read scans: "all" for the eleven of
	// FindByID and the per-version reads (valid_to among them), "live" for
	// the ten of the lists of live links.
	scans string
	list  func() ([]*links.Link, error)
}

// rtLinkReads is every read of the repository that scans a link, over the
// link id, its two ends and a project, at version 1 of each end.
func rtLinkReads(repo *LinkRepository, id, from, to, project string) []rtLinkRead {
	return []rtLinkRead{
		{"FindByID", "all", func() ([]*links.Link, error) {
			l, err := repo.FindByID(id)
			if l == nil {
				return nil, err
			}
			return []*links.Link{l}, err
		}},
		{"FindByFromID", "live", func() ([]*links.Link, error) { return repo.FindByFromID(from) }},
		{"FindByToID", "live", func() ([]*links.Link, error) { return repo.FindByToID(to) }},
		{"FindAll", "live", func() ([]*links.Link, error) { return repo.FindAll(project) }},
		{"FindByFromIDForVersion", "all", func() ([]*links.Link, error) { return repo.FindByFromIDForVersion(from, 1) }},
		{"FindByToIDForVersion", "all", func() ([]*links.Link, error) { return repo.FindByToIDForVersion(to, 1) }},
	}
}

func rtAttrs(note string) map[string]interface{} {
	return map[string]interface{}{"note": note, "count": float64(3), "tags": []interface{}{"a", "b"},
		"nested": map[string]interface{}{"ok": true, "none": nil}}
}

// Every column Save writes reads back through FindByID, the two lists of
// live links, FindAll and the two per-version lists, whole: the ends, the
// type, the suspect flag, the attributes, the version and the four stamps
// (the wall clock sent, its offset dropped, to the microsecond). Each column
// holds a value no other does, so a swap of two in a scan shows. The lists
// answer newest created_at first, the per-version lists with a closed link
// among them, carrying its valid_to; the live reads leave a closed link out;
// a link that runs the other way (from the to end) is read by the other end
// only, so the two ends are not swapped.
func TestLinkRepositoryReadsBackEveryColumnThroughEveryRead(t *testing.T) {
	w := newRTLinkWorld(t)
	a := w.artifact("", "requirement", "Alpha")
	b := w.artifact("", "test-case", "Bravo")

	first := w.save(&links.Link{ID: uuid.New().String(), FromID: a, ToID: b, Type: "verifies", Suspect: true,
		Attributes: rtAttrs("first"), Version: 3,
		ValidFrom: rtAt(-30).In(rtCEST), CreatedAt: rtAt(10), UpdatedAt: rtAt(13).In(rtCEST)})
	second := w.save(&links.Link{ID: uuid.New().String(), FromID: a, ToID: b, Type: "satisfies",
		Attributes: map[string]interface{}{}, Version: 1,
		ValidFrom: rtAt(-20), CreatedAt: rtAt(20), UpdatedAt: rtAt(22)})
	reverse := w.save(&links.Link{ID: uuid.New().String(), FromID: b, ToID: a, Type: "refines", Suspect: true,
		Attributes: nil, Version: 7,
		ValidFrom: rtAt(-5).In(rtCEST), CreatedAt: rtAt(30), UpdatedAt: rtAt(31).In(rtCEST)})
	closedAt := rtAt(40)
	closed := w.save(&links.Link{ID: uuid.New().String(), FromID: a, ToID: b, Type: "mitigates", Suspect: true,
		Attributes: rtAttrs("closed"), Version: 2, ValidFrom: rtAt(-10), ValidTo: &closedAt,
		CreatedAt: rtAt(5), UpdatedAt: rtAt(6)})

	// Written through Save, each link reads back through FindByID whole.
	for name, l := range map[string]*links.Link{"first": first, "second": second, "reverse": reverse} {
		got, err := w.repo.FindByID(l.ID)
		if err != nil {
			t.Fatalf("FindByID %s: %v", name, err)
		}
		rtWantLink(t, "FindByID "+name, got, l)
	}

	// Newest created_at first: second, first (the ends a to b).
	list, err := w.repo.FindByFromID(a)
	rtWantLinkList(t, "FindByFromID(a)", list, err, second, first)
	list, err = w.repo.FindByToID(b)
	rtWantLinkList(t, "FindByToID(b)", list, err, second, first)
	list, err = w.repo.FindByFromID(b)
	rtWantLinkList(t, "FindByFromID(b)", list, err, reverse)
	list, err = w.repo.FindByToID(a)
	rtWantLinkList(t, "FindByToID(a)", list, err, reverse)
	list, err = w.repo.FindAll(w.project)
	rtWantLinkList(t, "FindAll", list, err, reverse, second, first)

	// The per-version lists, at version 1 of each end: the closed link too,
	// the oldest, with its valid_to. Version 2 of the from end holds one
	// link, recorded for it alone.
	list, err = w.repo.FindByFromIDForVersion(a, 1)
	rtWantLinkList(t, "FindByFromIDForVersion(a, 1)", list, err, second, first, closed)
	list, err = w.repo.FindByToIDForVersion(b, 1)
	rtWantLinkList(t, "FindByToIDForVersion(b, 1)", list, err, second, first, closed)
	list, err = w.repo.FindByFromIDForVersion(b, 1)
	rtWantLinkList(t, "FindByFromIDForVersion(b, 1)", list, err, reverse)
	list, err = w.repo.FindByToIDForVersion(a, 1)
	rtWantLinkList(t, "FindByToIDForVersion(a, 1)", list, err, reverse)
	if err := w.repo.RecordLinkForArtifactVersion(first.ID, a, 2); err != nil {
		t.Fatal(err)
	}
	list, err = w.repo.FindByFromIDForVersion(a, 2)
	rtWantLinkList(t, "FindByFromIDForVersion(a, 2)", list, err, first)
	list, err = w.repo.FindByToIDForVersion(b, 2)
	rtWantNil(t, "FindByToIDForVersion(b, 2)", list, err)

	// A closed link is on no live read.
	if got, err := w.repo.FindByID(closed.ID); got != nil || err == nil || err.Error() != "link not found" {
		t.Errorf("FindByID of a closed link: %v, %v; want nil and link not found", got, err)
	}
}

// The attributes column reads three ways through every read that scans it:
// absent from the insert it is the column's default '{}', an empty map that
// is not nil; a NULL and a JSON null both read nil. A value that is not an
// object (an array, a string, a number) fails the read with the decoder's
// error as it came, and the read answers no link and, for a list, no list
// at all (nil, not what it had decoded), even where a good link came before.
func TestLinkRepositoryReadsOddAttributesThroughEveryRead(t *testing.T) {
	w := newRTLinkWorld(t)

	// seed inserts a link whose attributes column the SQL gives (or leaves
	// out, with "") over a fresh pair of ends, and answers the link's id,
	// ends and the stamp it was created at.
	seed := func(attributes string, created time.Time) (id, from, to string) {
		t.Helper()
		id, from, to = uuid.New().String(), w.artifact("", "requirement", "From"), w.artifact("", "test-case", "To")
		columns, values := "", ""
		if attributes != "" {
			columns, values = ", attributes", ", "+attributes
		}
		rtSeed(t, w.db, `INSERT INTO links (id, from_id, to_id, type, version, valid_from, created_at, updated_at`+columns+`)
			VALUES ($1, $2, $3, 'relates-to', 4, $4, $4, $4`+values+`)`, id, from, to, created)
		rtSeed(t, w.db, `INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 1)`, id, from, to)
		return id, from, to
	}

	for _, c := range []struct {
		name       string
		attributes string
		want       map[string]interface{}
	}{
		{"the column's default", "", map[string]interface{}{}},
		{"an empty object", `'{}'::jsonb`, map[string]interface{}{}},
		{"NULL", `NULL`, nil},
		{"a JSON null", `'null'::jsonb`, nil},
		{"an object", `'{"a": [1, {"b": null}]}'::jsonb`,
			map[string]interface{}{"a": []interface{}{float64(1), map[string]interface{}{"b": nil}}}},
	} {
		id, from, to := seed(c.attributes, rtAt(1))
		for _, read := range rtLinkReads(w.repo, id, from, to, w.project) {
			got, err := read.list()
			if err != nil || len(got) < 1 {
				t.Errorf("%s over %s: %v, %v; want the link", read.name, c.name, got, err)
				continue
			}
			for _, l := range got {
				if l.ID != id {
					continue
				}
				rtWantSame(t, read.name+" attributes over "+c.name, l.Attributes, c.want)
			}
		}
	}

	for _, c := range []struct {
		name       string
		attributes string
		want       string
	}{
		{"an array", `'[1, 2]'::jsonb`, "json: cannot unmarshal array into Go value of type map[string]interface {}"},
		{"a string", `'"text"'::jsonb`, "json: cannot unmarshal string into Go value of type map[string]interface {}"},
		{"a number", `'5'::jsonb`, "json: cannot unmarshal number into Go value of type map[string]interface {}"},
	} {
		// The good link, newer, shares the ends with the bad one, older, so
		// each list decodes the good link before it fails on the bad one.
		good, from, to := seed(`'{"fine": true}'::jsonb`, rtAt(1))
		bad := uuid.New().String()
		rtSeed(t, w.db, `INSERT INTO links (id, from_id, to_id, type, version, valid_from, created_at, updated_at, attributes)
			VALUES ($1, $2, $3, 'relates-to', 4, $4, $4, $4, `+c.attributes+`)`, bad, from, to, rtAt(0))
		rtSeed(t, w.db, `INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 1)`, bad, from, to)

		for _, read := range rtLinkReads(w.repo, bad, from, to, w.project) {
			got, err := read.list()
			var syntax *json.UnmarshalTypeError
			if got != nil || !errors.As(err, &syntax) || err.Error() != c.want {
				t.Errorf("%s over %s: %v, %v; want nil and %s as it came", read.name, c.name, got, err, c.want)
			}
		}
		rtSeed(t, w.db, `DELETE FROM links WHERE id = $1`, bad)
		// The good link alone still reads.
		if got, err := w.repo.FindByID(good); err != nil || got == nil || got.Attributes["fine"] != true {
			t.Errorf("FindByID of the good link beside %s: %v, %v", c.name, got, err)
		}
	}
}

// A column the schema forbids NULL, here one a test has let take it, fails
// the read that scans it, naming the column and its place among the scanned:
// FindByID and the two per-version reads scan eleven columns (id 0, from_id
// 1, to_id 2, type 3, suspect 4, attributes 5, version 6, valid_from 7,
// valid_to 8, created_at 9, updated_at 10), and the three lists of live
// links ten, without valid_to (created_at 8, updated_at 9). Each read answers
// no link and no list (nil, not []), and the error comes as the scan gave it,
// unwrapped. A link with a NULL end is out of the reads that match that end:
// those answer no link and no error. (The id is the primary key; it takes
// no NULL.)
func TestLinkRepositoryScanErrorsNameTheColumn(t *testing.T) {
	w := newRTLinkWorld(t)
	a := w.artifact("", "requirement", "Alpha")
	b := w.artifact("", "test-case", "Bravo")
	for _, column := range []string{"from_id", "to_id", "type", "suspect", "version", "valid_from", "created_at", "updated_at"} {
		rtSeed(t, w.db, `ALTER TABLE links ALTER COLUMN `+column+` DROP NOT NULL`)
	}

	const str = "converting NULL to string is unsupported"
	const num = "converting NULL to int is unsupported"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	// at is the column's index in the eleven columns, then in the ten.
	for _, c := range []struct {
		column  string
		at      [2]int
		message string
		// misses names the reads that cannot match the row.
		misses []string
	}{
		{"from_id", [2]int{1, 1}, str, []string{"FindByFromID", "FindAll", "FindByFromIDForVersion"}},
		{"to_id", [2]int{2, 2}, str, []string{"FindByToID", "FindAll", "FindByToIDForVersion"}},
		{"type", [2]int{3, 3}, str, nil},
		{"suspect", [2]int{4, 4}, "sql/driver: couldn't convert <nil> (<nil>) into type bool", nil},
		{"version", [2]int{6, 6}, num, nil},
		{"valid_from", [2]int{7, 7}, stamp, nil},
		{"created_at", [2]int{9, 8}, stamp, nil},
		{"updated_at", [2]int{10, 9}, stamp, nil},
	} {
		id := uuid.New().String()
		values := map[string]string{"from_id": "'" + a + "'", "to_id": "'" + b + "'", "type": "'verifies'", "suspect": "FALSE",
			"version": "1", "valid_from": "NOW()", "created_at": "NOW()", "updated_at": "NOW()"}
		values[c.column] = "NULL"
		rtSeed(t, w.db, fmt.Sprintf(`INSERT INTO links (id, from_id, to_id, type, suspect, version, valid_from, created_at, updated_at)
			VALUES ($1, %s, %s, %s, %s, %s, %s, %s, %s)`, values["from_id"], values["to_id"], values["type"], values["suspect"],
			values["version"], values["valid_from"], values["created_at"], values["updated_at"]), id)
		rtSeed(t, w.db, `INSERT INTO link_artifacts (link_id, artifact_id, artifact_version) VALUES ($1, $2, 1), ($1, $3, 1)`, id, a, b)

	reads:
		for _, read := range rtLinkReads(w.repo, id, a, b, w.project) {
			for _, miss := range c.misses {
				if read.name == miss {
					got, err := read.list()
					rtWantNil(t, fmt.Sprintf("%s over a row with a NULL %s", read.name, c.column), got, err)
					continue reads
				}
			}
			index := c.at[0]
			if read.scans == "live" {
				index = c.at[1]
			}
			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", index, c.column, c.message)
			got, err := read.list()
			if got != nil || err == nil || err.Error() != want {
				t.Errorf("%s over a row with a NULL %s: %v, %v; want nil and %s", read.name, c.column, got, err, want)
			}
		}
		rtSeed(t, w.db, `DELETE FROM link_artifacts WHERE link_id = $1`, id)
		rtSeed(t, w.db, `DELETE FROM links WHERE id = $1`, id)
	}
}

// A read for a row no one has: FindByID answers no link and the error "link
// not found", for an id no link has, a malformed one and a closed link; every
// list answers nil, no error, for an artifact or a project nothing matches,
// for a malformed id and for a version no link was made at; the suspect queue
// answers an empty, non-nil list for the same.
func TestLinkRepositoryAnswersNoRow(t *testing.T) {
	w := newRTLinkWorld(t)
	a := w.artifact("", "requirement", "Alpha")
	b := w.artifact("", "test-case", "Bravo")
	l := w.save(&links.Link{ID: uuid.New().String(), FromID: a, ToID: b, Type: "verifies", Version: 1,
		ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(0)})
	gone := w.save(&links.Link{ID: uuid.New().String(), FromID: a, ToID: b, Type: "verifies", Version: 1,
		ValidFrom: rtAt(0), CreatedAt: rtAt(1), UpdatedAt: rtAt(1)})
	if err := w.repo.Delete(gone.ID); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{uuid.New().String(), "not-a-uuid", "", gone.ID} {
		got, err := w.repo.FindByID(id)
		if got != nil || err == nil || err.Error() != "link not found" {
			t.Errorf("FindByID(%q): %v, %v; want nil and link not found", id, got, err)
		}
	}
	if got, err := w.repo.FindByID(l.ID); err != nil || got == nil {
		t.Fatalf("FindByID of the live link: %v, %v", got, err)
	}

	nobody := uuid.New().String()
	for _, id := range []string{nobody, "not-a-uuid", ""} {
		list, err := w.repo.FindByFromID(id)
		rtWantNil(t, fmt.Sprintf("FindByFromID(%q)", id), list, err)
		list, err = w.repo.FindByToID(id)
		rtWantNil(t, fmt.Sprintf("FindByToID(%q)", id), list, err)
		list, err = w.repo.FindAll(id)
		rtWantNil(t, fmt.Sprintf("FindAll(%q)", id), list, err)
		list, err = w.repo.FindByFromIDForVersion(id, 1)
		rtWantNil(t, fmt.Sprintf("FindByFromIDForVersion(%q, 1)", id), list, err)
		list, err = w.repo.FindByToIDForVersion(id, 1)
		rtWantNil(t, fmt.Sprintf("FindByToIDForVersion(%q, 1)", id), list, err)
		suspect, err := w.repo.FindSuspectByProject(id)
		rtWantEmpty(t, fmt.Sprintf("FindSuspectByProject(%q)", id), suspect, err)
	}
	// A project with artifacts but no live link, an artifact at a version
	// nothing was recorded for.
	empty := uuid.New().String()
	w.artifact(empty, "requirement", "Lonely")
	list, err := w.repo.FindAll(empty)
	rtWantNil(t, "FindAll of a project with no links", list, err)
	list, err = w.repo.FindByFromIDForVersion(a, 9)
	rtWantNil(t, "FindByFromIDForVersion(a, 9)", list, err)
	list, err = w.repo.FindByToIDForVersion(b, 9)
	rtWantNil(t, "FindByToIDForVersion(b, 9)", list, err)
	suspect, err := w.repo.FindSuspectByProject(w.project)
	rtWantEmpty(t, "FindSuspectByProject with none suspect", suspect, err)
}

// The suspect review queue's nine columns read back whole, each holding a
// value no other does: the link's id, type and updated_at, then each end's
// id, title and type. The titles and types are the artifacts' current ones
// (the older version of an edited artifact is not used); a link is in a
// project's queue when either end is in it, newest updated_at first; a link
// that is not suspect, one that is closed, one with an end no live artifact
// has, and one that touches only another project are left out.
func TestLinkRepositoryFindSuspectByProjectReadsEveryColumn(t *testing.T) {
	w := newRTLinkWorld(t)
	other := uuid.New().String()
	service := artifacts.NewDefaultService(NewArtifactRepository(w.db))

	req := w.artifact("", "requirement", "Alpha requirement")
	title := "Alpha requirement, revised"
	if _, err := service.UpdateArtifact(req, artifacts.UpdateArtifactRequest{Title: &title}); err != nil {
		t.Fatalf("edit the requirement: %v", err)
	}
	test := w.artifact("", "test-case", "Bravo test")
	design := w.artifact(other, "design-item", "Xray design")
	foreign := w.artifact(other, "interface", "Yankee interface")

	mk := func(from, to, typ string, suspect bool, at int, validTo *time.Time) *links.Link {
		t.Helper()
		l := &links.Link{ID: uuid.New().String(), FromID: from, ToID: to, Type: typ, Suspect: suspect, Version: 1,
			ValidFrom: rtAt(-60), ValidTo: validTo, CreatedAt: rtAt(-60), UpdatedAt: rtAt(at).In(rtCEST)}
		if err := w.repo.Save(l); err != nil {
			t.Fatalf("Save: %v", err)
		}
		return l
	}
	closedAt := rtAt(0)
	inside := mk(req, test, "verifies", true, 5, nil)
	crossing := mk(design, req, "refines", true, 15, nil)
	mk(test, req, "verifies", false, 50, nil)
	mk(req, test, "verifies", true, 51, &closedAt)
	mk(req, uuid.New().String(), "verifies", true, 52, nil)
	mk(uuid.New().String(), test, "verifies", true, 53, nil)
	elsewhere := mk(design, foreign, "implements", true, 25, nil)

	want := func(l *links.Link, fromTitle, fromType, toTitle, toType string) *links.SuspectLink {
		return &links.SuspectLink{ID: l.ID, Type: l.Type, FromID: l.FromID, FromTitle: fromTitle, FromType: fromType,
			ToID: l.ToID, ToTitle: toTitle, ToType: toType, UpdatedAt: l.UpdatedAt}
	}
	wantInside := want(inside, title, "requirement", "Bravo test", "test-case")
	wantCrossing := want(crossing, "Xray design", "design-item", title, "requirement")
	wantElsewhere := want(elsewhere, "Xray design", "design-item", "Yankee interface", "interface")

	check := func(project string, wants ...*links.SuspectLink) {
		t.Helper()
		got, err := w.repo.FindSuspectByProject(project)
		if err != nil {
			t.Errorf("FindSuspectByProject: %v", err)
			return
		}
		if len(got) != len(wants) {
			t.Errorf("FindSuspectByProject listed %s, want %s", rtJSON(got), rtJSON(wants))
			return
		}
		for i, g := range got {
			sent := *wants[i]
			sent.UpdatedAt = time.Time{}
			gotSansTime := *g
			gotSansTime.UpdatedAt = time.Time{}
			rtWantSame(t, fmt.Sprintf("suspect link %d", i), gotSansTime, sent)
			rtWantTimestamp(t, fmt.Sprintf("suspect link %d updated_at", i), g.UpdatedAt, wants[i].UpdatedAt)
		}
	}
	// Newest updated_at first: the crossing link, then the one inside.
	check(w.project, wantCrossing, wantInside)
	check(other, wantElsewhere, wantCrossing)
}

// A NULL in a column the suspect queue scans, here one a test has let take
// it, fails the read naming the column and its place among the nine (the
// link's type 1 and updated_at 2; each end's title 4 and type 5, 7 and 8),
// unwrapped, and answers no list (nil, not []). The ids are joined on and
// take no NULL.
func TestLinkRepositoryFindSuspectByProjectScanErrorsNameTheColumn(t *testing.T) {
	w := newRTLinkWorld(t)
	from := w.artifact("", "requirement", "Alpha")
	to := w.artifact("", "test-case", "Bravo")
	l := &links.Link{ID: uuid.New().String(), FromID: from, ToID: to, Type: "verifies", Suspect: true, Version: 1,
		ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := w.repo.Save(l); err != nil {
		t.Fatal(err)
	}
	rtSeed(t, w.db, `ALTER TABLE links ALTER COLUMN type DROP NOT NULL`)
	rtSeed(t, w.db, `ALTER TABLE links ALTER COLUMN updated_at DROP NOT NULL`)
	rtSeed(t, w.db, `ALTER TABLE artifacts ALTER COLUMN title DROP NOT NULL`)
	rtSeed(t, w.db, `ALTER TABLE artifacts ALTER COLUMN type DROP NOT NULL`)

	const str = "converting NULL to string is unsupported"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	for _, c := range []struct {
		name    string
		column  string
		index   int
		message string
		table   string
		id      string
	}{
		{"the link's type", "type", 1, str, "links", l.ID},
		{"the link's updated_at", "updated_at", 2, stamp, "links", l.ID},
		{"the from end's title", "title", 4, str, "artifacts", from},
		{"the from end's type", "type", 5, str, "artifacts", from},
		{"the to end's title", "title", 7, str, "artifacts", to},
		{"the to end's type", "type", 8, str, "artifacts", to},
	} {
		query := fmt.Sprintf(`UPDATE %s SET %s = NULL WHERE id = $1`, c.table, c.column)
		restore := fmt.Sprintf(`UPDATE %s SET %s = $2 WHERE id = $1`, c.table, c.column)
		var old interface{}
		if err := w.db.QueryRow(fmt.Sprintf(`SELECT %s FROM %s WHERE id = $1`, c.column, c.table), c.id).Scan(&old); err != nil {
			t.Fatal(err)
		}
		rtSeed(t, w.db, query, c.id)

		want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
		got, err := w.repo.FindSuspectByProject(w.project)
		if got != nil || err == nil || err.Error() != want {
			t.Errorf("FindSuspectByProject over %s NULL: %v, %v; want nil and %s", c.name, got, err, want)
		}
		rtSeed(t, w.db, restore, c.id, old)
	}
}

// rtLinkRow is the raw columns of a link row, as stored.
type rtLinkRow struct {
	ID, FromID, ToID, Type string
	Suspect                bool
	Attributes             sql.NullString
	Version                int
	ValidFrom              time.Time
	ValidTo                *time.Time
	CreatedAt, UpdatedAt   time.Time
}

func rtReadLinkRow(t *testing.T, db *sql.DB, id string) *rtLinkRow {
	t.Helper()
	r := new(rtLinkRow)
	err := db.QueryRow(`SELECT id::text, from_id::text, to_id::text, type, suspect, attributes::text, version, valid_from, valid_to, created_at, updated_at
		FROM links WHERE id = $1`, id).Scan(&r.ID, &r.FromID, &r.ToID, &r.Type, &r.Suspect, &r.Attributes, &r.Version,
		&r.ValidFrom, &r.ValidTo, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read link %s: %v", id, err)
	}
	return r
}

// Save writes all eleven columns as the link holds them, valid_to among
// them (so a link may be saved closed), attributes encoded as JSON: nil is
// the JSON null (not SQL NULL), an empty map is {}. Saving an id a link has
// is Postgres's refusal as it came, a malformed end the refusal of the uuid,
// attributes JSON cannot encode the encoder's, and the last two write no row.
func TestLinkRepositorySaveWritesEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewLinkRepository(db)
	closedAt := rtAt(40).In(rtCEST)
	l := &links.Link{ID: uuid.New().String(), FromID: uuid.New().String(), ToID: uuid.New().String(), Type: "mitigates",
		Suspect: true, Attributes: map[string]interface{}{"k": "v"}, Version: 6,
		ValidFrom: rtAt(-3).In(rtCEST), ValidTo: &closedAt, CreatedAt: rtAt(7), UpdatedAt: rtAt(9).In(rtCEST)}
	if err := repo.Save(l); err != nil {
		t.Fatalf("Save: %v", err)
	}
	row := rtReadLinkRow(t, db, l.ID)
	if row == nil {
		t.Fatal("Save wrote no row")
	}
	if row.ID != l.ID || row.FromID != l.FromID || row.ToID != l.ToID || row.Type != "mitigates" || !row.Suspect ||
		row.Attributes.String != `{"k": "v"}` || row.Version != 6 {
		t.Errorf("Save wrote %+v, want %s", row, rtJSON(l))
	}
	rtWantTimestamp(t, "Save valid_from", row.ValidFrom, l.ValidFrom)
	rtWantOptionalTimestamp(t, "Save valid_to", row.ValidTo, l.ValidTo)
	rtWantTimestamp(t, "Save created_at", row.CreatedAt, l.CreatedAt)
	rtWantTimestamp(t, "Save updated_at", row.UpdatedAt, l.UpdatedAt)

	for _, c := range []struct {
		name       string
		attributes map[string]interface{}
		want       string
	}{{"nil attributes", nil, "null"}, {"empty attributes", map[string]interface{}{}, "{}"}} {
		l := &links.Link{ID: uuid.New().String(), FromID: uuid.New().String(), ToID: uuid.New().String(), Type: "verifies",
			Attributes: c.attributes, Version: 1, ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.Save(l); err != nil {
			t.Fatalf("Save with %s: %v", c.name, err)
		}
		row := rtReadLinkRow(t, db, l.ID)
		if row == nil || !row.Attributes.Valid || row.Attributes.String != c.want || row.Suspect || row.ValidTo != nil {
			t.Errorf("Save with %s wrote %+v, want attributes %s, not suspect and open", c.name, row, c.want)
		}
	}

	again := *l
	again.Type = "other"
	err := repo.Save(&again)
	rtWantUnwrappedPQ(t, "Save of an id a link has", err, "23505")
	rtWantPQ(t, "Save of an id a link has", err, "23505", "links_pkey")
	if row := rtReadLinkRow(t, db, l.ID); row.Type != "mitigates" {
		t.Errorf("the refused Save changed the row: %+v", row)
	}

	bad := &links.Link{ID: uuid.New().String(), FromID: "not-a-uuid", ToID: uuid.New().String(), Type: "verifies", Version: 1,
		ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	rtWantRefused(t, "Save of a malformed end", repo.Save(bad))
	if rtReadLinkRow(t, db, bad.ID) != nil {
		t.Error("Save of a malformed end wrote a row")
	}
	unencodable := &links.Link{ID: uuid.New().String(), FromID: uuid.New().String(), ToID: uuid.New().String(), Type: "verifies",
		Attributes: rtUnencodable, Version: 1, ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	rtWantUnencodable(t, "Save", repo.Save(unencodable))
	if rtReadLinkRow(t, db, unencodable.ID) != nil {
		t.Error("Save of attributes JSON cannot encode wrote a row")
	}
}

// Update rewrites the type, the suspect flag, the attributes, the version
// and updated_at of a live link, and nothing else: not its ends, valid_from,
// valid_to nor created_at, whatever the link it is given holds. It is no
// error for a link no row has (and writes none), and does not touch a closed
// link; attributes JSON cannot encode fail before the statement and leave
// the row; a malformed id is Postgres's refusal as it came.
func TestLinkRepositoryUpdateWritesItsFiveColumns(t *testing.T) {
	w := newRTLinkWorld(t)
	repo := w.repo
	l := &links.Link{ID: uuid.New().String(), FromID: uuid.New().String(), ToID: uuid.New().String(), Type: "verifies",
		Attributes: map[string]interface{}{"old": true}, Version: 1,
		ValidFrom: rtAt(-3), CreatedAt: rtAt(1), UpdatedAt: rtAt(2)}
	if err := repo.Save(l); err != nil {
		t.Fatal(err)
	}
	before := rtReadLinkRow(t, w.db, l.ID)

	closedAt := rtAt(50)
	sent := &links.Link{ID: l.ID, FromID: uuid.New().String(), ToID: uuid.New().String(), Type: "satisfies", Suspect: true,
		Attributes: map[string]interface{}{"new": float64(1)}, Version: 8,
		ValidFrom: rtAt(-99), ValidTo: &closedAt, CreatedAt: rtAt(-98), UpdatedAt: rtAt(60).In(rtCEST)}
	if err := repo.Update(sent); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := rtReadLinkRow(t, w.db, l.ID)
	if after.Type != "satisfies" || !after.Suspect || after.Attributes.String != `{"new": 1}` || after.Version != 8 {
		t.Errorf("Update wrote %+v, want type, suspect, attributes and version of %s", after, rtJSON(sent))
	}
	rtWantTimestamp(t, "Update updated_at", after.UpdatedAt, sent.UpdatedAt)
	if after.FromID != before.FromID || after.ToID != before.ToID || !after.ValidFrom.Equal(before.ValidFrom) ||
		after.ValidTo != nil || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("Update changed the columns it never writes: %+v, was %+v", after, before)
	}

	// Nil attributes are written as the JSON null.
	sent.Attributes = nil
	if err := repo.Update(sent); err != nil {
		t.Fatal(err)
	}
	if got := rtReadLinkRow(t, w.db, l.ID); !got.Attributes.Valid || got.Attributes.String != "null" {
		t.Errorf("Update with nil attributes wrote %+v, want the JSON null", got.Attributes)
	}

	// Attributes JSON cannot encode fail first.
	snapshot := rtReadLinkRow(t, w.db, l.ID)
	sent.Attributes = rtUnencodable
	rtWantUnencodable(t, "Update", repo.Update(sent))
	rtWantSame(t, "the row after the refused Update", rtReadLinkRow(t, w.db, l.ID), snapshot)

	// A link no row has: no error, no row.
	missing := &links.Link{ID: uuid.New().String(), Type: "verifies", Version: 1, UpdatedAt: rtAt(0)}
	if err := repo.Update(missing); err != nil || rtReadLinkRow(t, w.db, missing.ID) != nil {
		t.Errorf("Update of a link no row has: %v, row %+v; want no error and no row", err, rtReadLinkRow(t, w.db, missing.ID))
	}
	rtWantRefused(t, "Update of a malformed id", repo.Update(&links.Link{ID: "not-a-uuid", Type: "verifies", Version: 1}))

	// A closed link is left as it is.
	if err := repo.Delete(l.ID); err != nil {
		t.Fatal(err)
	}
	closed := rtReadLinkRow(t, w.db, l.ID)
	sent.Attributes = map[string]interface{}{"again": true}
	sent.Type = "refines"
	if err := repo.Update(sent); err != nil {
		t.Fatal(err)
	}
	rtWantSame(t, "the closed link after Update", rtReadLinkRow(t, w.db, l.ID), closed)
}

// SetSuspect sets the flag on one live link and stamps updated_at with the
// database's clock; SetSuspectByArtifact sets it on every live link that has
// the artifact at either end, leaving updated_at as it was; neither touches a
// closed link, and neither is an error for an id no row has. Delete closes a
// live link's validity at the database's clock, and again changes nothing.
// A malformed id is Postgres's refusal as it came, from all four.
func TestLinkRepositorySuspectAndDeleteWrites(t *testing.T) {
	w := newRTLinkWorld(t)
	repo := w.repo
	end, other := uuid.New().String(), uuid.New().String()
	mk := func(from, to string) *links.Link {
		l := &links.Link{ID: uuid.New().String(), FromID: from, ToID: to, Type: "verifies", Version: 1,
			ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(1)}
		if err := repo.Save(l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	out, in, unrelated, closed := mk(end, other), mk(other, end), mk(other, other), mk(end, other)
	if err := repo.Delete(closed.ID); err != nil {
		t.Fatal(err)
	}
	closedRow := rtReadLinkRow(t, w.db, closed.ID)
	if closedRow.ValidTo == nil || !closedRow.ValidTo.After(rtAt(1)) {
		t.Fatalf("Delete left valid_to %v, want the database's clock", closedRow.ValidTo)
	}
	if err := repo.Delete(closed.ID); err != nil {
		t.Fatal(err)
	}
	rtWantSame(t, "the closed link after a second Delete", rtReadLinkRow(t, w.db, closed.ID), closedRow)

	if err := repo.SetSuspectByArtifact(end, true); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		l       *links.Link
		suspect bool
	}{"out": {out, true}, "in": {in, true}, "unrelated": {unrelated, false}, "closed": {closed, false}} {
		row := rtReadLinkRow(t, w.db, c.l.ID)
		if row.Suspect != c.suspect {
			t.Errorf("SetSuspectByArtifact left the %s link suspect = %v, want %v", name, row.Suspect, c.suspect)
		}
		if name != "closed" {
			rtWantTimestamp(t, "SetSuspectByArtifact leaves updated_at of the "+name+" link", row.UpdatedAt, rtAt(1))
		}
	}

	if err := repo.SetSuspect(out.ID, false); err != nil {
		t.Fatal(err)
	}
	row := rtReadLinkRow(t, w.db, out.ID)
	if row.Suspect || !row.UpdatedAt.After(rtAt(1).Add(time.Hour)) {
		t.Errorf("SetSuspect left %+v, want a trusted link stamped with the database's clock", row)
	}
	if rtReadLinkRow(t, w.db, in.ID).Suspect != true {
		t.Error("SetSuspect cleared another link")
	}
	if err := repo.SetSuspect(closed.ID, true); err != nil {
		t.Fatal(err)
	}
	rtWantSame(t, "the closed link after SetSuspect", rtReadLinkRow(t, w.db, closed.ID), closedRow)

	for name, err := range map[string]error{
		"SetSuspect":           repo.SetSuspect(uuid.New().String(), true),
		"SetSuspectByArtifact": repo.SetSuspectByArtifact(uuid.New().String(), true),
		"Delete":               repo.Delete(uuid.New().String()),
	} {
		if err != nil {
			t.Errorf("%s of an id no row has: %v, want no error", name, err)
		}
	}
	rtWantRefused(t, "SetSuspect of a malformed id", repo.SetSuspect("not-a-uuid", true))
	rtWantRefused(t, "SetSuspectByArtifact of a malformed id", repo.SetSuspectByArtifact("not-a-uuid", true))
	rtWantRefused(t, "Delete of a malformed id", repo.Delete("not-a-uuid"))
}

// On a database that fails, every method hands the driver's error back as it
// came, unwrapped: the reads answer no link and no list.
func TestLinkRepositoryOnAFailingDatabase(t *testing.T) {
	repo := NewLinkRepository(rtClosedDB(t))
	id := uuid.New().String()
	l := &links.Link{ID: id, FromID: id, ToID: id, Type: "verifies", Version: 1}

	if got, err := repo.FindByID(id); got != nil {
		t.Errorf("FindByID on a failing database answered %v", got)
	} else {
		rtWantClosed(t, "FindByID", err, "")
	}
	for name, read := range map[string]func() (interface{}, error){
		"FindByFromID":           func() (interface{}, error) { return repo.FindByFromID(id) },
		"FindByToID":             func() (interface{}, error) { return repo.FindByToID(id) },
		"FindAll":                func() (interface{}, error) { return repo.FindAll(id) },
		"FindByFromIDForVersion": func() (interface{}, error) { return repo.FindByFromIDForVersion(id, 1) },
		"FindByToIDForVersion":   func() (interface{}, error) { return repo.FindByToIDForVersion(id, 1) },
		"FindSuspectByProject":   func() (interface{}, error) { return repo.FindSuspectByProject(id) },
	} {
		got, err := read()
		rtWantClosed(t, name, err, "")
		if rv := fmt.Sprintf("%#v", got); !strings.Contains(rv, "(nil)") {
			t.Errorf("%s on a failing database answered %s, want a nil list", name, rv)
		}
	}
	rtWantClosed(t, "Save", repo.Save(l), "")
	rtWantClosed(t, "Update", repo.Update(l), "")
	rtWantClosed(t, "SetSuspect", repo.SetSuspect(id, true), "")
	rtWantClosed(t, "SetSuspectByArtifact", repo.SetSuspectByArtifact(id, true), "")
	rtWantClosed(t, "Delete", repo.Delete(id), "")
}
