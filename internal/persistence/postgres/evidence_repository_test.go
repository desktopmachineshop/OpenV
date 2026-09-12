package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// evidenceFixture is the world a physical test lives in: a workspace, a
// project, three test cases, one run, and a recorded result for each case.
type evidenceFixture struct {
	db        *sql.DB
	repo      *EvidenceRepository
	vvRepo    *VVRepository
	orgID     string
	projectID string
	runID     string
	caseIDs   []string
	resultIDs []string
}

func newEvidenceFixture(t *testing.T) *evidenceFixture {
	t.Helper()
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	f := &evidenceFixture{
		db:        db,
		repo:      NewEvidenceRepository(db),
		vvRepo:    NewVVRepository(db),
		orgID:     uuid.New().String(),
		projectID: uuid.New().String(),
		runID:     uuid.New().String(),
	}
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Acoustics Lab', $2)`,
		f.orgID, "acoustics-"+f.orgID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Cabinet')`,
		f.projectID, f.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO test_runs (id, project_id, name, status) VALUES ($1, $2, 'Noise campaign', 'in-progress')
	`, f.runID, f.projectID); err != nil {
		t.Fatal(err)
	}

	// Three conditions of the same physical test, each its own test case.
	for i, title := range []string{"Idle noise", "Half load noise", "Full load noise"} {
		caseID := uuid.New().String()
		if _, err := db.Exec(`
			INSERT INTO artifacts (id, project_id, type, title, ref, version)
			VALUES ($1, $2, 'test-case', $3, $4, 1)
		`, caseID, f.projectID, title, fmt.Sprintf("TC-%d", i+1)); err != nil {
			t.Fatal(err)
		}
		f.caseIDs = append(f.caseIDs, caseID)

		result := &vv.TestResult{
			ID:              uuid.New().String(),
			RunID:           f.runID,
			TestCaseID:      caseID,
			TestCaseVersion: 1,
			Status:          vv.ResultPass,
			Notes:           "measured on the rig",
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		}
		if err := f.vvRepo.UpsertResult(result); err != nil {
			t.Fatalf("seed result: %v", err)
		}
		f.resultIDs = append(f.resultIDs, result.ID)
	}
	return f
}

// The request this feature exists for: one long capture on a rig, cited by
// every test case it covers.
func TestOneBundleSupportsSeveralTestCases(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	captured := time.Date(2026, 9, 10, 9, 30, 0, 0, time.UTC)
	bundle, err := svc.Create(f.projectID, evidence.CreateRequest{
		Title:      "Noise sweep, 90 minutes, all load conditions",
		Summary:    "One continuous capture covering idle, half and full load.",
		CapturedAt: &captured,
		CapturedBy: "J. Patel, acoustics lab",
		Conditions: map[string]interface{}{"rig": "anechoic chamber 2", "ambient_c": 21.5},
	}, nil)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	if bundle.Ref != "EVD-1" {
		t.Fatalf("first bundle ref is %q, want EVD-1", bundle.Ref)
	}

	if err := svc.AddFile(bundle.ID, &evidence.File{
		Filename: "sweep-20260910.wav", MimeType: "audio/wav",
		FilePath: "/data/uploads/sweep.wav", FileSize: 48_000_000, SHA256: "abc123",
	}); err != nil {
		t.Fatalf("add file: %v", err)
	}

	// All three results rest on the one capture.
	for _, resultID := range f.resultIDs {
		if _, err := svc.Cite(resultID, bundle.ID, ""); err != nil {
			t.Fatalf("cite from %s: %v", resultID, err)
		}
	}

	cited, err := svc.Get(bundle.ID)
	if err != nil {
		t.Fatalf("get bundle: %v", err)
	}
	if len(cited.Citations) != 3 {
		t.Fatalf("bundle is cited by %d results, want 3", len(cited.Citations))
	}
	if cited.FileCount != 1 || cited.TotalSize != 48_000_000 {
		t.Fatalf("bundle reports %d files / %d bytes, want 1 / 48000000", cited.FileCount, cited.TotalSize)
	}
	// A citation carries enough to render without four more lookups.
	for _, c := range cited.Citations {
		if c.TestCaseTitle == "" || c.RunName != "Noise campaign" {
			t.Fatalf("citation is missing display fields: %+v", c)
		}
	}

	// And the run grid gets them all in one query, keyed by result.
	byResult, err := svc.CitationsForRun(f.runID)
	if err != nil {
		t.Fatalf("citations for run: %v", err)
	}
	if len(byResult) != 3 {
		t.Fatalf("run has citations against %d results, want 3", len(byResult))
	}
	for _, resultID := range f.resultIDs {
		if got := byResult[resultID]; len(got) != 1 || got[0].BundleRef != "EVD-1" {
			t.Fatalf("result %s cites %+v, want one EVD-1", resultID, got)
		}
	}
}

// The defect this design removes. Evidence used to live in an array ON the
// result row, and the result upsert wrote that column every time — so editing
// a status or a note silently discarded it. A citation is a row of its own,
// keyed to a result id that the upsert preserves, so re-recording the outcome
// cannot touch the evidence behind it.
func TestCitationsSurviveReRecordingTheResult(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	bundle, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "Rig capture"}, nil)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	if _, err := svc.Cite(f.resultIDs[0], bundle.ID, "channel 2 from 00:12"); err != nil {
		t.Fatalf("cite: %v", err)
	}

	// The tester comes back and changes their mind about the outcome, exactly
	// as the run grid does on every edit.
	again := &vv.TestResult{
		ID:              uuid.New().String(), // a fresh id, as the service mints
		RunID:           f.runID,
		TestCaseID:      f.caseIDs[0],
		TestCaseVersion: 1,
		Status:          vv.ResultFail,
		Notes:           "re-read the trace; it breaches at full load",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := f.vvRepo.UpsertResult(again); err != nil {
		t.Fatalf("re-record result: %v", err)
	}
	// The upsert conflicts on (run_id, test_case_id) and returns the ORIGINAL
	// id — which is what the citation is keyed to.
	if again.ID != f.resultIDs[0] {
		t.Fatalf("re-recording changed the result id from %s to %s; citations would be orphaned",
			f.resultIDs[0], again.ID)
	}

	still, err := svc.CitationsForResult(f.resultIDs[0])
	if err != nil {
		t.Fatalf("citations for result: %v", err)
	}
	if len(still) != 1 || still[0].Note != "channel 2 from 00:12" {
		t.Fatalf("after re-recording, the result cites %+v; the evidence was lost", still)
	}
}

// Citing twice is the state the caller already has, not a second fact and not
// an error.
func TestCitingTwiceIsNotAnError(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	bundle, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "Rig capture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cite(f.resultIDs[0], bundle.ID, "first"); err != nil {
		t.Fatalf("first citation: %v", err)
	}
	if _, err := svc.Cite(f.resultIDs[0], bundle.ID, "again"); err != nil {
		t.Fatalf("repeat citation should be accepted quietly, got %v", err)
	}
	list, err := svc.CitationsForResult(f.resultIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("%d citations after citing the same bundle twice, want 1", len(list))
	}
}

// Dropping a citation says "this result no longer rests on that capture". It
// must not destroy the capture, which other results may still cite.
func TestUncitingLeavesTheBundleAndOtherCitations(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	bundle, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "Shared capture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range f.resultIDs {
		if _, err := svc.Cite(id, bundle.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Uncite(f.resultIDs[0], bundle.ID); err != nil {
		t.Fatalf("uncite: %v", err)
	}

	after, err := svc.Get(bundle.ID)
	if err != nil {
		t.Fatalf("the bundle should still exist: %v", err)
	}
	if len(after.Citations) != 2 {
		t.Fatalf("%d citations remain, want 2", len(after.Citations))
	}
}

// Deleting a bundle is the destructive path, and it has to hand back the files
// so their bytes can be removed from the volume too.
func TestDeletingABundleReturnsItsFilesAndClearsCitations(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	bundle, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "Doomed capture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"a.csv", "b.csv"} {
		if err := svc.AddFile(bundle.ID, &evidence.File{
			Filename: name, FilePath: "/data/uploads/" + name, FileSize: int64(100 * (i + 1)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Cite(f.resultIDs[0], bundle.ID, ""); err != nil {
		t.Fatal(err)
	}

	removed, err := svc.Delete(bundle.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("delete returned %d files to unlink, want 2", len(removed))
	}
	if _, err := svc.Get(bundle.ID); !errors.Is(err, evidence.ErrNotFound) {
		t.Fatalf("bundle still readable after delete: %v", err)
	}
	// The citation went with it, rather than pointing at nothing.
	left, err := svc.CitationsForResult(f.resultIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("%d citations survive a deleted bundle", len(left))
	}
}

// The quota protects one shared volume, so it is measured across the whole
// workspace rather than per project.
func TestStorageIsCountedAcrossTheWorkspace(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	// A second project in the same workspace.
	other := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Second')`,
		other, f.orgID); err != nil {
		t.Fatal(err)
	}
	for _, projectID := range []string{f.projectID, other} {
		b, err := svc.Create(projectID, evidence.CreateRequest{Title: "Capture"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.AddFile(b.ID, &evidence.File{
			Filename: "data.bin", FilePath: "/data/uploads/x", FileSize: 30 * 1024 * 1024,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 60 MB used. A 50 MB limit is already blown; a 100 MB one has room for
	// 40 MB more and no more than that.
	if err := svc.CheckQuota(f.projectID, 1, 50*1024*1024); !errors.Is(err, evidence.ErrQuotaExceeded) {
		t.Fatalf("a full workspace accepted an upload: %v", err)
	}
	if err := svc.CheckQuota(f.projectID, 30*1024*1024, 100*1024*1024); err != nil {
		t.Fatalf("an upload that fits was refused: %v", err)
	}
	if err := svc.CheckQuota(f.projectID, 50*1024*1024, 100*1024*1024); !errors.Is(err, evidence.ErrQuotaExceeded) {
		t.Fatalf("an upload that does not fit was accepted")
	}
	// Unlimited stays unlimited.
	if err := svc.CheckQuota(f.projectID, 1<<40, 0); err != nil {
		t.Fatalf("an unlimited workspace refused an upload: %v", err)
	}
}

// Refs are per project and ascend, so two projects both start at EVD-1 and
// neither reuses a number.
func TestBundleRefsAscendPerProject(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)

	other := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Second')`,
		other, f.orgID); err != nil {
		t.Fatal(err)
	}

	var refs []string
	for i := 0; i < 3; i++ {
		b, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "Capture"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, b.Ref)
	}
	if refs[0] != "EVD-1" || refs[1] != "EVD-2" || refs[2] != "EVD-3" {
		t.Fatalf("refs did not ascend: %v", refs)
	}

	elsewhere, err := svc.Create(other, evidence.CreateRequest{Title: "Capture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elsewhere.Ref != "EVD-1" {
		t.Fatalf("a second project started at %q, want EVD-1", elsewhere.Ref)
	}

	// Deleting the newest does not free its number: a reference that has been
	// quoted anywhere must never come to mean something else.
	list, err := svc.List(f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(list[0].ID); err != nil {
		t.Fatal(err)
	}
	next, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "After a delete"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Ref != "EVD-4" {
		t.Fatalf("ref after deleting EVD-3 is %q, want EVD-4 — a number was reissued", next.Ref)
	}
}
