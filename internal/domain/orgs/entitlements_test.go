package orgs

import (
	"errors"
	"testing"
	"time"
)

// The entitled plan is the billed plan while the subscription is in good
// standing and the free tier once it is not. A workspace with no billing
// relationship at all — every one that exists today — resolves exactly as it
// did before billing existed, on every plan.
func TestEntitledPlanFollowsBillingStatus(t *testing.T) {
	keep := []string{"", PlanStatusNone, PlanStatusActive, PlanStatusTrialing, PlanStatusPastDue}
	drop := []string{PlanStatusUnpaid, PlanStatusPaused, PlanStatusCanceled, PlanStatusIncomplete, PlanStatusDisputed, "something-new"}

	for _, plan := range allPlans {
		for _, status := range keep {
			o := &Org{BilledPlan: plan, Billing: Billing{Status: status}}
			if got := o.EntitledPlan(); got != plan {
				t.Errorf("plan %q status %q: entitled to %q, want the billed plan", plan, status, got)
			}
		}
		for _, status := range drop {
			o := &Org{BilledPlan: plan, Billing: Billing{Status: status}}
			if got := o.EntitledPlan(); got != PlanSingle {
				t.Errorf("plan %q status %q: entitled to %q, want the free tier", plan, status, got)
			}
		}
	}
}

// EffectiveLimits reads the entitled plan, so a lapsed Business workspace
// gets the free tier's numbers with no other code knowing billing exists —
// and a past-due one keeps paying's numbers while the provider retries.
func TestEffectiveLimitsDropOnLapseAndHoldWhilePastDue(t *testing.T) {
	t.Cleanup(func() { SetDeploymentLimits(nil) })
	business, _ := LimitInt(PlanDefaults(PlanBusiness), LimitEvidenceStorageMB)
	single, _ := LimitInt(PlanDefaults(PlanSingle), LimitEvidenceStorageMB)
	if business == single {
		t.Fatal("the test needs two plans that differ on evidence storage")
	}

	o := &Org{BilledPlan: PlanBusiness, Billing: Billing{Status: PlanStatusPastDue}}
	if got, _ := LimitInt(o.EffectiveLimits(), LimitEvidenceStorageMB); got != business {
		t.Errorf("past due resolved %d, want the paid %d", got, business)
	}
	o.Billing.Status = PlanStatusCanceled
	if got, _ := LimitInt(o.EffectiveLimits(), LimitEvidenceStorageMB); got != single {
		t.Errorf("canceled resolved %d, want the free tier's %d", got, single)
	}

	// The layers above the plan still win on a lapse: a grandfathered
	// workspace's own override and the deployment's setting both survive.
	o.Limits = map[string]interface{}{LimitEvidenceStorageMB: 77}
	if got, _ := LimitInt(o.EffectiveLimits(), LimitEvidenceStorageMB); got != 77 {
		t.Errorf("a lapsed workspace lost its own override: %d", got)
	}
	o.Limits = nil
	SetDeploymentLimits(map[string]interface{}{LimitEvidenceStorageMB: 55})
	if got, _ := LimitInt(o.EffectiveLimits(), LimitEvidenceStorageMB); got != 55 {
		t.Errorf("a lapsed workspace lost the deployment layer: %d", got)
	}
}

// Self-hosting beats everything, billing status included: there is no
// billing relationship on somebody else's hardware.
func TestSelfHostedIgnoresBillingStatus(t *testing.T) {
	SetSelfHosted(true)
	t.Cleanup(func() { SetSelfHosted(false) })
	o := &Org{BilledPlan: PlanBusiness, Billing: Billing{Status: PlanStatusCanceled}}
	if _, capped := Ceiling(o.EffectiveLimits(), LimitEvidenceStorageMB); capped {
		t.Error("a self-hosted workspace was capped because of a billing status")
	}
}

