# Extending OpenV with modules

OpenV's server can take **modules**: Go packages that add HTTP routes,
watch workspace changes and tell the app what they offer, without changing
the core. The hosted service at openv.app adds billing this way, and so can
anyone running their own OpenV. The API is the package
[`ext`](../ext/ext.go); this page is how to use it. The plan behind it is
[`docs/plans/hosted-edition-separation.md`](plans/hosted-edition-separation.md).

## What a module can do

A module is any value with a `Name() string`. What it does is the
interfaces it also implements:

| Interface | What the server does with it |
|---|---|
| `ext.RouteModule` | calls `Routes(r)` once; each `r.Handle(method, path, handler, opts...)` becomes a route |
| `ext.HostAware` | calls `SetHost(h)` first, handing it the `ext.Host`: the caller, path variables, feature gates, workspaces, seat counts, rate limiters, and the API's own way of answering (`RespondJSON`, `WriteErrorCode`, ...) |
| `ext.WorkspaceObserver` | tells it, after the change is committed, that a workspace's seat count may have moved, or that a workspace was deleted or restored |
| `ext.CapabilityModule` | adds the names it returns to `capabilities` in `GET /api/v1/auth/config`, which the app reads to show or hide screens |
| `ext.PolicyModule` | installs the `ext.PlanPolicy` (each plan's default limits) and `ext.ChannelPolicy` (each plan's release channel) it returns in place of the built-in ones, before the server serves; a nil one leaves the built-in policy. Two modules setting the same policy stop the boot |

### Plans and channels

A plan's default limits are the bottom layer of a workspace's limits, under
the deployment's `OPENV_LIMITS` and the workspace's own. The built-in
policies (`ext.BuiltinPlans`, `ext.BuiltinChannels`) are openv.app's tiers
today; a policy of your own can embed them and change only what it means
to:

```go
type plans struct{ ext.BuiltinPlans }

// Every plan gets two-hour runner sessions.
func (p plans) PlanDefaults(plan string, tiers bool) map[string]any {
	m := p.BuiltinPlans.PlanDefaults(plan, tiers)
	m[ext.LimitRunnerSessionMinutes] = 120
	return m
}
```

On a self-hosted deployment every workspace is on the `self_host` plan, so
that is the plan your policy answers for there.

### Guards are declared, not coded

A route's access rule is an option of its registration, and the server runs
it before the handler with the core's own guard, so the answers (401, 403,
404, the read-only plan gate on a write) are exactly the core's:

```go
func (m *Module) Routes(r ext.Router) {
	admin := ext.RequireWorkspaceRole("id", ext.RoleAdmin)
	r.Handle("GET", "/api/v1/public/plans", m.GetPublicPlans)
	r.Handle("GET", "/api/v1/orgs/{id}/billing", m.GetOrgBilling, admin)
	r.Handle("POST", "/api/v1/orgs/{id}/billing/portal", m.OpenPortal, ext.AlwaysWritable(), admin)
}
```

`ext.AlwaysWritable()` keeps a write open on a workspace that holds more
than its plan allows, as the core does for export and member removal.

## Plugging a module in

Modules a program wants built at boot are registered by name, with a
factory the server calls at stage `billing`, before the API handler exists:

```go
package mymodule

import "github.com/openv/requirements-platform/ext"

func init() {
	ext.Register("mymodule", func(b ext.Boot) (ext.Module, error) {
		// b.Context() ends at shutdown; b.Workspaces() and b.Users() are
		// the core's services.
		return New(b.Workspaces()), nil
	})
}
```

A factory's error stops the server, as a malformed setting of the core's
own does. Registering two modules under one name panics.

The server is `cmd/server`, a `main` package, so a program links its
modules into it by adding one file to that directory at build time, which
imports the module packages for their registration:

```go
// cmd/server/zz_modules.go, added by your build, not committed here.
package main

import _ "example.com/yourcompany/openv-modules/mymodule"
```

With OpenV checked out beside your module (a git submodule, say) and a
`go.work` naming both, `go build ./cmd/server` from the OpenV checkout
builds a server with your modules. This is how the hosted service builds
openv.app's server.

A registered module named `billing` replaces the billing built into this
repository, which then starts nothing; that is how openv.app moves its
billing out of this repository.

## Building a module outside this repository

Another Go module cannot import this one's `internal/` packages, so a
module may use only `ext` from OpenV. `ext` re-exports what a module needs
of the core, as aliases of the core's own types (`ext.Workspace`,
`ext.WorkspaceBilling`, the plan and status constants,
`ext.ErrWorkspaceNotFound`) and the environment's parse rules
(`ext.EnvWholeNumber`, `ext.EnvSecret`). The packages that already leave
this repository are held to that by `TestExtModulesImportOnlyExt`
(`internal/archtest/ext_modules_test.go`).

`ext` changes with OpenV's releases; a change that breaks a module is named
in the release notes.
