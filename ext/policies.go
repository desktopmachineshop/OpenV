package ext

import "github.com/openv/requirements-platform/internal/domain/orgs"

// PlanPolicy sets each plan's default limits: the bottom layer of a
// workspace's limits, under the workspace's own and the deployment's
// OPENV_LIMITS.
type PlanPolicy = orgs.PlanPolicy

// ChannelPolicy sets each plan's release channel and whether its admins may
// choose another.
type ChannelPolicy = orgs.ChannelPolicy

// BuiltinPlans and BuiltinChannels are the policies a deployment has when
// no module sets one; a module's policy can defer to them for the plans it
// leaves alone.
type (
	BuiltinPlans    = orgs.BuiltinPlans
	BuiltinChannels = orgs.BuiltinChannels
)

// The release channels.
const (
	ChannelNightly = orgs.ChannelNightly
	ChannelStable  = orgs.ChannelStable
)

// The limit keys a PlanPolicy's defaults hold.
const (
	LimitRunnerMemoryMB           = orgs.LimitRunnerMemoryMB
	LimitRunnerCPUs               = orgs.LimitRunnerCPUs
	LimitRunnerSessionMinutes     = orgs.LimitRunnerSessionMinutes
	LimitRunnerSessionIdleMinutes = orgs.LimitRunnerSessionIdleMinutes
	LimitEvidenceStorageMB        = orgs.LimitEvidenceStorageMB
	LimitMaxUploadMB              = orgs.LimitMaxUploadMB
	LimitMaxMembers               = orgs.LimitMaxMembers
	LimitMaxSharedWorkspaces      = orgs.LimitMaxSharedWorkspaces
	LimitMaxProjects              = orgs.LimitMaxProjects
	LimitHostedRunnerMinutesMonth = orgs.LimitHostedRunnerMinutesMonth
	LimitHostedAutomation         = orgs.LimitHostedAutomation
	LimitTeams                    = orgs.LimitTeams
	LimitWorkspaceBudget          = orgs.LimitWorkspaceBudget
)

// PolicyModule sets what a plan means on this deployment. The server
// installs a module's policies when it builds the modules, before it
// serves; a nil policy leaves the built-in one. At most one module may set
// each.
type PolicyModule interface {
	Module
	PlanPolicy() PlanPolicy
	ChannelPolicy() ChannelPolicy
}
