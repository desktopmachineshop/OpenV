package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// Guided sessions, interviews, agent proposals, provider settings and
// sign-ins, and repository connections stamp the times they hand the
// database in UTC (#379 bug 162, the class of bug 93 that
// TestTheServicesStampTimesInUTC pins for other services). Their columns are
// TIMESTAMP: Postgres keeps the wall clock lib/pq sends and drops its
// offset, and every read takes that wall clock as UTC. Each service is
// driven through its real repositories with time.Local two hours east of UTC
// and then five hours west: each stamp is stored as the UTC wall clock of
// the moment it was taken, each time a service answers is the instant a
// later read answers, and a CLI sign-in request (whose claim stamps
// updated_at with NOW(), in UTC) is handed out again while it is fresh and
// abandoned and replaced once it has gone LoginStaleAfter without progress
// (west, a request made a moment ago read as five hours old and was
// abandoned at once; east, one silent for longer was handed out for two
// hours more).
func TestTheAgentSuiteRecordsStampTimesInUTC(t *testing.T) {
	for _, zone := range []*time.Location{rtCEST, arsWest} {
		t.Run(zone.String(), func(t *testing.T) {
			db := utsDB(t)
			arsInZone(t, zone)
			org, project, user := uuid.New().String(), uuid.New().String(), uuid.New().String()
			rtSeedOrg(t, db, org)
			rtSeedProject(t, db, project, org, "Mill")
			rtSeedUser(t, db, user, "dana@example.com", "Dana", "")

			var before time.Time
			start := func() { before = time.Now() }
			// stored reads one TIMESTAMP column of the row with id.
			stored := func(table, column, id string) time.Time {
				t.Helper()
				return utsAt(t, db, `SELECT `+column+` FROM `+table+` WHERE id = $1`, id)
			}

			t.Run("guided sessions", func(t *testing.T) {
				artifactSvc := artifacts.NewDefaultService(NewArtifactRepository(db))
				linkSvc := links.NewDefaultService(NewLinkRepository(db))
				linkSvc.SetArtifactService(artifactSvc)
				svc := guided.NewDefaultService(NewGuidedRepository(db), artifactSvc, linkSvc,
					chatter.NewDefaultService(NewChatterRepository(db)), products.NewDefaultService(NewProductProfileRepository(db)), nil)

				start()
				s, err := svc.StartSession(project, &user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a new session's created_at", stored("guided_sessions", "created_at", s.ID), s.CreatedAt, before, time.Now())
				rtStamp(t, "a new session's updated_at", stored("guided_sessions", "updated_at", s.ID), s.UpdatedAt, before, time.Now())

				start()
				saved, err := svc.SaveStep(s.ID, 1, map[string]interface{}{"step_1": "Mill aluminium"})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a saved step's updated_at", stored("guided_sessions", "updated_at", s.ID), saved.UpdatedAt, before, time.Now())

				start()
				drafted, err := svc.MaterializeDrafts(s.ID, []guided.DraftSpec{{Type: artifacts.TypeRequirement, Title: "Cut aluminium"}})
				if err != nil || len(drafted) != 1 {
					t.Fatalf("MaterializeDrafts: %v, %v", drafted, err)
				}
				rtStamp(t, "a session's updated_at after its drafts", stored("guided_sessions", "updated_at", s.ID), time.Time{},
					before, time.Now())

				start()
				message, err := svc.AppendChatMessage(s.ID, guided.ChatRoleUser, "What does the spindle need?")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a copilot message's created_at", stored("guided_session_messages", "created_at", message.ID),
					message.CreatedAt, before, time.Now())

				start()
				if err := svc.AttachAgentRun(s.ID, uuid.New().String()); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a session's updated_at after a copilot turn", stored("guided_sessions", "updated_at", s.ID), time.Time{},
					before, time.Now())

				start()
				committed, err := svc.Commit(s.ID)
				if err != nil || committed.Session == nil {
					t.Fatalf("Commit: %+v, %v", committed, err)
				}
				rtStamp(t, "a committed session's updated_at", stored("guided_sessions", "updated_at", s.ID),
					committed.Session.UpdatedAt, before, time.Now())

				other, err := svc.StartSession(project, &user)
				if err != nil {
					t.Fatal(err)
				}
				start()
				abandoned, err := svc.Abandon(other.ID)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an abandoned session's updated_at", stored("guided_sessions", "updated_at", other.ID),
					abandoned.UpdatedAt, before, time.Now())
			})

			t.Run("interviews", func(t *testing.T) {
				svc := interviews.NewDefaultService(NewInterviewRepository(db))
				start()
				iv, err := svc.CreateInterview(project, "Operators", "How they set up a job", nil, nil, nil, &user)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a new interview's created_at", stored("interviews", "created_at", iv.ID), iv.CreatedAt, before, time.Now())
				rtStamp(t, "a new interview's updated_at", stored("interviews", "updated_at", iv.ID), iv.UpdatedAt, before, time.Now())

				start()
				persona, err := svc.SetInterviewPersona(iv.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a persona change's updated_at", stored("interviews", "updated_at", iv.ID), persona.UpdatedAt, before, time.Now())

				start()
				invite, _, err := svc.CreateInvite(iv.ID, "Shop floor", nil)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an invite's created_at", stored("interview_invites", "created_at", invite.ID), invite.CreatedAt,
					before, time.Now())

				start()
				session, err := svc.StartOrResumeSession(invite.ID, iv.ID, "Sam")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an interview session's started_at", stored("interview_sessions", "started_at", session.ID),
					session.StartedAt, before, time.Now())

				start()
				message, err := svc.AppendMessage(session.ID, interviews.RoleParticipant, "I zero the vice first.")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an interview message's created_at", stored("interview_messages", "created_at", message.ID),
					message.CreatedAt, before, time.Now())

				start()
				if err := svc.CompleteSession(session.ID, "Zeroes the vice first."); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a completed session's ended_at", stored("interview_sessions", "ended_at", session.ID), time.Time{},
					before, time.Now())

				start()
				closed, err := svc.CloseInterview(iv.ID)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a closed interview's updated_at", stored("interviews", "updated_at", iv.ID), closed.UpdatedAt, before, time.Now())
			})

			t.Run("proposals", func(t *testing.T) {
				agent, run := uuid.New().String(), uuid.New().String()
				rtSeed(t, db, `INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'drafter', 'Drafter', 'claude')`, agent, org)
				rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, run_token_hash) VALUES ($1, $2, $3, $4, 'Work', 'hash')`,
					run, agent, org, project)
				svc := proposals.NewDefaultService(NewProposalRepository(db), proposals.Appliers{
					CreateArtifact: func(map[string]interface{}) (string, error) { return uuid.New().String(), nil },
				})
				propose := func(title string) *proposals.Proposal {
					t.Helper()
					p, err := svc.Propose(run, project, proposals.OpCreateArtifact, nil, map[string]interface{}{"title": title})
					if err != nil {
						t.Fatal(err)
					}
					return p
				}

				start()
				rejected := propose("Coolant")
				rtStamp(t, "a proposal's created_at", stored("agent_proposals", "created_at", rejected.ID), rejected.CreatedAt,
					before, time.Now())
				start()
				if rejected, err := svc.Reject(rejected.ID, &user, "Not yet"); err != nil {
					t.Fatal(err)
				} else {
					rtStamp(t, "a rejected proposal's reviewed_at", stored("agent_proposals", "reviewed_at", rejected.ID),
						*rejected.ReviewedAt, before, time.Now())
				}

				approved := propose("Spindle")
				start()
				if approved, err := svc.Approve(approved.ID, &user, ""); err != nil {
					t.Fatal(err)
				} else {
					rtStamp(t, "an approved proposal's reviewed_at", stored("agent_proposals", "reviewed_at", approved.ID),
						*approved.ReviewedAt, before, time.Now())
				}
			})

			t.Run("provider settings and sign-ins", func(t *testing.T) {
				settings := providers.NewDefaultService(NewProviderSettingRepository(db))
				start()
				setting := &providers.ProviderSetting{OrgID: org, Provider: providers.ProviderClaudeCode,
					AuthMode: providers.AuthSubscriptionCLI, Enabled: true}
				if err := settings.Upsert(setting); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a provider setting's updated_at", stored("provider_settings", "updated_at", setting.ID), setting.UpdatedAt,
					before, time.Now())
				start()
				if err := settings.RecordDetection(org, providers.ProviderClaudeCode, map[string]interface{}{"installed": true}); err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a detection's updated_at", stored("provider_settings", "updated_at", setting.ID), time.Time{},
					before, time.Now())

				logins := providers.NewLoginService(NewProviderLoginRepository(db))
				startLogin := func() *providers.LoginRequest {
					t.Helper()
					l, err := logins.StartLogin(org, providers.ProviderClaudeCode, providers.LoginTargetWorkspace, &user)
					if err != nil {
						t.Fatal(err)
					}
					return l
				}
				start()
				first := startLogin()
				rtStamp(t, "a sign-in request's created_at", stored("provider_logins", "created_at", first.ID), first.CreatedAt,
					before, time.Now())
				rtStamp(t, "a sign-in request's updated_at", stored("provider_logins", "updated_at", first.ID), first.UpdatedAt,
					before, time.Now())
				if again := startLogin(); again.ID != first.ID {
					t.Errorf("a sign-in requested a moment ago was abandoned and replaced by %s, want it handed out again", again.ID)
				}

				// The runner's claim stamps updated_at with NOW().
				if claimed, err := logins.Claim(org, user); err != nil || claimed == nil || claimed.ID != first.ID {
					t.Fatalf("Claim: %+v, %v; want %s", claimed, err, first.ID)
				}
				start()
				progressed, err := logins.Progress(first.ID, providers.LoginURLReady, "https://example.com/auth", "", "")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a runner's progress's updated_at", stored("provider_logins", "updated_at", first.ID), progressed.UpdatedAt,
					before, time.Now())
				start()
				coded, err := logins.SubmitCode(first.ID, "pasted-code")
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a pasted code's updated_at", stored("provider_logins", "updated_at", first.ID), coded.UpdatedAt,
					before, time.Now())
				if again := startLogin(); again.ID != first.ID {
					t.Errorf("a sign-in that made progress a moment ago was abandoned and replaced by %s, want it handed out again", again.ID)
				}

				// Silent for a minute more than LoginStaleAfter, as time
				// passing would leave it.
				rtSeed(t, db, `UPDATE provider_logins SET updated_at = updated_at - make_interval(secs => $2) WHERE id = $1`,
					first.ID, (providers.LoginStaleAfter + time.Minute).Seconds())
				start()
				replaced := startLogin()
				if replaced.ID == first.ID {
					t.Errorf("a sign-in silent for %s was handed out again, want it abandoned and replaced",
						providers.LoginStaleAfter+time.Minute)
				}
				rtStamp(t, "an abandoned sign-in's updated_at", stored("provider_logins", "updated_at", first.ID), time.Time{},
					before, time.Now())
				start()
				cancelled, err := logins.Cancel(replaced.ID)
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a cancelled sign-in's updated_at", stored("provider_logins", "updated_at", replaced.ID), cancelled.UpdatedAt,
					before, time.Now())
			})

			t.Run("repository connections", func(t *testing.T) {
				svc := repoconns.NewDefaultService(NewRepoConnectionRepository(db))
				start()
				c, err := svc.Create(repoconns.CreateRequest{ProjectID: project, Name: "Firmware", RemoteURL: "https://example.com/firmware.git"})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "a repository connection's created_at", stored("repo_connections", "created_at", c.ID), c.CreatedAt,
					before, time.Now())
				rtStamp(t, "a repository connection's updated_at", stored("repo_connections", "updated_at", c.ID), c.UpdatedAt,
					before, time.Now())
				start()
				name := "Controller firmware"
				updated, err := svc.Update(c.ID, repoconns.UpdateRequest{Name: &name})
				if err != nil {
					t.Fatal(err)
				}
				rtStamp(t, "an edited repository connection's updated_at", stored("repo_connections", "updated_at", c.ID),
					updated.UpdatedAt, before, time.Now())
			})
		})
	}
}
