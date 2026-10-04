package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
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
		h := newTestHandler(t, func(h *Handler) {
			h.LinkService = linkSvc
			h.ArtifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{
				artifactID: {ID: artifactID, ProjectID: projectID, Type: "requirement"},
			}}
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
				projectID: {ID: projectID, OrgID: orgID},
			}}
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{orgID: {}}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
				projectID: {
					"editor": members.RoleEditor,
					"viewer": members.RoleViewer,
				},
			}}
			h.AgentService = &fakeAgentService{byID: map[string]*agents.Agent{
				"agent-direct":   {ID: "agent-direct", WriteMode: agents.WriteModeDirect},
				"agent-proposal": {ID: "agent-proposal", WriteMode: agents.WriteModeProposal},
			}}
		})
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

	t.Run("non-member is told the link is not there", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := do(t, h, "stranger", linkID)
		if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != `{"error":"link not found"}` {
			t.Fatalf("answer = %d %q, want 404 link not found, as for a link no row has", w.Code, w.Body.String())
		}
		if len(linkSvc.confirms) != 0 {
			t.Errorf("ConfirmLink was called despite the refusal: %v", linkSvc.confirms)
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

	// Clearing suspect is a human review action; a proposal-mode agent run
	// must not do it directly (issue #176). Refusal mirrors the status gate.
	doRun := func(t *testing.T, h *Handler, agentID, target string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/links/"+target+"/confirm", nil)
		r = mux.SetURLVars(r, map[string]string{"id": target})
		pid := projectID
		r = r.WithContext(context.WithValue(r.Context(), ctxRun, &agentruns.Run{ID: "run-1", AgentID: agentID, ProjectID: &pid}))
		w := httptest.NewRecorder()
		h.ConfirmLink(w, r)
		return w
	}

	t.Run("proposal-mode agent run is refused", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := doRun(t, h, "agent-proposal", linkID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
		if len(linkSvc.confirms) != 0 {
			t.Errorf("ConfirmLink cleared suspect for a proposal-mode run: %v", linkSvc.confirms)
		}
	})

	t.Run("direct-mode agent run may confirm", func(t *testing.T) {
		h, linkSvc := newFixture()
		w := doRun(t, h, "agent-direct", linkID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if len(linkSvc.confirms) != 1 {
			t.Errorf("ConfirmLink calls = %v, want exactly [%s]", linkSvc.confirms, linkID)
		}
	})
}
