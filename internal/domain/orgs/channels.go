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

// ChannelOverrideAfterMove is the channel override a workspace holds once
// its plan moves from one plan to another: the one it holds, unless it holds
// none and the move takes it from a plan that always runs nightly onto one
// whose admins choose, when it is nightly. Without that the new plan's
// default would put the workspace on the stable channel with no stable
// release turned on, and every gated feature it was using would close at
// once. Every plan move takes this rule, a checkout's and the billing sync's
// (Repository.ApplyBillingState) as a platform admin's (SetPlan); the
// store applies it in the UPDATE that writes the plan, which sees the row's
// old plan, and the service to the workspace it answers.
func ChannelOverrideAfterMove(from, to, override string) string {
	if override == "" && !ChannelChoosable(from) && ChannelChoosable(to) {
		return ChannelNightly
	}
	return override
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

// CheckReleaseChannel is SetReleaseChannel's refusal without its write, so
// that a caller can refuse a request before writing any part of it:
// ErrChannelLocked for a plan that always runs nightly, ErrInvalidChannel
// for a name that is no channel ("" is the plan's default).
func CheckReleaseChannel(plan, channel string) error {
	if !ChannelChoosable(plan) {
		return ErrChannelLocked
	}
	if channel != "" && !ValidChannel(channel) {
		return ErrInvalidChannel
	}
	return nil
}

// CheckUpgradeWindow is SetUpgradeWindow's refusal without its write:
// ErrChannelLocked for a plan that cannot choose, ErrInvalidWindow for bad
// values.
func CheckUpgradeWindow(plan string, day, hour int, timezone string) error {
	if !ChannelChoosable(plan) {
		return ErrChannelLocked
	}
	return ValidateUpgradeWindow(day, hour, timezone)
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
	if o.ReleaseChannelOverride != "" && ChannelChoosable(o.BilledPlan) {
		o.ReleaseChannel = o.ReleaseChannelOverride
	} else {
		o.ReleaseChannel = ChannelForPlan(o.BilledPlan)
	}
	o.ReleaseChannelLocked = !ChannelChoosable(o.BilledPlan)
}

// ChannelSettings is the slice of Service for release channels: a
// workspace's channel, upgrade window and stable release, the workspaces and
// accounts on a channel, and a member's early switch to the next stable
// release.
type ChannelSettings interface {
	// SetReleaseChannel records the channel a company workspace's admin
	// chose ("" returns it to the plan's default) and returns the updated
	// workspace. ErrChannelLocked for a plan that always runs nightly,
	// ErrInvalidChannel for an unknown name.
	SetReleaseChannel(id, channel string) (*Org, error)
	// SetUpgradeWindow records when stable releases turn on for a company
	// workspace: day of month 1-28 and hour 0-23 in an IANA time zone; day
	// 0 clears the window so releases turn on at the cut. ErrChannelLocked
	// for a plan that cannot choose, ErrInvalidWindow for bad values.
	SetUpgradeWindow(id string, day, hour int, timezone string) (*Org, error)
	// SetStableRelease records the stable release now turned on for a
	// workspace (used by the release scheduler).
	SetStableRelease(id, version string) (*Org, error)
	// ListOrgsByChannel lists live workspaces on a channel.
	ListOrgsByChannel(channel string) ([]*Org, error)
	// ListMemberUserIDsByChannel lists accounts with a workspace on channel.
	ListMemberUserIDsByChannel(channel string) ([]string, error)
	// MemberPreview and SetMemberPreview read and write a member's own
	// early switch to the next stable release in one workspace
	// (ErrNotMember for an account that is not a member).
	MemberPreview(orgID, userID string) (bool, error)
	SetMemberPreview(orgID, userID string, enabled bool) error
}

// SetReleaseChannel implements Service.
func (s *DefaultService) SetReleaseChannel(id, channel string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := CheckReleaseChannel(org.BilledPlan, channel); err != nil {
		return nil, err
	}
	if err := s.repo.SetReleaseChannel(id, channel); err != nil {
		return nil, err
	}
	org.ReleaseChannelOverride = channel
	org.UpdatedAt = time.Now()
	org.ResolveReleaseChannel()
	return org, nil
}

// SetUpgradeWindow implements Service.
func (s *DefaultService) SetUpgradeWindow(id string, day, hour int, timezone string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := CheckUpgradeWindow(org.BilledPlan, day, hour, timezone); err != nil {
		return nil, err
	}
	if day == 0 {
		hour, timezone = 0, ""
	}
	if err := s.repo.SetUpgradeWindow(id, day, hour, timezone); err != nil {
		return nil, err
	}
	org.UpgradeDay, org.UpgradeHour, org.UpgradeTimezone = day, hour, timezone
	org.UpdatedAt = time.Now()
	return org, nil
}

// SetStableRelease implements Service.
func (s *DefaultService) SetStableRelease(id, version string) (*Org, error) {
	if err := s.repo.SetStableRelease(id, version); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// ListOrgsByChannel implements Service.
func (s *DefaultService) ListOrgsByChannel(channel string) ([]*Org, error) {
	return s.repo.ListOrgsByChannel(channel)
}

// ListMemberUserIDsByChannel implements Service.
func (s *DefaultService) ListMemberUserIDsByChannel(channel string) ([]string, error) {
	return s.repo.ListMemberUserIDsByChannel(channel)
}

// MemberPreview implements Service.
func (s *DefaultService) MemberPreview(orgID, userID string) (bool, error) {
	return s.repo.MemberPreview(orgID, userID)
}

// SetMemberPreview implements Service.
func (s *DefaultService) SetMemberPreview(orgID, userID string, enabled bool) error {
	return s.repo.SetMemberPreview(orgID, userID, enabled)
}
