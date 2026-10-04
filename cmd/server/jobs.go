package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/hosting"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The work main() does itself rather than hand to a service: the boot
// reconcile of hosted runners, and the two loops it starts with go, which
// stop when ctx, the signal context, is canceled.

// reconcileHostedRunners brings each stored hosted runner's status in line
// with its container's state. A failure is logged and skips that runner, or
// all of them when the list cannot be read.
func reconcileHostedRunners(provisioner hosting.Provisioner, hostedWorkerService *hostedworkers.DefaultService) {
	if hostedList, err := hostedWorkerService.ListAll(); err != nil {
		slog.Warn("failed to list hosted workers for reconcile", "error", err)
	} else {
		for _, hw := range hostedList {
			state, err := provisioner.ContainerState(hw.ContainerName)
			if err != nil {
				slog.Warn("failed to inspect hosted runner", "container", hw.ContainerName, "error", err)
				continue
			}
			status, detail := hw.Status, hw.Detail
			switch state {
			case "missing":
				status, detail = hostedworkers.StatusError, "container not found"
			case "running":
				status, detail = hostedworkers.StatusRunning, ""
			case "exited", "created", "paused", "dead":
				status, detail = hostedworkers.StatusStopped, ""
			}
			if status != hw.Status || detail != hw.Detail {
				if _, err := hostedWorkerService.SetStatus(hw.ID, status, detail); err != nil {
					slog.Warn("failed to reconcile hosted runner", "container", hw.ContainerName, "error", err)
				}
			}
		}
	}
}

// runPurgeLoop hard-deletes the workspaces whose deletion grace period has
// expired: at once, then every 24 hours. Once a workspace's purge has
// committed, the stored files of its figures, evidence and logo are removed
// from the uploads directory (#379 bug 143: they stayed on disk), and its
// agent definitions directory is removed (#379 bug 157: it stayed on disk,
// and the next boot registered its agents again), those of the workspaces
// purged before another's purge failed included. A file or directory that
// cannot be removed is logged; the purge stands.
func runPurgeLoop(ctx context.Context, orgService *orgs.DefaultService, agentService *agents.FileService) {
	purge := func() {
		purged, err := orgService.PurgeExpired(time.Now())
		removed := removeStoredFiles(purged.Files)
		for _, id := range purged.IDs {
			if err := agentService.RemoveOrg(id); err != nil {
				slog.Warn("failed to remove a purged workspace's agent definitions", "org_id", id, "error", err)
			}
		}
		if err != nil {
			slog.Error("workspace purge failed", "error", err, "purged", len(purged.IDs), "files_removed", removed)
		} else if len(purged.IDs) > 0 {
			slog.Info("purged expired deleted workspaces", "count", len(purged.IDs), "ids", purged.IDs, "files_removed", removed)
		}
	}
	purge()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}

// runReaper sweeps every 30 seconds, the first time 30 seconds after it
// starts: it fails stale runs, deletes expired sessions and invitations, and
// ends lapsed runner leases when there is a runner pool.
func runReaper(
	ctx context.Context,
	runService *agentruns.DefaultService,
	userRepo *postgres.UserRepository,
	invitationService *invitations.DefaultService,
	runnerSessionService runnersessions.Service,
	sessionPolicy users.SessionPolicy,
) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ids, err := runService.FailStale(2 * time.Minute); err != nil {
				slog.Error("reaper FailStale failed", "error", err)
			} else if len(ids) > 0 {
				slog.Warn("reaper failed stale runs", "count", len(ids))
			}
			_ = userRepo.DeleteExpiredSessions(time.Now(), sessionPolicy.MaxAge, sessionPolicy.Idle)
			// Invitations that nobody accepted expire; the rows are of
			// no further use to anyone.
			_ = invitationService.PurgeExpired(time.Now())
			// Transient runners: end lapsed leases (hard expiry, idle
			// window, or a node that stopped heartbeating) so their
			// nodes go back to the pool and their credentials die.
			if runnerSessionService != nil {
				if ended, err := runnerSessionService.Sweep(time.Now()); err != nil {
					slog.Error("runner session sweep failed", "error", err)
				} else if len(ended) > 0 {
					slog.Info("ended lapsed runner sessions", "count", len(ended))
				}
			}
		}
	}
}

// removeStoredFiles unlinks stored uploads whose rows a committed delete took
// with it, as internal/api's removeStoredFiles does for a request's, and
// answers how many it removed. A file already gone is no failure; any other
// failure is logged and the rest are still removed: the record is already
// correct.
func removeStoredFiles(paths []string) int {
	removed := 0
	for _, path := range paths {
		if path == "" {
			continue
		}
		switch err := os.Remove(path); {
		case err == nil:
			removed++
		case !os.IsNotExist(err):
			slog.Warn("failed to remove a deleted record's stored file", "error", err)
		}
	}
	return removed
}
