package orgs

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// The deployment policy: four settings that boot installs, each through a
// package setter, and that the service and the free functions read
// whenever they resolve something.
//
//	SetSelfHosted        cmd/server/wire_config.go
//	SetDefaultPlan       cmd/server/wire_config.go
//	SetDeploymentLimits  cmd/server/wire_config.go
//	SetTiersEnforced     cmd/server/wire_workspace.go, AFTER NewDefaultService
//
// These tests pin what each setting does when it is set after the service
// exists, as boot sets the tiers, so that a change which read a copy taken
// at construction would show. Every reading goes through a seam other
// packages use: the service (Get, CreateOrg, EnsurePersonalOrg,
// GrandfatherBefore), the workspace's EffectiveLimits, and the free
// functions (PlanDefaults, which is where tiered is read, MaxPlanUploadMB,
// ReadOnlyRemedy, a LimitError's message, CheckFlag and the four getters).
// The tests flip package state, so none of them runs in parallel, and each
// puts the package defaults back when it ends.

// atPolicyDefaults checks that the four settings are at the package
// defaults a boot starts from (tiers off, hosted, new workspaces on the
// single plan, no deployment layer) and puts them back there when the test
// ends, so no other test sees what this one set.
func atPolicyDefaults(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetTiersEnforced(false)
		SetSelfHosted(false)
		SetDefaultPlan(PlanSingle)
		SetDeploymentLimits(nil)
	})
	if TiersEnforced() || SelfHosted() || DefaultPlan() != PlanSingle || len(DeploymentLimits()) != 0 {
		t.Fatalf("an earlier test left the deployment policy changed: tiers %v, self-hosted %v, default plan %q, deployment limits %v",
			TiersEnforced(), SelfHosted(), DefaultPlan(), DeploymentLimits())
	}
}

// policyRepoFake is the smallest Repository that lets the service create,
// read and grandfather workspaces: the workspaces by id, and the overrides
// each grandfather step was asked to lay.
type policyRepoFake struct {
	Repository
	orgs        map[string]*Org
	grandfather []map[string]interface{}
}

func newPolicyRepoFake(stored ...*Org) *policyRepoFake {
	f := &policyRepoFake{orgs: map[string]*Org{}}
	for _, o := range stored {
		f.orgs[o.ID] = o
	}
	return f
}

func (f *policyRepoFake) SaveOrg(o *Org) error {
	c := *o
	f.orgs[o.ID] = &c
	return nil
}

func (f *policyRepoFake) UpsertMember(orgID, userID, role string) error { return nil }

func (f *policyRepoFake) FindOrgByID(id string) (*Org, error) {
	o, ok := f.orgs[id]
	if !ok {
		return nil, nil
	}
	c := *o
	return &c, nil
}

func (f *policyRepoFake) FindPersonalOrgForUser(userID string) (*Org, error) {
	for _, o := range f.orgs {
		if o.OrgType == TypePersonal && o.CreatedBy != nil && *o.CreatedBy == userID {
			c := *o
			return &c, nil
		}
	}
	return nil, nil
}

// GrandfatherBefore lays the overrides under the limits of every workspace
// created before cutoff that is not yet grandfathered, keeping a key the
// workspace already sets, as the postgres store's UPDATE does.
func (f *policyRepoFake) GrandfatherBefore(cutoff time.Time, overrides map[string]interface{}) (int64, error) {
	f.grandfather = append(f.grandfather, overrides)
	var n int64
	for _, o := range f.orgs {
		if !o.CreatedAt.Before(cutoff) || o.Billing.Grandfathered {
			continue
		}
		limits := map[string]interface{}{}
		for k, v := range overrides {
			limits[k] = v
		}
		for k, v := range o.Limits {
			limits[k] = v
		}
		o.Limits, o.Billing.Grandfathered = limits, true
		n++
	}
	return n, nil
}

// policyOrg is a stored workspace of the given type on the given plan,
// with no limits of its own, created at the given time.
func policyOrg(id, orgType, plan string, created time.Time) *Org {
	return &Org{ID: id, Name: id, OrgType: orgType, BilledPlan: plan, Limits: map[string]interface{}{}, CreatedAt: created}
}

// policyCreated is when the stored workspaces below were created: after
// the grandfather cutoff the boot-sequence test uses.
var policyCreated = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// policyLimits reads a workspace through the service, as every caller of
// the limits does, and resolves its limits.
func policyLimits(t *testing.T, svc *DefaultService, id string) map[string]interface{} {
	t.Helper()
	org, err := svc.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	return org.EffectiveLimits()
}

// tierReading is the part of a limits map the tiers move: the three
// counts, the hosted minutes (0 is no ceiling) and the three flags.
type tierReading struct {
	members, shared, projects, minutes int
	hosted, teams, budget              bool
}

