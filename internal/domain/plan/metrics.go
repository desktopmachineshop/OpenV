package plan

import (
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/targetreleases"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// The metrics a milestone's criterion can name. Each is a share of some of
// the project's artifacts (or of the plan's tasks) that have reached a
// state, as a whole percentage.
const (
	MetricNeedsAddressed       = "needs_addressed"
	MetricRequirementsApproved = "requirements_approved"
	MetricRequirementsDesigned = "requirements_designed"
	MetricRequirementsTested   = "requirements_tested"
	MetricRequirementsVerified = "requirements_verified"
	MetricTestsPassing         = "tests_passing"
	MetricHazardsMitigated     = "hazards_mitigated"
	MetricTasksDone            = "tasks_done"
	MetricMilestoneTasks       = "milestone_tasks"
)

// Metric describes one metric to a chooser.
type Metric struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Description says what is counted, in one sentence.
	Description string `json:"description"`
}

// Metrics lists the metrics in the order a project matures: definition,
// design, verification, delivery.
var Metrics = []Metric{
	{MetricNeedsAddressed, "User needs addressed", "User needs that at least one requirement derives from."},
	{MetricRequirementsApproved, "Requirements approved", "Requirements whose review status is approved."},
	{MetricRequirementsDesigned, "Requirements covered by design", "Requirements that at least one design item satisfies."},
	{MetricRequirementsTested, "Requirements with a test case", "Requirements that at least one test case verifies."},
	{MetricRequirementsVerified, "Requirements verified", "Requirements whose verification passes, by test or by a manual method."},
	{MetricTestsPassing, "Test cases passing", "Test cases whose latest result is a pass."},
	{MetricHazardsMitigated, "Hazards mitigated", "Hazards that at least one design item mitigates."},
	{MetricTasksDone, "Plan tasks done", "Tasks due by the milestone's date that are in the Done column; every task when the milestone has no date."},
	{MetricMilestoneTasks, "Milestone tasks done", "Tasks assigned to the milestone that are in the Done column. A task with sub-tasks stands for them: each of its sub-tasks counts, not the task itself."},
}

// ValidMetric reports whether key names a metric.
func ValidMetric(key string) bool {
	for _, m := range Metrics {
		if m.Key == key {
			return true
		}
	}
	return false
}

// MetricLabel is a metric's name, or the key itself for an unknown one.
func MetricLabel(key string) string {
	for _, m := range Metrics {
		if m.Key == key {
			return m.Label
		}
	}
	return key
}

// DefaultReleaseCriteria are a release milestone's criteria until someone
// sets its own: what the Releases page calls ready.
func DefaultReleaseCriteria() []Criterion {
	return []Criterion{{MetricRequirementsApproved, 100}, {MetricRequirementsVerified, 100}}
}

// Evidence is what the metrics read from the project: its current
// artifacts and links, its coverage (with the flow-down applied) and each
// test case's latest result status.
type Evidence struct {
	Artifacts []*artifacts.Artifact
	Links     []*links.Link
	Coverage  *vv.CoverageReport
	Latest    map[string]string
}

// MetricValue is one metric measured: Count of Total, as Percent.
type MetricValue struct {
	Metric  string `json:"metric"`
	Label   string `json:"label"`
	Count   int    `json:"count"`
	Total   int    `json:"total"`
	Percent int    `json:"percent"`
}

func value(metric string, count, total int) MetricValue {
	v := MetricValue{Metric: metric, Label: MetricLabel(metric), Count: count, Total: total}
	if total > 0 {
		v.Percent = count * 100 / total
	}
	return v
}

// evidenceIndex is the evidence read once, for every milestone to measure.
type evidenceIndex struct {
	ev Evidence
	// linked[type] holds the ids of artifacts a link of that type points at.
	linked   map[string]map[string]bool
	coverage map[string]vv.CoverageEntry
}

