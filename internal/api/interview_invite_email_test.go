package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// inviteInterviews holds one open interview and records the invites made.
type inviteInterviews struct {
	interviews.Service
	labels []string
}

func (f *inviteInterviews) GetInterview(id string) (*interviews.Interview, error) {
	if id != "iv-1" {
		return nil, interviews.ErrInterviewNotFound
	}
	return &interviews.Interview{ID: "iv-1", ProjectID: "proj-1", Name: "Brake pedal feel", Status: interviews.InterviewStatusOpen}, nil
}

func (f *inviteInterviews) CreateInvite(interviewID, label string, expiresAt *time.Time) (*interviews.Invite, string, error) {
	f.labels = append(f.labels, label)
	return &interviews.Invite{ID: "inv-1", InterviewID: interviewID, InviteeLabel: label, ExpiresAt: expiresAt}, "raw-token", nil
}

func interviewInviteFixture(t *testing.T, plan string, mailer *testMailer) (*Handler, *inviteInterviews) {
	t.Helper()
	svc := &inviteInterviews{}
	h := newTestHandler(t, func(h *Handler) {
		h.InterviewService = svc
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}}
		h.OrgService = &fakeOrgService{plan: plan, roles: map[string]map[string]string{
			"org-1": {"u-ed": orgs.RoleMember},
		}}
		h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"u-ed": members.RoleEditor},
		}}
		if mailer != nil {
			h.Mailer = mailer
		}
		h.EmailLinkBase = "https://app.example.com/"
	})
	return h, svc
}

func postInterviewInvite(h *Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/iv-1/invites", strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"id": "iv-1"})
	r = asUser(r, &users.User{ID: "u-ed", Name: "Ada Lovelace", Email: "ada@example.com"})
	w := httptest.NewRecorder()
	h.CreateInterviewInvite(w, r)
	return w
}

// An address on an interview invite has the link emailed to it, named by
// the person who sent it, and names the invite in the list when it has no
// label of its own.
func TestInterviewInviteEmailsTheLink(t *testing.T) {
	mailer := newTestMailer()
	h, svc := interviewInviteFixture(t, "", mailer)
	w := postInterviewInvite(h, `{"email":" Pat@Example.com "}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Path    string `json:"path"`
		Emailed *bool  `json:"emailed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Emailed == nil || !*resp.Emailed {
		t.Fatalf("emailed = %v, want true", resp.Emailed)
	}
	select {
	case <-mailer.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no email was sent")
	}
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	if mailer.to[0] != "pat@example.com" {
		t.Errorf("sent to %q, want the normalized address", mailer.to[0])
	}
	body := mailer.body[0]
	for _, want := range []string{"Ada Lovelace", "Brake pedal feel", "https://app.example.com/interview/raw-token"} {
		if !strings.Contains(body, want) {
			t.Errorf("email body lacks %q:\n%s", want, body)
		}
	}
	if len(svc.labels) != 1 || svc.labels[0] != "pat@example.com" {
		t.Errorf("invite labels = %v, want the address", svc.labels)
	}
}

// A link-only invite answers as it always has: no emailed key, no mail.
func TestInterviewInviteWithoutAnAddressSendsNothing(t *testing.T) {
	mailer := newTestMailer()
	h, svc := interviewInviteFixture(t, "", mailer)
	w := postInterviewInvite(h, `{"invitee_label":"Pat"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if strings.Contains(w.Body.String(), "emailed") {
		t.Errorf("a link-only invite answered emailed: %s", w.Body.String())
	}
	if len(mailer.to) != 0 {
		t.Errorf("sent %d emails, want none", len(mailer.to))
	}
	if svc.labels[0] != "Pat" {
		t.Errorf("label = %q, want Pat", svc.labels[0])
	}
}

// With no mailer the invite is still made, and emailed false tells the
// member to send the link themselves.
func TestInterviewInviteWithNoMailerSaysNotEmailed(t *testing.T) {
	h, svc := interviewInviteFixture(t, "", nil)
	w := postInterviewInvite(h, `{"email":"pat@example.com","invitee_label":"Pat"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"emailed":false`) {
		t.Errorf("body = %s, want emailed false", w.Body.String())
	}
	if len(svc.labels) != 1 || svc.labels[0] != "Pat" {
		t.Errorf("labels = %v, want the given label kept", svc.labels)
	}
}

func TestInterviewInviteRefusesAnAddressItCannotSendTo(t *testing.T) {
	for _, email := range []string{"not-an-address", "Pat <pat@example.com>", "a@example.com, b@example.com"} {
		mailer := newTestMailer()
		h, svc := interviewInviteFixture(t, "", mailer)
		body, _ := json.Marshal(map[string]string{"email": email})
		w := postInterviewInvite(h, string(body))
		wantAnswer(t, w, http.StatusBadRequest, "a valid email is required")
		if len(svc.labels) != 0 || len(mailer.to) != 0 {
			t.Errorf("%q: an invite was made or mailed", email)
		}
	}
}

// On the stable channel before the release that carries email-invites, an
// address is refused; a link-only invite still works.
func TestInterviewInviteEmailIsGated(t *testing.T) {
	mailer := newTestMailer()
	h, svc := interviewInviteFixture(t, orgs.PlanBusiness, mailer)
	wantAnswer(t, postInterviewInvite(h, `{"email":"pat@example.com"}`), http.StatusForbidden, featureGateMessage)
	if len(svc.labels) != 0 || len(mailer.to) != 0 {
		t.Error("a gated invite was made or mailed")
	}
	if w := postInterviewInvite(h, `{}`); w.Code != http.StatusCreated {
		t.Errorf("link-only invite: status = %d, want 201", w.Code)
	}
}
