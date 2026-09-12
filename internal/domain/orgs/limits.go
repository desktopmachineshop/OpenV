package orgs

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Workspace limits.
//
// Two kinds of limit live here, and they exist for different reasons.
//
// RESOURCE limits (runner memory, CPU, lease minutes, evidence storage) cost
// the operator real money and protect a shared machine. They are not a selling
// device; an uncapped one is an availability hazard.
//
// COUNT limits (members, shared workspaces, projects) cost almost nothing to
// serve — a project is a row with artifacts hanging off it. They exist to draw
// the line between what the hosted tiers offer, and they are deliberately
// drawn around PEOPLE rather than around work: capping projects would punish
// somebody for using a requirements tool the way it is meant to be used, and
// fragmenting a product's traceability is the opposite of what OpenV is for.
// max_projects is therefore an abuse ceiling, not a product gate.
//
// ZERO MEANS UNLIMITED, uniformly, for every key. A limit that is absent falls
// back through the layers below; a limit explicitly set to 0 is the operator
// saying "no ceiling". Self-hosted deployments run entirely on zeroes.
//
// Three layers resolve a value, most specific first:
//
//	org.Limits  >  deployment defaults (OPENV_LIMITS)  >  PlanDefaults(plan)
//
// The middle layer is what makes self-hosting honest: one environment variable
// retunes the whole deployment without touching the database, and an operator
// can still pin one workspace tighter or looser than the rest.

// Billing plans. Plan gates default resource limits (PlanDefaults); it is set
// at org creation from the deployment's configured default and has no
// self-serve upgrade path yet.
//
// The names track the tiers on the pricing page so that launching a tier is a
// config change rather than a migration. PlanFree and PlanTeam are the two
// names that shipped first and are kept as aliases: rows in the wild carry
// them, and they must keep resolving to something sensible forever.
const (
	// PlanSingle is the free hosted tier: one person, their own workspace.
	PlanSingle = "single"
	// PlanBusinessLite is one person with hosted agents running unattended.
	PlanBusinessLite = "business_lite"
	// PlanBusiness is shared company workspaces with teams.
	PlanBusiness = "business"
	// PlanEnterprise is negotiated; nothing is capped by default.
	PlanEnterprise = "enterprise"
	// PlanSelfHost is the default for a deployment somebody runs themselves:
	// every limit unlimited, because the hardware is theirs.
	PlanSelfHost = "self_host"

	// PlanFree is the original name for what is now PlanSingle.
	PlanFree = "free"
	// PlanTeam is the original name for what is now PlanBusiness.
	PlanTeam = "team"
)

// Limit keys understood by the platform. Org.Limits is a free-form JSON map
// (the `limits` JSONB column); these constants name the keys the platform
// reads.
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
	// LimitEvidenceStorageMB caps the total size of the workspace's uploaded
	// test evidence, in MiB. A workspace total rather than a per-project one
	// because the disk it protects is a single shared volume.
	LimitEvidenceStorageMB = "evidence_storage_mb"

	// LimitMaxMembers caps the people in one workspace. Pending invitations
	// count: an admin who could issue fifty invitations against five seats
	// would otherwise hand the confusing failure to the sixth person to
	// accept rather than to the admin who caused it.
	LimitMaxMembers = "max_members"
	// LimitMaxSharedWorkspaces caps how many SHARED workspaces one account
	// may create. A personal workspace is never counted — everybody has
	// exactly one and it is not a purchase.
	LimitMaxSharedWorkspaces = "max_shared_workspaces"
	// LimitMaxProjects caps the projects in one workspace. Set high where it
	// is set at all: this is a ceiling against runaway automation, not a
	// reason to make somebody choose which of their products to track.
	LimitMaxProjects = "max_projects"
)

// Unit describes how a limit's number should be read, so one catalogue can
// drive the docs, the API and the settings panel without any of them
// hard-coding a format.
type Unit string

const (
	UnitCount   Unit = "count"
	UnitMB      Unit = "mb"
	UnitMinutes Unit = "minutes"
	UnitCPUs    Unit = "cpus"
	UnitUnknown Unit = ""
)

// Definition is one limit's entry in the catalogue: what it is called, what it
// means, and how to read its number.
type Definition struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Description is written for the person who hits the limit, not for the
	// operator who sets it.
	Description string `json:"description"`
	Unit        Unit   `json:"unit"`
	// Countable marks a limit whose usage can be measured and shown beside
	// it ("3 of 10 used"). A runner's memory ceiling has no such reading.
	Countable bool `json:"countable"`
}

