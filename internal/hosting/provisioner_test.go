package hosting

import (
	"bytes"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// TestResourceLimitsForOrgPlanDefaults: an org with no explicit limits gets
// caps derived from its plan; unknown plans cap at the free tier.
func TestResourceLimitsForOrgPlanDefaults(t *testing.T) {
	cases := []struct {
		plan     string
		wantMem  int64 // MiB
		wantNano int64
	}{
		{orgs.PlanFree, 2048, 1e9},
		{orgs.PlanBusinessLite, 4096, 2e9},
		{orgs.PlanTeam, 8192, 4e9},
		{"mystery-plan", 2048, 1e9},
	}
	for _, tc := range cases {
		rl := ResourceLimitsForOrg(&orgs.Org{BilledPlan: tc.plan, Limits: map[string]interface{}{}})
		if rl.MemoryMB != tc.wantMem || rl.NanoCPUs != tc.wantNano {
			t.Errorf("plan %q: limits = %+v, want mem %d nano %d", tc.plan, rl, tc.wantMem, tc.wantNano)
		}
	}
}

// TestResourceLimitsForOrgOverrides: explicit org limits (as JSON numbers,
// i.e. float64) override the plan defaults, including fractional CPUs.
func TestResourceLimitsForOrgOverrides(t *testing.T) {
	rl := ResourceLimitsForOrg(&orgs.Org{
		BilledPlan: orgs.PlanFree,
		Limits: map[string]interface{}{
			orgs.LimitRunnerMemoryMB: float64(8192),
			orgs.LimitRunnerCPUs:     0.5,
		},
	})
	if rl.MemoryMB != 8192 {
		t.Errorf("MemoryMB = %d, want 8192", rl.MemoryMB)
	}
	if rl.NanoCPUs != 5e8 {
		t.Errorf("NanoCPUs = %d, want 5e8 (half a CPU)", rl.NanoCPUs)
	}
}

// TestResourceLimitsForOrgBadValues: a non-numeric explicit value falls back
// to the plan default (a corrupted row must never mean "unlimited"), while an
// explicit non-positive number is the deliberate "no cap" opt-out.
func TestResourceLimitsForOrgBadValues(t *testing.T) {
	rl := ResourceLimitsForOrg(&orgs.Org{
		BilledPlan: orgs.PlanFree,
		Limits: map[string]interface{}{
			orgs.LimitRunnerMemoryMB: "lots",     // garbage -> plan default
			orgs.LimitRunnerCPUs:     float64(0), // explicit opt-out -> no cap
		},
	})
	if rl.MemoryMB != 2048 {
		t.Errorf("MemoryMB = %d, want free-plan default 2048 for a non-numeric value", rl.MemoryMB)
	}
	if rl.NanoCPUs != 0 {
		t.Errorf("NanoCPUs = %d, want 0 (explicit no-cap opt-out)", rl.NanoCPUs)
	}
}

// TestBuildRunnerSpec: the container/host configuration handed to docker
// carries the runner image, the fixed platform env plus the org's extra env,
// the org label, the unless-stopped restart policy, the org's data volume
// bind and its resource caps. Extra env is emitted in sorted key order so the
// spec is reproducible.
func TestBuildRunnerSpec(t *testing.T) {
	spec := buildRunnerSpec(
		"openv-worker:latest",
		"", // no RUNNER_NETWORK
		"http://api:8080",
		"org-42",
		"wk-secret",
		map[string]string{"ZED": "z", "ANTHROPIC_API_KEY": "sk-test"},
		ResourceLimits{MemoryMB: 2048, NanoCPUs: 1e9},
		defaultPidsLimit,
	)

	if spec.config.Image != "openv-worker:latest" {
		t.Errorf("Image = %q, want openv-worker:latest", spec.config.Image)
	}
	wantEnv := []string{
		"OPENV_API_URL=http://api:8080",
		"WORKER_API_KEY=wk-secret",
		"OPENV_HOSTED=true",
		"ANTHROPIC_API_KEY=sk-test",
		"ZED=z",
	}
	if !reflect.DeepEqual(spec.config.Env, wantEnv) {
		t.Errorf("Env = %q, want %q", spec.config.Env, wantEnv)
	}
	if spec.config.Labels["openv.org"] != "org-42" {
		t.Errorf("Labels = %v, want openv.org=org-42", spec.config.Labels)
	}

	if got := spec.hostConfig.RestartPolicy.Name; got != container.RestartPolicyUnlessStopped {
		t.Errorf("RestartPolicy = %q, want unless-stopped", got)
	}
	wantBinds := []string{"openv-runner-org-42:/data"}
	if !reflect.DeepEqual(spec.hostConfig.Binds, wantBinds) {
		t.Errorf("Binds = %q, want %q", spec.hostConfig.Binds, wantBinds)
	}
	if spec.hostConfig.Resources.Memory != 2048*1024*1024 {
		t.Errorf("Memory = %d, want %d bytes", spec.hostConfig.Resources.Memory, 2048*1024*1024)
	}
	if spec.hostConfig.Resources.NanoCPUs != 1e9 {
		t.Errorf("NanoCPUs = %d, want 1e9", spec.hostConfig.Resources.NanoCPUs)
	}

	if spec.networking != nil {
		t.Errorf("networking = %+v, want nil when RUNNER_NETWORK is unset", spec.networking)
	}

	// REQ-96 / HAZ-2: the spec a runner is created from carries the isolation
	// too, not just the resource caps — see TestHostConfigForHardening for the
	// field-by-field reasoning.
	if !slices.Contains(spec.hostConfig.CapDrop, "ALL") {
		t.Errorf("CapDrop = %v, want ALL", spec.hostConfig.CapDrop)
	}
	if !slices.Contains(spec.hostConfig.SecurityOpt, "no-new-privileges:true") {
		t.Errorf("SecurityOpt = %v, want no-new-privileges:true", spec.hostConfig.SecurityOpt)
	}
	if spec.hostConfig.Resources.PidsLimit == nil || *spec.hostConfig.Resources.PidsLimit != defaultPidsLimit {
		t.Errorf("PidsLimit = %v, want %d", spec.hostConfig.Resources.PidsLimit, defaultPidsLimit)
	}
}

// TestBuildRunnerSpecNetworkAndNoCaps: RUNNER_NETWORK attaches the runner to
// that network, and zero ResourceLimits stay zero ("no cap" to docker).
func TestBuildRunnerSpecNetworkAndNoCaps(t *testing.T) {
	spec := buildRunnerSpec("img", "openv-net", "http://api:8080", "org-1", "wk", nil, ResourceLimits{}, 0)

	if spec.networking == nil {
		t.Fatal("networking = nil, want an endpoint for openv-net")
	}
	if _, ok := spec.networking.EndpointsConfig["openv-net"]; ok != true || len(spec.networking.EndpointsConfig) != 1 {
		t.Errorf("EndpointsConfig = %v, want exactly openv-net", spec.networking.EndpointsConfig)
	}
	if spec.hostConfig.Resources.Memory != 0 || spec.hostConfig.Resources.NanoCPUs != 0 {
		t.Errorf("resources = %+v, want zero (no cap)", spec.hostConfig.Resources)
	}
	wantEnv := []string{"OPENV_API_URL=http://api:8080", "WORKER_API_KEY=wk", "OPENV_HOSTED=true"}
	if !reflect.DeepEqual(spec.config.Env, wantEnv) {
		t.Errorf("Env = %q, want %q", spec.config.Env, wantEnv)
	}
}

// TestDisabledProvisionerRefusesEverything: with the feature off (or docker
// absent) every operation reports the feature is not enabled rather than
// touching a nil docker client.
func TestDisabledProvisionerRefusesEverything(t *testing.T) {
	var p Provisioner = disabledProvisioner{}
	if p.Enabled() {
		t.Error("Enabled() = true, want false")
	}
	if err := p.Provision("org", "c", "wk", nil, ResourceLimits{}); !errors.Is(err, errDisabled) {
		t.Errorf("Provision err = %v, want errDisabled", err)
	}
	if err := p.Start("c"); !errors.Is(err, errDisabled) {
		t.Errorf("Start err = %v, want errDisabled", err)
	}
	if err := p.Stop("c"); !errors.Is(err, errDisabled) {
		t.Errorf("Stop err = %v, want errDisabled", err)
	}
	if err := p.Remove("c", true, "org"); !errors.Is(err, errDisabled) {
		t.Errorf("Remove err = %v, want errDisabled", err)
	}
	if _, err := p.ContainerState("c"); !errors.Is(err, errDisabled) {
		t.Errorf("ContainerState err = %v, want errDisabled", err)
	}
}

// TestNewProvisionerOff: with hosted runners off (HOSTED_RUNNERS=off, which
// cmd/server reads through internal/config) NewProvisioner short-circuits
// before any docker connection is attempted, and reads none of the other
// settings.
func TestNewProvisionerOff(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	read := 0
	p := NewProvisioner(Settings{
		Off:       true,
		Container: func() Container { read++; return Container{} },
		PidsLimit: func() int64 { read++; return defaultPidsLimit },
	})
	if p.Enabled() {
		t.Error("NewProvisioner(Settings{Off: true}) is enabled")
	}
	if !strings.Contains(logged.String(), "Hosted runners disabled (HOSTED_RUNNERS=off)") {
		t.Errorf("hosted runners off did not switch them off before docker; logged %q", logged.String())
	}
	if read != 0 {
		t.Errorf("hosted runners off: NewProvisioner read %d settings, want none", read)
	}
}

// With hosted runners on, NewProvisioner reads the container's settings
// once docker answers, then the process cap once, after the line saying
// hosted runners are on, so that a malformed HOSTED_RUNNER_PIDS_LIMIT is
// named in the boot log (#379, question 15), not first when a workspace's
// runner is provisioned. A stand-in docker daemon answers the ping.
func TestNewProvisionerReadsItsSettingsOnceDockerAnswers(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.47")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(daemon.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+daemon.Listener.Addr().String())
	for _, name := range []string{"DOCKER_API_VERSION", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"} {
		t.Setenv(name, "")
	}
	var logged bytes.Buffer
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	var reads []string
	p := NewProvisioner(Settings{
		Container: func() Container {
			reads = append(reads, "container")
			return Container{Image: "openv-worker:x10b", Network: "x10b-net", APIURL: "http://api.x10b:8080"}
		},
		PidsLimit: func() int64 {
			reads = append(reads, "pids after: "+logged.String())
			return 64
		},
	})
	d, ok := p.(*dockerProvisioner)
	if !ok {
		t.Fatalf("NewProvisioner() = %T with a docker daemon answering; log:\n%s", p, logged.String())
	}
	t.Cleanup(func() { d.cli.Close() })
	if len(reads) != 2 || reads[0] != "container" || !strings.Contains(reads[1], "Hosted runners enabled (image openv-worker:x10b)") {
		t.Errorf("NewProvisioner read %q; want the container's settings, then the process cap once the line saying hosted runners are on is logged", reads)
	}
	if d.image != "openv-worker:x10b" || d.network != "x10b-net" || d.apiURL != "http://api.x10b:8080" || d.pidsLimit() != 64 {
		t.Errorf("the provisioner holds image %q, network %q, API URL %q and process cap %d; want the settings it was given",
			d.image, d.network, d.apiURL, d.pidsLimit())
	}
}

// REQ-96 / HAZ-2: a hosted runner container is created with no capabilities,
// no way to gain privilege, a process cap, and its memory/CPU caps. This is
// the whole isolation posture of a container that runs a vendor CLI on
// somebody's prompt, so it is asserted field by field.
func TestHostConfigForHardening(t *testing.T) {
	limits := ResourceLimits{MemoryMB: 2048, NanoCPUs: 1e9}
	cfg := hostConfigFor("openv-runner-org1", limits, defaultPidsLimit)

	if !slices.Contains(cfg.CapDrop, "ALL") {
		t.Errorf("CapDrop = %v, want ALL", cfg.CapDrop)
	}
	if len(cfg.CapAdd) != 0 {
		t.Errorf("CapAdd = %v, want nothing added back", cfg.CapAdd)
	}
	if !slices.Contains(cfg.SecurityOpt, "no-new-privileges:true") {
		t.Errorf("SecurityOpt = %v, want no-new-privileges:true", cfg.SecurityOpt)
	}
	if cfg.Resources.PidsLimit == nil || *cfg.Resources.PidsLimit != defaultPidsLimit {
		t.Errorf("PidsLimit = %v, want %d", cfg.Resources.PidsLimit, defaultPidsLimit)
	}
	if cfg.Resources.Memory != 2048*1024*1024 {
		t.Errorf("Memory = %d, want %d", cfg.Resources.Memory, int64(2048*1024*1024))
	}
	if cfg.Resources.NanoCPUs != 1e9 {
		t.Errorf("NanoCPUs = %d, want 1e9", cfg.Resources.NanoCPUs)
	}
	// The org's data volume is still the only thing mounted — in particular
	// never the docker socket, which would hand the container the host.
	if len(cfg.Binds) != 1 || cfg.Binds[0] != "openv-runner-org1:/data" {
		t.Errorf("Binds = %v, want only the org's data volume", cfg.Binds)
	}
	if cfg.Privileged {
		t.Error("a runner container must never be privileged")
	}
	// ReadonlyRootfs is knowingly off: the vendor CLIs in the runner image
	// write outside /data. If that changes, this test is where to say so.
	if cfg.ReadonlyRootfs {
		t.Error("ReadonlyRootfs is set but the runner image writes outside /data; give those paths tmpfs mounts first")
	}
}

// A cap of zero is docker's "no limit", and is what an operator gets by
// deliberately setting HOSTED_RUNNER_PIDS_LIMIT to 0 — so it must not be
// written as a literal zero, which docker would read as "no processes".
func TestHostConfigForNoPidsCap(t *testing.T) {
	cfg := hostConfigFor("v", ResourceLimits{}, 0)
	if cfg.Resources.PidsLimit != nil {
		t.Errorf("PidsLimit = %v, want unset", *cfg.Resources.PidsLimit)
	}
}
