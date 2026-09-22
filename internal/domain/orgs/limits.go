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
	// PlanOpenSource is the hosted service free for open-source projects and
	// charities (REQ-151): Business limits, and in return every project's
	// latest baseline is public on the open-source page. Live work stays
	// with the members and whoever they share it with.
	PlanOpenSource = "open_source"

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
	// LimitMaxUploadMB caps ONE uploaded figure — a drawing, a datasheet, a
	// CAD model — in MiB.
	//
	// It is a resource limit, not a product gate: the file lands on the same
	// shared volume as everything else. But 25 MB, the figure the platform
	// shipped with, was chosen when a figure meant a screenshot, and it makes
	// the CAD formats the catalogue accepts useless — a mid-sized assembly
	// clears it on its own (issue #364). Every tier is now measured in
	// hundreds of megabytes, with the paid tiers further up because that is
	// where the storage is paid for.
	LimitMaxUploadMB = "max_upload_mb"

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
	// LimitHostedRunnerMinutesMonth caps the cloud-runner minutes a
	// workspace may lease in a calendar month. It meters platform hardware
	// only: a run on a member's own machine through the Agent Connector
	// is never counted, whatever the plan.
	LimitHostedRunnerMinutesMonth = "hosted_runner_minutes_month"

	// The flags. A flag is a limit whose value is a bool rather than a
	// number: whether a tier includes something, not how much of it. They
	// live in the same catalogue and resolve through the same three layers,
	// so a per-workspace override — the grandfathering mechanism — and the
	// deployment layer cover them exactly as they cover a number.
	//
	// LimitHostedAutomation is unattended agents on platform hardware: the
	// always-on hosted runner and runs claimed by it. Runs on a member's own
	// machine through the Agent Connector are never behind it.
	LimitHostedAutomation = "hosted_automation"
	// LimitTeams is people-teams and per-project access grants.
	LimitTeams = "teams"
	// LimitWorkspaceBudget is the workspace-wide spend rollup and the
	// monthly budget; a member's own runs and costs are always theirs to see.
	LimitWorkspaceBudget = "workspace_budget"
)

// Kind says what shape a limit's value takes. A resource limit and a count
// limit are both numbers where 0 means unlimited; a flag is a bool, and it
// has to be its own kind because a 0/1 number would collide with that
// sentinel — "off" and "unlimited" cannot share a spelling.
type Kind string

const (
	KindResource Kind = "resource"
	KindCount    Kind = "count"
	KindFlag     Kind = "flag"
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
	// Kind is the value's shape; a number unless KindFlag.
	Kind Kind `json:"kind"`
	Unit Unit `json:"unit"`
	// Countable marks a limit whose usage can be measured and shown beside
	// it ("3 of 10 used"). A runner's memory ceiling has no such reading.
	Countable bool `json:"countable"`
}

