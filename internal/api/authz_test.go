package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
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

func (f *fakeMemberService) ProjectIDsForUser(userID string) ([]string, error) {
	var ids []string
	for projectID, byUser := range f.roles {
		if byUser[userID] != "" {
			ids = append(ids, projectID)
		}
	}
	return ids, nil
}

type fakeRunService struct {
	agentruns.Service
	byID     map[string]*agentruns.Run
	started  []string
	appended []string
	finished []string

	startErr  error // returned by MarkRunning after recording the call
	finishErr error // returned by Finish after recording the call

	listFilters []agentruns.ListFilter

	claimRun   *agentruns.Run
	reissueErr error
	reissued   []string
	released   [][2]string

	retryRun    *agentruns.Run // returned by Retry on success
	retryErr    error
	retryCalls  []string // source run IDs
	retryUsers  []string // launchedBy (deref'd, "" for nil)
	usageArgs   []string // orgIDs Usage was called with
	usageSince  []time.Time
	usageResult *agentruns.UsageSummary
	usageErr    error
}

func (f *fakeRunService) Get(id string) (*agentruns.Run, error) {
	if run, ok := f.byID[id]; ok {
		return run, nil
	}
	return nil, agentruns.ErrNotFound
}

func (f *fakeRunService) MarkRunning(id string) error {
	f.started = append(f.started, id)
	return f.startErr
}

func (f *fakeRunService) AppendLogs(id string, entries []agentruns.LogEntry) (*agentruns.Run, error) {
	f.appended = append(f.appended, id)
	return f.byID[id], nil
}

func (f *fakeRunService) Finish(id string, req agentruns.FinishRequest) (*agentruns.Run, error) {
	f.finished = append(f.finished, id)
	if f.finishErr != nil {
		return nil, f.finishErr
	}
	return f.byID[id], nil
}

func (f *fakeRunService) List(filter agentruns.ListFilter) ([]*agentruns.Run, error) {
	f.listFilters = append(f.listFilters, filter)
	return []*agentruns.Run{}, nil
}

func (f *fakeRunService) Claim(workerID, orgID, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*agentruns.Run, error) {
	return f.claimRun, nil
}

func (f *fakeRunService) ReissueToken(runID string) (string, error) {
	f.reissued = append(f.reissued, runID)
	if f.reissueErr != nil {
		return "", f.reissueErr
	}
	return "fresh-token", nil
}

func (f *fakeRunService) ReleaseClaim(runID, workerID string) error {
	f.released = append(f.released, [2]string{runID, workerID})
	return nil
}

func (f *fakeRunService) Retry(sourceRunID string, launchedBy *string) (*agentruns.Run, error) {
	f.retryCalls = append(f.retryCalls, sourceRunID)
	user := ""
	if launchedBy != nil {
		user = *launchedBy
	}
	f.retryUsers = append(f.retryUsers, user)
	if f.retryErr != nil {
		return nil, f.retryErr
	}
	return f.retryRun, nil
}

func (f *fakeRunService) Usage(orgID string, since time.Time) (*agentruns.UsageSummary, error) {
	f.usageArgs = append(f.usageArgs, orgID)
	f.usageSince = append(f.usageSince, since)
	if f.usageErr != nil {
		return nil, f.usageErr
	}
	if f.usageResult != nil {
		return f.usageResult, nil
	}
	return &agentruns.UsageSummary{ByAgent: []agentruns.AgentUsage{}, ByDay: []agentruns.DailyUsage{}}, nil
}

type fakeArtifactService struct {
	artifacts.Service
	byID map[string]*artifacts.Artifact
}

func (f *fakeArtifactService) GetArtifact(id string) (*artifacts.Artifact, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("artifact not found")
}

func (f *fakeArtifactService) UpdateArtifact(id string, req artifacts.UpdateArtifactRequest) (*artifacts.Artifact, error) {
	return f.byID[id], nil
}

type fakeLinkService struct {
	links.Service
	created []*links.Link
}