// catalog is the single source of truth for what limits exist. Docs, the
// limits endpoint and the settings panel all render from it, so a new limit
// cannot be added and left undocumented.
var catalog = []Definition{
	{
		Key: LimitMaxMembers, Label: "Workspace members", Unit: UnitCount, Countable: true,
		Description: "How many people can be in this workspace. Pending invitations count towards it.",
	},
	{
		Key: LimitMaxProjects, Label: "Projects", Unit: UnitCount, Countable: true,
		Description: "How many projects this workspace can hold.",
	},
	{
		Key: LimitMaxSharedWorkspaces, Label: "Shared workspaces", Unit: UnitCount, Countable: true,
		Description: "How many shared workspaces you can create. Your personal workspace is never counted.",
	},
	{
		Key: LimitEvidenceStorageMB, Label: "Test evidence storage", Unit: UnitMB, Countable: true,
		Description: "Total size of the test evidence files this workspace has uploaded.",
	},
	{
		Key: LimitRunnerSessionMinutes, Label: "Cloud runner lease", Unit: UnitMinutes,
		Description: "How long a leased cloud runner lasts before it is reclaimed.",
	},
	{
		Key: LimitRunnerSessionIdleMinutes, Label: "Cloud runner idle window", Unit: UnitMinutes,
		Description: "How long a leased cloud runner may sit unused before it is reclaimed.",
	},
	{
		Key: LimitRunnerMemoryMB, Label: "Hosted runner memory", Unit: UnitMB,
		Description: "Memory available to this workspace's always-on hosted runner.",
	},
	{
		Key: LimitRunnerCPUs, Label: "Hosted runner CPUs", Unit: UnitCPUs,
		Description: "CPU available to this workspace's always-on hosted runner.",
	},
}

// Catalog returns every limit the platform understands, in the order a person
// should read them.
func Catalog() []Definition {
	out := make([]Definition, len(catalog))
	copy(out, catalog)
	return out
}

// Describe returns one limit's definition, and whether it is a known key.
func Describe(key string) (Definition, bool) {
	for _, d := range catalog {
		if d.Key == key {
			return d, true
		}
	}
	return Definition{}, false
}

// KnownLimitKeys lists every catalogued key, sorted, for validation messages.
func KnownLimitKeys() []string {
	keys := make([]string, 0, len(catalog))
	for _, d := range catalog {
		keys = append(keys, d.Key)
	}
	sort.Strings(keys)
	return keys
}

// unlimited is every count limit's shipped value.
//
// The count limits ship OPEN on every plan, deliberately. The pricing page
// promises that during alpha "every workspace has every tier's features,
// free", and turning a cap on retroactively would break workspaces that are
// already over it. Shipping the mechanism at zero means it can be proven
// before it ever refuses anybody, and launching a tier is then a
// configuration change rather than a deployment.
const unlimited = 0

// PlanDefaults returns the default limits for a billing plan.
//
// An unknown or empty plan gets the most restrictive hosted defaults, so a bad
// plan value can never accidentally mean "unlimited".
func PlanDefaults(plan string) map[string]interface{} {
	switch plan {
	case PlanSelfHost, PlanEnterprise:
		// Somebody else's hardware, or a negotiated agreement. Nothing here
		// is ours to ration.
		return map[string]interface{}{
			LimitRunnerMemoryMB:           unlimited,
			LimitRunnerCPUs:               unlimited,
			LimitRunnerSessionMinutes:     unlimited,
			LimitRunnerSessionIdleMinutes: unlimited,
			LimitEvidenceStorageMB:        unlimited,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
		}
	case PlanBusiness, PlanTeam:
		return map[string]interface{}{
			LimitRunnerMemoryMB:           4096,
			LimitRunnerCPUs:               2.0,
			LimitRunnerSessionMinutes:     120,
			LimitRunnerSessionIdleMinutes: 20,
			LimitEvidenceStorageMB:        20480,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
		}
	case PlanBusinessLite:
		return map[string]interface{}{
			LimitRunnerMemoryMB:           4096,
			LimitRunnerCPUs:               2.0,
			LimitRunnerSessionMinutes:     120,
			LimitRunnerSessionIdleMinutes: 20,
			LimitEvidenceStorageMB:        10240,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
		}
	default: // PlanSingle, PlanFree and anything unrecognized
		return map[string]interface{}{
			LimitRunnerMemoryMB:           2048,
			LimitRunnerCPUs:               1.0,
			LimitRunnerSessionMinutes:     60,
			LimitRunnerSessionIdleMinutes: 15,
			LimitEvidenceStorageMB:        2048,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
		}
	}
}

