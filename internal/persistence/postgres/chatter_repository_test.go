package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/chatter"
)

// TestChatterAuthorshipRoundTrip is the issue-#181 regression test: the
// repository persists and reads back the authorship columns (created_by,
// author_name). A user entry carries both; an agent/system entry carries the
// display name with a NULL created_by; a legacy-style entry with neither stays
// blank (no backfill).
func TestChatterAuthorshipRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewChatterRepository(db)

	artifactID := uuid.New().String()
	userID := uuid.New().String()

	userEntry := chatter.NewChatterEntry(artifactID, "human comment", false, "comment")
	userEntry.CreatedBy = &userID
	userEntry.AuthorName = "Dave"

	agentEntry := chatter.NewChatterEntry(artifactID, "agent comment", true, "agent")
	agentEntry.AuthorName = "Reviewer Agent" // CreatedBy stays nil: no user identity.

	legacyEntry := chatter.NewChatterEntry(artifactID, "system note", true, "version-change")
	// legacyEntry: no CreatedBy, no AuthorName — mirrors a pre-authorship row.

	for _, e := range []*chatter.ChatterEntry{userEntry, agentEntry, legacyEntry} {
		if err := repo.Save(e); err != nil {
			t.Fatalf("save %s: %v", e.ID, err)
		}
	}

	entries, err := repo.FindByArtifactID(artifactID)
	if err != nil {
		t.Fatalf("FindByArtifactID: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	byID := map[string]*chatter.ChatterEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}

	gotUser := byID[userEntry.ID]
	if gotUser.CreatedBy == nil || *gotUser.CreatedBy != userID {
		t.Errorf("user entry created_by = %v, want %s", gotUser.CreatedBy, userID)
	}
	if gotUser.AuthorName != "Dave" {
		t.Errorf("user entry author_name = %q, want Dave", gotUser.AuthorName)
	}

	gotAgent := byID[agentEntry.ID]
	if gotAgent.CreatedBy != nil {
		t.Errorf("agent entry created_by = %v, want NULL", gotAgent.CreatedBy)
	}
	if gotAgent.AuthorName != "Reviewer Agent" {
		t.Errorf("agent entry author_name = %q, want Reviewer Agent", gotAgent.AuthorName)
	}

	gotLegacy := byID[legacyEntry.ID]
	if gotLegacy.CreatedBy != nil {
		t.Errorf("legacy entry created_by = %v, want NULL", gotLegacy.CreatedBy)
	}
	if gotLegacy.AuthorName != "" {
		t.Errorf("legacy entry author_name = %q, want empty", gotLegacy.AuthorName)
	}
}
