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
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeSharedProductService records what reached the domain layer.
type fakeSharedProductService struct {
	published  []sharedproducts.Product
	orgs       []string
	userIDs    []string
	reported   []string
	deleted    []string
	publishErr error
}

func (f *fakeSharedProductService) List(limit int) ([]*sharedproducts.Product, error) {
	return []*sharedproducts.Product{{
		ID: "p1", Category: "kitchen appliance", Name: "Kevinproof",
		Description: "A coffee tin that recognises Kevin and locks.",
		Vision:      "Kevinproof becomes the reason the bean jar survives.",
		Problem:     "Beans vanish overnight.",
		TargetUsers: "office workers whose beans keep leaving with Kevin",
		// Author metadata is set but must never be serialized.
		CreatedByOrg: "org-secret", CreatedByUser: "user-secret",
	}}, nil
}

func (f *fakeSharedProductService) Publish(in sharedproducts.Product, orgID, userID string) (*sharedproducts.Product, error) {
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	f.published = append(f.published, in)
	f.orgs = append(f.orgs, orgID)
	f.userIDs = append(f.userIDs, userID)
	out := in
	out.ID = "new"
	return &out, nil
}

func (f *fakeSharedProductService) Report(id, userID string) error {
	f.reported = append(f.reported, id+"/"+userID)
	return nil
}
func (f *fakeSharedProductService) Delete(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func sharedTestHandler(svc *fakeSharedProductService) *Handler {
	return &Handler{
		sharedProductService: svc,
		orgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"member": orgs.RoleMember, "admin": orgs.RoleAdmin},
		}},
	}
}

// inOrg attaches a signed-in user and their active workspace.
func inOrg(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	return r.WithContext(ctx)
}

const validShareBody = `{"category":"kitchen appliance","name":"Kevinproof",` +
	`"description":"A coffee tin that recognises Kevin and locks.",` +
	`"vision":"Kevinproof becomes the reason the bean jar survives.",` +
	`"problem":"Beans vanish overnight and nobody admits to the grinder.",` +
	`"target_users":"office workers whose beans keep leaving with Kevin"}`

// TestPublishSharedProductRequiresAPerson is the containment gate on the one
// cross-tenant write path in OpenV: an agent run token or a host worker key
// authenticates for plenty of API calls, but neither can put text in front of
// every other workspace. Only a signed-in member of the active workspace can.
//
// Inventions publish automatically, so this is what keeps the pool
// accountable: a run cannot publish on its own credentials, and every row is
// attributable to the member whose browser sent it — which is what the daily
// cap and the takedown trail hang on.
func TestPublishSharedProductRequiresAPerson(t *testing.T) {
	cases := []struct {
		name     string
		decorate func(*http.Request) *http.Request
		wantCode int
	}{
		{"workspace member", func(r *http.Request) *http.Request { return inOrg(r, "member") }, http.StatusCreated},
		{"agent run token", func(r *http.Request) *http.Request {
			ctx := context.WithValue(r.Context(), ctxRun, &agentruns.Run{ID: "run-1"})
			ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
			return r.WithContext(ctx)
		}, http.StatusForbidden},
		{"host worker key", func(r *http.Request) *http.Request {
			return r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, "org-1"))
		}, http.StatusForbidden},
		{"non-member of the active workspace", func(r *http.Request) *http.Request {
			return inOrg(r, "stranger")
		}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeSharedProductService{}
			h := sharedTestHandler(svc)
			r := tc.decorate(httptest.NewRequest(http.MethodPost, "/api/v1/shared-products", strings.NewReader(validShareBody)))
			w := httptest.NewRecorder()
			h.PublishSharedProduct(w, r)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if got := len(svc.published); (tc.wantCode == http.StatusCreated) != (got == 1) {
				t.Fatalf("published %d products for status %d", got, w.Code)
			}
		})
	}
}

