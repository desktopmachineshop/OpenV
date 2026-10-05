// The workspace itself: creating one (a personal one at sign-up), reading
// and listing, renaming, and its logo.

package orgs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Workspaces is the slice of Service for the workspace itself: creating,
// reading, listing and renaming it, its logo, and its deletion
// (deletion.go).
type Workspaces interface {
	CreateOrg(name, orgType string, createdBy string) (*Org, error)
	// EnsurePersonalOrg returns the user's personal org, creating it if
	// missing. Idempotent; used at signup and during backfill.
	EnsurePersonalOrg(userID, displayName string) (*Org, bool, error)
	Get(id string) (*Org, error)
	ListForUser(userID string) ([]*Org, error)
	// ListAll returns every organization id (trusted boot-time callers only).
	ListAll() ([]string, error)
	// EarliestPersonalOrgID returns the bootstrap workspace, the personal
	// workspace whose member account was created first, or "" when there is
	// none (trusted boot-time callers and the legacy worker key only).
	EarliestPersonalOrgID() (string, error)
	UpdateOrg(id string, name *string) (*Org, error)
	// SetLogo records the workspace logo's on-disk path and MIME type and
	// returns the updated org.
	SetLogo(id, path, mime string) (*Org, error)
	// ClearLogo forgets the workspace logo (stores empty path and MIME) and
	// returns the updated org. Removing the file is the caller's job.
	ClearLogo(id string) (*Org, error)

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
	// answering the purged ids and their stored files (Purged).
	PurgeExpired(now time.Time) (Purged, error)
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
	now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 162)
	org := &Org{
		ID:         uuid.New().String(),
		Name:       name,
		OrgType:    orgType,
		BilledPlan: DefaultPlan(),
		Limits:     map[string]interface{}{},
		CreatedBy:  &createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
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

// EarliestPersonalOrgID returns the bootstrap workspace: the personal
// workspace whose member account was created first, or "" when there is
// none. It is where the legacy WORKER_API_KEY is registered at boot, and
// where that key acts while no key row holds it.
func (s *DefaultService) EarliestPersonalOrgID() (string, error) {
	return s.repo.EarliestPersonalOrgID()
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
	org.UpdatedAt = time.Now().UTC() // a TIMESTAMP holding a UTC wall clock (#379 bug 162)
	if err := s.repo.UpdateOrg(org); err != nil {
		return nil, err
	}
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
