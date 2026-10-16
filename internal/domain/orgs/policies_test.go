package orgs

import (
	"slices"
	"testing"
)

// everyStable runs every plan on stable, chosen by nobody: a policy unlike
// the built-in one in each answer.
type everyStable struct{}

func (everyStable) ChannelForPlan(string) string { return ChannelStable }
func (everyStable) ChannelChoosable(string) bool { return false }
func (everyStable) ChoosablePlans() []string     { return []string{} }

// flatPlans gives every plan one member, tiers or not.
type flatPlans struct{}

func (flatPlans) PlanDefaults(string, bool) map[string]interface{} {
	return map[string]interface{}{LimitMaxMembers: 1}
}

func TestAnInstalledPolicyDecidesWhatAPlanMeans(t *testing.T) {
	t.Cleanup(func() { SetPlanPolicy(nil); SetChannelPolicy(nil) })
	SetPlanPolicy(flatPlans{})
	SetChannelPolicy(everyStable{})

	if got, _ := LimitInt(PlanDefaults(PlanBusiness), LimitMaxMembers); got != 1 {
		t.Errorf("PlanDefaults(business) members = %d under the installed policy, want 1", got)
	}
	if got := ChannelForPlan(PlanSingle); got != ChannelStable {
		t.Errorf("ChannelForPlan(single) = %q under the installed policy, want stable", got)
	}
	if ChannelChoosable(PlanBusiness) || len(ChoosablePlans()) != 0 {
		t.Error("business is choosable under a policy where no plan is")
	}
	org := &Org{BilledPlan: PlanBusiness, ReleaseChannelOverride: ChannelNightly}
	org.ResolveReleaseChannel()
	if org.ReleaseChannel != ChannelStable || !org.ReleaseChannelLocked {
		t.Errorf("resolved %q locked=%v, want stable and locked", org.ReleaseChannel, org.ReleaseChannelLocked)
	}

	SetPlanPolicy(nil)
	SetChannelPolicy(nil)
	if got := ChannelForPlan(PlanBusiness); got != ChannelStable || !ChannelChoosable(PlanBusiness) {
		t.Error("nil did not restore the built-in channels")
	}
	if got, _ := LimitInt(PlanDefaults(PlanSingle), LimitMaxUploadMB); got != 128 {
		t.Errorf("nil did not restore the built-in plans: single uploads %d MB, want 128", got)
	}
}

// The built-in channels' list is exactly the plans they let choose, as the
// SQL that reads the list (persistence) and the Go that asks each plan must
// agree.
func TestTheBuiltinChoosablePlansAreTheOnesThatMayChoose(t *testing.T) {
	var want []string
	for _, p := range PlanNames() {
		if (BuiltinChannels{}).ChannelChoosable(p) {
			want = append(want, p)
		}
	}
	got := slices.Clone(BuiltinChannels{}.ChoosablePlans())
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("ChoosablePlans() = %v, the plans ChannelChoosable accepts are %v", got, want)
	}
}
