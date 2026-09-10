package hosting

import (
	"context"
	"fmt"
	"os"
	"sort"
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
// for an org's runner. It touches no docker API, so it is the unit-testable
// half of Provision.
func buildRunnerSpec(image, netName, apiURL, orgID, workerKey string, extraEnv map[string]string, limits ResourceLimits) runnerSpec {
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
		hostConfig: &container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Binds:         []string{volumeName(orgID) + ":/data"},
			Resources: container.Resources{
				// Zero values mean "no cap" to docker, matching ResourceLimits.
				Memory:   limits.MemoryMB * 1024 * 1024,
				NanoCPUs: limits.NanoCPUs,
			},
		},
	}
	if netName != "" {
		spec.networking = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{netName: {}},
		}
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

	spec := buildRunnerSpec(p.image, p.network, p.apiURL, orgID, workerKey, extraEnv, limits)

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
