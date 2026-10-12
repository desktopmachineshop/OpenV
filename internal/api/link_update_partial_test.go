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

// memLinkRepo is a links.Repository holding one map of links; every method
// it does not declare panics. The real links.DefaultService runs over it, so
// these tests pin what a stored link ends up as, not a fake's reading of it.
type memLinkRepo struct {
	links.Repository
	byID map[string]*links.Link
}

func (r *memLinkRepo) FindByID(id string) (*links.Link, error) {
	l, ok := r.byID[id]
	if !ok {
		return nil, errors.New("link not found")
	}
	c := *l
	return &c, nil
}

func (r *memLinkRepo) Update(l *links.Link) error {
	c := *l
	r.byID[l.ID] = &c
	return nil
}

func (r *memLinkRepo) FindByFromID(string) ([]*links.Link, error) { return nil, nil }
func (r *memLinkRepo) FindByToID(string) ([]*links.Link, error)   { return nil, nil }

// TestUpdateLinkIsAPartialUpdateThatChecksTheLinkRules is the regression
// test for #642: PUT /api/v1/links/{id} replaced the type and the attributes
// wholesale, so a body that left one out blanked it, and a changed type was
// never checked against the link-type rules POST /links applies. A field
// left out now keeps its stored value, a field present replaces it, a
// changed type is checked as create checks it (400, nothing written), and an
// explicit empty type is refused.
func TestUpdateLinkIsAPartialUpdateThatChecksTheLinkRules(t *testing.T) {
	const project, org = "proj-1", "org-1"
	stored := func() map[string]interface{} { return map[string]interface{}{"method": "test"} }
	newFixture := func() (*Handler, *recordingBus, *memLinkRepo) {
		bus := &recordingBus{}
		repo := &memLinkRepo{byID: map[string]*links.Link{
			"link-1": {ID: "link-1", FromID: "art-tc", ToID: "art-req", Type: "verifies", Attributes: stored(), Version: 1},
		}}
		h := newTestHandler(t, func(h *Handler) {
			h.Bus = bus
			h.LinkService = links.NewDefaultService(repo)
			h.ChatterService = &fakeChatterService{}
			h.ArtifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{
				"art-tc":  {ID: "art-tc", ProjectID: project, Type: "test-case", Version: 1},
				"art-req": {ID: "art-req", ProjectID: project, Type: "requirement", Version: 2},
			}}
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{project: {ID: project, OrgID: org}}}
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{org: {}}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{project: {"editor": members.RoleEditor}}}
		})
		return h, bus, repo
	}
	put := func(h *Handler, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/links/link-1", strings.NewReader(body))
		r = mux.SetURLVars(asUser(r, &users.User{ID: "editor"}), map[string]string{"id": "link-1"})
		w := httptest.NewRecorder()
		h.UpdateLink(w, r)
		return w
	}

	t.Run("only attributes: the type is kept", func(t *testing.T) {
		h, _, repo := newFixture()
		if w := put(h, `{"attributes":{"method":"inspection"}}`); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		got := repo.byID["link-1"]
		if got.Type != "verifies" {
			t.Errorf("type = %q, want verifies kept", got.Type)
		}
		if want := map[string]interface{}{"method": "inspection"}; !reflect.DeepEqual(got.Attributes, want) {
			t.Errorf("attributes = %v, want %v", got.Attributes, want)
		}
	})

	t.Run("only a type: the attributes are kept", func(t *testing.T) {
		h, _, repo := newFixture()
		if w := put(h, `{"type":"relates-to"}`); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		got := repo.byID["link-1"]
		if got.Type != "relates-to" {
			t.Errorf("type = %q, want relates-to", got.Type)
		}
		if !reflect.DeepEqual(got.Attributes, stored()) {
			t.Errorf("attributes = %v, want %v kept", got.Attributes, stored())
		}
	})

	t.Run("a valid change of type publishes link.updated with the new type", func(t *testing.T) {
		h, bus, repo := newFixture()
		if w := put(h, `{"type":"impacts"}`); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if got := repo.byID["link-1"].Type; got != "impacts" {
			t.Errorf("stored type = %q, want impacts", got)
		}
		if got := bus.types(); !reflect.DeepEqual(got, []string{events.LinkUpdated}) {
			t.Fatalf("published %v, want one link.updated", got)
		}
		want := map[string]interface{}{"link_type": "impacts", "from_id": "art-tc", "to_id": "art-req"}
		if got := bus.published[0].Payload; !reflect.DeepEqual(got, want) {
			t.Errorf("link.updated payload = %v, want %v", got, want)
		}
	})

	t.Run("a type the ends do not allow answers 400, as create does, and writes nothing", func(t *testing.T) {
		h, bus, repo := newFixture()
		w := put(h, `{"type":"derives-from","attributes":{"method":"changed"}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		const msg = "link type 'derives-from' cannot originate from artifact type 'test-case'"
		if !strings.Contains(w.Body.String(), msg) {
			t.Errorf("body %q does not carry the create message %q", w.Body.String(), msg)
		}
		got := repo.byID["link-1"]
		if got.Type != "verifies" || got.Version != 1 || !reflect.DeepEqual(got.Attributes, stored()) {
			t.Errorf("stored link = %+v, want it unchanged", got)
		}
		if got := bus.types(); len(got) != 0 {
			t.Errorf("a refused update published %v, want nothing", got)
		}
	})

	t.Run("an unknown type answers 400", func(t *testing.T) {
		h, _, repo := newFixture()
		if w := put(h, `{"type":"no-such-type"}`); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if got := repo.byID["link-1"].Type; got != "verifies" {
			t.Errorf("stored type = %q, want it unchanged", got)
		}
	})

	t.Run("an explicit empty type answers 400 and is not stored", func(t *testing.T) {
		h, bus, repo := newFixture()
		w := put(h, `{"type":"","attributes":{"method":"changed"}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "type must not be empty") {
			t.Errorf("body %q does not say the type must not be empty", w.Body.String())
		}
		got := repo.byID["link-1"]
		if got.Type != "verifies" || got.Version != 1 || !reflect.DeepEqual(got.Attributes, stored()) {
			t.Errorf("stored link = %+v, want it unchanged", got)
		}
		if got := bus.types(); len(got) != 0 {
			t.Errorf("a refused update published %v, want nothing", got)
		}
	})
}
