package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Accounts, sign-in sessions, password reset links and email verification
// links stamp the times they hand the database in UTC (#379 bug 154, the
// class of bug 93 TestTheServicesStampTimesInUTC pins for other services).
// Their columns are TIMESTAMP: Postgres keeps the wall clock lib/pq sends and
// drops its offset, and every read takes that wall clock as UTC. The users
// service stamped time.Now() in the server's own zone, so on a server outside
// UTC every time it wrote moved by the zone's offset, and the session checks,
// which compare those times read back with the instant of the request,
// moved with them. The service is driven through its real repository with
// time.Local two hours east of UTC and then five hours west, under a session
// policy of a 3-hour lifetime and a 1-hour idle window: each stamp is stored
// as the UTC wall clock of the moment it was taken, each time the service
// answers is the instant a later read answers, and
//   - a session signed in a moment ago is live (west, every session read as
//     five hours idle, so none outlived its sign-in);
//   - a session idle for a minute less than the window is live, and one idle
//     for a minute more is not (east, the check let it live two hours more);
//   - a session a minute short of its stored deadline, or of the policy's
//     lifetime from sign-in, is live, and one a minute past either is not
//     (east, the check let each live two hours more);
//   - the reaper's sweep, handed the server's local now, keeps a session in
//     use and deletes an idle one, their times stored as UTC wall clocks
//     (east, a local now deleted sessions used in the last hour; west, it
//     kept idle ones five hours more; it agreed only with the local stamps);
//   - a reset link and a verification link work a minute before their
//     deadline and not a minute after (their local stamps and local checks
//     agreed; each moved with the other).
func TestTheSignInsStampTimesInUTC(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := rtDB(t)
			arsInZone(t, zone)
			repo := NewUserRepository(db)
			svc := users.NewDefaultService(repo)
			const maxAge, idle = 3 * time.Hour, time.Hour
			svc.SetSessionPolicy(users.SessionPolicy{MaxAge: maxAge, Idle: idle})

			var before time.Time
			start := func() { before = time.Now() }
			// stored reads one TIMESTAMP column of the row with id.
			stored := func(table, column, id string) time.Time {
				t.Helper()
				var at time.Time
				if err := db.QueryRow(`SELECT `+column+` FROM `+table+` WHERE id = $1`, id).Scan(&at); err != nil {
					t.Fatalf("%s.%s of %s: %v", table, column, id, err)
				}
				return at
			}
			// age moves a row's columns back by d, as time passing would.
			age := func(table, id string, d time.Duration, columns ...string) {
				t.Helper()
				for _, column := range columns {
					rtSeed(t, db, `UPDATE `+table+` SET `+column+` = `+column+` - make_interval(secs => $2) WHERE id = $1`,
						id, d.Seconds())
				}
			}
			register := func(email string) *users.User {
				t.Helper()
				user, err := svc.Register(email, "password1", "Dana")
				if err != nil {
					t.Fatalf("Register(%s): %v", email, err)
				}
				return user
			}

			start()
			dana := register("dana@example.com")
			rtStamp(t, "a new account's created_at", stored("users", "created_at", dana.ID), dana.CreatedAt, before, time.Now())
			rtStamp(t, "a new account's updated_at", stored("users", "updated_at", dana.ID), dana.UpdatedAt, before, time.Now())
			rtStamp(t, "a new account's email_verified_at", stored("users", "email_verified_at", dana.ID), *dana.EmailVerifiedAt,
				before, time.Now())

			t.Run("accounts", func(t *testing.T) {
				start()
				sam, _, err := svc.LoginWithSSO(users.ProviderOIDC, "sam@example.com", "Sam", "")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an identity provider's new account's created_at", stored("users", "created_at", sam.ID), sam.CreatedAt,
					before, time.Now())
				rtStamp(t, "an identity provider's new account's email_verified_at", stored("users", "email_verified_at", sam.ID),
					*sam.EmailVerifiedAt, before, time.Now())
				start()
				if sam, _, err = svc.LoginWithSSO(users.ProviderOIDC, "sam@example.com", "Sam Lee", ""); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an identity provider's sign-in's updated_at", stored("users", "updated_at", sam.ID), sam.UpdatedAt,
					before, time.Now())
				start()
				if sam, err = svc.SetAdmin(sam.ID, true); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a promotion's updated_at", stored("users", "updated_at", sam.ID), sam.UpdatedAt, before, time.Now())
				start()
				pictured, err := svc.SetAvatar(dana.ID, "/uploads/dana.png", "image/png", "/avatars/dana")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an avatar's updated_at", stored("users", "updated_at", dana.ID), pictured.UpdatedAt, before, time.Now())
				start()
				if err := svc.ChangePassword(dana.ID, "password1", "password2", ""); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a password change's updated_at", stored("users", "updated_at", dana.ID), time.Time{}, before, time.Now())
			})

			// signIn opens a session for Dana and answers its token and id.
			signIn := func() (string, string) {
				t.Helper()
				_, token, err := svc.Login("dana@example.com", "password2")
				if err != nil {
					t.Fatalf("Login: %v", err)
				}
				session, err := repo.FindSessionByTokenHash(users.HashToken(token))
				if err != nil || session == nil {
					t.Fatalf("the session: %v, %v", session, err)
				}
				return token, session.ID
			}
			live := func(what, token string, want bool) {
				t.Helper()
				_, err := svc.SessionByToken(token)
				if got := err == nil; got != want || (err != nil && !errors.Is(err, users.ErrSessionInvalid)) {
					t.Errorf("%s: SessionByToken answered %v, want live %v", what, err, want)
				}
			}

			t.Run("sessions", func(t *testing.T) {
				start()
				token, id := signIn()
				startedFrom, startedTo := before, time.Now()
				rtStamp(t, "a session's created_at", stored("sessions", "created_at", id), time.Time{}, startedFrom, startedTo)
				rtStamp(t, "a session's last_seen_at", stored("sessions", "last_seen_at", id), time.Time{}, startedFrom, startedTo)
				rtStamp(t, "a session's expires_at", stored("sessions", "expires_at", id), time.Time{},
					startedFrom.Add(maxAge), startedTo.Add(maxAge))
				if _, err := svc.GetBySessionToken(token); err != nil {
					t.Errorf("a session signed in a moment ago: GetBySessionToken answered %v, want its account", err)
				}
				live("a session signed in a moment ago", token, true)

				// A request more than SessionTouchInterval after the last one
				// stamps last_seen_at.
				age("sessions", id, 2*time.Minute, "last_seen_at")
				start()
				if _, err := svc.GetBySessionToken(token); err != nil {
					t.Fatalf("a session used 2 minutes ago: %v", err)
				}
				rtStamp(t, "a request's last_seen_at", stored("sessions", "last_seen_at", id), time.Time{}, before, time.Now())

				age("sessions", id, idle-time.Minute, "last_seen_at")
				live("a session idle for a minute less than the idle window", token, true)
				age("sessions", id, 2*time.Minute, "last_seen_at")
				live("a session idle for a minute more than the idle window", token, false)
				if _, err := svc.GetBySessionToken(token); !errors.Is(err, users.ErrSessionInvalid) {
					t.Errorf("a session idle for a minute more than the idle window: GetBySessionToken answered %v, want ErrSessionInvalid", err)
				}

				token, id = signIn()
				age("sessions", id, maxAge-time.Minute, "expires_at")
				live("a session a minute short of its stored deadline", token, true)
				age("sessions", id, 2*time.Minute, "expires_at")
				live("a session a minute past its stored deadline", token, false)

				token, id = signIn()
				age("sessions", id, maxAge-time.Minute, "created_at")
				live("a session signed in a minute less than the lifetime ago, and in use", token, true)
				age("sessions", id, 2*time.Minute, "created_at")
				live("a session signed in a minute more than the lifetime ago, and in use", token, false)
			})

			t.Run("the reaper's sweep", func(t *testing.T) {
				// Two sessions as the database holds them: signed in a minute
				// ago, one last used a minute inside the idle window, one a
				// minute outside it.
				inUse, inUseID := signIn()
				idleToken, idleID := signIn()
				now := time.Now().UTC()
				for id, lastSeen := range map[string]time.Time{inUseID: now.Add(-idle + time.Minute), idleID: now.Add(-idle - time.Minute)} {
					rtSeed(t, db, `UPDATE sessions SET created_at = $2, last_seen_at = $3, expires_at = $4 WHERE id = $1`,
						id, now.Add(-time.Minute), lastSeen, now.Add(maxAge-time.Minute))
				}
				// The reaper hands in its own time.Now().
				if err := repo.DeleteExpiredSessions(time.Now(), maxAge, idle); err != nil {
					t.Fatal(err)
				}
				var left []string
				if err := db.QueryRow(`SELECT COALESCE(array_agg(id::text), '{}') FROM sessions WHERE id IN ($1, $2)`, inUseID, idleID).
					Scan(pq.Array(&left)); err != nil {
					t.Fatal(err)
				}
				if len(left) != 1 || left[0] != inUseID {
					t.Errorf("the sweep left sessions %v of the one in use (%s) and the idle one (%s), want the one in use alone",
						left, inUseID, idleID)
				}
				live("a session in use after the sweep", inUse, true)
				live("an idle session after the sweep", idleToken, false)
			})

			t.Run("password resets", func(t *testing.T) {
				start()
				token, expires, err := svc.IssuePasswordReset(dana.ID, users.ResetDeliveryEmail, nil)
				if err != nil {
					t.Fatal(err)
				}
				var id string
				if err := db.QueryRow(`SELECT id FROM password_resets WHERE token_hash = $1`, users.HashToken(token)).Scan(&id); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a reset link's created_at", stored("password_resets", "created_at", id), time.Time{}, before, time.Now())
				rtStamp(t, "a reset link's expires_at", stored("password_resets", "expires_at", id), expires,
					before.Add(users.PasswordResetTTL), time.Now().Add(users.PasswordResetTTL))

				age("password_resets", id, users.PasswordResetTTL-time.Minute, "expires_at")
				start()
				if _, err := svc.ResetPassword(token, "password3"); err != nil {
					t.Errorf("a reset link a minute before its deadline: %v, want it to set the password", err)
				}
				rtStamp(t, "a reset's updated_at", stored("users", "updated_at", dana.ID), time.Time{}, before, time.Now())

				token, _, err = svc.IssuePasswordReset(dana.ID, users.ResetDeliveryEmail, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT id FROM password_resets WHERE token_hash = $1`, users.HashToken(token)).Scan(&id); err != nil {
					t.Fatal(err)
				}
				age("password_resets", id, users.PasswordResetTTL+time.Minute, "expires_at")
				if _, err := svc.ResetPassword(token, "password4"); !errors.Is(err, users.ErrResetInvalid) {
					t.Errorf("a reset link a minute past its deadline: %v, want ErrResetInvalid", err)
				}
			})

			t.Run("email verification", func(t *testing.T) {
				svc.SetEmailVerificationPolicy(users.EmailVerificationPolicy{Required: true})
				// issue mints a link for a new, unverified account and
				// answers the account, the token and the link's id.
				issue := func(email string) (*users.User, string, string) {
					t.Helper()
					user := register(email)
					start()
					token, _, err := svc.IssueEmailVerification(user.ID, "")
					if err != nil {
						t.Fatal(err)
					}
					var id string
					if err := db.QueryRow(`SELECT id FROM email_verifications WHERE token_hash = $1`, users.HashToken(token)).Scan(&id); err != nil {
						t.Fatal(err)
					}
					return user, token, id
				}

				lee, token, id := issue("lee@example.com")
				rtStamp(t, "a verification link's created_at", stored("email_verifications", "created_at", id), time.Time{},
					before, time.Now())
				rtStamp(t, "a verification link's expires_at", stored("email_verifications", "expires_at", id), time.Time{},
					before.Add(users.EmailVerificationTTL), time.Now().Add(users.EmailVerificationTTL))
				age("email_verifications", id, users.EmailVerificationTTL-time.Minute, "expires_at")
				start()
				verified, err := svc.ConfirmEmailVerification(token)
				if err != nil {
					t.Fatalf("a verification link a minute before its deadline: %v, want it to verify the address", err)
				}
				rtStamp(t, "a confirmed link's email_verified_at", stored("users", "email_verified_at", lee.ID), *verified.EmailVerifiedAt,
					before, time.Now())
				rtStamp(t, "a confirmed link's updated_at", stored("users", "updated_at", lee.ID), verified.UpdatedAt, before, time.Now())

				kim, token, id := issue("kim@example.com")
				age("email_verifications", id, users.EmailVerificationTTL+time.Minute, "expires_at")
				if _, err := svc.ConfirmEmailVerification(token); !errors.Is(err, users.ErrVerificationInvalid) {
					t.Errorf("a verification link a minute past its deadline: %v, want ErrVerificationInvalid", err)
				}

				start()
				marked, err := svc.MarkEmailVerified(kim.ID)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an address verified otherwise: email_verified_at", stored("users", "email_verified_at", kim.ID),
					*marked.EmailVerifiedAt, before, time.Now())
				rtStamp(t, "an address verified otherwise: updated_at", stored("users", "updated_at", kim.ID), marked.UpdatedAt,
					before, time.Now())
			})
		})
	}
}
