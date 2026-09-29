package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// A platform admin passes the project and workspace guards with no role,
// but only for a project or workspace that exists. The S5e matrix found the
// guards letting it by for ids no row has, and the handlers past them then
// answered 500 (the export, every download, the V&V reads, the project's
// delete, a connector pairing), 204 for nothing (a runner key, a member or a
// team grant that is not there, with project.member_removed published for
// the phantom, REQ-122), or 201 with rows no project owns (an artifact, an
// attribute definition, a guided session). Every other caller is refused
// before a handler runs (REQ-16, REQ-17); the admin now gets the 404 a
// worker key gets.

const phantomID = "00000000-0000-4000-8000-000000000000"

var platformAdminUser = &users.User{ID: "root", IsAdmin: true}

// adminPhantomReq is a platform admin's request with the given path
// variables, acting in its own workspace.
func adminPhantomReq(method, body string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, platformAdminUser)
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-admin")
	return mux.SetURLVars(r.WithContext(ctx), vars)
}

// phantomProjectService knows one project and records deletes.
type phantomProjectService struct {
	fakeProjectService
	deleted []string
}

func (f *phantomProjectService) DeleteProject(id string) error {
	if _, ok := f.byID[id]; !ok {
		return errors.New("project not found")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

// phantomMemberService records the membership writes a handler makes.
type phantomMemberService struct {
	members.Service
	removed []string
	revoked []string
}

func (f *phantomMemberService) RemoveMember(projectID, userID string) error {
	f.removed = append(f.removed, projectID+"/"+userID)
	return nil
}

func (f *phantomMemberService) RevokeTeam(projectID, teamID string) error {
	f.revoked = append(f.revoked, projectID+"/"+teamID)
	return nil
}

// phantomKeyService records the personal-key and pairing calls, and refuses
// a pairing for a workspace no row has, as the database's foreign key does.
type phantomKeyService struct {
	workerkeys.Service
	missing  map[string]bool
	calls    []string
	personal *workerkeys.Key
}

func (f *phantomKeyService) CreatePairing(orgID, userID string) (string, time.Time, error) {
	f.calls = append(f.calls, "pair "+orgID)
	if f.missing[orgID] {
		return "", time.Time{}, errors.New(`pq: insert or update on table "pairing_codes" violates foreign key constraint`)
	}
	return "code", time.Now().Add(10 * time.Minute), nil
}

func (f *phantomKeyService) PersonalKey(orgID, userID string) (*workerkeys.Key, error) {
	f.calls = append(f.calls, "personal "+orgID)
	return f.personal, nil
}

func (f *phantomKeyService) Revoke(orgID, keyID string) error {
	f.calls = append(f.calls, "revoke "+orgID)
	return nil
}

func phantomProjectFixture() (*Handler, *phantomProjectService, *phantomMemberService, *applierArtifactService, *recordingBus) {
	projectSvc := &phantomProjectService{fakeProjectService: fakeProjectService{byID: map[string]*projects.Project{
		"proj-1": {ID: "proj-1", OrgID: "org-1"},
	}}}
	memberSvc := &phantomMemberService{}
	artifactSvc := &applierArtifactService{}
	bus := &recordingBus{}
	h := NewHandler(HandlerDeps{
		ProjectService:  projectSvc,
		MemberService:   memberSvc,
		ArtifactService: artifactSvc,
		OrgService:      &fakeOrgService{},
		Bus:             bus,
	})
	return h, projectSvc, memberSvc, artifactSvc, bus
}

const projectNotFound = `{"error":"project not found"}` + "\n"

func TestAPlatformAdminPassesTheProjectGuardOnlyForAProjectThatExists(t *testing.T) {
	h, _, _, _, _ := phantomProjectFixture()

	w := httptest.NewRecorder()
	if h.requireProjectRole(w, adminPhantomReq(http.MethodGet, "", nil), phantomID, members.RoleViewer) {
		t.Fatal("the guard let a platform admin by for a project no row has")
	}
	if w.Code != http.StatusNotFound || w.Body.String() != projectNotFound {
		t.Fatalf("a phantom project: %d %q, want the 404 a worker key gets", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	if !h.requireProjectRole(w, adminPhantomReq(http.MethodDelete, "", nil), "proj-1", members.RoleOwner) {
		t.Fatalf("a platform admin was refused a project that exists: %d %q", w.Code, w.Body.String())
	}
}

// The project twin of the org.member_removed fix: removing a member of a
// project that does not exist answered 204 and told the admin's own
// workspace that the phantom account had left the phantom project.
func TestAPlatformAdminsWritesInAPhantomProjectAnswer404AndStoreNothing(t *testing.T) {
	cases := []struct {
		name string
		call func(h *Handler, w http.ResponseWriter, r *http.Request)
		body string
		vars map[string]string
	}{
		{"remove a member", (*Handler).RemoveProjectMember, "", map[string]string{"id": phantomID, "userId": phantomID}},
		{"revoke a team grant", (*Handler).RevokeProjectTeamAccess, "", map[string]string{"id": phantomID, "teamId": phantomID}},
		{"delete the project", (*Handler).DeleteProject, "", map[string]string{"id": phantomID}},
		{"create an artifact", (*Handler).CreateArtifact,
			`{"project_id":"` + phantomID + `","type":"requirement","title":"Tour phantom","body":"The system shall do nothing."}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, projectSvc, memberSvc, artifactSvc, bus := phantomProjectFixture()
			method := http.MethodDelete
			if tc.body != "" {
				method = http.MethodPost
			}
			w := httptest.NewRecorder()
			tc.call(h, w, adminPhantomReq(method, tc.body, tc.vars))
			if w.Code != http.StatusNotFound || w.Body.String() != projectNotFound {
				t.Fatalf("status %d %q, want 404 project not found", w.Code, w.Body.String())
			}
			if len(memberSvc.removed)+len(memberSvc.revoked)+len(projectSvc.deleted)+len(artifactSvc.created) != 0 {
				t.Fatalf("a refused write stored something: removed %v, revoked %v, deleted %v, created %d artifacts",
					memberSvc.removed, memberSvc.revoked, projectSvc.deleted, len(artifactSvc.created))
			}
			if len(bus.published) != 0 {
				t.Fatalf("a refused write published %v", bus.types())
			}
		})
	}

	// The same removal in a project that exists goes through and is told.
	h, _, memberSvc, _, bus := phantomProjectFixture()
	w := httptest.NewRecorder()
	h.RemoveProjectMember(w, adminPhantomReq(http.MethodDelete, "", map[string]string{"id": "proj-1", "userId": "u-1"}))
	if w.Code != http.StatusNoContent || len(memberSvc.removed) != 1 {
		t.Fatalf("removing a member of a real project: %d %q, removed %v", w.Code, w.Body.String(), memberSvc.removed)
	}
	if types := bus.types(); len(types) != 1 || types[0] != events.ProjectMemberRemoved {
		t.Fatalf("removing a member of a real project published %v", types)
	}
}

const workspaceNotFound = `{"error":"workspace not found"}` + "\n"

func TestAPlatformAdminPassesTheWorkspaceGuardOnlyForAWorkspaceThatExists(t *testing.T) {
	h := NewHandler(HandlerDeps{OrgService: &fakeOrgService{missing: map[string]bool{phantomID: true}}})

	w := httptest.NewRecorder()
	if h.requireOrgRole(w, adminPhantomReq(http.MethodGet, "", nil), phantomID, orgs.RoleMember) {
		t.Fatal("the guard let a platform admin by for a workspace no row has")
	}
	if w.Code != http.StatusNotFound || w.Body.String() != workspaceNotFound {
		t.Fatalf("a phantom workspace: %d %q, want 404 workspace not found", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	if !h.requireOrgRole(w, adminPhantomReq(http.MethodPost, "", nil), "org-1", orgs.RoleAdmin) {
		t.Fatalf("a platform admin was refused a workspace that exists: %d %q", w.Code, w.Body.String())
	}
}

// A connector pairing for a workspace that does not exist answered 500 (the
// pairing's foreign key), and revoking one's runner key there 204.
func TestAPlatformAdminsRunnerRequestsInAPhantomWorkspaceAnswer404(t *testing.T) {
	cases := []struct {
		name   string
		call   func(h *Handler, w http.ResponseWriter, r *http.Request)
		method string
	}{
		{"a connector pairing", (*Handler).CreateConnectorPairing, http.MethodPost},
		{"revoking its runner key", (*Handler).RevokeMyRunnerKey, http.MethodDelete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := &phantomKeyService{missing: map[string]bool{phantomID: true}}
			h := NewHandler(HandlerDeps{
				OrgService:       &fakeOrgService{missing: map[string]bool{phantomID: true}},
				WorkerKeyService: keys,
			})
			w := httptest.NewRecorder()
			tc.call(h, w, adminPhantomReq(tc.method, "", map[string]string{"id": phantomID}))
			if w.Code != http.StatusNotFound || w.Body.String() != workspaceNotFound {
				t.Fatalf("status %d %q, want 404 workspace not found", w.Code, w.Body.String())
			}
			if len(keys.calls) != 0 {
				t.Fatalf("the refused request reached the key service: %v", keys.calls)
			}
		})
	}
}
