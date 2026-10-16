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

// TestASelfHostedWorkspaceRunsNightlyWhateverItsRowSays: on a self-hosted
// deployment the channel follows the self-host plan, as limits do, so a
// workspace whose row still carries a company plan (and even a stable
// choice) runs nightly, locked, and sees every feature its own server
// ships; choosing a channel or an upgrade window is refused as for any
// nightly-only plan, and no plan's workspaces are listed on stable. Before,
// the channel read the billed plan, and such a workspace waited on a stable
// release nobody turns on for it.
func TestASelfHostedWorkspaceRunsNightlyWhateverItsRowSays(t *testing.T) {
	atPolicyDefaults(t)
	SetSelfHosted(true)

	for _, plan := range []string{PlanBusiness, PlanTeam, PlanEnterprise, PlanSingle} {
		for _, override := range []string{"", ChannelStable} {
			o := &Org{BilledPlan: plan, ReleaseChannelOverride: override}
			o.ResolveReleaseChannel()
			if o.ChannelPlan() != PlanSelfHost || o.ReleaseChannel != ChannelNightly || !o.ReleaseChannelLocked {
				t.Errorf("self-hosted %s (override %q): channel plan %q, channel %q, locked %v; want self_host, nightly, locked",
					plan, override, o.ChannelPlan(), o.ReleaseChannel, o.ReleaseChannelLocked)
			}
		}
	}
	if got := DeploymentChoosablePlans(); len(got) != 0 {
		t.Errorf("DeploymentChoosablePlans() = %v on a self-hosted deployment, want none", got)
	}

	repo := &planRepoFake{org: &Org{ID: "o1", BilledPlan: PlanBusiness}}
	svc := NewDefaultService(repo)
	if _, err := svc.SetReleaseChannel("o1", ChannelStable); err != ErrChannelLocked {
		t.Errorf("SetReleaseChannel on a self-hosted business row = %v, want ErrChannelLocked", err)
	}
	if _, err := svc.SetUpgradeWindow("o1", 1, 9, "UTC"); err != ErrChannelLocked {
		t.Errorf("SetUpgradeWindow on a self-hosted business row = %v, want ErrChannelLocked", err)
	}

	SetSelfHosted(false)
	o := &Org{BilledPlan: PlanBusiness}
	o.ResolveReleaseChannel()
	if o.ChannelPlan() != PlanBusiness || o.ReleaseChannel != ChannelStable || o.ReleaseChannelLocked {
		t.Errorf("hosted business: channel plan %q, channel %q, locked %v; want business, stable, choosable",
			o.ChannelPlan(), o.ReleaseChannel, o.ReleaseChannelLocked)
	}
	if got := DeploymentChoosablePlans(); len(got) != len(ChoosablePlans()) {
		t.Errorf("DeploymentChoosablePlans() = %v on the hosted service, want %v", got, ChoosablePlans())
	}
}
