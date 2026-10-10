package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// The artifact repository's round trip (refactor plan X13a's
// characterization, in the manner of S15b), pinned as found: every column
// read back through each read that scans the artifact's fifteen columns,
// what a read answers for a row no one has and an empty list, and both
// transactions, Save's and Update's, committing what they write or, when a
// statement inside them fails, leaving nothing behind (the ref counter
// included). X13a moves the column list into artifactColumns, the scan into
// scanArtifact and the two transactions into withTx; these tests are what
// holds the repository still while it does. The helpers are in
// repository_roundtrip_helpers_test.go.

func rtArtifactSansTimes(a *artifacts.Artifact) artifacts.Artifact {
	c := *a
	c.ValidFrom, c.CreatedAt, c.UpdatedAt, c.ValidTo = time.Time{}, time.Time{}, time.Time{}, nil
	return c
}

func rtArtifactTitles(list []*artifacts.Artifact) []string {
	var titles []string
	for _, a := range list {
		titles = append(titles, a.Title)
	}
	return titles
}

// rtWantArtifact fails unless got is sent as a current version reads back:
// every field but the times equal, each time the TIMESTAMP column's wall
// clock of the time sent, and no valid_to.
func rtWantArtifact(t *testing.T, what string, got, sent *artifacts.Artifact) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtArtifactSansTimes(got), rtArtifactSansTimes(sent))
	rtWantTimestamp(t, what+" valid_from", got.ValidFrom, sent.ValidFrom)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
	if got.ValidTo != nil {
		t.Errorf("%s valid_to read back as %v, want nil", what, got.ValidTo)
	}
}

// rtWantArtifactList fails unless list holds exactly the artifacts of want,
// in the order given, each read back whole.
func rtWantArtifactList(t *testing.T, what string, list []*artifacts.Artifact, err error, want ...*artifacts.Artifact) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var wantTitles []string
	for _, a := range want {
		wantTitles = append(wantTitles, a.Title)
	}
	if got := rtArtifactTitles(list); strings.Join(got, "|") != strings.Join(wantTitles, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantTitles)
		return
	}
	for i := range list {
		rtWantArtifact(t, what+" "+want[i].Title, list[i], want[i])
	}
}

// rtWantArtifactNotFound fails unless a read answered artifacts.ErrNotFound
// and nothing (quirk Q2, resolved: the repository's own sentinel).
func rtWantArtifactNotFound(t *testing.T, what string, got *artifacts.Artifact, err error) {
	t.Helper()
	if got != nil || !errors.Is(err, artifacts.ErrNotFound) || err.Error() != "artifact not found" {
		t.Errorf("%s: %v, %v; want nil and artifacts.ErrNotFound", what, got, err)
	}
}

