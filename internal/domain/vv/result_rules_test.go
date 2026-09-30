package vv

import (
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// A completed or aborted run is a record: a result sent to it is refused
// with ErrRunClosed, in the words the agent launch on a closed run uses, and
// nothing is stored (#379 bug 5, REQ-13).
func TestAClosedRunTakesNoResult(t *testing.T) {
	for _, status := range []string{RunStatusCompleted, RunStatusAborted} {
		t.Run(status, func(t *testing.T) {
			svc, repo := newEvidenceService(t)
			repo.run.Status = status

			_, err := svc.UpsertResult("run-1", UpsertResultRequest{TestCaseID: "tc-1", Status: ResultPass}, nil, "u-1", "")
			if !errors.Is(err, ErrRunClosed) {
				t.Fatalf("a result for a run that is %s answered %v, want ErrRunClosed", status, err)
			}
			if want := "this test run is " + status + "; only in-progress runs accept new results"; err.Error() != want {
				t.Fatalf("the refusal reads %q, want %q", err.Error(), want)
			}
			if len(repo.added) != 0 {
				t.Fatalf("a refused result was stored: %+v", repo.added)
			}
		})
	}
}

// A run verifies its own project's test cases. Another project's is answered
// exactly as a test case no row has, artifacts.ErrNotFound (the handler's
// 404 "artifact not found"), whatever its type, so the answer tells nothing
// about an artifact outside the run's project (#379 bug 5, and #419's rule
// that what the caller cannot reach answers as what does not exist).
func TestAnotherProjectsTestCaseIsAnsweredAsNone(t *testing.T) {
	for _, typ := range []string{"test-case", "heading"} {
		t.Run(typ, func(t *testing.T) {
			svc, repo := newEvidenceService(t)
			svc.artifactService = &evidenceFakeArtifacts{artifact: &artifacts.Artifact{
				ID: "tc-q", ProjectID: "p-2", Type: typ, Title: "Another project's", Version: 1,
			}}

			_, err := svc.UpsertResult("run-1", UpsertResultRequest{TestCaseID: "tc-q", Status: ResultPass}, nil, "u-1", "")
			if !errors.Is(err, artifacts.ErrNotFound) || err.Error() != artifacts.ErrNotFound.Error() {
				t.Fatalf("another project's %s answered %v, want artifacts.ErrNotFound as it is", typ, err)
			}
			if len(repo.added) != 0 {
				t.Fatalf("a refused result was stored: %+v", repo.added)
			}
		})
	}
}