func readTiers(limits map[string]interface{}) tierReading {
	var r tierReading
	r.members, _ = LimitInt(limits, LimitMaxMembers)
	r.shared, _ = LimitInt(limits, LimitMaxSharedWorkspaces)
	r.projects, _ = LimitInt(limits, LimitMaxProjects)
	r.minutes, _ = LimitInt(limits, LimitHostedRunnerMinutesMonth)
	r.hosted = Allowed(limits, LimitHostedAutomation)
	r.teams = Allowed(limits, LimitTeams)
	r.budget = Allowed(limits, LimitWorkspaceBudget)
	return r
}

// alphaSingleLimits is a shared workspace on the single plan with the
// tiers off: the plan's product limits, every count open, every flag on.
func alphaSingleLimits() map[string]interface{} {
	return map[string]interface{}{
		LimitRunnerMemoryMB:           2048,
		LimitRunnerCPUs:               1.0,
		LimitRunnerSessionMinutes:     60,
		LimitRunnerSessionIdleMinutes: 15,
		LimitEvidenceStorageMB:        2048,
		LimitMaxUploadMB:              128,
		LimitMaxMembers:               0,
		LimitMaxSharedWorkspaces:      0,
		LimitMaxProjects:              0,
		LimitHostedRunnerMinutesMonth: 0,
		LimitHostedAutomation:         true,
		LimitTeams:                    true,
		LimitWorkspaceBudget:          true,
	}
}

// tieredSingleLimits is the same workspace with the tiers on: the same
// product limits, the free tier's counts and minutes, every flag off.
func tieredSingleLimits() map[string]interface{} {
	return map[string]interface{}{
		LimitRunnerMemoryMB:           2048,
		LimitRunnerCPUs:               1.0,
		LimitRunnerSessionMinutes:     60,
		LimitRunnerSessionIdleMinutes: 15,
		LimitEvidenceStorageMB:        2048,
		LimitMaxUploadMB:              128,
		LimitMaxMembers:               2,
		LimitMaxSharedWorkspaces:      1,
		LimitMaxProjects:              200,
		LimitHostedRunnerMinutesMonth: 300,
		LimitHostedAutomation:         false,
		LimitTeams:                    false,
		LimitWorkspaceBudget:          false,
	}
}

// selfHostLimits is any shared workspace on a self-hosted deployment with
// no deployment layer: nothing capped, every flag on.
func selfHostLimits() map[string]interface{} {
	return map[string]interface{}{
		LimitRunnerMemoryMB:           0,
		LimitRunnerCPUs:               0,
		LimitRunnerSessionMinutes:     0,
		LimitRunnerSessionIdleMinutes: 0,
		LimitEvidenceStorageMB:        0,
		LimitMaxUploadMB:              0,
		LimitMaxMembers:               0,
		LimitMaxSharedWorkspaces:      0,
		LimitMaxProjects:              0,
		LimitHostedRunnerMinutesMonth: 0,
		LimitHostedAutomation:         true,
		LimitTeams:                    true,
		LimitWorkspaceBudget:          true,
	}
}

// The remedies a refusal offers, hosted and self-hosted.
const (
	hostedCountRemedy = "A workspace admin can raise this limit from the Billing tab in workspace settings, " +
		"by moving the workspace to a plan that allows more."
	selfHostedProjectsRemedy = "This deployment sets its own limits: raise max_projects in OPENV_LIMITS to change it everywhere, " +
		"or set it on this workspace alone to change it here."
)

