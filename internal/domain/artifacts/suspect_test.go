package artifacts

import (
	"testing"
	"time"
)

// fakeSuspectRepo extends the in-memory stub with version history for the
// restore test.
type fakeSuspectRepo struct {
	Repository
	byID     map[string]*Artifact
	updated  *Artifact
	versions []*Artifact
}

func (f *fakeSuspectRepo) FindByID(id string) (*Artifact, error) {
	if a, ok := f.byID[id]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, ErrNotFound
}

func (f *fakeSuspectRepo) Update(a *Artifact) error {
	f.updated = a
	f.byID[a.ID] = a
	return nil
}

func (f *fakeSuspectRepo) FindVersionsByID(id string) ([]*Artifact, error) {
	return f.versions, nil
}

// fakeSuspector records LinkSuspector calls from the artifact service.
type fakeSuspector struct {
	marked  []string
	cleared []string
}

func (f *fakeSuspector) MarkArtifactLinksSuspect(artifactID string) error {
	f.marked = append(f.marked, artifactID)
	return nil
}

func (f *fakeSuspector) ClearArtifactLinksSuspicion(artifactID string) error {
	f.cleared = append(f.cleared, artifactID)
	return nil
}

func newUpdateFixture(t *testing.T) (*DefaultService, *fakeSuspectRepo, *fakeSuspector, *Artifact) {
	t.Helper()
	base := &Artifact{
		ID:        "art-1",
		ProjectID: "proj-1",
		Type:      "requirement",
		Title:     "Original title",
		Body:      "Original body",
		Status:    StatusDraft,
		Attributes: map[string]interface{}{
			"status": StatusDraft,
			"origin": "import",
		},
		Version:   3,
		ValidFrom: time.Now().Add(-time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	repo := &fakeSuspectRepo{byID: map[string]*Artifact{base.ID: base}}
	svc := NewDefaultService(repo)
	suspector := &fakeSuspector{}
	svc.SetLinkSuspector(suspector)
	return svc, repo, suspector, base
}

// sameContentReq builds an update request that repeats the artifact's
// current content (the shape autoVersionLinkedArtifacts sends).
func sameContentReq(a *Artifact) UpdateArtifactRequest {
	return UpdateArtifactRequest{
		ParentID:   a.ParentID,
		Type:       a.Type,
		Title:      a.Title,
		Body:       a.Body,
		Attributes: a.Attributes,
	}
}

// TestUpdateArtifactMarksLinksSuspectOnContentChange: a body edit is a
// content change, so the artifact's links get flagged (issue #131).
func TestUpdateArtifactMarksLinksSuspectOnContentChange(t *testing.T) {
	svc, _, suspector, base := newUpdateFixture(t)

	req := sameContentReq(base)
	req.Body = "Changed body"
	if _, err := svc.UpdateArtifact(base.ID, req); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}

	if len(suspector.marked) != 1 || suspector.marked[0] != base.ID {
		t.Errorf("marked = %v, want exactly [%s]", suspector.marked, base.ID)
	}
	if len(suspector.cleared) != 0 {
		t.Errorf("cleared = %v, want none", suspector.cleared)
	}
}

// TestUpdateArtifactNoSuspicionWithoutContentChange: an update that repeats
// the same type/title/body (attribute-only writes such as the
// links_snapshot refresh, or sort-order moves) must NOT flag links —
// otherwise adding a link would immediately mark the artifact's other
// links suspect.
func TestUpdateArtifactNoSuspicionWithoutContentChange(t *testing.T) {
	svc, _, suspector, base := newUpdateFixture(t)

	req := sameContentReq(base)
	req.Attributes = map[string]interface{}{
		"status":         base.Status,
		"origin":         "import",
		"links_snapshot": []interface{}{"something"},
	}
	order := 42
	req.SortOrder = &order

	if _, err := svc.UpdateArtifact(base.ID, req); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	if len(suspector.marked) != 0 {
		t.Errorf("marked = %v, want none for an attribute/sort-only update", suspector.marked)
	}
}

// TestUpdateArtifactNilSuspectorIsSafe: suspicion tracking is optional.
func TestUpdateArtifactNilSuspectorIsSafe(t *testing.T) {
	svc, _, _, base := newUpdateFixture(t)
	svc.SetLinkSuspector(nil)

	req := sameContentReq(base)
	req.Title = "New title"
	if _, err := svc.UpdateArtifact(base.ID, req); err != nil {
		t.Fatalf("UpdateArtifact with nil suspector: %v", err)
	}
}

// TestChangeStatusApprovalClearsSuspicion: approval implies the reviewer
// reconfirmed traceability, so suspicion on the artifact's links clears.
// Other transitions leave it alone.
func TestChangeStatusApprovalClearsSuspicion(t *testing.T) {
	svc, _, suspector, base := newUpdateFixture(t)

	if _, err := svc.ChangeStatus(base.ID, StatusInReview); err != nil {
		t.Fatalf("ChangeStatus -> in_review: %v", err)
	}
	if len(suspector.cleared) != 0 {
		t.Fatalf("cleared after in_review = %v, want none", suspector.cleared)
	}

	if _, err := svc.ChangeStatus(base.ID, StatusApproved); err != nil {
		t.Fatalf("ChangeStatus -> approved: %v", err)
	}
	if len(suspector.cleared) != 1 || suspector.cleared[0] != base.ID {
		t.Errorf("cleared = %v, want exactly [%s]", suspector.cleared, base.ID)
	}
	if len(suspector.marked) != 0 {
		t.Errorf("marked = %v, want none for status-only transitions", suspector.marked)
	}
}

// TestRestoreMarksLinksSuspect: restoring an old version is a content edit
// when the restored content differs from the current version.
func TestRestoreMarksLinksSuspect(t *testing.T) {
	svc, repo, suspector, base := newUpdateFixture(t)

	// Change the body so version 4 differs from version 3, then restore v3.
	req := sameContentReq(base)
	req.Body = "Edited body"
	if _, err := svc.UpdateArtifact(base.ID, req); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	suspector.marked = nil

	old := *base // snapshot of version 3 content
	repo.versions = []*Artifact{&old}
	if _, err := svc.RestoreArtifactVersion(base.ID, 3); err != nil {
		t.Fatalf("RestoreArtifactVersion: %v", err)
	}
	if len(suspector.marked) != 1 || suspector.marked[0] != base.ID {
		t.Errorf("marked after restore = %v, want exactly [%s]", suspector.marked, base.ID)
	}
}

// TestUpdateArtifactAttributesContract locks in the issue-#125 contract:
// nil attributes = "no change", explicit empty map = clear (the status
// mirror is re-stamped either way).
func TestUpdateArtifactAttributesContract(t *testing.T) {
	t.Run("nil preserves existing attributes", func(t *testing.T) {
		svc, repo, _, base := newUpdateFixture(t)
		req := sameContentReq(base)
		req.Body = "Changed"
		req.Attributes = nil

		updated, err := svc.UpdateArtifact(base.ID, req)
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if got := updated.Attributes["origin"]; got != "import" {
			t.Errorf("origin = %v, want preserved %q", got, "import")
		}
		if repo.updated.Attributes["origin"] != "import" {
			t.Error("persisted artifact lost its attributes on a nil-attributes update")
		}
	})

	t.Run("explicit empty map clears attributes", func(t *testing.T) {
		svc, _, _, base := newUpdateFixture(t)
		req := sameContentReq(base)
		req.Attributes = map[string]interface{}{}

		updated, err := svc.UpdateArtifact(base.ID, req)
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if _, ok := updated.Attributes["origin"]; ok {
			t.Error("origin survived an explicit empty-map replacement")
		}
		// The status mirror is always re-stamped by the domain layer.
		if got := updated.Attributes["status"]; got != StatusDraft {
			t.Errorf("status mirror = %v, want %q", got, StatusDraft)
		}
	})
}

// TestUpdateArtifactStampsValidFrom is the domain half of the issue-#161
// regression: every update opens a fresh validity interval, so the new
// version's ValidFrom must move forward (the repository archives the old
// row with valid_to = this ValidFrom).
func TestUpdateArtifactStampsValidFrom(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	oldValidFrom := base.ValidFrom

	req := sameContentReq(base)
	req.Body = "Changed"
	updated, err := svc.UpdateArtifact(base.ID, req)
	if err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}

	if !updated.ValidFrom.After(oldValidFrom) {
		t.Errorf("ValidFrom = %v, want after previous version's %v", updated.ValidFrom, oldValidFrom)
	}
	if !updated.ValidFrom.Equal(updated.UpdatedAt) {
		t.Errorf("ValidFrom (%v) and UpdatedAt (%v) should carry the same stamp", updated.ValidFrom, updated.UpdatedAt)
	}
	if repo.updated == nil || !repo.updated.ValidFrom.Equal(updated.ValidFrom) {
		t.Error("persisted artifact does not carry the refreshed ValidFrom")
	}
}
