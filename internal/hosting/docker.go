package hosting

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// dockerProvisioner implements Provisioner against the local docker daemon
// (typically via a mounted /var/run/docker.sock).
type dockerProvisioner struct {
	cli     *client.Client
	image   string // RUNNER_IMAGE
	network string // RUNNER_NETWORK ("" = default bridge)
	apiURL  string // RUNNER_API_URL as seen from inside the runner container
}

// newDockerProvisioner connects to the docker daemon and verifies it is
// reachable; callers treat an error as "feature disabled". The ping doubles
// as the client's API-version negotiation.
func newDockerProvisioner() (*dockerProvisioner, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		cli.Close()
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}
	return &dockerProvisioner{
		cli:     cli,
		image:   envOr("RUNNER_IMAGE", "openv-worker:latest"),
		network: os.Getenv("RUNNER_NETWORK"),
		apiURL:  envOr("RUNNER_API_URL", "http://api:8080"),
	}, nil
}

func (p *dockerProvisioner) Enabled() bool { return true }

// defaultPidsLimit caps the processes a runner container may create. A vendor
// CLI is node plus a handful of children, so a few hundred is generous; the
// point is that a fork bomb — from a bug, a hostile repository, or a prompt
// injection that reached a shell — cannot take the docker host down with it.
const defaultPidsLimit int64 = 256

// PidsLimit is the per-container process cap, overridable with
// HOSTED_RUNNER_PIDS_LIMIT. A value of 0 or less means "no cap" (docker's own
// convention) for an operator who has to lift it; anything unparseable falls
// back to the default rather than to unlimited.
func PidsLimit() int64 {
	raw := os.Getenv("HOSTED_RUNNER_PIDS_LIMIT")
	if raw == "" {
		return defaultPidsLimit
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		log.Printf("hosting: HOSTED_RUNNER_PIDS_LIMIT=%q is not a number; using %d", raw, defaultPidsLimit)
		return defaultPidsLimit
	}
	if n <= 0 {
		return 0
	}
	return n
}

// hostConfigFor builds the runner container's HostConfig: the org's data
// volume, its resource caps, and the isolation a container that runs a vendor
// CLI on someone else's prompt needs (REQ-96, HAZ-2).
//
//   - CapDrop ALL — a runner needs no Linux capability at all; it runs one
//     node process as an unprivileged user.
//   - no-new-privileges — nothing inside can gain privilege through a setuid
//     binary, so a compromised CLI cannot climb.
//   - PidsLimit — a fork bomb hits a wall instead of the host.
//   - Memory / NanoCPUs — the org's plan caps, as before.
//
// ReadonlyRootfs is deliberately NOT set: the runner image's vendor CLIs write
// outside /data (npm and CLI caches under /tmp, git's temporary files), so a
// read-only root filesystem breaks runs today. Getting there means giving each
// of those paths a tmpfs mount, which is worth doing but is not this change.
func hostConfigFor(volName string, limits ResourceLimits, pidsLimit int64) *container.HostConfig {
	cfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Binds:         []string{volName + ":/data"},
		CapDrop:       []string{"ALL"},
		SecurityOpt:   []string{"no-new-privileges:true"},
		Resources: container.Resources{
			// Zero values mean "no cap" to docker, matching ResourceLimits.
			Memory:   limits.MemoryMB * 1024 * 1024,
			NanoCPUs: limits.NanoCPUs,
		},
	}
	if pidsLimit > 0 {
		cfg.Resources.PidsLimit = &pidsLimit
	}
	return cfg
}

// volumeName is the org's persistent runner volume (HOME, workspaces, CLI
// state live under /data).
func volumeName(orgID string) string {
	return "openv-runner-" + orgID
}

// runnerSpec is the docker-side description of an org's runner container: the
// three configuration blocks handed to ContainerCreate.
type runnerSpec struct {
	config     *container.Config
	hostConfig *container.HostConfig
	networking *network.NetworkingConfig
}

