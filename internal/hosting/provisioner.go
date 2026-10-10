// Package hosting provisions platform-managed runner containers ("hosted
// runners") on the local Docker daemon. One container per org, running the
// openv-worker image (agentd in --hosted mode) with the org's provider API
// keys injected as environment variables at provision time.
package hosting

import (
	"errors"
	"log"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// ResourceLimits caps a hosted runner container's resources. A zero value
// means "no cap" for that resource (docker's own convention), but callers
// normally build one via ResourceLimitsForOrg, which always produces caps.
type ResourceLimits struct {
	// MemoryMB is the container memory limit in MiB.
	MemoryMB int64
	// NanoCPUs is the CPU quota in units of 1e-9 CPUs (docker NanoCPUs).
	NanoCPUs int64
}

// ResourceLimitsForOrg derives the org's runner caps from its effective
// limits (explicit org limits merged over plan defaults). An explicit
// non-positive number is the operator's "no cap" opt-out; a non-numeric value
// falls back to the plan default, so a corrupted limits row can never mean
// "unlimited".
func ResourceLimitsForOrg(o *orgs.Org) ResourceLimits {
	eff := o.EffectiveLimits()
	defaults := orgs.PlanDefaults(o.EntitledPlan())
	read := func(key string) float64 {
		v, ok := orgs.LimitFloat(eff, key)
		if !ok {
			v, _ = orgs.LimitFloat(defaults, key)
		}
		return v
	}
	var rl ResourceLimits
	if v := read(orgs.LimitRunnerMemoryMB); v > 0 {
		rl.MemoryMB = int64(v)
	}
	if v := read(orgs.LimitRunnerCPUs); v > 0 {
		rl.NanoCPUs = int64(v * 1e9)
	}
	return rl
}

// Provisioner manages hosted runner containers.
type Provisioner interface {
	// Enabled reports whether hosted runners can be provisioned on this
	// deployment (feature on and docker daemon reachable at startup).
	Enabled() bool
	// Provision creates the org's data volume (openv-runner-<org>) and runs
	// the runner container capped at limits. extraEnv carries provider API
	// keys; they reach the container environment only and are never
	// persisted.
	Provision(orgID, containerName, workerKey string, extraEnv map[string]string, limits ResourceLimits) error
	Start(containerName string) error
	Stop(containerName string) error
	// Remove force-removes the container (tolerating absence) and, when
	// purgeVolume is set, the org's data volume.
	Remove(containerName string, purgeVolume bool, orgID string) error
	// ContainerState returns the container's state ("running", "exited",
	// ...) or "missing" when no such container exists.
	ContainerState(containerName string) (string, error)
}

// Settings are the hosted runners' settings, which cmd/server reads
// (internal/config) and hands NewProvisioner. Container and PidsLimit are
// read when NewProvisioner calls them, as it read the variables before
// refactor step X10b: Container once the docker daemon answers, and
// PidsLimit then and again for each runner the provisioner provisions.
type Settings struct {
	// Off is HOSTED_RUNNERS=off, in any case: the feature is off and docker
	// is never dialled. Any other value leaves it on, with a warning (#379,
	// bug 225), which the reader logs.
	Off bool
	// Container is what a runner container is started from.
	Container func() Container
	// PidsLimit is a runner container's process cap,
	// HOSTED_RUNNER_PIDS_LIMIT: a whole number, defaultPidsLimit by default
	// and 0 (no cap, docker's own convention) for 0 or less, the one count
	// whose zero keeps a meaning of its own; anything else malformed falls
	// back to the default rather than to unlimited, and warns once
	// (internal/envparse).
	PidsLimit func() int64
}

// Container is what a hosted runner container is started from.
type Container struct {
	Image   string // RUNNER_IMAGE, default openv-worker:latest
	Network string // RUNNER_NETWORK ("" = default bridge)
	APIURL  string // RUNNER_API_URL, default http://api:8080: the API as seen from inside the runner container
}

// NewProvisioner builds the deployment's provisioner from s. s.Off disables
// the feature, and an unreachable docker daemon auto-disables it (logged
// once at startup).
func NewProvisioner(s Settings) Provisioner {
	if s.Off {
		log.Print("Hosted runners disabled (HOSTED_RUNNERS=off)")
		return disabledProvisioner{}
	}
	p, err := newDockerProvisioner(s)
	if err != nil {
		log.Printf("Hosted runners disabled: %v", err)
		return disabledProvisioner{}
	}
	log.Printf("Hosted runners enabled (image %s)", p.image)
	// Provision reads the process cap for each runner; reading it here too
	// names a malformed HOSTED_RUNNER_PIDS_LIMIT in the boot log (#379,
	// question 15) rather than at the first provision.
	p.pidsLimit()
	return p
}

var errDisabled = errors.New("hosted runners are not enabled on this deployment")

// disabledProvisioner is the stub used when the feature is off or the docker
// daemon is unreachable.
type disabledProvisioner struct{}

func (disabledProvisioner) Enabled() bool { return false }
func (disabledProvisioner) Provision(orgID, containerName, workerKey string, extraEnv map[string]string, limits ResourceLimits) error {
	return errDisabled
}
func (disabledProvisioner) Start(containerName string) error { return errDisabled }
func (disabledProvisioner) Stop(containerName string) error  { return errDisabled }
func (disabledProvisioner) Remove(containerName string, purgeVolume bool, orgID string) error {
	return errDisabled
}
func (disabledProvisioner) ContainerState(containerName string) (string, error) {
	return "", errDisabled
}
