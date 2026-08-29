package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// TestArtifactStatusBackfill: migration 0003 adds the status column to a
// pre-ledger database and backfills it from the legacy Attributes["status"]
// mirror — recognizable values (including the legacy "in-review" spelling)
// carry over, everything else defaults to draft, and historical (archived)
// versions are backfilled the same way.
func TestArtifactStatusBackfill(t *testing.T) {
	db := testDB(t)

	// Simulate a database initialized before the ledger and before the
	// status column existed.
	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema (pre-ledger simulation): %v", err)
	}
	var hasColumn bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'artifacts' AND column_name = 'status'
		)`).Scan(&hasColumn); err != nil {
		t.Fatal(err)
	}
	if hasColumn {
		t.Fatal("precondition failed: baseline schema must not have the status column")
	}

	projectID := uuid.New().String()
	insert := func(attributes string, archived bool) string {
		id := uuid.New().String()
		validTo := "NULL"
		if archived {
			validTo = "NOW()"
		}
		if _, err := db.Exec(`
			INSERT INTO artifacts (id, project_id, type, title, body, attributes, version, valid_to)
			VALUES ($1, $2, 'requirement', 'Seeded', '', $3::jsonb, 1, `+validTo+`)
		`, id, projectID, attributes); err != nil {
			t.Fatalf("seed artifact: %v", err)
		}
		return id
	}

	approved := insert(`{"status": "approved"}`, false)
	legacyHyphen := insert(`{"status": "in-review"}`, false)
	underscore := insert(`{"status": "in_review"}`, false)
	noStatus := insert(`{"origin": "import"}`, false)
	bogus := insert(`{"status": "shipped"}`, false)
	archivedApproved := insert(`{"status": "approved"}`, true)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, ok := ledgerRows(t, db)[4]; !ok {
		t.Fatal("expected migration 0004 in the ledger")
	}

	statusOf := func(id string) string {
		t.Helper()
		var s string
		if err := db.QueryRow(`SELECT status FROM artifacts WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatalf("read status of %s: %v", id, err)
		}
		return s
	}

	for _, c := range []struct {
		id   string
		want string
	}{
		{approved, artifacts.StatusApproved},
		{legacyHyphen, artifacts.StatusInReview},
		{underscore, artifacts.StatusInReview},
		{noStatus, artifacts.StatusDraft},
		{bogus, artifacts.StatusDraft},
		{archivedApproved, artifacts.StatusApproved},
	} {
		if got := statusOf(c.id); got != c.want {
			t.Errorf("backfilled status of %s = %q, want %q", c.id, got, c.want)
		}
	}
}

// TestArtifactStatusRoundTrip: the repository persists and reads the status
// column through Save/Update/FindByID/FindVersionsByID, and a status change
// via the domain service creates a new version while the archived approved
// row keeps its status (temporal versioning, issue #127).
func TestArtifactStatusRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewArtifactRepository(db)
	svc := artifacts.NewDefaultService(repo)

	a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
		ProjectID: uuid.New().String(),
		Type:      "requirement",
		Title:     "Round trip",
	})
	if err := repo.Save(a); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.FindByID(a.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status != artifacts.StatusDraft {
		t.Fatalf("fresh artifact status = %q, want draft", got.Status)
	}

	// draft -> in_review -> approved through the domain service.
	if _, err := svc.ChangeStatus(a.ID, artifacts.StatusInReview); err != nil {
		t.Fatalf("draft -> in_review: %v", err)
	}
	approvedVersion, err := svc.ChangeStatus(a.ID, artifacts.StatusApproved)
	if err != nil {
		t.Fatalf("in_review -> approved: %v", err)
	}
	if approvedVersion.Version != 3 {
		t.Errorf("version after two transitions = %d, want 3", approvedVersion.Version)
	}

	// A content edit of the approved artifact demotes the NEW version to
	// draft; the archived approved version keeps its status.
	if _, err := svc.UpdateArtifact(a.ID, artifacts.UpdateArtifactRequest{
		Type: "requirement", Title: "Round trip (edited)",
		Attributes: map[string]interface{}{},
	}); err != nil {
		t.Fatalf("edit approved artifact: %v", err)
	}

	current, err := repo.FindByID(a.ID)
	if err != nil {
		t.Fatalf("find after edit: %v", err)
	}
	if current.Status != artifacts.StatusDraft {
		t.Errorf("status after editing approved artifact = %q, want draft", current.Status)
	}
	if current.Attributes["status"] != artifacts.StatusDraft {
		t.Errorf("attribute mirror = %v, want draft", current.Attributes["status"])
	}

	versions, err := repo.FindVersionsByID(a.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	byVersion := map[int]string{}
	for _, v := range versions {
		byVersion[v.Version] = v.Status
	}
	if byVersion[3] != artifacts.StatusApproved {
		t.Errorf("archived approved version status = %q, want approved (old row must not be touched)", byVersion[3])
	}
	if byVersion[4] != artifacts.StatusDraft {
		t.Errorf("new version status = %q, want draft", byVersion[4])
	}
}
