package orgs

import (
	"errors"
	"time"
)

// Org types.
const (
	TypePersonal = "personal"
	TypeCompany  = "company"
)

var (
	ErrNotFound = errors.New("organization not found")
	// ErrInvalidPlan is a plan name PlanDefaults knows nothing about.
	ErrInvalidPlan = errors.New("unknown plan")
	ErrNotMember   = errors.New("you are not a member of this organization")

	// ErrInvalidRole flags an unknown role name in a membership write. API
	// handlers use it to tell user-facing validation failures (400) apart
	// from repository failures (500), so wrap it with %w when adding new
	// validations.
	ErrInvalidRole = errors.New("invalid org role")

	// ErrPersonalOrgMembers flags an attempt to add members to a personal
	// workspace — user-facing validation, like ErrInvalidRole.
	ErrPersonalOrgMembers = errors.New("personal workspaces cannot have additional members. " + PersonalWorkspaceRemedy)

	// ErrInvalidBudget flags a negative monthly budget — user-facing
	// validation (400), like ErrInvalidRole.
	ErrInvalidBudget = errors.New("monthly budget must not be negative")

	// ErrPersonalOrgDelete flags an attempt to delete a personal workspace —
	// user-facing validation, like ErrInvalidRole.
	ErrPersonalOrgDelete = errors.New("personal workspaces cannot be deleted")

	// ErrNotDeleted flags a restore of a workspace that is not deleted —
	// user-facing validation, like ErrInvalidRole.
	ErrNotDeleted = errors.New("workspace is not deleted")

	// ErrLastAdmin flags demoting a workspace's only admin (400).
	ErrLastAdmin = errors.New("cannot demote the last admin of an organization")
)

