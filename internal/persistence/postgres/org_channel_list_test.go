package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// TestASelfHostedDeploymentListsNoWorkspaceOnStable: the stable scheduler
// and the release announcements find workspaces through ListOrgsByChannel
// and ListMemberUserIDsByChannel. On the hosted service a Business workspace
// is on stable and a Single one on nightly. On a self-hosted deployment the
// channel follows the self-host plan (orgs.Org.ChannelPlan), so the same two
// rows, the Business one with a stable choice even, are both on nightly and
// nothing is listed on stable; before, the SQL read the plan column and kept
// the Business row on stable.
func TestASelfHostedDeploymentListsNoWorkspaceOnStable(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewOrgRepository(db)
	svc := orgs.NewDefaultService(repo)
	t.Cleanup(func() { orgs.SetSelfHosted(false) })

	owner := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'C')`, owner, owner+"@example.com"); err != nil {
		t.Fatal(err)
	}
	business, err := svc.CreateOrg("Channel Business", orgs.TypeCompany, owner)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	single, err := svc.CreateOrg("Channel Single", orgs.TypeCompany, owner)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if _, err := db.Exec(`UPDATE organizations SET plan = $2, release_channel = $3 WHERE id = $1`, business.ID, orgs.PlanBusiness, orgs.ChannelStable); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE organizations SET plan = $2 WHERE id = $1`, single.ID, orgs.PlanSingle); err != nil {
		t.Fatal(err)
	}

	listed := func(channel string) map[string]bool {
		t.Helper()
		list, err := repo.ListOrgsByChannel(channel)
		if err != nil {
			t.Fatalf("ListOrgsByChannel(%s): %v", channel, err)
		}
		out := map[string]bool{}
		for _, o := range list {
			if o.ID == business.ID || o.ID == single.ID {
				out[o.ID] = true
			}
		}
		return out
	}
	ownerOn := func(channel string) bool {
		t.Helper()
		ids, err := repo.ListMemberUserIDsByChannel(channel)
		if err != nil {
			t.Fatalf("ListMemberUserIDsByChannel(%s): %v", channel, err)
		}
		for _, id := range ids {
			if id == owner {
				return true
			}
		}
		return false
	}

	if got := listed(orgs.ChannelStable); !got[business.ID] || got[single.ID] {
		t.Errorf("hosted, stable lists %v; want the Business workspace only", got)
	}
	if got := listed(orgs.ChannelNightly); got[business.ID] || !got[single.ID] {
		t.Errorf("hosted, nightly lists %v; want the Single workspace only", got)
	}
	if !ownerOn(orgs.ChannelStable) {
		t.Error("hosted: the owner of a stable workspace is not listed on stable")
	}

	orgs.SetSelfHosted(true)
	if got := listed(orgs.ChannelStable); len(got) != 0 {
		t.Errorf("self-hosted, stable lists %v; want none", got)
	}
	if got := listed(orgs.ChannelNightly); !got[business.ID] || !got[single.ID] {
		t.Errorf("self-hosted, nightly lists %v; want both workspaces", got)
	}
	if ownerOn(orgs.ChannelStable) {
		t.Error("self-hosted: an account is listed on stable")
	}
	got, err := svc.Get(business.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReleaseChannel != orgs.ChannelNightly || !got.ReleaseChannelLocked {
		t.Errorf("self-hosted Business workspace reads channel %q, locked %v; want nightly, locked", got.ReleaseChannel, got.ReleaseChannelLocked)
	}
}