func TestBillingLiveAndGrantedPlan(t *testing.T) {
	for _, status := range []string{PlanStatusTrialing, PlanStatusActive, PlanStatusPastDue} {
		if !(Billing{Status: status}).Live() {
			t.Errorf("%s should be live", status)
		}
	}
	for _, status := range []string{"", PlanStatusNone, PlanStatusCanceled, PlanStatusUnpaid, PlanStatusPaused, PlanStatusIncomplete, PlanStatusDisputed} {
		if (Billing{Status: status}).Live() {
			t.Errorf("%s should not be live", status)
		}
	}
	for _, plan := range allPlans {
		want := plan == PlanEnterprise || plan == PlanOpenSource
		if GrantedPlan(plan) != want {
			t.Errorf("GrantedPlan(%q) = %v", plan, !want)
		}
	}
}

// A grant and a subscription cannot both decide the plan: a platform admin
// cannot move a workspace with a live subscription onto a granted plan.
func TestSetPlanRefusesAGrantOverALiveSubscription(t *testing.T) {
	repo := &planRepoFake{org: &Org{ID: "o1", BilledPlan: PlanBusiness, Billing: Billing{Status: PlanStatusActive, SubscriptionRef: "sub_1"}}}
	svc := NewDefaultService(repo)

	if _, err := svc.SetPlan("o1", PlanEnterprise); !errors.Is(err, ErrBillingActive) {
		t.Fatalf("granting over a live subscription: err = %v, want ErrBillingActive", err)
	}
	if repo.plans != nil {
		t.Fatalf("a refused grant wrote %v", repo.plans)
	}
	// Moving between billable plans is not the grant path's concern.
	if _, err := svc.SetPlan("o1", PlanBusinessLite); err != nil {
		t.Fatalf("moving between tiers: %v", err)
	}
	// Once the subscription is over, the grant goes through.
	repo.org.Billing.Status = PlanStatusCanceled
	if _, err := svc.SetPlan("o1", PlanEnterprise); err != nil {
		t.Fatalf("granting after cancellation: %v", err)
	}
	if len(repo.plans) != 2 || repo.plans[1] != PlanEnterprise {
		t.Fatalf("writes = %v", repo.plans)
	}
}

// Flags parse as bools and only as bools, and an absent flag is allowed —
// a limit check must never be the reason a legitimate action fails.
func TestFlagsParseAsBoolsAndDefaultToAllowed(t *testing.T) {
	got, err := ParseLimits(`{"teams": false, "max_members": 4}`)
	if err != nil {
		t.Fatal(err)
	}
	if Allowed(got, LimitTeams) {
		t.Error("teams:false parsed as allowed")
	}
	if Allowed(got, LimitHostedAutomation) {
		// Absent from the parsed map: allowed. (After the three-layer
		// merge every plan sets it, so absence means "never resolved".)
	} else {
		t.Error("an absent flag read as refused")
	}
	if _, err := ParseLimits(`{"teams": 1}`); err == nil {
		t.Error("a number was accepted for a flag")
	}
	if _, err := ParseLimits(`{"max_members": true}`); err == nil {
		t.Error("a bool was accepted for a count")
	}

	// Every plan names every flag, on, for now.
	for _, plan := range allPlans {
		limits := (&Org{BilledPlan: plan}).EffectiveLimits()
		for _, key := range []string{LimitHostedAutomation, LimitTeams, LimitWorkspaceBudget} {
			if _, ok := limits[key].(bool); !ok {
				t.Errorf("plan %q does not set %s", plan, key)
			}
			if !Allowed(limits, key) {
				t.Errorf("plan %q ships with %s off; the flags land on before any tier turns one off", plan, key)
			}
		}
	}
	// A workspace override turns one off and the flag reads it.
	o := &Org{BilledPlan: PlanBusiness, Limits: map[string]interface{}{LimitTeams: false}}
	if Allowed(o.EffectiveLimits(), LimitTeams) {
		t.Error("a workspace override to false was not honoured")
	}
}

// planRepoFake is the smallest Repository that lets SetPlan run.
type planRepoFake struct {
	Repository
	org   *Org
	plans []string
}

func (f *planRepoFake) FindOrgByID(id string) (*Org, error) {
	if f.org != nil && f.org.ID == id {
		c := *f.org
		return &c, nil
	}
	return nil, nil
}

func (f *planRepoFake) SetPlan(id, plan string) error {
	f.plans = append(f.plans, plan)
	f.org.BilledPlan = plan
	return nil
}

var _ = time.Now
