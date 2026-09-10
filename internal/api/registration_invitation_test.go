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
	pending  map[string][]*invitations.Invitation // email -> pending
	byToken  map[string]*invitations.Invitation
	accepted []string // "token:userID" and "email:userID"
	created  []*invitations.Invitation
	revoked  []string
	// memberRoles is what the workspace already thinks of the account,
	// keyed "orgID:userID" — the real service reads it to leave an existing
	// role alone.
	memberRoles  map[string]string
	createErr    error
	lookupErr    error
	acceptAllErr error
}

func newFakeInviteService() *fakeInviteService {
	return &fakeInviteService{
		pending:     map[string][]*invitations.Invitation{},
		byToken:     map[string]*invitations.Invitation{},
		memberRoles: map[string]string{},
	}
}

func (f *fakeInviteService) Create(orgID, email, role string, invitedBy *string) (*invitations.Invitation, string, error) {
	if f.createErr != nil {
		return nil, "", f.createErr
	}
	if role == "" {
		role = orgs.RoleMember
	}
	// The real service reads the row back after saving it, so the names the
	// invitation mail needs are always populated; the fake mirrors that.
	inv := &invitations.Invitation{
		ID:        "inv-1",
		OrgID:     orgID,
		Email:     users.NormalizeEmail(email),
		Role:      role,
		OrgName:   "Test Workspace",
		InvitedBy: invitedBy,
		ExpiresAt: time.Now().Add(invitations.DefaultTTL),
	}
	if invitedBy != nil {
		inv.InvitedByName = "Ada Admin"
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
	return f.pending[users.NormalizeEmail(email)], nil
}

func (f *fakeInviteService) AcceptTokenForEmail(token, email, userID string) (*invitations.Acceptance, error) {
	inv, err := f.Lookup(token)
	if err != nil {
		return nil, err
	}
	if inv.Email != users.NormalizeEmail(email) {
		return nil, invitations.ErrEmailMismatch
	}
	f.accepted = append(f.accepted, token+":"+userID)
	delete(f.pending, inv.Email)
	role := inv.Role
	if existing := f.memberRoles[inv.OrgID+":"+userID]; existing != "" {
		// Mirrors the real service: an existing membership keeps its role.
		return &invitations.Acceptance{Invitation: inv, Role: existing, AlreadyMember: true}, nil
	}
	return &invitations.Acceptance{Invitation: inv, Role: role}, nil
}

func (f *fakeInviteService) AcceptAllForProviderVerifiedEmail(email, userID string) ([]*invitations.Acceptance, error) {
	email = users.NormalizeEmail(email)
	list := f.pending[email]
	if len(list) > 0 {
		f.accepted = append(f.accepted, email+":"+userID)
		delete(f.pending, email)
	}
	out := make([]*invitations.Acceptance, 0, len(list))
	for _, inv := range list {
		out = append(out, &invitations.Acceptance{Invitation: inv, Role: inv.Role})
	}
	return out, f.acceptAllErr
}

func (f *fakeInviteService) PurgeExpired(time.Time) error { return nil }

// invite adds a pending invitation for an address.
func (f *fakeInviteService) invite(orgID, email, role string) *invitations.Invitation {
	email = users.NormalizeEmail(email)
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
	return registerWithTokenReq(email, "")
}

// registerWithTokenReq posts a sign-up carrying an invite link's token, the
// way the login page does when it was opened with ?invite=.
func registerWithTokenReq(email, inviteToken string) *http.Request {
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "right", "name": "New", "invite_token": inviteToken,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(string(body)))
	r.RemoteAddr = "203.0.113.11:5555"
	r.Header.Set("Content-Type", "application/json")
	return r
}

// fakeMemberOrgs is the slice of orgs.Service the member/invite branch
// touches: who is already in the workspace, and whether it is a personal one.
type fakeMemberOrgs struct {
	orgs.Service
	roles    map[string]string // userID -> role
	personal bool
	added    []string // "orgID:userID:role"
}

func (f *fakeMemberOrgs) RoleInOrg(orgID, userID string) (string, error) {
	return f.roles[userID], nil
}

func (f *fakeMemberOrgs) AddMember(orgID, userID, role string) error {
	if f.personal {
		return orgs.ErrPersonalOrgMembers
	}
	f.added = append(f.added, orgID+":"+userID+":"+role)
	return nil
}

