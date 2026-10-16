// Package ext is OpenV's extension API: what a module built outside the
// core product needs to add routes, react to workspace changes and answer
// in the API's own shapes, without reaching into the server's internals.
//
// The hosted service at openv.app adds billing and its other hosted-only
// parts as modules (docs/plans/hosted-edition-separation.md); a
// self-hosted server runs with none. The package holds interfaces, small
// value types and aliases of the core types a module reads, nothing that
// runs on its own: the server is the Host, and it registers each Module's
// routes in the position the core gives them.
//
// The API is versioned with OpenV. A change that breaks a module is named
// in the release notes.
package ext

import (
	"context"
	"net/http"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Workspace is a workspace as the core holds it (the UI's word; orgs in the
// code).
type Workspace = orgs.Org

// WorkspaceService reads and writes workspaces.
type WorkspaceService = orgs.Service

// User is an account.
type User = users.User

// The workspace roles a route may require.
const (
	RoleAdmin  = orgs.RoleAdmin
	RoleMember = orgs.RoleMember
)

// FeatureWorkspaceBilling gates the purchase path on the release channel
// (release/features.go).
const FeatureWorkspaceBilling = release.FeatureWorkspaceBilling

// The error codes a module answers with, the same strings as the core's
// catalogue (internal/api/httperr.go), which a client branches on. A test
// in internal/api holds the two in step.
const (
	ErrCodeBillingUnavailable = "billing_unavailable"
	ErrCodeBillingUpstream    = "billing_upstream"
	ErrCodeUnknownPlan        = "unknown_plan"
	ErrCodeAlreadySubscribed  = "already_subscribed"
	ErrCodeNoSubscription     = "no_subscription"
	ErrCodeGrantedPlan        = "granted_plan"
	ErrCodeNoCustomer         = "no_customer"
	ErrCodeCheckoutMismatch   = "checkout_mismatch"
)

// Module is a unit of extension. What it does is the optional interfaces
// below that it implements.
type Module interface {
	// Name identifies the module in logs and route listings.
	Name() string
}

// RouteModule adds HTTP routes. The Host calls Routes once, when it builds
// its router, after SetHost.
type RouteModule interface {
	Module
	Routes(r Router)
}

// HostAware modules are handed the Host before anything else is called.
type HostAware interface {
	Module
	SetHost(h Host)
}

// WorkspaceObserver is told about workspace changes after the core has
// committed them. It must not block or fail the request: work that talks to
// a third party goes on a queue of the module's own.
type WorkspaceObserver interface {
	Module
	// SeatsChanged: a workspace's seat count may have moved (a member
	// added or removed, an invitation sent or revoked).
	SeatsChanged(orgID string)
	// WorkspaceDeleted and WorkspaceRestored follow a workspace into the
	// recycle bin and back.
	WorkspaceDeleted(ctx context.Context, org *Workspace)
	WorkspaceRestored(ctx context.Context, org *Workspace)
}

// Router is where a RouteModule declares its routes. A route's guard is
// declared, not coded: the Host runs it before the handler, exactly as the
// core's own guard of that kind, so a module cannot forget one and the
// route listings show it.
type Router interface {
	Handle(method, path string, handler http.HandlerFunc, opts ...RouteOption)
}

// RouteOption qualifies a route.
type RouteOption struct {
	kind    string
	pathVar string
	role    string
}

// Kind, PathVar and Role describe the option to the Host.
func (o RouteOption) Kind() string    { return o.kind }
func (o RouteOption) PathVar() string { return o.pathVar }
func (o RouteOption) Role() string    { return o.role }

// The option kinds.
const (
	OptionAlwaysWritable       = "always_writable"
	OptionRequireWorkspaceRole = "require_workspace_role"
)

// AlwaysWritable keeps a mutating route open on a workspace that holds
// more than its plan allows, as the core's alwaysWritable does for
// export and member removal.
func AlwaysWritable() RouteOption { return RouteOption{kind: OptionAlwaysWritable} }

// RequireWorkspaceRole refuses a caller below role in the workspace whose
// id is the path variable pathVar, with the core's own answers (401, 403,
// 404, and the read-only plan gate on a write).
func RequireWorkspaceRole(pathVar, role string) RouteOption {
	return RouteOption{kind: OptionRequireWorkspaceRole, pathVar: pathVar, role: role}
}

// Limiter is a token bucket per key.
type Limiter interface {
	// Allow consumes one token for key, or reports how long until one is
	// free.
	Allow(key string) (bool, time.Duration)
}

// Host is what the server lends a module: the request's caller and gates,
// and the API's own way of answering.
type Host interface {
	// CurrentUser is the signed-in account of the request, or nil.
	CurrentUser(r *http.Request) *User
	// PathVar is a variable of the route's path template.
	PathVar(r *http.Request, name string) string
	// FeatureEnabled is the release-channel gate of key for the caller in
	// workspace orgID.
	FeatureEnabled(r *http.Request, orgID, key string) bool
	// Workspaces reads and writes workspaces.
	Workspaces() WorkspaceService
	// CountSeats is the workspace's seat count, as its seat limit counts.
	CountSeats(orgID string) (int, error)
	// FrontendURL is the app's origin, without a trailing slash.
	FrontendURL() string
	// NewLimiter builds a token bucket of burst tokens refilled at
	// refillPerHour.
	NewLimiter(burst int, refillPerHour float64) Limiter

	// WriteFeatureGated answers a write that the workspace's release
	// channel has not received yet, as the core's own gates do.
	WriteFeatureGated(w http.ResponseWriter)

	// RespondJSON, WriteError, WriteErrorCode, WriteRateLimited,
	// RespondInternal and DecodeJSON answer in the API's envelope and
	// limits.
	RespondJSON(w http.ResponseWriter, status int, body any)
	WriteError(w http.ResponseWriter, status int, message string)
	WriteErrorCode(w http.ResponseWriter, status int, message, code string)
	WriteRateLimited(w http.ResponseWriter, message string, retryAfter time.Duration)
	RespondInternal(w http.ResponseWriter, r *http.Request, publicMessage string, err error)
	DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool
}
