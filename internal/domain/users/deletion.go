package users

import (
	"fmt"
	"strings"
	"time"
)

// Removing an account at the identity provider's request (DMSid ADR 0002,
// "Removing an account from a product"): the person asks their DMSid page to
// remove them from OpenV, DMSid sends a signed deletion token, and OpenV
// removes the account (api/oidc_account_deletion.go).
//
// "Removed" for OpenV is anonymised, not deleted: requirements, links,
// comments, test results and the event history are records other people
// rely on, so the users row stays as a "Deleted user" with nothing left
// that identifies the person, and everything that belongs to the person
// alone goes (sessions, keys, tokens, identities, subscriptions,
// memberships, their inbox). A workspace the person is the only member of
// is purged with its contents. Two things block it: being the only admin
// of a workspace others still use, and a live paid subscription on a
// workspace that would be purged.

// Account deletion outcomes, the ADR's "status" values.
const (
	DeletionDeleted  = "deleted"
	DeletionNotFound = "not_found"
	DeletionBlocked  = "blocked"
)

// Reasons a deletion is blocked.
const (
	// BlockSoleOwner: the person is the only admin of a workspace that has
	// other members.
	BlockSoleOwner = "sole_owner"
	// BlockSubscription: a workspace the deletion would purge still has a
	// live paid subscription.
	BlockSubscription = "subscription"
	// BlockPlatformAdmin: the person is the server's only platform admin.
	BlockPlatformAdmin = "platform_admin"
)

// DeletedUserName is the name an anonymised account carries.
const DeletedUserName = "Deleted user"

// DeletedUserEmail is the unique, non-routable address an anonymised
// account carries in place of its own (RFC 2606 reserves .invalid).
func DeletedUserEmail(userID string) string {
	return "deleted-" + userID + "@deleted.invalid"
}

// DeletionBlock says why an account cannot be removed now.
type DeletionBlock struct {
	Kind string
	// Workspace names the first workspace that blocks; empty for
	// BlockPlatformAdmin.
	Workspace string
	// Others counts the further workspaces blocking for the same reason.
	Others int
}

// Reason is the ADR's "reason": one or two plain sentences, addressed to the
// person, saying what to do first. DMSid shows it as it is.
func (b DeletionBlock) Reason() string {
	more := ""
	if b.Others == 1 {
		more = " One other workspace needs the same."
	} else if b.Others > 1 {
		more = fmt.Sprintf(" %d other workspaces need the same.", b.Others)
	}
	switch b.Kind {
	case BlockSubscription:
		return "Your OpenV workspace “" + b.Workspace + "” has a paid subscription. Cancel it in the workspace's billing settings, then try again." + more
	case BlockPlatformAdmin:
		return "You are the only platform administrator of OpenV. Make someone else an administrator, then try again."
	default:
		return "You are the only owner of the workspace “" + b.Workspace + "” in OpenV. Make someone else its owner, or remove the other members, then try again." + more
	}
}

// AccountDeletion is what one deletion request did.
type AccountDeletion struct {
	// Status is one of the Deletion* values.
	Status string
	// UserID is the account removed (or that blocked); empty for not_found.
	UserID string
	// Blocked is set exactly when Status is DeletionBlocked.
	Blocked *DeletionBlock
	// PurgedWorkspaces are the workspaces the person was the only member
	// of, purged with their contents.
	PurgedWorkspaces []string
	// LeftWorkspaces are the workspaces the person was removed from, which
	// carry on without them.
	LeftWorkspaces []string
	// Files are the stored files of the rows the deletion took (the avatar,
	// the purged workspaces' figures, evidence files and logos), for the
	// caller to remove once it has committed.
	Files []string
}

// AccountDeletionRepository removes an account in one transaction.
type AccountDeletionRepository interface {
	// DeleteAccountByEmail finds the account by email, case-insensitively,
	// and anonymises it, or answers not_found or blocked having changed
	// nothing. now stamps the rows it writes, as a UTC wall clock.
	DeleteAccountByEmail(email string, now time.Time) (*AccountDeletion, error)
}

// AccountDeleter removes an account at the identity provider's request.
type AccountDeleter interface {
	DeleteAccount(email string) (*AccountDeletion, error)
}

// AccountDeletionService is the AccountDeleter over a repository.
type AccountDeletionService struct {
	repo AccountDeletionRepository
}

// NewAccountDeletionService builds the AccountDeleter.
func NewAccountDeletionService(repo AccountDeletionRepository) *AccountDeletionService {
	return &AccountDeletionService{repo: repo}
}

// DeleteAccount removes the account the address names. An empty address
// names none.
func (s *AccountDeletionService) DeleteAccount(email string) (*AccountDeletion, error) {
	email = NormalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return &AccountDeletion{Status: DeletionNotFound}, nil
	}
	return s.repo.DeleteAccountByEmail(email, time.Now().UTC())
}
