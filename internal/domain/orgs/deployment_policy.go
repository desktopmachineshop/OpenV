// The deployment policy: the four settings that say how this deployment
// rations its workspaces, the package default instance that holds them, and
// the package setters boot installs them through.

package orgs

// DeploymentPolicy is the deployment's four settings. What reads them is
// its methods: PlanDefaults (the tiers), MaxPlanUploadMB, ReadOnlyRemedy and
// EffectiveLimits.
//
// The package keeps one, the default instance. The package setters below
// write it, at boot (cmd/server's wire_config.go, and wire_workspace.go for
// the tiers, after the service is built), and DefaultService, the free
// functions of the same names as the methods, Org.EffectiveLimits and a
// LimitError's message read it whenever they are called. Nothing holds a
// copy of it, so a setting installed after the service is built is in force
// from the next call. Its fields are read and written without a lock, as
// the package variables they replace were: boot writes them before it
// serves.
type DeploymentPolicy struct {
	// SelfHosted records whether this deployment is somebody's own
	// installation. It picks the remedy a refusal offers (LimitError) and
	// puts every workspace on the self-host plan (EffectiveLimits). The
	// hosted service leaves it false.
	SelfHosted bool
	// TiersEnforced is the switch between the alpha terms — every count
	// open, every flag on, for everybody — and the tiers as sold. It is
	// thrown by OPENV_BILLING_GRANDFATHER_BEFORE: naming the date is what
	// turns the tiers on, and the same boot grandfathers every workspace
	// created before it, so the two can never be out of step.
	TiersEnforced bool
	// DefaultPlan is the plan new workspaces are created on. The hosted
	// service leaves it at PlanSingle; a self-hosted deployment sets
	// PlanSelfHost, which is what makes "no limits from us" true rather
	// than merely advertised.
	DefaultPlan string
	// DeploymentLimits is the middle layer: a deployment-wide override read
	// once at boot from OPENV_LIMITS. Nil until SetDeploymentLimits is
	// called, which is the hosted service's state — it runs on plan
	// defaults alone.
	DeploymentLimits map[string]interface{}
}

// defaultPolicy is the package default instance, at the defaults a hosted
// boot starts from: the tiers off, hosted, new workspaces on the single
// plan, no deployment layer.
var defaultPolicy = &DeploymentPolicy{DefaultPlan: PlanSingle}

// SetSelfHosted tells the domain which remedy to offer when a limit is hit.
// Called once at boot from the deployment configuration.
func SetSelfHosted(v bool) { defaultPolicy.SelfHosted = v }

// SelfHosted reports the configured deployment mode.
func SelfHosted() bool { return defaultPolicy.SelfHosted }

// SetTiersEnforced turns the tier values on or off. Called once at boot.
func SetTiersEnforced(on bool) { defaultPolicy.TiersEnforced = on }

// TiersEnforced reports whether the tier values are in force.
func TiersEnforced() bool { return defaultPolicy.TiersEnforced }

// SetDefaultPlan installs the plan new workspaces are created on: any plan
// ValidPlan accepts, the open-source plan included. An unknown name is
// ignored rather than stored, so a typo cannot create workspaces on a plan
// whose defaults nobody has written; cmd/server warns about one at boot and
// installs the deployment's own default instead.
func SetDefaultPlan(plan string) {
	if ValidPlan(plan) {
		defaultPolicy.DefaultPlan = plan
	}
}

// DefaultPlan is the plan new workspaces are created on.
func DefaultPlan() string { return defaultPolicy.DefaultPlan }

// SetDeploymentLimits installs the deployment-wide defaults. Called once at
// boot; a nil or empty map leaves plan defaults in charge.
func SetDeploymentLimits(limits map[string]interface{}) {
	defaultPolicy.DeploymentLimits = limits
}

// DeploymentLimits returns the configured deployment-wide defaults.
func DeploymentLimits() map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range defaultPolicy.DeploymentLimits {
		out[k] = v
	}
	return out
}