// What a hosted boot starts from: the tiers off, hosted, new workspaces on
// the single plan, no deployment layer; a shared single-plan workspace on
// the alpha terms, and a 1024 MB bound on an upload request.
func TestTheDeploymentPolicyStartsAtThePackageDefaults(t *testing.T) {
	atPolicyDefaults(t)

	if TiersEnforced() {
		t.Error("TiersEnforced() = true, want false")
	}
	if SelfHosted() {
		t.Error("SelfHosted() = true, want false")
	}
	if got := DefaultPlan(); got != "single" {
		t.Errorf("DefaultPlan() = %q, want \"single\"", got)
	}
	if got := DeploymentLimits(); got == nil || len(got) != 0 {
		t.Errorf("DeploymentLimits() = %#v, want an empty, non-nil map", got)
	}
	if got := MaxPlanUploadMB(); got != 1024 {
		t.Errorf("MaxPlanUploadMB() = %d, want 1024", got)
	}

	svc := NewDefaultService(newPolicyRepoFake())
	org, err := svc.CreateOrg("Acme", TypeCompany, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if org.BilledPlan != "single" {
		t.Errorf("a new workspace is on %q, want \"single\"", org.BilledPlan)
	}
	if got := policyLimits(t, svc, org.ID); !reflect.DeepEqual(got, alphaSingleLimits()) {
		t.Errorf("a new workspace resolves\n%v\nwant\n%v", got, alphaSingleLimits())
	}
}

// X8a's row: boot builds the service, then turns the tiers on
// (cmd/server/wire_workspace.go). The service already built, and a
// workspace read before the switch, both resolve the tier values from the
// next read on, for every plan; turning the switch off again goes back.
func TestTiersEnforcedAfterTheServiceIsBuilt(t *testing.T) {
	atPolicyDefaults(t)

	rows := []struct {
		id, orgType, plan string
		off, on           tierReading
	}{
		{"w-single", TypeCompany, PlanSingle,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{2, 1, 200, 300, false, false, false}},
		{"w-free", TypeCompany, PlanFree,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{2, 1, 200, 300, false, false, false}},
		{"w-lite", TypeCompany, PlanBusinessLite,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{2, 1, 500, 0, true, false, false}},
		{"w-business", TypeCompany, PlanBusiness,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{0, 0, 1000, 0, true, true, true}},
		{"w-team", TypeCompany, PlanTeam,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{0, 0, 1000, 0, true, true, true}},
		{"w-open", TypeCompany, PlanOpenSource,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{0, 0, 1000, 0, true, true, true}},
		{"w-enterprise", TypeCompany, PlanEnterprise,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{0, 0, 0, 0, true, true, true}},
		{"w-self", TypeCompany, PlanSelfHost,
			tierReading{0, 0, 0, 0, true, true, true}, tierReading{0, 0, 0, 0, true, true, true}},
		{"p-single", TypePersonal, PlanSingle,
			tierReading{1, 0, 0, 0, true, true, true}, tierReading{1, 1, 200, 300, false, false, false}},
	}
	repo := newPolicyRepoFake()
	for _, r := range rows {
		repo.orgs[r.id] = policyOrg(r.id, r.orgType, r.plan, policyCreated)
	}
	svc := NewDefaultService(repo)
	before, err := svc.Get("w-single")
	if err != nil {
		t.Fatal(err)
	}

	check := func(phase string, on bool) {
		t.Helper()
		for _, r := range rows {
			want := r.off
			if on {
				want = r.on
			}
			if got := readTiers(policyLimits(t, svc, r.id)); got != want {
				t.Errorf("%s: %s (%s) resolves %+v, want %+v", phase, r.id, r.plan, got, want)
			}
			if r.orgType != TypeCompany {
				continue
			}
			if got := readTiers(PlanDefaults(r.plan)); got != want {
				t.Errorf("%s: PlanDefaults(%q) reads %+v, want %+v", phase, r.plan, got, want)
			}
		}
	}

	check("tiers off", false)
	if got := policyLimits(t, svc, "w-single"); !reflect.DeepEqual(got, alphaSingleLimits()) {
		t.Errorf("tiers off: w-single resolves\n%v\nwant\n%v", got, alphaSingleLimits())
	}

	SetTiersEnforced(true)
	if !TiersEnforced() {
		t.Fatal("TiersEnforced() = false after SetTiersEnforced(true)")
	}
	check("tiers on after the service was built", true)
	if got := policyLimits(t, svc, "w-single"); !reflect.DeepEqual(got, tieredSingleLimits()) {
		t.Errorf("tiers on: w-single resolves\n%v\nwant\n%v", got, tieredSingleLimits())
	}
	if got := before.EffectiveLimits(); !reflect.DeepEqual(got, tieredSingleLimits()) {
		t.Errorf("tiers on: w-single, read before the switch, resolves\n%v\nwant\n%v", got, tieredSingleLimits())
	}

	SetTiersEnforced(false)
	if TiersEnforced() {
		t.Fatal("TiersEnforced() = true after SetTiersEnforced(false)")
	}
	check("tiers off again", false)
	if got := before.EffectiveLimits(); !reflect.DeepEqual(got, alphaSingleLimits()) {
		t.Errorf("tiers off again: w-single, read before, resolves\n%v\nwant\n%v", got, alphaSingleLimits())
	}
}

// Boot builds the service and then turns the tiers on; the reverse order
// resolves the same limits, so neither the service nor a workspace holds
// the switch from the moment it was built.
func TestTiersEnforcedReadsTheSameInEitherOrder(t *testing.T) {
	atPolicyDefaults(t)

	repo := func() *policyRepoFake {
		return newPolicyRepoFake(
			policyOrg("w-single", TypeCompany, PlanSingle, policyCreated),
			policyOrg("w-lite", TypeCompany, PlanBusinessLite, policyCreated),
		)
	}
	for _, tc := range []struct {
		name  string
		build func() *DefaultService
	}{
		{"service built, then the tiers on, as boot does", func() *DefaultService {
			svc := NewDefaultService(repo())
			SetTiersEnforced(true)
			return svc
		}},
		{"the tiers on, then the service built", func() *DefaultService {
			SetTiersEnforced(true)
			return NewDefaultService(repo())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SetTiersEnforced(false)
			svc := tc.build()
			if got := policyLimits(t, svc, "w-single"); !reflect.DeepEqual(got, tieredSingleLimits()) {
				t.Errorf("w-single resolves\n%v\nwant\n%v", got, tieredSingleLimits())
			}
			if got, want := readTiers(policyLimits(t, svc, "w-lite")), (tierReading{2, 1, 500, 0, true, false, false}); got != want {
				t.Errorf("w-lite resolves %+v, want %+v", got, want)
			}
		})
	}
}

// The whole boot sequence of wire_workspace.go: the service is built, the
// workspaces created before the cutoff are grandfathered onto the alpha
// terms, and only then do the tiers go on. The terms laid are the same
// whichever way the switch stands, and the grandfathered workspace stays
// open while a later one on the same plan gets the tier values.
func TestTheBootSequenceGrandfathersThenTurnsTheTiersOn(t *testing.T) {
	atPolicyDefaults(t)

	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := newPolicyRepoFake(
		policyOrg("w-old", TypeCompany, PlanSingle, time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
		policyOrg("w-new", TypeCompany, PlanSingle, policyCreated),
	)
	svc := NewDefaultService(repo)

	n, err := svc.GrandfatherBefore(cutoff)
	if err != nil || n != 1 {
		t.Fatalf("GrandfatherBefore = (%d, %v), want (1, nil)", n, err)
	}
	SetTiersEnforced(true)

	alpha := map[string]interface{}{
		LimitMaxMembers:               0,
		LimitMaxSharedWorkspaces:      0,
		LimitMaxProjects:              0,
		LimitHostedRunnerMinutesMonth: 0,
		LimitHostedAutomation:         true,
		LimitTeams:                    true,
		LimitWorkspaceBudget:          true,
	}
	if got := repo.grandfather[0]; !reflect.DeepEqual(got, alpha) {
		t.Errorf("the grandfather step laid\n%v\nwant\n%v", got, alpha)
	}
	if got, want := readTiers(policyLimits(t, svc, "w-old")), (tierReading{0, 0, 0, 0, true, true, true}); got != want {
		t.Errorf("the grandfathered workspace resolves %+v, want %+v", got, want)
	}
	if got := policyLimits(t, svc, "w-new"); !reflect.DeepEqual(got, tieredSingleLimits()) {
		t.Errorf("the later workspace resolves\n%v\nwant\n%v", got, tieredSingleLimits())
	}

	// A second boot, with the tiers already on, lays the same terms and
	// finds nothing left to grandfather.
	n, err = svc.GrandfatherBefore(cutoff)
	if err != nil || n != 0 {
		t.Fatalf("a second GrandfatherBefore = (%d, %v), want (0, nil)", n, err)
	}
	if got := repo.grandfather[1]; !reflect.DeepEqual(got, alpha) {
		t.Errorf("with the tiers on, the grandfather step laid\n%v\nwant\n%v", got, alpha)
	}
}

// SetSelfHosted after the service is built: every workspace, whatever its
// plan or billing status, resolves the self-host plan from the next read;
// every remedy names OPENV_LIMITS rather than the Billing tab, including a
// refusal built before the switch; the upload request bound goes; and the
// plan new workspaces are created on does not move, since boot installs
// that separately (SetDefaultPlan). PlanDefaults reads the plan it is
// given, self-hosted or not.
func TestSelfHostedAfterTheServiceIsBuilt(t *testing.T) {
	atPolicyDefaults(t)

	canceled := policyOrg("w-canceled", TypeCompany, PlanBusiness, policyCreated)
	canceled.Billing.Status = PlanStatusCanceled
	repo := newPolicyRepoFake(
		policyOrg("w-single", TypeCompany, PlanSingle, policyCreated),
		policyOrg("p-single", TypePersonal, PlanSingle, policyCreated),
		canceled,
	)
	svc := NewDefaultService(repo)
	before, err := svc.Get("w-single")
	if err != nil {
		t.Fatal(err)
	}
	refusal := NewLimitError(LimitMaxProjects, 200, 200)
	flagLimits := map[string]interface{}{LimitTeams: false}
	over := []string{LimitMaxMembers, LimitMaxProjects}

	// Hosted.
	if got := policyLimits(t, svc, "w-single"); !reflect.DeepEqual(got, alphaSingleLimits()) {
		t.Errorf("hosted: w-single resolves\n%v\nwant\n%v", got, alphaSingleLimits())
	}
	if got, _ := LimitInt(policyLimits(t, svc, "w-canceled"), LimitEvidenceStorageMB); got != 2048 {
		t.Errorf("hosted: a canceled business workspace has %d MB of evidence storage, want the single plan's 2048", got)
	}
	if got, want := refusal.Error(), "Projects: this workspace allows 200 and already has 200. "+hostedCountRemedy; got != want {
		t.Errorf("hosted: refusal = %q, want %q", got, want)
	}
	if got, want := CheckFlag(flagLimits, LimitTeams).Error(),
		"Teams and per-project access: not included in this workspace's plan. "+
			"A workspace admin can add it from the Billing tab in workspace settings; the pricing page says which plan includes it."; got != want {
		t.Errorf("hosted: flag refusal = %q, want %q", got, want)
	}
	if got, want := ReadOnlyRemedy(over),
		"This workspace has more than its plan allows, so it is read-only until it is brought under the plan's limits or moved to a plan that fits. "+
			"Everything in it stays readable and exportable. "+
			"A workspace admin can subscribe from the Billing tab in workspace settings, remove members or delete projects."; got != want {
		t.Errorf("hosted: read-only remedy = %q, want %q", got, want)
	}
	if got := MaxPlanUploadMB(); got != 1024 {
		t.Errorf("hosted: MaxPlanUploadMB() = %d, want 1024", got)
	}

	SetSelfHosted(true)
	if !SelfHosted() {
		t.Fatal("SelfHosted() = false after SetSelfHosted(true)")
	}
	for _, id := range []string{"w-single", "w-canceled"} {
		if got := policyLimits(t, svc, id); !reflect.DeepEqual(got, selfHostLimits()) {
			t.Errorf("self-hosted: %s resolves\n%v\nwant\n%v", id, got, selfHostLimits())
		}
	}
	if got := before.EffectiveLimits(); !reflect.DeepEqual(got, selfHostLimits()) {
		t.Errorf("self-hosted: w-single, read before the switch, resolves\n%v\nwant\n%v", got, selfHostLimits())
	}
	personal := selfHostLimits()
	personal[LimitMaxMembers] = 1
	if got := policyLimits(t, svc, "p-single"); !reflect.DeepEqual(got, personal) {
		t.Errorf("self-hosted: the personal workspace resolves\n%v\nwant\n%v", got, personal)
	}
	if got, want := refusal.Error(), "Projects: this workspace allows 200 and already has 200. "+selfHostedProjectsRemedy; got != want {
		t.Errorf("self-hosted: a refusal built before the switch = %q, want %q", got, want)
	}
	if got, want := CheckFlag(flagLimits, LimitTeams).Error(),
		"Teams and per-project access: not included in this workspace's plan. "+
			"This deployment sets its own limits: raise teams in OPENV_LIMITS to change it everywhere, "+
			"or set it on this workspace alone to change it here."; got != want {
		t.Errorf("self-hosted: flag refusal = %q, want %q", got, want)
	}
	if got, want := ReadOnlyRemedy(over),
		"This workspace has more than this deployment's limits allow, so it is read-only until it is brought under them or they are raised. "+
			"Everything in it stays readable and exportable. "+
			"This deployment sets its own limits: raise max_members and max_projects in OPENV_LIMITS to change them everywhere, "+
			"or set them on this workspace alone to change them here. "+
			"A workspace admin can also remove members or delete projects."; got != want {
		t.Errorf("self-hosted: read-only remedy = %q, want %q", got, want)
	}
	if got := MaxPlanUploadMB(); got != 0 {
		t.Errorf("self-hosted: MaxPlanUploadMB() = %d, want 0", got)
	}
	if got, _ := LimitInt(PlanDefaults(PlanSingle), LimitEvidenceStorageMB); got != 2048 {
		t.Errorf("self-hosted: PlanDefaults(single) has %d MB of evidence storage, want 2048", got)
	}
	if got := DefaultPlan(); got != "single" {
		t.Errorf("self-hosted: DefaultPlan() = %q, want \"single\"", got)
	}
	created, err := svc.CreateOrg("Acme", TypeCompany, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if created.BilledPlan != "single" {
		t.Errorf("self-hosted: a new workspace is on %q, want \"single\"", created.BilledPlan)
	}
	if got := policyLimits(t, svc, created.ID); !reflect.DeepEqual(got, selfHostLimits()) {
		t.Errorf("self-hosted: a new workspace resolves\n%v\nwant\n%v", got, selfHostLimits())
	}

	SetSelfHosted(false)
	if SelfHosted() {
		t.Fatal("SelfHosted() = true after SetSelfHosted(false)")
	}
	if got := policyLimits(t, svc, "w-single"); !reflect.DeepEqual(got, alphaSingleLimits()) {
		t.Errorf("hosted again: w-single resolves\n%v\nwant\n%v", got, alphaSingleLimits())
	}
	if got, want := refusal.Error(), "Projects: this workspace allows 200 and already has 200. "+hostedCountRemedy; got != want {
		t.Errorf("hosted again: refusal = %q, want %q", got, want)
	}
	if got := MaxPlanUploadMB(); got != 1024 {
		t.Errorf("hosted again: MaxPlanUploadMB() = %d, want 1024", got)
	}
}

// The two switches together, on a shared single-plan workspace, set in
// either order after the service is built: self-hosting wins over the
// tiers for the workspace (its base is the self-host plan, which the tiers
// do not touch), while PlanDefaults(single) still follows the tiers, and
// the remedy and the upload bound follow self-hosting alone.
func TestTiersEnforcedAndSelfHostedTogether(t *testing.T) {
	atPolicyDefaults(t)

	svc := NewDefaultService(newPolicyRepoFake(policyOrg("w-single", TypeCompany, PlanSingle, policyCreated)))
	for _, row := range []struct {
		tiers, selfHosted bool
		projects          int    // the workspace's max_projects; 0 is no ceiling
		teams             bool   // the workspace's teams flag
		planProjects      int    // PlanDefaults(single)'s max_projects
		planTeams         bool   // PlanDefaults(single)'s teams flag
		uploadMB          int    // MaxPlanUploadMB()
		remedy            string // a max_projects refusal's remedy
	}{
		{false, false, 0, true, 0, true, 1024, hostedCountRemedy},
		{true, false, 200, false, 200, false, 1024, hostedCountRemedy},
		{false, true, 0, true, 0, true, 0, selfHostedProjectsRemedy},
		{true, true, 0, true, 200, false, 0, selfHostedProjectsRemedy},
	} {
		for _, tiersFirst := range []bool{true, false} {
			order := "self-hosted set first"
			if tiersFirst {
				order = "tiers set first"
			}
			t.Run(fmt.Sprintf("tiers %v, self-hosted %v, %s", row.tiers, row.selfHosted, order), func(t *testing.T) {
				SetTiersEnforced(false)
				SetSelfHosted(false)
				if tiersFirst {
					SetTiersEnforced(row.tiers)
					SetSelfHosted(row.selfHosted)
				} else {
					SetSelfHosted(row.selfHosted)
					SetTiersEnforced(row.tiers)
				}
				if TiersEnforced() != row.tiers || SelfHosted() != row.selfHosted {
					t.Fatalf("TiersEnforced() = %v, SelfHosted() = %v; want %v, %v", TiersEnforced(), SelfHosted(), row.tiers, row.selfHosted)
				}
				limits := policyLimits(t, svc, "w-single")
				if got, _ := LimitInt(limits, LimitMaxProjects); got != row.projects {
					t.Errorf("w-single max_projects = %d, want %d", got, row.projects)
				}
				if got := Allowed(limits, LimitTeams); got != row.teams {
					t.Errorf("w-single teams = %v, want %v", got, row.teams)
				}
				plan := PlanDefaults(PlanSingle)
				if got, _ := LimitInt(plan, LimitMaxProjects); got != row.planProjects {
					t.Errorf("PlanDefaults(single) max_projects = %d, want %d", got, row.planProjects)
				}
				if got := Allowed(plan, LimitTeams); got != row.planTeams {
					t.Errorf("PlanDefaults(single) teams = %v, want %v", got, row.planTeams)
				}
				if got := MaxPlanUploadMB(); got != row.uploadMB {
					t.Errorf("MaxPlanUploadMB() = %d, want %d", got, row.uploadMB)
				}
				if got := NewLimitError(LimitMaxProjects, 200, 200).Remedy(); got != row.remedy {
					t.Errorf("remedy = %q, want %q", got, row.remedy)
				}
			})
		}
	}
}

// SetDefaultPlan after the service is built: the next workspace the
// service creates, shared or personal, is on the plan installed, for every
// plan name and legacy alias, and resolves that plan's limits. A workspace
// created earlier keeps the plan it was created on. A name that is not a
// plan, the empty one included, is ignored: the previous default stays,
// rather than the package's own.
func TestDefaultPlanAfterTheServiceIsBuilt(t *testing.T) {
	atPolicyDefaults(t)

	svc := NewDefaultService(newPolicyRepoFake())
	first, err := svc.CreateOrg("First", TypeCompany, "u0")
	if err != nil {
		t.Fatal(err)
	}
	if first.BilledPlan != "single" {
		t.Fatalf("before any SetDefaultPlan, a new workspace is on %q, want \"single\"", first.BilledPlan)
	}

	for i, row := range []struct {
		plan      string
		storageMB int // the new workspace's evidence_storage_mb; 0 is no ceiling
	}{
		{"single", 2048},
		{"business_lite", 10240},
		{"business", 20480},
		{"enterprise", 0},
		{"self_host", 0},
		{"open_source", 20480},
		{"free", 2048},
		{"team", 20480},
	} {
		SetDefaultPlan(row.plan)
		if got := DefaultPlan(); got != row.plan {
			t.Errorf("SetDefaultPlan(%q): DefaultPlan() = %q", row.plan, got)
		}
		org, err := svc.CreateOrg("Acme", TypeCompany, "u1")
		if err != nil {
			t.Fatal(err)
		}
		if org.BilledPlan != row.plan {
			t.Errorf("SetDefaultPlan(%q): a new workspace is on %q", row.plan, org.BilledPlan)
		}
		if got, _ := LimitInt(policyLimits(t, svc, org.ID), LimitEvidenceStorageMB); got != row.storageMB {
			t.Errorf("SetDefaultPlan(%q): the new workspace has %d MB of evidence storage, want %d", row.plan, got, row.storageMB)
		}
		personal, created, err := svc.EnsurePersonalOrg(fmt.Sprintf("p%d", i), "Pat")
		if err != nil || !created {
			t.Fatalf("EnsurePersonalOrg = (%v, %v, %v)", personal, created, err)
		}
		if personal.BilledPlan != row.plan {
			t.Errorf("SetDefaultPlan(%q): a new personal workspace is on %q", row.plan, personal.BilledPlan)
		}
	}
	kept, err := svc.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.BilledPlan != "single" {
		t.Errorf("the workspace created first is now on %q, want \"single\"", kept.BilledPlan)
	}

	SetDefaultPlan("business_lite")
	for _, name := range []string{"", "platinum", "Business", "SINGLE", "open-source", "self-host", " single", "single "} {
		SetDefaultPlan(name)
		if got := DefaultPlan(); got != "business_lite" {
			t.Errorf("SetDefaultPlan(%q): DefaultPlan() = %q, want \"business_lite\" kept", name, got)
		}
		org, err := svc.CreateOrg("Acme", TypeCompany, "u1")
		if err != nil {
			t.Fatal(err)
		}
		if org.BilledPlan != "business_lite" {
			t.Errorf("SetDefaultPlan(%q): a new workspace is on %q, want \"business_lite\"", name, org.BilledPlan)
		}
	}
}

// SetDeploymentLimits after the service is built: each call replaces the
// whole middle layer (it does not merge with the previous one), nil and an
// empty map both leave the plan in charge, a partial map moves only the
// keys it names, flags included, and the layer sits above the tiers and
// applies on a self-hosted deployment too. The map a parse of OPENV_LIMITS
// answers is installed as it is, floats and all.
func TestDeploymentLimitsAfterTheServiceIsBuilt(t *testing.T) {
	atPolicyDefaults(t)

	svc := NewDefaultService(newPolicyRepoFake(policyOrg("w-single", TypeCompany, PlanSingle, policyCreated)))
	before, err := svc.Get("w-single")
	if err != nil {
		t.Fatal(err)
	}

	type reading struct {
		storage, session, projects int
		teams                      bool
	}
	read := func(limits map[string]interface{}) reading {
		var r reading
		r.storage, _ = LimitInt(limits, LimitEvidenceStorageMB)
		r.session, _ = LimitInt(limits, LimitRunnerSessionMinutes)
		r.projects, _ = LimitInt(limits, LimitMaxProjects)
		r.teams = Allowed(limits, LimitTeams)
		return r
	}
	for _, row := range []struct {
		name      string
		set       map[string]interface{}
		installed map[string]interface{} // what DeploymentLimits() answers
		want      reading
	}{
		{"nil", nil,
			map[string]interface{}{}, reading{2048, 60, 0, true}},
		{"one key", map[string]interface{}{LimitEvidenceStorageMB: 500},
			map[string]interface{}{LimitEvidenceStorageMB: 500}, reading{500, 60, 0, true}},
		{"another key and a flag, replacing the first", map[string]interface{}{LimitMaxProjects: 5, LimitTeams: false},
			map[string]interface{}{LimitMaxProjects: 5, LimitTeams: false}, reading{2048, 60, 5, false}},
		{"empty", map[string]interface{}{},
			map[string]interface{}{}, reading{2048, 60, 0, true}},
		{"one key again", map[string]interface{}{LimitRunnerSessionMinutes: 30},
			map[string]interface{}{LimitRunnerSessionMinutes: 30}, reading{2048, 30, 0, true}},
		{"nil again", nil,
			map[string]interface{}{}, reading{2048, 60, 0, true}},
	} {
		SetDeploymentLimits(row.set)
		if got := DeploymentLimits(); !reflect.DeepEqual(got, row.installed) {
			t.Errorf("%s: DeploymentLimits() = %#v, want %#v", row.name, got, row.installed)
		}
		if got := read(policyLimits(t, svc, "w-single")); got != row.want {
			t.Errorf("%s: w-single resolves %+v, want %+v", row.name, got, row.want)
		}
		if got := read(before.EffectiveLimits()); got != row.want {
			t.Errorf("%s: w-single, read before any call, resolves %+v, want %+v", row.name, got, row.want)
		}
		if got := read(PlanDefaults(PlanSingle)); got != (reading{2048, 60, 0, true}) {
			t.Errorf("%s: PlanDefaults(single) reads %+v; the plan's defaults moved with the deployment layer", row.name, got)
		}
	}

	// The layer sits above the tiers: a 0 there lifts a tier's ceiling,
	// and the keys it does not name keep the tier values.
	SetTiersEnforced(true)
	SetDeploymentLimits(map[string]interface{}{LimitMaxMembers: 0, LimitMaxProjects: 0})
	if got, want := readTiers(policyLimits(t, svc, "w-single")), (tierReading{0, 1, 0, 300, false, false, false}); got != want {
		t.Errorf("tiers on, deployment layer opening two counts: w-single resolves %+v, want %+v", got, want)
	}
	SetTiersEnforced(false)

	// On a self-hosted deployment the layer still caps.
	SetSelfHosted(true)
	SetDeploymentLimits(map[string]interface{}{LimitMaxProjects: 1})
	if got, _ := LimitInt(policyLimits(t, svc, "w-single"), LimitMaxProjects); got != 1 {
		t.Errorf("self-hosted, deployment layer max_projects 1: w-single max_projects = %d, want 1", got)
	}
	SetSelfHosted(false)

	// Boot installs what ParseLimits answers, which holds JSON's float64.
	parsed, err := ParseLimits(`{"max_projects": 5, "teams": false}`)
	if err != nil {
		t.Fatal(err)
	}
	SetDeploymentLimits(parsed)
	limits := policyLimits(t, svc, "w-single")
	if got, ok := limits[LimitMaxProjects].(float64); !ok || got != 5 {
		t.Errorf("parsed layer: max_projects = %#v, want float64(5)", limits[LimitMaxProjects])
	}
	if got := Allowed(limits, LimitTeams); got {
		t.Error("parsed layer: teams is allowed, want false")
	}
}

// What the setter does with the map it is given, as it stands: it installs
// it without validation (ParseLimits is the validator, at boot) and keeps
// the caller's map rather than a copy, so a later write to that map moves
// the layer; DeploymentLimits answers a copy, so a write to that does not.
// MaxPlanUploadMB reads the plans alone, so a deployment layer above every
// plan's upload ceiling does not raise it.
func TestDeploymentLimitsSetterKeepsTheMapItIsGiven(t *testing.T) {
	atPolicyDefaults(t)

	svc := NewDefaultService(newPolicyRepoFake(policyOrg("w-single", TypeCompany, PlanSingle, policyCreated)))

	SetDeploymentLimits(map[string]interface{}{"not_catalogued": 7})
	if got := policyLimits(t, svc, "w-single")["not_catalogued"]; got != 7 {
		t.Errorf("an uncatalogued key resolves %#v, want 7", got)
	}

	given := map[string]interface{}{LimitEvidenceStorageMB: 500}
	SetDeploymentLimits(given)
	given[LimitEvidenceStorageMB] = 600
	if got, _ := LimitInt(policyLimits(t, svc, "w-single"), LimitEvidenceStorageMB); got != 600 {
		t.Errorf("after a write to the map given: evidence_storage_mb = %d, want 600", got)
	}
	answered := DeploymentLimits()
	answered[LimitEvidenceStorageMB] = 700
	if got, _ := LimitInt(policyLimits(t, svc, "w-single"), LimitEvidenceStorageMB); got != 600 {
		t.Errorf("after a write to the map DeploymentLimits answered: evidence_storage_mb = %d, want 600", got)
	}

	SetDeploymentLimits(map[string]interface{}{LimitMaxUploadMB: 4096})
	if got, _ := LimitInt(policyLimits(t, svc, "w-single"), LimitMaxUploadMB); got != 4096 {
		t.Errorf("deployment layer max_upload_mb 4096: w-single max_upload_mb = %d, want 4096", got)
	}
	if got := MaxPlanUploadMB(); got != 1024 {
		t.Errorf("deployment layer max_upload_mb 4096: MaxPlanUploadMB() = %d, want 1024", got)
	}
}
