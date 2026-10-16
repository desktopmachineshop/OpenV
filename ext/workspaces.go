package ext

import (
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/envparse"
)

// What a module reads and writes of a workspace's plan and billing state,
// re-exported from the core so that a module outside this repository can
// name it (it cannot import internal packages). These are the core's own
// types and values, not copies: a module's Workspace is the server's.

// WorkspaceBilling is a workspace's mirrored subscription; WorkspaceBillingState
// is what a billing sync writes.
type (
	WorkspaceBilling      = orgs.Billing
	WorkspaceBillingState = orgs.BillingState
)

// The workspace types.
const (
	WorkspacePersonal = orgs.TypePersonal
	WorkspaceCompany  = orgs.TypeCompany
)

// The plans a workspace can be on.
const (
	PlanSingle       = orgs.PlanSingle
	PlanBusinessLite = orgs.PlanBusinessLite
	PlanBusiness     = orgs.PlanBusiness
	PlanEnterprise   = orgs.PlanEnterprise
	PlanSelfHost     = orgs.PlanSelfHost
	PlanOpenSource   = orgs.PlanOpenSource
)

// A subscription's status, as the core reads it for a workspace's
// entitlement.
const (
	PlanStatusNone       = orgs.PlanStatusNone
	PlanStatusTrialing   = orgs.PlanStatusTrialing
	PlanStatusActive     = orgs.PlanStatusActive
	PlanStatusPastDue    = orgs.PlanStatusPastDue
	PlanStatusUnpaid     = orgs.PlanStatusUnpaid
	PlanStatusPaused     = orgs.PlanStatusPaused
	PlanStatusCanceled   = orgs.PlanStatusCanceled
	PlanStatusIncomplete = orgs.PlanStatusIncomplete
	PlanStatusDisputed   = orgs.PlanStatusDisputed
)

// The kinds of provider reference a workspace is found by.
const (
	BillingRefCustomer     = orgs.BillingRefCustomer
	BillingRefSubscription = orgs.BillingRefSubscription
)

// ErrWorkspaceNotFound is what WorkspaceService answers for an id no row has.
var ErrWorkspaceNotFound = orgs.ErrNotFound

// GrantedPlan reports whether a platform admin grants plan rather than a
// subscription buying it.
func GrantedPlan(plan string) bool { return orgs.GrantedPlan(plan) }

// SelfHosted reports whether this deployment is somebody's own
// installation (OPENV_SELF_HOSTED).
func SelfHosted() bool { return orgs.SelfHosted() }

// The environment's own rules for a setting (internal/envparse), so a module
// reads its settings as the core reads its own.

// EnvWholeNumber parses a count: digits only, no sign, unit or exponent.
func EnvWholeNumber(raw string) (int, bool) { return envparse.WholeNumber(raw) }

// EnvSecret reads a secret named name, trimmed, warning on a value that is
// only spaces without ever logging the value.
func EnvSecret(name, raw string) string { return envparse.Secret(name, raw) }

// EnvCount reads a count setting named name: def when unset, and def with a
// warning naming the variable when malformed.
func EnvCount(name, raw string, def int) int { return envparse.Count(name, raw, def) }

// EnvRate reads a refill rate named name: def when unset, and def with a
// warning naming the variable when malformed or not positive and finite.
func EnvRate(name, raw string, def float64) float64 { return envparse.Rate(name, raw, def) }
