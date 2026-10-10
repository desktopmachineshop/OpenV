package main

import (
	"log/slog"
	"time"

	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/hosting"
)

// workspace builds the user, member and workspace services, turns the plan
// tiers on where configured, and builds invitations, worker keys, hosted
// workers and the transient runners.
func (a *app) workspace() {
	cfg := a.env()
	// Suite services.
	a.userService = users.NewDefaultService(a.userRepo)
	a.memberService = members.NewDefaultService(a.memberRepo)
	a.orgService = orgs.NewDefaultService(a.orgRepo)

	// The tiers turn on by naming the date before which a workspace keeps
	// the alpha terms (OPENV_BILLING_GRANDFATHER_BEFORE, RFC 3339). The same
	// boot writes those terms into every earlier workspace's own limits,
	// once, so the promise and the enforcement can never be out of step:
	// unset, everyone is on the alpha terms and nothing is grandfathered;
	// set, the tier values apply to workspaces created after it. A failed
	// grandfather step is fatal — booting with the tiers on and the promise
	// unkept is the one outcome that must not happen. Self-hosted
	// deployments are on their own plan and are left alone: the date is
	// parsed only when it is set and the install is not self-hosted.
	if cutoff, on, err := cfg.GrandfatherBefore(); on {
		if err != nil {
			fatal("OPENV_BILLING_GRANDFATHER_BEFORE must be an RFC 3339 date-time", err)
		}
		n, err := a.orgService.GrandfatherBefore(cutoff)
		if err != nil {
			fatal("could not grandfather workspaces created before the announced date", err)
		}
		orgs.SetTiersEnforced(true)
		slog.Info("plan tiers in force", "grandfathered_before", cutoff.UTC().Format(time.RFC3339), "newly_grandfathered", n)
	}
	a.orgTeamService = orgs.NewTeamService(a.orgRepo, a.orgService)
	// Workspace invitations (REQ-95): the only way into a workspace for
	// someone with no account, and the prerequisite for closing self-service
	// registration below.
	a.invitationService = invitations.NewDefaultService(a.invitationRepo, a.orgService)
	a.workerKeyService = workerkeys.NewDefaultService(a.workerKeyRepo)
	a.workerKeyService.SetPairingRepository(a.workerKeyRepo)
	a.hostedWorkerService = hostedworkers.NewDefaultService(a.hostedWorkerRepo)

	// Transient runners. A deployment opts in by setting RUNNER_POOL_KEY —
	// the shared credential its pool nodes present — because without a pool
	// there is nothing to lease. Everything else about the feature is on by
	// default for the workspaces on that deployment.
	a.runnerPoolKey = cfg.RunnerPoolKey()
	if a.runnerPoolKey != "" {
		a.runnerSessionService = runnersessions.NewDefaultService(a.runnerSessionRepo, a.workerKeyService)
		slog.Info("transient runners enabled (runner pool configured)")
	} else {
		slog.Info("transient runners disabled (set RUNNER_POOL_KEY to enable)")
	}
}

// runners reconciles the hosted runners with their containers and registers
// the legacy worker key.
func (a *app) runners() {
	// Hosted runner provisioner (Docker). Disabled when HOSTED_RUNNERS=off
	// or the docker daemon is unreachable. Boot reconcile syncs stored
	// records with actual container state. The provisioner reads the
	// container's settings once docker answers, and the process cap then and
	// for each runner it provisions.
	cfg := a.env()
	a.provisioner = hosting.NewProvisioner(hosting.Settings{
		Off: cfg.HostedRunnersOff(),
		Container: func() hosting.Container {
			c := cfg.RunnerContainer()
			return hosting.Container{Image: c.Image, Network: c.Network, APIURL: c.APIURL}
		},
		PidsLimit: func() int64 { return cfg.HostedRunnerPidsLimit() },
	})
	if a.provisioner.Enabled() {
		reconcileHostedRunners(a.provisioner, a.hostedWorkerService)
	}

	a.bootstrapOrgID = bootstrapOrgID(a.db)
	if a.workerKey != "" {
		if orgID := a.bootstrapOrgID(); orgID != "" {
			if err := a.workerKeyService.EnsureBootstrapKey(orgID, a.workerKey, "env-bootstrap"); err != nil {
				slog.Warn("failed to register WORKER_API_KEY as an org key", "error", err)
			}
		}
	}
}