// TestPublishSharedProductAttributesToTheCaller: the client cannot choose who
// a product is attributed to — the org and user come from the session, so the
// rate limit and the takedown trail cannot be spoofed by the payload.
func TestPublishSharedProductAttributesToTheCaller(t *testing.T) {
	svc := &fakeSharedProductService{}
	h := sharedTestHandler(svc)

	body := `{"id":"forged","created_by_org":"someone-else","category":"toy","name":"Widget",` +
		`"description":"A toy that does a thing.","vision":"Widget wins.","problem":"Things.",` +
		`"target_users":"people who like things that do things"}`
	r := inOrg(httptest.NewRequest(http.MethodPost, "/api/v1/shared-products", strings.NewReader(body)), "member")
	w := httptest.NewRecorder()
	h.PublishSharedProduct(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	if svc.orgs[0] != "org-1" || svc.userIDs[0] != "member" {
		t.Errorf("attributed to %q/%q, want org-1/member", svc.orgs[0], svc.userIDs[0])
	}
	if svc.published[0].ID != "" || svc.published[0].CreatedByOrg != "" {
		t.Errorf("client-supplied id/author leaked into the domain: %+v", svc.published[0])
	}
}

// TestListSharedProductsHidesAuthors: the pool is cross-tenant, so the payload
// must carry the joke and nothing about who wrote it or where they work.
func TestListSharedProductsHidesAuthors(t *testing.T) {
	h := sharedTestHandler(&fakeSharedProductService{})
	r := inOrg(httptest.NewRequest(http.MethodGet, "/api/v1/shared-products", nil), "member")
	w := httptest.NewRecorder()
	h.ListSharedProducts(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, secret := range []string{"org-secret", "user-secret", "created_by"} {
		if strings.Contains(body, secret) {
			t.Errorf("list payload leaks %q: %s", secret, body)
		}
	}
	var got []map[string]interface{}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("payload is not a product list: %v", err)
	}
	if len(got) != 1 || got[0]["name"] != "Kevinproof" {
		t.Errorf("unexpected payload: %s", body)
	}
}

// TestSharedProductErrorStatuses maps the refusals a publisher can hit onto
// codes the UI can act on, rather than a blanket 500.
func TestSharedProductErrorStatuses(t *testing.T) {
	cases := []struct {
		err      error
		wantCode int
	}{
		{sharedproducts.ErrLinksNotAllowed, http.StatusBadRequest},
		{sharedproducts.ErrDisallowedText, http.StatusBadRequest},
		{sharedproducts.ErrTooLong, http.StatusBadRequest},
		{sharedproducts.ErrEmptyField, http.StatusBadRequest},
		{sharedproducts.ErrDuplicate, http.StatusConflict},
		{sharedproducts.ErrRateLimited, http.StatusTooManyRequests},
		{sharedproducts.ErrPoolFull, http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			h := sharedTestHandler(&fakeSharedProductService{publishErr: tc.err})
			r := inOrg(httptest.NewRequest(http.MethodPost, "/api/v1/shared-products", strings.NewReader(validShareBody)), "member")
			w := httptest.NewRecorder()
			h.PublishSharedProduct(w, r)
			if w.Code != tc.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tc.wantCode)
			}
		})
	}
}

// TestDeleteSharedProductIsAdminOnly: takedown is a platform-admin act. An
// ordinary member reporting is the path everyone else has.
func TestDeleteSharedProductIsAdminOnly(t *testing.T) {
	newReq := func(userID string, admin bool) *http.Request {
		r := mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/api/v1/shared-products/p1", nil), map[string]string{"id": "p1"})
		return r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID, IsAdmin: admin}))
	}

	svc := &fakeSharedProductService{}
	h := sharedTestHandler(svc)
	w := httptest.NewRecorder()
	h.DeleteSharedProduct(w, newReq("member", false))
	if w.Code != http.StatusForbidden {
		t.Errorf("member delete status = %d, want 403", w.Code)
	}
	if len(svc.deleted) != 0 {
		t.Errorf("a non-admin deleted %v", svc.deleted)
	}

	w = httptest.NewRecorder()
	h.DeleteSharedProduct(w, newReq("root", true))
	if w.Code != http.StatusNoContent {
		t.Errorf("admin delete status = %d, want 204", w.Code)
	}
	if len(svc.deleted) != 1 {
		t.Errorf("admin delete recorded %v", svc.deleted)
	}
}

// TestReportSharedProductNeedsAPerson: reports are attributed, which is what
// makes "several distinct reporters hide an entry" mean anything.
func TestReportSharedProductNeedsAPerson(t *testing.T) {
	svc := &fakeSharedProductService{}
	h := sharedTestHandler(svc)

	anon := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/v1/shared-products/p1/report", nil), map[string]string{"id": "p1"})
	w := httptest.NewRecorder()
	h.ReportSharedProduct(w, anon)
	if w.Code != http.StatusForbidden {
		t.Errorf("unauthenticated report status = %d, want 403", w.Code)
	}

	signed := mux.SetURLVars(
		inOrg(httptest.NewRequest(http.MethodPost, "/api/v1/shared-products/p1/report", nil), "member"),
		map[string]string{"id": "p1"},
	)
	w = httptest.NewRecorder()
	h.ReportSharedProduct(w, signed)
	if w.Code != http.StatusNoContent {
		t.Fatalf("report status = %d (body %q)", w.Code, w.Body.String())
	}
	if len(svc.reported) != 1 || svc.reported[0] != "p1/member" {
		t.Errorf("reported = %v, want [p1/member]", svc.reported)
	}
}
