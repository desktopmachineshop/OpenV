// Deleting a workspace: the soft delete, the restore within the grace
// period, and the purge once it has passed.

package orgs

import (
	"fmt"
	"time"
)

// DeletionGraceDays is how long a soft-deleted workspace stays restorable
// before the purge job hard-deletes it and everything it contains.
const DeletionGraceDays = 30

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
	// deleted_at is a TIMESTAMP holding a UTC wall clock, which the purge
	// compares with its cutoff (#379 bug 155).
	now := time.Now().UTC()
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
	// Answer the row as restored (the write stamps updated_at); the restore
	// is stored, so a failed read back still answers it.
	if restored, err := s.Get(id); err == nil {
		return restored, nil
	}
	org.DeletedAt = nil
	org.UpdatedAt = time.Now()
	return org, nil
}

// ListDeletedForUser returns the user's soft-deleted workspaces.
func (s *DefaultService) ListDeletedForUser(userID string) ([]*Org, error) {
	return s.repo.ListDeletedOrgsForUser(userID)
}

// PurgeExpired hard-deletes every workspace soft-deleted more than
// DeletionGraceDays ago. Purges are independent: one failure doesn't stop the
// rest, and the ids actually purged are returned alongside the first error.
// The purge loop hands in its own time.Now(); deleted_at is a TIMESTAMP
// holding a UTC wall clock, so the cutoff is taken in UTC (#379 bug 155).
func (s *DefaultService) PurgeExpired(now time.Time) ([]string, error) {
	cutoff := now.UTC().Add(-DeletionGraceDays * 24 * time.Hour)
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
