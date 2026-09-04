package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// fakeRunnerSessions drives the transient runner handlers.
type fakeRunnerSessions struct {
	runnersessions.Service
	startErr error
	session  *runnersessions.Session
	counts   runnersessions.PoolCounts
	touched  []string
}

func (f *fakeRunnerSessions) Start(orgID, userID string, sessionMinutes, idleMinutes int) (*runnersessions.Session, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	return f.session, nil
}

func (f *fakeRunnerSessions) Get(orgID, userID string) (*runnersessions.Session, error) {
	return f.session, nil
}

func (f *fakeRunnerSessions) Counts(pool string) (runnersessions.PoolCounts, error) {
	return f.counts, nil
}

func (f *fakeRunnerSessions) Touch(sessionID string) error {
	f.touched = append(f.touched, sessionID)
	return nil
}

// memberOrgService answers the membership check the member endpoints make.
func memberOrgService() *fakeOrgService {
	return &fakeOrgService{roles: map[string]map[string]string{"org-1": {"user-1": orgs.RoleMember}}}
}

// memberRequest builds a request authenticated as an ordinary workspace
// member.
func memberRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "user-1"})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": "org-1"})
}

// An empty pool is not an error the member caused: they get 503 and a payload
// that says how the pool looks, so the UI can explain the wait.
func TestStartRunnerSessionWithEmptyPool(t *testing.T) {
	svc := &fakeRunnerSessions{startErr: runnersessions.ErrNoNodes, counts: runnersessions.PoolCounts{Total: 2, Leased: 2}}
	h := &Handler{runnerSessionService: svc, orgService: memberOrgService()}

	w := httptest.NewRecorder()
	h.StartRunnerSession(w, memberRequest(http.MethodPost, "/api/v1/orgs/org-1/runner-session"))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pool, ok := body["pool"].(map[string]interface{})
	if !ok || pool["leased"].(float64) != 2 {
		t.Errorf("payload did not report pool occupancy: %s", w.Body.String())
	}
}

// A deployment with no runner pool says so plainly rather than 500-ing.
func TestRunnerSessionDisabledDeployment(t *testing.T) {
	h := &Handler{orgService: memberOrgService()}

	w := httptest.NewRecorder()
	h.GetRunnerSession(w, memberRequest(http.MethodGet, "/api/v1/orgs/org-1/runner-session"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["enabled"] != false {
		t.Errorf("enabled = %v, want false on a deployment with no pool", body["enabled"])
	}

	w = httptest.NewRecorder()
	h.StartRunnerSession(w, memberRequest(http.MethodPost, "/api/v1/orgs/org-1/runner-session"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("start status = %d, want 400 when the feature is off", w.Code)
	}
}

// The pool endpoints take the pool key and nothing else: a member's session
// cookie, or a workspace worker key, must not be able to register a node or
// collect somebody's lease credential.
func TestPoolEndpointsRequirePoolCredentials(t *testing.T) {
	h := &Handler{runnerSessionService: &fakeRunnerSessions{}}

	for _, tc := range []struct {
		name string
		r    *http.Request
	}{
		{"member session", memberRequest(http.MethodPost, "/api/v1/runner-pool/nodes")},
		{"worker key", func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/runner-pool/nodes", strings.NewReader("{}"))
			return r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, "org-1"))
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.RegisterPoolNode(w, tc.r)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", w.Code)
			}
			w = httptest.NewRecorder()
			h.PoolNodeHeartbeat(w, tc.r)
			if w.Code != http.StatusForbidden {
				t.Errorf("heartbeat status = %d, want 403", w.Code)
			}
		})
	}
}

// fakeWorkerKeys resolves nothing, so the middleware falls through to the
// pool key.
type fakeWorkerKeys struct{ workerkeys.Service }

func (fakeWorkerKeys) Resolve(token string) (workerkeys.Resolved, error) {
	return workerkeys.Resolved{}, nil
}

// fakeRunLookup resolves no run tokens.
type fakeRunLookup struct{ agentruns.Service }

func (fakeRunLookup) GetByToken(token string) (*agentruns.Run, error) { return nil, nil }

// The pool key authenticates a pool node and grants nothing else: no
// workspace, no user, no worker identity. A wrong key is still rejected.
func TestPoolKeyGrantsOnlyPoolIdentity(t *testing.T) {
	m := &AuthMiddleware{workerService: fakeWorkerKeys{}, runService: fakeRunLookup{}}
	m.SetPoolKey("pool-secret")

	var sawPoolNode, sawWorker bool
	var sawOrg string
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPoolNode = IsPoolNode(r)
		sawWorker = IsWorker(r)
		sawOrg = ActiveOrg(r)
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/v1/runner-pool/nodes", nil)
	r.Header.Set("Authorization", "Bearer pool-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !sawPoolNode {
		t.Error("the pool key did not authenticate as a pool node")
	}
	if sawWorker || sawOrg != "" {
		t.Errorf("the pool key carried a workspace identity (worker=%v org=%q); it must carry none", sawWorker, sawOrg)
	}

	r = httptest.NewRequest(http.MethodPost, "/api/v1/runner-pool/nodes", nil)
	r.Header.Set("Authorization", "Bearer not-the-pool-key")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("a wrong pool key got status %d, want 401", w.Code)
	}
}

// The idle clock measures work, not polling: claiming a run touches the
// lease, and an ordinary worker key touches nothing.
func TestTouchRunnerSessionOnlyForLeasedCredentials(t *testing.T) {
	svc := &fakeRunnerSessions{}
	h := &Handler{runnerSessionService: svc}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/claim", nil)
	h.touchRunnerSession(r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, "org-1")))
	if len(svc.touched) != 0 {
		t.Errorf("an ordinary worker key touched leases: %v", svc.touched)
	}

	ctx := context.WithValue(r.Context(), ctxWorkerOrg, "org-1")
	ctx = context.WithValue(ctx, ctxWorkerSession, "session-1")
	h.touchRunnerSession(r.WithContext(ctx))
	if len(svc.touched) != 1 || svc.touched[0] != "session-1" {
		t.Errorf("touched = %v, want [session-1]", svc.touched)
	}
}
