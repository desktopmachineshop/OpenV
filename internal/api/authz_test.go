package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// --- fakes: embed the interface so only the methods requireProjectRole
// touches need real implementations (anything else would panic loudly).

type fakeProjectService struct {
	projects.Service
	byID map[string]*projects.Project
}

func (f *fakeProjectService) GetProject(id string) (*projects.Project, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, errors.New("project not found")
}

type fakeOrgService struct {
	orgs.Service
	// roles maps orgID -> userID -> role
	roles map[string]map[string]string
}

func (f *fakeOrgService) RoleInOrg(orgID, userID string) (string, error) {
	return f.roles[orgID][userID], nil
}

type fakeMemberService struct {
	members.Service
	// roles maps projectID -> userID -> effective role (direct or team grant)
	roles map[string]map[string]string
}

func (f *fakeMemberService) EffectiveRole(projectID, userID string) (string, error) {
	return f.roles[projectID][userID], nil
}

// --- request builders using the package's unexported context keys ---

func reqWithUser(u *users.User) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(context.WithValue(r.Context(), ctxUser, u))
}

func reqWithRun(run *agentruns.Run) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(context.WithValue(r.Context(), ctxRun, run))
}

func reqWithWorker(orgID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, orgID))
}

func TestRequireProjectRole(t *testing.T) {
	const (
		projectID = "proj-1"
		orgID     = "org-1"
	)

	newHandler := func() *Handler {
		return &Handler{
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				projectID: {ID: projectID, OrgID: orgID},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"org-admin": orgs.RoleAdmin, "org-member": orgs.RoleMember},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				projectID: {
					"direct-editor": members.RoleEditor,
					"team-viewer":   members.RoleViewer,
					"org-member":    "",
				},
			}},
		}
	}

	ownProject := projectID
	foreignProject := "proj-other"

	cases := []struct {
		name     string
		request  *http.Request
		minRole  string
		wantPass bool
		wantCode int // checked only when !wantPass
	}{
		{
			name:     "platform admin passes owner",
			request:  reqWithUser(&users.User{ID: "root", IsAdmin: true}),
			minRole:  members.RoleOwner,
			wantPass: true,
		},
		{
			name:     "org admin of project org passes owner",
			request:  reqWithUser(&users.User{ID: "org-admin"}),
			minRole:  members.RoleOwner,
			wantPass: true,
		},
		{
			name:     "direct editor meets editor",
			request:  reqWithUser(&users.User{ID: "direct-editor"}),
			minRole:  members.RoleEditor,
			wantPass: true,
		},
		{
			name:     "direct editor fails owner",
			request:  reqWithUser(&users.User{ID: "direct-editor"}),
			minRole:  members.RoleOwner,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "team-grant viewer meets viewer",
			request:  reqWithUser(&users.User{ID: "team-viewer"}),
			minRole:  members.RoleViewer,
			wantPass: true,
		},
		{
			name:     "team-grant viewer fails editor",
			request:  reqWithUser(&users.User{ID: "team-viewer"}),
			minRole:  members.RoleEditor,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "non-member gets 403",
			request:  reqWithUser(&users.User{ID: "stranger"}),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "unauthenticated gets 401",
			request:  httptest.NewRequest(http.MethodGet, "/", nil),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "worker same org passes",
			request:  reqWithWorker(orgID),
			minRole:  members.RoleEditor,
			wantPass: true,
		},
		{
			name:     "worker foreign org gets 403",
			request:  reqWithWorker("org-other"),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "run token own project passes editor",
			request:  reqWithRun(&agentruns.Run{ID: "run-1", OrgID: orgID, ProjectID: &ownProject}),
			minRole:  members.RoleEditor,
			wantPass: true,
		},
		{
			name:     "run token foreign project gets 403",
			request:  reqWithRun(&agentruns.Run{ID: "run-2", OrgID: orgID, ProjectID: &foreignProject}),
			minRole:  members.RoleEditor,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "run token cannot act as owner",
			request:  reqWithRun(&agentruns.Run{ID: "run-3", OrgID: orgID, ProjectID: &ownProject}),
			minRole:  members.RoleOwner,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "empty project id gets 404",
			request:  reqWithUser(&users.User{ID: "direct-editor"}),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandler()
			w := httptest.NewRecorder()
			target := projectID
			if tc.name == "empty project id gets 404" {
				target = ""
			}
			got := h.requireProjectRole(w, tc.request, target, tc.minRole)
			if got != tc.wantPass {
				t.Fatalf("requireProjectRole = %v, want %v (response %d %q)", got, tc.wantPass, w.Code, w.Body.String())
			}
			if !tc.wantPass && w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}
