package projects

import (
	"testing"
	"time"
)

type stampRepo struct {
	CoverRepository
	stamps map[string]time.Time
	asked  [][]string
}

func (r *stampRepo) CoverStamps(ids []string) (map[string]time.Time, error) {
	r.asked = append(r.asked, ids)
	return r.stamps, nil
}

// Stamp reads the whole list at once, marks the projects that have a cover
// with when it was stored (in UTC), and clears a stale mark from one that
// no longer has one.
func TestStampMarksOnlyProjectsWithACover(t *testing.T) {
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	repo := &stampRepo{stamps: map[string]time.Time{"a": at}}
	stale := at
	a, b := &Project{ID: "a"}, &Project{ID: "b", CoverUpdatedAt: &stale}

	if err := NewCoverService(repo).Stamp(a, nil, b); err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if len(repo.asked) != 1 || len(repo.asked[0]) != 2 {
		t.Fatalf("asked the repository %v; want one read of both ids", repo.asked)
	}
	if a.CoverUpdatedAt == nil || !a.CoverUpdatedAt.Equal(at) || a.CoverUpdatedAt.Location() != time.UTC {
		t.Errorf("a: cover_updated_at = %v, want %v in UTC", a.CoverUpdatedAt, at)
	}
	if b.CoverUpdatedAt != nil {
		t.Errorf("b: cover_updated_at = %v, want none", b.CoverUpdatedAt)
	}

	repo.asked = nil
	if err := NewCoverService(repo).Stamp(); err != nil || repo.asked != nil {
		t.Errorf("an empty list read the repository (%v, %v)", repo.asked, err)
	}
}