// catalog is the single source of truth for what limits exist. Docs, the
// limits endpoint and the settings panel all render from it, so a new limit
// cannot be added and left undocumented.
var catalog = []Definition{
	{
		Key: LimitMaxMembers, Label: "Workspace members", Kind: KindCount, Unit: UnitCount, Countable: true,
		Description: "How many people can be in this workspace. Pending invitations count towards it.",
	},
	{
		Key: LimitMaxProjects, Label: "Projects", Kind: KindCount, Unit: UnitCount, Countable: true,
		Description: "How many projects this workspace can hold.",
	},
	{
		Key: LimitMaxSharedWorkspaces, Label: "Shared workspaces", Kind: KindCount, Unit: UnitCount, Countable: true,
		Description: "How many shared workspaces you can create. Your personal workspace is never counted.",
	},
	{
		Key: LimitEvidenceStorageMB, Label: "Test evidence storage", Kind: KindResource, Unit: UnitMB, Countable: true,
		Description: "Total size of the test evidence files this workspace has uploaded.",
	},
	{
		Key: LimitMaxUploadMB, Label: "Largest figure", Kind: KindResource, Unit: UnitMB,
		Description: "The biggest single file you can attach to an artifact — a drawing, a datasheet or a CAD model.",
	},
	{
		Key: LimitHostedRunnerMinutesMonth, Label: "Cloud runner minutes this month", Kind: KindResource, Unit: UnitMinutes, Countable: true,
		Description: "How many minutes of leased cloud runner this workspace can use in a calendar month. Agents on your own machine through the Agent Connector are never counted.",
	},
	{
		Key: LimitRunnerSessionMinutes, Label: "Cloud runner lease", Kind: KindResource, Unit: UnitMinutes,
		Description: "How long a leased cloud runner lasts before it is reclaimed.",
	},
	{
		Key: LimitRunnerSessionIdleMinutes, Label: "Cloud runner idle window", Kind: KindResource, Unit: UnitMinutes,
		Description: "How long a leased cloud runner may sit unused before it is reclaimed.",
	},
	{
		Key: LimitRunnerMemoryMB, Label: "Hosted runner memory", Kind: KindResource, Unit: UnitMB,
		Description: "Memory available to this workspace's always-on hosted runner.",
	},
	{
		Key: LimitRunnerCPUs, Label: "Hosted runner CPUs", Kind: KindResource, Unit: UnitCPUs,
		Description: "CPU available to this workspace's always-on hosted runner.",
	},
	{
		Key: LimitHostedAutomation, Label: "Always-on hosted agents", Kind: KindFlag,
		Description: "An always-on hosted runner on OpenV's hardware, so cron and event automations run while nobody is signed in. Agents on your own machine through the Agent Connector never depend on this.",
	},
	{
		Key: LimitTeams, Label: "Teams and per-project access", Kind: KindFlag,
		Description: "People-teams and per-project access grants, so a company can decide who works on what.",
	},
	{
		Key: LimitWorkspaceBudget, Label: "Workspace AI budget", Kind: KindFlag,
		Description: "A workspace-wide spend rollup and monthly budget. Your own runs and their cost are always yours to see.",
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

// PersonalWorkspaceMembers is how many people a personal workspace holds. It
// is not a plan gate and no tier raises it: a personal workspace is one
// person's by definition, which is why AddMember and the invitation path
// refuse it outright. Stating it as a limit is what makes the settings panel
// honest — "No limit" on a workspace nobody can ever be added to would be a
// lie, and the refusal on the way in reads better with a number behind it.
const PersonalWorkspaceMembers = 1

// PersonalWorkspaceRemedy replaces the usual "upgrade" or "change the setting"
// advice when somebody tries to add a person to a personal workspace. Neither
// remedy applies: no plan and no configuration changes what a personal
// workspace is, so the only useful sentence points at a shared one.
const PersonalWorkspaceRemedy = "A personal workspace is only ever you. " +
	"Create a shared workspace to work with other people."

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
			LimitMaxUploadMB:              unlimited,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
			LimitHostedRunnerMinutesMonth: unlimited,
			LimitHostedAutomation:         true,
			LimitTeams:                    true,
			LimitWorkspaceBudget:          true,
		}
	case PlanBusiness, PlanTeam, PlanOpenSource:
		// Business is sold as a strict superset of Business Lite, and until
		// now its cloud runner was the same machine for the same time (issue
		// #361): every runner number was byte-identical, so the tier bought
		// company features and nothing else. The lease is the figure that
		// matters to somebody running an agent over a real repository, so it
		// is the one that moves — four hours rather than two, on twice the
		// memory, with a longer idle window to match.
		return tiered(plan, map[string]interface{}{
			LimitRunnerMemoryMB:           8192,
			LimitRunnerCPUs:               4.0,
			LimitRunnerSessionMinutes:     240,
			LimitRunnerSessionIdleMinutes: 30,
			LimitEvidenceStorageMB:        20480,
			LimitMaxUploadMB:              1024,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
			LimitHostedRunnerMinutesMonth: unlimited,
			LimitHostedAutomation:         true,
			LimitTeams:                    true,
			LimitWorkspaceBudget:          true,
		})
	case PlanBusinessLite:
		return tiered(plan, map[string]interface{}{
			LimitRunnerMemoryMB:           4096,
			LimitRunnerCPUs:               2.0,
			LimitRunnerSessionMinutes:     120,
			LimitRunnerSessionIdleMinutes: 20,
			LimitEvidenceStorageMB:        10240,
			LimitMaxUploadMB:              512,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
			LimitHostedRunnerMinutesMonth: unlimited,
			LimitHostedAutomation:         true,
			LimitTeams:                    true,
			LimitWorkspaceBudget:          true,
		})
	default: // PlanSingle, PlanFree and anything unrecognized
		return tiered(plan, map[string]interface{}{
			LimitRunnerMemoryMB:           2048,
			LimitRunnerCPUs:               1.0,
			LimitRunnerSessionMinutes:     60,
			LimitRunnerSessionIdleMinutes: 15,
			LimitEvidenceStorageMB:        2048,
			LimitMaxUploadMB:              128,
			LimitMaxMembers:               unlimited,
			LimitMaxSharedWorkspaces:      unlimited,
			LimitMaxProjects:              unlimited,
			LimitHostedRunnerMinutesMonth: unlimited,
			// The flags ship ON for every plan, as the counts ship at
			// zero: the mechanism lands and is proven before any tier
			// turns one off, and turning one off is then a value change.
			LimitHostedAutomation: true,
			LimitTeams:            true,
			LimitWorkspaceBudget:  true,
		})
	}
}

