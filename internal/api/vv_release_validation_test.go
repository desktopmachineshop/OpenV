package api

import (
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/targetreleases"
)

// fakeTargetReleases serves a fixed list of releases.
type fakeTargetReleases struct {
	targetreleases.Service
	list []*targetreleases.Release
}

func (f *fakeTargetReleases) List(string) ([]*targetreleases.Release, error) { return f.list, nil }

func TestValidateTargetReleases(t *testing.T) {
	h := newTestHandler(t, func(h *Handler) {
		h.TargetReleaseService = &fakeTargetReleases{list: []*targetreleases.Release{
			{ID: "mvp", Position: 0}, {ID: "v1", Position: 1},
		}}
	})
	from, until := targetreleases.AttrFrom, targetreleases.AttrUntil
	cases := []struct {
		name       string
		attrs, old map[string]interface{}
		want       error
	}{
		{"no targeting", map[string]interface{}{"priority": "must"}, nil, nil},
		{"a range", map[string]interface{}{from: "mvp", until: "v1"}, nil, nil},
		{"backwards", map[string]interface{}{from: "v1", until: "mvp"}, nil, targetreleases.ErrRangeOrder},
		{"unknown", map[string]interface{}{from: "elsewhere"}, nil, targetreleases.ErrUnknownTarget},
		{"unchanged foreign id on update", map[string]interface{}{from: "elsewhere", "x": 1},
			map[string]interface{}{from: "elsewhere"}, nil},
		{"changed to a foreign id on update", map[string]interface{}{from: "elsewhere"},
			map[string]interface{}{from: "mvp"}, targetreleases.ErrUnknownTarget},
	}
	for _, c := range cases {
		if err := h.validateTargetReleases("p", c.attrs, c.old); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	// A value that is not a string is refused, never compared.
	if err := h.validateTargetReleases("p", map[string]interface{}{from: map[string]interface{}{}},
		map[string]interface{}{from: []interface{}{}}); err == nil {
		t.Error("a non-string target was accepted")
	}
}
