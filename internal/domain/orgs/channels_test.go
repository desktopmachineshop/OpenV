package orgs

import "testing"

// TestChannelForPlan: one-person tiers and self-hosted run nightly; company
// tiers, including the legacy team name, run stable and may choose.
func TestChannelForPlan(t *testing.T) {
	for plan, want := range map[string]string{
		PlanSingle: ChannelNightly, PlanFree: ChannelNightly, PlanBusinessLite: ChannelNightly, PlanSelfHost: ChannelNightly, "": ChannelNightly,
		PlanBusiness: ChannelStable, PlanTeam: ChannelStable, PlanEnterprise: ChannelStable,
	} {
		if got := ChannelForPlan(plan); got != want {
			t.Errorf("ChannelForPlan(%q) = %q, want %q", plan, got, want)
		}
		if got := ChannelChoosable(plan); got != (want == ChannelStable) {
			t.Errorf("ChannelChoosable(%q) = %v", plan, got)
		}
	}
}

// TestResolveReleaseChannel: an override counts only where the plan lets an
// admin choose; a personal-tier row that somehow carries one still resolves
// to nightly and reports itself locked.
func TestResolveReleaseChannel(t *testing.T) {
	o := &Org{Plan: PlanBusiness, ReleaseChannelOverride: ChannelNightly}
	o.ResolveReleaseChannel()
	if o.ReleaseChannel != ChannelNightly || o.ReleaseChannelLocked {
		t.Fatalf("business override: %+v", o)
	}
	o = &Org{Plan: PlanBusiness}
	o.ResolveReleaseChannel()
	if o.ReleaseChannel != ChannelStable {
		t.Fatalf("business default: %+v", o)
	}
	o = &Org{Plan: PlanSingle, ReleaseChannelOverride: ChannelStable}
	o.ResolveReleaseChannel()
	if o.ReleaseChannel != ChannelNightly || !o.ReleaseChannelLocked {
		t.Fatalf("single with stray override: %+v", o)
	}
}
