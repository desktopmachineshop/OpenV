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

// Purged is what PurgeExpired hard-deleted: the workspaces, and the stored
// files of their figures (every version), evidence files and logos, which
// the caller removes from the upload store, each purge having committed
// (#379 bug 143: they stayed on disk).
type Purged struct {
	IDs   []string
	Files []string
}

// PurgeExpired hard-deletes every workspace soft-deleted more than
// DeletionGraceDays ago. Purges are independent: one failure doesn't stop the
// rest, and what the purges that succeeded took is answered alongside the
// first error.
func (s *DefaultService) PurgeExpired(now time.Time) (Purged, error) {
	cutoff := now.Add(-DeletionGraceDays * 24 * time.Hour)
	ids, err := s.repo.ListExpiredDeletedOrgIDs(cutoff)
	if err != nil {
		return Purged{}, err
	}
	var purged Purged
	var firstErr error
	for _, id := range ids {
		files, err := s.repo.PurgeOrg(id)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("purge org %s: %w", id, err)
			}
			continue
		}
		purged.IDs = append(purged.IDs, id)
		purged.Files = append(purged.Files, files...)
	}
	return purged, firstErr
}