// rtArtifactRows counts every row, current or not, an artifact id has.
func rtArtifactRows(t *testing.T, repo *ArtifactRepository, id string) int {
	t.Helper()
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func rtArtifact(projectID string, parentID *string, typ, title string, sortOrder int, at time.Time) *artifacts.Artifact {
	return &artifacts.Artifact{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		ParentID:  parentID,
		Type:      typ,
		Title:     title,
		Body:      title + " body",
		SortOrder: sortOrder,
		Status:    "draft",
		Version:   1,
		ValidFrom: at,
		CreatedAt: at,
		UpdatedAt: at,
	}
}

// Every column Save writes reads back through FindByID and each list that
// scans the fifteen columns: the parent, the minted ref, the body, the sort
// order, the status, the attributes (a JSON number comes back a float64,
// nil stays nil and an empty map stays empty), the version and the three
// TIMESTAMP stamps (the wall clock sent, its offset dropped, to the
// microsecond).
func TestArtifactRepositoryReadsBackEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewArtifactRepository(db)
	projectID := uuid.New().String()
	seedProjects(t, db, projectID)

	heading := rtArtifact(projectID, nil, artifacts.TypeHeading, "Heading", 1, rtAt(0).In(rtCEST))
	none := rtArtifact(projectID, nil, artifacts.TypeRequirement, "No attributes", 2, rtAt(1))
	empty := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Empty attributes", 3, rtAt(2))
	empty.Attributes = map[string]interface{}{}
	full := &artifacts.Artifact{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		ParentID:  &heading.ID,
		Type:      artifacts.TypeRequirement,
		Title:     "Every column",
		Body:      "The brake shall stop the spindle.",
		SortOrder: 7,
		Status:    "in_review",
		Attributes: map[string]interface{}{
			"owner": "dave", "count": 2, "weight": 1.5,
			"tags": []interface{}{"a", "b"}, "nested": map[string]interface{}{"k": true},
		},
		Version:   3,
		ValidFrom: rtAt(5).In(rtCEST),
		CreatedAt: rtAt(-10).In(rtCEST),
		UpdatedAt: rtAt(6).In(rtCEST),
	}
	for _, a := range []*artifacts.Artifact{heading, none, empty, full} {
		if err := repo.Save(a); err != nil {
			t.Fatalf("Save(%s): %v", a.Title, err)
		}
	}
	if none.Ref != "REQ-1" || empty.Ref != "REQ-2" || full.Ref != "REQ-3" {
		t.Fatalf("minted refs %q, %q, %q; want REQ-1, REQ-2, REQ-3 in the order saved", none.Ref, empty.Ref, full.Ref)
	}
	// What a read hands back: the sent artifact with its attributes as JSON
	// decodes them.
	fullRead := *full
	fullRead.Attributes = map[string]interface{}{
		"owner": "dave", "count": float64(2), "weight": 1.5,
		"tags": []interface{}{"a", "b"}, "nested": map[string]interface{}{"k": true},
	}

	for _, sent := range []*artifacts.Artifact{heading, none, empty, &fullRead} {
		got, err := repo.FindByID(sent.ID)
		if err != nil {
			t.Fatalf("FindByID(%s): %v", sent.Title, err)
		}
		rtWantArtifact(t, "FindByID "+sent.Title, got, sent)
	}

	// The tree order: the roots by sort order, then the heading's child.
	list, err := repo.FindByProjectID(projectID)
	rtWantArtifactList(t, "FindByProjectID", list, err, heading, none, empty, &fullRead)
	list, err = repo.FindByProjectAndType(projectID, artifacts.TypeRequirement)
	rtWantArtifactList(t, "FindByProjectAndType", list, err, none, empty, &fullRead)
	list, err = repo.FindByProjectAndStatus(projectID, "in_review")
	rtWantArtifactList(t, "FindByProjectAndStatus", list, err, &fullRead)
	list, err = repo.FindPageByProject(projectID, artifacts.TypeRequirement, "dave", 10, 0)
	rtWantArtifactList(t, "FindPageByProject", list, err, &fullRead)
	list, err = repo.FindPageByProject(projectID, "", "", 2, 1)
	rtWantArtifactList(t, "FindPageByProject's second page of two", list, err, none, empty)
	list, err = repo.FindVersionsByID(full.ID)
	rtWantArtifactList(t, "FindVersionsByID", list, err, &fullRead)
}

// A row no repository write leaves (an older schema's, or one written by
// hand): a NULL ref reads as "", a NULL attributes column as nil, and a NULL
// body fails the read, naming the body's place among the columns scanned.
func TestArtifactRepositoryReadsNullColumns(t *testing.T) {
	db := rtDB(t)
	repo := NewArtifactRepository(db)
	projectID, brokenProject := uuid.New().String(), uuid.New().String()
	seedProjects(t, db, projectID, brokenProject)

	bare := uuid.New().String()
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, body, attributes, ref, valid_from, created_at, updated_at)
		VALUES ($1, $2, 'requirement', 'Bare', '', NULL, NULL, $3, $3, $3)`, bare, projectID, rtAt(0))
	want := &artifacts.Artifact{ID: bare, ProjectID: projectID, Type: "requirement", Title: "Bare", Status: "draft",
		Version: 1, ValidFrom: rtAt(0), CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	got, err := repo.FindByID(bare)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	rtWantArtifact(t, "FindByID of a row with no ref and no attributes", got, want)
	list, err := repo.FindByProjectID(projectID)
	rtWantArtifactList(t, "FindByProjectID of a row with no ref and no attributes", list, err, want)

	bodiless := uuid.New().String()
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, body) VALUES ($1, $2, 'requirement', 'No body', NULL)`,
		bodiless, brokenProject)
	const scanErr = `sql: Scan error on column index 6, name "body": converting NULL to string is unsupported`
	if got, err := repo.FindByID(bodiless); got != nil || err == nil || err.Error() != scanErr {
		t.Errorf("FindByID of a row with a NULL body: %v, %v; want %q", got, err, scanErr)
	}
	if got, err := repo.FindByProjectID(brokenProject); got != nil || err == nil || err.Error() != scanErr {
		t.Errorf("FindByProjectID over a row with a NULL body: %v, %v; want %q", got, err, scanErr)
	}
}

