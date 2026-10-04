package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func updateOrgReq(userID, orgID, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/"+orgID, strings.NewReader(body))
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

func budgetFixture(t *testing.T) *Handler {
	const orgID = "org-1"
	return newTestHandler(t, func(h *Handler) {
		h.OrgService = &fakeOrgService{roles: map[string]map[string]string{
			orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
		}}
	})
}

// TestUpdateOrgBudgetAuthz locks in the admin-only budget write: admins may
// set it, plain members and non-members are refused before the service is
// touched, and an unauthenticated caller gets 401.
func TestUpdateOrgBudgetAuthz(t *testing.T) {
	const orgID = "org-1"
	cases := []struct {
		name       string
		userID     string
		wantCode   int
		wantCalled bool
	}{
		{"admin sets a budget", "admin", http.StatusOK, true},
		{"member is refused", "member", http.StatusForbidden, false},
		{"non-member is told the workspace is not there", "stranger", http.StatusNotFound, false},
		{"unauthenticated is refused", "", http.StatusUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := budgetFixture(t)
			w := httptest.NewRecorder()
			h.UpdateOrg(w, updateOrgReq(tc.userID, orgID, `{"monthly_budget_usd": 250.5}`))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			fake := h.OrgService.(*fakeOrgService)
			if got := len(fake.budgetCalls) > 0; got != tc.wantCalled {
				t.Fatalf("SetMonthlyBudget called = %v, want %v", got, tc.wantCalled)
			}
			if tc.wantCalled {
				if fake.budgetCalls[0] == nil || *fake.budgetCalls[0] != 250.5 {
					t.Errorf("budget = %v, want 250.5", fake.budgetCalls[0])
				}
			}
		})
	}
}

// TestUpdateOrgBudgetBodyHandling locks in the presence semantics: a number
// sets, null clears, an absent key leaves the budget untouched, and a
// non-numeric value is a 400.
func TestUpdateOrgBudgetBodyHandling(t *testing.T) {
	const orgID = "org-1"

	t.Run("null clears the budget", func(t *testing.T) {
		h := budgetFixture(t)
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"monthly_budget_usd": null}`))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		fake := h.OrgService.(*fakeOrgService)
		if len(fake.budgetCalls) != 1 || fake.budgetCalls[0] != nil {
			t.Fatalf("budgetCalls = %v, want a single nil (clear)", fake.budgetCalls)
		}
	})

	t.Run("a plain rename never touches the budget", func(t *testing.T) {
		h := budgetFixture(t)
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"name": "Renamed"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		fake := h.OrgService.(*fakeOrgService)
		if len(fake.budgetCalls) != 0 {
			t.Fatalf("SetMonthlyBudget called on a rename-only update: %v", fake.budgetCalls)
		}
		if len(fake.updatedNames) != 1 {
			t.Fatalf("UpdateOrg calls = %d, want 1", len(fake.updatedNames))
		}
	})

	t.Run("a non-numeric budget is a 400", func(t *testing.T) {
		h := budgetFixture(t)
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"monthly_budget_usd": "lots"}`))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		fake := h.OrgService.(*fakeOrgService)
		if len(fake.budgetCalls) != 0 {
			t.Fatalf("SetMonthlyBudget called on invalid input: %v", fake.budgetCalls)
		}
	})

	t.Run("a negative budget surfaces the validation error as 400", func(t *testing.T) {
		h := budgetFixture(t)
		h.OrgService.(*fakeOrgService).budgetErr = orgs.ErrInvalidBudget
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"monthly_budget_usd": -5}`))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
	})
}

// TestUpdateOrgRefusedRequestChangesNothing: a request refused for any one
// part writes none of the others. UpdateOrg used to store the rename first
// and then refuse the budget sent with it, so the refusal left the new name
// in place, and a budget sent with a refused channel or window was stored
// too. Each case sends a rename with the part that is refused, and a valid
// budget where the budget is not the refused part.
func TestUpdateOrgRefusedRequestChangesNothing(t *testing.T) {
	const orgID = "org-1"
	cases := []struct {
		name     string
		tiers    bool
		body     string
		wantCode int
	}{
		{"a budget that is not a number", false,
			`{"name":"Renamed","monthly_budget_usd":"lots"}`, http.StatusBadRequest},
		{"a negative budget", false,
			`{"name":"Renamed","monthly_budget_usd":-1}`, http.StatusBadRequest},
		{"a budget the plan does not include", true,
			`{"name":"Renamed","monthly_budget_usd":100}`, http.StatusForbidden},
		{"a channel the plan cannot choose", false,
			`{"name":"Renamed","monthly_budget_usd":100,"release_channel":"stable"}`, http.StatusBadRequest},
		{"an upgrade window the plan cannot choose", false,
			`{"name":"Renamed","monthly_budget_usd":100,"upgrade_window":{"day":15,"hour":9,"timezone":"UTC"}}`,
			http.StatusBadRequest},
		{"an upgrade window that is not an object", false,
			`{"name":"Renamed","monthly_budget_usd":100,"upgrade_window":"soon"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tiers {
				enforceTiers(t)
			}
			h := budgetFixture(t)
			fake := h.OrgService.(*fakeOrgService)
			fake.plan = orgs.PlanSingle
			w := httptest.NewRecorder()
			h.UpdateOrg(w, updateOrgReq("admin", orgID, tc.body))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if len(fake.updatedNames) != 0 || len(fake.budgetCalls) != 0 || len(fake.channelCalls) != 0 ||
				len(fake.windowCalls) != 0 {
				t.Fatalf("a refused request wrote: names %d, budgets %d, channels %v, windows %v",
					len(fake.updatedNames), len(fake.budgetCalls), fake.channelCalls, fake.windowCalls)
			}
		})
	}

	// The same parts, each valid on a plan that may choose its channel, are
	// all written, in one request.
	h := budgetFixture(t)
	fake := h.OrgService.(*fakeOrgService)
	fake.plan = orgs.PlanBusiness
	w := httptest.NewRecorder()
	h.UpdateOrg(w, updateOrgReq("admin", orgID,
		`{"name":"Renamed","monthly_budget_usd":100,"release_channel":"nightly","upgrade_window":{"day":15,"hour":9,"timezone":"UTC"}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if len(fake.updatedNames) != 1 || len(fake.budgetCalls) != 1 || len(fake.channelCalls) != 1 ||
		len(fake.windowCalls) != 1 || fake.windowCalls[0] != "15/9/UTC" {
		t.Fatalf("writes: names %d, budgets %d, channels %v, windows %v; want one of each",
			len(fake.updatedNames), len(fake.budgetCalls), fake.channelCalls, fake.windowCalls)
	}
}
