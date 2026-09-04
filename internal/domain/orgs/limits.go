package orgs

import "encoding/json"

// Billing plans. Plan gates default resource limits (PlanDefaults); it is set
// to PlanFree at org creation and has no self-serve upgrade path yet.
const (
	PlanFree = "free"
	PlanTeam = "team"
)

// Limit keys understood by the platform. Org.Limits is a free-form JSON map
// (the `limits` JSONB column); these constants name the keys the hosted-runner
// provisioner reads. There is no API for editing Limits yet — operators set
// them directly in the database; anything an org leaves unset falls back to
// its plan's defaults (see EffectiveLimits).
const (
	// LimitRunnerMemoryMB caps the org's hosted runner container memory, in
	// MiB (mapped to docker HostConfig.Resources.Memory).
	LimitRunnerMemoryMB = "runner_memory_mb"
	// LimitRunnerCPUs caps the org's hosted runner container CPU, in CPUs
	// (fractions allowed; mapped to docker NanoCPUs).
	LimitRunnerCPUs = "runner_cpus"
	// LimitRunnerSessionMinutes is the maximum lifetime of a transient
	// runner lease, in minutes.
	LimitRunnerSessionMinutes = "runner_session_minutes"
	// LimitRunnerSessionIdleMinutes is how long a transient runner lease may
	// go without run activity before it is reclaimed, in minutes.
	LimitRunnerSessionIdleMinutes = "runner_session_idle_minutes"
)

// PlanDefaults returns the default limits for a billing plan. The values are
// placeholders until plans are productized; an unknown or empty plan gets the
// free plan's defaults, so a bad plan value can never mean "unlimited".
func PlanDefaults(plan string) map[string]interface{} {
	switch plan {
	case PlanTeam:
		return map[string]interface{}{
			LimitRunnerMemoryMB:           4096,
			LimitRunnerCPUs:               2.0,
			LimitRunnerSessionMinutes:     120,
			LimitRunnerSessionIdleMinutes: 20,
		}
	default: // PlanFree and anything unrecognized
		return map[string]interface{}{
			LimitRunnerMemoryMB:           2048,
			LimitRunnerCPUs:               1.0,
			LimitRunnerSessionMinutes:     60,
			LimitRunnerSessionIdleMinutes: 15,
		}
	}
}

// EffectiveLimits merges the org's explicit Limits over its plan's defaults:
// a key present in Limits wins; keys the org leaves absent fall back to
// PlanDefaults(o.Plan). The receiver's map is never mutated.
func (o *Org) EffectiveLimits() map[string]interface{} {
	merged := PlanDefaults(o.Plan)
	for k, v := range o.Limits {
		merged[k] = v
	}
	return merged
}

// LimitFloat reads a numeric limit from a limits map, tolerating the numeric
// types that reach it in practice: float64 (JSON unmarshal), int/int64
// (defaults built in Go), and json.Number. Returns (0, false) when the key is
// absent or not numeric.
func LimitFloat(limits map[string]interface{}, key string) (float64, bool) {
	switch v := limits[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
