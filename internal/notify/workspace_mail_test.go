package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeNoticeStore is a workspace, its members and the notice claims, kept
// in memory with the repository's once-per-key rule.
type fakeNoticeStore struct {
	org        *orgs.Org
	members    []*orgs.Member
	claimed    map[string]string
	recorded   map[string]int
	released   []string
	listErr    error
	claimCalls int
}

func newFakeNoticeStore(members ...*orgs.Member) *fakeNoticeStore {
	return &fakeNoticeStore{
		org:      &orgs.Org{ID: "org-1", Name: "Acme\nLabs"},
		members:  members,
		claimed:  map[string]string{},
		recorded: map[string]int{},
	}
}

func (s *fakeNoticeStore) Get(id string) (*orgs.Org, error) { return s.org, nil }
func (s *fakeNoticeStore) ListMembers(orgID string) ([]*orgs.Member, error) {
	return s.members, s.listErr
}
func (s *fakeNoticeStore) ClaimNotice(orgID, key, subject string) (bool, error) {
	s.claimCalls++
	if _, ok := s.claimed[orgID+"|"+key]; ok {
		return false, nil
	}
	s.claimed[orgID+"|"+key] = subject
	return true, nil
}
func (s *fakeNoticeStore) RecordNotice(orgID, key string, recipients int) error {
	s.recorded[orgID+"|"+key] = recipients
	return nil
}
func (s *fakeNoticeStore) ReleaseNotice(orgID, key string) error {
	delete(s.claimed, orgID+"|"+key)
	s.released = append(s.released, key)
	return nil
}

// failingMailer refuses the addresses it names.
type failingMailer struct {
	captureMailer
	refuse map[string]bool
}

func (m *failingMailer) Send(to, subject, body string) error {
	if m.refuse[to] {
		return errors.New("mailbox unavailable")
	}
	return m.captureMailer.Send(to, subject, body)
}

func noticeDir() *fakeDir {
	return &fakeDir{byID: map[string]*users.User{
		"admin-1":  {ID: "admin-1", Email: "ada@example.com", Name: "Ada"},
		"admin-2":  {ID: "admin-2", Email: "bo@example.com", EmailNotifications: false},
		"member-1": optedIn("member-1", "max@example.com"),
		"admin-3":  {ID: "admin-3"},
	}}
}

func noticeMembers() []*orgs.Member {
	return []*orgs.Member{
		{UserID: "admin-1", Role: orgs.RoleAdmin},
		{UserID: "member-1", Role: orgs.RoleMember},
		{UserID: "admin-2", Role: orgs.RoleAdmin},
		{UserID: "admin-3", Role: orgs.RoleAdmin},
	}
}

var testNotice = WorkspaceNotice{
	Key:     "billing:payment_failed:sub_1:1700000000",
	Subject: "Payment failed for Acme",
	Body:    "The card was declined.",
	Path:    "/org/settings?tab=billing",
}

// A notice reaches every admin with an address, whatever their opt-out
// (a notice is a service message), and no member; asking again sends
// nothing, and the record says how many it reached.
func TestWorkspaceNoticeGoesToAdminsOnce(t *testing.T) {
	mailer := &captureMailer{enabled: true}
	store := newFakeNoticeStore(noticeMembers()...)
	w := NewWorkspaceMail(mailer, store, noticeDir(), "https://openv.app/")

	sent, err := w.SendToAdmins(context.Background(), "org-1", testNotice)
	if err != nil || !sent {
		t.Fatalf("first send = %v, %v; want sent", sent, err)
	}
	if got := strings.Join(mailer.to, ","); got != "ada@example.com,bo@example.com" {
		t.Fatalf("recipients = %s", got)
	}
	body := mailer.body[0]
	for _, want := range []string{
		"Hi Ada,\n\nThe card was declined.\n",
		"\nhttps://openv.app/org/settings?tab=billing\n",
		"you administer the OpenV workspace Acme Labs.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if !strings.HasPrefix(mailer.body[1], "Hi,\n\n") {
		t.Errorf("an admin with no name is greeted %q", strings.SplitN(mailer.body[1], "\n", 2)[0])
	}
	if store.recorded["org-1|"+testNotice.Key] != 2 {
		t.Fatalf("recorded = %v", store.recorded)
	}

	sent, err = w.SendToAdmins(context.Background(), "org-1", testNotice)
	if err != nil || sent || len(mailer.to) != 2 {
		t.Fatalf("second send = %v, %v with %d emails; want nothing sent", sent, err, len(mailer.to))
	}
}

