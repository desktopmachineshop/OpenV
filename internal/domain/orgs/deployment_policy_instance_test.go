package orgs

import (
	"reflect"
	"strings"
	"testing"
)

// A policy of the test's own, rather than the package default the setters
// write: its methods read it and nothing else, so the test needs no setter
// and no cleanup, and runs in parallel. Each row is one of the deployments
// deployment_policy_test.go pins through the default instance, read the
// same way here.
func TestAPolicyOfItsOwnIsReadByItsMethods(t *testing.T) {
	t.Parallel()

	layered := alphaSingleLimits()
	layered[LimitMaxUploadMB], layered[LimitMaxProjects] = 4096, 5
	for _, row := range []struct {
		name         string
		policy       *DeploymentPolicy
		limits       map[string]interface{} // a shared single-plan workspace's EffectiveLimits
		planProjects int                    // PlanDefaults(single)'s max_projects
		uploadMB     int                    // MaxPlanUploadMB()
		remedy       string                 // the start of ReadOnlyRemedy's answer
	}{
		{"hosted, the tiers off", &DeploymentPolicy{DefaultPlan: PlanSingle},
			alphaSingleLimits(), 0, 1024, "This workspace has more than its plan allows"},
		{"hosted, the tiers on", &DeploymentPolicy{TiersEnforced: true, DefaultPlan: PlanSingle},
			tieredSingleLimits(), 200, 1024, "This workspace has more than its plan allows"},
		{"self-hosted", &DeploymentPolicy{SelfHosted: true, DefaultPlan: PlanSelfHost},
			selfHostLimits(), 0, 0, "This workspace has more than this deployment's limits allow"},
		{"hosted, a deployment layer", &DeploymentPolicy{DefaultPlan: PlanSingle,
			DeploymentLimits: map[string]interface{}{LimitMaxUploadMB: 4096, LimitMaxProjects: 5}},
			layered, 0, 4096, "This workspace has more than its plan allows"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			org := policyOrg("w-single", TypeCompany, PlanSingle, policyCreated)
			if got := row.policy.EffectiveLimits(org); !reflect.DeepEqual(got, row.limits) {
				t.Errorf("EffectiveLimits resolves\n%v\nwant\n%v", got, row.limits)
			}
			if got, _ := LimitInt(row.policy.PlanDefaults(PlanSingle), LimitMaxProjects); got != row.planProjects {
				t.Errorf("PlanDefaults(single) max_projects = %d, want %d", got, row.planProjects)
			}
			if got := row.policy.MaxPlanUploadMB(); got != row.uploadMB {
				t.Errorf("MaxPlanUploadMB() = %d, want %d", got, row.uploadMB)
			}
			if got := row.policy.ReadOnlyRemedy([]string{LimitMaxProjects}); !strings.HasPrefix(got, row.remedy) {
				t.Errorf("ReadOnlyRemedy = %q, want it to start %q", got, row.remedy)
			}
		})
	}
}