func (f *fakeMemberOrgs) Get(id string) (*orgs.Org, error) {
	orgType := orgs.TypeCompany
	if f.personal {
		orgType = orgs.TypePersonal
	}
	return &orgs.Org{ID: id, Name: "Test Workspace", OrgType: orgType}, nil
}

func newRegistrationHandler(policy string) (*Handler, *fakeLoginService, *fakeInviteService) {
	svc := &fakeLoginService{}
	invites := newFakeInviteService()
	return &Handler{
		userService:          svc,
		invitationService:    invites,
		registration:         policy,
		registerIPLimiter:    newRateLimiter(100, 1),
		authIPLimiter:        newRateLimiter(100, 1),
		authAccountLimiter:   newRateLimiter(100, 1),
		invitePreviewLimiter: newRateLimiter(100, 1),
		emailLinkBase:        "https://app.example.com",
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

// On a closed deployment the invitation LINK is the door — the pending row
// is not. Answering differently for an invited address would tell a prober
// who the admins have invited, and would let whoever learns an invited
// address register it first and sit on it.
func TestRegistrationClosedRefusesAnInvitedAddressWithoutTheLink(t *testing.T) {
	h, svc, invites := newRegistrationHandler(RegistrationClosed)
	invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)

	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("Invited@Example.com"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	// Byte for byte the answer an uninvited address gets, so the form is not
	// an oracle for who has been invited.
	if rec.Body.String() != closedRegistrationBody(t, h) {
		t.Errorf("an invited address gets a different refusal than a stranger:\n%s", rec.Body.String())
	}
	if svc.registered != 0 {
		t.Error("a refused registration must not create an account")
	}
	if len(invites.pending["invited@example.com"]) != 1 {
		t.Error("the invitation must still be waiting for its link")
	}
}

// closedRegistrationBody is what a plain stranger is told on a closed
// deployment, for comparing an invited address's refusal against.
func closedRegistrationBody(t *testing.T, h *Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("stranger-baseline@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baseline status = %d, want 403", rec.Code)
	}
	return rec.Body.String()
}

// The link admits its own address and only its own: a token that reached one
// mailbox cannot register a different address on a closed deployment.
func TestRegistrationClosedAdmitsOnlyTheLinksOwnAddress(t *testing.T) {
	h, svc, invites := newRegistrationHandler(RegistrationClosed)
	invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)

	rec := httptest.NewRecorder()
	h.Register(rec, registerWithTokenReq("Invited@Example.com", "tok-invited@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.registered != 1 {
		t.Errorf("registrations = %d, want 1", svc.registered)
	}

	rec = httptest.NewRecorder()
	h.Register(rec, registerWithTokenReq("squatter@example.com", "tok-invited@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("a link used for another address returned %d, want 403", rec.Code)
	}
	if svc.registered != 1 {
		t.Error("a link for another address must not create an account")
	}
}

// The security rule in one test: an address is not a proof of anything. Only
// the token from the invitation mail — or, later, the verification link —
// turns a pending invitation into a membership.
func TestRegisteringAnInvitedAddressGrantsNothingWithoutTheToken(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)

	rec := httptest.NewRecorder()
	h.Register(rec, registerReq("invited@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(invites.accepted) != 0 {
		t.Fatalf("registration alone joined the workspace: %v", invites.accepted)
	}

	// A token issued to a DIFFERENT address is no better: it grants nothing
	// to the account registering here.
	invites.invite("org-2", "someone-else@example.com", orgs.RoleAdmin)
	rec = httptest.NewRecorder()
	h.Register(rec, registerWithTokenReq("other@example.com", "tok-someone-else@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(invites.accepted) != 0 {
		t.Errorf("a token for another address was accepted: %v", invites.accepted)
	}
}

// With the token, registration does join the workspace: holding the link is
// proof the invited mailbox was read.
func TestRegisteringWithTheInviteTokenJoinsTheWorkspace(t *testing.T) {
	for _, policy := range []string{"", RegistrationClosed} {
		h, svc, invites := newRegistrationHandler(policy)
		invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)

		rec := httptest.NewRecorder()
		h.Register(rec, registerWithTokenReq("Invited@Example.com", "tok-invited@example.com"))
		if rec.Code != http.StatusOK {
			t.Fatalf("policy %q: status = %d, want 200: %s", policy, rec.Code, rec.Body.String())
		}
		if svc.registered != 1 {
			t.Errorf("policy %q: registrations = %d, want 1", policy, svc.registered)
		}
		if len(invites.accepted) != 1 {
			t.Errorf("policy %q: accepted = %v, want the token to be taken up", policy, invites.accepted)
		}
	}
}

// Confirming a verification link grants NO membership. The address it
// confirms is one the account asked the mail to be sent to (the
// change-of-address flow), so an attacker could point a verification mail at
// an address an admin had invited and be handed the workspace. "Verified"
// here proves the account can read that mailbox's link, not that the account
// is the person who was invited.
func TestVerifyingTheEmailGrantsNoMembership(t *testing.T) {
	h, svc, invites := newRegistrationHandler("")
	h.emailVerification = users.EmailVerificationPolicy{Required: true}
	svc.confirmed = &users.User{ID: "u-2", Email: "invited@example.com", EmailVerified: true}
	invites.invite("org-1", "invited@example.com", orgs.RoleMember)
	invites.invite("org-2", "invited@example.com", orgs.RoleAdmin)

	rec := httptest.NewRecorder()
	h.VerifyEmail(rec, jsonReq(http.MethodPost, "/api/v1/auth/verify-email", `{"token":"verify-me"}`, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(invites.accepted) != 0 {
		t.Errorf("verification joined workspaces: %v", invites.accepted)
	}
	if len(invites.pending["invited@example.com"]) != 2 {
		t.Error("the invitations must still be waiting for their links")
	}
}

// A provider-verified sign-in takes up the invitations waiting for the
// address — and a membership that could not be written is reported (the
// handler logs it) without failing the sign-in the person actually asked
// for. The invitation stays acceptable through its link.
func TestProviderVerifiedSignInJoinsAndSurvivesAFailure(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	invites.invite("org-1", "sso@example.com", orgs.RoleMember)
	invites.acceptAllErr = errors.New("one workspace is gone")

	// Returns normally: the caller has already signed the person in.
	h.acceptInvitationsForProviderVerifiedEmail("u-7", "SSO@Example.com")

	if len(invites.accepted) != 1 || invites.accepted[0] != "sso@example.com:u-7" {
		t.Errorf("accepted = %v, want the invitations that could be taken up", invites.accepted)
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
	h, svc, invites := newRegistrationHandler(RegistrationClosed)
	invites.invite("org-1", "invited@example.com", orgs.RoleMember)
	invites.lookupErr = errors.New("database is down")
	rec := httptest.NewRecorder()
	h.Register(rec, registerWithTokenReq("invited@example.com", "tok-invited@example.com"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if svc.registered != 0 {
		t.Error("an unanswerable lookup must not create an account")
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
	svc.sessions = map[string]*users.User{"cookie-1": {ID: "u-1", Email: "Member@Example.com"}}

	// The session's address is the invited one (however it is cased), so the
	// link converts.
	rec := httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"tok-member@example.com"}`, "cookie-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(invites.accepted) != 1 {
		t.Errorf("accepted = %v", invites.accepted)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["org_id"] != "org-1" || body["role"] != orgs.RoleMember || body["already_member"] != false {
		t.Errorf("body = %+v", body)
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

// Holding the link is not enough: the signed-in account must BE the invited
// address. Otherwise a forwarded link would put whoever is signed in on that
// browser into somebody else's workspace.
func TestAcceptInvitationRefusesAnotherAddress(t *testing.T) {
	h, svc, invites := newRegistrationHandler("")
	invites.invite("org-1", "invited@example.com", orgs.RoleAdmin)
	svc.sessions = map[string]*users.User{"cookie-2": {ID: "u-9", Email: "someone-else@example.com"}}

	rec := httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"tok-invited@example.com"}`, "cookie-2"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != ErrCodeInvitationEmailMismatch {
		t.Errorf("code = %q, want %q", body.Code, ErrCodeInvitationEmailMismatch)
	}
	// The refusal reaches whoever is signed in on this browser, who is not
	// necessarily the person the link was sent to: it must not name the
	// invited address (the preview endpoint tells the link's holder that).
	if strings.Contains(rec.Body.String(), "invited@example.com") {
		t.Errorf("the invited address leaked into the refusal: %s", rec.Body.String())
	}
	if len(invites.accepted) != 0 {
		t.Errorf("a mismatched account joined anyway: %v", invites.accepted)
	}
	if len(invites.pending["invited@example.com"]) != 1 {
		t.Error("a refused acceptance must leave the invitation pending")
	}
}

// An invitation never rewrites a role: an admin who follows a later "member"
// link stays an admin, and the answer says so.
func TestAcceptInvitationLeavesAnExistingRoleAlone(t *testing.T) {
	h, svc, invites := newRegistrationHandler("")
	invites.invite("org-1", "boss@example.com", orgs.RoleMember)
	invites.memberRoles["org-1:u-3"] = orgs.RoleAdmin
	svc.sessions = map[string]*users.User{"cookie-3": {ID: "u-3", Email: "boss@example.com"}}

	rec := httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"tok-boss@example.com"}`, "cookie-3"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["role"] != orgs.RoleAdmin || body["already_member"] != true {
		t.Errorf("body = %+v, want the role they already held", body)
	}
}

// Opening an invite link must never spend somebody's sign-in budget: the
// preview draws on its own bucket, so a person who reloads the link a dozen
// times can still sign in to the account they were invited to use.
func TestPreviewInvitationDoesNotSpendTheSignInBudget(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	invites.invite("org-1", "invited@example.com", orgs.RoleMember)
	h.authIPLimiter = newRateLimiter(1, 1)

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.PreviewInvitation(rec, muxReq(http.MethodGet, "/api/v1/auth/invitations/tok", "",
			map[string]string{"token": "tok-invited@example.com"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("preview %d: status = %d, want 200", i, rec.Code)
		}
	}
	// The one sign-in token is untouched.
	if ok, _ := h.authIPLimiter.check("203.0.113.12"); !ok {
		t.Error("previewing an invite link consumed the sign-in bucket")
	}

	// Its own bucket still bounds it.
	h.invitePreviewLimiter = newRateLimiter(1, 1)
	rec := httptest.NewRecorder()
	h.PreviewInvitation(rec, muxReq(http.MethodGet, "/api/v1/auth/invitations/tok", "",
		map[string]string{"token": "tok-invited@example.com"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("first preview status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.PreviewInvitation(rec, muxReq(http.MethodGet, "/api/v1/auth/invitations/tok", "",
		map[string]string{"token": "tok-invited@example.com"}))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second preview status = %d, want 429", rec.Code)
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

// The invitation email names the workspace and the person who sent it — the
// invitee has no membership yet to read either from, so they only ever see
// what Create put on the row.
func TestInvitationMailNamesTheWorkspaceAndInviter(t *testing.T) {
	h, _, _ := newRegistrationHandler("")
	mailer := newTestMailer()
	h.mailer = mailer
	admin := &users.User{ID: "u-admin", Email: "admin@example.com", Name: "Ada Admin", IsAdmin: true}

	rec := httptest.NewRecorder()
	h.CreateOrgInvitation(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-1/invitations",
		`{"email":"new@example.com","role":"member"}`, map[string]string{"id": "org-1"}), admin))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var resp invitationResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Emailed {
		t.Error("emailed = false with a working mailer")
	}
	if resp.Invitation.OrgName != "Test Workspace" || resp.Invitation.InvitedByName != "Ada Admin" {
		t.Errorf("the response does not name the workspace and inviter: %+v", resp.Invitation)
	}
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	if len(mailer.body) != 1 {
		t.Fatalf("mails sent = %d", len(mailer.body))
	}
	if !strings.Contains(mailer.body[0], "Test Workspace") || !strings.Contains(mailer.body[0], "Ada Admin") {
		t.Errorf("mail body names neither workspace nor inviter:\n%s", mailer.body[0])
	}
}

// Inviting into a personal workspace is refused before a row is written —
// a personal space cannot have members, so it cannot promise one.
func TestInvitingAPersonalWorkspaceIsRefused(t *testing.T) {
	h, _, invites := newRegistrationHandler("")
	invites.createErr = orgs.ErrPersonalOrgMembers
	h.orgService = &fakeMemberOrgs{roles: map[string]string{}, personal: true}
	admin := &users.User{ID: "u-admin", Email: "admin@example.com", IsAdmin: true}

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"invitations": h.CreateOrgInvitation,
		"members":     h.AddOrgMember,
	} {
		rec := httptest.NewRecorder()
		call(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-personal/"+name,
			`{"email":"nobody@example.com","role":"member"}`, map[string]string{"id": "org-personal"}), admin))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
		if len(invites.created) != 0 {
			t.Errorf("%s wrote an invitation into a personal workspace", name)
		}
	}
}

// Both entry points take the same branch: an address with an account joins
// now, one already in the workspace is a conflict, one with no account is
// invited. Only the statuses differ.
func TestInvitingAnAddressThatAlreadyHasAnAccount(t *testing.T) {
	h, svc, invites := newRegistrationHandler("")
	member := &users.User{ID: "u-9", Email: "known@example.com", Name: "Known"}
	svc.accounts = map[string]*users.User{"known@example.com": member}
	orgSvc := &fakeMemberOrgs{roles: map[string]string{}}
	h.orgService = orgSvc
	admin := &users.User{ID: "u-admin", Email: "admin@example.com", IsAdmin: true}

	// POST /orgs/{id}/invitations on an address that has an account adds the
	// membership instead of mailing a link nobody needs.
	rec := httptest.NewRecorder()
	h.CreateOrgInvitation(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-1/invitations",
		`{"email":"Known@example.com","role":"admin"}`, map[string]string{"id": "org-1"}), admin))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got orgs.Member
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserID != "u-9" || got.Role != orgs.RoleAdmin || got.UserEmail != "known@example.com" {
		t.Errorf("member = %+v", got)
	}
	if len(orgSvc.added) != 1 || orgSvc.added[0] != "org-1:u-9:admin" {
		t.Errorf("memberships = %v", orgSvc.added)
	}
	if len(invites.created) != 0 {
		t.Errorf("an account holder was sent an invitation: %v", invites.created)
	}

	// Already a member: a conflict from either endpoint, not a second row
	// and not a silent role rewrite.
	orgSvc.roles["u-9"] = orgs.RoleMember
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"invitations": h.CreateOrgInvitation,
		"members":     h.AddOrgMember,
	} {
		rec = httptest.NewRecorder()
		call(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-1/"+name,
			`{"email":"known@example.com","role":"admin"}`, map[string]string{"id": "org-1"}), admin))
		if rec.Code != http.StatusConflict {
			t.Errorf("%s status = %d, want 409: %s", name, rec.Code, rec.Body.String())
		}
	}
	if len(orgSvc.added) != 1 {
		t.Errorf("memberships after the conflicts = %v", orgSvc.added)
	}

	// An address with no account is still invited (201 here, 202 from the
	// members route).
	rec = httptest.NewRecorder()
	h.CreateOrgInvitation(rec, asUser(muxReq(http.MethodPost, "/api/v1/orgs/org-1/invitations",
		`{"email":"stranger@example.com","role":"member"}`, map[string]string{"id": "org-1"}), admin))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if len(invites.created) != 1 || invites.created[0].Email != "stranger@example.com" {
		t.Errorf("created = %v", invites.created)
	}
}

// Someone who already has an account and follows an invite link while signed
// out signs in first, then accepts the token: the handler pair has to work
// in that order, on the session the sign-in just issued.
func TestSignInThenAcceptTheInviteToken(t *testing.T) {
	h, svc, invites := newRegistrationHandler("")
	invites.invite("org-1", "member@example.com", orgs.RoleMember)
	svc.sessions = map[string]*users.User{"session-token": {ID: "u1", Email: "member@example.com"}}

	rec := httptest.NewRecorder()
	h.Login(rec, jsonReq(http.MethodPost, "/api/v1/auth/login",
		`{"email":"member@example.com","password":"right"}`, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
	}
	var session string
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("sign-in issued no session cookie")
	}
	if len(invites.accepted) != 0 {
		t.Errorf("signing in must not accept anything on its own: %v", invites.accepted)
	}

	rec = httptest.NewRecorder()
	h.AcceptInvitation(rec, jsonReq(http.MethodPost, "/api/v1/auth/invitations/accept",
		`{"token":"tok-member@example.com"}`, session))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(invites.accepted) != 1 || invites.accepted[0] != "tok-member@example.com:u1" {
		t.Errorf("accepted = %v", invites.accepted)
	}
}