// With mail off, or no admin to write to, nothing is sent and nothing is
// claimed, so the notice can still go out later.
func TestWorkspaceNoticeWithNobodyToTellIsNotRecorded(t *testing.T) {
	off := NewWorkspaceMail(&captureMailer{enabled: false}, newFakeNoticeStore(noticeMembers()...), noticeDir(), "")
	if off.Enabled() {
		t.Fatal("enabled with mail off")
	}
	if sent, err := off.SendToAdmins(context.Background(), "org-1", testNotice); sent || err != nil {
		t.Fatalf("mail off: %v, %v", sent, err)
	}

	store := newFakeNoticeStore(&orgs.Member{UserID: "member-1", Role: orgs.RoleMember}, &orgs.Member{UserID: "admin-3", Role: orgs.RoleAdmin})
	mailer := &captureMailer{enabled: true}
	w := NewWorkspaceMail(mailer, store, noticeDir(), "")
	if sent, err := w.SendToAdmins(context.Background(), "org-1", testNotice); sent || err != nil || len(mailer.to) != 0 {
		t.Fatalf("no admin address: %v, %v, %v", sent, err, mailer.to)
	}
	if store.claimCalls != 0 {
		t.Fatal("a notice nobody could receive was claimed")
	}
}

// A notice that reached no admin releases its claim and is sent on the next
// call; one that reached some is kept, and err names the failure.
func TestWorkspaceNoticeFailures(t *testing.T) {
	store := newFakeNoticeStore(noticeMembers()...)
	mailer := &failingMailer{captureMailer: captureMailer{enabled: true}, refuse: map[string]bool{"ada@example.com": true, "bo@example.com": true}}
	w := NewWorkspaceMail(mailer, store, noticeDir(), "")
	if sent, err := w.SendToAdmins(context.Background(), "org-1", testNotice); sent || err == nil {
		t.Fatalf("all refused: %v, %v", sent, err)
	}
	if len(store.released) != 1 || len(store.claimed) != 0 {
		t.Fatalf("claim not released: %v %v", store.released, store.claimed)
	}

	mailer.refuse = map[string]bool{"bo@example.com": true}
	sent, err := w.SendToAdmins(context.Background(), "org-1", testNotice)
	if !sent || err == nil {
		t.Fatalf("one refused: %v, %v", sent, err)
	}
	if store.recorded["org-1|"+testNotice.Key] != 1 {
		t.Fatalf("recorded = %v", store.recorded)
	}
	mailer.refuse = nil
	if sent, _ := w.SendToAdmins(context.Background(), "org-1", testNotice); sent {
		t.Fatal("a notice that reached an admin was sent again")
	}
}

// A notice without a key, subject or body is refused before anything is read.
func TestWorkspaceNoticeMalformed(t *testing.T) {
	w := NewWorkspaceMail(&captureMailer{enabled: true}, newFakeNoticeStore(noticeMembers()...), noticeDir(), "")
	for _, n := range []WorkspaceNotice{
		{Subject: "s", Body: "b"},
		{Key: "k", Body: "b"},
		{Key: "k", Subject: "s"},
		{Key: strings.Repeat("k", 201), Subject: "s", Body: "b"},
	} {
		if _, err := w.SendToAdmins(context.Background(), "org-1", n); !errors.Is(err, ErrNoticeMalformed) {
			t.Errorf("%+v: err = %v", n, err)
		}
	}
}
