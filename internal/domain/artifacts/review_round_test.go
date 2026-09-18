package artifacts

import (
	"errors"
	"testing"
)

// fakeRoundRepo is an in-memory Repository stub for the review-round tests.
// It keeps the project's artifacts in a slice so ordering is stable, and
// records every Update so a test can assert exactly what the round wrote.
type fakeRoundRepo struct {
	Repository
	all     []*Artifact
	updates []*Artifact
	failIDs map[string]bool
}

func (f *fakeRoundRepo) FindByProjectID(projectID string) ([]*Artifact, error) {
	var out []*Artifact
	for _, a := range f.all {
		if a.ProjectID == projectID {
			copied := *a
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakeRoundRepo) Update(a *Artifact) error {
	if f.failIDs[a.ID] {
		return errors.New("write failed")
	}
	f.updates = append(f.updates, a)
	for i, existing := range f.all {
		if existing.ID == a.ID {
			copied := *a
			f.all[i] = &copied
		}
	}
	return nil
}

// roundArt builds a current artifact in project p1.
func roundArt(id, artifactType, status string) *Artifact {
	a := &Artifact{ID: id, ProjectID: "p1", Type: artifactType, Title: id, Status: status, Version: 1}
	a.syncStatusAttribute()
	return a
}

// statuses reads the repo's current statuses by id, for readable assertions.
func (f *fakeRoundRepo) statuses() map[string]string {
	out := map[string]string{}
	for _, a := range f.all {
		out[a.ID] = a.Status
	}
	return out
}

// TestStartProjectReviewFirstRun: the initial run is the whole point — a
// project full of drafts goes into review in one action, with no artifact
// left behind and no reviewer walking the tree.
func TestStartProjectReviewFirstRun(t *testing.T) {
	repo := &fakeRoundRepo{all: []*Artifact{
		roundArt("r1", TypeRequirement, StatusDraft),
		roundArt("r2", TypeRequirement, StatusDraft),
		roundArt("t1", TypeTestCase, StatusDraft),
	}}
	svc := NewDefaultService(repo)

	result, err := svc.StartProjectReview("p1", ReviewRoundRequest{})
	if err != nil {
		t.Fatalf("StartProjectReview: %v", err)
	}
	if len(result.Moved) != 3 {
		t.Fatalf("moved %d artifacts, want 3", len(result.Moved))
	}
	for id, status := range repo.statuses() {
		if status != StatusInReview {
			t.Errorf("%s = %q, want in_review", id, status)
		}
	}
	// The status column is authoritative, but the deprecated attribute mirror
	// still backs older readers and must not drift (status.go).
	for _, a := range result.Moved {
		if a.Attributes["status"] != StatusInReview {
			t.Errorf("%s attribute mirror = %v, want in_review", a.ID, a.Attributes["status"])
		}
		if a.Version != 2 {
			t.Errorf("%s version = %d, want 2 — a round's move is a new temporal version", a.ID, a.Version)
		}
	}
}

// TestStartProjectReviewSecondRun is the behaviour the process exists for:
// run it again and an untouched approved artifact stays approved, while one
// whose content was edited — which UpdateArtifact has already demoted to
// draft — is pulled back into review. Nothing re-asks a reviewer to sign the
// same words twice.
func TestStartProjectReviewSecondRun(t *testing.T) {
	repo := &fakeRoundRepo{all: []*Artifact{
		roundArt("approved-untouched", TypeRequirement, StatusApproved),
		roundArt("edited-since-approval", TypeRequirement, StatusDraft),
		roundArt("still-waiting", TypeRequirement, StatusInReview),
		roundArt("retired", TypeRequirement, StatusSuperseded),
		roundArt("brand-new", TypeRequirement, StatusDraft),
	}}
	svc := NewDefaultService(repo)

	result, err := svc.StartProjectReview("p1", ReviewRoundRequest{})
	if err != nil {
		t.Fatalf("StartProjectReview: %v", err)
	}

	got := repo.statuses()
	want := map[string]string{
		"approved-untouched":    StatusApproved,
		"edited-since-approval": StatusInReview,
		"still-waiting":         StatusInReview,
		"retired":               StatusSuperseded,
		"brand-new":             StatusInReview,
	}
	for id, wantStatus := range want {
		if got[id] != wantStatus {
			t.Errorf("%s = %q, want %q", id, got[id], wantStatus)
		}
	}

	if len(result.Moved) != 2 {
		t.Errorf("moved %d, want 2 (the edited one and the new one)", len(result.Moved))
	}
	if result.Approved != 1 {
		t.Errorf("approved = %d, want 1", result.Approved)
	}
	if result.AlreadyInReview != 1 {
		t.Errorf("already_in_review = %d, want 1", result.AlreadyInReview)
	}
	if result.Superseded != 1 {
		t.Errorf("superseded = %d, want 1", result.Superseded)
	}

	// Only the two drafts were written: an approved artifact must not get a
	// new version out of a round it was not part of.
	if len(repo.updates) != 2 {
		t.Errorf("wrote %d artifacts, want 2 — a round touches only what it moves", len(repo.updates))
	}
}

// TestStartProjectReviewIsIdempotent: running the round twice with nothing in
// between is a no-op the second time, so a reviewer who clicks it again does
// not churn versions or re-notify anyone.
func TestStartProjectReviewIsIdempotent(t *testing.T) {
	repo := &fakeRoundRepo{all: []*Artifact{roundArt("r1", TypeRequirement, StatusDraft)}}
	svc := NewDefaultService(repo)

	if _, err := svc.StartProjectReview("p1", ReviewRoundRequest{}); err != nil {
		t.Fatalf("first round: %v", err)
	}
	writesAfterFirst := len(repo.updates)

	second, err := svc.StartProjectReview("p1", ReviewRoundRequest{})
	if err != nil {
		t.Fatalf("second round: %v", err)
	}
	if len(second.Moved) != 0 {
		t.Errorf("second round moved %d artifacts, want 0", len(second.Moved))
	}
	if len(repo.updates) != writesAfterFirst {
		t.Errorf("second round wrote %d more artifacts, want 0", len(repo.updates)-writesAfterFirst)
	}
}

// TestStartProjectReviewScope: headings and descriptions are structure and
// narration, not claims to sign off, so the default scope leaves them alone —
// and a caller that wants them names them.
func TestStartProjectReviewScope(t *testing.T) {
	newRepo := func() *fakeRoundRepo {
		return &fakeRoundRepo{all: []*Artifact{
			roundArt("h1", TypeHeading, StatusDraft),
			roundArt("d1", TypeDescription, StatusDraft),
			roundArt("r1", TypeRequirement, StatusDraft),
		}}
	}

	t.Run("default scope skips structure", func(t *testing.T) {
		repo := newRepo()
		result, err := NewDefaultService(repo).StartProjectReview("p1", ReviewRoundRequest{})
		if err != nil {
			t.Fatalf("StartProjectReview: %v", err)
		}
		got := repo.statuses()
		if got["h1"] != StatusDraft || got["d1"] != StatusDraft {
			t.Errorf("structure statuses = %v, want both still draft", got)
		}
		if got["r1"] != StatusInReview {
			t.Errorf("r1 = %q, want in_review", got["r1"])
		}
		if result.OutOfScope != 2 {
			t.Errorf("out_of_scope = %d, want 2", result.OutOfScope)
		}
	})

	t.Run("an explicit scope is honoured", func(t *testing.T) {
		repo := newRepo()
		result, err := NewDefaultService(repo).StartProjectReview("p1", ReviewRoundRequest{Types: []string{TypeHeading}})
		if err != nil {
			t.Fatalf("StartProjectReview: %v", err)
		}
		got := repo.statuses()
		if got["h1"] != StatusInReview {
			t.Errorf("h1 = %q, want in_review", got["h1"])
		}
		if got["r1"] != StatusDraft {
			t.Errorf("r1 = %q, want draft — it was not in the named scope", got["r1"])
		}
		if len(result.Types) != 1 || result.Types[0] != TypeHeading {
			t.Errorf("result types = %v, want [heading]", result.Types)
		}
	})

	t.Run("an unknown type is refused before anything is written", func(t *testing.T) {
		repo := newRepo()
		_, err := NewDefaultService(repo).StartProjectReview("p1", ReviewRoundRequest{Types: []string{TypeRequirement, "nonsense"}})
		if !errors.Is(err, ErrInvalidType) {
			t.Fatalf("err = %v, want ErrInvalidType", err)
		}
		if len(repo.updates) != 0 {
			t.Errorf("wrote %d artifacts, want 0 — a bad scope must not half-run the round", len(repo.updates))
		}
	})
}

// TestStartProjectReviewSurvivesOneFailure: a round that gave up halfway
// would leave the project in a state nobody asked for and no record of where
// it stopped, so a failed artifact is skipped and the rest still go in.
func TestStartProjectReviewSurvivesOneFailure(t *testing.T) {
	repo := &fakeRoundRepo{
		all: []*Artifact{
			roundArt("r1", TypeRequirement, StatusDraft),
			roundArt("r2", TypeRequirement, StatusDraft),
			roundArt("r3", TypeRequirement, StatusDraft),
		},
		failIDs: map[string]bool{"r2": true},
	}

	result, err := NewDefaultService(repo).StartProjectReview("p1", ReviewRoundRequest{})
	if err != nil {
		t.Fatalf("StartProjectReview: %v", err)
	}
	if len(result.Moved) != 2 {
		t.Fatalf("moved %d, want 2", len(result.Moved))
	}
	got := repo.statuses()
	if got["r1"] != StatusInReview || got["r3"] != StatusInReview {
		t.Errorf("statuses = %v, want r1 and r3 in review", got)
	}
	if got["r2"] != StatusDraft {
		t.Errorf("r2 = %q, want draft — its write failed", got["r2"])
	}
}

// TestStartProjectReviewIgnoresOtherProjects guards the tenancy boundary: the
// round is scoped to one project's artifacts and nothing else.
func TestStartProjectReviewIgnoresOtherProjects(t *testing.T) {
	other := roundArt("elsewhere", TypeRequirement, StatusDraft)
	other.ProjectID = "p2"
	repo := &fakeRoundRepo{all: []*Artifact{roundArt("r1", TypeRequirement, StatusDraft), other}}

	if _, err := NewDefaultService(repo).StartProjectReview("p1", ReviewRoundRequest{}); err != nil {
		t.Fatalf("StartProjectReview: %v", err)
	}
	if got := repo.statuses()["elsewhere"]; got != StatusDraft {
		t.Errorf("other project's artifact = %q, want draft", got)
	}
}

// TestDefaultRoundTypesIsACopy: the exported default must not be a handle on
// the package's own slice, or one caller's narrowing would change everybody's
// scope.
func TestDefaultRoundTypesIsACopy(t *testing.T) {
	got := DefaultRoundTypes()
	got[0] = "tampered"
	if DefaultRoundTypes()[0] == "tampered" {
		t.Error("DefaultRoundTypes returned the package slice itself")
	}
}
