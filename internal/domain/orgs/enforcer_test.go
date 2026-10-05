package orgs

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The limits enforcer counts and decides through its ports, and returns a
// read it could not make as that read's error: refusing nothing on one is
// the API's choice (failOpen in internal/api/limits.go), not the
// enforcer's. internal/api's limits, plan_gates and read-only tests pin the
// API's answers end to end; these pin the enforcer's own, reads included.
// Each workspace sets its ceilings in its own limits, which win over every
// other layer, so no test here changes the deployment policy.

// Service is what the API hands the enforcer as its workspace reads.
var _ WorkspaceReads = Service(nil)

// enforcerReads is a workspace store the enforcer reads, recording each
// read in order, and each port's, in calls.
type enforcerReads struct {
	orgs       map[string]*Org
	members    map[string]int
	pending    map[string]int
	projects   map[string]int
	forUser    []*Org
	minutes    int
	evidence   int64
	getErr     error
	membersErr error
	pendingErr error
	projectErr error
	forUserErr error
	minutesErr error
	since      time.Time
	calls      []string
}

func (f *enforcerReads) Get(id string) (*Org, error) {
	f.calls = append(f.calls, "Get "+id)
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.orgs[id], nil
}

func (f *enforcerReads) ListMembers(orgID string) ([]*Member, error) {
	f.calls = append(f.calls, "ListMembers "+orgID)
	if f.membersErr != nil {
		return nil, f.membersErr
	}
	return make([]*Member, f.members[orgID]), nil
}

func (f *enforcerReads) ListForUser(userID string) ([]*Org, error) {
	f.calls = append(f.calls, "ListForUser "+userID)
	if f.forUserErr != nil {
		return nil, f.forUserErr
	}
	return f.forUser, nil
}

// enforcer is a LimitEnforcer over f with every port set.
func (f *enforcerReads) enforcer() *LimitEnforcer {
	return &LimitEnforcer{
		Orgs: f,
		PendingInvitations: func(orgID string) (int, error) {
			f.calls = append(f.calls, "PendingInvitations "+orgID)
			return f.pending[orgID], f.pendingErr
		},
		Projects: func(orgID string) (int, error) {
			f.calls = append(f.calls, "Projects "+orgID)
			return f.projects[orgID], f.projectErr
		},
		RunnerMinutes: func(orgID string, since time.Time) (int, error) {
			f.calls = append(f.calls, "RunnerMinutes "+orgID)
			f.since = since
			return f.minutes, f.minutesErr
		},
		EvidenceBytes: func(orgID string) (int64, error) {
			f.calls = append(f.calls, "EvidenceBytes "+orgID)
			return f.evidence, nil
		},
	}
}

func (f *enforcerReads) wantCalls(t *testing.T, want ...string) {
	t.Helper()
	if len(want) == 0 {
		want = nil
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("reads %q, want %q", f.calls, want)
	}
	f.calls = nil
}

// sharedOrg is a shared workspace on the single plan with the ceilings
// given in its own limits.
func sharedOrg(id string, limits map[string]interface{}) *Org {
	return &Org{ID: id, OrgType: TypeCompany, BilledPlan: PlanSingle, Limits: limits}
}

func asLimitError(t *testing.T, err error) *LimitError {
	t.Helper()
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("err = %v, want a *LimitError", err)
	}
	return limitErr
}

