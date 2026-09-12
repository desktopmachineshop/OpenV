package orgs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestZeroMeansUnlimitedEverywhere(t *testing.T) {
	limits := map[string]interface{}{
		LimitMaxMembers:        0,
		LimitMaxProjects:       float64(0),
		LimitEvidenceStorageMB: 5,
	}
	if _, capped := Ceiling(limits, LimitMaxMembers); capped {
		t.Error("an explicit 0 was read as a cap rather than unlimited")
	}
	if _, capped := Ceiling(limits, LimitMaxProjects); capped {
		t.Error("a float 0 was read as a cap")
	}
	if _, capped := Ceiling(limits, "absent_key"); capped {
		t.Error("an absent key was read as a cap")
	}
	if got, capped := Ceiling(limits, LimitEvidenceStorageMB); !capped || got != 5 {
		t.Errorf("a real ceiling read as (%d, %v), want (5, true)", got, capped)
	}
}

// The three layers, and which wins. This is what makes a self-hosted
// deployment tunable without touching the database, while still letting an
// operator pin one workspace differently.
func TestLimitsResolveOrgOverDeploymentOverPlan(t *testing.T) {
	t.Cleanup(func() { SetDeploymentLimits(nil) })

	org := &Org{Plan: PlanSingle}
	if got, _ := LimitInt(org.EffectiveLimits(), LimitEvidenceStorageMB); got != 2048 {
		t.Fatalf("plan default is %d, want 2048", got)
	}

	// The deployment overrides the plan.
	SetDeploymentLimits(map[string]interface{}{LimitEvidenceStorageMB: 500})
	if got, _ := LimitInt(org.EffectiveLimits(), LimitEvidenceStorageMB); got != 500 {
		t.Fatalf("deployment default is %d, want 500", got)
	}

	// The workspace's own limit overrides the deployment.
	org.Limits = map[string]interface{}{LimitEvidenceStorageMB: 99}
	if got, _ := LimitInt(org.EffectiveLimits(), LimitEvidenceStorageMB); got != 99 {
		t.Fatalf("workspace limit is %d, want 99", got)
	}

	// A key nobody overrode still falls all the way back to the plan.
	if got, _ := LimitInt(org.EffectiveLimits(), LimitRunnerSessionMinutes); got != 60 {
		t.Fatalf("unoverridden key is %d, want the plan's 60", got)
	}
}

// EffectiveLimits must never mutate what it merges, or one workspace's
// override would leak into the deployment defaults for every other.
func TestEffectiveLimitsDoesNotMutateItsSources(t *testing.T) {
	t.Cleanup(func() { SetDeploymentLimits(nil) })
	SetDeploymentLimits(map[string]interface{}{LimitMaxMembers: 10})

	org := &Org{Plan: PlanSingle, Limits: map[string]interface{}{LimitMaxMembers: 3}}
	_ = org.EffectiveLimits()

	if got, _ := LimitInt(DeploymentLimits(), LimitMaxMembers); got != 10 {
		t.Errorf("the deployment defaults were mutated to %d", got)
	}
	if got, _ := LimitInt(org.Limits, LimitMaxMembers); got != 3 {
		t.Errorf("the workspace's own limits were mutated to %d", got)
	}
	other := &Org{Plan: PlanSingle}
	if got, _ := LimitInt(other.EffectiveLimits(), LimitMaxMembers); got != 10 {
		t.Errorf("another workspace saw %d; one workspace's override leaked", got)
	}
}

// A workspace on an unrecognized plan must land on the tightest hosted
// defaults, never on unlimited: a typo in the column cannot become free rein.
func TestAnUnknownPlanIsNotUnlimited(t *testing.T) {
	for _, plan := range []string{"", "gold", "TEAM"} {
		org := &Org{Plan: plan}
		got, _ := LimitInt(org.EffectiveLimits(), LimitEvidenceStorageMB)
		if got != 2048 {
			t.Errorf("plan %q got %d MB of storage, want the restrictive 2048", plan, got)
		}
	}
}

// The names that shipped first are still in the database and must keep
// resolving to the tier they were.
func TestLegacyPlanNamesStillResolve(t *testing.T) {
	free, _ := LimitInt(PlanDefaults(PlanFree), LimitEvidenceStorageMB)
	single, _ := LimitInt(PlanDefaults(PlanSingle), LimitEvidenceStorageMB)
	if free != single {
		t.Errorf("free=%d single=%d; the legacy name drifted from its tier", free, single)
	}
	team, _ := LimitInt(PlanDefaults(PlanTeam), LimitEvidenceStorageMB)
	business, _ := LimitInt(PlanDefaults(PlanBusiness), LimitEvidenceStorageMB)
	if team != business {
		t.Errorf("team=%d business=%d; the legacy name drifted from its tier", team, business)
	}
}

