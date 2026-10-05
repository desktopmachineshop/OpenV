package agentruns

import "time"

// PersonalRunners reports whether an account's personal runner in a
// workspace has polled since a time. workerkeys.DefaultService is one;
// cmd/server passes it in, since this package does not import workerkeys.
type PersonalRunners interface {
	HasOnlinePersonalRunner(orgID, userID string, since time.Time) (bool, error)
}

// RunnerGrace gives a workspace's runner grace in whole seconds, or 0 when
// it sets none or cannot be read, which leaves DefaultGraceSeconds to apply.
// orgs.DefaultService is one; cmd/server passes it in.
type RunnerGrace interface {
	RunnerGraceSeconds(orgID string) int
}

// RoutingPolicy is the first-refusal routing that SetRoutingPolicy takes, as
// its two functions in order. hasPersonalRunner: the launcher's personal
// runner in the run's workspace polled within the last 30 seconds; a check
// that fails reserves nothing, whatever it answered. graceSeconds: the
// workspace's runner grace, read each time Launch reserves a run.
func RoutingPolicy(runners PersonalRunners, grace RunnerGrace) (hasPersonalRunner func(orgID, userID string) bool, graceSeconds func(orgID string) int) {
	hasPersonalRunner = func(orgID, userID string) bool {
		online, err := runners.HasOnlinePersonalRunner(orgID, userID, time.Now().Add(-30*time.Second))
		return err == nil && online
	}
	return hasPersonalRunner, grace.RunnerGraceSeconds
}
