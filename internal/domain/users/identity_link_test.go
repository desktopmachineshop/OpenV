package users

import (
	"errors"
	"testing"
	"time"
)

func TestIdentityLinkConfirmedByPasswordLetsTheProviderIn(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	user, err := svc.Register("dave@example.com", "password1", "Dave")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, _, err := svc.LoginWithGoogle("Dave@example.com", "Google Dave", "https://pic"); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("google before linking: got %v, want ErrProviderMismatch", err)
	}

	token, err := svc.RequestIdentityLink(ProviderGoogle, "Dave@example.com")
	if err != nil {
		t.Fatalf("RequestIdentityLink: %v", err)
	}
	preview, err := svc.PreviewIdentityLink(token)
	if err != nil || preview.Provider != ProviderGoogle || preview.Email != "dave@example.com" {
		t.Fatalf("PreviewIdentityLink = %+v, %v", preview, err)
	}

	// A wrong password does not spend the request: the person retypes it.
	if _, _, err := svc.ConfirmIdentityLink(token, "wrong-password"); !errors.Is(err, ErrPasswordIncorrect) {
		t.Fatalf("wrong password: got %v, want ErrPasswordIncorrect", err)
	}
	linked, session, err := svc.ConfirmIdentityLink(token, "password1")
	if err != nil {
		t.Fatalf("ConfirmIdentityLink: %v", err)
	}
	if linked.ID != user.ID || session == "" {
		t.Fatalf("confirm entered %q with session %q, want %q and a session", linked.ID, session, user.ID)
	}
	if _, _, err := svc.ConfirmIdentityLink(token, "password1"); !errors.Is(err, ErrLinkInvalid) {
		t.Fatalf("second confirm: got %v, want ErrLinkInvalid", err)
	}

	again, _, err := svc.LoginWithGoogle("dave@example.com", "Google Dave", "https://pic")
	if err != nil {
		t.Fatalf("google after linking: %v", err)
	}
	if again.ID != user.ID || again.AuthProvider != ProviderPassword || again.Name != "Dave" {
		t.Errorf("google after linking entered %+v, want the password account with its own name", again)
	}
	if _, _, err := svc.Login("dave@example.com", "password1"); err != nil {
		t.Errorf("the password still signs in after linking: %v", err)
	}
	// Linking Google says nothing about another provider.
	if _, _, err := svc.LoginWithSSO(ProviderOIDC, "dave@example.com", "Dave", ""); !errors.Is(err, ErrProviderMismatch) {
		t.Errorf("oidc after linking google: got %v, want ErrProviderMismatch", err)
	}
}

func TestIdentityLinkNeedsAPasswordAccount(t *testing.T) {
	svc := NewDefaultService(newMemRepo())
	if _, err := svc.RequestIdentityLink(ProviderGoogle, "nobody@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown address: got %v, want ErrUserNotFound", err)
	}
	if _, _, err := svc.LoginWithGoogle("g@example.com", "G", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequestIdentityLink(ProviderOIDC, "g@example.com"); !errors.Is(err, ErrNoPassword) {
		t.Errorf("google account met by oidc: got %v, want ErrNoPassword", err)
	}
}

func TestIdentityLinkExpires(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	if _, err := svc.Register("e@example.com", "password1", "E"); err != nil {
		t.Fatal(err)
	}
	token, err := svc.RequestIdentityLink(ProviderGoogle, "e@example.com")
	if err != nil {
		t.Fatal(err)
	}
	repo.links[HashToken(token)].ExpiresAt = time.Now().UTC().Add(-time.Second)
	if _, err := svc.PreviewIdentityLink(token); !errors.Is(err, ErrLinkInvalid) {
		t.Errorf("expired preview: got %v, want ErrLinkInvalid", err)
	}
	if _, _, err := svc.ConfirmIdentityLink(token, "password1"); !errors.Is(err, ErrLinkInvalid) {
		t.Errorf("expired confirm: got %v, want ErrLinkInvalid", err)
	}
	if _, _, err := svc.ConfirmIdentityLink("", "password1"); !errors.Is(err, ErrLinkInvalid) {
		t.Errorf("no token: got %v, want ErrLinkInvalid", err)
	}
}
