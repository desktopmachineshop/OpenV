package orgs

import (
	"errors"
	"time"
)

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

// ErrInvalidWindow is answered for an upgrade window outside day 1-28,
// hour 0-23, or with a time zone the platform does not know.
var ErrInvalidWindow = errors.New("upgrade window must be a day of the month from 1 to 28, an hour from 0 to 23, and a known time zone")

// ChoosablePlans lists the plans whose admins pick the channel; persistence
// needs the list to compute the effective channel in SQL.
var ChoosablePlans = []string{PlanBusiness, PlanTeam, PlanEnterprise}

// ValidateUpgradeWindow checks a window: day 0 means none; otherwise day
// 1-28 (so it exists in every month), hour 0-23, and a loadable IANA zone
// (empty means UTC).
func ValidateUpgradeWindow(day, hour int, timezone string) error {
	if day == 0 {
		return nil
	}
	if day < 1 || day > 28 || hour < 0 || hour > 23 {
		return ErrInvalidWindow
	}
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return ErrInvalidWindow
		}
	}
	return nil
}

// UpgradeWindowDays is how long after a stable cut a workspace may wait
// before the release turns on for it, whatever window it chose.
const UpgradeWindowDays = 14

// UpgradeTimeFor is when a stable release cut at cutOn turns on for a
// workspace with the given window: at the cut when there is no window,
// otherwise at the first day-of-month/hour in the zone after the cut, and
// never later than UpgradeWindowDays after it. An unknown zone counts as
// UTC.
func UpgradeTimeFor(cutOn time.Time, day, hour int, timezone string) time.Time {
	if day < 1 {
		return cutOn
	}
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	local := cutOn.In(loc)
	candidate := time.Date(local.Year(), local.Month(), day, hour, 0, 0, 0, loc)
	if !candidate.After(cutOn) {
		candidate = time.Date(local.Year(), local.Month()+1, day, hour, 0, 0, 0, loc)
	}
	if latest := cutOn.Add(UpgradeWindowDays * 24 * time.Hour); candidate.After(latest) {
		return latest
	}
	return candidate
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