func indexEvidence(ev Evidence) *evidenceIndex {
	ix := &evidenceIndex{ev: ev, linked: map[string]map[string]bool{}, coverage: map[string]vv.CoverageEntry{}}
	for _, l := range ev.Links {
		if l == nil || l.ValidTo != nil {
			continue
		}
		if ix.linked[l.Type] == nil {
			ix.linked[l.Type] = map[string]bool{}
		}
		ix.linked[l.Type][l.ToID] = true
	}
	if ev.Coverage != nil {
		for _, e := range ev.Coverage.Entries {
			ix.coverage[e.RequirementID] = e
		}
	}
	return ix
}

// measure computes every artifact metric. scope, when not nil, narrows the
// requirement metrics to the requirements it holds (a release's).
func (ix *evidenceIndex) measure(scope map[string]bool) map[string]MetricValue {
	var needs, needsAddressed, reqs, approved, designed, tested, verified, tests, passing, hazards, mitigated int
	for _, a := range ix.ev.Artifacts {
		if a == nil {
			continue
		}
		switch a.Type {
		case artifacts.TypeUserNeed:
			needs++
			if ix.linked["derives-from"][a.ID] {
				needsAddressed++
			}
		case artifacts.TypeRequirement:
			if scope != nil && !scope[a.ID] {
				continue
			}
			reqs++
			if artifacts.NormalizeStatus(a.Status) == artifacts.StatusApproved {
				approved++
			}
			if ix.linked["satisfies"][a.ID] {
				designed++
			}
			c := ix.coverage[a.ID]
			if len(c.TestCaseIDs) > 0 {
				tested++
			}
			if targetreleases.Verified(c.Rollup) {
				verified++
			}
		case artifacts.TypeTestCase:
			tests++
			if ix.ev.Latest[a.ID] == vv.ResultPass {
				passing++
			}
		case artifacts.TypeHazard:
			hazards++
			if ix.linked["mitigates"][a.ID] {
				mitigated++
			}
		}
	}
	return map[string]MetricValue{
		MetricNeedsAddressed:       value(MetricNeedsAddressed, needsAddressed, needs),
		MetricRequirementsApproved: value(MetricRequirementsApproved, approved, reqs),
		MetricRequirementsDesigned: value(MetricRequirementsDesigned, designed, reqs),
		MetricRequirementsTested:   value(MetricRequirementsTested, tested, reqs),
		MetricRequirementsVerified: value(MetricRequirementsVerified, verified, reqs),
		MetricTestsPassing:         value(MetricTestsPassing, passing, tests),
		MetricHazardsMitigated:     value(MetricHazardsMitigated, mitigated, hazards),
	}
}

// tasksDone measures MetricTasksDone: the tasks due on or before date (every
// task when date is nil) that are done.
func tasksDone(tasks []*Task, date *string) MetricValue {
	var done, total int
	for _, t := range tasks {
		if date != nil && (t.SpanEnd == nil || *t.SpanEnd > *date) {
			continue
		}
		total++
		if t.Done {
			done++
		}
	}
	return value(MetricTasksDone, done, total)
}

// milestoneTasks measures MetricMilestoneTasks: of the tasks a milestone
// counts, those that are done.
func milestoneTasks(tasks []*Task) MetricValue {
	done := 0
	for _, t := range tasks {
		if t.Done {
			done++
		}
	}
	return value(MetricMilestoneTasks, done, len(tasks))
}

// assignMilestones gives each task the milestone it counts toward: the one
// it is assigned to, or else the one of the nearest task above it that has
// one, so assigning a task assigns its sub-tasks. It answers, per milestone
// key, the tasks that count: those with no sub-tasks. tasks is in outline
// order, a parent before its sub-tasks.
func assignMilestones(tasks []*Task, own map[string]string) map[string][]*Task {
	byID := map[string]*Task{}
	counted := map[string][]*Task{}
	for _, t := range tasks {
		byID[t.ID] = t
		if key, ok := own[t.ID]; ok {
			k := key
			t.Milestone = &k
		} else if t.ParentID != nil && byID[*t.ParentID] != nil && byID[*t.ParentID].Milestone != nil {
			t.Milestone, t.MilestoneInherited = byID[*t.ParentID].Milestone, true
		}
		if t.Milestone != nil && t.SubtasksTotal == 0 {
			counted[*t.Milestone] = append(counted[*t.Milestone], t)
		}
	}
	return counted
}
