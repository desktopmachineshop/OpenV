package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
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

// shareBaselineRepo backs the real baselines.DefaultService and answers as
// the Postgres BaselineRepository does: the list is a project's baselines
// newest first without their snapshots (ListByProjectID selects no snapshot
// column), and only GetByID carries one. A fake whose list carried the
// snapshots hid the showcase reading them from the list, where production
// has none, so every project was skipped (REQ-151). listErr and getErr
// fail the list or the load, as a database can.
type shareBaselineRepo struct {
	baselines.Repository
	rows            []*baselines.Baseline
	listErr, getErr error
}

func (f *shareBaselineRepo) ListByProjectID(projectID string) ([]*baselines.Baseline, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*baselines.Baseline
	for _, b := range f.rows {
		if b.ProjectID == projectID {
			summary := *b
			summary.Snapshot = nil
			out = append(out, &summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *shareBaselineRepo) GetByID(id string) (*baselines.Baseline, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, b := range f.rows {
		if b.ID == id {
			return b, nil
		}
	}
	return nil, errors.New("baseline not found")
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
	h := newTestHandler(t, func(h *Handler) {
		h.frontendURL = "https://app.example"
		h.PublicAPIURL = "https://app.example"
		h.invitePreviewLimiter = newRateLimiter(100, 1)
		h.ShareLinkService = &shareLinkFake{byToken: map[string]*sharelinks.Link{
			"pub": {ID: "l1", ProjectID: "p1", Role: sharelinks.RolePublic, Label: "Customer"},
			"rev": {ID: "l2", ProjectID: "p1", Role: sharelinks.RoleReviewer, Label: "Reviewers"},
		}}
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
			"p1": {ID: "p1", OrgID: "o1", Name: "OpenV Platform", Description: "The requirements of OpenV itself."},
		}}
		h.OrgService = &shareOrgFake{fakeOrgService: fakeOrgService{plan: orgs.PlanOpenSource}, ids: []string{"o1"}}
		h.ExportService = &shareExportFake{export: shareExport()}
		h.MemberService = member
		h.UserService = &shareUserFake{user: &users.User{ID: "u1", Email: "r@example.com"}}
		h.BaselineService = baselines.NewService(&shareBaselineRepo{})
	})
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
	openID := func(serve http.HandlerFunc, id, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/public/open-source/projects/"+id+path, nil)
		w := httptest.NewRecorder()
		serve(w, mux.SetURLVars(r, map[string]string{"id": id}))
		return w
	}
	open := func(serve http.HandlerFunc, path string) *httptest.ResponseRecorder { return openID(serve, "p1", path) }
	routes := map[string]http.HandlerFunc{
		"": h.GetOpenSourceProject, "/page": h.OpenSourceProjectPage, "/preview.png": h.OpenSourceProjectPreview,
	}
	// A project with no baseline, or outside an open-source workspace, gets
	// an unknown project's 404 on every route: nothing is published, the
	// live state least of all, and nothing tells it from a missing project
	// (REQ-151). The message leaves the body out: a preview served by
	// mistake would print a PNG.
	notFound := func(when string) {
		t.Helper()
		for path, serve := range routes {
			unknown := openID(serve, "nope", path)
			if w := open(serve, path); w.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound || w.Body.String() != unknown.Body.String() {
				t.Errorf("%s %q: status = %d type = %s, want the unknown project's 404 %s", when, path, w.Code, w.Header().Get("Content-Type"), unknown.Body.String())
			}
		}
	}
	if got := list(); len(got) != 0 {
		t.Errorf("no baseline: listed %d", len(got))
	}
	notFound("no baseline")

	// Two baselines: the showcase shows the newer one, whose snapshot it
	// has to load by id, since the list carries none. The newer one names
	// the project as it was then, and one of its requirements is refined
	// by a project of another workspace.
	kickoff, _ := json.Marshal(&exports.ProjectExport{ProjectName: "OpenV Platform", Artifacts: []*artifacts.Artifact{
		{ID: "r1", Type: "requirement", Title: "One"},
	}})
	reviewed := shareExport()
	reviewed.ProjectDesc = "The requirements of OpenV, as reviewed."
	reviewed.LinkedArtifacts = []*exports.LinkedArtifact{{
		ID: "x1", ProjectID: "q1", ProjectName: "ACME Motor Program", Ref: "REQ-7", Type: "requirement", Title: "Torque from vendor X", Status: "draft",
	}}
	// A baseline keeps the attribute definitions in effect (REQ-5), the
	// workspace's among them, which the showcase does not publish.
	workspace := "o1"
	reviewed.AttributeDefs = []*attributes.Definition{{ID: "d1", OrgID: &workspace, Key: "supplier_margin",
		Label: "Supplier margin", DataType: attributes.DataTypeText}}
	review, _ := json.Marshal(reviewed)
	now := time.Now()
	h.BaselineService = baselines.NewService(&shareBaselineRepo{rows: []*baselines.Baseline{
		{ID: "b1", ProjectID: "p1", Name: "Kick-off", Snapshot: kickoff, CreatedAt: now.Add(-time.Hour)},
		{ID: "b2", ProjectID: "p1", Name: "Design review", Snapshot: review, CreatedAt: now},
	}})
	// Since then the project was renamed and described anew: live work,
	// which stays private until the next baseline like the rest.
	h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
		"p1": {ID: "p1", OrgID: "o1", Name: "Renamed Platform", Description: "A roadmap drafted since the review."},
	}}
	// published fails a body that carries the live name or description, or
	// the other workspace's project and requirement.
	published := func(what, body string) {
		t.Helper()
		for _, private := range []string{"Renamed", "roadmap", "ACME", "vendor X", "supplier_margin", "attribute_definitions"} {
			if strings.Contains(body, private) {
				t.Errorf("%s publishes %q: %s", what, private, body)
			}
		}
	}
	w := httptest.NewRecorder()
	h.ListOpenSourceProjects(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/open-source/projects", nil))
	published("listing", w.Body.String())
	got := list()
	if len(got) != 1 || got[0].BaselineID != "b2" || got[0].Baseline != "Design review" || got[0].Counts["requirement"] != 2 ||
		got[0].Name != "OpenV Platform" || got[0].Description != reviewed.ProjectDesc {
		t.Errorf("listed = %+v", got)
	}

	// The project's public address answers that snapshot as JSON, as the
	// unfurl page and as the card, and 404s once the workspace leaves the
	// open-source plan.
	w = open(h.GetOpenSourceProject, "")
	var view sharedProject
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	if w.Code != http.StatusOK || view.Baseline == nil || view.Baseline.ID != "b2" || view.Snapshot == nil || len(view.Snapshot.Artifacts) != 4 {
		t.Errorf("snapshot: status = %d body = %s", w.Code, w.Body.String())
	}
	if view.Project.Name != "OpenV Platform" || view.Project.Description != reviewed.ProjectDesc {
		t.Errorf("snapshot: project = %+v, want the baseline's name and description", view.Project)
	}
	published("snapshot", w.Body.String())
	w = open(h.OpenSourceProjectPage, "/page")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "2 requirements · 1 test case") ||
		!strings.Contains(w.Body.String(), "OpenV Platform · ") || !strings.Contains(w.Body.String(), reviewed.ProjectDesc) {
		t.Errorf("page: status = %d body = %s", w.Code, w.Body.String())
	}
	published("page", w.Body.String())
	// The card is drawn from the baseline too: exactly this card.
	card, err := renderPreview(previewCard{
		Eyebrow: "Open-source project", Title: "OpenV Platform",
		Lines:  []string{"2 requirements · 1 test case", reviewed.ProjectDesc},
		Footer: "Latest snapshot · Design review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := open(h.OpenSourceProjectPreview, "/preview.png"); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), card) {
		t.Errorf("preview: status = %d type = %s, want the card of the baseline's name and description", w.Code, w.Header().Get("Content-Type"))
	}
	h.OrgService = &shareOrgFake{fakeOrgService: fakeOrgService{plan: orgs.PlanBusiness}, ids: []string{"o1"}}
	if got := list(); len(got) != 0 {
		t.Errorf("business workspace listed %d", len(got))
	}
	notFound("private project")
}

