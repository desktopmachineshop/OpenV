package hosting

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
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
// reachable; callers treat an error as "feature disabled".
func newDockerProvisioner() (*dockerProvisioner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
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

// Provision creates the org volume and runs the runner container, capped at
// the org's resource limits. The image is assumed to be present on the docker
// host (built via `make worker-image`); no pull is attempted.
func (p *dockerProvisioner) Provision(orgID, containerName, workerKey string, extraEnv map[string]string, limits ResourceLimits) error {
	ctx := context.Background()

	volName := volumeName(orgID)
	if _, err := p.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   volName,
		Labels: map[string]string{"openv.org": orgID},
	}); err != nil {
		return fmt.Errorf("create volume %s: %w", volName, err)
	}

	env := []string{
		"OPENV_API_URL=" + p.apiURL,
		"WORKER_API_KEY=" + workerKey,
		"OPENV_HOSTED=true",
	}
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}

	cfg := &container.Config{
		Image:  p.image,
		Env:    env,
		Labels: map[string]string{"openv.org": orgID},
	}
	hostCfg := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		Binds:         []string{volName + ":/data"},
		Resources: container.Resources{
			// Zero values mean "no cap" to docker, matching ResourceLimits.
			Memory:   limits.MemoryMB * 1024 * 1024,
			NanoCPUs: limits.NanoCPUs,
		},
	}
	var netCfg *network.NetworkingConfig
	if p.network != "" {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{p.network: {}},
		}
	}

	created, err := p.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") {
			return fmt.Errorf("runner image %q not found on the docker host — build it with `make worker-image` first", p.image)
		}
		return fmt.Errorf("create container %s: %w", containerName, err)
	}
	if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", containerName, err)
	}
	return nil
}

// Start starts a stopped runner container.
func (p *dockerProvisioner) Start(containerName string) error {
	if err := p.cli.ContainerStart(context.Background(), containerName, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", containerName, err)
	}
	return nil
}

// Stop stops a running runner container.
func (p *dockerProvisioner) Stop(containerName string) error {
	timeout := 30
	if err := p.cli.ContainerStop(context.Background(), containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("stop container %s: %w", containerName, err)
	}
	return nil
}

// Remove force-removes the container (a missing container is not an error)
// and optionally the org's data volume.
func (p *dockerProvisioner) Remove(containerName string, purgeVolume bool, orgID string) error {
	ctx := context.Background()
	if err := p.cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true}); err != nil && !client.IsErrNotFound(err) {
		return fmt.Errorf("remove container %s: %w", containerName, err)
	}
	if purgeVolume {
		if err := p.cli.VolumeRemove(ctx, volumeName(orgID), true); err != nil && !client.IsErrNotFound(err) {
			return fmt.Errorf("remove volume %s: %w", volumeName(orgID), err)
		}
	}
	return nil
}

// ContainerState reports the container's docker state, or "missing".
func (p *dockerProvisioner) ContainerState(containerName string) (string, error) {
	inspect, err := p.cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "missing", nil
		}
		return "", err
	}
	if inspect.State == nil {
		return "unknown", nil
	}
	return inspect.State.Status, nil
}