func (f *fakeLinkService) CreateLink(link *links.Link) error {
	f.created = append(f.created, link)
	return nil
}

func (f *fakeLinkService) GetLinksFrom(artifactID string) ([]*links.Link, error) { return nil, nil }
func (f *fakeLinkService) GetLinksTo(artifactID string) ([]*links.Link, error)   { return nil, nil }

type fakeChatterService struct {
	chatter.Service
	entries []*chatter.ChatterEntry
}

func (f *fakeChatterService) CreateEntry(entry *chatter.ChatterEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

type fakeEventRepo struct {
	events.Repository
	byOrg map[string][]events.Event
}

func (f *fakeEventRepo) List(orgID, projectID, eventType string, limit int) ([]events.Event, error) {
	var out []events.Event
	for _, e := range f.byOrg[orgID] {
		if projectID != "" && e.ProjectID != projectID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
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

// workerRunReq builds a worker-authenticated request against a run lifecycle
// endpoint ({id} route var), optionally as a personal runner key (userID).
func workerRunReq(body, orgID, userID, runID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxWorkerOrg, orgID)
	if userID != "" {
		ctx = context.WithValue(ctx, ctxWorkerUser, userID)
	}
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": runID})
}

// TestWorkerRunLifecycleScoping locks in that a worker key can only drive the
// start/logs/finish lifecycle of runs in its own org (foreign and unknown run
// IDs are indistinguishable 404s), and that a personal runner key cannot
// touch runs launched by another member.
func TestWorkerRunLifecycleScoping(t *testing.T) {
	launcher := "user-1"
	newRuns := func() map[string]*agentruns.Run {
		return map[string]*agentruns.Run{
			"run-own":     {ID: "run-own", OrgID: "org-1", Status: agentruns.StatusClaimed, LaunchedBy: &launcher},
			"run-foreign": {ID: "run-foreign", OrgID: "org-2", Status: agentruns.StatusClaimed},
			"run-unowned": {ID: "run-unowned", OrgID: "org-1", Status: agentruns.StatusClaimed},
		}
	}

	endpoints := []struct {
		name  string
		call  func(h *Handler, w http.ResponseWriter, r *http.Request)
		body  string
		calls func(f *fakeRunService) int
	}{
		{"start", (*Handler).StartAgentRun, "", func(f *fakeRunService) int { return len(f.started) }},
		{"logs", (*Handler).AppendAgentRunLogs, "[]", func(f *fakeRunService) int { return len(f.appended) }},
		{"finish", (*Handler).FinishAgentRun, `{"status":"succeeded"}`, func(f *fakeRunService) int { return len(f.finished) }},
	}

	cases := []struct {
		name       string
		runID      string
		workerOrg  string
		workerUser string
		wantCode   int // 0 means success
	}{
		{"own org passes", "run-own", "org-1", "", 0},
		{"foreign org run gets 404", "run-own", "org-2", "", http.StatusNotFound},
		{"unknown run gets 404", "run-missing", "org-1", "", http.StatusNotFound},
		{"personal key on another member's run gets 404", "run-own", "org-1", "user-2", http.StatusNotFound},
		{"personal key on own run passes", "run-own", "org-1", "user-1", 0},
		{"personal key on ownerless run passes", "run-unowned", "org-1", "user-2", 0},
	}

	for _, ep := range endpoints {
		for _, tc := range cases {
			t.Run(ep.name+"/"+tc.name, func(t *testing.T) {
				runSvc := &fakeRunService{byID: newRuns()}
				h := &Handler{runService: runSvc}
				w := httptest.NewRecorder()
				ep.call(h, w, workerRunReq(ep.body, tc.workerOrg, tc.workerUser, tc.runID))
				if tc.wantCode == 0 {
					if w.Code >= 300 {
						t.Fatalf("status = %d, want success (body %q)", w.Code, w.Body.String())
					}
					if got := ep.calls(runSvc); got != 1 {
						t.Fatalf("service called %d times, want 1", got)
					}
					return
				}
				if w.Code != tc.wantCode {
					t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
				}
				if got := ep.calls(runSvc); got != 0 {
					t.Fatalf("service called %d times on denied request, want 0", got)
				}
			})
		}
	}
}

// linkTestHandler wires a handler with two projects in one org: the user
// "editor-a" can edit only proj-a, "editor-both" can edit both. Artifact
// art-a lives in proj-a, art-b in proj-b.
func linkTestHandler(linkSvc *fakeLinkService) *Handler {
	return &Handler{
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
			"proj-b": {ID: "proj-b", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"editor-a": members.RoleEditor, "editor-both": members.RoleEditor},
			"proj-b": {"editor-both": members.RoleEditor},
		}},
		artifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"art-a": {ID: "art-a", ProjectID: "proj-a", Type: "requirement"},
			"art-b": {ID: "art-b", ProjectID: "proj-b", Type: "requirement"},
		}},
		linkService:    linkSvc,
		chatterService: &fakeChatterService{},
	}
}