func TestTheEnforcerReadsNothingWithoutAWorkspace(t *testing.T) {
	atPolicyDefaults(t)

	none := &LimitEnforcer{}
	if org, err := none.Org("w"); org != nil || err != nil {
		t.Errorf("Org with no workspace reads = %v, %v", org, err)
	}
	if err := none.CheckFlag("w", LimitTeams); err != nil {
		t.Errorf("CheckFlag: %v", err)
	}
	if err := none.CheckSeats("w", 1); err != nil {
		t.Errorf("CheckSeats: %v", err)
	}
	if err := none.CheckProjectCount("w"); err != nil {
		t.Errorf("CheckProjectCount: %v", err)
	}
	if err := none.CheckSharedWorkspaceCount("u"); err != nil {
		t.Errorf("CheckSharedWorkspaceCount: %v", err)
	}
	if n, err := none.LeaseMinutesAllowed("w", 30); n != 30 || err != nil {
		t.Errorf("LeaseMinutesAllowed = %d, %v, want the whole lease", n, err)
	}
	if over, err := none.OverPlan(nil); over != nil || err != nil {
		t.Errorf("OverPlan(nil) = %v, %v", over, err)
	}

	// No id, or no person, reads nothing either.
	f := &enforcerReads{}
	e := f.enforcer()
	_ = e.CheckFlag("", LimitTeams)
	_ = e.CheckSeats("", 1)
	_ = e.CheckProjectCount("")
	_ = e.CheckSharedWorkspaceCount("")
	_, _ = e.LeaseMinutesAllowed("", 30)
	f.wantCalls(t)
}

func TestTheEnforcerReturnsAReadItCouldNotMake(t *testing.T) {
	atPolicyDefaults(t)
	down := errors.New("the database is down")
	capped := map[string]interface{}{LimitMaxMembers: 2, LimitMaxProjects: 2, LimitHostedRunnerMinutesMonth: 100}

	// The workspace itself.
	f := &enforcerReads{getErr: down}
	e := f.enforcer()
	for name, err := range map[string]error{
		"CheckFlag":         e.CheckFlag("w", LimitTeams),
		"CheckSeats":        e.CheckSeats("w", 1),
		"CheckProjectCount": e.CheckProjectCount("w"),
	} {
		if !errors.Is(err, down) || errors.Is(err, ErrLimitReached) {
			t.Errorf("%s = %v, want the read's error and no refusal", name, err)
		}
	}
	if n, err := e.LeaseMinutesAllowed("w", 30); n != 0 || !errors.Is(err, down) {
		t.Errorf("LeaseMinutesAllowed = %d, %v", n, err)
	}
	if org, err := e.Org("w"); org != nil || !errors.Is(err, down) {
		t.Errorf("Org = %v, %v", org, err)
	}

	// Each count.
	f = &enforcerReads{orgs: map[string]*Org{"w": sharedOrg("w", capped)}, membersErr: down}
	if err := f.enforcer().CheckSeats("w", 1); !errors.Is(err, down) {
		t.Errorf("CheckSeats with members unread = %v", err)
	}
	f = &enforcerReads{orgs: map[string]*Org{"w": sharedOrg("w", capped)}, pendingErr: down}
	if n, err := f.enforcer().CountSeats("w"); n != 0 || !errors.Is(err, down) {
		t.Errorf("CountSeats with invitations unread = %d, %v", n, err)
	}
	f = &enforcerReads{orgs: map[string]*Org{"w": sharedOrg("w", capped)}, projectErr: down}
	if err := f.enforcer().CheckProjectCount("w"); !errors.Is(err, down) {
		t.Errorf("CheckProjectCount with projects unread = %v", err)
	}
	f = &enforcerReads{forUserErr: down}
	if err := f.enforcer().CheckSharedWorkspaceCount("u"); !errors.Is(err, down) {
		t.Errorf("CheckSharedWorkspaceCount with workspaces unread = %v", err)
	}
	f = &enforcerReads{orgs: map[string]*Org{"w": sharedOrg("w", capped)}, minutesErr: down}
	if n, err := f.enforcer().LeaseMinutesAllowed("w", 30); n != 0 || !errors.Is(err, down) {
		t.Errorf("LeaseMinutesAllowed with minutes unread = %d, %v", n, err)
	}

	// A workspace over both ceilings whose counts cannot be read is over
	// nothing, and both reads' errors come back.
	membersDown, projectsDown := errors.New("members down"), errors.New("projects down")
	f = &enforcerReads{membersErr: membersDown, projectErr: projectsDown}
	over, err := f.enforcer().OverPlan(sharedOrg("w", capped))
	if over != nil || !errors.Is(err, membersDown) || !errors.Is(err, projectsDown) {
		t.Errorf("OverPlan with its counts unread = %v, %v", over, err)
	}
	f.wantCalls(t, "ListMembers w", "Projects w")
}

