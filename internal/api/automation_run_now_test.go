package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// The run-now copy, refactor plan step S11 (services-4, and services-5's
// third copy of the launch logic; OpenV REQ-24, an automation launched
// manually). POST /api/v1/automations/{id}/run-now (RunAutomationNow)
// builds its run as the scheduler's fire does (internal/scheduler) and the
// trigger matcher's (internal/automation), each its own copy: this test
// pins this copy's request beside those packages' S11 tests. The S5d tour
// pins what the route answers through the real server; this test pins the
// whole launch request the run service is handed, which the tour sees only
// in part.

// runNowAutomations serves the automations of the run-now fixture. It
// embeds automations.Service, which has no stamp: run-now marks no run on
// the automation.
type runNowAutomations struct {
	automations.Service
	byID map[string]*automations.Automation
}

func (f *runNowAutomations) Get(id string) (*automations.Automation, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("automation not found")
}

// runNowFixture is workspace org-1, whose admin is "admin", with project
// proj-1, which "editor" edits; and these automations,
// each pinned to proj-1 unless named otherwise:
//   - au-plain: agent-helper's, "Nightly", no template, manual;
//   - au-template, au-unknown, au-space: agent-helper's, with a template that
//     uses automation.name and an event variable, one of only unknown
//     placeholders, and one of unknown placeholders around a space;
//   - au-none (no target);
//   - au-disabled: a disabled scheduled automation;
//   - au-guarded: a triggered automation that ran a second ago, with a
//     cooldown of an hour and a cap of one run an hour;
//   - au-workspace: agent-helper's, for the whole workspace.
func runNowFixture() (*Handler, *fakeRunService) {
	project, helper := "proj-1", "agent-helper"
	justNow := time.Now().Add(-time.Second)
	next := time.Now().Add(time.Hour)
	automation := func(id, name string) *automations.Automation {
		agent, p := helper, project
		return &automations.Automation{ID: id, OrgID: "org-1", Name: name, AgentID: &agent, ProjectID: &p,
			Kind: automations.KindManual, Enabled: true, EventFilter: map[string]interface{}{},
			CooldownSeconds: 60, MaxRunsPerHour: 10}
	}
	byID := map[string]*automations.Automation{}
	add := func(a *automations.Automation, edit func(a *automations.Automation)) {
		if edit != nil {
			edit(a)
		}
		byID[a.ID] = a
	}
	add(automation("au-plain", "Nightly"), nil)
	add(automation("au-template", "Templated"), func(a *automations.Automation) {
		a.PromptTemplate = "Run {{automation.name}} on {{event.type}}"
	})
	add(automation("au-unknown", "Unknown"), func(a *automations.Automation) {
		a.PromptTemplate = "{{event.type}}{{project.id}}"
	})
	add(automation("au-space", "Spaced"), func(a *automations.Automation) {
		a.PromptTemplate = "{{event.type}} {{project.id}}"
	})
	add(automation("au-none", "Nobody"), func(a *automations.Automation) { a.AgentID = nil })
	add(automation("au-disabled", "Disabled"), func(a *automations.Automation) {
		a.Kind, a.Enabled, a.CronExpr, a.NextRunAt = automations.KindScheduled, false, "0 9 * * 1", &next
	})
	add(automation("au-guarded", "Guarded"), func(a *automations.Automation) {
		a.Kind, a.EventType, a.LastRunAt = automations.KindTriggered, "artifact.created", &justNow
		a.CooldownSeconds, a.MaxRunsPerHour = 3600, 1
	})
	add(automation("au-workspace", "Workspace"), func(a *automations.Automation) { a.ProjectID = nil })

	runs := &fakeRunService{}
	h := NewHandler(HandlerDeps{
		RunService:     runs,
		AgentService:   &fakeAgentService{},
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{project: {ID: project, OrgID: "org-1"}}},
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"admin": orgs.RoleAdmin, "editor": orgs.RoleMember},
		}},
		MemberService:     &fakeMemberService{roles: map[string]map[string]string{project: {"editor": members.RoleEditor}}},
		AutomationService: &runNowAutomations{byID: byID},
		SSEHub:            NewSSEHub(),
	})
	return h, runs
}

// runNow sends POST /api/v1/automations/{id}/run-now as userID, active in
// org-1; a handler that panics on a service it should never have reached
// answers 599 with the panic, so the test fails on it.
func runNow(h *Handler, id, userID string) (w *httptest.ResponseRecorder) {
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/automations/"+id+"/run-now", nil)
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	r = mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": id})
	defer func() {
		if p := recover(); p != nil {
			w = httptest.NewRecorder()
			w.Code = 599
			fmt.Fprintf(w.Body, "panicked: %v", p)
		}
	}()
	h.RunAutomationNow(w, r)
	return w
}