// TestCreateLinkCrossProject locks in that creating a link whose target
// artifact lives in another project requires editor rights on that project
// too — the target gets a version bump and chatter written.
func TestCreateLinkCrossProject(t *testing.T) {
	body := `{"from_id":"art-a","to_id":"art-b","type":"relates-to"}`

	t.Run("editor on source project only is denied", func(t *testing.T) {
		linkSvc := &fakeLinkService{}
		h := linkTestHandler(linkSvc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor-a"}))
		h.CreateLink(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusForbidden, w.Body.String())
		}
		if len(linkSvc.created) != 0 {
			t.Fatalf("link was created despite missing rights on the target project")
		}
	})

	t.Run("editor on both projects passes", func(t *testing.T) {
		linkSvc := &fakeLinkService{}
		h := linkTestHandler(linkSvc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor-both"}))
		h.CreateLink(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusCreated, w.Body.String())
		}
		if len(linkSvc.created) != 1 {
			t.Fatalf("created %d links, want 1", len(linkSvc.created))
		}
	})
}

// TestManagedLinkChangesCrossProject locks in the same rule for the managed
// link-add path used by UpdateArtifact (PendingLinkAdds).
func TestManagedLinkChangesCrossProject(t *testing.T) {
	toAdd := []interface{}{map[string]interface{}{
		"from_id": "art-a",
		"to_id":   "art-b",
		"type":    "relates-to",
	}}

	t.Run("editor on base project only cannot add cross-project link", func(t *testing.T) {
		linkSvc := &fakeLinkService{}
		h := linkTestHandler(linkSvc)
		r := reqWithUser(&users.User{ID: "editor-a"})
		if _, err := h.processManagedLinkChanges(r, "proj-a", "art-a", toAdd, nil); err != nil {
			t.Fatalf("processManagedLinkChanges: %v", err)
		}
		if len(linkSvc.created) != 0 {
			t.Fatalf("cross-project link was created despite missing rights on the target project")
		}
	})

	t.Run("editor on both projects can add cross-project link", func(t *testing.T) {
		linkSvc := &fakeLinkService{}
		h := linkTestHandler(linkSvc)
		r := reqWithUser(&users.User{ID: "editor-both"})
		if _, err := h.processManagedLinkChanges(r, "proj-a", "art-a", toAdd, nil); err != nil {
			t.Fatalf("processManagedLinkChanges: %v", err)
		}
		if len(linkSvc.created) != 1 {
			t.Fatalf("created %d links, want 1", len(linkSvc.created))
		}
	})
}

