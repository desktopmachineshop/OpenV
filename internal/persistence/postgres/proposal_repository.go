package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/proposals"
)

// ProposalRepository implements proposals.Repository.
type ProposalRepository struct {
	db *sql.DB
}

// NewProposalRepository creates a new proposal repository.
func NewProposalRepository(db *sql.DB) *ProposalRepository {
	return &ProposalRepository{db: db}
}

const proposalColumns = `id, run_id, project_id, op, target_id, payload, status, review_note, applied_entity_id, reviewed_by, created_at, reviewed_at`

func scanProposal(row interface{ Scan(...interface{}) error }) (*proposals.Proposal, error) {
	p := new(proposals.Proposal)
	var targetID, appliedEntityID, reviewedBy sql.NullString
	var reviewedAt sql.NullTime
	var payload []byte
	err := row.Scan(&p.ID, &p.RunID, &p.ProjectID, &p.Op, &targetID, &payload, &p.Status, &p.ReviewNote, &appliedEntityID, &reviewedBy, &p.CreatedAt, &reviewedAt)
	if err != nil {
		return nil, err
	}
	if targetID.Valid {
		v := targetID.String
		p.TargetID = &v
	}
	if appliedEntityID.Valid {
		v := appliedEntityID.String
		p.AppliedEntityID = &v
	}
	if reviewedBy.Valid {
		v := reviewedBy.String
		p.ReviewedBy = &v
	}
	if reviewedAt.Valid {
		t := reviewedAt.Time
		p.ReviewedAt = &t
	}
	if err := json.Unmarshal(payload, &p.Payload); err != nil || p.Payload == nil {
		p.Payload = map[string]interface{}{}
	}
	return p, nil
}

// Save inserts a proposal.
func (r *ProposalRepository) Save(p *proposals.Proposal) error {
	payload, err := json.Marshal(p.Payload)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO agent_proposals (id, run_id, project_id, op, target_id, payload, status, review_note, applied_entity_id, reviewed_by, created_at, reviewed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, p.ID, p.RunID, p.ProjectID, p.Op, p.TargetID, payload, p.Status, p.ReviewNote, p.AppliedEntityID, p.ReviewedBy, p.CreatedAt, p.ReviewedAt)
	return err
}

// Update rewrites a proposal's review state.
func (r *ProposalRepository) Update(p *proposals.Proposal) error {
	_, err := r.db.Exec(`
		UPDATE agent_proposals SET status = $2, review_note = $3, applied_entity_id = $4, reviewed_by = $5, reviewed_at = $6
		WHERE id = $1
	`, p.ID, p.Status, p.ReviewNote, p.AppliedEntityID, p.ReviewedBy, p.ReviewedAt)
	return err
}

// FindByID returns a proposal, or nil.
func (r *ProposalRepository) FindByID(id string) (*proposals.Proposal, error) {
	p, err := scanProposal(r.db.QueryRow(`SELECT `+proposalColumns+` FROM agent_proposals WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// List returns proposals filtered by project/status/run, newest first.
func (r *ProposalRepository) List(projectID, status, runID string) ([]*proposals.Proposal, error) {
	rows, err := r.db.Query(`
		SELECT `+proposalColumns+` FROM agent_proposals
		WHERE ($1 = '' OR project_id = $1::uuid)
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR run_id = $3::uuid)
		ORDER BY created_at DESC
		LIMIT 500
	`, projectID, status, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*proposals.Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// CountByRun counts a run's proposals.
func (r *ProposalRepository) CountByRun(runID string) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_proposals WHERE run_id = $1`, runID).Scan(&count)
	return count, err
}
