package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// confirmLinkService fakes just enough of links.Service for the confirm
// endpoint: GetLink for authz resolution and ConfirmLink itself.
type confirmLinkService struct {
	links.Service
	byID     map[string]*links.Link
	confirms []string
}

func (f *confirmLinkService) GetLink(id string) (*links.Link, error) {
	if l, ok := f.byID[id]; ok {
		return l, nil
	}
	return nil, errors.New("link not found")
}

func (f *confirmLinkService) ConfirmLink(id string) (*links.Link, error) {
	f.confirms = append(f.confirms, id)
	l, ok := f.byID[id]
	if !ok {
		return nil, errors.New("link not found")
	}
	copied := *l
	copied.Suspect = false
	return &copied, nil
}

func TestConfirmLink(t *testing.T) {
	const (
		projectID  = "proj-1"
		orgID      = "org-1"
		artifactID = "art-from"
		linkID     = "link-1"
	)

	newFixture := func() (*Handler, *confirmLinkService) {
		linkSvc := &confirmLinkService{byID: map[string]*links.Link{
			linkID: {ID: linkID, FromID: artifactID, ToID: "art-to", Type: "verifies", Suspect: true},
		}}
		h := &Handler{
			linkService: linkSvc,
			artifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{
				artifactID: {ID: artifactID, ProjectID: projectID, Type: "requirement"},
			}},
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				projectID: {ID: projectID, OrgID: orgID},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{orgID: {}}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				projectID: {
					"editor": members.RoleEditor,
					"viewer": members.RoleViewer,
				},
			}},
		}
		return h, linkSvc
	}

	do := func(t *testing.T, h *Handler, userID, target string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/links/"+target+"/confirm", nil)
		r = mux.SetURLVars(r, map[string]string{"id": target})
		if userID != "" {
			r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
		}
		w := httptest.NewRecorder()
		h.ConfirmLink(w, r)
		return w
	}

	t.Run("editor confirms a suspect link", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := do(t, h, "editor", linkID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var got links.Link
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.Suspect {
			t.Error("confirmed link still suspect in response")
		}
		if len(linkSvc.confirms) != 1 || linkSvc.confirms[0] != linkID {
			t.Errorf("ConfirmLink calls = %v, want exactly [%s]", linkSvc.confirms, linkID)
		}
	})

	t.Run("viewer is refused", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := do(t, h, "viewer", linkID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", w.Code)
		}
		if len(linkSvc.confirms) != 0 {
			t.Errorf("ConfirmLink was called despite 403: %v", linkSvc.confirms)
		}
	})

	t.Run("non-member is refused", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := do(t, h, "stranger", linkID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", w.Code)
		}
		if len(linkSvc.confirms) != 0 {
			t.Errorf("ConfirmLink was called despite 403: %v", linkSvc.confirms)
		}
	})

	t.Run("unknown link is 404", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := do(t, h, "editor", "nope")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
		if len(linkSvc.confirms) != 0 {
			t.Errorf("ConfirmLink was called despite 404: %v", linkSvc.confirms)
		}
	})
}
