package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/usermetrics"
	"github.com/openv/requirements-platform/internal/domain/users"
)

type usageFake struct{}

func (usageFake) Usage() (*usermetrics.Usage, error) {
	return &usermetrics.Usage{
		Weeks:      []string{"2026-11-10"},
		PaceWeeks:  usermetrics.PaceWeeks,
		Workspaces: []usermetrics.WorkspaceUsage{{ID: "org-1", WorkspaceCounts: usermetrics.WorkspaceCounts{Requirements: 4}, Pace: usermetrics.Pace{Trend: usermetrics.TrendNew}}},
		Users:      []usermetrics.UserUsage{},
	}, nil
}

// The usage view is a platform admin's: counts and paces, nobody else's.
func TestAdminUsageMetrics(t *testing.T) {
	h, _ := newAdminHandler(t)
	h.UsageMetrics = usageFake{}
	for name, user := range map[string]*users.User{"visitor": nil, "member": {ID: "dave"}} {
		w := httptest.NewRecorder()
		h.AdminUsageMetrics(w, adminReq(http.MethodGet, "/api/v1/admin/metrics/usage", "", user, nil))
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d", name, w.Code)
		}
	}
	// A stable-channel workspace with no stable release yet waits for it.
	w := httptest.NewRecorder()
	h.AdminUsageMetrics(w, adminReq(http.MethodGet, "/api/v1/admin/metrics/usage", "", &users.User{ID: "root", IsAdmin: true}, nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("before its release: status = %d", w.Code)
	}
	h.OrgService = &adminOrgFake{fakeOrgService: fakeOrgService{plan: orgs.PlanFree}, ids: []string{"org-1"}}
	w = httptest.NewRecorder()
	h.AdminUsageMetrics(w, adminReq(http.MethodGet, "/api/v1/admin/metrics/usage", "", &users.User{ID: "root", IsAdmin: true}, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("admin: status = %d %s", w.Code, w.Body.String())
	}
	for _, want := range []string{`"requirements":4`, `"trend":"new"`, `"pace_weeks":4`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, w.Body.String())
		}
	}

	h.UsageMetrics = nil
	w = httptest.NewRecorder()
	h.AdminUsageMetrics(w, adminReq(http.MethodGet, "/api/v1/admin/metrics/usage", "", &users.User{ID: "root", IsAdmin: true}, nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("no service: status = %d", w.Code)
	}
}
