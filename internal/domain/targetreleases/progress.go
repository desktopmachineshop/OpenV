package targetreleases

import (
	"sort"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// Range is an artifact's effective targeting: the release it is introduced
// in, the last release it is in ("" for every later one), and the artifact
// the range was set on when it is inherited from an ancestor.
type Range struct {
	From  string `json:"from"`
	Until string `json:"until,omitempty"`
	// InheritedFrom is the id of the ancestor whose range this is, or ""
	// when the artifact sets its own.
	InheritedFrom string `json:"inherited_from,omitempty"`
}

// Contains reports whether the range covers the release at index idx of o.
func (r Range) Contains(o Order, idx int) bool {
	fi, ok := o[r.From]
	if !ok || idx < fi {
		return false
	}
	if r.Until == "" {
		return true
	}
	ui, ok := o[r.Until]
	return !ok || idx <= ui
}

// Resolve gives each artifact its effective range: its own when its
// AttrFrom names a release of o, else its nearest such ancestor's. An
// artifact with no range anywhere up its tree is left out. A value naming
// no release of the project (a deleted one, or another project's carried in
// by an import) counts as not set, so the ancestor's range shows through.
func Resolve(arts []*artifacts.Artifact, o Order) map[string]Range {
	byID := make(map[string]*artifacts.Artifact, len(arts))
	for _, a := range arts {
		byID[a.ID] = a
	}
	own := func(a *artifacts.Artifact) (Range, bool) {
		from := attr(a.Attributes, AttrFrom)
		if _, ok := o[from]; !ok {
			return Range{}, false
		}
		until := attr(a.Attributes, AttrUntil)
		if _, ok := o[until]; !ok {
			until = ""
		}
		return Range{From: from, Until: until}, true
	}
	out := map[string]Range{}
	for _, a := range arts {
		if r, ok := own(a); ok {
			out[a.ID] = r
			continue
		}
		seen := map[string]bool{a.ID: true}
		for p := a.ParentID; p != nil && !seen[*p]; {
			seen[*p] = true
			parent := byID[*p]
			if parent == nil {
				break
			}
			if r, ok := own(parent); ok {
				r.InheritedFrom = parent.ID
				out[a.ID] = r
				break
			}
			p = parent.ParentID
		}
	}
	return out
}

// Entry is one requirement in a release's progress.
type Entry struct {
	RequirementID string            `json:"requirement_id"`
	Ref           string            `json:"ref"`
	Title         string            `json:"title"`
	Status        string            `json:"status"`
	Rollup        string            `json:"rollup"`
	Range         Range             `json:"range"`
	Introduced    bool              `json:"introduced"`
	TestCaseIDs   []string          `json:"test_case_ids"`
	LatestResults map[string]string `json:"latest_results"`
}

// Summary counts a release's requirements.
type Summary struct {
	// Total is the requirements in the release; Introduced those whose
	// range starts at it, the rest carried over from earlier releases.
	Total      int `json:"total"`
	Introduced int `json:"introduced"`
	// Approved counts review status approved; Verified a rollup of pass or
	// verified-manually; Ready both.
	Approved int `json:"approved"`
	Verified int `json:"verified"`
	Ready    int `json:"ready"`
	// ByStatus counts review statuses; ByRollup V&V rollups (vv.Rollup*).
	ByStatus map[string]int `json:"by_status"`
	ByRollup map[string]int `json:"by_rollup"`
}

// Point is one day of a burn-up: the requirements in the release that
// existed by the end of the day (Scope) and those verified by then.
type Point struct {
	Date     string `json:"date"`
	Scope    int    `json:"scope"`
	Verified int    `json:"verified"`
}

// Progress is one release's progress against its requirements.
type Progress struct {
	Release *Release `json:"release"`
	Summary Summary  `json:"summary"`
	Entries []Entry  `json:"entries"`
	BurnUp  []Point  `json:"burn_up,omitempty"`
}

// ProjectOverview is every release's summary, plus the requirements no
// release targets.
type ProjectOverview struct {
	Releases   []ReleaseSummary `json:"releases"`
	Unassigned int              `json:"unassigned"`
	// UnassignedIDs are the requirements no release targets.
	UnassignedIDs []string `json:"unassigned_ids"`
}

// ReleaseSummary pairs a release with its summary.
type ReleaseSummary struct {
	Release *Release `json:"release"`
	Summary Summary  `json:"summary"`
}

// Verified reports whether a rollup counts as verified.
func Verified(rollup string) bool {
	return rollup == vv.RollupPass || rollup == vv.RollupVerifiedManually
}

func newSummary() Summary {
	return Summary{ByStatus: map[string]int{}, ByRollup: map[string]int{}}
}

func (s *Summary) add(e Entry) {
	s.Total++
	if e.Introduced {
		s.Introduced++
	}
	s.ByStatus[e.Status]++
	s.ByRollup[e.Rollup]++
	approved := e.Status == artifacts.StatusApproved
	verified := Verified(e.Rollup)
	if approved {
		s.Approved++
	}
	if verified {
		s.Verified++
	}
	if approved && verified {
		s.Ready++
	}
}

// Inputs is what progress is computed from: the project's current
// artifacts, its releases and its coverage report (with the flow-down from
// child projects applied).
type Inputs struct {
	Artifacts []*artifacts.Artifact
	Releases  []*Release
	Coverage  *vv.CoverageReport
}

// entries returns the requirements in the release at index idx.
func entries(in Inputs, o Order, ranges map[string]Range, idx int) []Entry {
	cov := map[string]vv.CoverageEntry{}
	if in.Coverage != nil {
		for _, e := range in.Coverage.Entries {
			cov[e.RequirementID] = e
		}
	}
	out := []Entry{}
	for _, a := range in.Artifacts {
		if a.Type != "requirement" {
			continue
		}
		r, ok := ranges[a.ID]
		if !ok || !r.Contains(o, idx) {
			continue
		}
		c := cov[a.ID]
		rollup := c.Rollup
		if rollup == "" {
			rollup = vv.RollupMethodMissing
		}
		tcs := c.TestCaseIDs
		if tcs == nil {
			tcs = []string{}
		}
		latest := c.LatestResults
		if latest == nil {
			latest = map[string]string{}
		}
		out = append(out, Entry{
			RequirementID: a.ID,
			Ref:           a.Ref,
			Title:         a.Title,
			Status:        artifacts.NormalizeStatus(a.Status),
			Rollup:        rollup,
			Range:         r,
			Introduced:    o[r.From] == idx,
			TestCaseIDs:   tcs,
			LatestResults: latest,
		})
	}
	return out
}

// Overview summarises every release of the project.
func Overview(in Inputs) *ProjectOverview {
	o := NewOrder(in.Releases)
	ranges := Resolve(in.Artifacts, o)
	sorted := append([]*Release(nil), in.Releases...)
	sort.SliceStable(sorted, func(i, j int) bool { return o[sorted[i].ID] < o[sorted[j].ID] })
	ov := &ProjectOverview{Releases: []ReleaseSummary{}, UnassignedIDs: []string{}}
	for _, rel := range sorted {
		s := newSummary()
		for _, e := range entries(in, o, ranges, o[rel.ID]) {
			s.add(e)
		}
		ov.Releases = append(ov.Releases, ReleaseSummary{Release: rel, Summary: s})
	}
	for _, a := range in.Artifacts {
		if a.Type != "requirement" {
			continue
		}
		if _, ok := ranges[a.ID]; !ok {
			ov.UnassignedIDs = append(ov.UnassignedIDs, a.ID)
		}
	}
	ov.Unassigned = len(ov.UnassignedIDs)
	return ov
}

// ForRelease computes one release's progress. history, when not nil, is
// every test result the project has recorded, and adds the burn-up up to
// now (or to the day the release was released).
func ForRelease(in Inputs, release *Release, history []*vv.TestResult, now time.Time) *Progress {
	o := NewOrder(in.Releases)
	ranges := Resolve(in.Artifacts, o)
	p := &Progress{Release: release, Summary: newSummary(), Entries: entries(in, o, ranges, o[release.ID])}
	for _, e := range p.Entries {
		p.Summary.add(e)
	}
	if history != nil {
		end := now
		if release.Status == StatusReleased && release.ReleasedAt != nil {
			end = *release.ReleasedAt
		}
		p.BurnUp = BurnUp(p.Entries, in.Artifacts, history, release.CreatedAt, end)
	}
	return p
}

// maxPoints caps a burn-up; a longer span is sampled every few days, its
// last point always the end day.
const maxPoints = 120

func day(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func resultTime(r *vv.TestResult) time.Time {
	if r.ExecutedAt != nil {
		return *r.ExecutedAt
	}
	return r.CreatedAt
}

// BurnUp counts, for each day from start to end, the release's requirements
// that existed by then and those verified by then.
//
// A requirement verified by test counts from the first day on which every
// test case verifying it had passed as its latest result, read from the
// result history the way coverage reads the latest result. A requirement
// whose current evidence is not a test result (a manual attestation, or
// child-project refinements) has no dated history: it counts from the day
// its current version was written, when the attestation was last true,
// which is never earlier than the attestation itself. A requirement that is
// not verified now is not counted on any day, so the last point always
// agrees with the summary.
func BurnUp(es []Entry, arts []*artifacts.Artifact, history []*vv.TestResult, start, end time.Time) []Point {
	byID := map[string]*artifacts.Artifact{}
	for _, a := range arts {
		byID[a.ID] = a
	}
	// Each test case's results, oldest first.
	byCase := map[string][]*vv.TestResult{}
	first := time.Time{}
	for _, r := range history {
		byCase[r.TestCaseID] = append(byCase[r.TestCaseID], r)
	}
	for _, rs := range byCase {
		sort.SliceStable(rs, func(i, j int) bool { return resultTime(rs[i]).Before(resultTime(rs[j])) })
	}
	type req struct {
		created  time.Time
		verified time.Time // zero: not verified now
	}
	reqs := []req{}
	for _, e := range es {
		a := byID[e.RequirementID]
		rq := req{}
		if a != nil {
			rq.created = a.CreatedAt
		}
		if Verified(e.Rollup) {
			rq.verified = verifiedSince(e, byCase)
			if rq.verified.IsZero() && a != nil {
				rq.verified = a.ValidFrom
			}
			if rq.verified.IsZero() {
				rq.verified = end
			}
		}
		if first.IsZero() || (!rq.created.IsZero() && rq.created.Before(first)) {
			first = rq.created
		}
		reqs = append(reqs, rq)
	}
	from := day(start)
	if !first.IsZero() && day(first).Before(from) {
		from = day(first)
	}
	to := day(end)
	if to.Before(from) {
		to = from
	}
	days := int(to.Sub(from).Hours()/24) + 1
	step := 1
	if days > maxPoints {
		step = (days + maxPoints - 1) / maxPoints
	}
	points := []Point{}
	for d := 0; ; d += step {
		if d >= days {
			d = days - 1
		}
		cur := from.AddDate(0, 0, d)
		cutoff := cur.AddDate(0, 0, 1)
		pt := Point{Date: cur.Format("2006-01-02")}
		for _, rq := range reqs {
			if rq.created.Before(cutoff) {
				pt.Scope++
			}
			if !rq.verified.IsZero() && rq.verified.Before(cutoff) {
				pt.Verified++
			}
		}
		points = append(points, pt)
		if d == days-1 {
			break
		}
	}
	return points
}

// verifiedSince is when a test-verified requirement last became verified:
// the time after which every one of its test cases had pass as its latest
// result, without a later non-pass. Zero when the requirement's evidence is
// not its own test cases.
func verifiedSince(e Entry, byCase map[string][]*vv.TestResult) time.Time {
	if e.Rollup != vv.RollupPass || len(e.TestCaseIDs) == 0 {
		return time.Time{}
	}
	var since time.Time
	for _, tc := range e.TestCaseIDs {
		rs := byCase[tc]
		// The start of the final unbroken run of passes.
		var passSince time.Time
		for _, r := range rs {
			if r.Status == vv.ResultPass {
				if passSince.IsZero() {
					passSince = resultTime(r)
				}
			} else {
				passSince = time.Time{}
			}
		}
		if passSince.IsZero() {
			// The coverage says pass but the history shows none: evidence
			// from outside the history (a flow-down); treat as undated.
			return time.Time{}
		}
		if passSince.After(since) {
			since = passSince
		}
	}
	return since
}
