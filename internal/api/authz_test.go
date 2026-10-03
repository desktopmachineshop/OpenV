package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
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
	// created records every CreateProject, in order.
	created []*projects.Project
}

// CreateProject stores the project under the id NewProject gave it.
func (f *fakeProjectService) CreateProject(p *projects.Project) error {
	if f.byID == nil {
		f.byID = map[string]*projects.Project{}
	}
	f.byID[p.ID] = p
	f.created = append(f.created, p)
	return nil
}

func (f *fakeProjectService) GetProject(id string) (*projects.Project, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, errors.New("project not found")
}

// ListProjectsByOrg mirrors the SQL fail-closed contract: an empty orgID
// returns nothing, otherwise only the projects in that workspace.
func (f *fakeProjectService) ListProjectsByOrg(orgID string) ([]*projects.Project, error) {
	if orgID == "" {
		return nil, nil
	}
	var out []*projects.Project
	for _, p := range f.byID {
		if p.OrgID == orgID {
			out = append(out, p)
		}
	}
	return out, nil
}

type fakeOrgService struct {
	orgs.Service
	// roles maps orgID -> userID -> role
	roles map[string]map[string]string

	// Budget CRUD recording (issue #186).
	updatedNames []*string
	budgetCalls  []*float64
	budgetErr    error

	// Logo state recorded by SetLogo / ClearLogo and echoed by Get.
	logoPath, logoMime string

	// Release channel recording (REQ-136): the plan Get answers with, and
	// the channels SetReleaseChannel was asked for.
	plan         string
	channelCalls []string
	// Stable release and per-member previews (REQ-137, REQ-138).
	stableRelease string
	previews      map[string]bool

	// missing lists the ids Get answers orgs.ErrNotFound for: a workspace
	// that does not exist, which only a platform admin's request reaches
	// past the guard.
	missing map[string]bool
	// windowCalls records each SetUpgradeWindow as "day/hour/timezone".
	windowCalls []string
	// setLogoErr, when set, is SetLogo's answer, with nothing recorded.
	setLogoErr error
}

func (f *fakeOrgService) MemberPreview(orgID, userID string) (bool, error) {
	return f.previews[orgID+"/"+userID], nil
}

// SetMemberPreview refuses an account with no membership, as the
// repository does: the choice is stored on the membership.
func (f *fakeOrgService) SetMemberPreview(orgID, userID string, enabled bool) error {
	if f.roles[orgID][userID] == "" {
		return orgs.ErrNotMember
	}
	if f.previews == nil {
		f.previews = map[string]bool{}
	}
	f.previews[orgID+"/"+userID] = enabled
	return nil
}

func (f *fakeOrgService) SetReleaseChannel(id, channel string) (*orgs.Org, error) {
	o, _ := f.Get(id)
	if !orgs.ChannelChoosable(o.BilledPlan) {
		return nil, orgs.ErrChannelLocked
	}
	if channel != "" && !orgs.ValidChannel(channel) {
		return nil, orgs.ErrInvalidChannel
	}
	f.channelCalls = append(f.channelCalls, channel)
	o.ReleaseChannelOverride = channel
	o.ResolveReleaseChannel()
	return o, nil
}

func (f *fakeOrgService) RoleInOrg(orgID, userID string) (string, error) {
	return f.roles[orgID][userID], nil
}

// ListForUser lists the workspaces roles makes the account a member of, by
// id.
func (f *fakeOrgService) ListForUser(userID string) ([]*orgs.Org, error) {
	var ids []string
	for orgID, byUser := range f.roles {
		if byUser[userID] != "" {
			ids = append(ids, orgID)
		}
	}
	sort.Strings(ids)
	list := make([]*orgs.Org, 0, len(ids))
	for _, id := range ids {
		list = append(list, &orgs.Org{ID: id, Role: f.roles[id][userID]})
	}
	return list, nil
}

// Get answers the handlers that read a workspace's effective limits (e.g.
// transient runner lease timings) with a plain free-plan workspace.
func (f *fakeOrgService) Get(id string) (*orgs.Org, error) {
	if f.missing[id] {
		return nil, orgs.ErrNotFound
	}
	plan := f.plan
	if plan == "" {
		plan = orgs.PlanFree
	}
	o := &orgs.Org{ID: id, BilledPlan: plan, LogoPath: f.logoPath, LogoMime: f.logoMime, HasLogo: f.logoPath != "", StableRelease: f.stableRelease}
	o.ResolveReleaseChannel()
	return o, nil
}

