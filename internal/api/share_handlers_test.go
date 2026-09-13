package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/sharelinks"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Share links (REQ-149), the reviewer role (REQ-150) and the open-source
// showcase (REQ-151) through the handlers, with the services faked.

type shareLinkFake struct {
	sharelinks.Service
	byToken map[string]*sharelinks.Link
}

func (f *shareLinkFake) Resolve(token string) (*sharelinks.Link, error) {
	if l, ok := f.byToken[token]; ok {
		return l, nil
	}
	return nil, sharelinks.ErrInvalidToken
}

type shareExportFake struct {
	exports.Service
	export *exports.ProjectExport
}

func (f *shareExportFake) PrepareExport(projectID string, _ bool) (*exports.ProjectExport, error) {
	return f.export, nil
}

type shareMemberFake struct {
	fakeMemberService
	added []string
}

func (f *shareMemberFake) AddMember(projectID, userID, role string) error {
	f.added = append(f.added, projectID+"/"+userID+"/"+role)
	return nil
}

type shareUserFake struct {
	users.Service
	user *users.User
}

func (f *shareUserFake) GetBySessionToken(token string) (*users.User, error) {
	if token == "good" && f.user != nil {
		return f.user, nil
	}
	return nil, users.ErrSessionInvalid
}

type shareOrgFake struct {
	fakeOrgService
	ids []string
}

func (f *shareOrgFake) ListAll() ([]string, error) { return f.ids, nil }

type shareBaselineFake struct {
	baselines.Service
	list []*baselines.Baseline
}

func (f *shareBaselineFake) ListBaselines(projectID string) ([]*baselines.Baseline, error) {
	return f.list, nil
}

func shareExport() *exports.ProjectExport {
	return &exports.ProjectExport{
		ProjectName: "OpenV Platform",
		Artifacts: []*artifacts.Artifact{
			{ID: "h", Type: artifacts.TypeHeading, Title: "Section"},
			{ID: "r1", Type: "requirement", Title: "One"},
			{ID: "r2", Type: "requirement", Title: "Two"},
			{ID: "t1", Type: "test-case", Title: "Suite"},
		},
	}
}

func shareHandler(t *testing.T) (*Handler, *shareMemberFake) {
	t.Helper()
	member := &shareMemberFake{}
	h := &Handler{
		frontendURL:          "https://app.example",
		publicAPIURL:         "https://app.example",
		invitePreviewLimiter: newRateLimiter(100, 1),
		shareLinkService: &shareLinkFake{byToken: map[string]*sharelinks.Link{
			"pub": {ID: "l1", ProjectID: "p1", Role: sharelinks.RolePublic, Label: "Customer"},
			"rev": {ID: "l2", ProjectID: "p1", Role: sharelinks.RoleReviewer, Label: "Reviewers"},
		}},
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"p1": {ID: "p1", OrgID: "o1", Name: "OpenV Platform", Description: "The requirements of OpenV itself."},
		}},
		orgService:      &shareOrgFake{fakeOrgService: fakeOrgService{plan: orgs.PlanOpenSource}, ids: []string{"o1"}},
		exportService:   &shareExportFake{export: shareExport()},
		memberService:   member,
		userService:     &shareUserFake{user: &users.User{ID: "u1", Email: "r@example.com"}},
		baselineService: &shareBaselineFake{},
	}
	return h, member
}

func tokenRequest(path, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/share/"+token+path, nil)
	return mux.SetURLVars(r, map[string]string{"token": token})
}

