package commentreads

import (
	"errors"
	"testing"
	"time"
)

const a1 = "11111111-1111-1111-1111-111111111111"
const a2 = "22222222-2222-2222-2222-222222222222"

func TestNormalize(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	got, err := Normalize([]Read{
		{ArtifactID: a1},
		{ArtifactID: a2, ReadAt: now.Add(time.Hour)},
		{ArtifactID: a1, ReadAt: past},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ArtifactID != a1 || !got[0].ReadAt.Equal(past) || !got[1].ReadAt.Equal(now) {
		t.Fatalf("normalize = %+v", got)
	}
	for name, in := range map[string][]Read{
		"none":   nil,
		"bad id": {{ArtifactID: "nope"}},
	} {
		if _, err := Normalize(in, now); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	many := make([]Read, MaxPerRequest+1)
	if _, err := Normalize(many, now); !errors.Is(err, ErrTooMany) {
		t.Errorf("too many: %v", err)
	}
}

type fakeRepo struct{ listed []string }

func (f *fakeRepo) List(_, _ string, ids []string) ([]Read, error) { f.listed = ids; return nil, nil }
func (f *fakeRepo) Set(_, _ string, r []Read) ([]Read, error)      { return r, nil }

func TestListDropsNonIDs(t *testing.T) {
	repo := &fakeRepo{}
	s := NewDefaultService(repo)
	got, err := s.List("u", "o", []string{"x"})
	if err != nil || len(got) != 0 || repo.listed != nil {
		t.Fatalf("got %v, %v, listed %v", got, err, repo.listed)
	}
	if _, err := s.List("u", "o", []string{a1, "x"}); err != nil || len(repo.listed) != 1 {
		t.Fatalf("listed %v, %v", repo.listed, err)
	}
}
