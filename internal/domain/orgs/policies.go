// The plan and channel policies: what a plan's name means on this
// deployment. The built-in ones are the tiers and channels as the hosted
// service sells them today; a module installs its own through ext at boot
// (docs/plans/hosted-edition-separation.md, Phase 1).

package orgs

// PlanPolicy sets each plan's default limits, the bottom layer of a
// workspace's limits (limits.go).
type PlanPolicy interface {
	// PlanDefaults is plan's default limits, with the tier values laid
	// over them when tiers is true (DeploymentPolicy.TiersEnforced). It
	// returns a new map each call, which the caller may change, and must
	// answer an unknown or empty plan with restrictive defaults, never
	// unlimited ones.
	PlanDefaults(plan string, tiers bool) map[string]interface{}
}

// ChannelPolicy sets a plan's release channel (REQ-135, REQ-136).
type ChannelPolicy interface {
	// ChannelForPlan is the channel a plan is on unless the workspace
	// chose otherwise: ChannelNightly or ChannelStable.
	ChannelForPlan(plan string) string
	// ChannelChoosable reports whether a plan's admins may pick the
	// channel.
	ChannelChoosable(plan string) bool
	// ChoosablePlans lists exactly the plans ChannelChoosable accepts.
	ChoosablePlans() []string
}

// BuiltinPlans is the plan policy a deployment has unless a module installs
// another: the tiers in plans.go.
type BuiltinPlans struct{}

// BuiltinChannels is the channel policy a deployment has unless a module
// installs another: company plans choose and default to stable, every other
// plan runs nightly.
type BuiltinChannels struct{}

var (
	_ PlanPolicy    = BuiltinPlans{}
	_ ChannelPolicy = BuiltinChannels{}
)

func (p *DeploymentPolicy) plans() PlanPolicy {
	if p.Plans != nil {
		return p.Plans
	}
	return BuiltinPlans{}
}

func (p *DeploymentPolicy) channels() ChannelPolicy {
	if p.Channels != nil {
		return p.Channels
	}
	return BuiltinChannels{}
}

// SetPlanPolicy installs the deployment's plan policy; nil restores the
// built-in one. Called at boot, before the server serves.
func SetPlanPolicy(p PlanPolicy) { defaultPolicy.Plans = p }

// SetChannelPolicy installs the deployment's channel policy; nil restores
// the built-in one. Called at boot, before the server serves.
func SetChannelPolicy(c ChannelPolicy) { defaultPolicy.Channels = c }