// buildRunnerSpec assembles the container, host and networking configuration
// for an org's runner — including the isolation hostConfigFor applies. It
// touches no docker API, so it is the unit-testable half of Provision.
func buildRunnerSpec(image, netName, apiURL, orgID, workerKey string, extraEnv map[string]string, limits ResourceLimits, pidsLimit int64) runnerSpec {
	env := []string{
		"OPENV_API_URL=" + apiURL,
		"WORKER_API_KEY=" + workerKey,
		"OPENV_HOSTED=true",
	}
	keys := make([]string, 0, len(extraEnv))
	for k := range extraEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extraEnv[k])
	}

	spec := runnerSpec{
		config: &container.Config{
			Image:  image,
			Env:    env,
			Labels: map[string]string{"openv.org": orgID},
		},
		hostConfig: hostConfigFor(volumeName(orgID), limits, pidsLimit),
	}
	// The runner belongs on a network of its own, reaching the API and the
	// providers and nothing else — not the database, not the other services
	// (REQ-96). Without RUNNER_NETWORK it lands on docker's default bridge,
	// where it can talk to every other container on it, so say so once per
	// spec rather than letting it pass silently.
	if netName != "" {
		spec.networking = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{netName: {}},
		}
	} else {
		log.Printf("hosting: RUNNER_NETWORK is unset, so the runner for org %s joins the default bridge network and can reach every container on it; see docs/operations.md (hosted runner isolation)", orgID)
	}
	return spec
}

// Provision creates the org volume and runs the runner container, capped at
// the org's resource limits. The image is assumed to be present on the docker
// host (built via `make worker-image`); no pull is attempted.
func (p *dockerProvisioner) Provision(orgID, containerName, workerKey string, extraEnv map[string]string, limits ResourceLimits) error {
	ctx := context.Background()

	volName := volumeName(orgID)
	if _, err := p.cli.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:   volName,
		Labels: map[string]string{"openv.org": orgID},
	}); err != nil {
		return fmt.Errorf("create volume %s: %w", volName, err)
	}

	spec := buildRunnerSpec(p.image, p.network, p.apiURL, orgID, workerKey, extraEnv, limits, PidsLimit())

	created, err := p.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           spec.config,
		HostConfig:       spec.hostConfig,
		NetworkingConfig: spec.networking,
		Name:             containerName,
	})
	if err != nil {
		if strings.Contains(err.Error(), "No such image") {
			return fmt.Errorf("runner image %q not found on the docker host — build it with `make worker-image` first", p.image)
		}
		return fmt.Errorf("create container %s: %w", containerName, err)
	}
	if _, err := p.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", containerName, err)
	}
	return nil
}

// Start starts a stopped runner container.
func (p *dockerProvisioner) Start(containerName string) error {
	if _, err := p.cli.ContainerStart(context.Background(), containerName, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", containerName, err)
	}
	return nil
}

// Stop stops a running runner container.
func (p *dockerProvisioner) Stop(containerName string) error {
	timeout := 30
	if _, err := p.cli.ContainerStop(context.Background(), containerName, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("stop container %s: %w", containerName, err)
	}
	return nil
}

// Remove force-removes the container (a missing container is not an error)
// and optionally the org's data volume.
func (p *dockerProvisioner) Remove(containerName string, purgeVolume bool, orgID string) error {
	ctx := context.Background()
	if _, err := p.cli.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("remove container %s: %w", containerName, err)
	}
	if purgeVolume {
		if _, err := p.cli.VolumeRemove(ctx, volumeName(orgID), client.VolumeRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("remove volume %s: %w", volumeName(orgID), err)
		}
	}
	return nil
}

// ContainerState reports the container's docker state, or "missing".
func (p *dockerProvisioner) ContainerState(containerName string) (string, error) {
	inspect, err := p.cli.ContainerInspect(context.Background(), containerName, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "missing", nil
		}
		return "", err
	}
	if inspect.Container.State == nil {
		return "unknown", nil
	}
	return string(inspect.Container.State.Status), nil
}