// runNowLaunchLine prints every field of a launch request, pointers
// dereferenced and nil as -, so a test compares the whole request and a
// field added to agentruns.LaunchRequest shows up in every expectation.
func runNowLaunchLine(req agentruns.LaunchRequest) string {
	v := reflect.ValueOf(req)
	var parts []string
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		s := "-"
		switch {
		case f.Kind() == reflect.Pointer && f.IsNil():
		case f.Kind() == reflect.Pointer:
			s = fmt.Sprintf("%q", fmt.Sprint(f.Elem().Interface()))
		case f.Kind() == reflect.String:
			s = fmt.Sprintf("%q", f.String())
		default:
			s = fmt.Sprint(f.Interface())
		}
		parts = append(parts, v.Type().Field(i).Name+"="+s)
	}
	return strings.Join(parts, " ")
}

// TestRunNowCopy pins the run run-now launches: the automation's workspace
// and project, its agent (ResolveTarget), the automation, the caller as launcher, no
// trigger event and no parent (a person's request); and its prompt, the
// template rendered with automation.name as its one variable, or "Manual
// run of automation: <name>" when that renders to the empty string or only
// whitespace, as the scheduler's and the matcher's copies do (the regression
// test for bug 77 of issue #379: run-now and the scheduler kept a render of
// only whitespace as the prompt). Run-now checks no guard: a disabled automation, one
// in its cooldown and one at its hourly cap each run all the same, and none
// is stamped (the run service is asked no count, and the automation service
// has no stamp). It answers 201 with the run. A target it cannot resolve
// answers 400 with ResolveTarget's error and launches nothing; a launch the
// run service refuses answers 400 with its error.
func TestRunNowCopy(t *testing.T) {
	str := func(s string) *string { return &s }
	run := func(id, agent, prompt string, project *string, launcher string) string {
		return runNowLaunchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: agent, ProjectID: project,
			AutomationID: str(id), Prompt: prompt, LaunchedBy: str(launcher)})
	}
	p := str("proj-1")
	cases := []struct {
		id, user string
		launch   string
	}{
		{"au-plain", "editor", run("au-plain", "agent-helper", "Manual run of automation: Nightly", p, "editor")},
		{"au-template", "editor", run("au-template", "agent-helper", "Run Templated on ", p, "editor")},
		{"au-unknown", "editor", run("au-unknown", "agent-helper", "Manual run of automation: Unknown", p, "editor")},
		{"au-space", "editor", run("au-space", "agent-helper", "Manual run of automation: Spaced", p, "editor")},
		{"au-disabled", "editor", run("au-disabled", "agent-helper", "Manual run of automation: Disabled", p, "editor")},
		{"au-guarded", "editor", run("au-guarded", "agent-helper", "Manual run of automation: Guarded", p, "editor")},
		{"au-workspace", "admin", run("au-workspace", "agent-helper", "Manual run of automation: Workspace", nil, "admin")},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			h, runs := runNowFixture()
			w := runNow(h, tc.id, tc.user)
			if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"id":"run-new"`) {
				t.Fatalf("status = %d, body %q: want 201 with the run", w.Code, w.Body.String())
			}
			if len(runs.launchReqs) != 1 {
				t.Fatalf("launches = %d, want 1", len(runs.launchReqs))
			}
			if got := runNowLaunchLine(runs.launchReqs[0]); got != tc.launch {
				t.Errorf("launched\n  %s\nwant\n  %s", got, tc.launch)
			}
		})
	}

	refusals := []struct {
		name, id  string
		launchErr error
		body      string
		launches  int
	}{
		{name: "no target", id: "au-none", body: `{"error":"automation has no agent target"}`},
		{name: "a launch refused", id: "au-plain", launchErr: errors.New("this workspace has reached its monthly budget"),
			body: `{"error":"this workspace has reached its monthly budget"}`, launches: 1},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			h, runs := runNowFixture()
			runs.launchErr = tc.launchErr
			w := runNow(h, tc.id, "editor")
			if w.Code != http.StatusBadRequest || strings.TrimSpace(w.Body.String()) != tc.body {
				t.Errorf("status = %d, body %q: want 400 %s", w.Code, w.Body.String(), tc.body)
			}
			if len(runs.launchReqs) != tc.launches {
				t.Errorf("launches = %d, want %d", len(runs.launchReqs), tc.launches)
			}
		})
	}
}
