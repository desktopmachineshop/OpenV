package postgres

import (
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// utsDB is a fresh database with the production schema, opened through
// Connect as the server opens it, so that the SQL's NOW() is UTC (#379 bug
// 156) whatever the test server's own zone: the stamps of #379 bug 162 sit
// beside NOW() stamps (a removed link's valid_to, a claimed sign-in's
// updated_at), and must agree with them as the server's do.
func utsDB(t *testing.T) *sql.DB {
	t.Helper()
	plain := rtDB(t)
	var database string
	if err := plain.QueryRow(`SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv(TestDatabaseURLEnv))
	if err != nil {
		t.Fatalf("parse %s: %v", TestDatabaseURLEnv, err)
	}
	u.Path = "/" + database
	db, err := Connect(u.String())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// utsAt reads the one time query answers: a TIMESTAMP column of one row.
func utsAt(t *testing.T, db *sql.DB, query string, args ...interface{}) time.Time {
	t.Helper()
	var at time.Time
	if err := db.QueryRow(query, args...).Scan(&at); err != nil {
		t.Fatalf("%s %v: %v", query, args, err)
	}
	return at
}

// The artifacts, links, baselines, chatter and test runs stamp the times
// they hand the database in UTC (#379 bug 162, the class of bug 93 that
// TestTheServicesStampTimesInUTC pins for other services). Their columns are
// TIMESTAMP: Postgres keeps the wall clock lib/pq sends and drops its
// offset, and every read takes that wall clock as UTC. Each service is
// driven through its real repositories with time.Local two hours east of UTC
// and then five hours west: each stamp is stored as the UTC wall clock of
// the moment it was taken, each time a service answers is the instant a
// later read answers, and
//   - a removed link's history runs from when it was made to when it was
//     removed: its valid_from is the service's stamp and its valid_to the
//     database's NOW(), which is UTC, so east of UTC the link was removed two
//     hours before it began, and west it began five hours before it was made;
//   - a test case's latest result is the one recorded a moment ago, not one
//     a server in UTC recorded an hour before, in another run (west, the
//     result recorded now read as five hours old and lost).
func TestTheRequirementsStampTimesInUTC(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := utsDB(t)
			arsInZone(t, zone)
			org, project := uuid.New().String(), uuid.New().String()
			rtSeedOrg(t, db, org)
			rtSeedProject(t, db, project, org, "Rig")

			var before time.Time
			start := func() { before = time.Now() }
			artifactSvc := artifacts.NewDefaultService(NewArtifactRepository(db))
			linkSvc := links.NewDefaultService(NewLinkRepository(db))
			linkSvc.SetArtifactService(artifactSvc)
			artifactSvc.SetLinkSuspector(linkSvc)
			chatterSvc := chatter.NewDefaultService(NewChatterRepository(db))
			create := func(typ, title string) *artifacts.Artifact {
				t.Helper()
				a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: typ, Title: title})
				if err := artifactSvc.CreateArtifact(a); err != nil {
					t.Fatal(err)
				}
				return a
			}
			// version reads one TIMESTAMP column of one version of an artifact.
			version := func(id string, v int, column string) time.Time {
				t.Helper()
				return utsAt(t, db, `SELECT `+column+` FROM artifacts WHERE id = $1 AND version = $2`, id, v)
			}

			t.Run("artifacts", func(t *testing.T) {
				start()
				a := create(artifacts.TypeRequirement, "Spindle speed")
				rtStamp(t, "a new artifact's valid_from", version(a.ID, 1, "valid_from"), a.ValidFrom, before, time.Now())
				rtStamp(t, "a new artifact's created_at", version(a.ID, 1, "created_at"), a.CreatedAt, before, time.Now())
				rtStamp(t, "a new artifact's updated_at", version(a.ID, 1, "updated_at"), a.UpdatedAt, before, time.Now())

				start()
				title := "Spindle speed range"
				edited, err := artifactSvc.UpdateArtifact(a.ID, artifacts.UpdateArtifactRequest{Title: &title})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an edit's valid_from", version(a.ID, 2, "valid_from"), edited.ValidFrom, before, time.Now())
				rtStamp(t, "an edit's updated_at", version(a.ID, 2, "updated_at"), edited.UpdatedAt, before, time.Now())
				rtStamp(t, "the edited version's valid_to", version(a.ID, 1, "valid_to"), edited.ValidFrom, before, time.Now())

				start()
				reviewed, err := artifactSvc.ChangeStatus(a.ID, artifacts.StatusInReview)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a status change's valid_from", version(a.ID, 3, "valid_from"), reviewed.ValidFrom, before, time.Now())
				rtStamp(t, "a status change's updated_at", version(a.ID, 3, "updated_at"), reviewed.UpdatedAt, before, time.Now())

				start()
				restored, err := artifactSvc.RestoreArtifactVersion(a.ID, 1)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a restore's valid_from", version(a.ID, 4, "valid_from"), restored.ValidFrom, before, time.Now())
				rtStamp(t, "a restore's updated_at", version(a.ID, 4, "updated_at"), restored.UpdatedAt, before, time.Now())

				draft := create(artifacts.TypeRequirement, "Coolant flow")
				start()
				round, err := artifactSvc.StartProjectReview(project, artifacts.ReviewRoundRequest{Types: []string{artifacts.TypeRequirement}})
				if err != nil {
					t.Fatal(err)
				}
				var moved *artifacts.Artifact
				for _, m := range round.Moved {
					if m.ID == draft.ID {
						moved = m
					}
				}
				if moved == nil {
					t.Fatalf("the review round moved %d artifacts, not the draft %s", len(round.Moved), draft.ID)
				}
				rtStamp(t, "a review round's valid_from", version(draft.ID, 2, "valid_from"), moved.ValidFrom, before, time.Now())
				rtStamp(t, "a review round's updated_at", version(draft.ID, 2, "updated_at"), moved.UpdatedAt, before, time.Now())

				start()
				if err := artifactSvc.DeleteArtifact(a.ID); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a deleted artifact's valid_to", version(a.ID, 4, "valid_to"), time.Time{}, before, time.Now())
			})

			t.Run("links", func(t *testing.T) {
				from := create(artifacts.TypeRequirement, "Feed rate")
				to := create(artifacts.TypeTestCase, "Feed rate check")
				start()
				link := links.NewLink(links.CreateLinkRequest{FromID: from.ID, ToID: to.ID, Type: "verifies"})
				if err := linkSvc.CreateLink(link); err != nil {
					t.Fatal(err)
				}
				made, madeBy := before, time.Now()
				stored := func(column string) time.Time {
					t.Helper()
					return utsAt(t, db, `SELECT `+column+` FROM links WHERE id = $1`, link.ID)
				}
				rtStamp(t, "a new link's valid_from", stored("valid_from"), link.ValidFrom, made, madeBy)
				rtStamp(t, "a new link's created_at", stored("created_at"), link.CreatedAt, made, madeBy)
				rtStamp(t, "a new link's updated_at", stored("updated_at"), link.UpdatedAt, made, madeBy)

				start()
				updated, err := linkSvc.UpdateLink(link.ID, links.UpdateLinkRequest{Type: "verifies",
					Attributes: map[string]interface{}{"note": "on the bench"}})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an updated link's updated_at", stored("updated_at"), updated.UpdatedAt, before, time.Now())

				// The repository closes the link with valid_to = NOW().
				start()
				if err := linkSvc.DeleteLink(link.ID); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a removed link's valid_to", stored("valid_to"), time.Time{}, before, time.Now())
				history, err := linkSvc.GetLinksForArtifactVersion(from.ID, from.Version)
				if err != nil || len(history) != 1 || history[0].ValidTo == nil {
					t.Fatalf("the links of %s at version %d: %s, %v; want the removed link, with its validity", from.ID,
						from.Version, rtJSON(history), err)
				}
				h := history[0]
				if h.ValidTo.Before(h.ValidFrom) {
					t.Errorf("the removed link's history ends at %s, before it begins, at %s",
						h.ValidTo.Format(time.RFC3339Nano), h.ValidFrom.Format(time.RFC3339Nano))
				}
				rtStamp(t, "the removed link's history's valid_from", h.ValidFrom, link.ValidFrom, made, madeBy)
			})

			t.Run("baselines", func(t *testing.T) {
				svc := baselines.NewService(NewBaselineRepository(db))
				start()
				b, err := svc.CreateBaseline(project, "Design review", []byte(`{"artifacts":[]}`), nil)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a baseline's created_at", utsAt(t, db, `SELECT created_at FROM baselines WHERE id = $1`, b.ID),
					b.CreatedAt, before, time.Now())
			})

			t.Run("chatter", func(t *testing.T) {
				subject := create(artifacts.TypeRequirement, "Chip load")
				start()
				entry := chatter.NewChatterEntry(subject.ID, "Checked on the bench", false, "comment")
				if err := chatterSvc.CreateEntry(entry); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a note's created_at", utsAt(t, db, `SELECT created_at FROM chatter WHERE id = $1`, entry.ID),
					entry.CreatedAt, before, time.Now())
				rtStamp(t, "a note's updated_at", utsAt(t, db, `SELECT updated_at FROM chatter WHERE id = $1`, entry.ID),
					entry.UpdatedAt, before, time.Now())
			})

			t.Run("test runs", func(t *testing.T) {
				svc := vv.NewDefaultService(NewVVRepository(db), artifactSvc, chatterSvc, nil)
				testCase := create(artifacts.TypeTestCase, "Spindle runout")
				start()
				run, err := svc.CreateRun(vv.CreateRunRequest{ProjectID: project, Name: "Bench"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				runAt := func(column string) time.Time {
					t.Helper()
					return utsAt(t, db, `SELECT `+column+` FROM test_runs WHERE id = $1`, run.ID)
				}
				rtStamp(t, "a new run's started_at", runAt("started_at"), run.StartedAt, before, time.Now())
				rtStamp(t, "a new run's created_at", runAt("created_at"), run.CreatedAt, before, time.Now())
				rtStamp(t, "a new run's updated_at", runAt("updated_at"), run.UpdatedAt, before, time.Now())

				// A result a server in UTC recorded an hour ago, in a run of its
				// own.
				earlierRun, earlierResult := uuid.New().String(), uuid.New().String()
				hourAgo := time.Now().UTC().Add(-time.Hour)
				rtSeed(t, db, `INSERT INTO test_runs (id, project_id, name, status, started_at, created_at, updated_at)
					VALUES ($1, $2, 'Earlier', 'completed', $3, $3, $3)`, earlierRun, project, hourAgo)
				rtSeed(t, db, `INSERT INTO test_results (id, run_id, test_case_id, test_case_version, status, executed_at, created_at, updated_at)
					VALUES ($1, $2, $3, 1, 'fail', $4, $4, $4)`, earlierResult, earlierRun, testCase.ID, hourAgo)

				start()
				result, err := svc.UpsertResult(run.ID, vv.UpsertResultRequest{TestCaseID: testCase.ID, Status: vv.ResultPass}, nil, "", "")
				if err != nil {
					t.Fatal(err)
				}
				resultAt := func(column string) time.Time {
					t.Helper()
					return utsAt(t, db, `SELECT `+column+` FROM test_results WHERE id = $1`, result.ID)
				}
				rtStamp(t, "a result's executed_at", resultAt("executed_at"), *result.ExecutedAt, before, time.Now())
				rtStamp(t, "a result's created_at", resultAt("created_at"), result.CreatedAt, before, time.Now())
				rtStamp(t, "a result's updated_at", resultAt("updated_at"), result.UpdatedAt, before, time.Now())
				latest, err := svc.LatestResults(project)
				if err != nil {
					t.Fatal(err)
				}
				if got := latest[testCase.ID]; got == nil || got.ID != result.ID {
					t.Errorf("the test case's latest result is %s, want the one recorded a moment ago (%s), not the one a server "+
						"in UTC recorded an hour before (%s)", rtJSON(got), result.ID, earlierResult)
				}

				start()
				closed, err := svc.UpdateRunStatus(run.ID, vv.RunStatusCompleted)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a completed run's completed_at", runAt("completed_at"), *closed.CompletedAt, before, time.Now())
				rtStamp(t, "a completed run's updated_at", runAt("updated_at"), closed.UpdatedAt, before, time.Now())
			})
		})
	}
}