// What a read answers for no row: FindByID artifacts.ErrNotFound, for an id
// no row has and for a deleted artifact; each list nil (JSON null, quirk
// Q14), but the search, which answers an empty list; a count 0. A delete of
// an id no row has is no error.
func TestArtifactRepositoryAnswersNoRow(t *testing.T) {
	db := rtDB(t)
	repo := NewArtifactRepository(db)
	projectID := uuid.New().String()
	seedProjects(t, db, projectID)

	got, err := repo.FindByID(uuid.New().String())
	rtWantArtifactNotFound(t, "FindByID of an id no row has", got, err)

	list, err := repo.FindByProjectID(projectID)
	rtWantNil(t, "FindByProjectID of an empty project", list, err)
	list, err = repo.FindByProjectAndType(projectID, artifacts.TypeRequirement)
	rtWantNil(t, "FindByProjectAndType of an empty project", list, err)
	list, err = repo.FindByProjectAndStatus(projectID, "draft")
	rtWantNil(t, "FindByProjectAndStatus of an empty project", list, err)
	list, err = repo.FindPageByProject(projectID, "", "", 10, 0)
	rtWantNil(t, "FindPageByProject of an empty project", list, err)
	list, err = repo.FindVersionsByID(uuid.New().String())
	rtWantNil(t, "FindVersionsByID of an id no row has", list, err)
	hits, err := repo.SearchInProjects([]string{projectID}, "brake", 10, artifacts.SearchOptions{})
	rtWantEmpty(t, "SearchInProjects of an empty project", hits, err)
	hits, err = repo.SearchInProjects(nil, "brake", 10, artifacts.SearchOptions{})
	rtWantEmpty(t, "SearchInProjects of no project", hits, err)
	if n, err := repo.CountByProject(projectID, "", ""); n != 0 || err != nil {
		t.Errorf("CountByProject of an empty project = %d, %v; want 0", n, err)
	}
	if err := repo.Delete(uuid.New().String()); err != nil {
		t.Errorf("Delete of an id no row has: %v, want no error", err)
	}

	gone := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Gone", 1, rtAt(0))
	if err := repo.Save(gone); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(gone.ID); err != nil {
		t.Fatal(err)
	}
	got, err = repo.FindByID(gone.ID)
	rtWantArtifactNotFound(t, "FindByID of a deleted artifact", got, err)
	list, err = repo.FindByProjectID(projectID)
	rtWantNil(t, "FindByProjectID of a project whose one artifact is deleted", list, err)
	// The deleted artifact's history keeps it, closed.
	list, err = repo.FindVersionsByID(gone.ID)
	if err != nil || len(list) != 1 || list[0].ValidTo == nil {
		t.Errorf("FindVersionsByID of a deleted artifact: %s, %v; want its one version, closed", rtJSON(list), err)
	}
}