// Somebody else's hardware is not ours to ration.
func TestSelfHostAndEnterpriseAreUnlimited(t *testing.T) {
	for _, plan := range []string{PlanSelfHost, PlanEnterprise} {
		limits := PlanDefaults(plan)
		for _, def := range Catalog() {
			if _, capped := Ceiling(limits, def.Key); capped {
				t.Errorf("plan %q caps %s", plan, def.Key)
			}
		}
	}
}

// The count limits ship open on every plan: the pricing page promises alpha
// workspaces every tier's features, and switching a cap on retroactively would
// break workspaces already over it.
func TestCountLimitsShipOpenOnEveryPlan(t *testing.T) {
	counts := []string{LimitMaxMembers, LimitMaxProjects, LimitMaxSharedWorkspaces}
	for _, plan := range []string{PlanSingle, PlanBusinessLite, PlanBusiness, PlanEnterprise, PlanSelfHost, PlanFree, PlanTeam} {
		limits := PlanDefaults(plan)
		for _, key := range counts {
			if _, capped := Ceiling(limits, key); capped {
				t.Errorf("plan %q already caps %s; alpha workspaces would start being refused", plan, key)
			}
		}
	}
}

func TestParseLimitsAcceptsAKnownObject(t *testing.T) {
	got, err := ParseLimits(`{"max_members": 5, "evidence_storage_mb": 1024}`)
	if err != nil {
		t.Fatalf("a valid object was refused: %v", err)
	}
	if v, _ := LimitInt(got, LimitMaxMembers); v != 5 {
		t.Errorf("max_members parsed as %d", v)
	}
}

// A typo that silently did nothing would be indistinguishable from a limit
// that does not work, so an unknown key is refused loudly.
func TestParseLimitsRefusesWhatItCannotHonour(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"unknown key", `{"max_membrs": 5}`, "unknown limit"},
		{"not a number", `{"max_members": "five"}`, "must be a number"},
		{"negative", `{"max_members": -1}`, "must not be negative"},
		{"not an object", `nonsense`, "not a JSON object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseLimits(tc.raw)
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not explain the problem (want %q)", err, tc.want)
			}
		})
	}
	if got, err := ParseLimits(""); got != nil || err != nil {
		t.Errorf("an unset variable should be a quiet no-op, got (%v, %v)", got, err)
	}
}

// Every catalogued limit must be described, because the catalogue is what the
// docs, the API and the settings panel all render from.
func TestEveryCataloguedLimitIsDescribed(t *testing.T) {
	for _, def := range Catalog() {
		if def.Label == "" || def.Description == "" {
			t.Errorf("%s is catalogued without a label or description: %+v", def.Key, def)
		}
		if def.Unit == UnitUnknown {
			t.Errorf("%s has no unit, so its number cannot be rendered", def.Key)
		}
	}
	// And every key a plan sets must be in the catalogue, or it is a limit
	// nobody can discover.
	for _, plan := range []string{PlanSingle, PlanBusiness, PlanSelfHost} {
		for key := range PlanDefaults(plan) {
			if _, known := Describe(key); !known {
				t.Errorf("plan %q sets %q, which is not catalogued", plan, key)
			}
		}
	}
}

func TestCheckCeilingAllowsRoomAndRefusesTheCrossing(t *testing.T) {
	limits := map[string]interface{}{LimitMaxMembers: 3}

	if err := CheckCeiling(limits, LimitMaxMembers, 2, 1); err != nil {
		t.Errorf("the last free seat was refused: %v", err)
	}
	err := CheckCeiling(limits, LimitMaxMembers, 3, 1)
	if err == nil {
		t.Fatal("one over the ceiling was allowed")
	}
	if !errors.Is(err, ErrLimitReached) {
		t.Errorf("the refusal does not match ErrLimitReached: %v", err)
	}
	// Unlimited never refuses, however large the request.
	if err := CheckCeiling(map[string]interface{}{}, LimitMaxMembers, 1_000_000, 50); err != nil {
		t.Errorf("an unlimited workspace refused: %v", err)
	}
}

