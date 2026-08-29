package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeAttributeService records calls and returns canned definitions.
type fakeAttributeService struct {
	attributes.Service
	byID    map[string]*attributes.Definition
	created []attributes.CreateDefinitionRequest
	updated []string
	deleted []string
}

func (f *fakeAttributeService) CreateDefinition(req attributes.CreateDefinitionRequest) (*attributes.Definition, error) {
	f.created = append(f.created, req)
	return &attributes.Definition{ID: "new", OrgID: req.OrgID, ProjectID: req.ProjectID, Key: req.Key, DataType: req.DataType}, nil
}
func (f *fakeAttributeService) UpdateDefinition(id string, req attributes.UpdateDefinitionRequest) (*attributes.Definition, error) {
	f.updated = append(f.updated, id)
	return f.byID[id], nil
}
func (f *fakeAttributeService) DeleteDefinition(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeAttributeService) GetDefinition(id string) (*attributes.Definition, error) {
	if d, ok := f.byID[id]; ok {
		return d, nil
	}
	return nil, attributes.ErrNotFound
}
func (f *fakeAttributeService) EffectiveForProject(orgID, projectID string) ([]*attributes.Definition, error) {
	return []*attributes.Definition{}, nil
}
func (f *fakeAttributeService) ListByOrg(orgID string) ([]*attributes.Definition, error) {
	return []*attributes.Definition{}, nil
}
func (f *fakeAttributeService) ListByProject(projectID string) ([]*attributes.Definition, error) {
	return []*attributes.Definition{}, nil
}

func attrTestHandler(attrSvc *fakeAttributeService) *Handler {
	return &Handler{
		attributeService: attrSvc,
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"org-admin": orgs.RoleAdmin, "org-member": orgs.RoleMember},
		}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"proj-editor": members.RoleEditor, "proj-viewer": members.RoleViewer},
		}},
	}
}

func withUser(r *http.Request, id string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: id}))
}

// TestCreateAttributeDefinitionAuthz locks the write gate: org-wide needs org
// admin, project-scoped needs project editor.
func TestCreateAttributeDefinitionAuthz(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		userID   string
		wantCode int
	}{
		{"org-wide by org admin", `{"org_id":"org-1","key":"prio","data_type":"text"}`, "org-admin", http.StatusCreated},
		{"org-wide by org member denied", `{"org_id":"org-1","key":"prio","data_type":"text"}`, "org-member", http.StatusForbidden},
		{"project-scoped by editor", `{"project_id":"proj-1","key":"prio","data_type":"text"}`, "proj-editor", http.StatusCreated},
		{"project-scoped by viewer denied", `{"project_id":"proj-1","key":"prio","data_type":"text"}`, "proj-viewer", http.StatusForbidden},
		{"no scope is 400", `{"key":"prio","data_type":"text"}`, "org-admin", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrSvc := &fakeAttributeService{}
			h := attrTestHandler(attrSvc)
			r := withUser(httptest.NewRequest(http.MethodPost, "/api/v1/attribute-definitions", strings.NewReader(tc.body)), tc.userID)
			w := httptest.NewRecorder()
			h.CreateAttributeDefinition(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			created := len(attrSvc.created) == 1
			if (tc.wantCode == http.StatusCreated) != created {
				t.Fatalf("created=%v but wanted success=%v", created, tc.wantCode == http.StatusCreated)
			}
		})
	}
}

// TestUpdateDeleteAttributeDefinitionAuthz gates by the existing definition's
// scope.
func TestUpdateDeleteAttributeDefinitionAuthz(t *testing.T) {
	orgID, projID := "org-1", "proj-1"
	newSvc := func() *fakeAttributeService {
		return &fakeAttributeService{byID: map[string]*attributes.Definition{
			"org-def":  {ID: "org-def", OrgID: &orgID, Key: "a", DataType: attributes.DataTypeText},
			"proj-def": {ID: "proj-def", ProjectID: &projID, Key: "b", DataType: attributes.DataTypeText},
		}}
	}

	body := `{"data_type":"text","label":"x"}`
	cases := []struct {
		name     string
		defID    string
		userID   string
		wantCode int
	}{
		{"org def updated by org admin", "org-def", "org-admin", http.StatusOK},
		{"org def blocked for org member", "org-def", "org-member", http.StatusForbidden},
		{"project def updated by editor", "proj-def", "proj-editor", http.StatusOK},
		{"project def blocked for viewer", "proj-def", "proj-viewer", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run("update/"+tc.name, func(t *testing.T) {
			attrSvc := newSvc()
			h := attrTestHandler(attrSvc)
			r := withUser(httptest.NewRequest(http.MethodPut, "/api/v1/attribute-definitions/"+tc.defID, strings.NewReader(body)), tc.userID)
			r = mux.SetURLVars(r, map[string]string{"id": tc.defID})
			w := httptest.NewRecorder()
			h.UpdateAttributeDefinition(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
		})
		t.Run("delete/"+tc.name, func(t *testing.T) {
			attrSvc := newSvc()
			h := attrTestHandler(attrSvc)
			r := withUser(httptest.NewRequest(http.MethodDelete, "/api/v1/attribute-definitions/"+tc.defID, nil), tc.userID)
			r = mux.SetURLVars(r, map[string]string{"id": tc.defID})
			w := httptest.NewRecorder()
			h.DeleteAttributeDefinition(w, r)
			wantDelete := http.StatusNoContent
			if tc.wantCode != http.StatusOK {
				wantDelete = tc.wantCode
			}
			if w.Code != wantDelete {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, wantDelete, w.Body.String())
			}
		})
	}
}
