package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/crewtemplates"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeCrewWriter records CreateTeam calls; the other teams.Service methods are
// unused by an authz-focused import test (bodies carry no nodes/edges).
type fakeCrewWriter struct {
	teams.Service
	created []string // org ids passed to CreateTeam
}

func (f *fakeCrewWriter) CreateTeam(orgID, name, description string, projectID *string) (*teams.Team, error) {
	f.created = append(f.created, orgID)
	return &teams.Team{ID: "crew-new", OrgID: orgID, Name: name, ProjectID: projectID}, nil
}

// fakeAgentDir satisfies the sliver of agents.Service the import path may call.
type fakeAgentDir struct{ agents.Service }

func (fakeAgentDir) GetBySlug(orgID, slug string) (*agents.Agent, error) { return nil, nil }

func importReq(userID, orgID, projectID, body string) *http.Request {
	url := "/api/v1/crews/import"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	r := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
	return r.WithContext(ctx)
}

// TestImportCrewAuthz locks in that importing mirrors CreateTeam's access
// ladder: a workspace-wide import needs workspace admin, a project-pinned
// import needs project editor, and denied requests never reach the service.
func TestImportCrewAuthz(t *testing.T) {
	const orgID = "org-1"
	const project = "proj-1"
	// A minimal valid document (no nodes) — enough to reach CreateTeam.
	body := `{"kind":"openv.crew","version":"1.0","name":"Imported","nodes":[],"edges":[]}`

	newFixture := func() (*Handler, *fakeCrewWriter) {
		writer := &fakeCrewWriter{}
		h := &Handler{
			teamService:  writer,
			agentService: fakeAgentDir{},
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				project: {ID: project, OrgID: orgID},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"org-admin": orgs.RoleAdmin, "org-member": orgs.RoleMember, "editor": orgs.RoleMember, "viewer": orgs.RoleMember},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				project: {"editor": members.RoleEditor, "viewer": members.RoleViewer},
			}},
		}
		return h, writer
	}

	cases := []struct {
		name      string
		userID    string
		projectID string
		wantCode  int // 0 = created
	}{
		{"org admin imports workspace-wide", "org-admin", "", 0},
		{"plain member denied workspace-wide", "org-member", "", http.StatusForbidden},
		{"non-member denied", "stranger", "", http.StatusForbidden},
		{"project editor imports project-pinned", "editor", project, 0},
		{"project viewer denied project-pinned", "viewer", project, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, writer := newFixture()
			w := httptest.NewRecorder()
			h.ImportCrew(w, importReq(tc.userID, orgID, tc.projectID, body))
			if tc.wantCode == 0 {
				if w.Code != http.StatusCreated {
					t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
				}
				if len(writer.created) != 1 || writer.created[0] != orgID {
					t.Fatalf("CreateTeam calls = %v, want one for %s", writer.created, orgID)
				}
				var res crewtemplates.ImportResult
				if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
					t.Fatalf("decode result: %v (body %q)", err, w.Body.String())
				}
				if res.Team == nil || res.Team.OrgID != orgID {
					t.Fatalf("result team = %+v, want org %s", res.Team, orgID)
				}
				return
			}
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if len(writer.created) != 0 {
				t.Fatalf("denied import reached the service (%d create calls)", len(writer.created))
			}
		})
	}
}

// TestListCrewTemplatesShape verifies the catalog endpoint returns the built-in
// presets with their portable graphs.
func TestListCrewTemplatesShape(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/crew-templates", nil)
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1"})
	h.ListCrewTemplates(w, r.WithContext(ctx))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var list []*crewtemplates.CrewTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected at least one built-in crew template")
	}
	for _, tmpl := range list {
		if tmpl.Key == "" || len(tmpl.Crew.Nodes) == 0 {
			t.Fatalf("template %q has no key or nodes", tmpl.Name)
		}
	}
}
