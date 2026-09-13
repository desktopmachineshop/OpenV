package orgs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Org roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Org types.
const (
	TypePersonal = "personal"
	TypeCompany  = "company"
)

var (
	ErrNotFound  = errors.New("organization not found")
	ErrNotMember = errors.New("you are not a member of this organization")

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
)

// DeletionGraceDays is how long a soft-deleted workspace stays restorable
// before the purge job hard-deletes it and everything it contains.
const DeletionGraceDays = 30

// Org is a tenant: a personal space or a company workspace.
type Org struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Slug      string                 `json:"slug"`
	OrgType   string                 `json:"type"`
	Plan      string                 `json:"plan"`
	Limits    map[string]interface{} `json:"limits"`
	CreatedBy *string                `json:"created_by,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`

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

// Member is a user's membership in an org, denormalized for display.
type Member struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UserName  string    `json:"user_name,omitempty"`
	UserEmail string    `json:"user_email,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
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
	// SetMemberPreview records that choice.
	SetMemberPreview(orgID, userID string, enabled bool) error
	// ClaimBudgetAlert atomically records that an alert for (month, threshold)
	// is being sent, and reports whether THIS caller won the claim. It writes
	// only when the row's recorded month differs or the new threshold is
	// higher than the recorded one, so an alert fires exactly once per
	// threshold per month even under concurrent finishers or replicas.
	ClaimBudgetAlert(orgID, month string, threshold int) (bool, error)
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
	RemoveMember(orgID, userID string) error
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
	// early switch to the next stable release in one workspace.
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

	// DeleteOrg soft-deletes a company workspace: hidden and locked, restorable
	// for DeletionGraceDays, then hard-deleted by PurgeExpired. Personal
	// workspaces are refused with ErrPersonalOrgDelete. Idempotent.
	DeleteOrg(id string) (*Org, error)
	// RestoreOrg brings a soft-deleted workspace back within the grace period.
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

var slugCleaner = regexp.MustCompile(`[^a-z0-9]+`)

func makeSlug(name, id string) string {
	base := strings.Trim(slugCleaner.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "org"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	return base + "-" + strings.ReplaceAll(id, "-", "")[:8]
}

// CreateOrg creates an org and makes the creator its admin.
func (s *DefaultService) CreateOrg(name, orgType string, createdBy string) (*Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("organization name is required")
	}
	if orgType != TypePersonal && orgType != TypeCompany {
		return nil, fmt.Errorf("invalid organization type %q", orgType)
	}
	now := time.Now()
	org := &Org{
		ID:        uuid.New().String(),
		Name:      name,
		OrgType:   orgType,
		Plan:      DefaultPlan(),
		Limits:    map[string]interface{}{},
		CreatedBy: &createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	org.Slug = makeSlug(name, org.ID)
	if err := s.repo.SaveOrg(org); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertMember(org.ID, createdBy, RoleAdmin); err != nil {
		return nil, err
	}
	org.Role = RoleAdmin
	return org, nil
}

// EnsurePersonalOrg returns (org, created, error).
func (s *DefaultService) EnsurePersonalOrg(userID, displayName string) (*Org, bool, error) {
	existing, err := s.repo.FindPersonalOrgForUser(userID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "Personal"
	}
	org, err := s.CreateOrg(name+"'s Space", TypePersonal, userID)
	if err != nil {
		return nil, false, err
	}
	return org, true, nil
}

// Get returns an org by id.
func (s *DefaultService) Get(id string) (*Org, error) {
	org, err := s.repo.FindOrgByID(id)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, ErrNotFound
	}
	return org, nil
}

// ListForUser returns the user's orgs with their role populated.
func (s *DefaultService) ListForUser(userID string) ([]*Org, error) {
	return s.repo.ListOrgsForUser(userID)
}

// ListAll returns every organization id.
func (s *DefaultService) ListAll() ([]string, error) {
	return s.repo.ListAllOrgIDs()
}

// UpdateOrg renames an org.
func (s *DefaultService) UpdateOrg(id string, name *string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		org.Name = strings.TrimSpace(*name)
	}
	org.UpdatedAt = time.Now()
	if err := s.repo.UpdateOrg(org); err != nil {
		return nil, err
	}
	return org, nil
}

// SetReleaseChannel implements Service.
func (s *DefaultService) SetReleaseChannel(id, channel string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if !ChannelChoosable(org.Plan) {
		return nil, ErrChannelLocked
	}
	if channel != "" && !ValidChannel(channel) {
		return nil, ErrInvalidChannel
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
	if !ChannelChoosable(org.Plan) {
		return nil, ErrChannelLocked
	}
	if err := ValidateUpgradeWindow(day, hour, timezone); err != nil {
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

// SetMonthlyBudget sets or clears (nil) the workspace's monthly spend budget.
// The write only touches monthly_budget_usd; the alert-dedupe columns are left
// to the atomic ClaimBudgetAlert path. A negative amount is rejected.
func (s *DefaultService) SetMonthlyBudget(id string, budget *float64) (*Org, error) {
	if budget != nil && *budget < 0 {
		return nil, ErrInvalidBudget
	}
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetBudget(id, budget); err != nil {
		return nil, err
	}
	org.MonthlyBudgetUSD = budget
	org.UpdatedAt = time.Now()
	return org, nil
}

// SetLogo records where the workspace logo lives and what image type it is.
// The write touches only the two logo columns.
func (s *DefaultService) SetLogo(id, path, mime string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetLogo(id, path, mime); err != nil {
		return nil, err
	}
	org.LogoPath = path
	org.LogoMime = mime
	org.HasLogo = path != ""
	org.UpdatedAt = time.Now()
	return org, nil
}

// ClearLogo forgets the workspace logo. The file on disk is the API's to
// remove; the domain only records that there is no logo any more.
func (s *DefaultService) ClearLogo(id string) (*Org, error) {
	return s.SetLogo(id, "", "")
}

// ClaimBudgetAlert delegates the atomic per-threshold-per-month dedupe claim
// to the repository (see Repository.ClaimBudgetAlert).
func (s *DefaultService) ClaimBudgetAlert(orgID, month string, threshold int) (bool, error) {
	return s.repo.ClaimBudgetAlert(orgID, month, threshold)
}

// DeleteOrg soft-deletes a company workspace. Refuses personal workspaces;
// re-deleting an already-deleted workspace is a no-op returning current state.
func (s *DefaultService) DeleteOrg(id string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if org.OrgType == TypePersonal {
		return nil, ErrPersonalOrgDelete
	}
	if org.DeletedAt != nil {
		return org, nil
	}
	now := time.Now()
	if err := s.repo.SoftDeleteOrg(id, now); err != nil {
		return nil, err
	}
	org.DeletedAt = &now
	return org, nil
}

// RestoreOrg clears a workspace's soft delete within the grace period.
func (s *DefaultService) RestoreOrg(id string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if org.DeletedAt == nil {
		return nil, ErrNotDeleted
	}
	if err := s.repo.RestoreOrg(id); err != nil {
		return nil, err
	}
	org.DeletedAt = nil
	return org, nil
}

// ListDeletedForUser returns the user's soft-deleted workspaces.
func (s *DefaultService) ListDeletedForUser(userID string) ([]*Org, error) {
	return s.repo.ListDeletedOrgsForUser(userID)
}

// PurgeExpired hard-deletes every workspace soft-deleted more than
// DeletionGraceDays ago. Purges are independent: one failure doesn't stop the
// rest, and the ids actually purged are returned alongside the first error.
func (s *DefaultService) PurgeExpired(now time.Time) ([]string, error) {
	cutoff := now.Add(-DeletionGraceDays * 24 * time.Hour)
	ids, err := s.repo.ListExpiredDeletedOrgIDs(cutoff)
	if err != nil {
		return nil, err
	}
	var purged []string
	var firstErr error
	for _, id := range ids {
		if err := s.repo.PurgeOrg(id); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("purge org %s: %w", id, err)
			}
			continue
		}
		purged = append(purged, id)
	}
	return purged, firstErr
}

// RoleInOrgAny returns the user's role including deleted workspaces.
func (s *DefaultService) RoleInOrgAny(orgID, userID string) (string, error) {
	return s.repo.MemberRoleAny(orgID, userID)
}

func validOrgRole(role string) bool {
	return role == RoleAdmin || role == RoleMember
}

// AddMember adds or updates a membership.
func (s *DefaultService) AddMember(orgID, userID, role string) error {
	if !validOrgRole(role) {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	org, err := s.Get(orgID)
	if err != nil {
		return err
	}
	if org.OrgType == TypePersonal {
		return ErrPersonalOrgMembers
	}
	return s.repo.UpsertMember(orgID, userID, role)
}

// RemoveMember removes a member, refusing to remove the last admin.
func (s *DefaultService) RemoveMember(orgID, userID string) error {
	role, err := s.repo.MemberRole(orgID, userID)
	if err != nil {
		return err
	}
	if role == RoleAdmin {
		admins, err := s.repo.CountAdmins(orgID)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("cannot remove the last admin of an organization")
		}
	}
	return s.repo.RemoveMember(orgID, userID)
}

// SetMemberRole changes a member's role, refusing to demote the last admin.
func (s *DefaultService) SetMemberRole(orgID, userID, role string) error {
	if !validOrgRole(role) {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	current, err := s.repo.MemberRole(orgID, userID)
	if err != nil {
		return err
	}
	if current == "" {
		return ErrNotMember
	}
	if current == RoleAdmin && role != RoleAdmin {
		admins, err := s.repo.CountAdmins(orgID)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("cannot demote the last admin of an organization")
		}
	}
	return s.repo.UpsertMember(orgID, userID, role)
}

// ListMembers returns an org's members.
func (s *DefaultService) ListMembers(orgID string) ([]*Member, error) {
	return s.repo.ListMembers(orgID)
}

// RoleInOrg returns the user's role, "" when not a member.
func (s *DefaultService) RoleInOrg(orgID, userID string) (string, error) {
	return s.repo.MemberRole(orgID, userID)
}

// IsMember reports membership.
func (s *DefaultService) IsMember(orgID, userID string) (bool, error) {
	role, err := s.repo.MemberRole(orgID, userID)
	return role != "", err
}
