package users

import (
	"errors"
	"testing"
	"time"
)

// Password reset (REQ-158): a link sets the password once, ends every
// session, and an emailed link also proves the address.
func TestPasswordResetSetsThePasswordAndEndsEverySession(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, err := svc.Register("owner@example.com", "old-password", "Owner")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.EmailVerified {
		t.Fatal("fixture: the account should start unverified")
	}
	_, laptop, _ := svc.Login("owner@example.com", "old-password")
	_, phone, _ := svc.Login("owner@example.com", "old-password")

	token, expires, err := svc.IssuePasswordReset(user.ID, ResetDeliveryEmail, nil)
	if err != nil {
		t.Fatalf("IssuePasswordReset: %v", err)
	}
	if until := time.Until(expires); until > PasswordResetTTL || until < PasswordResetTTL-time.Minute {
		t.Errorf("emailed link expires in %s, want about %s", until, PasswordResetTTL)
	}

	// A weak password is refused BEFORE the link is spent.
	if _, err := svc.ResetPassword(token, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password returned %v, want ErrWeakPassword", err)
	}
	got, err := svc.ResetPassword(token, "brand-new-password")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if got.ID != user.ID || !got.EmailVerified {
		t.Errorf("reset answered %+v, want the account, verified by the emailed link", got)
	}
	for name, tok := range map[string]string{"laptop": laptop, "phone": phone} {
		if _, err := svc.GetBySessionToken(tok); !errors.Is(err, ErrSessionInvalid) {
			t.Errorf("%s session returned %v, want ErrSessionInvalid", name, err)
		}
	}
	if _, _, err := svc.Login("owner@example.com", "brand-new-password"); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
	if _, _, err := svc.Login("owner@example.com", "old-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password returned %v, want ErrInvalidCredentials", err)
	}
	// Single use.
	if _, err := svc.ResetPassword(token, "another-password"); !errors.Is(err, ErrResetInvalid) {
		t.Errorf("spent token returned %v, want ErrResetInvalid", err)
	}
	if _, err := svc.ResetPassword("", "another-password"); !errors.Is(err, ErrResetInvalid) {
		t.Errorf("empty token returned %v, want ErrResetInvalid", err)
	}
}

func TestPasswordResetAdminLinkVerifiesNothingAndLastsLonger(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, _ := svc.Register("owner@example.com", "old-password", "Owner")
	admin := "admin-1"
	token, expires, err := svc.IssuePasswordReset(user.ID, ResetDeliveryAdmin, &admin)
	if err != nil {
		t.Fatalf("IssuePasswordReset: %v", err)
	}
	if until := time.Until(expires); until > AdminPasswordResetTTL || until < AdminPasswordResetTTL-time.Minute {
		t.Errorf("admin link expires in %s, want about %s", until, AdminPasswordResetTTL)
	}
	stored := repo.resets[HashToken(token)]
	if stored == nil || stored.Delivery != ResetDeliveryAdmin || stored.IssuedBy == nil || *stored.IssuedBy != admin {
		t.Fatalf("stored link = %+v, want an admin link issued by %s", stored, admin)
	}
	got, err := svc.ResetPassword(token, "brand-new-password")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if got.EmailVerified {
		t.Error("an admin-minted link proves nothing about the mailbox and must not verify the address")
	}
}

func TestPasswordResetRefusals(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	if _, _, err := svc.IssuePasswordReset("nobody", ResetDeliveryEmail, nil); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown account returned %v, want ErrUserNotFound", err)
	}
	sso, _, err := svc.LoginWithSSO("oidc", "sso@example.com", "SSO", "")
	if err != nil {
		t.Fatalf("LoginWithSSO: %v", err)
	}
	if _, _, err := svc.IssuePasswordReset(sso.ID, ResetDeliveryEmail, nil); !errors.Is(err, ErrNoPassword) {
		t.Errorf("SSO account returned %v, want ErrNoPassword", err)
	}

	// Issuing again replaces the pending link, and an expired one is dead.
	user, _ := svc.Register("owner@example.com", "old-password", "Owner")
	first, _, _ := svc.IssuePasswordReset(user.ID, ResetDeliveryEmail, nil)
	second, _, _ := svc.IssuePasswordReset(user.ID, ResetDeliveryEmail, nil)
	if _, err := svc.ResetPassword(first, "brand-new-password"); !errors.Is(err, ErrResetInvalid) {
		t.Errorf("superseded token returned %v, want ErrResetInvalid", err)
	}
	repo.resets[HashToken(second)].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := svc.ResetPassword(second, "brand-new-password"); !errors.Is(err, ErrResetInvalid) {
		t.Errorf("expired token returned %v, want ErrResetInvalid", err)
	}
	if _, _, err := svc.Login("owner@example.com", "old-password"); err != nil {
		t.Errorf("a refused reset must leave the password alone: %v", err)
	}
}
