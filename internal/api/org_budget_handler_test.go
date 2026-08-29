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

func budgetFixture() *Handler {
	const orgID = "org-1"
	return &Handler{
		orgService: &fakeOrgService{roles: map[string]map[string]string{
			orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
		}},
	}
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
		{"non-member is refused", "stranger", http.StatusForbidden, false},
		{"unauthenticated is refused", "", http.StatusUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := budgetFixture()
			w := httptest.NewRecorder()
			h.UpdateOrg(w, updateOrgReq(tc.userID, orgID, `{"monthly_budget_usd": 250.5}`))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			fake := h.orgService.(*fakeOrgService)
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
		h := budgetFixture()
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"monthly_budget_usd": null}`))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		fake := h.orgService.(*fakeOrgService)
		if len(fake.budgetCalls) != 1 || fake.budgetCalls[0] != nil {
			t.Fatalf("budgetCalls = %v, want a single nil (clear)", fake.budgetCalls)
		}
	})

	t.Run("a plain rename never touches the budget", func(t *testing.T) {
		h := budgetFixture()
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"name": "Renamed"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		fake := h.orgService.(*fakeOrgService)
		if len(fake.budgetCalls) != 0 {
			t.Fatalf("SetMonthlyBudget called on a rename-only update: %v", fake.budgetCalls)
		}
		if len(fake.updatedNames) != 1 {
			t.Fatalf("UpdateOrg calls = %d, want 1", len(fake.updatedNames))
		}
	})

	t.Run("a non-numeric budget is a 400", func(t *testing.T) {
		h := budgetFixture()
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"monthly_budget_usd": "lots"}`))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		fake := h.orgService.(*fakeOrgService)
		if len(fake.budgetCalls) != 0 {
			t.Fatalf("SetMonthlyBudget called on invalid input: %v", fake.budgetCalls)
		}
	})

	t.Run("a negative budget surfaces the validation error as 400", func(t *testing.T) {
		h := budgetFixture()
		h.orgService.(*fakeOrgService).budgetErr = orgs.ErrInvalidBudget
		w := httptest.NewRecorder()
		h.UpdateOrg(w, updateOrgReq("admin", orgID, `{"monthly_budget_usd": -5}`))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
	})
}
