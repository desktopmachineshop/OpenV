package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
