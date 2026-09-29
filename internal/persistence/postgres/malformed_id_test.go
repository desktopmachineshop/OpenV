package postgres

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
)

// A route takes whatever text a caller puts where it expects an id. Text
// Postgres refuses as a uuid used to fail the query, which the handlers
// answered 500; a repository now answers it exactly as it answers a
// well-formed id no row has (ids.go). Postgres-gated
// (OPENV_TEST_DATABASE_URL).

const malformed = "not-a-uuid"

// malformedIDs are the kinds of text that is no UUID: text Postgres reads and
// refuses as a uuid (SQLSTATE 22P02), and text it refuses before reading any
// type, a byte that is not UTF-8 (a path's %FF, a query's too) or a NUL (a
// JSON body's \u0000), both SQLSTATE 22021.
var malformedIDs = []string{malformed, "\xff", "a\x00b", "a\xc3"}

// isUUID must read text as Postgres's own uuid input does: uuidsOnly and the
// crew list's filter decide with it which ids can match a row.
func TestIsUUIDReadsTextAsPostgresDoes(t *testing.T) {
	db := testDB(t)
	for _, s := range append([]string{
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11",
		"{a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11}",
		"a0eebc999c0b4ef8bb6d6bb9bd380a11",
		"a0ee-bc99-9c0b-4ef8-bb6d-6bb9-bd38-0a11",
		"{a0eebc99-9c0b4ef8-bb6d6bb9-bd380a11}",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a1",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a111",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11-",
		"-a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"a0eebc99--9c0b-4ef8-bb6d-6bb9bd380a11",
		"a0eeb-c999c0b4ef8bb6d6bb9bd380a11",
		"{a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11}",
		" a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11 ",
		"urn:uuid:a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"g0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"{}", "{", "}", "", "-", "00000000-0000-0000-0000-000000000000",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a1\xff", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11\x00",
	}, malformedIDs...) {
		_, err := db.Exec(`SELECT $1::text::uuid`, s)
		if pg := err == nil; pg != isUUID(s) {
			t.Errorf("isUUID(%q) = %v, but Postgres reads it as a uuid: %v (%v)", s, isUUID(s), pg, err)
		}
	}
}

// Each read and write a route reaches with an id of its path, query or body
// answers a malformed id, of each kind, exactly as a well-formed id no row
// has: the same value and the same error, never the driver's refusal.
func TestAMalformedIDAnswersAsAnIDNoRowHas(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	phantom := uuid.New().String()

	projects := NewProjectRepository(db)
	orgRepo := NewOrgRepository(db)
	memberRepo := NewMemberRepository(db)
	userRepo := NewUserRepository(db)
	templateRepo := NewTemplateRepository(db)
	attributeRepo := NewAttributeDefinitionRepository(db)
	evidenceRepo := NewEvidenceRepository(db)
	sharedRepo := NewSharedProductRepository(db)
	runnerRepo := NewRunnerSessionRepository(db)
	automationRepo := NewAutomationRepository(db)
	proposalRepo := NewProposalRepository(db)
	runRepo := NewAgentRunRepository(db)
	notificationRepo := NewNotificationRepository(db)
	workerKeyRepo := NewWorkerKeyRepository(db)
	invitationRepo := NewInvitationRepository(db)
	artifactRepo := NewArtifactRepository(db)

	reads := []struct {
		name string
		read func(id string) (interface{}, error)
	}{
		{"a project", func(id string) (interface{}, error) { return projects.GetByID(id) }},
		{"a workspace", func(id string) (interface{}, error) { return orgRepo.FindOrgByID(id) }},
		{"a role in a workspace", func(id string) (interface{}, error) { return orgRepo.MemberRole(id, phantom) }},
		{"a role in a deleted workspace", func(id string) (interface{}, error) { return orgRepo.MemberRoleAny(id, phantom) }},
		{"the roles in a project", func(id string) (interface{}, error) { return memberRepo.RolesFor(id, phantom) }},
		{"a membership", func(id string) (interface{}, error) { return memberRepo.Find(phantom, id) }},
		{"a membership's removal", func(id string) (interface{}, error) { return nil, memberRepo.Remove(phantom, id) }},
		{"a team grant's removal", func(id string) (interface{}, error) { return nil, memberRepo.RemoveTeamGrant(phantom, id) }},
		{"an account", func(id string) (interface{}, error) { return userRepo.FindUserByID(id) }},
		{"a template", func(id string) (interface{}, error) { return templateRepo.GetByID(id) }},
		{"an attribute definition", func(id string) (interface{}, error) { return attributeRepo.Get(id) }},
		{"an evidence bundle", func(id string) (interface{}, error) { return evidenceRepo.FindByID(id) }},
		{"a test result's project", func(id string) (interface{}, error) { return evidenceRepo.ProjectForResult(id) }},
		{"a citation's removal", func(id string) (interface{}, error) { return nil, evidenceRepo.RemoveCitation(id, phantom) }},
		{"a shared product's report", func(id string) (interface{}, error) { return sharedRepo.AddReport(id, phantom) }},
		{"a shared product's hiding", func(id string) (interface{}, error) { return nil, sharedRepo.SetHidden(id, true) }},
		{"a shared product's removal", func(id string) (interface{}, error) { return nil, sharedRepo.Delete(id) }},
		{"a pool node", func(id string) (interface{}, error) { return runnerRepo.FindNodeByID(id) }},
		{"a pool node's heartbeat", func(id string) (interface{}, error) { return runnerRepo.TouchNode(id, time.Now()) }},
		{"a people-team member's removal", func(id string) (interface{}, error) { return nil, orgRepo.RemoveTeamMember(phantom, id) }},
		{"the automations of a project", func(id string) (interface{}, error) { return automationRepo.List(phantom, id) }},
		{"the proposals of a run", func(id string) (interface{}, error) { return proposalRepo.List("", "", "", id) }},
		{"the runs of an agent", func(id string) (interface{}, error) {
			return runRepo.List(agentruns.ListFilter{OrgID: phantom, AgentID: id})
		}},
		{"a notification's flag", func(id string) (interface{}, error) { return notificationRepo.SetFlagged(phantom, id, true) }},
		{"notifications marked read", func(id string) (interface{}, error) { return notificationRepo.MarkRead(phantom, []string{id}) }},
		{"a worker key", func(id string) (interface{}, error) { return workerKeyRepo.FindByID(id) }},
		{"an invitation", func(id string) (interface{}, error) { return invitationRepo.FindByID(id) }},
		{"an artifact", func(id string) (interface{}, error) { return artifactRepo.FindByID(id) }},
	}
	for _, tc := range reads {
		t.Run(tc.name, func(t *testing.T) {
			want, wantErr := tc.read(uuid.New().String())
			for _, id := range malformedIDs {
				got, err := tc.read(id)
				if !sameError(err, wantErr) || !reflect.DeepEqual(got, want) {
					t.Errorf("the malformed id %q: %#v, %v; a well-formed id no row has: %#v, %v", id, got, err, want, wantErr)
				}
			}
		})
	}
}

