package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeAgentDefService drives CreateAgent: GetBySlug answers the friendly
// pre-check, saveErr simulates the registry insert outcome.
type fakeAgentDefService struct {
	agents.Service
	bySlug  map[string]*agents.Agent
	saveErr error
	saved   []string
}

func (f *fakeAgentDefService) GetBySlug(orgID, slug string) (*agents.Agent, error) {
	return f.bySlug[slug], nil
}

func (f *fakeAgentDefService) SaveDefinition(orgID string, def *agents.Definition) (*agents.Agent, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	f.saved = append(f.saved, def.Slug)
	return &agents.Agent{ID: "agent-1", OrgID: orgID, Slug: def.Slug, Name: def.Name, Provider: def.Provider}, nil
}

// TestCreateAgentDuplicateSlug locks in that a duplicate slug answers 409
// through both detection paths: the friendly pre-check, and — when a
// concurrent create wins the race between check and insert — the registry's
// unique-index violation surfaced as agents.ErrSlugExists.
func TestCreateAgentDuplicateSlug(t *testing.T) {
	createAgent := func(t *testing.T, svc *fakeAgentDefService) *httptest.ResponseRecorder {
		t.Helper()
		h := newTestHandler(t, func(h *Handler) { h.AgentService = svc })
		body := `{"slug":"reviewer","name":"Reviewer","provider":"claude","allowed_tools":["mcp__openv__*"]}`
		r := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true})
		ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
		w := httptest.NewRecorder()
		h.CreateAgent(w, r.WithContext(ctx))
		return w
	}

	t.Run("pre-check hit answers 409", func(t *testing.T) {
		svc := &fakeAgentDefService{bySlug: map[string]*agents.Agent{
			"reviewer": {ID: "agent-0", Slug: "reviewer"},
		}}
		if w := createAgent(t, svc); w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusConflict, w.Body.String())
		}
		if len(svc.saved) != 0 {
			t.Fatal("SaveDefinition must not be called when the pre-check hits")
		}
	})

	t.Run("lost race surfaces the unique violation as 409", func(t *testing.T) {
		svc := &fakeAgentDefService{bySlug: map[string]*agents.Agent{}, saveErr: agents.ErrSlugExists}
		if w := createAgent(t, svc); w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusConflict, w.Body.String())
		}
	})

	t.Run("other save errors stay 400", func(t *testing.T) {
		svc := &fakeAgentDefService{bySlug: map[string]*agents.Agent{}, saveErr: errors.New("disk full")}
		if w := createAgent(t, svc); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("new slug is created", func(t *testing.T) {
		svc := &fakeAgentDefService{bySlug: map[string]*agents.Agent{}}
		if w := createAgent(t, svc); w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusCreated, w.Body.String())
		}
		if len(svc.saved) != 1 || svc.saved[0] != "reviewer" {
			t.Fatalf("saved = %v, want the reviewer definition", svc.saved)
		}
	})
}

