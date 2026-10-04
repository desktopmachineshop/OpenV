package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// updateLinkService fakes the links.Service an update reaches: the lookup
// the guard resolves the project from, the update itself (as the real one,
// type and attributes replaced as sent and the version bumped), and the
// link lists the auto-version reads.
type updateLinkService struct {
	links.Service
	byID      map[string]*links.Link
	updateErr error
}

func (f *updateLinkService) GetLink(id string) (*links.Link, error) {
	if l, ok := f.byID[id]; ok {
		return l, nil
	}
	return nil, errors.New("link not found")
}

func (f *updateLinkService) UpdateLink(id string, req links.UpdateLinkRequest) (*links.Link, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	l := *f.byID[id]
	l.Type, l.Attributes, l.Version = req.Type, req.Attributes, l.Version+1
	return &l, nil
}

func (f *updateLinkService) GetLinksFrom(string) ([]*links.Link, error) { return nil, nil }
func (f *updateLinkService) GetLinksTo(string) ([]*links.Link, error)   { return nil, nil }

// TestUpdateLinkPublishesLinkUpdated is the regression test for #379 bug
// 132: link.updated was declared, listed in the activity log's filter and
// offered as an automation trigger, but nothing published it, so an
// automation triggered on it never fired. PUT /api/v1/links/{id}, the one
// path that changes a link's type or attributes, now publishes it once the
// update is stored, shaped as link.created is: the link as entity, the
// source artifact's project, the caller as actor, and {link_type, from_id,
// to_id} with the type the link has now. A refused or failed update
// publishes nothing.
func TestUpdateLinkPublishesLinkUpdated(t *testing.T) {
	const project, org = "proj-1", "org-1"
	newFixture := func() (*Handler, *recordingBus, *updateLinkService) {
		bus := &recordingBus{}
		linkSvc := &updateLinkService{byID: map[string]*links.Link{
			"link-1": {ID: "link-1", FromID: "art-tc", ToID: "art-req", Type: "verifies", Version: 1},
		}}
		h := newTestHandler(t, func(h *Handler) {
			h.Bus = bus
			h.LinkService = linkSvc
			h.ChatterService = &fakeChatterService{}
			h.ArtifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{
				"art-tc":  {ID: "art-tc", ProjectID: project, Type: "test-case", Version: 1},
				"art-req": {ID: "art-req", ProjectID: project, Type: "requirement", Version: 2},
			}}
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{project: {ID: project, OrgID: org}}}
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{org: {}}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{project: {
				"editor": members.RoleEditor, "viewer": members.RoleViewer,
			}}}
		})
		return h, bus, linkSvc
	}
	put := func(h *Handler, userID, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/links/link-1", strings.NewReader(body))
		r = mux.SetURLVars(asUser(r, &users.User{ID: userID}), map[string]string{"id": "link-1"})
		w := httptest.NewRecorder()
		h.UpdateLink(w, r)
		return w
	}

	t.Run("an editor retypes a link", func(t *testing.T) {
		h, bus, _ := newFixture()
		w := put(h, "editor", `{"type":"derives-from","attributes":{"note":"retyped"}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if got := bus.types(); !reflect.DeepEqual(got, []string{events.LinkUpdated}) {
			t.Fatalf("published %v, want one link.updated", got)
		}
		e := bus.published[0]
		if e.EntityID != "link-1" || e.ProjectID != project || e.OrgID != org || e.Actor != "user:editor" {
			t.Errorf("link.updated: entity %q, project %q, org %q, actor %q; want link-1, %s, %s, user:editor",
				e.EntityID, e.ProjectID, e.OrgID, e.Actor, project, org)
		}
		want := map[string]interface{}{"link_type": "derives-from", "from_id": "art-tc", "to_id": "art-req"}
		if !reflect.DeepEqual(e.Payload, want) {
			t.Errorf("link.updated payload = %v, want %v, as link.created's with the type the link has now", e.Payload, want)
		}
	})

	t.Run("a viewer is refused and nothing is published", func(t *testing.T) {
		h, bus, _ := newFixture()
		if w := put(h, "viewer", `{"type":"derives-from"}`); w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if got := bus.types(); len(got) != 0 {
			t.Errorf("a refused update published %v, want nothing", got)
		}
	})

	t.Run("a failed update publishes nothing", func(t *testing.T) {
		h, bus, linkSvc := newFixture()
		linkSvc.updateErr = errors.New("connection refused")
		if w := put(h, "editor", `{"type":"derives-from"}`); w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
		}
		if got := bus.types(); len(got) != 0 {
			t.Errorf("a failed update published %v, want nothing", got)
		}
	})
}
