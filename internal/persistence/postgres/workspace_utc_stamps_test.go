package postgres

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Hosted runners, workspace invitations and a workspace's deletion stamp the
// times they hand the database in UTC (#379 bug 155, the class of bug 93
// TestTheServicesStampTimesInUTC pins for other services): hosted_workers'
// created_at and updated_at, an invitation's created_at, last_emailed_at and
// accepted_at, and a workspace's deleted_at are TIMESTAMP columns, which keep
// the wall clock lib/pq sends, drop its offset and are read back as UTC. Each
// service is driven through its real repositories with time.Local two hours
// east of UTC and then five hours west: each stamp is stored as the UTC wall
// clock of the moment it was taken, each time a service answers is the
// instant a later read answers, and
//   - a link emailed a moment ago reads as emailed a moment ago, which is what
//     the invite handler's resend window measures (east, it read as emailed
//     in two hours' time, and a resend was held back for three hours; west,
//     as five hours ago, and never held back);
//   - an invitation with an hour to run can be accepted, by its link or by a
//     provider-verified sign-in, and one that expired after it was looked up
//     cannot, and its accepted_at is the UTC wall clock of the claim (these
//     held already: the claim's parameter is typed by expires_at, a
//     TIMESTAMPTZ, so it keeps its offset, and accepted_at takes it in the
//     session's zone, UTC);
//   - the purge, handed the server's local now, keeps a workspace deleted a
//     minute less than the grace period ago and purges one deleted a minute
//     more, whether DeleteOrg stamped it or a server in UTC did (east, it
//     purged UTC-stamped ones two hours early; west, five hours late).
func TestTheWorkspaceRecordsStampTimesInUTC(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := rtDB(t)
			arsInZone(t, zone)
			org, inviter := uuid.New().String(), uuid.New().String()
			rtSeedOrg(t, db, org)
			rtSeedUser(t, db, inviter, "dana@example.com", "Dana", "")
			rtSeed(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`, org, inviter)

			var before time.Time
			start := func() { before = time.Now() }
			stored := func(table, column, id string) time.Time {
				t.Helper()
				var at time.Time
				if err := db.QueryRow(`SELECT `+column+` FROM `+table+` WHERE id = $1`, id).Scan(&at); err != nil {
					t.Fatalf("%s.%s of %s: %v", table, column, id, err)
				}
				return at
			}
			orgService := orgs.NewDefaultService(NewOrgRepository(db))

			t.Run("hosted runners", func(t *testing.T) {
				svc := hostedworkers.NewDefaultService(NewHostedWorkerRepository(db))
				start()
				w, err := svc.Create(org, "openv-runner-utc", nil, &inviter)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a hosted runner's created_at", stored("hosted_workers", "created_at", w.ID), w.CreatedAt, before, time.Now())
				rtStamp(t, "a hosted runner's updated_at", stored("hosted_workers", "updated_at", w.ID), w.UpdatedAt, before, time.Now())
				start()
				running, err := svc.SetStatus(w.ID, hostedworkers.StatusRunning, "")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a hosted runner's new status's updated_at", stored("hosted_workers", "updated_at", w.ID), running.UpdatedAt,
					before, time.Now())
			})

			t.Run("invitations", func(t *testing.T) {
				svc := invitations.NewDefaultService(NewInvitationRepository(db), orgService)
				// invite seeds an account for email and invites it.
				invite := func(email string) (*invitations.Invitation, string, string) {
					t.Helper()
					user := uuid.New().String()
					rtSeedUser(t, db, user, email, "Invitee", "")
					start()
					inv, token, err := svc.Create(org, email, orgs.RoleMember, &inviter)
					if err != nil {
						t.Fatal(err)
					}
					return inv, token, user
				}
				expiresIn := func(id string, d time.Duration) {
					t.Helper()
					rtSeed(t, db, `UPDATE org_invitations SET expires_at = NOW() + make_interval(secs => $2) WHERE id = $1`, id, d.Seconds())
				}

				inv, token, kim := invite("kim@example.com")
				rtStamp(t, "an invitation's created_at", stored("org_invitations", "created_at", inv.ID), inv.CreatedAt, before, time.Now())

				// The invite handler stamps a delivered link with its own
				// time.Now(), and holds back a resend for an hour after it.
				start()
				if err := svc.MarkEmailed(inv.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an emailed link's last_emailed_at", stored("org_invitations", "last_emailed_at", inv.ID), time.Time{},
					before, time.Now())
				found, err := svc.FindPending(org, "kim@example.com")
				if err != nil || found == nil || found.LastEmailedAt == nil {
					t.Fatalf("FindPending: %+v, %v", found, err)
				}
				if since := time.Since(*found.LastEmailedAt); since < 0 || since > time.Minute {
					t.Errorf("a link emailed a moment ago reads as emailed %s ago, want a moment", since)
				}

				expiresIn(inv.ID, time.Hour)
				start()
				accepted, err := svc.AcceptTokenForEmail(token, "kim@example.com", kim)
				if err != nil {
					t.Fatalf("an invitation with an hour to run: %v, want it accepted", err)
				}
				rtStamp(t, "an accepted invitation's accepted_at", stored("org_invitations", "accepted_at", inv.ID),
					*accepted.Invitation.AcceptedAt, before, time.Now())

				inv, _, max := invite("max@example.com")
				expiresIn(inv.ID, time.Hour)
				if all, err := svc.AcceptAllForProviderVerifiedEmail("max@example.com", max); err != nil || len(all) != 1 {
					t.Errorf("an invitation with an hour to run, at a provider-verified sign-in: %d accepted (%v), want 1", len(all), err)
				}

				inv, token, lee := invite("lee@example.com")
				looked, err := svc.Lookup(token)
				if err != nil {
					t.Fatal(err)
				}
				expiresIn(inv.ID, -time.Minute)
				if _, err := svc.AcceptResolvedForEmail(looked, "lee@example.com", lee); !errors.Is(err, invitations.ErrInvalidToken) {
					t.Errorf("an invitation that expired a minute ago, after it was looked up: %v, want ErrInvalidToken", err)
				}
				if role, err := orgService.RoleInOrg(org, lee); err != nil || role != "" {
					t.Errorf("the expired invitation's invitee holds role %q (%v), want none", role, err)
				}
			})

			t.Run("workspace deletion", func(t *testing.T) {
				const grace = orgs.DeletionGraceDays * 24 * time.Hour
				// deleted soft-deletes a new workspace through DeleteOrg, its
				// deleted_at then moved back by ago, as time passing would.
				deleted := func(ago time.Duration) string {
					t.Helper()
					id := uuid.New().String()
					rtSeedOrg(t, db, id)
					start()
					got, err := orgService.DeleteOrg(id)
					if err != nil {
						t.Fatal(err)
					}
					rtStamp(t, "a deleted workspace's deleted_at", stored("organizations", "deleted_at", id), *got.DeletedAt,
						before, time.Now())
					rtStamp(t, "a deleted workspace's updated_at", stored("organizations", "updated_at", id), time.Time{},
						before, time.Now())
					rtSeed(t, db, `UPDATE organizations SET deleted_at = deleted_at - make_interval(secs => $2) WHERE id = $1`,
						id, ago.Seconds())
					return id
				}
				// deletedInUTC is a workspace whose deleted_at a server in UTC
				// stamped ago.
				deletedInUTC := func(ago time.Duration) string {
					t.Helper()
					id := uuid.New().String()
					rtSeedOrg(t, db, id)
					rtSeed(t, db, `UPDATE organizations SET deleted_at = $2 WHERE id = $1`, id, time.Now().UTC().Add(-ago))
					return id
				}
				kept, purged := deleted(grace-time.Minute), deleted(grace+time.Minute)
				keptUTC, purgedUTC := deletedInUTC(grace-time.Minute), deletedInUTC(grace+time.Minute)
				// The purge loop hands in its own time.Now().
				result, err := orgService.PurgeExpired(time.Now())
				if err != nil {
					t.Fatal(err)
				}
				ids := result.IDs
				sort.Strings(ids)
				want := []string{purged, purgedUTC}
				sort.Strings(want)
				if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
					t.Errorf("the purge purged %v, want the workspaces deleted the grace period and a minute ago, %v "+
						"(and not %s or %s, deleted a minute less than it ago)", ids, want, kept, keptUTC)
				}
			})
		})
	}
}
