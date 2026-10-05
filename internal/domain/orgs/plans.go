// The plans a workspace can be on: their names, the limits each defaults to,
// the tier values once the tiers are in force, the alpha terms, and the
// largest upload a plan allows. The plan new workspaces are created on is
// the deployment policy's (deployment_policy.go); how a workspace's limits
// resolve is in limits.go.

package orgs

import "slices"

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

// unlimited is every count limit's shipped value.
//
// The count limits ship OPEN on every plan, deliberately. The pricing page
// promises that during alpha "every workspace has every tier's features,
// free", and turning a cap on retroactively would break workspaces that are
// already over it. Shipping the mechanism at zero means it can be proven
// before it ever refuses anybody, and launching a tier is then a
// configuration change rather than a deployment.
const unlimited = 0

// PlanDefaults returns the default limits for a billing plan under the
// package default policy (DeploymentPolicy.PlanDefaults).
func PlanDefaults(plan string) map[string]interface{} {
	return defaultPolicy.PlanDefaults(plan)
}

// PlanDefaults returns the default limits for a billing plan, with the tier
// values laid over them when p enforces the tiers.
//
// An unknown or empty plan gets the most restrictive hosted defaults, so a bad
// plan value can never accidentally mean "unlimited".
func (p *DeploymentPolicy) PlanDefaults(plan string) map[string]interface{} {
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
		return p.tiered(plan, map[string]interface{}{
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
		return p.tiered(plan, map[string]interface{}{
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
		return p.tiered(plan, map[string]interface{}{
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

// FreeHostedRunnerMinutes is the free tier's monthly cloud-runner allowance
// once the tiers are in force.
const FreeHostedRunnerMinutes = 300

// tiered lays the tier's counts and flags over a plan's alpha defaults when
// p enforces the tiers. The paid tiers pay for the company layer, not the
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
func (p *DeploymentPolicy) tiered(plan string, m map[string]interface{}) map[string]interface{} {
	if !p.TiersEnforced {
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

// allPlans is every plan a workspace can be on, for the derived readings
// below. It is the same list ValidPlan accepts.
var allPlans = []string{
	PlanSingle, PlanBusinessLite, PlanBusiness, PlanEnterprise,
	PlanSelfHost, PlanOpenSource, PlanFree, PlanTeam,
}

// MaxPlanUploadMB is DeploymentPolicy.MaxPlanUploadMB under the package
// default policy.
func MaxPlanUploadMB() int { return defaultPolicy.MaxPlanUploadMB() }

// MaxPlanUploadMB is the largest per-file upload any CAPPED plan allows, or
// the deployment layer's max_upload_mb where that is larger, or 0 where no
// plan on this deployment has a ceiling.
//
// It is derived from PlanDefaults and the deployment layer rather than written
// down a second time, so raising a tier's ceiling, or the deployment's, cannot
// leave a bound elsewhere quietly refusing what the tier now permits. An upload
// handler uses it to bound a request BEFORE it knows which workspace the upload
// is for: parsing a multipart body spools every part to disk, so something has
// to say how much disk one request may take while the answer is still unknown.
// What the bytes are finally measured against is the workspace's own limit.
//
// The deployment layer (OPENV_LIMITS) sits above every plan, so a
// max_upload_mb there above the plans' largest is what every workspace without
// one of its own resolves, and the bound takes it: otherwise such a workspace
// is refused at the request with the plans' smaller figure (#379, bug 191). A
// figure there below the plans' largest leaves that in charge; the layer only
// ever lifts the bound.
//
// The uncapped plans (self-host, enterprise) are skipped rather than collapsing
// the answer to "no bound", and so is a 0 in the deployment layer. A plan with
// no ceiling means OpenV rations nothing, not that one HTTP request may be any
// size at all; a deployment that is itself self-hosted reports 0, whatever its
// deployment layer says, and leaves the bound to the operator. The bound there
// is already the transport ceiling, which the layer could not lift, and taking
// the layer's figure would lower it, refusing at the request a workspace the
// operator raised on its own.
func (p *DeploymentPolicy) MaxPlanUploadMB() int {
	if p.SelfHosted {
		return 0
	}
	largest := 0
	for _, plan := range allPlans {
		if mb, ok := LimitInt(p.PlanDefaults(plan), LimitMaxUploadMB); ok && mb > largest {
			largest = mb
		}
	}
	if mb, ok := LimitInt(p.DeploymentLimits, LimitMaxUploadMB); ok && mb > largest {
		largest = mb
	}
	return largest
}

// PlanNames is every plan ValidPlan accepts, in the order a list of them
// is shown: the tiers, the self-host plan, the open-source plan and the
// two legacy aliases.
func PlanNames() []string { return slices.Clone(allPlans) }

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
