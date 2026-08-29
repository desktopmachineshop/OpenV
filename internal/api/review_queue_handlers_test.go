package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// reviewQueueLinkService fakes links.Service for the review-queue endpoint:
// ListSuspectByProject records the project it was asked about and answers a
// per-project fixture.
type reviewQueueLinkService struct {
	links.Service
	byProject map[string][]*links.SuspectLink
	asked     []string
}

func (f *reviewQueueLinkService) ListSuspectByProject(projectID string) ([]*links.SuspectLink, error) {
	f.asked = append(f.asked, projectID)
	return f.byProject[projectID], nil
}

// reviewQueueArtifactService fakes artifacts.Service: ListByStatus records the
// (project, status) pair and answers a per-project fixture.
type reviewQueueArtifactService struct {
	artifacts.Service
	byProject map[string][]*artifacts.Artifact
	askedProj []string
	askedStat []string
}

func (f *reviewQueueArtifactService) ListByStatus(projectID, status string) ([]*artifacts.Artifact, error) {
	f.askedProj = append(f.askedProj, projectID)
	f.askedStat = append(f.askedStat, status)
	return f.byProject[projectID], nil
}

func TestReviewQueue(t *testing.T) {
	const (
		projectID = "proj-1"
		orgID     = "org-1"
	)

	newFixture := func() (*Handler, *reviewQueueLinkService, *reviewQueueArtifactService) {
		linkSvc := &reviewQueueLinkService{byProject: map[string][]*links.SuspectLink{
			projectID: {
				{ID: "link-1", Type: "verifies", FromID: "a-from", FromTitle: "REQ-1", FromType: "requirement", ToID: "a-to", ToTitle: "TC-1", ToType: "test-case"},
			},
		}}
		artSvc := &reviewQueueArtifactService{byProject: map[string][]*artifacts.Artifact{
			projectID: {
				{ID: "a-rev", ProjectID: projectID, Type: "requirement", Title: "In review", Status: artifacts.StatusInReview},
			},
		}}
		h := &Handler{
			linkService:     linkSvc,
			artifactService: artSvc,
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				projectID: {ID: projectID, OrgID: orgID},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{orgID: {}}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				projectID: {"viewer": members.RoleViewer},
			}},
		}
		return h, linkSvc, artSvc
	}

	do := func(t *testing.T, h *Handler, userID, target string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+target+"/review-queue", nil)
		r = mux.SetURLVars(r, map[string]string{"id": target})
		if userID != "" {
			r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
		}
		w := httptest.NewRecorder()
		h.ReviewQueue(w, r)
		return w
	}

	t.Run("viewer gets both lists in the documented shape", func(t *testing.T) {
		h, _, artSvc := newFixture()
		w := do(t, h, "viewer", projectID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var got struct {
			SuspectLinks      []links.SuspectLink  `json:"suspect_links"`
			InReviewArtifacts []artifacts.Artifact `json:"in_review_artifacts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got.SuspectLinks) != 1 || got.SuspectLinks[0].ID != "link-1" {
			t.Errorf("suspect_links = %+v, want one link-1", got.SuspectLinks)
		}
		if got.SuspectLinks[0].FromTitle != "REQ-1" || got.SuspectLinks[0].ToTitle != "TC-1" {
			t.Errorf("suspect link not enriched with titles: %+v", got.SuspectLinks[0])
		}
		if len(got.InReviewArtifacts) != 1 || got.InReviewArtifacts[0].ID != "a-rev" {
			t.Errorf("in_review_artifacts = %+v, want one a-rev", got.InReviewArtifacts)
		}
		// The artifact query must filter by the in_review status, not all.
		if len(artSvc.askedStat) != 1 || artSvc.askedStat[0] != artifacts.StatusInReview {
			t.Errorf("ListByStatus status args = %v, want [%s]", artSvc.askedStat, artifacts.StatusInReview)
		}
	})

	t.Run("empty queue serializes as arrays not null", func(t *testing.T) {
		h, linkSvc, artSvc := newFixture()
		linkSvc.byProject = map[string][]*links.SuspectLink{}
		artSvc.byProject = map[string][]*artifacts.Artifact{}
		w := do(t, h, "viewer", projectID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, `"suspect_links":[]`) || !strings.Contains(body, `"in_review_artifacts":[]`) {
			t.Errorf("empty lists must serialize as [], got %s", body)
		}
	})

	t.Run("non-member is refused and no data is read", func(t *testing.T) {
		h, linkSvc, artSvc := newFixture()
		w := do(t, h, "stranger", projectID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", w.Code)
		}
		if len(linkSvc.asked) != 0 || len(artSvc.askedProj) != 0 {
			t.Errorf("data was read despite 403: links=%v artifacts=%v", linkSvc.asked, artSvc.askedProj)
		}
	})

	t.Run("unauthenticated is 401", func(t *testing.T) {
		h, _, _ := newFixture()
		w := do(t, h, "", projectID)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("queries are scoped to the path project", func(t *testing.T) {
		h, linkSvc, artSvc := newFixture()
		do(t, h, "viewer", projectID)
		if len(linkSvc.asked) != 1 || linkSvc.asked[0] != projectID {
			t.Errorf("ListSuspectByProject scope = %v, want [%s]", linkSvc.asked, projectID)
		}
		if len(artSvc.askedProj) != 1 || artSvc.askedProj[0] != projectID {
			t.Errorf("ListByStatus scope = %v, want [%s]", artSvc.askedProj, projectID)
		}
	})
}