// An open-source project is named as its baseline names it. The current
// description never stands in, not even for a baseline taken while the
// project had none; the current name only for a snapshot that names no
// project.
func TestOpenSourceNamesAProjectAsItsBaselineDoes(t *testing.T) {
	live := &projects.Project{ID: "p1", Name: "Renamed Platform", Description: "A roadmap drafted since the review."}
	for _, tc := range []struct {
		snapshot          exports.ProjectExport
		name, description string
	}{
		{exports.ProjectExport{ProjectName: "OpenV Platform", ProjectDesc: "As reviewed."}, "OpenV Platform", "As reviewed."},
		{exports.ProjectExport{ProjectName: "OpenV Platform"}, "OpenV Platform", ""},
		{exports.ProjectExport{}, "Renamed Platform", ""},
	} {
		if name, description := baselinedIdentity(live, &tc.snapshot); name != tc.name || description != tc.description {
			t.Errorf("snapshot naming %q/%q: published %q/%q, want %q/%q",
				tc.snapshot.ProjectName, tc.snapshot.ProjectDesc, name, description, tc.name, tc.description)
		}
	}
}

// A baseline the showcase cannot read leaves its project unpublished, and
// the server log says which one and why: skipping it silently is how the
// showcase listed nothing without anyone noticing.
func TestOpenSourceLogsABaselineItCannotRead(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	stored := func() []*baselines.Baseline {
		snapshot, _ := json.Marshal(shareExport())
		return []*baselines.Baseline{{ID: "b1", ProjectID: "p1", Name: "Design review", Snapshot: snapshot, CreatedAt: time.Now()}}
	}
	unreadable := stored()
	unreadable[0].Snapshot = json.RawMessage(`{"artifacts": 1}`)
	for name, tc := range map[string]struct {
		repo *shareBaselineRepo
		want []string
	}{
		"list fails":          {&shareBaselineRepo{rows: stored(), listErr: errors.New("connection refused")}, []string{"connection refused"}},
		"baseline load fails": {&shareBaselineRepo{rows: stored(), getErr: errors.New("connection reset")}, []string{"baseline_id=b1"}},
		"snapshot unreadable": {&shareBaselineRepo{rows: unreadable}, []string{"baseline_id=b1", "cannot unmarshal"}},
	} {
		buf.Reset()
		h, _ := shareHandler(t)
		h.BaselineService = baselines.NewService(tc.repo)
		w := httptest.NewRecorder()
		h.ListOpenSourceProjects(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/open-source/projects", nil))
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
			t.Errorf("%s: listing status = %d body = %s", name, w.Code, w.Body.String())
		}
		r := httptest.NewRequest(http.MethodGet, "/api/v1/public/open-source/projects/p1", nil)
		w = httptest.NewRecorder()
		h.GetOpenSourceProject(w, mux.SetURLVars(r, map[string]string{"id": "p1"}))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: address status = %d", name, w.Code)
		}
		logged := buf.String()
		for _, want := range append([]string{"level=ERROR", "project_id=p1"}, tc.want...) {
			if !strings.Contains(logged, want) {
				t.Errorf("%s: log lacks %q: %s", name, want, logged)
			}
		}
	}
}

