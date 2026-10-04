package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/templates"
)

// Workspaces, people-teams, product profiles, templates, the activity log,
// figures and push subscriptions stamp the times they hand the database in
// UTC (#379 bug 162, the class of bug 93 that TestTheServicesStampTimesInUTC
// pins for other services). Their columns are TIMESTAMP: Postgres keeps the
// wall clock lib/pq sends and drops its offset, and every read takes that
// wall clock as UTC, so a local time.Now() was shown hours off: two hours
// east of UTC a workspace created at 09:00 UTC read as created at 11:00 UTC,
// two hours in the future. Each service is driven through its real
// repositories with time.Local two hours east of UTC and then five hours
// west: each stamp is stored as the UTC wall clock of the moment it was
// taken, and each time a service answers is the instant a later read
// answers.
func TestTheWorkspaceContentStampsTimesInUTC(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := utsDB(t)
			arsInZone(t, zone)
			org, project, user := uuid.New().String(), uuid.New().String(), uuid.New().String()
			rtSeedOrg(t, db, org)
			rtSeedProject(t, db, project, org, "Lathe")
			rtSeedUser(t, db, user, "dana@example.com", "Dana", "")

			var before time.Time
			start := func() { before = time.Now() }
			// stored reads one TIMESTAMP column of the row with id.
			stored := func(table, column, id string) time.Time {
				t.Helper()
				return utsAt(t, db, `SELECT `+column+` FROM `+table+` WHERE id = $1`, id)
			}
			artifactSvc := artifacts.NewDefaultService(NewArtifactRepository(db))
			linkSvc := links.NewDefaultService(NewLinkRepository(db))
			linkSvc.SetArtifactService(artifactSvc)

			t.Run("workspaces and people-teams", func(t *testing.T) {
				orgSvc := orgs.NewDefaultService(NewOrgRepository(db))
				start()
				created, err := orgSvc.CreateOrg("Machine shop", orgs.TypeCompany, user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a new workspace's created_at", stored("organizations", "created_at", created.ID), created.CreatedAt,
					before, time.Now())
				rtStamp(t, "a new workspace's updated_at", stored("organizations", "updated_at", created.ID), created.UpdatedAt,
					before, time.Now())
				start()
				name := "Machine shop north"
				renamed, err := orgSvc.UpdateOrg(created.ID, &name)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a renamed workspace's updated_at", stored("organizations", "updated_at", created.ID), renamed.UpdatedAt,
					before, time.Now())

				teamSvc := orgs.NewTeamService(NewOrgRepository(db), orgSvc)
				start()
				team, err := teamSvc.CreateTeam(created.ID, "Setters", "", &user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a new people-team's created_at", stored("org_teams", "created_at", team.ID), team.CreatedAt, before, time.Now())
				rtStamp(t, "a new people-team's updated_at", stored("org_teams", "updated_at", team.ID), team.UpdatedAt, before, time.Now())
				start()
				teamName := "Setters and operators"
				updated, err := teamSvc.UpdateTeam(team.ID, &teamName, nil)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an edited people-team's updated_at", stored("org_teams", "updated_at", team.ID), updated.UpdatedAt,
					before, time.Now())
			})

			t.Run("product profile", func(t *testing.T) {
				svc := products.NewDefaultService(NewProductProfileRepository(db))
				profileAt := func(column string) time.Time {
					t.Helper()
					return utsAt(t, db, `SELECT `+column+` FROM product_profiles WHERE project_id = $1`, project)
				}
				start()
				profile, err := svc.GetProfile(project)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a new product profile's created_at", profileAt("created_at"), profile.CreatedAt, before, time.Now())
				rtStamp(t, "a new product profile's updated_at", profileAt("updated_at"), profile.UpdatedAt, before, time.Now())
				start()
				updated, err := svc.UpdateProfile(project, products.UpdateProfileRequest{Vision: "Turn brass in one setup"})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an edited product profile's updated_at", profileAt("updated_at"), updated.UpdatedAt, before, time.Now())
			})

			t.Run("templates", func(t *testing.T) {
				exportSvc := exports.NewService(artifactSvc, linkSvc, attachments.NewDefaultService(NewAttachmentRepository(db)),
					NewProjectInfoRepository(db), projects.NewService(NewProjectRepository(db)))
				svc := templates.NewService(NewTemplateRepository(db), exportSvc)
				start()
				if err := svc.SeedDefaults(); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a bundled template's created_at",
					utsAt(t, db, `SELECT created_at FROM templates WHERE template_key = 'guided-product-skeleton'`), time.Time{},
					before, time.Now())
				start()
				tpl, err := svc.CreateTemplateFromProject(project, "Lathe starter", "", org)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a project's template's created_at", stored("templates", "created_at", tpl.ID), tpl.CreatedAt, before, time.Now())
			})

			t.Run("activity log", func(t *testing.T) {
				start()
				e := events.New(events.TestRunRecorded, project, uuid.New().String(), "user:"+user, nil)
				e.OrgID = org
				if err := NewEventRepository(db).Save(e); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an event's created_at", stored("domain_events", "created_at", e.ID), e.CreatedAt, before, time.Now())
			})

			t.Run("figures", func(t *testing.T) {
				a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: artifacts.TypeRequirement, Title: "Chuck"})
				if err := artifactSvc.CreateArtifact(a); err != nil {
					t.Fatal(err)
				}
				svc := attachments.NewDefaultService(NewAttachmentRepository(db))
				versionAt := func(id string, v int) time.Time {
					t.Helper()
					return utsAt(t, db, `SELECT created_at FROM attachment_versions WHERE attachment_id = $1 AND version = $2`, id, v)
				}
				start()
				figure := attachments.NewAttachment(attachments.CreateAttachmentRequest{ArtifactID: a.ID, Filename: "chuck.png",
					OriginalFilename: "chuck.png", Title: "Chuck", MimeType: "image/png", FilePath: "/uploads/chuck.png", FileSize: 10})
				if err := svc.CreateFigure(figure, a.Ref); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a new figure's created_at", stored("attachments", "created_at", figure.ID), figure.CreatedAt, before, time.Now())
				rtStamp(t, "a new figure's first version's created_at", versionAt(figure.ID, 1), figure.CreatedAt, before, time.Now())

				start()
				next, err := svc.AddVersion(figure.ID, &attachments.Version{Filename: "chuck-2.png", OriginalFilename: "chuck-2.png",
					MimeType: "image/png", FilePath: "/uploads/chuck-2.png", FileSize: 11})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an uploaded version's created_at", versionAt(figure.ID, next), time.Time{}, before, time.Now())

				start()
				renamed, err := svc.RenameFigure(figure.ID, "Chuck, side view", &user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a rename's created_at", versionAt(figure.ID, renamed), time.Time{}, before, time.Now())

				start()
				restored, err := svc.RestoreVersion(figure.ID, 1, &user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a restore's created_at", versionAt(figure.ID, restored.Version), restored.CreatedAt, before, time.Now())
			})

			t.Run("push subscriptions", func(t *testing.T) {
				svc := pushsubs.NewDefaultService(NewPushSubscriptionRepository(db))
				start()
				sub := pushsubs.New(user, "https://push.example.com/dana", "p256dh-key", "auth-secret", "Firefox")
				if err := svc.Subscribe(sub); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a push subscription's created_at", stored("push_subscriptions", "created_at", sub.ID), sub.CreatedAt,
					before, time.Now())
			})
		})
	}
}
