package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// The reaper's sweep deletes the sign-up verification and password reset
// links that were used or have expired, and keeps a live one (#379 bug 163:
// nothing called DeleteSpentEmailVerifications, and nothing deleted a used
// reset link at all, or an expired one until its account asked for another).
// Each is handed the server's local time.Now(), as the reaper hands it, with
// time.Local two hours east of UTC and then five hours west: a link with a
// minute to run is kept and one that expired a minute ago is deleted (with
// the cutoff left local, east deleted the first and west kept the second).
func TestTheSweepDeletesSpentLinksOnly(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := rtDB(t)
			arsInZone(t, zone)
			repo := NewUserRepository(db)
			now := time.Now().UTC()
			// Each link is an account's own: saving a link discards the
			// account's other unused ones.
			kinds := map[string]struct {
				expires    time.Time
				used, keep bool
			}{
				"live":    {expires: now.Add(time.Minute), keep: true},
				"used":    {expires: now.Add(time.Hour), used: true},
				"expired": {expires: now.Add(-time.Minute)},
			}
			for name, k := range kinds {
				user := saveTestUser(t, repo, name+"@example.com", users.ProviderPassword, false)
				saveLink(t, repo, user.ID, user.Email, "verify-"+name, k.expires)
				if err := repo.SavePasswordReset(&users.PasswordReset{ID: uuid.New().String(), UserID: user.ID,
					TokenHash: users.HashToken("reset-" + name), Delivery: users.ResetDeliveryEmail, ExpiresAt: k.expires,
					CreatedAt: now}); err != nil {
					t.Fatal(err)
				}
				if k.used {
					rtSeed(t, db, `UPDATE email_verifications SET used = TRUE WHERE token_hash = $1`, users.HashToken("verify-"+name))
					rtSeed(t, db, `UPDATE password_resets SET used = TRUE WHERE token_hash = $1`, users.HashToken("reset-"+name))
				}
			}

			if err := repo.DeleteSpentEmailVerifications(time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := repo.DeleteSpentPasswordResets(time.Now()); err != nil {
				t.Fatal(err)
			}
			for name, k := range kinds {
				for table, prefix := range map[string]string{"email_verifications": "verify-", "password_resets": "reset-"} {
					var n int
					if err := db.QueryRow(`SELECT count(*) FROM `+table+` WHERE token_hash = $1`, users.HashToken(prefix+name)).
						Scan(&n); err != nil {
						t.Fatal(err)
					}
					if kept := n == 1; kept != k.keep {
						t.Errorf("the sweep left the %s link in %s: %v, want %v", name, table, kept, k.keep)
					}
				}
			}
		})
	}
}
