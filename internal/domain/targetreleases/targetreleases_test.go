package targetreleases

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

type memRepo struct{ rows map[string]*Release }

func newMemRepo() *memRepo { return &memRepo{rows: map[string]*Release{}} }

func (m *memRepo) Create(r *Release) error { c := *r; m.rows[r.ID] = &c; return nil }
func (m *memRepo) Update(r *Release) error { c := *r; m.rows[r.ID] = &c; return nil }
func (m *memRepo) Delete(id string) error  { delete(m.rows, id); return nil }
func (m *memRepo) Get(id string) (*Release, error) {
	if r, ok := m.rows[id]; ok {
		c := *r
		return &c, nil
	}
	return nil, ErrNotFound
}
func (m *memRepo) ListByProject(projectID string) ([]*Release, error) {
	var out []*Release
	for _, r := range m.rows {
		if r.ProjectID == projectID {
			c := *r
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}
func (m *memRepo) SetPositions(projectID string, ids []string) error {
	for i, id := range ids {
		m.rows[id].Position = i
	}
	return nil
}

func TestCreateOrdersAndValidates(t *testing.T) {
	s := NewDefaultService(newMemRepo(), nil)
	mvp, err := s.Create("p", CreateRequest{Name: " MVP "})
	if err != nil || mvp.Name != "MVP" || mvp.Position != 0 || mvp.Status != StatusPlanned {
		t.Fatalf("create MVP: %+v %v", mvp, err)
	}
	date := "2027-01-31"
	v1, err := s.Create("p", CreateRequest{Name: "V1.0", TargetDate: &date})
	if err != nil || v1.Position != 1 || *v1.TargetDate != date {
		t.Fatalf("create V1.0: %+v %v", v1, err)
	}
	if _, err := s.Create("p", CreateRequest{Name: "mvp"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := s.Create("p", CreateRequest{Name: ""}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("empty name: %v", err)
	}
	bad := "31/01/2027"
	if _, err := s.Create("p", CreateRequest{Name: "X", TargetDate: &bad}); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("bad date: %v", err)
	}
	if _, err := s.Create("p", CreateRequest{Name: "X", Status: "done"}); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("bad status: %v", err)
	}
	// Another project's names do not clash.
	if _, err := s.Create("q", CreateRequest{Name: "MVP"}); err != nil {
		t.Fatalf("other project: %v", err)
	}
}

func TestUpdateStampsReleasedAt(t *testing.T) {
	s := NewDefaultService(newMemRepo(), nil)
	r, _ := s.Create("p", CreateRequest{Name: "MVP"})
	released := StatusReleased
	got, err := s.Update(r.ID, UpdateRequest{Status: &released})
	if err != nil || got.ReleasedAt == nil {
		t.Fatalf("release: %+v %v", got, err)
	}
	planned := StatusPlanned
	empty := ""
	got, _ = s.Update(r.ID, UpdateRequest{Status: &planned, TargetDate: &empty})
	if got.ReleasedAt != nil || got.TargetDate != nil {
		t.Fatalf("back to planned keeps a released_at or a date: %+v", got)
	}
}

func TestDeleteRefusesATargetedRelease(t *testing.T) {
	n := 1
	s := NewDefaultService(newMemRepo(), func(projectID, releaseID string) (int, error) { return n, nil })
	r, _ := s.Create("p", CreateRequest{Name: "MVP"})
	if err := s.Delete(r.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete targeted: %v", err)
	}
	n = 0
	if err := s.Delete(r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
}

func TestReorderNeedsEveryReleaseOnce(t *testing.T) {
	s := NewDefaultService(newMemRepo(), nil)
	a, _ := s.Create("p", CreateRequest{Name: "A"})
	b, _ := s.Create("p", CreateRequest{Name: "B"})
	if _, err := s.Reorder("p", []string{a.ID}); !errors.Is(err, ErrReorder) {
		t.Fatalf("partial: %v", err)
	}
	if _, err := s.Reorder("p", []string{a.ID, a.ID}); !errors.Is(err, ErrReorder) {
		t.Fatalf("duplicate: %v", err)
	}
	list, err := s.Reorder("p", []string{b.ID, a.ID})
	if err != nil || list[0].ID != b.ID {
		t.Fatalf("reorder: %v %v", list, err)
	}
}

func rel(id string, pos int) *Release { return &Release{ID: id, Name: id, Position: pos} }

func TestValidateTargets(t *testing.T) {
	o := NewOrder([]*Release{rel("v1", 1), rel("mvp", 0), rel("v2", 2)})
	cases := []struct {
		attrs map[string]interface{}
		want  error
	}{
		{nil, nil},
		{map[string]interface{}{AttrFrom: "mvp"}, nil},
		{map[string]interface{}{AttrFrom: "mvp", AttrUntil: "v1"}, nil},
		{map[string]interface{}{AttrFrom: "v1", AttrUntil: "v1"}, nil},
		{map[string]interface{}{AttrFrom: "v2", AttrUntil: "mvp"}, ErrRangeOrder},
		{map[string]interface{}{AttrUntil: "v1"}, ErrUntilWithout},
		{map[string]interface{}{AttrFrom: "other"}, ErrUnknownTarget},
		{map[string]interface{}{AttrFrom: "mvp", AttrUntil: "other"}, ErrUnknownTarget},
	}
	for i, c := range cases {
		if err := ValidateTargets(c.attrs, o); !errors.Is(err, c.want) {
			t.Errorf("case %d: got %v, want %v", i, err, c.want)
		}
	}
}

func ptr(s string) *string { return &s }

func art(id, typ string, parent *string, attrs map[string]interface{}) *artifacts.Artifact {
	return &artifacts.Artifact{ID: id, Ref: id, Type: typ, ParentID: parent, Title: id, Status: artifacts.StatusDraft, Attributes: attrs}
}

func TestResolveInheritsAndOverrides(t *testing.T) {
	releases := []*Release{rel("mvp", 0), rel("v1", 1), rel("v2", 2)}
	o := NewOrder(releases)
	arts := []*artifacts.Artifact{
		art("h", "heading", nil, map[string]interface{}{AttrFrom: "mvp"}),
		art("r1", "requirement", ptr("h"), nil),
		art("r2", "requirement", ptr("h"), map[string]interface{}{AttrFrom: "v2"}),
		art("r3", "requirement", ptr("r1"), nil),
		art("r4", "requirement", nil, map[string]interface{}{AttrFrom: "gone"}),
		art("r5", "requirement", ptr("h"), map[string]interface{}{AttrFrom: "mvp", AttrUntil: "mvp"}),
	}
	got := Resolve(arts, o)
	if got["r1"].From != "mvp" || got["r1"].InheritedFrom != "h" {
		t.Errorf("r1 inherits: %+v", got["r1"])
	}
	if got["r2"].From != "v2" || got["r2"].InheritedFrom != "" {
		t.Errorf("r2 overrides: %+v", got["r2"])
	}
	if got["r3"].From != "mvp" || got["r3"].InheritedFrom != "h" {
		t.Errorf("r3 inherits through its parent: %+v", got["r3"])
	}
	if _, ok := got["r4"]; ok {
		t.Errorf("r4 names no release of the project: %+v", got["r4"])
	}
	if !got["r1"].Contains(o, 2) || !got["r5"].Contains(o, 0) || got["r5"].Contains(o, 1) || got["r2"].Contains(o, 1) {
		t.Errorf("Contains is wrong")
	}
}

func TestOverviewAndProgress(t *testing.T) {
	releases := []*Release{rel("mvp", 0), rel("v1", 1)}
	approved := art("a", "requirement", nil, map[string]interface{}{AttrFrom: "mvp"})
	approved.Status = artifacts.StatusApproved
	arts := []*artifacts.Artifact{
		approved,
		art("b", "requirement", nil, map[string]interface{}{AttrFrom: "v1"}),
		art("c", "requirement", nil, nil),
		art("t", "test-case", nil, map[string]interface{}{AttrFrom: "mvp"}),
	}
	cov := &vv.CoverageReport{Entries: []vv.CoverageEntry{
		{RequirementID: "a", Rollup: vv.RollupPass, TestCaseIDs: []string{"t"}, LatestResults: map[string]string{"t": "pass"}},
		{RequirementID: "b", Rollup: vv.RollupUncovered},
		{RequirementID: "c", Rollup: vv.RollupUncovered},
	}}
	in := Inputs{Artifacts: arts, Releases: releases, Coverage: cov}
	ov := Overview(in)
	if ov.Unassigned != 1 || ov.UnassignedIDs[0] != "c" {
		t.Fatalf("unassigned: %+v", ov)
	}
	mvp, v1 := ov.Releases[0].Summary, ov.Releases[1].Summary
	if mvp.Total != 1 || mvp.Ready != 1 || mvp.Introduced != 1 {
		t.Errorf("MVP: %+v", mvp)
	}
	if v1.Total != 2 || v1.Introduced != 1 || v1.Verified != 1 || v1.Approved != 1 || v1.ByRollup[vv.RollupUncovered] != 1 {
		t.Errorf("V1.0 carries MVP's requirement: %+v", v1)
	}
	p := ForRelease(in, releases[1], nil, time.Now())
	if len(p.Entries) != 2 || p.BurnUp != nil {
		t.Errorf("progress: %+v", p)
	}
}

func TestBurnUpFollowsTheResultHistory(t *testing.T) {
	d := func(n int) time.Time { return time.Date(2026, 10, n, 12, 0, 0, 0, time.UTC) }
	a := art("a", "requirement", nil, nil)
	a.CreatedAt = d(1)
	b := art("b", "requirement", nil, nil)
	b.CreatedAt = d(3)
	m := art("m", "requirement", nil, nil)
	m.CreatedAt = d(1)
	m.ValidFrom = d(4)
	es := []Entry{
		{RequirementID: "a", Rollup: vv.RollupPass, TestCaseIDs: []string{"t1", "t2"}},
		{RequirementID: "b", Rollup: vv.RollupFail, TestCaseIDs: []string{"t3"}},
		{RequirementID: "m", Rollup: vv.RollupVerifiedManually},
	}
	at := func(n int) *time.Time { t := d(n); return &t }
	history := []*vv.TestResult{
		{TestCaseID: "t1", Status: vv.ResultPass, ExecutedAt: at(2)},
		{TestCaseID: "t2", Status: vv.ResultFail, ExecutedAt: at(2)},
		{TestCaseID: "t2", Status: vv.ResultPass, ExecutedAt: at(5)},
		{TestCaseID: "t3", Status: vv.ResultPass, ExecutedAt: at(3)},
		{TestCaseID: "t3", Status: vv.ResultFail, ExecutedAt: at(4)},
	}
	pts := BurnUp(es, []*artifacts.Artifact{a, b, m}, history, d(2), d(6))
	want := []Point{
		{"2026-10-01", 2, 0},
		{"2026-10-02", 2, 0},
		{"2026-10-03", 3, 0},
		{"2026-10-04", 3, 1},
		{"2026-10-05", 3, 2},
		{"2026-10-06", 3, 2},
	}
	if len(pts) != len(want) {
		t.Fatalf("points: %+v", pts)
	}
	for i := range want {
		if pts[i] != want[i] {
			t.Errorf("day %d: got %+v, want %+v", i, pts[i], want[i])
		}
	}
}

func TestBurnUpSamplesALongSpan(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 400)
	pts := BurnUp(nil, nil, nil, start, end)
	if len(pts) > maxPoints+1 || pts[len(pts)-1].Date != end.Format("2006-01-02") {
		t.Fatalf("%d points, last %s", len(pts), pts[len(pts)-1].Date)
	}
}
