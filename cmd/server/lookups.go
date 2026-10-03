package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// The function values main() hands to services: each looks something up in
// live state when the service calls it.

// projectOrgResolver is the event bus's org resolver: the workspace of a
// project, or "" when the lookup fails.
func projectOrgResolver(db *sql.DB) func(projectID string) string {
	return func(projectID string) string {
		var orgID string
		if err := db.QueryRow(`SELECT COALESCE(org_id::text, '') FROM projects WHERE id = $1::uuid`, projectID).Scan(&orgID); err != nil {
			return ""
		}
		return orgID
	}
}

// bootstrapOrgID resolves the earliest personal org (legacy worker-key
// fallback + env-key registration).
func bootstrapOrgID(db *sql.DB) func() string {
	return func() string {
		var id string
		err := db.QueryRow(`
			SELECT o.id FROM organizations o
			JOIN org_members m ON m.org_id = o.id
			JOIN users u ON u.id = m.user_id
			WHERE o.org_type = 'personal'
			ORDER BY u.created_at LIMIT 1
		`).Scan(&id)
		if err != nil {
			return ""
		}
		return id
	}
}

// downloadEvidenceSource is the test evidence a document download carries:
// the project's latest result per test case and its test runs.
func downloadEvidenceSource(vvService *vv.DefaultService) downloads.EvidenceSource {
	return func(projectID string) (map[string]*vv.TestResult, []*vv.TestRun, error) {
		latest, err := vvService.LatestResults(projectID)
		if err != nil {
			return nil, nil, err
		}
		runs, err := vvService.ListRuns(projectID)
		if err != nil {
			return nil, nil, err
		}
		return latest, runs, nil
	}
}

// downloadWorkspaceSource is what a document download shows of the
// project's workspace: its name, and its logo when that can be read.
func downloadWorkspaceSource(projectService projects.Service, orgService *orgs.DefaultService) downloads.WorkspaceSource {
	return func(projectID string) (reports.Workspace, error) {
		project, err := projectService.GetProject(projectID)
		if err != nil || project == nil || project.OrgID == "" {
			return reports.Workspace{}, err
		}
		org, err := orgService.Get(project.OrgID)
		if err != nil || org == nil {
			return reports.Workspace{}, err
		}
		ws := reports.Workspace{Name: org.Name}
		if org.LogoPath != "" {
			if logo, err := os.ReadFile(org.LogoPath); err == nil {
				ws.Logo, ws.LogoMime = logo, org.LogoMime
			} else {
				slog.Warn("download: workspace logo could not be read", "org_id", org.ID, "error", err)
			}
		}
		return ws, nil
	}
}

// budgetGuard is the over-budget soft-block: it refuses a launch once the
// workspace's month-to-date spend has reached its monthly budget, and lets
// it through when the workspace has no budget or a lookup fails.
func budgetGuard(orgService *orgs.DefaultService, runService *agentruns.DefaultService) func(orgID string) (bool, string) {
	return func(orgID string) (bool, string) {
		org, err := orgService.Get(orgID)
		if err != nil || org == nil || org.MonthlyBudgetUSD == nil || *org.MonthlyBudgetUSD <= 0 {
			return false, ""
		}
		now := time.Now().UTC()
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		spend, err := runService.MonthlySpend(orgID, monthStart)
		if err != nil || spend < *org.MonthlyBudgetUSD {
			return false, ""
		}
		return true, fmt.Sprintf("this workspace has reached its $%.2f monthly budget ($%.2f spent); new runs are blocked until next month or the budget is raised", *org.MonthlyBudgetUSD, spend)
	}
}
