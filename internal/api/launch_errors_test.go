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

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// Refactor plan X3c (docs/plans/codebase-refactor.md §6.7, quirk Q9):
// characterization of the six launch routes' answers to a failed launch,
// which X3c names as the tables launchErrs402, launchErrs400 and
// launchErrsDelegate. Each route is driven up to its run service's Launch,
// which answers one of three errors: the budget guard's refusal and an
// invalid run status transition, each wrapped the way the agentruns package
// wraps them ("%w: detail"), and a plain error with the shape of a database
// failure. A row pins the status, the headers and the body byte for byte,
// and that the route reached Launch once. The S5d tour pins the budget
// answers against a real run service; this table adds the other two errors,
// so DelegateRun's 409 for a transition and every route's answer to an
// error it does not name are pinned too. What a route logs is not asserted.

var (
	// launchErrBudget is the budget guard's refusal as DefaultService.Launch
	// wraps it.
	launchErrBudget = fmt.Errorf("%w: this workspace has reached its $1.00 monthly budget ($5.00 spent)",
		agentruns.ErrBudgetExceeded)
	// launchErrTransition is an invalid run status transition, wrapped as
	// the lifecycle wraps it.
	launchErrTransition = fmt.Errorf("%w: run already finished", agentruns.ErrInvalidTransition)
	// launchErrPlain is an error no route names: a database failure, quotes
	// and all, so a route that passes err.Error() through shows it.
	launchErrPlain = errors.New(`pq: relation "agent_runs" does not exist`)
)

// launchErrName names one of the three errors for a subtest.
func launchErrName(err error) string {
	switch err {
	case launchErrBudget:
		return "over budget"
	case launchErrTransition:
		return "invalid transition"
	case launchErrPlain:
		return "plain error"
	}
	return err.Error()
}

// launchErrorSend drives one launch route with its run service's Launch
// answering err, and returns the answer and how many launches the run
// service saw.
type launchErrorSend func(t *testing.T, err error) (*httptest.ResponseRecorder, int)

// launchErrorsAsEditor sends a route of the launch fixture
// (launch_run_token_test.go) as "editor", who edits proj-1, in org-1.
func launchErrorsAsEditor(route launchRoute) launchErrorSend {
	return func(t *testing.T, err error) (*httptest.ResponseRecorder, int) {
		h, runs, _ := launchFixture()
		runs.launchErr = err
		w := launchAs(route, h, asPerson("editor", "org-1"))
		return w, len(runs.launchReqs)
	}
}

// launchErrorsDraft drafts test cases for one requirement of proj-1 as its
// editor (draft_test_cases_test.go's fixture).
func launchErrorsDraft(t *testing.T, err error) (*httptest.ResponseRecorder, int) {
	h, runs := newDraftFixture(t)
	runs.launchErr = err
	w := httptest.NewRecorder()
	h.DraftTestCases(w, draftReq("editor", "proj-1", `{"requirement_ids":["`+draftReqUUID1+`"]}`))
	return w, len(runs.launchReqs)
}

// launchErrorDelegates is a crew whose every node delegates to Analyst.
type launchErrorDelegates struct{ teams.Service }

func (launchErrorDelegates) ResolveDelegates(nodeID string) ([]*teams.Node, error) {
	return []*teams.Node{{ID: "node-analyst", TeamID: "crew-1", NodeType: teams.NodeAgent, AgentID: "agent-analyst",
		Label: "Analyst"}}, nil
}

// launchErrorsDelegate delegates to Analyst with the token of a run on the
// lead node of crew-1.
func launchErrorsDelegate(t *testing.T, err error) (*httptest.ResponseRecorder, int) {
	runs := &fakeRunService{launchErr: err}
	h := newTestHandler(t, func(h *Handler) {
		h.RunService = runs
		h.TeamService = launchErrorDelegates{}
	})
	node, team, project := "node-lead", "crew-1", "proj-1"
	run := &agentruns.Run{ID: "run-lead", OrgID: "org-1", AgentID: "agent-lead", ProjectID: &project, TeamID: &team,
		TeamNodeID: &node, Status: agentruns.StatusRunning}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/delegate",
		strings.NewReader(`{"role_label":"Analyst","prompt":"Analyse P."}`))
	r = r.WithContext(context.WithValue(r.Context(), ctxRun, run))
	w := httptest.NewRecorder()
	h.DelegateRun(w, r)
	return w, len(runs.launchReqs)
}

