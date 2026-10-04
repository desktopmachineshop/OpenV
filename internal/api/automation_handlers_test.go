package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeAutomationService records the request CreateAutomation hands the
// service and answers with the automation the real service would store.
type fakeAutomationService struct {
	automations.Service
	created []automations.CreateAutomationRequest
}

func (f *fakeAutomationService) Create(req automations.CreateAutomationRequest) (*automations.Automation, error) {
	f.created = append(f.created, req)
	return &automations.Automation{ID: "auto-1", OrgID: req.OrgID, Name: req.Name, AgentID: req.AgentID,
		Kind: req.Kind, CreatedBy: req.CreatedBy}, nil
}

// TestCreateAutomationStampsTheCaller is the regression test for an
// automation whose created_by was whatever the request body named: the
// field decoded straight from the body and the handler never set it, so a
// workspace admin could record an automation as made by any account. The
// server now stamps the authenticated caller, as it stamps the workspace,
// and a created_by in the body is ignored.
func TestCreateAutomationStampsTheCaller(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"a body naming another account", `{"name":"Nightly","kind":"manual","agent_id":"agent-1","created_by":"mallory"}`},
		{"a body naming no one", `{"name":"Nightly","kind":"manual","agent_id":"agent-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeAutomationService{}
			h := newTestHandler(t, func(h *Handler) { h.AutomationService = svc })
			r := httptest.NewRequest(http.MethodPost, "/api/v1/automations", strings.NewReader(tc.body))
			ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "alice", IsAdmin: true})
			r = r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-1"))
			w := httptest.NewRecorder()
			h.CreateAutomation(w, r)

			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
			}
			if len(svc.created) != 1 {
				t.Fatalf("service saw %d creates, want 1", len(svc.created))
			}
			req := svc.created[0]
			if req.CreatedBy == nil || *req.CreatedBy != "alice" {
				got := "none"
				if req.CreatedBy != nil {
					got = *req.CreatedBy
				}
				t.Fatalf("created_by handed to the service = %s, want the caller alice", got)
			}
			if req.OrgID != "org-1" {
				t.Fatalf("org_id handed to the service = %q, want the active workspace org-1", req.OrgID)
			}
			var got automations.Automation
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("answer %q: %v", w.Body.String(), err)
			}
			if got.CreatedBy == nil || *got.CreatedBy != "alice" {
				t.Fatalf("answer %s: created_by is not the caller alice", w.Body.String())
			}
		})
	}
}
