package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/proposals"
)

// TestProposalRepositoryRefRoundTrip proves migration 0017 plus the repository
// wiring: a proposal's temporary ref token survives Save -> FindByID -> List,
// and a proposal that mints no token reads back as the empty string (the column
// default), never NULL (issue #235).
func TestProposalRepositoryRefRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	agentID := uuid.New().String()
	runID := uuid.New().String()
	projectID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO agents (id, slug, name, provider) VALUES ($1, 'tc', 'TC', 'claude-code')`, agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO agent_runs (id, agent_id, prompt) VALUES ($1, $2, 'go')`, runID, agentID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	repo := NewProposalRepository(db)

	withRef := &proposals.Proposal{
		ID:        uuid.New().String(),
		RunID:     runID,
		ProjectID: projectID,
		Op:        proposals.OpCreateArtifact,
		Payload:   map[string]interface{}{"title": "Verify login"},
		Ref:       "tc1",
		Status:    proposals.StatusPending,
		CreatedAt: time.Now(),
	}
	noRef := &proposals.Proposal{
		ID:        uuid.New().String(),
		RunID:     runID,
		ProjectID: projectID,
		Op:        proposals.OpCreateLink,
		Payload:   map[string]interface{}{"from_id": "tc1", "to_id": "req-1", "type": "verifies"},
		Status:    proposals.StatusPending,
		CreatedAt: time.Now(),
	}
	if err := repo.Save(withRef); err != nil {
		t.Fatalf("save withRef: %v", err)
	}
	if err := repo.Save(noRef); err != nil {
		t.Fatalf("save noRef: %v", err)
	}

	got, err := repo.FindByID(withRef.ID)
	if err != nil || got == nil {
		t.Fatalf("find withRef: %v (got %v)", err, got)
	}
	if got.Ref != "tc1" {
		t.Fatalf("Ref = %q, want tc1", got.Ref)
	}

	gotNoRef, err := repo.FindByID(noRef.ID)
	if err != nil || gotNoRef == nil {
		t.Fatalf("find noRef: %v", err)
	}
	if gotNoRef.Ref != "" {
		t.Fatalf("a proposal with no token should read back ref=\"\", got %q", gotNoRef.Ref)
	}

	list, err := repo.List("", "", runID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var seenRef string
	for _, p := range list {
		if p.ID == withRef.ID {
			seenRef = p.Ref
		}
	}
	if seenRef != "tc1" {
		t.Fatalf("List did not carry the ref token; got %q", seenRef)
	}
}
