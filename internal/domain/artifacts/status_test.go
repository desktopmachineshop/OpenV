package artifacts

import (
	"errors"
	"testing"
)

// fakeStatusRepo is an in-memory Repository stub for the status/service
// tests: it embeds the interface so only the touched methods are real.
type fakeStatusRepo struct {
	Repository
	byID    map[string]*Artifact
	updated *Artifact
}

func (f *fakeStatusRepo) FindByID(id string) (*Artifact, error) {
	if a, ok := f.byID[id]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, ErrNotFound
}

func (f *fakeStatusRepo) Update(a *Artifact) error {
	f.updated = a
	f.byID[a.ID] = a
	return nil
}

// TestStatusTransitionTable locks in the review state machine:
// draft <-> in_review -> approved -> superseded, superseded terminal.
func TestStatusTransitionTable(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusDraft, StatusInReview, true},
		{StatusInReview, StatusDraft, true},
		{StatusInReview, StatusApproved, true},
		{StatusApproved, StatusSuperseded, true},

		{StatusDraft, StatusApproved, false},   // no self-approval shortcut
		{StatusDraft, StatusSuperseded, false},
		{StatusDraft, StatusDraft, false},
		{StatusInReview, StatusSuperseded, false},
		{StatusApproved, StatusDraft, false},    // demotion only via content edit
		{StatusApproved, StatusInReview, false},
		{StatusApproved, StatusApproved, false},
		{StatusSuperseded, StatusDraft, false}, // terminal
		{StatusSuperseded, StatusInReview, false},
		{StatusSuperseded, StatusApproved, false},
		{"bogus", StatusInReview, false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.ok {
			t.Errorf("CanTransition(%q, %q) = %v, want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestNormalizeStatus(t *testing.T) {
	cases := map[string]string{
		"":            StatusDraft,
		"bogus":       StatusDraft,
		"draft":       StatusDraft,
		"in-review":   StatusInReview, // legacy issue-#127 spelling
		"in_review":   StatusInReview,
		"approved":    StatusApproved,
		"superseded":  StatusSuperseded,
	}
	for in, want := range cases {
		if got := NormalizeStatus(in); got != want {
			t.Errorf("NormalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNewArtifactStatus: new artifacts default to draft; a legacy
// Attributes["status"] seeds the column (normalized), and the mirror is
// written back.
func TestNewArtifactStatus(t *testing.T) {
	a := NewArtifact(CreateArtifactRequest{ProjectID: "p", Type: TypeRequirement, Title: "t"})
	if a.Status != StatusDraft {
		t.Errorf("default status = %q, want draft", a.Status)
	}
	if a.Attributes["status"] != StatusDraft {
		t.Errorf("attribute mirror = %v, want draft", a.Attributes["status"])
	}

	b := NewArtifact(CreateArtifactRequest{
		ProjectID:  "p",
		Type:       TypeRequirement,
		Title:      "t",
		Attributes: map[string]interface{}{"status": "in-review"},
	})
	if b.Status != StatusInReview {
		t.Errorf("seeded status = %q, want in_review", b.Status)
	}
	if b.Attributes["status"] != StatusInReview {
		t.Errorf("attribute mirror = %v, want in_review", b.Attributes["status"])
	}
}

// TestChangeStatus: a legal transition persists a new temporal version with
// the new status mirrored into attributes; illegal moves return the
// sentinels without persisting anything.
func TestChangeStatus(t *testing.T) {
	newSvc := func(status string) (*DefaultService, *fakeStatusRepo) {
		repo := &fakeStatusRepo{byID: map[string]*Artifact{
			"a1": {ID: "a1", ProjectID: "p", Status: status, Version: 3,
				Attributes: map[string]interface{}{"status": status, "origin": "import"}},
		}}
		return NewDefaultService(repo), repo
	}

	t.Run("legal transition versions and mirrors", func(t *testing.T) {
		svc, repo := newSvc(StatusDraft)
		updated, err := svc.ChangeStatus("a1", StatusInReview)
		if err != nil {
			t.Fatalf("ChangeStatus: %v", err)
		}
		if updated.Status != StatusInReview {
			t.Errorf("status = %q, want in_review", updated.Status)
		}
		if updated.Version != 4 {
			t.Errorf("version = %d, want 4 (status change is a new temporal version)", updated.Version)
		}
		if updated.Attributes["status"] != StatusInReview {
			t.Errorf("attribute mirror = %v, want in_review", updated.Attributes["status"])
		}
		if updated.Attributes["origin"] != "import" {
			t.Errorf("other attributes must survive: origin = %v", updated.Attributes["origin"])
		}
		if repo.updated == nil {
			t.Fatal("repo.Update was not called")
		}
	})

	t.Run("unknown status is ErrInvalidStatus", func(t *testing.T) {
		svc, repo := newSvc(StatusDraft)
		if _, err := svc.ChangeStatus("a1", "shipped"); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("err = %v, want ErrInvalidStatus", err)
		}
		if repo.updated != nil {
			t.Error("invalid status must not persist")
		}
	})

	t.Run("illegal transition is ErrInvalidStatusTransition", func(t *testing.T) {
		svc, repo := newSvc(StatusDraft)
		if _, err := svc.ChangeStatus("a1", StatusApproved); !errors.Is(err, ErrInvalidStatusTransition) {
			t.Fatalf("err = %v, want ErrInvalidStatusTransition", err)
		}
		if repo.updated != nil {
			t.Error("illegal transition must not persist")
		}
	})

	t.Run("empty current status is treated as draft", func(t *testing.T) {
		svc, _ := newSvc("")
		updated, err := svc.ChangeStatus("a1", StatusInReview)
		if err != nil {
			t.Fatalf("ChangeStatus from empty status: %v", err)
		}
		if updated.Status != StatusInReview {
			t.Errorf("status = %q, want in_review", updated.Status)
		}
	})

	t.Run("missing artifact is ErrNotFound", func(t *testing.T) {
		svc, _ := newSvc(StatusDraft)
		if _, err := svc.ChangeStatus("nope", StatusInReview); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

// TestUpdateArtifactStatusPolicy: content edits carry the status forward,
// except approved demotes to draft (the approved snapshot stays in history),
// and a smuggled Attributes["status"] never changes the column (issue #125
// wholesale-attributes trap).
func TestUpdateArtifactStatusPolicy(t *testing.T) {
	newSvc := func(status string) *DefaultService {
		return NewDefaultService(&fakeStatusRepo{byID: map[string]*Artifact{
			"a1": {ID: "a1", ProjectID: "p", Status: status, Version: 1,
				Attributes: map[string]interface{}{"status": status}},
		}})
	}

	t.Run("approved demotes to draft on edit", func(t *testing.T) {
		svc := newSvc(StatusApproved)
		updated, err := svc.UpdateArtifact("a1", UpdateArtifactRequest{
			Type: strPtr(TypeRequirement), Title: strPtr("edited"),
			Attributes: map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.Status != StatusDraft {
			t.Errorf("status after editing approved artifact = %q, want draft", updated.Status)
		}
		if updated.Attributes["status"] != StatusDraft {
			t.Errorf("attribute mirror = %v, want draft", updated.Attributes["status"])
		}
	})

	t.Run("in_review carries forward on edit", func(t *testing.T) {
		svc := newSvc(StatusInReview)
		updated, err := svc.UpdateArtifact("a1", UpdateArtifactRequest{
			Type: strPtr(TypeRequirement), Title: strPtr("edited"),
			Attributes: map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.Status != StatusInReview {
			t.Errorf("status = %q, want in_review", updated.Status)
		}
	})

	t.Run("attributes cannot smuggle an approval", func(t *testing.T) {
		svc := newSvc(StatusDraft)
		updated, err := svc.UpdateArtifact("a1", UpdateArtifactRequest{
			Type: strPtr(TypeRequirement), Title: strPtr("edited"),
			Attributes: map[string]interface{}{"status": StatusApproved},
		})
		if err != nil {
			t.Fatalf("UpdateArtifact: %v", err)
		}
		if updated.Status != StatusDraft {
			t.Errorf("status = %q, want draft (attribute writes must not change the column)", updated.Status)
		}
		if updated.Attributes["status"] != StatusDraft {
			t.Errorf("attribute mirror = %v, want draft", updated.Attributes["status"])
		}
	})
}
