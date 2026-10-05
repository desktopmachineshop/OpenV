package api

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
)

// A limit check must never be the reason a legitimate action fails: a
// workspace or a count that cannot be read refuses nothing, and the panel
// leaves out a reading it could not make. Only the billing path's seat
// count passes the failed read on. These are the reads each check makes,
// failing one at a time, on a workspace whose own limits cap every count at
// one and leave nothing included, so that any reading that got through
// would refuse.

var errLimitReadDown = errors.New("the limit read is down")

// limitReadOrgs answers the workspace reads, each failing when its error is
// set.
type limitReadOrgs struct {
	orgs.Service
	org, personal                *orgs.Org
	getErr, membersErr, usersErr error
}

func (f *limitReadOrgs) Get(id string) (*orgs.Org, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.org, nil
}

func (f *limitReadOrgs) ListMembers(orgID string) ([]*orgs.Member, error) {
	if f.membersErr != nil {
		return nil, f.membersErr
	}
	return []*orgs.Member{{OrgID: orgID, UserID: "u1"}, {OrgID: orgID, UserID: "u2"}}, nil
}

func (f *limitReadOrgs) ListForUser(userID string) ([]*orgs.Org, error) {
	if f.usersErr != nil {
		return nil, f.usersErr
	}
	return []*orgs.Org{f.personal, f.org}, nil
}

type limitReadInvitations struct {
	invitations.Service
	err error
}

func (f *limitReadInvitations) ListPending(orgID string) ([]*invitations.Invitation, error) {
	return nil, f.err
}

type limitReadProjects struct {
	projects.Service
	err error
}

func (f *limitReadProjects) ListProjectsByOrg(orgID string) ([]*projects.Project, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []*projects.Project{{ID: "p1", OrgID: orgID}, {ID: "p2", OrgID: orgID}}, nil
}

type limitReadMinutes struct {
	runnersessions.Service
	err error
}

func (f *limitReadMinutes) MinutesUsed(orgID string, since time.Time) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return 50, nil
}

type limitReadEvidence struct {
	evidence.Service
	err error
}

func (f *limitReadEvidence) StorageUsedByOrg(orgID string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return 50 * 1024 * 1024, nil
}

// limitReadHandler is a handler over w, a shared workspace that its own
// limits cap at one of everything with nothing included, created by u1,
// whose personal workspace allows one shared workspace. set, if any, makes
// some of the reads fail.
func limitReadHandler(t *testing.T, set func(o *limitReadOrgs, i *limitReadInvitations, p *limitReadProjects, m *limitReadMinutes, e *limitReadEvidence)) *Handler {
	me := "u1"
	o := &limitReadOrgs{org: &orgs.Org{ID: "w", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle, CreatedBy: &me,
		Limits: map[string]interface{}{
			orgs.LimitMaxMembers: 1, orgs.LimitMaxProjects: 1, orgs.LimitMaxSharedWorkspaces: 1,
			orgs.LimitHostedRunnerMinutesMonth: 1, orgs.LimitEvidenceStorageMB: 1,
			orgs.LimitTeams: false, orgs.LimitHostedAutomation: false, orgs.LimitWorkspaceBudget: false,
		}}, personal: &orgs.Org{ID: "p", OrgType: orgs.TypePersonal, BilledPlan: orgs.PlanSingle, CreatedBy: &me,
		Limits: map[string]interface{}{orgs.LimitMaxSharedWorkspaces: 1}}}
	i, p, m, e := &limitReadInvitations{}, &limitReadProjects{}, &limitReadMinutes{}, &limitReadEvidence{}
	if set != nil {
		set(o, i, p, m, e)
	}
	return newTestHandler(t, func(h *Handler) {
		h.OrgService, h.InvitationService, h.ProjectService = o, i, p
		h.RunnerSessionService, h.EvidenceService = m, e
	})
}