// tiersEnforced is the switch between the alpha terms — every count open,
// every flag on, for everybody — and the tiers as sold. It is thrown by
// OPENV_BILLING_GRANDFATHER_BEFORE: naming the date is what turns the tiers
// on, and the same boot grandfathers every workspace created before it, so
// the two can never be out of step.
var tiersEnforced bool

// SetTiersEnforced turns the tier values on or off. Called once at boot.
func SetTiersEnforced(on bool) { tiersEnforced = on }

// TiersEnforced reports whether the tier values are in force.
func TiersEnforced() bool { return tiersEnforced }

// FreeHostedRunnerMinutes is the free tier's monthly cloud-runner allowance
// once the tiers are in force.
const FreeHostedRunnerMinutes = 300

// tiered lays the tier's counts and flags over a plan's alpha defaults when
// the tiers are in force. The paid tiers pay for the company layer, not the
// product: what moves is seats, shared workspaces, the flags and hosted
// minutes; every product limit stays as it was.
//
//	plan             members  shared  projects  hosted_automation  teams  budget  minutes
//	single / free    2        1       200       off                off    off     300
//	business_lite    2        1       500       on                 off    off     unlimited
//	business & co    0        0       1000      on                 on     on      unlimited
//
// Business keeps members unlimited: nobody caps seats on a plan billed per
// seat. Lite gets the free tier's counts, not fewer. The project caps are
// abuse ceilings, high enough that nobody chooses which product to track.
func tiered(plan string, m map[string]interface{}) map[string]interface{} {
	if !tiersEnforced {
		return m
	}
	switch plan {
	case PlanBusiness, PlanTeam, PlanOpenSource:
		m[LimitMaxProjects] = 1000
	case PlanBusinessLite:
		m[LimitMaxMembers] = 2
		m[LimitMaxSharedWorkspaces] = 1
		m[LimitMaxProjects] = 500
		m[LimitTeams] = false
		m[LimitWorkspaceBudget] = false
	default:
		m[LimitMaxMembers] = 2
		m[LimitMaxSharedWorkspaces] = 1
		m[LimitMaxProjects] = 200
		m[LimitHostedRunnerMinutesMonth] = FreeHostedRunnerMinutes
		m[LimitHostedAutomation] = false
		m[LimitTeams] = false
		m[LimitWorkspaceBudget] = false
	}
	return m
}

// AlphaTerms is the per-workspace override that keeps a grandfathered
// workspace on the alpha terms whatever its plan: every count open, every
// flag on, hosted minutes unmetered. Written into org.Limits, the most
// specific layer, it is the published promise enforced by the same data the
// enforcement reads.
func AlphaTerms() map[string]interface{} {
	return map[string]interface{}{
		LimitMaxMembers:               unlimited,
		LimitMaxSharedWorkspaces:      unlimited,
		LimitMaxProjects:              unlimited,
		LimitHostedRunnerMinutesMonth: unlimited,
		LimitHostedAutomation:         true,
		LimitTeams:                    true,
		LimitWorkspaceBudget:          true,
	}
}

