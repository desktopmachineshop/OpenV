package artifacts

import (
	"fmt"
	"log/slog"
	"time"
)

// Project-wide review rounds (REQ-165): putting a whole project into review in
// one action instead of walking the tree and flipping each artifact by hand.
//
// The round is STATELESS and re-runnable, which is what makes the "later runs"
// behaviour the process needs fall out of the existing state machine rather
// than a stored round record:
//
//   - the first run finds everything in draft and moves it to in_review;
//   - a later run leaves an approved artifact approved, because approval means
//     a reviewer signed off on that exact content;
//   - a later run picks up a CHANGED artifact, because editing an approved
//     artifact's content already demotes the new version to draft
//     (UpdateArtifact, issue #127) — so by the time the round runs, a changed
//     requirement is a draft again and the round pulls it back into review.
//
// Nothing here demotes an approved artifact on its own: an approved artifact
// that nobody has touched is unchanged by definition, and a round that reset
// it would ask reviewers to sign the same words twice a cycle.

// Round scope defaults. A heading and a description carry no claim to sign
// off on — they title and narrate the artifacts that do — so a round that
// swept them in would bury the requirements under structure in the reviewer's
// queue. They stay reviewable individually, and a caller that wants them in a
// round asks for them by type.
var defaultRoundTypes = []string{
	TypeRequirement,
	TypeUserNeed,
	TypePersona,
	TypeTestCase,
	TypeHazard,
	TypeDesignItem,
	TypeOther,
}

// DefaultRoundTypes returns the artifact types a review round covers when the
// caller names none, in catalog order.
func DefaultRoundTypes() []string {
	out := make([]string, len(defaultRoundTypes))
	copy(out, defaultRoundTypes)
	return out
}

// ReviewRoundRequest scopes one run of a project's review process.
type ReviewRoundRequest struct {
	// Types narrows the round to these artifact types. Empty means
	// DefaultRoundTypes(). Every entry must be in the type catalog.
	Types []string
}

// ReviewRoundResult reports what one run did, artifact by artifact in the
// Moved list and in totals for everything it left alone. The four counts plus
// len(Moved) account for every current artifact in the project, so a caller
// can show "12 sent for review, 30 already approved" without a second read.
type ReviewRoundResult struct {
	// Moved are the artifacts this run took from draft to in_review, in the
	// order they were visited.
	Moved []*Artifact `json:"moved"`
	// AlreadyInReview counts artifacts a previous run (or a person) had
	// already submitted and that are still waiting on a reviewer.
	AlreadyInReview int `json:"already_in_review"`
	// Approved counts approved artifacts left approved: unchanged since
	// somebody signed them off.
	Approved int `json:"approved"`
	// Superseded counts artifacts in the terminal state, which no round
	// reopens.
	Superseded int `json:"superseded"`
	// OutOfScope counts current artifacts whose type the round did not cover.
	OutOfScope int `json:"out_of_scope"`
	// Types is the scope the run actually used, so a caller that named none
	// can report the default it got.
	Types []string `json:"types"`
}

// StartProjectReview runs one round of a project's review process: every
// artifact in scope that is in draft moves to in_review, and everything else
// is left exactly as it is. See the package comment above for why a re-run
// does the right thing without any stored round state.
//
// The move reuses the ordinary state machine (draft -> in_review), so a round
// can never reach a state a person could not reach one artifact at a time.
// Failures are per-artifact and do not abort the round: a round that stopped
// halfway would leave the project in a state nobody asked for and no record of
// where it stopped. A failed artifact is logged and counted as left alone.
func (s *DefaultService) StartProjectReview(projectID string, req ReviewRoundRequest) (*ReviewRoundResult, error) {
	types := req.Types
	if len(types) == 0 {
		types = DefaultRoundTypes()
	}
	inScope := make(map[string]bool, len(types))
	for _, t := range types {
		if !ValidType(t) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidType, t)
		}
		inScope[t] = true
	}

	all, err := s.repo.FindByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	result := &ReviewRoundResult{Moved: []*Artifact{}, Types: types}
	for _, a := range all {
		if !inScope[a.Type] {
			result.OutOfScope++
			continue
		}
		switch NormalizeStatus(a.Status) {
		case StatusInReview:
			result.AlreadyInReview++
		case StatusApproved:
			result.Approved++
		case StatusSuperseded:
			result.Superseded++
		default: // draft
			moved, err := s.submitForReview(a)
			if err != nil {
				slog.Warn("artifacts: review round could not submit artifact",
					"project_id", projectID, "artifact_id", a.ID, "error", err)
				continue
			}
			result.Moved = append(result.Moved, moved)
		}
	}
	return result, nil
}

// submitForReview is ChangeStatus's draft -> in_review move on an artifact
// already in hand, so a round of N artifacts costs N writes rather than 2N
// reads and writes. It deliberately shares the version/timestamp bookkeeping
// with ChangeStatus: a round's move is an ordinary status change and must be
// indistinguishable from one in the artifact's history.
func (s *DefaultService) submitForReview(a *Artifact) (*Artifact, error) {
	a.Status = StatusInReview
	a.syncStatusAttribute()
	a.Version++
	now := time.Now()
	a.ValidFrom = now
	a.UpdatedAt = now
	if err := s.repo.Update(a); err != nil {
		return nil, err
	}
	return a, nil
}
