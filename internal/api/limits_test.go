package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// A limit refusal is a 403 carrying the arithmetic and the remedy, so the
// frontend can tell somebody what stopped them and what to do without parsing
// prose out of a sentence.
func TestALimitRefusalCarriesItsNumbersAndRemedy(t *testing.T) {
	t.Cleanup(func() { orgs.SetSelfHosted(false) })
	orgs.SetSelfHosted(false)

	h := NewHandler(HandlerDeps{})
	rec := httptest.NewRecorder()

	err := orgs.NewLimitError(orgs.LimitMaxMembers, 5, 5).WithDetail("including invitations not yet accepted")
	if !h.writeLimitError(rec, err) {
		t.Fatal("a LimitError was not recognised as one")
	}
	if rec.Code != 403 {
		t.Errorf("status %d, want 403", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}
	if body["code"] != ErrCodeLimitReached {
		t.Errorf("code is %v, want %q", body["code"], ErrCodeLimitReached)
	}
	if body["limit"] != orgs.LimitMaxMembers {
		t.Errorf("the refusal does not name the limit: %v", body["limit"])
	}
	if body["used"] != float64(5) || body["allowed"] != float64(5) {
		t.Errorf("the arithmetic is missing: used=%v allowed=%v", body["used"], body["allowed"])
	}
	remedy, _ := body["remedy"].(string)
	if !strings.Contains(remedy, "Upgrade") {
		t.Errorf("a hosted refusal does not offer the upgrade: %q", remedy)
	}
	// The prose message stands on its own too, for anything that only shows
	// `error`.
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "already has 5") || !strings.Contains(msg, "Upgrade") {
		t.Errorf("the message is not self-sufficient: %q", msg)
	}
}

// The remedy has to suit whoever can act on it. A self-hoster has no plan to
// upgrade and no one to pay; they have a setting.
func TestASelfHostedRefusalOffersTheSettingNotAPlan(t *testing.T) {
	t.Cleanup(func() { orgs.SetSelfHosted(false) })
	orgs.SetSelfHosted(true)

	h := NewHandler(HandlerDeps{})
	rec := httptest.NewRecorder()
	h.writeLimitError(rec, orgs.NewLimitError(orgs.LimitMaxProjects, 50, 50))

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	remedy, _ := body["remedy"].(string)
	if strings.Contains(remedy, "Upgrade") {
		t.Errorf("a self-hosted deployment offered a plan upgrade: %q", remedy)
	}
	if !strings.Contains(remedy, "OPENV_LIMITS") {
		t.Errorf("a self-hosted remedy does not name the setting: %q", remedy)
	}
}

// Anything that is not a limit failure must fall through untouched, so a real
// error is never disguised as a billing message.
func TestWriteLimitErrorIgnoresOtherFailures(t *testing.T) {
	h := NewHandler(HandlerDeps{})
	rec := httptest.NewRecorder()
	if h.writeLimitError(rec, orgs.ErrNotFound) {
		t.Error("a non-limit error was answered as a limit refusal")
	}
	if rec.Code != 200 {
		t.Errorf("the recorder was written to: %d", rec.Code)
	}
}

// A deployment that sets nothing enforces nothing: the count limits ship open,
// so no existing workspace starts being refused the day this lands.
func TestNothingIsEnforcedOutOfTheBox(t *testing.T) {
	t.Cleanup(func() { orgs.SetDeploymentLimits(nil) })
	orgs.SetDeploymentLimits(nil)

	for _, plan := range []string{orgs.PlanSingle, orgs.PlanFree, orgs.PlanBusiness, orgs.PlanSelfHost} {
		org := &orgs.Org{Plan: plan}
		limits := org.EffectiveLimits()
		for _, key := range []string{orgs.LimitMaxMembers, orgs.LimitMaxProjects, orgs.LimitMaxSharedWorkspaces} {
			if err := orgs.CheckCeiling(limits, key, 10_000, 1); err != nil {
				t.Errorf("plan %q refuses %s out of the box: %v", plan, key, err)
			}
		}
	}
}

// The deployment override is the self-hosting story: one variable retunes
// every workspace without touching the database.
func TestTheDeploymentOverrideTakesEffectWithoutTouchingAWorkspace(t *testing.T) {
	t.Cleanup(func() { orgs.SetDeploymentLimits(nil) })

	org := &orgs.Org{Plan: orgs.PlanSelfHost}
	if err := orgs.CheckCeiling(org.EffectiveLimits(), orgs.LimitMaxMembers, 3, 1); err != nil {
		t.Fatalf("self-host refused before any override: %v", err)
	}

	parsed, err := orgs.ParseLimits(`{"max_members": 3}`)
	if err != nil {
		t.Fatal(err)
	}
	orgs.SetDeploymentLimits(parsed)

	if err := orgs.CheckCeiling(org.EffectiveLimits(), orgs.LimitMaxMembers, 3, 1); err == nil {
		t.Error("the deployment override did not take effect on an untouched workspace")
	}
	// And the workspace can still be let past it individually.
	org.Limits = map[string]interface{}{orgs.LimitMaxMembers: 0}
	if err := orgs.CheckCeiling(org.EffectiveLimits(), orgs.LimitMaxMembers, 3, 1); err != nil {
		t.Errorf("a workspace could not be lifted above the deployment override: %v", err)
	}
}
