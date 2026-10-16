package postgres

import (
	"testing"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/providers"
)

// A token request is claimed only by its member's runner, and only when that
// runner says it can mint; an ordinary sign-in and a token request for the
// same provider are separate active requests.
func TestTokenLoginsAreClaimedOnlyByAMintingRunner(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	org, member, other := uuid.New().String(), uuid.New().String(), uuid.New().String()
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Org', 'org')`, org); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{member, other} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Member')`, u, u+"@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	logins := providers.NewLoginService(NewProviderLoginRepository(db))
	token, err := logins.StartTokenLogin(org, member)
	if err != nil {
		t.Fatal(err)
	}
	signIn, err := logins.StartLogin(org, providers.ProviderClaudeCode, providers.LoginTargetUser, &member)
	if err != nil {
		t.Fatal(err)
	}
	if signIn.ID == token.ID {
		t.Fatal("the sign-in was handed the token request")
	}
	if again, _ := logins.StartTokenLogin(org, member); again.ID != token.ID {
		t.Fatal("a second token request replaced the active one")
	}

	// A runner that cannot mint takes the sign-in and leaves the token request.
	got, err := logins.Claim(org, member, false)
	if err != nil || got == nil || got.ID != signIn.ID {
		t.Fatalf("non-minting claim = %+v, %v", got, err)
	}
	if got, _ := logins.Claim(org, member, false); got != nil {
		t.Fatalf("a non-minting runner claimed the token request: %+v", got)
	}
	if got, _ := logins.Claim(org, other, true); got != nil {
		t.Fatalf("another member's runner claimed it: %+v", got)
	}
	got, err = logins.Claim(org, member, true)
	if err != nil || got == nil || got.ID != token.ID || got.Purpose != providers.LoginPurposeToken {
		t.Fatalf("minting claim = %+v, %v", got, err)
	}
	if done, err := logins.CompleteTokenLogin(token.ID); err != nil || done.Status != providers.LoginCompleted {
		t.Fatalf("complete = %+v, %v", done, err)
	}
}
