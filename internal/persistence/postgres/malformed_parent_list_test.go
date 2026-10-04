package postgres

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/notifications"
)

// A list filtered by a parent id answers a malformed one as it answers a
// well-formed id no row has (#379 bug 139, the maintainer's question 40):
// Postgres's refusal of the id was handed back, which a handler answered
// 500, where bug 88 had done this for the project lists alone. A list of ids
// answers as it would with the malformed ones left out (uuidsOnly), and a
// database that fails is still a failure, never an empty list. The
// malformed-id answers are Postgres-gated (OPENV_TEST_DATABASE_URL); the
// failing database is not.

// parentList is one repository list, read by one parent id.
type parentList struct {
	name string
	read func(id string) (interface{}, error)
}

// parentLists is every repository list filtered by a parent id, each read
// with a well-formed id beside the one it is given wherever it takes more.
func parentLists(db *sql.DB) []parentList {
	other := uuid.New().String()
	agentRepo := NewAgentRepository(db)
	runRepo := NewAgentRunRepository(db)
	artifactRepo := NewArtifactRepository(db)
	attachmentRepo := NewAttachmentRepository(db)
	attributeRepo := NewAttributeDefinitionRepository(db)
	baselineRepo := NewBaselineRepository(db)
	chatterRepo := NewChatterRepository(db)
	embeddingRepo := NewEmbeddingRepository(db)
	eventRepo := NewEventRepository(db)
	evidenceRepo := NewEvidenceRepository(db)
	guidedRepo := NewGuidedRepository(db)
	interviewRepo := NewInterviewRepository(db)
	invitationRepo := NewInvitationRepository(db)
	linkRepo := NewLinkRepository(db)
	memberRepo := NewMemberRepository(db)
	notificationRepo := NewNotificationRepository(db)
	orgRepo := NewOrgRepository(db)
	providerRepo := NewProviderSettingRepository(db)
	pushRepo := NewPushSubscriptionRepository(db)
	repoConnRepo := NewRepoConnectionRepository(db)
	runnerRepo := NewRunnerSessionRepository(db)
	shareLinkRepo := NewShareLinkRepository(db)
	teamRepo := NewTeamRepository(db)
	templateRepo := NewTemplateRepository(db)
	vvRepo := NewVVRepository(db)
	workerKeyRepo := NewWorkerKeyRepository(db)
	workItemRepo := NewWorkItemRepository(db)
	return []parentList{
		{"a workspace's agents", func(id string) (interface{}, error) { return agentRepo.List(id) }},
		{"a run's children", func(id string) (interface{}, error) { return runRepo.ListChildren(id) }},
		{"a run's log", func(id string) (interface{}, error) { return runRepo.ListLogs(id, 0) }},
		{"a project's artifacts", func(id string) (interface{}, error) { return artifactRepo.FindByProjectID(id) }},
		{"a project's artifacts of a type", func(id string) (interface{}, error) {
			return artifactRepo.FindByProjectAndType(id, "requirement")
		}},
		{"a project's artifacts in a status", func(id string) (interface{}, error) {
			return artifactRepo.FindByProjectAndStatus(id, "draft")
		}},
		{"a page of a project's artifacts", func(id string) (interface{}, error) {
			return artifactRepo.FindPageByProject(id, "", "", 10, 0)
		}},
		{"an artifact's figures", func(id string) (interface{}, error) { return attachmentRepo.FindByArtifactID(id) }},
		{"a project's figures", func(id string) (interface{}, error) { return attachmentRepo.FindByProjectID(id) }},
		{"a figure's versions", func(id string) (interface{}, error) { return attachmentRepo.ListVersions(id) }},
		{"a workspace's attribute definitions", func(id string) (interface{}, error) { return attributeRepo.ListByOrg(id) }},
		{"a project's attribute definitions", func(id string) (interface{}, error) { return attributeRepo.ListByProject(id) }},
		{"a project's baselines", func(id string) (interface{}, error) { return baselineRepo.ListByProjectID(id) }},
		{"an artifact's chatter", func(id string) (interface{}, error) { return chatterRepo.FindByArtifactID(id) }},
		{"a project's duplicate candidates", func(id string) (interface{}, error) {
			return embeddingRepo.DuplicateCandidates(id, 0.1, 10)
		}},
		{"a workspace's events", func(id string) (interface{}, error) { return eventRepo.List(id, "", "", "", 10) }},
		{"a project's events", func(id string) (interface{}, error) { return eventRepo.List(other, id, "", "", 10) }},
		{"the events before an event", func(id string) (interface{}, error) { return eventRepo.List(other, "", "", id, 10) }},
		{"a project's evidence bundles", func(id string) (interface{}, error) { return evidenceRepo.ListByProject(id) }},
		{"a bundle's files", func(id string) (interface{}, error) { return evidenceRepo.ListFiles(id) }},
		{"a bundle's citations", func(id string) (interface{}, error) { return evidenceRepo.ListCitationsForBundle(id) }},
		{"a result's citations", func(id string) (interface{}, error) { return evidenceRepo.ListCitationsForResult(id) }},
		{"a test run's citations", func(id string) (interface{}, error) { return evidenceRepo.ListCitationsForRun(id) }},
		{"a guided session's chat", func(id string) (interface{}, error) { return guidedRepo.ListChatMessages(id) }},
		{"a project's guided sessions", func(id string) (interface{}, error) { return guidedRepo.ListByProject(id) }},
		{"a project's interviews", func(id string) (interface{}, error) { return interviewRepo.ListInterviewsByProject(id) }},
		{"an interview's invites", func(id string) (interface{}, error) { return interviewRepo.ListInvitesByInterview(id) }},
		{"an interview's sessions", func(id string) (interface{}, error) { return interviewRepo.ListSessionsByInterview(id) }},
		{"a project's interview sessions", func(id string) (interface{}, error) {
			return interviewRepo.ListSessionsByProject(id, 10)
		}},
		{"an interview session's messages", func(id string) (interface{}, error) { return interviewRepo.ListMessagesBySession(id) }},
		{"a workspace's pending invitations", func(id string) (interface{}, error) {
			return invitationRepo.ListPending(id, time.Now())
		}},
		{"the links from an artifact", func(id string) (interface{}, error) { return linkRepo.FindByFromID(id) }},
		{"the links to an artifact", func(id string) (interface{}, error) { return linkRepo.FindByToID(id) }},
		{"a project's links", func(id string) (interface{}, error) { return linkRepo.FindAll(id) }},
		{"a project's suspect links", func(id string) (interface{}, error) { return linkRepo.FindSuspectByProject(id) }},
		{"the links from an artifact's version", func(id string) (interface{}, error) { return linkRepo.FindByFromIDForVersion(id, 1) }},
		{"the links to an artifact's version", func(id string) (interface{}, error) { return linkRepo.FindByToIDForVersion(id, 1) }},
		{"a project's members", func(id string) (interface{}, error) { return memberRepo.ListByProject(id) }},
		{"an account's projects", func(id string) (interface{}, error) { return memberRepo.ListProjectIDsForUser(id) }},
		{"a project's team grants", func(id string) (interface{}, error) { return memberRepo.ListTeamGrants(id) }},
		{"an account's notifications", func(id string) (interface{}, error) {
			return notificationRepo.List(id, notifications.ListQuery{Limit: 10})
		}},
		{"an account's workspaces", func(id string) (interface{}, error) { return orgRepo.ListOrgsForUser(id) }},
		{"an account's deleted workspaces", func(id string) (interface{}, error) { return orgRepo.ListDeletedOrgsForUser(id) }},
		{"a workspace's members", func(id string) (interface{}, error) { return orgRepo.ListMembers(id) }},
		{"a workspace's people teams", func(id string) (interface{}, error) { return orgRepo.ListTeams(id) }},
		{"a people team's members", func(id string) (interface{}, error) { return orgRepo.ListTeamMembers(id) }},
		{"a workspace's provider settings", func(id string) (interface{}, error) { return providerRepo.List(id) }},
		{"an account's push subscriptions", func(id string) (interface{}, error) { return pushRepo.ListForUser(id) }},
		{"a project's repository connections", func(id string) (interface{}, error) { return repoConnRepo.ListByProject(id) }},
		{"an account's local paths", func(id string) (interface{}, error) { return repoConnRepo.UserPaths(id, other) }},
		{"the local paths in a project", func(id string) (interface{}, error) { return repoConnRepo.UserPaths(other, id) }},
		{"a workspace's live runner leases", func(id string) (interface{}, error) { return runnerRepo.ListLiveSessions(id) }},
		{"a project's share links", func(id string) (interface{}, error) { return shareLinkRepo.ListByProject(id) }},
		{"a workspace's crews", func(id string) (interface{}, error) { return teamRepo.ListTeams(id, "") }},
		{"a crew's nodes", func(id string) (interface{}, error) { return teamRepo.ListNodesByTeam(id) }},
		{"a crew's edges", func(id string) (interface{}, error) { return teamRepo.ListEdgesByTeam(id) }},
		{"a workspace's templates", func(id string) (interface{}, error) { return templateRepo.List(id) }},
		{"a project's test runs", func(id string) (interface{}, error) { return vvRepo.ListRunsByProject(id) }},
		{"a test run's results", func(id string) (interface{}, error) { return vvRepo.ListResultsByRun(id) }},
		{"a test run's result history", func(id string) (interface{}, error) { return vvRepo.ListResultHistoryByRun(id) }},
		{"a project's latest result per test case", func(id string) (interface{}, error) { return vvRepo.LatestResultPerCase(id) }},
		{"a workspace's worker keys", func(id string) (interface{}, error) { return workerKeyRepo.List(id) }},
		{"a project's work items", func(id string) (interface{}, error) { return workItemRepo.ListByProject(id) }},
		{"a work item's activity", func(id string) (interface{}, error) { return workItemRepo.ListActivity(id) }},
		// Lists filtered by a parent id that already answered a malformed
		// one as one no row has, kept here so that none slips back.
		{"a project's runs", func(id string) (interface{}, error) {
			return runRepo.List(agentruns.ListFilter{OrgID: other, ProjectID: id})
		}},
		{"a workspace's projects", func(id string) (interface{}, error) { return NewProjectRepository(db).ListByOrg(id) }},
		{"a project's children", func(id string) (interface{}, error) { return NewProjectRepository(db).ListChildren(id) }},
		{"an artifact's versions", func(id string) (interface{}, error) { return artifactRepo.FindVersionsByID(id) }},
		{"a project's automations", func(id string) (interface{}, error) { return NewAutomationRepository(db).List(other, id) }},
		{"a run's proposals", func(id string) (interface{}, error) { return NewProposalRepository(db).List("", "", "", id) }},
		{"a project's crews", func(id string) (interface{}, error) { return teamRepo.ListTeams(other, id) }},
	}
}