// Save commits the ref it mints with the row. When the insert fails after
// the ref is minted, nothing is left behind: no row, and no number burned
// from the counter, though the artifact passed in keeps the ref minted for
// it. A ref the caller supplies does not advance the counter when its
// insert fails either. A counter write that fails (a project no row has)
// fails the save before the insert, and an attribute JSON cannot hold fails
// it before the transaction.
func TestArtifactSaveCommitsOrLeavesNothing(t *testing.T) {
	db := rtDB(t)
	repo := NewArtifactRepository(db)
	projectID := uuid.New().String()
	seedProjects(t, db, projectID)

	first := rtArtifact(projectID, nil, artifacts.TypeRequirement, "First", 1, rtAt(0))
	if err := repo.Save(first); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if first.Ref != "REQ-1" {
		t.Fatalf("first ref = %q, want REQ-1", first.Ref)
	}

	// The same id at the same version: the counter's upsert succeeds, then
	// the insert is refused.
	clash := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Clash", 2, rtAt(1))
	clash.ID = first.ID
	rtWantPQ(t, "Save of an id and version a row has", repo.Save(clash), "23505", "")
	if clash.Ref != "REQ-2" {
		t.Errorf("the failed save's artifact holds ref %q, want REQ-2 (minted, then rolled back)", clash.Ref)
	}
	imported := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Imported clash", 3, rtAt(2))
	imported.ID, imported.Ref = first.ID, "REQ-40"
	rtWantPQ(t, "Save of an imported ref over an id and version a row has", repo.Save(imported), "23505", "")
	if got, err := repo.FindByID(first.ID); err != nil || got.Title != "First" || got.Ref != "REQ-1" {
		t.Errorf("after the failed saves FindByID = %s, %v; want the first save's row", rtJSON(got), err)
	}
	if n := rtArtifactRows(t, repo, first.ID); n != 1 {
		t.Errorf("after the failed saves the id has %d rows, want 1", n)
	}

	missing := rtArtifact(uuid.New().String(), nil, artifacts.TypeRequirement, "No project", 1, rtAt(3))
	rtWantPQ(t, "Save into a project no row has", repo.Save(missing), "23503", "artifact_ref_counters_project_id_fkey")
	if missing.Ref != "" {
		t.Errorf("the save that minted nothing left ref %q, want none", missing.Ref)
	}
	if n := rtArtifactRows(t, repo, missing.ID); n != 0 {
		t.Errorf("the save into a project no row has wrote %d rows, want 0", n)
	}

	unencodable := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Unencodable", 4, rtAt(4))
	unencodable.Attributes = rtUnencodable
	rtWantUnencodable(t, "Save", repo.Save(unencodable))
	if unencodable.Ref != "" {
		t.Errorf("the save refused before its transaction minted ref %q, want none", unencodable.Ref)
	}

	// No number was burned: the next save draws REQ-2.
	next := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Next", 5, rtAt(5))
	if err := repo.Save(next); err != nil {
		t.Fatalf("Save after the failures: %v", err)
	}
	if next.Ref != "REQ-2" {
		t.Errorf("ref after the failed saves = %q, want REQ-2", next.Ref)
	}
}