// REQ-91: an agent definition with no tool allowlist is refused, on create and
// on update alike, with the message that says what to add. Nothing reaches the
// store — an agent with no allowlist is one whose vendor CLI would run with
// every tool it has.
func TestAgentDefinitionRequiresAllowedTools(t *testing.T) {
	bodies := []string{
		`{"slug":"reviewer","name":"Reviewer","provider":"claude-code"}`,
		`{"slug":"reviewer","name":"Reviewer","provider":"claude-code","allowed_tools":[]}`,
		`{"slug":"reviewer","name":"Reviewer","provider":"claude-code","allowed_tools":["  "]}`,
	}
	call := func(t *testing.T, method, body string) (*httptest.ResponseRecorder, *fakeAgentDefService) {
		t.Helper()
		svc := &fakeAgentDefService{bySlug: map[string]*agents.Agent{}}
		h := newTestHandler(t, func(h *Handler) { h.AgentService = svc })
		r := httptest.NewRequest(method, "/api/v1/agents/reviewer", strings.NewReader(body))
		r = mux.SetURLVars(r, map[string]string{"slug": "reviewer"})
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true})
		ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
		w := httptest.NewRecorder()
		if method == http.MethodPost {
			h.CreateAgent(w, r.WithContext(ctx))
		} else {
			h.UpdateAgent(w, r.WithContext(ctx))
		}
		return w, svc
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for _, body := range bodies {
			w, svc := call(t, method, body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s %s: status = %d, want 400 (body %q)", method, body, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "allowed_tools") {
				t.Errorf("%s %s: error should name allowed_tools, got %q", method, body, w.Body.String())
			}
			if len(svc.saved) != 0 {
				t.Errorf("%s %s: definition reached the store anyway", method, body)
			}
		}
	}

	// The same definition with an allowlist is accepted, so the rule is about
	// the tools and nothing else.
	ok := `{"slug":"reviewer","name":"Reviewer","provider":"claude-code","allowed_tools":["mcp__openv__*"]}`
	if w, svc := call(t, http.MethodPost, ok); w.Code != http.StatusCreated || len(svc.saved) != 1 {
		t.Errorf("a definition with an allowlist was refused: status %d body %q", w.Code, w.Body.String())
	}
}

// #379 bug 186: an allowed_tools entry holding a comma is refused on create,
// on update and on a raw file save, with a 400 that says to list each tool on
// its own, and nothing is stored. claude-code receives the list joined with
// commas, so such an entry is several tools there.
func TestAgentDefinitionRefusesACommaInAToolEntry(t *testing.T) {
	const body = `{"slug":"reviewer","name":"Reviewer","provider":"claude-code","allowed_tools":["mcp__openv__*","mcp__openv__get_artifact,Bash"]}`
	wantSaid := func(t *testing.T, what string, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %q)", what, w.Code, w.Body.String())
		}
		for _, want := range []string{"allowed_tools", "mcp__openv__get_artifact,Bash", "comma", "one tool per entry"} {
			if !strings.Contains(w.Body.String(), want) {
				t.Errorf("%s: error %q should say %q", what, w.Body.String(), want)
			}
		}
	}

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		svc := &fakeAgentDefService{bySlug: map[string]*agents.Agent{}}
		h := newTestHandler(t, func(h *Handler) { h.AgentService = svc })
		w := httptest.NewRecorder()
		r := agentReq(method, "/api/v1/agents/reviewer", map[string]string{"slug": "reviewer"}, body)
		if method == http.MethodPost {
			h.CreateAgent(w, r)
		} else {
			h.UpdateAgent(w, r)
		}
		wantSaid(t, method, w)
		if len(svc.saved) != 0 {
			t.Errorf("%s: the definition reached the store anyway", method)
		}
	}

	// The raw markdown editor, through the real file store: refused before
	// the file is written.
	store := &recordingAgentStore{}
	svc, err := agents.NewFileService(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, func(h *Handler) { h.AgentService = svc })
	content := "---\nslug: reviewer\nname: Reviewer\nprovider: claude-code\nallowed_tools:\n  - mcp__openv__*\n  - \"mcp__openv__get_artifact,Bash\"\n---\nReview.\n"
	raw, _ := json.Marshal(map[string]string{"content": content})
	w := httptest.NewRecorder()
	h.SaveAgentRaw(w, agentReq(http.MethodPut, "/api/v1/agents/reviewer/raw", map[string]string{"slug": "reviewer"}, string(raw)))
	wantSaid(t, "raw save", w)
	if _, err := svc.RawFile("org-1", "reviewer"); err == nil {
		t.Error("raw save: the file was written anyway")
	}
	if len(store.saved) != 0 {
		t.Errorf("raw save: %v reached the registry anyway", store.saved)
	}
}

// recordingAgentStore is an agent registry with no agents that records what a
// save writes to it.
type recordingAgentStore struct {
	emptyAgentStore
	saved []string
}