// idLists is every repository list filtered by a list of ids.
func idLists(db *sql.DB) []struct {
	name string
	read func(ids []string) (interface{}, error)
} {
	artifactRepo := NewArtifactRepository(db)
	attachmentRepo := NewAttachmentRepository(db)
	embeddingRepo := NewEmbeddingRepository(db)
	workItemRepo := NewWorkItemRepository(db)
	return []struct {
		name string
		read func(ids []string) (interface{}, error)
	}{
		{"the figures of artifacts", func(ids []string) (interface{}, error) { return attachmentRepo.FindByArtifactIDs(ids) }},
		{"the work items of notes", func(ids []string) (interface{}, error) { return workItemRepo.ListBySourceChatterIDs(ids) }},
		{"a search in projects", func(ids []string) (interface{}, error) {
			return artifactRepo.SearchInProjects(ids, "brake", 10, artifacts.SearchOptions{MatchRefs: true})
		}},
		{"the nearest artifacts in projects", func(ids []string) (interface{}, error) {
			return embeddingRepo.NearestByEmbedding(ids, make([]float32, embeddingDimensions), 10)
		}},
	}
}

// A list by a malformed parent id, of each kind, is the list of a
// well-formed id no row has: the same value, an empty list or nil as that
// list answers for no rows (nil encodes as JSON null, quirk Q14), and the
// same error, which is none but where the vector extension is missing (the
// duplicate candidates' embeddings.ErrVectorUnavailable).
func TestAListByAMalformedParentIDListsAsOneNoRowHas(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	for _, list := range parentLists(db) {
		t.Run(list.name, func(t *testing.T) {
			want, wantErr := list.read(uuid.New().String())
			for _, id := range malformedIDs {
				got, err := list.read(id)
				if !sameError(err, wantErr) || !reflect.DeepEqual(got, want) {
					t.Errorf("the malformed id %q: %#v, %v; a well-formed id no row has: %#v, %v", id, got, err, want, wantErr)
				}
			}
		})
	}
}

