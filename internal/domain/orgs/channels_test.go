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
	o := &Org{BilledPlan: PlanBusiness, ReleaseChannelOverride: ChannelNightly}
	o.ResolveReleaseChannel()
	if o.ReleaseChannel != ChannelNightly || o.ReleaseChannelLocked {
		t.Fatalf("business override: %+v", o)
	}
	o = &Org{BilledPlan: PlanBusiness}
	o.ResolveReleaseChannel()
	if o.ReleaseChannel != ChannelStable {
		t.Fatalf("business default: %+v", o)
	}
	o = &Org{BilledPlan: PlanSingle, ReleaseChannelOverride: ChannelStable}
	o.ResolveReleaseChannel()
	if o.ReleaseChannel != ChannelNightly || !o.ReleaseChannelLocked {
		t.Fatalf("single with stray override: %+v", o)
	}
}

// TestAPlatformAdminsPlanMoveKeepsNightly: a platform admin's move from a
// nightly-only plan onto Business, Team or Enterprise keeps a workspace that
// chose no channel on nightly, as a checkout does, so the features it uses
// stay on (#379 question 19, REQ-154); it resolved to the new plan's stable
// default, where no stable release is turned on and every gate is shut. A
// move onto a nightly-only plan, between two choosing plans, or of a
// workspace that holds a choice keeps what the workspace held.
func TestAPlatformAdminsPlanMoveKeepsNightly(t *testing.T) {
	for _, tc := range []struct {
		from, override, to        string
		wantOverride, wantChannel string
	}{
		{PlanSingle, "", PlanBusiness, ChannelNightly, ChannelNightly},
		{PlanBusinessLite, "", PlanEnterprise, ChannelNightly, ChannelNightly},
		{PlanOpenSource, "", PlanTeam, ChannelNightly, ChannelNightly},
		{PlanSelfHost, "", PlanBusiness, ChannelNightly, ChannelNightly},
		{PlanSingle, "", PlanBusinessLite, "", ChannelNightly},
		{PlanBusiness, "", PlanEnterprise, "", ChannelStable},
		{PlanBusiness, "", PlanSingle, "", ChannelNightly},
		// A choice made on a company plan survives a stay on a nightly-only
		// one: the workspace chose, so the move does not choose for it.
		{PlanSingle, ChannelStable, PlanBusiness, ChannelStable, ChannelStable},
		{PlanBusiness, ChannelStable, PlanEnterprise, ChannelStable, ChannelStable},
	} {
		if got := ChannelOverrideAfterMove(tc.from, tc.to, tc.override); got != tc.wantOverride {
			t.Errorf("ChannelOverrideAfterMove(%q, %q, %q) = %q, want %q", tc.from, tc.to, tc.override, got, tc.wantOverride)
		}
		repo := &planRepoFake{org: &Org{ID: "o1", BilledPlan: tc.from, ReleaseChannelOverride: tc.override}}
		org, err := NewDefaultService(repo).SetPlan("o1", tc.to)
		if err != nil {
			t.Fatalf("%s → %s: %v", tc.from, tc.to, err)
		}
		if org.ReleaseChannelOverride != tc.wantOverride || org.ReleaseChannel != tc.wantChannel {
			t.Errorf("%s (override %q) → %s: answered override %q, channel %q; want %q, %q",
				tc.from, tc.override, tc.to, org.ReleaseChannelOverride, org.ReleaseChannel, tc.wantOverride, tc.wantChannel)
		}
	}
}