func TestTheSeatsAreMembersPlusPendingInvitations(t *testing.T) {
	atPolicyDefaults(t)
	f := &enforcerReads{
		orgs:    map[string]*Org{"w": sharedOrg("w", map[string]interface{}{LimitMaxMembers: 3})},
		members: map[string]int{"w": 1},
		pending: map[string]int{"w": 1},
	}
	e := f.enforcer()
	if n, err := e.CountSeats("w"); n != 2 || err != nil {
		t.Fatalf("CountSeats = %d, %v, want 2", n, err)
	}
	f.wantCalls(t, "ListMembers w", "PendingInvitations w")

	// With no invitations port, members alone.
	alone := *e
	alone.PendingInvitations = nil
	if n, err := alone.CountSeats("w"); n != 1 || err != nil {
		t.Fatalf("CountSeats with no invitations = %d, %v, want 1", n, err)
	}
	f.calls = nil

	// Room for one more, then none.
	if err := e.CheckSeats("w", 1); err != nil {
		t.Fatalf("the third seat of three was refused: %v", err)
	}
	f.wantCalls(t, "Get w", "ListMembers w", "PendingInvitations w")
	f.pending["w"] = 2
	limitErr := asLimitError(t, e.CheckSeats("w", 1))
	if limitErr.Key != LimitMaxMembers || limitErr.Used != 3 || limitErr.Allowed != 3 ||
		limitErr.Detail != "including invitations not yet accepted" {
		t.Errorf("the seat refusal: %+v", limitErr)
	}
	f.calls = nil

	// A personal workspace is the domain's to refuse, and nothing is
	// counted for it; nor for a workspace with no ceiling.
	f.orgs["p"] = &Org{ID: "p", OrgType: TypePersonal, BilledPlan: PlanSingle}
	f.orgs["open"] = sharedOrg("open", nil)
	f.members["p"], f.members["open"] = 5, 500
	if err := e.CheckSeats("p", 1); err != nil {
		t.Errorf("a personal workspace's seat check: %v", err)
	}
	if err := e.CheckSeats("open", 1); err != nil {
		t.Errorf("an uncapped workspace's seat check: %v", err)
	}
	f.wantCalls(t, "Get p", "Get open")
}

func TestTheProjectCountRefusesAtTheCeiling(t *testing.T) {
	atPolicyDefaults(t)
	f := &enforcerReads{
		orgs:     map[string]*Org{"w": sharedOrg("w", map[string]interface{}{LimitMaxProjects: 2}), "open": sharedOrg("open", nil)},
		projects: map[string]int{"w": 1, "open": 9000},
	}
	e := f.enforcer()
	if err := e.CheckProjectCount("w"); err != nil {
		t.Fatalf("the second project of two was refused: %v", err)
	}
	f.wantCalls(t, "Get w", "Projects w")
	f.projects["w"] = 2
	limitErr := asLimitError(t, e.CheckProjectCount("w"))
	if limitErr.Key != LimitMaxProjects || limitErr.Used != 2 || limitErr.Allowed != 2 || limitErr.Detail != "" {
		t.Errorf("the project refusal: %+v", limitErr)
	}
	f.calls = nil

	if err := e.CheckProjectCount("open"); err != nil {
		t.Errorf("an uncapped workspace's project check: %v", err)
	}
	f.wantCalls(t, "Get open")

	noProjects := *e
	noProjects.Projects = nil
	if err := noProjects.CheckProjectCount("w"); err != nil {
		t.Errorf("a project check with no project port: %v", err)
	}
}

