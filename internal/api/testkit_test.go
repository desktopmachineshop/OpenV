package api

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// newTestHandler builds the Handler a test drives: a zero Handler with each
// option applied in order. It is the one place a test builds a Handler (K6;
// archtest's handler_literals_in_tests ratchet counts any other literal), so
// a new dependency is one option, not an edit to every test.
//
// It keeps exactly what a &Handler{...} literal gave: unlike NewHandler it
// creates no rate limiters, derives no values (cookie SameSite, the trimmed
// frontend URL), reads no environment and leaves billing unwired. An option
// sets the fields a test needs:
//
//	h := newTestHandler(t, func(h *Handler) { h.exportService = fake })
func newTestHandler(t testing.TB, opts ...func(*Handler)) *Handler {
	t.Helper()
	h := &Handler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// --- shared fakes. Each embeds the interface it stands in for and leaves it
// nil, so it implements only the methods tests call: any other method
// panics through the nil interface when called, and a method added to the
// interface is one the embedding already supplies, so widening a Service
// breaks no fake. fakeOrgService embeds the narrow interfaces orgs.Service
// is made of (M12b) instead of orgs.Service itself.

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
	// It answers methods of Workspaces, Membership, ChannelSettings and
	// Alerts below. BillingStore's are left to the fakes that embed this one
	// (billingOrgFake, purchaseOrgFake, planOrgFake); it is embedded so that
	// the fake, with all five, is an orgs.Service.
	orgs.Workspaces
	orgs.Membership
	orgs.BillingStore
	orgs.ChannelSettings
	orgs.Alerts

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
