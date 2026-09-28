package orgs

import (
	"errors"
	"testing"
	"time"
)

// restoreRepoFake is the smallest Repository that lets RestoreOrg run:
// it holds one soft-deleted workspace, and once the restore is stored every
// read fails with readErr.
type restoreRepoFake struct {
	Repository
	org      *Org
	restored bool
	readErr  error
}

func (f *restoreRepoFake) FindOrgByID(id string) (*Org, error) {
	if f.restored && f.readErr != nil {
		return nil, f.readErr
	}
	if f.org == nil || f.org.ID != id {
		return nil, nil
	}
	c := *f.org
	return &c, nil
}

func (f *restoreRepoFake) RestoreOrg(id string) error {
	f.restored = true
	f.org.DeletedAt = nil
	f.org.UpdatedAt = time.Now()
	return nil
}

// TestRestoreOrgStoredAnswersTheWorkspace: once the restore is stored,
// RestoreOrg answers the workspace, restored, even when reading it back
// fails, so that the handler answers the restore and runs the billing hook
// that lifts the delete's cancellation; an error there would leave the
// subscription to lapse, and a retry is refused as not deleted.
func TestRestoreOrgStoredAnswersTheWorkspace(t *testing.T) {
	deleted := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name    string
		readErr error
	}{
		{"read back", nil},
		{"read back failing", errors.New("connection reset")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &restoreRepoFake{
				org:     &Org{ID: "w", Name: "W", UpdatedAt: deleted, DeletedAt: &deleted},
				readErr: tc.readErr,
			}
			org, err := NewDefaultService(repo).RestoreOrg("w")
			if err != nil || org == nil {
				t.Fatalf("RestoreOrg = (%v, %v), want the workspace and no error", org, err)
			}
			if !repo.restored {
				t.Fatal("the restore was not stored")
			}
			if org.ID != "w" || org.Name != "W" || org.DeletedAt != nil || !org.UpdatedAt.After(deleted) {
				t.Fatalf("answered %s %q deleted_at %v updated_at %v; want W, no deleted_at, updated_at after the delete's %v",
					org.ID, org.Name, org.DeletedAt, org.UpdatedAt, deleted)
			}
		})
	}
}