// The refusal has to answer three questions: what stopped me, how close am I,
// and what do I do now.
func TestALimitRefusalSaysWhatAndHowClose(t *testing.T) {
	t.Cleanup(func() { SetSelfHosted(false) })
	SetSelfHosted(false)

	err := NewLimitError(LimitMaxMembers, 5, 5).WithDetail("including pending invitations")
	msg := err.Error()
	for _, want := range []string{"Workspace members", "allows 5", "already has 5", "including pending invitations"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal is missing %q: %s", want, msg)
		}
	}
	if !strings.Contains(msg, "Upgrade") {
		t.Errorf("a hosted refusal does not offer the upgrade: %s", msg)
	}
}

// Telling a self-hoster to upgrade their plan is nonsense — they own the
// hardware and there is nobody to pay. They need the setting to change.
func TestASelfHostedRefusalNamesTheSettingNotAPlan(t *testing.T) {
	t.Cleanup(func() { SetSelfHosted(false) })
	SetSelfHosted(true)

	msg := NewLimitError(LimitMaxMembers, 5, 5).Error()
	if strings.Contains(msg, "Upgrade") || strings.Contains(msg, "plan") {
		t.Errorf("a self-hosted refusal offers a plan upgrade: %s", msg)
	}
	if !strings.Contains(msg, "OPENV_LIMITS") || !strings.Contains(msg, LimitMaxMembers) {
		t.Errorf("a self-hosted refusal does not name the setting to change: %s", msg)
	}
}

// A storage refusal must not read as a count of things.
func TestARefusalRendersItsUnit(t *testing.T) {
	msg := NewLimitError(LimitEvidenceStorageMB, 2048, 2048).Error()
	if !strings.Contains(msg, "2048 MB") {
		t.Errorf("a storage limit does not render as MB: %s", msg)
	}
	count := NewLimitError(LimitMaxProjects, 50, 50).Error()
	if strings.Contains(count, "MB") {
		t.Errorf("a count limit rendered a unit: %s", count)
	}
}

// TestLimitFloatCoercions: every numeric shape that can reach a limits map
// reads back; non-numbers and absent keys report !ok.
func TestLimitFloatCoercions(t *testing.T) {
	m := map[string]interface{}{
		"f":       float64(1.5),
		"i":       int(7),
		"i64":     int64(9),
		"num":     json.Number("3.25"),
		"bad_num": json.Number("not-a-number"),
		"str":     "2048",
		"nil":     nil,
	}
	want := map[string]struct {
		v  float64
		ok bool
	}{
		"f":       {1.5, true},
		"i":       {7, true},
		"i64":     {9, true},
		"num":     {3.25, true},
		"bad_num": {0, false},
		"str":     {0, false},
		"nil":     {0, false},
		"absent":  {0, false},
	}
	for key, w := range want {
		v, ok := LimitFloat(m, key)
		if v != w.v || ok != w.ok {
			t.Errorf("LimitFloat(%q) = (%v, %v), want (%v, %v)", key, v, ok, w.v, w.ok)
		}
	}
}

// A deployment that turns OPENV_SELF_HOSTED on later must lift the ceilings
// for the workspaces it ALREADY has. Those were created on a hosted plan, and
// they are exactly the population the setting exists for — reading the stored
// plan would leave "no limits from us" false for every existing install.
func TestSelfHostedIgnoresTheStoredPlan(t *testing.T) {
	t.Cleanup(func() { SetSelfHosted(false); SetDeploymentLimits(nil) })

	existing := &Org{Plan: PlanSingle}
	if _, capped := Ceiling(existing.EffectiveLimits(), LimitEvidenceStorageMB); !capped {
		t.Fatal("a hosted workspace is uncapped before the flag; the test proves nothing")
	}

	SetSelfHosted(true)
	for _, def := range Catalog() {
		if _, capped := Ceiling(existing.EffectiveLimits(), def.Key); capped {
			t.Errorf("a pre-existing workspace is still capped on %s after going self-hosted", def.Key)
		}
	}

	// The operator can still cap one, through either layer above.
	SetDeploymentLimits(map[string]interface{}{LimitMaxProjects: 5})
	if got, capped := Ceiling(existing.EffectiveLimits(), LimitMaxProjects); !capped || got != 5 {
		t.Errorf("a self-hosted operator could not set a limit: (%d, %v)", got, capped)
	}
	existing.Limits = map[string]interface{}{LimitMaxProjects: 9}
	if got, _ := Ceiling(existing.EffectiveLimits(), LimitMaxProjects); got != 9 {
		t.Errorf("a self-hosted operator could not set one workspace apart: %d", got)
	}
}
