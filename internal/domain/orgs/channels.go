package orgs

import "errors"

// Release channels (REQ-135, REQ-136). The shared service runs one build for
// everyone; a workspace's channel decides when user-visible changes turn on
// for it and which releases its members are told about. Personal-tier plans
// ride the nightly channel and cannot change it; company plans default to
// stable and may opt into nightly.
const (
	// ChannelNightly is every promoted release, the day it ships.
	ChannelNightly = "nightly"
	// ChannelStable is the monthly release, turned on at the workspace's
	// upgrade time.
	ChannelStable = "stable"
)

// ErrInvalidChannel names a channel the platform does not have.
var ErrInvalidChannel = errors.New("release channel must be nightly or stable")

// ErrChannelLocked is answered when a plan that always runs nightly asks to
// change channel.
var ErrChannelLocked = errors.New("this plan always runs the nightly channel")

// ChannelForPlan is the channel a plan is on unless the workspace chose
// otherwise: the one-person tiers and self-hosted deployments run nightly
// (a self-hoster decides when to deploy, so nothing is held back from what
// they deployed); company tiers run stable.
func ChannelForPlan(plan string) string {
	switch plan {
	case PlanBusiness, PlanTeam, PlanEnterprise:
		return ChannelStable
	default:
		return ChannelNightly
	}
}

// ChannelChoosable reports whether a plan's admins may pick the channel.
func ChannelChoosable(plan string) bool {
	switch plan {
	case PlanBusiness, PlanTeam, PlanEnterprise:
		return true
	default:
		return false
	}
}

// ValidChannel reports whether name is a channel.
func ValidChannel(name string) bool {
	return name == ChannelNightly || name == ChannelStable
}

// ResolveReleaseChannel fills the serialised channel fields from the stored
// override and the plan. Persistence calls it after a scan and the service
// after a write, so a client always sees the effective channel.
func (o *Org) ResolveReleaseChannel() {
	if o.ReleaseChannelOverride != "" && ChannelChoosable(o.Plan) {
		o.ReleaseChannel = o.ReleaseChannelOverride
	} else {
		o.ReleaseChannel = ChannelForPlan(o.Plan)
	}
	o.ReleaseChannelLocked = !ChannelChoosable(o.Plan)
}