// Org is a tenant: a personal space or a company workspace.
type Org struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	OrgType string `json:"type"`
	// BilledPlan is the plan the workspace is on commercially: what a
	// platform admin granted or what a subscription bought. Never read it to
	// decide what a workspace may do — read EffectiveLimits(), which resolves
	// the plan through Billing.Status. A past-due workspace's BilledPlan is
	// business and it is entitled to it; a canceled workspace's BilledPlan is
	// also business and it is not.
	BilledPlan string                 `json:"plan"`
	Limits     map[string]interface{} `json:"limits"`
	// Billing is the mirrored subscription snapshot: never money, only what
	// decides entitlement and what the Billing tab shows. Writable only by
	// the billing sync path and the platform-admin plan endpoint.
	Billing   Billing   `json:"billing"`
	CreatedBy *string   `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// DeletedAt marks a soft-deleted workspace: hidden and locked, restorable
	// until the purge job hard-deletes it DeletionGraceDays after this time.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	// MonthlyBudgetUSD is the workspace's monthly spend budget (issue #186).
	// nil means no budget is set — the default, which leaves budget alerting
	// and any soft-block disabled. Editable by org admins only.
	MonthlyBudgetUSD *float64 `json:"monthly_budget_usd,omitempty"`
	// BudgetAlertMonth (YYYY-MM, UTC) and BudgetAlertThreshold (0/80/100) are
	// the dedupe state the budget-alert subscriber claims atomically: the last
	// month an alert fired and the highest percent threshold already alerted
	// that month. Surfaced so the UI can show the current alert state.
	BudgetAlertMonth     string `json:"budget_alert_month,omitempty"`
	BudgetAlertThreshold int    `json:"budget_alert_threshold,omitempty"`

	// LogoPath and LogoMime locate the workspace logo on disk (under the
	// uploads directory) and record its image type; both are empty when no
	// logo has been uploaded. Never serialised — clients learn only HasLogo
	// and fetch the bytes through the logo endpoint.
	LogoPath string `json:"-"`
	LogoMime string `json:"-"`
	// HasLogo reports whether a logo is stored (LogoPath != "").
	HasLogo bool `json:"has_logo"`

	// ReleaseChannelOverride is the channel an admin chose, stored; empty
	// means the plan's default. Never serialised: clients see the effective
	// ReleaseChannel and whether the plan lets them change it.
	ReleaseChannelOverride string `json:"-"`
	ReleaseChannel         string `json:"release_channel"`
	ReleaseChannelLocked   bool   `json:"release_channel_locked"`

	// StableRelease is the stable release currently turned on for a
	// stable-channel workspace (2026.09); empty until the first stable is
	// cut and turns on. Gated features are resolved against it (REQ-137).
	StableRelease string `json:"stable_release"`
	// UpgradeDay (1-28, 0 = none) and UpgradeHour (0-23) in UpgradeTimezone
	// (IANA name, empty = UTC) are the workspace's upgrade window: when a
	// new stable release turns on for it, at most 14 days after the cut
	// (REQ-138). Without a window a stable release turns on at the cut.
	UpgradeDay      int    `json:"upgrade_day"`
	UpgradeHour     int    `json:"upgrade_hour"`
	UpgradeTimezone string `json:"upgrade_timezone"`

	// Role is the requesting user's role, populated by ListForUser.
	Role string `json:"role,omitempty"`
}

// Repository defines persistence for orgs and memberships.
// Find methods return (nil, nil) when no row matches.
type Repository interface {
	SaveOrg(o *Org) error
	UpdateOrg(o *Org) error
	// SetBudget writes only monthly_budget_usd (nil clears it), leaving the
	// alert-dedupe columns untouched so it never races the alert claim.
	SetBudget(orgID string, budget *float64) error
	// SetLogo writes only logo_path and logo_mime (empty strings clear them).
	SetLogo(id, path, mime string) error
	// SetReleaseChannel writes only release_channel ("" returns the
	// workspace to its plan's default).
	SetReleaseChannel(id, channel string) error
	// SetPlan writes plan, and the override ChannelOverrideAfterMove calls for.
	SetPlan(id, plan string) error
	// SetStableRelease writes only stable_release: the stable release now
	// turned on for the workspace.
	SetStableRelease(id, version string) error
	// SetUpgradeWindow writes only the upgrade window columns (day 0 clears).
	SetUpgradeWindow(id string, day, hour int, timezone string) error
	// ListOrgsByChannel lists the live workspaces whose effective release
	// channel is channel (plan default unless overridden on a company plan).
	ListOrgsByChannel(channel string) ([]*Org, error)
	// ListMemberUserIDsByChannel lists every account that belongs to at
	// least one live workspace on channel.
	ListMemberUserIDsByChannel(channel string) ([]string, error)
	// MemberPreview reports whether a member turned the next stable release
	// on early for their own account in this workspace (REQ-138).
	MemberPreview(orgID, userID string) (bool, error)
	// SetMemberPreview records that choice, on the membership: ErrNotMember
	// when the account holds none.
	SetMemberPreview(orgID, userID string, enabled bool) error
	// ClaimBudgetAlert atomically records that an alert for (month, threshold)
	// is being sent, and reports whether THIS caller won the claim. It writes
	// only when the row's recorded month differs or the new threshold is
	// higher than the recorded one, so an alert fires exactly once per
	// threshold per month even under concurrent finishers or replicas.
	ClaimBudgetAlert(orgID, month string, threshold int) (bool, error)
	// ClaimMinutesAlert is the same dedupe claim for the hosted-minutes
	// allowance (migration 47).
	ClaimMinutesAlert(orgID, month string, threshold int) (bool, error)

	// Billing writers. Each touches only the billing columns it names; the
	// plan column is written only by SetPlan and ApplyBillingState.
	//
	// SetBillingCustomer records the provider's customer and the currency
	// that customer is locked to.
	SetBillingCustomer(orgID, customerRef, currency string) error
	// ApplyBillingState writes one subscription snapshot atomically and
	// reports whether it was applied: a snapshot read earlier than the
	// row's plan_synced_at loses, which is a success, not an error. It
	// never moves a granted plan (enterprise, open_source), and it writes a
	// nightly release-channel override when a workspace first moves from a
	// nightly-only plan to a channel-choosing one, so nothing gated by the
	// stable channel disappears from under a new subscriber.
	ApplyBillingState(orgID string, state BillingState) (bool, error)
	// SetBilledSeats writes only plan_seats.
	SetBilledSeats(orgID string, seats int) error
	// ClearBillingSubscription forgets the subscription and item refs and
	// marks the status canceled, keeping the customer ref for a later
	// checkout.
	ClearBillingSubscription(orgID string) error
	// SetGrandfathered writes only plan_grandfathered.
	SetGrandfathered(orgID string, on bool) error
	// GrandfatherBefore lays the given overrides under the limits of every
	// workspace created before cutoff that is not yet grandfathered — soft-
	// deleted ones included, so a restore comes back to the terms it left
	// under — and marks them. A key the workspace already sets is kept.
	// Returns how many rows it changed; a second run changes none.
	GrandfatherBefore(cutoff time.Time, overrides map[string]interface{}) (int64, error)
	// FindOrgByBillingRef finds the workspace holding a customer or
	// subscription ref (kind BillingRefCustomer / BillingRefSubscription),
	// deleted ones included; nil when none does.
	FindOrgByBillingRef(kind, ref string) (*Org, error)
	// ListBillingOrgs lists workspaces with a subscription ref, least
	// recently synced first, deleted ones included so a lapsed workspace's
	// subscription can still be cancelled.
	ListBillingOrgs(limit int) ([]*Org, error)

	FindOrgByID(id string) (*Org, error)
	ListOrgsForUser(userID string) ([]*Org, error)
	FindPersonalOrgForUser(userID string) (*Org, error)
	// ListAllOrgIDs returns every organization id (boot-time seeding).
	ListAllOrgIDs() ([]string, error)

	// SoftDeleteOrg stamps deleted_at, hiding and locking the workspace.
	SoftDeleteOrg(id string, at time.Time) error
	// RestoreOrg clears deleted_at within the grace period.
	RestoreOrg(id string) error
	// ListDeletedOrgsForUser returns the user's soft-deleted orgs with role.
	ListDeletedOrgsForUser(userID string) ([]*Org, error)
	// ListExpiredDeletedOrgIDs returns orgs soft-deleted before the cutoff.
	ListExpiredDeletedOrgIDs(before time.Time) ([]string, error)
	// PurgeOrg hard-deletes the org and everything it contains.
	PurgeOrg(id string) error

	UpsertMember(orgID, userID, role string) error
	RemoveMember(orgID, userID string) error         // ErrNotMember when there was none
	MemberRole(orgID, userID string) (string, error) // "" when not a member or org deleted
	// MemberRoleAny is MemberRole without the deleted-org exclusion (restore path).
	MemberRoleAny(orgID, userID string) (string, error)
	ListMembers(orgID string) ([]*Member, error)
	CountAdmins(orgID string) (int, error)
}

// Service defines org domain logic.
type Service interface {
	CreateOrg(name, orgType string, createdBy string) (*Org, error)
	// EnsurePersonalOrg returns the user's personal org, creating it if
	// missing. Idempotent; used at signup and during backfill.
	EnsurePersonalOrg(userID, displayName string) (*Org, bool, error)
	Get(id string) (*Org, error)
	ListForUser(userID string) ([]*Org, error)
	// ListAll returns every organization id (trusted boot-time callers only).
	ListAll() ([]string, error)
	UpdateOrg(id string, name *string) (*Org, error)
	// SetReleaseChannel records the channel a company workspace's admin
	// chose ("" returns it to the plan's default) and returns the updated
	// workspace. ErrChannelLocked for a plan that always runs nightly,
	// ErrInvalidChannel for an unknown name.
	SetReleaseChannel(id, channel string) (*Org, error)
	// SetPlan moves a workspace to another plan (REQ-154): a platform
	// operator's act, since a plan decides limits, the release channel's
	// default and, for the open-source plan, what is public. ErrInvalidPlan
	// for a name PlanDefaults does not know. The channel override follows
	// ChannelOverrideAfterMove, so a move off a nightly-only plan keeps nightly.
	// ErrBillingActive for a granted plan while a subscription is live.
	SetPlan(id, plan string) (*Org, error)
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
	// SetMonthlyBudget sets (or clears, with nil) the workspace's monthly
	// spend budget. Rejects a negative amount with ErrInvalidBudget.
	SetMonthlyBudget(id string, budget *float64) (*Org, error)
	// SetLogo records the workspace logo's on-disk path and MIME type and
	// returns the updated org.
	SetLogo(id, path, mime string) (*Org, error)
	// ClearLogo forgets the workspace logo (stores empty path and MIME) and
	// returns the updated org. Removing the file is the caller's job.
	ClearLogo(id string) (*Org, error)
	// ClaimBudgetAlert is the atomic dedupe claim used by the budget-alert
	// subscriber; see Repository.ClaimBudgetAlert.
	ClaimBudgetAlert(orgID, month string, threshold int) (bool, error)
	// ClaimMinutesAlert is the same dedupe claim for the hosted-minutes
	// allowance (migration 47).
	ClaimMinutesAlert(orgID, month string, threshold int) (bool, error)

	// Billing pass-throughs; see Repository.
	SetBillingCustomer(orgID, customerRef, currency string) error
	ApplyBillingState(orgID string, state BillingState) (bool, error)
	SetBilledSeats(orgID string, seats int) error
	ClearBillingSubscription(orgID string) error
	SetGrandfathered(orgID string, on bool) error
	// GrandfatherBefore keeps every workspace created before cutoff on the
	// alpha terms; see Repository.GrandfatherBefore.
	GrandfatherBefore(cutoff time.Time) (int64, error)
	FindOrgByBillingRef(kind, ref string) (*Org, error)
	ListBillingOrgs(limit int) ([]*Org, error)

	// DeleteOrg soft-deletes a company workspace: hidden and locked, restorable
	// for DeletionGraceDays, then hard-deleted by PurgeExpired. Personal
	// workspaces are refused with ErrPersonalOrgDelete. Idempotent.
	DeleteOrg(id string) (*Org, error)
	// RestoreOrg brings a soft-deleted workspace back within the grace
	// period and returns it as restored.
	RestoreOrg(id string) (*Org, error)
	// ListDeletedForUser returns the caller's soft-deleted workspaces.
	ListDeletedForUser(userID string) ([]*Org, error)
	// PurgeExpired hard-deletes workspaces whose grace period has passed,
	// returning the purged ids.
	PurgeExpired(now time.Time) ([]string, error)

	AddMember(orgID, userID, role string) error
	RemoveMember(orgID, userID string) error
	SetMemberRole(orgID, userID, role string) error
	ListMembers(orgID string) ([]*Member, error)
	RoleInOrg(orgID, userID string) (string, error)
	// RoleInOrgAny is RoleInOrg including deleted workspaces (restore path).
	RoleInOrgAny(orgID, userID string) (string, error)
	// IsMember is a convenience wrapper over RoleInOrg.
	IsMember(orgID, userID string) (bool, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates an org service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}
