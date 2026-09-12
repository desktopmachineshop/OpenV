package vv

import (
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
)

// Each fake implements only what UpsertResult touches; the embedded interface
// makes the rest a compile-time promise rather than a wall of stubs.

type evidenceFakeRepo struct {
	Repository
	run     *TestRun
	stored  map[string]*TestResult // keyed by run+case, as the real unique index is
	lookups int
}

func (f *evidenceFakeRepo) FindRunByID(string) (*TestRun, error) { return f.run, nil }

func (f *evidenceFakeRepo) FindResultByCase(runID, testCaseID string) (*TestResult, error) {
	f.lookups++
	return f.stored[runID+"|"+testCaseID], nil
}

func (f *evidenceFakeRepo) UpsertResult(r *TestResult) error {
	key := r.RunID + "|" + r.TestCaseID
	// The real table conflicts on (run_id, test_case_id) and keeps the
	// original id, so the fake does too.
	if existing, ok := f.stored[key]; ok {
		r.ID = existing.ID
	}
	copied := *r
	f.stored[key] = &copied
	return nil
}

type evidenceFakeArtifacts struct {
	artifacts.Service
	artifact *artifacts.Artifact
}

func (f *evidenceFakeArtifacts) GetArtifact(string) (*artifacts.Artifact, error) {
	return f.artifact, nil
}

type evidenceFakeChatter struct {
	chatter.Service
}

func (f *evidenceFakeChatter) CreateEntry(*chatter.ChatterEntry) error { return nil }

func newEvidenceService(t *testing.T) (*DefaultService, *evidenceFakeRepo) {
	t.Helper()
	repo := &evidenceFakeRepo{
		run:    &TestRun{ID: "run-1", ProjectID: "p-1", Name: "Noise campaign"},
		stored: map[string]*TestResult{},
	}
	art := &evidenceFakeArtifacts{artifact: &artifacts.Artifact{
		ID: "tc-1", Type: "test-case", Title: "Idle noise", Version: 3,
	}}
	return NewDefaultService(repo, art, &evidenceFakeChatter{}, nil), repo
}

// The run grid sends only status and notes on every edit. Before this, an
// omitted evidence field was written as an empty list, so the commonest edit
// in the product silently destroyed the rarest thing in it.
func TestOmittedEvidenceIsLeftAlone(t *testing.T) {
	svc, repo := newEvidenceService(t)
	repo.stored["run-1|tc-1"] = &TestResult{
		ID: "result-1", RunID: "run-1", TestCaseID: "tc-1",
		Status: ResultPass, Evidence: []string{"att-1", "att-2"},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	got, err := svc.UpsertResult("run-1", UpsertResultRequest{
		TestCaseID: "tc-1",
		Status:     ResultFail,
		Notes:      "re-read the trace",
		// Evidence omitted, exactly as the grid sends it.
	}, nil, "u-1", "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(got.Evidence) != 2 || got.Evidence[0] != "att-1" {
		t.Fatalf("editing the notes discarded the evidence: %+v", got.Evidence)
	}
	if got.Status != ResultFail || got.Notes != "re-read the trace" {
		t.Fatalf("the edit itself was not applied: %+v", got)
	}
}

// An explicit empty list is the caller saying "clear it", and must still work.
func TestAnExplicitEmptyListClearsEvidence(t *testing.T) {
	svc, repo := newEvidenceService(t)
	repo.stored["run-1|tc-1"] = &TestResult{
		ID: "result-1", RunID: "run-1", TestCaseID: "tc-1",
		Status: ResultPass, Evidence: []string{"att-1"},
	}

	got, err := svc.UpsertResult("run-1", UpsertResultRequest{
		TestCaseID: "tc-1",
		Status:     ResultPass,
		Evidence:   []string{},
	}, nil, "u-1", "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(got.Evidence) != 0 {
		t.Fatalf("an explicit empty list did not clear the evidence: %+v", got.Evidence)
	}
}

// A first result for a case has nothing to carry forward, and must not invent
// anything or fail looking.
func TestAFirstResultStartsWithNoEvidence(t *testing.T) {
	svc, repo := newEvidenceService(t)

	got, err := svc.UpsertResult("run-1", UpsertResultRequest{
		TestCaseID: "tc-1",
		Status:     ResultPass,
	}, nil, "u-1", "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got.Evidence == nil || len(got.Evidence) != 0 {
		t.Fatalf("a first result carries %+v, want an empty list", got.Evidence)
	}
	if repo.lookups != 1 {
		t.Fatalf("the carry-forward lookup ran %d times, want 1", repo.lookups)
	}
}

// Supplying evidence outright neither consults the old row nor merges with it:
// the caller said what the evidence is.
func TestSuppliedEvidenceReplacesWithoutALookup(t *testing.T) {
	svc, repo := newEvidenceService(t)
	repo.stored["run-1|tc-1"] = &TestResult{
		ID: "result-1", RunID: "run-1", TestCaseID: "tc-1", Evidence: []string{"old"},
	}

	got, err := svc.UpsertResult("run-1", UpsertResultRequest{
		TestCaseID: "tc-1",
		Status:     ResultPass,
		Evidence:   []string{"new"},
	}, nil, "u-1", "")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(got.Evidence) != 1 || got.Evidence[0] != "new" {
		t.Fatalf("supplied evidence was not used verbatim: %+v", got.Evidence)
	}
	if repo.lookups != 0 {
		t.Fatalf("a supplied list still cost %d lookups", repo.lookups)
	}
}