// TestUpdateArtifactAddedLinkChatter locks in that the version-change chatter
// entry written by UpdateArtifact describes added links. PendingLinkAdds
// entries are link objects ({from_id,to_id,type}), not link IDs — the old
// code type-asserted them to string, never matched, and added-link details
// were silently empty.
func TestUpdateArtifactAddedLinkChatter(t *testing.T) {
	linkSvc := &fakeLinkService{}
	chatterSvc := &fakeChatterService{}
	h := &Handler{
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"editor-a": members.RoleEditor},
		}},
		artifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"art-a": {ID: "art-a", ProjectID: "proj-a", Type: "requirement", Title: "Login required"},
			"art-b": {ID: "art-b", ProjectID: "proj-a", Type: "requirement", Title: "Password policy"},
		}},
		linkService:    linkSvc,
		chatterService: chatterSvc,
	}

	body := `{"pendingLinkAdds":[{"from_id":"art-a","to_id":"art-b","type":"relates-to"}]}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/art-a", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor-a"}))
	r = mux.SetURLVars(r, map[string]string{"id": "art-a"})
	w := httptest.NewRecorder()
	h.UpdateArtifact(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if len(linkSvc.created) != 1 {
		t.Fatalf("created %d links, want 1", len(linkSvc.created))
	}
	// The updated artifact gets a version-change entry (the linked artifact
	// gets its own auto-version entry, not under test here).
	var msg string
	for _, entry := range chatterSvc.entries {
		if entry.ArtifactID == "art-a" && entry.EntryType == "version-change" {
			msg = entry.Message
		}
	}
	if msg == "" {
		t.Fatalf("no version-change chatter entry written for art-a (entries: %+v)", chatterSvc.entries)
	}
	if !strings.Contains(msg, "relates-to: Password policy (added)") {
		t.Fatalf("chatter message %q is missing the added-link detail", msg)
	}
}

// TestListDomainEventsScoping locks in that, without a project_id filter, org
// admins see the whole workspace audit while plain members only see events
// for projects they can access.
func TestListDomainEventsScoping(t *testing.T) {
	const orgID = "org-1"
	newHandler := func() *Handler {
		return &Handler{
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				"proj-1": {"member": members.RoleViewer},
			}},
			eventRepo: &fakeEventRepo{byOrg: map[string][]events.Event{
				orgID: {
					{ID: "e1", OrgID: orgID, ProjectID: "proj-1", EventType: "artifact.updated"},
					{ID: "e2", OrgID: orgID, ProjectID: "proj-2", EventType: "artifact.updated"},
					{ID: "e3", OrgID: orgID, ProjectID: "", EventType: "agentrun.finished"},
				},
			}},
		}
	}

	listEvents := func(t *testing.T, h *Handler, userID string) []events.Event {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.ListDomainEvents(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var list []events.Event
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		return list
	}

	t.Run("org admin sees all events", func(t *testing.T) {
		list := listEvents(t, newHandler(), "admin")
		if len(list) != 3 {
			t.Fatalf("admin saw %d events, want 3", len(list))
		}
	})

	t.Run("member only sees accessible projects' events", func(t *testing.T) {
		list := listEvents(t, newHandler(), "member")
		if len(list) != 1 || list[0].ProjectID != "proj-1" {
			t.Fatalf("member saw %+v, want only the proj-1 event", list)
		}
	})
}

// TestListAgentRunsScoping locks in that the run listing pushes workspace and
// launcher scoping into the repository filter (so predicates apply before the
// SQL LIMIT) instead of post-filtering a page that a busy sibling workspace
// could starve: org admins get the whole workspace, plain members without a
// project filter only their own runs.
func TestListAgentRunsScoping(t *testing.T) {
	const orgID = "org-1"
	newFixture := func() (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{}
		h := &Handler{
			runService: runSvc,
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}},
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				"proj-1": {ID: "proj-1", OrgID: orgID},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				"proj-1": {"member": members.RoleViewer},
			}},
		}
		return h, runSvc
	}

	listRuns := func(t *testing.T, h *Handler, userID, query string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs"+query, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.ListAgentRuns(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
	}

	t.Run("member without project filter is scoped to own runs in own org", func(t *testing.T) {
		h, runSvc := newFixture()
		listRuns(t, h, "member", "")
		if len(runSvc.listFilters) != 1 {
			t.Fatalf("List called %d times, want 1", len(runSvc.listFilters))
		}
		filter := runSvc.listFilters[0]
		if filter.OrgID != orgID {
			t.Errorf("filter.OrgID = %q, want %q", filter.OrgID, orgID)
		}
		if filter.LaunchedBy != "member" {
			t.Errorf("filter.LaunchedBy = %q, want the member's own id", filter.LaunchedBy)
		}
	})

	t.Run("org admin without project filter sees whole workspace", func(t *testing.T) {
		h, runSvc := newFixture()
		listRuns(t, h, "admin", "")
		filter := runSvc.listFilters[0]
		if filter.OrgID != orgID {
			t.Errorf("filter.OrgID = %q, want %q", filter.OrgID, orgID)
		}
		if filter.LaunchedBy != "" {
			t.Errorf("filter.LaunchedBy = %q, want unset for an admin", filter.LaunchedBy)
		}
	})

	t.Run("member with project filter sees the whole project", func(t *testing.T) {
		h, runSvc := newFixture()
		listRuns(t, h, "member", "?project_id=proj-1")
		filter := runSvc.listFilters[0]
		if filter.ProjectID != "proj-1" || filter.OrgID != orgID {
			t.Errorf("filter = %+v, want proj-1 scoped to %s", filter, orgID)
		}
		if filter.LaunchedBy != "" {
			t.Errorf("filter.LaunchedBy = %q, want unset when a project filter passed the role check", filter.LaunchedBy)
		}
	})
}

type fakeAgentService struct {
	agents.Service
	byID map[string]*agents.Agent
}

func (f *fakeAgentService) Get(id string) (*agents.Agent, error) {
	return f.byID[id], nil
}

// TestClaimHandshakeFailureReleasesRun locks in that when the claim handshake
// fails after a successful Claim (agent lookup or token mint), the handler
// rolls the claim back to queued instead of stranding the run until the
// stale reaper.
func TestClaimHandshakeFailureReleasesRun(t *testing.T) {
	newClaim := func(agentKnown bool, reissueErr error) (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{
			claimRun:   &agentruns.Run{ID: "run-1", OrgID: "org-1", AgentID: "agent-1", Status: agentruns.StatusClaimed, WorkerID: "w-1"},
			reissueErr: reissueErr,
		}
		agentSvc := &fakeAgentService{byID: map[string]*agents.Agent{}}
		if agentKnown {
			agentSvc.byID["agent-1"] = &agents.Agent{ID: "agent-1", Name: "Agent", Provider: "claude"}
		}
		return &Handler{runService: runSvc, agentService: agentSvc}, runSvc
	}

	claim := func(t *testing.T, h *Handler) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/claim", strings.NewReader(`{"worker_id":"w-1"}`))
		r = r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, "org-1"))
		w := httptest.NewRecorder()
		h.ClaimAgentRun(w, r)
		return w
	}

	t.Run("missing agent releases the claim", func(t *testing.T) {
		h, runSvc := newClaim(false, nil)
		if w := claim(t, h); w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.released) != 1 || runSvc.released[0] != [2]string{"run-1", "w-1"} {
			t.Fatalf("released = %v, want the claimed run handed back for worker w-1", runSvc.released)
		}
	})

	t.Run("token mint failure releases the claim", func(t *testing.T) {
		h, runSvc := newClaim(true, errors.New("token store down"))
		if w := claim(t, h); w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.released) != 1 {
			t.Fatalf("released = %v, want exactly one release", runSvc.released)
		}
	})

	t.Run("successful handshake keeps the claim", func(t *testing.T) {
		h, runSvc := newClaim(true, nil)
		w := claim(t, h)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.released) != 0 {
			t.Fatalf("released = %v, want no release on success", runSvc.released)
		}
		if !strings.Contains(w.Body.String(), "fresh-token") {
			t.Fatalf("response should carry the reissued run token: %q", w.Body.String())
		}
	})
}
