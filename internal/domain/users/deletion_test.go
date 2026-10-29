package users

import (
	"strings"
	"testing"
	"time"
)

type deletionRepoFake struct{ emails []string }

func (f *deletionRepoFake) DeleteAccountByEmail(email string, now time.Time) (*AccountDeletion, error) {
	f.emails = append(f.emails, email)
	return &AccountDeletion{Status: DeletionDeleted}, nil
}

// The address is matched as the rest of OpenV folds it, and an empty one
// names no account without asking the repository.
func TestDeleteAccountNormalisesTheAddress(t *testing.T) {
	repo := &deletionRepoFake{}
	svc := NewAccountDeletionService(repo)
	if out, err := svc.DeleteAccount("  Dave@Example.COM "); err != nil || out.Status != DeletionDeleted {
		t.Fatalf("got %+v, %v", out, err)
	}
	if len(repo.emails) != 1 || repo.emails[0] != "dave@example.com" {
		t.Errorf("repository asked for %v", repo.emails)
	}
	for _, email := range []string{"", "   ", "not-an-address"} {
		if out, _ := svc.DeleteAccount(email); out.Status != DeletionNotFound {
			t.Errorf("%q: %+v, want not_found", email, out)
		}
	}
	if len(repo.emails) != 1 {
		t.Errorf("an empty address reached the repository: %v", repo.emails)
	}
}

// A blocked deletion's reason names the workspace and says what to do,
// in a sentence or two DMSid shows as it is.
func TestDeletionBlockReasons(t *testing.T) {
	cases := []struct {
		block DeletionBlock
		want  string
	}{
		{DeletionBlock{Kind: BlockSoleOwner, Workspace: "Acme"},
			"You are the only owner of the workspace “Acme” in OpenV. Make someone else its owner, or remove the other members, then try again."},
		{DeletionBlock{Kind: BlockSoleOwner, Workspace: "Acme", Others: 2},
			"You are the only owner of the workspace “Acme” in OpenV. Make someone else its owner, or remove the other members, then try again. 2 other workspaces need the same."},
		{DeletionBlock{Kind: BlockSubscription, Workspace: "Solo", Others: 1},
			"Your OpenV workspace “Solo” has a paid subscription. Cancel it in the workspace's billing settings, then try again. One other workspace needs the same."},
		{DeletionBlock{Kind: BlockPlatformAdmin},
			"You are the only platform administrator of OpenV. Make someone else an administrator, then try again."},
	}
	for _, c := range cases {
		if got := c.block.Reason(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.block, got, c.want)
		}
	}
	if !strings.HasSuffix(DeletedUserEmail("abc"), ".invalid") {
		t.Error("the placeholder address must not be routable")
	}
}