func (f *fakeOrgService) SetLogo(id, path, mime string) (*orgs.Org, error) {
	if f.setLogoErr != nil {
		return nil, f.setLogoErr
	}
	f.logoPath, f.logoMime = path, mime
	return f.Get(id)
}

func (f *fakeOrgService) ClearLogo(id string) (*orgs.Org, error) {
	return f.SetLogo(id, "", "")
}

// Budget CRUD recording (issue #186). updatedNames captures UpdateOrg's name
// arg; budgetCalls captures each SetMonthlyBudget budget (nil = cleared);
// budgetErr, when set, is returned by SetMonthlyBudget after recording.
func (f *fakeOrgService) UpdateOrg(id string, name *string) (*orgs.Org, error) {
	o := &orgs.Org{ID: id}
	if name != nil {
		o.Name = *name
	}
	f.updatedNames = append(f.updatedNames, name)
	return o, nil
}

// SetUpgradeWindow records the window and refuses as the service does.
func (f *fakeOrgService) SetUpgradeWindow(id string, day, hour int, timezone string) (*orgs.Org, error) {
	o, err := f.Get(id)
	if err != nil {
		return nil, err
	}
	if !orgs.ChannelChoosable(o.BilledPlan) {
		return nil, orgs.ErrChannelLocked
	}
	if err := orgs.ValidateUpgradeWindow(day, hour, timezone); err != nil {
		return nil, err
	}
	f.windowCalls = append(f.windowCalls, fmt.Sprintf("%d/%d/%s", day, hour, timezone))
	o.UpgradeDay, o.UpgradeHour, o.UpgradeTimezone = day, hour, timezone
	return o, nil
}

// ListMembers answers the workspace's members from roles, for the seat
// count a limit check makes under the tiers.
func (f *fakeOrgService) ListMembers(orgID string) ([]*orgs.Member, error) {
	var out []*orgs.Member
	for userID, role := range f.roles[orgID] {
		out = append(out, &orgs.Member{OrgID: orgID, UserID: userID, Role: role})
	}
	return out, nil
}

func (f *fakeOrgService) SetMonthlyBudget(id string, budget *float64) (*orgs.Org, error) {
	f.budgetCalls = append(f.budgetCalls, budget)
	if f.budgetErr != nil {
		return nil, f.budgetErr
	}
	return &orgs.Org{ID: id, MonthlyBudgetUSD: budget}, nil
}

type fakeMemberService struct {
	members.Service
	// roles maps projectID -> userID -> effective role (direct or team grant)
	roles map[string]map[string]string
}

func (f *fakeMemberService) EffectiveRole(projectID, userID string) (string, error) {
	return f.roles[projectID][userID], nil
}

// AddMember grants the role, as a project's creator is made its owner.
func (f *fakeMemberService) AddMember(projectID, userID, role string) error {
	if f.roles == nil {
		f.roles = map[string]map[string]string{}
	}
	if f.roles[projectID] == nil {
		f.roles[projectID] = map[string]string{}
	}
	f.roles[projectID][userID] = role
	return nil
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
	// Log pushes as they arrived, for the streaming tests.
	appendedEntries  [][]agentruns.LogEntry
	appendedPartials []string
	finished         []string

	startErr  error // returned by MarkRunning after recording the call
	finishErr error // returned by Finish after recording the call

	listFilters []agentruns.ListFilter

	claimRun   *agentruns.Run
	reissueErr error
	reissued   []string
	released   [][2]string

	launchReqs []agentruns.LaunchRequest // every Launch call, in order
	launchRun  *agentruns.Run            // returned by Launch on success
	launchErr  error                     // returned by Launch after recording

	retryRun    *agentruns.Run // returned by Retry on success
	retryErr    error
	retryCalls  []string // source run IDs
	retryUsers  []string // launchedBy (deref'd, "" for nil)
	usageArgs   []string // orgIDs Usage was called with
	usageSince  []time.Time
	usageResult *agentruns.UsageSummary
	usageErr    error

	monthSpend    float64
	monthSpendErr error
}

