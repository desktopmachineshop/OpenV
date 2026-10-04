package postgres

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// TestListTheRunsWithNoProject pins ListFilter.NoProject, the workspace
// Runs page's listing (#379 bug 168): the runs of the workspace with no
// project, newest first, whoever launched them or none did, and those a
// project's delete left behind (its foreign key sets project_id NULL);
// never a project's run, nor another workspace's run with no project.
// LaunchedBy narrows it further, to a member's own.
func TestListTheRunsWithNoProject(t *testing.T) {
	f := newClaimFixture(t)
	doomed := f.project(t, "Doomed")
	kept := f.project(t, "Kept")

	byA := f.queueRun(t, runSpec{launchedBy: &f.userA, age: 5 * time.Minute})
	byB := f.queueRun(t, runSpec{launchedBy: &f.userB, age: 4 * time.Minute})
	byNobody := f.queueRun(t, runSpec{age: 3 * time.Minute})
	inKept := f.queueRun(t, runSpec{projectID: &kept, launchedBy: &f.userA, age: 2 * time.Minute})
	leftBehind := f.queueRun(t, runSpec{projectID: &doomed, launchedBy: &f.userA, age: time.Minute})
	f.exec(t, `DELETE FROM projects WHERE id = $1`, doomed)

	// Another workspace's run with no project, launched by the same member.
	otherOrg, otherAgent := uuid.New().String(), uuid.New().String()
	f.exec(t, `INSERT INTO organizations (id, name, slug) VALUES ($1, 'Other Org', 'other-org')`, otherOrg)
	f.exec(t, `INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'worker', 'Worker', 'claude')`, otherAgent, otherOrg)
	elsewhere := uuid.New().String()
	if err := f.repo.Save(&agentruns.Run{
		ID: elsewhere, OrgID: otherOrg, AgentID: otherAgent, Status: agentruns.StatusQueued, Prompt: "elsewhere",
		ArtifactsTouched: []map[string]interface{}{}, LaunchedBy: &f.userA, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save the other workspace's run: %v", err)
	}

	ids := func(t *testing.T, filter agentruns.ListFilter) []string {
		t.Helper()
		list, err := f.repo.List(filter)
		if err != nil {
			t.Fatalf("List(%+v): %v", filter, err)
		}
		out := []string{}
		for _, run := range list {
			out = append(out, run.ID)
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		filter agentruns.ListFilter
		want   []string
	}{
		{"the workspace's, newest first", agentruns.ListFilter{OrgID: f.orgID, NoProject: true},
			[]string{leftBehind, byNobody, byB, byA}},
		{"a member's own", agentruns.ListFilter{OrgID: f.orgID, NoProject: true, LaunchedBy: f.userA},
			[]string{leftBehind, byA}},
		{"the other workspace's", agentruns.ListFilter{OrgID: otherOrg, NoProject: true}, []string{elsewhere}},
		{"without NoProject, every run as before", agentruns.ListFilter{OrgID: f.orgID},
			[]string{leftBehind, inKept, byNobody, byB, byA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, tc.filter); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("List(%+v) = %v, want %v", tc.filter, got, tc.want)
			}
		})
	}
}