func TestSharedWorkspacesCountCreationsAgainstThePersonalPlan(t *testing.T) {
	atPolicyDefaults(t)
	me, them := "u1", "u2"
	personal := &Org{ID: "p", OrgType: TypePersonal, CreatedBy: &me, BilledPlan: PlanSingle,
		Limits: map[string]interface{}{LimitMaxSharedWorkspaces: 2}}
	mine := &Org{ID: "a", OrgType: TypeCompany, CreatedBy: &me, BilledPlan: PlanSingle}
	theirs := &Org{ID: "b", OrgType: TypeCompany, CreatedBy: &them, BilledPlan: PlanBusiness}
	f := &enforcerReads{forUser: []*Org{personal, mine, theirs}}
	e := f.enforcer()

	if err := e.CheckSharedWorkspaceCount(me); err != nil {
		t.Fatalf("a second creation of two was refused: %v", err)
	}
	f.wantCalls(t, "ListForUser u1")
	personal.Limits[LimitMaxSharedWorkspaces] = 1
	limitErr := asLimitError(t, e.CheckSharedWorkspaceCount(me))
	if limitErr.Key != LimitMaxSharedWorkspaces || limitErr.Used != 1 || limitErr.Allowed != 1 {
		t.Errorf("the shared-workspace refusal: %+v", limitErr)
	}

	// The panel's reading is the same count.
	if n, err := e.Used(LimitMaxSharedWorkspaces, mine); n != 1 || err != nil {
		t.Errorf("Used(max_shared_workspaces) = %d, %v, want 1", n, err)
	}

	// No personal workspace, no limit.
	f.forUser = []*Org{mine, theirs}
	if err := e.CheckSharedWorkspaceCount(me); err != nil {
		t.Errorf("no personal workspace: %v", err)
	}
}

func TestOverPlanCountsOnlyWhereACeilingApplies(t *testing.T) {
	atPolicyDefaults(t)
	f := &enforcerReads{
		members:  map[string]int{"w": 3},
		pending:  map[string]int{"w": 0},
		projects: map[string]int{"w": 2},
	}
	e := f.enforcer()

	w := sharedOrg("w", map[string]interface{}{LimitMaxMembers: 2, LimitMaxProjects: 2})
	if over, err := e.OverPlan(w); !reflect.DeepEqual(over, []string{LimitMaxMembers}) || err != nil {
		t.Errorf("OverPlan = %v, %v, want max_members alone", over, err)
	}
	f.wantCalls(t, "ListMembers w", "PendingInvitations w", "Projects w")

	f.projects["w"] = 3
	if over, _ := e.OverPlan(w); !reflect.DeepEqual(over, []string{LimitMaxMembers, LimitMaxProjects}) {
		t.Errorf("OverPlan = %v, want both", over)
	}
	f.calls = nil

	// No ceiling, no counting; a personal workspace is never over.
	if over, err := e.OverPlan(sharedOrg("w", nil)); over != nil || err != nil {
		t.Errorf("an uncapped workspace is over %v, %v", over, err)
	}
	personal := &Org{ID: "w", OrgType: TypePersonal, BilledPlan: PlanSingle}
	if over, err := e.OverPlan(personal); over != nil || err != nil {
		t.Errorf("a personal workspace is over %v, %v", over, err)
	}
	f.wantCalls(t)
}

