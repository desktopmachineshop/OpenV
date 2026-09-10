package api

// Handler-level cover for the registration policy, invitation endpoints and
// password change (REQ-95, REQ-99). The services are faked: what is under
// test here is the HTTP contract — which status, which code, and what the
// handler does to the account on the way through.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeInviteService is the slice of invitations.Service the handlers touch.
type fakeInviteService struct {
	pending    map[string][]*invitations.Invitation // email -> pending
	byToken    map[string]*invitations.Invitation
	accepted   []string // "token:userID" and "email:userID"
	created    []*invitations.Invitation
	revoked    []string
	createErr  error
	lookupErr  error
	pendingErr error
}

func newFakeInviteService() *fakeInviteService {
	return &fakeInviteService{
		pending: map[string][]*invitations.Invitation{},
		byToken: map[string]*invitations.Invitation{},
	}
}

func (f *fakeInviteService) Create(orgID, email, role string, invitedBy *string) (*invitations.Invitation, string, error) {
	if f.createErr != nil {
		return nil, "", f.createErr
	}
	if role == "" {
		role = orgs.RoleMember
	}
	inv := &invitations.Invitation{
		ID:        "inv-1",
		OrgID:     orgID,
		Email:     invitations.NormalizeEmail(email),
		Role:      role,
		OrgName:   "Test Workspace",
		ExpiresAt: time.Now().Add(invitations.DefaultTTL),
	}
	f.created = append(f.created, inv)
	f.byToken["tok-"+inv.Email] = inv
	return inv, "tok-" + inv.Email, nil
}

func (f *fakeInviteService) ListPending(orgID string) ([]*invitations.Invitation, error) {
	var out []*invitations.Invitation
	for _, list := range f.pending {
		for _, inv := range list {
			if inv.OrgID == orgID {
				out = append(out, inv)
			}
		}
	}
	return out, nil
}

func (f *fakeInviteService) Revoke(orgID, invID string) error {
	if invID == "missing" {
		return invitations.ErrNotFound
	}
	f.revoked = append(f.revoked, orgID+":"+invID)
	return nil
}

func (f *fakeInviteService) Lookup(token string) (*invitations.Invitation, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	if inv := f.byToken[token]; inv != nil {
		return inv, nil
	}
	return nil, invitations.ErrInvalidToken
}

func (f *fakeInviteService) PendingForEmail(email string) ([]*invitations.Invitation, error) {
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	return f.pending[invitations.NormalizeEmail(email)], nil
}

func (f *fakeInviteService) AcceptToken(token, userID string) (*invitations.Invitation, error) {
	inv, err := f.Lookup(token)
	if err != nil {
		return nil, err
	}
	f.accepted = append(f.accepted, token+":"+userID)
	return inv, nil
}

func (f *fakeInviteService) AcceptAllForEmail(email, userID string) ([]*invitations.Invitation, error) {
	email = invitations.NormalizeEmail(email)
	list := f.pending[email]
	if len(list) > 0 {
		f.accepted = append(f.accepted, email+":"+userID)
		delete(f.pending, email)
	}
	return list, nil
}

func (f *fakeInviteService) PurgeExpired(time.Time) error { return nil }

// invite adds a pending invitation for an address.
func (f *fakeInviteService) invite(orgID, email, role string) *invitations.Invitation {
	email = invitations.NormalizeEmail(email)
	inv := &invitations.Invitation{
		ID: "inv-" + email, OrgID: orgID, Email: email, Role: role,
		OrgName: "Test Workspace", ExpiresAt: time.Now().Add(time.Hour),
	}
	f.pending[email] = append(f.pending[email], inv)
	f.byToken["tok-"+email] = inv
	return inv
}

