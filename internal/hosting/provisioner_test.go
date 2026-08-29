package hosting

import (
	"testing"

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
		{orgs.PlanTeam, 4096, 2e9},
		{"mystery-plan", 2048, 1e9},
	}
	for _, tc := range cases {
		rl := ResourceLimitsForOrg(&orgs.Org{Plan: tc.plan, Limits: map[string]interface{}{}})
		if rl.MemoryMB != tc.wantMem || rl.NanoCPUs != tc.wantNano {
			t.Errorf("plan %q: limits = %+v, want mem %d nano %d", tc.plan, rl, tc.wantMem, tc.wantNano)
		}
	}
}

// TestResourceLimitsForOrgOverrides: explicit org limits (as JSON numbers,
// i.e. float64) override the plan defaults, including fractional CPUs.
func TestResourceLimitsForOrgOverrides(t *testing.T) {
	rl := ResourceLimitsForOrg(&orgs.Org{
		Plan: orgs.PlanFree,
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
		Plan: orgs.PlanFree,
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
