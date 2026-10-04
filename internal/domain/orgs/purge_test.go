package orgs

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// purgeRepoFake purges the workspaces it lists, answering each one's files,
// and fails the purge of failing.
type purgeRepoFake struct {
	Repository
	expired []string
	files   map[string][]string
	failing string
}

func (f *purgeRepoFake) ListExpiredDeletedOrgIDs(time.Time) ([]string, error) { return f.expired, nil }

func (f *purgeRepoFake) PurgeOrg(id string) ([]string, error) {
	if id == f.failing {
		return nil, errors.New("the database went away")
	}
	return f.files[id], nil
}

// A purge that fails for one workspace still answers what the others' took:
// their purges committed, and their stored files are the caller's to remove
// (#379 bug 143).
func TestPurgeExpiredAnswersTheFilesOfEveryWorkspaceItPurged(t *testing.T) {
	repo := &purgeRepoFake{
		expired: []string{"org-a", "org-b", "org-c"},
		files:   map[string][]string{"org-a": {"/u/a1", "/u/a2"}, "org-c": {"/u/c1"}},
		failing: "org-b",
	}
	purged, err := NewDefaultService(repo).PurgeExpired(time.Now())
	if err == nil {
		t.Errorf("PurgeExpired with one purge failing answered no error")
	}
	want := Purged{IDs: []string{"org-a", "org-c"}, Files: []string{"/u/a1", "/u/a2", "/u/c1"}}
	if !reflect.DeepEqual(purged, want) {
		t.Errorf("PurgeExpired = %+v, want %+v", purged, want)
	}
}