func (f *fakeRunService) Launch(req agentruns.LaunchRequest) (*agentruns.Run, string, error) {
	f.launchReqs = append(f.launchReqs, req)
	if f.launchErr != nil {
		return nil, "", f.launchErr
	}
	run := f.launchRun
	if run == nil {
		run = &agentruns.Run{ID: "run-new", OrgID: req.OrgID, AgentID: req.AgentID, Status: agentruns.StatusQueued}
	}
	return run, "run-token", nil
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

func (f *fakeRunService) AppendLogs(id string, entries []agentruns.LogEntry, partialText string) (*agentruns.Run, error) {
	f.appended = append(f.appended, id)
	f.appendedEntries = append(f.appendedEntries, entries)
	f.appendedPartials = append(f.appendedPartials, partialText)
	if run, ok := f.byID[id]; ok && partialText != "" {
		run.PartialText = partialText
	}
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

func (f *fakeRunService) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	return f.monthSpend, f.monthSpendErr
}

type fakeArtifactService struct {
	artifacts.Service
	byID       map[string]*artifacts.Artifact
	updateReqs []artifacts.UpdateArtifactRequest
}

func (f *fakeArtifactService) GetArtifact(id string) (*artifacts.Artifact, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, artifacts.ErrNotFound
}

func (f *fakeArtifactService) UpdateArtifact(id string, req artifacts.UpdateArtifactRequest) (*artifacts.Artifact, error) {
	f.updateReqs = append(f.updateReqs, req)
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

func (f *fakeEventRepo) List(orgID, projectID, eventType, beforeID string, limit int) ([]events.Event, error) {
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

// reqWithPersonalKey is reqWithWorker for a member's personal runner key.
func reqWithPersonalKey(orgID, userID string) *http.Request {
	r := reqWithWorker(orgID)
	return r.WithContext(context.WithValue(r.Context(), ctxWorkerUser, userID))
}

func TestRequireProjectRole(t *testing.T) {
	const (
		projectID = "proj-1"
		orgID     = "org-1"
	)

	newHandler := func() *Handler {
		return newTestHandler(t, func(h *Handler) {
			h.projectService = &fakeProjectService{byID: map[string]*projects.Project{
				projectID: {ID: projectID, OrgID: orgID},
			}}
			h.orgService = &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"org-admin": orgs.RoleAdmin, "org-member": orgs.RoleMember},
			}}
			h.memberService = &fakeMemberService{roles: map[string]map[string]string{
				projectID: {
					"direct-editor": members.RoleEditor,
					"team-viewer":   members.RoleViewer,
					"org-member":    "",
				},
			}}
		})
	}

	ownProject := projectID
	foreignProject := "proj-other"

	cases := []struct {
		name     string
		request  *http.Request
		minRole  string
		wantPass bool
		wantCode int    // checked only when !wantPass
		wantErr  string // the refusal's error text, checked when set
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
			wantErr:  "you do not have access to this project",
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
			// No role at all: the project answers as one no row has (I3).
			name:     "non-member gets 404",
			request:  reqWithUser(&users.User{ID: "stranger"}),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
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
			name:     "workspace key reads as a viewer",
			request:  reqWithWorker(orgID),
			minRole:  members.RoleViewer,
			wantPass: true,
		},
		{
			// A workspace key carries a workspace-wide editor's rights (REQ-42),
			// not an owner's: it is refused as a project editor is.
			name:     "workspace key fails owner",
			request:  reqWithWorker(orgID),
			minRole:  members.RoleOwner,
			wantPass: false,
			wantCode: http.StatusForbidden,
			wantErr:  "you do not have access to this project",
		},
		{
			name:     "worker foreign org gets 404",
			request:  reqWithWorker("org-other"),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
		},
		{
			name:     "editor's personal key meets editor",
			request:  reqWithPersonalKey(orgID, "direct-editor"),
			minRole:  members.RoleEditor,
			wantPass: true,
		},
		{
			name:     "editor's personal key fails owner",
			request:  reqWithPersonalKey(orgID, "direct-editor"),
			minRole:  members.RoleOwner,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "viewer's personal key fails editor",
			request:  reqWithPersonalKey(orgID, "team-viewer"),
			minRole:  members.RoleEditor,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "viewer's personal key fails reviewer",
			request:  reqWithPersonalKey(orgID, "team-viewer"),
			minRole:  members.RoleReviewer,
			wantPass: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:     "org admin's personal key passes owner",
			request:  reqWithPersonalKey(orgID, "org-admin"),
			minRole:  members.RoleOwner,
			wantPass: true,
		},
		{
			// A project its holder has no role in answers the key as one
			// no row has (I3), as it answers the holder's session.
			name:     "roleless member's personal key fails editor with 404",
			request:  reqWithPersonalKey(orgID, "org-member"),
			minRole:  members.RoleEditor,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
		},
		{
			// A personal key reads only where its holder's own session
			// would: a member with no role in the project is refused a
			// viewer's read as their session is, as for a project no row
			// has (I3).
			name:     "roleless member's personal key is refused a viewer's read with 404",
			request:  reqWithPersonalKey(orgID, "org-member"),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
		},
		{
			name:     "viewer's personal key reads as a viewer",
			request:  reqWithPersonalKey(orgID, "team-viewer"),
			minRole:  members.RoleViewer,
			wantPass: true,
		},
		{
			name:     "org admin's personal key reads a project it has no role in",
			request:  reqWithPersonalKey(orgID, "org-admin"),
			minRole:  members.RoleViewer,
			wantPass: true,
		},
		{
			name:     "personal key foreign org gets 404",
			request:  reqWithPersonalKey("org-other", "org-admin"),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
		},
		{
			name:     "run token own project passes editor",
			request:  reqWithRun(&agentruns.Run{ID: "run-1", OrgID: orgID, ProjectID: &ownProject}),
			minRole:  members.RoleEditor,
			wantPass: true,
		},
		{
			name:     "run token foreign project gets 404",
			request:  reqWithRun(&agentruns.Run{ID: "run-2", OrgID: orgID, ProjectID: &foreignProject}),
			minRole:  members.RoleEditor,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
		},
		{
			name:     "run token with no project gets 404",
			request:  reqWithRun(&agentruns.Run{ID: "run-4", OrgID: orgID}),
			minRole:  members.RoleViewer,
			wantPass: false,
			wantCode: http.StatusNotFound,
			wantErr:  "project not found",
		},
		{
			// In its own project the run is refused for its role, not its
			// scope, which the refusal names.
			name:     "run token cannot act as owner",
			request:  reqWithRun(&agentruns.Run{ID: "run-3", OrgID: orgID, ProjectID: &ownProject}),
			minRole:  members.RoleOwner,
			wantPass: false,
			wantCode: http.StatusForbidden,
			wantErr:  "agent runs act at most as a project editor",
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
			if want := `{"error":"` + tc.wantErr + `"}`; tc.wantErr != "" && strings.TrimSpace(w.Body.String()) != want {
				t.Fatalf("body = %q, want %s", w.Body.String(), want)
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
// IDs are indistinguishable 404s), and that a personal runner key touches
// only the runs it could claim: its member's own, and the ownerless runs its
// member could see (a run in a project they hold a role in, and, for a
// workspace admin, any run of the workspace, one with no project included).
// Every other run answers the same 404 (OpenV REQ-27, REQ-16).
func TestWorkerRunLifecycleScoping(t *testing.T) {
	launcher := "user-1"
	project := "proj-1"
	newRuns := func() map[string]*agentruns.Run {
		return map[string]*agentruns.Run{
			"run-own":       {ID: "run-own", OrgID: "org-1", Status: agentruns.StatusClaimed, LaunchedBy: &launcher},
			"run-foreign":   {ID: "run-foreign", OrgID: "org-2", Status: agentruns.StatusClaimed},
			"run-unowned":   {ID: "run-unowned", OrgID: "org-1", Status: agentruns.StatusClaimed},
			"run-unowned-p": {ID: "run-unowned-p", OrgID: "org-1", Status: agentruns.StatusClaimed, ProjectID: &project},
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
		{"personal key on an ownerless run in a project its holder views passes", "run-unowned-p", "org-1", "user-2", 0},
		{"personal key on an ownerless run in a project its holder has no role in gets 404", "run-unowned-p", "org-1",
			"user-3", http.StatusNotFound},
		{"personal key on an ownerless run with no project gets 404", "run-unowned", "org-1", "user-2",
			http.StatusNotFound},
		{"workspace admin's personal key on an ownerless run with no project passes", "run-unowned", "org-1",
			"user-admin", 0},
		{"workspace key on an ownerless run with no project passes", "run-unowned", "org-1", "", 0},
	}

	for _, ep := range endpoints {
		for _, tc := range cases {
			t.Run(ep.name+"/"+tc.name, func(t *testing.T) {
				runSvc := &fakeRunService{byID: newRuns()}
				h := newTestHandler(t, func(h *Handler) {
					h.runService = runSvc
					h.orgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {
						"user-1": orgs.RoleMember, "user-2": orgs.RoleMember, "user-3": orgs.RoleMember,
						"user-admin": orgs.RoleAdmin}}}
					h.memberService = &fakeMemberService{roles: map[string]map[string]string{
						project: {"user-2": members.RoleViewer}}}
				})
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
func linkTestHandler(t *testing.T, linkSvc *fakeLinkService) *Handler {
	return newTestHandler(t, func(h *Handler) {
		h.projectService = &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
			"proj-b": {ID: "proj-b", OrgID: "org-1"},
		}}
		h.orgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}}
		h.memberService = &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"editor-a": members.RoleEditor, "editor-both": members.RoleEditor, "editor-a-viewer-b": members.RoleEditor},
			"proj-b": {"editor-both": members.RoleEditor, "editor-a-viewer-b": members.RoleViewer},
		}}
		h.artifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"art-a": {ID: "art-a", ProjectID: "proj-a", Type: "requirement"},
			"art-b": {ID: "art-b", ProjectID: "proj-b", Type: "requirement"},
		}}
		h.linkService = linkSvc
		h.chatterService = &fakeChatterService{}
	})
}

