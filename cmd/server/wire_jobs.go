package main

import (
	"log/slog"

	"github.com/openv/requirements-platform/internal/automation"
	"github.com/openv/requirements-platform/internal/scheduler"
)

// jobs sets the budget guard where enforced, starts the trigger matcher and
// the scheduler, and starts the workspace purge and the reaper.
func (a *app) jobs() {
	// Optional over-budget soft-block (default OFF — warn-only). When
	// OPENV_BUDGET_ENFORCE=true, new launches are refused once a workspace has
	// hit 100% of its monthly budget. Fails open on lookup errors so a budget
	// hiccup never blocks work.
	if envBool("OPENV_BUDGET_ENFORCE", false) {
		a.runService.SetBudgetGuard(budgetGuard(a.orgService, a.runService))
		slog.Info("workspace budget enforcement enabled: launches soft-block at 100% of budget")
	}

	// Trigger matcher + scheduler + reaper. The scheduler and reaper loops
	// stop when the signal context is canceled.
	automation.NewTriggerMatcher(a.automationRepo, a.runService, a.teamService).Start(a.bus)
	scheduler.New(a.automationRepo, a.runService, a.teamService).Start(a.ctx)

	// Workspace purge: hard-delete workspaces whose soft-delete grace period
	// (orgs.DeletionGraceDays) has expired — once at boot, then daily.
	go runPurgeLoop(a.ctx, a.orgService)
	go runReaper(a.ctx, a.runService, a.userRepo, a.invitationService, a.runnerSessionService, a.sessionPolicy)
}
