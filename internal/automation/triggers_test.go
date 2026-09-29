package automation

import (
	"sort"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
)

// fakeAutomationRepo answers the matcher's query with every enabled
// triggered automation on the event type, whatever its workspace, as the
// repository does, and records the runs it stamps.
type fakeAutomationRepo struct {
	automations.Repository
	list   []*automations.Automation
	marked []string
}

func (f *fakeAutomationRepo) ListEnabledTriggered(eventType string) ([]*automations.Automation, error) {
	var out []*automations.Automation
	for _, a := range f.list {
		if a.Enabled && a.Kind == automations.KindTriggered && a.EventType == eventType {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeAutomationRepo) MarkRun(id string, lastRun time.Time, nextRun *time.Time) error {
	f.marked = append(f.marked, id)
	return nil
}

// fakeRunService records the launches.
type fakeRunService struct {
	agentruns.Service
	launched []agentruns.LaunchRequest
}

func (f *fakeRunService) Launch(req agentruns.LaunchRequest) (*agentruns.Run, string, error) {
	f.launched = append(f.launched, req)
	return &agentruns.Run{ID: "run", OrgID: req.OrgID, ProjectID: req.ProjectID}, "token", nil
}

func (f *fakeRunService) CountRunsSince(automationID string, since time.Time) (int, error) {
	return 0, nil
}

// triggered is an enabled automation of workspace org on artifact.created,
// for the whole workspace or, with a project, pinned to it.
func triggered(id, org, project string) *automations.Automation {
	agent := "agent-" + org
	a := &automations.Automation{ID: id, OrgID: org, AgentID: &agent, Kind: automations.KindTriggered,
		Enabled: true, EventType: domainevents.ArtifactCreated, EventFilter: map[string]interface{}{}}
	if project != "" {
		a.ProjectID = &project
	}
	return a
}

func derefOr(s *string, none string) string {
	if s == nil {
		return none
	}
	return *s
}

// TestTriggerMatcherKeepsWorkspacesApart is the regression test for a
// triggered automation that fired on every workspace's events: the matcher
// compared an event's project with the automation's only for an automation
// pinned to one, and never its workspace, so workspace A's automation for
// all of A fired on an artifact created in workspace B, and its run took
// B's project. Each workspace's automations now see only that workspace's
// events, and a workspace-wide run is scoped to the event's project in its
// own workspace (OpenV REQ-17).
func TestTriggerMatcherKeepsWorkspacesApart(t *testing.T) {
	repo := &fakeAutomationRepo{list: []*automations.Automation{
		triggered("a-all", "org-a", ""),
		triggered("a-pinned", "org-a", "proj-a"),
		triggered("b-all", "org-b", ""),
		triggered("b-pinned", "org-b", "proj-b"),
	}}

	cases := []struct {
		name  string
		event domainevents.Event
		fired []string // automation ids, sorted
		// projects is the project each fired automation's run is scoped to.
		projects map[string]string
	}{
		{
			name:     "an event in A's project fires only A's automations",
			event:    domainevents.New(domainevents.ArtifactCreated, "proj-a", "art-1", "user:u1", nil).WithOrg("org-a"),
			fired:    []string{"a-all", "a-pinned"},
			projects: map[string]string{"a-all": "proj-a", "a-pinned": "proj-a"},
		},
		{
			name:     "an event in B's project fires only B's automations",
			event:    domainevents.New(domainevents.ArtifactCreated, "proj-b", "art-2", "user:u2", nil).WithOrg("org-b"),
			fired:    []string{"b-all", "b-pinned"},
			projects: map[string]string{"b-all": "proj-b", "b-pinned": "proj-b"},
		},
		{
			name:     "an event in another project of B fires only B's automation for all of B",
			event:    domainevents.New(domainevents.ArtifactCreated, "proj-b2", "art-3", "user:u2", nil).WithOrg("org-b"),
			fired:    []string{"b-all"},
			projects: map[string]string{"b-all": "proj-b2"},
		},
		{
			name:  "an event of a third workspace fires none",
			event: domainevents.New(domainevents.ArtifactCreated, "proj-c", "art-4", "user:u3", nil).WithOrg("org-c"),
		},
		{
			name:  "an event with no workspace fires none",
			event: domainevents.New(domainevents.ArtifactCreated, "proj-a", "art-5", "user:u1", nil),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runs := &fakeRunService{}
			m := NewTriggerMatcher(repo, runs, nil)
			repo.marked = nil
			m.handle(tc.event)

			var fired []string
			for _, req := range runs.launched {
				id := *req.AutomationID
				fired = append(fired, id)
				wantOrg := map[string]string{"a": "org-a", "b": "org-b"}[id[:1]]
				if req.OrgID != wantOrg {
					t.Errorf("%s launched a run in workspace %q, want %q", id, req.OrgID, wantOrg)
				}
				if got := derefOr(req.ProjectID, "no project"); got != tc.projects[id] {
					t.Errorf("%s launched a run in %s, want %q", id, got, tc.projects[id])
				}
			}
			sort.Strings(fired)
			if len(fired) != len(tc.fired) {
				t.Fatalf("fired %v, want %v", fired, tc.fired)
			}
			for i := range fired {
				if fired[i] != tc.fired[i] {
					t.Fatalf("fired %v, want %v", fired, tc.fired)
				}
			}
			if len(repo.marked) != len(tc.fired) {
				t.Fatalf("stamped last_run_at on %v, want the %d that fired", repo.marked, len(tc.fired))
			}
		})
	}
}