// TestCreateLinkCrossProject locks in that creating a link whose target
// artifact lives in another project requires editor rights on that project
// too — the target gets a version bump and chatter written. A target in a
// project the caller has no role in at all answers as a target no row has
// (I3), so the refusal tells nothing of whether it exists.
func TestCreateLinkCrossProject(t *testing.T) {
	body := `{"from_id":"art-a","to_id":"art-b","type":"relates-to"}`

	for _, tc := range []struct {
		name, user string
		wantCode   int
		wantBody   string
	}{
		{"editor on source project only is told the target is not there", "editor-a",
			http.StatusBadRequest, `{"error":"target artifact not found"}`},
		{"viewer of the target project is denied", "editor-a-viewer-b",
			http.StatusForbidden, `{"error":"you do not have access to this project"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linkSvc := &fakeLinkService{}
			h := linkTestHandler(t, linkSvc)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
			r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: tc.user}))
			h.CreateLink(w, r)
			if w.Code != tc.wantCode || strings.TrimSpace(w.Body.String()) != tc.wantBody {
				t.Fatalf("answer = %d %q, want %d %s", w.Code, w.Body.String(), tc.wantCode, tc.wantBody)
			}
			if len(linkSvc.created) != 0 {
				t.Fatalf("link was created despite missing rights on the target project")
			}
		})
	}

	t.Run("editor on both projects passes", func(t *testing.T) {
		linkSvc := &fakeLinkService{}
		h := linkTestHandler(t, linkSvc)
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
		h := linkTestHandler(t, linkSvc)
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
		h := linkTestHandler(t, linkSvc)
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
	h := newTestHandler(t, func(h *Handler) {
		h.projectService = &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
		}}
		h.orgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}}
		h.memberService = &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"editor-a": members.RoleEditor},
		}}
		h.artifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"art-a": {ID: "art-a", ProjectID: "proj-a", Type: "requirement", Title: "Login required"},
			"art-b": {ID: "art-b", ProjectID: "proj-a", Type: "requirement", Title: "Password policy"},
		}}
		h.linkService = linkSvc
		h.chatterService = chatterSvc
	})

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

// TestUpdateArtifactHandlerParentPresence is the HTTP half of the issue-#172
// contract: the handler's JSON decode must hand the domain layer a
// presence-aware parent_id — absent stays not-present (keep parent), null
// arrives present-and-nil (move to root), a string arrives present-and-set.
// Before the fix, a parent-less PUT (e.g. from the MCP update_artifact tool)
// decoded to the same nil as explicit null and reparented the artifact to
// the root.
func TestUpdateArtifactHandlerParentPresence(t *testing.T) {
	newFixture := func() (*Handler, *fakeArtifactService) {
		parent := "art-parent"
		artifactSvc := &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"art-a": {ID: "art-a", ProjectID: "proj-a", ParentID: &parent, Type: "requirement", Title: "Child"},
		}}
		h := newTestHandler(t, func(h *Handler) {
			h.projectService = &fakeProjectService{byID: map[string]*projects.Project{
				"proj-a": {ID: "proj-a", OrgID: "org-1"},
			}}
			h.orgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}}
			h.memberService = &fakeMemberService{roles: map[string]map[string]string{
				"proj-a": {"editor-a": members.RoleEditor},
			}}
			h.artifactService = artifactSvc
			h.linkService = &fakeLinkService{}
			h.chatterService = &fakeChatterService{}
		})
		return h, artifactSvc
	}

	put := func(t *testing.T, h *Handler, body string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/art-a", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor-a"}))
		r = mux.SetURLVars(r, map[string]string{"id": "art-a"})
		w := httptest.NewRecorder()
		h.UpdateArtifact(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
	}

	t.Run("omitted parent_id decodes as not present", func(t *testing.T) {
		h, svc := newFixture()
		put(t, h, `{"title":"Renamed"}`)
		if len(svc.updateReqs) != 1 {
			t.Fatalf("UpdateArtifact calls = %d, want 1", len(svc.updateReqs))
		}
		if svc.updateReqs[0].ParentID.Present {
			t.Error("omitted parent_id reached the domain as present (would reparent to root)")
		}
	})

	t.Run("null parent_id decodes as present nil", func(t *testing.T) {
		h, svc := newFixture()
		put(t, h, `{"parent_id":null}`)
		req := svc.updateReqs[0]
		if !req.ParentID.Present || req.ParentID.Value != nil {
			t.Errorf("parent_id:null decoded as %+v, want present with nil value (move to root)", req.ParentID)
		}
	})

	t.Run("set parent_id decodes as present value", func(t *testing.T) {
		h, svc := newFixture()
		put(t, h, `{"parent_id":"art-new-parent"}`)
		req := svc.updateReqs[0]
		if !req.ParentID.Present || req.ParentID.Value == nil || *req.ParentID.Value != "art-new-parent" {
			t.Errorf("parent_id decoded as %+v, want present art-new-parent", req.ParentID)
		}
	})
}

// TestListDomainEventsScoping locks in that, without a project_id filter, org
// admins see the whole workspace audit while plain members only see events
// for projects they can access.
func TestListDomainEventsScoping(t *testing.T) {
	const orgID = "org-1"
	newHandler := func() *Handler {
		return newTestHandler(t, func(h *Handler) {
			h.orgService = &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}}
			h.memberService = &fakeMemberService{roles: map[string]map[string]string{
				"proj-1": {"member": members.RoleViewer},
			}}
			h.eventRepo = &fakeEventRepo{byOrg: map[string][]events.Event{
				orgID: {
					{ID: "e1", OrgID: orgID, ProjectID: "proj-1", EventType: "artifact.updated"},
					{ID: "e2", OrgID: orgID, ProjectID: "proj-2", EventType: "artifact.updated"},
					{ID: "e3", OrgID: orgID, ProjectID: "", EventType: "agentrun.finished"},
				},
			}}
		})
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
		h := newTestHandler(t, func(h *Handler) {
			h.runService = runSvc
			h.orgService = &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}}
			h.projectService = &fakeProjectService{byID: map[string]*projects.Project{
				"proj-1": {ID: "proj-1", OrgID: orgID},
			}}
			h.memberService = &fakeMemberService{roles: map[string]map[string]string{
				"proj-1": {"member": members.RoleViewer},
			}}
		})
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

func (f *fakeAgentService) GetBySlug(orgID, slug string) (*agents.Agent, error) {
	for _, a := range f.byID {
		if a.Slug == slug {
			return a, nil
		}
	}
	return nil, nil
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
		return newTestHandler(t, func(h *Handler) {
			h.runService = runSvc
			h.agentService = agentSvc
		}), runSvc
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