// registerReq posts a sign-up. The password is the one fakeLoginService
// accepts, since Register signs the new account straight in.
func registerReq(email string) *http.Request {
	body, _ := json.Marshal(map[string]string{"email": email, "password": "right", "name": "New"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(string(body)))
	r.RemoteAddr = "203.0.113.11:5555"
	r.Header.Set("Content-Type", "application/json")
	return r
}

func newRegistrationHandler(policy string) (*Handler, *fakeLoginService, *fakeInviteService) {
	svc := &fakeLoginService{}
	invites := newFakeInviteService()
	return &Handler{
		userService:        svc,
		invitationService:  invites,
		registration:       policy,
		registerIPLimiter:  newRateLimiter(100, 1),
		authIPLimiter:      newRateLimiter(100, 1),
		authAccountLimiter: newRateLimiter(100, 1),
		emailLinkBase:      "https://app.example.com",
	}, svc, invites
}

func TestRegistrationOpenByDefault(t *testing.T) {
	h, svc, _ := newRegistrationHandler("")
	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("stranger@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.registered != 1 {
		t.Errorf("registrations = %d, want 1", svc.registered)
	}
}

func TestRegistrationClosedRefusesAnUninvitedAddress(t *testing.T) {
	h, svc, _ := newRegistrationHandler(RegistrationClosed)
	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("stranger@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != ErrCodeRegistrationClosed || body.Error != "registration is closed" {
		t.Errorf("body = %+v", body)
	}
	if svc.registered != 0 {
		t.Error("a refused registration must not create an account")
	}
}

func TestRegistrationClosedAdmitsAnInvitedAddress(t *testing.T) {
	h, svc, invites := newRegistrationHandler(RegistrationClosed)
	invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)

	// The address is matched however it is typed.
	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("Invited@Example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.registered != 1 {
		t.Errorf("registrations = %d, want 1", svc.registered)
	}
	// Registering accepts the invitation, so the new account lands in the
	// workspace that invited it rather than an empty personal space.
	if len(invites.accepted) != 1 {
		t.Errorf("accepted = %v, want the invitation to be taken up", invites.accepted)
	}
}

// With no invitation service to consult, a closed deployment says no rather
// than falling open.
func TestRegistrationClosedWithoutInvitationsRefuses(t *testing.T) {
	h, _, _ := newRegistrationHandler(RegistrationClosed)
	h.invitationService = nil
	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("someone@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// A lookup failure on a closed deployment is not a reason to let someone in.
func TestRegistrationClosedFailsSafeOnLookupError(t *testing.T) {
	h, _, invites := newRegistrationHandler(RegistrationClosed)
	invites.pendingErr = errors.New("database is down")
	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("invited@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAuthPolicyEndpoint(t *testing.T) {
	for _, tc := range []struct{ configured, want string }{
		{"", RegistrationOpen},
		{RegistrationOpen, RegistrationOpen},
		{RegistrationClosed, RegistrationClosed},
	} {
		h, _, _ := newRegistrationHandler(tc.configured)
		rec := httptest.NewRecorder()
		h.AuthPolicy(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/policy", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["registration"] != tc.want {
			t.Errorf("registration = %q for %q, want %q", body["registration"], tc.configured, tc.want)
		}
	}
}

// --- invitation endpoints --------------------------------------------------

// muxReq attaches route variables the way gorilla/mux does, so a handler can
// be driven without standing up the whole router.
func muxReq(method, path, body string, vars map[string]string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.RemoteAddr = "203.0.113.12:6666"
	return mux.SetURLVars(r, vars)
}

// asUser puts an authenticated platform admin on the request, which passes
// requireOrgRole without an org service.
func asUser(r *http.Request, user *users.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxUser, user))
}

func TestPreviewInvitationRevealsOnlyTheInvitation(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)

	rec := httptest.NewRecorder()
	h.PreviewInvitation(rec, muxReq(http.MethodGet, "/api/v1/auth/invitations/tok", "",
		map[string]string{"token": "tok-invited@example.com"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["email"] != "invited@example.com" || body["role"] != orgs.RoleAdmin || body["org_name"] != "Test Workspace" {
		t.Errorf("body = %+v", body)
	}
	// Nothing about the invitation's identity leaks.
	if _, ok := body["id"]; ok {
		t.Error("the preview must not carry the invitation id")
	}

	// Every failure is one 404.
	rec = httptest.NewRecorder()
	h.PreviewInvitation(rec, muxReq(http.MethodGet, "/api/v1/auth/invitations/x", "",
		map[string]string{"token": "no-such-token"}))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown token status = %d, want 404", rec.Code)
	}
}

func TestCreateAndRevokeInvitation(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	admin := &users.User{ID: "u-admin", Email: "admin@example.com", IsAdmin: true}

	rec := httptest.NewRecorder()
	h.CreateOrgInvitation(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-1/invitations",
		`{"email":"New@Example.com","role":"member"}`, map[string]string{"id": "org-1"}), admin))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var resp invitationResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Invitation.Email != "new@example.com" {
		t.Errorf("email = %q", resp.Invitation.Email)
	}
	// The one-time link is handed back so an admin on a mailer-less
	// deployment can pass it on, and it points at the login page.
	if !strings.HasPrefix(resp.Link, "https://app.example.com/login?invite=") {
		t.Errorf("link = %q", resp.Link)
	}
	if resp.Emailed {
		t.Error("emailed must be false with no mailer configured")
	}
	// The token itself is never part of the invitation body.
	if strings.Contains(rec.Body.String(), `"token_hash"`) {
		t.Error("the token hash must not be serialized")
	}

	rec = httptest.NewRecorder()
	h.RevokeOrgInvitation(rec, asUser(muxReq(http.MethodDelete, "/api/v1/orgs/org-1/invitations/inv-1", "",
		map[string]string{"id": "org-1", "invId": "inv-1"}), admin))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", rec.Code)
	}
	if len(invites.revoked) != 1 || invites.revoked[0] != "org-1:inv-1" {
		t.Errorf("revoked = %v", invites.revoked)
	}

	rec = httptest.NewRecorder()
	h.RevokeOrgInvitation(rec, asUser(muxReq(http.MethodDelete, "/api/v1/orgs/org-1/invitations/missing", "",
		map[string]string{"id": "org-1", "invId": "missing"}), admin))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown invitation status = %d, want 404", rec.Code)
	}
}

