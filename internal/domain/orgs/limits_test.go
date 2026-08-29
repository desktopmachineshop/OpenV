package orgs

import (
	"encoding/json"
	"testing"
)

// TestPlanDefaultsPerPlan: each known plan has its own caps, and anything
// unrecognized (including empty) falls back to the free plan — never to
// "unlimited".
func TestPlanDefaultsPerPlan(t *testing.T) {
	cases := []struct {
		plan    string
		wantMem float64
		wantCPU float64
	}{
		{PlanFree, 2048, 1},
		{PlanTeam, 4096, 2},
		{"", 2048, 1},
		{"enterprise-nonsense", 2048, 1},
	}
	for _, tc := range cases {
		d := PlanDefaults(tc.plan)
		mem, ok := LimitFloat(d, LimitRunnerMemoryMB)
		if !ok || mem != tc.wantMem {
			t.Errorf("PlanDefaults(%q) memory = %v ok=%v, want %v", tc.plan, mem, ok, tc.wantMem)
		}
		cpu, ok := LimitFloat(d, LimitRunnerCPUs)
		if !ok || cpu != tc.wantCPU {
			t.Errorf("PlanDefaults(%q) cpus = %v ok=%v, want %v", tc.plan, cpu, ok, tc.wantCPU)
		}
	}
}

// TestEffectiveLimitsMerge: explicit org limits win key-by-key; absent keys
// fall back to the plan defaults; unrelated keys pass through.
func TestEffectiveLimitsMerge(t *testing.T) {
	org := &Org{
		Plan: PlanFree,
		Limits: map[string]interface{}{
			LimitRunnerMemoryMB: float64(512), // as JSON unmarshal delivers it
			"custom_key":        "hello",
		},
	}
	eff := org.EffectiveLimits()

	if mem, _ := LimitFloat(eff, LimitRunnerMemoryMB); mem != 512 {
		t.Errorf("explicit memory limit = %v, want 512 (org override must win)", mem)
	}
	if cpu, _ := LimitFloat(eff, LimitRunnerCPUs); cpu != 1 {
		t.Errorf("cpus = %v, want free-plan default 1", cpu)
	}
	if eff["custom_key"] != "hello" {
		t.Errorf("unrelated key lost in merge: %v", eff["custom_key"])
	}
	// The merge must not write plan defaults back into the org's own map.
	if _, ok := org.Limits[LimitRunnerCPUs]; ok {
		t.Error("EffectiveLimits mutated org.Limits")
	}
}

// TestEffectiveLimitsEmpty: an org with no explicit limits gets exactly its
// plan's defaults.
func TestEffectiveLimitsEmpty(t *testing.T) {
	org := &Org{Plan: PlanTeam, Limits: map[string]interface{}{}}
	eff := org.EffectiveLimits()
	if mem, _ := LimitFloat(eff, LimitRunnerMemoryMB); mem != 4096 {
		t.Errorf("memory = %v, want team default 4096", mem)
	}
	if cpu, _ := LimitFloat(eff, LimitRunnerCPUs); cpu != 2 {
		t.Errorf("cpus = %v, want team default 2", cpu)
	}
}

// TestLimitFloatCoercions: every numeric shape that can reach a limits map
// reads back; non-numbers and absent keys report !ok.
func TestLimitFloatCoercions(t *testing.T) {
	m := map[string]interface{}{
		"f":       float64(1.5),
		"i":       int(7),
		"i64":     int64(9),
		"num":     json.Number("3.25"),
		"bad_num": json.Number("not-a-number"),
		"str":     "2048",
		"nil":     nil,
	}
	want := map[string]struct {
		v  float64
		ok bool
	}{
		"f":       {1.5, true},
		"i":       {7, true},
		"i64":     {9, true},
		"num":     {3.25, true},
		"bad_num": {0, false},
		"str":     {0, false},
		"nil":     {0, false},
		"absent":  {0, false},
	}
	for key, w := range want {
		v, ok := LimitFloat(m, key)
		if v != w.v || ok != w.ok {
			t.Errorf("LimitFloat(%q) = (%v, %v), want (%v, %v)", key, v, ok, w.v, w.ok)
		}
	}
}