func TestOpenShareLinkPublicCarriesTheSnapshot(t *testing.T) {
	h, _ := shareHandler(t)
	w := httptest.NewRecorder()
	h.OpenShareLink(w, tokenRequest("", "pub"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got sharedProject
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Role != "public" || got.Project.Name != "OpenV Platform" || got.Snapshot == nil || len(got.Snapshot.Artifacts) != 4 {
		t.Errorf("unexpected answer: role=%q name=%q snapshot=%v", got.Role, got.Project.Name, got.Snapshot != nil)
	}
	if got.Counts["requirement"] != 2 || got.Counts["test-case"] != 1 || got.Counts["heading"] != 0 {
		t.Errorf("counts = %v", got.Counts)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestOpenShareLinkReviewerNamesTheProjectOnly(t *testing.T) {
	h, _ := shareHandler(t)
	w := httptest.NewRecorder()
	h.OpenShareLink(w, tokenRequest("", "rev"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got sharedProject
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Role != "reviewer" || got.Snapshot != nil || got.Project.Name == "" {
		t.Errorf("reviewer link answered %+v", got)
	}
}

func TestOpenShareLinkUnknownIs404(t *testing.T) {
	h, _ := shareHandler(t)
	for _, path := range []string{"", "/page", "/preview.png"} {
		w := httptest.NewRecorder()
		switch path {
		case "":
			h.OpenShareLink(w, tokenRequest(path, "nope"))
		case "/page":
			h.ShareLinkPage(w, tokenRequest(path, "nope"))
		default:
			h.ShareLinkPreview(w, tokenRequest(path, "nope"))
		}
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d", path, w.Code)
		}
	}
}

func TestShareLinkPageUnfurls(t *testing.T) {
	h, _ := shareHandler(t)
	w := httptest.NewRecorder()
	h.ShareLinkPage(w, tokenRequest("/page", "pub"))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status = %d type = %s", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, want := range []string{
		`og:title" content="OpenV Platform`,
		`og:image" content="https://app.example/api/v1/public/share/pub/preview.png"`,
		`og:url" content="https://app.example/share/pub"`,
		`url=https://app.example/s/pub"`,
		"view only",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	w = httptest.NewRecorder()
	h.ShareLinkPreview(w, tokenRequest("/preview.png", "pub"))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("preview: status = %d type = %s", w.Code, w.Header().Get("Content-Type"))
	}
}

func acceptRequest(token string, signedIn bool) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/share/accept", strings.NewReader(`{"token":"`+token+`"}`))
	if signedIn {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "good"})
	}
	return r
}

func TestAcceptShareLinkGrantsReviewer(t *testing.T) {
	h, member := shareHandler(t)

	w := httptest.NewRecorder()
	h.AcceptShareLink(w, acceptRequest("rev", false))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("signed out: status = %d", w.Code)
	}

	w = httptest.NewRecorder()
	h.AcceptShareLink(w, acceptRequest("pub", true))
	if w.Code != http.StatusBadRequest {
		t.Errorf("public link: status = %d", w.Code)
	}

	w = httptest.NewRecorder()
	h.AcceptShareLink(w, acceptRequest("rev", true))
	if w.Code != http.StatusOK {
		t.Fatalf("reviewer link: status = %d: %s", w.Code, w.Body.String())
	}
	if len(member.added) != 1 || member.added[0] != "p1/u1/"+members.RoleReviewer {
		t.Errorf("memberships added = %v", member.added)
	}
	var got map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["project_id"] != "p1" || got["role"] != members.RoleReviewer {
		t.Errorf("answer = %v", got)
	}

	// An editor keeps the stronger role.
	member.roles = map[string]map[string]string{"p1": {"u1": members.RoleEditor}}
	member.added = nil
	w = httptest.NewRecorder()
	h.AcceptShareLink(w, acceptRequest("rev", true))
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(member.added) != 0 || got["role"] != members.RoleEditor {
		t.Errorf("editor: added = %v role = %q", member.added, got["role"])
	}
}

func TestOpenSourceListingTakesBaselinedProjectsOfOpenSourceWorkspaces(t *testing.T) {
	h, _ := shareHandler(t)
	list := func() []openSourceEntry {
		w := httptest.NewRecorder()
		h.ListOpenSourceProjects(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/open-source/projects", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var out []openSourceEntry
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	if got := list(); len(got) != 0 {
		t.Errorf("no baseline: listed %d", len(got))
	}

	snapshot, _ := json.Marshal(shareExport())
	h.baselineService = &shareBaselineFake{list: []*baselines.Baseline{
		{ID: "b1", ProjectID: "p1", Name: "Design review", Snapshot: snapshot, CreatedAt: time.Now()},
	}}
	got := list()
	if len(got) != 1 || got[0].Baseline != "Design review" || got[0].Counts["requirement"] != 2 {
		t.Fatalf("listed = %+v", got)
	}

	// The snapshot endpoint answers the same project, and 404s a private one.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/open-source/projects/p1", nil)
	w := httptest.NewRecorder()
	h.GetOpenSourceProject(w, mux.SetURLVars(r, map[string]string{"id": "p1"}))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"baseline":{"id":"b1"`) {
		t.Errorf("snapshot: status = %d body = %s", w.Code, w.Body.String())
	}
	h.orgService = &shareOrgFake{fakeOrgService: fakeOrgService{plan: orgs.PlanBusiness}, ids: []string{"o1"}}
	if got := list(); len(got) != 0 {
		t.Errorf("business workspace listed %d", len(got))
	}
	w = httptest.NewRecorder()
	h.GetOpenSourceProject(w, mux.SetURLVars(r, map[string]string{"id": "p1"}))
	if w.Code != http.StatusNotFound {
		t.Errorf("private project: status = %d", w.Code)
	}
}

// The reviewer role passes the viewer gate and fails the editor gate.
func TestReviewerRoleOnTheLadder(t *testing.T) {
	h := &Handler{
		projectService: &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "o1"}}},
		memberService:  &fakeMemberService{roles: map[string]map[string]string{"p1": {"u1": members.RoleReviewer}}},
		orgService:     &fakeOrgService{},
	}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1"}))
	for role, want := range map[string]bool{members.RoleViewer: true, members.RoleReviewer: true, members.RoleEditor: false, members.RoleOwner: false} {
		w := httptest.NewRecorder()
		if got := h.requireProjectRole(w, r, "p1", role); got != want {
			t.Errorf("reviewer against %s gate: %v, want %v", role, got, want)
		}
	}
}