// A list of ids answers as it would with the malformed ones left out: with
// only malformed ids, as with none; beside a well-formed id, as with that
// id alone.
func TestAListOfIDsLeavesTheMalformedOnesOut(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	for _, list := range idLists(db) {
		t.Run(list.name, func(t *testing.T) {
			none, noneErr := list.read(nil)
			got, err := list.read(malformedIDs)
			if !sameError(err, noneErr) || !reflect.DeepEqual(got, none) {
				t.Errorf("only malformed ids: %#v, %v; no ids: %#v, %v", got, err, none, noneErr)
			}
			well := uuid.New().String()
			alone, aloneErr := list.read([]string{well})
			got, err = list.read(append([]string{well}, malformedIDs...))
			if !sameError(err, aloneErr) || !reflect.DeepEqual(got, alone) {
				t.Errorf("a well-formed id beside malformed ones: %#v, %v; the well-formed id alone: %#v, %v", got, err, alone, aloneErr)
			}
		})
	}
}

// A database that fails every statement fails every list, a malformed
// parent id's answer notwithstanding: the failure is handed back, never read
// as an empty list.
func TestAListOnAFailingDatabaseFails(t *testing.T) {
	db := rtClosedDB(t)
	for _, list := range parentLists(db) {
		if _, err := list.read(uuid.New().String()); err == nil || !strings.Contains(err.Error(), "sql: database is closed") {
			t.Errorf("%s on a failing database: %v, want the failure", list.name, err)
		}
	}
	for _, list := range idLists(db) {
		if _, err := list.read([]string{uuid.New().String()}); err == nil || !strings.Contains(err.Error(), "sql: database is closed") {
			t.Errorf("%s on a failing database: %v, want the failure", list.name, err)
		}
	}
}