func (s *recordingAgentStore) Save(a *agents.Agent) error {
	s.saved = append(s.saved, a.Slug)
	return nil
}

func (s *recordingAgentStore) Update(a *agents.Agent) error { return s.Save(a) }

// TestWorkerLifecycleErrorContract locks in the 409-vs-500 split on the
// worker lifecycle endpoints: a status-transition conflict is the worker's
// problem (409, sentinel text preserved), while any other failure — e.g. a DB
// blip — must answer 5xx so the worker retries instead of abandoning the run,
// and must not leak internal error text.
func TestWorkerLifecycleErrorContract(t *testing.T) {
	const internalDetail = "pq: connection reset by peer"
	transitionErr := fmt.Errorf("%w: run already succeeded", agentruns.ErrInvalidTransition)

	newRunSvc := func(startErr, finishErr error) *fakeRunService {
		return &fakeRunService{
			byID: map[string]*agentruns.Run{
				"run-1": {ID: "run-1", OrgID: "org-1", Status: agentruns.StatusClaimed},
			},
			startErr:  startErr,
			finishErr: finishErr,
		}
	}

	endpoints := []struct {
		name string
		call func(h *Handler, w http.ResponseWriter, r *http.Request)
		body string
		svc  func(err error) *fakeRunService
	}{
		{"start", (*Handler).StartAgentRun, "", func(err error) *fakeRunService { return newRunSvc(err, nil) }},
		{"finish", (*Handler).FinishAgentRun, `{"status":"succeeded"}`, func(err error) *fakeRunService { return newRunSvc(nil, err) }},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+"/invalid transition answers 409 with sentinel", func(t *testing.T) {
			h := newTestHandler(t, func(h *Handler) { h.RunService = ep.svc(transitionErr) })
			w := httptest.NewRecorder()
			ep.call(h, w, workerRunReq(ep.body, "org-1", "", "run-1"))
			if w.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body %q)", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), agentruns.ErrInvalidTransition.Error()) {
				t.Fatalf("409 body %q should carry the sentinel text", w.Body.String())
			}
		})

		t.Run(ep.name+"/internal error answers 500 without leaking", func(t *testing.T) {
			h := newTestHandler(t, func(h *Handler) { h.RunService = ep.svc(errors.New(internalDetail)) })
			w := httptest.NewRecorder()
			ep.call(h, w, workerRunReq(ep.body, "org-1", "", "run-1"))
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), internalDetail) {
				t.Fatalf("500 body %q leaks the internal error text", w.Body.String())
			}
		})
	}
}

// retryReq builds an authenticated retry request against a run id.
func retryReq(userID, runID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/"+runID+"/retry", nil)
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": runID})
}