// OverPlan names the count limits a workspace is already past, given the
// current usage per key. A workspace over its plan is read-only (see
// ReadOnlyRemedy) until it upgrades or trims; nothing is ever deleted for
// it. A key with no usage reading is never over.
func OverPlan(limits map[string]interface{}, usage map[string]int) []string {
	var over []string
	for _, def := range catalog {
		if def.Kind != KindCount {
			continue
		}
		used, ok := usage[def.Key]
		if !ok {
			continue
		}
		if allowed, capped := Ceiling(limits, def.Key); capped && used > allowed {
			over = append(over, def.Key)
		}
	}
	return over
}

// ReadOnlyRemedy is what a workspace over its plan is told on every write it
// is refused. Reading and export are never refused, in any state.
const ReadOnlyRemedy = "This workspace has more than its plan allows, so it is read-only until it is " +
	"brought under the plan's limits or moved to a plan that fits. Everything in it stays readable and exportable. " +
	"A workspace admin can subscribe from the Billing tab in workspace settings, remove members or delete projects."

// allPlans is every plan a workspace can be on, for the derived readings
// below. It is the same list ValidPlan accepts.
var allPlans = []string{
	PlanSingle, PlanBusinessLite, PlanBusiness, PlanEnterprise,
	PlanSelfHost, PlanOpenSource, PlanFree, PlanTeam,
}

// MaxPlanUploadMB is the largest per-file upload any CAPPED plan allows, or 0
// where no plan on this deployment has a ceiling.
//
// It is derived from PlanDefaults rather than written down a second time, so
// raising a tier's ceiling cannot leave a bound elsewhere quietly refusing
// what the tier now permits. An upload handler uses it to bound a request
// BEFORE it knows which workspace the upload is for: parsing a multipart body
// spools every part to disk, so something has to say how much disk one request
// may take while the answer is still unknown. What the bytes are finally
// measured against is the workspace's own limit.
//
// The uncapped plans (self-host, enterprise) are skipped rather than collapsing
// the answer to "no bound". A plan with no ceiling means OpenV rations nothing,
// not that one HTTP request may be any size at all; a deployment that is itself
// self-hosted reports 0 and leaves the bound to the operator.
func MaxPlanUploadMB() int {
	if selfHosted {
		return 0
	}
	largest := 0
	for _, plan := range allPlans {
		if mb, ok := LimitInt(PlanDefaults(plan), LimitMaxUploadMB); ok && mb > largest {
			largest = mb
		}
	}
	return largest
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

// ValidPlan reports whether name is a plan a workspace can be put on: the
// tiers, the self-host plan, the open-source plan (REQ-154) and the two
// legacy aliases.
func ValidPlan(name string) bool {
	switch name {
	case PlanSingle, PlanBusinessLite, PlanBusiness, PlanEnterprise, PlanSelfHost, PlanOpenSource, PlanFree, PlanTeam:
		return true
	}
	return false
}

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
		def, known := Describe(key)
		if !known {
			return nil, fmt.Errorf("unknown limit %q (known limits: %v)", key, KnownLimitKeys())
		}
		if def.Kind == KindFlag {
			flag, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("limit %q is a flag and must be true or false, got %T", key, value)
			}
			out[key] = flag
			continue
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
	//
	// Otherwise the base is the ENTITLED plan: the billed plan while the
	// subscription is in good standing, the free tier once it is not. That
	// is the only place billing status touches enforcement, so every check
	// that reads limits follows it without knowing billing exists.
	plan := o.EntitledPlan()
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
	if o.OrgType == TypePersonal {
		// Applied after every layer, including the operator's, because this
		// one is not a ration: a personal workspace with two people in it is
		// not a more generous personal workspace, it is a shared one that
		// nobody can leave or be an admin of. Self-hosting does not change
		// that either — the hardware is theirs, the definition is not.
		merged[LimitMaxMembers] = PersonalWorkspaceMembers
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

// Allowed reads a flag. An absent or unreadable flag is allowed: a limit
// check must never be the reason a legitimate action fails, and every plan's
// defaults name every flag, so absence only happens on a map that was never
// resolved through EffectiveLimits.
func Allowed(limits map[string]interface{}, key string) bool {
	flag, ok := limits[key].(bool)
	if !ok {
		return true
	}
	return flag
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
