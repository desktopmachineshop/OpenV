package main

import (
	"log/slog"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
	"github.com/openv/requirements-platform/internal/seeds"
)

// agents builds the agent engine: the agent and run services with their
// routing and retry policies, automations, repositories, providers, crews and
// proposals, and seeds each workspace's default agents and crew.
func (a *app) agents() {
	// Agent engine services.
	// The file sync backfills a definition that carries no allowlist (REQ-91),
	// and it runs before seeds.EnsureOrgDefaults, so it is the one that
	// decides what a seeded agent ends up with — hence the seed lookup.
	var err error
	a.agentService, err = agents.NewFileService(a.agentsDir, a.agentRepo,
		agents.WithSeedAllowedTools(seeds.SeedAllowedTools))
	if err != nil {
		fatal("failed to initialize agent service", err)
	}
	if err := a.agentService.SyncAllFromDisk(); err != nil {
		slog.Warn("agent sync completed with warnings", "error", err)
	}
	a.runService = agentruns.NewDefaultService(a.agentRunRepo, a.agentService, a.bus)
	// First-refusal routing: runs launched by a user with an online personal
	// runner wait for it before hosted/workspace runners may claim.
	a.runService.SetRoutingPolicy(
		func(orgID, userID string) bool {
			online, err := a.workerKeyService.HasOnlinePersonalRunner(orgID, userID, time.Now().Add(-30*time.Second))
			return err == nil && online
		},
		func(orgID string) int {
			org, err := a.orgService.Get(orgID)
			if err != nil {
				return 0
			}
			if v, ok := org.Limits["runner_grace_seconds"].(float64); ok {
				return int(v)
			}
			return 0
		})
	// Bounded auto-retry (issue #184): a retryable terminal failure
	// (provider_unavailable | timeout | worker_error) re-enqueues a fresh
	// attempt with backoff while attempts remain. OPENV_RUN_MAX_ATTEMPTS caps
	// the chain (default 3); OPENV_RUN_AUTO_RETRY=false (or a cap of 1) opts
	// out and the failure simply stands.
	maxAttempts := envInt("OPENV_RUN_MAX_ATTEMPTS", agentruns.DefaultMaxAttempts)
	autoRetry := envBool("OPENV_RUN_AUTO_RETRY", true)
	a.runService.SetRetryPolicy(maxAttempts, autoRetry)
	a.automationService = automations.NewDefaultService(a.automationRepo)
	a.repoConnService = repoconns.NewDefaultService(a.repoConnRepo)
	a.providerService = providers.NewDefaultService(a.providerRepo)
	a.loginService = providers.NewLoginService(postgres.NewProviderLoginRepository(a.db))
	a.teamService = teams.NewDefaultService(a.teamRepo)
	// Human crew members must belong to the crew's workspace.
	a.teamService.SetMemberValidator(func(orgID, userID string) bool {
		ok, err := a.orgService.IsMember(orgID, userID)
		return err == nil && ok
	})

	// Proposal appliers execute approved agent writes via the real services.
	// They are wired after the HTTP handler is built (handler.ProposalAppliers
	// below): the appliers run the handler's own domain writes — events,
	// link-snapshot auto-versioning — but the handler needs this service, so
	// the callbacks are injected once the cycle can be closed.
	a.proposalService = proposals.NewDefaultService(a.proposalRepo, proposals.Appliers{})

	// When a run's last proposal is reviewed, finalize the run: an
	// awaiting_approval run leaves that absorbing state for succeeded (or
	// failed if any approved write failed to apply), publishing RunFinished so
	// crew successors, the notifier and automation triggers fire on the real
	// outcome. Covers single + bulk review and the applier path alike.
	a.proposalService.OnResolved(func(runID string) {
		if _, err := a.runService.FinalizeIfResolved(runID); err != nil {
			slog.Error("proposal resolution: failed to finalize run", "run_id", runID, "error", err)
		}
	})

	// Seed default agents + crew into every workspace missing them.
	if orgIDs, err := a.orgService.ListAll(); err != nil {
		slog.Warn("failed to list organizations for seeding", "error", err)
	} else {
		for _, orgID := range orgIDs {
			if err := seeds.EnsureOrgDefaults(orgID, a.agentService, a.teamService); err != nil {
				slog.Warn("failed to seed default agents/team", "org_id", orgID, "error", err)
			}
		}
	}
}
