package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// The plan endpoint (REQ-154): a platform admin moves a workspace to
// another plan; a workspace admin, a member and a visitor are refused; an
// unknown plan is 400 and writes nothing; a grant over a live subscription
// is 409 already_subscribed, the checkout's answer for the same state, and
// writes nothing (REQ-168).

type planOrgFake struct {
	fakeOrgService
	setPlans []string
}

func (f *planOrgFake) SetPlan(id, plan string) (*orgs.Org, error) {
	if !orgs.ValidPlan(plan) {
		return nil, orgs.ErrInvalidPlan
	}
	if id == "missing" {
		return nil, orgs.ErrNotFound
	}
	if id == "subscribed" && orgs.GrantedPlan(plan) {
		return nil, orgs.ErrBillingActive
	}
	f.setPlans = append(f.setPlans, plan)
	f.plan = plan
	return f.Get(id)
}

func planReq(orgID, body string, user *users.User) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/"+orgID+"/plan", strings.NewReader(body))
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

func TestSetOrgPlan(t *testing.T) {
	svc := &planOrgFake{fakeOrgService: fakeOrgService{
		plan:  orgs.PlanBusiness,
		roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}},
	}}
	h := newTestHandler(t, func(h *Handler) { h.OrgService = svc })
	root := &users.User{ID: "root", IsAdmin: true}

	w := httptest.NewRecorder()
	h.SetOrgPlan(w, planReq("org-1", `{"plan":"open_source"}`, root))
	if w.Code != http.StatusOK {
		t.Fatalf("platform admin: status = %d (body %q)", w.Code, w.Body.String())
	}
	var o orgs.Org
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.BilledPlan != orgs.PlanOpenSource || len(svc.setPlans) != 1 {
		t.Fatalf("answer = %+v, writes = %v", o, svc.setPlans)
	}

	for name, user := range map[string]*users.User{
		"workspace admin": {ID: "admin"},
		"member":          {ID: "member"},
	} {
		w = httptest.NewRecorder()
		h.SetOrgPlan(w, planReq("org-1", `{"plan":"enterprise"}`, user))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d", name, w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.SetOrgPlan(w, planReq("org-1", `{"plan":"enterprise"}`, nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("visitor: status = %d", w.Code)
	}

	w = httptest.NewRecorder()
	h.SetOrgPlan(w, planReq("org-1", `{"plan":"platinum"}`, root))
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown plan: status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.SetOrgPlan(w, planReq("missing", `{"plan":"single"}`, root))
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown workspace: status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.SetOrgPlan(w, planReq("subscribed", `{"plan":"open_source"}`, root))
	var refusal errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if w.Code != http.StatusConflict || refusal.Code != ErrCodeAlreadySubscribed || refusal.Error != orgs.ErrBillingActive.Error() {
		t.Errorf("a grant over a live subscription: status = %d (body %q)", w.Code, w.Body.String())
	}
	if len(svc.setPlans) != 1 {
		t.Errorf("refused calls wrote plans: %v", svc.setPlans)
	}
}