// The reviewer role passes the viewer gate and fails the editor gate.
func TestReviewerRoleOnTheLadder(t *testing.T) {
	h := newTestHandler(t, func(h *Handler) {
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "o1"}}}
		h.MemberService = &fakeMemberService{roles: map[string]map[string]string{"p1": {"u1": members.RoleReviewer}}}
		h.OrgService = &fakeOrgService{}
	})
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "u1"}))
	for role, want := range map[string]bool{members.RoleViewer: true, members.RoleReviewer: true, members.RoleEditor: false, members.RoleOwner: false} {
		w := httptest.NewRecorder()
		if got := h.requireProjectRole(w, r, "p1", role); got != want {
			t.Errorf("reviewer against %s gate: %v, want %v", role, got, want)
		}
	}
}

// shareLinkRepoFake keeps links in memory and reads each expiry back in
// UTC, as the Postgres ShareLinkRepository does since migration 0051.
type shareLinkRepoFake struct {
	sharelinks.Repository
	links []*sharelinks.Link
}

func (f *shareLinkRepoFake) Create(link *sharelinks.Link, _ string) error {
	cp := *link
	f.links = append(f.links, &cp)
	return nil
}

func (f *shareLinkRepoFake) ListByProject(projectID string) ([]*sharelinks.Link, error) {
	var out []*sharelinks.Link
	for _, l := range f.links {
		if l.ProjectID == projectID {
			cp := *l
			if cp.ExpiresAt != nil {
				utc := cp.ExpiresAt.UTC()
				cp.ExpiresAt = &utc
			}
			out = append(out, &cp)
		}
	}
	return out, nil
}

// A share link's expiry is listed in UTC, and Go writes no JSON time after
// year 9999, so an expiry that is valid as sent but falls in year 10000 in
// UTC is refused with 400. Minted, it left the owner's list answering 200
// with an empty body for good (a link is revoked, never deleted), and the
// Access tab showed none of the project's links.
func TestCreateShareLinkRefusesAnExpiryItCouldNotList(t *testing.T) {
	repo := &shareLinkRepoFake{}
	h, member := shareHandler(t)
	h.ShareLinkService = sharelinks.NewService(repo)
	h.OrgService = nil // no feature gate or plan limit to pass
	member.roles = map[string]map[string]string{"p1": {"u1": members.RoleOwner}}
	call := func(method, body string, handle http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/projects/p1/share-links", strings.NewReader(body))
		r = withUser(mux.SetURLVars(r, map[string]string{"id": "p1"}), "u1")
		w := httptest.NewRecorder()
		handle(w, r)
		return w
	}

	w := call(http.MethodPost, `{"role":"public","expires_at":"9999-12-31T23:00:00-05:00"}`, h.CreateShareLink)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), sharelinks.ErrInvalidExpiry.Error()) {
		t.Errorf("an expiry in year 10000 in UTC: status = %d: %s; want 400 naming the years", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, `{"role":"public","expires_at":"9999-12-31T23:00:00+05:00"}`, h.CreateShareLink)
	if w.Code != http.StatusCreated {
		t.Fatalf("an expiry in year 9999 in UTC: status = %d: %s", w.Code, w.Body.String())
	}

	w = call(http.MethodGet, "", h.ListShareLinks)
	var listed []struct {
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); w.Code != http.StatusOK || err != nil {
		t.Fatalf("the owner's list: status = %d, body %q: %v", w.Code, w.Body.String(), err)
	}
	if len(listed) != 1 || listed[0].ExpiresAt == nil ||
		!listed[0].ExpiresAt.Equal(time.Date(9999, 12, 31, 18, 0, 0, 0, time.UTC)) {
		t.Errorf("the owner's list = %s; want the one link, expiring 9999-12-31T18:00:00Z", w.Body.String())
	}
}
