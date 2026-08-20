// Package hosting provisions platform-managed runner containers ("hosted
// runners") on the local Docker daemon. One container per org, running the
// openv-worker image (agentd in --hosted mode) with the org's provider API
// keys injected as environment variables at provision time.
package hosting

import (
	"errors"
	"log"
	"os"
	"strings"
)

// Provisioner manages hosted runner containers.
type Provisioner interface {
	// Enabled reports whether hosted runners can be provisioned on this
	// deployment (feature on and docker daemon reachable at startup).
	Enabled() bool
	// Provision creates the org's data volume (openv-runner-<org>) and runs
	// the runner container. extraEnv carries provider API keys; they reach
	// the container environment only and are never persisted.
	Provision(orgID, containerName, workerKey string, extraEnv map[string]string) error
	Start(containerName string) error
	Stop(containerName string) error
	// Remove force-removes the container (tolerating absence) and, when
	// purgeVolume is set, the org's data volume.
	Remove(containerName string, purgeVolume bool, orgID string) error
	// ContainerState returns the container's state ("running", "exited",
	// ...) or "missing" when no such container exists.
	ContainerState(containerName string) (string, error)
}

// NewProvisioner builds the deployment's provisioner. HOSTED_RUNNERS=off
// disables the feature; an unreachable docker daemon auto-disables it (logged
// once at startup).
func NewProvisioner() Provisioner {
	if strings.EqualFold(os.Getenv("HOSTED_RUNNERS"), "off") {
		log.Print("Hosted runners disabled (HOSTED_RUNNERS=off)")
		return disabledProvisioner{}
	}
	p, err := newDockerProvisioner()
	if err != nil {
		log.Printf("Hosted runners disabled: %v", err)
		return disabledProvisioner{}
	}
	log.Printf("Hosted runners enabled (image %s)", p.image)
	return p
}

var errDisabled = errors.New("hosted runners are not enabled on this deployment")

// disabledProvisioner is the stub used when the feature is off or the docker
// daemon is unreachable.
type disabledProvisioner struct{}

func (disabledProvisioner) Enabled() bool { return false }
func (disabledProvisioner) Provision(orgID, containerName, workerKey string, extraEnv map[string]string) error {
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
