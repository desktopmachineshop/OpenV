package artifacts

import (
	"encoding/json"
	"testing"
	"time"
)

// fakeSuspectRepo extends the in-memory stub with version history for the
// restore test.
type fakeSuspectRepo struct {
	Repository
	byID               map[string]*Artifact
	updated            *Artifact
	versions           []*Artifact
	nextSortOrderCalls int
}

// NextSortOrder returns a sentinel order so tests can tell when a parent
// change triggered auto-assignment.
func (f *fakeSuspectRepo) NextSortOrder(projectID string, parentID *string) (int, error) {
	f.nextSortOrderCalls++
	return 99, nil
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

// strPtr returns a pointer to s (UpdateArtifactRequest content fields are
// pointers: nil = no change, pointer-to-value = replace; issue #170).
func strPtr(s string) *string { return &s }

// sameContentReq builds an update request that repeats the artifact's
// current content (the shape autoVersionLinkedArtifacts used to send;
// it now omits content fields entirely, which the nil-pointer contract
// treats identically).
func sameContentReq(a *Artifact) UpdateArtifactRequest {
	return UpdateArtifactRequest{
		ParentID:   PresentString(a.ParentID),
		Type:       strPtr(a.Type),
		Title:      strPtr(a.Title),
		Body:       strPtr(a.Body),
		Attributes: a.Attributes,
	}
}

// TestUpdateArtifactMarksLinksSuspectOnContentChange: a body edit is a
// content change, so the artifact's links get flagged (issue #131).
func TestUpdateArtifactMarksLinksSuspectOnContentChange(t *testing.T) {
	svc, _, suspector, base := newUpdateFixture(t)

	req := sameContentReq(base)
	req.Body = strPtr("Changed body")
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
	req.Title = strPtr("New title")
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
	req.Body = strPtr("Edited body")
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
		req.Body = strPtr("Changed")
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

// TestUpdateArtifactContentFieldsContract locks in the issue-#170 contract:
// nil Type/Title/Body = "no change" (values carry forward, no suspect-link
// flagging), while an explicit pointer — even to "" — replaces the value
// and counts as a content change.
func TestUpdateArtifactContentFieldsContract(t *testing.T) {
	t.Run("nil content fields carry current values forward", func(t *testing.T) {
		svc, _, suspector, base := newUpdateFixture(t)

		updated, err := svc.UpdateArtifact(base.ID, UpdateArtifactRequest{
			Attributes: map[string]interface{}{"status": base.Status, "touched": true},
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.Type != "requirement" || updated.Title != "Original title" || updated.Body != "Original body" {
			t.Errorf("content = %q/%q/%q, want carried forward unchanged",
				updated.Type, updated.Title, updated.Body)
		}
		if len(suspector.marked) != 0 {
			t.Errorf("marked = %v, want none for a nil-content update", suspector.marked)
		}
	})

	t.Run("explicit empty body replaces and is a content change", func(t *testing.T) {
		svc, _, suspector, base := newUpdateFixture(t)

		req := sameContentReq(base)
		req.Body = strPtr("")
		updated, err := svc.UpdateArtifact(base.ID, req)
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.Body != "" {
			t.Errorf("body = %q, want cleared by explicit empty pointer", updated.Body)
		}
		if len(suspector.marked) != 1 {
			t.Errorf("marked = %v, want the artifact flagged for an explicit body wipe", suspector.marked)
		}
	})
}

// TestUpdateArtifactParentContract locks in the issue-#172 contract:
// parent_id is presence-aware. Omitted = keep the current parent (the old
// *string field decoded omitted and explicit null to the same nil, so every
// parent-less update silently reparented the artifact to root). Explicit
// null = move to root. A set ID = reparent (with sort order auto-assigned
// unless the request also carries one). Parent moves are structural, never
// content changes, so they must not flag links suspect.
func TestUpdateArtifactParentContract(t *testing.T) {
	parented := func(t *testing.T) (*DefaultService, *fakeSuspectRepo, *fakeSuspector, *Artifact) {
		svc, repo, suspector, base := newUpdateFixture(t)
		base.ParentID = strPtr("par-1")
		return svc, repo, suspector, base
	}

	t.Run("omitted parent keeps the current parent", func(t *testing.T) {
		svc, repo, _, base := parented(t)

		updated, err := svc.UpdateArtifact(base.ID, UpdateArtifactRequest{
			Title: strPtr("New title"),
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.ParentID == nil || *updated.ParentID != "par-1" {
			t.Errorf("ParentID = %v, want par-1 carried forward on an omitted parent_id", updated.ParentID)
		}
		if repo.nextSortOrderCalls != 0 {
			t.Errorf("NextSortOrder calls = %d, want 0 (no parent change)", repo.nextSortOrderCalls)
		}
	})

	t.Run("explicit null moves to root", func(t *testing.T) {
		svc, repo, suspector, base := parented(t)

		updated, err := svc.UpdateArtifact(base.ID, UpdateArtifactRequest{
			ParentID: PresentString(nil),
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.ParentID != nil {
			t.Errorf("ParentID = %v, want nil (moved to root)", *updated.ParentID)
		}
		if repo.nextSortOrderCalls != 1 || updated.SortOrder != 99 {
			t.Errorf("NextSortOrder calls = %d, SortOrder = %d; want auto-assigned order 99 after the move",
				repo.nextSortOrderCalls, updated.SortOrder)
		}
		if len(suspector.marked) != 0 {
			t.Errorf("marked = %v, want none: a structural move is not a content change", suspector.marked)
		}
	})

	t.Run("set ID reparents", func(t *testing.T) {
		svc, repo, suspector, base := parented(t)

		updated, err := svc.UpdateArtifact(base.ID, UpdateArtifactRequest{
			ParentID: PresentString(strPtr("par-2")),
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.ParentID == nil || *updated.ParentID != "par-2" {
			t.Errorf("ParentID = %v, want par-2", updated.ParentID)
		}
		if repo.nextSortOrderCalls != 1 {
			t.Errorf("NextSortOrder calls = %d, want 1 (reparent auto-assigns order)", repo.nextSortOrderCalls)
		}
		if len(suspector.marked) != 0 {
			t.Errorf("marked = %v, want none for a reparent", suspector.marked)
		}
	})

	t.Run("present same parent is a no-op move", func(t *testing.T) {
		svc, repo, _, base := parented(t)

		updated, err := svc.UpdateArtifact(base.ID, UpdateArtifactRequest{
			ParentID: PresentString(strPtr("par-1")),
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.ParentID == nil || *updated.ParentID != "par-1" {
			t.Errorf("ParentID = %v, want par-1 unchanged", updated.ParentID)
		}
		if repo.nextSortOrderCalls != 0 {
			t.Errorf("NextSortOrder calls = %d, want 0 (same parent, no reorder)", repo.nextSortOrderCalls)
		}
	})
}

// TestUpdateArtifactRequestParentJSON pins the wire contract for parent_id
// and its survival of the proposal queue's JSON round trip (the request is
// marshaled into a payload map at propose time and re-decoded on approval;
// an omitted field must stay omitted, not degrade into null = move-to-root).
func TestUpdateArtifactRequestParentJSON(t *testing.T) {
	decode := func(t *testing.T, raw string) UpdateArtifactRequest {
		t.Helper()
		var req UpdateArtifactRequest
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		return req
	}

	t.Run("omitted decodes as not present", func(t *testing.T) {
		req := decode(t, `{"title":"T"}`)
		if req.ParentID.Present {
			t.Error("omitted parent_id must decode as Present=false")
		}
	})

	t.Run("null decodes as present with nil value", func(t *testing.T) {
		req := decode(t, `{"parent_id":null}`)
		if !req.ParentID.Present || req.ParentID.Value != nil {
			t.Errorf("parent_id:null = %+v, want Present with nil Value", req.ParentID)
		}
	})

	t.Run("set decodes as present with value", func(t *testing.T) {
		req := decode(t, `{"parent_id":"abc"}`)
		if !req.ParentID.Present || req.ParentID.Value == nil || *req.ParentID.Value != "abc" {
			t.Errorf("parent_id:\"abc\" = %+v, want Present with value abc", req.ParentID)
		}
	})

	// roundTrip mimics maybePropose + the proposal applier: struct -> JSON
	// -> generic map -> JSON -> struct.
	roundTrip := func(t *testing.T, req UpdateArtifactRequest) UpdateArtifactRequest {
		t.Helper()
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("to map: %v", err)
		}
		raw2, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		return decode(t, string(raw2))
	}

	t.Run("omitted survives the proposal round trip", func(t *testing.T) {
		out := roundTrip(t, UpdateArtifactRequest{Title: strPtr("T")})
		if out.ParentID.Present {
			t.Error("omitted parent_id came back present after the round trip (omitzero tag lost?)")
		}
	})

	t.Run("null survives the proposal round trip", func(t *testing.T) {
		out := roundTrip(t, UpdateArtifactRequest{ParentID: PresentString(nil)})
		if !out.ParentID.Present || out.ParentID.Value != nil {
			t.Errorf("explicit null came back as %+v, want Present with nil Value", out.ParentID)
		}
	})

	t.Run("set value survives the proposal round trip", func(t *testing.T) {
		out := roundTrip(t, UpdateArtifactRequest{ParentID: PresentString(strPtr("par-9"))})
		if !out.ParentID.Present || out.ParentID.Value == nil || *out.ParentID.Value != "par-9" {
			t.Errorf("set parent came back as %+v, want par-9", out.ParentID)
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
	req.Body = strPtr("Changed")
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