func TestALimitThatCannotBeReadRefusesNothing(t *testing.T) {
	// Every reading succeeds: everything is refused, which is what makes
	// the rows below mean something.
	h := limitReadHandler(t, nil)
	if h.checkFlag("w", orgs.LimitTeams) == nil || h.checkOrgSeats("w", 1) == nil || h.checkProjectCount("w") == nil {
		t.Fatal("a workspace capped at one of everything was not refused")
	}
	if n, err := h.leaseMinutesAllowed("w", 30); n != 0 || err == nil {
		t.Fatalf("a lease past the allowance = %d, %v", n, err)
	}
	if over := h.overPlan(h.orgForLimits("w")); !reflect.DeepEqual(over, []string{orgs.LimitMaxMembers, orgs.LimitMaxProjects}) {
		t.Fatalf("over plan: %v", over)
	}

	// The workspace cannot be read.
	h = limitReadHandler(t, func(o *limitReadOrgs, _ *limitReadInvitations, _ *limitReadProjects, _ *limitReadMinutes, _ *limitReadEvidence) {
		o.getErr = errLimitReadDown
	})
	for name, err := range map[string]error{
		"checkFlag":         h.checkFlag("w", orgs.LimitTeams),
		"checkOrgSeats":     h.checkOrgSeats("w", 1),
		"checkProjectCount": h.checkProjectCount("w"),
	} {
		if err != nil {
			t.Errorf("%s refused on an unread workspace: %v", name, err)
		}
	}
	if n, err := h.leaseMinutesAllowed("w", 30); n != 30 || err != nil {
		t.Errorf("a lease on an unread workspace = %d, %v, want the whole lease", n, err)
	}
	if org := h.orgForLimits("w"); org != nil {
		t.Errorf("orgForLimits on an unread workspace = %+v", org)
	}

	// Its members, or its invitations, cannot be read.
	for name, set := range map[string]func(o *limitReadOrgs, i *limitReadInvitations){
		"members":     func(o *limitReadOrgs, _ *limitReadInvitations) { o.membersErr = errLimitReadDown },
		"invitations": func(_ *limitReadOrgs, i *limitReadInvitations) { i.err = errLimitReadDown },
	} {
		h = limitReadHandler(t, func(o *limitReadOrgs, i *limitReadInvitations, _ *limitReadProjects, _ *limitReadMinutes, _ *limitReadEvidence) {
			set(o, i)
		})
		if err := h.checkOrgSeats("w", 1); err != nil {
			t.Errorf("%s unread: the seat check refused: %v", name, err)
		}
		if over := h.overPlan(h.orgForLimits("w")); !reflect.DeepEqual(over, []string{orgs.LimitMaxProjects}) {
			t.Errorf("%s unread: over plan %v, want max_projects alone", name, over)
		}
		if n, err := h.countOrgSeats("w"); n != 0 || !errors.Is(err, errLimitReadDown) {
			t.Errorf("%s unread: the billing seat count = %d, %v, want the read's error", name, n, err)
		}
		if _, ok := h.countFor(orgs.LimitMaxMembers, h.orgForLimits("w")); ok {
			t.Errorf("%s unread: the panel reported a seat count", name)
		}
	}

	// Its projects cannot be read.
	h = limitReadHandler(t, func(_ *limitReadOrgs, _ *limitReadInvitations, p *limitReadProjects, _ *limitReadMinutes, _ *limitReadEvidence) {
		p.err = errLimitReadDown
	})
	if err := h.checkProjectCount("w"); err != nil {
		t.Errorf("projects unread: the project check refused: %v", err)
	}
	if over := h.overPlan(h.orgForLimits("w")); !reflect.DeepEqual(over, []string{orgs.LimitMaxMembers}) {
		t.Errorf("projects unread: over plan %v, want max_members alone", over)
	}
	if _, ok := h.countFor(orgs.LimitMaxProjects, h.orgForLimits("w")); ok {
		t.Error("projects unread: the panel reported a project count")
	}

	// The person's workspaces cannot be read. (Read, u1 has created w, the
	// one shared workspace their personal one allows.)
	h = limitReadHandler(t, nil)
	if h.checkSharedWorkspaceCount("u1") == nil {
		t.Fatal("a second shared workspace past a ceiling of one was not refused")
	}
	if n, ok := h.countFor(orgs.LimitMaxSharedWorkspaces, h.OrgService.(*limitReadOrgs).org); n != 1 || !ok {
		t.Fatalf("the panel's shared-workspace count = %d, %v, want 1", n, ok)
	}
	h.OrgService.(*limitReadOrgs).usersErr = errLimitReadDown
	if err := h.checkSharedWorkspaceCount("u1"); err != nil {
		t.Errorf("workspaces unread: the shared-workspace check refused: %v", err)
	}
	if _, ok := h.countFor(orgs.LimitMaxSharedWorkspaces, h.OrgService.(*limitReadOrgs).org); ok {
		t.Error("workspaces unread: the panel reported a shared-workspace count")
	}

	// The month's minutes, or the evidence, cannot be read.
	h = limitReadHandler(t, func(_ *limitReadOrgs, _ *limitReadInvitations, _ *limitReadProjects, m *limitReadMinutes, e *limitReadEvidence) {
		m.err, e.err = errLimitReadDown, errLimitReadDown
	})
	if n, err := h.leaseMinutesAllowed("w", 30); n != 30 || err != nil {
		t.Errorf("minutes unread: a lease = %d, %v, want the whole lease", n, err)
	}
	for _, key := range []string{orgs.LimitHostedRunnerMinutesMonth, orgs.LimitEvidenceStorageMB} {
		if _, ok := h.countFor(key, h.orgForLimits("w")); ok {
			t.Errorf("%s unread: the panel reported a reading", key)
		}
	}
	res, err := h.buildLimitsResponse("w")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range res.Limits {
		if l.Key == orgs.LimitHostedRunnerMinutesMonth || l.Key == orgs.LimitEvidenceStorageMB {
			if l.Used != nil {
				t.Errorf("%s unread: the limits response reports %d used", l.Key, *l.Used)
			}
		}
	}
}