// defaultPlan is the plan new workspaces are created on. The hosted service
// leaves it at PlanSingle; a self-hosted deployment sets PlanSelfHost, which
// is what makes "no limits from us" true rather than merely advertised.
var defaultPlan = PlanSingle

// SetDefaultPlan installs the plan new workspaces are created on. An unknown
// name is ignored rather than stored, so a typo cannot create workspaces on a
// plan whose defaults nobody has written.
func SetDefaultPlan(plan string) {
	switch plan {
	case PlanSingle, PlanBusinessLite, PlanBusiness, PlanEnterprise, PlanSelfHost, PlanFree, PlanTeam:
		defaultPlan = plan
	}
}

// DefaultPlan is the plan new workspaces are created on.
func DefaultPlan() string { return defaultPlan }

// deploymentLimits is the middle layer: a deployment-wide override read once
// at boot from OPENV_LIMITS. Nil until SetDeploymentLimits is called, which is
// the hosted service's state — it runs on plan defaults alone.
var deploymentLimits map[string]interface{}

// SetDeploymentLimits installs the deployment-wide defaults. Called once at
// boot; a nil or empty map leaves plan defaults in charge.
func SetDeploymentLimits(limits map[string]interface{}) {
	deploymentLimits = limits
}

// DeploymentLimits returns the configured deployment-wide defaults.
func DeploymentLimits() map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range deploymentLimits {
		out[k] = v
	}
	return out
}

// ParseLimits reads a JSON object of limit values, rejecting keys the platform
// does not understand and values that are not numbers. Refusing an unknown key
// is deliberate: a typo in OPENV_LIMITS that silently did nothing would look
// exactly like a limit that does not work.
func ParseLimits(raw string) (map[string]interface{}, error) {
	if raw == "" {
		return nil, nil
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("not a JSON object of limit values: %w", err)
	}
	out := map[string]interface{}{}
	for key, value := range parsed {
		if _, known := Describe(key); !known {
			return nil, fmt.Errorf("unknown limit %q (known limits: %v)", key, KnownLimitKeys())
		}
		number, ok := numeric(value)
		if !ok {
			return nil, fmt.Errorf("limit %q must be a number, got %T", key, value)
		}
		if number < 0 {
			return nil, fmt.Errorf("limit %q must not be negative (0 means unlimited)", key)
		}
		out[key] = number
	}
	return out, nil
}

// EffectiveLimits resolves the three layers for this workspace: the org's own
// limits win, then the deployment's, then the plan's. The receiver's map is
// never mutated.
func (o *Org) EffectiveLimits() map[string]interface{} {
	// On a deployment somebody runs themselves the plan column is meaningless
	// — there is no billing relationship to describe — so the base is
	// PlanSelfHost whatever the row says. Reading the column instead would
	// leave every workspace created BEFORE the operator set OPENV_SELF_HOSTED
	// on the hosted tier's ceilings, which is precisely the population the
	// setting exists for, and "no limits from us" would stay false for them.
	// The layers above still apply, so an operator can cap a workspace.
	plan := o.Plan
	if selfHosted {
		plan = PlanSelfHost
	}
	merged := PlanDefaults(plan)
	for k, v := range deploymentLimits {
		merged[k] = v
	}
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
	return numeric(limits[key])
}

// LimitInt reads a limit as a whole number. The second result is false when
// the key is absent or unusable, which callers treat as "no opinion" rather
// than as zero.
func LimitInt(limits map[string]interface{}, key string) (int, bool) {
	v, ok := LimitFloat(limits, key)
	if !ok {
		return 0, false
	}
	return int(v), true
}

// Ceiling reads a limit as a cap where 0 (or absent) means unlimited. It is
// the one reading every count limit uses, so "unlimited" cannot come to mean
// different things in different handlers.
func Ceiling(limits map[string]interface{}, key string) (cap int, capped bool) {
	v, ok := LimitInt(limits, key)
	if !ok || v <= 0 {
		return 0, false
	}
	return v, true
}

func numeric(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
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
