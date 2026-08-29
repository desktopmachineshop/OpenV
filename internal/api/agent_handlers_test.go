package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
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
		h := &Handler{agentService: svc}
		body := `{"slug":"reviewer","name":"Reviewer","provider":"claude"}`
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
			h := &Handler{runService: ep.svc(transitionErr)}
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
			h := &Handler{runService: ep.svc(errors.New(internalDetail))}
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
