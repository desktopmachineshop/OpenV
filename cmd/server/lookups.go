package main

import (
	"database/sql"
	"log/slog"
	"os"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The function values main() hands to services: each looks something up in
// live state when the service calls it.

// projectOrgResolver is the event bus's org resolver: the workspace of a
// project, ProjectRepository.OrgIDForProject read each time it is called,
// or "" when the lookup fails.
func projectOrgResolver(db *sql.DB) func(projectID string) string {
	projectRepo := postgres.NewProjectRepository(db)
	return func(projectID string) string {
		orgID, err := projectRepo.OrgIDForProject(projectID)
		if err != nil {
			return ""
		}
		return orgID
	}
}

// bootstrapOrgID resolves the earliest personal org (legacy worker-key
// fallback + env-key registration), OrgRepository.EarliestPersonalOrgID
// read through the orgs service each time it is called, or "" when there is
// none or the lookup fails.
func bootstrapOrgID(db *sql.DB) func() string {
	orgService := orgs.NewDefaultService(postgres.NewOrgRepository(db))
	return func() string {
		id, err := orgService.EarliestPersonalOrgID()
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

// budgetGuard is the over-budget soft-block, agentruns.BudgetGuard over the
// workspace's budget and the run service's month-to-date spend.
func budgetGuard(orgService *orgs.DefaultService, runService *agentruns.DefaultService) func(orgID string) (bool, string) {
	return agentruns.BudgetGuard(orgService, runService)
}