// Update closes the current version at the new one's valid_from and inserts
// the new one, in one transaction. When the insert fails after the close,
// the close is undone and the current version stays current; a ref minted
// for a type change is not burned (the artifact passed in keeps it). A
// counter write that fails fails the update before the close.
func TestArtifactUpdateCommitsOrLeavesNothing(t *testing.T) {
	db := rtDB(t)
	repo := NewArtifactRepository(db)
	projectID := uuid.New().String()
	seedProjects(t, db, projectID)

	original := rtArtifact(projectID, nil, artifacts.TypeRequirement, "Original", 1, rtAt(0))
	if err := repo.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantUnchanged := func(what string) {
		t.Helper()
		got, err := repo.FindByID(original.ID)
		if err != nil {
			t.Fatalf("%s: FindByID: %v", what, err)
		}
		rtWantArtifact(t, what, got, original)
		if n := rtArtifactRows(t, repo, original.ID); n != 1 {
			t.Errorf("%s: the artifact has %d rows, want 1", what, n)
		}
	}

	// The same version again: the close succeeds, then the insert is refused.
	clash := *original
	clash.Title, clash.ValidFrom, clash.UpdatedAt = "Clash", rtAt(10), rtAt(10)
	rtWantPQ(t, "Update to a version the artifact has", repo.Update(&clash), "23505", "")
	wantUnchanged("after the update whose insert failed")

	// A type change clears the ref, so the update mints a test case's.
	retyped := clash
	retyped.Type, retyped.Ref = artifacts.TypeTestCase, ""
	rtWantPQ(t, "Update with a type change to a version the artifact has", repo.Update(&retyped), "23505", "")
	if retyped.Ref != "TC-1" {
		t.Errorf("the failed update's artifact holds ref %q, want TC-1 (minted, then rolled back)", retyped.Ref)
	}
	wantUnchanged("after the retyping update whose insert failed")

	moved := clash
	moved.Version, moved.ProjectID, moved.Ref = 2, uuid.New().String(), ""
	rtWantPQ(t, "Update into a project no row has", repo.Update(&moved), "23503", "artifact_ref_counters_project_id_fkey")
	wantUnchanged("after the update whose counter write failed")

	unencodable := clash
	unencodable.Version, unencodable.Attributes = 2, rtUnencodable
	rtWantUnencodable(t, "Update", repo.Update(&unencodable))
	wantUnchanged("after the update refused before its transaction")

	// No number was burned: a new test case draws TC-1.
	tc := rtArtifact(projectID, nil, artifacts.TypeTestCase, "Test case", 2, rtAt(1))
	if err := repo.Save(tc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if tc.Ref != "TC-1" {
		t.Errorf("test case ref after the failed updates = %q, want TC-1", tc.Ref)
	}

	// An update that succeeds commits both statements.
	edited := *original
	edited.Title, edited.Version, edited.ValidFrom, edited.UpdatedAt = "Edited", 2, rtAt(20).In(rtCEST), rtAt(20).In(rtCEST)
	if err := repo.Update(&edited); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := repo.FindByID(original.ID)
	if err != nil {
		t.Fatalf("FindByID after the update: %v", err)
	}
	rtWantArtifact(t, "FindByID after the update", got, &edited)
	versions, err := repo.FindVersionsByID(original.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("FindVersionsByID after the update: %s, %v; want two versions", rtJSON(versions), err)
	}
	rtWantArtifact(t, "the new version", versions[0], &edited)
	closed := versions[1]
	if closed.Version != 1 || closed.Title != "Original" || closed.ValidTo == nil {
		t.Fatalf("the old version read back as %s, want version 1, closed", rtJSON(closed))
	}
	rtWantTimestamp(t, "the old version's valid_to", *closed.ValidTo, edited.ValidFrom)
}

// On a database that fails every statement, each method hands the failure
// back as it came: the reads from their query, Save and Update from the
// transaction's begin. It needs no server.
func TestArtifactRepositoryOnAFailingDatabase(t *testing.T) {
	repo := NewArtifactRepository(rtClosedDB(t))
	id := uuid.New().String()
	a := rtArtifact(id, nil, artifacts.TypeRequirement, "Closed", 1, rtAt(0))

	rtWantClosed(t, "Save", repo.Save(a), "")
	if a.Ref != "" {
		t.Errorf("Save on a failing database minted ref %q, want none", a.Ref)
	}
	rtWantClosed(t, "Update", repo.Update(a), "")
	got, err := repo.FindByID(id)
	if got != nil {
		t.Errorf("FindByID on a failing database answered %v, want nothing", got)
	}
	rtWantClosed(t, "FindByID", err, "")
	_, err = repo.FindByProjectID(id)
	rtWantClosed(t, "FindByProjectID", err, "")
	_, err = repo.FindByProjectAndType(id, artifacts.TypeRequirement)
	rtWantClosed(t, "FindByProjectAndType", err, "")
	_, err = repo.FindByProjectAndStatus(id, "draft")
	rtWantClosed(t, "FindByProjectAndStatus", err, "")
	_, err = repo.FindPageByProject(id, "", "", 10, 0)
	rtWantClosed(t, "FindPageByProject", err, "")
	_, err = repo.FindVersionsByID(id)
	rtWantClosed(t, "FindVersionsByID", err, "")
}