func TestInvitationEndpointsRefuseAnAnonymousCaller(t *testing.T) {
	h, _, _ := newRegistrationHandler("")
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"create": h.CreateOrgInvitation,
		"list":   h.ListOrgInvitations,
		"revoke": h.RevokeOrgInvitation,
	} {
		rec := httptest.NewRecorder()
		call(rec, muxReq(http.MethodPost, "/api/v1/orgs/org-1/invitations", `{"email":"a@b.com"}`,
			map[string]string{"id": "org-1", "invId": "inv-1"}))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", name, rec.Code)
		}
	}
}

func TestAcceptInvitationJoinsTheSignedInAccount(t *testing.T) {
	h, svc, invites := newRegistrationHandler("")
	invites.invite("org-1", "member@example.com", orgs.RoleMember)
	svc.sessions = map[string]*users.User{"cookie-1": {ID: "u-1", Email: "member@example.com"}}

	rec := httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"tok-member@example.com"}`, "cookie-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(invites.accepted) != 1 {
		t.Errorf("accepted = %v", invites.accepted)
	}

	// No session: nothing is joined.
	rec = httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"tok-member@example.com"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous accept status = %d, want 401", rec.Code)
	}

	// An unusable link is one 404.
	rec = httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"nope"}`, "cookie-1"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("bad token status = %d, want 404", rec.Code)
	}
}

// AddOrgMember no longer dead-ends on an address with no account: it invites.
func TestAddOrgMemberInvitesAnAddressWithNoAccount(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	h.userService = &fakeLoginService{}
	admin := &users.User{ID: "u-admin", Email: "admin@example.com", IsAdmin: true}

	rec := httptest.NewRecorder()
	h.AddOrgMember(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-1/members",
		`{"email":"nobody@example.com","role":"member"}`, map[string]string{"id": "org-1"}), admin))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if len(invites.created) != 1 || invites.created[0].Email != "nobody@example.com" {
		t.Errorf("created = %v", invites.created)
	}
}