// TestRetryAgentRunAuthz locks in that retrying mirrors the launch/cancel
// access ladder: the original launcher always passes, project-scoped runs
// need project editor rights, unscoped runs need workspace admin rights —
// and a denied request never reaches the service. A caller who may not
// read the run gets its 404, as for a run no row has.
func TestRetryAgentRunAuthz(t *testing.T) {
	const orgID = "org-1"
	launcher := "launcher"
	project := "proj-1"

	newFixture := func() (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{
			byID: map[string]*agentruns.Run{
				"run-project":  {ID: "run-project", OrgID: orgID, AgentID: "agent-1", ProjectID: &project, Status: agentruns.StatusFailed, LaunchedBy: &launcher},
				"run-unscoped": {ID: "run-unscoped", OrgID: orgID, AgentID: "agent-1", Status: agentruns.StatusFailed, LaunchedBy: &launcher},
			},
			retryRun: &agentruns.Run{ID: "run-new", OrgID: orgID, Status: agentruns.StatusQueued},
		}
		h := newTestHandler(t, func(h *Handler) {
			h.RunService = runSvc
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
				project: {ID: project, OrgID: orgID},
			}}
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"org-admin": orgs.RoleAdmin, "org-member": orgs.RoleMember, "editor": orgs.RoleMember, "viewer": orgs.RoleMember, launcher: orgs.RoleMember},
			}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
				project: {"editor": members.RoleEditor, "viewer": members.RoleViewer},
			}}
		})
		return h, runSvc
	}

	cases := []struct {
		name     string
		userID   string
		runID    string
		wantCode int // 0 = created
	}{
		{"launcher retries own unscoped run", launcher, "run-unscoped", 0},
		{"project editor retries another member's run", "editor", "run-project", 0},
		{"project viewer is denied", "viewer", "run-project", http.StatusForbidden},
		// Nor may they read it, so it is to them one no row has (#379 bug 172).
		{"plain member cannot retry another member's unscoped run", "org-member", "run-unscoped", http.StatusNotFound},
		{"org admin retries an unscoped run", "org-admin", "run-unscoped", 0},
		{"unknown run answers 404", "org-admin", "run-missing", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, runSvc := newFixture()
			w := httptest.NewRecorder()
			h.RetryAgentRun(w, retryReq(tc.userID, tc.runID))
			if tc.wantCode == 0 {
				if w.Code != http.StatusCreated {
					t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
				}
				if len(runSvc.retryCalls) != 1 || runSvc.retryCalls[0] != tc.runID {
					t.Fatalf("retryCalls = %v, want [%s]", runSvc.retryCalls, tc.runID)
				}
				if runSvc.retryUsers[0] != tc.userID {
					t.Fatalf("launchedBy = %q, want the retrying user %q (personal-runner preference)", runSvc.retryUsers[0], tc.userID)
				}
				var got agentruns.Run
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.ID != "run-new" {
					t.Fatalf("body = %q (err %v), want the new run", w.Body.String(), err)
				}
				return
			}
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if len(runSvc.retryCalls) != 0 {
				t.Fatalf("service called %d times on denied request, want 0", len(runSvc.retryCalls))
			}
		})
	}
}

// TestRetryAgentRunErrorContract locks in the 409-vs-500 split: a
// non-retryable status conflict answers 409 carrying the sentinel text,
// while any other failure answers 500 without leaking internals.
func TestRetryAgentRunErrorContract(t *testing.T) {
	launcher := "launcher"
	newHandler := func(retryErr error) (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{
			byID: map[string]*agentruns.Run{
				"run-1": {ID: "run-1", OrgID: "org-1", Status: agentruns.StatusRunning, LaunchedBy: &launcher},
			},
			retryErr: retryErr,
		}
		return newTestHandler(t, func(h *Handler) { h.RunService = runSvc }), runSvc
	}

	t.Run("not retryable answers 409 with sentinel", func(t *testing.T) {
		h, _ := newHandler(fmt.Errorf("%w: run is running", agentruns.ErrNotRetryable))
		w := httptest.NewRecorder()
		h.RetryAgentRun(w, retryReq(launcher, "run-1"))
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body %q)", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), agentruns.ErrNotRetryable.Error()) {
			t.Fatalf("409 body %q should carry the sentinel text", w.Body.String())
		}
	})

	t.Run("internal error answers 500 without leaking", func(t *testing.T) {
		const internalDetail = "pq: connection reset by peer"
		h, _ := newHandler(errors.New(internalDetail))
		w := httptest.NewRecorder()
		h.RetryAgentRun(w, retryReq(launcher, "run-1"))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), internalDetail) {
			t.Fatalf("500 body %q leaks the internal error text", w.Body.String())
		}
	})

	t.Run("unauthenticated answers 401", func(t *testing.T) {
		h, runSvc := newHandler(nil)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/run-1/retry", nil)
		h.RetryAgentRun(w, mux.SetURLVars(r, map[string]string{"id": "run-1"}))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
		}
		if len(runSvc.retryCalls) != 0 {
			t.Fatal("service must not be called without a user")
		}
	})
}
