package main

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/ext"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

type policyModule struct {
	name     string
	plans    ext.PlanPolicy
	channels ext.ChannelPolicy
}

func (m policyModule) Name() string                     { return m.name }
func (m policyModule) PlanPolicy() ext.PlanPolicy       { return m.plans }
func (m policyModule) ChannelPolicy() ext.ChannelPolicy { return m.channels }

// nightlyOnly runs every plan nightly, with no choice.
type nightlyOnly struct{}

func (nightlyOnly) ChannelForPlan(string) string { return ext.ChannelNightly }
func (nightlyOnly) ChannelChoosable(string) bool { return false }
func (nightlyOnly) ChoosablePlans() []string     { return []string{} }

func TestAModulesChannelPolicyIsInstalled(t *testing.T) {
	t.Cleanup(func() { orgs.SetPlanPolicy(nil); orgs.SetChannelPolicy(nil) })
	if err := installPolicies([]ext.Module{policyModule{name: "channels", channels: nightlyOnly{}}}); err != nil {
		t.Fatal(err)
	}
	if got := orgs.ChannelForPlan(orgs.PlanBusiness); got != orgs.ChannelNightly {
		t.Errorf("business runs %q after the module set its policy, want nightly", got)
	}
}

func TestTwoModulesSettingOnePolicyIsAnError(t *testing.T) {
	t.Cleanup(func() { orgs.SetPlanPolicy(nil); orgs.SetChannelPolicy(nil) })
	err := installPolicies([]ext.Module{
		policyModule{name: "a", plans: ext.BuiltinPlans{}},
		policyModule{name: "b", plans: ext.BuiltinPlans{}},
	})
	if err == nil || !strings.Contains(err.Error(), "a and b") {
		t.Errorf("installPolicies = %v, want the two modules named", err)
	}
}
