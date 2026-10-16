package api

import (
	"context"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/ext"
)

// Modules (ext, docs/plans/hosted-edition-separation.md): the parts of the
// product that plug in rather than live in this package, such as billing on
// the hosted service. A module declares its routes with their guards, and
// the Handler registers them at registerModuleRoutes's place in
// RegisterRoutes, runs each declared guard with the core's own require*
// helper, and tells observers about workspace changes. The Handler is the
// modules' Host, through moduleHost.

// moduleRoute is one route a module declared, as registered: what the route
// goldens record for it (route_binding_test.go reads these rather than the
// module's source, which is not this package's).
type moduleRoute struct {
	module   string
	method   string
	path     string
	handler  string   // the module's method name, as handlerIdentity names a core one
	wrappers []string // alwaysWritable, as the core's own wrapper reads in the goldens
	guards   []string // the canonical guard kinds the declared guards are
	run      http.HandlerFunc
	opts     []ext.RouteOption
}

// moduleRoutes collects what the modules declare, in declaration order.
type moduleRoutes struct {
	module string
	routes []moduleRoute
}

func (c *moduleRoutes) Handle(method, path string, handler http.HandlerFunc, opts ...ext.RouteOption) {
	c.routes = append(c.routes, moduleRoute{
		module:  c.module,
		method:  strings.ToUpper(method),
		path:    path,
		handler: moduleHandlerName(handler),
		run:     handler,
	})
	r := &c.routes[len(c.routes)-1]
	for _, o := range opts {
		switch o.Kind() {
		case ext.OptionAlwaysWritable:
			r.wrappers = append(r.wrappers, "alwaysWritable")
		case ext.OptionRequireWorkspaceRole:
			r.guards = append(r.guards, "org:"+o.Role())
		}
	}
	r.opts = opts
}

// moduleHandlerName is a module handler's method name: "GetOrgBilling" for
// the method value (*Module).GetOrgBilling.
func moduleHandlerName(handler http.HandlerFunc) string {
	name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	name = strings.TrimSuffix(name, "-fm")
	return name[strings.LastIndex(name, ".")+1:]
}

// declaredModuleRoutes asks every route module for its routes, once.
func (h *Handler) declaredModuleRoutes() []moduleRoute {
	if h.moduleRouteList != nil {
		return h.moduleRouteList
	}
	h.moduleRouteList = []moduleRoute{}
	for _, m := range h.Modules {
		rm, ok := m.(ext.RouteModule)
		if !ok {
			continue
		}
		c := &moduleRoutes{module: m.Name()}
		rm.Routes(c)
		h.moduleRouteList = append(h.moduleRouteList, c.routes...)
	}
	return h.moduleRouteList
}

// registerModuleRoutes registers the modules' routes, each wrapped in the
// guards it declared: alwaysWritable outermost, as the core writes
// h.alwaysWritable(h.Handler), then the workspace role guard, which is the
// first thing a core handler of that shape does.
func (h *Handler) registerModuleRoutes(router *mux.Router) {
	for _, mr := range h.declaredModuleRoutes() {
		router.HandleFunc(mr.path, h.moduleHandler(mr)).Methods(mr.method)
	}
}

func (h *Handler) moduleHandler(mr moduleRoute) http.HandlerFunc {
	next := mr.run
	for i := len(mr.opts) - 1; i >= 0; i-- {
		if o := mr.opts[i]; o.Kind() == ext.OptionRequireWorkspaceRole {
			next = h.guardModuleRoute(o.PathVar(), o.Role(), next)
		}
	}
	for _, o := range mr.opts {
		if o.Kind() == ext.OptionAlwaysWritable {
			next = h.alwaysWritable(next)
			break
		}
	}
	return next
}

func (h *Handler) guardModuleRoute(pathVar, role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireOrgRole(w, r, mux.Vars(r)[pathVar], role) {
			return
		}
		next(w, r)
	}
}

// workspaceObservers are the modules that watch workspace changes.
func (h *Handler) workspaceObservers() []ext.WorkspaceObserver {
	var out []ext.WorkspaceObserver
	for _, m := range h.Modules {
		if o, ok := m.(ext.WorkspaceObserver); ok {
			out = append(out, o)
		}
	}
	return out
}

// seatsChanged tells the observers a workspace's seat count may have moved,
// after the local write committed. Never inline and never a failure the
// caller sees: a membership change must not wait on, or fail because of,
// a module's third party. Accepting an invitation is not a change — the
// invitation was already a seat — so acceptance does not call this.
func (h *Handler) seatsChanged(orgID string) {
	for _, o := range h.workspaceObservers() {
		o.SeatsChanged(orgID)
	}
}

func (h *Handler) workspaceDeleted(ctx context.Context, org *ext.Workspace) {
	for _, o := range h.workspaceObservers() {
		o.WorkspaceDeleted(ctx, org)
	}
}

func (h *Handler) workspaceRestored(ctx context.Context, org *ext.Workspace) {
	for _, o := range h.workspaceObservers() {
		o.WorkspaceRestored(ctx, org)
	}
}

// moduleHost is the Handler as the modules' Host.
type moduleHost struct{ h *Handler }

var _ ext.Host = moduleHost{}

func (m moduleHost) CurrentUser(r *http.Request) *ext.User { return CurrentUser(r) }
func (m moduleHost) PathVar(r *http.Request, name string) string {
	return mux.Vars(r)[name]
}
func (m moduleHost) FeatureEnabled(r *http.Request, orgID, key string) bool {
	return m.h.featureEnabled(r, orgID, key)
}
func (m moduleHost) Workspaces() ext.WorkspaceService     { return m.h.OrgService }
func (m moduleHost) CountSeats(orgID string) (int, error) { return m.h.countOrgSeats(orgID) }
func (m moduleHost) FrontendURL() string                  { return m.h.frontendURL }
func (m moduleHost) NewLimiter(burst int, refill float64) ext.Limiter {
	return limiterAdapter{newRateLimiter(burst, refill)}
}
func (m moduleHost) WriteFeatureGated(w http.ResponseWriter) {
	writeJSONError(w, http.StatusForbidden, featureGateMessage)
}
func (m moduleHost) RespondJSON(w http.ResponseWriter, status int, body any) {
	respondJSON(w, status, body)
}
func (m moduleHost) WriteError(w http.ResponseWriter, status int, message string) {
	writeJSONError(w, status, message)
}
func (m moduleHost) WriteErrorCode(w http.ResponseWriter, status int, message, code string) {
	writeJSONErrorCode(w, status, message, code)
}
func (m moduleHost) WriteRateLimited(w http.ResponseWriter, message string, retryAfter time.Duration) {
	writeRateLimited(w, message, retryAfter)
}
func (m moduleHost) RespondInternal(w http.ResponseWriter, r *http.Request, publicMessage string, err error) {
	respondInternal(w, r, publicMessage, err)
}
func (m moduleHost) DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSON(w, r, v)
}

type limiterAdapter struct{ l *rateLimiter }

func (a limiterAdapter) Allow(key string) (bool, time.Duration) { return a.l.allow(key) }
