package main

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The reaper's sweep deletes the sign-up verification and password reset
// links that were used or have expired, and keeps a live one (#379 bug 163).
// UserRepository.DeleteSpentEmailVerifications was never called and nothing
// deleted a used reset link, so both tables only grew. Against a real
// database (OPENV_TEST_DATABASE_URL; skipped when unset), one sweep of the
// reaper, as runReaper makes on each tick, leaves the live link of each kind
// and deletes the used and the expired one.
func TestTheReaperDeletesSpentLinks(t *testing.T) {
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := postgres.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	userRepo := postgres.NewUserRepository(conn)
	orgService := orgs.NewDefaultService(postgres.NewOrgRepository(conn))
	runService := agentruns.NewDefaultService(postgres.NewAgentRunRepository(conn), nil, nil)
	invitationService := invitations.NewDefaultService(postgres.NewInvitationRepository(conn), orgService)

	// linkTables names each kind of link's table and the prefix of its
	// tokens here.
	linkTables := map[string]string{"email_verifications": "verify-", "password_resets": "reset-"}
	now := time.Now().UTC()
	links := map[string]struct {
		expires    time.Time
		used, keep bool
	}{
		"live":    {expires: now.Add(time.Hour), keep: true},
		"used":    {expires: now.Add(time.Hour), used: true},
		"expired": {expires: now.Add(-time.Minute)},
	}
	for name, l := range links {
		// Each link is an account's own: saving a link discards the
		// account's other unused ones.
		user := &users.User{ID: uuid.NewString(), Email: name + "@example.com", Name: "Dana", AuthProvider: users.ProviderPassword,
			CreatedAt: now, UpdatedAt: now}
		if err := userRepo.SaveUser(user); err != nil {
			t.Fatal(err)
		}
		if err := userRepo.SaveEmailVerification(&users.EmailVerification{ID: uuid.NewString(), UserID: user.ID, Email: user.Email,
			TokenHash: users.HashToken("verify-" + name), ExpiresAt: l.expires, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := userRepo.SavePasswordReset(&users.PasswordReset{ID: uuid.NewString(), UserID: user.ID,
			TokenHash: users.HashToken("reset-" + name), Delivery: users.ResetDeliveryEmail, ExpiresAt: l.expires,
			CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if l.used {
			for table, prefix := range linkTables {
				if _, err := conn.Exec(`UPDATE `+table+` SET used = TRUE WHERE token_hash = $1`, users.HashToken(prefix+name)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	reap(runService, userRepo, invitationService, nil,
		users.SessionPolicy{MaxAge: users.DefaultSessionMaxAge, Idle: users.DefaultSessionIdle})

	for name, l := range links {
		for table, prefix := range linkTables {
			var n int
			if err := conn.QueryRow(`SELECT count(*) FROM `+table+` WHERE token_hash = $1`, users.HashToken(prefix+name)).
				Scan(&n); err != nil {
				t.Fatal(err)
			}
			if kept := n == 1; kept != l.keep {
				t.Errorf("after a sweep of the reaper, the %s link in %s is kept: %v, want %v", name, table, kept, l.keep)
			}
		}
	}
}