func TestALeaseIsFittedUnderTheMonth(t *testing.T) {
	atPolicyDefaults(t)
	f := &enforcerReads{
		orgs:    map[string]*Org{"w": sharedOrg("w", map[string]interface{}{LimitHostedRunnerMinutesMonth: 100}), "open": sharedOrg("open", nil)},
		minutes: 90,
	}
	e := f.enforcer()
	before := monthStart(time.Now())
	if n, err := e.LeaseMinutesAllowed("w", 30); n != 10 || err != nil {
		t.Fatalf("a lease with 10 minutes left = %d, %v", n, err)
	}
	if after := monthStart(time.Now()); !f.since.Equal(before) && !f.since.Equal(after) || f.since.Location() != time.UTC ||
		before.Day() != 1 || before.Hour() != 0 || before.Minute() != 0 || before.Nanosecond() != 0 {
		t.Errorf("minutes counted since %v, want the start of this month in UTC", f.since)
	}
	if n, err := e.LeaseMinutesAllowed("w", 5); n != 5 || err != nil {
		t.Errorf("a lease that fits = %d, %v", n, err)
	}
	f.wantCalls(t, "Get w", "RunnerMinutes w", "Get w", "RunnerMinutes w")

	f.minutes = 100
	n, err := e.LeaseMinutesAllowed("w", 30)
	limitErr := asLimitError(t, err)
	if n != 0 || limitErr.Key != LimitHostedRunnerMinutesMonth || limitErr.Used != 100 || limitErr.Allowed != 100 ||
		!strings.Contains(limitErr.Detail, "Agent Connector") {
		t.Errorf("a lease at the allowance = %d, %+v", n, limitErr)
	}
	f.calls = nil

	if n, err := e.LeaseMinutesAllowed("open", 30); n != 30 || err != nil {
		t.Errorf("an uncapped lease = %d, %v", n, err)
	}
	f.wantCalls(t, "Get open")
}

func TestUsedMeasuresEachCountableLimit(t *testing.T) {
	atPolicyDefaults(t)
	me := "u1"
	w := &Org{ID: "w", OrgType: TypeCompany, CreatedBy: &me, BilledPlan: PlanSingle}
	f := &enforcerReads{
		members:  map[string]int{"w": 2},
		pending:  map[string]int{"w": 1},
		projects: map[string]int{"w": 7},
		forUser:  []*Org{w},
		minutes:  42,
		evidence: 5*1024*1024 + 1,
	}
	e := f.enforcer()
	for key, want := range map[string]int{
		LimitMaxMembers:               3,
		LimitMaxProjects:              7,
		LimitMaxSharedWorkspaces:      1,
		LimitHostedRunnerMinutesMonth: 42,
		LimitEvidenceStorageMB:        5,
	} {
		if n, err := e.Used(key, w); n != want || err != nil {
			t.Errorf("Used(%s) = %d, %v, want %d", key, n, err, want)
		}
	}

	// What cannot be measured here says so.
	none := &LimitEnforcer{Orgs: f}
	for _, key := range []string{LimitMaxProjects, LimitHostedRunnerMinutesMonth, LimitEvidenceStorageMB, LimitMaxUploadMB, LimitTeams} {
		if _, err := none.Used(key, w); !errors.Is(err, ErrNotMeasured) {
			t.Errorf("Used(%s) with no port = %v, want ErrNotMeasured", key, err)
		}
	}
	if _, err := e.Used(LimitMaxSharedWorkspaces, sharedOrg("x", nil)); !errors.Is(err, ErrNotMeasured) {
		t.Errorf("Used(max_shared_workspaces) of a workspace nobody created = %v", err)
	}
}

func TestTheFlagCheckReadsTheWorkspacesPlan(t *testing.T) {
	atPolicyDefaults(t)
	f := &enforcerReads{orgs: map[string]*Org{
		"off": sharedOrg("off", map[string]interface{}{LimitTeams: false}),
		"on":  sharedOrg("on", map[string]interface{}{LimitTeams: true}),
	}}
	e := f.enforcer()
	limitErr := asLimitError(t, e.CheckFlag("off", LimitTeams))
	if !limitErr.Flag || limitErr.Key != LimitTeams {
		t.Errorf("the flag refusal: %+v", limitErr)
	}
	if err := e.CheckFlag("on", LimitTeams); err != nil {
		t.Errorf("an included flag was refused: %v", err)
	}
	if err := e.CheckFlag("nobody", LimitTeams); err != nil {
		t.Errorf("a workspace no row has was refused: %v", err)
	}
	f.wantCalls(t, "Get off", "Get on", "Get nobody")
}
