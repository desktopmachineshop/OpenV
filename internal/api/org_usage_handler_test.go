package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func usageReq(userID, orgID, query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/"+orgID+"/usage"+query, nil)
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

// TestGetOrgUsageAccess locks in the access decision: every workspace member
// gets the org-wide usage read (it is their shared workspace and the rollup
// carries no run content), non-members are refused, and a denied request
// never reaches the service.
func TestGetOrgUsageAccess(t *testing.T) {
	const orgID = "org-1"
	newFixture := func() (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{}
		h := &Handler{
			runService: runSvc,
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}},
		}
		return h, runSvc
	}

	cases := []struct {
		name     string
		userID   string
		wantCode int
	}{
		{"plain member gets the org-wide rollup", "member", http.StatusOK},
		{"admin passes too", "admin", http.StatusOK},
		{"non-member gets 403", "stranger", http.StatusForbidden},
		{"unauthenticated gets 401", "", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, runSvc := newFixture()
			w := httptest.NewRecorder()
			h.GetOrgUsage(w, usageReq(tc.userID, orgID, ""))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.wantCode == http.StatusOK {
				if len(runSvc.usageArgs) != 1 || runSvc.usageArgs[0] != orgID {
					t.Fatalf("Usage called with %v, want [%s]", runSvc.usageArgs, orgID)
				}
			} else if len(runSvc.usageArgs) != 0 {
				t.Fatalf("service called %d times on denied request, want 0", len(runSvc.usageArgs))
			}
		})
	}
}

// TestGetOrgUsageWindow locks in the ?days= contract: default 30, clamp to
// 365, reject junk — and the response echoes the effective window.
func TestGetOrgUsageWindow(t *testing.T) {
	const orgID = "org-1"
	newFixture := func() (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{usageResult: &agentruns.UsageSummary{
			ByAgent: []agentruns.AgentUsage{{AgentSlug: "reviewer", Runs: 2, TokensIn: 10, TokensOut: 5, CostUSD: 0.25}},
			ByDay:   []agentruns.DailyUsage{{Day: "2026-08-01", Runs: 2, TokensIn: 10, TokensOut: 5, CostUSD: 0.25}},
		}}
		h := &Handler{
			runService: runSvc,
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"member": orgs.RoleMember},
			}},
		}
		return h, runSvc
	}

	getDays := func(t *testing.T, query string) (int, *fakeRunService) {
		t.Helper()
		h, runSvc := newFixture()
		w := httptest.NewRecorder()
		h.GetOrgUsage(w, usageReq("member", orgID, query))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var summary agentruns.UsageSummary
		if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		return summary.Days, runSvc
	}

	t.Run("defaults to 30 days", func(t *testing.T) {
		days, runSvc := getDays(t, "")
		if days != 30 {
			t.Errorf("days = %d, want 30", days)
		}
		wantSince := time.Now().UTC().AddDate(0, 0, -30)
		if got := runSvc.usageSince[0]; got.Before(wantSince.Add(-time.Minute)) || got.After(wantSince.Add(time.Minute)) {
			t.Errorf("since = %v, want ~%v", got, wantSince)
		}
	})

	t.Run("honors an explicit window", func(t *testing.T) {
		if days, _ := getDays(t, "?days=7"); days != 7 {
			t.Errorf("days = %d, want 7", days)
		}
	})

	t.Run("clamps oversized windows to 365", func(t *testing.T) {
		if days, _ := getDays(t, "?days=9999"); days != 365 {
			t.Errorf("days = %d, want 365", days)
		}
	})

	t.Run("rejects junk", func(t *testing.T) {
		for _, q := range []string{"?days=0", "?days=-3", "?days=abc"} {
			h, runSvc := newFixture()
			w := httptest.NewRecorder()
			h.GetOrgUsage(w, usageReq("member", orgID, q))
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", q, w.Code)
			}
			if len(runSvc.usageArgs) != 0 {
				t.Errorf("%s: service called on invalid input", q)
			}
		}
	})
}