func TestLaunchErrorAnswers(t *testing.T) {
	const (
		budget     = `{"error":"workspace monthly budget exceeded: this workspace has reached its $1.00 monthly budget ($5.00 spent)"}` + "\n"
		transition = `{"error":"invalid run status transition: run already finished"}` + "\n"
		plain      = `{"error":"pq: relation \"agent_runs\" does not exist"}` + "\n"
		delegation = `{"error":"failed to launch delegated run"}` + "\n"
	)
	launchAgent := launchErrorsAsEditor(launchRoute{name: "POST /api/v1/agents/{slug}/runs", handle: (*Handler).LaunchAgentRun,
		vars: map[string]string{"slug": "helper"}, body: `{"project_id":"proj-1","prompt":"Summarise the project."}`})
	runNow := launchErrorsAsEditor(launchRoute{name: "POST /api/v1/automations/{id}/run-now", handle: (*Handler).RunAutomationNow,
		vars: map[string]string{"id": "auto-1"}})
	crewLaunch := launchErrorsAsEditor(launchRoute{name: "POST /api/v1/crews/{id}/runs", handle: (*Handler).LaunchTeamRun,
		vars: map[string]string{"id": "crew-1"}, body: `{"project_id":"proj-1","prompt":"Summarise as a crew."}`})
	testRunAgent := launchErrorsAsEditor(launchRoute{name: "POST /api/v1/test-runs/{id}/agent-run", handle: (*Handler).LaunchTestRunAgent,
		vars: map[string]string{"id": "trun-1"}, body: `{"agent_slug":"helper"}`})

	cases := []struct {
		route      string
		send       launchErrorSend
		err        error
		wantStatus int
		wantBody   string
	}{
		// launchErrs402: an over-budget launch is a 402, anything else a 400,
		// each with the error's text.
		{"LaunchAgentRun", launchAgent, launchErrBudget, http.StatusPaymentRequired, budget},
		{"LaunchAgentRun", launchAgent, launchErrTransition, http.StatusBadRequest, transition},
		{"LaunchAgentRun", launchAgent, launchErrPlain, http.StatusBadRequest, plain},
		{"DraftTestCases", launchErrorsDraft, launchErrBudget, http.StatusPaymentRequired, budget},
		{"DraftTestCases", launchErrorsDraft, launchErrTransition, http.StatusBadRequest, transition},
		{"DraftTestCases", launchErrorsDraft, launchErrPlain, http.StatusBadRequest, plain},
		// launchErrs400: every failed launch is a 400 with the error's text.
		{"RunAutomationNow", runNow, launchErrBudget, http.StatusBadRequest, budget},
		{"RunAutomationNow", runNow, launchErrTransition, http.StatusBadRequest, transition},
		{"RunAutomationNow", runNow, launchErrPlain, http.StatusBadRequest, plain},
		{"LaunchTeamRun", crewLaunch, launchErrBudget, http.StatusBadRequest, budget},
		{"LaunchTeamRun", crewLaunch, launchErrTransition, http.StatusBadRequest, transition},
		{"LaunchTeamRun", crewLaunch, launchErrPlain, http.StatusBadRequest, plain},
		{"LaunchTestRunAgent", testRunAgent, launchErrBudget, http.StatusBadRequest, budget},
		{"LaunchTestRunAgent", testRunAgent, launchErrTransition, http.StatusBadRequest, transition},
		{"LaunchTestRunAgent", testRunAgent, launchErrPlain, http.StatusBadRequest, plain},
		// launchErrsDelegate: a transition is a 409 with its text; anything
		// else, an over-budget refusal included, a 500 whose text reaches
		// only the log.
		{"DelegateRun", launchErrorsDelegate, launchErrBudget, http.StatusInternalServerError, delegation},
		{"DelegateRun", launchErrorsDelegate, launchErrTransition, http.StatusConflict, transition},
		{"DelegateRun", launchErrorsDelegate, launchErrPlain, http.StatusInternalServerError, delegation},
	}
	for _, tc := range cases {
		t.Run(tc.route+", "+launchErrName(tc.err), func(t *testing.T) {
			w, launches := tc.send(t, tc.err)
			if launches != 1 {
				t.Fatalf("the run service saw %d launches, want 1 (status %d, body %q)", launches, w.Code, w.Body.String())
			}
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if want := (http.Header{"Content-Type": {"application/json"}}); !reflect.DeepEqual(w.Header(), want) {
				t.Errorf("headers = %v, want %v", w.Header(), want)
			}
			if w.Body.String() != tc.wantBody {
				t.Errorf("body = %q, want %q", w.Body.String(), tc.wantBody)
			}
		})
	}
}
