package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

type contextKey string

const (
	ctxUser       contextKey = "openv-user"
	ctxRun        contextKey = "openv-run"
	ctxWorkerOrg  contextKey = "openv-worker-org"
	ctxWorkerUser contextKey = "openv-worker-user"
	ctxActiveOrg  contextKey = "openv-active-org"
)

// SessionCookieName is the browser session cookie.
const SessionCookieName = "openv_session"

// OrgHeader carries the caller's active workspace.
const OrgHeader = "X-Org-ID"

// AuthMiddleware authenticates every API request as one of: a human user
// (session cookie, with an active-org context), an agent run (Bearer run
// token), or an org-scoped host worker (Bearer worker key). Auth routes,
// public interview routes, and /health stay open.
type AuthMiddleware struct {
	userService   users.Service
	runService    agentruns.Service
	orgService    orgs.Service
	workerService workerkeys.Service
	// legacyWorkerKey keeps a WORKER_API_KEY-only deployment working until
	// the boot process registers it as an org key (resolved by hash first).
	legacyWorkerKey string
	legacyOrgID     func() string
}

// NewAuthMiddleware creates the middleware. legacyOrgID lazily resolves the
// bootstrap org for a raw env worker key (may return "" when none exists yet).
func NewAuthMiddleware(userService users.Service, runService agentruns.Service, orgService orgs.Service, workerService workerkeys.Service, legacyWorkerKey string, legacyOrgID func() string) *AuthMiddleware {
	return &AuthMiddleware{
		userService:     userService,
		runService:      runService,
		orgService:      orgService,
		workerService:   workerService,
		legacyWorkerKey: legacyWorkerKey,
		legacyOrgID:     legacyOrgID,
	}
}

func isOpenPath(path string) bool {
	if path == "/health" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/auth/") {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/public/") {
		return true
	}
	return false
}

// Wrap returns the middleware handler.
func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || isOpenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Bearer credentials: org worker key or agent run token.
		authz := r.Header.Get("Authorization")
		if strings.HasPrefix(authz, "Bearer ") {
			token := strings.TrimPrefix(authz, "Bearer ")

			if resolved, err := m.workerService.Resolve(token); err == nil && resolved.OrgID != "" {
				ctx := context.WithValue(r.Context(), ctxWorkerOrg, resolved.OrgID)
				if resolved.UserID != "" {
					ctx = context.WithValue(ctx, ctxWorkerUser, resolved.UserID)
				}
				annotateRequestLog(ctx, resolved.OrgID, resolved.UserID, "worker")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// Legacy env key: resolve to the bootstrap org.
			if m.legacyWorkerKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(m.legacyWorkerKey)) == 1 {
				if orgID := m.legacyOrgID(); orgID != "" {
					ctx := context.WithValue(r.Context(), ctxWorkerOrg, orgID)
					annotateRequestLog(ctx, orgID, "", "worker")
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			if run, err := m.runService.GetByToken(token); err == nil && run != nil {
				ctx := context.WithValue(r.Context(), ctxRun, run)
				annotateRequestLog(ctx, run.OrgID, "", "run")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		// Session cookie.
		if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
			if user, err := m.userService.GetBySessionToken(cookie.Value); err == nil && user != nil {
				activeOrg := m.resolveActiveOrg(r, cookie.Value, user)
				ctx := context.WithValue(r.Context(), ctxUser, user)
				ctx = context.WithValue(ctx, ctxActiveOrg, activeOrg)
				annotateRequestLog(ctx, activeOrg, user.ID, "user")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}

// annotateRequestLog records the resolved identity on the request-log meta
// holder (a no-op when the logging middleware isn't wired, e.g. in tests).
func annotateRequestLog(ctx context.Context, orgID, userID, actor string) {
	if meta := metaFrom(ctx); meta != nil {
		meta.orgID = orgID
		meta.userID = userID
		meta.actor = actor
	}
}

// resolveActiveOrg picks the request's workspace: validated X-Org-ID header,
// else the session's stored default, else the user's personal org.
func (m *AuthMiddleware) resolveActiveOrg(r *http.Request, sessionToken string, user *users.User) string {
	if header := strings.TrimSpace(r.Header.Get(OrgHeader)); header != "" {
		if ok, err := m.orgService.IsMember(header, user.ID); err == nil && ok {
			return header
		}
		// Invalid header falls through to the stored/personal default so a
		// stale tab degrades instead of hard-failing every request.
	}
	if session, err := m.userService.SessionByToken(sessionToken); err == nil && session.ActiveOrgID != "" {
		if ok, err := m.orgService.IsMember(session.ActiveOrgID, user.ID); err == nil && ok {
			return session.ActiveOrgID
		}
	}
	if personal, _, err := m.orgService.EnsurePersonalOrg(user.ID, user.Name); err == nil {
		return personal.ID
	}
	return ""
}

// CurrentUser returns the authenticated user, or nil.
func CurrentUser(r *http.Request) *users.User {
	if u, ok := r.Context().Value(ctxUser).(*users.User); ok {
		return u
	}
	return nil
}

// CurrentRun returns the authenticated agent run, or nil.
func CurrentRun(r *http.Request) *agentruns.Run {
	if run, ok := r.Context().Value(ctxRun).(*agentruns.Run); ok {
		return run
	}
	return nil
}

// WorkerOrg returns the org a worker credential belongs to ("" when the
// request is not from a worker).
func WorkerOrg(r *http.Request) string {
	if v, ok := r.Context().Value(ctxWorkerOrg).(string); ok {
		return v
	}
	return ""
}

// WorkerUser returns the user a personal runner key belongs to ("" for
// workspace keys or non-worker requests).
func WorkerUser(r *http.Request) string {
	if v, ok := r.Context().Value(ctxWorkerUser).(string); ok {
		return v
	}
	return ""
}

// IsWorker reports whether the request authenticated with a worker key.
func IsWorker(r *http.Request) bool {
	return WorkerOrg(r) != ""
}

// ActiveOrg returns the request's workspace context: the user's resolved
// active org, the worker's org, or the run's org.
func ActiveOrg(r *http.Request) string {
	if v, ok := r.Context().Value(ctxActiveOrg).(string); ok && v != "" {
		return v
	}
	if org := WorkerOrg(r); org != "" {
		return org
	}
	if run := CurrentRun(r); run != nil && run.OrgID != "" {
		return run.OrgID
	}
	return ""
}

// Actor returns the request's actor string for events/attribution.
func Actor(r *http.Request) string {
	if u := CurrentUser(r); u != nil {
		return "user:" + u.ID
	}
	if run := CurrentRun(r); run != nil {
		return "agent:" + run.ID
	}
	if org := WorkerOrg(r); org != "" {
		if user := WorkerUser(r); user != "" {
			return "worker:" + org + ":user:" + user
		}
		return "worker:" + org
	}
	return "system"
}

// CurrentUserID returns the authenticated user's id pointer, or nil.
func CurrentUserID(r *http.Request) *string {
	if u := CurrentUser(r); u != nil {
		id := u.ID
		return &id
	}
	return nil
}