// An artifact no row has is artifacts.ErrNotFound, the sentinel the handlers
// answer 404 for (a result's test case, a status change, an update and a
// restore), where an error of the repository's own fell to their 500 (quirk
// Q2); a malformed id answers the same (TestAMalformedIDAnswersAsAnIDNoRowHas).
func TestAnArtifactNoRowHasIsErrNotFound(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
		if got, err := NewArtifactRepository(db).FindByID(id); got != nil || !errors.Is(err, artifacts.ErrNotFound) {
			t.Errorf("FindByID(%q) = %v, %v; want artifacts.ErrNotFound", id, got, err)
		}
	}
}

// sameError reports whether two answers fail alike: both nil, or the same
// error (a sentinel, as errors.Is sees it).
func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	return errors.Is(a, b) || a.Error() == b.Error()
}

// The writes and lists where a malformed id sits beside real rows: a
// project role for an account no row has is members.ErrUnknownUser (it was
// the foreign key's refusal, a 500), a crew list filtered by a project no row
// has keeps the workspace-wide crews, and a mark-read keeps the ids that can
// match.
func TestAMalformedIDBesideRealRows(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	orgID, projectID, crewID, userID := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	for _, q := range []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Malformed', $2)`, []interface{}{orgID, "malformed-" + orgID[:8]}},
		{`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'P')`, []interface{}{projectID, orgID}},
		{`INSERT INTO agent_teams (id, org_id, name) VALUES ($1, $2, 'Workspace-wide')`, []interface{}{crewID, orgID}},
		{`INSERT INTO users (id, email, name, auth_provider, created_at, updated_at)
			VALUES ($1, $2, 'Reader', 'password', NOW(), NOW())`, []interface{}{userID, userID + "@example.com"}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	memberRepo := NewMemberRepository(db)
	for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
		err := memberRepo.Upsert(&members.Member{ProjectID: projectID, UserID: id, Role: members.RoleViewer, CreatedAt: time.Now()})
		if !errors.Is(err, members.ErrUnknownUser) {
			t.Errorf("a project role for %q: %v, want members.ErrUnknownUser", id, err)
		}
	}

	teamRepo := NewTeamRepository(db)
	want, err := teamRepo.ListTeams(orgID, uuid.New().String())
	if err != nil || len(want) != 1 || want[0].ID != crewID {
		t.Fatalf("crews for a project no row has: %v, %v; want the workspace-wide crew", want, err)
	}
	for _, id := range malformedIDs {
		if got, err := teamRepo.ListTeams(orgID, id); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("crews for the malformed project id %q: %v, %v; want %v", id, got, err, want)
		}
	}

	notificationRepo := NewNotificationRepository(db)
	n := notifications.New("", userID, notifications.TypeRunFailed, "unread", "body", nil)
	if err := notificationRepo.Insert(n); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if updated, err := notificationRepo.MarkRead(userID, append(append([]string(nil), malformedIDs...), n.ID)); err != nil || updated != 1 {
		t.Fatalf("mark read beside a malformed id: %d, %v; want the real one marked", updated, err)
	}
}
