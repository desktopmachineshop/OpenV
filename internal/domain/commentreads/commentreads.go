// Package commentreads holds when a member last read an artifact's comments,
// so the Home pages' Needs you lists only the comments that came after.
//
// The read state is one time per member and artifact: a comment is unread
// when it was written after it. Opening an artifact's comments, or Mark read
// on a Needs you row, sets it to now; Mark unread on a comment sets it to just
// before that comment, so it and anything written after it are unread again.
// The member's own comments are never theirs to read, which the client
// decides, since it already knows who wrote each one.
//
// The shape mirrors internal/domain/savedsearches: a Repository the postgres
// package implements and a Service façade the handlers depend on. Every read
// and write is scoped by the member and the workspace in the query itself.
package commentreads

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// MaxPerRequest bounds how many artifacts one read or write names. The Home
// pages ask about the artifacts of the 30 latest comments at most.
const MaxPerRequest = 200

// Read is when a member last read one artifact's comments.
type Read struct {
	ArtifactID string    `json:"artifact_id"`
	ReadAt     time.Time `json:"read_at"`
}

// Validation errors, each a message a client can show.
var (
	ErrArtifactID = errors.New("artifact_id must be an artifact's id")
	ErrTooMany    = errors.New("at most 200 artifacts at a time")
	ErrNone       = errors.New("reads must name at least one artifact")
)

// Normalize validates a write and settles its times: a missing read_at, or
// one in the future, is now. Later entries for the same artifact win.
func Normalize(reads []Read, now time.Time) ([]Read, error) {
	if len(reads) == 0 {
		return nil, ErrNone
	}
	if len(reads) > MaxPerRequest {
		return nil, ErrTooMany
	}
	now = now.UTC()
	index := map[string]int{}
	out := make([]Read, 0, len(reads))
	for _, r := range reads {
		if _, err := uuid.Parse(r.ArtifactID); err != nil {
			return nil, ErrArtifactID
		}
		at := r.ReadAt.UTC()
		if at.IsZero() || at.After(now) {
			at = now
		}
		if i, ok := index[r.ArtifactID]; ok {
			out[i].ReadAt = at
			continue
		}
		index[r.ArtifactID] = len(out)
		out = append(out, Read{ArtifactID: r.ArtifactID, ReadAt: at})
	}
	return out, nil
}

// Repository persists read state.
type Repository interface {
	// List returns the member's read times in the workspace for those of
	// artifactIDs they have one for, in no particular order.
	List(userID, orgID string, artifactIDs []string) ([]Read, error)
	// Set stores each read time, replacing the one already stored. An
	// artifact that is not a live artifact of the workspace is skipped. It
	// returns the reads it stored.
	Set(userID, orgID string, reads []Read) ([]Read, error)
}

// Service is the domain façade the handlers use.
type Service interface {
	List(userID, orgID string, artifactIDs []string) ([]Read, error)
	Set(userID, orgID string, reads []Read) ([]Read, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
	now  func() time.Time
}

// NewDefaultService creates the service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo, now: time.Now}
}

// List answers the member's read times for the artifacts named; ids that are
// not ids are dropped, since they name nothing anyone has read.
func (s *DefaultService) List(userID, orgID string, artifactIDs []string) ([]Read, error) {
	if len(artifactIDs) > MaxPerRequest {
		return nil, ErrTooMany
	}
	ids := make([]string, 0, len(artifactIDs))
	for _, id := range artifactIDs {
		if _, err := uuid.Parse(id); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []Read{}, nil
	}
	return s.repo.List(userID, orgID, ids)
}

// Set validates and stores the reads (see Normalize and Repository.Set).
func (s *DefaultService) Set(userID, orgID string, reads []Read) ([]Read, error) {
	norm, err := Normalize(reads, s.now())
	if err != nil {
		return nil, err
	}
	return s.repo.Set(userID, orgID, norm)
}
